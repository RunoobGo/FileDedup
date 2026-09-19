package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// ─────────────────────────────────────────────────────────────────────────────
// 缺陷 6（2026-09-19）回归测试
//
// 用户报告：「硬链接后的文件，其中一个修改时，另一个未发生变化，实际硬链接
// 未生效。重复扫描时，也不会（把）已操作硬链接（实际未生效）的文件识别（为）
// 重复文件。」（末句按上下文应为：仍被识别为重复文件）
//
// 现场事实（用户确认）：提示条为绿色「成功 N（已合并为硬链接 X，占用不变）」，
// 且未翻页/滚动，选择集完整。
//
// 据此，后端「声称成功」属可信（见 TestHardlinkSuccessImpliesRealLink），
// 真正的可复现缺陷在于**残留工作临时文件污染后续扫描**：
//   - 合并成功后 `_ = os.Remove(backup)` 静默吞掉删除失败；
//   - 残留的 `dup.fdd-old` 是合并前的完整独立副本，内容与保留源逐字节相同；
//   - 该名字以用户文件名开头、不以 "." 开头，扫描器的隐藏跳过规则拦不住；
//   - 于是重扫必然把它与被保留的文件配成一个"重复组"。
//
// 本文件为上述每一条给出可证伪的断言。
// ─────────────────────────────────────────────────────────────────────────────

// TestHardlinkSuccessImpliesRealLink 钉住核心不变式：
// 「HardlinkMerge 返回 nil」必须蕴含「dup 与 keep 真的是同一份物理文件」。
//
// 这是用户现场判断的分水岭。若此不变式成立，则绿色「成功」不能被解释为
// "后端谎报"——问题必然在别处（残留文件、扫描口径）。若被破坏，
// 说明复核逻辑失效，必须优先修这里。
func TestHardlinkSuccessImpliesRealLink(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, []byte("same-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatalf("合并应成功: %v", err)
	}
	ki, err := os.Stat(keep)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dup)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(ki, di) {
		t.Fatal("返回 nil 却未建立真实硬链接——复核逻辑失效")
	}
	// 写入穿透：改 dup，keep 必须跟随（用户用来判定的观察方式）
	if err := os.WriteFile(dup, []byte("CHANGED"), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, _ := os.ReadFile(keep)
	if string(kb) != "CHANGED" {
		t.Fatalf("硬链接未生效：keep 未跟随 dup 变化，读到 %q", kb)
	}
}

// TestIsWorkTempNameRecognizesAllAppArtifacts 遍历应用实际产生的每一种
// 工作临时名，确保它们全部被识别（扫描器据此忽略）。
//
// 若日后新增一种临时名却忘了登记，本测试会失败——这正是本次缺陷的成因：
// 「产生临时名」与「忽略临时名」两处各自为政，没有共同定义。
func TestIsWorkTempNameRecognizesAllAppArtifacts(t *testing.T) {
	cases := []string{
		"a.bin.fdd-tmp",
		"a.bin.fdd-old",
		"a.bin.fdd-old.undo",
		"a.bin.fdd-undo-tmp",
		"photo.fdd-restored.jpg",
		"中文名称.docx.fdd-old",
		"no_ext.fdd-old",
	}
	for _, n := range cases {
		if !IsWorkTempName(n) {
			t.Errorf("%q 应被识别为应用工作临时名（否则会污染扫描结果）", n)
		}
	}
	// 反向：正常用户文件名绝不能被误伤
	negatives := []string{
		"a.bin", "报告.docx", "fdd-cli.exe", "fdd备份.bin",
		"a.FDD-old", // 大写不算：我们自己只生成小写 ".fdd-"
		"fdd.bin",
	}
	for _, n := range negatives {
		if IsWorkTempName(n) {
			t.Errorf("%q 是正常文件名，不应被忽略（会漏扫用户文件）", n)
		}
	}
}

// TestHardlinkMergeDeleteFailureLeavesNoSilentResidue 模拟 Windows 上
// 「原副本删除失败」（杀软/索引器占句柄 → ERROR_SHARING_VIOLATION）。
//
// 修正前的行为：`_ = os.Remove(backup)` 静默吞掉错误，函数仍返回 nil。
// 后果：链接建好了，但一份与保留源逐字节相同的 .fdd-old 永久留在用户
// 目录里；重扫时它与保留源构成"重复组"——用户看到「做过的文件又变重复」。
//
// 修正后：返回 *ResidueError，让上层能区分「失败」与「成功但有残留」，
// 并据此提示用户。绝不静默。
func TestHardlinkMergeDeleteFailureLeavesNoSilentResidue(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, []byte("residue-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 模拟 Windows 上「原副本被占用，删除失败」：把清理钩子换成恒失败。
	// 采用注入而非造不可删文件，是为了**精确**只让"清理"这一步失败，
	// 不影响前面的 rename/链接步骤，从而隔离出缺陷点。
	orig := workTempRemove
	defer func() { workTempRemove = orig }()
	workTempRemove = func(string) error {
		return errors.New("simulated sharing violation (ERROR_SHARING_VIOLATION)")
	}

	err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{})
	if err == nil {
		t.Fatal("删除 backup 失败时必须返回错误（不得静默成功）")
	}
	var residue *ResidueError
	if !errors.As(err, &residue) {
		t.Fatalf("应返回 ResidueError（成功但有残留），实际: %T %v", err, err)
	}
	if !strings.Contains(residue.Path, FddOldSuffix) {
		t.Fatalf("ResidueError 应指明残留路径，实际 %q", residue.Path)
	}

	// 关键：链接本身仍必须已建立（残留不该否定合并结果）
	ki, _ := os.Stat(keep)
	di, _ := os.Stat(dup)
	if !os.SameFile(ki, di) {
		t.Fatal("删除残留失败不应影响已建立的硬链接")
	}
	// 残留文件确实存在（这正是需要告知用户的东西）
	if _, serr := os.Stat(dup + FddOldSuffix); serr != nil {
		t.Fatalf("残留副本应确实存在，以便上层提示用户手动清理: %v", serr)
	}
	t.Logf("如实回报残留：%v", residue)
}

// TestHardlinkMergeCleanPathLeavesNoResidue 正常路径必须零残留——
// 防止为修残留而引入新的残留（如临时硬链接未清理）。
func TestHardlinkMergeCleanPathLeavesNoResidue(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, []byte("clean-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatalf("合并应成功: %v", err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if IsWorkTempName(e.Name()) {
			t.Errorf("正常合并后不应残留工作临时文件：%s", e.Name())
		}
	}
	if len(ents) != 2 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("目录应恰好剩 2 个文件，实际 %d 个: %v", len(ents), names)
	}
}

// TestExecutorResidueIsSuccessNotFailure 执行器层：ResidueError 必须
// 计为成功（不进 Failed），同时把警告独立上报。
//
// 理由：链接确实建立了。若计为失败，用户会以为合并没生效并反复重试，
// 而重试时 dup 与 keep 已是同一文件，行为更难解释。
func TestExecutorResidueIsSuccessNotFailure(t *testing.T) {
	dir := t.TempDir()
	content := []byte("executor-residue-case")
	ka := filepath.Join(dir, "keep.bin")
	db := filepath.Join(dir, "dup.bin")
	for _, p := range []string{ka, db} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 模拟清理失败（残留）：见上一个测试的说明。
	orig := workTempRemove
	defer func() { workTempRemove = orig }()
	workTempRemove = func(string) error { return errors.New("simulated sharing violation") }

	ek := &model.FileEntry{ID: 1, Path: ka, Size: uint64(len(content)), Ext: ".bin"}
	ed := &model.FileEntry{ID: 2, Path: db, Size: uint64(len(content)), Ext: ".bin"}
	h := hashOfContent(t, content)
	group := &model.DuplicateGroup{GroupID: 1, Hash: h, Files: []*model.FileEntry{ek, ed}}

	res := Execute(
		Options{Groups: []*model.DuplicateGroup{group}, KeepIDs: map[uint64]bool{1: true},
			Pool: hasher.NewPool()},
		model.OpRequest{Kind: "hardlink", FileIDs: []uint64{2}},
	)

	if len(res.Failed) != 0 {
		t.Fatalf("有残留时应计入成功而非失败，实际 Failed=%+v", res.Failed)
	}
	if len(res.OK) != 1 {
		t.Fatalf("应成功 1 项，实际 OK=%v", res.OK)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("应上报 1 条警告（残留），实际 %d 条: %v", len(res.Warnings), res.Warnings)
	}
	if !strings.Contains(res.Warnings[0], FddOldSuffix) {
		t.Fatalf("警告应指明残留文件，实际 %q", res.Warnings[0])
	}
	if res.Reclaimed != 0 {
		t.Fatalf("硬链接不应计入已释放空间，Reclaimed=%d", res.Reclaimed)
	}
	if res.LinkedBytes != uint64(len(content)) {
		t.Fatalf("应记录共享字节数，LinkedBytes=%d", res.LinkedBytes)
	}
	t.Logf("成功但有残留（如实上报）：%s", res.Warnings[0])
}
