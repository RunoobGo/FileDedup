package ops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"filededup/internal/fsid"
)

// MoveFile 移动文件至 targetDir（M3-T04）：
//   - 同卷：os.Rename 原子完成
//   - 跨卷（EXDEV）：复制（校验 size + fsync + 还原元数据）后删源；
//     任何失败保留源文件不删（安全优先）
//   - 目标重名：name_1.ext 递增（file-deduplicator 风格）
//
// 返回目标完整路径。
func MoveFile(src, targetDir string) (string, error) {
	if targetDir == "" {
		return "", fmt.Errorf("未指定目标目录")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	dst := uniqueDst(targetDir, filepath.Base(src))

	if err := os.Rename(src, dst); err == nil {
		return dst, nil // 同卷快路径
	} else if !isCrossDevice(err) {
		return "", fmt.Errorf("移动失败: %w", err)
	}

	// 跨卷：复制 → 校验 → 还原元数据 → 删源
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if err := copyVerify(src, dst, st); err != nil {
		os.Remove(dst) // 清理半成品
		return "", err
	}
	if err := os.Remove(src); err != nil {
		return dst, fmt.Errorf("已复制但删除源失败（两份并存）: %w", err)
	}
	return dst, nil
}

// HardlinkMerge 硬链接合并（M3-T05）：冗余路径替换为指向 keep 的硬链接。
// 仅同卷可用（跨卷时 os.Link 天然失败兜底）。
//
// H2：keepID/dupID 来自 VerifyFile 通过校验那一刻的 fstat。临时硬链接建立后
// 复核 tmp 的 inode == keepID（窗口内 keep 路径被替换时，链接会指向非预期
// 文件）；替换 dup 前复核 dup 路径仍指向 dupID。零值 ID（未解析平台）跳过。
func HardlinkMerge(keep, dup string, keepID, dupID fsid.ID) error {
	if keep == dup {
		return fmt.Errorf("同一路径")
	}
	// 先建指向 keep 的临时硬链接，再把原 dup 备份后原子替换为硬链接。
	// 任一步失败都能把 dup 恢复为原始文件，杜绝「删 dup 后改名失败」导致的数据丢失。
	tmp := dup + ".fdd-tmp"
	_ = os.Remove(tmp) // 清理上次残留
	if err := os.Link(keep, tmp); err != nil {
		return fmt.Errorf("硬链接失败（可能跨卷或权限）: %w", err)
	}
	if !identityStill(tmp, keepID) {
		_ = os.Remove(tmp)
		return fmt.Errorf("保留源在校验后被替换（inode 已变化），已拦截（S1）")
	}
	if !identityStill(dup, dupID) {
		_ = os.Remove(tmp)
		return fmt.Errorf("目标文件在校验后被替换（inode 已变化），已拦截（S1）")
	}
	backup := dup + ".fdd-old"
	_ = os.Remove(backup)
	if err := hardlinkRename(dup, backup); err != nil {
		// 无法把原 dup 移走（极罕见）：清理临时硬链接，dup 原样保留（安全）。
		_ = os.Remove(tmp)
		return err
	}
	if err := hardlinkRename(tmp, dup); err != nil {
		// 替换失败：尽力把备份恢复回 dup 路径，并清理临时硬链接。
		_ = hardlinkRename(backup, dup)
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(backup) // 合并成功：删除原独立副本
	return nil
}

// copyVerify 复制并校验（size + fsync），随后还原权限位与修改时间。
//
// P2：跨卷复制路径此前用 os.Create（0666&^umask）落地，可执行/只读等模式位
// 会丢失；目标 mtime 也会变成"现在"，导致后续按 (path,size,mtime) 命中哈希缓存
// 与保留策略全部失效。内容校验通过后才还原元数据——还原失败不作为整体失败
// （数据已在目标处），单独返回错误供上层记录。
func copyVerify(src, dst string, st os.FileInfo) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	if err := copyAndSync(sf, dst, st.Size()); err != nil {
		return err
	}
	return restoreMeta(dst, st)
}

// copyAndSync 复制到 dst 并校验字节数、落盘。
func copyAndSync(sf io.Reader, dst string, size int64) error {
	df, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(df, sf)
	if err != nil {
		df.Close()
		return err
	}
	if n != size {
		df.Close()
		return fmt.Errorf("复制不完整 %d/%d", n, size)
	}
	if err := df.Sync(); err != nil {
		df.Close()
		return err
	}
	return df.Close()
}

// restoreMeta 还原权限位与修改时间。umask 只会让新建文件更严格，
// Chmod 到源模式是放宽方向，root 与普通用户均可成功。
// 属主（uid/gid）与 xattr/ACL 不还原：跨卷移动到用户目录的场景下强行 chown
// 需要特权且可能覆盖目标卷已有策略，超出本功能范围。
func restoreMeta(dst string, st os.FileInfo) error {
	if err := os.Chmod(dst, st.Mode().Perm()); err != nil {
		return err
	}
	// 源 mtime 以纳秒精度记录（FileEntry.ModTime），此处还原到亚秒精度：
	// 哈希缓存键使用 ns，缓存会在下次扫描未命中后以新 mtime 重新写入，
	// 属可接受的一次性回退，好过把"刚移动的重复文件"再当作扫描后被修改。
	return os.Chtimes(dst, time.Now(), st.ModTime())
}

// uniqueDst 目标重名递增：name.ext → name_1.ext → name_2.ext ...
func uniqueDst(dir, name string) string {
	dst := filepath.Join(dir, name)
	if !dstExists(dst) {
		return dst
	}
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	for i := 1; ; i++ {
		dst = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
		if !dstExists(dst) {
			return dst
		}
	}
}

// dstExists 仅 ENOENT 视为不存在；其他 Stat 错误（权限等）也放行该名——
// 后续 Create/Rename 会暴露真实错误，避免持续错误导致无限递增死循环。
func dstExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// isCrossDevice 仅把真实的 EXDEV 判定为跨卷。
//
// P2：旧实现把所有 *os.LinkError 都当作跨卷并回退到「复制+删源」，
// 于是 EPERM/EACCES/EDQUOT 之类的失败会在目标卷产生一份源文件副本，
// 表面上"移动成功"，实际上是把权限问题伪装成了成功——属于越权兜底。
// 现在非 EXDEV 直接返回错误，由操作层记为 Failed。
func isCrossDevice(err error) bool {
	var le *os.LinkError
	if !errors.As(err, &le) {
		return false
	}
	return errors.Is(le.Err, syscall.EXDEV) || isCrossDeviceExtra(le.Err)
}

// hardlinkRename 默认等于 os.Rename，测试可临时替换以模拟重命名失败，
// 用于验证 HardlinkMerge 的回滚路径不会丢失数据。
var hardlinkRename = os.Rename
