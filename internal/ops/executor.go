package ops

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// opWorkers 独立文件操作的并发上限（Y4）：delete/hardlink/回收站回退均为
// 单文件元数据 syscall（延迟主导），有界并发显著缩短大批量耗时；
// 上限保守，避免机械盘/网络盘上的寻道抖动。
const opWorkers = 4

// runIndexed 有界并发执行 n 个相互独立的任务；结果按下标写入，
// 由调用方按序汇总，从而在并发的同时保证输出顺序确定。
// ctx 取消后不再派发新任务（已在途的单次 syscall 不可抢占），剩余下标保持
// 未置位，由调用方汇总为「已取消」。
// onPanic 为可选兜底（C7）：fn 内的 panic（如 trash/VerifyFile/HardlinkMerge
// 遇畸形输入）会被对应 worker 捕获并回调，避免进程崩溃、操作互斥标记卡死
// （P2 死锁的终防）。未提供时等价于不拦截——panic 仍穿透进程，便于测试暴露意外。
func runIndexed(ctx context.Context, n, workers int, fn func(i int), onPanic ...func(i int, r any)) {
	if n <= 0 {
		return
	}
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	var next atomic.Int64
	guard := func(i int) {
		if len(onPanic) == 0 || onPanic[0] == nil {
			return
		}
		if r := recover(); r != nil {
			onPanic[0](i, r)
		}
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				func() {
					defer guard(i)
					fn(i)
				}()
			}
		}()
	}
	wg.Wait()
}

// ItemResult 单个文件走完校验/执行阶段的终态（v0.5.0 写前日志收口用）。
// State ∈ done / failed / skipped；DestPath 为 trash/move 的实际去向
// （回收站映射缺失时为空），LinkSrc 为 hardlink 指向的保留源。
type ItemResult struct {
	OrigPath string
	DestPath string
	LinkSrc  string
	State    string
	Err      string
}

// Options 执行器配置。
type Options struct {
	Groups     []*model.DuplicateGroup // 当前结果集（含组哈希）
	KeepIDs    map[uint64]bool         // 各组保留文件 ID（S2：不可操作）
	Pool       *hasher.Pool
	TrashFn    func(paths []string) (map[string]string, error) // 回收站（可注入，默认平台实现）
	OnProgress func(done, total int, current string)

	// OnItem 每个进入校验/执行阶段的文件恰好回调一次（nil 安全）。
	// 保留项拒绝与结果集外 id 不回调——它们不在写前日志的计划里；
	// 取消后未派发项不回调，由上层 Finalize 统一归为 cancelled。
	OnItem func(ItemResult)

	// OnPanic C7 兜底：worker 内 panic 时回调（由调用方发 ops:error 并复位在途标志）。
	// 不提供则 runIndexed 的 panic 守卫退化为不拦截（保留默认崩溃行为，便于测试暴露缺陷）。
	OnPanic func(err error)

	// Ctx 取消信号（P2）：修正前 Execute 无法中止——任何一次卡住
	// （回收站服务无响应、网络卷挂起）都会让 app 层的操作互斥标记永不复位，
	// 此后可清理与保留策略永久返回「操作执行中」，只能重启应用。
	// 注意：已在途的单个 syscall 无法抢占，取消保证的是「不再派发新条目」，
	// 配合 app 层的代际校验共同消除永久锁死。
	Ctx context.Context
}

// Execute 执行清理操作（trash/delete/move/hardlink），返回聚合结果。
// 安全语义（01 §9 / 02 决策 7）：
//   - 保留项拒绝执行（S2）；delete 必须 ConfirmDanger（S4）
//   - 操作前逐文件校验（S1 篡改拦截 / S8 ENOENT→Skipped）
//   - 单文件失败不中断整体（S7），逐项计入清单
//   - Execute 须由上层串行调用（app 层单任务约束）
func Execute(opts Options, op model.OpRequest) model.OpsResult {
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	res := model.OpsResult{
		OK: []string{}, Failed: []model.FailedItem{}, Skipped: []string{}, Cancelled: []string{},
	}
	if len(op.FileIDs) == 0 {
		return res
	}

	// 定位：文件ID → 条目 / 组哈希 / 组内保留源（hardlink 用）
	byID := make(map[uint64]*model.FileEntry, len(op.FileIDs)*2)
	hashByID := make(map[uint64][32]byte, len(op.FileIDs)*2)
	keepSrcByID := make(map[uint64]*model.FileEntry, len(op.FileIDs)*2) // 冗余ID → 组保留者
	for _, g := range opts.Groups {
		var keep *model.FileEntry
		for _, f := range g.Files {
			if opts.KeepIDs != nil && opts.KeepIDs[f.ID] {
				keep = f
				break
			}
		}
		if keep == nil {
			keep = g.Files[pickShortest(g)] // 未决策时默认路径最短（01 §9）
		}
		for _, f := range g.Files {
			byID[f.ID] = f
			hashByID[f.ID] = g.Hash
			keepSrcByID[f.ID] = keep
		}
	}

	// delete 强制显式确认（S4：后端是最后防线）
	if op.Kind == "delete" && !op.ConfirmDanger {
		res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: "永久删除需要显式确认（ConfirmDanger）"})
		return res
	}

	pool := opts.Pool
	if pool == nil {
		pool = hasher.NewPool()
	}
	trash := opts.TrashFn
	if trash == nil {
		trash = Trash
	}

	total := len(op.FileIDs)
	var progDone atomic.Int64
	report := func(path string) {
		n := int(progDone.Add(1))
		if opts.OnProgress != nil {
			opts.OnProgress(n, total, path)
		}
	}
	emitItem := func(r ItemResult) {
		if opts.OnItem != nil {
			opts.OnItem(r)
		}
	}

	// 校验阶段（S1/S8）
	// H2：procIDs 与 toProcess 下标平行，记录「通过校验那一刻」的文件身份；
	// 各执行分支在破坏性动作前用 identityStill 复核路径未被换成另一 inode。
	var toProcess []*model.FileEntry
	var procIDs []fsid.ID
	for _, fid := range op.FileIDs {
		e, ok := byID[fid]
		if !ok {
			res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: fmt.Sprintf("文件不在当前结果集（id=%d）", fid)})
			report("")
			continue
		}
		if opts.KeepIDs != nil && opts.KeepIDs[fid] {
			res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: "保留文件不可操作"})
			report(e.Path)
			continue
		}
		v, vid := VerifyFile(e, hashByID[fid], pool)
		switch v {
		case VerdictSkipped:
			res.Skipped = append(res.Skipped, e.Path) // S8：已消失 = 目标达成
			emitItem(ItemResult{OrigPath: e.Path, State: "skipped"})
			report(e.Path)
		case VerdictFailed:
			res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: "校验失败：文件在扫描后被修改"})
			emitItem(ItemResult{OrigPath: e.Path, State: "failed", Err: "校验失败：文件在扫描后被修改"})
			report(e.Path)
		default:
			toProcess = append(toProcess, e)
			procIDs = append(procIDs, vid)
		}
	}

	// 执行阶段：结果按下标收集（并发安全），末尾按序汇总（输出顺序确定）
	// ocNone 为非零哨兵：未显式置位的下标视为「未处理」而非成功，防误报 OK
	const (
		ocNone = iota
		ocOK
		ocFailed
		ocSkipped
	)
	type outcome struct {
		code    int
		stage   string // 失败阶段（空 = ops，用于保留源校验等 verify 语义）
		err     string
		dst     string // trash/move 实际去向（未知时空）
		linkSrc string // hardlink 指向的保留源
	}
	outcomes := make([]outcome, len(toProcess))

	// C7：worker 内 panic 守卫落地。fn（trash/VerifyFile/HardlinkMerge）panic 会
	// 穿透 goTask 的 recover（仅包主 goroutine）直接崩进程；此处捕获后把该下标
	// 标记为失败，并回调 OnPanic（由调用方发 ops:error）。进程不死、操作互斥标记
	// 仍由最终的 ops:done 正常复位（P2 死锁的最终防线）。
	onPanic := func(i int, r any) {
		if i < 0 || i >= len(toProcess) {
			if opts.OnPanic != nil {
				opts.OnPanic(fmt.Errorf("worker panic（下标越界 %d）: %v", i, r))
			}
			return
		}
		e := toProcess[i]
		outcomes[i] = outcome{code: ocFailed, stage: "ops",
			err: fmt.Sprintf("内部错误（panic 已被捕获，未崩溃）: %v", r)}
		report(e.Path)
		if opts.OnPanic != nil {
			opts.OnPanic(fmt.Errorf("文件 %s 操作 panic: %v", e.Path, r))
		}
	}
	aggregate := func() {
		for i, e := range toProcess {
			switch outcomes[i].code {
			case ocOK:
				res.OK = append(res.OK, e.Path)
				res.Reclaimed += e.Size
				emitItem(ItemResult{OrigPath: e.Path, DestPath: outcomes[i].dst,
					LinkSrc: outcomes[i].linkSrc, State: "done"})
			case ocSkipped:
				res.Skipped = append(res.Skipped, e.Path)
				emitItem(ItemResult{OrigPath: e.Path, State: "skipped"})
			case ocNone:
				// 未派发：取消时是常态（P2），须与真正的内部状态缺失区分开，
				// 否则用户取消一次会得到一堆"内部状态缺失"的误导信息。
				// 取消项不回调：写前日志由上层 Finalize 统一置 cancelled。
				if ctx.Err() != nil {
					res.Cancelled = append(res.Cancelled, e.Path)
				} else {
					res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: "未执行（内部状态缺失）"})
					emitItem(ItemResult{OrigPath: e.Path, State: "failed", Err: "未执行（内部状态缺失）"})
				}
			default:
				stage := outcomes[i].stage
				if stage == "" {
					stage = "ops"
				}
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: stage, Err: outcomes[i].err})
				emitItem(ItemResult{OrigPath: e.Path, State: "failed", Err: outcomes[i].err})
			}
		}
	}

	// 先校验操作类型，未知类型不进入汇总（否则会为每个文件误报状态）
	switch op.Kind {
	case "trash", "delete", "move", "hardlink":
	default:
		res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: fmt.Sprintf("未知操作类型 %q", op.Kind)})
		return res
	}

	switch op.Kind {
	case "trash":
		// H2：入站前复核身份。校验（读内容）与回收站（动路径）之间，
		// 路径可能被换成另一 inode——彼时删的是"第三方文件"，必须拦截。
		usable := make([]int, 0, len(toProcess))
		for i, e := range toProcess {
			if !identityStill(e.Path, procIDs[i]) {
				outcomes[i] = outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"}
				report(e.Path)
				continue
			}
			usable = append(usable, i)
		}
		paths := make([]string, 0, len(usable))
		for _, i := range usable {
			paths = append(paths, toProcess[i].Path)
		}
		// 已取消则整批不派发（outcome 保持 ocNone → aggregate 归入 Cancelled）
		if len(paths) > 0 && ctx.Err() == nil {
			if dstMap, err := trash(paths); err == nil {
				for _, i := range usable {
					outcomes[i].code = ocOK
					outcomes[i].dst = dstMap[toProcess[i].Path]
					report(toProcess[i].Path)
				}
			} else {
				// 批量失败退化为逐文件执行以隔离错误项（S5/S7）；
				// 各文件相互独立 → 有界并发。
				// C6：批量 trash 可能已部分成功（整批返回一个 error，无从得知
				// 哪些已移走）。回退前逐个检查源是否还在——已不在的按 S8 语义
				// 记 Skipped（目标已达成），否则会被记 Failed，但文件实际已在
				// 回收站，结果集却仍显示「存在」，状态不一致。
				runIndexed(ctx, len(usable), opWorkers, func(k int) {
					i := usable[k]
					p := toProcess[i].Path
					if _, err := os.Stat(p); os.IsNotExist(err) {
						outcomes[i].code = ocSkipped
						report(p)
						return
					}
					if m, err := trash([]string{p}); err != nil {
						outcomes[i] = outcome{code: ocFailed, err: err.Error()}
					} else {
						outcomes[i] = outcome{code: ocOK, dst: m[p]}
					}
					report(p)
				}, func(k int, r any) { // 下标经 usable 映射回 toProcess
					onPanic(usable[k], r)
				})
			}
		}

	case "delete":
		runIndexed(ctx, len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
			if !identityStill(e.Path, procIDs[i]) { // H2
				outcomes[i] = outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"}
				report(e.Path)
				return
			}
			if err := os.Remove(e.Path); err != nil {
				if os.IsNotExist(err) {
					outcomes[i].code = ocSkipped
				} else {
					outcomes[i] = outcome{code: ocFailed, err: err.Error()}
				}
			} else {
				outcomes[i].code = ocOK
			}
			report(e.Path)
		}, onPanic)

	case "move":
		// 串行（安全优先）：MoveFile 的目标重名递增（uniqueDst）是「先查后用」，
		// 并发处理同目录同基名文件时两个 worker 会选中同一目标名，后写者覆盖前者
		// → 静默数据丢失。该风险高于并发收益，故此处不做并发（01 §9 安全语义）。
		for i, e := range toProcess {
			if ctx.Err() != nil {
				break // move 串行执行，取消后立即停止派发（P2）
			}
			if !identityStill(e.Path, procIDs[i]) { // H2
				outcomes[i] = outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"}
				report(e.Path)
				continue
			}
			if dst, err := MoveFile(e.Path, op.TargetDir); err != nil {
				outcomes[i] = outcome{code: ocFailed, err: err.Error()}
			} else {
				outcomes[i] = outcome{code: ocOK, dst: dst}
			}
			report(e.Path)
		}

	case "hardlink":
		runIndexed(ctx, len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
			src := keepSrcByID[e.ID]
			if src == nil || src.ID == e.ID {
				outcomes[i] = outcome{code: ocFailed, err: "未找到组内保留源"}
				report(e.Path)
				return
			}
			// H2：dup 在动作前仍须指向校验过的那份内容
			if !identityStill(e.Path, procIDs[i]) {
				outcomes[i] = outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"}
				report(e.Path)
				return
			}
			// S1 扩展：keep 源也须校验——源在扫描后被篡改时硬链接会用新内容
			// 覆盖 dup 的原内容（不可逆），必须拦截。
			// P0-3：校验为内容级（见 VerifyFile），时间戳未变不再放行。
			// H2：源身份一并下传，HardlinkMerge 在建立临时链接后复核其
			// inode 仍是校验时那一个（防 verify→act 窗口内源被替换）。
			// 注：同组多个 dup 共享同一 keep 源，校验为只读操作，并发安全。
			v, srcID := VerifyFile(src, hashByID[src.ID], pool)
			switch v {
			case VerdictSkipped:
				outcomes[i] = outcome{code: ocFailed, stage: "verify", err: "保留源已消失，无法合并（S1 拦截）"}
				report(e.Path)
				return
			case VerdictFailed:
				outcomes[i] = outcome{code: ocFailed, stage: "verify", err: "保留源在扫描后被修改，已拦截（S1）"}
				report(e.Path)
				return
			}
			// HardlinkMerge 使用「dup 路径 + .fdd-tmp」临时名，路径互不冲突 → 并发安全
			if err := HardlinkMerge(src.Path, e.Path, srcID, procIDs[i]); err != nil {
				outcomes[i] = outcome{code: ocFailed, err: err.Error()}
			} else {
				outcomes[i] = outcome{code: ocOK, linkSrc: src.Path}
			}
			report(e.Path)
		}, onPanic)

	default:
		// 已在入口拦截未知类型，此处不可达（防御性保留）
	}

	aggregate()
	return res
}
