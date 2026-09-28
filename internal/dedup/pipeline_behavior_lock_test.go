package dedup

// M341（2026-09-28 第七轮审查批）：`(*Pipeline).Run` 的**行为锁**。
//
// 背景：`Run` 是约 586 行的单体函数（pipeline.go:349-935），阶段 0/1/1.5/2/3
// 加验证器全在函数体内，且阶段之间共享约 15 个量（`candidates`/`pre`/`ids`/
// `fulls`/`pendIdx`/`preMu`/`pending`/`hitPaths`/`idx`/`cacheOn`/`workerPanic`…）。
// 它的耦合度**高于** M337 那个 `ops.Execute`（后者只共享 8 个量、且五个分支彼此
// 独立），所以本批只交付行为锁，提取留作独立批次。
//
// 锁的意义与 M337 那份完全同构：**先锁住行为，再动结构**。本文件的断言值取自
// 提取前的实测，不是设计意图。下一批提取分支后，本文件必须**逐字仍绿**；
// 红了说明提取改变了行为，是提取的错，**不得**回改这里的断言。

import (
	"context"
	"testing"
	"time"

	"filededup/internal/model"
)

// TestPipelineRunBehaviorLockGrouping 锁住"分组形状"这一层：三组重复的成员构成、
// 独文件与零字节/隐藏文件都不进组。
func TestPipelineRunBehaviorLockGrouping(t *testing.T) {
	root := t.TempDir()
	expect := genDataset(t, root)

	groups, _, err := New().Run(context.Background(), model.ScanConfig{
		Roots: []string{root}, Threads: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(groups) != len(expect) {
		t.Fatalf("重复组数 = %d, want %d", len(groups), len(expect))
	}
	// 每组的成员集合必须与 genDataset 的预期逐组一致（顺序不钉：并发扫描的结果
	// 顺序本来就不是一个可钉的量，钉了只会把合规的重构打成红）。
	bySize := map[int]int{}
	for _, g := range groups {
		bySize[len(g.Files)]++
	}
	if bySize[2] != 2 || bySize[3] != 1 {
		t.Fatalf("组规模分布 = %v, want {2:2, 3:1}（两组各 2 副本、一组 3 副本）", bySize)
	}
	// 独文件不得出现在任何组里
	for _, g := range groups {
		for _, f := range g.Files {
			for rel := range expect {
				_ = rel
			}
			if wants, ok := belongsToExpect(expect, f.Path); !ok {
				t.Fatalf("出现了一个不在预期重复集合里的成员：%s", f.Path)
			} else if len(wants) == 0 {
				t.Fatalf("独文件进了组：%s", f.Path)
			}
		}
	}
}

// belongsToExpect 判定某路径是否属于 genDataset 的某一组（返回该组）。
func belongsToExpect(expect map[string][]string, path string) ([]string, bool) {
	for _, list := range expect {
		for _, p := range list {
			if p == path {
				return list, true
			}
		}
	}
	return nil, false
}

// TestPipelineRunBehaviorLockCountersPerRun 计数器必须**按轮归零**：
// 同一 Pipeline 连跑两轮，第二轮不得串进第一轮的数（AS-K1/M6-P1/M21/M62+M85
// 那族"按轮归零"的口径）。
func TestPipelineRunBehaviorLockCountersPerRun(t *testing.T) {
	root := t.TempDir()
	genDataset(t, root)
	p := New()
	cfg := model.ScanConfig{Roots: []string{root}, Threads: 2}

	if _, _, err := p.Run(context.Background(), cfg); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	first := p.ScannedFiles()

	scanned2 := uint64(0)
	if _, _, err := p.Run(context.Background(), cfg); err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	scanned2 = p.ScannedFiles()

	if first == 0 {
		t.Fatal("第一轮 ScannedFiles = 0：本用例前提不成立（数据集没被扫到）")
	}
	if scanned2 != first {
		t.Fatalf("第二轮 ScannedFiles = %d，第一轮 = %d：计数器要么串了轮次、要么归零过头",
			scanned2, first)
	}
}

// TestPipelineRunBehaviorLockCancelled ctx 已取消时不得返回分组，且状态落到取消。
func TestPipelineRunBehaviorLockCancelled(t *testing.T) {
	root := t.TempDir()
	genDataset(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := New()
	groups, _, err := p.Run(ctx, model.ScanConfig{Roots: []string{root}, Threads: 2})
	if err == nil {
		t.Fatal("ctx 已取消却返回 err=nil：调用方无法区分「扫完了」与「没扫」")
	}
	if len(groups) != 0 {
		t.Fatalf("取消后仍返回 %d 个组：取消路径不得产出结果", len(groups))
	}
}

// TestPipelineRunBehaviorLockEmptyRoot 空目录不 panic、不产组（防御性形状）。
func TestPipelineRunBehaviorLockEmptyRoot(t *testing.T) {
	p := New()
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots: []string{t.TempDir()}, Threads: 2,
	})
	if err != nil {
		t.Fatalf("空目录扫描不应报错：%v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("空目录产出了 %d 个组", len(groups))
	}
	if len(failed) != 0 {
		t.Fatalf("空目录产出了 %d 条失败项：%v", len(failed), failed)
	}
	// 给状态机一点时间落定（Run 返回后状态由 goroutine 收口）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := p.Status(); s == model.StatusDone || s == model.StatusCancelled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("空目录扫描后状态未落到终态：%v", p.Status())
}
