package hasher

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// AS-H2（2026-09-20 全仓审计，第 7 篇 §三）：只防"变短"不防"变长" → 假重复组。
//
// 2026-09-19 的修复把 io.LimitReader 的静默截断改成显式计数（n != size 即报错），
// 但只对「实际比声明短」生效：文件在扫描期间**长过** size 时，LimitReader 照样
// 把尾部静默切掉，n == size 顺利通过。于是
//
//	A = "PREFIX"（扫描时 6 字节）
//	B = "PREFIX" + "刚追加的新数据"
//
// 两者按声明 size=6 哈希得到**同一个** BLAKE3 → 进同一组 → 用户删掉含新数据的
// 那一份。触发面是常见工况：断点续传、追加型日志、正在写入的镜像文件。
// 修法与短读同等待遇：宁可入失败清单，也不给出可能错删的分组证据。

func mkFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func hashFullOf(t *testing.T, p string, declared int64) ([32]byte, error) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, StreamChunk)
	return HashFull(f, declared, buf)
}

// TestHashFullRejectsFileLongerThanDeclaredSize 声明长度之外还有可读字节 → 报错。
func TestHashFullRejectsFileLongerThanDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	p := mkFile(t, dir, "grew.bin", bytes.Repeat([]byte("A"), 4096))

	if _, err := hashFullOf(t, p, 1024); err == nil {
		t.Fatal("文件在声明 size 之后仍有 3072 字节可读（扫描期间变长），" +
			"HashFull 却静默给出「前 1024 字节」的哈希——这会与同样只有前段相同的" +
			"另一文件判成重复，用户删掉的是含新数据的那一份（AS-H2）")
	} else {
		t.Logf("正确报错: %v", err)
	}
}

// TestHashFullFalseGroupFromAppend 假重复组的构造性证明：
// 两个内容不同的文件，按较短那份的声明长度哈希，不得得出相同结果。
func TestHashFullFalseGroupFromAppend(t *testing.T) {
	dir := t.TempDir()
	prefix := bytes.Repeat([]byte("P"), 2048)
	a := mkFile(t, dir, "a.bin", prefix)
	b := mkFile(t, dir, "b.bin", append(append([]byte{}, prefix...), []byte("-appended-after-scan")...))

	ha, err := hashFullOf(t, a, 2048)
	if err != nil {
		t.Fatalf("a 未变长，不应报错: %v", err)
	}
	hb, err := hashFullOf(t, b, 2048)
	if err == nil {
		if ha == hb {
			t.Fatal("两个内容不同的文件得出同一全量哈希 → 假重复组（AS-H2）：" +
				"按声明长度哈希前必须确认其后无字节可读")
		}
		t.Logf("两哈希不同（未构成假组），但仍需拒绝变长文件: %x", hb)
	}
}

// TestHashFullAcceptsExactlyDeclaredSize 负例：长度正好等于声明值时必须放行，
// 否则每个正常文件都会被判失败（守卫过严同样是缺陷）。
func TestHashFullAcceptsExactlyDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	p := mkFile(t, dir, "exact.bin", bytes.Repeat([]byte("E"), 3000))
	if _, err := hashFullOf(t, p, 3000); err != nil {
		t.Fatalf("长度恰等于声明值却报错: %v", err)
	}
}

// TestHashHeadTailSmallRejectsGrowth 小文件一趟（size ≤ SmallFileMax）同受其害：
// io.ReadFull 读满声明长度即返回成功，尾部新数据被无声忽略，Full 字段直接入组。
func TestHashHeadTailSmallRejectsGrowth(t *testing.T) {
	dir := t.TempDir()
	p := mkFile(t, dir, "small-grew.bin", bytes.Repeat([]byte("A"), 4096))

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, SmallFileMax)
	if _, err := HashHeadTail(f, 1024, buf); err == nil {
		t.Fatal("小文件趟在文件变长时必须同样报错，不得给出「前 1024 字节即全文件」的结论")
	}
}

// TestHashHeadTailSmallAcceptsExactSize 负例：小文件长度恰等声明值时正常返回。
func TestHashHeadTailSmallAcceptsExactSize(t *testing.T) {
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("E"), 2048)
	p := mkFile(t, dir, "small-exact.bin", payload)

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, SmallFileMax)
	r, err := HashHeadTail(f, 2048, buf)
	if err != nil {
		t.Fatalf("长度恰等声明值却报错: %v", err)
	}
	if !r.Small || r.Short {
		t.Fatalf("小文件路径标记异常: Small=%v Short=%v", r.Small, r.Short)
	}
}

// TestHashFullSegmentedRejectsGrowth 大文件分段流水线：它绕过 HashFull 的顺序读
// 路径（depth>0 时自行 ReadAt 各段），变长守卫必须在那条路上也在场。
func TestHashFullSegmentedRejectsGrowth(t *testing.T) {
	dir := t.TempDir()
	p := mkFile(t, dir, "big-grew.bin", bytes.Repeat([]byte("B"), 3*LargeSeg+4096))

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := HashFullSegmented(f, int64(3*LargeSeg), LargeSeg, 2, nil); err == nil {
		t.Fatal("分段路径在文件变长时必须报错（AS-H2 同一缺陷的另一条入口）")
	}
}

// TestHashFullSegmentedAcceptsExactSize 负例：分段路径长度吻合时不得误判。
func TestHashFullSegmentedAcceptsExactSize(t *testing.T) {
	dir := t.TempDir()
	size := int64(2*LargeSeg + 12345)
	p := mkFile(t, dir, "big-exact.bin", bytes.Repeat([]byte("C"), int(size)))

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := HashFullSegmented(f, size, LargeSeg, 2, nil)
	if err != nil {
		t.Fatalf("长度恰等声明值却报错: %v", err)
	}
	want, err := hashFullOf(t, p, size)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("分段与顺序全量哈希结果不一致")
	}
}
