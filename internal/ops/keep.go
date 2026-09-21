// Package ops 操作闭环（04 M3）：保留策略 / 操作前校验 / 回收站三平台 / 移动 / 硬链接。
package ops

import (
	"path/filepath"
	"strings"

	"filededup/internal/fscase"
	"filededup/internal/model"
	"filededup/internal/pathnorm"
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
	return ApplyKeepPolicyWith(groups, policy, nil)
}

// ApplyKeepPolicyWith 是 ApplyKeepPolicy 的持锁安全版本（APP-1，2026-09-21 全量审查）：
// 卷语义由调用方在**取锁之前**用 WarmSensitivity 预热好传进来。
//
// 为什么要有这个变体：AS-R3 只修了处理策略那半边（ApplyProcessPolicyWith），
// 保留策略的 directory 分支经 pickInDirectory 同样会就地调 fscase.Sensitive，
// 而那是**往用户目录写探测文件**的 I/O。app.go 的 ApplyKeepPolicy 全程持 a.mu，
// 一个死挂载就能把整把锁连同全部 Wails 绑定卡住。
// resolve 为 nil 时按需就地实测（与 ApplyProcessPolicyWith 同一口径）。
func ApplyKeepPolicyWith(groups []*model.DuplicateGroup, policy model.KeepPolicy,
	resolve SensResolver) (decisions []KeepDecision, unmatched []uint64) {
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
			keep = pickByDirectoryPriority(g, policy.Directories, resolve)
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
func pickByDirectoryPriority(g *model.DuplicateGroup, dirs []string, resolve SensResolver) int {
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if i := pickInDirectory(g, d, resolve); i >= 0 {
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
func pickInDirectory(g *model.DuplicateGroup, dir string, resolve SensResolver) int {
	if dir == "" {
		return -1
	}
	sensitive := resolveOf(resolve, dir)
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

// resolveOf 取某目录的卷语义：预热过就查表，没预热（resolve==nil）才就地实测。
// 与 normalizeDirs 的 nil 处理同一条规则，收在一处避免两边各写一遍。
func resolveOf(resolve SensResolver, dir string) bool {
	if resolve == nil {
		return fscase.Sensitive(dir)
	}
	return resolve(dir)
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
// ★ 2026-09-20（AS-H6）：这条"唯一"被前端破了——pathpolicy.ts 自己实现了一份
// dirContains 用来算「将处理 N / 已排除 M」，且实测漂移（不做 Clean 归一、
// 按路径形状猜大小写语义）。现在判据收回后端一处，前端经 FilterInDirs 取真值；
// 本函数与 FilterInDirs 共用 normalizeDirs/inAnyDir，口径不可能再分叉。
//
// 语义：
//   - 按 dir 所在卷的大小写语义折叠（不敏感卷上 /Users/X 与 /users/x 等价）
//   - 统一分隔符后再比较（Windows 上 "\" 与 "/" 混用不误判）
//   - 目录根（"/"、"C:\"）剥掉尾分隔符后命中该卷下全部绝对路径
//   - dir 为空或纯空白返回 false（调用方自行 TrimSpace）
func inDir(p, dir string) bool {
	return inAnyDir(p, normalizeDirs([]string{dir}, nil)) >= 0
}

// normDir 一条已归一的优先目录：用户原文（回显用）+ 该卷的大小写语义。
type normDir struct {
	raw       string
	sensitive bool
}

// SensResolver 报告某目录所在卷是否区分大小写。
type SensResolver func(dir string) bool

// dirKey 归一「同一目录」的比较键：预热表与查询两侧共用同一个函数，
// 避免出现"存进去的键和查出来的键不是一套"这类静默失效。
func dirKey(dir string) string { return filepath.Clean(strings.TrimSpace(dir)) }

// WarmSensitivity 提前把各目录的卷语义探测好，返回一个纯查表的解析器。
//
// 为什么要单独一步（AS-R3，2026-09-20 全仓审计）：fscase.Sensitive 不是纯函数，
// 它要往目标目录**写一个探测文件**再反向 Lstat。调用方若在持有应用锁的窗口里
// 触发它，一个死挂载（网络盘拔走后 stat 会挂死）就会把 a.mu 连同全部 Wails
// 绑定一起卡住。所以必须"I/O 在锁外做完，锁内只剩纯比较"。
//
// 探测结论在 fscase 内按目录缓存，预热之后同一目录再问不重复写盘。
func WarmSensitivity(dirs []string) SensResolver {
	sens := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		raw := strings.TrimSpace(d)
		if raw == "" {
			continue
		}
		if _, ok := sens[dirKey(raw)]; !ok {
			sens[dirKey(raw)] = fscase.Sensitive(raw)
		}
	}
	return func(dir string) bool {
		if v, ok := sens[dirKey(dir)]; ok {
			return v
		}
		// 表外目录：调用方传的 dirs 与这里问的 dirs 本该同一批，真出现意外时
		// 实测一次也比拿平台默认值瞎猜更安全。
		return fscase.Sensitive(dir)
	}
}

// normalizeDirs 归一优先目录列表：丢弃空白项、按折叠后形态去重（保留首条原文）。
//
// resolve 为 nil 时按需就地实测卷语义（无锁场景，如 FilterInDirs）；
// 持锁的调用方必须先 ops.WarmSensitivity 再把解析器传进来（AS-R3）。
//
// 去重键用 Clean+Fold 后的形态，所以 "/proc"、"/proc/"、" /proc " 只留第一条——
// ApplyProcessPolicy 靠这一点避免 UnmatchedDirs 出现重复条目。
// raw 保留**用户输入的原文**：提示信息要指出"你加的这一条没起作用"，
// 回显原文他才找得到自己加的是哪条。
func normalizeDirs(dirs []string, resolve SensResolver) []normDir {
	if resolve == nil {
		resolve = fscase.Sensitive
	}
	out := make([]normDir, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		raw := strings.TrimSpace(d)
		if raw == "" {
			continue // 与 HasUsableDir 同口径：空白项忽略
		}
		sensitive := resolve(raw)
		norm := fscase.Fold(filepath.Clean(raw), sensitive)
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, normDir{raw: raw, sensitive: sensitive})
	}
	return out
}

// inAnyDir 返回 p 命中的第一个归一目录的下标，全不命中返回 -1。
//
// 目录与文件在同一卷，敏感性一致；以**目录**的探测结果为准折叠，
// 是为了在"目录写错大小写"时仍能命中（I4 的核心场景）。
// inDirFold 的契约是接收**已折叠**的路径（见其注释），所以折叠必须在这里做。
func inAnyDir(p string, dirs []normDir) int {
	for i, e := range dirs {
		if inDirFold(fscase.Fold(p, e.sensitive), e.raw, e.sensitive) {
			return i
		}
	}
	return -1
}

// FilterInDirs 返回 paths 中落在 dirs 任一条目之下的**下标**（升序、不重复）。
//
// 为什么要有它（AS-H6，2026-09-20 全仓审计）：界面上的「将处理 N / 已排除 M」
// 此前由前端自己判路径归属，而真正决定执行范围的是后端的 inDir。同一判据两份
// 实现必然漂移，这次漂移的方向是**少报命中**——前端说"已排除"的那一项，后端其实
// 会处理，等于界面对"不会动的文件"做了承诺。前端只该负责显示。
//
// 语义与 ApplyProcessPolicy 完全一致（共用 normalizeDirs/inAnyDir）：
// dirs 是**并集**，无优先级；空白目录忽略；全部空白或 dirs 为空 → 空结果，
// 调用方据此判定"未启用处理策略"。
func FilterInDirs(dirs []string, paths []string) []int {
	normalized := normalizeDirs(dirs, nil)
	if len(normalized) == 0 {
		return []int{}
	}
	out := make([]int, 0, len(paths))
	for i, p := range paths {
		if inAnyDir(p, normalized) >= 0 {
			out = append(out, i)
		}
	}
	return out
}

// inDirFold 是 inDir 的内核，接收已折叠好的敏感性与已归一化的目录。
//
// 拆出来是为了让 pickInDirectory 复用：它需要对组内每个路径用**同一个**
// sensitive 反复判定，没必要每次都重算 fscase.Sensitive(dir)。
func inDirFold(foldedPath, dir string, sensitive bool) bool {
	// 归一后剥掉尾分隔符；根目录（"/"）剥完为空串，此时 HasPrefix(p, ""+"/")
	// 恰好命中全部绝对路径，语义仍是「该卷下全部保留」。
	prefix := strings.TrimSuffix(fscase.Fold(filepath.Clean(dir), sensitive), "/")
	return pathnorm.Under(foldedPath, prefix)
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
	return ApplyProcessPolicyWith(groups, dirs, keepIDs, nil)
}

// ApplyProcessPolicyWith 是 ApplyProcessPolicy 的持锁安全版本：
// 卷语义由调用方在**取锁之前**用 WarmSensitivity 预热好传进来。
//
// 为什么要有这个参数（AS-R3）：默认那一路会在归一目录时按需在用户目录写探测文件，
// 而那几步 I/O 一旦落在调用方的临界区里，死挂载就能把整把锁卡死。
// 结论按目录缓存，预热与就地实测给出的判定逐位相同。
func ApplyProcessPolicyWith(groups []*model.DuplicateGroup, dirs []string,
	keepIDs map[uint64]bool, resolve SensResolver) ProcessPolicyOutcome {

	out := ProcessPolicyOutcome{MatchIDs: map[uint64]bool{}}

	// 归一目录列表（丢弃空白、按折叠形态去重），同时逐条记录是否命中。
	// 判据与 FilterInDirs 共用 inAnyDir——预览计数与执行范围必须来自同一个内核。
	entries := normalizeDirs(dirs, resolve)
	if len(entries) == 0 {
		return out
	}
	hit := make([]bool, len(entries))

	for _, g := range groups {
		for _, f := range g.Files {
			if keepIDs != nil && keepIDs[f.ID] {
				continue // 保留项不可处理，也不计入命中数
			}
			if i := inAnyDir(f.Path, entries); i >= 0 {
				out.MatchIDs[f.ID] = true
				hit[i] = true // 落在多个目录下也只记第一个（与命中判定同步收口）
			}
		}
	}

	for i, e := range entries {
		if !hit[i] {
			out.UnmatchedDirs = append(out.UnmatchedDirs, e.raw)
		}
	}
	out.MatchedFiles = len(out.MatchIDs)
	return out
}
