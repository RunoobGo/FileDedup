package history

// DDP-9（2026-10-03 审查 P0-1）：外来账本不得让导入行拿到"把本机文件搬到任意外部路径"的回撤能力。
//
// 形状与 M355（内容证据那一半）成对：M355 封住的是"零哈希 ⇒ undoSourceCheck 同时跳过
// 全量 BLAKE3 与身份复核"，而**补上 hash 之后路径那一半仍然开着**——
//
//	internal/ops/undo.go:91  undoSourceCheck 只 Lstat/IsRegular/size/BLAKE3 校验 **DestPath**
//	internal/ops/undo.go:160 restoreInPlace 拿 OrigPath 直接 MkdirAll
//	internal/ops/undo.go:209 restoreInPlace 拿 OrigPath 直接 renameFile
//
// 换句话说 DestPath 的校验拦的是"搬错文件"，**没有一条判据管"搬到哪里去"**。
// 于是攻击者填 orig_path / dest_path / size / state / undoable 五个字段即可：
// 若本机 P 处恰有一份 size=S、blake3=H 的文件，就把它 rename 到任意外部 Q，
// 并在 Q 之下 MkdirAll 出任意目录树，而账本记"回撤成功"。
//
// 判据只收在"回撤真会吃到的那一集合"（undoable=1 且 state ∈ {done, undo_failed}），
// 与 M355 逐字同一集合：多拦一分就是过度拒绝，会打死"只搬台账看数据"那一档正常用法。
// 三个放行条件（根前缀 / hist_files 精确同值 / op_items 精确同值）各有负控制钉住。

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// ddp9Source 造一份外来账本影像，其可回撤条目的 orig_path 可控。
// 走真实导出件（src.ExportTo 的 .db 影像）作源，理由同 M355 那一族：
// 手搓源库会让"导出的东西导不回来"这一格永远绿着。
//
// ★ roots 一律走 insertScanRootAt（逐字可控）而不是 mkScanAt：后者把 root 拼成
// `["` + root + `"]`，单反斜杠的 Windows 路径落进去就成了非法 JSON（`\U`），
// 于是夹具会在导出那一步先红掉，红的地方与本组要验的判据无关。
func ddp9Source(t *testing.T, name, scanRoot, origPath string) string {
	t.Helper()
	src := newStoreAt(t, t.TempDir(), "ddp9src.db")
	histID := insertScanRootAtRet(t, src, scanRoot)
	// 满长非零内容证据：这一格刻意**不**留空，否则红会落在 M355 那条闸上，
	// 本组要验的是"证据齐备但落点越界"这一档。
	mkOpRawEvidPath(t, src, 1700000900, "trash", "/theirs/trash", histID, true, StateDone,
		bytes.Repeat([]byte{0x7F}, 32), origPath)
	return exportImage(t, src, name)
}

// P-1 攻击形状：证据齐备、状态可撤，而落点在本机扫描根之外 ⇒ 降级成不可撤。
//
// ★ 修前必红的一格：改前只有 checkUndoEvidence，而本组的 hash 是满长非零的。
//
//	改法是**降级不是拒收**——见 markForeignUndoPaths 的注释：整单拒绝会打死
//	"把 A 机账本导到 B 机看"这个正当用途（本组所在文件其余各格都是这一档）。
//	判据因此是"落进本机了，但 undoable=0"三件事同时成立。
func TestDDP9ImportDowngradesUndoableItemLandingOutsideScanRoots(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	image := ddp9Source(t, "ledger-escape.db", `["/theirs/c"]`, "/etc/authorized_keys")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：越界落点不该让整单导入失败（那是过度拒绝，会打死跨机导入）: %v", err)
	}
	if sum.OpsUndoDowngraded != 1 {
		t.Errorf("DDP-9：回执没如实记下降级笔数 %+v，want OpsUndoDowngraded=1", sum)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：记录本身应当照常导入（看台账这一档不受影响），实得 %+v", sum)
	}
	assertNoAppUndo(t, local, "P-1 越界落点")
	if !containsCJK(sum.OpsUndoDowngradeNote) || !strings.Contains(sum.OpsUndoDowngradeNote, "落点") {
		t.Errorf("DDP-9：降级说明必须中文且点名「落点」，实得 %q", sum.OpsUndoDowngradeNote)
	}
}

// P-2 正控制：同机导出导回（根前缀相同）⇒ 照收。
// 变异用：把 foreignPathAllowed 的 roots 分支删掉，本格必红。
func TestDDP9ImportAcceptsUndoableItemUnderScanRoot(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	image := ddp9Source(t, "ledger-samescan.db", `["/mine/scanned"]`, "/mine/scanned/sub/victim.bin")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：落点在本机扫描根之下的正常记录被拒了: %v", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：正控制回执失真 %+v，want OpsAdded=1", sum)
	}
	var got string
	if err := local.db.QueryRow(`SELECT orig_path FROM op_items ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "/mine/scanned/sub/victim.bin" {
		t.Errorf("DDP-9：搬运途中 orig_path 被改写 %q", got)
	}
}

// P-3 负控制：不可撤的记录（undoable=0）落在扫描根之外 ⇒ 照收。
// 拦它是过度拒绝——那类条目回撤腿根本不会吃它，正是手册 09 §6.8 承诺的
// "只搬历史台账看数据"那一档。变异用：把 undoable 条件删掉，本格必红。
func TestDDP9ImportAllowsNonUndoableItemOutsideScanRoots(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	src := newStoreAt(t, t.TempDir(), "ddp9src.db")
	histID := mkScanAt(t, src, 1700000200, "/theirs/c", 1)
	mkOpRawEvidPath(t, src, 1700000900, "trash", "/theirs/trash", histID, false, StateDone,
		bytes.Repeat([]byte{0x7F}, 32), "/etc/passwd")
	image := exportImage(t, src, "ledger-nonundo.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：undoable=0 的记录没能导入（%v）⇒ 闸超出了回撤选择集", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：负控制回执失真 %+v", sum)
	}
}

// P-4 负控制：已经撤成的条目（state=undone）落在扫描根之外 ⇒ 照收。
// 与 M355 的 P-5 同款：钉的是"判据与 app_ops.go 回撤选择集逐字同一集合"。
func TestDDP9ImportAllowsUndoneItemOutsideScanRoots(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	src := newStoreAt(t, t.TempDir(), "ddp9src.db")
	histID := mkScanAt(t, src, 1700000200, "/theirs/c", 1)
	mkOpRawEvidPath(t, src, 1700000900, "trash", "/theirs/trash", histID, true, StateUndone,
		bytes.Repeat([]byte{0x7F}, 32), "/etc/shadow")
	image := exportImage(t, src, "ledger-undone.db")

	if _, err := local.ImportFrom(image); err != nil {
		t.Fatalf("DDP-9：已撤成条目没能导入（%v）⇒ 闸管到了回撤吃不到的行", err)
	}
}

// P-5 精确同值出口：扫描行已被 MaxScanHistory 裁掉，但本机 hist_files 仍有那条路径
// ⇒ 照收。这是条件 2 的负控制；变异用：把 known 分支删掉，本格必红。
func TestDDP9ImportAcceptsExactMatchAgainstLocalHistFile(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	// 只有一条与本机扫描根**无关**的根，而待导入条目的落点精确等于本机某条 hist_files.path
	mkScanAt(t, local, 1700000000, "/mine/other", 1)
	insertHistFileAt(t, local, "/elsewhere/kept.bin")

	image := ddp9Source(t, "ledger-exact.db", `["/theirs/c"]`, "/elsewhere/kept.bin")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：与本机 hist_files 精确同值的落点被拒了（%v）⇒ 扫描行被裁掉后回撤条目全成死账", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：精确同值回执失真 %+v，want OpsAdded=1", sum)
	}
}

// P-6 空落点：外来落点为空串 ⇒ 降级。空串在回撤侧会相对工作目录解析，
// 而 GUI 进程的 CWD 可能是 /，落点不可预测。
func TestDDP9ImportDowngradesEmptyLandingPath(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	image := ddp9Source(t, "ledger-emptypath.db", `["/theirs/c"]`, "")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：空落点不该让整单导入失败: %v", err)
	}
	if sum.OpsUndoDowngraded != 1 {
		t.Errorf("DDP-9：空落点没被降级 %+v", sum)
	}
	assertNoAppUndo(t, local, "P-6 空落点")
}

// P-7 相对路径：落点无盘根、无前导 / ⇒ 降级（同 P-6 的理由，形状不同）。
func TestDDP9ImportDowngradesRelativeLandingPath(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	image := ddp9Source(t, "ledger-relpath.db", `["/theirs/c"]`, "relative/victim.bin")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：相对路径落点不该让整单导入失败: %v", err)
	}
	if sum.OpsUndoDowngraded != 1 {
		t.Errorf("DDP-9：相对路径落点没被降级 %+v", sum)
	}
	assertNoAppUndo(t, local, "P-7 相对路径落点")
}

// P-8 本机零扫描根时的文案：闸不许静默放行，且要说清"先扫一次盘"这条出路。
// 这一格是"全新安装 + 导入第一份外来账本"的真实形状。
func TestDDP9ImportDowngradesWithNoLocalScanRootsAndSaysScanFirst(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")

	image := ddp9Source(t, "ledger-noroots.db", `["/theirs/c"]`, "/theirsonly/victim.bin")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：零根不该让整单导入失败: %v", err)
	}
	if sum.OpsUndoDowngraded != 1 {
		t.Errorf("DDP-9：本机零扫描根时等于把闸整个关掉了（回执 %+v）", sum)
	}
	if !strings.Contains(sum.OpsUndoDowngradeNote, "扫描") {
		t.Errorf("DDP-9：零根时的降级说明要点名「先在本机扫描一次」这条出路，实得 %q", sum.OpsUndoDowngradeNote)
	}
	assertNoAppUndo(t, local, "P-8 零扫描根")
}

// P-9 同一笔里的一个条目越界 ⇒ 整笔降级（不是只降那一条）。
// 这一格钉的是"按 op 归并"这个形状：回撤是**整笔**触发的，
// 留一条缝在批量回撤里就等于没封（用户点一次整笔回撤，越界那条照样被搬走）。
func TestDDP9ImportDowngradesWholeOpWhenOneItemEscapes(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	mkScanAt(t, local, 1700000000, "/mine/scanned", 1)

	src := newStoreAt(t, t.TempDir(), "ddp9src.db")
	histID := mkScanAt(t, src, 1700000200, "/theirs/c", 1)
	opID := mkOpRawEvidPath(t, src, 1700000900, "trash", "/theirs/trash", histID, true, StateDone,
		bytes.Repeat([]byte{0x7F}, 32), "/theirs/c/safe.bin")
	// 同一个 op 里的第二个条目，落在扫描根之外
	if _, err := src.db.Exec(`INSERT INTO op_items
		(op_id, orig_path, dest_path, link_src, hash, size, mtime_ns, state, err)
		VALUES (?, '/System/Library/x.bin', '', '', ?, 1024, 7, 'done', '')`,
		opID, bytes.Repeat([]byte{0x7E}, 32)); err != nil {
		t.Fatalf("夹具落第二个 op_items 失败: %v", err)
	}
	image := exportImage(t, src, "ledger-mixed.db")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：一条越界不该让整单失败: %v", err)
	}
	if sum.OpsUndoDowngraded != 1 {
		t.Errorf("DDP-9：降级笔数 %+v，want 1（按 op 归并，一个 op 只降一次）", sum)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：记录本身照常导入，实得 %+v", sum)
	}
	assertNoAppUndo(t, local, "P-9 一个 op 里一条越界")
}

// P-10 Windows 写法的落点：外来账本里存的是 `C:\Users\me\x.bin`，本机根也是同一写法
// ⇒ 照收。这一格钉的是"分隔符归一只走 pathnorm，不自己写一份"（M64 的教训）：
// 变异用：把 foreignPathSep 改成 string(filepath.Separator)，在非 Windows 上本格会红
// —— 因为那时 `\` 不再被当分隔符，`C:\Users\me\x.bin` 整串成一个键。
func TestDDP9ImportAcceptsWindowsStyleLandingUnderSameRoot(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	insertScanRootAt(t, local, `["C:\\Users\\me\\docs"]`)

	image := ddp9Source(t, "ledger-win.db", `["C:\\Users\\me\\docs"]`, `C:\Users\me\docs\victim.bin`)

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：Windows 写法的落点在本机同写法扫描根之下却被拒（%v）⇒ 分隔符归一失效", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：Windows 写法回执失真 %+v，want OpsAdded=1", sum)
	}
}

// P-11 分隔符混写：`/` 与 `\` 在同一个键空间里等价（Slash 的职责）。
// 外来落点用 `/`、本机根用 `\`，同一位置必须认成"在根之下"。
func TestDDP9ImportTreatsBothSlashFlavorsAsSameKey(t *testing.T) {
	local := newStoreAt(t, t.TempDir(), "local.db")
	insertScanRootAt(t, local, `["C:\\Users\\me\\docs"]`)

	image := ddp9Source(t, "ledger-mixedslash.db", `["/theirs/c"]`, "C:/Users/me/docs/victim.bin")

	sum, err := local.ImportFrom(image)
	if err != nil {
		t.Fatalf("DDP-9：同一路径的两种分隔符写法被判成两处（%v）⇒ Slash 归一没生效", err)
	}
	if sum.OpsAdded != 1 {
		t.Errorf("DDP-9：混写分隔符回执失真 %+v，want OpsAdded=1", sum)
	}
}

// ---- 本组专用夹具 ----

// mkOpRawEvidPath 与 M355 族的 mkOpRawEvid 同构，差别只有一处：**orig_path 可控**。
// 复用 mkOpRawEvid 的话落点恒为 filepath.Join(targetDir, "victim.bin")，
// 而 targetDir 在本组必须与落点分开（根下 vs 根外是两个变量）。故立这一个变体。
func mkOpRawEvidPath(t *testing.T, s *Store, createdAt int64, kind, targetDir string,
	histID int64, undoable bool, state string, hash []byte, origPath string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO op_records
		(op_kind, created_at, target_dir, hist_id, undoable, done_count, reclaimed)
		VALUES (?, ?, ?, ?, ?, 1, 1024)`, kind, createdAt, targetDir, histID, undoable)
	if err != nil {
		t.Fatalf("夹具落 op_records 失败: %v", err)
	}
	opID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("夹具取 op id 失败: %v", err)
	}
	if origPath == "" {
		origPath = filepath.Join(targetDir, "victim.bin")
	}
	if _, err := s.db.Exec(`INSERT INTO op_items
		(op_id, orig_path, dest_path, link_src, hash, size, mtime_ns, state, err)
		VALUES (?, ?, '', '', ?, 1024, 7, ?, '')`, opID, origPath, hash, state); err != nil {
		t.Fatalf("夹具落 op_items 失败: %v", err)
	}
	return opID
}

// insertScanRootAtRet 落一条 roots 字段逐字可控的扫描行，返回 hist_id。
// 走 mkScanAt 的话 roots 恒是 `["<root>"]` 且 root 是单参数，凑不出
// "本机根用 \ 写法、待导入落点用 / 写法"这一档；且它把 root 拼进 JSON 时，
// 单反斜杠的 Windows 路径会成为非法 JSON（`\U`）⇒ 夹具先在导出那一步红掉。
// 所以调用方传进来的必须是**完整 JSON 文本**（`["C:\\Users\\me\\docs"]` 这种）。
//
// 顺带落一条 hist_groups（外键要它），让导出件更接近真实形状。
func insertScanRootAtRet(t *testing.T, s *Store, rootsJSON string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO scan_history
		(saved_at, roots, filters, threads, paranoid, groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths)
		VALUES (1700000000, ?, '{}', 4, 0, 1, 3, 3, 2000, '[]', '[]')`, rootsJSON)
	if err != nil {
		t.Fatalf("夹具落 scan_history 失败: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("夹具取 hist id 失败: %v", err)
	}
	var h [32]byte
	h[0] = 1
	if _, err := s.db.Exec(`INSERT INTO hist_groups (hist_id, hash, size, reclaimable, ord)
		VALUES (?, ?, 1000, 2000, 0)`, id, h[:]); err != nil {
		t.Fatalf("夹具落 hist_groups 失败: %v", err)
	}
	return id
}

// insertScanRootAt 是 insertScanRootAtRet 的无返回值形态。
func insertScanRootAt(t *testing.T, s *Store, rootsJSON string) {
	t.Helper()
	insertScanRootAtRet(t, s, rootsJSON)
}

// insertHistFileAt 直接往 hist_files 落一行指定路径（绕过 mkScanAt 的组/文件构造），
// 用来造"扫描行还在、但 hist_files.path 是另一处"这一档精确同值出口。
func insertHistFileAt(t *testing.T, s *Store, path string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO hist_files (hist_id, group_id, path, size, mtime_ns)
		VALUES (1, 1, ?, 1024, 7)`, path); err != nil {
		t.Fatalf("夹具落 hist_files 失败: %v", err)
	}
}

// assertNoAppUndo 断言"这笔记录在本机没有任何入口能发起应用内回撤"。
//
// ★ 为什么不只查 undoable 列：`undoable=0` 是本闸写下的值，而**回撤能不能发起**
//
//	走的是 GetOp 读出来的 meta.Undoable（app_ops.go:331 据它直接回绝）——
//	两处读的是同一列，但断言要落在**读侧**，否则"闸写了但读侧不认"这一格永远绿。
func assertNoAppUndo(t *testing.T, local *Store, what string) {
	t.Helper()
	_, ops, err := local.GetOp(1)
	if err != nil {
		t.Fatalf("%s：读回这笔记录失败 %v", what, err)
	}
	if len(ops) == 0 {
		t.Fatalf("%s：本地没有 op_items 行（降级把整笔删了？）", what)
	}
	meta, _, err := local.GetOp(1)
	if err != nil {
		t.Fatalf("%s：GetOp 失败 %v", what, err)
	}
	if meta.Undoable {
		t.Errorf("%s：回撤侧读出来 meta.Undoable 仍为 true ⇒ 降级没生效，威胁还在", what)
	}
}
