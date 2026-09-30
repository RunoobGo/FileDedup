// app_ops.go —— 清理执行与回撤。
//
// M336（2026-09-28 第七轮审查批）：由 app.go 按职责簇拆分而来。
// **方法名与签名一字未改**——Wails 绑定按方法名解析，与所在文件无关，
// 因此这是零行为改动的纯移动：前后端契约、调用方、既有测试都不受影响。
//
// 包级类型/常量/变量与非方法函数仍留在 app.go：它们被多簇共用，
// 拆开只会让「这个类型在哪」变成第二次查找。
package main

import (
	"context"
	"filededup/internal/history"
	"filededup/internal/model"
	"filededup/internal/ops"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// ExecuteOperation 操作执行器（M3-T02~T06）：
// 校验 → 执行（回收站/永久删除/移动/硬链接）→ 事件反馈 → 结果集清理。
// 互斥：执行期间拒绝再次操作与再次扫描（两组 goroutine 并发写结果集会互相覆盖）。
func (a *App) ExecuteOperation(op model.OpRequest) (string, error) {
	// ★ R2-3（2026-09-23 第四轮全仓审查，设计段 §30.5）：卷语义探测要**往用户目录写探测
	// 文件**，是 I/O，必须落在 opsRunning 置位**之前**。这是这一族的第三处漏网——
	// 预览（AS-R3，:1372）与保留策略（APP-1，:1448）都已是"锁外预热、锁内纯比较"，
	// 而下面的 ApplyProcessPolicy 改前就地实测。它卡住（死挂载）不会锁住 a.mu，
	// 却会把 opsRunning 永久钉成"清理操作执行中"：resetOps 在那条永不返回的 I/O 之后，
	// 此后每次清理（:1823）与每次新扫描（:611）都被拒，CancelOperation 也解不了。
	var resolve ops.SensResolver
	if all := warmDirs(op.ProcessDirs, op.ExcludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}
	// P1-1：快照读取与 opsRunning 置位在同一个临界区内完成。
	// 修正前分了两段（读快照 → 释放锁 → 查状态 → 再取锁双检置位），
	// 中间窗口里新扫描可以启动并清空结果集，本操作的 goroutine 随后
	// 会用「基于陈旧结果集清理后的列表」覆盖新扫描结果 → 新结果静默丢失。
	// StartScan 同样在锁内检查 opsRunning，二者互斥因此是双向闭合的。
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("上一个清理操作仍在执行中")
	}
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("扫描进行中，请等待结束后再执行清理")
	}
	// M359（第八轮批 2）：维护动作在途时不受理清理。清缓存期间落进来的清理会与
	// "快照拍到半张表"同时发生；清空清理记录期间落进来的清理更重——它先写好写前账本，
	// 随后那次整表删除把账抽走，文件已经动了而回撤无从进行（M364）。
	if a.maintaining != "" {
		m := a.maintaining
		a.mu.Unlock()
		return "", fmt.Errorf("%s进行中，请等待完成后再执行清理", m)
	}
	// v0.5.0：门槛由「pipe 状态 Done」改为「结果集就绪」。历史恢复出的结果集
	// 引擎处于 Idle，旧判定会永久拒绝清理；resultsReady 在扫描完成/历史载入
	// 时置真，StartScan 入口置假，语义与旧判定在扫描路径上完全等价。
	if !a.resultsReady {
		a.mu.Unlock()
		return "", fmt.Errorf("暂无可操作的结果集（请先完成扫描或打开历史记录）")
	}
	if len(a.groups) == 0 {
		a.mu.Unlock()
		return "", fmt.Errorf("暂无结果集")
	}
	// H5：move 的目标是前端传入的字符串——只允许落在本会话经原生对话框
	// 选择过的目录（或其子目录）内，堵住"绑定层写任意路径"的越权面。
	//
	// M204（2026-09-24 裁定）：这一次校验管不到派发窗口——放行之后才是逐文件校验
	// 与串行搬移，窗口内目标被换成指向别处的链接就会写越界。故在**同一把锁内**
	// 取授权集快照，交给执行器逐条目复审（见 ops.Options.MoveLandingAllowed）。
	// 取快照而不是让执行器读 a.authDirs：① 执行器在另一条 goroutine 上，读活表是
	// 数据竞争；② 用户中途又能选目录会让授权集漂，"这一批凭什么被放行"必须有唯一时刻。
	var moveLanding func(string) error
	if op.Kind == "move" {
		if !a.moveTargetAllowed(op.TargetDir) {
			a.mu.Unlock()
			return "", fmt.Errorf("移动目标无效或未经选择目录对话框授权，请重新选择目标目录")
		}
		moveLanding = a.landingGuard(a.authSnapshotLocked())
	}
	// M61（04 §6.11 APP-13）：S4「永久删除须显式确认」原先只在执行器里
	//（internal/ops/executor.go 的 delete 分支），而本函数的写前账本 beginJournal
	// 在它**上游**。delete 走 undoable=false 那一档，beginJournal 不会因账本问题
	// 拒绝 ⇒ 未确认的删除照样先在 op_records 落一条，执行器整批拒掉后再由
	// FinalizeOp 收成一个"什么都没动"的记录，最后仍走完 ops:done 横幅。
	// 用户从历史页读到的是"做过一次删除"。
	// 现在把这道判断前移到置 opsRunning 之前：拒绝时既不落账、不派发、也不占互斥。
	// 执行器那道**照旧保留** —— Execute 是导出 API，任何调用方都可能绕过 app 层
	// 直接进（既有钉子 TestS4DeleteRequiresConfirm 钉的正是那道）。
	if op.Kind == "delete" && !op.ConfirmDanger {
		a.mu.Unlock()
		return "", fmt.Errorf("永久删除需要显式确认（ConfirmDanger），已拒绝且未写入历史记录")
	}
	// 过滤器收窄（2026-09-20 白名单 / 2026-09-23 功能 2 黑名单）：
	// 实际处理范围 = FileIDs ∩ 优先文件夹 −（若启用）不处理目录。
	//
	// ★ 判据收进 pendingIDsLocked（功能 2 起）：此前这里自带一段
	// ApplyProcessPolicyWith 求交，与预览/清单是三份相似代码——AS-H6 的
	// 温床形状。现在执行/预览/清单同一内核，"将处理 N"与实际处理 N 不可能漂移。
	//
	// 为什么在**这里**取交集，而不是下沉到 ops.Execute：
	//   ① ops.Execute 是纯执行器，不该感知"处理策略"这个业务概念；
	//   ② 它在 len(FileIDs)==0 时会早退——若把过滤下沉，勾选全部落空时
	//      会**静默执行 0 个文件**，用户以为做了其实什么都没做。这正是要避免的。
	//   ③ 写前账本（beginJournal → planOpItems）也用 op.FileIDs，必须在此之前
	//      收窄，否则账本记录的范围超出实际执行范围，账本失真。
	//
	// 为什么挪进锁内（相对 2026-09-20 的位置）：pendingIDsLocked 要求持 a.mu；
	// 且拒绝路径因此落在 opsRunning 置位**之前**——不再有"先占闸再撤闸"的
	// 复位舞蹈，P2 死锁形状从结构上不存在了。resolve 是入口那次锁外预热的
	// 查表版（R2-3）：预热覆盖 dirs+excludeDirs 同一批目录，表必命中，
	// 锁内这一段没有任何写盘 I/O。
	//
	// op 是值传递：op.FileIDs = kept 只改本地副本，前端勾选状态不受影响。
	filtered := len(op.ProcessDirs) > 0 || len(op.ExcludeDirs) > 0
	var filteredMatched, filteredSelected int
	var filteredUnmatched []string
	if filtered {
		filteredSelected = len(op.FileIDs) // 过滤前的勾选数，用于事件与拒绝文案
		kept, _, um := a.pendingIDsLocked(op.ProcessDirs, op.ExcludeDirs, resolve, op.FileIDs)
		if len(kept) == 0 {
			// 交集为空必须**明确拒绝**，不能静默执行 0 项。此刻还没置
			// opsRunning、没建 opCtx——直接放锁返回即可，无需任何复位。
			a.mu.Unlock()
			return "", fmt.Errorf("%s", emptyIntersectionMsg(
				filteredSelected, len(um), len(op.ProcessDirs) > 0, len(op.ExcludeDirs) > 0))
		}
		op.FileIDs = kept
		filteredMatched = len(kept)
		filteredUnmatched = um
	}
	groups := a.groups
	keepIDs := a.keepIDs
	failed := a.failed
	hs := a.hist // 锁内快照：goroutine 内不得再解引用可变字段
	histID := a.curHistID
	a.opsRunning = true // 置位后新扫描会被拒（StartScan 检 opsRunning）
	opCtx, cancelOp := context.WithCancel(context.Background())
	a.opsCancel = cancelOp
	a.mu.Unlock()

	opID := fmt.Sprintf("ops-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))

	// 收窄已在锁内完成（见上）。通知前端本次过滤的实际范围——用事件而不是
	// 改返回值：ExecuteOperation 的返回值是 opID，改签名会波及 Wails 绑定、
	// TS 签名与一批后端测试。事件在派发前发出，前端据此在执行完成横幅里
	// 说明"已选 M 项中 K 项未处理"。
	if filtered {
		if a.emit != nil && a.ctx != nil {
			a.emit(a.ctx, "ops:filtered", map[string]any{
				"matched":   filteredMatched,
				"selected":  filteredSelected,
				"unmatched": filteredUnmatched,
			})
		}
	}

	// 2026-09-18 审查 C3：写前账本改为 fail-closed。原先 BeginOp 失败只发一条事件、
	// journalID 保持 0 后照常移动文件（hist 为 nil 时更是零提示），与手册
	// 09 §6.7「动手之前先把完整计划写入账本」的承诺相反——用户按界面预期
	// 可回撤，实际账本上什么都没有。此处在派发前落盘计划，失败即拒绝执行。
	// 放在锁外是因为 hist 访问绝不与 a.mu 嵌套（见 App.hist 注释）；
	// opsRunning 已在锁内置位，新扫描与新操作在此期间都被拒。
	// resetOps 复位在途标志并释放取消函数。M8：它在下面被调用**两次**——
	// 一次在终止事件之前，一次留给 goTask 的 defer。
	// defer 那一次一定晚于 body 内的 emit（`defer reset()` 注册在 `body()` 之前），
	// 于是前端在 ops:done 回调里立刻 StartScan / ApplyKeepPolicy 会被
	// 「清理操作执行中」误拒。与扫描路径的 resetInFlight 同构（见 :496 注释）。
	// 保留 defer 那份是 panic 展开路径的兜底；两次调用均为幂等。
	resetOps := func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}
	journalID, jerr := a.beginJournal(hs, histID, op, groups, keepIDs)
	if jerr != nil {
		resetOps()
		return "", jerr
	}

	a.goTask("ops", resetOps, func() {
		res := opsExecuteFn(ops.Options{
			Ctx:     opCtx,
			Groups:  groups,
			KeepIDs: keepIDs,
			// 平台能力声明：Windows 的回收站实现会独立复核落位，
			// 故"源消失但无落点"必须按失败上报（见 ops.TrashVerifiesRecycle）。
			TrashVerifiesRecycle: ops.TrashVerifiesRecycle(),
			// M204：move 腿每个条目在实际落点上复审一次授权（非 move 操作恒 nil）。
			MoveLandingAllowed: moveLanding,
			OnProgress: func(done, total int, current string) {
				a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: done, Total: total, Current: current})
			},
			// v0.5.0：逐条收口写前账本（OnItem 由执行器保证每个进入
			// 校验/执行阶段的文件恰好一次；取消未派发项留给 Finalize 归位）。
			OnItem: func(r ops.ItemResult) {
				if journalID == 0 {
					return
				}
				if err := hs.FinishItem(journalID, r.OrigPath, r.DestPath, r.LinkSrc, r.State, r.Err); err != nil {
					a.warnLedger(fmt.Sprintf("条目收口失败 %s：%v，该条在历史记录中可能仍是「计划中」，回撤入口不完整", r.OrigPath, err))
				}
			},
			// C7：worker 内 panic 兜底。runIndexed 已捕获 panic 并标记失败，
			// 此处把异常转成 ops:error 事件，使前端能复位 opsRunning（P2 死锁终防）。
			OnPanic: func(err error) {
				if a.emit != nil && a.ctx != nil {
					a.emit(a.ctx, "ops:error", errorEvent(err)) // M202：同上（C7 捕获的 worker panic）
				}
			},
		}, op)
		if journalID != 0 {
			// 残留 planned（取消未派发等）→ cancelled，冻结 done 计数；
			// 回收字节直接落执行器已按 kind 算好的 res.Reclaimed（B6 / R-操作-3：
			// 库内不再 SUM(size) 重算，避免 trash/链接类/同卷 move 记成假账）。
			if err := hs.FinalizeOp(journalID, res.Reclaimed); err != nil {
				a.warnLedger(fmt.Sprintf("操作账本收尾失败：%v，残留的「计划中」条目未归位，回撤范围以历史记录为准", err))
			}
		}
		// 失败并入统一失败清单；跳过按 S8 裁定（docs/04:569：计 Skipped、不算失败、
		// 不计空间）**只作"从结果集移除"**，不入清单——清单是"本该处理却没处理"的集合，
		// 跳过是"按规则本就不处理"。
		// ★M323（APP-37）：这行注释原先写的是"失败/跳过并入统一失败清单"，
		//   代码从来只并 res.Failed（下面那行），是注释说错了。
		a.mu.Lock()
		a.failed = append(append([]model.FailedItem{}, failed...), res.Failed...)
		// 结果集清理：OK/Skipped 的文件移出组，组 <2 则移除组
		// （OK 记录源路径：move 成功后源路径即结果集应清理的条目）
		gone := make(map[string]bool, len(res.OK)+len(res.Skipped))
		for _, p := range append(append([]string{}, res.OK...), res.Skipped...) {
			gone[p] = true
		}
		var kept []*model.DuplicateGroup
		for _, g := range groups {
			files := g.Files[:0]
			for _, f := range g.Files {
				if gone[f.Path] {
					delete(a.byID, f.ID)
				} else {
					files = append(files, f)
				}
			}
			if len(files) >= 2 {
				g.Files = files
				g.Reclaimable = (uint64(len(files)) - 1) * files[0].Size
				// 实占必须同步重算：不清零/不沿用旧值的话，清理掉一半成员后
				// 结果页仍显示清理前的可释放量（同一类"数字比盘上多"）。
				g.ReclaimableActual = model.ReclaimActual(files)
				kept = append(kept, g)
			}
		}
		a.groups = kept
		a.invalidateViewCacheLocked() // Y3：清理后组结构变化，排序缓存失效
		a.mu.Unlock()
		// v0.5.0：历史联动裁剪（锁外）。gone 为本 goroutine 私有 map，
		// 解锁后无并发写；历史行被删（curHistID 已清零）时 Prune 报"不存在"，
		// 属可忽略的陈旧关联，留痕即可。
		if hs != nil && histID != 0 && len(gone) > 0 {
			if perr := hs.PruneScanFiles(histID, gone); perr != nil {
				// APP-3（2026-09-21 全量审查）：原先只写 stderr。M7 已裁定过
				// "打包 GUI 没有控制台，只写 stderr 等于没写"，本处是同族漏网。
				// 后果不是崩溃而是账本与盘上不一致：历史行仍列着已删文件，
				// 下次从历史页恢复会得到一批不存在的路径。
				a.warnLedger(fmt.Sprintf("历史裁剪失败（记录 %d）：%v，该条历史仍可能列出已清理的文件，从历史页恢复前请先重扫", histID, perr))
			}
		}
		// M8：终止事件必须在复位之后发。结果集回写已完成（上面解锁那一刻起
		// 本 goroutine 不再改写 a.groups），此后任何新扫描/新操作都与它无关。
		resetOps()
		a.emit(a.ctx, "ops:done", res)
	})
	return opID, nil
}

// UndoOperation 回撤一条清理记录：入口同步校验（历史库/互斥/记录可撤性），
// 通过后异步逐项执行，进度复用 ops:progress，终止发 ops:undo:done。
// 处理 state=done 与 state=undo_failed 的条目——失败/跳过/取消项本就没动过
// 文件系统，不在此列；而回撤失败的项确实动过、只是没撤成，必须留在批量范围里
// 供用户修正后重试（APP-4；原先只收 done，把文档承诺的那条路堵死了）。
// done 项撤完转 undone，天然幂等。结果集不动：恢复的文件需要重新扫描确认状态。
func (a *App) UndoOperation(opLogID int64) (string, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("清理/回撤操作执行中，请稍候")
	}
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("扫描进行中，请等待结束后再回撤")
	}
	hs := a.hist // 锁内快照：goroutine 内不得再解引用可变字段
	if hs == nil {
		a.mu.Unlock()
		return "", fmt.Errorf("历史库不可用")
	}
	a.opsRunning = true
	opCtx, cancelOp := context.WithCancel(context.Background())
	a.opsCancel = cancelOp
	a.mu.Unlock()

	release := func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}

	meta, items, err := hs.GetOp(opLogID)
	if err != nil {
		release()
		return "", shellRPCError(err) // M367：回撤腿同样不许把驱动原话直接端给界面
	}
	if !meta.Undoable {
		release()
		return "", fmt.Errorf("%s", undoReasonCode(meta.Kind))
	}

	undoID := fmt.Sprintf("undo-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))
	a.goTask("ops", release, func() {
		var todo []history.OpItem
		for _, it := range items {
			// APP-4（2026-09-21 全量审查）：undo_failed 也要收。函数文档一直写着
			// "回撤失败的项保持 undo_failed，用户可修正后再次回撤"，单项通道也确实
			// 放行 done || undo_failed，只有这里把失败项永久排除在批量之外——
			// 于是"修好问题再点一次全部回撤"一个条目都不动。
			// undone / undoing / 未执行态仍然排除：那三类要么已撤成、要么没动过文件。
			if it.State == history.StateDone || it.State == history.StateUndoFailed {
				todo = append(todo, it)
			}
		}
		res := UndoResult{OpID: opLogID, Restored: []string{}, Failed: []model.FailedItem{}}
		total := len(todo)
		for i, it := range todo {
			if opCtx.Err() != nil {
				break // 剩余项保持 done，可再次回撤
			}
			a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: i, Total: total, Current: it.OrigPath})
			restored, uerr := a.undoExecuteItem(hs, meta.Kind, it)
			if uerr != nil {
				res.Failed = append(res.Failed, undoFailure(it, restored, uerr))
				continue
			}
			res.OK++
			res.Restored = append(res.Restored, restored)
		}
		a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: total, Total: total, Current: ""})
		// M8：同 ExecuteOperation——终止事件前必须先复位（release 也留给
		// goTask 的 defer 兜 panic 路径，两次调用幂等）。
		release()
		a.emit(a.ctx, "ops:undo:done", res)
	})
	return undoID, nil
}

// undoExecuteItem 执行单条回撤并落账（批量/单项共用）。写前落账（2026-09-18
// 审查 I6）：先把条目置为 undoing 再动文件系统——原先先移动后落账，中途被杀会让
// 账本永久停在 done，文件其实已回家却显示"未回撤"，重试还必报"已不存在"。
// 收口：成功置 undone，失败置 undo_failed 并保留原因供排查与重试；
// 写前落账失败则拒绝执行（与 C3「无账本不动文件」同口径），调用方据 error 汇总成败。
func (a *App) undoExecuteItem(hs *history.Store, kind string, it history.OpItem) (string, error) {
	if err := hs.MarkItemUndo(it.ID, history.StateUndoing, ""); err != nil {
		return "", fmt.Errorf("回撤写前落账失败，已放弃执行（文件系统未改动）: %w", err)
	}
	var restored string
	var uerr error
	if kind == "trash" && it.DestPath == "" {
		// darwin 旧版/映射失败时条目没有回收站落点，无法定位
		uerr = fmt.Errorf("无法定位回收站位置，请打开系统回收站手动还原")
	} else {
		restored, uerr = undoOneFn(ops.UndoItem{
			Kind: kind, OrigPath: it.OrigPath, DestPath: it.DestPath,
			LinkSrc: it.LinkSrc, Hash: it.Hash, Size: it.Size, MtimeNs: it.MtimeNs,
		})
	}
	if uerr != nil {
		if merr := hs.MarkItemUndo(it.ID, history.StateUndoFailed, uerr.Error()); merr != nil {
			a.warnLedger(fmt.Sprintf("回撤失败态落库出错 %s：%v，该条可能停留在「回撤中」，重试前请核对文件实际状态", it.OrigPath, merr))
		}
		// M86（04 §6.11 OPS-14b）：restored 非空 = 数据已经回到盘上、只是收尾动作失败
		//（两份并存那一类）。改前这里 `return "", uerr` 把落点就地丢掉，调用方只剩
		// OrigPath 可报，用户不知道文件现在在哪。结构上保留，展示走 FailedItem.Err。
		return restored, uerr
	}
	if merr := hs.MarkItemUndo(it.ID, history.StateUndone, ""); merr != nil {
		a.warnLedger(fmt.Sprintf("回撤成功态落库出错 %s：%v，文件已还原但记录未更新，历史页可能仍显示为可回撤", it.OrigPath, merr))
	}
	return restored, nil
}

// UndoOperationItem 回撤记录中的单个条目（全部回撤之外的增量通道）：
// done 与 undo_failed（修正后重试）可撤；互斥/可撤性/条目状态入口同步校验，
// 受理后异步执行，终止事件与批量回撤同构（ops:undo:done）。
// 与批量口径一致：已回撤项不重复处理（同步拒绝），结果集不动。
func (a *App) UndoOperationItem(opLogID, itemID int64) (string, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("清理/回撤操作执行中，请稍候")
	}
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("扫描进行中，请等待结束后再回撤")
	}
	hs := a.hist // 锁内快照：goroutine 内不得再解引用可变字段
	if hs == nil {
		a.mu.Unlock()
		return "", fmt.Errorf("历史库不可用")
	}
	a.opsRunning = true
	opCtx, cancelOp := context.WithCancel(context.Background())
	a.opsCancel = cancelOp
	a.mu.Unlock()

	release := func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}

	meta, items, err := hs.GetOp(opLogID)
	if err != nil {
		release()
		return "", shellRPCError(err) // M367：回撤腿同样不许把驱动原话直接端给界面
	}
	if !meta.Undoable {
		release()
		return "", fmt.Errorf("%s", undoReasonCode(meta.Kind))
	}
	var target *history.OpItem
	for i := range items {
		if items[i].ID == itemID {
			target = &items[i]
			break
		}
	}
	if target == nil {
		release()
		return "", fmt.Errorf("该记录中不存在此条目（itemID=%d）", itemID)
	}
	if target.State != history.StateDone && target.State != history.StateUndoFailed {
		release()
		return "", fmt.Errorf("该项不可回撤（当前状态：%s）", target.State)
	}
	it := *target

	undoID := fmt.Sprintf("undo-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))
	a.goTask("ops", release, func() {
		res := UndoResult{OpID: opLogID, Restored: []string{}, Failed: []model.FailedItem{}}
		if opCtx.Err() == nil {
			restored, uerr := a.undoExecuteItem(hs, meta.Kind, it)
			if uerr != nil {
				res.Failed = append(res.Failed, undoFailure(it, restored, uerr))
			} else {
				res.OK++
				res.Restored = append(res.Restored, restored)
			}
		}
		// M8：单项回撤与批量回撤同构——终止事件前必须先复位。
		release()
		a.emit(a.ctx, "ops:undo:done", res)
	})
	return undoID, nil
}

// OpenTrash 打开系统回收站（M3-T06：恢复引导）。
func (a *App) OpenTrash() error {
	// M58：三平台共用一句失败文案与同一条出口。回收站打不开时用户正需要它
	//（清理完想找回东西），静默等于把恢复引导变成"点了没反应"。
	// M288：Windows 那一臂走的是 explorer，退出码不作判据，由同一出口分流。
	// ★ 执行缝从裸 startCmd 换成 execRevealCmd：生产值恒等（默认就是 startCmd），
	//   差别只在于本条从此可测——改前它没有任何用例，因为用例一跑就真的弹出回收站窗口。
	var cmd *exec.Cmd
	onExit := func(werr error) {
		a.warnRevealExit(runtime.GOOS, cmd, "trash", "打开系统回收站失败", werr)
	}
	switch runtime.GOOS {
	case "darwin":
		// M368（§3.2b）：主目录拿不到就**一条命令都不发**。改前是 `home, _ :=` 忽略错误后
		// filepath.Join ⇒ 拼出相对当前工作目录的 `.Trash`，命令"成功启动"而什么都没打开，
		// 用户此刻正是清理完想找回东西的那个人。形状照 app_lifecycle.go 的 herr==nil 那一支。
		home, herr := os.UserHomeDir()
		if herr != nil {
			return fmt.Errorf("拿不到用户主目录，无法打开系统回收站: %w", herr)
		}
		cmd = exec.Command("open", filepath.Join(home, ".Trash"))
	case "windows":
		cmd = exec.Command("explorer", "shell:RecycleBinFolder")
	default:
		root := os.Getenv("XDG_DATA_HOME")
		if root == "" {
			home, herr := os.UserHomeDir()
			if herr != nil {
				return fmt.Errorf("拿不到用户主目录，无法打开系统回收站: %w", herr)
			}
			root = filepath.Join(home, ".local", "share")
		}
		cmd = exec.Command("xdg-open", filepath.Join(root, "Trash", "files"))
	}
	return execRevealCmd(a, cmd, onExit)
}
