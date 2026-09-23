package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// XDG Trash 规范自研实现（02 决策 7：零外部依赖）。§31（2026-09-24 第五轮审查 P0 批）
// 起本文件**无 build tag**：这里全部是纯 os/path 逻辑，下沉到无 tag 文件是为了让
// 删源身份守卫的"修前红/负控制/变异"三件套在非 linux 开发机上真跑（H6 惯例）；
// 生产上仍只有 linux 会调用（defaultTrash 各平台一份，见 trash_linux.go 的薄壳）。
//
// 重名按 .2/.3 递增（规范要求）；trashinfo 记录原路径与时间。
// trashXDGGuard 进程内互斥：uniqueXDG 是「先查后用」（TOCTOU），并发 trash 同名
// 文件会同时选中同一 dst，导致后到者覆盖已入回收站的文件、源被删（数据丢失）。
// 锁覆盖「选名 + 写 info + 移动」整段，保证同一回收站命名空间下串行化。
var trashXDGGuard sync.Mutex

// trashXDG 规范实现（root 可注入：测试用）。
// 返回 src→dst 映射，键为传入的原始路径（未 Abs 化），失败时返回已完成部分。
func trashXDG(trashDir string, paths []string) (map[string]string, error) {
	filesDir := filepath.Join(trashDir, "files")
	infoDir := filepath.Join(trashDir, "info")
	dstMap := make(map[string]string, len(paths))
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return dstMap, err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return dstMap, err
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return dstMap, err
		}
		trashXDGGuard.Lock()
		dst, err := uniqueXDG(filesDir, filepath.Base(abs))
		if err != nil {
			trashXDGGuard.Unlock()
			return dstMap, err
		}
		// P2：trashinfo 必须先写。若顺序颠倒，moveIntoTrash 成功后写 info 失败
		// 会留下无元数据的孤儿文件——回收站看不到原始路径，用户无法还原，
		// 且此时"报错并继续下一个"会让前面的孤儿已无法回滚。先写 info 时，
		// 移动失败只留下一个无害的空 info（回收站会忽略无对应文件的条目）。
		if err := writeTrashInfo(infoDir, filepath.Base(dst), abs); err != nil {
			trashXDGGuard.Unlock()
			return dstMap, err
		}
		if err := moveIntoTrash(abs, dst); err != nil {
			// §31：被顶替那一格**不回滚 trashinfo**——dst 副本装的是原件字节，
			// info 与副本必须成对留存，否则副本退化成回收站孤儿（用户无从还原）。
			if !errors.Is(err, errCopiedSrcSwapped) {
				os.Remove(filepath.Join(infoDir, filepath.Base(dst)+".trashinfo"))
			}
			trashXDGGuard.Unlock()
			return dstMap, err
		}
		trashXDGGuard.Unlock()
		dstMap[p] = dst
	}
	return dstMap, nil
}

// errCopiedSrcSwapped 是"复制腿成功、但删源前身份复核不过"的哨兵（§31）。
// trashXDG 靠它决定 trashinfo 回滚与否：被顶替时副本与 info 必须成对留存。
var errCopiedSrcSwapped = errors.New("源在复制期间被替换")

// preRemoveRecheck 是上面那道删源前复核的包级接缝（测试直接换装，同 copyVerifyFile 惯例）。
// 存在的理由不是"能换"，而是换入的顶替时序**必须发生在 src 读句柄关闭之后**：
// Go 的 syscall.Open 在 Windows 的 sharemode 只有 READ|WRITE、不带
// FILE_SHARE_DELETE（syscall/syscall_windows.go 的 openFile），
// 句柄开着时 os.Rename(src) 必撞 sharing violation——首版把顶替钩在 copyAndSyncFile
// 里（句柄还开着）在 CI windows 腿直接红（run 35931760089，§6.29 复批）。
// 钩在本接缝上（moveIntoTrash 已 f.Close、未 identityStill）两平台都能构造同一时序。
var preRemoveRecheck = identityStill

// moveIntoTrash 同卷 rename；跨卷退化复制+删除（复制失败时清理半成品再返回错误）。
//
// 与 MoveFile 的跨卷路径不同：这里刻意不还原权限位与 mtime。
// 移入回收站后源 mtime 应保留在原处语义（恢复时由 DE 按 trashinfo 处理），
// 权限还原对回收站条目无意义，恢复体验也以内容一致为优先。
func moveIntoTrash(src, dst string) error {
	if err := renameFile(src, dst); err == nil {
		return nil
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	// AS-H4/§31：复制可持续数秒到数分钟，是删源前窗口最大的一处。复制前按路径
	// 取身份底片（FromPathNoFollow，与复核侧同口径），删源前复核；窗口内第三方
	// 以 rename 顶替 src（同步盘/下载器原子落子的时序）时删掉的会是别人的新文件。
	srcID, err := pathIdentity(src)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	err = copyAndSync(f, dst, st.Size())
	f.Close()
	if err != nil {
		os.Remove(dst) // 清理半成品，避免"看起来进了回收站实为残片"
		return err
	}
	if !preRemoveRecheck(src, srcID) {
		// 与 move.go 跨卷腿同取向：不删源、不算成功。副本（原件字节）留在 dst，
		// trashinfo 由调用方豁免回滚 ⇒ 两份并存交给用户核对。
		return fmt.Errorf("%w（inode 已变化）：为避免误删第三方文件**未删除源**，"+
			"复制好的副本留在回收站 %s，源路径现为顶替者 %s，请核对后自行处理其一",
			errCopiedSrcSwapped, dst, src)
	}
	return removeSrc(src)
}

// nameFree 判定候选名可用：Lstat 报 NotExist 才算空位。
// 悬空符号链接（目标不存在）Lstat 成功返回 → 视为占用：rename 会把它连同
// 链接本身一起移走，若当作空位则目标端语义丢失。
// 其他错误（EACCES、EIO…）无法判定，返回错误让调用方失败退出——修正前
// 这类错误被当作"NotExist 为假 = 占用"继续递增，掩盖故障且可能永动。
func nameFree(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}

// uniqueXDG XDG 规范重名：name → name.2 → name.3（不带扩展名拆分）。
// 返回错误表示命名空间不可用或超出上限，调用方必须中止而非跳过硬用。
//
// 注：不用 O_EXCL 占位来消除 TOCTOU——占位文件会破坏目录移入：
// rename(目录 → 已存在的普通文件) 在 Linux 上返回 ENOTDIR，
// 而 trash 源完全可能是目录（见跨卷测试）。进程内竞态由
// trashXDGGuard 互斥覆盖；跨进程竞态（两个实例同时 trash）在
// 单实例桌面应用语境下属可接受残余风险。
func uniqueXDG(dir, name string) (string, error) {
	dst := filepath.Join(dir, name)
	if free, err := nameFree(dst); err != nil {
		return "", fmt.Errorf("回收站命名空间不可读: %w", err)
	} else if free {
		return dst, nil
	}
	for i := 2; i <= nameMaxTry+1; i++ {
		dst = filepath.Join(dir, fmt.Sprintf("%s.%d", name, i))
		free, err := nameFree(dst)
		if err != nil {
			return "", fmt.Errorf("回收站命名空间不可读: %w", err)
		}
		if free {
			return dst, nil
		}
	}
	return "", fmt.Errorf("回收站中 %s 的重名条目已达上限 %d，拒绝继续递增", name, nameMaxTry)
}

// writeTrashInfo 生成规范 .trashinfo。
//
// H4：Path= 必须是"绝对路径或相对路径"（freedesktop Trash Spec 1.0），
// 修正前写 `file:///...`，会被按相对路径解析到 $XDG_DATA_HOME 下 → 回收站
// 无法还原，文件事实上永久失联。换行符格式无法表达，直接拒绝。
//
// 按规范 Path 值需做 desktop-entry location 转义（glib/Nautilus 即如此）：
// 空格与非 ASCII 字节写成原样会让 key-file 解析产生歧义，回收站按
// %XX 解码后找不到原路径。此处仅保留 unreserved 字符与 '/'，其余字节
// （含多字节 UTF-8）逐字节 percent 编码。
func writeTrashInfo(infoDir, name, origAbs string) error {
	if strings.ContainsAny(origAbs, "\n\r") {
		return fmt.Errorf("路径含换行符，trashinfo 格式无法表达: %s", origAbs)
	}
	now := time.Now().Format("2026-01-02T15:04:05")
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", xdgEscapePath(origAbs), now)
	return os.WriteFile(filepath.Join(infoDir, name+".trashinfo"), []byte(content), 0o600)
}

// xdgEscapePath 桌面入口 location 转义：A-Za-z0-9-._~/ 之外的字节 → %XX。
func xdgEscapePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		const upperhex = "0123456789ABCDEF"
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~', c == '/':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&0x0f])
		}
	}
	return b.String()
}
