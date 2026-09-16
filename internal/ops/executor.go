package ops

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"filededup/internal/hasher"
	"filededup/internal/model"
)

// opWorkers 独立文件操作的并发上限（Y4）：delete/hardlink/回收站回退均为
// 单文件元数据 syscall（延迟主导），有界并发显著缩短大批量耗时；
// 上限保守，避免机械盘/网络盘上的寻道抖动。
const opWorkers = 4

// runIndexed 有界并发执行 n 个相互独立的任务；结果按下标写入，
// 由调用方按序汇总，从而在并发的同时保证输出顺序确定。
func runIndexed(n, workers int, fn func(i int)) {
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
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

// Options 执行器配置。
type Options struct {
	Groups     []*model.DuplicateGroup // 当前结果集（含组哈希）
	KeepIDs    map[uint64]bool         // 各组保留文件 ID（S2：不可操作）
	Pool       *hasher.Pool
	TrashFn    func(paths []string) error // 回收站（可注入，默认平台实现）
	OnProgress func(done, total int, current string)
}

// Execute 执行清理操作（trash/delete/move/hardlink），返回聚合结果。
// 安全语义（01 §9 / 02 决策 7）：
//   - 保留项拒绝执行（S2）；delete 必须 ConfirmDanger（S4）
//   - 操作前逐文件校验（S1 篡改拦截 / S8 ENOENT→Skipped）
//   - 单文件失败不中断整体（S7），逐项计入清单
//   - Execute 须由上层串行调用（app 层单任务约束）
func Execute(opts Options, op model.OpRequest) model.OpsResult {
	res := model.OpsResult{OK: []string{}, Failed: []model.FailedItem{}, Skipped: []string{}}
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

	// 校验阶段（S1/S8）
	var toProcess []*model.FileEntry
	for _, id := range op.FileIDs {
		e, ok := byID[id]
		if !ok {
			res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: fmt.Sprintf("文件不在当前结果集（id=%d）", id)})
			report("")
			continue
		}
		if opts.KeepIDs != nil && opts.KeepIDs[id] {
			res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: "保留文件不可操作"})
			report(e.Path)
			continue
		}
		switch VerifyFile(e, hashByID[id], pool) {
		case VerdictSkipped:
			res.Skipped = append(res.Skipped, e.Path) // S8：已消失 = 目标达成
			report(e.Path)
		case VerdictFailed:
			res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: "校验失败：文件在扫描后被修改"})
			report(e.Path)
		default:
			toProcess = append(toProcess, e)
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
		code  int
		stage string // 失败阶段（空 = ops，用于保留源校验等 verify 语义）
		err   string
	}
	outcomes := make([]outcome, len(toProcess))
	aggregate := func() {
		for i, e := range toProcess {
			switch outcomes[i].code {
			case ocOK:
				res.OK = append(res.OK, e.Path)
				res.Reclaimed += e.Size
			case ocSkipped:
				res.Skipped = append(res.Skipped, e.Path)
			case ocNone:
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: "未执行（内部状态缺失）"})
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
	case "trash", "delete", "move", "hardlink":
	default:
		res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: fmt.Sprintf("未知操作类型 %q", op.Kind)})
		return res
	}

	switch op.Kind {
	case "trash":
		paths := make([]string, 0, len(toProcess))
		for _, e := range toProcess {
			paths = append(paths, e.Path)
		}
		if len(paths) > 0 {
			if err := trash(paths); err == nil {
				for i := range toProcess {
					outcomes[i].code = ocOK
					report(toProcess[i].Path)
				}
			} else {
				// 批量失败退化为逐文件执行以隔离错误项（S5/S7）；
				// 各文件相互独立 → 有界并发
				runIndexed(len(toProcess), opWorkers, func(i int) {
					if err := trash([]string{toProcess[i].Path}); err != nil {
						outcomes[i] = outcome{code: ocFailed, err: err.Error()}
					} else {
						outcomes[i].code = ocOK
					}
					report(toProcess[i].Path)
				})
			}
		}

	case "delete":
		runIndexed(len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
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
		})

	case "move":
		// 串行（安全优先）：MoveFile 的目标重名递增（uniqueDst）是「先查后用」，
		// 并发处理同目录同基名文件时两个 worker 会选中同一目标名，后写者覆盖前者
		// → 静默数据丢失。该风险高于并发收益，故此处不做并发（01 §9 安全语义）。
		for i, e := range toProcess {
			if _, err := MoveFile(e.Path, op.TargetDir); err != nil {
				outcomes[i] = outcome{code: ocFailed, err: err.Error()}
			} else {
				outcomes[i].code = ocOK
			}
			report(e.Path)
		}

	case "hardlink":
		runIndexed(len(toProcess), opWorkers, func(i int) {
			e := toProcess[i]
			src := keepSrcByID[e.ID]
			if src == nil || src.ID == e.ID {
				outcomes[i] = outcome{code: ocFailed, err: "未找到组内保留源"}
				report(e.Path)
				return
			}
			// S1 扩展：keep 源也须校验——源在扫描后被篡改时硬链接会用新内容
			// 覆盖 dup 的原内容（不可逆），必须拦截。
			// 注：同组多个 dup 共享同一 keep 源，VerifyFile 为只读操作，并发安全。
			switch VerifyFile(src, hashByID[src.ID], pool) {
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
			if err := HardlinkMerge(src.Path, e.Path); err != nil {
				outcomes[i] = outcome{code: ocFailed, err: err.Error()}
			} else {
				outcomes[i].code = ocOK
			}
			report(e.Path)
		})

	default:
		// 已在入口拦截未知类型，此处不可达（防御性保留）
	}

	aggregate()
	return res
}
