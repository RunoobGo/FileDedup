// app_scan.go —— 扫描控制与目录选择。
//
// M336（2026-09-28 第七轮审查批）：由 app.go 按职责簇拆分而来。
// **方法名与签名一字未改**——Wails 绑定按方法名解析，与所在文件无关，
// 因此这是零行为改动的纯移动：前后端契约、调用方、既有测试都不受影响。
//
// 包级类型/常量/变量与非方法函数仍留在 app.go：它们被多簇共用，
// 拆开只会让「这个类型在哪」变成第二次查找。
package main

import (
	"filededup/internal/model"
	"fmt"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"time"
)

// SelectDirectory 调起系统目录选择器。
// 注：Wails v2 目录选择为单选（平台对话框限制），前端可连续多次添加。
// H5：选择结果同时登记为本会话授权目录（move 目标的白名单来源）。
func (a *App) SelectDirectory() (string, error) {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择目录",
	})
	if err == nil && dir != "" {
		a.authorizeDir(dir)
	}
	return dir, err
}

// StartScan 创建扫描任务，返回 taskID；运行中重复调用返回错误。
func (a *App) StartScan(cfg model.ScanConfig) (string, error) {
	if len(cfg.Roots) == 0 {
		return "", fmt.Errorf("请至少添加一个扫描目录")
	}
	if s := a.pipe.Status(); s != model.StatusIdle && s != model.StatusDone &&
		s != model.StatusCancelled && s != model.StatusFailed {
		return "", fmt.Errorf("任务进行中（%s），请先暂停或取消", s)
	}
	// 在途互斥：pipe 状态与 goroutine 收尾之间存在窗口（如 Run 已置 Done 但
	// 结果集未写完），此时放行新扫描会让旧 goroutine 的陈旧结果覆盖新任务
	a.mu.Lock()
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("上一个扫描任务尚未收尾，请稍候")
	}
	// P1-1：清理操作 goroutine 在途时禁止开新扫描。ExecuteOperation 在锁外捕获
	// groups 快照、执行完在锁内回写 a.groups；若此间开新扫描并清空结果集，
	// 旧操作的收尾会用「基于旧结果集清理后的列表」整个覆盖新扫描结果，
	// 新结果静默丢失且无任何提示。
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("清理操作执行中，请等待完成后再扫描")
	}
	// M359（第八轮批 2）：维护动作（清空缓存 / 清空清理记录）在途时不开新扫描。
	// 清缓存吃的是 hash_cache 整表 + WAL 截断，新扫描正要把算出来的条目往同一张表写回；
	// 反向那一半（清缓存拒绝在扫描在途时开工）在 CacheClear 里，两边同锁串行才叫闭合。
	if a.maintaining != "" {
		m := a.maintaining
		a.mu.Unlock()
		return "", fmt.Errorf("%s进行中，请等待完成后再扫描", m)
	}
	a.scanInFlight = true
	myGen := a.resultGen.Add(1) // 代际号：goroutine 收尾据此判断自己是否已被取代
	a.groups = nil
	a.byID = make(map[uint64]*model.FileEntry)
	a.keepIDs = nil
	a.failed = nil
	// ★ F1①（2026-09-23 第四轮全仓审查）：lastEvs 也在这里作废。它是进度回调的唯一写者，
	//   不复位就等于：新扫描已把 groups 清空、GetScanProgress 却仍回吐上一轮的终值——
	//   对本轮进度的一次**谎报**（注释自称的"断线重连语义"因此不成立）。零值是诚实答案：
	//   本轮还没有任何进度。（前端目前无消费者，见 §30.11 的如实收窄。）
	a.lastEvs = model.ProgressEvent{}
	a.resultsReady = false // 旧结果集作废（历史恢复/上次扫描均不再可操作）
	a.curHistID = 0
	a.invalidateViewCacheLocked() // Y3：新任务清空旧视图
	a.mu.Unlock()

	// P3：时间戳秒级精度不是唯一 ID——同一秒内重扫（取消后立刻重试很常见）会
	// 产生重复 taskID，前端若据此关联事件就会串台。追加原子序号保证单调唯一。
	taskID := fmt.Sprintf("scan-%s-%d", time.Now().Format("20060102-150405"), a.taskSeq.Add(1))
	a.goTask("scan", func() {
		a.mu.Lock()
		a.scanInFlight = false
		a.mu.Unlock()
	}, func() {
		start := time.Now()
		groups, failed, err := a.pipe.Run(a.ctx, cfg)
		elapsed := time.Since(start)
		// 复位必须先于终止事件发出：前端（与单测）收到 done/cancelled/error 后
		// 可能立刻重扫，若此刻在途标志仍为 true 会被误拒。goTask 的 reset
		// 保留为 panic 展开路径的兜底（其 LIFO 顺序本就先于 recover 发事件）。
		resetInFlight := func() {
			a.mu.Lock()
			a.scanInFlight = false
			a.mu.Unlock()
		}
		// 代际判定：复位在途标志后本 goroutine 还剩「写回结果集 → SaveScan →
		// resultsReady/curHistID → 终止事件」几拍，其间新扫描完全可能已被用户
		// 发起（StartScan 只看 scanInFlight）。代际不符即弃写、弃发事件，
		// 否则旧任务会把新扫描的 curHistID 换成自己的历史行——之后的清理
		// 裁剪的是上一条记录，当前记录永远残留已删文件。
		superseded := func() bool { return a.resultSuperseded(myGen) }
		if err != nil {
			if a.pipe.Status() == model.StatusCancelled {
				resetInFlight()
				if !superseded() {
					a.emit(a.ctx, "scan:cancelled", ScanSummary{Elapsed: elapsed.String()})
				}
			} else {
				resetInFlight()
				if !superseded() {
					a.emit(a.ctx, "scan:error", errorEvent(err)) // M202：中文外壳，原文降级 detail
				}
			}
			return
		}
		a.mu.Lock()
		a.groups = groups
		a.failed = failed
		a.invalidateViewCacheLocked() // Y3：新结果集使旧排序缓存失效
		for _, g := range groups {
			for _, f := range g.Files {
				a.byID[f.ID] = f
			}
		}
		var reclaim uint64
		var reclaimActual uint64
		for _, g := range groups {
			reclaim += g.Reclaimable
			reclaimActual += g.ReclaimableActual
		}
		a.scanInFlight = false
		a.mu.Unlock()
		// F1（②，2026-09-23 第四轮全仓审查）：判代际必须**早于**落库。改前的顺序是
		// SaveScan（锁外，慢）→ 再判 superseded → 弃写，于是被取代的那一轮照样占一格
		// history.MaxScanHistory，把最旧的**有效**记录挤掉（外键 CASCADE 连子行一起删）。
		// ★ 前移只是把窗口从"SaveScan + 两拍"收窄到"本行到 SaveScan 起跑之间"，**不消除**：
		//   SaveScan 依 App.hist 的既有约束必须在锁外，判代际与落库之间天然让一次锁。
		if scanAboutToSaveHook != nil {
			scanAboutToSaveHook()
		}
		if superseded() {
			fmt.Fprintf(os.Stderr, "[scan] 第 %d 代扫描已被新任务取代，历史与结果集均弃写\n", myGen)
			return
		}
		// v0.5.0 功能 3：扫描成功收尾自动写历史（锁外调用，见 App.hist 注释）。
		// 保存失败不影响结果集可用，只失去本次记录的恢复/裁剪联动。
		var histID int64
		if hs := a.histSnapshot(); hs != nil {
			id, serr := hs.SaveScan(cfg, groups, failed)
			if serr != nil {
				// M7：本行曾是全仓唯一"双通道留痕"的写法，其余四处只写 stderr。
				// 现在统一走 warnLedger（本函数即由此抽出）。
				a.warnLedger(fmt.Sprintf("扫描历史保存失败（不影响当前结果）：%v", serr))
			} else {
				histID = id
			}
		}
		a.mu.Lock()
		if superseded() {
			a.mu.Unlock()
			fmt.Fprintf(os.Stderr, "[scan] 第 %d 代扫描已被新任务取代，收尾结果弃写\n", myGen)
			return
		}
		a.resultsReady = true
		a.curHistID = histID
		// M6-P4 / M21 / M62+M85：五个"按轮计数"口径在与 superseded() 判定同一临界区内取。
		// 为什么不能留到锁外的 emit 里现取：Run 一开始就把按轮计数器归零，
		// 而 a.scanInFlight 在"结果集写回"那一拍就复位（紧接其后的落库/认领这段全程为 false，
		// 新扫描因此可通过在途检查，只靠 resultGen 判取代）。锁内取数等于把结论钉死成"未被取代 ⇒
		// 没有新的 StartScan ⇒ 没有新一轮 Run ⇒ 这几个值仍是本轮的"。
		// ★ F1③：这里原写的是"在扫描体第一行就复位"——那个位置没有复位语句，
		//   真复位点在上面的 `a.scanInFlight = false`（与结果集写回同一个临界区）。
		pDirs := a.pipe.ProtectedDirs()
		pFiles := a.pipe.ProtectedFiles()
		cloudSkipped := a.pipe.CloudSkipped()
		wtSkipped := a.pipe.WorkTempSkipped()
		caseUnproven := a.pipe.CaseProbeUnproven()
		unprot := a.pipe.UnprotectedRoots()
		a.mu.Unlock()
		a.emit(a.ctx, "scan:done", ScanSummary{
			Groups:               len(groups),
			Reclaimable:          reclaim,
			ReclaimableActual:    reclaimActual,
			FilesFailed:          len(failed),
			Elapsed:              elapsed.String(),
			ProtectedDirs:        pDirs,
			ProtectedFiles:       pFiles,
			SkippedCloudFiles:    cloudSkipped,
			SkippedWorkTempFiles: wtSkipped,
			CaseProbeUnproven:    caseUnproven,
			UnprotectedRoots:     unprot,
		})
	})
	return taskID, nil
}

// PauseScan 暂停（运行态生效）。P3：无任务时返回错误，前端据此提示而非误显示"已暂停"。
func (a *App) PauseScan() error { return a.pipe.Pause() }

// ResumeScan 恢复。
func (a *App) ResumeScan() error { return a.pipe.Resume() }

// CancelScan 取消。
func (a *App) CancelScan() error { return a.pipe.Cancel() }

// CancelOperation 取消进行中的清理操作（P2）。
// 语义：停止派发后续条目，已完成的部分保持完成（不可回滚），
// 未派发的条目计入 OpsResult.Cancelled 且不会从结果集中移除。
// 没有这个出口时，一次卡住的网络卷/回收站调用会让 opsRunning 永不复位，
// 之后所有清理与保留策略都返回「操作执行中」，只能重启应用。
func (a *App) CancelOperation() error {
	a.mu.Lock()
	cancel := a.opsCancel
	a.mu.Unlock()
	if cancel == nil {
		return fmt.Errorf("当前没有进行中的清理操作")
	}
	cancel()
	return nil
}

// GetScanProgress 主动拉取当前进度（断线重连语义）。
func (a *App) GetScanProgress() model.ProgressEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastEvs
}

// GetStatus 当前任务状态字符串。
func (a *App) GetStatus() string { return string(a.pipe.Status()) }
