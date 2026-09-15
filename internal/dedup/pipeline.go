// Package dedup 多阶段过滤流水线编排 + 任务状态机（04 M1-T09，01 §4）。
package dedup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"filededup/internal/cache"
	"filededup/internal/hasher"
	"filededup/internal/model"
	"filededup/internal/progress"
	"filededup/internal/scanner"
)

// Pipeline 一次扫描任务。
type Pipeline struct {
	mu          sync.Mutex
	status      model.TaskStatus
	afterResume model.TaskStatus // 暂停期间到达的阶段，Resume 时恢复
	gate        *Gate
	cancel      context.CancelFunc
	cch         *cache.Cache // 可选：哈希缓存（M4）

	OnProgress func(model.ProgressEvent) // 可选：进度回调
	OnStage    func(model.StageEvent)    // 可选：阶段回调
}

// WithCache 启用哈希缓存（链式）。
func (p *Pipeline) WithCache(c *cache.Cache) *Pipeline {
	if c != nil {
		p.cch = c
	}
	return p
}

// New 创建流水线（初始 Idle）。
func New() *Pipeline {
	return &Pipeline{status: model.StatusIdle, gate: NewGate()}
}

// Status 当前状态。
func (p *Pipeline) Status() model.TaskStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Pause 暂停：停止派发新哈希任务，in-flight 完成后挂起（01 §5.5）。
// 仅运行态生效（Idle/Done/Cancelled/Failed 无操作）。
func (p *Pipeline) Pause() {
	p.mu.Lock()
	switch p.status {
	case model.StatusIdle, model.StatusDone, model.StatusCancelled, model.StatusFailed:
		p.mu.Unlock()
		return
	}
	p.status = model.StatusPaused
	p.mu.Unlock()
	p.gate.Pause()
}

// Resume 恢复：回到暂停期间到达的阶段。
func (p *Pipeline) Resume() {
	p.mu.Lock()
	if p.status == model.StatusPaused {
		if p.afterResume != "" {
			p.status = p.afterResume
			p.afterResume = ""
		} else {
			p.status = model.StatusScanning
		}
	}
	p.mu.Unlock()
	p.gate.Resume()
}

// Cancel 取消：全部 worker 退出，部分结果丢弃。
func (p *Pipeline) Cancel() {
	if p.cancel != nil {
		p.cancel()
	}
	p.gate.Resume() // 唤醒暂停中的 worker 使其感知取消
}

// setStatus 运行态阶段切换；暂停中则记忆待恢复阶段（不覆盖 Paused 显示）。
func (p *Pipeline) setStatus(s model.TaskStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch s {
	case model.StatusDone, model.StatusCancelled, model.StatusFailed:
		// 终态直接设置（即使暂停中也生效：任务已结束）
		p.status = s
		p.afterResume = ""
	default:
		if p.status == model.StatusPaused {
			p.afterResume = s
		} else {
			p.status = s
		}
	}
}

// Run 执行完整流水线。返回重复组与失败清单；ctx 取消返回 context 错误。
func (p *Pipeline) Run(parent context.Context, cfg model.ScanConfig) ([]*model.DuplicateGroup, []model.FailedItem, error) {
	if !model.ValidateTransition(p.Status(), model.StatusScanning) {
		return nil, nil, fmt.Errorf("非法状态转换: %s → Scanning", p.Status())
	}
	ctx, cancel := context.WithCancel(parent)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()
	defer cancel()
	p.setStatus(model.StatusScanning)

	tracker := progress.New(500*time.Millisecond, p.OnProgress)
	tracker.Start(ctx)
	defer func() { tracker.Stop() }()

	stage := func(s, desc string) {
		tracker.SetStage(s)
		if p.OnStage != nil {
			p.OnStage(model.StageEvent{Stage: s, Desc: desc})
		}
	}
	_ = stage

	threads := cfg.Threads
	if threads < 1 {
		threads = defaultThreads()
	}
	workers := threads
	pool := hasher.NewPool()

	// ---------- 阶段 0：元数据扫描 ----------
	stage("scan", "扫描目录")
	scan := scanner.Walk(ctx, cfg.Roots, &cfg.Filters, workers)
	if ctx.Err() != nil {
		p.setStatus(model.StatusCancelled)
		return nil, scan.Failed, ctx.Err()
	}
	failed := scan.Failed
	files := scan.Files
	tracker.SetTotal(uint64(len(files)), sumSize(files))

	// ---------- 阶段 1：size 分组，淘汰独 size ----------
	bySize := make(map[uint64][]*model.FileEntry)
	for _, e := range files {
		bySize[e.Size] = append(bySize[e.Size], e)
	}
	var candidates []*model.FileEntry
	for size, group := range bySize {
		if len(group) >= 2 {
			candidates = append(candidates, group...)
		}
		_ = size
	}
	sortEntries(candidates)

	// ---------- 阶段 1.5：候选组 FileKey 去重（硬链接只计一次）----------
	seen := make(map[model.FileKey]*model.FileEntry, len(candidates))
	deduped := candidates[:0]
	for _, e := range candidates {
		k := scanner.ResolveKey(e)
		if k.Resolved {
			if prev, dup := seen[k]; dup {
				// 硬链接多路径：保留路径较短者
				if len(e.Path) < len(prev.Path) {
					*prev = *e // 原地替换（ID 保持引用稳定，避免结果集悬挂）
					continue
				}
				continue
			}
			seen[k] = e
		}
		deduped = append(deduped, e)
	}
	candidates = deduped
	// 去重后重算候选（组内可能只剩 1 个）
	candidates = refilterUnique(candidates)

	// ---------- 阶段 2：首尾预筛（小文件一趟双哈希）----------
	p.setStatus(model.StatusPrefiltering)
	stage("prefilter", "预筛哈希")
	type preKey struct {
		size      uint64
		head      uint64
		tail      uint64
		small     bool
		full      [32]byte
		fullValid bool // 缓存命中带全量 / 小文件必然
		skip      bool // 预筛失败：已入失败清单，不参与任何分组（防零哈希假组）
	}
	pre := make([]preKey, len(candidates))
	var preMu sync.Mutex      // failed/pending 共享写锁
	var pending []cache.Entry // M4：任务结束批量写回
	idx := atomic.Int64{}
	cacheOn := p.cch != nil && cfg.UseCache
	runWorkers(ctx, workers, func() error {
		for {
			i := int(idx.Add(1) - 1)
			if i >= len(candidates) {
				return nil
			}
			if err := p.gate.Wait(ctx); err != nil {
				return err
			}
			e := candidates[i]
			// M4 缓存命中：零读盘直接组装（partial 必有；full 视缓存）
			if cacheOn {
				if ent, hit, fullValid := p.cch.Lookup(e.Path, e.Size, e.ModTime); hit {
					var full [32]byte
					if fullValid {
						copy(full[:], ent.Full)
					}
					pre[i] = preKey{
						size: e.Size, head: ent.Head, tail: ent.Tail,
						// 无全量缓存的小文件不直接入最终分组（full 为零值会造出
						// 假重复组），改走 preGroups → 阶段 3 补全
						small: fullValid && e.Size <= hasher.SmallFileMax,
						full:  full, fullValid: fullValid,
					}
					tracker.AddFile()
					continue
				}
			}
			f, err := os.Open(e.Path)
			if err != nil {
				preMu.Lock()
				failed = append(failed, model.FailedItem{Path: e.Path, Stage: "prefilter", Err: err.Error()})
				preMu.Unlock()
				pre[i].skip = true
				continue
			}
			buf := pool.GetSmallBuf()
			r, err := hasher.HashHeadTail(f, int64(e.Size), buf)
			pool.PutSmallBuf(buf)
			f.Close()
			if err != nil {
				preMu.Lock()
				failed = append(failed, model.FailedItem{Path: e.Path, Stage: "prefilter", Err: err.Error()})
				preMu.Unlock()
				pre[i].skip = true
				continue
			}
			var ent cache.Entry
			if r.Small { // 小文件一趟双哈希：full 一并缓存
				ent = cache.Entry{Path: e.Path, Size: e.Size, MtimeNs: e.ModTime,
					Head: r.Partial.Head, Tail: r.Partial.Tail, Full: r.Full[:]}
			} else { // 大文件先缓存 partial（full 阶段 3 后补/UPSERT 覆盖）
				ent = cache.Entry{Path: e.Path, Size: e.Size, MtimeNs: e.ModTime,
					Head: r.Partial.Head, Tail: r.Partial.Tail}
			}
			preMu.Lock()
			pending = append(pending, ent)
			preMu.Unlock()
			pre[i] = preKey{size: e.Size, head: r.Partial.Head, tail: r.Partial.Tail,
				small: r.Small, full: r.Full, fullValid: r.Small}
			tracker.AddFile()
			tracker.AddBytes(minU64(e.Size, hasher.SmallFileMax))
		}
	})
	if ctx.Err() != nil {
		p.setStatus(model.StatusCancelled)
		return nil, failed, ctx.Err()
	}

	// 按 (size, head, tail) 分组；小文件直接进入最终分组（已有全量哈希）
	type finalKey struct {
		size uint64
		full [32]byte
	}
	finalGroups := make(map[finalKey][]*model.FileEntry)
	preGroups := make(map[preKey][]*model.FileEntry)
	for i, e := range candidates {
		if pre[i].skip {
			continue // 预筛失败：已入失败清单，不得进入分组（防零值键假组）
		}
		if pre[i].small || pre[i].fullValid {
			// 小文件（一趟完成）或缓存命中带全量：直接进入最终分组
			finalGroups[finalKey{size: e.Size, full: pre[i].full}] = append(
				finalGroups[finalKey{size: e.Size, full: pre[i].full}], e)
		} else {
			k := pre[i]
			preGroups[k] = append(preGroups[k], e)
		}
	}

	// ---------- 阶段 3：大文件全量 BLAKE3 ----------
	p.setStatus(model.StatusHashing)
	stage("hash", "全量哈希")
	var hashTargets []*model.FileEntry
	var hashPreKeys []preKey
	for k, g := range preGroups {
		if len(g) >= 2 {
			hashTargets = append(hashTargets, g...)
			for range g {
				hashPreKeys = append(hashPreKeys, k)
			}
		}
	}
	idx.Store(0)
	runWorkers(ctx, workers, func() error {
		for {
			i := int(idx.Add(1) - 1)
			if i >= len(hashTargets) {
				return nil
			}
			if err := p.gate.Wait(ctx); err != nil {
				return err
			}
			e := hashTargets[i]
			f, err := os.Open(e.Path)
			if err != nil {
				preMu.Lock()
				failed = append(failed, model.FailedItem{Path: e.Path, Stage: "hash", Err: err.Error()})
				preMu.Unlock()
				hashTargets[i] = nil // 失败剔除，不参与最终分组（防零哈希假组）
				continue
			}
			buf := pool.GetStreamBuf()
			var full [32]byte
			if e.Size > 512<<20 { // 大文件：分段预取流水线
				full, err = hasher.HashFullSegmented(f, int64(e.Size), hasher.LargeSeg, 2, buf)
			} else {
				full, err = hasher.HashFull(f, int64(e.Size), buf)
			}
			pool.PutStreamBuf(buf)
			f.Close()
			if err != nil {
				preMu.Lock()
				failed = append(failed, model.FailedItem{Path: e.Path, Stage: "hash", Err: err.Error()})
				preMu.Unlock()
				hashTargets[i] = nil
				continue
			}
			hashPreKeys[i] = preKey{size: e.Size, full: full} // 复用槽位存最终哈希
			preMu.Lock()
			pending = append(pending, cache.Entry{Path: e.Path, Size: e.Size, MtimeNs: e.ModTime, Full: full[:]})
			preMu.Unlock()
			tracker.AddFile()
			tracker.AddBytes(e.Size)
		}
	})
	if ctx.Err() != nil {
		p.setStatus(model.StatusCancelled)
		return nil, failed, ctx.Err()
	}
	for i := range hashTargets {
		if hashTargets[i] == nil {
			continue // 哈希失败条目：已入失败清单，剔除分组（防零哈希假组）
		}
		k := finalKey{size: hashTargets[i].Size, full: hashPreKeys[i].full}
		finalGroups[k] = append(finalGroups[k], hashTargets[i])
	}

	// ---------- 阶段 4（可选）：paranoid 逐字节确认 ----------
	if cfg.Paranoid {
		stage("verify", "逐字节确认")
	}

	// ---------- 输出 ----------
	var groups []*model.DuplicateGroup
	id := uint64(0)
	for k, g := range finalGroups {
		if len(g) < 2 {
			continue
		}
		sortEntries(g)
		if cfg.Paranoid {
			g, dropped := verifyGroup(g, failed)
			if len(g) < 2 {
				continue
			}
			failed = append(failed, dropped...)
		}
		id++
		groups = append(groups, &model.DuplicateGroup{
			GroupID:     id,
			Files:       g,
			Reclaimable: (uint64(len(g)) - 1) * g[0].Size,
			Hash:        k.full, // 组内容哈希（M3 操作前校验依据）
		})
	}
	// 稳定排序：可释放空间降序，其次组大小
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Reclaimable != groups[j].Reclaimable {
			return groups[i].Reclaimable > groups[j].Reclaimable
		}
		return groups[i].GroupID < groups[j].GroupID
	})
	// M4：任务结束单事务批量写回（含 Cancelled：已算哈希不浪费，01 §5.5）
	if cacheOn && len(pending) > 0 {
		if err := p.cch.Store(pending); err != nil {
			// 缓存失败不影响结果正确性：计入失败清单提示
			failed = append(failed, model.FailedItem{Stage: "cache", Err: "缓存写回失败: " + err.Error()})
		}
	}
	p.setStatus(model.StatusDone)
	return groups, failed, nil
}

// verifyGroup paranoid 模式：组内所有文件与代表文件逐字节流式比对；
// 不一致者移出组并计入失败清单。
func verifyGroup(g []*model.FileEntry, failed []model.FailedItem) ([]*model.FileEntry, []model.FailedItem) {
	rep, err := os.Open(g[0].Path)
	if err != nil {
		failed = append(failed, model.FailedItem{Path: g[0].Path, Stage: "verify", Err: err.Error()})
		return nil, failed
	}
	defer rep.Close()
	kept := []*model.FileEntry{g[0]}
	for _, e := range g[1:] {
		cur, err := os.Open(e.Path)
		if err != nil {
			failed = append(failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: err.Error()})
			continue
		}
		// 代表文件句柄偏移复位（上一次比较已读至 EOF）
		if _, err := rep.Seek(0, io.SeekStart); err != nil {
			cur.Close()
			failed = append(failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: err.Error()})
			continue
		}
		same, err := streamEqual(rep, cur, int64(g[0].Size))
		cur.Close()
		if err != nil {
			failed = append(failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: err.Error()})
			continue
		}
		if same {
			kept = append(kept, e)
		} else {
			failed = append(failed, model.FailedItem{Path: e.Path, Stage: "verify", Err: "逐字节比对不一致"})
		}
	}
	return kept, failed
}

func streamEqual(a, b *os.File, size int64) (bool, error) {
	buf1 := make([]byte, 256<<10)
	buf2 := make([]byte, 256<<10)
	remain := size
	for remain > 0 {
		n := int64(len(buf1))
		if remain < n {
			n = remain
		}
		_, err := io.ReadFull(a, buf1[:n])
		if err != nil {
			return false, err
		}
		_, err = io.ReadFull(b, buf2[:n])
		if err != nil {
			return false, err
		}
		if !bytes.Equal(buf1[:n], buf2[:n]) {
			return false, nil
		}
		remain -= n
	}
	return true, nil
}

// ---------- 工具 ----------

func defaultThreads() int {
	n := runtime.NumCPU()
	if n > 1 {
		return n - 1 // 默认留 1 核（01 §5.1）
	}
	return 1
}

func sumSize(files []*model.FileEntry) uint64 {
	var s uint64
	for _, f := range files {
		s += f.Size
	}
	return s
}

func sortEntries(files []*model.FileEntry) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

// refilterUnique FileKey 去重后重算：组内只剩 1 个的 size 淘汰。
func refilterUnique(candidates []*model.FileEntry) []*model.FileEntry {
	bySize := make(map[uint64]int)
	for _, e := range candidates {
		bySize[e.Size]++
	}
	out := candidates[:0]
	for _, e := range candidates {
		if bySize[e.Size] >= 2 {
			out = append(out, e)
		}
	}
	return out
}

func minU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// runWorkers 固定 worker 池：任一 worker 返回错误即等待全部退出（不中断他人）。
func runWorkers(ctx context.Context, n int, fn func() error) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fn() // 错误经由 ctx/failed 通道表达
		}()
	}
	wg.Wait()
}
