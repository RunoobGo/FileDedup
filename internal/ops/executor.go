package ops

import (
	"fmt"
	"os"

	"filededup/internal/hasher"
	"filededup/internal/model"
)

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
	done := 0
	report := func(path string) {
		done++
		if opts.OnProgress != nil {
			opts.OnProgress(done, total, path)
		}
	}

	// 校验阶段（S1/S8）
	var toProcess []*model.FileEntry
	for _, id := range op.FileIDs {
		e, ok := byID[id]
		if !ok {
			res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: fmt.Sprintf("文件不在当前结果集（id=%d）", id)})
			continue
		}
		if opts.KeepIDs != nil && opts.KeepIDs[id] {
			res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: "保留文件不可操作"})
			continue
		}
		switch VerifyFile(e, hashByID[id], pool) {
		case VerdictSkipped:
			res.Skipped = append(res.Skipped, e.Path) // S8：已消失 = 目标达成
		case VerdictFailed:
			res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: "校验失败：文件在扫描后被修改"})
		default:
			toProcess = append(toProcess, e)
		}
	}

	// 执行阶段
	switch op.Kind {
	case "trash":
		paths := make([]string, 0, len(toProcess))
		for _, e := range toProcess {
			paths = append(paths, e.Path)
		}
		if err := trash(paths); err != nil {
			// 批量失败退化为逐文件执行以隔离错误项（S5/S7）
			for _, e := range toProcess {
				if err := trash([]string{e.Path}); err != nil {
					res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: err.Error()})
					continue
				}
				res.OK = append(res.OK, e.Path)
				res.Reclaimed += e.Size
				report(e.Path)
			}
			return res
		}
		for _, e := range toProcess {
			res.OK = append(res.OK, e.Path)
			res.Reclaimed += e.Size
			report(e.Path)
		}

	case "delete":
		for _, e := range toProcess {
			if err := os.Remove(e.Path); err != nil {
				if os.IsNotExist(err) {
					res.Skipped = append(res.Skipped, e.Path)
				} else {
					res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: err.Error()})
				}
				continue
			}
			res.OK = append(res.OK, e.Path)
			res.Reclaimed += e.Size
			report(e.Path)
		}

	case "move":
		for _, e := range toProcess {
			if _, err := MoveFile(e.Path, op.TargetDir); err != nil {
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: err.Error()})
				continue
			}
			// OK 语义 = 结果集中已移除的源路径（上层清理与统计依据），
			// 与 trash/delete/hardlink 保持一致
			res.OK = append(res.OK, e.Path)
			res.Reclaimed += e.Size
			report(e.Path)
		}

	case "hardlink":
		for _, e := range toProcess {
			src := keepSrcByID[e.ID]
			if src == nil || src.ID == e.ID {
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: "未找到组内保留源"})
				continue
			}
			// S1 扩展：keep 源也须校验——源在扫描后被篡改时硬链接会用新内容
			// 覆盖 dup 的原内容（不可逆），必须拦截
			switch VerifyFile(src, hashByID[src.ID], pool) {
			case VerdictSkipped:
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "verify",
					Err: "保留源已消失，无法合并（S1 拦截）"})
				continue
			case VerdictFailed:
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "verify",
					Err: "保留源在扫描后被修改，已拦截（S1）"})
				continue
			}
			if err := HardlinkMerge(src.Path, e.Path); err != nil {
				res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ops", Err: err.Error()})
				continue
			}
			res.OK = append(res.OK, e.Path)
			res.Reclaimed += e.Size
			report(e.Path)
		}

	default:
		res.Failed = append(res.Failed, model.FailedItem{Stage: "ops", Err: fmt.Sprintf("未知操作类型 %q", op.Kind)})
	}
	return res
}
