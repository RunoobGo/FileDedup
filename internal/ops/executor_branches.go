package ops

// M337（2026-09-28 第七轮审查批）：`Execute` 五种操作的分支体。
//
// 背景：`Execute` 曾是约 754 行的单体函数（现 `executor.go:193` 起，五个 `case`
// 的完整分支都在函数体内）。本文件把五个分支**整体搬**成 `execScope` 的方法，
// `Execute` 只留"定位 → 校验 → 分发 → 收口"。
//
// ★ 这是**纯结构改动**：分支内的语句、注释、判据分格**逐字未动**，只是把外层
// 闭包变量改成 `s.xxx`。验证它的判据是 `executor_behavior_lock_test.go`
// （提取前的行为锁）必须**逐字仍绿**。
//
// 为什么用 `execScope` 承载闭包而不是把它们也一并提升为方法：
// `settle`/`guardIdentity`/`guardContent`/`onPanic` 四个闭包各自依赖 Execute 内的
// `outcomes`/`settled`/`procIDs`/`emitItem`/`report` 等局部量，把它们一并提升
// 会让这次改动的**形状**从"搬分支"变成"重写执行器"——那是另一种风险量级。
// 本批只搬分支，闭包原样从 Execute 传入；后续若要再收一层，可在本结构上继续，
// 不必回头推翻本批。

import (
	"context"
	"errors"
	"fmt"
	"os"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// execScope 五个分支共用的执行上下文。
//
// 前四个字段是 Execute 内定义的**闭包**（不是方法）：它们的定义与注释仍在
// `executor.go`，本文件只持有引用，避免两处各写一份造成口径漂移。
type execScope struct {
	ctx     context.Context
	opts    Options
	op      model.OpRequest
	pool    *hasher.Pool
	trashFn func([]string) (map[string]string, error)

	toProcess   []*model.FileEntry
	procIDs     []fsid.ID
	hashByID    map[uint64][32]byte
	keepSrcByID map[uint64]*model.FileEntry // 冗余 ID → 组保留者

	settle        func(i int, o outcome)
	guardIdentity func(i int, path string) bool
	guardContent  func(i int, e *model.FileEntry) (bool, fsid.ID)
	onPanic       func(i int, r any)
}

// execTrash 移入回收站。
func (s *execScope) execTrash() {
	ctx := s.ctx
	toProcess := s.toProcess
	// H2：入站前复核身份。校验（读内容）与回收站（动路径）之间，
	// 路径可能被换成另一 inode——彼时删的是"第三方文件"，必须拦截。
	usable := make([]int, 0, len(toProcess))
	for i, e := range toProcess {
		if s.guardIdentity(i, e.Path) { // H2 复核；M54 把"已消失"与"被替换"分开处置
			continue
		}
		// trash 同样**不接** M91 内容复核（理由与下面 move 那一格同一条，§28.4）：
		// 入回收站是一次改名/搬运，dup 的原内容还在回收站里，销毁不发生在这里。
		usable = append(usable, i)
	}
	paths := make([]string, 0, len(usable))
	for _, i := range usable {
		paths = append(paths, toProcess[i].Path)
	}
	// 已取消则整批不派发（outcome 保持 ocNone → aggregate 归入 Cancelled）
	if len(paths) > 0 && ctx.Err() == nil {
		if dstMap, err := s.trashFn(paths); err == nil {
			for _, i := range usable {
				s.settle(i, outcome{code: ocOK, dst: dstMap[toProcess[i].Path]})
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
			strict := s.opts.TrashVerifiesRecycle
			runIndexed(ctx, len(usable), opWorkers, func(k int) {
				i := usable[k]
				p := toProcess[i].Path
				if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
					if d, ok := known[p]; ok {
						s.settle(i, outcome{code: ocOK, dst: d})
						return
					}
					if !strict {
						s.settle(i, outcome{code: ocSkipped})
						return
					}
					// 源已消失，但平台有能力验证却未报告任何落点 →
					// 无法证明进了回收站。这正是「疑似被直接删除」的形状，
					// 绝不能记成 Skipped。
					s.settle(i, outcome{code: ocFailed, stage: "trash",
						err: "文件已从原路径消失，但未能确认其进入回收站；" +
							"可能已被直接删除，请立即到回收站核实" +
							"（若不在回收站，请停止后续操作并考虑用数据恢复工具找回）"})
					return
				}
				// OPS-7（2026-09-21 全量审查）：pre-pass 的 identityStill 与这次
				// 单独派发之间隔着**整批 trash 的 I/O** + 逐个分支的并发排队，
				// 是全仓唯一"复核不紧贴动作"的时序（delete/hardlink/symlink/move
				// 都在动手前一行复核）。批量失败到逐个重试这段延迟不是零，
				// 窗口内第三方顶替后，我们照旧把顶替者派进回收站。
				// ★ M316（OPS-44，2026-09-28）复核这半句时的现读更正：move 的
				// "动手前一行"当时只在**跨卷腿**成立，同卷快路径的复核点离 rename
				// 隔着 MkdirAll + claimDst；同批已在 moveFileDetailed 里补上紧贴
				// renameFile 的源复核（接缝 preRenameRecheck），这半句至此全对得上。
				if s.guardIdentity(i, p) { // OPS-7：紧贴动作再复核一次
					return
				}
				if m, err := s.trashFn([]string{p}); err != nil {
					// ★ M279 ③：stage 必须与上面那条 strict 臂同源。修前这里不给 stage，
					// 聚合时落成 "ops"，而同一批的假警报是 "trash" ⇒ 同一抽屉里
					// 同类操作被分成两种来源标签，用户按来源筛就漏一半。
					s.settle(i, outcome{code: ocFailed, stage: "trash", err: err.Error()})
				} else {
					s.settle(i, outcome{code: ocOK, dst: m[p]})
				}
			}, func(k int, r any) { // 下标经 usable 映射回 toProcess
				s.onPanic(usable[k], r)
			})
		}
	}
}

// execDelete 永久删除。
func (s *execScope) execDelete() {
	runIndexed(s.ctx, len(s.toProcess), opWorkers, func(i int) {
		e := s.toProcess[i]
		if s.guardIdentity(i, e.Path) { // H2 复核；M54 分处置
			return
		}
		// M91：删除会把这份内容永久销毁，身份对了不等于内容还是扫到的那份。
		handled, vid := s.guardContent(i, e)
		if handled {
			return
		}
		// R1-1：内容复核通过到 os.Remove 之间隔着一次**全量重算**（大文件秒到分钟级），
		// 而上面的 guardIdentity 读的是更早的校验循环那次身份。哈希绑 fd、删除按路径，
		// 这一段窗口里被 rename 顶替时两侧判据都点头——本道复核把"我删的就是我刚哈希那份"
		// 钉成一句可执行的话。处置分格与 guardIdentity 逐条一致（M54/M114 的同一把尺子）；
		// 未解析身份（FAT/exFAT）由 identityCheck 的放行分支接住，与那道复核同口径。
		if v, why := identityCheck(e.Path, vid); v != vSame {
			switch v {
			case vGone:
				s.settle(i, outcome{code: ocSkipped})
			case vReplaced:
				s.settle(i, outcome{code: ocFailed, stage: "verify",
					err: "内容复核通过后、删除前文件被替换（inode 已变化），已拦截：盘上未做任何改动"})
			default:
				// 与 guardIdentity 同样是 default 而非 case vUnknown：新增归因时忘了登记，
				// 后果是"被当成无从判定拦下"（安全），而不是"宣称看到过一个不存在的新对象"。
				s.settle(i, outcome{code: ocFailed, stage: "verify",
					err: fmt.Sprintf("内容复核通过后无法确认文件仍是那个对象（%s），已拦截：盘上未做任何改动", why)})
			}
			return
		}
		if err := os.Remove(e.Path); err != nil {
			if os.IsNotExist(err) {
				s.settle(i, outcome{code: ocSkipped})
			} else {
				s.settle(i, outcome{code: ocFailed, err: err.Error()})
			}
			return
		}
		s.settle(i, outcome{code: ocOK})
	}, s.onPanic)
}

// execMove 移动到目标目录。
func (s *execScope) execMove() {
	// 串行。历史上这里的理由是「uniqueDst 先查后用，两个 worker 会挑到同一目标名，
	// 后写者覆盖前者」（M2）；该缺陷已随 claimDst 的原子抢占修掉，串行不再是为了
	// 正确性，只是为了保留逐项取消检查与既有落位顺序。放开并发属独立优化，本批不做。
	for i, e := range s.toProcess {
		if s.ctx.Err() != nil {
			break // move 串行执行，取消后立即停止派发（P2）
		}
		if s.guardIdentity(i, e.Path) { // H2 复核；M54 分处置
			continue
		}
		// ★ 这里**不接** M91 的内容复核，是设计裁定而不是漏（§28.4）：move 与 trash 都把
		// 文件整体搬走、内容不丢，扫到的哈希对不上也不会销毁任何东西。把它们纳进来
		// 就是拿两倍的读盘换零收益。（delete/hardlink/symlink 三条腿会销毁 dup 原内容，
		// 那边才接。别"顺手补一致"。）
		dst, crossVol, err := moveFileDetailed(e.Path, s.op.TargetDir, s.opts.MoveLandingAllowed)
		switch {
		case err != nil:
			// M89（§24.3.1）：部分成功（MoveFile 已复制出副本、只是没删成源）
			// 原先在这里把 dst 丢掉，只留下错误文本 ⇒ 那份多出来的副本既不进
			// 账本也不可在历史页看到。改后 dst 随 outcome 一起交下去，
			// 文本仍由 DestHint 兜一层（move.go 两条腿本来就自带落点，
			// 所以这里是幂等的 no-op）。
			s.settle(i, outcome{code: ocFailed, dst: dst, err: DestHint(err.Error(), dst)})
		default:
			// M40（§24.3.2）：只有**跨卷**那份数据真的离开了源卷才进 Reclaimed
			// （见下面汇总处的 switch）。同卷 move 是一次改名，磁盘总量一分未减，
			// 修正前却与 delete 共用 default 落进 Reclaimed——09-19 硬链接、
			// 09-21 回收站两笔假账的第三个同型。
			s.settle(i, outcome{code: ocOK, dst: dst, crossVol: crossVol})
		}
	}
}

// execHardlink 硬链接合并。
func (s *execScope) execHardlink() {
	runIndexed(s.ctx, len(s.toProcess), opWorkers, func(i int) {
		e := s.toProcess[i]
		src := s.keepSrcByID[e.ID]
		if src == nil || src.ID == e.ID {
			s.settle(i, outcome{code: ocFailed, err: "未找到组内保留源"})
			return
		}
		// H2：dup 在动作前仍须指向校验过的那份内容
		if s.guardIdentity(i, e.Path) { // H2 复核；M54 分处置
			return
		}
		// M91：dup 侧同样要内容级复核——HardlinkMerge 会用源覆盖 dup 位置上的
		// 那份数据，就地改写过的 dup（同一个 inode）光靠身份复核挡不住。
		// vid 不消费（R1-1 只补 delete 这一格）：真正销毁数据的改名在 HardlinkMerge 内部，
		// 它自己在 rename 前一行跑 identityGuardSentence（move.go:125），已经紧贴动作。
		if handled, _ := s.guardContent(i, e); handled {
			return
		}
		// S1 扩展：keep 源也须校验——源在扫描后被篡改时硬链接会用新内容
		// 覆盖 dup 的原内容（不可逆），必须拦截。
		// P0-3：校验为内容级（见 VerifyFile），时间戳未变不再放行。
		// H2：源身份一并下传，HardlinkMerge 在建立临时链接后复核其
		// inode 仍是校验时那一个（防 verify→act 窗口内源被替换）。
		// 注：同组多个 dup 共享同一 keep 源，校验为只读操作，并发安全。
		v, srcID := VerifyFile(src, s.hashByID[src.ID], s.pool)
		switch v {
		case VerdictSkipped:
			s.settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源已消失，无法合并（S1 拦截）"})
			return
		case VerdictFailed:
			s.settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源在扫描后被修改，已拦截（S1）"})
			return
		case VerdictUnverifiable:
			// M52：与 dup 侧同一分开口径（处置都是拦截，话不能说成"被改过"）。
			s.settle(i, outcome{code: ocFailed, stage: "verify",
				err: "保留源无从校验（打不开、读不了或不是普通文件），已拦截（S1）"})
			return
		case VerdictPass:
			// 通过：显式列出来，好让下面那条 default 真的是"未知"兜底。
			// 少了这一格，VerdictPass 自己就会掉进 default ⇒ 合并全被拦死。
		default:
			// M52 加固腿：这两个 switch 修前**没有** default，新增枚举值会静默
			// 什么都不做地往下走去改文件——与 :239 那道循环同一个坑。
			s.settle(i, outcome{code: ocFailed, stage: "verify",
				err: fmt.Sprintf("保留源校验返回未知结论（值 %d），已拦截", int(v))})
			return
		}
		// HardlinkMerge 使用「dup 路径 + .fdd-tmp」临时名，路径互不冲突 → 并发安全
		if err := HardlinkMerge(src.Path, e.Path, srcID, s.procIDs[i]); err != nil {
			// 2026-09-19：区分「真失败」与「已成功但有临时文件残留」。
			// 后者链接已经建好，若计为失败，用户会以为合并没生效，进而
			// 反复重试——而重试时 dup 与 keep 已是同一文件，行为更费解。
			var residue *ResidueError
			if errors.As(err, &residue) {
				s.settle(i, outcome{code: ocOK, linkSrc: src.Path, warn: residue.Error()})
			} else {
				s.settle(i, outcome{code: ocFailed, err: err.Error()})
			}
		} else {
			s.settle(i, outcome{code: ocOK, linkSrc: src.Path})
		}
	}, s.onPanic)
}

// execSymlink 软链接合并（跨卷）。
func (s *execScope) execSymlink() {
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
	runIndexed(s.ctx, len(s.toProcess), opWorkers, func(i int) {
		e := s.toProcess[i]
		src := s.keepSrcByID[e.ID]
		if src == nil || src.ID == e.ID {
			s.settle(i, outcome{code: ocFailed, err: "未找到组内保留源"})
			return
		}
		// H2：dup 在动作前仍须指向校验过的那份内容
		if s.guardIdentity(i, e.Path) { // H2 复核；M54 分处置
			return
		}
		// M91：与 hardlink 同一条腿——SymlinkMerge 会删掉 dup 位置上的原数据再放链接，
		// 那份数据如果已被就地改写，就没有任何东西能把它找回来。
		// vid 不消费的理由与上面 hardlink 那处相同：SymlinkMerge 在改名前一行自己有
		// identityGuardSentence（symlink.go:84）。
		if handled, _ := s.guardContent(i, e); handled {
			return
		}
		// S1 扩展：keep 源也须内容级校验（源被篡改时链接会指向被改过的
		// 数据，用户以为"还是原来那份"——同样不可逆，必须拦截）。
		v, srcID := VerifyFile(src, s.hashByID[src.ID], s.pool)
		switch v {
		case VerdictSkipped:
			s.settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源已消失，无法合并（S1 拦截）"})
			return
		case VerdictFailed:
			s.settle(i, outcome{code: ocFailed, stage: "verify", err: "保留源在扫描后被修改，已拦截（S1）"})
			return
		case VerdictUnverifiable:
			// M52：与 dup 侧同一分开口径（处置都是拦截，话不能说成"被改过"）。
			s.settle(i, outcome{code: ocFailed, stage: "verify",
				err: "保留源无从校验（打不开、读不了或不是普通文件），已拦截（S1）"})
			return
		case VerdictPass:
			// 通过：显式列出来，好让下面那条 default 真的是"未知"兜底。
			// 少了这一格，VerdictPass 自己就会掉进 default ⇒ 合并全被拦死。
		default:
			// M52 加固腿：这两个 switch 修前**没有** default，新增枚举值会静默
			// 什么都不做地往下走去改文件——与 :239 那道循环同一个坑。
			s.settle(i, outcome{code: ocFailed, stage: "verify",
				err: fmt.Sprintf("保留源校验返回未知结论（值 %d），已拦截", int(v))})
			return
		}
		warn := ""
		if sameVolume(src.Path, e.Path) {
			warn = "这两个文件在同一卷上，使用「硬链接合并」更安全" +
				"（硬链接不需要管理员权限，且删除任一名字数据都还在，不会出现链接失效）"
		}
		if err := SymlinkMerge(src.Path, e.Path, srcID, s.procIDs[i]); err != nil {
			// 与 hardlink 一致：「已成功但有残留」不应判失败，否则用户
			// 会以为没生效而反复重试（重试时 dup 已是链接，行为更费解）。
			var residue *ResidueError
			if errors.As(err, &residue) {
				s.settle(i, outcome{code: ocOK, linkSrc: src.Path,
					warn: joinWarn(warn, residue.Error())})
			} else if isSymlinkNeedsPrivilege(err) {
				// 权限不足是**环境级**问题（Windows 未提权且未开开发者模式）：
				// 同一台机器上每个文件都会以同样方式失败。逐条重复长文案
				// 会让结果页被同一句话淹没，故此处只留短标记，
				// 完整指引由 AggregateWarnings 汇总成一条（见本文件尾部）。
				s.settle(i, outcome{code: ocFailed, stage: symlinkPrivilegeStage, err: err.Error()})
			} else {
				s.settle(i, outcome{code: ocFailed, err: err.Error()})
			}
		} else {
			s.settle(i, outcome{code: ocOK, linkSrc: src.Path, warn: warn})
		}
	}, s.onPanic)
}
