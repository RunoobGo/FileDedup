// app_result.go —— 结果集查询、授权与保留策略。
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
	"filededup/internal/ops"
	"fmt"
	"math"
	"path/filepath"
	"sort"
)

// GetResultGroups 分页查询重复组（M2-T07 排序/筛选；Y3 排序缓存）。
//
// 功能 3（2026-09-23）：q.Dirs/q.ExcludeDirs 非空时逐行填 FileView.IsPending，
// 投影与执行判据同收归 pendingIDsLocked（见 pendingSetLocked），这里只是把
// 全量结果集的 ID 喂进内核。预热（写探测文件）必须保持在取锁**之前**（AS-R3），
// 且仅在有策略目录时才发生——翻页是高频动作，无策略时零额外 I/O。
func (a *App) GetResultGroups(q ResultQuery) (PagedResult, error) {
	var resolve ops.SensResolver
	if all := warmDirs(q.Dirs, q.ExcludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if q.PageSize <= 0 {
		q.PageSize = 100
	} else if q.PageSize > 500 {
		q.PageSize = 500 // 超限钳到上限（而非重置默认，语义更直观）
	}
	if q.Page < 0 {
		q.Page = 0
	}
	// Y3：筛选 + 排序结果按 (sort, ext) 复用。修正前每次翻页都全量重排
	// （O(n log n)）并重扫扩展名（O(组×文件)），且全程持锁阻塞进度事件。
	key := sortKey{sort: q.Sort, ext: normalizeExt(q.Ext)}
	guard := resultGuard(a.groups)
	entry, ok := a.viewCache[key]
	if !ok || entry.guard != guard {
		entry = viewCacheEntry{groups: a.buildSortedGroupsLocked(key), guard: guard}
		a.viewCache[key] = entry
	}
	gs := entry.groups

	total := uint64(len(gs))
	// 全量可释放空间（含未加载页）：统计条与 totalGroups 同口径。
	// M6-P2：实占口径必须同样走全量——只给当页之和会让统计条在翻页时数字
	// 跳动，正是 M4 审查修过的那类"部分当全部"。
	var totalReclaim, totalReclaimActual uint64
	for _, g := range gs {
		totalReclaim += g.Reclaimable
		totalReclaimActual += g.ReclaimableActual
	}
	// M10a（2026-09-21 全仓审计 §五 10）：起点必须**溢出安全**。
	// q.Page/q.PageSize 是绑定层入参（int），前端能给任意值：修正前直接
	// `q.Page * q.PageSize`，乘积回绕成负数会让下面的 `start >= len(gs)`
	// 判不出来、`gs[start:end]` 当场 panic；回绕成正数则会拿出错误的一页。
	// 溢出只有一种含义（起点远在结果集之外），先用除法查出来。
	// 用 math.MaxInt 而非 MaxInt64：切片下标是平台 int，32 位目标上
	// 天花板是 MaxInt32，按 64 位判会把"已经回绕"的乘积放过去。
	if q.Page > 0 && q.PageSize > math.MaxInt/q.Page {
		return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, TotalReclaimableActual: totalReclaimActual, Groups: []GroupView{}}, nil
	}
	start := q.Page * q.PageSize
	if start >= len(gs) {
		return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, TotalReclaimableActual: totalReclaimActual, Groups: []GroupView{}}, nil
	}
	end := start + q.PageSize
	if end > len(gs) {
		end = len(gs)
	}
	views := make([]GroupView, 0, end-start)
	// IsPending 投影：只在这里算一次（本页所有组共用同一份集合）。
	// 放在切片判空之后：空页不必为"没有行的结果"遍历整个 byID。
	pending := a.pendingSetLocked(q.Dirs, q.ExcludeDirs, resolve)
	for _, g := range gs[start:end] {
		views = append(views, toGroupView(g, a.keepIDs, pending))
	}
	return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, TotalReclaimableActual: totalReclaimActual, Groups: views}, nil
}

// buildSortedGroupsLocked 执行一次扩展名筛选 + 排序（调用方须持 a.mu）。
// 返回切片只读复用：调用方不得原地修改其元素顺序。
func (a *App) buildSortedGroupsLocked(key sortKey) []*model.DuplicateGroup {
	var gs []*model.DuplicateGroup
	for _, g := range a.groups {
		// 防御：空组直接剔除，不进入排序与分页。
		// size 排序分支读 gs[i].Files[0].Size，空组会越界 panic；count 与
		// reclaimable 分支虽不读 Files[0]，但把空组展示成"0 个文件"的行同样
		// 无意义（既不能勾选也没有可释放空间）。畸形组只可能来自历史库被
		// 外部改写（扫描流水线输出前有 len<2 过滤），这里统一挡掉。
		if len(g.Files) == 0 {
			continue
		}
		if key.ext != "" && !groupHasExt(g, key.ext) {
			continue
		}
		gs = append(gs, g)
	}
	// 排序（均为降序；稳定：组 ID 兜底）
	switch key.sort {
	case "size":
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].Files[0].Size != gs[j].Files[0].Size {
				return gs[i].Files[0].Size > gs[j].Files[0].Size
			}
			return gs[i].GroupID < gs[j].GroupID
		})
	case "count":
		sort.Slice(gs, func(i, j int) bool {
			if len(gs[i].Files) != len(gs[j].Files) {
				return len(gs[i].Files) > len(gs[j].Files)
			}
			return gs[i].GroupID < gs[j].GroupID
		})
	default: // reclaimable
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].Reclaimable != gs[j].Reclaimable {
				return gs[i].Reclaimable > gs[j].Reclaimable
			}
			return gs[i].GroupID < gs[j].GroupID
		})
	}
	return gs
}

// invalidateViewCacheLocked 结果集结构变更后清空视图缓存（调用方须持 a.mu）。
func (a *App) invalidateViewCacheLocked() {
	if a.viewCache != nil {
		clear(a.viewCache)
	}
}

// GetFailedItems 失败清单（扫描失败；操作失败随 M3 并入）。
func (a *App) GetFailedItems() []model.FailedItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed == nil {
		return []model.FailedItem{}
	}
	// M322（APP-36）：交副本，不交内部切片的头。绑定调用方在锁**外**序列化，
	// 拿到的数组若与 a.failed 同底层，任何一次写回（今天的 `a.failed = ...`、
	// 将来最自然的 `a.failed = append(a.failed, ...)`）都会与它共享内存：
	// 轻则调用方一笔写就改到 App 状态（本批的判据用例钉这一格），
	// 重则读侧与在途写并发（race 门禁第 7/8 行）。同文件其余交出点都在锁内做了
	// 投影/拷贝，这一处是唯一漏网的。
	out := make([]model.FailedItem, len(a.failed))
	copy(out, a.failed)
	return out
}

// FilterInDirs 批量判定「哪些路径位于任一优先目录下」，返回 paths 的下标（升序、不重复）。
//
// ★ 2026-09-20（全仓审计 AS-H6 / 决策 D-1 方案 b）：界面上的「将处理 N / 已排除 M」
// 此前由前端自己实现一份路径归属判据算出来，而后端执行范围由 ops.inDir 决定——
// 同一判据两份实现，且前端那份已经实测漂移（不做 Clean 归一、按路径形状猜大小写
// 语义），漂移方向是**少报命中**：界面说"已排除"的那一项后端其实会处理，
// 等于对"不会动的文件"做了假承诺。现在前端只显示这里返回的下标。
//
// 与 PreviewProcessPolicy 的分工：那个绑定求的是「勾选 ID ∩ 后端结果集」，
// 需要 a.mu；本绑定是纯字符串判据，输入完全由前端给出（前端手上有当前
// 展示的路径），因此**不加锁、不受 opsRunning 互斥限制**，可以在清理进行中调用。
//
// 语义与执行侧严格一致：并集、无优先级、空白目录忽略；dirs 全空白时返回空列表
// （= 未启用处理策略，前端据此走"不过滤"的原路径）。
func (a *App) FilterInDirs(dirs []string, paths []string) []int {
	return ops.FilterInDirs(dirs, paths)
}

// ApplyKeepPolicy 保留策略引擎（M3-T01）：返回决策并记录 keepIDs（S2 保护依据）。
// 遍历阶段全程持锁（须与操作 goroutine 的结果集清理写互斥）；
// 历史持久化放到放锁之后（hist 自有锁，禁止与 a.mu 嵌套）。
func (a *App) ApplyKeepPolicy(policy model.KeepPolicy) (KeepOutcome, error) {
	// ★ APP-1（2026-09-21 全量审查）：AS-R3 的同一件事在处理策略侧已修，
	// 保留策略侧漏了。directory 分支要按卷问 fscase.Sensitive，而它**要往用户
	// 目录写探测文件**——落在下面的持锁窗口里，一个死挂载就能把 a.mu 连同
	// 全部 Wails 绑定一起卡住。预热之后锁内只剩纯查表 + 纯比较。
	var resolve ops.SensResolver
	if policy.Kind == "directory" && len(policy.Directories) > 0 {
		resolve = ops.WarmSensitivity(policy.Directories)
	}

	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return KeepOutcome{}, fmt.Errorf("清理操作执行中，请稍后再应用保留策略")
	}
	if len(a.groups) == 0 {
		a.mu.Unlock()
		return KeepOutcome{}, fmt.Errorf("暂无结果集")
	}
	if policy.Kind == "directory" && !ops.HasUsableDir(policy.Directories) {
		a.mu.Unlock()
		return KeepOutcome{}, fmt.Errorf("请至少添加一个保留目录")
	}
	decisions, unmatched := ops.ApplyKeepPolicyWith(a.groups, policy, resolve)
	a.keepIDs = make(map[uint64]bool, len(decisions))
	for _, d := range decisions {
		a.keepIDs[d.KeepID] = true
	}
	histID, paths := a.curHistID, a.keepPathsLocked()
	a.mu.Unlock()
	a.persistKeepPaths(histID, paths)
	// I4：directory 策略下没有任何成员命中保留目录的组，一个保留者都不会被标出来。
	// 这类组不受 S2 保护，必须让 UI 说清楚，不能让用户以为"已应用=已保护"。
	return KeepOutcome{Decisions: decisions, UnmatchedGroups: len(unmatched)}, nil
}

// ClearKeepDecisions 清除保留决策（回到默认建议）。
// B3-1：与 ApplyKeepPolicy 同口径的在途互斥。修正前只有本方法无守卫——清理
// 执行中点「重置」会成功改写 a.keepIDs，而同一次操作实际用的是派发时的快照，
// 于是「界面已重置、文件系统仍按旧保护集执行」两套事实并存。
func (a *App) ClearKeepDecisions() error {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return fmt.Errorf("清理操作执行中，请稍后重置保留策略")
	}
	a.keepIDs = nil
	histID := a.curHistID
	a.mu.Unlock()
	a.persistKeepPaths(histID, nil)
	return nil
}

// keepPathsLocked 当前保留决策对应的路径集合（须持 a.mu 调用）。
// 跨会话只有路径稳定——历史恢复后按 path→rowid 重建 keepIDs。
func (a *App) keepPathsLocked() []string {
	// 空集合用 [] 而非 nil：本函数的返回值语义就是"路径列表"，
	// nil 在这里只是"没数据"的另一种写法，徒增调用方的判空负担。
	out := make([]string, 0, len(a.keepIDs))
	if len(a.keepIDs) == 0 {
		return out
	}
	for _, g := range a.groups {
		for _, f := range g.Files {
			if a.keepIDs[f.ID] {
				out = append(out, f.Path)
			}
		}
	}
	return out
}

// persistKeepPaths 把保留决策同步进当前历史行（锁外调用；无历史联动则跳过）。
func (a *App) persistKeepPaths(histID int64, paths []string) {
	hs := a.histSnapshot()
	if hs == nil || histID == 0 {
		return
	}
	if err := hs.UpdateKeepPaths(histID, paths); err != nil {
		a.warnLedger(fmt.Sprintf("保留决策保存失败：%v，恢复该条历史时保留项会回到上一次成功保存的状态", err))
	}
}

// pendingIDsLocked 是「拟处理集」判据的唯一内核：
// 勾选 ∩ 结果集内非保留 ∩（若启用）优先目录 −（若启用）黑名单目录。
//
// 为什么必须收归一份（设计段 §3）：预览计数、拟处理清单、执行派发三处都要问
// "哪些文件真的会被动"。此前预览与执行已按 M10c 修齐口径但仍是两段相似代码，
// 清单再加一段就是三套判据——AS-H6/I5 两类事故（判据漂移 ⇒ 界面对"不会动的
// 文件"做承诺）的温床。现在三处只有这一份实现。
//
// 返回：ids 按 selectedIDs 顺序去重（同 planOpItems 的 seen）；reason 给每个
// 被排除的勾选 id 归类（keep/outside/gone/excluded，见 Pending* 常量）；unmatched 是
// 一个可处理文件都没命中的优先目录（用户原文，回显定位用）。
//
// 须持 a.mu 调用；resolve 由调用方在**取锁之前**用 ops.WarmSensitivity 预热（AS-R3：
// 卷语义探测要写用户目录探测文件，锁内做 I/O 会被死挂载连坐卡死全部绑定）。
// 预热必须覆盖 dirs 与 excludeDirs **两批**目录——黑名单漏预热就是锁内写盘。
//
// 分岔语义与修正前逐格一致（M10c）：dirs 为空走 byID 存在性 + keepIDs 过滤；
// 非空走引擎 MatchIDs（引擎已剔保留项）。排除归类按 gone→keep→outside→excluded
// 四个判据**短路**——最先拦住它的是谁就记谁：白名单没放行谈不上黑名单，
// 保留项在执行器里本来就是硬拒绝，excluded 的说法会夸大黑名单的能力。
// 对结果集外的陈旧 id，两分支归类相同（gone），生效集合不变。
//
// excludeDirs 匹配走与 ProcessDirs 完全相同的引擎（ApplyProcessPolicyWith 的
// inAnyDir 判据，含卷大小写敏感性折叠）：黑名单判错的后果是"说了不处理却
// 处理了"（fail-dangerous），必须与白名单同一把尺子。keepIDs 传 nil——这里只要
// "路径归属"，保留项已在上一格短路，不该被引擎再吞一次归类信息。
func (a *App) pendingIDsLocked(dirs, excludeDirs []string, resolve ops.SensResolver,
	selectedIDs []uint64) (ids []uint64, reason map[uint64]string, unmatched []string) {
	var match map[uint64]bool
	if len(dirs) > 0 {
		out := ops.ApplyProcessPolicyWith(a.groups, dirs, a.keepIDs, resolve)
		match = out.MatchIDs
		unmatched = out.UnmatchedDirs
	}
	var excluded map[uint64]bool
	if len(excludeDirs) > 0 {
		excluded = ops.ApplyProcessPolicyWith(a.groups, excludeDirs, nil, resolve).MatchIDs
	}
	ids = []uint64{} // 空集合回 [] 不回 nil：这条线直达 JSON，nil 会序列化成 null
	reason = make(map[uint64]string, len(selectedIDs))
	seen := make(map[uint64]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		if seen[id] {
			continue // 重复勾选只算一次（同 planOpItems 的 seen）
		}
		seen[id] = true
		if _, ok := a.byID[id]; !ok {
			reason[id] = PendingGone
			continue
		}
		if a.keepIDs[id] {
			reason[id] = PendingKeep
			continue
		}
		if match != nil && !match[id] {
			reason[id] = PendingOutside
			continue
		}
		if excluded[id] {
			reason[id] = PendingExcluded
			continue
		}
		ids = append(ids, id)
	}
	return ids, reason, unmatched
}

// pendingSetLocked 结果页投影用的「策略可处理」集合（功能 3）：把 byID 全量
// 当候选集喂进 pendingIDsLocked，返回 ID 集合。
//
// ★ 为什么不另写一份"非保留 ∩ 白 − 黑"：那会是拟处理判据的第二实现——
// AS-H6（前端自算命中）与 I5（视图自选保留者）两类事故都是同一个形状：
// 第二份实现漂移后，界面对"不会动的文件"做承诺、或把该留的藏了。
// 这里付出的 O(结果文件数) 每次翻页一遍，换"藏的行 == 引擎会动的行"这个不变式。
//
// gone 归类天然不会命中（候选集就是结果集全量）；勾选上下文不在口径里——
// IsPending 说"策略上可处理"，"用户勾没勾"是显示层的另一层，见 FileView.IsPending。
//
// 须持 a.mu；resolve 由调用方在取锁前预热（AS-R3，与 pendingIDsLocked 同一约定）。
func (a *App) pendingSetLocked(dirs, excludeDirs []string, resolve ops.SensResolver) map[uint64]bool {
	all := make([]uint64, 0, len(a.byID))
	for id := range a.byID {
		all = append(all, id)
	}
	ids, _, _ := a.pendingIDsLocked(dirs, excludeDirs, resolve, all)
	set := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// PreviewProcessPolicy 在**不执行任何操作**的前提下，算出处理策略的实际生效范围。
//
// 为什么需要它：处理策略只做执行时过滤、不改动用户勾选，所以"实际会处理哪些"
// 在界面上天然不可见。若不预览，用户会看到"已勾选 40 项"点下去却只处理 12 项，
// 然后以为操作失败了。有了它，按钮计数、选择区说明、确认框三处都能显示真实数量。
//
// 参数带上 selectedIDs 并在此处求交，是为了让"交集"只有一份实现——
// 若让前端自己算，就又出现两处独立实现，正是 I5 那类事故的温床。
// 2026-09-23 起该求交收进 pendingIDsLocked，与 GetPendingFiles 共用；
// 同日功能 2 加 excludeDirs（「不处理的文件夹」黑名单），判据仍只有那一份。
//
// 无副作用：只读 groups/keepIDs 快照，不置 opsRunning、不写账本、不动文件系统。
// 因此它**不受 opsRunning 互斥限制**（预览是只读的，不该被正在进行的清理挡住）。
func (a *App) PreviewProcessPolicy(dirs, excludeDirs []string, selectedIDs []uint64) (ProcessPreview, error) {
	// ★ AS-R3（2026-09-20 全仓审计）：卷语义探测要**往用户目录写探测文件**，
	// 属于 I/O，必须在取锁之前做完——死挂载（网络盘拔走后 stat 挂死）时，
	// 锁内一次写盘就能把 a.mu 连同全部 Wails 绑定一起卡住，而预览本应是只读操作。
	// 预热之后（结论按目录缓存），锁内那次调用是纯查表 + 纯比较。
	// 黑名单与白名单走同一引擎，预热也必须一并覆盖（功能 2）。
	var resolve ops.SensResolver
	if all := warmDirs(dirs, excludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}

	// 全程持锁：这里只剩纯内存计算（无 I/O、不回调整个 App），
	// 耗时与组数成正比，锁住它不伤害交互。此前"锁内浅快照、锁外遍历"的写法
	// 挡不住**元素级**竞争——a.groups 是 []*DuplicateGroup，浅拷贝共享指针，
	// 执行器收尾会在锁内就地改写 g.Files（app.go 结果集清理段 g.Files = files），
	// 无锁遍历即构成数据竞争（2026-09-20 -race 实测复现，TestPreviewProcessPolicyConcurrentWithCleanupNoRace）。
	a.mu.Lock()
	defer a.mu.Unlock()

	ids, _, unmatched := a.pendingIDsLocked(dirs, excludeDirs, resolve, selectedIDs)
	pv := ProcessPreview{EffectiveIDs: ids, EffectiveCount: len(ids), UnmatchedDirs: []string{}}
	// 空结果集也要算出未命中目录（用户加了目录但当前没有重复文件，
	// 正是最需要提示的场景），所以不在这里提前返回。
	if unmatched != nil {
		pv.UnmatchedDirs = append(pv.UnmatchedDirs, unmatched...)
	}
	return pv, nil
}

// GetPendingFiles 分页查询「执行前拟处理清单」：勾选里哪些真的会被处理、
// 哪些不会（及原因）。2026-09-23 新增（设计段 2026-09-23-pending-files-query）。
//
// 与 PreviewProcessPolicy 的关系：同一内核（pendingIDsLocked）的两个投影——
// 预览给计数（按钮禁用、确认框用），清单给明细（用户核对"到底动哪几个文件"）。
// 二者对同一输入必须逐 id 相等，由 TestPendingFilesMirrorsPreviewKernel 钉住。
//
// 行序：先「将处理」段（按 Sort：group=结果集组序、size 降序、path 升序，
// tie-break 一律落到 GroupID→ID——M144 教训：同值不落地就会翻页跳行），
// 后「不会处理」段（勾选原序）。分界索引 = PendingCount，前端据此插分区标题。
//
// 锁与副作用语义与 PreviewProcessPolicy 逐条相同（只读、持 a.mu、锁外预热、
// 不受 opsRunning 互斥限制）。分页钳位与溢出安全同 GetResultGroups（M10a）。
func (a *App) GetPendingFiles(q PendingQuery) (PendingPage, error) {
	var resolve ops.SensResolver
	if all := warmDirs(q.Dirs, q.ExcludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if q.PageSize <= 0 {
		q.PageSize = 100
	} else if q.PageSize > 500 {
		q.PageSize = 500
	}
	if q.Page < 0 {
		q.Page = 0
	}

	ids, reason, _ := a.pendingIDsLocked(q.Dirs, q.ExcludeDirs, resolve, q.SelectedIDs)
	pending := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		pending[id] = true
	}

	// 组内位置一次遍历拿齐：GroupID 展示用，(组序, 组内序) 是 group 排序键。
	// 覆盖所有勾选（含被排除项）——"属于哪个组"对排除行同样是有效信息。
	type loc struct {
		gid     uint64
		grpIdx  int
		fileIdx int
	}
	sel := make(map[uint64]bool, len(q.SelectedIDs))
	for _, id := range q.SelectedIDs {
		sel[id] = true
	}
	pos := make(map[uint64]loc, len(sel))
	for gi, g := range a.groups {
		for fi, f := range g.Files {
			if sel[f.ID] {
				pos[f.ID] = loc{g.GroupID, gi, fi}
			}
		}
	}

	rows := make([]PendingRow, 0, len(ids))
	excludedRows := make([]PendingRow, 0, len(q.SelectedIDs)-len(ids))
	var keepN, outsideN, goneN, excludedN int
	emitted := make(map[uint64]bool, len(q.SelectedIDs)) // 重复勾选只出一行（与内核 seen 同口径）
	for _, id := range q.SelectedIDs {
		if emitted[id] {
			continue
		}
		emitted[id] = true
		if pending[id] {
			e := a.byID[id]
			p := pos[id]
			rows = append(rows, PendingRow{
				ID: id, Path: e.Path, Name: filepath.Base(e.Path),
				Size: e.Size, GroupID: p.gid, Pending: true,
			})
			continue
		}
		r := reason[id]
		if r == "" {
			// 理论不可达：pendingIDsLocked 对每个被排除 id 必归三类之一。
			// 兜成 gone（最保守的说法——"它不在会被动的清单里"为大方向真），
			// 而不是 panic 或漏行：漏行会让 Total 说谎。
			r = PendingGone
		}
		switch r {
		case PendingKeep:
			keepN++
		case PendingOutside:
			outsideN++
		case PendingExcluded:
			excludedN++
		default:
			goneN++
		}
		// gone 的 id 已不在 byID，路径无从得知——行里只有 ID+原因。
		// 这是能力的边界不是实现的偷懒：给它编一个"上一条已知路径"就是说谎。
		var e *model.FileEntry
		if r != PendingGone {
			e = a.byID[id]
		}
		row := PendingRow{ID: id, Reason: r, GroupID: pos[id].gid} // 组已消散时零值 0，仅展示用
		if e != nil {
			row.Path = e.Path
			row.Name = filepath.Base(e.Path)
			row.Size = e.Size
		}
		excludedRows = append(excludedRows, row)
	}
	pendingRows := rows
	// 「将处理」段按 Sort 重排；「不会处理」段保持勾选原序（它只是备注，
	// 给它排序只会让人怀疑"排除也有优先级"——处理策略没有优先级，见 keep.go）。
	switch q.Sort {
	case "size":
		sort.SliceStable(pendingRows, func(i, j int) bool {
			if pendingRows[i].Size != pendingRows[j].Size {
				return pendingRows[i].Size > pendingRows[j].Size
			}
			if pendingRows[i].GroupID != pendingRows[j].GroupID {
				return pendingRows[i].GroupID < pendingRows[j].GroupID
			}
			return pendingRows[i].ID < pendingRows[j].ID
		})
	case "path":
		sort.SliceStable(pendingRows, func(i, j int) bool {
			if pendingRows[i].Path != pendingRows[j].Path {
				return pendingRows[i].Path < pendingRows[j].Path
			}
			return pendingRows[i].ID < pendingRows[j].ID
		})
	default: // group：结果集组序 → 组内成员序 → ID 兜底（三键全序，翻页必稳定）
		sort.SliceStable(pendingRows, func(i, j int) bool {
			pi, pj := pos[pendingRows[i].ID], pos[pendingRows[j].ID]
			if pi.grpIdx != pj.grpIdx {
				return pi.grpIdx < pj.grpIdx
			}
			if pi.fileIdx != pj.fileIdx {
				return pi.fileIdx < pj.fileIdx
			}
			return pendingRows[i].ID < pendingRows[j].ID
		})
	}
	all := append(pendingRows, excludedRows...)
	total := len(all)

	page := PendingPage{
		Total: total, Page: q.Page, PageSize: q.PageSize,
		PendingCount: len(pendingRows), KeepCount: keepN, OutsideCount: outsideN,
		GoneCount: goneN, ExcludedCount: excludedN,
		Rows: []PendingRow{},
	}
	// M10a 同形的溢出安全起点：Page/PageSize 是绑定层入参，乘积回绕会把
	// "远在集合外"读成合法页或直接 panic。溢出只有一种含义——空页但计数照给。
	if q.Page > 0 && q.PageSize > math.MaxInt/q.Page {
		return page, nil
	}
	start := q.Page * q.PageSize
	if start >= total {
		return page, nil
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	page.Rows = all[start:end]
	return page, nil
}

// resultSuperseded 报告 myGen 这一代结果集是否已被更新的任务取走
// （2026-09-18 审查 C7）。收尾 goroutine 据此弃写、弃发终止事件。
func (a *App) resultSuperseded(myGen uint64) bool {
	return myGen != a.resultGen.Load()
}

// authSnapshotLocked 取授权目录集合的不可变拷贝。**调用方须持 a.mu**。
func (a *App) authSnapshotLocked() []string {
	roots := make([]string, 0, len(a.authDirs))
	for dir := range a.authDirs {
		roots = append(roots, dir)
	}
	return roots
}

// authorizeDir 登记授权目录（Clean 绝对路径；符号链接解析变体一并登记）。
func (a *App) authorizeDir(dir string) {
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authDirs == nil {
		a.authDirs = make(map[string]bool)
	}
	a.authDirs[abs] = true
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		a.authDirs[filepath.Clean(ev)] = true
	}
}

// moveTargetAllowed 校验 move 目标：必须位于某个授权目录内（含授权目录本身）。
//
// AS-H3（2026-09-20 全仓审计）：修正前把 `abs` 与 `EvalSymlinks(abs)` 并列成候选，
// **任一**候选落在**任一**授权目录内即放行——与注释自述的"防经由链接逃逸"正好相反。
// 授权目录内部的一个链接（root/esc → 外部）让 abs 命中授权，解析出的越界结果被无视，
// 随后 move.go 的 MkdirAll + Rename 就经这条链接写到用户从未授权的位置。
// 现在**只认解析后的真实路径**：目标不存在时对最近的已存在祖先求解，再接回尾段。
//
// 授权侧无需再解析：authorizeDir 已把 Clean 绝对路径与其 EvalSymlinks 变体一并登记，
// 两者都是操作系统给出的同一真实大小写形态。
//
// 调用方须持 a.mu。
func (a *App) moveTargetAllowed(dir string) bool {
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	resolved, err := resolveTargetPath(abs)
	if err != nil {
		return false
	}
	for auth := range a.authDirs {
		if withinDir(resolved, auth) {
			return true
		}
	}
	return false
}

// landingGuard 生成 move 腿的**落点复审**（M204）：给实际落点全路径（含 `name_1.ext`
// 递增后的那个名字），解析成真实物理位置后问一次"还在授权树里吗"。
//
// 判据与入口那次 `moveTargetAllowed` **同一份**（`resolveTargetPath` + `withinDir`
// 都只有一处实现——AS-H6 一族点名的形状是"两处实现必然漂移"），差别只在问的对象：
// 入口问的是用户填的名义目录，这里问的是即将写入的那个具体位置。
//
// ★ fail-closed：解析不动即拒绝（"解析不了就先放行"正是 AS-H3 那笔修掉的形状）。
// 返回的闭包不触碰 `a` 的任何字段，只捕获 `roots` 切片 ⇒ 执行器在锁外调用安全。
func (a *App) landingGuard(roots []string) func(string) error {
	return func(landing string) error {
		resolved, err := resolveTargetPath(landing)
		if err != nil {
			return fmt.Errorf("移动目标在操作期间已不可解析，已拒绝移动（源文件未改动）：%s", landing)
		}
		for _, auth := range roots {
			if withinDir(resolved, auth) {
				return nil
			}
		}
		return fmt.Errorf("移动目标在操作期间被替换（链接或改名），实际落点已不在授权目录内，"+
			"已拒绝移动且未改动源文件：%s", landing)
	}
}
