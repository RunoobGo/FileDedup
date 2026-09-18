//go:build linux

package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultTrash XDG Trash 规范自研实现（02 决策 7：零外部依赖）：
// ~/.local/share/Trash/{files,info}；重名按 .2/.3 递增（规范要求）；trashinfo 记录原路径与时间。
// trashXDGGuard 进程内互斥：uniqueXDG 是「先查后用」（TOCTOU），并发 trash 同名
// 文件会同时选中同一 dst，导致后到者覆盖已入回收站的文件、源被删（数据丢失）。
// 锁覆盖「选名 + 写 info + 移动」整段，保证同一回收站命名空间下串行化。
var trashXDGGuard sync.Mutex

func defaultTrash(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		root = filepath.Join(home, ".local", "share")
	}
	return trashXDG(filepath.Join(root, "Trash"), paths)
}

// trashXDG 规范实现（root 可注入：测试用）。
func trashXDG(trashDir string, paths []string) error {
	filesDir := filepath.Join(trashDir, "files")
	infoDir := filepath.Join(trashDir, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		trashXDGGuard.Lock()
		dst, err := uniqueXDG(filesDir, filepath.Base(abs))
		if err != nil {
			trashXDGGuard.Unlock()
			return err
		}
		// P2：trashinfo 必须先写。若顺序颠倒，moveIntoTrash 成功后写 info 失败
		// 会留下无元数据的孤儿文件——回收站看不到原始路径，用户无法还原，
		// 且此时"报错并继续下一个"会让前面的孤儿已无法回滚。先写 info 时，
		// 移动失败只留下一个无害的空 info（回收站会忽略无对应文件的条目）。
		if err := writeTrashInfo(infoDir, filepath.Base(dst), abs); err != nil {
			trashXDGGuard.Unlock()
			return err
		}
		if err := moveIntoTrash(abs, dst); err != nil {
			os.Remove(filepath.Join(infoDir, filepath.Base(dst)+".trashinfo"))
			trashXDGGuard.Unlock()
			return err
		}
		trashXDGGuard.Unlock()
	}
	return nil
}

// moveIntoTrash 同卷 rename；跨卷退化复制+删除（复制失败时清理半成品再返回错误）。
//
// 与 MoveFile 的跨卷路径不同：这里刻意不还原权限位与 mtime。
// 移入回收站后源 mtime 应保留在原处语义（恢复时由 DE 按 trashinfo 处理），
// 权限还原对回收站条目无意义，恢复体验也以内容一致为优先。
func moveIntoTrash(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	st, err := os.Stat(src)
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
	return os.Remove(src)
}

// xdgNameMaxTry 重名递增上限。不设上限时任何"永远查不出 NotExist"的环境
// （如 files/ 所在目录被 chmod 000，Stat 恒返回 EACCES）都会让循环无限转，
// 且循环整体持锁 → 全部并发 trash goroutine 一起挂死。
const xdgNameMaxTry = 10000

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
	for i := 2; i <= xdgNameMaxTry+1; i++ {
		dst = filepath.Join(dir, fmt.Sprintf("%s.%d", name, i))
		free, err := nameFree(dst)
		if err != nil {
			return "", fmt.Errorf("回收站命名空间不可读: %w", err)
		}
		if free {
			return dst, nil
		}
	}
	return "", fmt.Errorf("回收站中 %s 的重名条目已达上限 %d，拒绝继续递增", name, xdgNameMaxTry)
}

// writeTrashInfo 生成规范 .trashinfo。
//
// H4：Path= 必须是"绝对路径或相对路径"（freedesktop Trash Spec 1.0），
// 主流实现（glib/Nautilus）写的是原样绝对路径。修正前写 `file:///...`，
// 会被按相对路径解析到 $XDG_DATA_HOME 下 → 回收站无法还原，
// 文件事实上永久失联。换行符（含 %5Cn 编码歧义）格式无法表达，直接拒绝。
func writeTrashInfo(infoDir, name, origAbs string) error {
	if strings.ContainsAny(origAbs, "\n\r") {
		return fmt.Errorf("路径含换行符，trashinfo 格式无法表达: %s", origAbs)
	}
	now := time.Now().Format("2006-01-02T15:04:05")
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", origAbs, now)
	return os.WriteFile(filepath.Join(infoDir, name+".trashinfo"), []byte(content), 0o600)
}
