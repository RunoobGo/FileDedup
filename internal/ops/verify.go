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
)

// VerifyFile 操作前校验（M3-T02，01 §9）：
//   - ENOENT → Skipped（不计失败、不计释放空间）
//   - 非普通文件 / size 与扫描时不一致 → Failed（组内成员 size 恒等，尺寸变了即非原物）
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
		return VerdictFailed, fsid.ID{}
	}
	defer f.Close()
	st, err := f.Stat() // fstat：身份与内容出自同一 inode
	if err != nil || !st.Mode().IsRegular() {
		return VerdictFailed, fsid.ID{}
	}
	if uint64(st.Size()) != e.Size {
		return VerdictFailed, fsid.ID{}
	}
	id := fsid.FromFile(f) // 句柄查询：AS-H1，Windows 上唯一解析得动的口径
	buf := pool.GetStreamBuf()
	full, err := hasher.HashFull(f, st.Size(), buf)
	pool.PutStreamBuf(buf)
	if err != nil {
		return VerdictFailed, fsid.ID{}
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
	if !id.Resolved {
		return true
	}
	cur, err := fsid.FromPathNoFollow(path)
	if err != nil {
		return false
	}
	if !cur.Resolved {
		// 原先能解析、现在解析不出：卷行为异常或路径已被换成不支持索引的对象。
		// 判否——宁可拦一次让用户重扫，也不放行一次可能覆盖他人文件的操作。
		return false
	}
	return cur.SameIdentity(id)
}
