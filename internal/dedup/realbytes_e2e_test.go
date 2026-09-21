package dedup

// M6-P2（2026-09-21）实占口径的组级会计探针（设计稿 §3.3 的 U2 组腿 + U4）。
//
// 组级是这套数字唯一被用户看到的地方（"清这一组能腾出多少"），所以断言必须
// 打在 Pipeline 的输出上，而不是只打在条目字段上。三条形状：
//   - 逻辑口径 Reclaimable **一字不改**（历史可比）；
//   - 实占口径 ReclaimableActual 只算**冗余成员**，保留项不计；
//   - 稀疏文件组的实占显著小于逻辑——收益本体。
//
// 平台读数走 realbytes 的真实链路（本包不 mock），因此这些用例在 Linux 门禁
// 上就是真夹具真断言；Windows 腿（GetCompressedFileSizeW）仍只有交叉编译证据。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
	"filededup/internal/realbytes"
)

// u4Payload 普通夹具的尺寸（>SmallFileMax 会走 head+tail 路径，这里取 300 KiB
// 与同包既有用例一致，确保进入的是同一条哈希路径）。
const u4Payload = 300 << 10

// u4SparseSize 稀疏夹具的逻辑大小，与 internal/scanner 同一取值。
// 必须 ≥ 32 MiB：本机 APFS 对 16 MiB 以下的"truncate + 尾部写"会整文件落地
// （实占=逻辑），小夹具会在正常机器上正确地红。详见 04 §6.9.3 前置实验。
const u4SparseSize = int64(64 << 20)

func writeSame(t *testing.T, paths []string, payload []byte) {
	t.Helper()
	for _, p := range paths {
		if err := os.WriteFile(p, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runOnce(t *testing.T, root string) []*model.DuplicateGroup {
	t.Helper()
	groups, failed, err := New().Run(context.Background(), model.ScanConfig{
		Roots:   []string{root},
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(failed) > 0 {
		t.Fatalf("不该有失败项：%+v", failed)
	}
	return groups
}

func soleGroup(t *testing.T, groups []*model.DuplicateGroup, wantMembers int) *model.DuplicateGroup {
	t.Helper()
	if len(groups) != 1 {
		t.Fatalf("组数 = %d, want 1（夹具只造一组）：%+v", len(groups), groups)
	}
	if len(groups[0].Files) != wantMembers {
		t.Fatalf("组内成员 = %d, want %d", len(groups[0].Files), wantMembers)
	}
	return groups[0]
}

// TestGroupReclaimableActualExcludesKeep U4：三成员组的两个口径逐项对齐。
func TestGroupReclaimableActualExcludesKeep(t *testing.T) {
	root := t.TempDir()
	payload := make([]byte, u4Payload)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	paths := []string{
		filepath.Join(root, "a1.bin"),
		filepath.Join(root, "a2.bin"),
		filepath.Join(root, "sub_a3.bin"),
	}
	writeSame(t, paths, payload)

	g := soleGroup(t, runOnce(t, root), 3)

	// ① 逻辑口径一字不改：(n-1)*size
	if want := uint64(2 * u4Payload); g.Reclaimable != want {
		t.Errorf("Reclaimable = %d, want %d（逻辑口径是历史表的既有语义，不许动）", g.Reclaimable, want)
	}
	// ② 实占口径 = 冗余成员（下标 1..n-1，files[0] 是保留项）之和
	var redundant, all uint64
	for i, f := range g.Files {
		all += f.ActualBytes()
		if i > 0 {
			redundant += f.ActualBytes()
		}
	}
	if g.ReclaimableActual != redundant {
		t.Errorf("ReclaimableActual = %d, want %d（只该算冗余成员）", g.ReclaimableActual, redundant)
	}
	if g.ReclaimableActual == all {
		t.Errorf("ReclaimableActual = %d 等于组内**全部**成员之和——保留项被算进可释放量，"+
			"这一组会被报成能腾出 3 份而不是 2 份", g.ReclaimableActual)
	}
	if !g.AnyActualKnown() {
		t.Error("AnyActualKnown = false, want true（本卷读得到 st_blocks）")
	}
	// ③ 普通文件的两个口径应当只差块对齐，实占 ≥ 逻辑。
	if g.ReclaimableActual < g.Reclaimable {
		t.Errorf("普通文件组：实占合计 %d < 逻辑合计 %d，不成立",
			g.ReclaimableActual, g.Reclaimable)
	}
}

// TestGroupSparseReclaimsFarLessThanLogical U2 组腿：稀疏重复组的实占远小于逻辑。
//
// 修正前这个数是 8 MiB × 1，而盘上真正能腾出的只有几 KiB——用户照着逻辑数字
// 决定"值得清理"，清完发现空间一点没少。
func TestGroupSparseReclaimsFarLessThanLogical(t *testing.T) {
	root := t.TempDir()
	requireU4Sparse(t, root)

	mk := func(name string) {
		mkSparseFile(t, filepath.Join(root, name), u4SparseSize, 1024)
	}
	mk("s1.bin")
	mk("s2.bin")

	g := soleGroup(t, runOnce(t, root), 2)
	if want := uint64(u4SparseSize); g.Reclaimable != want {
		t.Errorf("Reclaimable = %d, want %d", g.Reclaimable, want)
	}
	if g.ReclaimableActual*4 >= g.Reclaimable {
		t.Errorf("稀疏组实占合计 %d 未显著小于逻辑 %d：数量级虚高仍在",
			g.ReclaimableActual, g.Reclaimable)
	}
}

// requireU4Sparse 环境前提：本卷对"大文件 + 尾部写"确实按稀疏计账，否则跳过。
// （跳过而非进隔离清单：理由同 scripts/test-windows-quarantine.sh 头部说明——
// 环境差异不该占用"代码已知缺陷"的白名单。）
func requireU4Sparse(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "probe-sparse.bin")
	mkSparseFile(t, probe, u4SparseSize, 1024)
	defer os.Remove(probe)
	st, err := os.Stat(probe)
	if err != nil {
		t.Skipf("探针 stat 失败：%v", err)
	}
	rep, rok := realbytes.Reported(probe, st)
	actual, known := realbytes.From(uint64(u4SparseSize), rep, rok, false)
	if !known {
		t.Skipf("本平台读不到实占，本用例前提不成立")
	}
	if actual*4 >= uint64(u4SparseSize) {
		t.Skipf("该卷对 %d 字节文件报实占 %d 字节（不留洞），本用例前提不成立", u4SparseSize, actual)
	}
}

// mkSparseFile 造一个 size 字节、尾部写 tailLen 字节的文件，其余全是洞。
// 用 WriteAt 而非 buffered seek：后者会朝洞里灌零，夹具就不再是稀疏的。
func mkSparseFile(t *testing.T, path string, size, tailLen int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	tail := make([]byte, tailLen)
	copy(tail, []byte("SPARSE-TAIL-MARKER"))
	if _, err := f.WriteAt(tail, size-tailLen); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
