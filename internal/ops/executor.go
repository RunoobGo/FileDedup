package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"filededup/internal/ads"
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
// Warn 为「操作已成功、但有需要告知用户的情况」（如临时文件残留未能删除）。
type ItemResult struct {
	OrigPath string
	DestPath string
	LinkSrc  string
	State    string
	Err      string
	Warn     string
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

	// TrashVerifiesRecycle 声明本平台的 trash 实现能否**可靠地验证**
	// "文件确实进了回收站"。
	//
	// 该标志存在的原因（2026-09-20 缺陷：跨盘移入回收站被静默永久删除）：
	//
	// 批量 trash 失败后回退逐文件时，若某个源路径已不存在，代码需要判断
	// 这是"已被批量执行成功移走"还是"被静默永久删除"。两种情况在
	// 文件系统层面**完全同形**（都是 os.Stat 返回 ENOENT），唯一能区分的
	// 依据是平台有没有报告过落点（dstMap）。
	//
	//   true  —— 平台承诺"若没报告落点，就不算成功"。
	//            Windows 走此分支：defaultTrash 内部会复核回收站条目数，
	//            验不过即返回 error，因此"源消失 + 无落点"必须按**失败**上报，
	//            否则会把数据丢失伪装成无害的跳过。
	//   false —— 平台按 C6/S8 旧契约：源消失即视为目标达成，记 Skipped。
	//            darwin 的 Finder 落点解析可能配对失败而返回空 dstMap，
	//            此时文件确实已进回收站，记 Failed 是误报。
	//
	// 默认 false 以保持既有平台行为不变；Windows 由 app 层显式置为 true。
	TrashVerifiesRecycle bool
}

// adsCheck NTFS 备用数据流判据（M6-P3）。抽成包级变量供测试注入假判据：
// 真实的命名流只有 NTFS 造得出来（`echo x > f.txt:note` 在 APFS/exFAT 上不会创建
// 流），而"该不该拦""拦了之后账怎么记"这两件真正可测的事不该留白。
var adsCheck = ads.Check

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
		Warnings: []string{},
	}
	if len(op.FileIDs) == 0 {
		return res
	}

	// 定位：文件ID → 条目 / 组哈希 / 组内保留源（hardlink 用）
	byID := make(map[uint64]*model.FileEntry, len(op.FileIDs)*2)
	hashByID := make(map[uint64][32]byte, len(op.FileIDs)*2)
	keepSrcByID := make(map[uint64]*model.FileEntry, len(op.FileIDs)*2) // 冗余ID → 组保留者
	for _, g := range opts.Groups {
		// 防御：空组没有任何成员，`g.Files[0]` 与 pickShortest 都会越界 panic。
		// 扫描流水线自身不可能产空组（internal/dedup/pipeline.go 输出前有
		// `len(g) < 2 { continue }`），但 **Groups 是调用方传入的**：历史恢复
		// （history.LoadScan 读 hist_groups/hist_files 两张表）在其数据被外部
		// 工具改写或库影像损坏时，完全可能交出一个没有任何 hist_files 行的组。
		// 执行器是"后端最后防线"，不该因为上游给了畸形输入就把整个进程带走
		// （执行发生在 goroutine 里，panic 会连带丢掉整批清理的收尾落账）。
		if len(g.Files) == 0 {
			continue
		}
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
			// M6-P3 备用数据流守卫：内容校验已通过、正要进 toProcess 的那一刻。
			// 位置只有这一处是有意的——五种 op.Kind（trash/delete/move/hardlink/
			// symlink）全部只处理进了 toProcess 的项，而 toProcess 唯一来源就是
			// 上面这道循环，所以一道守卫覆盖全部破坏性动作。
			//
			// 记 Failed 而不是像 M6-P1 那样记 Skipped：语境相反。扫描期的跳过是
			// 引擎替用户省流量（用户没要求操作）；这里用户已经点了清理，这一项
			// **没做成**，必须进失败抽屉。Stage 用 "ads" 而非复用 "verify"，
			// 因为两者的处置建议完全不同（"文件被改过"要重扫，"有备用流"要手工处理）。
			if oc := adsCheck(e.Path); oc.Reject {
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ads", Err: oc.Reason})
				emitItem(ItemResult{OrigPath: e.Path, State: "failed", Err: oc.Reason})
				report(e.Path)
				continue
			}
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
		warn    string // 成功但有需要告知用户的情况（如临时文件残留）
	}
	outcomes := make([]outcome, len(toProcess))
	// C6（2026-09-18 审查）：账本收口必须紧跟每次 syscall，而不是等全批走完
	// 在 aggregate 里统一回调。否则进程在批内被杀/退出时全部条目停在 planned，
	// 重启后归为 interrupted，而回撤只认 done（见 app.go UndoOperation）——
	// 结果是「文件已在回收站、应用内永久无法回撤」。
	// settle 同时完成：写下标结果 + 即时落账 + 进度上报；幂等，
	// 使 panic 守卫可与正常路径安全竞争同一行。
	settled := make([]bool, len(toProcess))
	settle := func(i int, o outcome) {
		if settled[i] {
			return
		}
		settled[i] = true
		outcomes[i] = o
		switch o.code {
		case ocOK:
			emitItem(ItemResult{OrigPath: toProcess[i].Path, DestPath: o.dst,
				LinkSrc: o.linkSrc, State: "done", Warn: o.warn})
		case ocSkipped:
			emitItem(ItemResult{OrigPath: toProcess[i].Path, State: "skipped"})
		case ocFailed:
			emitItem(ItemResult{OrigPath: toProcess[i].Path, State: "failed", Err: o.err})
		}
		report(toProcess[i].Path)
	}

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
		settle(i, outcome{code: ocFailed, stage: "ops",
			err: fmt.Sprintf("内部错误（panic 已被捕获，未崩溃）: %v", r)})
		if opts.OnPanic != nil {
			opts.OnPanic(fmt.Errorf("文件 %s 操作 panic: %v", e.Path, r))
		}
	}
	// aggregate 仅按序汇总 res（输出顺序确定）。落账已由 settle 在每次 syscall
	// 之后即时完成（C6），此处不得再对已 settle 的行 emitItem（会二次收口）。
	aggregate := func() {
		for i, e := range toProcess {
			switch outcomes[i].code {
			case ocOK:
				res.OK = append(res.OK, e.Path)
				// 2026-09-19：硬链接合并**不立即释放空间**，只把数据块变为共享。
				// 修正前此处对 hardlink 也累加 e.Size，UI 于是显示"释放 X"，
				// 而用户去资源管理器一看文件夹占用丝毫未变——与事实不符的假账，
				// 也让"总占用未变化"看起来像操作失败。
				// 现在把硬链接的贡献单列（LinkedBytes），Reclaimed 只统计
				// 真正从磁盘上消失的数据量（trash/delete/move 出卷）。
				//
				// 2026-09-20：软链接同属"链接类"合并——磁盘上少了一份完整数据
				// （dup 位置只剩一个几百字节的链接对象），但保留项的数据块并未
				// 被共享；Reclaimed 记 e.Size 是**符合事实**的（磁盘占用确实少了
				// 一整份文件）。不过为了与硬链接在 UI 上可区分、且在回撤语义上
				// 不误导（回撤要重新占回这块空间），这里同样单列，不混进
				// Reclaimed，让前端能分别表述。
				switch op.Kind {
				case "hardlink":
					res.LinkedBytes += e.Size
				case "symlink":
					res.SymlinkedBytes += e.Size
				default:
					res.Reclaimed += e.Size
				}
				// 成功但有残留等情况：逐条收集，供上层提示（不改变成败判定）
				if outcomes[i].warn != "" {
					res.Warnings = append(res.Warnings,
						fmt.Sprintf("%s：%s", e.Path, outcomes[i].warn))
				}
			case ocSkipped:
				res.Skipped = append(res.Skipped, e.Path)
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
			}
		}
	}

	// 先校验操作类型，未知类型不进入汇总（否则会为每个文件误报状态）
	switch op.Kind {
	case "trash", "delete", "move", "hardlink", "symlink":
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
				settle(i, outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"})
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
					settle(i, outcome{code: ocOK, dst: dstMap[toProcess[i].Path]})
				}
			} else {
				// 批量失败退化为逐文件执行以隔离错误项（S5/S7）；
				// 各文件相互独立 → 有界并发。
				// C6：批量 trash 可能已部分成功（整批返回一个 error，无从得知
				// 哪些已移走）。回退前逐个检查源是否还在——已不在的按 S8 语义
				// 记 Skipped（目标已达成），否则会被记 Failed，但文件实际已在
				// 回收站，结果集却仍显示「存在」，状态不一致。
				// 2026-09-18 审查 C2：darwin/linux 的 trash 实现都是「已完成部分 dstMap +
				// error」一起返回。此前 err 分支整包丢弃 dstMap，回退只能把已
				// 入站文件记成去向为空的 Skipped，而 Skipped 永不进回撤
				// （app.go UndoOperation 只认 done）→ 这批文件永久失去应用内
				// 回撤。故先收下已知去向，命中者按 done+dst 落账。
				//
				// ★ 2026-09-20 修复「跨盘移入回收站被静默永久删除」时的重要修正：
				//
				// 上面那条「源已不在 → 记 Skipped」的推断，在**静默永久删除**的
				// 场景下会变成帮凶：Windows 的 defaultTrash 现在会用 verifyRecycled
				// 检出「文件消失但回收站条目数未增加」并返回错误，而回退逻辑随即
				// 看到 os.IsNotExist(p) 成立，就把这项记成 Skipped——**刚亮起的
				// 数据丢失警报被重新按了下去**，用户看到的仍是一个无害的「跳过」。
				//
				// 判据缺陷在于：os.Stat 只能证明「文件不在了」，无法区分
				// 「进了回收站」与「被永久删除」。因此必须结合两个信息判定：
				//
				//   1. 平台是否**能**验证回收站（opts.TrashVerifiesRecycle）
				//   2. 平台本次是否**报告过**该文件的落点（known[p]）
				//
				// 能验证 + 无落点 → 无法证明进过回收站，按**失败**上报（Windows）
				// 不能验证        → 保持 C6/S8 旧契约，记 Skipped（darwin 等）
				//
				// 把「无法证明成功」升级为失败，是本缺陷的修复要点：宁可让用户
				// 看到一条需要核实的错误，也不能让他以为文件好好地躺在回收站里。
				known := dstMap
				if known == nil {
					known = map[string]string{}
				}
				strict := opts.TrashVerifiesRecycle
				runIndexed(ctx, len(usable), opWorkers, func(k int) {
					i := usable[k]
					p := toProcess[i].Path
					if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
						if d, ok := known[p]; ok {
							settle(i, outcome{code: ocOK, dst: d})
							return
						}
						if !strict {
							settle(i, outcome{code: ocSkipped})
							return
						}
						// 源已消失，但平台有能力验证却未报告任何落点 →
						// 无法证明进了回收站。这正是「疑似被直接删除」的形状，
						// 绝不能记成 Skipped。
						settle(i, outcome{code: ocFailed, stage: "trash",
							err: "文件已从原路径消失，但未能确认其进入回收站；" +
								"可能已被直接删除，请立即到回收站核实" +
								"（若不在回收站，请停止后续操作并考虑用数据恢复工具找回）"})
						return
					}
					if m, err := trash([]string{p}); err != nil {
						settle(i, outcome{code: ocFailed, err: err.Error()})
					} else {
						settle(i, outcome{code: ocOK, dst: m[p]})
					}
				}, func(k int, r any) { // 下标经 usable 映射回 toProcess
					onPanic(usable[k], r)
				})
			}
		}

	case "delete":
		runIndexed(ctx, len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
			if !identityStill(e.Path, procIDs[i]) { // H2
				settle(i, outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"})
				return
			}
			if err := os.Remove(e.Path); err != nil {
				if os.IsNotExist(err) {
					settle(i, outcome{code: ocSkipped})
				} else {
					settle(i, outcome{code: ocFailed, err: err.Error()})
				}
				return
			}
			settle(i, outcome{code: ocOK})
		}, onPanic)

	case "move":
		// 串行。历史上这里的理由是「uniqueDst 先查后用，两个 worker 会挑到同一目标名，
		// 后写者覆盖前者」（M2）；该缺陷已随 claimDst 的原子抢占修掉，串行不再是为了
		// 正确性，只是为了保留逐项取消检查与既有落位顺序。放开并发属独立优化，本批不做。
		for i, e := range toProcess {
			if ctx.Err() != nil {
				break // move 串行执行，取消后立即停止派发（P2）
			}
			if !identityStill(e.Path, procIDs[i]) { // H2
				settle(i, outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"})
				continue
			}
			if dst, err := MoveFile(e.Path, op.TargetDir); err != nil {
				settle(i, outcome{code: ocFailed, err: err.Error()})
			} else {
				settle(i, outcome{code: ocOK, dst: dst})
			}
		}

	case "hardlink":
		runIndexed(ctx, len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
			src := keepSrcByID[e.ID]
			if src == nil || src.ID == e.ID {
				settle(i, outcome{code: ocFailed, err: "未找到组内保留源"})
				return
			}
			// H2：dup 在动作前仍须指向校验过的那份内容
			if !identityStill(e.Path, procIDs[i]) {
				settle(i, outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"})
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
				settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源已消失，无法合并（S1 拦截）"})
				return
			case VerdictFailed:
				settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源在扫描后被修改，已拦截（S1）"})
				return
			}
			// HardlinkMerge 使用「dup 路径 + .fdd-tmp」临时名，路径互不冲突 → 并发安全
			if err := HardlinkMerge(src.Path, e.Path, srcID, procIDs[i]); err != nil {
				// 2026-09-19：区分「真失败」与「已成功但有临时文件残留」。
				// 后者链接已经建好，若计为失败，用户会以为合并没生效，进而
				// 反复重试——而重试时 dup 与 keep 已是同一文件，行为更费解。
				var residue *ResidueError
				if errors.As(err, &residue) {
					settle(i, outcome{code: ocOK, linkSrc: src.Path, warn: residue.Error()})
				} else {
					settle(i, outcome{code: ocFailed, err: err.Error()})
				}
			} else {
				settle(i, outcome{code: ocOK, linkSrc: src.Path})
			}
		}, onPanic)

	case "symlink":
		// 跨卷软链接合并（2026-09-20）。与 hardlink 分支的前置校验**完全一致**
		// ——dup 与 keep 都必须复核身份、源必须重算内容——因为两者的安全前提
		// 是同一个："执行时的对象还是校验时的对象"。
		//
		// 差异只在两点：
		//   ① 不要求同卷（软链接能跨卷，这正是它的存在理由）；
		//   ② 同卷时加一条**非阻断**提示（同卷用硬链接更安全）。
		//
		// 为什么同卷只提示不拒绝：软链接合并本身是安全的（复核齐全），
		// 只是"同卷有更优解"。把用户的选择直接判为失败，是拿应用的偏好
		// 去否决一个正确且无害的操作，代价大于收益。
		//
		// 并发安全性与 hardlink 相同：SymlinkMerge 使用的临时名是
		// 「dup 路径 + .fdd-tmp」，每个 dup 各自独立，互不覆盖。
		runIndexed(ctx, len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
			src := keepSrcByID[e.ID]
			if src == nil || src.ID == e.ID {
				settle(i, outcome{code: ocFailed, err: "未找到组内保留源"})
				return
			}
			// H2：dup 在动作前仍须指向校验过的那份内容
			if !identityStill(e.Path, procIDs[i]) {
				settle(i, outcome{code: ocFailed, stage: "verify",
					err: "文件在扫描后被替换（inode 已变化），已拦截"})
				return
			}
			// S1 扩展：keep 源也须内容级校验（源被篡改时链接会指向被改过的
			// 数据，用户以为"还是原来那份"——同样不可逆，必须拦截）。
			v, srcID := VerifyFile(src, hashByID[src.ID], pool)
			switch v {
			case VerdictSkipped:
				settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源已消失，无法合并（S1 拦截）"})
				return
			case VerdictFailed:
				settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源在扫描后被修改，已拦截（S1）"})
				return
			}
			warn := ""
			if sameVolume(src.Path, e.Path) {
				warn = "这两个文件在同一卷上，使用「硬链接合并」更安全" +
					"（硬链接不需要管理员权限，且删除任一名字数据都还在，不会出现链接失效）"
			}
			if err := SymlinkMerge(src.Path, e.Path, srcID, procIDs[i]); err != nil {
				// 与 hardlink 一致：「已成功但有残留」不应判失败，否则用户
				// 会以为没生效而反复重试（重试时 dup 已是链接，行为更费解）。
				var residue *ResidueError
				if errors.As(err, &residue) {
					settle(i, outcome{code: ocOK, linkSrc: src.Path,
						warn: joinWarn(warn, residue.Error())})
				} else if isSymlinkNeedsPrivilege(err) {
					// 权限不足是**环境级**问题（Windows 未提权且未开开发者模式）：
					// 同一台机器上每个文件都会以同样方式失败。逐条重复长文案
					// 会让结果页被同一句话淹没，故此处只留短标记，
					// 完整指引由 AggregateWarnings 汇总成一条（见本文件尾部）。
					settle(i, outcome{code: ocFailed, stage: symlinkPrivilegeStage, err: err.Error()})
				} else {
					settle(i, outcome{code: ocFailed, err: err.Error()})
				}
			} else {
				settle(i, outcome{code: ocOK, linkSrc: src.Path, warn: warn})
			}
		}, onPanic)

	default:
		// 已在入口拦截未知类型，此处不可达（防御性保留）
	}

	aggregate()
	if op.Kind == "symlink" {
		AggregateWarnings(&res)
	}
	return res
}

// 权限失败的合并提示。为什么不在 worker 里逐条给出完整指引：
// 「未提权 + 未开开发者模式」是**环境级**问题，同一台机器上每个文件都会以
// 同样方式失败。8 个文件就是 8 条一模一样的数百字长文案，结果页被同一句话
// 淹没，用户反而找不到真正的失败原因。
//
// 因此分两层：
//   - 条目级（aggregate 里生成）：短句 + 文件路径，逐条可追；
//   - 汇总级（本函数）：一条完整可操作的指引。
//
// 单独成函数而不是内联，是为了让它可被独立测试，也便于上层（如 app 层）
// 在推送事件前做同样的归一化。
const symlinkPrivilegeSummary = "软链接合并失败：当前程序没有创建符号链接的权限。" +
	"Windows 要求 SeCreateSymbolicLinkPrivilege，普通权限下默认不具备。请任选其一后重试——" +
	"① 右键程序图标选「以管理员身份运行」；" +
	"② 在「设置 → 系统 → 开发者选项」中开启「开发者模式」（开启后普通权限即可创建）；" +
	"③ 若这些文件在同一磁盘卷内，改用「硬链接合并」（不需要任何权限，且无链接失效风险）。"

// AggregateWarnings 把「环境级、可一次性说清」的失败归一化成一条汇总提示。
//
// 目前只处理软链接的权限失败（stage == symlinkPrivilegeStage）。其余失败
// 各文件原因不同（被篡改、路径冲突、卷不支持……），逐条保留原样才是对的，
// 不做归并。
//
// 归一化后条目级 Error 仍留在 Failed 列表里（上层据此计数与定位），
// 只是把重复的长文案换成短句，完整指引进 Warnings 供 UI 顶部展示。
//
// 设计取舍：这里**不修改** res.Failed 的条数——"有几个文件失败"是事实，
// 不能因为原因相同就合并成一条，否则结果页的计数会与实际不符。
func AggregateWarnings(res *model.OpsResult) {
	if res == nil {
		return
	}
	n := 0
	for i := range res.Failed {
		if res.Failed[i].Stage != symlinkPrivilegeStage {
			continue
		}
		n++
		// 统一为一句可读的中文，不再逐条重复平台层的长文案。
		res.Failed[i].Err = "创建符号链接需要权限（详见下方汇总提示）"
	}
	if n == 0 {
		return
	}
	res.Warnings = append(res.Warnings,
		fmt.Sprintf("%s（本次 %d 个文件因此失败）", symlinkPrivilegeSummary, n))
}

// symlinkPrivilegeStage 「环境缺少创建符号链接的权限」的失败阶段标记。
// worker 与 AggregateWarnings 共用同一常量，避免两处字面量漂移。
const symlinkPrivilegeStage = "symlink-privilege"

// joinWarn 合并两条提示（同卷建议 + 残留告警），空串自动略过。
func joinWarn(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "；另外：" + b
	}
}
