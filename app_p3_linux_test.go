//go:build linux

package main

// P3 回归：Linux「打开所在文件夹并选中」的命令选择。
//
// 修正前只有 xdg-open <父目录>：既不会选中目标文件（几千个同规模目录里用户
// 仍要自己找），也没探测可用性——缺 xdg-open 时报的是 exec 层的
// "executable file not found"，用户分不清是操作失败还是环境缺工具。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// setPath 临时改写 PATH（extra 为空时使用空目录），返回恢复函数。
func setPath(t *testing.T, extra string) func() {
	t.Helper()
	old := os.Getenv("PATH")
	if extra == "" {
		extra = t.TempDir() // 空目录 → 无任何可执行文件
	}
	if err := os.Setenv("PATH", extra); err != nil {
		t.Fatal(err)
	}
	return func() { _ = os.Setenv("PATH", old) }
}

func TestRevealCmdPrefersSelectCapable(t *testing.T) {
	const path = "/tmp/dir with space/文件 名.txt"
	defer setPath(t, "")()
	_, err := revealCmd(path)
	if err == nil {
		t.Fatal("PATH 为空时应明确报错（而不是 exec 一个不存在的命令）")
	}
	if !strings.Contains(err.Error(), "无法定位文件") {
		t.Errorf("错误信息应能指导用户安装工具: %v", err)
	}

	bin := t.TempDir()
	fake := filepath.Join(bin, "nautilus")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer setPath(t, bin)()
	cmd, err := revealCmd(path)
	if err != nil {
		t.Fatalf("有 nautilus 时不应报错: %v", err)
	}
	if filepath.Base(cmd.Path) != "nautilus" {
		t.Errorf("应优先 nautilus, got %s", cmd.Path)
	}
	if len(cmd.Args) < 3 || cmd.Args[1] != "--select" || cmd.Args[2] != path {
		t.Errorf("Linux 应带 --select 选中文件本身: %#v", cmd.Args)
	}
}

// 只能打开目录的实现（gio open）应排在最后，但可用时不报错。
func TestRevealCmdFallsBackToDirectory(t *testing.T) {
	const path = "/tmp/x/y.bin"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gio"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer setPath(t, bin)()
	cmd, err := revealCmd(path)
	if err != nil {
		t.Fatalf("仅有 gio 时也应可用: %v", err)
	}
	if filepath.Base(cmd.Path) != "gio" {
		t.Errorf("应回落到 gio, got %s", cmd.Path)
	}
	if strings.Join(cmd.Args[1:], " ") != "open /tmp/x" {
		t.Errorf("gio 参数异常: %#v", cmd.Args)
	}
}

// RevealInFolder 不得在命令组装失败时静默成功。
func TestRevealInFolderPropagatesError(t *testing.T) {
	a, _, root := newProbeApp(t)
	p := filepath.Join(root, "a.bin") // newProbeApp 已建同名文件
	if _, err := os.Stat(p); err != nil {
		t.Skipf("装置文件缺失: %v", err)
	}
	a.mu.Lock()
	a.byID[4242] = &model.FileEntry{ID: 4242, Path: p}
	a.mu.Unlock()
	defer setPath(t, "")()
	if err := a.RevealInFolder(4242); err == nil {
		t.Error("无任何定位工具时应返回错误（前端据此提示）")
	}
}
