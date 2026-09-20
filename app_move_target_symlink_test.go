package main

// AS-H3（2026-09-20 全仓审计，第 7 篇 §三）：moveTargetAllowed 的符号链接逃逸门禁取 OR。
//
// 修正前 candidates = [abs, EvalSymlinks(abs)]，**任一**候选落在**任一**授权目录内
// 即放行。授权目录内部的一个符号链接（root/esc → outside/）使 abs 这一候选命中授权，
// 而解析后的越界候选被完全无视；随后 move.go 的 MkdirAll + Rename 就经这条链接写到
// 授权范围之外。H5 白名单的语义由此从「目标必须在授权内」退化为
// 「目标字符串碰巧在授权附近」。
//
// 本文件同时钉住反方向：解析不得把**合法**目标误拒（符号化授权目录、未创建的目标目录）。

import (
	"os"
	"path/filepath"
	"testing"
)

func allowedWithAuth(t *testing.T, a *App, target string) bool {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.moveTargetAllowed(target)
}

// TestMoveTargetRejectedThroughSymlinkInsideAuthDir 授权目录内的链接指向外部 → 必须拒绝。
func TestMoveTargetRejectedThroughSymlinkInsideAuthDir(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	outside := t.TempDir()
	esc := filepath.Join(root, "esc")
	if err := os.Symlink(outside, esc); err != nil {
		t.Skipf("平台不支持符号链接: %v", err)
	}
	a.authorizeDir(root)

	if allowedWithAuth(t, a, esc) {
		t.Fatalf("moveTargetAllowed(%q) 放行：链接目标 %q 在授权范围之外，"+
			"随后的 MkdirAll+Rename 会写到用户从未授权的位置（AS-H3）", esc, outside)
	}
	// 链接之下的更深路径同样要拒（abs 前缀命中授权，真实位置不命中）
	if allowedWithAuth(t, a, filepath.Join(esc, "subdir")) {
		t.Fatalf("moveTargetAllowed 放行了 %q 之下的子目录：经链接逃逸", esc)
	}
	// 越界目标必须真的还在授权外（防测试构造本身失效）
	if got, err := filepath.EvalSymlinks(esc); err != nil || got == "" {
		t.Fatalf("链接解析失败，测试构造无效: %v", err)
	}
}

// TestMoveTargetRejectedWhenAuthDirIsSymlinkEscape 授权的**父目录**里放链接：
// 目标字符串以授权目录为前缀，但真实位置在授权外 → 拒。
func TestMoveTargetRejectedWhenAuthDirIsSymlinkEscape(t *testing.T) {
	a := newTestApp(t)
	realDir := t.TempDir()
	outside := t.TempDir()

	// authRoot 指向 outside，用户授权的是 outside 之下另一处；
	// 攻击面：以 authRoot 为前缀拼出 outside 之外的路径。
	authRoot := filepath.Join(realDir, "auth")
	if err := os.MkdirAll(authRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// link 在 outside 内，指向 outside 之外的一处真实目录
	evil := filepath.Join(realDir, "evil")
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(outside, "link")
	if err := os.Symlink(evil, link); err != nil {
		t.Skipf("平台不支持符号链接: %v", err)
	}
	a.authorizeDir(outside)

	if allowedWithAuth(t, a, link) {
		t.Fatalf("moveTargetAllowed(%q) 放行：abs 前缀命中授权 %q，"+
			"真实位置 %q 在授权外（AS-H3）", link, outside, evil)
	}
	// 对照：outside 内的真实目录仍须放行（防把守卫修成一律拒绝）
	if !allowedWithAuth(t, a, filepath.Join(outside, "legit")) {
		t.Fatal("outside 之下的合法目标被误拒：守卫不得过严")
	}
}

// TestMoveTargetAllowedForLegitTargets 负控制：合法目标必须放行，
// 含授权目录本身、其下已存在的子目录、以及**尚未创建**的子目录。
func TestMoveTargetAllowedForLegitTargets(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a.authorizeDir(root)

	for _, p := range []string{root, sub, filepath.Join(root, "not-yet-created"), filepath.Join(sub, "deep", "deeper")} {
		if !allowedWithAuth(t, a, p) {
			t.Fatalf("合法目标 %q 被误拒（AS-H3 的修法不得把守卫改成恒拒绝）", p)
		}
	}
}

// TestMoveTargetAllowedWhenAuthDirItselfIsSymlink 授权目录本身是链接时，
// 经该链接进入的合法目标必须仍放行——真实路径已随 authorizeDir 一并登记。
func TestMoveTargetAllowedWhenAuthDirItselfIsSymlink(t *testing.T) {
	realDir := t.TempDir()
	linkBase := t.TempDir()
	link := filepath.Join(linkBase, "authlink")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("平台不支持符号链接: %v", err)
	}
	a := newTestApp(t)
	a.authorizeDir(link)

	if !allowedWithAuth(t, a, filepath.Join(link, "dest")) {
		t.Fatalf("授权目录 %q 本身是符号链接，其下的合法目标被误拒: real=%q",
			link, realDir)
	}
}

// TestMoveTargetRejectedOutsideAuth 最基本的白名单行为不得回退。
func TestMoveTargetRejectedOutsideAuth(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	elsewhere := t.TempDir()
	a.authorizeDir(root)

	if !allowedWithAuth(t, a, root) {
		t.Fatal("授权目录自身应被接受")
	}
	if allowedWithAuth(t, a, elsewhere) {
		t.Fatalf("未授权目录 %q 被放行", elsewhere)
	}
	if allowedWithAuth(t, a, "") {
		t.Fatal("空目标必须拒绝")
	}
}
