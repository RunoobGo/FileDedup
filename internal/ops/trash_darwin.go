//go:build darwin

package ops

import (
	"fmt"
	"os/exec"
	"strings"
)

// trashScript 静态 AppleScript：路径经 osascript 的 run handler argv 传入，
// 不再拼进脚本文本（C8）。
//
// 修正前用 fmt.Sprintf("POSIX file %q", p) 内插路径——Go 的 %q 转义（\xNN、
// \n、\"）不是 AppleScript 字符串转义，路径含双引号/反斜杠/换行时脚本非法，
// 移回收站直接失败。改为 on run argv + POSIX file (contents of p) 后，
// 任意路径（含引号/反斜杠/换行/unicode）都原样到达脚本，无转义面。
const trashScript = `on run argv
set itemList to {}
repeat with p in argv
set end of itemList to POSIX file (contents of p)
end repeat
tell application "Finder" to move itemList to trash
end run`

// osascriptArgs 构造命令行：静态脚本 + 原样路径参数（argv 机制，无转义）。
func osascriptArgs(paths []string) []string {
	args := make([]string, 0, len(paths)+2)
	args = append(args, "-e", trashScript)
	return append(args, paths...)
}

func defaultTrash(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	// 单次 osascript 批量移入 Finder 回收站（避免逐文件进程开销）
	out, err := exec.Command("osascript", osascriptArgs(paths)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
