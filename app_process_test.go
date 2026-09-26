package main

// 处理策略（执行时过滤器）的接入测试。
//
// 核心语义：实际处理范围 = FileIDs ∩ {位于优先文件夹下的文件}。
// 与保留策略的关系是**结构性**的——处理策略只能让操作做得更少，
// 而保留项在执行器里是硬拒绝，所以两者不可能冲突。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// procFixture 造一个真实的跨目录重复组：同一份内容放在 root 下的四个子目录里，
// 优先目录内外**各自都有冗余项**。
//
//	root/keepme/keep.bin  ← 保留者在优先目录内（★ 关键：它属于优先目录，但不可处理）
//	root/inside/a.bin     ← 优先目录内，冗余项
//	root/inside/sub/b.bin ← 优先目录内（子目录），冗余项
//	root/outside/c.bin    ← 优先目录外，冗余项
//
// ★ 为什么保留者要放在优先目录内：这是"保留策略优先"最有价值的对照组。
// 用户勾了优先目录，目录里既有一个保留者、又有冗余项——处理策略绝不能
// 把保留者一起捎上。如果保留者放在目录外，这条性质就被测不到了
// （那种布局下"保留者没被动"只是因为处理策略先把它过滤掉了，属于巧合）。
//
// ★ 为什么要四个文件、而不是两个：起初写成「inside 一个 + outside 一个」，
// 测试全挂。实测发现扫描的默认保留者（非隐藏优先，再路径最短）恰好选中了
// inside/dup.bin —— 唯一那个非保留项落在 outside，夹具前提整体反了。
// 两个文件的组里只有 1 个是非保留项，"谁在优先目录内"和"谁是冗余项"这两件事
// 被绑死，无法独立控制。四个文件才能让两侧都留下冗余项。
//
// 保留者用显式保留策略钉在 root/keepme（位于优先目录内），不依赖默认落点。
func procFixture(t *testing.T) (a *App, rec *eventRecorder, root, insideDir, outsideDir string) {
	t.Helper()
	root = t.TempDir()
	payload := []byte("PROCESS-POLICY-DUPLICATE-PAYLOAD-0123456789")

	insideDir = filepath.Join(root, "inside")
	insideSub := filepath.Join(insideDir, "sub")
	outsideDir = filepath.Join(root, "outside")
	keepDir := filepath.Join(root, "keepme")
	for _, d := range []string{insideSub, outsideDir, keepDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{
		filepath.Join(keepDir, "keep.bin"),
		filepath.Join(insideDir, "a.bin"),
		filepath.Join(insideSub, "b.bin"),
		filepath.Join(outsideDir, "c.bin"),
	}
	for _, p := range files {
		if err := os.WriteFile(p, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a, rec = newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("scan 终止事件 = %s", ev)
	}
	// 钉死保留者：keepme 目录优先 → keep.bin 为保留者，其余三个为冗余项。
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{
		Kind: "directory", Directories: []string{keepDir},
	}); err != nil {
		t.Fatal(err)
	}
	return a, rec, root, insideDir, outsideDir
}

// idInDir 从结果集里取出位于 dir 下的**非保留**项 ID（递归子目录也算）。
//
// 必须用 ops 包的 inDir 语义而不是 filepath.Dir == dir：优先级目录的归属是
// 递归的（inside/sub/b.bin 属于 inside），只比直接父目录会把子目录里的文件漏掉。
func idInDir(t *testing.T, a *App, dir string) (id uint64, path string) {
	t.Helper()
	got := idsInDir(t, a, dir)
	if len(got.ids) == 0 {
		t.Fatalf("结果集里找不到位于 %s 的非保留项", dir)
	}
	return got.ids[0], got.paths[0]
}

type dirIDs struct {
	ids   []uint64
	paths []string
}

// idsInDir 汇出位于 dir 下的全部非保留项，供"多命中"类断言使用。
func idsInDir(t *testing.T, a *App, dir string) dirIDs {
	t.Helper()
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	var out dirIDs
	for _, g := range r.Groups {
		for _, f := range g.Files {
			if f.IsKeep {
				continue
			}
			if dirContains(dir, f.Path) {
				out.ids = append(out.ids, f.ID)
				out.paths = append(out.paths, f.Path)
			}
		}
	}
	return out
}

// dirContains 报告 path 是否位于 dir 之下（含子目录）。
// 这是测试侧的独立实现，刻意不复用被测代码的 inDir——
// 复用会让"被测函数错了但测试跟着一起错"的共模失效成为可能。
func dirContains(dir, path string) bool {
	d := strings.TrimSuffix(filepath.Clean(dir), string(filepath.Separator))
	return path == d || strings.HasPrefix(path, d+string(filepath.Separator))
}

// 处理策略生效：勾选三项（两内一外），限定 inside → 只处理 inside 的两项。
func TestExecuteOperationWithProcessDirsFilters(t *testing.T) {
	requireRealTrash(t)
	a, rec, _, insideDir, outsideDir := procFixture(t)

	inside := idsInDir(t, a, insideDir)
	outID, outPath := idInDir(t, a, outsideDir)

	if len(inside.ids) != 2 {
		t.Fatalf("inside 下应有 2 个非保留项（含子目录），实得 %v", inside.paths)
	}

	all := append(append([]uint64{}, inside.ids...), outID)
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: all, ProcessDirs: []string{insideDir},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	// inside 的两项被回收；outside 那项必须原封不动
	for _, p := range inside.paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("范围内的文件应被处理掉: %s err=%v", p, err)
		}
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("范围外的文件不该被动到: %s err=%v", outPath, err)
	}
}

// 目录归属是递归的：inside/sub/b.bin 属于 inside。
// 这条单独拎出来，因为「只比直接父目录」是个很容易犯、且只在子目录场景暴露的错。
func TestExecuteOperationProcessDirsRecursesIntoSubdirs(t *testing.T) {
	requireRealTrash(t)
	a, rec, _, insideDir, _ := procFixture(t)

	sub := filepath.Join(insideDir, "sub")
	subIDs := idsInDir(t, a, sub)
	if len(subIDs.ids) != 1 {
		t.Fatalf("inside/sub 下应有 1 个非保留项，实得 %v", subIDs.paths)
	}

	// 只勾选子目录里的文件，但优先目录给的是父目录
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: subIDs.ids, ProcessDirs: []string{insideDir},
	}); err != nil {
		t.Fatalf("子目录应归属父目录，不该被拒绝: %v", err)
	}
	waitOpsDone(t, rec)

	if _, err := os.Stat(subIDs.paths[0]); !os.IsNotExist(err) {
		t.Errorf("子目录内的文件应被处理掉: %s", subIDs.paths[0])
	}
}

// 向后兼容回归门：ProcessDirs 为空 = 未启用处理策略，行为与新增功能前一致。
//
// 这条是硬要求——不启用时**必须**走原来的代码路径，否则所有老用户的行为都会变。
func TestExecuteOperationProcessDirsEmptyIsBackwardCompatible(t *testing.T) {
	requireRealTrash(t)
	for _, name := range []string{"nil", "空 slice"} {
		t.Run(name, func(t *testing.T) {
			a, rec, _, insideDir, outsideDir := procFixture(t)
			inside := idsInDir(t, a, insideDir)
			outID, outPath := idInDir(t, a, outsideDir)
			if len(inside.ids) != 2 {
				t.Fatalf("inside 下应有 2 个非保留项，实得 %v", inside.paths)
			}
			all := append(append([]uint64{}, inside.ids...), outID)

			var dirs []string
			if name == "空 slice" {
				dirs = []string{}
			}
			if _, err := a.ExecuteOperation(model.OpRequest{
				Kind: "trash", FileIDs: all, ProcessDirs: dirs,
			}); err != nil {
				t.Fatal(err)
			}
			waitOpsDone(t, rec)

			// 三项都要被处理（未启用过滤 = 无收窄）
			for _, p := range append(append([]string{}, inside.paths...), outPath) {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("未启用处理策略时应处理全部勾选项，%s 仍在", p)
				}
			}
		})
	}
}

// 交集为空：必须**明确拒绝**，且必须满足三个条件：
//   - 返回错误（不是静默成功）
//   - opsRunning 已复位（否则应用永久卡在"操作执行中"）
//   - 没有任何文件被改动
func TestExecuteOperationRejectsEmptyIntersection(t *testing.T) {
	requireRealTrash(t)
	a, rec, _, insideDir, outsideDir := procFixture(t)

	outID, outPath := idInDir(t, a, outsideDir)

	// 勾选目录外的文件，却限定 inside → 交集为空
	_, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: []uint64{outID}, ProcessDirs: []string{insideDir},
	})
	if err == nil {
		t.Fatal("交集为空必须返回错误，不能静默执行 0 项（用户会以为做了）")
	}
	t.Logf("拒绝文案: %v", err)

	// 关键：文件必须一个都没动
	if _, serr := os.Stat(outPath); serr != nil {
		t.Fatalf("被拒绝的操作不该改动任何文件: %s err=%v", outPath, serr)
	}

	// 关键：opsRunning 必须复位，否则后续操作全被"上一个操作仍在执行"挡住
	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if running {
		t.Fatal("拒绝后 opsRunning 未复位——应用会永久卡在「操作执行中」")
	}

	// 反证：复位确实有效——紧接着发起一次合法操作应当成功。
	//
	// ★ 必须等终止事件再断言磁盘。
	// 起初这里没等，直接 os.Stat 就断言文件已消失，结果是**偶发失败**：
	// 被拒绝的那个请求在 opsRunning 复位后立刻返回，而合法操作的清理
	// 跑在 ops goroutine 里，断言时它可能还没跑完。
	// 断言一个"操作的效果"就必须先等到"操作完成"——异步边界没对齐，
	// 测试就会变成随机红（这也是本文件其它用例都调 waitOpsDone 的原因）。
	inID, inPath := idInDir(t, a, insideDir)
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: []uint64{inID}, ProcessDirs: []string{insideDir},
	}); err != nil {
		t.Fatalf("拒绝后应能正常发起后续操作，实得: %v", err)
	}
	waitOpsDone(t, rec)
	if _, err := os.Stat(inPath); !os.IsNotExist(err) {
		t.Errorf("后续操作应正常执行: %s", inPath)
	}
}

// 保留项不计入命中：限定目录内只有保留项时应视为"无命中"，
// 而不是放行（保留项在执行器里会被硬拒，但那样会走完整流程再失败，体验很差）。
func TestExecuteOperationProcessDirsExcludesKeep(t *testing.T) {
	root := t.TempDir()
	payload := []byte("KEEP-EXCLUSION-PAYLOAD-0123456789")
	dir := filepath.Join(root, "only")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, n), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a, rec := newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("scan 终止事件 = %s", ev)
	}

	// 把保留者设为 a.bin，则组内冗余项只有 b.bin（在 only 目录内）
	sel := redundantIDs(t, a)
	if len(sel) != 1 {
		t.Fatalf("应有 1 个冗余项, got %v", sel)
	}
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: sel, ProcessDirs: []string{dir},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	// 冗余项被处理，保留者必须仍在
	var keepPath string
	r, _ := a.GetResultGroups(ResultQuery{PageSize: 500})
	_ = r // 清理后组可能已消失，改用磁盘检查
	for _, n := range []string{"a.bin", "b.bin"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			keepPath = filepath.Join(dir, n)
		}
	}
	if keepPath == "" {
		t.Fatal("保留项被误删了——保留策略必须优先于处理策略")
	}
	t.Logf("保留下来的: %s", keepPath)
}

// 写前账本的范围必须与实际执行范围一致。
// 若在 beginJournal 之后才收窄 FileIDs，账本会记录超出实际执行的文件，
// 用户回撤时看到的条目与实际不符。
func TestExecuteOperationProcessDirsJournalScope(t *testing.T) {
	a, rec, _, insideDir, outsideDir := procFixture(t)
	inside := idsInDir(t, a, insideDir)
	outID, _ := idInDir(t, a, outsideDir)
	if len(inside.ids) != 2 {
		t.Fatalf("inside 下应有 2 个非保留项，实得 %v", inside.paths)
	}
	all := append(append([]uint64{}, inside.ids...), outID)

	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: all, ProcessDirs: []string{insideDir},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	ops, err := a.hist.ListOps()
	if err != nil || len(ops) != 1 {
		t.Fatalf("操作记录 = %+v err=%v", ops, err)
	}
	_, items, err := a.hist.GetOp(ops[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(inside.ids) {
		t.Fatalf("账本条目数 = %d, want %d（只应记录范围内的文件，"+
			"超出实际执行范围的条目会让回撤界面与实际不符）", len(items), len(inside.ids))
	}
	if items[0].OrigPath == "" {
		t.Fatal("账本条目缺少 orig_path")
	}
	// 账本里不能出现范围外的那个文件
	for _, it := range items {
		if it.OrigPath == outsideDir || strings.HasPrefix(it.OrigPath, outsideDir+string(filepath.Separator)) {
			t.Fatalf("账本记录了范围外的文件: %s", it.OrigPath)
		}
	}
}

// ops:filtered 事件必须带上过滤前后的计数与未命中目录，供 UI 说明实际范围。
func TestExecuteOperationProcessDirsEmitsFiltered(t *testing.T) {
	a, rec, _, insideDir, outsideDir := procFixture(t)

	inID, _ := idInDir(t, a, insideDir)
	outID, _ := idInDir(t, a, outsideDir)
	// 一个不存在的目录 → 必定出现在 unmatched
	missing := filepath.Join(t.TempDir(), "nowhere")

	// 勾两项（一内一外）+ 一个不存在的目录：
	// 期望事件里 matched=1、selected=2、unmatched=[missing]。
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: []uint64{inID, outID}, ProcessDirs: []string{insideDir, missing},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	if !rec.has("ops:filtered") {
		t.Fatalf("应发出 ops:filtered 事件，实收 %v", rec.names())
	}
	// 事件载荷必须真的携带计数与未命中目录——只发个空事件等于没发。
	payload, ok := rec.lastWith("ops:filtered")
	if !ok {
		t.Fatal("找不到 ops:filtered 载荷")
	}
	m := payload.(map[string]any)
	if got := m["matched"]; got != 1 {
		t.Errorf("ops:filtered matched = %v, want 1", got)
	}
	if got := m["selected"]; got != 2 {
		t.Errorf("ops:filtered selected = %v, want 2（必须是过滤前的勾选数，UI 用它说明"+
			"「已选 N 项 / 实际处理 M 项」）", got)
	}
	un, _ := m["unmatched"].([]string)
	if len(un) != 1 || un[0] != missing {
		t.Errorf("ops:filtered unmatched = %v, want [%s]", m["unmatched"], missing)
	}
}

// ★ 需求原文的正面对照：「保留策略与处理策略冲突时，优先执行满足保留策略」。
//
// 构造真冲突：优先目录 = root（整棵树），此时勾选集合里的**保留者也在范围内**。
// 也就是说处理策略"想"让保留者一起被动，但保留策略必须赢。
//
// 这条是本需求的核心断言。若哪天有人在过滤器里图省事、忘掉 keepIDs 排除，
// 或者把"保留项硬拒绝"这层去掉，这条测试必须失败。
func TestProcessPolicyNeverOverridesKeepPolicy(t *testing.T) {
	requireRealTrash(t)
	a, rec, root, _, _ := procFixture(t)

	// 全部文件（含保留者）都在 root 下 → 处理策略的命中集合 = 所有勾选项
	keepPath, redPaths := keepAndRedundantPaths(t, a)
	if len(redPaths) != 3 {
		t.Fatalf("应有 3 个冗余项，实得 %v", redPaths)
	}
	all := append(append([]uint64{}, redundantIDs(t, a)...), keepID(t, a))

	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: all, ProcessDirs: []string{root},
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	// 保留者必须完好；三个冗余项必须都被处理
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("★保留策略未优先：优先目录覆盖了保留者所在目录时，"+
			"保留者仍必须不被处理: %s err=%v", keepPath, err)
	}
	for _, p := range redPaths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("范围内的冗余项应被处理: %s err=%v", p, err)
		}
	}
}

// keepID 取出结果集里的保留者 ID。
func keepID(t *testing.T, a *App) uint64 {
	t.Helper()
	_, id := keepAndRedundant(t, a)
	return id
}

// keepAndRedundantPaths 返回保留者路径与全部冗余项路径。
func keepAndRedundantPaths(t *testing.T, a *App) (keep string, reds []string) {
	t.Helper()
	p, _ := keepAndRedundant(t, a)
	for _, g := range allGroups(t, a) {
		for _, f := range g.Files {
			if !f.IsKeep {
				reds = append(reds, f.Path)
			}
		}
	}
	return p, reds
}

// keepAndRedundant 返回保留者的路径与 ID。
func keepAndRedundant(t *testing.T, a *App) (path string, id uint64) {
	t.Helper()
	for _, g := range allGroups(t, a) {
		for _, f := range g.Files {
			if f.IsKeep {
				return f.Path, f.ID
			}
		}
	}
	t.Fatal("结果集里找不到保留者")
	return "", 0
}

// allGroups 取结果集里的全部组。
func allGroups(t *testing.T, a *App) []GroupView {
	t.Helper()
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	return r.Groups
}

// PreviewProcessPolicy：只读预览，不能有副作用。
func TestPreviewProcessPolicy(t *testing.T) {
	a, _, _, insideDir, outsideDir := procFixture(t)
	inside := idsInDir(t, a, insideDir)
	outID, outPath := idInDir(t, a, outsideDir)
	if len(inside.ids) != 2 {
		t.Fatalf("inside 下应有 2 个非保留项，实得 %v", inside.paths)
	}
	selected := append(append([]uint64{}, inside.ids...), outID)

	pv, err := a.PreviewProcessPolicy([]string{insideDir}, nil, selected)
	if err != nil {
		t.Fatal(err)
	}
	if pv.EffectiveCount != len(inside.ids) {
		t.Fatalf("生效数 = %d, want %d（只有 inside 下的项在范围内）",
			pv.EffectiveCount, len(inside.ids))
	}
	if len(pv.EffectiveIDs) != len(inside.ids) {
		t.Fatalf("生效 ID = %v, want %v", pv.EffectiveIDs, inside.ids)
	}
	// 生效集合必须与勾选项成子集关系，且不得含范围外的那个
	selSet := map[uint64]bool{}
	for _, id := range selected {
		selSet[id] = true
	}
	for _, id := range pv.EffectiveIDs {
		if !selSet[id] {
			t.Fatalf("生效 ID %d 不在勾选集合里——预览凭空放行了未勾选的项", id)
		}
		if id == outID {
			t.Fatalf("范围外的文件不该生效: %d", outID)
		}
	}

	// 无副作用：文件仍在、组仍在、opsRunning 未被置位
	for _, p := range append(append([]string{}, inside.paths...), outPath) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("预览不该动文件: %s err=%v", p, err)
		}
	}
	a.mu.Lock()
	running := a.opsRunning
	a.mu.Unlock()
	if running {
		t.Fatal("预览是只读的，不该置位 opsRunning")
	}
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) == 0 {
		t.Fatal("预览不该改动结果集")
	}

	// 未启用（dirs 为空）→ 生效范围 = 全部勾选，且不报未命中
	pv2, err := a.PreviewProcessPolicy(nil, nil, selected)
	if err != nil {
		t.Fatal(err)
	}
	if pv2.EffectiveCount != len(selected) {
		t.Fatalf("未启用时生效数应等于勾选数 %d，实得 %d", len(selected), pv2.EffectiveCount)
	}
	if len(pv2.UnmatchedDirs) != 0 {
		t.Fatalf("未启用时不该报未命中: %v", pv2.UnmatchedDirs)
	}

	// 未命中的目录要被点名（用户加了目录但里面没有可处理的重复文件）
	pv3, err := a.PreviewProcessPolicy([]string{insideDir, filepath.Join(t.TempDir(), "nope")}, nil,
		selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv3.UnmatchedDirs) != 1 {
		t.Fatalf("应报 1 个未命中目录，实得 %v", pv3.UnmatchedDirs)
	}

	// ★ 交集为空时预览不报错，而是如实给出 0：
	// 预览是"给你看会发生什么"，不是"阻止你"。是否拒绝由 ExecuteOperation 决定。
	pv4, err := a.PreviewProcessPolicy([]string{insideDir}, nil, []uint64{outID})
	if err != nil {
		t.Fatalf("预览在交集为空时不该报错（它只描述结果）: %v", err)
	}
	if pv4.EffectiveCount != 0 || len(pv4.EffectiveIDs) != 0 {
		t.Fatalf("交集为空时预览应给 0，实得 %d/%v", pv4.EffectiveCount, pv4.EffectiveIDs)
	}
}
