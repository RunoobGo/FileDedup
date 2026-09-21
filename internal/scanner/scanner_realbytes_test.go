package scanner

// M6-P2（2026-09-21）实占口径的遍历层探针（设计稿 §3.3 的 U2 遍历腿 + U3，
// 外加实施中新增的两条边界用例）。
//
// 这一层钉住的是"扫描器有没有把平台读数如实带进条目"：
//   - 稀疏文件：实占显著小于逻辑大小——本口径的全部收益所在；
//   - 普通文件：实占可读，且不小于逻辑大小（块对齐只会更大）；
//   - 全洞文件：实占**判为未知**而非 0（刻意取舍，理由见
//     TestWalkAllHoleFileNeverReportsZeroActual 的注释）。
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

// probeActual 走被测链路（realbytes）读一个夹具的实占。
func probeActual(t *testing.T, path string) (actual uint64, known bool) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Skipf("探针 stat 失败，跳过：%v", err)
	}
	return realbytes.Of(path, uint64(st.Size()), st)
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

// TestWalkAllHoleFileNeverReportsZeroActual 全洞文件（不写任何数据）的处置。
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
// 想同时拿到收益与正确性，需要按卷判 st_blocks 是否可信（statfs 的 f_type +
// 每卷一次探测），已登记为开放项，不在本轮范围。
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
		t.Log("本卷对全洞报 0 blocks：按『未统计』回退逻辑口径（刻意放弃这一类收益，见函数注释）")
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
