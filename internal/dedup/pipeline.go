// Package dedup 多阶段过滤流水线编排 + 任务状态机（04 M1-T09，01 §4）。
package dedup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"filededup/internal/cache"
	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/media"
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

// Pause 暂停（仅运行态生效）。
// P3：与 Resume/Cancel 一致返回 error——此前空转调用静默成功，前端按
// "调用成功" 把按钮切成"已暂停"，用户以为生效实际没暂停，只能事后重读状态。
func (p *Pipeline) Pause() error {
	p.mu.Lock()
	switch p.status {
	case model.StatusIdle, model.StatusDone, model.StatusCancelled, model.StatusFailed:
		from := p.status
		p.mu.Unlock()
		return fmt.Errorf("当前不在扫描中（%s），无法暂停", from)
	case model.StatusPaused:
		p.mu.Unlock()
		return fmt.Errorf("任务已处于暂停状态")
	}
	// 记住被打断的阶段：若阶段内后续不再调用 setStatus，Resume 否则会显示成 Scanning
	p.afterResume = p.status
	p.status = model.StatusPaused
	p.mu.Unlock()
	p.gate.Pause()
	return nil
}

// Resume 恢复：回到暂停期间到达的阶段。
func (p *Pipeline) Resume() error {
	p.mu.Lock()
	if p.status != model.StatusPaused {
		from := p.status
		p.mu.Unlock()
		return fmt.Errorf("当前未暂停（%s），无需恢复", from)
	}
	if p.afterResume != "" {
		p.status = p.afterResume
		p.afterResume = ""
	} else {
		p.status = model.StatusScanning
	}
	p.mu.Unlock()
	p.gate.Resume()
	return nil
}

// Cancel 取消：全部 worker 退出，部分结果丢弃。
// Y1：p.cancel 由 Run 持锁写入，此处必须同样持锁读取，避免数据竞争。
func (p *Pipeline) Cancel() error {
	p.mu.Lock()
	cancel := p.cancel
	running := !p.isTerminalLocked() && p.status != model.StatusIdle
	p.mu.Unlock()
	if !running || cancel == nil {
		return fmt.Errorf("当前没有进行中的扫描任务")
	}
	cancel()
	p.gate.Resume() // 唤醒暂停中的 worker 使其感知取消
	return nil
}

// Abort 强制把非终态置为 Failed（仅 panic 兜底路径使用）。
//
// Run 中途 panic 时，其内部 setStatus 分支不会执行，状态会永久停在运行态；
// 而 StartScan 只接受 Idle/终态 → 用户此后无法再扫描，只能重启应用。
// 因此绑定层 recover 之后必须显式收敛状态机，让"再次扫描"重新可用。
func (p *Pipeline) Abort() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isTerminalLocked() || p.status == model.StatusIdle {
		return
	}
	p.status = model.StatusFailed
	p.afterResume = ""
}

// isTerminalLocked 是否处于终态（调用方须持 p.mu）。P0-1：终态可被下一次 Run 复位。
func (p *Pipeline) isTerminalLocked() bool {
	switch p.status {
	case model.StatusDone, model.StatusCancelled, model.StatusFailed:
		return true
	}
	return false
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
// 命名返回值：defer 中的缓存写回（含 Cancelled 路径，01 §5.5）需修改返回值。
func (p *Pipeline) Run(parent context.Context, cfg model.ScanConfig) (groups []*model.DuplicateGroup, failed []model.FailedItem, err error) {
	ctx, cancel := context.WithCancel(parent)
	p.mu.Lock()
	// P0-1：终态（Done/Cancelled/Failed）自动经 Idle 复位，使「再次扫描」成为
	// 受支持的路径。状态机表本身不动（仍只允许 Idle→Scanning 进入运行态），
	// 因此 Done→Scanning 依旧非法——复位是一次显式的 Done→Idle→Scanning。
	// 不这样做的话：Run 结束只会置终态、无人置 Idle，第二次 Run 永久被拒，
	// 而 app 层已清空旧结果集 → 界面永久卡在「扫描中」（增量缓存特性完全不可达）。
	if p.isTerminalLocked() {
		p.status = model.StatusIdle
	}
	if !model.ValidateTransition(p.status, model.StatusScanning) {
		from := p.status
		p.mu.Unlock()
		cancel()
		return nil, nil, fmt.Errorf("非法状态转换: %s → Scanning", from)
	}
	p.cancel = cancel
	p.afterResume = ""
	p.mu.Unlock()
	// 闸门复位：上一轮若在 Paused 下被取消（父 ctx 直接取消、未走 CancelScan），
	// gate 仍处于关闭态；不复位则本轮 worker 的 gate.Wait 会永久阻塞。
	p.gate.Resume()
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
		threads = autoThreads(cfg.Roots)
	} else if threads > MaxThreads {
		// P2：并发度必须有上限。修正前只兜 <1，threads=100000 会真的开 10 万
		// worker，每个大文件 worker 另占 (depth+1)×16MiB 环形段缓冲 → 直接 OOM。
		// 按「后端是最后防线」的原则，钳制而非报错：设置界面不该能拖垮进程。
		threads = MaxThreads
	}
	workers := threads
	pool := hasher.NewPool()

	// ---------- 阶段 0：元数据扫描 ----------
	stage("scan", "扫描目录")
	scan := scanner.WalkWithGate(ctx, cfg.Roots, &cfg.Filters, workers, p.gate)
	if ctx.Err() != nil {
		p.setStatus(model.StatusCancelled)
		return nil, scan.Failed, ctx.Err()
	}
	failed = scan.Failed
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
					// C3：仅替换路径派生字段（Path/Ext）。指针原地替换保证引用
					// 稳定（无悬垂），但 ID/Key 不得被丢弃项覆盖——下游按 ID
					// 关联选中态/保留决策/预览，覆盖会让 ID 与结果集错位；
					// Key 同 inode 必然相同，Size/Mtime 同 inode 必然一致，均无需复制。
					prev.Path = e.Path
					prev.Ext = e.Ext
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
	// R2：本阶段只处理候选文件、且每文件最多读 SmallFileMax 字节，
	// 总量口径必须与 AddFile/AddBytes 一致，否则进度会虚满、ETA 提前归零
	tracker.SetTotal(uint64(len(candidates)), prefilterBytes(candidates))
	// sampleKey 预筛分桶键；preEntry 记录该文件的采样与（若有）全量哈希。
	// P0-2：分桶键只能由"采样"构成。此前缓存命中带全量的文件被绕过预筛分组
	// 直接送进最终分组，使它与本轮实算的同内容文件不在同一桶内，导致静默漏报。
	type sampleKey struct {
		size uint64
		head uint64
		tail uint64
		mid1 uint64
		mid2 uint64
	}
	type preEntry struct {
		sample    sampleKey
		full      [32]byte
		fullValid bool // 小文件一趟完成，或缓存带全量
		skip      bool // 预筛失败：已入失败清单，不参与任何分组（防零哈希假组）
	}
	pre := make([]preEntry, len(candidates))
	// H1：打开文件句柄后立刻 fstat，记录物理身份 (dev, ino, ctime)。
	// 它是哈希所依据的那份内容的凭证，用于缓存命中判定与阶段 3 写回。
	ids := make([]fsid.ID, len(candidates))
	// 阶段 3 全量哈希产物，与 candidates 下标平行（独立槽位，避免与采样字段互相污染）
	fulls := make([][32]byte, len(candidates))
	fullsValid := make([]bool, len(candidates))
	pendIdx := make([]int, len(candidates)) // candidates → pending 下标（-1 = 未入队）
	for i := range pendIdx {
		pendIdx[i] = -1
	}
	var preMu sync.Mutex      // failed/pending 共享写锁
	var pending []cache.Entry // M4：任务结束批量写回
	idx := atomic.Int64{}
	cacheOn := p.cch != nil && cfg.UseCache
	var hitPaths []string // R1：缓存命中路径，任务结束批量续期 last_hit（LRU 语义）
	// ②-P：worker panic 收口——记失败清单 + cancel + 标记，Run 在取消检查点
	// 据标记区分「内部故障 Failed」与「用户 Cancelled」（两种路径前端语义不同）。
	var workerPanic atomic.Pointer[string]
	stagePanicHandler := func(stage string) func(string) {
		return func(msg string) {
			m := stage + ": " + msg
			workerPanic.CompareAndSwap(nil, &m)
			preMu.Lock()
			failed = append(failed, model.FailedItem{Stage: "worker", Err: m})
			preMu.Unlock()
			cancel()
		}
	}
	// M4：任务结束单事务批量写回 + 命中续期（含 Cancelled：已算哈希不浪费，01 §5.5）
	defer func() {
		if !cacheOn {
			return
		}
		if len(pending) > 0 {
			if e := p.cch.Store(pending); e != nil {
				// 缓存失败不影响结果正确性：计入失败清单提示
				failed = append(failed, model.FailedItem{Stage: "cache", Err: "缓存写回失败: " + e.Error()})
			}
		}
		if len(hitPaths) > 0 {
			if e := p.cch.Touch(hitPaths); e != nil {
				failed = append(failed, model.FailedItem{Stage: "cache", Err: "缓存命中续期失败: " + e.Error()})
			}
		}
	}()
	runWorkers(ctx, workers, stagePanicHandler("prefilter"), func() error {
		localHits := make([]string, 0, 64) // R1：worker 本地累积，退出时合并（免锁热路径）
		defer func() {
			if len(localHits) == 0 {
				return
			}
			preMu.Lock()
			hitPaths = append(hitPaths, localHits...)
			preMu.Unlock()
		}()
		for {
			i := int(idx.Add(1) - 1)
			if i >= len(candidates) {
				return nil
			}
			if err := p.gate.Wait(ctx); err != nil {
				return err
			}
			e := candidates[i]
			// 计算实际文件的抽样哈希（head/tail）。即便命中缓存也要重算：size/mtime
			// 可信，但内容可能因 cp -p/COW/FAT 2s 粒度/原地改写保 mtime 等而变更且
			// mtime 不变（C1）。命中时以实际抽样与缓存抽样比对，一致才信任缓存 full，
			// 否则内容已变、full 须在阶段 3 重算，避免把"内容已变但缓存哈希仍是旧内容"
			// 的文件误判进重复组（进而误删）。
			f, err := os.Open(e.Path)
			if err != nil {
				preMu.Lock()
				failed = append(failed, model.FailedItem{Path: e.Path, Stage: "prefilter", Err: err.Error()})
				preMu.Unlock()
				pre[i].skip = true
				continue
			}
			if st, serr := f.Stat(); serr == nil {
				ids[i] = fsid.FromFileInfo(st)
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
			if cacheOn {
				if ent, hit, fullValid := p.cch.Lookup(e.Path, e.Size, e.ModTime, ids[i]); hit {
					localHits = append(localHits, e.Path)
					var full [32]byte
					fullValidNow := false
					// 四点采样全一致 → 内容极可能未变，信任缓存 full；否则内容已变，full 须重算。
					if r.Partial.Head == ent.Head && r.Partial.Tail == ent.Tail &&
						r.Partial.Mid1 == ent.Mid1 && r.Partial.Mid2 == ent.Mid2 {
						if fullValid {
							copy(full[:], ent.Full)
						}
						fullValidNow = fullValid
					}
					pre[i] = preEntry{
						sample: sampleKey{
							size: e.Size, head: r.Partial.Head, tail: r.Partial.Tail,
							mid1: r.Partial.Mid1, mid2: r.Partial.Mid2,
						},
						full:      full,
						fullValid: fullValidNow,
					}
					tracker.AddFile()
					// R2：命中视作该文件的预筛工作量已完成，字节数同样计入，
					// 否则命中越多进度越滞后（总量口径已按预筛读量设定）
					tracker.AddBytes(minU64(e.Size, hasher.PrefilterMax))
					continue
				}
			}
			// 未命中：用实际抽样分桶，full 视小文件与否（小文件一趟即得全量）
			var ent cache.Entry
			ent = cache.Entry{Path: e.Path, Size: e.Size, MtimeNs: e.ModTime,
				Head: r.Partial.Head, Tail: r.Partial.Tail,
				Mid1: r.Partial.Mid1, Mid2: r.Partial.Mid2,
				Dev: ids[i].Dev, Ino: ids[i].Ino, CtimeNs: ids[i].CtimeNs}
			if r.Small { // 小文件一趟双哈希：full 一并缓存
				ent.Full = r.Full[:]
			}
			preMu.Lock()
			pendIdx[i] = len(pending) // P0-2：阶段 3 补算后原位更新，不再追加第二条
			pending = append(pending, ent)
			preMu.Unlock()
			pre[i] = preEntry{
				sample: sampleKey{
					size: e.Size, head: r.Partial.Head, tail: r.Partial.Tail,
					mid1: r.Partial.Mid1, mid2: r.Partial.Mid2,
				},
				full:      r.Full,
				fullValid: r.Small, // 小文件一趟即得全量
			}
			tracker.AddFile()
			tracker.AddBytes(minU64(e.Size, hasher.PrefilterMax))
		}
	})
	if ctx.Err() != nil {
		if pm := workerPanic.Load(); pm != nil { // ②-P：内部故障按 Failed 收口
			p.setStatus(model.StatusFailed)
			return nil, failed, fmt.Errorf("扫描内部故障（%s）", *pm)
		}
		p.setStatus(model.StatusCancelled)
		return nil, failed, ctx.Err()
	}

	// ---------- 分桶：一律按 (size, head, tail) 采样聚合 ----------
	// P0-2：修正前带全量哈希的缓存命中文件会绕过采样分桶、直接进最终分组，
	// 于是同内容的新文件只能在"本轮实算采样的文件"里找同伴，永远碰不到
	// 已缓存的那份 → 自己孤身一桶 → 进不了阶段 3 → 被静默丢弃（漏报）。
	// 实测：三个内容相同的大文件，二次扫描只报出 2 个。
	// 现在命中缓存仅免除阶段 3 的重算，不再改变分组资格。
	type finalKey struct {
		size uint64
		full [32]byte
	}
	finalGroups := make(map[finalKey][]*model.FileEntry)
	sampleGroups := make(map[sampleKey][]int) // 采样 → candidates 下标
	for i := range candidates {
		if pre[i].skip {
			continue // 预筛失败：已入失败清单，不得进入分组（防零值键假组）
		}
		k := pre[i].sample
		sampleGroups[k] = append(sampleGroups[k], i)
	}

	// ---------- 阶段 3：大文件全量 BLAKE3（仅补算缺失者）----------
	p.setStatus(model.StatusHashing)
	stage("hash", "全量哈希")
	var hashTargets []*model.FileEntry
	var hashSlot []int // 与 hashTargets 平行：candidates 下标
	for _, idxs := range sampleGroups {
		if len(idxs) < 2 {
			continue // 采样唯一 → 无同伴，不可能成组
		}
		for _, ci := range idxs {
			if pre[ci].fullValid {
				continue // 已有全量哈希（缓存命中或小文件一趟），不重算
			}
			hashTargets = append(hashTargets, candidates[ci])
			hashSlot = append(hashSlot, ci)
		}
	}
	idx.Store(0)
	// R2：切换总量口径——已完成量 + 本阶段待哈希量。
	// 修正前 Phase2 的预筛读量与 Phase3 的全量读量重复累加，
	// BytesDone 可超 BytesTotal，导致进度虚满、ETA 提前归零。
	filesDone, bytesDone := tracker.Done()
	tracker.SetTotal(filesDone+uint64(len(hashTargets)), bytesDone+sumSize(hashTargets))
	runWorkers(ctx, workers, stagePanicHandler("hash"), func() error {
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
			var full [32]byte
			if e.Size > 512<<20 {
				// 大文件：分段预取流水线。段缓冲由环形池自管理（Y2），
				// 无需借 pooled 读缓冲——旧实现借出却未使用，纯属浪费
				full, err = hasher.HashFullSegmented(f, int64(e.Size), hasher.LargeSeg, 2, nil)
			} else {
				buf := pool.GetStreamBuf()
				full, err = hasher.HashFull(f, int64(e.Size), buf)
				pool.PutStreamBuf(buf)
			}
			f.Close()
			if err != nil {
				preMu.Lock()
				failed = append(failed, model.FailedItem{Path: e.Path, Stage: "hash", Err: err.Error()})
				preMu.Unlock()
				hashTargets[i] = nil
				continue
			}
			ci := hashSlot[i]
			// 全量哈希只写进独立的 fulls 槽位：pre[ci].sample 不得改动，
			// 它仍是本阶段分桶与写回缓存的依据。
			preMu.Lock()
			fulls[ci] = full
			fullsValid[ci] = true
			// P0-2：原位补上 full，保留阶段 2 已写入的采样。
			// 修正前这里 append 一条不含 Head/Tail 的新行，UPSERT 整行覆盖
			// 把缓存里的 partial 清零 → 该文件后续扫描再也无法与新文件同桶。
			if pi := pendIdx[ci]; pi >= 0 && pi < len(pending) {
				cp := make([]byte, len(full))
				copy(cp, full[:])
				pending[pi].Full = cp
			} else {
				cp := make([]byte, len(full))
				copy(cp, full[:])
				pending = append(pending, cache.Entry{
					Path: e.Path, Size: e.Size, MtimeNs: e.ModTime,
					Head: pre[ci].sample.head, Tail: pre[ci].sample.tail,
					Mid1: pre[ci].sample.mid1, Mid2: pre[ci].sample.mid2,
					Dev: ids[ci].Dev, Ino: ids[ci].Ino, CtimeNs: ids[ci].CtimeNs,
					Full: cp,
				})
			}
			preMu.Unlock()
			tracker.AddFile()
			tracker.AddBytes(e.Size)
		}
	})
	if ctx.Err() != nil {
		if pm := workerPanic.Load(); pm != nil { // ②-P：内部故障按 Failed 收口
			p.setStatus(model.StatusFailed)
			return nil, failed, fmt.Errorf("扫描内部故障（%s）", *pm)
		}
		p.setStatus(model.StatusCancelled)
		return nil, failed, ctx.Err()
	}

	// ---------- 最终分组：按全量哈希聚合 ----------
	// fulls[] 是阶段 3 的产物，与 candidates 下标平行；pre[ci].full 覆盖缓存/小文件来源。
	for _, idxs := range sampleGroups {
		if len(idxs) < 2 {
			continue
		}
		for _, i := range idxs {
			full := pre[i].full
			if !pre[i].fullValid {
				if !fullsValid[i] {
					continue // 全量哈希失败：已入失败清单，剔除分组（防零哈希假组）
				}
				full = fulls[i]
			}
			k := finalKey{size: candidates[i].Size, full: full}
			finalGroups[k] = append(finalGroups[k], candidates[i])
		}
	}
	// 采样唯一（桶内仅 1 个）的文件不可能与任何文件重复，无需进入最终分组。

	// ---------- 阶段 4（可选）：paranoid 逐字节确认 ----------
	// G1：比对缓冲在整趟 paranoid 中只分配一对，组间与文件间复用。
	// 原实现每次比对新分配 2×256KiB（组内文件数-1 次），
	// 万级组场景下成为纯 GC 负担。
	var ver *verifier
	if cfg.Paranoid {
		stage("verify", "逐字节确认")
		ver = newVerifier()
	}

	// ---------- 输出 ----------
	id := uint64(0)
	for k, g := range finalGroups {
		if len(g) < 2 {
			continue
		}
		sortEntries(g)
		if cfg.Paranoid {
			// ②-B：paranoid 是纯 I/O 长阶段——组间过 gate（暂停即时停步），
			// 取消经 ctx 在组内逐文件/逐块感知（见 group/equal）。
			if err := p.gate.Wait(ctx); err != nil {
				p.setStatus(model.StatusCancelled)
				return nil, failed, ctx.Err()
			}
			g, dropped := ver.group(ctx, g, failed)
			if err := ctx.Err(); err != nil {
				p.setStatus(model.StatusCancelled)
				return nil, failed, err
			}
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
	// M4：写回与命中续期统一在 defer 中执行（含 Cancelled 路径）
	p.setStatus(model.StatusDone)
	return groups, failed, nil
}

// verifyBufSize 逐字节比对的工作缓冲大小（G1）。
const verifyBufSize = 256 << 10

// verifier paranoid 逐字节确认器：持有一对可复用的读缓冲。
// G1：原实现把缓冲分配放在 streamEqual 内部，每次比对 2×256KiB；
// 改为整趟扫描只分配一对，组间/文件间复用，分配次数由
// O(Σ 组内文件数) 降为 O(1)。
type verifier struct {
	buf1, buf2 []byte
}

func newVerifier() *verifier {
	return &verifier{
		buf1: make([]byte, verifyBufSize),
		buf2: make([]byte, verifyBufSize),
	}
}

// group paranoid 模式：组内所有文件与代表文件逐字节流式比对；
// 不一致者移出组并计入失败清单。②-B：ctx 取消时立即停步返回，
// 未处理的文件不记失败（取消是用户意图，不是文件问题）。
func (v *verifier) group(ctx context.Context, g []*model.FileEntry, failed []model.FailedItem) ([]*model.FileEntry, []model.FailedItem) {
	rep, err := os.Open(g[0].Path)
	if err != nil {
		failed = append(failed, model.FailedItem{Path: g[0].Path, Stage: "verify", Err: err.Error()})
		return nil, failed
	}
	defer rep.Close()
	kept := []*model.FileEntry{g[0]}
	for _, e := range g[1:] {
		if ctx.Err() != nil {
			return kept, failed
		}
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
		same, err := v.equal(ctx, rep, cur, int64(g[0].Size))
		cur.Close()
		if ctx.Err() != nil {
			return kept, failed // 比对中途取消：err 是 ctx 错误，不记文件失败
		}
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

// equal 复用 v 的缓冲逐字节比对两个等长流（顺序读，非并发安全）。
// ②-B：每块（256KiB）之间检查取消——单对超大文件也不能拖住取消路径。
func (v *verifier) equal(ctx context.Context, a, b *os.File, size int64) (bool, error) {
	buf1, buf2 := v.buf1, v.buf2
	remain := size
	for remain > 0 {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		n := int64(len(buf1))
		if remain < n {
			n = remain
		}
		if _, err := io.ReadFull(a, buf1[:n]); err != nil {
			return false, err
		}
		if _, err := io.ReadFull(b, buf2[:n]); err != nil {
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

// MaxThreads 显式并发度上限（P2）：固定值而非 NumCPU 的倍数，
// 使同一配置在不同机器上的资源占用可预期。上限内已足够打满常规 SSD。
// 导出以便设置界面（app.go）用同一口径钳制，避免"界面能存下引擎不认的值"。
const MaxThreads = 64

func defaultThreads() int {
	n := runtime.NumCPU()
	if n > 1 {
		return n - 1 // 默认留 1 核（01 §5.1）
	}
	return 1
}

// autoThreads 自动并发度（G6）：在「核数-1」基础上按扫描根所在存储介质调整。
// 机械盘并发随机读会因寻道抖动、网络卷会因往返排队而互相拖累，
// 这两类介质下调到 media 包给出的低并发档；其余维持核数-1。
//
// 安全性：探测只降级不升级，且探测失败/平台不支持时返回 Unknown
// （= 维持核数-1），因此本函数最坏情况等价于改造前行为。
// 探测结果按挂载点缓存，首次约 90ms（且根卷走免子进程快路径）。
func autoThreads(roots []string) int {
	base := defaultThreads()
	return media.AutoWorkers(media.RootsClass(roots, media.Probe), base)
}

func sumSize(files []*model.FileEntry) uint64 {
	var s uint64
	for _, f := range files {
		s += f.Size
	}
	return s
}

// prefilterBytes 预筛阶段理论读量（R2 口径）：每候选文件最多 PrefilterMax 字节。
func prefilterBytes(files []*model.FileEntry) uint64 {
	var s uint64
	for _, f := range files {
		s += minU64(f.Size, hasher.PrefilterMax)
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
//
// ②-P：worker  panic 不得带走整个进程（现象曾是"点扫描后软件消失"）。
// 每个 worker 挂 recover：堆栈写 stderr 留痕，一行摘要经 onPanic 上报——
// 调用方的 onPanic 负责记入失败清单并 cancel ctx，兄弟 worker 经
// gate.Wait/循环头感知取消后尽快退出，Run 据 panic 标记以 Failed 收口。
func runWorkers(ctx context.Context, n int, onPanic func(string), fn func() error) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "[panic] dedup worker 已恢复: %v\n%s\n", r, debug.Stack())
					if onPanic != nil {
						onPanic(fmt.Sprintf("worker panic（已拦截）: %v", r))
					}
				}
			}()
			_ = fn() // 错误经由 ctx/failed 通道表达
		}()
	}
	wg.Wait()
}
