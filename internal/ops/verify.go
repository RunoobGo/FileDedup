package ops

import (
	"errors"
	"os"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// Verdict 校验结论。
type Verdict int

const (
	VerdictPass    Verdict = iota // 通过（可执行操作）
	VerdictSkipped                // 文件已消失（ENOENT，S8：目标已达成）
	VerdictFailed                 // 校验失败（文件被修改，S1：拦截）

	// VerdictUnverifiable 「无从判定」（M52/OPS-11，04 §6.11）：打不开、读不了、
	// 不是普通文件——**没有任何**内容级证据说明文件变过，也没有证据说明它没变。
	// 修前这些一律折进 VerdictFailed，于是失败抽屉里写着"文件在扫描后被修改"，
	// 而实际可能只是一个 EACCES 或一个命名管道：文案把"我们查不动"说成
	// "用户改过文件"，处置建议也就跟着错（前者要修权限/换文件，后者要重扫）。
	// 拦截动作与 VerdictFailed 完全相同（都不许进 toProcess），本条只分开**表述**。
	VerdictUnverifiable
)

// VerifyFile 操作前校验（M3-T02，01 §9）：
//   - ENOENT → Skipped（不计失败、不计释放空间）
//   - size 与扫描时不一致 / 内容哈希不一致 → Failed（组内成员 size 恒等，尺寸变了即非原物）
//   - 打不开（非 ENOENT）、fstat 失败、非普通文件、重算时 I/O 错 → Unverifiable
//     （M52：这三类是"无从判定"，处置与 Failed 相同但不许共用"被修改"那句话）
//   - 其余情况一律经同一文件句柄重算 BLAKE3 与组哈希比对：相同通过，不同 Failed
//
// P0-3：修正前存在「size+mtime 双一致 → 免重算」的快速路径，隐含前提是
// 「原地改写内容必然推进 mtime」。实测该前提不成立（粗粒度卷/同刻度原地
// 改写），S1 可被完全绕过。正确性只能建立在内容比对上。
//
// H2：校验改为「open → fstat → 经 fd 哈希」全程绑 inode。修正前用
// os.Stat（跟随符号链接）+ 按路径重开哈希，校验对象与随后被删除/替换的
// 对象之间可以隔着一次 rename。现在返回通过校验那一刻的 fsid.ID，
// 执行器在破坏性动作前用它复核路径仍指向同一 inode（见 identityStill）。
//
// AS-H1（2026-09-20 全仓审计）：参照身份取自 **FromFile(f)（句柄查询）** 而非
// FromFileInfo(st)。Windows 的 fromInfo 恒返回未解析 ID（卷号与文件索引只在
// BY_HANDLE_FILE_INFORMATION 里有，Lstat 产物拿不到），于是修正前本函数在
// Windows 上恒返回零值 ID → identityStill 开头即放行 → 执行器五处破坏性动作前
// 复核与 symlink 的 keepID 守卫**全部空转**。句柄查询在两个平台都解析，
// 且 f 已在此处打开，零额外开销。仅当卷不提供稳定索引（FAT/exFAT）时
// 才返回未解析，由 identityStill 的放行分支接住。
func VerifyFile(e *model.FileEntry, groupHash [32]byte, pool *hasher.Pool) (Verdict, fsid.ID) {
	f, err := os.Open(e.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return VerdictSkipped, fsid.ID{}
		}
		// M52：EACCES/EBUSY/悬空链接一类"打不开"。这里**没有**任何内容级证据，
		// 说"被修改"是把我们的无能为力写成用户的行为。
		return VerdictUnverifiable, fsid.ID{}
	}
	defer f.Close()
	st, err := f.Stat() // fstat：身份与内容出自同一 inode
	if err != nil || !st.Mode().IsRegular() {
		// M52：stat 失败与"不是普通文件"（目录/FIFO/字符设备）同属无从判定。
		// 修前这两件与 size 不符挤在相邻两行里，读代码时看不出来。
		return VerdictUnverifiable, fsid.ID{}
	}
	if uint64(st.Size()) != e.Size {
		return VerdictFailed, fsid.ID{}
	}
	id := fsid.FromFile(f) // 句柄查询：AS-H1，Windows 上唯一解析得动的口径
	buf := pool.GetStreamBuf()
	full, err := hasher.HashFull(f, st.Size(), buf)
	pool.PutStreamBuf(buf)
	if err != nil {
		return VerdictUnverifiable, fsid.ID{} // M52：I/O 错同样无从判定
	}
	if full == groupHash {
		// 内容未变（例如仅 touch/chmod）。注意：不回写 e.ModTime/e.Size——
		// FileEntry 为结果集共享对象，无锁写入会与持锁读取方构成数据竞争；
		// 正确性优先
		return VerdictPass, id
	}
	return VerdictFailed, fsid.ID{}
}

// pathIdentity 取路径身份，供「手上只有路径、还没开文件」的动作在动手前留底
// （如跨卷复制的删源前复核，AS-H4）。
//
// 用 FromPathNoFollow 而不是 os.Open + FromFile：与复核侧 identityStill 的
// 取身份口径**完全一致**，否则「取底跟随链接、复核不跟随」会在链接对象上
// 恒判否；且它只需属性读权限，不因数据读权限被拒而误报不可达（见 fsid_windows 注释）。
// 同样不走 FromFileInfo(Lstat(...))——Windows 上那条恒未解析，复核会平凡通过（AS-H1）。
func pathIdentity(path string) (fsid.ID, error) {
	return fsid.FromPathNoFollow(path)
}

// fsidFromPathFn 是"读一个路径的当前身份"的包级接缝，**仅供测试注入读不动的情形**
// （M114：EACCES / EIO 这类"不是不存在、而是读不出"的归因在本包无法用真文件系统造出来
// ——无 root、无法可靠造出一个 Lstat 会失败而文件确实在的位置）。
// 生产路径就是 fsid.FromPathNoFollow 本身，接一层不改任何语义。
var fsidFromPathFn = fsid.FromPathNoFollow

// identityVerdict 是"动手前再读一次身份"的四种归因（M114/OPS-13b，设计段 §23.3）。
//
// 为什么要有第四格：判据自 M54 起就已经是三路分发（放行 / 已消失 / 被顶替），
// 但形状是 `(still, gone bool, bool)`——两个布尔只有三种合法组合，
// 而 `identityStatus` 的 `(false, false)` 实际由**两条**不同的来路产生：
//   - 读身份报错且不是 ENOENT（父目录 EACCES、断线卷 EIO/ESTALE、路径形状 ENOTDIR）；
//   - 原先能解析、现在解析不出（`cur.Resolved` 为假）。
//
// 两条都是"我们不知道"，调用方却只能说"文件在扫描后被替换（inode 已变化）"——
// 那是只有真看到另一个 inode 时才允许说的话。拦下（fail-closed）从来不是争议点，
// 争议在**说法**：用户据此去找一个根本不存在的"新文件"，而该做的动作是重扫或查挂载。
type identityVerdict int

const (
	// vSame 路径仍指向记录时那个对象。**包含**"记录里的身份本就未解析"那一格
	// （FAT/exFAT 不给稳定索引）：那一格的处置与"确认同一个"完全相同（放行），
	// 硬拆成两格会让每个调用点都要决定"未解析算放行还是算拦下"，而 M114 要修的只有文案。
	vSame identityVerdict = iota
	// vReplaced 确证被换成另一个对象（两侧都可解析且不同）⇒ 必须拦。
	vReplaced
	// vGone 这个位置上的对象已经没了（ENOENT）⇒ 目标已达成，记 Skipped（M54）。
	vGone
	// vUnknown 无从判定（读不动 / 原先能解析现在不能）⇒ 拦下，但**不得**说成被替换。
	vUnknown
)

// identityCheck 是身份复核的判据本体，把归因一次说全，附带一句可展示的因由。
// why 只在 vUnknown 时非空（调用方直接拼进文案），其余格给空串。
func identityCheck(path string, id fsid.ID) (identityVerdict, string) {
	if !id.Resolved {
		return vSame, "" // 见 vSame 的注释：未解析按放行处理，与"确认同一个"同格
	}
	cur, err := fsidFromPathFn(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return vGone, ""
		}
		return vUnknown, err.Error()
	}
	if !cur.Resolved {
		return vUnknown, "原先能解析身份，现在解析不出（卷行为异常，或路径已被换成不提供稳定索引的对象）"
	}
	if cur.SameIdentity(id) {
		return vSame, ""
	}
	return vReplaced, ""
}

// identityGuardSentence 把"破坏性动作前的身份复核没过"这句话定成分格的说法。
// 空串 = 复核通过（vSame），调用方继续；非空即拦截理由（调用方自行清理 tmp 后返回错误）。
//
// 为什么要有这个 helper（M114 残半 a，第 3 轮 §24.3.3）：HardlinkMerge 与 SymlinkMerge
// 各留了两道 `if !identityStill(...) { 说"被替换" }`，用的正是 M54/M114 在 executor
// 那六处已经换掉的两布尔视图 ⇒ "读不动身份 / 原先能解析现在解析不出"被说成
// "在校验后被替换（inode 已变化）"，宣称看到过一个并不存在的新对象。
// 处置方向一个字没改（三格全部拦下、全部 fail-closed），改的只有说法。
//
// ★ vReplaced 的文本与改前**逐字节相同**：既有文案、既有测试、用户可见措辞都不受影响。
// subject 是这份东西在用户话术里的名字（"保留源" / "目标文件"）。
func identityGuardSentence(subject, path string, id fsid.ID) string {
	v, why := identityCheck(path, id)
	switch v {
	case vSame:
		return ""
	case vReplaced:
		return subject + "在校验后被替换（inode 已变化），已拦截（S1）"
	case vGone:
		return subject + "已消失，无法合并（S1 拦截）"
	default:
		// 与 executor.go 的 guardIdentity 同形：写成 default 而非 case vUnknown，
		// 将来新增归因时的默认落点是"说不准"，不是"说死了"。
		return "无法确认" + subject + "仍是校验时那个对象（" + why + "），已拦截（S1）"
	}
}

// identityStill 复核 path 当前指向的物理文件仍是 id 记录的那一个。
// 不跟随符号链接：路径被换成链接/目录/另一文件时，身份必不同。
//
// 2026-09-19（关联隐患修复）：修正前直接 FromFileInfo(Lstat(...))。在 Windows 上
// 这条链恒返回未解析 ID，于是本函数**恒返回 true**——"扫描后文件被替换"
// 这重拦截在 Windows 上完全不存在。改为 fsid.FromPathNoFollow：
// unix 等价 lstat，Windows 走 CreateFileW(+OPEN_REPARSE_POINT) 的句柄查询，
// 两个平台都拿得到真实身份，且都不跟随链接。
//
// 仍保留 id 未解析时放行：那是"我们本来就不知道原身份"（例如 FAT/exFAT
// 不提供稳定索引），此时无从比对，强行判否会让这些卷上完全无法操作。
// 内容级证据（VerifyFile）仍是这些平台上的实际防线。
func identityStill(path string, id fsid.ID) bool {
	still, _ := identityStatus(path, id)
	return still
}

// identityStatus 是 identityCheck 的**两布尔视图**（M54/OPS-13 的形状，04 §6.11）。
//
// 修前 identityStill 的 `err != nil → false` 把两件事压成一件：
//   - 用户（或另一个程序）自己把 dup 删了 ⇒ 目标其实**已达成**，与 VerifyFile
//     返回 VerdictSkipped、以及 delete 分支里 os.Remove 撞 ENOENT 是同一形状，
//     那两条都记 Skipped，唯独这里记 Failed "已被替换、已拦截"；
//   - 路径被 rename 换成另一个 inode ⇒ 必须拦截，放行就是错删第三方文件。
//
// 混在一起的后果不只是文案难看：Skipped 与 OK 同路进 app.go 的 gone 集合
// （app.go:1898-1901）去清结果集与 byID，记 Failed 则留下一个盘上已不存在的路径。
//
// gone 的判据用 errors.Is(err, os.ErrNotExist)：unix 腿（fsid_unix.go:27）返回
// os.Lstat 的 *PathError，Windows 腿（fsid_windows.go:128）返回 syscall.Errno，
// 两侧该判据都成立（Errno 自带 Is）⇒ 一个纯函数跨平台，不需要真机。
//
// ★ M114 之后本视图只剩两格，而且这两格在生产里**只有一个读者**：identityStill
// （下面 :166 那处）拿 still 而把 gone 丢掉 ⇒ 生产链路上没有任何地方消费 `gone`，
// 它 presently 只被 verify_m52_m54_test.go 直接调用着（M54 的判据是从这个视图上钉的）。
// 这一位是"注释承诺 > 实现"的形状，登记而不顺手删（删它要连带搬 M54 的断言，属独立改动面）。
//
// 原先这段写的是"executor 那六处改直读 identityCheck，其余十处继续用 identityStill"
// ——两处都对不上现在的码（M114-b 本轮校正）：
//   - executor 那六处不是直读，而是共用 executor.go:371 的 guardIdentity（它读 identityCheck）；
//   - 2026-09-22（M114 残半 a）之后，HardlinkMerge 与 SymlinkMerge 的四道守卫也不再
//     用本视图，它们改走 identityGuardSentence；剩下的 identityStill 用户是
//     move.go:72（跨卷删源前）、undo.go:173（回撤回填前）、symlink.go:117（步骤 5 终局复核）、
//     claimedDst.stillOurs 与 merge_guard.go:44/:90/:133 —— 各自的"消失"处置语义并不相同，
//     其中 merge_guard.go:90 是**刻意**的 fail-closed，与 identityStill 的 fail-open 相反。
func identityStatus(path string, id fsid.ID) (still bool, gone bool) {
	v, _ := identityCheck(path, id)
	return v == vSame, v == vGone
}
