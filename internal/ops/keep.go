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
//
// 路径归属判据统一走 inDir——本函数只负责"取最深的那一个"。
func pickInDirectory(g *model.DuplicateGroup, dir string) int {
	if dir == "" {
		return -1
	}
	sensitive := fscase.Sensitive(dir)
	best, bestLen := -1, 0
	for i, f := range g.Files {
		p := fscase.Fold(f.Path, sensitive)
		if !inDirFold(p, dir, sensitive) {
			continue
		}
		if l := len(p); l > bestLen {
			best, bestLen = i, l // 深层匹配更精确
		}
	}
	return best
}

// inDir 报告路径 p 是否位于 dir 之下（含 dir 自身）。
//
// ★ 这是本包内**唯一**的「路径属于目录」判据。保留策略的 pickInDirectory 与
// 处理策略的 ApplyProcessPolicy 都必须经由它，不得另写前缀比较。
//
// 为什么必须唯一（I5 的教训，见 SuggestKeepIndex 上方注释）：本项目曾出现过
// 两份独立的路径比较实现，在含隐藏目录的组上选出不同的文件——界面上被星标
// 的那一份，正是应用默认策略后要被清掉的那一份。多一份实现就多一次这种事故。
//
// 语义：
//   - 按 dir 所在卷的大小写语义折叠（不敏感卷上 /Users/X 与 /users/x 等价）
//   - 统一分隔符后再比较（Windows 上 "\" 与 "/" 混用不误判）
//   - 目录根（"/"、"C:\"）剥掉尾分隔符后命中该卷下全部绝对路径
//   - dir 为空或纯空白返回 false（调用方自行 TrimSpace）
func inDir(p, dir string) bool {
	d := strings.TrimSpace(dir)
	if d == "" {
		return false
	}
	return inDirFold(p, d, fscase.Sensitive(d))
}

// inDirFold 是 inDir 的内核，接收已折叠好的敏感性与已归一化的目录。
//
// 拆出来是为了让 pickInDirectory 复用：它需要对组内每个路径用**同一个**
// sensitive 反复判定，没必要每次都重算 fscase.Sensitive(dir)。
func inDirFold(foldedPath, dir string, sensitive bool) bool {
	// 归一后剥掉尾分隔符；根目录（"/"）剥完为空串，此时 HasPrefix(p, ""+"/")
	// 恰好命中全部绝对路径，语义仍是「该卷下全部保留」。
	prefix := strings.TrimSuffix(fscase.Fold(filepath.Clean(dir), sensitive), "/")
	return foldedPath == prefix || strings.HasPrefix(foldedPath, prefix+"/")
}

// ProcessPolicyOutcome 处理策略（优先处理的文件夹）的匹配结果。
type ProcessPolicyOutcome struct {
	// MatchIDs 位于任一优先目录下、且**不是保留项**的可处理文件 ID 集合。
	//
	// ★ 这里剔除保留项，**不是**「保留策略优先」的兑现点。那句话的兑现点是
	// 执行器的硬拒绝（executor.go：`保留文件不可操作`）——那是一道与 UI、
	// 与调用顺序都无关的兜底，谁也绕不过去（把 app.go 里的 KeepIDs 传参改成
	// nil 会让 TestProcessPolicyNeverOverridesKeepPolicy 立刻失败，已验证）。
	//
	// 本处在引擎里再剔一次，目的是**让计数说真话**：若把保留项放进集合，
	// 界面「将处理 N 项」就会虚高——用户看到 N，实际只处理 < N 项，
	// 差额还被当成"失败"记一笔。同一个数字有两个来源就会有两个答案，
	// 引擎给出的必须是那个能直接展示的。
	MatchIDs map[uint64]bool
	// UnmatchedDirs 在当前结果集里一个可处理文件都没命中的优先目录。
	//
	// 存的是**用户输入的原文**，不做大小写归一、不剥尾分隔符。
	// 理由：提示信息要给用户指出"你加的这一条没起作用"，
	// 回显他输入的原文他才找得到自己加的是哪条。
	UnmatchedDirs []string
	// MatchedFiles 命中文件数（= len(MatchIDs)），供调用方直接展示。
	MatchedFiles int
}

// ApplyProcessPolicy 计算「优先处理的文件夹」在本次结果集内的实际生效范围。
//
// 语义是**并集**，不是优先级：文件只要落在 dirs 中任意一个之下即命中。
// 这一点与保留策略的 directory 完全不同——那个要在多个目录里挑出**一个**，
// 所以必须有顺序；本函数是"这些目录里的都算"，没有优先级概念，
// 因此 dirs 无序，界面上也不提供排序（给了排序反而暗示存在优先级，是错误的心理模型）。
//
// keepIDs 为当前保留决策；命中的保留项会被剔除（见 ProcessPolicyOutcome.MatchIDs）。
// dirs 为空或全部为空白时返回空结果——调用方据此判定「未启用处理策略」，
// 走与新增本功能之前完全一致的代码路径。
func ApplyProcessPolicy(groups []*model.DuplicateGroup, dirs []string,
	keepIDs map[uint64]bool) ProcessPolicyOutcome {

	out := ProcessPolicyOutcome{MatchIDs: map[uint64]bool{}}

	// 归一目录列表，同时按"当前结果集内是否有可处理文件"逐条判定未命中。
	// 用 seen 记已处理过的目录（折叠后比较），避免用户重复添加同一目录时
	// UnmatchedDirs 里出现重复条目。
	type dirEntry struct {
		raw       string // 用户原文，用于回显
		sensitive bool
		hit       bool
	}
	entries := make([]*dirEntry, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		raw := strings.TrimSpace(d)
		if raw == "" {
			continue // 与 HasUsableDir 同口径：空白项忽略，不计入未命中
		}
		norm := fscase.Fold(filepath.Clean(raw), fscase.Sensitive(raw))
		if seen[norm] {
			continue // 重复目录只留第一条
		}
		seen[norm] = true
		entries = append(entries, &dirEntry{raw: raw, sensitive: fscase.Sensitive(raw)})
	}
	if len(entries) == 0 {
		return out
	}

	for _, g := range groups {
		for _, f := range g.Files {
			if keepIDs != nil && keepIDs[f.ID] {
				continue // 保留项不可处理，也不计入命中数
			}
			for _, e := range entries {
				// 按该目录的敏感性折叠文件路径：目录与文件在同一卷，
				// 敏感性一致；以目录的探测结果为准，是为了在"目录写错
				// 大小写"时仍能命中（I4 的核心场景）。
				if inDirFold(fscase.Fold(f.Path, e.sensitive), e.raw, e.sensitive) {
					out.MatchIDs[f.ID] = true
					e.hit = true
					break // 落在多个目录下也只计一次
				}
			}
		}
	}

	for _, e := range entries {
		if !e.hit {
			out.UnmatchedDirs = append(out.UnmatchedDirs, e.raw)
		}
	}
	out.MatchedFiles = len(out.MatchIDs)
	return out
}
