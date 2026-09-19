// Package ops 操作闭环（04 M3）：保留策略 / 操作前校验 / 回收站三平台 / 移动 / 硬链接。
package ops

import (
	"path/filepath"
	"strings"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// KeepDecision 一组文件的保留决策。
type KeepDecision struct {
	GroupID   uint64   `json:"groupID"`
	KeepID    uint64   `json:"keepID"`
	RemoveIDs []uint64 `json:"removeIDs"`
}

// ApplyKeepPolicy 对全部组计算保留决策（M3-T01）。
// manual 策略：保持调用方已有勾选，本函数不产生决策（返回空，由前端状态决定）。
//
// unmatched（2026-09-18 审查 I4）：directory 策略下「组内没有任何成员位于
// 指定保留目录」的组 ID。修正前这类组被静默 continue，整组一个保留者都不标，
// 用户以为「应用过策略 = 已保护」，实际整组都可被清掉——必须回给调用方明示。
func ApplyKeepPolicy(groups []*model.DuplicateGroup, policy model.KeepPolicy) (decisions []KeepDecision, unmatched []uint64) {
	for _, g := range groups {
		if len(g.Files) < 2 {
			continue
		}
		var keep int
		switch policy.Kind {
		case "newest":
			keep = pickBy(g, func(a, b *model.FileEntry) bool { return a.ModTime > b.ModTime })
		case "oldest":
			keep = pickBy(g, func(a, b *model.FileEntry) bool { return a.ModTime < b.ModTime })
		case "directory":
			keep = pickByDirectoryPriority(g, policy.Directories)
			if keep < 0 {
				// 无匹配：该组保持现状（用户未选择），并计入 unmatched
				unmatched = append(unmatched, g.GroupID)
				continue
			}
		case "manual":
			continue // 前端已勾选
		default: // shortest（默认建议，01 §9：路径最短且非隐藏）
			keep = pickShortest(g)
		}
		d := KeepDecision{GroupID: g.GroupID, KeepID: g.Files[keep].ID}
		for i, f := range g.Files {
			if i != keep {
				d.RemoveIDs = append(d.RemoveIDs, f.ID)
			}
		}
		decisions = append(decisions, d)
	}
	return decisions, unmatched
}

// SuggestKeepIndex 默认建议保留者索引（01 §9：优先非隐藏，再路径最短）。
//
// 2026-09-18 审查 I5：结果页星标与 "shortest" 策略必须走同一份实现。
// 修正前 app.go 另写了一份「只比路径长度」的规则，两份在含隐藏目录的组上
// 会选出不同的文件——界面上被星标的那一份，正是应用默认策略后被清掉的那一份。
// 无成员时返回 -1。
func SuggestKeepIndex(g *model.DuplicateGroup) int {
	if len(g.Files) == 0 {
		return -1
	}
	return pickShortest(g)
}

// pickBy 返回满足 cmp(a,b) 的第一个文件索引。
func pickBy(g *model.DuplicateGroup, cmp func(a, b *model.FileEntry) bool) int {
	best := 0
	for i := 1; i < len(g.Files); i++ {
		if cmp(g.Files[i], g.Files[best]) {
			best = i
		}
	}
	return best
}

// pickShortest 路径最短且不在隐藏目录（01 §9 默认建议）。
func pickShortest(g *model.DuplicateGroup) int {
	best := 0
	for i := 1; i < len(g.Files); i++ {
		bi, gi := hidden(g.Files[i].Path), hidden(g.Files[best].Path)
		switch {
		case bi != gi:
			if !bi {
				best = i
			}
		case len(g.Files[i].Path) < len(g.Files[best].Path):
			best = i
		}
	}
	return best
}

func hidden(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.HasPrefix(seg, ".") && seg != "." && seg != ".." {
			return true
		}
	}
	return false
}

// pickByDirectoryPriority 按目录优先级顺序取首个命中目录的保留者，
// 全部未命中返回 -1。dirs 中空/纯空白项忽略。
func pickByDirectoryPriority(g *model.DuplicateGroup, dirs []string) int {
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if i := pickInDirectory(g, d); i >= 0 {
			return i
		}
	}
	return -1
}

// HasUsableDir 目录优先级列表中是否存在有效项（非空/非纯空白）。
func HasUsableDir(dirs []string) bool {
	for _, d := range dirs {
		if strings.TrimSpace(d) != "" {
			return true
		}
	}
	return false
}

// pickInDirectory 保留位于 dir 下的文件（最长前缀优先），无匹配返回 -1。
//
// I4（2026-09-18 审查）：比较前按 dir 所在卷的大小写语义折叠，并统一分隔符。
// 修正前是裸的字符串前缀比较：APFS 不敏感卷上用户写 /users/x/docs、实际路径
// /Users/X/Docs，整组匹配不上 → 整组不受保护；Windows 上 "\" 与 "/" 混用同理。
func pickInDirectory(g *model.DuplicateGroup, dir string) int {
	if dir == "" {
		return -1
	}
	sensitive := fscase.Sensitive(dir)
	// 归一后剥掉尾分隔符；根目录（"/"）剥完为空串，此时 HasPrefix(p, ""+"/")
	// 恰好命中全部绝对路径，语义仍是「该卷下全部保留」。
	prefix := strings.TrimSuffix(fscase.Fold(filepath.Clean(dir), sensitive), "/")
	best, bestLen := -1, 0
	for i, f := range g.Files {
		p := fscase.Fold(f.Path, sensitive)
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			if l := len(p); l > bestLen {
				best, bestLen = i, l // 深层匹配更精确
			}
		}
	}
	return best
}
