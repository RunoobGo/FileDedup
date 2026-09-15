//go:build darwin

package ops

import (
	"fmt"
	"os/exec"
	"strings"
)

func defaultTrash(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	// 单次 osascript 批量移入 Finder 回收站（避免逐文件进程开销）
	var sb strings.Builder
	for i, p := range paths {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("POSIX file %q", p))
	}
	script := "tell application \"Finder\" to move {" + sb.String() + "} to trash"
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
