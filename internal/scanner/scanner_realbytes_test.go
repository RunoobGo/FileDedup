package scanner

// M6-P2（2026-09-21）实占口径的遍历层探针（设计稿 §3.3 的 U2 遍历腿 + U3，
// 外加实施中新增的两条边界用例）。M28（2026-09-21）在其上补三条：卷级证据成立 /
// 不成立 / 与被过滤文件无关（设计稿 §11.3 的 V5~V7）。
//
// 这一层钉住的是"扫描器有没有把平台读数如实带进条目"：
//   - 稀疏文件：实占显著小于逻辑大小——本口径的全部收益所在；
//   - 普通文件：实占可读，且不小于逻辑大小（块对齐只会更大）；
//   - 全洞文件：**无证据的卷**上判为未知（M6-P2 的取舍），**有证据的卷**上
//     采信为真 0（M28 解锁的收益）——两侧各有一条用例，互为反向对照。
//
// 断言里刻意**不出现块大小常量**：512 与 4096 在不同卷上都会出现，把块尺寸
// 钉进断言等于把一个平台事实伪装成跨平台事实。
//
// ★ 夹具尺寸不是随手取的。本机 APFS 实测（04 §6.9.3）：truncate 到 16 MiB 再
// 在尾部写 1 KiB，整文件会被落地（实占=逻辑）；24 MiB 以上才真的留洞。
// 所以稀疏夹具必须 ≥ 32 MiB，否则用例会在一台正常的 macOS 上"正确地红"。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
	"filededup/internal/realbytes"
)

// sparseSize 稀疏夹具的逻辑大小（64 MiB，见文件头关于阈值的说明）。
const sparseSize = int64(64 << 20)

// mkSparse 造一个 size 字节、尾部写 tailLen 字节的文件，其余全是洞。
// 用 WriteAt 而非 buffered seek：后者会朝洞里灌零，夹具就不再是稀疏的。
func mkSparse(t *testing.T, path string, size int64, tailLen int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if tailLen > 0 {
		tail := make([]byte, tailLen)
		copy(tail, []byte("SPARSE-TAIL-MARKER"))
		if _, err := f.WriteAt(tail, size-tailLen); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// probeActual 读一个夹具的实占（不带卷级证据的单文件口径）。
// M28 之后没有"一步到位"的入口可用了，这里显式写两步：读原始读数 + 按
// "本卷未被证明"判定——正是 requireTailSparse 想问的那个问题。
func probeActual(t *testing.T, path string) (actual uint64, known bool) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Skipf("探针 stat 失败，跳过：%v", err)
	}
	rep, ok := realbytes.Reported(path, st)
	return realbytes.From(uint64(st.Size()), rep, ok, false)
}

// requireAllHoleReportsZero 环境前提（M28）：本卷对"一个字节都不写"的文件
// 报 0 blocks。不满足（把洞落地分配、或根本读不到）时 skip——判据在这种卷上
// 没有可断言的对象（照 scripts/test-windows-quarantine.sh 的约定：环境不满足
// 在使用点自探，不进隔离清单）。
//
// 这里**直接看原始读数**而不是走 realbytes.From：要问的正是"平台报了什么"，
// 而不是"判定成了什么"（后者正是 M28 要改的那一步）。
func requireAllHoleReportsZero(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("夹具 stat 失败：%v", err)
	}
	rep, ok := realbytes.Reported(path, st)
	if !ok {
		t.Skipf("本平台读不到实占（ok=false），全洞判据的前提不成立")
	}
	if rep != 0 {
		t.Skipf("本卷把全洞文件落地分配（报 %d 字节），全洞判据的前提不成立", rep)
	}
}

// requireTailSparse 环境前提：本卷对"大文件 + 尾部写"确实按稀疏计账。
//
// 不满足时 t.Skipf，而不是塞进 scripts/test-windows-quarantine.sh 的隔离清单
// ——遵循该脚本既有的约定：环境不满足应在使用点自探。隔离清单只用于
// "代码已知缺陷"，拿它装环境差异会把同族的真实回归一起吞掉。
func requireTailSparse(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "probe-tail-sparse.bin")
	mkSparse(t, probe, sparseSize, 1024)
	defer os.Remove(probe)
	actual, known := probeActual(t, probe)
	if !known {
		t.Skipf("本平台读不到实占（known=false），U2 的前提不成立")
	}
	if actual*4 >= uint64(sparseSize) {
		t.Skipf("该卷对 %d 字节文件报实占 %d 字节（不留洞/整文件落地），U2 的前提不成立",
			sparseSize, actual)
	}
}

// TestWalkRecordsSparseActualBelowSize U2 遍历腿：稀疏文件的实占如实小于逻辑。
func TestWalkRecordsSparseActualBelowSize(t *testing.T) {
	root := t.TempDir()
	requireTailSparse(t, root)
	mkSparse(t, filepath.Join(root, "sparse.bin"), sparseSize, 1024)

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 1 {
		t.Fatalf("收集数 = %d, want 1", len(res.Files))
	}
	e := res.Files[0]
	if !e.ActualKnown {
		t.Fatal("ActualKnown = false, want true（同一卷刚探到实占，采集链路却丢了它）")
	}
	if e.Size != uint64(sparseSize) {
		t.Fatalf("Size = %d, want %d（引入实占后逻辑口径必须不动）", e.Size, sparseSize)
	}
	if e.Actual*4 >= e.Size {
		t.Errorf("实占 %d 未显著小于逻辑 %d：稀疏文件的数量级虚高没有被纠正", e.Actual, e.Size)
	}
}

// TestWalkAllHoleFileNeverReportsZeroActual 全洞文件（不写任何数据）在**无证据**卷上的处置。
//
// 这是 M6-P2 里唯一一处**故意放弃收益**的判定，必须留字：
// st_blocks==0 有两种成因——① 文件全是洞（真占 0 字节）；② 该卷压根不跟踪
// 块数（sshfs/s3fs/fuse-overlayfs 一类 FUSE 与部分 NFS 客户端恒报 0）。
// 单看一个文件无法区分二者，于是两个方向只能挑一个错法：
//   - 认它是真 0：故障卷上"1 GB 重复组实占 0"，是一个自信的错误数字；
//   - 判它未统计（本实现）：全洞文件退回今天的行为（按逻辑计 + 标注未统计），
//     不新增任何错误结论，只是这一类收益拿不到。
//
// 选后者的依据是本轮贯穿全仓的一条纪律：数字宁可说"不知道"，不许说"知道且错"。
//
// ★ M28（2026-09-21）更新了这条注释（**断言一字未动**）：本用例的目录里只有全洞
// 文件，因此本卷在本趟遍历里**没有任何非零证据**，正好落在"未统计"一侧——它从
// "唯一可能的处置"变成了"无证据一侧的钉子"。有证据一侧（真 0）由
// TestWalkAllHoleOnProvenVolumeReportsRealZero 钉，两侧互为反向对照。
//
// 本用例不做环境探测、任何卷上都成立：全洞文件的 Actual 在两种卷行为下都必然
// 等于 Size（报 0 blocks → 回退逻辑；把洞落地 → 实占本来就等于逻辑）。
// 要钉的是那条绝对不变量——非空文件的实占**绝不为 0**。
func TestWalkAllHoleFileNeverReportsZeroActual(t *testing.T) {
	root := t.TempDir()
	mkSparse(t, filepath.Join(root, "holes.bin"), sparseSize, 0)

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 1 {
		t.Fatalf("收集数 = %d, want 1", len(res.Files))
	}
	e := res.Files[0]
	if e.Actual == 0 {
		t.Errorf("非空文件（%d 字节）实占报 0——恒报 0 blocks 的故障卷会就此出厂"+
			"『1 GB 重复组可释放 0 字节』这类自信的错误数字", e.Size)
	}
	if e.Actual != uint64(sparseSize) {
		t.Errorf("Actual = %d, want %d（全洞文件两种卷行为都应收敛到逻辑大小）",
			e.Actual, sparseSize)
	}
	if e.ActualKnown {
		t.Log("本卷把全洞落地分配：实占确实等于逻辑，known=true 合理")
	} else {
		t.Log("本卷对全洞报 0 blocks 且本趟无任何非零证据：按『未统计』回退逻辑口径" +
			"（M6-P2 的取舍；M28 只对有证据的卷解锁真 0）")
	}
}

// TestWalkActualKnownForPlainFile U3：普通文件的实占可读，且不会小于逻辑大小。
func TestWalkActualKnownForPlainFile(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, n int) {
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("one-kib.bin", 1024)  // 块粒度：实占必然 > 逻辑
	mk("aligned.bin", 1<<20) // 整块：两数相等
	mk("tiny.bin", 1)        // 最小非空：仍占一个块

	files := Walk(context.Background(), []string{root}, &model.Filters{}, 2).Files
	if len(files) != 3 {
		t.Fatalf("收集数 = %d, want 3", len(files))
	}
	for _, e := range files {
		name := filepath.Base(e.Path)
		if !e.ActualKnown {
			t.Errorf("%s: ActualKnown = false, want true", name)
			continue
		}
		if e.Actual < e.Size {
			t.Errorf("%s: 实占 %d < 逻辑 %d——实占不可能小于已经写进文件的字节数",
				name, e.Actual, e.Size)
		}
		// 上界只防"读错字段"这一类事故（把块数当字节数、把 inode 号当尺寸）。
		// 富余给 64 KiB：预分配与扩展属性都可能让实占略高，这里要的是量级
		// 正确，不是精确等式。
		if e.Actual > e.Size*2+64*1024 {
			t.Errorf("%s: 实占 %d 远超逻辑 %d，疑似读错字段", name, e.Actual, e.Size)
		}
	}
}

// TestWalkZeroSizeFileStillSkipped 钉住"0 字节内置跳过"不因实占采集而松动：
// 若有人为了让"实占口径完整"放 0 字节文件进来，语料会凭空多出一堆空文件组。
func TestWalkZeroSizeFileStillSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.bin"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 0 {
		t.Fatalf("0 字节文件进了语料：%+v", res.Files)
	}
}

// TestWalkAllHoleOnProvenVolumeReportsRealZero M28（设计稿 §11.3 V5）：本卷已有
// "会报非零实占"的证据时，全洞文件的 0 采信为真读数。
//
// 证据就是同目录那枚普通文件——它报出非零即证明本卷在 st_blocks 里报真实分配，
// 于是全洞文件的 0 不再是"故障卷恒报 0"的可疑形态。这条与 U6 是同一枚夹具的
// 两个世界：U6 的目录里只有全洞文件（无证据 ⇒ 未统计），本用例多一枚普通文件。
func TestWalkAllHoleOnProvenVolumeReportsRealZero(t *testing.T) {
	root := t.TempDir()
	hole := filepath.Join(root, "holes.bin")
	mkSparse(t, hole, sparseSize, 0)
	requireAllHoleReportsZero(t, hole)
	if err := os.WriteFile(filepath.Join(root, "plain.bin"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 2 {
		t.Fatalf("收集数 = %d, want 2", len(res.Files))
	}
	for _, e := range res.Files {
		switch filepath.Base(e.Path) {
		case "holes.bin":
			if !e.ActualKnown {
				t.Fatal("全洞条目 ActualKnown = false：同卷普通文件的非零读数没有构成证据")
			}
			if e.Actual != 0 {
				t.Errorf("全洞条目实占 = %d, want 0", e.Actual)
			}
			if e.ActualBytes() != 0 {
				t.Errorf("ActualBytes = %d, want 0（组级可释放量由此归零）", e.ActualBytes())
			}
		case "plain.bin":
			if !e.ActualKnown || e.Actual == 0 {
				t.Errorf("普通条目 = (%d,%v), want 非零且 known", e.Actual, e.ActualKnown)
			}
		default:
			t.Errorf("意外条目 %s", e.Path)
		}
	}
}

// TestWalkAllHoleOnUnprovenVolumeStaysUnknown M28（设计稿 §11.3 V6）：**没有**任何
// 非零证据的卷上，全洞文件的 0 一律判"未统计"（fail-closed）——这正是"恒报 0 的
// 故障卷"在读数上的形态，也是 M6-P2 规则 2 的全部理由。
//
// 它是 V5 的反向对照：证据机制被拆掉（M-M28-a/c）时本用例仍绿、V5 红；
// 而"信任判据放宽"（M-M28-b/c）时本用例红。两条各守一边，缺一条就等于没有判据。
func TestWalkAllHoleOnUnprovenVolumeStaysUnknown(t *testing.T) {
	root := t.TempDir()
	hole := filepath.Join(root, "holes.bin")
	mkSparse(t, hole, sparseSize, 0)
	requireAllHoleReportsZero(t, hole)

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if len(res.Files) != 1 {
		t.Fatalf("收集数 = %d, want 1", len(res.Files))
	}
	e := res.Files[0]
	if e.ActualKnown {
		t.Error("ActualKnown = true：本趟没有任何非零证据，0 却被采信了")
	}
	if e.Actual != uint64(sparseSize) {
		t.Errorf("Actual = %d, want %d（未统计退回逻辑口径）", e.Actual, sparseSize)
	}
}

// TestWalkEvidenceIgnoresFilters M28（设计稿 §11.1 的口径选择，§11.3 V7）：卷证据的
// 采集**不看用户过滤**——被 ExcludeExts 挡掉的文件同样构成证据（卷会不会报非零
// 实占，与该卷上用户想不想要某类文件无关）。采集点若退回 matcher.Apply 之后，
// 本例即红（变异 M-M28-g）。
func TestWalkEvidenceIgnoresFilters(t *testing.T) {
	root := t.TempDir()
	hole := filepath.Join(root, "holes.bin")
	mkSparse(t, hole, sparseSize, 0)
	requireAllHoleReportsZero(t, hole)
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Walk(context.Background(), []string{root}, &model.Filters{ExcludeExts: []string{".txt"}}, 2)
	if len(res.Files) != 1 {
		t.Fatalf("收集数 = %d, want 1（.txt 已被排除，证据不该把文件带进语料）", len(res.Files))
	}
	e := res.Files[0]
	if filepath.Base(e.Path) != "holes.bin" {
		t.Fatalf("意外条目 %s", e.Path)
	}
	if !e.ActualKnown || e.Actual != 0 {
		t.Errorf("全洞条目 = (%d,%v), want (0,true)：被排除的普通文件也是本卷证据", e.Actual, e.ActualKnown)
	}
}
