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
	// M211 契约改写（修前红由 TestOsascriptArgsDashTerminator 提供）：
	// [-e, script, --, paths...]，路径必须在 -- 之后原样出现。
	if len(args) != 5 {
		t.Fatalf("args 长度 = %d, want 5（-e + script + -- + 2 paths）", len(args))
	}
	if args[0] != "-e" || args[1] != trashScript || args[2] != "--" {
		t.Fatalf("前三个参数应为 -e + 静态脚本 + -- 终止符，got %q %q %q", args[0], args[1], args[2])
	}
	if args[3] != tricky || args[4] != "/normal/path" {
		t.Fatalf("路径必须原样进 argv（无转义），got %q %q", args[3], args[4])
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

// v0.5.0 UI 验收回归（两连击）：
//  1. Finder `move <单条目列表> to trash` 返回奇异引用（非列表），
//     `repeat with o in <非列表>` 迭代 0 次 → 旧批量脚本输出空串。
//  2. 入站重名（同名文件先后回收，Finder 改名为 "b 19.04.56.bin"）时
//     按基名配对整体放弃（宁缺勿错配）→ 账本 DestPath 为空，回撤失效。
//
// 修正：脚本逐文件 move 并输出 "src␟dst" 成对行——配对由脚本内一一对应
// 完成，与 Finder 改名无关；POSIX file 解析必须在 tell 块外（tell 内会
// 被当作 Finder 对象引用报 -1728）。
func TestTrashScriptEmitsPairedResults(t *testing.T) {
	iPosix := strings.Index(trashScript, `set srcItem to POSIX file (contents of p)`)
	iTell := strings.Index(trashScript, `tell application "Finder"`)
	if iPosix < 0 || iTell < 0 || iPosix > iTell {
		t.Error(`POSIX file 解析必须在 tell application "Finder" 之前（tell 内报 -1728）`)
	}
	if !strings.Contains(trashScript, "move srcItem to trash") {
		t.Error("必须逐文件 move（批量列表返回奇异引用）")
	}
	if !strings.Contains(trashScript, "(character id 31)") {
		t.Error("输出必须是 src␟dst 成对行（0x1F 分隔），不得按基名回推配对")
	}
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

// v0.5.0 功能 4：解析 Finder「src␟dst」成对输出为 src→dst 映射（回撤账本用）。
// 真实 Finder 移动不入单测；parseTrashOutput 是纯函数。

const us = "\x1f" // 0x1F 字段分隔符

func TestParseTrashOutputPairsBySrcField(t *testing.T) {
	inputs := []string{"/vol/a/report.txt", "/vol/b/照片.png"}
	// 第二个文件入站被 Finder 重名改写——配对仍精确（不依赖基名）
	stdout := "/vol/a/report.txt" + us + "/Volumes/.Trash/report.txt\n" +
		"/vol/b/照片.png" + us + "/Volumes/.Trash/照片 19.04.56.png\n"
	m := parseTrashOutput(stdout, inputs)
	if m[inputs[0]] != "/Volumes/.Trash/report.txt" ||
		m[inputs[1]] != "/Volumes/.Trash/照片 19.04.56.png" {
		t.Fatalf("映射失真: %+v", m)
	}
}

func TestParseTrashOutputDropsBadLines(t *testing.T) {
	inputs := []string{"/x/a.txt", "/y/b.txt"}
	stdout := strings.Join([]string{
		"/x/a.txt" + us + "/T/a.txt",             // 正常
		"no-separator-line",                      // 破坏协议 → 丢行
		"/z/unknown.txt" + us + "/T/unknown.txt", // src 不在本批输入 → 丢弃
		"/y/b.txt",                               // 缺 dst 字段 → 丢行
		"",
	}, "\n")
	m := parseTrashOutput(stdout, inputs)
	if len(m) != 1 || m["/x/a.txt"] != "/T/a.txt" {
		t.Fatalf("应仅保留可信配对: %+v", m)
	}
	// 含换行的文件名破坏所在行 → 该输入无映射（其余不受影响）
	m2 := parseTrashOutput("/x/a\nb.txt"+us+"/T/whatever\n/y/b.txt"+us+"/T/b.txt\n", inputs)
	if _, ok := m2["/y/b.txt"]; !ok || len(m2) != 1 {
		t.Errorf("坏行只应伤及自身: %+v", m2)
	}
	// 空输出 → 空映射（非 nil），调用方按「去向未知」逐项处理
	if m3 := parseTrashOutput("", nil); m3 == nil || len(m3) != 0 {
		t.Errorf("空输出应为空映射, got %+v", m3)
	}
}

// M211（2026-09-24 第五轮审查批）：paths 前必须有 "--" 终止符。
// 取证（本机实测）：osascript 在 `--` 之前按自身选项解析后续 token——
// 文件名 "-dash.txt" → `illegal option -- d` rc=2（整批失败）；
// 文件名恰 "-i" → rc=0 且零输出（handler 根本没跑）→ executor 记整批 done 虚账。
// 本条是**修前红探针**：改前 osascriptArgs 无 "--"，必须红。
func TestOsascriptArgsDashTerminator(t *testing.T) {
	paths := []string{"-dash.txt", "/normal/path"}
	args := osascriptArgs(paths)
	term := -1
	for i, a := range args {
		if a == "--" {
			term = i
			break
		}
	}
	if term < 0 {
		t.Fatalf("args 必须在 paths 之前含 -- 终止符，got %#v", args)
	}
	if len(args) != term+1+len(paths) {
		t.Fatalf("-- 之后必须恰是全部 paths（原样，无转义），got %#v", args)
	}
	for i, p := range paths {
		if args[term+1+i] != p {
			t.Errorf("paths[%d] = %q, want %q", i, args[term+1+i], p)
		}
	}
	// 空批次不得悬空 "--"
	if got := osascriptArgs(nil); len(got) != 2 || got[0] != "-e" {
		t.Errorf("空 paths 应只有 [-e script]，got %#v", got)
	}
}

// M211 P-211-b（行为级，安全：count 脚本不触 Finder、不碰文件）：
// 以生产同一条构造路径（osascriptArgsFor）把 "-i"/"-dash.txt" 交给真 osascript，
// 必须原样数回 3 个 argv。若哪天 "--" 被删（变异 M-211-2），本条当场红：
// "-i" 吞掉后续 token → 计数失真或 rc≠0。
func TestOsascriptDashArgFidelity(t *testing.T) {
	const countScript = "on run argv\nreturn (count of argv) as string\nend run"
	paths := []string{"-i", "-dash.txt", "n.txt"}
	cmd := exec.Command("osascript", osascriptArgsFor(countScript, paths)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("osascript 真跑失败（- 开头路径被当选项？）: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "3" {
		t.Fatalf("argv 计数 = %q, want 3（-- 之后 3 条路径须原样进 handler）", got)
	}
}

// M211 P-211-c：零配对防线的三格判据。
func TestBatchOutputGap(t *testing.T) {
	if !batchOutputGap(0, 5) {
		t.Error("5 输入零配对必须判缺口（-i 静默形状）")
	}
	if batchOutputGap(5, 5) {
		t.Error("全配对不得判缺口")
	}
	if batchOutputGap(0, 0) {
		t.Error("空批次平凡通过（defaultTrash 已在入口拦 len==0，这里双保险）")
	}
}
