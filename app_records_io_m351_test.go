package main

// M351/M352 的 App 层判据（2026-09-29 设计段 §5.3 / §6.3）。
//
// 这一层的承重判据只有三类，全都在**真实文件系统**上断言：
//   - 两件产物成对落盘、且真能打开读回（导出的存在理由）；
//   - 失败/取消/在途这三档**盘上一个文件都不许多**（半本账比没账更害人）；
//   - 回执上的每个数字都得对得上盘上的事实（界面要照它报数）。
//
// 对话框接缝（pickExportDir / pickImportFile）只用来把"用户选了哪儿"交进来，
// ★ 任何一条判据都不许建立在接缝的返回值上（批 1 §11.2 的同一条自纠纪律）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"filededup/internal/history"
	"filededup/internal/model"
)

// stubDialog 把两个对话框接缝指到固定路径，返回还原函数。
// 空串 = 模拟用户在对话框里点了取消（Wails 那条腿给的就是空串 + nil error）。
// ★ 接缝只负责"把用户的选择交进来"，下面每条判据断言的都是真实文件系统/真实账本。
func stubDialog(t *testing.T, wantDir, wantFile string) func() {
	t.Helper()
	oldDir, oldFile := pickExportDir, pickImportFile
	pickExportDir = func(context.Context) (string, error) { return wantDir, nil }
	pickImportFile = func(context.Context) (string, error) { return wantFile, nil }
	return func() { pickExportDir, pickImportFile = oldDir, oldFile }
}

// fixedNow 让两次导出落到**同一个文件名**上（同秒二次点击的形状）。
func fixedNow() time.Time { return time.Unix(1700000000, 0) }

// seedForeignLedger 在另一个库里种一条与本地自然键不同的扫描 + 一笔清理记录。
// 与 seedLedgerRows（本地夹具）刻意用不同 roots —— 否则两条自然键相同，
// "增量合并"会退化成"全跳过"，本批最想验的那一格就永远测不到。
func seedForeignLedger(t *testing.T, hs *history.Store) {
	t.Helper()
	if _, err := hs.SaveScan(model.ScanConfig{Roots: []string{"/other/root"}},
		[]*model.DuplicateGroup{mkGroup(1, 2048, "/other/root/a.bin", "/other/root/b.bin")}, nil); err != nil {
		t.Fatal(err)
	}
	var h [32]byte
	opID, err := hs.BeginOp("move", "/other/root", 1, true,
		[]history.OpItemPlan{{OrigPath: "/other/root/b.bin", Hash: h, Size: 2048, MtimeNs: 9}})
	if err != nil {
		t.Fatal(err)
	}
	if err := hs.FinishItem(opID, "/other/root/b.bin", "/other/root/.trash/b.bin", "", history.StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := hs.FinalizeOp(opID, 2048); err != nil {
		t.Fatal(err)
	}
}

// ledgerCounts 是"本地一字没动"的对照物：两张清单的逐行摘要拼成一个串。
// 只数条数挡不住 UPDATE 改写内容，所以把每行的关键字段一并进去。
func ledgerCounts(t *testing.T, a *App) string {
	t.Helper()
	scans, err := a.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	ops, err := a.ListOpRecords()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, sc := range scans {
		fmt.Fprintf(&b, "scan#%d saved=%d files=%d groups=%d roots=%s\n",
			sc.ID, sc.SavedAt, sc.Files, sc.Groups, strings.Join(sc.Roots, ","))
	}
	for _, op := range ops {
		fmt.Fprintf(&b, "op#%d kind=%s done=%d undone=%d hist=%d\n",
			op.ID, op.Kind, op.Done, op.Undone, op.HistID)
	}
	return b.String()
}

// recordsApp 带真实账本（无缓存句柄：这两个方法不碰缓存）。
func recordsApp(t *testing.T) *App {
	t.Helper()
	a, _ := newHistApp(t)
	seedLedgerRows(t, a.hist)
	return a
}

// ---- M351 导出 ----

func TestExportRecordsWritesBothFiles(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	restore := stubDialog(t, dir, "")
	defer restore()

	res, err := a.ExportRecords()
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if res.Cancelled {
		t.Fatal("夹具给了目录却回执 Cancelled")
	}
	for _, p := range []string{res.DbPath, res.JsonPath} {
		if st, serr := os.Stat(p); serr != nil || st.Size() == 0 {
			t.Fatalf("M351：产物缺失或为空 %s（%v）", p, serr)
		}
	}
	if filepath.Dir(res.DbPath) != dir || filepath.Dir(res.JsonPath) != dir {
		t.Errorf("M351：产物没落在用户选的目录：db=%s json=%s dir=%s", res.DbPath, res.JsonPath, dir)
	}
	if !strings.HasSuffix(res.DbPath, ".db") || !strings.HasSuffix(res.JsonPath, ".json") {
		t.Errorf("M351：两件套的后缀不对 db=%s json=%s", res.DbPath, res.JsonPath)
	}
	if res.Scans < 1 || res.Ops < 1 {
		t.Errorf("M351：回执条数失真 %+v（账本里明明有种好的记录）", res)
	}
	// ★ .db 必须**导得回来**：这是"备份"两个字唯一的可执行含义
	im, err := history.Open(res.DbPath)
	if err != nil {
		t.Fatalf("M351：导出的 .db 打不开（这份备份防不了误删）: %v", err)
	}
	defer im.Close()
	scans, err := im.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != res.Scans {
		t.Errorf("M351：回执说 %d 条，影像里读出 %d 条", res.Scans, len(scans))
	}
	// .json 给人核对 ⇒ 至少要是能解开的 JSON，且条数与 .db 一致
	body, err := os.ReadFile(res.JsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var m history.Mirror
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("M351：.json 不是合法 JSON（人没法核对）: %v", err)
	}
	if len(m.Scans) != len(scans) || len(m.Ops) != res.Ops {
		t.Errorf("M351：两件各讲一个故事 json{scans:%d ops:%d} db{scans:%d} 回执{ops:%d}",
			len(m.Scans), len(m.Ops), len(scans), res.Ops)
	}
	// 不许留 .tmp（半件导出会让人以为已经备份好了）
	if leftover, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(leftover) != 0 {
		t.Errorf("M351：导出成功后仍残留临时文件 %v", leftover)
	}
}

// 两件产物的权限收口（M353；windows 腿的读数由 M354 补）。
//
// 这条不是形式主义：导出目录是用户选的，很可能是外置盘或共享目录，而两件里装的都是
// **这台机器上的全部文件路径**。实测 `VACUUM INTO` 建的影像是 0644（跟 SQLite 默认），
// 只有 `os.WriteFile` 那件吃了 0o600 ⇒ 影像得显式 Chmod，否则承诺只兑了一半。
//
// ★ 判据分平台，因为**操作系统的权限模型不同**，不是为了让哪条腿变绿：
//   - unix（darwin / linux CI 腿）：POSIX 位真实生效 ⇒ 硬断 `0600`。删掉 Chmod 的变异
//     MU-s 在这两条腿上红（读数回到 0644）。
//   - windows：POSIX 位**不参与**该文件的访问判定（实际权限由所在目录的 NTFS ACL 继承），
//     且 Go 侧 `Stat` 对普通可写文件恒报 `0666`。首版断言吃了这条错前提，CI windows 腿
//     那条红（`run 36587694247`，读数 **0666**）就是它的照片。
//     ⇒ windows 这一臂**不判成功**，而是把"承诺在这条腿上落空"钉成一条会红的断言：
//     读数若不是那个"POSIX 位不被表达"的形状（owner 可写、且 group/other 没被收掉），
//     说明平台语义变了、或代码真的收口成功 ⇒ 必须回来同步手册措辞与本条判据。
func TestExportArtifactsAreOwnerOnly(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	res, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{res.DbPath, res.JsonPath} {
			st, serr := os.Stat(p)
			if serr != nil {
				t.Fatal(serr)
			}
			// 本轮 CI 基线 **0666**（`run 36587694247` 现读）：Go 在 Windows 上只把
			// "只读属性"映射进模式位，`0600` 与 `0644` 在那条腿上读回同一个 0666，
			// 真正的访问权由所在目录的 NTFS ACL 决定 ⇒ "仅所有者可读写"这句**不成立**。
			// 读数若不再是 0666，说明 Go/OS 语义变了或代码真的收口了：两者都要求回来
			// 同步 09 §6.8 / 10 §2.13 的措辞与本条判据，不许让它悄悄变成"承诺已兑现"的假象。
			if perm := st.Mode().Perm(); perm != 0o666 {
				t.Errorf("M354：%s 在 windows 腿报出 %04o（本轮基线 0666 ⇒ POSIX 位在这条腿上不被表达）。"+
					"读数变了就得回来同步手册措辞与本条判据", filepath.Base(p), perm)
			}
		}
		return
	}
	for _, p := range []string{res.DbPath, res.JsonPath} {
		st, serr := os.Stat(p)
		if serr != nil {
			t.Fatal(serr)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Errorf("M353：%s 权限是 %04o，要求 0600（账本里是用户机器上的全部路径）",
				filepath.Base(p), perm)
		}
	}
}

// 在途拒绝 ⇒ 两个文件都不生成（半本账不该被固化成一个看起来完整的文件）。
func TestExportRefusesWhileOpsRunning(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	restore := stubDialog(t, dir, "")
	defer restore()

	a.mu.Lock()
	a.opsRunning = true
	a.mu.Unlock()

	if _, err := a.ExportRecords(); err == nil {
		t.Fatal("M351：操作在途却报告导出成功")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("M351：在途拒绝后目录里留下了文件 %v（拒绝必须是不落盘）", entries)
	}
}

// 目标目录不可写 ⇒ 中文错 + 无 .tmp 残留。
func TestExportIntoNonWritableDir(t *testing.T) {
	a := recordsApp(t)
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	restore := stubDialog(t, dir, "")
	defer restore()

	// 有写权限的账号（root / CI 上的某些容器）这一档造不出来，如实跳过而不是假过。
	if err := os.WriteFile(filepath.Join(dir, "probe"), []byte("x"), 0o600); err == nil {
		t.Skip("本机允许往 0o500 目录里写（权限位没挡住），这一格交回其它腿")
	}
	_ = os.Remove(filepath.Join(dir, "probe"))

	if _, err := a.ExportRecords(); err == nil {
		t.Fatal("M351：写不进目标目录却报告导出成功")
	} else if !strings.Contains(err.Error(), "导出") && !strings.Contains(err.Error(), "影像") &&
		!strings.Contains(err.Error(), "镜像") {
		t.Errorf("M351：错误没说是哪一步失败：%v", err)
	}
	leftover, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(leftover) != 0 {
		t.Errorf("M351：失败后残留 %v（清理没做干净）", leftover)
	}
}

// 取消不是失败：回执 Cancelled=true、err=nil、盘上什么都没有。
// 这一格防的是把"用户点了又反悔"显示成一次错误（那会让人以为导出功能坏了）。
func TestExportCancelIsNotFailure(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	restore := stubDialog(t, "", "") // 空串 = Wails 对话框的取消形状
	defer restore()

	res, err := a.ExportRecords()
	if err != nil {
		t.Fatalf("M351：取消被报成错误: %v", err)
	}
	if !res.Cancelled {
		t.Errorf("M351：取消没进回执（前端只能靠空字段猜）：%+v", res)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("M351：取消后目录里留下了文件 %v", entries)
	}
}

// 同秒二次导出撞名 ⇒ 必须停手，不许用改名把第一份影像顶掉。
// （os.Rename 覆盖既有文件是静默的；§6.63 那条 P0 就是同一形状。）
func TestExportRefusesNameCollision(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	restore := stubDialog(t, dir, "")
	defer restore()

	first, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(first.DbPath)
	if err != nil {
		t.Fatal(err)
	}
	// 同一时间戳再导一次 ⇒ 名字相同（真机里就是同一秒内点两次）
	_, err = a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err == nil {
		t.Fatal("M351：撞名却报告第二次导出成功")
	}
	if !strings.Contains(err.Error(), "同名") {
		t.Errorf("M351：撞名的文案没点名撞名：%v", err)
	}
	after, err := os.ReadFile(first.DbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("M351：撞名时把第一份影像覆盖了（改名是静默覆盖）")
	}
}

// 撞名的东西即使是一个**目录**，也必须在动手前挡下来（exists 只看名字有没有被占）。
// 这一格钉的是"导出永远不会覆盖/牵连已有同名物"，与 .db 版撞名同一条判据的另一侧。
func TestExportRefusesNameTakenByDirectory(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	stem := "filededup-records-" + fixedNow().Format("20060102-150405")
	if err := os.MkdirAll(filepath.Join(dir, stem+".json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err == nil {
		t.Fatal("M351：最终名字被目录占着却报告导出成功")
	}
	if !strings.Contains(err.Error(), "同名") {
		t.Errorf("M351：没报成撞名：%v", err)
	}
	// 挡在动手前 ⇒ 那个目录原样、旁边不多任何文件
	st, serr := os.Stat(filepath.Join(dir, stem+".json"))
	if serr != nil || !st.IsDir() {
		t.Errorf("M351：占位的目录被动了（%v）", serr)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("M351：撞名拒绝后目录里多出了文件 %v", names)
	}
}

// corruptScanRoots 从**第二条连接**把账本里那条扫描的 roots 列写成非法 JSON。
// 公开 API 造不出坏列（`encodeJSON` 只会写合法 JSON，M321 之后连失败都上抛），
// 所以这一格必须直接动 SQL；路径取自 newHistApp 的落点，文件不在 ⇒ 当场 Fatal，
// 不会静默放行（改夹具文件名时这条会红，而不是变成假绿）。
func corruptScanRoots(t *testing.T, a *App, histID int64) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(a.cfgDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE scan_history SET roots = '这不是 JSON' WHERE id = ?`, histID); err != nil {
		t.Fatal(err)
	}
}

// 影像**已经生成**、镜像建到一半失败的那一档：这一格是唯一会真的在盘上留下一件
// "看起来是完整导出"的孤 .db 的路径（其余失败都发生在落位之前）。
// 变异读数（设计段 §12.6 MU-r2）：把这条分支上的 cleanup() 改成空操作，
// 补上本用例之前全盘无感，补上之后红在"目录里多出了文件"。
func TestExportImageBuiltThenFailureLeavesNoResidue(t *testing.T) {
	a := recordsApp(t)
	scans, err := a.ListScanHistory()
	if err != nil || len(scans) == 0 {
		t.Fatalf("夹具没有可改坏的扫描记录: %v", err)
	}
	corruptScanRoots(t, a, scans[0].ID)

	dir := t.TempDir()
	res, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err == nil {
		t.Fatal("M351：账本列是坏 JSON 却报告导出成功")
	}
	if !strings.Contains(err.Error(), "不是合法 JSON") {
		t.Errorf("M351：没报成坏列：%v", err)
	}
	if res.DbPath != "" || res.JsonPath != "" {
		t.Errorf("M351：失败的回执上还挂着产物路径 %q %q", res.DbPath, res.JsonPath)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("M351：导出中途失败留下了残留 %v（影像已生成，得靠 cleanup 收回）", names)
	}
}

// ---- M352 导入 ----

// 导一份外来影像进本地：只新增本地没有的，回执五个数对得上。
func TestImportRecordsMergesAndReports(t *testing.T) {
	local := recordsApp(t)
	// 外来账本：另开一个库，含本地没有的一条扫描 + 一笔清理记录
	srcDir := t.TempDir()
	src, err := history.Open(filepath.Join(srcDir, "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	seedForeignLedger(t, src)
	image := filepath.Join(srcDir, "image.db")
	if _, err := src.ExportTo(image); err != nil {
		t.Fatal(err)
	}

	beforeScans, err := local.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	beforeOps, err := local.ListOpRecords()
	if err != nil {
		t.Fatal(err)
	}

	restore := stubDialog(t, "", image)
	defer restore()
	res, err := local.ImportRecords()
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Cancelled {
		t.Fatal("夹具给了文件却回执 Cancelled")
	}
	if res.Source != image {
		t.Errorf("M352：回执没带上导入来源（界面无从说明是哪份文件）：%s", res.Source)
	}
	if res.ScansAdded != 1 || res.ScansSkipped != 0 || res.OpsAdded != 1 {
		t.Errorf("M352：回执计数不对 %+v，want scansAdded=1 opsAdded=1", res)
	}
	afterScans, err := local.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	afterOps, err := local.ListOpRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterScans) != len(beforeScans)+res.ScansAdded {
		t.Errorf("M352：扫描历史条数与回执不符 before=%d after=%d 回执=%d",
			len(beforeScans), len(afterScans), res.ScansAdded)
	}
	if len(afterOps) != len(beforeOps)+res.OpsAdded {
		t.Errorf("M352：清理记录条数与回执不符 before=%d after=%d 回执=%d",
			len(beforeOps), len(afterOps), res.OpsAdded)
	}
	// 本地原有那条必须还在，而且还能展开明细（导入把外来的行搬进来不该踩坏本地的关联）
	if len(beforeScans) > 0 {
		if _, err := local.LoadScanHistory(beforeScans[0].ID); err != nil {
			t.Errorf("M352：导入后本地原有记录读不出了: %v", err)
		}
	}
}

// 同一份文件导两次不翻倍（R-2 的"增量"落地）。
func TestImportRecordsTwiceDoesNotDouble(t *testing.T) {
	local := recordsApp(t)
	srcDir := t.TempDir()
	src, err := history.Open(filepath.Join(srcDir, "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	seedForeignLedger(t, src)
	image := filepath.Join(srcDir, "image.db")
	if _, err := src.ExportTo(image); err != nil {
		t.Fatal(err)
	}
	restore := stubDialog(t, "", image)
	defer restore()

	if _, err := local.ImportRecords(); err != nil {
		t.Fatal(err)
	}
	scans1, _ := local.ListScanHistory()
	ops1, _ := local.ListOpRecords()
	second, err := local.ImportRecords()
	if err != nil {
		t.Fatalf("第二次导入报错（幂等腿断了）: %v", err)
	}
	if second.ScansAdded != 0 || second.OpsAdded != 0 {
		t.Errorf("M352：第二次导入仍在新增 %+v", second)
	}
	scans2, _ := local.ListScanHistory()
	ops2, _ := local.ListOpRecords()
	if len(scans2) != len(scans1) || len(ops2) != len(ops1) {
		t.Errorf("M352：二次导入后条数变了 scans %d→%d ops %d→%d", len(scans1), len(scans2), len(ops1), len(ops2))
	}
}

func TestImportRefusesWhileOpsRunning(t *testing.T) {
	local := recordsApp(t)
	srcDir := t.TempDir()
	src, err := history.Open(filepath.Join(srcDir, "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	seedForeignLedger(t, src)
	image := filepath.Join(srcDir, "image.db")
	if _, err := src.ExportTo(image); err != nil {
		t.Fatal(err)
	}
	before := ledgerCounts(t, local)

	restore := stubDialog(t, "", image)
	defer restore()
	a := local
	a.mu.Lock()
	a.opsRunning = true
	a.mu.Unlock()

	if _, err := a.ImportRecords(); err == nil {
		t.Fatal("M352：操作在途却报告导入成功")
	}
	if got := ledgerCounts(t, a); got != before {
		t.Errorf("M352：在途拒绝却改了账本 before=%v after=%v", before, got)
	}
}

// 取消不是失败（与导出同一形状）。
func TestImportCancelIsNotFailure(t *testing.T) {
	a := recordsApp(t)
	before := ledgerCounts(t, a)
	restore := stubDialog(t, "", "")
	defer restore()

	res, err := a.ImportRecords()
	if err != nil {
		t.Fatalf("M352：取消被报成错误（界面会显示一次失败）: %v", err)
	}
	if !res.Cancelled {
		t.Errorf("M352：取消没进回执：%+v", res)
	}
	if got := ledgerCounts(t, a); got != before {
		t.Errorf("M352：取消后账本变了 before=%v after=%v", before, got)
	}
}

// 非账本文件 ⇒ 中文错 + 本地一字不动。
func TestImportRejectsNonLedger(t *testing.T) {
	a := recordsApp(t)
	before := ledgerCounts(t, a)
	bad := filepath.Join(t.TempDir(), "notes.db")
	if err := os.WriteFile(bad, []byte("这是备忘录文本，不是账本"), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := stubDialog(t, "", bad)
	defer restore()

	if _, err := a.ImportRecords(); err == nil {
		t.Fatal("M352：喂非账本文件却报告导入成功")
	}
	if got := ledgerCounts(t, a); got != before {
		t.Errorf("M352：坏文件改了本地账本 before=%v after=%v", before, got)
	}
}
