package main

// APP-40（2026-10-03 审查 P1-3）：载入历史 / 导出 / 导入三条腿必须接上维护闩。
//
// 形状：`app.go` 的覆盖清单（"已接闩 / 未接闩"那份）自己写着一条未接项——
//
//	未接闩：载入历史（LoadScanHistory 只问 opsRunning/scanInFlight）、
//	        导出/导入（app_records_io.go 只问 opsRunning）
//
// 本文件把那三处补齐，并把两种**具体后果**钉成判据（此前只有"清单里记着"这句自陈，
// 没有一条行为判据，所以它一直不红）：
//
//	① 载入历史 → curHistID 悬空：维护占闩期间载入一条即将被删的行，
//	   `a.curHistID = id` 落在删除之后 ⇒ 后续清理的 PruneScanFiles 拿 ErrNoRows，
//	   整笔裁剪回滚，用户看到一句与真实原因无关的「历史裁剪失败…请先重扫」。
//	② 导入 → 回执说谎：「清空清理记录」的整表删除在途时导入照样跑完
//	   （两者只在 s.mu 上串行，**串行不等于互斥**），前端弹「导入成功、新增 M 条」，
//	   而那 M 条连同其 op_items 已被那次删除抽走，用户界面上从未存在过。
//
// ★ 只用改前就存在的东西（pauseInLedgerClear 的暂停缝、stubDialog 接缝、
//   seedForeignLedger、ledgerCounts），判据落在行为上而不是编译错误上。
// ★ 每条都带**归因自检**（M93 的教训）：同一份影像、同一个 App，把闩放开之后必须
//   真的成功。缺这一格，"被拒"可能来自门禁链下游的另一道门（例如 M355 的内容证据闸），
//   于是本组用例会在闸门被摘掉时照样绿。

import (
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/history"
)

// m40ForeignImage 造一份外来账本影像，返回其路径与还原函数已就绪的 App。
// roots 取 /other/root（seedForeignLedger 的固定值），与本地 seedLedgerRows 的根不同，
// 否则自然键相同、增量合并退化成"全跳过"，P-49 的负控制就测不到任何东西。
func m40ForeignImage(t *testing.T) string {
	t.Helper()
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
	return image
}

// P-47 载入历史：清空清理记录在途 ⇒ 被拒，且 curHistID 不许指向那条即将被删的行。
// 这一格是①的判据：改前不接闩 ⇒ 载入成功 ⇒ curHistID 变成悬空 id。
func TestAPP40LoadScanHistoryRejectedWhileMaintaining(t *testing.T) {
	a := recordsApp(t)
	id := m381SeedScans(t, a, 2)

	pauseInLedgerClear(t, a, func() {
		if _, err := a.LoadScanHistory(id); err == nil {
			t.Fatal("APP-40：清空清理记录在途时载入历史被受理 ⇒ curHistID 会指向一条即将被删的行")
		} else if !strings.Contains(err.Error(), "清空清理记录") {
			t.Errorf("APP-40：拒绝话术没点名在等什么（期望含「清空清理记录」）：%v", err)
		}
		a.mu.Lock()
		cur := a.curHistID
		a.mu.Unlock()
		if cur == id {
			t.Errorf("APP-40：被拒的一格仍然写了 curHistID=%d ⇒ 悬空 id 已成立", cur)
		}
	})

	// 归因自检：闩释放后载入必须照常成功（防"被拒"来自下游另一道门）。
	if _, err := a.LoadScanHistory(id); err != nil {
		t.Fatalf("APP-40 归因自检：闩释放后载入历史仍被拒：%v", err)
	}
	a.mu.Lock()
	cur := a.curHistID
	a.mu.Unlock()
	if cur != id {
		t.Errorf("APP-40 归因自检：载入成功后 curHistID=%d，期望 %d", cur, id)
	}
}

// P-48 导出：清空清理记录在途 ⇒ 被拒。
// 后果是导出的镜像可能是**改写中的一致性切片**（用户在另一台机器上导回一份
// 半截账本，而那正是"导入"这条腿要处理的对象）。
func TestAPP40ExportRecordsRejectedWhileMaintaining(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	restore := stubDialog(t, dir, "")
	defer restore()

	pauseInLedgerClear(t, a, func() {
		res, err := a.ExportRecords()
		if err == nil {
			t.Fatalf("APP-40：清空清理记录在途时导出被受理（回执 %+v）⇒ 镜像是改写中的切片", res)
		}
		if !strings.Contains(err.Error(), "清空清理记录") {
			t.Errorf("APP-40 导出：拒绝话术没点名在等什么：%v", err)
		}
		if res.Cancelled {
			t.Errorf("APP-40 导出：被闸拒掉的请求不该报成「用户取消」")
		}
	})

	// 归因自检：闩释放后导出照常成功。
	res, err := a.ExportRecords()
	if err != nil {
		t.Fatalf("APP-40 导出归因自检：闩释放后仍被拒：%v", err)
	}
	if res.Cancelled || res.DbPath == "" {
		t.Errorf("APP-40 导出归因自检：闩释放后导出没有真的落盘（回执 %+v）", res)
	}
}

// P-49 导入：清空清理记录在途 ⇒ 被拒，且**账本一个字节都没动**。
// 这一格是②的判据：改前不接闩 ⇒ 导入成功 ⇒ 回执说谎。
func TestAPP40ImportRecordsRejectedWhileMaintaining(t *testing.T) {
	a := recordsApp(t)
	before := ledgerCounts(t, a)
	image := m40ForeignImage(t)
	restore := stubDialog(t, "", image)
	defer restore()

	pauseInLedgerClear(t, a, func() {
		res, err := a.ImportRecords()
		if err == nil {
			t.Fatalf("APP-40：清空清理记录在途时导入被受理（回执 %+v）⇒ 回执会说谎", res)
		}
		if !strings.Contains(err.Error(), "清空清理记录") {
			t.Errorf("APP-40 导入：拒绝话术没点名在等什么：%v", err)
		}
		if res.OpsAdded != 0 || res.ScansAdded != 0 {
			t.Errorf("APP-40 导入：被闸拒掉的请求不该带任何计数（回执 %+v）", res)
		}
		if got := ledgerCounts(t, a); got != before {
			t.Errorf("APP-40 导入：在途拒绝却改了账本 before=%v after=%v", before, got)
		}
	})

	// 归因自检：闩释放后导入照常成功，且账本真的增长了。
	if _, err := a.ImportRecords(); err != nil {
		t.Fatalf("APP-40 导入归因自检：闩释放后仍被拒：%v", err)
	}
	if got := ledgerCounts(t, a); got == before {
		t.Errorf("APP-40 导入归因自检：闩释放后导入没有写进任何行（前后都是 %v）", before)
	}
}

// P-50 导入：扫描在途 ⇒ 被拒。
// 这一问是同批补的：扫描收尾写 SaveScan，而导入按 localScanKeys 判重自然键——
// 两者不共享 s.mu 之外的任何东西，但**判重依据会被改写**，于是用户会看到
// "新增 0 条（跳过 1 条）"，而那一条其实是他自己一分钟前刚扫出来的。
func TestAPP40ImportRecordsRejectedWhileScanning(t *testing.T) {
	a := recordsApp(t)
	before := ledgerCounts(t, a)
	image := m40ForeignImage(t)
	restore := stubDialog(t, "", image)
	defer restore()

	setScanInFlight(a, true)
	res, err := a.ImportRecords()
	setScanInFlight(a, false)
	if err == nil {
		t.Fatalf("APP-40：扫描在途时导入被受理（回执 %+v）⇒ 判重依据会被改写", res)
	}
	if !strings.Contains(err.Error(), "扫描") {
		t.Errorf("APP-40 导入：拒绝话术没点名在等什么：%v", err)
	}
	if got := ledgerCounts(t, a); got != before {
		t.Errorf("APP-40 导入：扫描在途拒绝却改了账本 before=%v after=%v", before, got)
	}

	// 归因自检：扫描标志放开后必须真的导入成功。
	if _, err := a.ImportRecords(); err != nil {
		t.Fatalf("APP-40 导入归因自检：扫描标志放开后仍被拒：%v", err)
	}
	if got := ledgerCounts(t, a); got == before {
		t.Errorf("APP-40 导入归因自检：放开后导入没有写进任何行（前后都是 %v）", before)
	}
}

// P-51 静态锚：三条腿的维护闩问都必须在源码里，且 LoadScanHistory 有**两处**
// （入口 + 写 a.curHistID 那个临界区）。
//
// ★ 为什么这一格必须存在（本轮实测踩出来的）：P-47 的行为判据在**只删掉入口那一处**时
//
//	**照样绿** —— 因为临界区里那处仍拦得住。行为判据验的是"会不会被拒"，
//	验不出"哪一处问的"；而两处少一处就意味着读库期间维护抢占那一窗口重新敞开
//	（入口问过、读库、然后维护占闩删行、最后写 curHistID ⇒ 悬空 id 成立）。
//	这是 M93「恒绿却什么都没测」的同形：门禁链下游的任一道都能让结果变成"被拒"。
func TestAPP40MaintainingChecksExistInSource(t *testing.T) {
	// 载入历史：入口一处 + 临界区一处，缺任一即红。
	src := readSource(t, "app_history.go")
	sig := "func (a *App) LoadScanHistory(id int64) (ScanSummary, error) {"
	start := strings.Index(src, sig)
	if start < 0 {
		t.Fatalf("APP-40 静态锚读不到 %s —— 入口签名变了，本锚失效不得读作通过", sig)
	}
	body := src[start:]
	// ★ 找 `a.curHistID = id` 的**真赋值**而不是注释里提到它的那一句：
	//   app_history.go:58 的注释就在讲"若 `a.curHistID = id` 落在…"，
	//   裸 strings.Index 会抓到 :58 那处（偏在第一次加锁之前），于是
	//   LastIndex 找临界区那次锁时只会找到入口那一次 ⇒ 锚把两处混成一处。
	//   判据是"该行以 tab 缩进开头"——本仓缩进一律用 tab，而注释一律 `//` 开头。
	//   累加**字节**长度求偏移（下面几处都按字节用，不能拿行号当偏移）。
	var curAbs = -1
	var off int
	for _, line := range strings.SplitAfter(body, "\n") {
		if strings.HasPrefix(line, "\ta.curHistID = id") {
			curAbs = off
			break
		}
		off += len(line)
	}
	if curAbs < 0 {
		t.Fatalf("APP-40：LoadScanHistory 里读不到真赋值的 a.curHistID = id —— 结构变了，本锚失效")
	}
	lockIdx := strings.LastIndex(body[:curAbs], "a.mu.Lock()")
	if lockIdx < 0 {
		t.Fatalf("APP-40：LoadScanHistory 里找不到写 curHistID 之前的最后一次加锁")
	}
	critical := body[lockIdx:curAbs]
	// ★ 匹配 `a.maintaining`（字段名）而不是 `maint`（快照变量名）：临界区那处直接读字段，
	//   入口那处才用快照。两处字面量不同正是本锚能分辨它们的依据——
	//   早先按 `maint != ""` 匹配，把入口那处当成了临界区那处，删掉真的临界区问照样绿。
	if m := strings.Index(critical, "a.maintaining"); m < 0 {
		t.Error("APP-40：写 a.curHistID 的那个临界区里没有闩问 ⇒ 读库期间维护抢占的窗口重新敞开")
	}
	// ★ 入口那一问落在**第一次加锁之后、第二次（写回临界区）之前**：
	//   入口把 busy/maint 一起在锁内取快照、落锁之后才问。所以判据是
	//   "第一次锁之前没有闩问、第一次锁到第二次锁之间有一处" ——
	//   早先按 `body[:lockIdx]`（第二次锁之前 = 含入口那处）去找，方向对；
	//   但入口那处字面量是快照变量 `maint`，必须与临界区那处（字段 `a.maintaining`）分开匹配。
	firstLock := strings.Index(body, "a.mu.Lock()")
	if firstLock < 0 {
		t.Fatalf("APP-40：LoadScanHistory 里找不到第一次加锁 —— 结构变了，本锚失效")
	}
	entry := body[firstLock:lockIdx]
	if !strings.Contains(entry, `if maint != ""`) {
		t.Error("APP-40：LoadScanHistory 入口没有据 maint 拒绝的分支")
	}

	// 导出/导入：各一处，且必须与 opsRunning 那问**并列**（都在 a.mu 临界区里取快照）。
	rio := readSource(t, "app_records_io.go")
	for _, s := range []string{
		"func (a *App) ExportRecords() (RecordsExportResult, error) {",
		"func (a *App) ImportRecords() (RecordsImportResult, error) {",
	} {
		i := strings.Index(rio, s)
		if i < 0 {
			t.Fatalf("APP-40 静态锚读不到 %s —— 入口签名变了，本锚失效不得读作通过", s)
		}
		rest := rio[i:]
		if !strings.Contains(rest, `maint := a.maintaining`) {
			t.Errorf("APP-40：%s 没在同一临界区里取 a.maintaining 的快照", s)
		}
		if !strings.Contains(rest, `if maint != ""`) {
			t.Errorf("APP-40：%s 没有据 maint 拒绝的分支", s)
		}
	}
}
