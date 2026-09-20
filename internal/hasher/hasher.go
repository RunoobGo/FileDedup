// Package hasher 哈希模块：首尾 xxHash64 预筛 + 小文件一趟双哈希 + 全量 BLAKE3（04 M1-T07/T08）。
package hasher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/cespare/xxhash/v2"
	"lukechampine.com/blake3"
)

const (
	// HeadTailChunk 预筛采样：头/尾各 64KiB。
	HeadTailChunk = 64 * 1024
	// SmallFileMax ≤128KiB：首尾样本覆盖全文件，预筛即全量，一趟双哈希。
	SmallFileMax = 128 * 1024
	// LargeSeg 大文件分段读取粒度（16MiB，预取流水线用）。
	LargeSeg = 16 << 20
	// StreamChunk 顺序流式读取块（1MiB）。
	StreamChunk = 1 << 20
)

// Partial 多点 xxHash64 指纹（H1 修复）：头 + 尾 + 两个中点。
// 此前只有头尾 2 点：>128KiB 文件若中段被原地改写而首尾 128KiB 与
// size/mtime 全部保持不变（cp -p/备份还原、粗时间戳卷），会命中旧缓存
// 全量哈希 → 组成假重复组。分桶键只由采样构成，多点采样直接压缩该窗口。
type Partial struct {
	Head uint64
	Tail uint64
	Mid1 uint64 // size/2 处 64KiB
	Mid2 uint64 // 3size/4 处 64KiB
}

// Result 哈希结果。
type Result struct {
	Partial Partial
	Full    [32]byte // BLAKE3-256
	Small   bool     // 小文件路径：一趟已完成全量哈希
	// Short 短读标记：实际可读字节数 < 调用方传入的 size。
	//
	// 触发场景：目录遍历（ReadDir/Info 取 size）与本次 open 之间，文件被截断、
	// 被并发改写，或所在卷（exFAT/FAT32/SMB 网络盘、OneDrive 云占位文件、
	// 稀疏/压缩文件）报出的逻辑大小与真实可读长度不一致。
	//
	// 语义约定：短读**不是错误**。本 Result 里的哈希一律对应「实际读到的字节」，
	// 调用方必须据 Short 用实际长度重建条目，不得继续沿用失真的 size——
	// 否则两个不同内容的文件可能因截断被算成同一哈希，造出假重复组。
	Short bool
	// ActualSize 实际读到的字节数（= 各个采样点实际读到量的上界口径）。
	// Short 为 false 时等于传入的 size。
	ActualSize int64
}

// ErrClosed 由上层注入文件句柄失败时使用。
var ErrClosed = errors.New("hasher: file handle closed")

// PrefilterMax 单文件预筛最大读量：4 个采样点 × HeadTailChunk。
const PrefilterMax = 4 * HeadTailChunk

// shortReadOK 判定读错误是否属于「短读」——即文件比元数据声明的短。
//
// 2026-09-19 修复（Windows 首扫 0 组 / 未保存缓存）：
// 修正前一律写作 `err != nil && err != io.EOF`，而 io.ReadFull 在「读满前
// 遇到文件尾」时返回的是 io.ErrUnexpectedEOF，**不是** io.EOF，于是短读被
// 当成真实故障返回。调用方（pipeline 阶段 2）据此把该文件标记 skip 并
// 整个剔除出预筛分组，后果是：
//   - 它与任何文件都不可能同桶 → 整组重复静默消失（用户：扫不到重复）
//   - 它永远走不到 pending 入队那一行 → 缓存里永远没有它（用户：未保存缓存）
//
// windows CI 实测（scripts/test-windows-quarantine.sh 的隔离清单）已记录该
// 现象家族的原文：「子目录里的副本文件在 prefilter 阶段报 unexpected EOF
// （记录 size 大于实际可读长度）……首扫 0 组」。
//
// io.EOF 同样放过：ReadAt 在偏移恰好落在文件尾时也可能返回纯 io.EOF。
func shortReadOK(err error) bool {
	return err == nil || err == io.EOF || err == io.ErrUnexpectedEOF
}

// sampleOffsets 预筛采样偏移（升序、仅由 size 决定，两次计算必然一致）：
// 头 0、中点 size/2、3size/4、尾 size-chunk；中点越界时钳制到 size-chunk
// （128KiB<size<256KiB 时 mid2 与 tail 重合，无害）。
func sampleOffsets(size int64) [4]int64 {
	chunk := int64(HeadTailChunk)
	tail := size - chunk
	mid1 := size / 2
	mid2 := size * 3 / 4
	if mid1 > tail {
		mid1 = tail
	}
	if mid2 > tail {
		mid2 = tail
	}
	return [4]int64{0, mid1, mid2, tail}
}

// HashHeadTail 预筛哈希（一次 open 已由调用方完成）：
//   - size ≤ SmallFileMax：读全文件，同趟完成 xxHash64 + BLAKE3（小文件捷径，决策 8）
//   - size >  SmallFileMax：按 sampleOffsets 读 4×64KiB 各自 xxHash64
//
// 短读语义见 Result.Short：读不满不算错误，用实际读到的字节算哈希并如实标记。
func HashHeadTail(f *os.File, size int64, buf []byte) (Result, error) {
	if size <= SmallFileMax {
		n, err := io.ReadFull(f, buf[:size])
		if !shortReadOK(err) {
			return Result{}, err
		}
		// AS-H2：小文件一趟把「读满声明长度」当作「读完了整个文件」，
		// Full 字段直接入组。文件在扫描后变长时这个前提不成立，与短读同等待遇。
		if int64(n) == size {
			if gerr := rejectGrowthBeyond(f, size); gerr != nil {
				return Result{}, gerr
			}
		}
		b := buf[:n]
		h := xxhash.Sum64(b)
		return Result{
			// 全文件被单点覆盖：四个指纹相同
			Partial:    Partial{Head: h, Tail: h, Mid1: h, Mid2: h},
			Full:       blake3.Sum256(b),
			Small:      true,
			Short:      int64(n) != size,
			ActualSize: int64(n),
		}, nil
	}
	var r Result
	offs := sampleOffsets(size)
	reads := [4]int{}
	dst := []*uint64{&r.Partial.Head, &r.Partial.Mid1, &r.Partial.Mid2, &r.Partial.Tail}
	short := false
	for i, off := range offs {
		n, err := f.ReadAt(buf[:HeadTailChunk], off)
		if !shortReadOK(err) {
			return Result{}, err
		}
		if n < HeadTailChunk {
			// 该采样点没读满：文件比声明的短（或该偏移已越界）。
			// 不视为错误——该点按实际读到的字节算指纹；整份结果标记 Short
			// 让调用方知道 size 不可信。
			short = true
		}
		reads[i] = n
		*dst[i] = xxhash.Sum64(buf[:n])
	}
	r.Short = short
	if !short {
		r.ActualSize = size
		return r, nil
	}
	// 短读：把「采样点读到的字节数」还原成实际文件长度。
	//
	// 采样点按偏移升序是 [0, mid1, mid2, tail]。若某点读满 HeadTailChunk，
	// 说明该偏移之后至少还有 chunk 字节可读，即 实际长度 >= off+chunk；
	// 若某点读不满 n（n>0），说明该偏移处只剩 n 可读，即 实际长度 = off+n
	// ——这是唯一能直接读出真值的证据。
	//
	// 取**全部读不满的点里最小的 off+n**。注意 n=0 的点（偏移已越过 EOF）
	// 给出的 off 只是**上界**而非真值：当真实长度落在所有未读满点偏移之下时，
	// 这里只能得到"最小的越界偏移"这一偏大的估计。因此 ActualSize 的契约
	// 是「不低于真实长度的保守上界」，调用方不得将其当作精确值持久化
	// （pipeline 的短读纠偏会在阶段 3 以实际读量再校正）。
	//
	// 边界：若四个点全部读满却仍被判 short（不可能，short 的定义就是有读不满的
	// 点），此处 best 保持 -1 → 归 0，由调用方按"不可读"处理。
	best := int64(-1) // -1 = 尚未取得任何"读不满点"的证据（0 是合法真值，不能当哨兵）
	for i, off := range offs {
		if reads[i] == HeadTailChunk {
			continue // 读满了：只提供下界，不能定真值
		}
		if v := off + int64(reads[i]); best < 0 || v < best {
			best = v
		}
	}
	if best < 0 {
		best = 0
	}
	// 头部采样点（off=0）读满时，实际长度至少 chunk；此时 best 只能来自
	// 后面读不满的点。若那个点算出 v < chunk，说明与头部矛盾（并发改写），
	// 取下界 chunk 保证不小于已确证可读的量。
	if reads[0] == HeadTailChunk && best < HeadTailChunk {
		best = HeadTailChunk
	}
	r.ActualSize = best
	return r, nil
}

// rejectGrowthBeyond 探测「声明长度之后是否还有可读字节」——即文件是否**变长**。
//
// AS-H2（2026-09-20 全仓审计）：2026-09-19 的短读修复只挡住了"实际比声明短"。
// 反向不成立时 io.LimitReader 照样静默切尾、n == size 顺利通过，于是
// 「前 size 字节相同 + 尾部是刚追加的数据」的两个文件会算出同一哈希、进同一组，
// 用户删掉的正是含新数据的那一份。触发面是常见工况：断点续传、追加型日志、
// 正在写入的镜像文件。
//
// 用 ReadAt 而非顺序 Read：不扰动调用方的读位置，且分段流水线（自行 ReadAt
// 各段）与顺序路径共用同一探测口径。
//
// 只把 n>0 当作证据；探测读到的错误一律放过（EOF 是正常答案，其余 I/O 异常
// 会在真正的读取里显形，此处不作为判据）。
func rejectGrowthBeyond(f *os.File, size int64) error {
	var probe [1]byte
	if n, _ := f.ReadAt(probe[:], size); n > 0 {
		return fmt.Errorf("hasher: 文件比声明长（声明 %d 字节，偏移 %d 之后仍可读）: %w",
			size, size, io.ErrUnexpectedEOF)
	}
	return nil
}

// HashFull 顺序流式全量 BLAKE3（默认路径）。
//
// 2026-09-19 修复：修正前用 io.LimitReader(f, size) 包一层，而 LimitReader 读
// 不满就返回 io.EOF，io.CopyBuffer 视其为正常结束——于是"记录 size 大于实际可读
// 长度"时，函数**静默**算出的是"较短那段内容"的哈希，调用方毫不知情，
// 可能把截断后的内容与别的文件判成重复。现在改为显式计数：读到的字节数与
// 声明的 size 不符即报错，把判断权交回调用方（宁可计入失败清单，也不静默出错）。
//
// AS-H2（2026-09-20）：同一条理由覆盖反向情形——变长时 LimitReader 同样静默切尾，
// 故 n == size 之后还要探测「size 处是否仍可读到字节」。
func HashFull(f *os.File, size int64, buf []byte) ([32]byte, error) {
	h := blake3.New(32, nil)
	n, err := io.CopyBuffer(h, io.LimitReader(f, size), buf)
	if err != nil {
		return [32]byte{}, err
	}
	if n != size {
		return [32]byte{}, fmt.Errorf("hasher: 文件短读（声明 %d 字节，实际可读 %d 字节）: %w", size, n, io.ErrUnexpectedEOF)
	}
	if err := rejectGrowthBeyond(f, size); err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// HashFullSegmented 大文件分段流水线：单生产者 ReadAt 预取各段（深度 depth），
// 主协程按段序喂入单一 Hasher——结果与顺序读取完全一致（等价性由测试保证），
// 同时实现 IO 与哈希计算重叠。
//
// Y2 修复：段缓冲改为「depth+1 个缓冲的环形池」复用。此前每段 make(16MiB)
// 用完即弃——10GiB 文件会产生 640 次 16MiB 分配，GC 压力大且内存尖峰高；
// 环形池把在途缓冲数固定为 depth+1，分配次数与段数解耦。
// 注意：环形池由本函数自管理，depth>0 时 buf 参数不再参与（仅 depth<=0 的顺序
// 退化路径使用），因此调用方无需为大文件路径预借读缓冲。
func HashFullSegmented(f *os.File, size int64, seg int64, depth int, buf []byte) ([32]byte, error) {
	if seg <= 0 {
		seg = LargeSeg
	}
	if depth <= 0 {
		return HashFull(f, size, buf)
	}
	type block struct {
		data []byte
		err  error
	}
	// 环形缓冲池：容量 depth+1（1 个在哈希 + depth 个在途预取）。
	// 生产者从 free 取、消费者写完后归还——池满即自然背压，不会死锁。
	free := make(chan []byte, depth+1)
	for i := 0; i < depth+1; i++ {
		free <- make([]byte, seg)
	}
	ch := make(chan block, depth)
	done := make(chan struct{})
	defer close(done) // 消费方提前返回时唤醒生产者退出，防 goroutine 与段缓冲泄漏
	go func() {
		defer close(ch)
		for off := int64(0); off < size; off += seg {
			end := off + seg
			if end > size {
				end = size
			}
			var full []byte
			select {
			case full = <-free:
			case <-done:
				return
			}
			b := full[:int(end-off)]
			n, err := f.ReadAt(b, off)
			if err == nil && int64(n) != int64(end-off) {
				// ReadAt 契约上「读满才返回 nil」——真读不满必带 io.EOF/
				// io.ErrUnexpectedEOF。这里只是把该不变式钉死，防底层实现异常时
				// 把未填充的零字节当内容喂进哈希。
				err = fmt.Errorf("hasher: 分段读短读（段 [%d,%d) 实际 %d 字节）: %w", off, end, n, io.ErrUnexpectedEOF)
			}
			select {
			case ch <- block{data: b, err: err}:
			case <-done:
				return
			}
		}
	}()
	h := blake3.New(32, nil)
	for b := range ch {
		if b.err != nil {
			return [32]byte{}, b.err // 提前返回：生产者由 done 唤醒并退出
		}
		if _, err := h.Write(b.data); err != nil {
			return [32]byte{}, err
		}
		// 归还缓冲（len 被截断，cap 仍为 seg；池容量恒够，归还不会阻塞）
		free <- b.data[:cap(b.data)]
	}
	// AS-H2：本函数绕过 HashFull 的顺序读路径自行 ReadAt 各段，变长守卫必须
	// 在这条路上也在场——否则 >512MiB 的追加型文件仍是假重复组的入口。
	if err := rejectGrowthBeyond(f, size); err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// Pool 段缓冲/读缓冲复用池（内存控制：sync.Pool）。
type Pool struct {
	stream sync.Pool // 1MiB 读缓冲
	small  sync.Pool // 128KiB 小文件缓冲
}

// NewPool 创建缓冲池。
func NewPool() *Pool {
	return &Pool{
		stream: sync.Pool{New: func() any { b := make([]byte, StreamChunk); return &b }},
		small:  sync.Pool{New: func() any { b := make([]byte, SmallFileMax); return &b }},
	}
}

// GetStreamBuf 1MiB 读缓冲。
func (p *Pool) GetStreamBuf() []byte  { return *p.stream.Get().(*[]byte) }
func (p *Pool) PutStreamBuf(b []byte) { p.stream.Put(&b) }

// GetSmallBuf 128KiB 预筛/小文件缓冲。
func (p *Pool) GetSmallBuf() []byte  { return *p.small.Get().(*[]byte) }
func (p *Pool) PutSmallBuf(b []byte) { p.small.Put(&b) }
