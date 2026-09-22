package hasher

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"lukechampine.com/blake3"
)

func writeFile(t *testing.T, size int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.bin")
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHashHeadTailSmallFile(t *testing.T) {
	// 小文件（≤128KiB）：一趟双哈希，xxHash 与 BLAKE3 均为全文件哈希
	for _, size := range []int{1, 1000, SmallFileMax} {
		p := writeFile(t, size)
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, SmallFileMax)
		r, err := HashHeadTail(f, int64(size), buf)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !r.Small {
			t.Fatalf("size=%d 应走小文件路径", size)
		}
		raw, _ := os.ReadFile(p)
		if want := xxhash.Sum64(raw); r.Partial.Head != want || r.Partial.Tail != want {
			t.Fatalf("size=%d xxHash 指纹不等于全文件哈希", size)
		}
		if want := blake3.Sum256(raw); r.Full != want {
			t.Fatalf("size=%d BLAKE3 不等于全文件哈希", size)
		}
	}
}

func TestHashHeadTailLargeFile(t *testing.T) {
	// 大文件（128KiB+1 起）：仅首尾指纹，头尾不重叠
	size := SmallFileMax + 1
	p := writeFile(t, size)
	f, _ := os.Open(p)
	buf := make([]byte, SmallFileMax)
	r, err := HashHeadTail(f, int64(size), buf)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if r.Small {
		t.Fatal("128KiB+1 应走大文件路径")
	}
	raw, _ := os.ReadFile(p)
	if want := xxhash.Sum64(raw[:HeadTailChunk]); r.Partial.Head != want {
		t.Fatal("头部指纹错误")
	}
	if want := xxhash.Sum64(raw[len(raw)-HeadTailChunk:]); r.Partial.Tail != want {
		t.Fatal("尾部指纹错误")
	}
}

func TestHashHeadTailIdenticalFiles(t *testing.T) {
	// H1 修正后语义：预筛采 4 点（头/中点/3/4/尾）。
	//   - 相同内容 → 相同指纹；
	//   - 改在采样窗口内（size/2 处）→ 预筛即可区分（旧两点采样漏检的正是这类）；
	//   - 改在未采样的空隙 [64KiB,128KiB) → 预筛指纹仍相同，正确性由全量哈希兜底。
	base := bytes.Repeat([]byte("A"), SmallFileMax*2)
	p1 := filepath.Join(t.TempDir(), "1.bin")
	p2 := filepath.Join(t.TempDir(), "2.bin")
	p3 := filepath.Join(t.TempDir(), "3.bin")
	if err := os.WriteFile(p1, base, 0o644); err != nil {
		t.Fatal(err)
	}
	poke := func(off, n int, b byte) []byte {
		m := append([]byte{}, base...)
		for i := off; i < off+n; i++ {
			m[i] = b
		}
		return m
	}
	// size/2 = 128KiB：落在 mid1 采样窗口 [128KiB,192KiB) 内
	if err := os.WriteFile(p2, poke(SmallFileMax, 64, 'B'), 0o644); err != nil {
		t.Fatal(err)
	}
	// 96KiB：落在未采样空隙 [64KiB,128KiB) 内
	if err := os.WriteFile(p3, poke(SmallFileMax/2+HeadTailChunk/2, 64, 'C'), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, h2, h3 := headTail(t, p1), headTail(t, p2), headTail(t, p3)
	if h1 != headTail(t, p1) {
		t.Fatal("相同内容的预筛指纹必须一致")
	}
	if h1 == h2 {
		t.Fatal("中段采样窗口内的改动应被 4 点预筛区分（H1 回归）")
	}
	if h1 != h3 {
		t.Fatal("未采样空隙内的改动不应影响预筛指纹")
	}
	full1, full3 := fullOf(t, p1), fullOf(t, p3)
	if full1 == full3 {
		t.Fatal("内容不同的文件全量哈希不应相同")
	}
}

func headTail(t *testing.T, p string) Partial {
	t.Helper()
	f, _ := os.Open(p)
	defer f.Close()
	st, _ := os.Stat(p)
	r, err := HashHeadTail(f, st.Size(), make([]byte, SmallFileMax))
	if err != nil {
		t.Fatal(err)
	}
	return r.Partial
}

func fullOf(t *testing.T, p string) [32]byte {
	t.Helper()
	f, _ := os.Open(p)
	defer f.Close()
	st, _ := os.Stat(p)
	out, err := HashFull(f, st.Size(), make([]byte, StreamChunk))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHashFullSegmentedEquivalence(t *testing.T) {
	// 强等价：分段预取流水线结果 == 顺序流式结果（04 M1-T08 DoD）
	// 用小参数模拟大文件路径：size=1MB, seg=64KB, depth=2
	size := 1 << 20
	p := writeFile(t, size)
	f, _ := os.Open(p)
	seq, err := HashFull(f, int64(size), make([]byte, StreamChunk))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	f, _ = os.Open(p)
	seg, err := HashFullSegmented(f, int64(size), 64<<10, 2, make([]byte, StreamChunk))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if seq != seg {
		t.Fatal("分段并行哈希与顺序哈希结果不一致")
	}
}

// TestHashFullReadErrorPropagates M129（第 2 轮 §23.10）。
//
// 改前这条叫 TestHashFullOpenError，函数体只有 `os.Open(missing)` + `if err == nil`
// ——**从未调用 HashFull**，断的是标准库会不会打开一个不存在的文件。
// 于是 HashFull 读路径上的任何守卫被改坏（把 CopyBuffer 的错误吞掉、
// 或错误地当成"读到 0 字节即结束"）它都照绿，是 §22/§23 反复处理的同一形状：
// 名字承诺了一个判据，体子里没有那个判据。
//
// 现在真调：句柄有效但已关闭 ⇒ 读必失败（Go 在三条腿上都是 fs.ErrClosed，
// 不是 EOF），错误必须原样交回、且返回值必须是零数组——
// 调用方按"进失败清单"处理，绝不能拿一个看似正常的指纹去比内容。
func TestHashFullReadErrorPropagates(t *testing.T) {
	const size = 4096
	p := writeFile(t, size)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, StreamChunk)
	if _, err := HashFull(f, size, buf); err != nil {
		f.Close()
		t.Fatalf("基线（句柄有效）应成功：%v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := HashFull(f, size, buf)
	if err == nil {
		t.Fatalf("句柄已关 ⇒ HashFull 必须报错，实得指纹 %x（静默出指纹=把读不动的文件当内容参与去重）", out)
	}
	if out != ([32]byte{}) {
		t.Errorf("出错时不得交回部分指纹，实得 %x（err=%v）", out, err)
	}
	// M136（第 3 轮 §24.4 HAS-4）：上面两条只断言「回来的是个错误」，判的并不是
	// hasher.go:260-262 那一格——把 `if err != nil { return [32]byte{}, err }` 整块删掉
	// （吞掉 io.CopyBuffer 的读错误）之后，n(=0) != size 立即成立，函数掉进下面的短读支
	// **照样报错**，只是换成一枚包装 io.ErrUnexpectedEOF 的错误 ⇒ 变异全绿（§24.2 R3-MU2）。
	// 也就是说「句柄已关」与「文件真的短读」两类故障在这两条断言下不可分：被吞掉的读错误
	// 会以「文件短读」的面目出现，真因（谁把读打断了）就此丢失。
	// 因此补上错误**身份**判据：已关闭句柄的读在三条腿上都是 fs.ErrClosed，
	// 短读支包装的是 io.ErrUnexpectedEOF，两者可分 ⇒ 只认前者，且必须原样交回。
	if !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("错误身份不符：HashFull 必须原样交回读错误的真因（fs.ErrClosed），实得 %v "+
			"（若它其实是 io.ErrUnexpectedEOF，说明 CopyBuffer 的错误被吞了、"+
			"这一格由短读支代答，判据没钉在 hasher.go:260-262 上）", err)
	}
}

// TestHashFullSegmentedReusesBuffers Y2 回归：段缓冲必须复用而非每段新分配。
// 修复前每段独立 make(seg)（nSeg 次分配）；环形池化后额外分配应仅为
// (depth+1)×seg 量级，与段数解耦。
func TestHashFullSegmentedReusesBuffers(t *testing.T) {
	const seg, nseg = 1 << 20, 32 // 32MiB 文件 / 1MiB 段
	size := seg * nseg
	p := writeFile(t, size)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := HashFullSegmented(f, int64(size), seg, 2, nil); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)

	allocated := after.TotalAlloc - before.TotalAlloc
	// 环形池 (2+1)×1MiB = 3MiB；修复前为 32MiB
	if allocated > 8<<20 {
		t.Fatalf("分段哈希额外分配 %d 字节（>8MiB），段缓冲疑似未复用（Y2 回归）", allocated)
	}
	t.Logf("32MiB 文件分段哈希额外分配 = %d 字节", allocated)
}

// TestHashFullSegmentedNoGoroutineLeakOnError 回归：读错误提前返回后，
// 生产者 goroutine 必须及时退出（旧实现阻塞在 ch <- 永久泄漏）。
func TestHashFullSegmentedNoGoroutineLeakOnError(t *testing.T) {
	// 实际文件小于声明的 size → 首段 ReadAt 返回 io.EOF 触发错误提前返回
	p := writeFile(t, 4096)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before := runtime.NumGoroutine()
	for i := 0; i < 8; i++ {
		if _, err := HashFullSegmented(f, 1<<20, 64<<10, 2, make([]byte, StreamChunk)); err == nil {
			t.Fatal("预期读取错误")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > before+2 {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Fatalf("疑似 goroutine 泄漏: before=%d after=%d", before, n)
	}
}
