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
	dir := t.TempDir()
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
	argvs := strings.Split((*got)[0], "\x00")
	// V5 的正面：整条路径恰好是一个 argv 元素（不过 shell 的直证）
	found := false
	for _, el := range argvs {
		if el == f {
			found = true
		}
	}
	if !found {
		t.Errorf("reveal argv 里没有完整路径元素：%q", argvs)
	}
	for _, argv := range [2][]string{argvs, strings.Split((*got)[1], "\x00")} {
		base := filepath.Base(argv[0])
		if base == "sh" || base == "bash" || base == "cmd" || base == "powershell" {
			t.Errorf("命令经 shell 解释器下发（注入面）：%q", argv)
		}
	}
	switch runtime.GOOS {
	case "darwin":
		if argvs[0] != "open" {
			t.Errorf("darwin reveal 应为 open -R：%q", argvs)
		}
		if len(argvs) < 2 || argvs[1] != "-R" {
			t.Errorf("文件行 reveal 漏了 -R（选中）参数：%q", argvs)
		}
		fileOpen := strings.Split((*got)[1], "\x00")
		if fileOpen[0] != "open" || len(fileOpen) != 2 || fileOpen[1] != f {
			t.Errorf("darwin 打开文件应为 open <path>：%q", fileOpen)
		}
	}
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
	if runtime.GOOS == "darwin" {
		// 与 OpenPath 同形：open <dir>，没有 -R
		if argvs[0] != "open" || len(argvs) != 2 || argvs[1] != sub {
			t.Errorf("目录行 reveal 不该带 -R：%q", argvs)
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
