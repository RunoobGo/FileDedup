//go:build darwin

package ops

// C8 回归：路径不再经 %q 内插进 AppleScript（Go 转义 ≠ AppleScript 转义，
// 含双引号/反斜杠/换行的路径会让脚本非法、移回收站失败），改经 osascript
// argv 传入。真实 Finder 移动不入单测（会把测试文件移进用户回收站），
// 此处验证脚本静态性与 argv 对特殊字符路径的保真性（同一 osascript 机制）。

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTrashScriptIsStatic 脚本必须是常量：路径一律走 argv，不在脚本文本里。
func TestTrashScriptIsStatic(t *testing.T) {
	tricky := `/a"b\c
d` + "\x80\xe4\xb8\xad"
	args := osascriptArgs([]string{tricky, "/normal/path"})
	if len(args) != 4 {
		t.Fatalf("args 长度 = %d, want 4", len(args))
	}
	if args[0] != "-e" || args[1] != trashScript {
		t.Fatalf("前两个参数应为 -e + 静态脚本，got %q %q", args[0], args[1])
	}
	if args[2] != tricky || args[3] != "/normal/path" {
		t.Fatalf("路径必须原样进 argv（无转义），got %q %q", args[2], args[3])
	}
	if strings.Contains(trashScript, `POSIX file %q`) || strings.Contains(trashScript, "%q") {
		t.Error("脚本不得内插路径")
	}
}

// TestOsascriptArgVFidelity 验证 argv 机制本身对特殊字符路径保真：
// osascript 把参数原样交回脚本（与 defaultTrash 的传参方式完全一致）。
func TestOsascriptArgVFidelity(t *testing.T) {
	tricky := []string{
		`he said "hi"`,
		`back\slash`,
		"new\nline",
		"中文 /path",
	}
	// 回显脚本：argv 以 0x1F 分隔拼回（按行分割无法容纳含换行的路径）
	echo := "on run argv\nset out to \"\"\nrepeat with p in argv\n" +
		"set out to out & (contents of p) & (character id 31)\nend repeat\nreturn out\nend run"
	args := append([]string{"-e", echo}, tricky...)
	out, err := exec.Command("osascript", args...).Output()
	if err != nil {
		t.Fatalf("osascript: %v", err)
	}
	outStr := strings.TrimSuffix(string(out), "\n") // osascript 输出自带换行
	got := strings.Split(strings.TrimSuffix(outStr, "\x1f"), "\x1f")
	if len(got) != len(tricky) {
		t.Fatalf("回显 %d 项, want %d: %q", len(got), len(tricky), got)
	}
	for i, p := range tricky {
		if got[i] != p {
			t.Errorf("argv 第 %d 项失真: got %q, want %q", i, got[i], p)
		}
	}
}
