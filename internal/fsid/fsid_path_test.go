package fsid

// 2026-09-20（跨卷软链接合并）：FromPath / FromPathNoFollow 的语义分界测试。
//
// 这两个函数的差异是软链接合并能否正确复核的**唯一依据**：
//   - FromPathNoFollow(链接) → 链接自身的身份（≠ 目标）
//   - FromPath(链接)         → 目标的身份（== 目标）
//
// 如果某天有人把它们的实现调换/合并，软链接的终局复核会在"每次成功合并时
// 报失败"，或将"链接被换成普通文件"误判为通过。本文件把这条语义钉死。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// canSymlink 探测当前环境是否允许创建符号链接。
// Windows 未提权且未开开发者模式时会失败——此时相关用例跳过而非失败，
// 因为那属于环境限制，不是代码缺陷（真机专项见 docs 的 Windows 验证清单）。
func canSymlink(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, ".__probe_target")
	link := filepath.Join(dir, ".__probe_link")
	if err := os.WriteFile(probe, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(probe, link); err != nil {
		t.Logf("本环境不支持创建符号链接（%v），跳过", err)
		return false
	}
	os.Remove(link)
	os.Remove(probe)
	return true
}

// TestFromPathFollowsSymlink 核心语义：FromPath 跟随，FromPathNoFollow 不跟随。
func TestFromPathFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	if !canSymlink(t, dir) {
		t.Skip("环境不支持符号链接")
	}
	target := filepath.Join(dir, "target.bin")
	link := filepath.Join(dir, "link.bin")
	if err := os.WriteFile(target, []byte("PAYLOAD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	tid, err := FromPath(target)
	if err != nil {
		t.Fatalf("FromPath(target) 失败: %v", err)
	}
	lid, err := FromPath(link)
	if err != nil {
		t.Fatalf("FromPath(link) 失败: %v", err)
	}
	nfid, err := FromPathNoFollow(link)
	if err != nil {
		t.Fatalf("FromPathNoFollow(link) 失败: %v", err)
	}

	// ① 跟随：链接解析出的身份必须等于目标身份
	if !tid.SameIdentity(lid) {
		t.Fatalf("❌ FromPath(链接) 未跟随到目标：目标 %+v vs 链接 %+v", tid, lid)
	}
	// ② 不跟随：链接自身的身份必须**不等于**目标身份
	//    （unix 保证；Windows 上链接是独立的重解析点对象，同样不等）
	if tid.Resolved && nfid.Resolved && tid.SameIdentity(nfid) {
		t.Fatalf("❌ FromPathNoFollow(链接) 跟随了链接：%+v 与目标相同", nfid)
	}
	// ③ FromPathNoFollow(目标) 与 FromPath(目标) 必须一致
	tidNF, err := FromPathNoFollow(target)
	if err != nil {
		t.Fatal(err)
	}
	if tid.Resolved != tidNF.Resolved {
		t.Fatalf("普通文件上两条路径解析结果不一致: %+v vs %+v", tid, tidNF)
	}
	if tid.Resolved && !tid.SameIdentity(tidNF) {
		t.Fatalf("普通文件上 FromPath 与 FromPathNoFollow 身份不符: %+v vs %+v", tid, tidNF)
	}

	if runtime.GOOS != "windows" && (!lid.Resolved || !nfid.Resolved) {
		t.Fatalf("unix 上两侧都应解析出身份: FromPath=%+v FromPathNoFollow=%+v", lid, nfid)
	}
}

// TestFromPathDanglingSymlinkErrors 悬空链接：FromPath 必须报错（目标不可达），
// 而 FromPathNoFollow 仍能成功（链接对象本身还在）。
//
// 这正是"悬空检测"能工作的前提：检测方用 FromPath 探活，用 NoFollow 查存在性。
func TestFromPathDanglingSymlinkErrors(t *testing.T) {
	dir := t.TempDir()
	if !canSymlink(t, dir) {
		t.Skip("环境不支持符号链接")
	}
	target := filepath.Join(dir, "gone.bin")
	link := filepath.Join(dir, "dangling.bin")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	if _, err := FromPath(link); err == nil {
		t.Fatal("❌ 悬空链接上 FromPath 应报错（目标不可达）")
	} else {
		t.Logf("✅ FromPath 正确报出目标不可达: %v", err)
	}
	if _, err := FromPathNoFollow(link); err != nil {
		t.Fatalf("❌ 悬空链接自身仍是有效对象，FromPathNoFollow 不应报错: %v", err)
	}
}

// TestFromPathMissingFileErrors 路径根本不存在时两者都应报错。
func TestFromPathMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.bin")
	if _, err := FromPath(missing); err == nil {
		t.Fatal("不存在的路径 FromPath 应报错")
	}
	if _, err := FromPathNoFollow(missing); err == nil {
		t.Fatal("不存在的路径 FromPathNoFollow 应报错")
	}
}

// TestFromPathRegularFileMatchesHandle 普通文件上 FromPath 应与句柄查询一致
// （非 Windows：两条 API 语义对齐，不得各说各话）。
func TestFromPathRegularFileMatchesHandle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的 Stat 产物不带卷号/索引，两侧实现路径不同，另有专用测试")
	}
	dir := t.TempDir()
	p := mkTempFile(t, dir, "a.bin")
	id, err := FromPath(p)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if hid := FromFile(f); !id.SameIdentity(hid) || id.Resolved != hid.Resolved {
		t.Fatalf("FromPath %+v 与 FromFile %+v 不符", id, hid)
	}
}
