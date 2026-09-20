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
//
// 2026-09-19（缺陷：Windows 上"显示已执行但链接未生效"）：
// 此前本函数**只要有一步没报错就返回 nil**，从不确认链接真的建立了。
// 而 Windows 上 os.Link → CreateHardLinkW **仅 NTFS 支持**（MSDN 原文：
// "only supported on the NTFS file system"，且 ReFS 不支持、exFAT/FAT 完全不支持），
// 跨卷也一律失败。一旦平台/卷不支持，上层却把它记为 done 并累加"已释放空间"，
// 用户看到"执行成功"但文件夹总占用不变、改一个文件另一个不跟着变。
// 现在收尾处**强制复核**：dup 与 keep 必须真的互为同一文件（同 inode /
// 同 64 位文件索引）。对不上就回滚成原独立文件并报错——宁可显式失败，
// 也不留下"假装成功"的假账。
func HardlinkMerge(keep, dup string, keepID, dupID fsid.ID) error {
	if keep == dup {
		return fmt.Errorf("同一路径")
	}
	// 先建指向 keep 的临时硬链接，再把原 dup 备份后原子替换为硬链接。
	// 任一步失败都能把 dup 恢复为原始文件，杜绝「删 dup 后改名失败」导致的数据丢失。
	tmp := dup + FddTempSuffix
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
	backup := dup + FddOldSuffix
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
	// 收尾复核（见函数头注释）：确认 dup 现在真的是 keep 的那个文件。
	// 这里的证据是「同文件」而非「无错误」——两者在 Windows 上不等价。
	if err := verifyHardlinked(keep, dup); err != nil {
		// 未真正建立链接：把 dup 还原为原来的独立文件，不留假成功的账。
		if rerr := hardlinkRename(dup, backup+".undo"); rerr == nil {
			if berr := hardlinkRename(backup, dup); berr != nil {
				// 还原失败：至少保证两份都存在（数据不丢），并明确指出残留位置。
				return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失）",
					err, backup+".undo")
			}
			_ = os.Remove(backup + ".undo")
			return err
		}
		return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失）", err, backup)
	}
	// 合并成功：删除原独立副本。
	//
	// 2026-09-19（缺陷：残留 .fdd-old 污染后续扫描）：此处原先写作
	// `_ = os.Remove(backup)`，把删除失败**静默吞掉**。而 Windows 上
	// 该失败很常见：杀软/索引器/资源管理器预览持有句柄时删除会返回
	// ERROR_SHARING_VIOLATION。此时链接虽已建立，但一份与原文件逐字节
	// 相同的 .fdd-old 被永久留在用户目录里；由于它以用户文件名开头、
	// 不以 "." 开头，扫描器的隐藏跳过规则对它无效 → 下次扫描必然把它
	// 与被保留的文件配成一个"重复组"。用户看到的就是
	// 「明明做过硬链接合并，重扫还是有一堆重复文件」。
	//
	// 处置：链接已成立，不把整个操作判失败（那会让用户以为没合并）；
	// 但把残留如实回报，由上层提示用户，且扫描侧已同步忽略该名字
	// （IsWorkTempName），双重兜底。
	if err := workTempRemove(backup); err != nil {
		_ = os.Remove(tmp) // 确保临时硬链接不残留（本已不指向任何用户可见名）
		return &ResidueError{Path: backup, Err: err}
	}
	return nil
}

// ResidueError 表示「操作已成功，但有一个应用工作临时文件未能清除」。
//
// 之所以单独成类型：调用方需要区分「操作失败」与「操作成功但有残留」。
// 前者要回滚、要计失败；后者文件已经合并到位，多留了一个内部临时文件，
// 属于需要提示但不应推翻结果的情况。
type ResidueError struct {
	Path string
	Err  error
}

func (e *ResidueError) Error() string {
	return fmt.Sprintf("合并已完成，但临时文件 %s 未能删除（可能被其他程序占用），"+
		"请手动删除；它不会影响链接本身，扫描也已自动忽略该名字", e.Path)
}

func (e *ResidueError) Unwrap() error { return e.Err }

// verifyHardlinked 确认 dup 与 keep 现在指向同一份物理文件。
//
// 这是**平台无关**的终局判据，不依赖 fsid.ID 是否解析得出来：
//   - os.SameFile 比较 os.FileInfo 的 (dev, ino)，在 unix 上是 inode、
//     在 Windows 上由 Go 用文件索引填充——正是硬链接成立的定义。
//
// 之所以必须显式复核：os.Link/os.Rename 系列在 Windows 上有若干"语义近似但不等价"的
// 成功路径（卷不支持、重定向、Filter 驱动介入），仅凭"没返回错误"不足以断言
// 链接已建立。这里补上唯一的、可证伪的终态检查。
func verifyHardlinked(keep, dup string) error {
	ki, err := os.Stat(keep)
	if err != nil {
		return fmt.Errorf("硬链接复核失败：无法读取保留源 %s: %w", keep, err)
	}
	di, err := os.Stat(dup)
	if err != nil {
		return fmt.Errorf("硬链接复核失败：无法读取目标 %s: %w", dup, err)
	}
	if !ki.Mode().IsRegular() || !di.Mode().IsRegular() {
		return fmt.Errorf("硬链接复核失败：keep/dup 不都是普通文件")
	}
	if !os.SameFile(ki, di) {
		return fmt.Errorf("硬链接未生效：%s 与 %s 仍是两个独立文件"+
			"（该卷可能不支持硬链接，如 exFAT/FAT/ReFS，或两者不在同一卷）", dup, keep)
	}
	// 尺寸一致（同文件必然一致，这里防的是竞态下的诡异状态）
	if ki.Size() != di.Size() {
		return fmt.Errorf("硬链接复核失败：%s(%d) 与 %s(%d) 尺寸不一致",
			dup, di.Size(), keep, ki.Size())
	}
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

// workTempRemove 默认等于 cleanupWorkTemp，测试可临时替换以模拟
// 「原副本删除失败」（Windows 上杀软/索引器占用句柄时的高频路径），
// 用于验证残留会被如实回报而不是被静默吞掉（缺陷 6）。
var workTempRemove = cleanupWorkTemp
