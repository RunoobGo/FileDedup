// Package realbytes 采集并判定文件的「实占字节」（磁盘上真正占用的空间）。
//
// 为什么需要它（M6-P2，04 §6.7 C 组 2）：重复组的可释放空间一直按 `Size`
// （逻辑大小）计账，而逻辑大小与磁盘占用可以相差几个量级——稀疏文件
// （65 MiB 逻辑 / 1 MiB 实占，本机 APFS 实测 65 倍虚高）、NTFS 压缩卷、
// 预分配文件。用户据此判断"清这一组能腾出多少"，账面数字就是错的。
//
// 分层约定（与 internal/sysguard、internal/worktemp 同族）：
//   - 本文件无 build tag，承载**全部判定与回退规则**（From）——因此在
//     Linux 主门禁里是可执行断言，不需要平台豁免；
//   - 带 tag 的文件只做"读一个数、读不到就报 false"，不含任何判定。
package realbytes

import "os"

// BlockSize 是 st_blocks 的计数单位（POSIX 规定恒为 512 字节，与卷扇区大小无关）。
// 导出的用途只有一个：调用方在展示"实占"时需要说明它按块对齐，
// 因此实占**可以大于**逻辑大小。
const BlockSize = 512

// From 把平台读数折成（实占, 是否可信）。
//
// 三条规则，每条都对应一类实测过的平台行为：
//  1. `!ok`（平台没读到：句柄失效、非 NTFS 卷、FUSE 不支持）→ 回退逻辑大小，
//     known=false。回退值只作"下限参考"，界面必须能区分"实占未知"与"实占=0"；
//  2. `reported==0 && size>0`（若干 NFS/FUSE 实现把 st_blocks 恒报 0）→ 同样判
//     读不到。非空文件"实占 0"在物理上不成立，照单全收会让整片重复组显示为
//     零字节可释放；
//  3. 其余情形**原样返回，不做封顶**——实占大于逻辑是真实存在的一类
//     （块粒度：1 KiB 实写占 4 KiB；预分配：mkfile -n 8m 占 8 MiB）。
//     `min(实占, 逻辑)` 会凭空抹掉这一块。
func From(size, reported uint64, ok bool) (actual uint64, known bool) {
	if !ok {
		return size, false
	}
	if reported == 0 && size > 0 {
		return size, false
	}
	return reported, true
}

// Of 取单个文件的实占。size 传逻辑大小，info 传遍历时已经拿到的 FileInfo
// （unix 复用其 Stat_t，零额外 syscall）。
func Of(path string, size uint64, info os.FileInfo) (actual uint64, known bool) {
	read, ok := reported(path, info)
	return From(size, read, ok)
}
