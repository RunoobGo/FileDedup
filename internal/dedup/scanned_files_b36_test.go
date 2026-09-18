package dedup

// B3-6 回归：报告口径的「本轮扫描文件总数」必须走 Pipeline.ScannedFiles()，
// 不能从进度事件反推——进度事件的 FilesTotal 按设计是阶段口径（R2），
// 缓存命中复扫时只剩候选量，用它做语料数会虚低（fdd-cli 双跑比对曾因此误报）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/model"
)

// corpusDataset：1 个大文件重复组 + 若干唯一尺寸文件。
// 返回语料总数（含唯一文件，它们在阶段 1 就被淘汰，不进入任何后续阶段）。
func corpusDataset(t *testing.T, root string) int {
	t.Helper()
	dup := lgPayload(31, 300*1024) // > SmallFileMax：冷扫走两阶段，复扫全命中缓存
	for _, name := range []string{"D1.bin", "D2.bin"} {
		if err := os.WriteFile(filepath.Join(root, name), dup, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		name := string(rune('U'+i)) + ".bin"
		if err := os.WriteFile(filepath.Join(root, name), lgPayload(byte(101+i), 1000+i*7), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return 8
}

func TestScannedFilesReportsCorpusCount(t *testing.T) {
	root := t.TempDir()
	want := corpusDataset(t, root)
	cch, err := cache.Open(filepath.Join(t.TempDir(), "b36.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	cfg := model.ScanConfig{Roots: []string{root}, UseCache: true, Threads: 2}

	runs := []struct {
		name string
	}{
		{"冷扫（全未命中）"},
		{"复扫（全命中缓存）"},
	}
	for i, rn := range runs {
		p := New().WithCache(cch)
		var last model.ProgressEvent
		seen := 0
		p.OnProgress = func(ev model.ProgressEvent) { last = ev; seen++ }
		groups, _, err := p.Run(context.Background(), cfg)
		if err != nil {
			t.Fatalf("%s: %v", rn.name, err)
		}
		if len(groups) != 1 {
			t.Fatalf("%s: 期望 1 组, got %d", rn.name, len(groups))
		}
		if got := p.ScannedFiles(); got != uint64(want) {
			t.Fatalf("%s: ScannedFiles()=%d want %d（语料总数，第 %d 轮）", rn.name, got, want, i+1)
		}
		// 记录而非断言：进度口径随阶段收缩正是本项修复的动因。
		// 若将来进度改为累计口径，这里只会少一条说明，不会误伤测试。
		t.Logf("%s: 终值进度事件 FilesDone=%d FilesTotal=%d（事件 %d 次），语料=%d",
			rn.name, last.FilesDone, last.FilesTotal, seen, want)
		if seen == 0 {
			t.Fatalf("%s: 未收到任何进度事件", rn.name)
		}
	}
}

// Run 入口必须清零：上一轮的语料数不得泄漏到失败/取消的本轮。
func TestScannedFilesResetsPerRun(t *testing.T) {
	root := t.TempDir()
	want := corpusDataset(t, root)
	p := New()
	if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if got := p.ScannedFiles(); got != uint64(want) {
		t.Fatalf("首跑: got %d want %d", got, want)
	}
	// 第二轮在阶段 0 就被取消 → 计数不得残留首轮的读数
	// （不存在的根目录只产生失败清单、不报错，不适合做这条断言）
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.Run(cctx, model.ScanConfig{Roots: []string{root}, Threads: 2}); err == nil {
		t.Fatal("期望取消的扫描返回错误")
	}
	if got := p.ScannedFiles(); got != 0 {
		t.Fatalf("失败轮残留上轮读数: got %d want 0", got)
	}
}
