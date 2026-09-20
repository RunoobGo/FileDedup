package hasher

// 短读（short read）语义覆盖（2026-09-19 修复）。
//
// 背景：Windows 上「扫描有动作但 0 组 + 未保存缓存」的根因是
// HashHeadTail 把 io.ErrUnexpectedEOF 当真实错误返回，调用方据此把文件
// 整个剔除出预筛分组（pipeline 的 skip 分支），于是：
//   - 它与任何文件都不可能同桶 → 整组重复静默消失
//   - 它永远走不到 pending 入队那一行 → 缓存里永远没有它
// windows CI 的隔离清单（scripts/test-windows-quarantine.sh）原文已记录该
// 现象：「子目录里的副本文件在 prefilter 阶段报 unexpected EOF（记录 size
// 大于实际可读长度）……首扫 0 组」。
//
// 本文件两头钉住修复后的契约：
//   1. 短读不得报错（否则文件被丢弃）
//   2. 短读必须被标记，且 ActualSize 如实反映实际长度（否则会造出假重复组）

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writeNamed 写入内容并返回路径（不复用 hasher_test.go 的 writeFile，避免重名）。
func writeNamed(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestHashHeadTailShortReadSmallFile 小文件路径：记录 size 大于实际长度。
// 修正前会返回 io.ErrUnexpectedEOF → 文件被 pipeline 丢弃。
func TestHashHeadTailShortReadSmallFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello-duplicate-content-0123456789") // 34 字节
	p := writeNamed(t, dir, "a.bin", content)

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// 谎报 size 比真实大 1 字节（模拟「目录遍历与 open 之间文件被截断」）
	declared := int64(len(content)) + 1
	buf := make([]byte, SmallFileMax)
	r, err := HashHeadTail(f, declared, buf)

	if err != nil {
		t.Fatalf("短读被当成错误返回（这正是 Windows 0 组的根因）: %v", err)
	}
	if !r.Short {
		t.Fatalf("短读未被标记 Short：declared=%d actual=%d", declared, r.ActualSize)
	}
	if r.ActualSize != int64(len(content)) {
		t.Fatalf("ActualSize = %d, want %d（必须如实反映实际可读长度，否则会造出假重复组）",
			r.ActualSize, len(content))
	}
	if !r.Small {
		t.Fatalf("小文件路径应标记 Small")
	}
	// 哈希必须对应「实际读到的 34 字节」，而不是 35 字节（后者根本不存在）
	f2, _ := os.Open(p)
	defer f2.Close()
	want, _ := HashHeadTail(f2, int64(len(content)), buf)
	if r.Full != want.Full {
		t.Fatalf("短读的 full 哈希与「按真实长度读」不一致：%x vs %x", r.Full[:8], want.Full[:8])
	}
}

// TestHashHeadTailShortReadLargeFile 大文件路径：记录 size 大于实际长度。
// 四个采样点中靠后的点会读不满，必须不报错且标记 Short。
func TestHashHeadTailShortReadLargeFile(t *testing.T) {
	dir := t.TempDir()
	content := make([]byte, 200<<10) // 200KiB > SmallFileMax(128KiB)
	for i := range content {
		content[i] = byte(i % 251)
	}
	p := writeNamed(t, dir, "big.bin", content)

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// 谎报更大 size：尾点偏移 (declared-chunk) 会落到真实文件之外
	declared := int64(len(content)) + 300<<10
	buf := make([]byte, SmallFileMax)
	r, err := HashHeadTail(f, declared, buf)

	if err != nil {
		t.Fatalf("大文件短读被当成错误返回: %v", err)
	}
	if !r.Short {
		t.Fatalf("大文件短读未被标记 Short")
	}
	if r.Small {
		t.Fatalf("200KiB 文件不应走小文件路径")
	}
	// ActualSize 必须落在「头部已确证可读」与「声明长度」之间，且不得高估到
	// 把另一个真实文件误判为同长度。这里要求它不超过真实长度 + 一个采样块。
	if r.ActualSize <= 0 || r.ActualSize > declared {
		t.Fatalf("ActualSize = %d 不合理（declared=%d, real=%d）",
			r.ActualSize, declared, len(content))
	}
	// 关键约束：不得高估（否则分桶键失真，可能造出假重复组）。
	// 允许的上界是「真实长度 + 一个 64KiB 采样块」——大文件路径只能靠
	// 采样点定位尾部，这是该口径下可达的精度上限。
	if r.ActualSize > int64(len(content))+HeadTailChunk {
		t.Fatalf("ActualSize = %d 相对真实长度 %d 高估超过一个采样块（会致分桶键失真）",
			r.ActualSize, len(content))
	}
	t.Logf("declared=%d real=%d 推导 ActualSize=%d（误差 %d 字节）",
		declared, len(content), r.ActualSize, int64(len(content))-r.ActualSize)
}

// TestHashHeadTailNormalNotFlagged 正常文件不得被误标 Short（防修复过度触发）。
func TestHashHeadTailNormalNotFlagged(t *testing.T) {
	dir := t.TempDir()
	small := writeNamed(t, dir, "s.txt", []byte("exact-content"))
	f, _ := os.Open(small)
	defer f.Close()
	buf := make([]byte, SmallFileMax)
	r, err := HashHeadTail(f, 13, buf)
	if err != nil {
		t.Fatalf("正常文件报错: %v", err)
	}
	if r.Short {
		t.Fatalf("正常小文件被误标 Short")
	}
	if r.ActualSize != 13 {
		t.Fatalf("正常小文件 ActualSize = %d, want 13", r.ActualSize)
	}

	big := make([]byte, 300<<10)
	for i := range big {
		big[i] = byte(i % 97)
	}
	bp := writeNamed(t, dir, "b.bin", big)
	f2, _ := os.Open(bp)
	defer f2.Close()
	r2, err := HashHeadTail(f2, int64(len(big)), buf)
	if err != nil {
		t.Fatalf("正常大文件报错: %v", err)
	}
	if r2.Short {
		t.Fatalf("正常大文件被误标 Short")
	}
	if r2.ActualSize != int64(len(big)) {
		t.Fatalf("正常大文件 ActualSize = %d, want %d", r2.ActualSize, len(big))
	}
}

// TestHashFullReportsShortRead HashFull 不得静默吞掉短读。
//
// 修正前用 io.LimitReader 包一层，读不满即 io.EOF，io.CopyBuffer 视其为
// 正常结束 → 静默算出「较短那段内容」的哈希，调用方毫不知情。
func TestHashFullReportsShortRead(t *testing.T) {
	dir := t.TempDir()
	content := []byte("short-content") // 13 字节
	p := writeNamed(t, dir, "s.bin", content)

	f, _ := os.Open(p)
	defer f.Close()
	buf := make([]byte, StreamChunk)

	// 谎报 size 比真实大 100 字节
	_, err := HashFull(f, int64(len(content))+100, buf)
	if err == nil {
		t.Fatalf("HashFull 静默吞掉了短读（应报错，否则会算出错误内容的哈希）")
	}
	if !errorsIs(err, io.ErrUnexpectedEOF) {
		t.Logf("错误未包装 io.ErrUnexpectedEOF，实际: %v", err)
	}

	// 正确 size 必须正常返回
	f2, _ := os.Open(p)
	defer f2.Close()
	sum, err2 := HashFull(f2, int64(len(content)), buf)
	if err2 != nil {
		t.Fatalf("正确 size 报错: %v", err2)
	}
	if sum == ([32]byte{}) {
		t.Fatalf("正常路径返回零哈希")
	}
}

// errorsIs 用 errors.Is 判定（避免直接 import errors 造成命名冲突时的可读性问题）。
func errorsIs(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
