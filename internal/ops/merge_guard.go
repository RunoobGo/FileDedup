package ops

// 合并流程的「归属守卫」（M1 + M3，2026-09-21，第 7 篇 §五 1/3）。
//
// 硬链接与软链接两条合并路径共用同一段改名序列，因此守卫也共用一份
// （I5 的教训：判据在两个文件里各写一遍，早晚会只修一边）。
//
//	复核 identityStill(dup, dupID)  ✅
//	[窗口 A] rename(dup → backup)
//	[窗口 B] rename(tmp → dup)
//	终局复核（看的是 keep 与链接）  ✅
//	[窗口 C] remove(backup)
//
// 窗口 A：第三方把新文件放进 dup 位（同步盘落一个同名文件、下载器把临时文件
// 原子改名进来，都是这个时序）。此时被 rename 走的是**那个第三方文件**，
// 而后续每一步复核检查的都是 keep 与链接，全部成立，最后
// `workTempRemove(backup)` 会把别人的文件当应用残留**永久删除**。
// 丢的还是从未授权我们处置的文件 → 见 abandonForeignBackup。
//
// 窗口 C：顶替来得更晚（在落位改名之后）。数据已经只剩链接一份，
// 但 backup 位上装的已经不是我们的备份 → 见 removeOwnBackup。
//
// 两处共同口径：宁可显式失败并说清文件在哪，也不替第三方销毁文件。
//
// M3 是同一序列的失败分支：窗口 B 改名失败时用 `_ = hardlinkRename(backup, dup)`
// 还原，还原也失败就只返回裸 err——用户数据留在 dup+".fdd-old"，而这个名字
// 扫描器按 worktemp.IsTempName 忽略，等于用户的文件在应用视角里静默消失
// → 见 rollbackAfterSwapFailure。

import (
	"errors"
	"fmt"
	"os"

	"filededup/internal/fsid"
)

// backupOwnershipStill 在 dup 已被改名为 backup 之后核对：backup 位上装的
// 仍是当初校验过的那个文件。
//
// 必须复核 **backup** 而不是 dup：改名完成后 dup 位已经不是我们的对象了
// （要么空着，要么被第三方占着），按路径查 dup 只会查到顶替者。
func backupOwnershipStill(backup string, dupID fsid.ID) bool {
	return identityStill(backup, dupID)
}

// claimSlot 认领合并要占用的一个临时名（tmp / backup）。
//
// 三档处置，方向由"错了会损失什么"决定：
//   - 槽位不存在 → 直接放行；
//   - 槽位被占且 ours() 能**正面证明**它就是我们上一次运行留下的对象 → 清掉再用
//     （这些名字与 keep/dup 同一身份，删掉的只是一个名字，不丢任何字节）；
//   - 证明不了 → 显式失败，一个字节都不碰。
//
// M6（2026-09-20 全仓审计 §五 6）：这里原先是两行无条件
// `_ = os.Remove(tmp)` / `_ = os.Remove(backup)`——真以 .fdd-tmp/.fdd-old 结尾的
// **用户**文件会被当成应用残留永久删除，而且这类名字扫描器还会忽略，删完连痕迹
// 都找不到（用户只会觉得"文件自己没了"）。
//
// 残余窗口（已知、不可消除）：Lstat 与后面的改名之间第三方恰好新建同名文件。
// Go 没有 O_EXCL 语义的改名原语（os.Rename 一律替换），所以这里不写无法证伪的
// 守卫，只把"操作开始前就存在的文件"这一半确定性地保住。
func claimSlot(path string, ours func() bool) error {
	// 只做存在性判定；归属取证交给各路径的 ours()。
	// 目录槽位天然过不了两种取证（身份与文件不同、也不是链接），于是走到
	// "显式失败"分支——这正是想要的：绝不递归清空一个陌生目录。
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("无法确认 %s 是否空闲，已放弃本次操作: %w", path, err)
	}
	if ours != nil && ours() {
		if rmErr := workTempRemove(path); rmErr != nil {
			return fmt.Errorf("清理上次运行残留 %s 失败，已放弃本次操作: %w", path, rmErr)
		}
		return nil
	}
	return fmt.Errorf("%s 已存在一个不属于本次操作的文件（本应用不会删除它，已放弃本次操作）："+
		"请先把该文件改名或移走再重试", path)
}

// slotProvesHardlink 判断槽位上的对象是否与 id 同一物理文件。
//
// ★ 与 identityStill 的 fail-open **刻意相反**：identityStill 用在"要不要放行一次
// 合并"上，判错的代价是少检出一个替换；这里用在"要不要删掉一个文件"上，判错的
// 代价是永久销毁一个从未授权我们处置的文件。卷不给稳定索引（FAT/exFAT）时
// 无从证明，就按证明不了处理。
func slotProvesHardlink(path string, id fsid.ID) bool {
	return id.Resolved && identityStill(path, id)
}

// slotProvesSymlink 是软链接路径的对应取证：tmp 槽位只有"确实是指向 keep 的
// 链接"才算我们留下的。不能照搬身份比对——链接自身身份与 keep 恒不相等
// （同 SymlinkMerge 步骤 2 的口径）。
func slotProvesSymlink(keep, path string) bool {
	li, err := os.Lstat(path)
	if err != nil || li.Mode()&os.ModeSymlink == 0 {
		return false
	}
	return verifySymlinked(keep, path) == nil
}

// slotProvesUndoTemp 回撤暂存槽位（`原名.fdd-undo-tmp`）的归属取证：
// 只有**逐字节等于本次记录内容**的普通文件才算我们上一次运行写好的暂存。
//
// M199（2026-09-24，登记 §6.29，设计稿 `2026-09-24-undo-tmp-claim-slot-m199-design`）：
// undoHardlink 此前对该名字**无条件盲删**（`_ = os.Remove(tmp)`，错误被吞）——
// 第三方的同名文件会被删掉且因 worktemp 忽略规则连扫描痕迹都没有。回撤 tmp 是
// 新建副本而非硬链接，`slotProvesHardlink` 的"同 inode"证明在这里无从可用；
// 而"内容 == 记录哈希"是本格子里充分的归属证据：这份字节本来就来自 LinkSrc，
// 删掉重写不丢任何字节（源本尊还在）。
//
// 判据 fail-closed，与 `slotProvesHardlink` 的取向同理（证明不了 ⇒ 一个字节不碰，
// 代价是撕裂残留会挡回撤直到用户处置——设计稿 §4-2 记为自觉边界）：
// 记录哈希为零值（老记录无从复核）、尺寸不符、读不出/哈希不等，全部判"不是我们的"。
func slotProvesUndoTemp(path string, size uint64, h [32]byte) bool {
	if h == [32]byte{} {
		return false
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || uint64(st.Size()) != size {
		return false
	}
	got, herr := hashFile(path)
	return herr == nil && got == h
}

// abandonForeignBackup 处理「窗口 A 被第三方顶替」：放弃本次合并，
// 把第三方文件放回它能被找到的位置，清理我们的临时链接，返回错误。
//
// 归还策略按「不覆盖任何现存对象」排：
//   - dup 位空着（我们的改名把它腾空的）→ 原样 rename 回去，现场与操作前一致；
//   - dup 位又被占了，或归还改名失败 → 保持不动，只把位置报给用户。
//     这里绝不做"先删再放"：那等于我们主动删掉一个陌生文件。
//
// M48（2026-09-22 裁定，设计稿 §27.6）：「空着」原来是 Lstat 问一次、再改名上位——
// 两步之间第三方能把文件落进去，而下一次改名会把它连内容顶掉。现在改成先以
// O_EXCL 占住那个名字（claimExact）再改名：**O_EXCL 本身就是一次问 + 一次占**，
// 所以这里不再有独立的 Lstat（保留 Lstat 再叠一层 claim 是双查，不是抢占）。
func abandonForeignBackup(backup, dup, tmp string) error {
	_ = os.Remove(tmp) // 我们自己的临时链接，正常清理
	if claim, ok, _ := claimExact(dup); ok {
		fireBeforeClaimRename(dup)
		if rerr := hardlinkRename(backup, dup); rerr == nil {
			return fmt.Errorf("目标位置在校验后被第三方文件顶替，已放弃合并并把它放回原位 %s（本次未处置任何文件）", dup)
		}
		claim.release() // 改名没成，名字还给系统；backup 仍在原位，走下面那句报位置
	}
	return fmt.Errorf("目标位置在校验后被第三方文件顶替，已放弃合并；该文件现位于 %s"+
		"（不属于本次操作，扫描会自动忽略此名，请自行核对后再处置）", backup)
}

// requireOriginalInBackup 在**动手还原之前**核对：backup 位上装的仍是本次操作
// 那个原文件。是则放行；不是（或证明不了）则返回包装后的 cause，一个字节都不碰。
//
// M20（2026-09-21，设计稿 §8）：两条回滚分支原先都直接
// `hardlinkRename(backup, dup)`——把 backup 位上的**任何**东西搬回用户眼皮底下。
// backup 名（.fdd-old）扫描器按 worktemp.IsTempName 忽略，用户平时看不到它，
// 于是"顶替发生在回滚窗口里"这种时序（同步盘把新版本落到该名、用户手动把别的
// 文件移成该名）会让一个从未授权我们处置的文件被搬到 dup 位，且带着成功路径的
// 文案。回滚分支与成功路径的 removeOwnBackup 是同一类动作、同一份风险，
// 守卫自然也该同一份口径。
//
// ★ 这里的判据是 identityStill（fail-open），**刻意不同于**删文件侧的
// slotProvesHardlink（fail-closed）。理由：零值 dupID（卷不提供稳定身份）时，
// 前者放行 → 行为退回修复前，最坏是"搬错一个文件"；后者拦截 → 会把**全部**
// 零 ID 的合并回滚永久堵死（既有测试正以零 ID 覆盖这些分支）。方向由
// "判错的代价"决定：这里判错只是少检出一次顶替，不等于销毁数据。
func requireOriginalInBackup(backup, dup string, dupID fsid.ID, cause error) error {
	if backupOwnershipStill(backup, dupID) {
		return nil
	}
	return fmt.Errorf("%w；且 %s 上的文件已不是本次操作的原文件（疑似被第三方顶替）："+
		"为避免把陌生文件搬到 %s，本次未做任何还原动作，请人工核对后再处理", cause, backup, dup)
}

// rollbackUnverifiedSwap 处理「终局复核失败」：dup 上是刚放上去的**假链接**，
// backup 里是真原文件，要做的是把假的挪开、把真的放回 dup（步骤 5 的原地舞）。
//
// 舞步与原先逐字一致（move.go / symlink.go 两份重复已随本次修复收归此处，
// I5）：唯一的加固是在动手前先过 requireOriginalInBackup——被顶替时宁可
// 停在失败现场并指认位置，也不把陌生文件搬到用户眼皮底下。
func rollbackUnverifiedSwap(verifyErr error, backup, dup string, dupID fsid.ID) error {
	if err := requireOriginalInBackup(backup, dup, dupID, verifyErr); err != nil {
		return err
	}
	if rerr := hardlinkRename(dup, backup+".undo"); rerr == nil {
		if berr := hardlinkRename(backup, dup); berr != nil {
			// 还原失败：至少保证两份都存在（数据不丢），并明确指出残留位置。
			// 注意路径归属：真原文件在 backup；backup+".undo" 里是刚被
			// 挪开的**假链接**。恢复现场必须指向 backup（2026-09-20 修正：
			// 此前消息把两者说反）。
			return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失；假链接残留在 %s，可自行删除）",
				verifyErr, backup, backup+".undo")
		}
		_ = os.Remove(backup + ".undo")
		return verifyErr
	}
	return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失）", verifyErr, backup)
}

// rollbackAfterSwapFailure 处理「窗口 B 落位改名失败」：把原文件还原回 dup，
// 清理临时链接；还原也失败时**必须**把数据留在哪里说清楚（M3）。
//
// M20（2026-09-21）：还原动作同样先过 requireOriginalInBackup。被顶替时
// 仍要清掉我们的 tmp（它不指向任何用户可见名，且本次操作已放弃），
// 但绝不碰 backup 位上的陌生文件。
func rollbackAfterSwapFailure(backup, dup, tmp string, dupID fsid.ID, swapErr error) error {
	if err := requireOriginalInBackup(backup, dup, dupID, swapErr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	restoreErr := hardlinkRename(backup, dup)
	_ = os.Remove(tmp)
	if restoreErr != nil {
		return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失，"+
			"扫描会自动忽略此名，请勿删除）", swapErr, backup)
	}
	return swapErr
}

// removeOwnBackup 是合并成功后的收尾：删除原独立副本的备份。
//
// 返回 nil 表示删掉了；返回 *ResidueError 表示「合并已到位，但 backup 位上
// 还留着一个文件」——调用方按「成功 + 告警」记账，不推翻结果。两种成因：
//   - 窗口 C 的晚到顶替：那个文件已不属于本次操作，**不删**（否则替陌生人销毁文件）；
//   - 删除本身失败（Windows 上杀软/索引器持句柄时的 ERROR_SHARING_VIOLATION 很常见）。
//
// 2026-09-19（缺陷：残留 .fdd-old 污染后续扫描）：删除失败原先写作
// `_ = os.Remove(backup)` 被**静默吞掉**。此时链接虽已建立，但一份与原文件
// 逐字节相同的 .fdd-old 永久留在用户目录里；它以用户文件名开头、不以 "." 开头，
// 扫描器的隐藏跳过规则对它无效 → 下次扫描必然把它与被保留的文件配成一个
// "重复组"，用户看到的就是「明明做过硬链接合并，重扫还是有一堆重复文件」。
// 因此如实回报 + 扫描侧按 worktemp.IsTempName 忽略，形成双重兜底。
func removeOwnBackup(backup string, dupID fsid.ID) error {
	if !backupOwnershipStill(backup, dupID) {
		return &ResidueError{Path: backup, Note: fmt.Sprintf(
			"合并已完成，但 %s 在删除前被发现不属于本次操作（疑似第三方文件顶替），已保留不删；"+
				"扫描会自动忽略此名，请自行核对后再处置", backup)}
	}
	if err := workTempRemove(backup); err != nil {
		return &ResidueError{Path: backup, Err: err}
	}
	return nil
}
