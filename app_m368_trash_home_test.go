package main

// 第八轮批 3（拟 M368）P-27：拿不到主目录时，OpenTrash 一条命令都不许发出去。
//
// 取证在册（§3.1b）：`app_ops.go` 的 darwin 臂与 linux 臂都写的是
// `home, _ := os.UserHomeDir()`，忽略错误后直接 `filepath.Join`。HOME 未设时 `home`
// 是空串 ⇒ 拼出来的是 `.Trash` 与 `.local/share/Trash/files` 这两个**相对当前工作目录**
// 的名字：命令"成功启动"、什么都没打开，而用户此刻正是清理完想找回东西的那个人。
// 本仓已有的正确形状在 `app_lifecycle.go`（`} else if home, herr := os.UserHomeDir(); herr == nil {`），
// M60 也判死过"取不到目录就不动"这一档。
//
// ★ 正控制格（M93 的教训）：同一夹具在 HOME 有值时必须**真的把命令发出去**，
//   否则"没发命令"这件事无法归因给被测的那道检查。

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestM368OpenTrashWithoutHomeStartsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 臂不吃 HOME（走的是 explorer + shell:RecycleBinFolder），这一格的前提不在")
	}
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()

	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "") // linux 臂的兜底名同样从主目录拼出来

	err := a.OpenTrash()
	if err == nil {
		t.Fatalf("M368：拿不到主目录却报告「已打开回收站」（记录数=%d）", len(*got))
	}
	if !strings.Contains(err.Error(), "主目录") {
		t.Errorf("M368：错误没点名是哪一步失败，用户无从自查：%v", err)
	}
	if !hasCJKText(err.Error()) {
		t.Errorf("M368：错误没走中文壳：%v", err)
	}
	if len(*got) != 0 {
		t.Errorf("M368：前提不成立仍把命令发了出去（相对路径会指向当前工作目录）：%q", *got)
	}
}

// 正控制：HOME 有值时这一格必须真的走到执行缝，否则上面的"零条命令"是夹具的功劳。
func TestM368OpenTrashWithHomeStillExecutes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 臂不吃 HOME，正控制格同样不在这一臂")
	}
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	if err := a.OpenTrash(); err != nil {
		t.Fatalf("M368：HOME 有值时被拒了（正控制不成立）：%v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("M368：执行缝收到的命令数 = %d, want 1（%q）", len(*got), *got)
	}
	argv := strings.Split((*got)[0], "\x00")
	want := filepath.Join(home, ".Trash")
	if runtime.GOOS != "darwin" {
		want = filepath.Join(home, ".local", "share", "Trash", "files")
	}
	if argv[len(argv)-1] != want {
		t.Errorf("M368：发出去的路径不是主目录下的回收站：%q want %q", argv[len(argv)-1], want)
	}
}
