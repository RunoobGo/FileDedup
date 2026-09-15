package hasher

import (
	"bytes"
	"crypto/rand"
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
	// 相同内容 → 相同指纹；首尾相同中间不同 → 预筛相同（正确性由全量兜底）
	base := bytes.Repeat([]byte("A"), SmallFileMax*2)
	p1 := filepath.Join(t.TempDir(), "1.bin")
	p2 := filepath.Join(t.TempDir(), "2.bin")
	if err := os.WriteFile(p1, base, 0o644); err != nil {
		t.Fatal(err)
	}
	mid := append(append([]byte{}, base[:SmallFileMax]...), append(bytes.Repeat([]byte("B"), 64), base[SmallFileMax+64:]...)...)
	if err := os.WriteFile(p2, mid, 0o644); err != nil {
		t.Fatal(err)
	}
	h1, h2 := headTail(t, p1), headTail(t, p2)
	if h1 != h2 {
		t.Fatal("首尾相同中间不同的文件预筛指纹应相同（全量哈希兜底正确性）")
	}
	full1, full2 := fullOf(t, p1), fullOf(t, p2)
	if full1 == full2 {
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

func TestHashFullOpenError(t *testing.T) {
	// ReadAt 失败注入：不存在的文件 → 错误返回（不 panic）
	f, err := os.Open(filepath.Join(t.TempDir(), "missing.bin"))
	if err == nil {
		f.Close()
		t.Fatal("预期打开失败")
	}
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
