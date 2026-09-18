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
// 返回的 ID 在未解析平台（Windows）为零值，复核平凡通过。
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
	id := fsid.FromFileInfo(st)
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

// identityStill 复核 path 当前指向的物理文件仍是 id 记录的那一个。
// Lstat 不跟随符号链接：路径被换成链接/目录/另一文件时 dev+ino 必不同。
// id 未解析（Windows）时无从判断，返回 true（内容由 VerifyFile 已证）。
func identityStill(path string, id fsid.ID) bool {
	if !id.Resolved {
		return true
	}
	lst, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fsid.FromFileInfo(lst).SameIdentity(id)
}
