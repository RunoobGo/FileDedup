package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
)

// UndoItem 单条回撤任务（来自 op_items 的 done 状态记录）。
//   - trash：DestPath = 回收站实际落点（darwin/linux 映射；Windows 恒空 → 上层拦截）
//   - move：DestPath = 移动后的新路径
//   - hardlink：LinkSrc = 合并时指向的保留源路径
//   - symlink：LinkSrc = 合并时指向的保留源路径（与 hardlink 同栏；
//     OrigPath 位置上是链接，备份在 OrigPath + worktemp.SuffixOld）
//
// Hash/Size/MtimeNs 为扫描时的内容证据，回撤前据此复核现状未被第三方改动：
// trash/move/hardlink 三条路径都做**全量 BLAKE3** 比对（M4），零值 Hash（旧账本）
// 只能比 size，此时按"无从比对"放行而不是拦死。
type UndoItem struct {
	Kind     string // trash / move / hardlink / symlink
	OrigPath string
	DestPath string
	LinkSrc  string
	Hash     [32]byte
	Size     uint64
	MtimeNs  int64
}

// undoPool 回撤内容校验的缓冲复用池（仅回撤路径使用，串行调用为主）。
var undoPool = hasher.NewPool()

// errRestoredAlready 目的地已空、原位证据齐全：上次回撤动了文件系统却没来得及落账
// （I6 写前标记的崩溃窗口）。上层据此记成功而非"文件已不存在"。
var errRestoredAlready = errors.New("已还原（上次回撤未落账）")

// alreadyRestored 判定"文件其实已经回家"：回收站/移动目标侧已无文件，
// 原位却存在一份与记录同尺寸、同 mtime 且内容哈希相符的普通文件。
// 证据不齐（含旧账本无哈希）一律返回 false，交给正常报错让用户自己核对。
func alreadyRestored(it UndoItem) bool {
	if it.DestPath == "" {
		return false // 无落点记录（映射缺失）：无从判定，交给上层原因报错
	}
	if _, err := os.Lstat(it.DestPath); err == nil {
		return false // 目的地仍有文件，不是已还原
	}
	st, err := os.Lstat(it.OrigPath)
	if err != nil || !st.Mode().IsRegular() || uint64(st.Size()) != it.Size {
		return false
	}
	if it.MtimeNs > 0 && st.ModTime().UnixNano() != it.MtimeNs {
		return false
	}
	if it.Hash == [32]byte{} {
		return false // 无内容证据（旧账本），不猜
	}
	h, err := hashFile(it.OrigPath)
	return err == nil && h == it.Hash
}

// UndoOne 回撤单个已执行条目（spec §7.2），单文件独立成败。
// 安全语义与正向操作对称：任何"记录对象已非原物"的迹象都拦截而非强行恢复。
func UndoOne(it UndoItem) (string, error) {
	switch it.Kind {
	case "trash":
		return undoTrash(it)
	case "move":
		return undoMove(it)
	case "hardlink":
		return undoHardlink(it)
	case "symlink":
		return undoSymlink(it)
	case "delete":
		return "", fmt.Errorf("永久删除不可回撤")
	default:
		return "", fmt.Errorf("不支持回撤的操作类型 %q", it.Kind)
	}
}

// undoSourceCheck 回撤前确认 DestPath（回收站落点/移动目标）仍是记录中那份文件。
func undoSourceCheck(it UndoItem, where string) (os.FileInfo, error) {
	st, err := os.Lstat(it.DestPath)
	if err != nil {
		if os.IsNotExist(err) {
			if alreadyRestored(it) {
				return nil, errRestoredAlready
			}
			return nil, fmt.Errorf("%s中的文件已不存在（可能已被清空或手动还原）: %s", where, it.DestPath)
		}
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s中的路径不是普通文件: %s", where, it.DestPath)
	}
	if uint64(st.Size()) != it.Size {
		return nil, fmt.Errorf("%s文件大小与记录不符（%d ≠ %d），可能已被替换，已拦截", where, st.Size(), it.Size)
	}
	// M4：尺寸相等不等于"还是那一份文件"。同长度改写（回收站里那份被写工具
	// 原地更新、移动目标被同名文件顶替）只比 size 会一路放行，把错内容放回家，
	// 紧接着 applyMtime 又把 mtime 拨回记录值——事后连时间戳都看不出动过。
	// undoHardlink 一直做全量 BLAKE3，三条回撤路径的判据必须一致。
	//
	// it.Hash 为零值时**跳过而不是拦死**：升级前写入的旧账本没有内容证据，
	// "无从比对"不等于"校验失败"，一律拦会让历史记录全都撤不回。
	if it.Hash != ([32]byte{}) {
		h, err := hashFile(it.DestPath)
		if err != nil {
			return nil, err
		}
		if h != it.Hash {
			return nil, fmt.Errorf("%s中的文件内容与扫描记录不符（已被修改或替换），已拦截（S1）: %s", where, it.DestPath)
		}
	}
	return st, nil
}

// undoTrash 回收站 → 原位：原位被占时落到 name.fdd-restored.ext（宁另名不覆盖）。
func undoTrash(it UndoItem) (string, error) {
	st, err := undoSourceCheck(it, "回收站")
	if errors.Is(err, errRestoredAlready) {
		return it.OrigPath, nil
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(it.OrigPath), 0o755); err != nil {
		return "", err
	}
	target := it.OrigPath
	var claim claimedDst
	if c, ok, _ := claimExact(it.OrigPath); ok {
		// M19（2026-09-21，设计稿 §8）：原位这个**确切名字**先被我们抢下（0 字节
		// 占位），随后的改名替换的是我们自己的占位，而不是"赌窗口里没人落子"。
		// 抢到即意味着这次恢复回原路径——名字就是结果本身，不能换名。
		//
		// 残余窗口（不可消除，同 claimDst 注释）：第三方先删掉我们的占位、再在
		// 同一位置建自己的文件，随后改名覆盖。那需要针对本次操作做删除+重建，
		// 不属于同步盘/下载器被动落子的形态。
		claim = c
	} else {
		// 名字被占（含悬空链接与目录）或连能否占用都没问出来（错误被丢弃的理由
		// 见 claimExact 注释）：一律改落 name.fdd-restored.ext。宁另名，不覆盖。
		base := filepath.Base(it.OrigPath)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext) + FddRestoreMark + ext
		c, err := claimDst(filepath.Dir(it.OrigPath), name)
		if err != nil {
			return "", err
		}
		claim, target = c, c.path
	}
	if err := renameFile(it.DestPath, target); err != nil {
		if !isCrossDevice(err) {
			claim.release()
			return "", fmt.Errorf("恢复失败: %w", err)
		}
		// AS-H4 同型（2026-09-20 全仓审计）：回收站侧文件在复制窗口内同样可能被
		// 第三方以 rename 顶替，而这里原先按路径盲删。复制前取身份、删前复核。
		srcID, err := pathIdentity(it.DestPath)
		if err != nil {
			claim.release()
			return "", err
		}
		if err := copyVerifyFile(it.DestPath, target, st); err != nil {
			claim.release()
			return "", err
		}
		if !identityStill(it.DestPath, srcID) {
			return target, fmt.Errorf("已恢复到 %s，但回收站侧文件在复制期间被替换（inode 已变化）："+
				"为避免误删第三方文件**未清理回收站侧**，两份并存，请核对后自行处理其一: %s", target, it.DestPath)
		}
		if err := removeSrc(it.DestPath); err != nil {
			// M86（04 §6.11 OPS-14b，设计段 §19.0-2）：数据已经在家、只是回收站侧
			// 没清掉，盘上是两份。这句原先只说"两份并存"却不给落点，用户既不知道
			// 哪份是真的、也不知道该去哪删多余的一份 —— 与同函数 :174 那条同型
			// 分支的写法不对称。补上 target 与残留位置。
			return target, fmt.Errorf("已复制但清理回收站侧失败（两份并存，已恢复的文件在 %s，"+
				"回收站侧残留 %s，请核对后自行删去其一）: %w", target, it.DestPath, err)
		}
	}
	applyMtime(target, it.MtimeNs)
	return target, nil
}

// undoMove 移动目标 → 原目录：复用 MoveFile（重名递增天然不覆盖）。
func undoMove(it UndoItem) (string, error) {
	if _, err := undoSourceCheck(it, "移动目标"); errors.Is(err, errRestoredAlready) {
		return it.OrigPath, nil
	} else if err != nil {
		return "", err
	}
	dst, err := MoveFile(it.DestPath, filepath.Dir(it.OrigPath))
	if err != nil {
		// M113（04 §6.11 OPS-14e，设计段 §23.2）：这里原先是 `return "", err`。
		// MoveFile 有两条"返回 dst 且 err≠nil"的部分成功路径（move.go:60-65 源被顶替、
		// :66-72 删源失败），此时盘上已经是两份，而 app 层 undoFailure 的
		// "（数据已在 …）"补写（M86 立的做法）认的就是这个返回值 —— 丢掉落点等于
		// 让那份孤儿副本在界面上不存在。完全失败时 MoveFile 回空串，行为不变。
		return dst, err
	}
	applyMtime(dst, it.MtimeNs)
	return dst, nil
}

// undoHardlink 拆除指向 LinkSrc 的硬链接，恢复独立文件。
// 三重防线：① OrigPath 必须仍是与 LinkSrc 同身份的那个 inode
// （用户已把 dup 换成别的文件时拦截，避免覆盖第三方）；
// ② 从 LinkSrc 复制到临时文件后全量 BLAKE3 必须等于记录哈希
// （源被篡改时拦截——此时恢复出的将是错误内容）；
// ③ 只有 ①②全过才 rename 替换，任何失败都不动 OrigPath 现状。
func undoHardlink(it UndoItem) (string, error) {
	lst, err := os.Lstat(it.OrigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("目标已不存在（可能被再次删除），回撤中止: %s", it.OrigPath)
		}
		return "", err
	}
	if !lst.Mode().IsRegular() {
		return "", fmt.Errorf("目标不是普通文件，回撤中止: %s", it.OrigPath)
	}
	kst, err := os.Lstat(it.LinkSrc)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("保留源已不存在，无法恢复内容: %s", it.LinkSrc)
		}
		return "", err
	}
	if !kst.Mode().IsRegular() {
		return "", fmt.Errorf("保留源不是普通文件: %s", it.LinkSrc)
	}
	lid, lerr := identityByHandle(it.OrigPath)
	kid, kerr := identityByHandle(it.LinkSrc)
	switch {
	case lerr == nil && kerr == nil && lid.Resolved && kid.Resolved:
		if !lid.SameIdentity(kid) {
			// 崩溃残留自检：拆链已完成（原位独立成文件且内容等于记录），
			// 只是账本没落上——记成功，不再拦成"永不可撤"。
			if it.Hash != [32]byte{} && uint64(lst.Size()) == it.Size {
				if h, herr := hashFile(it.OrigPath); herr == nil && h == it.Hash {
					return it.OrigPath, nil
				}
			}
			return "", fmt.Errorf("目标已非合并时的硬链接（inode 不符），为避免覆盖第三方文件已拦截")
		}
	default:
		// 2026-09-19（关联隐患修复）：此处原先直接用 fsid.FromFileInfo(Lstat 产物)。
		// Windows 的 Stat/Lstat **不携带卷序列号与 64 位文件索引**，FromFileInfo
		// 必然返回未解析 ID，于是身份防线①在 Windows 上**整条失效**，退化为
		// 「只比大小」。后果：只要用户把 OrigPath 换成另一个**同样大小**的无关
		// 文件，回撤就会认它为"该恢复的目标"并 rename 覆盖上去——防线①形同虚设。
		// 现在改为经句柄查询取身份（identityByHandle），Windows 上同样拿得到
		// (卷序列号, 文件索引)，防线①真正生效。
		//
		// 落在 default 分支 = 句柄身份不可用（文件被独占锁、权限不足等）：
		// 此时只能退回大小校验——否则在杀软占句柄的机器上会完全无法回撤。
		// 风险面被第②道防线收敛：临时文件仍要全量 BLAKE3 等于记录哈希才会改名，
		// 因此"被换成内容不同的文件"仍会被拦；仅"同大小的第三方文件恰好出现在
		// 原路径且内容也恰好相同"这一极端组合能穿过，而那与原文件本就无法区分。
		if uint64(kst.Size()) != it.Size || uint64(lst.Size()) != it.Size {
			return "", fmt.Errorf("目标或保留源大小与记录不一致，可能已被修改，已拦截")
		}
	}

	tmp := it.OrigPath + FddUndoSuffix
	_ = os.Remove(tmp)
	sf, err := os.Open(it.LinkSrc)
	if err != nil {
		return "", err
	}
	err = copyAndSync(sf, tmp, kst.Size())
	sf.Close()
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("读取保留源失败: %w", err)
	}
	if h, herr := hashFile(tmp); herr != nil {
		os.Remove(tmp)
		return "", herr
	} else if h != it.Hash {
		os.Remove(tmp)
		return "", fmt.Errorf("保留源内容与扫描记录不一致（已被修改），已拦截（S1）")
	}
	if err := hardlinkRename(tmp, it.OrigPath); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("恢复独立文件失败: %w", err)
	}
	applyMtime(it.OrigPath, it.MtimeNs)
	return it.OrigPath, nil
}

// readlinkTarget 是 symlinkTarget 的测试接缝：让"读链接这一步自己失败"
// 可确定性构造（真机上 Lstat 已判定是链接之后再让 readlink 出错，造不出来）。
// 生产路径就是跨平台访问器本身，不改变行为。
var readlinkTarget = symlinkTarget

// undoSymlink 回撤软链接合并（2026-09-20）：拆除链接，把备份还原回原位。
//
// 三步，与 undoHardlink 的三重防线对齐但**判据不同**：
//
//	① OrigPath 必须仍是"指向 LinkSrc 的符号链接"
//	   （用 verifySymlinked —— 软链接自身身份无意义，必须解析目标再比）
//	② 备份（OrigPath + .fdd-old）必须存在
//	③ 删链接 → 还原备份；任一步失败都不破坏现有数据
//
// 与 undoHardlink 的关键差异（决定了本函数反而**更安全**）：
// OrigPath 位置上的链接是独立对象，它的内容就是"目标路径"这个字符串，
// 数据本体一直在 LinkSrc 处。因此这里**不需要**从 LinkSrc 复制内容回来，
// 也就没有"源被篡改导致恢复出错误内容"的风险面——直接改名备份即可，
// 而备份是合并前就在磁盘上的那份原文件，内容天然正确。
//
// 但 ① 这一层不能省：用户可能在合并后把 OrigPath 位置上的链接删掉、
// 换成自己的另一个文件。若不做校验就直接删 + 改名，就会把那个第三方文件
// 删掉、把备份顶上去——用户会发现自己放进去的东西消失了。这是 R5
// （回撤时误删第三方文件）的具体形态。
//
// 悬空链接（保留项已被删/移动）**仍可回撤**：数据在备份里，与链接是否
// 有效无关。这种情况反而是最需要回撤的（用户想恢复原文件）。
func undoSymlink(it UndoItem) (string, error) {
	if it.LinkSrc == "" {
		// 旧账本/异常记录：没有目标信息就无法校验 OrigPath 是不是我们建的链接
		return "", fmt.Errorf("缺少链接目标记录，无法安全回撤（为避免误删第三方文件已拒绝）")
	}

	// ① 原位必须仍是本次创建的链接（指向 LinkSrc）
	li, err := os.Lstat(it.OrigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("目标位置已不存在（可能被手动清理），回撤中止: %s", it.OrigPath)
		}
		return "", err
	}
	backup := it.OrigPath + FddOldSuffix

	if li.Mode()&os.ModeSymlink == 0 {
		// 原位已不是链接。可能是"用户把链接换成了自己的文件"（须拦截），
		// 也可能是"上次回撤已删链接、只差改名备份"的崩溃残局——后者用
		// 备份仍在 + 原位内容等于记录哈希来识别（与 undoHardlink 的自检同构）。
		if _, berr := os.Lstat(backup); berr == nil && it.Hash != [32]byte{} {
			if st, serr := os.Lstat(it.OrigPath); serr == nil &&
				st.Mode().IsRegular() && uint64(st.Size()) == it.Size {
				if h, herr := hashFile(it.OrigPath); herr == nil && h == it.Hash {
					// 原位内容就是记录里那份数据 → 视为已还原（上次回撤未落账）
					return it.OrigPath, nil
				}
			}
		}
		return "", fmt.Errorf("目标位置已不是本次创建的软链接" +
			"（可能被替换为其他文件），为避免覆盖或删除第三方文件已拦截")
	}

	// 悬空链接也允许继续：数据在备份里，链接有效性与此无关。
	// 但若链接指向的目标已不是 LinkSrc（例如用户重建过链接指到别处），
	// 说明现状已被改动，仍按"已非本次创建的链接"拦截。
	//
	// 2026-09-20 审查修正：原先整块检查以 Lstat(LinkSrc) 成功为前提，
	// 而"保留源被删除 → 链接悬空 → 用户在同一位置建了指向别处的链接"恰好
	// 绕开全部校验，回撤会把用户的链接当成我们的删掉。现改为无条件比对。
	if verr := verifySymlinked(it.LinkSrc, it.OrigPath); verr != nil {
		// 目标不可达（悬空）时 verifySymlinked 会失败，那是**预期**的；
		// 只有"目标可达但不是 LinkSrc"才需要拦截。
		if _, serr := os.Stat(it.LinkSrc); serr == nil {
			return "", fmt.Errorf("目标位置上的链接已不指向原保留源，"+
				"为避免误删第三方文件已拦截: %w", verr)
		}
		// 目标不可达 → 身份无从比对，但链接的字面目标必须正是 LinkSrc
		// 才算"我们还是我们建的那个链接"。
		//
		// ★ 顺带项（2026-09-20 全仓审计）：修正前这里直接 os.Readlink，
		// 绕过了包内跨平台访问器 symlinkTarget（其注释明写"调用方无需分平台"），
		// 并把"读链接失败"与"目标不符"合并成同一句拦截理由——I/O 故障被
		// 说成"存在一个指向别处的第三方链接"，用户去找根本不存在的东西，
		// 真实原因反倒被吞掉。现在两者分开报，且 I/O 原因用 %w 包出。
		tgt, rerr := readlinkTarget(it.OrigPath)
		if rerr != nil {
			return "", fmt.Errorf("无法读取 %s 的链接目标，现状未知，"+
				"为避免误删第三方文件已拦截: %w", it.OrigPath, rerr)
		}
		if filepath.Clean(tgt) != filepath.Clean(it.LinkSrc) {
			return "", fmt.Errorf("目标位置上的链接不指向原保留源 %s（实际指向 %s），"+
				"为避免误删第三方文件已拦截", it.LinkSrc, tgt)
		}
	}

	// ② 备份必须在
	if _, err := os.Lstat(backup); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("找不到合并前的原始备份 %s，无法回撤"+
				"（链接保留未动，数据在保留源处）", backup)
		}
		return "", err
	}

	// ③ 删链接 → 还原备份。
	// 顺序不能反：先删链接才能把备份改名到该路径（改名会覆盖，但显式删除
	// 让语义更清楚，也避免某些平台上"改名覆盖已存在文件"的失败）。
	// 万一"删链接成功但改名失败"，数据仍在 backup 处，报错里给出位置。
	if err := os.Remove(it.OrigPath); err != nil {
		return "", fmt.Errorf("删除软链接失败: %w", err)
	}
	if err := hardlinkRename(backup, it.OrigPath); err != nil {
		return "", fmt.Errorf("链接已删除，但还原备份失败，原文件保留在 %s（数据未丢失）: %w",
			backup, err)
	}
	applyMtime(it.OrigPath, it.MtimeNs)
	return it.OrigPath, nil
}

// hashFile 全量 BLAKE3（回撤内容复核）。
// identityByHandle 经**句柄**取文件的物理身份（卷序列号 + 64 位文件索引）。
//
// 为什么不直接用 fsid.FromFileInfo(Lstat 产物)：Windows 的 Stat/Lstat 结果
// 不携带卷序列号与文件索引，FromFileInfo 在 Windows 上恒返回未解析 ID，
// 使依赖它的身份校验**静默失效**（2026-09-19 关联隐患）。
// 句柄查询（fsid.FromFile）在 Windows 上能拿到真实身份，unix 上等价于
// (dev, ino)——两条路径都成立。
//
// 返回的 error 非 nil 表示"连打开都失败"（不存在/权限/独占锁），
// 与"打开成功但身份未解析"（ID.Resolved == false）是两种不同情形，
// 调用方需分开处置。
func identityByHandle(p string) (fsid.ID, error) {
	f, err := os.Open(p)
	if err != nil {
		return fsid.ID{}, err
	}
	defer f.Close()
	return fsid.FromFile(f), nil
}

func hashFile(p string) ([32]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return [32]byte{}, err
	}
	buf := undoPool.GetStreamBuf()
	defer undoPool.PutStreamBuf(buf)
	return hasher.HashFull(f, st.Size(), buf)
}

// applyMtime 还原扫描时记录的修改时间（纳秒精度）。失败不作为整体失败：
// 数据已恢复，时间戳偏差可由下次扫描自愈。
func applyMtime(p string, ns int64) {
	if ns > 0 {
		_ = os.Chtimes(p, time.Now(), time.Unix(0, ns))
	}
}
