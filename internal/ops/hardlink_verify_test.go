package ops

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

// ============================================================================
// 硬链接合并回归（2026-09-19 缺陷）
//
// 用户报障：硬链接"显示已执行"，但
//   ① 文件夹总占用未变化
//   ② 修改 keep 或 dup 中任一个，另一个内容不跟着变
//
// 根因：HardlinkMerge 原先**只要没有 syscall 报错就返回 nil**，从不确认链接
// 真的建立了。Windows 上 os.Link → CreateHardLinkW 仅 NTFS 支持
// （MSDN："only supported on the NTFS file system"；ReFS 不支持，exFAT/FAT
// 完全不支持），跨卷也一律失败。这些情况下上层却把它记为 done 并累加
// "已释放空间"，于是出现"假成功"。
//
// 修复：收尾处强制复核 os.SameFile(keep, dup)，不成立则回滚并报错。
//
// 下面的用例全部以**用户可观测的判据**断言，而不是只看返回值：
//   - 判据 A：os.SameFile(keep, dup) 必须为真（同一物理文件）
//   - 判据 B：改写其中一个，另一个内容必须同步变化
//   - 判据 C：失败时不得留下"假成功"，且原文件内容必须完好
// ============================================================================

// assertUserCriterion 用用户的两条实操判据校验硬链接是否真的成立。
func assertUserCriterion(t *testing.T, keep, dup string) {
	t.Helper()

	// 判据 A：同一物理文件
	ki, err := os.Stat(keep)
	if err != nil {
		t.Fatalf("stat keep: %v", err)
	}
	di, err := os.Stat(dup)
	if err != nil {
		t.Fatalf("stat dup: %v", err)
	}
	if !os.SameFile(ki, di) {
		t.Fatalf("❌ 判据A失败：%s 与 %s 不是同一物理文件（硬链接未建立）", keep, dup)
	}

	// 判据 B：改写 keep → dup 必须同步（这是用户实际验证的方式）
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
		t.Fatalf("❌ 判据B失败：改写 keep 后 dup 内容未同步\n"+
			"  keep 写入 = %q\n  dup 读出  = %q", marker, got)
	}

	// 反向再验一次（改写 dup → keep 同步）
	rev := append([]byte("REVERSE-"), marker...)
	if err := os.WriteFile(dup, rev, 0o644); err != nil {
		t.Fatalf("write dup: %v", err)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != string(rev) {
		t.Fatalf("❌ 判据B(反向)失败：改写 dup 后 keep 未同步: %q err=%v", got, err)
	}
}

// assertNoResidue 确认失败/成功后都不留 .fdd-tmp / .fdd-old / .fdd-old.undo。
func assertNoResidue(t *testing.T, dup string) {
	t.Helper()
	for _, suf := range []string{".fdd-tmp", ".fdd-old", ".fdd-old.undo"} {
		if _, err := os.Stat(dup + suf); err == nil {
			t.Errorf("❌ 残留临时文件: %s%s", dup, suf)
		}
	}
}

// TestHardlinkMerge_SatisfiesUserCriteria 正常路径必须同时满足两条用户判据。
func TestHardlinkMerge_SatisfiesUserCriteria(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	content := []byte("IDENTICAL-CONTENT-0123456789")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatalf("HardlinkMerge 失败: %v", err)
	}

	assertUserCriterion(t, keep, dup)
	assertNoResidue(t, dup)

	// 两路径都必须可读且内容一致
	for _, p := range []string{keep, dup} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", p, err)
		}
		if len(b) == 0 {
			t.Fatalf("%s 内容为空", p)
		}
	}
}

// TestVerifyHardlinked_RejectsIndependentFiles 直接单测复核函数：
// 两个内容相同但彼此独立的文件必须被判为"未建立链接"。
//
// 这是修复的核心——修正前没有任何一处会做这个判断，于是"内容相同"
// 被误当成"链接已建立"，而二者是两回事（这正是用户遇到的假成功）。
func TestVerifyHardlinked_RejectsIndependentFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	content := []byte("SAME-BYTES-BUT-SEPARATE-FILES")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 内容完全相同，但各自独立 → 必须报错
	if err := verifyHardlinked(a, b); err == nil {
		t.Fatal("❌ 两个内容相同的独立文件不应被判为已硬链接")
	} else {
		t.Logf("✅ 正确拒绝独立文件: %v", err)
	}

	// 真正建立链接后应通过
	if err := os.Link(a, filepath.Join(dir, "c.bin")); err != nil {
		t.Skipf("本平台/文件系统不支持硬链接，跳过（%v）", err)
	}
	if err := verifyHardlinked(a, filepath.Join(dir, "c.bin")); err != nil {
		t.Fatalf("❌ 真硬链接应通过复核: %v", err)
	}
}

// TestVerifyHardlinked_DetectsContentDrift 尺寸不一致必须被发现。
func TestVerifyHardlinked_DetectsContentDrift(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	os.WriteFile(a, []byte("12345"), 0o644)

	// 不存在的路径
	if err := verifyHardlinked(a, filepath.Join(dir, "nope.bin")); err == nil {
		t.Fatal("❌ 目标不存在应报错")
	}
	// keep 不存在
	if err := verifyHardlinked(filepath.Join(dir, "nope.bin"), a); err == nil {
		t.Fatal("❌ 源不存在应报错")
	}
	// 目录不是普通文件
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	if err := verifyHardlinked(a, sub); err == nil {
		t.Fatal("❌ 目录不应通过普通文件校验")
	}
}

// TestHardlinkMerge_FailureKeepsOriginalIntact 若最终复核失败，
// 必须回滚为原独立文件：内容完好、无残留、且返回错误（不假成功）。
//
// 用 hardlinkRename 钩子模拟"重命名看似成功但链接其实没建立"的 Windows 情形：
// 让第二次 rename 变成一次**复制**（生成独立文件而非改名链接）。
func TestHardlinkMerge_FailureKeepsOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	original := []byte("ORIGINAL-DUP-CONTENT-MUST-SURVIVE")
	if err := os.WriteFile(keep, []byte("KEEP-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, original, 0o644); err != nil {
		t.Fatal(err)
	}

	origRename := hardlinkRename
	defer func() { hardlinkRename = origRename }()

	calls := 0
	hardlinkRename = func(oldp, newp string) error {
		calls++
		// 第二次 rename（tmp → dup）时不做真改名，改为复制：
		// 结果 dup 会是一份**独立**文件 → 复核必须失败。
		if calls == 2 {
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

	err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	if err == nil {
		t.Fatal("❌ 链接未真正建立时必须返回错误，而不是报成功")
	}
	t.Logf("✅ 正确报错（非假成功）: %v", err)

	// dup 必须仍是原来的内容（要么是原文件、要么已被回滚）
	b, rerr := os.ReadFile(dup)
	if rerr != nil {
		t.Fatalf("❌ dup 应存在且内容完好: %v", rerr)
	}
	if string(b) != string(original) {
		t.Fatalf("❌ dup 内容被破坏:\n  want %q\n  got  %q", original, b)
	}
	// keep 不能被牵连
	if kb, _ := os.ReadFile(keep); string(kb) != "KEEP-CONTENT" {
		t.Fatalf("❌ keep 内容被破坏: %q", kb)
	}
	assertNoResidue(t, dup)
}

// TestHardlinkMerge_Idempotent 重复合并必须幂等：已合并的再合一次仍成立，
// 且不破坏内容、不留残留。
func TestHardlinkMerge_Idempotent(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	content := []byte("IDEMPOTENT-CONTENT")
	for _, p := range []string{keep, dup} {
		os.WriteFile(p, content, 0o644)
	}

	for i := 1; i <= 3; i++ {
		if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
			t.Fatalf("第 %d 次合并失败: %v", i, err)
		}
		assertUserCriterion(t, keep, dup)
		assertNoResidue(t, dup)
	}
}

// TestHardlinkMerge_FirstCriteriaRegression_LinuxPasses 明确记录：
// 本缺陷在 Linux（支持硬链接）上**无法复现**，因此修复的价值主要在
// 平台无关的**终局复核**——它把"没报错"升级为"确实同文件"。
func TestHardlinkMerge_FirstCriteriaRegression_LinuxPasses(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "k")
	dup := filepath.Join(dir, "d")
	os.WriteFile(keep, []byte("x"), 0o644)
	os.WriteFile(dup, []byte("x"), 0o644)

	if err := os.Link(keep, filepath.Join(dir, "probe")); err != nil {
		t.Skipf("本文件系统不支持硬链接（%v）：修复主要作用于 Windows，跳过", err)
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatalf("HardlinkMerge 失败: %v", err)
	}
	assertUserCriterion(t, keep, dup)
}
