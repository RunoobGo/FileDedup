package ops

// ============================================================================
// 软链接合并的回撤（2026-09-20）
//
// 回撤的判据与硬链接**不同**，本文件专门把差异钉住：
//
//   硬链接回撤：从 LinkSrc 复制内容回来 → 必须复核"源未被篡改"，
//              否则会把被改过的内容当成原文件恢复出来
//   软链接回撤：数据本体一直在备份（.fdd-old）里 → 不需要复制、不可能恢复出
//              错误内容，但**必须**确认原位还是"我们建的那个链接"，
//              否则会删掉用户自己放进去的文件
//
// 因此三条核心断言：
//   ① 正常回撤后 dup 恢复为普通文件、内容完整
//   ② 原位被换成第三方文件 → 拦截，绝不删除
//   ③ 链接已悬空（保留项被删）→ 仍能回撤（这正是用户最需要回撤的场景）
// ============================================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// symlinkUndoFixture 造一个"已完成软链接合并"的现场：
// dup 位置是链接、dup.fdd-old 是原始独立副本（真实调用 SymlinkMerge 产生）。
//
// 为什么要临时把 workTempRemove 换掉：合并**成功**的最后一件事就是删除备份，
// 留着备份反而是失败路径。而回撤正是要还原那个备份，所以现场必须保留它。
// 用"模拟删除失败"这个既有注入点来定格中间态，比手写一遍合并逻辑更可信
// ——造出来的现场就是真实代码产生的，不会与实现漂移。
func symlinkUndoFixture(t *testing.T) (keep, dup string, content string) {
	t.Helper()
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	content = "UNDO-FIXTURE-CONTENT-0123456789"
	keep, dup = writePair(t, dir, content)

	origRemove := workTempRemove
	workTempRemove = func(string) error { return os.ErrPermission }
	err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	workTempRemove = origRemove

	// 预期得到 ResidueError：链接已建好，备份被"留下"
	var residue *ResidueError
	if !asResidue(err, &residue) {
		t.Fatalf("现场准备失败（期望残留告警）: %v", err)
	}
	if _, serr := os.Lstat(dup + FddOldSuffix); serr != nil {
		t.Fatalf("现场准备失败：备份应保留: %v", serr)
	}
	return keep, dup, content
}

// TestUndoSymlinkRestoresOriginal 正常回撤：链接拆除、备份归位、内容完整。
func TestUndoSymlinkRestoresOriginal(t *testing.T) {
	keep, dup, content := symlinkUndoFixture(t)

	restored, err := UndoOne(UndoItem{
		Kind: "symlink", OrigPath: dup, LinkSrc: keep,
		Size: uint64(len(content)),
	})
	if err != nil {
		t.Fatalf("回撤失败: %v", err)
	}
	if restored != dup {
		t.Fatalf("回撤落点应为 %s，实得 %s", dup, restored)
	}

	// dup 必须是普通文件（不再是链接）
	li, err := os.Lstat(dup)
	if err != nil {
		t.Fatalf("dup 应存在: %v", err)
	}
	if li.Mode()&os.ModeSymlink != 0 {
		t.Fatal("❌ 回撤后 dup 不应仍是符号链接")
	}
	// 内容必须与原始一致
	if b, err := os.ReadFile(dup); err != nil || string(b) != content {
		t.Fatalf("❌ 回撤后内容不符:\n  want %q\n  got  %q err=%v", content, b, err)
	}
	// 保留源不能被牵连
	if b, err := os.ReadFile(keep); err != nil || string(b) != content {
		t.Fatalf("❌ 保留源被牵连: %q err=%v", b, err)
	}
	// 备份必须已归位（不再残留 .fdd-old）
	if _, err := os.Lstat(dup + FddOldSuffix); err == nil {
		t.Fatal("❌ 备份应已被改名归位，不应残留")
	}
}

// TestUndoSymlinkBlocksSwappedTarget ★ 核心安全用例 ★
//
// 用户在合并后把链接删掉、换成自己的文件 → 回撤必须拦截，
// 绝不能把这个第三方文件删掉、把备份顶上去。
func TestUndoSymlinkBlocksSwappedTarget(t *testing.T) {
	keep, dup, _ := symlinkUndoFixture(t)

	// 用户把链接换成自己的文件
	if err := os.Remove(dup); err != nil {
		t.Fatal(err)
	}
	const thirdParty = "THIRD-PARTY-IMPORTANT-DATA"
	if err := os.WriteFile(dup, []byte(thirdParty), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := UndoOne(UndoItem{
		Kind: "symlink", OrigPath: dup, LinkSrc: keep, Size: 29,
	})
	if err == nil {
		t.Fatal("❌ 原位已被换成第三方文件时，回撤必须拦截")
	}
	if !strings.Contains(err.Error(), "已不是本次创建的软链接") {
		t.Fatalf("错误信息应说明拦截原因，实得: %v", err)
	}

	// 第三方文件必须完好无损
	if b, rerr := os.ReadFile(dup); rerr != nil || string(b) != thirdParty {
		t.Fatalf("❌ 第三方文件被破坏: %q err=%v", b, rerr)
	}
	// 备份必须仍在（用户还能手动恢复）
	if _, serr := os.Lstat(dup + FddOldSuffix); serr != nil {
		t.Fatalf("❌ 拦截后备份应原样保留: %v", serr)
	}
}

// TestUndoSymlinkDanglingLinkStillUndoable 链接已悬空（保留项被删）仍可回撤。
//
// 这是**最重要的可回撤场景**：用户删掉了保留项，发现链接打不开，
// 想通过回撤恢复出原来的独立文件。回撤依赖的是备份，与链接是否有效无关，
// 因此必须成功。
func TestUndoSymlinkDanglingLinkStillUndoable(t *testing.T) {
	keep, dup, content := symlinkUndoFixture(t)

	// 保留项被用户删除 → 链接悬空
	if err := os.Remove(keep); err != nil {
		t.Fatal(err)
	}
	if !symlinkIsDangling(dup) {
		t.Fatal("现场准备有误：链接应已悬空")
	}

	restored, err := UndoOne(UndoItem{
		Kind: "symlink", OrigPath: dup, LinkSrc: keep,
		Size: uint64(len(content)),
	})
	if err != nil {
		t.Fatalf("❌ 悬空链接仍应可回撤（数据在备份里）: %v", err)
	}
	if restored != dup {
		t.Fatalf("回撤落点应为 %s，实得 %s", dup, restored)
	}
	if b, rerr := os.ReadFile(dup); rerr != nil || string(b) != content {
		t.Fatalf("❌ 回撤后内容应完整: %q err=%v", b, rerr)
	}
	li, _ := os.Lstat(dup)
	if li.Mode()&os.ModeSymlink != 0 {
		t.Fatal("❌ 回撤后不应仍是链接")
	}
}

// TestUndoSymlinkMissingBackup 备份缺失 → 明确报错，且**不动链接**。
//
// 宁可让用户看到"缺备份"，也不要在数据不完整的情况下破坏现场。
func TestUndoSymlinkMissingBackup(t *testing.T) {
	keep, dup, _ := symlinkUndoFixture(t)

	if err := os.Remove(dup + FddOldSuffix); err != nil {
		t.Fatal(err)
	}

	_, err := UndoOne(UndoItem{Kind: "symlink", OrigPath: dup, LinkSrc: keep})
	if err == nil {
		t.Fatal("❌ 备份缺失应报错")
	}
	if !strings.Contains(err.Error(), "找不到合并前的原始备份") {
		t.Fatalf("错误信息应说明缺备份，实得: %v", err)
	}
	// 链接必须保留未动（不能删了链接才发现没备份）
	if !symlinkIsDangling(dup) {
		if li, _ := os.Lstat(dup); li.Mode()&os.ModeSymlink == 0 {
			t.Fatal("❌ 备份缺失时不应改动链接")
		}
	}
}

// TestUndoSymlinkMissingLinkSrc 账本无 LinkSrc（旧记录）→ 拒绝执行（无从校验）。
func TestUndoSymlinkMissingLinkSrc(t *testing.T) {
	keep, dup, _ := symlinkUndoFixture(t)

	_, err := UndoOne(UndoItem{Kind: "symlink", OrigPath: dup}) // LinkSrc 为空
	if err == nil {
		t.Fatal("❌ 缺少目标记录时应拒绝回撤")
	}
	if !strings.Contains(err.Error(), "缺少链接目标记录") {
		t.Fatalf("错误信息应说明原因，实得: %v", err)
	}
	// 场景未被破坏
	if _, serr := os.Lstat(dup + FddOldSuffix); serr != nil {
		t.Fatalf("❌ 拒绝回撤时不应改动现场: %v", serr)
	}
	_ = keep
}

// TestUndoSymlinkCrashResidueRecognized 崩溃残局自检：
// 上次回撤已删掉链接、把备份改名回原位，只是账本没落上。
// 此时原位是"内容等于记录哈希的普通文件" → 应记为已还原（幂等），
// 而不是报"已不是链接"让用户以为出了问题。
func TestUndoSymlinkCrashResidueRecognized(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "dup.bin")
	const content = "ALREADY-UNDONE-CONTENT"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// 造一个残留备份（模拟"改名失败"或"备份没删掉"）
	if err := os.WriteFile(p+FddOldSuffix, []byte("STALE-BACKUP"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := hashFile(p)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := UndoOne(UndoItem{
		Kind: "symlink", OrigPath: p, LinkSrc: filepath.Join(dir, "keep.bin"),
		Hash: h, Size: uint64(len(content)),
	})
	if err != nil {
		t.Fatalf("❌ 崩溃残局应被识别为已还原: %v", err)
	}
	if restored != p {
		t.Fatalf("应返回原路径，实得 %s", restored)
	}
	if b, _ := os.ReadFile(p); string(b) != content {
		t.Fatalf("内容不应被改动: %q", b)
	}
}

// TestUndoSymlinkNoFollowSwapWithAnotherLink 原位被换成**指向别处**的链接 → 拦截。
//
// 比"换成普通文件"更隐蔽的一种替换：它仍然是个链接，模式检查会通过。
func TestUndoSymlinkNoFollowSwapWithAnotherLink(t *testing.T) {
	keep, dup, _ := symlinkUndoFixture(t)

	// 用户把链接重建为指向另一个文件
	elsewhere := filepath.Join(filepath.Dir(dup), "elsewhere.bin")
	if err := os.WriteFile(elsewhere, []byte("OTHER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dup); err != nil {
		t.Fatal(err)
	}
	if err := symlinkCreate(elsewhere, dup); err != nil {
		t.Fatal(err)
	}

	_, err := UndoOne(UndoItem{Kind: "symlink", OrigPath: dup, LinkSrc: keep, Size: 15})
	if err == nil {
		t.Fatal("❌ 指向别处的链接不应通过回撤校验")
	}
	t.Logf("✅ 正确拦截：%v", err)
	// 关键：那个"别处"的文件绝不能被碰
	if b, rerr := os.ReadFile(elsewhere); rerr != nil || string(b) != "OTHER" {
		t.Fatalf("❌ 第三方目标文件被破坏: %q err=%v", b, rerr)
	}
}

// TestUndoSymlinkIdempotent 已回撤后再回撤一次：第二次应报错但不破坏内容。
func TestUndoSymlinkIdempotent(t *testing.T) {
	keep, dup, content := symlinkUndoFixture(t)
	it := UndoItem{Kind: "symlink", OrigPath: dup, LinkSrc: keep, Size: uint64(len(content))}

	if _, err := UndoOne(it); err != nil {
		t.Fatalf("首次回撤失败: %v", err)
	}
	// 第二次：备份已经归位，无备份可还原 → 应报错（或按已还原识别）
	_, err := UndoOne(it)
	if err == nil {
		// 也可以接受"识别为已还原"的实现；这里明确断言不破坏内容
		t.Log("第二次回撤被接受（按已还原处理）")
	} else {
		t.Logf("第二次回撤被拒（预期）: %v", err)
	}
	if b, rerr := os.ReadFile(dup); rerr != nil || string(b) != content {
		t.Fatalf("❌ 重复回撤破坏了内容: %q err=%v", b, rerr)
	}
}

// TestUndoOneDispatchIncludesSymlink 分派表必须认 symlink
// （防"功能写完但忘了在 UndoOne 的 switch 里注册"）。
func TestUndoOneDispatchIncludesSymlink(t *testing.T) {
	_, err := UndoOne(UndoItem{Kind: "symlink", OrigPath: "/nonexistent/x", LinkSrc: "/nonexistent/y"})
	if err == nil {
		t.Fatal("路径不存在应报错")
	}
	if strings.Contains(err.Error(), "不支持回撤的操作类型") {
		t.Fatalf("❌ symlink 未在 UndoOne 中注册: %v", err)
	}
}
