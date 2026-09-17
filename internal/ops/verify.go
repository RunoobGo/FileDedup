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
//   - size 与扫描时不一致 → Failed（组内成员 size 恒等，尺寸变了即非原物）
//   - 其余情况一律重算 BLAKE3 与组哈希比对：相同通过（如仅 touch/chmod），不同 Failed
//
// P0-3：修正前存在「size+mtime 双一致 → 免重算」的快速路径，隐含前提是
// 「原地改写内容必然推进 mtime」。实测该前提不成立——同一次时钟刻度内的
// 原地改写，mtime 与 ctime 都可以保持逐纳秒不变（本沙箱 ext4/overlayfs 复现），
// 粗粒度卷（FAT32/exFAT 2s、HFS+ 与部分 SMB/NFS 1s）则整个刻度内都不变。
// 于是 S1 被完全绕过：硬链接合并会用 keep 的新内容不可逆地覆盖 dup 的原内容，
// 删除/移动则把改写后的新内容当作重复项清掉。两个单测因此稳定失败。
//
// 时间戳既然不能证明内容未变，正确性就只能建立在内容比对上：这里以
// 「每个文件重算一次全量哈希」为校验成本，换取 S1 的真实保证。
// 代价是清理大批量文件时多一趟读盘——相对"不可逆误删"这是必须付的价钱；
// 若日后需要恢复快速路径，必须换成内容级证据（如比对扫描期已算的采样指纹），
// 而不是退回时间戳推断。
func VerifyFile(e *model.FileEntry, groupHash [32]byte, pool *hasher.Pool) Verdict {
	st, err := os.Stat(e.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return VerdictSkipped
		}
		return VerdictFailed
	}
	// 重复组按 size 分桶产生，成员尺寸恒等；尺寸变化说明文件已非扫描时那份。
	if uint64(st.Size()) != e.Size {
		return VerdictFailed
	}
	return hashMatches(e.Path, st.Size(), groupHash, pool)
}

// hashMatches 重算全量 BLAKE3 并与组哈希比对。
func hashMatches(path string, size int64, groupHash [32]byte, pool *hasher.Pool) Verdict {
	f, err := os.Open(path)
	if err != nil {
		return VerdictFailed
	}
	defer f.Close()
	buf := pool.GetStreamBuf()
	full, err := hasher.HashFull(f, size, buf)
	pool.PutStreamBuf(buf)
	if err != nil {
		return VerdictFailed
	}
	if full == groupHash {
		// 内容未变（例如仅 touch/chmod）。注意：不回写 e.ModTime/e.Size——
		// FileEntry 为结果集共享对象，无锁写入会与持锁读取方构成数据竞争；
		// 正确性优先
		return VerdictPass
	}
	return VerdictFailed
}
