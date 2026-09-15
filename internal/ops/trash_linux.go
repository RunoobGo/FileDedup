//go:build linux

package ops

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultTrash XDG Trash 规范自研实现（02 决策 7：零外部依赖）：
// ~/.local/share/Trash/{files,info}；重名按 .2/.3 递增（规范要求）；trashinfo 记录原路径与时间。
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
		dst := uniqueXDG(filesDir, filepath.Base(abs))
		if err := moveIntoTrash(abs, dst); err != nil {
			return err
		}
		if err := writeTrashInfo(infoDir, filepath.Base(dst), abs); err != nil {
			return err
		}
	}
	return nil
}

// moveIntoTrash 同卷 rename；跨卷退化复制+删除（复制失败时清理半成品再返回错误）。
func moveIntoTrash(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := copyVerify(src, dst, st.Size()); err != nil {
		os.Remove(dst) // 清理半成品，避免"看起来进了回收站实为残片"
		return err
	}
	return os.Remove(src)
}

// uniqueXDG XDG 规范重名：name → name.2 → name.3（不带扩展名拆分）。
func uniqueXDG(dir, name string) string {
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return dst
	}
	for i := 2; ; i++ {
		dst = filepath.Join(dir, fmt.Sprintf("%s.%d", name, i))
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			return dst
		}
	}
}

// writeTrashInfo 生成规范 .trashinfo。
func writeTrashInfo(infoDir, name, origAbs string) error {
	uri := "file://" + urlPathEscapeSlash(origAbs)
	now := time.Now().Format("2006-01-02T15:04:05")
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", uri, now)
	return os.WriteFile(filepath.Join(infoDir, name+".trashinfo"), []byte(content), 0o600)
}

// PathEscapeSlash 保持路径分隔符不转义的 percent-encoding（net/url 转义 / 会破坏路径）。
func urlPathEscapeSlash(p string) string {
	seg := strings.Split(p, "/")
	for i, s := range seg {
		seg[i] = url.PathEscape(s)
	}
	return strings.Join(seg, "/")
}
