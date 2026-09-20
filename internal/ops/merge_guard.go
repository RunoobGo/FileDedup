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

// abandonForeignBackup 处理「窗口 A 被第三方顶替」：放弃本次合并，
// 把第三方文件放回它能被找到的位置，清理我们的临时链接，返回错误。
//
// 归还策略按「不覆盖任何现存对象」排：
//   - dup 位空着（我们的改名把它腾空的）→ 原样 rename 回去，现场与操作前一致；
//   - dup 位又被占了，或归还改名失败 → 保持不动，只把位置报给用户。
//     这里绝不做"先删再放"：那等于我们主动删掉一个陌生文件。
func abandonForeignBackup(backup, dup, tmp string) error {
	_ = os.Remove(tmp) // 我们自己的临时链接，正常清理
	if _, err := os.Lstat(dup); errors.Is(err, os.ErrNotExist) {
		if rerr := hardlinkRename(backup, dup); rerr == nil {
			return fmt.Errorf("目标位置在校验后被第三方文件顶替，已放弃合并并把它放回原位 %s（本次未处置任何文件）", dup)
		}
	}
	return fmt.Errorf("目标位置在校验后被第三方文件顶替，已放弃合并；该文件现位于 %s"+
		"（不属于本次操作，扫描会自动忽略此名，请自行核对后再处置）", backup)
}

// rollbackAfterSwapFailure 处理「窗口 B 落位改名失败」：把原文件还原回 dup，
// 清理临时链接；还原也失败时**必须**把数据留在哪里说清楚（M3）。
func rollbackAfterSwapFailure(backup, dup, tmp string, swapErr error) error {
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
