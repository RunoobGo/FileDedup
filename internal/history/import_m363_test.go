package history

// 第八轮审查批 2（拟 M363）：并发导入必须"连点两次不翻倍"。
//
// 「连点两次不翻倍」这条在册承诺今天靠的是 ImportFrom 自己读一遍本地自然键集合
// （localScanKeys / localOpKeys）再逐条判重，而这两步与后面的写事务**不在同一临界区**
// 里（internal/history 非测试代码 16 处 s.mu.Lock()，import.go 一处都没有）。
// ⇒ 两个并发 ImportFrom 同一份影像各自都读到"本地没有"，双双插到底。
// ★ 本机只有 darwin：这一格是概率性的（红不红取决于调度），复现次数与格数如实记进 §6.67。

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// P-21：N 轮并发双导入，任何一轮行数翻倍即红。
func TestM363ConcurrentImportsDoNotDoubleRows(t *testing.T) {
	const trials = 24
	const scansPerImage = 60 // 影像做大一点：读集合与写事务之间那段窗口才有可乘之机

	dir := t.TempDir()
	src := newStoreAt(t, dir, "src.db")
	for i := 0; i < scansPerImage; i++ {
		seedScan(t, src, fmt.Sprintf("/root/%03d", i), 2)
	}
	// ★ 期望值取**影像自己的行数**而不是种下去的次数：本仓扫描历史有"保留最新 20 条"
	// 的收尾裁剪，seed 60 条之后影像里只剩 20 条。首版在这里写死 60，红在夹具前提上
	// （40 = 20 条翻倍），判据一格没碰到——同一处读数改成对账才看得见。
	wantScans := countRows(t, src, "scan_history")
	if wantScans < 10 {
		t.Fatalf("夹具前提走样：影像里只有 %d 条扫描，翻倍与不翻倍分不开", wantScans)
	}
	image := exportImage(t, src, "image.db")

	var firstBad [2]string
	var bad atomic.Int32
	for trial := 0; trial < trials && bad.Load() == 0; trial++ {
		local := newStoreAt(t, dir, fmt.Sprintf("local-%02d.db", trial))
		var wg sync.WaitGroup
		errs := make([]error, 2) // 每个 goroutine 只写自己那一格（M92 的教训）
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, errs[i] = local.ImportFrom(image)
			}(i)
		}
		close(start)
		wg.Wait()

		scans := countRows(t, local, "scan_history")
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("第 %d 轮导入本身失败了：A=%v B=%v", trial, errs[0], errs[1])
		}
		if scans != wantScans {
			bad.Add(1)
			extra := ""
			if scans == 2*wantScans {
				extra = "（正好翻倍：两路各自判成「本地没有」）"
			}
			firstBad[0] = fmt.Sprintf("第 %d 轮", trial)
			firstBad[1] = fmt.Sprintf("scan_history=%d，影像只有 %d 条%s", scans, wantScans, extra)
		}
		if err := local.Close(); err != nil {
			t.Fatalf("第 %d 轮关句柄失败（M335：TempDir 会被残留句柄顶掉）：%v", trial, err)
		}
	}
	if bad.Load() > 0 {
		t.Errorf("M363：并发导入翻倍 —— %s：%s（读集合与写事务不在同一临界区，「连点两次不翻倍」被踩穿）",
			firstBad[0], firstBad[1])
	}
}
