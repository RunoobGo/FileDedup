package ops

import (
	"errors"
	"fmt"
	"os"

	"filededup/internal/fsid"
)

// ============================================================================
// 跨卷软链接合并（2026-09-20）
//
// 需求来源：硬链接是**文件系统级**的同卷操作，os.Link/CreateHardLinkW 跨卷必失败。
// 当重复组跨越两个卷（例如 D 盘的软件数据目录与 F 盘的备份），此前没有任何
// 「保路径 + 省空间」的手段：要么把两份都留着（不省空间），要么删掉一份
// （原路径消失，依赖该路径的程序会找不到文件）。
//
// 软链接恰好补上这个缺口：它是一个很小的独立文件，内容就是"目标路径"这个字符串，
// 打开它时由操作系统重定向到目标。因此
//   - 原路径继续可访问（满足"保路径"）；
//   - 数据只有一份，省下的是整份文件的空间；
//   - 能跨卷（满足硬链接做不到的那一半）。
//
// 代价必须说清楚（UI 也照此提示，见 frontend/src/views/ResultView.vue）：
// 软链接是**指向路径的替身**，不是指向数据的第二个名字。保留项被删除/移动、
// 或所在磁盘被拔出，链接就指向虚空（悬空），文件"打不开"。
// 而硬链接是同一 inode 的第二个名字，删掉任何一个名字，数据都还在。
//
// 适用边界（设计文档 §1.3）：
//   同卷 → 一律硬链接（零权限门槛、有引用计数保护、无悬空风险）
//   跨卷 → 软链接（硬链接无解）
// **不把软链接做成同卷的可选项**：同卷用它只有净损失。
// ============================================================================

// SymlinkMerge 把 dup 替换为指向 keep 的符号链接。
//
// 五步安全模式与 HardlinkMerge **同构**（理由见 move.go 该函数头注释：
// 破坏性操作必须能任意步骤失败后把 dup 还原为原文件，绝不允许"先删再建"）：
//
//  1. 先建临时软链接（不动 dup，失败则原样返回）
//  2. 复核 keep 与 dup 在校验之后没被替换（压缩 verify→act 的 TOCTOU 窗口）
//  3. 把 dup 改名为备份（数据仍在磁盘上，只是暂时不在原路径）
//  4. 把临时软链接改名到 dup 路径（原子顶替）
//  5. 终局复核"链接确实指向 keep 的数据"；通过后才删除备份
//
// **步骤 2 是本函数与硬链接唯一实质性不同之处**：tmp 此刻是一个软链接，
// 对软链接必须用 verifySymlinked（解析目标后比对），不能照搬
// identityStill(tmp, keepID)——identityStill 取的是"路径位置上的对象本身"，
// 也就是链接自己的身份，与 keep 恒不相等，照搬会让**每一次合并都报失败**。
// 这是设计文档 §3.4 明确标注的最易错点，测试用例
// TestSymlinkMergeVerifiesResolvedTarget 专门钉死它。
func SymlinkMerge(keep, dup string, keepID, dupID fsid.ID) error {
	if keep == dup {
		return fmt.Errorf("同一路径")
	}

	// ---- 步骤 1：建临时软链接（指向 keep）----
	// 用 dup + 后缀 作为临时名：与用户文件名同前缀，路径唯一，
	// 并发处理同组多个 dup 时互不冲突（每个 dup 自己的后缀）。
	tmp := dup + FddTempSuffix
	// 槽位被占时先取证再决定：本路径的 tmp 是**链接**，取证方式与硬链接不同（M6）。
	if err := claimSlot(tmp, func() bool { return slotProvesSymlink(keep, tmp) }); err != nil {
		return err
	}
	if err := symlinkCreate(keep, tmp); err != nil {
		// 权限不足等错误已在平台层翻译成可操作指引，直接透出。
		return err
	}

	// ---- 步骤 2：复核身份（见函数头注释：此处必须用 verifySymlinked）----
	if err := verifySymlinked(keep, tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保留源在校验后被替换，已拦截（S1）: %w", err)
	}
	// verifySymlinked 的两侧身份都沿 keep 这条路径字符串解析，恒相等——它证明
	// 的是"链接建对了"，检不出"keep 本身被换"。与 HardlinkMerge 的
	// identityStill(tmp, keepID) 同构的守卫必须由 keepID 承担（2026-09-20 审查：
	// 修正前 keepID 参数收而不用，S1 提示形同虚设）。
	if s := identityGuardSentence("保留源", keep, keepID); s != "" {
		_ = os.Remove(tmp)
		return errors.New(s)
	}
	if s := identityGuardSentence("目标文件", dup, dupID); s != "" {
		_ = os.Remove(tmp)
		return errors.New(s)
	}

	// ---- 步骤 3：备份原 dup（绝不先删）----
	backup := dup + FddOldSuffix
	// backup 位上若有不属于本次操作的对象，下面的 rename 会把它静默覆盖掉（M6）。
	// 硬链接路径的同位置守卫见 move.go。
	if err := claimSlot(backup, func() bool { return slotProvesHardlink(backup, dupID) }); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := hardlinkRename(dup, backup); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if !backupOwnershipStill(backup, dupID) {
		return abandonForeignBackup(backup, dup, tmp)
	}

	// ---- 步骤 4：临时链接原子顶替 dup 位置 ----
	if err := hardlinkRename(tmp, dup); err != nil {
		return rollbackAfterSwapFailure(backup, dup, tmp, dupID, err)
	}

	// ---- 步骤 5：终局复核 ----
	// 与硬链接同理：os 层的"没报错"不等于"链接已按预期建立"
	// （卷不支持重解析点、安全软件把链接改写/拦截等都可能悄悄发生）。
	// 证据是"解析后确为 keep 的数据"，而非"调用没报错"。
	// keepID 复核同样要做——步骤 2 与此刻之间 keep 仍可能被换；若带着被换的
	// keep 走到删备份，原始内容的最后一份副本就没了。
	step5Err := verifySymlinked(keep, dup)
	if step5Err == nil && !identityStill(keep, keepID) {
		step5Err = errors.New("保留源在合并期间被替换（inode 已变化）")
	}
	if step5Err != nil {
		// 未真正建立：把 dup 还原成原来的独立文件，不留"假成功"的账。
		// 舞步已收归 rollbackUnverifiedSwap（move/symlink 原先各一份逐字重复）。
		return rollbackUnverifiedSwap(step5Err, backup, dup, dupID)
	}

	// ---- 成功：删除备份 ----
	// 删除失败与"backup 位被第三方顶替"都不推翻结果，但必须如实回报残留
	// （理由见 removeOwnBackup 与 merge_guard.go）。
	if err := removeOwnBackup(backup, dupID); err != nil {
		return err
	}
	return nil
}

// verifySymlinked 确认 linkPath 是一个**指向 keep 所代表数据**的符号链接。
//
// 与 verifyHardlinked 的区别（关键，勿混）：
//
//	硬链接：dup 与 keep 是同一 inode → 直接比两者身份
//	软链接：dup 是**独立对象**，自身身份与目标毫无关系 → 必须先解析再比
//
// 因此本函数做两层校验：
//
//	① dup 确实是符号链接（而不是普通文件/目录/已被删除）
//	② 跟随链接取到的目标身份 == keep 的身份
//
// 第 ① 层不能省：若只做第 ② 层，一个"内容恰好与 keep 相同"的普通文件
// 也能通过（跟着它取到的是它自己的身份，与 keep 不同——这层其实会拦住）。
// 反过来，只做 ① 也不能省 ②：链接可能指向别的文件。
//
// 身份不可解析时（该卷不提供稳定索引，如 FAT/exFAT）退化为
// "目标可达 + 大小一致"，并**明确按弱校验处理**——此时宁可放过也不误杀，
// 因为内容级证据（VerifyFile 的 BLAKE3）仍是这些卷上的实际防线。
func verifySymlinked(keep, linkPath string) error {
	li, err := os.Lstat(linkPath)
	if err != nil {
		return fmt.Errorf("复核失败：无法读取 %s: %w", linkPath, err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("复核失败：%s 不是符号链接（实际模式 %v）", linkPath, li.Mode())
	}

	// 解析目标身份——**要跟随**，看的是目标本身（fsid.FromPath 而非 FromPathNoFollow）。
	tid, err := fsid.FromPath(linkPath)
	if err != nil {
		// 悬空链接、或目标被移除/所在盘已拔出。
		return fmt.Errorf("复核失败：%s 的目标不可达（悬空链接？）: %w", linkPath, err)
	}
	kid, err := fsid.FromPath(keep)
	if err != nil {
		return fmt.Errorf("复核失败：保留源 %s 不可达: %w", keep, err)
	}

	if !tid.Resolved || !kid.Resolved {
		// 弱校验兜底：至少确认"目标能打开"且"大小与保留源一致"。
		st1, e1 := os.Stat(linkPath)
		st2, e2 := os.Stat(keep)
		if e1 != nil || e2 != nil {
			return fmt.Errorf("复核失败：链接目标或保留源不可达")
		}
		if st1.Size() != st2.Size() {
			return fmt.Errorf("复核失败：链接目标大小(%d)与保留源(%d)不一致",
				st1.Size(), st2.Size())
		}
		return nil
	}
	if !tid.SameIdentity(kid) {
		return fmt.Errorf("复核失败：链接目标与保留源不是同一文件（标识不符）")
	}
	return nil
}

// SymlinkStatus 检测一个路径的链接状态：是否符号链接、是否已悬空。
//
// 供上层（app.go GetOpRecord / 历史记录页）逐条标注用。返回两个值而非一个
// 枚举，因为两者是**正交**的：普通文件 (false,false)、有效链接 (true,false)、
// 悬空链接 (true,true)。合并成一个枚举反而要在调用处再拆开。
//
// 调用约定：
//
//	① 只对 state == done 的条目调用。失败/跳过/取消的条目本就没在文件系统上
//	   动过手，原位可能是用户自己的文件——去测它没有意义，标红只会误导。
//	② 路径不存在时返回 (false,false) 而非报错：条目可能已被回撤（undone）
//	   或用户手动删掉了链接。这不是异常，前端按"没有链接"渲染即可。
//	③ **不**跟随链接判断"好坏"以外的任何事：不做自动修复。
//	   链接失效是用户环境变化（拔盘、移动保留文件）的结果，
//	   应用替用户"修好"它可能指向错误的地方——只提示，由用户决定。
//
// 与 symlinkIsDangling 的分工：后者是内部判据（只判悬空），本函数是
// 对外契约（同时给"是不是链接"），避免调用方为了拿到前者而自己在
// app 层重写一遍 Lstat 逻辑——那样两处判据会各自漂移。
func SymlinkStatus(path string) (isSymlink, dangling bool) {
	li, err := os.Lstat(path)
	if err != nil || li.Mode()&os.ModeSymlink == 0 {
		return false, false
	}
	if _, err := os.Stat(path); err != nil {
		return true, true
	}
	return true, false
}

// symlinkIsDangling 判断一个路径是否是**已悬空**的符号链接。
//
// 判据：路径自身是符号链接（Lstat 成功 + ModeSymlink），但跟随它打不开
// （fsid.FromPath / os.Stat 失败）。用于历史记录页把失效链接标红提示
// （见 app.go 的悬空检测与 frontend RecordsView.vue）。
//
// 非符号链接一律返回 false：调用方据此只对链接做提示，不误伤普通文件。
func symlinkIsDangling(path string) bool {
	li, err := os.Lstat(path)
	if err != nil || li.Mode()&os.ModeSymlink == 0 {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return true
	}
	return false
}

// symlinkCreate 创建软链接（经可注入的变量），默认直通平台实现。
//
// 为什么要这一层间接：`createSymlink` 是**平台相关**函数，测试无法在不提权的
// Windows 上制造"权限不足"这一最重要的失败场景（那正是真机上最常见的失败）。
// 有了注入点，测试可以在任意平台上确定性地模拟
// ① 权限不足（ErrSymlinkNeedsPrivilege）② 卷不支持 等失败，从而验证
// "建链接失败绝不改动 dup"这条安全性质。
//
// 与 hardlinkRename / workTempRemove 同属本包的既有惯例（见 move.go 尾部）。
//
// 注意：SymlinkMerge 内部只允许调用本函数（**不要**直接调 createSymlink），
// 否则注入点会被绕过，测试就失去约束力。
func symlinkCreate(target, linkPath string) error {
	return symlinkCreateFn(target, linkPath)
}

// symlinkCreateFn 为 symlinkCreate 的可替换实现，测试专用。
//
// 必须在测试结束时还原（defer 恢复原值），否则会污染同包内其他用例——
// 本包测试并发执行（-race -count=N），全局变量替换要求用例自身串行协调。
var symlinkCreateFn = createSymlink

// isSymlinkNeedsPrivilege 判定错误是否为"环境缺权限"（Windows 1314 等）。
//
// 上层据此在结果页给出**一次性**的引导（"以管理员身份重启"），
// 而不是为每个文件重复一条同样的长文案。
//
// ErrSymlinkNeedsPrivilege 只在 Windows 实现里被构造；unix 上创建符号链接
// 无权限门槛，该哨兵永不被命中——故此处不做平台分支。
func isSymlinkNeedsPrivilege(err error) bool {
	return errors.Is(err, ErrSymlinkNeedsPrivilege)
}
