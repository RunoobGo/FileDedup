// Package scanner 并行目录遍历 + 平台 FileKey 抽象（04 M1-T03/T04/T05）。
package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"filededup/internal/filter"
	"filededup/internal/model"
)

// dirQueue 无界目录队列：worker 既是消费者也是生产者，
// 有界 channel 在"全部 worker 同时投递且队列满"时会死锁，无界队列从根上消除该风险。
type dirQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []string
	closed bool
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *dirQueue) push(d string) {
	q.mu.Lock()
	q.items = append(q.items, d)
	q.mu.Unlock()
	q.cond.Signal()
}

// pop 阻塞取目录；队列关闭且空时返回 false。
func (q *dirQueue) pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return "", false
	}
	d := q.items[0]
	q.items = q.items[1:]
	return d, true
}

// close 关闭队列（唤醒全部等待者）。
func (q *dirQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

// Result 扫描结果。
type Result struct {
	Files   []*model.FileEntry
	Failed  []model.FailedItem
	Visited int // 实际访问目录数（诊断用）
}

// Walk 并行遍历 roots：
//   - 跳过符号链接与 0 字节文件（内置行为）
//   - 应用过滤器
//   - 重叠根目录与子目录去重（平台大小写折叠）
//   - unix 平台 FileKey 由 lstat 顺带填充；Windows 留待 ResolveKey 按需解析
func Walk(ctx context.Context, roots []string, f *model.Filters, workers int) *Result {
	if workers < 1 {
		workers = 4
	}
	res := &Result{}
	cleaned := dedupeRoots(roots)

	var (
		mu      sync.Mutex
		nextID  atomic.Uint64
		visited = make(map[string]struct{})
		q       = newDirQueue()
		pend    sync.WaitGroup // 在途目录计数（队列中 + 处理中）
	)

	for _, r := range cleaned {
		visited[foldPath(r)] = struct{}{}
	}

	// submit 投递目录：先计数再入队（保证 closer 的 Wait 不早于 Add）。
	submit := func(dir string) {
		pend.Add(1)
		q.push(dir)
	}

	locals := make([][]*model.FileEntry, workers)
	fails := make([][]model.FailedItem, workers)
	var workerWg sync.WaitGroup

	for i := 0; i < workers; i++ {
		workerWg.Add(1)
		go func(idx int) {
			defer workerWg.Done()
			local := make([]*model.FileEntry, 0, 1024)
			for {
				dir, ok := q.pop()
				if !ok {
					break // 队列关闭且空：全部完成
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					fails[idx] = append(fails[idx], model.FailedItem{Path: dir, Stage: "scan", Err: err.Error()})
					pend.Done()
					continue
				}
				if ctx.Err() != nil {
					pend.Done()
					continue // 取消：清空剩余在途目录
				}
				for _, de := range entries {
					full := filepath.Join(dir, de.Name())
					typ := de.Type()
					if typ&fs.ModeSymlink != 0 {
						continue // 符号链接：不跟随
					}
					if typ.IsDir() {
						if !f.IncludeHidden && strings.HasPrefix(de.Name(), ".") {
							continue
						}
						key := foldPath(full)
						mu.Lock()
						_, seen := visited[key]
						if !seen {
							visited[key] = struct{}{}
						}
						mu.Unlock()
						if !seen {
							submit(full)
						}
						continue
					}
					// 普通文件（Type 未知时用 Info 兜底）
					info, err := de.Info()
					if err != nil {
						fails[idx] = append(fails[idx], model.FailedItem{Path: full, Stage: "scan", Err: err.Error()})
						continue
					}
					if !info.Mode().IsRegular() {
						continue
					}
					size := uint64(info.Size())
					if size == 0 {
						continue // 0 字节内置跳过
					}
					rel := relativeTo(cleaned, full)
					if !filter.Apply(f, de.Name(), rel, size) {
						continue
					}
					e := &model.FileEntry{
						ID:      nextID.Add(1),
						Path:    full,
						Size:    size,
						ModTime: info.ModTime().UnixNano(),
						Ext:     strings.ToLower(filepath.Ext(full)),
					}
					e.Key = keyFromInfo(full, info) // unix 填充；win 返回未解析
					local = append(local, e)
				}
				pend.Done()
			}
			locals[idx] = local
		}(i)
	}

	// 先投递根目录（完成全部 Add），再启动 closer：
	// 在途计数归零 → 关闭队列 → worker 逐个退出。
	for _, r := range cleaned {
		submit(r)
	}
	go func() {
		pend.Wait()
		q.close()
	}()

	workerWg.Wait()

	for _, l := range locals {
		res.Files = append(res.Files, l...)
	}
	for _, fl := range fails {
		res.Failed = append(res.Failed, fl...)
	}
	res.Visited = len(visited)
	return res
}

// dedupeRoots 规范化并剔除被其他根包含的子根（a 与 a/b 同扫时丢弃 a/b）。
func dedupeRoots(roots []string) []string {
	var out []string
	for _, r := range roots {
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = filepath.Clean(r)
		}
		out = append(out, filepath.Clean(abs))
	}
	sort.Strings(out)
	var kept []string
	for _, r := range out {
		dup := false
		for _, k := range kept {
			// k 是 r 的前缀目录（折叠大小写比较）
			fk, fr := foldPath(k), foldPath(r)
			if fr == fk || strings.HasPrefix(fr, fk+string(filepath.Separator)) {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, r)
		}
	}
	return kept
}

// relativeTo 计算 full 相对最近扫描根的路径（过滤 glob 用）。
func relativeTo(roots []string, full string) string {
	for _, r := range roots {
		if strings.HasPrefix(full, r+string(filepath.Separator)) {
			return full[len(r)+1:]
		}
	}
	return filepath.Base(full)
}
