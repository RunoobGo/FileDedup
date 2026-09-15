package ops

import (
	"os"

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
//   - size/mtime 与扫描时一致 → 快速通过（重算哈希的常见情形优化）
//   - 不一致 → 重算 BLAKE3 与组哈希比对：相同仍通过（如仅 touch），不同则 Failed
func VerifyFile(e *model.FileEntry, groupHash [32]byte, pool *hasher.Pool) Verdict {
	st, err := os.Stat(e.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return VerdictSkipped
		}
		return VerdictFailed
	}
	if uint64(st.Size()) == e.Size && st.ModTime().UnixNano() == e.ModTime {
		return VerdictPass // 快速路径：元数据未变
	}
	// 元数据变化：重算哈希兜底
	f, err := os.Open(e.Path)
	if err != nil {
		return VerdictFailed
	}
	defer f.Close()
	buf := pool.GetStreamBuf()
	full, err := hasher.HashFull(f, st.Size(), buf)
	pool.PutStreamBuf(buf)
	if err != nil {
		return VerdictFailed
	}
	if full == groupHash {
		// 内容未变（例如仅 touch）。注意：不回写 e.ModTime/e.Size——
		// FileEntry 为结果集共享对象，无锁写入会与持锁读取方构成数据竞争；
		// 代价仅是下次校验重算一次哈希，正确性优先
		return VerdictPass
	}
	return VerdictFailed
}
