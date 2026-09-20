package main

// PreviewProcessPolicy 与执行器就地清理的数据竞争回归（2026-09-20 ocr 审查）。
//
// 修正前：锁内只快照 a.groups（[]*DuplicateGroup 指针浅拷贝）就解锁，
// ApplyProcessPolicy 在**无锁**状态下遍历共享的 g.Files；执行器收尾清理
// 持锁就地改写同一批结构（`g.Files = files` 且 `g.Files[:0]` 复用底层数组，
// app.go ExecuteOperation 结果集清理段）。指针浅拷贝挡不住元素级写读竞争。
//
// 该函数不受 opsRunning 互斥（注释明说"预览是只读的，不该被清理挡住"），
// 所以与执行并发是**设计内**场景——竞争必须消除，而不是靠约定规避。

import (
	"strings"
	"sync"
	"testing"
)

func TestPreviewProcessPolicyConcurrentWithCleanupNoRace(t *testing.T) {
	a, _, _, insideDir, _ := procFixture(t)

	var ids []uint64
	a.mu.Lock()
	for _, g := range a.groups {
		for _, f := range g.Files {
			ids = append(ids, f.ID)
		}
	}
	a.mu.Unlock()
	if len(ids) < 3 {
		t.Fatalf("夹具至少应有 3 个候选文件，实际 %d", len(ids))
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 模拟执行器收尾清理：持锁就地压缩 g.Files（与 ExecuteOperation 同款写法）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			a.mu.Lock()
			for _, g := range a.groups {
				if len(g.Files) < 2 {
					continue
				}
				files := g.Files[:0]
				for _, f := range g.Files {
					if !strings.HasSuffix(f.Path, "a.bin") {
						files = append(files, f)
					}
				}
				g.Files = files
			}
			a.mu.Unlock()
		}
	}()

	for i := 0; i < 300; i++ {
		if _, err := a.PreviewProcessPolicy([]string{insideDir}, ids); err != nil {
			t.Fatalf("PreviewProcessPolicy 出错: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}
