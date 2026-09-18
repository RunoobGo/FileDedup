//go:build darwin

package ops

// C8 回归：路径不再经 %q 内插进 AppleScript（Go 转义 ≠ AppleScript 转义，
// 含双引号/反斜杠/换行的路径会让脚本非法、移回收站失败），改经 osascript
// argv 传入。真实 Finder 移动不入单测（会把测试文件移进用户回收站），
// 此处验证脚本静态性与 argv 对特殊字符路径的保真性（同一 osascript 机制）。

import (
	"os"
	"os/exec"
	"strconv"
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

// D6 回归：分批为纯函数（不触碰 Finder）；超时/常量为静态契约。
func TestChunkPaths(t *testing.T) {
	if got := chunkPaths(nil, 100); got != nil {
		t.Errorf("空输入应无批次, got %v", got)
	}
	paths := make([]string, 250)
	for i := range paths {
		paths[i] = "/p" + strconv.Itoa(i)
	}
	got := chunkPaths(paths, 100)
	if len(got) != 3 || len(got[0]) != 100 || len(got[1]) != 100 || len(got[2]) != 50 {
		t.Fatalf("250 条按 100 分批应为 100/100/50, got %v", sizes(got))
	}
	// 顺序保真：拼接回去必须与原切片一致
	var flat []string
	for _, b := range got {
		flat = append(flat, b...)
	}
	for i := range flat {
		if flat[i] != paths[i] {
			t.Fatalf("批次乱码: %d 处 %q != %q", i, flat[i], paths[i])
		}
	}
	if g := chunkPaths([]string{"a", "b"}, 0); len(g) != 2 {
		t.Errorf("size<1 应按 1 处理, got %v", sizes(g))
	}
}

func sizes(batches [][]string) []int {
	out := make([]int, len(batches))
	for i, b := range batches {
		out[i] = len(b)
	}
	return out
}

// defaultTrash 必须用带超时的 CommandContext 且按批执行（静态断言，
// 避免真实调用 Finder 污染用户回收站）。
func TestDefaultTrashUsesTimeoutAndChunks(t *testing.T) {
	src, err := os.ReadFile("trash_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{"exec.CommandContext", "context.WithTimeout", "cmd.WaitDelay", "chunkPaths(paths, trashBatchSize)"} {
		if !strings.Contains(s, want) {
			t.Errorf("trash_darwin.go 缺少 %q（超时/分批收口不得回退）", want)
		}
	}
	if strings.Contains(s, `exec.Command("osascript"`) {
		t.Error("不得保留无超时的 exec.Command(\"osascript\"...)")
	}
}

// v0.5.0 功能 4：Finder 输出解析为 src→dst 映射（回撤账本用）。
// 真实 Finder 移动不入单测；parseTrashOutput 是纯函数。

func TestParseTrashOutputPairsByBasename(t *testing.T) {
	inputs := []string{"/vol/a/report.txt", "/vol/b/照片.png"}
	stdout := "/Volumes/.Trash/report.txt\n/Volumes/.Trash/照片.png\n"
	m := parseTrashOutput(stdout, inputs)
	if m == nil {
		t.Fatal("匹配批次不应返回 nil")
	}
	if m[inputs[0]] != "/Volumes/.Trash/report.txt" || m[inputs[1]] != "/Volumes/.Trash/照片.png" {
		t.Fatalf("映射失真: %+v", m)
	}
}

func TestParseTrashOutputRejectsMismatch(t *testing.T) {
	// 数量不符
	if m := parseTrashOutput("/T/a.txt\n", []string{"/x/a.txt", "/y/b.txt"}); m != nil {
		t.Errorf("数量不符应放弃: %+v", m)
	}
	// 基名多重集不符（Finder 重命名为 "a 2.txt" 时宁缺勿错配）
	inputs := []string{"/x/a.txt", "/y/other.txt"}
	stdout := "/T/a.txt\n/T/a 2.txt\n"
	if m := parseTrashOutput(stdout, inputs); m != nil {
		t.Errorf("基名对不上应放弃: %+v", m)
	}
	// 空 stdout + 空输入 → 空映射（非 nil）
	if m := parseTrashOutput("", nil); m == nil || len(m) != 0 {
		t.Errorf("双双为空应为空映射, got %+v", m)
	}
	// 含换行的文件名破坏行协议 → 数量对不上 → nil（安全降级）
	if m := parseTrashOutput("/T/a\nb.txt\n", []string{"/x/a\nb.txt"}); m != nil {
		t.Errorf("行协议被破坏时应放弃: %+v", m)
	}
}
