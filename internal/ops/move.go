package ops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"filededup/internal/fsid"
)

// MoveFile 移动文件至 targetDir（M3-T04）：
//   - 同卷：os.Rename 原子完成
//   - 跨卷（EXDEV）：复制（校验 size + fsync + 还原元数据）后删源；
//     任何失败保留源文件不删（安全优先）
//   - 目标重名：O_EXCL 原子抢占 + name_1.ext 递增（见 claimDst）
//
// 返回目标完整路径。
func MoveFile(src, targetDir string) (string, error) {
	if targetDir == "" {
		return "", fmt.Errorf("未指定目标目录")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	dst, err := claimDst(targetDir, filepath.Base(src))
	if err != nil {
		return "", err
	}

	if err := renameFile(src, dst.path); err == nil {
		return dst.path, nil // 同卷快路径
	} else if !isCrossDevice(err) {
		dst.release()
		return "", fmt.Errorf("移动失败: %w", err)
	}

	// 跨卷：复制 → 校验 → 还原元数据 → 删源
	st, err := os.Stat(src)
	if err != nil {
		dst.release()
		return "", err
	}
	// AS-H4（2026-09-20 全仓审计）：复制可持续数秒到数分钟，是全部已加固点里
	// **窗口最大**的一个，而删源用的是按路径的 os.Remove——窗口内第三方以 rename
	// 顶替 src（同步盘、下载器的原子写入正是这个时序）时，删掉的是别人的新文件，
	// 账本还记 done。因此复制前经已开句柄取身份，删源前复核路径仍指向同一文件。
	srcID, err := pathIdentity(src)
	if err != nil {
		dst.release()
		return "", err
	}
	if err := copyVerifyFile(src, dst.path, st); err != nil {
		dst.release() // 清理半成品（仅当占位仍属于我们）
		return "", err
	}
	if !identityStill(src, srcID) {
		// 不删源，也不把这次算成功：两份并存交给用户核对，
		// 代价远小于替用户删掉一个他没打算删的第三方文件。
		return dst.path, fmt.Errorf("已复制到 %s，但源文件在复制期间被替换（inode 已变化）："+
			"为避免误删第三方文件**未删除源**，两份并存，请核对后自行处理其一: %s", dst.path, src)
	}
	if err := removeSrc(src); err != nil {
		return dst.path, fmt.Errorf("已复制但删除源失败（两份并存）: %w", err)
	}
	return dst.path, nil
}

// HardlinkMerge 硬链接合并（M3-T05）：冗余路径替换为指向 keep 的硬链接。
// 仅同卷可用（跨卷时 os.Link 天然失败兜底）。
//
// H2：keepID/dupID 来自 VerifyFile 通过校验那一刻的 fstat。临时硬链接建立后
// 复核 tmp 的 inode == keepID（窗口内 keep 路径被替换时，链接会指向非预期
// 文件）；替换 dup 前复核 dup 路径仍指向 dupID。零值 ID（未解析平台）跳过。
//
// 2026-09-19（缺陷：Windows 上"显示已执行但链接未生效"）：
// 此前本函数**只要有一步没报错就返回 nil**，从不确认链接真的建立了。
// 而 Windows 上 os.Link → CreateHardLinkW **仅 NTFS 支持**（MSDN 原文：
// "only supported on the NTFS file system"，且 ReFS 不支持、exFAT/FAT 完全不支持），
// 跨卷也一律失败。一旦平台/卷不支持，上层却把它记为 done 并累加"已释放空间"，
// 用户看到"执行成功"但文件夹总占用不变、改一个文件另一个不跟着变。
// 现在收尾处**强制复核**：dup 与 keep 必须真的互为同一文件（同 inode /
// 同 64 位文件索引）。对不上就回滚成原独立文件并报错——宁可显式失败，
// 也不留下"假装成功"的假账。
func HardlinkMerge(keep, dup string, keepID, dupID fsid.ID) error {
	if keep == dup {
		return fmt.Errorf("同一路径")
	}
	// 先建指向 keep 的临时硬链接，再把原 dup 备份后原子替换为硬链接。
	// 任一步失败都能把 dup 恢复为原始文件，杜绝「删 dup 后改名失败」导致的数据丢失。
	tmp := dup + FddTempSuffix
	// 槽位被占时先取证再决定：只有能证明是上次运行留下的才清（M6）。
	if err := claimSlot(tmp, func() bool { return slotProvesHardlink(tmp, keepID) }); err != nil {
		return err
	}
	if err := os.Link(keep, tmp); err != nil {
		return fmt.Errorf("硬链接失败（可能跨卷或权限）: %w", err)
	}
	if !identityStill(tmp, keepID) {
		_ = os.Remove(tmp)
		return fmt.Errorf("保留源在校验后被替换（inode 已变化），已拦截（S1）")
	}
	if !identityStill(dup, dupID) {
		_ = os.Remove(tmp)
		return fmt.Errorf("目标文件在校验后被替换（inode 已变化），已拦截（S1）")
	}
	backup := dup + FddOldSuffix
	// backup 位上若有不属于本次操作的对象，rename 会把它静默覆盖掉（M6）。
	if err := claimSlot(backup, func() bool { return slotProvesHardlink(backup, dupID) }); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := hardlinkRename(dup, backup); err != nil {
		// 无法把原 dup 移走（极罕见）：清理临时硬链接，dup 原样保留（安全）。
		_ = os.Remove(tmp)
		return err
	}
	if !backupOwnershipStill(backup, dupID) {
		return abandonForeignBackup(backup, dup, tmp)
	}
	if err := hardlinkRename(tmp, dup); err != nil {
		return rollbackAfterSwapFailure(backup, dup, tmp, dupID, err)
	}
	// 收尾复核（见函数头注释）：确认 dup 现在真的是 keep 的那个文件。
	// 这里的证据是「同文件」而非「无错误」——两者在 Windows 上不等价。
	if err := verifyHardlinked(keep, dup); err != nil {
		// 未真正建立链接：把 dup 还原为原来的独立文件，不留假成功的账。
		// 舞步已收归 rollbackUnverifiedSwap（move/symlink 原先各一份逐字重复）。
		return rollbackUnverifiedSwap(err, backup, dup, dupID)
	}
	// 合并成功：删除原独立副本的备份。删除失败与"backup 位被第三方顶替"
	// 都不推翻结果，但必须如实回报残留（理由见 removeOwnBackup 与 merge_guard.go）。
	//
	// OPS-1（2026-09-21 全量审查）：这里原先跟一句 `_ = os.Remove(tmp)`。tmp 这个
	// 名字已在上面被 hardlinkRename(tmp, dup) **消耗**，此后该路径上是谁的东西本函数
	// 一无所知——那一句删的可能是第三方刚落在这个名字上的文件，且完全没有证据支撑。
	// 软链接侧（SymlinkMerge）从一开始就没有这一行，两条腿至此对称。
	if err := removeOwnBackup(backup, dupID); err != nil {
		return err
	}
	return nil
}

// ResidueError 表示「操作已成功，但有一个应用工作临时文件未能清除」。
//
// 之所以单独成类型：调用方需要区分「操作失败」与「操作成功但有残留」。
// 前者要回滚、要计失败；后者文件已经合并到位，多留了一个内部临时文件，
// 属于需要提示但不应推翻结果的情况。
type ResidueError struct {
	Path string
	Err  error
	// Note 覆盖默认文案。用于残留**不是我们的文件**的场合（M1：backup 位被
	// 第三方顶替）——默认文案"请手动删除"在这种情况下会引导用户删掉别人的文件。
	Note string
}

func (e *ResidueError) Error() string {
	if e.Note != "" {
		return e.Note
	}
	return fmt.Sprintf("合并已完成，但临时文件 %s 未能删除（可能被其他程序占用），"+
		"请手动删除；它不会影响链接本身，扫描也已自动忽略该名字", e.Path)
}

func (e *ResidueError) Unwrap() error { return e.Err }

// verifyHardlinked 确认 dup 与 keep 现在指向同一份物理文件。
//
// 这是**平台无关**的终局判据，不依赖 fsid.ID 是否解析得出来：
//   - os.SameFile 比较 os.FileInfo 的 (dev, ino)，在 unix 上是 inode、
//     在 Windows 上由 Go 用文件索引填充——正是硬链接成立的定义。
//
// 之所以必须显式复核：os.Link/os.Rename 系列在 Windows 上有若干"语义近似但不等价"的
// 成功路径（卷不支持、重定向、Filter 驱动介入），仅凭"没返回错误"不足以断言
// 链接已建立。这里补上唯一的、可证伪的终态检查。
func verifyHardlinked(keep, dup string) error {
	ki, err := os.Stat(keep)
	if err != nil {
		return fmt.Errorf("硬链接复核失败：无法读取保留源 %s: %w", keep, err)
	}
	di, err := os.Stat(dup)
	if err != nil {
		return fmt.Errorf("硬链接复核失败：无法读取目标 %s: %w", dup, err)
	}
	if !ki.Mode().IsRegular() || !di.Mode().IsRegular() {
		return fmt.Errorf("硬链接复核失败：keep/dup 不都是普通文件")
	}
	if !os.SameFile(ki, di) {
		return fmt.Errorf("硬链接未生效：%s 与 %s 仍是两个独立文件"+
			"（该卷可能不支持硬链接，如 exFAT/FAT/ReFS，或两者不在同一卷）", dup, keep)
	}
	// 尺寸一致（同文件必然一致，这里防的是竞态下的诡异状态）
	if ki.Size() != di.Size() {
		return fmt.Errorf("硬链接复核失败：%s(%d) 与 %s(%d) 尺寸不一致",
			dup, di.Size(), keep, ki.Size())
	}
	return nil
}

// copyVerify 复制并校验（size + fsync），随后还原权限位与修改时间。
//
// P2：跨卷复制路径此前用 os.Create（0666&^umask）落地，可执行/只读等模式位
// 会丢失；目标 mtime 也会变成"现在"，导致后续按 (path,size,mtime) 命中哈希缓存
// 与保留策略全部失效。内容校验通过后才还原元数据——还原失败不作为整体失败
// （数据已在目标处），单独返回错误供上层记录。
func copyVerify(src, dst string, st os.FileInfo) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	if err := copyAndSync(sf, dst, st.Size()); err != nil {
		return err
	}
	return restoreMeta(dst, st)
}

// copyAndSync 复制到 dst 并校验字节数、落盘。
func copyAndSync(sf io.Reader, dst string, size int64) error {
	df, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(df, sf)
	if err != nil {
		df.Close()
		return err
	}
	if n != size {
		df.Close()
		return fmt.Errorf("复制不完整 %d/%d", n, size)
	}
	if err := df.Sync(); err != nil {
		df.Close()
		return err
	}
	return df.Close()
}

// restoreMeta 还原权限位与修改时间。umask 只会让新建文件更严格，
// Chmod 到源模式是放宽方向，root 与普通用户均可成功。
// 属主（uid/gid）与 xattr/ACL 不还原：跨卷移动到用户目录的场景下强行 chown
// 需要特权且可能覆盖目标卷已有策略，超出本功能范围。
func restoreMeta(dst string, st os.FileInfo) error {
	if err := os.Chmod(dst, st.Mode().Perm()); err != nil {
		return err
	}
	// 源 mtime 以纳秒精度记录（FileEntry.ModTime），此处还原到亚秒精度：
	// 哈希缓存键使用 ns，缓存会在下次扫描未命中后以新 mtime 重新写入，
	// 属可接受的一次性回退，好过把"刚移动的重复文件"再当作扫描后被修改。
	return os.Chtimes(dst, time.Now(), st.ModTime())
}

// claimedDst 是一个**已经抢到**的目标位置：路径 + 我们在该路径上创建的
// 0 字节占位文件的身份。
type claimedDst struct {
	path string
	id   fsid.ID
}

// claimDst 原子抢占目标名：O_CREATE|O_EXCL 建 0 字节占位，抢到即拥有；
// EEXIST 才递增到 name_1.ext → name_2.ext ...
//
// 取代原先 uniqueDst + dstExists 的"先查后用"组合（M2），一并修掉两个窗口：
//   - 查询与使用之间第三方把文件放进那个名字 → 我们的 rename / 复制的 O_TRUNC
//     会**静默覆盖**它；
//   - dstExists 用 os.Stat 判断占用，而 Stat 会跟随链接、且把所有非 nil 错误
//     一律当作"不存在"——悬空符号链接在 Stat 眼里等于空位，在 rename 眼里却是一个
//     有主的位置。
//
// 抢占失败的真错误（目录不可写、只读卷）如实返回，不再"放行这个名字、
// 赌下一步的破坏性动作会报错"——赌输的代价是别人的文件。
//
// 仍留一个不可消除的残余窗口：第三方先删掉我们的占位、再在同一位置建自己的文件，
// 随后我们改名覆盖。那需要主动针对本次操作做删除+重建，不属于同步盘/下载器
// 被动落子的形态；Go 标准库没有 NOREPLACE 改名原语，加一次身份复核也只是把
// 微秒级窗口缩成微秒级窗口，故不写这道无法证伪的守卫。
func claimDst(dir, name string) (claimedDst, error) {
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	for i := 0; i < nameMaxTry; i++ {
		n := name
		if i > 0 {
			// 序号插在扩展名**之前**（a.fdd-restored_1.bin）：扫描侧按同一
			// 形态识别应用工作临时名，改动会让两边判据脱钩（AS-R1）。
			n = fmt.Sprintf("%s_%d%s", base, i, ext)
		}
		p := filepath.Join(dir, n)
		f, err := openExclusive(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		switch {
		case err == nil:
			_ = f.Close()
			id, ierr := pathIdentity(p)
			if ierr != nil {
				// 占位已经抢到（名字归我们处置），只是取不到身份：
				// 按"未解析"返回，与全仓 identityStill 的 fail-open 口径一致。
				return claimedDst{path: p}, nil
			}
			return claimedDst{path: p, id: id}, nil
		case errors.Is(err, os.ErrExist):
			continue
		default:
			return claimedDst{}, fmt.Errorf("无法占用目标名 %s: %w", p, err)
		}
	}
	// OPS-10（2026-09-21 全量审查）：循环原先写成 `for i := 0; ; i++`，无上限。
	// 同包 uniqueXDG 对**同形状**的递增循环已经给过结论并在 nameMaxTry 处收口
	// （"永远查不出 NotExist 的环境会让全部并发 goroutine 一起挂死"）。
	// 同一条理由在本包成立过两次，第二处属漏改而不是取舍。
	return claimedDst{}, fmt.Errorf("目标目录中 %s 的重名条目已达上限 %d，拒绝继续递增", name, nameMaxTry)
}

// stillOurs 占位是否仍是我们创建的那一个。
func (c claimedDst) stillOurs() bool { return identityStill(c.path, c.id) }

// release 放弃时清掉占位。两种情况必须不动手：没抢到（零值）、
// 抢到的位置已被第三方换掉。原实现在复制失败分支里无条件 `os.Remove(target)`，
// 而"原位被占→另名恢复"分支的 target 可能等于用户已有的 OrigPath——那一下删的是别人的文件。
func (c claimedDst) release() {
	if c.path == "" || !c.stillOurs() {
		return
	}
	_ = os.Remove(c.path)
}

// claimExact 原子抢占一个**确切名字**：O_CREATE|O_EXCL 建 0 字节占位，抢到即拥有。
//
// 与 claimDst 的唯一差别：**名字被占时不递增**，把 ok 置 false 交调用方换路。
// 用于"这个名字就是结果本身、换名即换语义"的场合——目前只有回收站回撤的原位恢复
// （undo.go：抢到 → 恢复回原路径；抢不到 → 另落 name.fdd-restored.ext）。
//
// M19（2026-09-21，设计稿 §8）：那一支原先用 `os.Lstat(OrigPath)` 判空后**不认领**
// 直接改名，而 os.Rename 对已存在的普通文件是**静默替换**（Windows 腿是
// MoveFileEx + MOVEFILE_REPLACE_EXISTING）——Lstat 与改名之间第三方落子，
// 它连名字带 inode 一起消失。抢到占位之后，随后的改名替换的是**我们自己的
// 0 字节占位**，不再赌"没人来"。
//
// 返回的 error 只在"连能不能占用都问不出来"时非空（目录不可写、只读卷等）；
// 调用方对它与 ok=false 的处置相同（走另名恢复），真正的错误会在同一个目录上
// 以同样的原因再报一次——与 claimDst 分支对 Lstat 异常的处理同口径。
func claimExact(path string) (claimedDst, bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case err == nil:
		_ = f.Close()
		id, ierr := pathIdentity(path)
		if ierr != nil {
			return claimedDst{path: path}, true, nil
		}
		return claimedDst{path: path, id: id}, true, nil
	case errors.Is(err, os.ErrExist):
		return claimedDst{}, false, nil
	default:
		return claimedDst{}, false, err
	}
}

// isCrossDevice 仅把真实的 EXDEV 判定为跨卷。
//
// P2：旧实现把所有 *os.LinkError 都当作跨卷并回退到「复制+删源」，
// 于是 EPERM/EACCES/EDQUOT 之类的失败会在目标卷产生一份源文件副本，
// 表面上"移动成功"，实际上是把权限问题伪装成了成功——属于越权兜底。
// 现在非 EXDEV 直接返回错误，由操作层记为 Failed。
func isCrossDevice(err error) bool {
	var le *os.LinkError
	if !errors.As(err, &le) {
		return false
	}
	return errors.Is(le.Err, syscall.EXDEV) || isCrossDeviceExtra(le.Err)
}

// nameMaxTry 重名递增循环的统一上限（原先只叫 xdgNameMaxTry、只服务回收站一条腿）。
// 不设上限时任何"永远查不出 NotExist"的环境（如 files/ 所在目录被 chmod 000，
// Stat 恒返回 EACCES）都会让循环无限转，且循环整体持锁 → 全部并发 goroutine 一起挂死。
// 放在无 build tag 的文件里，是为了让同形状的 uniqueXDG 与 claimDst 共用同一个数
// 而不是各写一个（OPS-10）。
const nameMaxTry = 10000

// openExclusive 是占位抢名的唯一落点：默认 os.OpenFile。
// 抽成 var 不改变任何行为，只是让"候选名永远查不出可用"这一环境（名字被截断的
// 文件系统、被第三方持续抢占的目录）成为可在测试里构造的形状——见 claimNameMaxTry。
var openExclusive = os.OpenFile

// hardlinkRename 默认等于 os.Rename，测试可临时替换以模拟重命名失败，
// 用于验证 HardlinkMerge 的回滚路径不会丢失数据。
var hardlinkRename = os.Rename

// workTempRemove 默认等于 cleanupWorkTemp，测试可临时替换以模拟
// 「原副本删除失败」（Windows 上杀软/索引器占用句柄时的高频路径），
// 用于验证残留会被如实回报而不是被静默吞掉（缺陷 6）。
var workTempRemove = cleanupWorkTemp

// —— 跨卷「复制 + 删源」的三个接缝（AS-H4，2026-09-20 全仓审计）——
//
// 该路径的危害窗口在「复制完成」与「删源」之间，可持续数秒到数分钟，
// 不加接缝就无法在测试里稳定构造，只能靠 sleep 赌时序。

// renameFile 默认 os.Rename：测试用它让首次改名稳定失败于 EXDEV，
// 从而确定性地进入跨卷分支（本机造不出第二个真实挂载卷）。
var renameFile = os.Rename

// copyVerifyFile 默认等于 copyVerify：测试用它在「复制已完成、源尚未删除」
// 这一刻构造第三方以 rename 顶替源路径。
var copyVerifyFile = copyVerify

// removeSrc 默认 os.Remove：测试据此断言「守卫生效时一次都不该删」。
// 抽成 var 本身不改变行为，但把"是否真的动手"变成可观测的事实。
var removeSrc = os.Remove
