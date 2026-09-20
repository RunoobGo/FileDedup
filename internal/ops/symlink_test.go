package ops

// ============================================================================
// 跨卷软链接合并回归（2026-09-20）
//
// 本文件的核心目的有两个：
//
//  1. 用**用户可观测的判据**断言合并成立，而不是只看函数返回值：
//     判据 A：dup 现在是一个符号链接（Lstat 的 ModeSymlink）
//     判据 B：读 dup 得到的是 keep 的内容；改写 keep，dup 跟着变
//     判据 C：失败时 dup 必须还原为**普通文件**、内容完好、无残留、不假成功
//
//  2. **钉死"复核解析目标"这一最易错点**（TestSymlinkMergeVerifiesResolvedTarget）。
//     若有人把硬链接的 identityStill(tmp, keepID) 照搬过来，每次合并都会
//     报"保留源在校验后被替换"——那个用例会立刻失败。
// ============================================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// requireSymlinkSupport 探测环境是否允许创建符号链接。
//
// 为什么不直接 Skip 所有用例：CI 的 Linux/macOS 一定能建；Windows 上未提权
// 且未开开发者模式时会失败——那时跳过并**打印原因**，真机专项走
// scripts/test-windows-quarantine.sh 登记的手工清单。
func requireSymlinkSupport(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, ".__sym_probe")
	link := filepath.Join(dir, ".__sym_probe_link")
	if err := os.WriteFile(probe, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	if err := os.Symlink(probe, link); err != nil {
		t.Skipf("本环境不支持创建符号链接，跳过（%v）", err)
	}
	os.Remove(link)
}

// writePair 造一对同内容的独立文件。
func writePair(t *testing.T, dir, content string) (keep, dup string) {
	t.Helper()
	keep = filepath.Join(dir, "keep.bin")
	dup = filepath.Join(dir, "dup.bin")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return keep, dup
}

// assertSymlinkCriterion 用用户的两条实操判据校验软链接是否真的成立。
//
// 注意与 assertUserCriterion（硬链接）的区别：软链接**不能**用
// os.SameFile(keep, dup)——链接自身的身份与目标不同，那会必然失败。
// 软链接的正确判据是"打开 dup 读到的就是 keep 的数据"。
func assertSymlinkCriterion(t *testing.T, keep, dup string) {
	t.Helper()

	// 判据 A：dup 必须是符号链接（不是普通文件）
	li, err := os.Lstat(dup)
	if err != nil {
		t.Fatalf("lstat dup: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("❌ 判据A失败：%s 不是符号链接（mode=%v）", dup, li.Mode())
	}

	// 判据 B：内容跟随 keep 变化
	orig, err := os.ReadFile(dup)
	if err != nil {
		t.Fatalf("read dup: %v", err)
	}
	marker := append([]byte("TOUCHED-"), orig...)
	if err := os.WriteFile(keep, marker, 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	got, err := os.ReadFile(dup)
	if err != nil {
		t.Fatalf("read dup after write: %v", err)
	}
	if string(got) != string(marker) {
		t.Fatalf("❌ 判据B失败：改写 keep 后经链接读到的内容未同步\n"+
			"  keep 写入 = %q\n  dup 读出  = %q", marker, got)
	}
	// 反向：经链接写入，keep 也必须变（说明落到了同一份数据上）
	rev := append([]byte("VIA-LINK-"), marker...)
	if err := os.WriteFile(dup, rev, 0o644); err != nil {
		t.Fatalf("write via link: %v", err)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != string(rev) {
		t.Fatalf("❌ 判据B(反向)失败：经链接写入后 keep 未同步: %q err=%v", got, err)
	}
}

// assertDupIsPlainFileWithContent 断言 dup 已被还原为普通文件且内容等于 want。
func assertDupIsPlainFileWithContent(t *testing.T, dup, want string) {
	t.Helper()
	li, err := os.Lstat(dup)
	if err != nil {
		t.Fatalf("❌ dup 应存在: %v", err)
	}
	if li.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("❌ 失败后 dup 不应残留为符号链接")
	}
	b, err := os.ReadFile(dup)
	if err != nil {
		t.Fatalf("❌ 读取 dup 失败: %v", err)
	}
	if string(b) != want {
		t.Fatalf("❌ dup 内容被破坏:\n  want %q\n  got  %q", want, b)
	}
}

// symbollessResidue 确认失败/成功后都不留 .fdd-tmp / .fdd-old / .fdd-old.undo。
func assertNoSymlinkResidue(t *testing.T, dup string) {
	t.Helper()
	for _, suf := range []string{".fdd-tmp", ".fdd-old", ".fdd-old.undo"} {
		if _, err := os.Lstat(dup + suf); err == nil {
			t.Errorf("❌ 残留临时文件: %s%s", dup, suf)
		}
	}
}

// ---------------------------------------------------------------------------
// verifySymlinked 单测
// ---------------------------------------------------------------------------

// TestVerifySymlinkedRejectsRegularFile 普通文件（哪怕内容相同）不得通过复核。
//
// 这一条防的是"把 verifyHardlinked 的实现照搬过来"：硬链接的复核只比身份，
// 而软链接必须先确认"它是个链接"。省掉 ① 层会让任何普通文件都能通过。
func TestVerifySymlinkedRejectsRegularFile(t *testing.T) {
	dir := t.TempDir()
	keep, dup := writePair(t, dir, "SAME-CONTENT")

	err := verifySymlinked(keep, dup)
	if err == nil {
		t.Fatal("❌ 普通文件不应通过软链接复核")
	}
	if !strings.Contains(err.Error(), "不是符号链接") {
		t.Fatalf("❌ 错误信息应指出「不是符号链接」，实得: %v", err)
	}
	t.Logf("✅ 正确拒绝普通文件: %v", err)
}

// TestVerifySymlinkedAcceptsCorrectLink 正确的链接必须通过。
func TestVerifySymlinkedAcceptsCorrectLink(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, _ := writePair(t, dir, "PAYLOAD")
	link := filepath.Join(dir, "link.bin")
	if err := createSymlink(keep, link); err != nil {
		t.Fatalf("createSymlink: %v", err)
	}
	if err := verifySymlinked(keep, link); err != nil {
		t.Fatalf("❌ 正确链接应通过复核: %v", err)
	}
}

// TestVerifySymlinkedRejectsWrongTarget 指向**另一个文件**的链接必须被拒。
//
// 防的是"链接建出来了，但指错了地方"——例如 keep 路径被换成另一个文件后，
// 或者并发场景下拿错了 keep。
func TestVerifySymlinkedRejectsWrongTarget(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, _ := writePair(t, dir, "KEEP-CONTENT")
	other := filepath.Join(dir, "other.bin")
	if err := os.WriteFile(other, []byte("OTHER-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.bin")
	if err := createSymlink(other, link); err != nil { // 指向 other 而非 keep
		t.Fatalf("createSymlink: %v", err)
	}

	err := verifySymlinked(keep, link)
	if err == nil {
		t.Fatal("❌ 指向别的文件的链接不应通过复核")
	}
	t.Logf("✅ 正确拒绝错误目标: %v", err)
}

// TestVerifySymlinkedDetectsDangling 悬空链接（目标已删）必须被拒。
//
// 这是 R1（悬空链接）在代码层面的第一道拦截：绝不允许把一个"打开就报错"
// 的链接当成合并成功。
func TestVerifySymlinkedDetectsDangling(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, _ := writePair(t, dir, "WILL-BE-GONE")
	link := filepath.Join(dir, "link.bin")
	if err := createSymlink(keep, link); err != nil {
		t.Fatalf("createSymlink: %v", err)
	}
	if err := os.Remove(keep); err != nil {
		t.Fatal(err)
	}

	err := verifySymlinked(keep, link)
	if err == nil {
		t.Fatal("❌ 悬空链接不应通过复核")
	}
	if !strings.Contains(err.Error(), "不可达") {
		t.Fatalf("❌ 错误信息应指出目标不可达，实得: %v", err)
	}
	t.Logf("✅ 正确检出悬空链接: %v", err)
}

// TestVerifySymlinkedMissingLink 链接路径本身不存在 → 报错。
func TestVerifySymlinkedMissingLink(t *testing.T) {
	dir := t.TempDir()
	keep, _ := writePair(t, dir, "x")
	if err := verifySymlinked(keep, filepath.Join(dir, "nope.bin")); err == nil {
		t.Fatal("❌ 链接不存在应报错")
	}
}

// TestVerifySymlinkedRejectsDirectory 目录不是文件链接 → 报错。
func TestVerifySymlinkedRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	keep, _ := writePair(t, dir, "x")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// 目录链接（SYMBOLIC_LINK_FLAG_DIRECTORY）在 Lstat 下同样是 ModeSymlink，
	// 但目标不是普通文件。本用例覆盖"路径位置上是目录"这一支。
	if err := verifySymlinked(keep, sub); err == nil {
		t.Fatal("❌ 目录不应通过软链接复核")
	}
}

// ---------------------------------------------------------------------------
// SymlinkMerge 单测
// ---------------------------------------------------------------------------

// TestSymlinkMergeCreatesWorkingLink 正常路径：链接成立 + 两条用户判据 + 无残留。
func TestSymlinkMergeCreatesWorkingLink(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "IDENTICAL-CONTENT-0123456789")

	if err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatalf("SymlinkMerge 失败: %v", err)
	}

	assertSymlinkCriterion(t, keep, dup)
	assertNoSymlinkResidue(t, dup)

	// 链接里保存的目标路径应指向 keep（用户排查时最需要看到的信息）
	got, err := symlinkTarget(dup)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if got != keep {
		t.Fatalf("链接目标应为 %q，实得 %q", keep, got)
	}
}

// TestSymlinkMergeVerifiesResolvedTarget ★ 核心用例 ★
//
// 目的：钉死"步骤 2 必须用 verifySymlinked（解析目标），不得用 identityStill"。
//
// 做法：不依赖具体实现，只断言**语义**——用一个"身份可解析"的真实
// keep/dup，正常合并必须成功。若实现里把 tmp（软链接）交给 identityStill，
// 取到的会是链接自身的身份，与 keepID 恒不等 → 合并必然失败，
// 本用例立刻红。
//
// 为什么这样设计而不是"注入一个错误实现来证明"：测试只能测被测代码。
// 这里保证的是"这条语义被固定住了"——任何把 identityStill 用到软链接上的
// 改动都会让本用例失败（因为链接身份 ≠ 目标身份，这也是
// TestIdentityStillOnSymlinkDiffersFromTarget 单独证明的前提）。
func TestSymlinkMergeVerifiesResolvedTarget(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "RESOLVED-TARGET-MATTERS")

	// 取"校验那一刻"的真实身份，模拟执行器的正常入参（非零值 ID）
	kid, err := fsid.FromPath(keep)
	if err != nil {
		t.Fatal(err)
	}
	did, err := fsid.FromPath(dup)
	if err != nil {
		t.Fatal(err)
	}

	if err := SymlinkMerge(keep, dup, kid, did); err != nil {
		t.Fatalf("❌ 在身份可解析的正常环境下，带真实 ID 的合并必须成功。"+
			"失败通常意味着步骤 2 误用了 identityStill(tmp, keepID)"+
			"（软链接自身的身份与 keep 不同，必然不等）: %v", err)
	}
	assertSymlinkCriterion(t, keep, dup)
	assertNoSymlinkResidue(t, dup)
}

// TestIdentityStillOnSymlinkDiffersFromTarget 用**证据**支撑上一条用例的前提：
// identityStill 作用在软链接上时，比对的是"链接自身"，因此与目标身份不等。
//
// 这是设计文档 §3.4 所指的那个坑的实证，也是上一条用例能作为回归守卫的原因。
func TestIdentityStillOnSymlinkDiffersFromTarget(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, _ := writePair(t, dir, "SAME")
	link := filepath.Join(dir, "l")
	if err := createSymlink(keep, link); err != nil {
		t.Fatal(err)
	}
	kid, err := fsid.FromPath(keep)
	if err != nil || !kid.Resolved {
		t.Skipf("本平台/卷不提供稳定文件身份（%v / %+v），本用例无意义", err, kid)
	}

	// identityStill 用 FromPathNoFollow → 链接自身的身份
	if identityStill(link, kid) {
		t.Fatal("❌ identityStill(链接, keepID) 不应为真——" +
			"若为真，说明它跟随了链接，会把'链接被换成另一个文件'的替换检测放过")
	}
	// 而 verifySymlinked 解析目标后必须为真
	if err := verifySymlinked(keep, link); err != nil {
		t.Fatalf("❌ verifySymlinked 应通过: %v", err)
	}
	t.Log("✅ 已实证：identityStill 取链接自身身份、verifySymlinked 取目标身份——" +
		"SymlinkMerge 的步骤 2 必须用后者")
}

// TestSymlinkMergeRejectsSamePath keep == dup → 报错，且不破坏文件。
func TestSymlinkMergeRejectsSamePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkMerge(p, p, fsid.ID{}, fsid.ID{}); err == nil {
		t.Fatal("❌ 同一路径应报错")
	}
	if _, err := os.ReadFile(p); err != nil {
		t.Fatalf("❌ 原文件不应受影响: %v", err)
	}
}

// TestSymlinkMerge_RenameFailureKeepsOriginal 步骤 3 失败（dup → backup 改名失败）
// 必须让 dup 原样保留，且不残留临时链接。
func TestSymlinkMerge_RenameFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "ORIGINAL-MUST-SURVIVE")

	origRename := hardlinkRename
	defer func() { hardlinkRename = origRename }()
	// 让"把 dup 移成备份"这一步失败（步骤 3）；后续步骤不该被走到。
	hardlinkRename = func(oldp, newp string) error {
		if oldp == dup {
			return os.ErrPermission
		}
		return os.Rename(oldp, newp)
	}

	err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	if err == nil {
		t.Fatal("❌ 改名失败必须返回错误")
	}
	assertDupIsPlainFileWithContent(t, dup, "ORIGINAL-MUST-SURVIVE")
	assertNoSymlinkResidue(t, dup) // .fdd-tmp 也必须被清掉
}

// TestSymlinkMerge_RollbackWhenVerifyFails 步骤 5 复核失败时必须回滚为普通文件。
//
// 注入方式：让**第二次** rename（tmp → dup）变成一次"复制文件"，
// 于是 dup 位置最终是一个独立普通文件而非链接 → verifySymlinked 必失败
// → 必须走回滚分支，把备份还原回 dup。
func TestSymlinkMerge_RollbackWhenVerifyFails(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "ROLLBACK-TARGET-CONTENT")

	origRename := hardlinkRename
	defer func() { hardlinkRename = origRename }()
	calls := 0
	hardlinkRename = func(oldp, newp string) error {
		calls++
		if calls == 2 {
			// 把 tmp（链接）"改名"成一次复制：结果 dup 是普通文件
			b, err := os.ReadFile(oldp)
			if err != nil {
				return err
			}
			if err := os.WriteFile(newp, b, 0o644); err != nil {
				return err
			}
			return os.Remove(oldp)
		}
		return os.Rename(oldp, newp)
	}

	err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	if err == nil {
		t.Fatal("❌ 链接未真正建立时必须返回错误，而不是报成功")
	}
	t.Logf("✅ 正确报错（非假成功）: %v", err)

	assertDupIsPlainFileWithContent(t, dup, "ROLLBACK-TARGET-CONTENT")
	if kb, _ := os.ReadFile(keep); string(kb) != "ROLLBACK-TARGET-CONTENT" {
		t.Fatalf("❌ keep 内容被牵连: %q", kb)
	}
	assertNoSymlinkResidue(t, dup)
}

// TestSymlinkMerge_ResidueReported 备份删除失败 → 返回 ResidueError，
// 但链接仍然有效（结果不该被判失败，否则用户会反复重试）。
//
// 对应 Windows 上杀软/索引器占用句柄导致 os.Remove 失败的高频场景。
func TestSymlinkMerge_ResidueReported(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "RESIDUE-CONTENT")

	origRemove := workTempRemove
	defer func() { workTempRemove = origRemove }()
	workTempRemove = func(string) error { return os.ErrPermission }

	err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	if err == nil {
		t.Fatal("❌ 备份未删除应返回错误（供上层提示残留）")
	}
	var residue *ResidueError
	if !asResidue(err, &residue) {
		t.Fatalf("❌ 应为 *ResidueError（区分'失败'与'成功但有残留'）: %v", err)
	}
	if !strings.Contains(residue.Path, ".fdd-old") {
		t.Fatalf("残留路径应指向备份文件，实得 %q", residue.Path)
	}
	// 链接本身必须仍然有效——这是"不把残留判成失败"的理由
	assertSymlinkCriterion(t, keep, dup)
	t.Logf("✅ 如实回报残留且链接有效: %v", err)
}

// TestSymlinkMerge_Idempotent 幂等：连着合并多次都必须成立，不破坏内容、不留残留。
//
// 第二次起 dup 已经是链接：步骤 1 建的 tmp 仍指向 keep，
// 步骤 2 复核 tmp 通过，步骤 3 把链接本身改名为备份，
// 步骤 4 让新链接顶替——语义上依然是"dup 是指向 keep 的链接"。
func TestSymlinkMerge_Idempotent(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "IDEMPOTENT")

	for i := 1; i <= 3; i++ {
		if err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
			t.Fatalf("第 %d 次合并失败: %v", i, err)
		}
		assertSymlinkCriterion(t, keep, dup)
		assertNoSymlinkResidue(t, dup)
	}
}

// TestSymlinkMerge_LinkCreationFailsNoChanges 步骤 1 建链接失败（如 Windows 未提权）
// 时，绝不能动 dup。
func TestSymlinkMerge_LinkCreationFailsNoChanges(t *testing.T) {
	dir := t.TempDir()
	keep, dup := writePair(t, dir, "UNTOUCHED")

	origSymlink := symlinkCreateFn
	defer func() { symlinkCreateFn = origSymlink }()
	symlinkCreateFn = func(string, string) error { return ErrSymlinkNeedsPrivilege }

	err := SymlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	if err == nil {
		t.Fatal("❌ 建链接失败必须返回错误")
	}
	if !isSymlinkNeedsPrivilege(err) {
		t.Fatalf("❌ 应可被判定为权限问题: %v", err)
	}
	assertDupIsPlainFileWithContent(t, dup, "UNTOUCHED")
	assertNoSymlinkResidue(t, dup)
}

// TestSymlinkIsDangling 悬空判定：链接失效 → true；链接有效/普通文件 → false。
func TestSymlinkIsDangling(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, plain := writePair(t, dir, "X")
	link := filepath.Join(dir, "live")
	if err := createSymlink(keep, link); err != nil {
		t.Fatal(err)
	}
	if symlinkIsDangling(link) {
		t.Fatal("有效链接不应被判为悬空")
	}
	if symlinkIsDangling(plain) {
		t.Fatal("普通文件不应被判为悬空链接")
	}
	if symlinkIsDangling(filepath.Join(dir, "nope")) {
		t.Fatal("不存在的路径不应被判为悬空链接")
	}
	if err := os.Remove(keep); err != nil {
		t.Fatal(err)
	}
	if !symlinkIsDangling(link) {
		t.Fatal("❌ 目标已删除，链接应被判为悬空")
	}
}

// asResidue 小工具：errors.As 的薄封装，避免在每个用例里重复写类型断言样板。
func asResidue(err error, target **ResidueError) bool {
	if err == nil {
		return false
	}
	if r, ok := err.(*ResidueError); ok {
		*target = r
		return true
	}
	return false
}

// -- SymlinkStatus（对外契约：历史记录页逐条标注用）--

// TestSymlinkStatusDistinguishesThreeStates 三态必须两两可区分，
// 因为前端按这两个布尔量决定渲染（普通/有效链接/悬空标红）。
func TestSymlinkStatusDistinguishesThreeStates(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.bin")
	if err := os.WriteFile(plain, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// ① 普通文件
	if is, dang := SymlinkStatus(plain); is || dang {
		t.Fatalf("普通文件应为 (false,false)，实得 (%v,%v)", is, dang)
	}

	// ② 有效链接
	requireSymlinkSupport(t, dir)
	good := filepath.Join(dir, "good.link")
	if err := symlinkCreate(plain, good); err != nil {
		t.Fatal(err)
	}
	if is, dang := SymlinkStatus(good); !is || dang {
		t.Fatalf("有效链接应为 (true,false)，实得 (%v,%v)", is, dang)
	}

	// ③ 悬空链接（目标被删）
	dead := filepath.Join(dir, "dead.link")
	if err := symlinkCreate(filepath.Join(dir, "gone.bin"), dead); err != nil {
		t.Fatal(err)
	}
	if is, dang := SymlinkStatus(dead); !is || !dang {
		t.Fatalf("悬空链接应为 (true,true)，实得 (%v,%v)", is, dang)
	}
}

// TestSymlinkStatusMissingPathIsNotAnError 路径不存在时返回"没有链接"。
//
// 为什么这很重要：GetOpRecord 会对每一条 done 记录调用本函数，而记录可能
// 已被回撤（undone，链接已删除）、或用户手动清掉了链接。若把"不存在"
// 当成异常，历史记录页会在最正常不过的场景下满屏报错。
func TestSymlinkStatusMissingPathIsNotAnError(t *testing.T) {
	is, dang := SymlinkStatus(filepath.Join(t.TempDir(), "nope"))
	if is || dang {
		t.Fatalf("不存在的路径应为 (false,false)（不是错误），实得 (%v,%v)", is, dang)
	}
}

// TestSymlinkStatusDoesNotFollowRegularFileChain 回归：普通文件即使内容
// 与别的文件相同也不得被误判为链接——判据是 Lstat 的模式位，不是内容。
func TestSymlinkStatusDoesNotFollowRegularFileChain(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	body := []byte("identical")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{a, b} {
		if is, dang := SymlinkStatus(p); is || dang {
			t.Fatalf("%s 是普通文件，不应被标为链接 (%v,%v)", p, is, dang)
		}
	}
}

// TestSymlinkMergeDetectsKeepReplacement 步骤 2/5 必须真正比对 keepID。
//
// 修正前 SymlinkMerge 收下的 keepID 从头到尾没用过：verifySymlinked(keep, tmp)
// 里 tmp 是**指向 keep 路径字符串**的链接，两侧沿同一条路径解析，恒相等——
// "保留源在校验后被替换（S1）"这道与 HardlinkMerge 同构的防线形同虚设。
// 后果：VerifyFile 之后 keep 路径被整体替换（新 inode、异内容）时合并照常
// 成功，步骤 5 删掉备份——而备份是两份相同内容在世上最后的原件副本。
func TestSymlinkMergeDetectsKeepReplacement(t *testing.T) {
	dir := t.TempDir()
	requireSymlinkSupport(t, dir)
	keep, dup := writePair(t, dir, "ORIGINAL-COPY-ONLY-LEFT-IN-DUP")

	kid, err := fsid.FromPath(keep)
	if err != nil {
		t.Fatal(err)
	}
	did, err := fsid.FromPath(dup)
	if err != nil {
		t.Fatal(err)
	}
	if !kid.Resolved || !did.Resolved {
		t.Skipf("本平台/卷不提供稳定文件身份，无法校验 keep 身份：kid=%+v did=%+v", kid, did)
	}

	// 模拟 TOCTOU 窗口：校验返回后、合并复核前，keep 路径被整体替换。
	if err := os.Remove(keep); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("ATTACKER-REPLACEMENT-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = SymlinkMerge(keep, dup, kid, did)
	if err == nil {
		t.Fatal("❌ keep 已被替换仍合并成功：dup 的备份（原始内容最后副本）会被当残留删掉")
	}
	if !strings.Contains(err.Error(), "保留源") {
		t.Fatalf("应以 keep 保留源被替换拦截，实得: %v", err)
	}
	assertDupIsPlainFileWithContent(t, dup, "ORIGINAL-COPY-ONLY-LEFT-IN-DUP")
	assertNoSymlinkResidue(t, dup)
}
