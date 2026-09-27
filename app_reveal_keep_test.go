package main

// M290/M297 的实施腿（用户裁定：改文案 + 给直达出口，**不**改备份生命周期）。
//
// 真机定案摆在那里：合并成功路径无条件删 `.fdd-old`，回撤把"备份必须在"当硬前提
// ⇒ 软链接合并的应用内回撤**走不到**，数据只在保留源那一份上。既然不替用户搬回来，
// 界面至少要把"去哪儿拿回它"给出来 —— 本方法就是那个出口：
//   - 保留源还在 → 打开所在文件夹并**选中它**（与 RevealPath 同一条腿）；
//   - 保留源已被删/挪走（悬空那一臂，正是最需要找回数据的场景）→ 打开**它所在的目录**；
//   - 连目录都不可达（盘拔了、网络卷断了）→ 明确报错，不弹空窗。
//
// ★ 为什么不在前端算父目录：路径语义（`E:\a\b` 与 `/a/b`、尾分隔符、UNC）归 Go，
//   前端再写一份就是 M116/AS-H6 那一族"两份实现各自漂移"的成因。
// ★ 为什么复用 RevealPath 而不是新装配命令：选中/打开的判据（目录不带选中参数）
//   已经由 app_reveal_path_test.go 钉住，这里只新增"目标不在时退一档到父目录"这一条。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRevealKeepSourceSelectsExistingTarget 保留源在场：定位到它并选中。
func TestRevealKeepSourceSelectsExistingTarget(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	root := t.TempDir()
	dir := filepath.Join(root, "kept dir; echo pwned")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.RevealKeepSource(keep); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("exec 次数 = %d，应为 1", len(*got))
	}
	argv := strings.Split((*got)[0], "\x00")
	if runtime.GOOS == "windows" {
		if argv[0] != "explorer" || len(argv) != 3 || argv[1] != "/select," || argv[2] != keep {
			t.Errorf("保留源在场时应为 explorer /select, <保留源>：%q", argv)
		}
		return
	}
	// linux 的探测表是环境相关的（revealCmd 优先级注释的两档设计）：选中档
	// （nautilus/dolphin/thunar/nemo）收到保留源本身，只开档（pcmanfm 收目录、
	// gio/xdg-open 收父目录）收到的只是目录。M312：CI runner 上装的是 gio，
	// 走的就是只开档 ⇒ 判据按"两档任一"断言；containsExact 的整串不拆
	// （名字里的 `; echo pwned` 注入形状）逐字保留。
	parent := filepath.Dir(keep)
	if !containsExact(argv, keep) && !containsExact(argv, parent) {
		t.Errorf("argv 里应恰好有一个元素是完整保留源路径或完整父目录：%q", argv)
	}
}

// TestRevealKeepSourceFallsBackToParentDir 悬空那一臂（M290 真机形状：保留源被外部删掉）
// ——目标本身没了，但用户要的"放回原位"靠的是**那个目录**：打开目录，不带选中参数。
func TestRevealKeepSourceFallsBackToParentDir(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	dir := filepath.Join(t.TempDir(), "kept dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "dup payload; echo pwned.bin") // 从未创建 = 保留源已没
	if err := a.RevealKeepSource(gone); err != nil {
		t.Fatalf("保留源没了但目录还在，这一臂必须成功：%v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("exec 次数 = %d，应为 1", len(*got))
	}
	argv := strings.Split((*got)[0], "\x00")
	for _, el := range argv {
		if el == "-R" || el == "--select" || strings.HasPrefix(el, "/select,") {
			t.Errorf("保留源已不存在，不该再要求选中它（%q）：%q", el, argv)
		}
	}
	if !containsExact(argv, dir) {
		t.Errorf("应送出**完整**父目录（不得被拆开/改写）：%q", argv)
	}
}

// TestRevealKeepSourceRejectsUnreachableVolume 目录也不可达（盘拔了/网络卷断了）：
// 错误必须回得到用户手上，而不是弹一个空窗——这与 checkRevealPath 同一条理由。
func TestRevealKeepSourceRejectsUnreachableVolume(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	ghost := filepath.Join(t.TempDir(), "gone dir", "gone.bin")
	err := a.RevealKeepSource(ghost)
	if err == nil || !strings.Contains(err.Error(), "不可达") {
		t.Errorf("保留源与所在目录都不存在时应报「不可达」：%v", err)
	}
	if len(*got) != 0 {
		t.Errorf("校验拒绝前就已 exec（弹出的只会是空窗）：%v", *got)
	}
}

// TestRevealKeepSourceRejectsBlank 空白路径一律拒（与 RevealPath/OpenPath 同一档）。
func TestRevealKeepSourceRejectsBlank(t *testing.T) {
	a, _ := newHistApp(t)
	got, restore := stubRevealExec(t)
	defer restore()
	for _, bad := range []string{"", "   ", "\t\n"} {
		if err := a.RevealKeepSource(bad); err == nil || !strings.Contains(err.Error(), "路径为空") {
			t.Errorf("RevealKeepSource(%q) 错误不符：%v", bad, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("空路径不得 exec：%v", *got)
	}
}

// containsExact 报告 argv 里是否有**恰好等于** want 的那个元素（整串一个元素 = 未经 shell 拆分）。
func containsExact(argv []string, want string) bool {
	n := 0
	for _, el := range argv {
		if el == want {
			n++
		}
	}
	return n == 1
}
