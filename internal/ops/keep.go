// Package ops 操作闭环（04 M3）：保留策略 / 操作前校验 / 回收站三平台 / 移动 / 硬链接。
package ops

import (
	"path/filepath"
	"strings"

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
func ApplyKeepPolicy(groups []*model.DuplicateGroup, policy model.KeepPolicy) []KeepDecision {
	var out []KeepDecision
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
				continue // 无匹配：该组保持现状（用户未选择）
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
		out = append(out, d)
	}
	return out
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
// 全部未命中返回 -1。dirs 中空串项忽略。
func pickByDirectoryPriority(g *model.DuplicateGroup, dirs []string) int {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if i := pickInDirectory(g, d); i >= 0 {
			return i
		}
	}
	return -1
}

// pickInDirectory 保留位于 dir 下的文件（最长前缀优先），无匹配返回 -1。
func pickInDirectory(g *model.DuplicateGroup, dir string) int {
	if dir == "" {
		return -1
	}
	dir = filepath.Clean(dir)
	best, bestLen := -1, 0
	for i, f := range g.Files {
		if strings.HasPrefix(f.Path, dir+string(filepath.Separator)) || f.Path == dir {
			if l := len(f.Path); l > bestLen {
				best, bestLen = i, l // 深层匹配更精确
			}
		}
	}
	return best
}
