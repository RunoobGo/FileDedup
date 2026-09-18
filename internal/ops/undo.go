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
//
// Hash/Size/MtimeNs 为扫描时的内容证据，回撤前据此复核现状未被第三方改动。
type UndoItem struct {
	Kind     string // trash / move / hardlink
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
	if _, err := os.Lstat(it.OrigPath); err == nil || !os.IsNotExist(err) {
		// Lstat 报非 ENOENT 的错误时同样另名恢复：rename 会静默覆盖已存在目标
		base := filepath.Base(it.OrigPath)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext) + ".fdd-restored" + ext
		target = uniqueDst(filepath.Dir(it.OrigPath), name)
	}
	if err := os.Rename(it.DestPath, target); err != nil {
		if !isCrossDevice(err) {
			return "", fmt.Errorf("恢复失败: %w", err)
		}
		if err := copyVerify(it.DestPath, target, st); err != nil {
			os.Remove(target)
			return "", err
		}
		if err := os.Remove(it.DestPath); err != nil {
			return target, fmt.Errorf("已复制但清理回收站侧失败（两份并存）: %w", err)
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
		return "", err
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
	lid, kid := fsid.FromFileInfo(lst), fsid.FromFileInfo(kst)
	if lid.Resolved && kid.Resolved {
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
	} else if uint64(kst.Size()) != it.Size || uint64(lst.Size()) != it.Size {
		// 未解析平台（Windows）降级：路径存在 + 大小与记录一致
		return "", fmt.Errorf("目标或保留源大小与记录不一致，可能已被修改，已拦截")
	}

	tmp := it.OrigPath + ".fdd-undo-tmp"
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

// hashFile 全量 BLAKE3（回撤内容复核）。
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
