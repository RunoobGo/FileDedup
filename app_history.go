// app_history.go —— 扫描历史与操作记录。
//
// M336（2026-09-28 第七轮审查批）：由 app.go 按职责簇拆分而来。
// **方法名与签名一字未改**——Wails 绑定按方法名解析，与所在文件无关，
// 因此这是零行为改动的纯移动：前后端契约、调用方、既有测试都不受影响。
//
// 包级类型/常量/变量与非方法函数仍留在 app.go：它们被多簇共用，
// 拆开只会让「这个类型在哪」变成第二次查找。
package main

import (
	"filededup/internal/cache"
	"filededup/internal/history"
	"filededup/internal/model"
	"filededup/internal/ops"
	"fmt"
	"os"
	"runtime"
)

// ListScanHistory 历史列表（新→旧）。
func (a *App) ListScanHistory() ([]HistoryMeta, error) {
	hs := a.histSnapshot()
	if hs == nil {
		return nil, fmt.Errorf("历史库不可用")
	}
	ms, err := hs.ListScans()
	if err != nil {
		return nil, shellRPCError(err) // M367：账本腿的裸原话进中文壳（§3.2a）
	}
	out := make([]HistoryMeta, 0, len(ms))
	for _, m := range ms {
		out = append(out, HistoryMeta{
			ID: m.ID, SavedAt: m.SavedAt, Roots: m.Roots, Filters: m.Filters,
			Threads: m.Threads, Paranoid: m.Paranoid,
			Groups: m.Groups, Files: m.Files, OrigFiles: m.OrigFiles,
			Reclaimable: m.Reclaimable,
		})
	}
	return out, nil
}

// LoadScanHistory 恢复一条历史为当前结果集（可直接继续清理）。
// 陈旧文件安全性由操作前的逐文件校验兜底（S1 篡改拦截 / S8 消失即跳过），
// 载入时不做文件系统遍历。
func (a *App) LoadScanHistory(id int64) (ScanSummary, error) {
	hs := a.histSnapshot()
	if hs == nil {
		return ScanSummary{}, fmt.Errorf("历史库不可用")
	}
	a.mu.Lock()
	busy := a.opsRunning || a.scanInFlight
	a.mu.Unlock()
	if busy {
		return ScanSummary{}, fmt.Errorf("扫描/清理进行中，请稍后再打开历史")
	}

	meta, groups, err := hs.LoadScan(id)
	if err != nil {
		return ScanSummary{}, shellRPCError(err)
	}
	byID := make(map[uint64]*model.FileEntry, meta.Files)
	var reclaim uint64
	var reclaimActual uint64
	for _, g := range groups {
		reclaim += g.Reclaimable
		// M6-P2：历史库没有实占口径，LoadScan 按"成员全部 unknown"回填，
		// 所以这里两数**必然相等**。相等不是"实占恰好等于逻辑大小"的结论，
		// 而是"没统计过"——界面须靠成员的 ActualKnown 判定并显示"未统计"，
		// 不得把这个数当成实占播报（与 M6-P4 三项计数同一处置）。
		reclaimActual += g.ReclaimableActual
		for _, f := range g.Files {
			byID[f.ID] = f
		}
	}
	// 保留决策持久化的是路径（功能 1 语义），恢复时映射回本次载入的行 ID
	keepSet := make(map[string]bool, len(meta.KeepPaths))
	for _, p := range meta.KeepPaths {
		keepSet[p] = true
	}
	var keepIDs map[uint64]bool
	for _, g := range groups {
		for _, f := range g.Files {
			if keepSet[f.Path] {
				if keepIDs == nil {
					keepIDs = make(map[uint64]bool)
				}
				keepIDs[f.ID] = true
			}
		}
	}

	a.mu.Lock()
	// 二次确认：读库期间可能有任务抢占（与 StartScan 的 check-and-set 同锁串行）
	if a.opsRunning || a.scanInFlight {
		a.mu.Unlock()
		return ScanSummary{}, fmt.Errorf("任务已开始，历史未载入")
	}
	a.groups = groups
	a.byID = byID
	a.keepIDs = keepIDs
	a.failed = meta.Failed
	a.invalidateViewCacheLocked()
	// 载入即换代（2026-09-18 审查 C7）：此刻 scanInFlight 可能已是 false 而旧
	// 扫描 goroutine 仍在收尾，不换新代际就会被它把 curHistID 写回自己那行。
	a.resultGen.Add(1)
	a.curHistID = id
	a.resultsReady = true
	a.mu.Unlock()

	return ScanSummary{
		Groups: len(groups), Reclaimable: reclaim, ReclaimableActual: reclaimActual,
		FilesFailed: len(meta.Failed),
	}, nil
}

// DeleteScanHistory 删除一条历史；若正是当前结果集的来源，仅断开联动
// （内存结果仍可看可清，只是后续裁剪不再回写）。
//
// M381（第九轮批 A2）：这条腿原先**一条闸都不接** —— 报告 P1-2 的"确认清空"四层防线
// 缺的就是后端这一层。现在与 ClearOpRecords 同形：claimMaintenance 占位期间做 SQL
// （闩不是锁，库操作在 a.mu 之外，见 app.go 的 maintaining 注释）。
func (a *App) DeleteScanHistory(id int64) error {
	if err := a.claimMaintenance("删除历史记录", "删除历史记录"); err != nil {
		return err
	}
	defer a.releaseMaintenance()
	hs := a.histSnapshot()
	if hs == nil {
		return fmt.Errorf("历史库不可用")
	}
	if err := hs.DeleteScan(id); err != nil {
		return shellRPCError(err)
	}
	a.mu.Lock()
	if a.curHistID == id {
		a.curHistID = 0
	}
	a.mu.Unlock()
	return nil
}

// ClearScanHistory 清空全部扫描历史（当前结果集仅断开联动）。
//
// M381：同 DeleteScanHistory —— 整表删除必须与扫描写回、清理逐项落账、回撤、
// 以及另一项维护互斥，否则"清空成功"会把一笔正在落账的扫描的归属抽走。
func (a *App) ClearScanHistory() error {
	if err := a.claimMaintenance("清空全部历史", "清空全部历史"); err != nil {
		return err
	}
	defer a.releaseMaintenance()
	hs := a.histSnapshot()
	if hs == nil {
		return fmt.Errorf("历史库不可用")
	}
	if err := hs.ClearScans(); err != nil {
		return shellRPCError(err)
	}
	a.mu.Lock()
	a.curHistID = 0
	a.mu.Unlock()
	return nil
}

// histSnapshot 在锁内取账本句柄（M9，2026-09-21 全仓审计 §五 9）。
//
// `a.hist` 是受 `a.mu` 保护的字段：`shutdown` 在锁内把它置 nil（Close 之后置空），
// 而绑定层的 RPC 与扫描 goroutine 随时可能正在读它。直接 `if a.hist == nil` 再
// `a.hist.X()` 是**解引用两次**且都在锁外——前者是数据竞争，后者还可能拿到刚被
// 置空的值。取一次快照后所有调用都用 hs，字段本身只读一次、且在锁内读。
//
// 已经在大临界区里取过快照的调用点（ExecuteOperation / UndoOperation 等）不必
// 走这里，注释里的「锁内快照」与此同源。
func (a *App) histSnapshot() *history.Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hist
}

// cchSnapshot 在锁内取哈希缓存句柄（APP-2，2026-09-21 全量审查）。
//
// 与 M9 的 histSnapshot 逐字同形、同一条理由：`a.cch` 由 `a.mu` 保护
// （shutdown 在锁内 Close 并置空），而绑定层入口原先写的是
// `if a.cch == nil {...}; return a.cch.GetStats()`——锁外解引用两次，
// -race 下是一次真竞争（本仓已用探针复现，见 app_cch_race_test.go 的改前读数）。
// M9 的 AST 门禁白名单只管 `a.hist`，所以同一类缺陷的第二例一路漏到本轮。
func (a *App) cchSnapshot() *cache.Cache {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cch
}

// warnBackground 是"后台动作没成，但不影响主流程结论"的统一出口：
// stderr + app:error 双通道，同一句话。
//
// 为什么必须发事件：打包后的 GUI 没有控制台，只写 stderr 等于没写。
// 出口本身只保证一件事：**绝不静默**；"哪一笔、后果是什么"由调用方写进 userMsg。
//
// M58 前只有 warnLedger 一个出口，M58（定位命令失败）复用同一条通道时才发现它
// 把 "[history]" 与"账本写入失败："写死了 —— 直接复用会造出一句假话
// （一条 Finder 启动失败被报成"账本写入失败"）。故抽出本函数，warnLedger 退居一行包装。
func (a *App) warnBackground(tag, userMsg string) {
	fmt.Fprintf(os.Stderr, "[%s] %s\n", tag, userMsg)
	if a.emit != nil && a.ctx != nil {
		a.emit(a.ctx, "app:error", map[string]string{"error": userMsg})
	}
}

// warnLedger 是"账本没写进去"的统一出口（M7，2026-09-21）。
//
// 修正前 FinishItem/FinalizeOp/persistKeepPaths/MarkItemUndo 四处落账失败都只写 stderr，
// 磁盘满时条目停在 planned、`ops:done` 横幅照报"成功"，用户按 09 §6.7
// 「动手之前先写账本」预期可回撤，实际无账本可撤。
//
// 口径与 SaveScan 一致（本函数即从那里抽出），失败原因逐点由调用方写清"哪一笔、
// 后果是什么"。
func (a *App) warnLedger(msg string) {
	a.warnBackground("history", "账本写入失败："+msg)
}

// beginJournal 在任何文件系统动作之前把本次清理的完整计划落盘（写前账本）。
// undoable 判定：delete 不可撤；Windows 回收站拿不到 src→dst 映射，
// 回撤改由「打开系统回收站」引导；其余可撤。
//
// 2026-09-18 审查 C3（用户裁定：仅可回撤类型拒绝）：账本不可用（hist 为 nil 或 BeginOp
// 失败）时，承诺过可回撤的操作（回收站/移动/硬链接）一律拒绝执行——原先
// 只发一条事件便照常移动文件，用户按手册 09 §6.7 预期可回撤而实际无账本可撤。
// 不承诺回撤的（永久删除、Windows 回收站）仍放行：这类操作故障前后能力等价，
// 若一并拒绝反而会把用户逼向唯一可用的破坏性路径；改为 stderr + app:error
// 显式留痕（绝不静默）。
//
// 必须在 a.mu 之外调用（hist 访问不与 a.mu 嵌套）；调用方已置 opsRunning。
func (a *App) beginJournal(hs *history.Store, histID int64, op model.OpRequest,
	groups []*model.DuplicateGroup, keepIDs map[uint64]bool) (int64, error) {
	plans := planOpItems(groups, keepIDs, op.FileIDs)
	if len(plans) == 0 {
		return 0, fmt.Errorf("无可操作文件（所选 id 均不在当前结果集，或均为保留项）")
	}
	undoable := undoableFor(op.Kind, runtime.GOOS)
	jid, jerr := func() (int64, error) {
		if hs == nil {
			return 0, fmt.Errorf("history.db 未就绪")
		}
		return hs.BeginOp(op.Kind, op.TargetDir, histID, undoable, plans)
	}()
	if jerr == nil {
		return jid, nil
	}
	if undoable {
		return 0, fmt.Errorf("清理账本不可用，已拒绝执行（无账本即无法回撤与追溯）：%w", jerr)
	}
	// 不可回撤类：留痕放行
	msg := fmt.Sprintf("本次操作未写入清理账本（%v）：该操作本就不支持应用内回撤，已照常执行并在此留痕", jerr)
	// M373（§3.2g）：走统一出口，而不是裸调 emit。Wails 的 runtime 在拿到空 ctx 时
	// 走的是 log.Fatal ⇒ **整个进程直接退出**（比 panic 更狠，`recoverGoroutine` 都兜不住），
	// 而 `a.ctx` 要等 startup 才接线。warnBackground 带 nil 守卫，stderr 那行逐字不变。
	a.warnBackground("history", msg)
	return 0, nil
}

// ListOpRecords 清理记录列表（新→旧）。
func (a *App) ListOpRecords() ([]history.OpMeta, error) {
	hs := a.histSnapshot()
	if hs == nil {
		return nil, fmt.Errorf("历史库不可用")
	}
	ms, err := hs.ListOps()
	if err != nil {
		return nil, shellRPCError(err)
	}
	return ms, nil
}

// GetOpRecord 单条清理记录的条目明细。
//
// 逐条标注链接状态（2026-09-20）：只对**已成功执行**的 done 条目做检测——
// 其余状态（failed/skipped/cancelled）本就没在文件系统上动手，
// 原位可能有意料之外的第三方文件，检测它们只会产出误导性的标红。
//
// ★ F1⑥（2026-09-23 登记，**不改行为**）：下面那个循环是**逐条同步 Lstat**，
// 条目数没有上限（一笔大批量清理可上千条），且路径取自账本、可能是当初那个
// 网络卷/外置盘。死挂载与拔走的 SMB 上 Lstat 可以长时间不返回 ⇒ 这条 RPC 会拖着
// 记录页一起不返回，而界面上没有取消入口。这与 B2（R2-2）收的"探测听取消"同族，
// 差别在这里要收的是**读侧的逐条 stat**：要么加"只检测前 N 条"、要么给整条 RPC 配
// ctx 与超时（= 改 OpRecordItem 的检测时机/契约），两条都超出一行级小修的范围 ⇒
// 本批只把风险写进注释，改法随批登记为欠账（J-6 取向）。
func (a *App) GetOpRecord(opID int64) (OpRecordDetail, error) {
	hs := a.histSnapshot()
	if hs == nil {
		return OpRecordDetail{}, fmt.Errorf("历史库不可用")
	}
	m, items, err := hs.GetOp(opID)
	if err != nil {
		return OpRecordDetail{}, shellRPCError(err)
	}
	list := make([]OpRecordItem, 0, len(items))
	for _, it := range items {
		view := OpRecordItem{OpItem: it}
		if it.State == history.StateDone {
			view.IsSymlink, view.Dangling = ops.SymlinkStatus(it.OrigPath)
		}
		list = append(list, view)
	}
	return OpRecordDetail{Meta: *m, Items: list}, nil
}

// ClearOpRecords 删除全部清理账本（不动已回收/已移动的文件本身）。
// B3-1：清理/回撤在途时拒绝——账本此刻正被 FinishItem/FinalizeOp 逐项落账，
// 整表删除会让一个仍在移动文件的操作失去全部记录（事后既无从回撤也无从追溯），
// 而界面上只表现为"清空成功"。
// M364（第八轮批 2）：那一道闸原先是「锁内问一句 → 锁外删整表」两段式，
// 中间放行的一笔新清理会先写好写前账本、再把账本被这次整表删除抽走；同一段也没有
// 挡住在途扫描（扫描的写回腿正往 history.db 落行）。现在走 claimMaintenance，
// 三问与占位在同一个临界区里定下，并在整表删除期间一直保持占住（闩，不是锁）。
//
// ★ 两个名字不合成一个：撞在 opsRunning 上那半句要用**改前就立好的原话**
//
//	「清理/回撤操作执行中，请等待结束后再清空记录」（B3 的文案，一字不许动），
//	而别人被这一格挡在门外时该听到「清空清理记录进行中……」（与「清空缓存」对称，
//	用户要知道在等什么）。一个串两用必然挑错一种 ⇒ claimMaintenance 收两个参数。
//
// ledgerClearOps 是"整表删除清理账本"的注入点（接缝惯例同 cacheClearSnapshot、
// cacheClearStats）。
//
// 为什么这条也要接缝：M364 的承重判据是**编排顺序**——"有没有在途操作"这一问与
// "整表删除"这一步必须在同一个临界区里定下来（判完到删之间放行一笔新清理，
// 就会把一笔正在逐项落账的操作的账本抽走）。要断言这一句，测试必须能在删除真正
// 发生的那一刻暂停下来，从旁边看一眼闸门是否已经占住；而 `hs.ClearOps()` 本身
// 是一个瞬间完成的 SQL，没有可插的缝。
var ledgerClearOps = func(hs *history.Store) error { return hs.ClearOps() }

func (a *App) ClearOpRecords() error {
	if err := a.claimMaintenance("清空清理记录", "清空记录"); err != nil {
		return err
	}
	defer a.releaseMaintenance()
	hs := a.histSnapshot()
	if hs == nil {
		return fmt.Errorf("历史库不可用")
	}
	return ledgerClearOps(hs)
}
