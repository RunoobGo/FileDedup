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
//   - 带 tag 的文件只做"读一个数、读一个卷标识，读不到就报 false"，不含任何判定。
package realbytes

// BlockSize 是 st_blocks 的计数单位（POSIX 规定恒为 512 字节，与卷扇区大小无关）。
// 导出的用途只有一个：调用方在展示"实占"时需要说明它按块对齐，
// 因此实占**可以大于**逻辑大小。
const BlockSize = 512

// From 把平台读数折成（实占, 是否可信）。
//
// 四条规则，每条都对应一类实测过的平台行为：
//  1. `!ok`（平台没读到：句柄失效、非 NTFS 卷、FUSE 不支持）→ 回退逻辑大小，
//     known=false。回退值只作"下限参考"，界面必须能区分"实占未知"与"实占=0"。
//     **trustsZero 在本条上无效**：卷级证据说的是"这个 0 可信"，不是"读得到"。
//  2. `reported==0 && size>0`：0 blocks 有两种成因——真全洞（一个字节都没写），
//     或该卷不跟踪块数（若干 NFS/FUSE 实现恒报 0）。单文件不可区分，只能请
//     卷级证据裁决（M28，Tracking）：该卷已被证明会报非零实占 ⇒ 采信为真读数
//     `(0,true)`；无证据 ⇒ 判读不到，回退逻辑大小（M6-P2 的口径，仍是默认）。
//  3. 其余情形**原样返回，不做封顶**——实占大于逻辑是真实存在的一类
//     （块粒度：1 KiB 实写占 4 KiB；预分配：mkfile -n 8m 占 8 MiB）。
//     `min(实占, 逻辑)` 会凭空抹掉这一块。
//     **反方向也有一类（M27）**：CoW 克隆/块级去重卷上共享 extent 被每一份各计一次
//     （本机 APFS 真读数：克隆对各自报满额、删掉一份只释放 0 B）⇒ 返回值在这类卷上是
//     **上限**而非确定值。per-file 不可区分（用户态没有 extent 映射这条路），
//     呈现侧按卷标注属 M8；夹具见 realbytes_clone_darwin_test.go，算术与读数见 04 §6.9.13。
func From(size, reported uint64, ok, trustsZero bool) (actual uint64, known bool) {
	if !ok {
		return size, false
	}
	if reported == 0 && size > 0 {
		if trustsZero {
			return 0, true
		}
		return size, false
	}
	return reported, true
}

// Tracking 采集"某卷确实报告过非零实占"这一证据（M28，04 §6.8.8）。
//
// 为什么需要它：`st_blocks==0` 在"真全洞"与"该卷恒报 0"之间单文件不可区分；
// 而**同卷上任何一个非空普通文件报出非零**，就足以证明该卷会在 st_blocks 里
// 报真实分配——于是本卷其余文件的 0 可以采信为真读数。证据**只在本卷内成立**：
// 换一个挂载实例就是另一份语义，连 statfs 的 f_type 相同都不够（设计稿 §11.0 E3）。
//
// 零值可用（首次 Observe 时初始化内部集合）。**非并发安全**：遍历期每个 worker
// 一份、收尾 Merge 合并——热路径每文件一次 Observe，加锁会把并发 worker
// 串在同一把锁上。
type Tracking struct{ seen map[uint64]struct{} }

// Observe 登记一次读数。只有"读到了且非零"才算证据：读不到（!ok）没有信息量，
// 而 `reported==0` 恰恰是待解释的那个值（拿它作证据就是循环论证）。
func (t *Tracking) Observe(vid, reported uint64, ok bool) {
	if !ok || reported == 0 {
		return
	}
	t.note(vid)
}

// Trust 该卷是否已有"会报非零实占"的证据。没有证据时一律 false——全洞语料卷、
// 恒报 0 的故障卷、以及 VolumeID 拿不到的平台，都落在这一侧（fail-closed）。
func (t *Tracking) Trust(vid uint64) bool {
	_, ok := t.seen[vid]
	return ok
}

// Merge 并入另一份证据（收尾时合并各 worker 的）。
func (t *Tracking) Merge(o *Tracking) {
	for vid := range o.seen {
		t.note(vid)
	}
}

func (t *Tracking) note(vid uint64) {
	if t.seen == nil {
		t.seen = make(map[uint64]struct{})
	}
	t.seen[vid] = struct{}{}
}
