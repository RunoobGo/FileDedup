//go:build linux

package ops

import (
	"os"
	"path/filepath"
)

// defaultTrash Linux 回收站 = XDG Trash 规范自研实现（判据本体在无 tag 的
// trash_xdg.go，§31）：~/.local/share/Trash/{files,info}。
func defaultTrash(paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil
	}
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".local", "share")
	}
	return trashXDG(filepath.Join(root, "Trash"), paths)
}
