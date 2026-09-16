// Package hasher 哈希模块：首尾 xxHash64 预筛 + 小文件一趟双哈希 + 全量 BLAKE3（04 M1-T07/T08）。
package hasher

import (
	"errors"
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

// Partial 首尾 xxHash64 双指纹。
type Partial struct {
	Head uint64
	Tail uint64
}

// Result 哈希结果。
type Result struct {
	Partial Partial
	Full    [32]byte // BLAKE3-256
	Small   bool     // 小文件路径：一趟已完成全量哈希
}

// ErrClosed 由上层注入文件句柄失败时使用。
var ErrClosed = errors.New("hasher: file handle closed")

// HashHeadTail 预筛哈希（一次 open 已由调用方完成）：
//   - size ≤ SmallFileMax：读全文件，同趟完成 xxHash64 + BLAKE3（小文件捷径，决策 8）
//   - size >  SmallFileMax：ReadAt 头/尾各 64KiB 的 xxHash64
func HashHeadTail(f *os.File, size int64, buf []byte) (Result, error) {
	if size <= SmallFileMax {
		n, err := io.ReadFull(f, buf[:size])
		if err != nil && err != io.EOF {
			return Result{}, err
		}
		b := buf[:n]
		h := xxhash.Sum64(b)
		return Result{
			Partial: Partial{Head: h, Tail: h}, // 全文件：头尾指纹相同
			Full:    blake3.Sum256(b),
			Small:   true,
		}, nil
	}
	var r Result
	// 头部
	n, err := f.ReadAt(buf[:HeadTailChunk], 0)
	if err != nil && err != io.EOF {
		return Result{}, err
	}
	r.Partial.Head = xxhash.Sum64(buf[:n])
	// 尾部（size > 128KiB 时与头部不重叠）
	off := size - HeadTailChunk
	n, err = f.ReadAt(buf[:HeadTailChunk], off)
	if err != nil && err != io.EOF {
		return Result{}, err
	}
	r.Partial.Tail = xxhash.Sum64(buf[:n])
	return r, nil
}

// HashFull 顺序流式全量 BLAKE3（默认路径）。
func HashFull(f *os.File, size int64, buf []byte) ([32]byte, error) {
	h := blake3.New(32, nil)
	if _, err := io.CopyBuffer(h, io.LimitReader(f, size), buf); err != nil {
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
			_, err := f.ReadAt(b, off)
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
