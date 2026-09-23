package main

// 功能 4（2026-09-23）：失败清单的「打开文件 / 打开所在目录」。
//
// 为什么是路径版绑定而不是复用 RevealInFolder(id)：失败项**不在结果集里**
// （扫描失败的条目压根没进 byID），ID 判据到不了它们。路径从后端出发、
// 经前端回送，这条往返是新的信任边界，所以校验必须在这里立起来：
//   - 空/全空白 → 拒（不 exec）；
//   - 不存在/无法 stat → 拒（不 exec）——失败清单里躺着的多半正是"访问不了"
//     的路径，把 ENOENT 原样抛回去比弹一个空 Finder 有用得多；
//   - 命令一律 argv 数组直发（复用 revealCmd/openCmd 的形状），**永不**过 shell：
//     路径里带 `; rm -rf` 之类只会是一个文件名，不会是一段指令。V5 钉的就是
//     "带空格与元字符的路径在 Args 里恰好是一个元素"。
//
// ★ V5 跨平台断言的边界（CI run 35886993233 在 ubuntu 上把这条写红了）：
// revealCmd 的 Linux 探测表里，gio/pcmanfm/xdg-open 三条腿**按设计只送父目录**
// （P3 的排序是"能选中文件 > 只能打开目录"，只有前一类带完整路径）。所以
// "完整文件路径必须出现在 argv 里"不是跨平台不变量，它是 darwin/windows 加
// nautilus/dolphin/thunar/nemo 那一档的保证。跨平台只成立的是这两条：
//   - 不下发给 shell 解释器；
//   - 送出去的那一项（完整路径**或**父目录）整串落在恰好一个元素里，没被拆开。
// 于是这里把父目录也埋进同名载荷，让"只送父目录"的那几条腿同样有东西可证。
//
// execRevealCmd 是注入缝（M155 卷型缝同族）：不桩掉的话，每个用例都会真的
// 弹一个 Finder/文件管理器窗口——测试不许劫持用户的桌面。

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubRevealExec 把执行缝换成"只记录不启动"，返回恢复函数与记录指针。
func stubRevealExec(t *testing.T) (got *[]string, restore func()) {
	t.Helper()
	rec := []string{}
	old := execRevealCmd
	execRevealCmd = func(cmd *exec.Cmd, _ func(error)) error {
		// 记录 argv：元素用 \x00 连接后整体存一条——"路径是一个元素"可以逐字节断言
		rec = append(rec, strings.Join(cmd.Args, "\x00"))
		return nil
	}
	return &rec, func() { execRevealCmd = old }
}

func TestRevealPathRejectsBlank(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	for _, bad := range []string{"", "   ", "\t\n"} {
		if err := a.RevealPath(bad); err == nil || !strings.Contains(err.Error(), "路径为空") {
			t.Errorf("RevealPath(%q) 错误不符：%v", bad, err)
		}
		if err := a.OpenPath(bad); err == nil || !strings.Contains(err.Error(), "路径为空") {
			t.Errorf("OpenPath(%q) 错误不符：%v", bad, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("校验拒绝前就已 exec 了命令：%v", *got)
	}
}

func TestRevealPathRejectsMissing(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	ghost := filepath.Join(t.TempDir(), "早就不存在.bin")
	for _, call := range []struct {
		name string
		fn   func(string) error
	}{
		{"RevealPath", a.RevealPath},
		{"OpenPath", a.OpenPath},
	} {
		err := call.fn(ghost)
		if err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Errorf("%s 对幽灵路径应报「不存在」：%v", call.name, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("路径不存在仍去 exec（弹出的只会是一个空窗）：%v", *got)
	}
}

func TestRevealPathFileOpensRevealCommand(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	// 父目录带同样的载荷：见文件头 ★——只把 hostile 放在文件名上时，Linux 那几条
	// "只送父目录"的腿等于什么都没测。
	root := t.TempDir()
	dir := filepath.Join(root, "in dir; echo pwned")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "a b; echo pwned.txt") // 空格 + shell 元字符都在名字里
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.RevealPath(f); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenPath(f); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 {
		t.Fatalf("两次调用各该 exec 一次，得到 %d", len(*got))
	}
	// 三平台共用的两条硬保证（V5 的正面与反面各一条）。
	for i, rec := range *got {
		label := "reveal"
		if i == 1 {
			label = "open"
		}
		argv := strings.Split(rec, "\x00")
		switch filepath.Base(argv[0]) {
		case "sh", "bash", "cmd", "powershell":
			t.Errorf("%s 命令经 shell 解释器下发（注入面）：%q", label, argv)
		}
		whole := 0
		for _, el := range argv {
			if el == f || el == dir {
				whole++
			}
		}
		if whole != 1 {
			t.Errorf("%s 应恰好有一个元素是完整路径或完整父目录（实得 %d）：%q", label, whole, argv)
		}
		for _, el := range argv {
			if el == f || el == dir {
				continue
			}
			if strings.Contains(el, "echo") || strings.Contains(el, "pwned") {
				t.Errorf("%s 路径被拆开/改写后才下发，元素 %q 只剩片段：%q", label, el, argv)
			}
		}
	}
	reveal := strings.Split((*got)[0], "\x00")
	fileOpen := strings.Split((*got)[1], "\x00")
	switch runtime.GOOS {
	case "darwin":
		if reveal[0] != "open" || len(reveal) < 2 || reveal[1] != "-R" {
			t.Errorf("darwin reveal 应为 open -R：%q", reveal)
		}
		if len(reveal) != 3 || reveal[2] != f {
			t.Errorf("darwin 文件行 reveal 应为 open -R <path>：%q", reveal)
		}
		if fileOpen[0] != "open" || len(fileOpen) != 2 || fileOpen[1] != f {
			t.Errorf("darwin 打开文件应为 open <path>：%q", fileOpen)
		}
	case "windows":
		if reveal[0] != "explorer" || len(reveal) != 3 || reveal[1] != "/select," || reveal[2] != f {
			t.Errorf("windows 文件行 reveal 应为 explorer /select, <path>：%q", reveal)
		}
		if fileOpen[0] != "explorer" || len(fileOpen) != 2 || fileOpen[1] != f {
			t.Errorf("windows 打开文件应为 explorer <path>：%q", fileOpen)
		}
	case "linux":
		// Linux 只能断到"命中探测表"这一层：送完整路径还是送父目录，取决于装的
		// 是哪个 DE 的工具先被 LookPath 命中，两种都算履行契约。真正跨平台的
		// 注入保证在上面那两条通用断言里，不在这里。
		if !inNames(reveal[0], "nautilus", "dolphin", "thunar", "nemo", "pcmanfm", "gio", "xdg-open") {
			t.Errorf("linux reveal 命令不在 revealCmd 的探测表里：%q", reveal)
		}
		if !inNames(fileOpen[0], "xdg-open", "gio") {
			t.Errorf("linux 打开文件命令不在 openCmd 的探测表里：%q", fileOpen)
		}
	}
}

// inNames 报告 exe 是否就是探测表里的某一条（比较 argv[0] 的基名，容忍绝对路径）。
func inNames(argv0 string, names ...string) bool {
	base := filepath.Base(argv0)
	for _, n := range names {
		if base == n {
			return true
		}
	}
	return false
}

func TestRevealPathDirectoryOpensItself(t *testing.T) {
	// 目录条目没有"到父目录里选中它"的使用价值——失败清单里的目录行，
	// 用户想看的是**里面有什么**（比如权限错在哪个子项上）。
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	dir := t.TempDir()
	sub := filepath.Join(dir, "some dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.RevealPath(sub); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("exec 次数 = %d", len(*got))
	}
	argvs := strings.Split((*got)[0], "\x00")
	// 「目录不带选中参数」是这条用例的全部命题，三平台同真：darwin 的 -R、
	// Linux/Windows 的 --select、/select, 一个都不该出现。此前只在 darwin 断言，
	// 换到别的平台这条用例就退化成"exec 了一次"。
	if runtime.GOOS == "darwin" {
		// 与 OpenPath 同形：open <dir>，没有 -R
		if argvs[0] != "open" || len(argvs) != 2 || argvs[1] != sub {
			t.Errorf("目录行 reveal 不该带 -R：%q", argvs)
		}
	}
	for _, el := range argvs {
		if el == "-R" || el == "--select" || strings.HasPrefix(el, "/select,") {
			t.Errorf("目录行走成了「到父目录里选中」而不是打开自身（%q）：%q", el, argvs)
		}
	}
}

func TestOpenPathErrorGoesNowhereNearShell(t *testing.T) {
	// 负控制：错误路径（空/不存在）之外，校验通过但**命令装配失败**也要从
	// 返回值走，不得 panic、不得静默。这里用不存在的绝对路径已覆盖主要分支，
	// 本条只钉"拒绝先于装配"的顺序性（缝未启动 + 错误可见）。
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	err := a.OpenPath(filepath.Join(t.TempDir(), "gone", "deeper", "x.bin"))
	if err == nil {
		t.Fatal("期望错误")
	}
	if len(*got) != 0 {
		t.Errorf("装配失败前不应 exec：%v", *got)
	}
}
