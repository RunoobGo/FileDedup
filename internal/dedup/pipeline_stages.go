package dedup

// M341（2026-09-28 第七轮审查批）：`(*Pipeline).Run` 的各阶段体。
//
// 背景：`Run` 曾是约 586 行的单体函数（阶段 0/1/1.5/2/3/4 + 输出全在函数体内），
// 且阶段之间共享约 15 个量。本文件把它们搬成 `scanRun` 的方法，`Run` 只留
// 「认领 → 复位 → 构造上下文 → 依次调用 → 收口」。
//
// ★ **纯结构改动**：每个阶段体内的语句与注释**逐字未动**，只做了两件机械变换——
//   ① 外层局部变量改写成 `s.xxx`；② 取消检查点的 `return nil, failed, err`
//   改成 `return err`（由 `Run` 统一返回 `nil, s.failed, err`）。
// 验证它的判据是 `pipeline_behavior_lock_test.go`（提取前的行为锁）**逐字仍绿**。

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"filededup/internal/cache"
	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
	"filededup/internal/progress"
	"filededup/internal/scanner"
	"sort"
)

// scanRun 一趟扫描的共享上下文。字段与 Run 里的局部变量一一对应。
type scanRun struct {
	p       *Pipeline
	cfg     model.ScanConfig
	ctx     context.Context
	cancel  context.CancelFunc
	tracker *progress.Tracker
	stage   func(s, desc string)
	workers int
	pool    *hasher.Pool

	files      []*model.FileEntry
	candidates []*model.FileEntry
	bySize     map[uint64][]*model.FileEntry

	pre        []preEntry
	ids        []fsid.ID
	fulls      [][32]byte
	fullsValid []bool
	pendIdx    []int

	preMu             sync.Mutex
	pending           []cache.Entry
	hitPaths          []string
	idx               atomic.Int64
	cacheOn           bool
	workerPanic       atomic.Pointer[string]
	stagePanicHandler func(string) func(string)

	sampleGroups map[sampleKey][]int
	finalGroups  map[finalKey][]*model.FileEntry
	finalIdx     map[finalKey][]int
	hashTargets  []*model.FileEntry
	hashSlot     []int

	ver    *verifier
	failed []model.FailedItem
	groups []*model.DuplicateGroup
}

// sampleKey 预筛分桶键（P0-2：分桶键只能由采样构成）。
type sampleKey struct {
	size uint64
	head uint64
	tail uint64
	mid1 uint64
	mid2 uint64
}

// preEntry 记录该文件的采样与（若有）全量哈希。
type preEntry struct {
	sample    sampleKey
	full      [32]byte
	fullValid bool // 小文件一趟完成，或缓存带全量
	skip      bool // 预筛失败：已入失败清单，不参与任何分组（防零哈希假组）
}

type finalKey struct {
	size uint64
	full [32]byte
}

// stageScan （原 pipeline.go:398-417，语句逐字搬移）
func (s *scanRun) stageScan() error {
	// ---------- 阶段 0：元数据扫描 ----------
	s.stage("scan", "扫描目录")
	scan := scanner.WalkWithGate(s.ctx, s.cfg.Roots, &s.cfg.Filters, s.workers, s.p.gate)
	if s.ctx.Err() != nil {
		s.p.setStatus(model.StatusCancelled)
		s.failed = scan.Failed
		return s.ctx.Err()
	}
	s.failed = scan.Failed
	s.files = scan.Files
	s.p.scannedFiles.Store(uint64(len(s.files)))
	// M6-P4：保护清单的计数与逃逸根随本轮结果一起记账。
	s.p.protectedDirs.Store(uint64(scan.ProtectedDirs))
	s.p.protectedFiles.Store(uint64(scan.ProtectedFiles))
	s.p.cloudSkipped.Store(uint64(scan.SkippedCloudFiles))
	s.p.workTempSkipped.Store(uint64(scan.SkippedWorkTempFiles)) // M21
	s.p.caseProbeUnproven.Store(uint64(scan.CaseProbeUnproven))  // M62+M85
	s.p.mu.Lock()
	s.p.unprotectedRoots = scan.UnprotectedRoots
	s.p.mu.Unlock()
	s.tracker.SetTotal(uint64(len(s.files)), sumSize(s.files))
	return nil
}

// stageSizeGroup （原 pipeline.go:419-431，语句逐字搬移）
func (s *scanRun) stageSizeGroup() error {
	// ---------- 阶段 1：size 分组，淘汰独 size ----------
	s.bySize = make(map[uint64][]*model.FileEntry)
	for _, e := range s.files {
		s.bySize[e.Size] = append(s.bySize[e.Size], e)
	}
	for size, group := range s.bySize {
		if len(group) >= 2 {
			s.candidates = append(s.candidates, group...)
		}
		_ = size
	}
	sortEntries(s.candidates)
	return nil
}

// stageDedupFileKey （原 pipeline.go:433-458，语句逐字搬移）
func (s *scanRun) stageDedupFileKey() error {
	// ---------- 阶段 1.5：候选组 FileKey 去重（硬链接只计一次）----------
	seen := make(map[model.FileKey]*model.FileEntry, len(s.candidates))
	deduped := s.candidates[:0]
	for _, e := range s.candidates {
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
	s.candidates = deduped
	// 去重后重算候选（组内可能只剩 1 个）
	s.candidates = refilterUnique(s.candidates)
	return nil
}

// stagePrefilter （原 pipeline.go:460-686，语句逐字搬移）
func (s *scanRun) stagePrefilter() error {
	// ---------- 阶段 2：首尾预筛（小文件一趟双哈希）----------
	s.p.setStatus(model.StatusPrefiltering)
	s.stage("prefilter", "预筛哈希")
	// R2：本阶段只处理候选文件、且每文件最多读 SmallFileMax 字节，
	// 总量口径必须与 AddFile/AddBytes 一致，否则进度会虚满、ETA 提前归零
	s.tracker.SetTotal(uint64(len(s.candidates)), prefilterBytes(s.candidates))
	// sampleKey 预筛分桶键；preEntry 记录该文件的采样与（若有）全量哈希。
	// P0-2：分桶键只能由"采样"构成。此前缓存命中带全量的文件被绕过预筛分组
	// 直接送进最终分组，使它与本轮实算的同内容文件不在同一桶内，导致静默漏报。
	s.pre = make([]preEntry, len(s.candidates))
	// H1：打开文件句柄后立刻 fstat，记录物理身份 (dev, ino, ctime)。
	// 它是哈希所依据的那份内容的凭证，用于缓存命中判定与阶段 3 写回。
	s.ids = make([]fsid.ID, len(s.candidates))
	// 阶段 3 全量哈希产物，与 s.candidates 下标平行（独立槽位，避免与采样字段互相污染）
	s.fulls = make([][32]byte, len(s.candidates))
	s.fullsValid = make([]bool, len(s.candidates))
	s.pendIdx = make([]int, len(s.candidates)) // s.candidates → s.pending 下标（-1 = 未入队）
	for i := range s.pendIdx {
		s.pendIdx[i] = -1
	}
	s.idx = atomic.Int64{}
	s.cacheOn = s.p.cch != nil && s.cfg.UseCache
	// ②-P：worker panic 收口——记失败清单 + cancel + 标记，Run 在取消检查点
	// 据标记区分「内部故障 Failed」与「用户 Cancelled」（两种路径前端语义不同）。
	// ★ 这里的参数名仍是 `stage`（搬移时它遮蔽了 s.stage，属**闭包形参**，
	//   不能加 s. 前缀——机械替换曾把它写成 s.stage 导致编译不过，已就地修正）。
	s.stagePanicHandler = func(stage string) func(string) {
		return func(msg string) {
			m := stage + ": " + msg
			s.workerPanic.CompareAndSwap(nil, &m)
			s.preMu.Lock()
			s.failed = append(s.failed, model.FailedItem{Stage: "worker", Err: m})
			s.preMu.Unlock()
			s.cancel()
		}
	}
	runWorkers(s.ctx, s.workers, s.stagePanicHandler("prefilter"), func() error {
		localHits := make([]string, 0, 64) // R1：worker 本地累积，退出时合并（免锁热路径）
		defer func() {
			if len(localHits) == 0 {
				return
			}
			s.preMu.Lock()
			s.hitPaths = append(s.hitPaths, localHits...)
			s.preMu.Unlock()
		}()
		for {
			i := int(s.idx.Add(1) - 1)
			if i >= len(s.candidates) {
				return nil
			}
			if err := s.p.gate.Wait(s.ctx); err != nil {
				return err
			}
			e := s.candidates[i]
			// 计算实际文件的抽样哈希（head/tail）。即便命中缓存也要重算：size/mtime
			// 可信，但内容可能因 cp -p/COW/FAT 2s 粒度/原地改写保 mtime 等而变更且
			// mtime 不变（C1）。命中时以实际抽样与缓存抽样比对，一致才信任缓存 full，
			// 否则内容已变、full 须在阶段 3 重算，避免把"内容已变但缓存哈希仍是旧内容"
			// 的文件误判进重复组（进而误删）。
			f, err := os.Open(e.Path)
			if err != nil {
				s.preMu.Lock()
				s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "prefilter", Err: err.Error()})
				s.preMu.Unlock()
				s.pre[i].skip = true
				continue
			}
			// H1：持有句柄时立刻取物理身份 (dev, ino, ctime)。它是哈希所依据的
			// 那份内容的凭证，用于缓存命中判定与阶段 3 写回。Windows 只有句柄
			// 查询拿得到（I7），故统一走 FromFile 而非 FileInfo。
			s.ids[i] = fsid.FromFile(f)
			buf := s.pool.GetSmallBuf()
			r, err := hasher.HashHeadTail(f, int64(e.Size), buf)
			s.pool.PutSmallBuf(buf)
			f.Close()
			if err != nil {
				s.preMu.Lock()
				s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "prefilter", Err: err.Error()})
				s.preMu.Unlock()
				s.pre[i].skip = true
				continue
			}
			// 2026-09-19 修复（Windows 首扫 0 组 / 未保存缓存）：
			// 短读（实际可读 < 遍历时记录的 size）不再丢弃该文件，而是**用实际
			// 长度就地纠正条目**再参与分桶。此前调用方拿不到短读事实，只能沿用
			// 失真的 size；而 hasher 又会把 ErrUnexpectedEOF 当错误返回，文件被
			// skip=true 整个剔除 → 整组重复静默消失（用户：扫不到重复），且永远
			// 走不到 s.pending 入队那一行（用户：未保存缓存）。
			//
			// 纠正而非沿用是关键：若仍用失真的 size 去分桶，两个内容不同的文件
			// 可能因截断被算成同一哈希，造出假重复组——比漏报更危险。
			if r.Short {
				actual := uint64(r.ActualSize)
				if actual == 0 {
					// 推不出任何有效长度（文件已被删空/不可读）：记失败并剔除，
					// 避免把零长度塞进分桶造出假组。
					s.preMu.Lock()
					s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "prefilter",
						Err: "文件在扫描期间被截断为空或不可读，已跳过"})
					s.preMu.Unlock()
					s.pre[i].skip = true
					continue
				}
				s.preMu.Lock()
				s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "prefilter",
					Err: fmt.Sprintf("文件在扫描期间被修改：记录 %d 字节，实际可读 %d 字节；已按实际长度参与去重",
						e.Size, actual)})
				s.preMu.Unlock()
				// 就地纠正：size 是分桶键，必须与哈希所依据的内容一致。
				e.Size = actual
				// REAL-1（2026-09-21 审查）：实占栏必须同步作废。Actual 是按**截断前
				// 那份**测得的读数，留着它就成了"逻辑栏已纠正、实占栏没纠正"——
				// 双口径并存的前提是两个口径各自说真话（I6），否则界面上一栏说真话、
				// 另一栏冒充，比两处都错更难查。作废后按 ActualBytes() 的回退口径
				// 退回逻辑大小，并由 AnyActualKnown 如实显示"未统计"。
				e.ActualKnown = false
				e.Actual = 0
			}
			if s.cacheOn {
				if ent, hit, fullValid := s.p.cch.Lookup(e.Path, e.Size, e.ModTime, s.ids[i]); hit {
					localHits = append(localHits, e.Path)
					s.p.cacheHits.Add(1) // AS-K1：正向信号，"查到记录"即计一次
					var full [32]byte
					fullValidNow := false
					// 四点采样全一致 → 内容极可能未变，信任缓存 full；否则内容已变。
					if r.Partial.Head == ent.Head && r.Partial.Tail == ent.Tail &&
						r.Partial.Mid1 == ent.Mid1 && r.Partial.Mid2 == ent.Mid2 {
						if fullValid {
							copy(full[:], ent.Full)
						}
						fullValidNow = fullValid
					} else if r.Small {
						// R-算法-6：采样不符 → 缓存 full 已过时（弃用）；但小文件本轮一趟
						// 已算出真 full（r.Full），直接采用，免去阶段 3 对同一文件的全量重算
						// （结果等价，纯省一次全量读；方向安全）。
						full = r.Full
						fullValidNow = true
					}
					s.pre[i] = preEntry{
						sample: sampleKey{
							size: e.Size, head: r.Partial.Head, tail: r.Partial.Tail,
							mid1: r.Partial.Mid1, mid2: r.Partial.Mid2,
						},
						full:      full,
						fullValid: fullValidNow,
					}
					s.tracker.AddFile()
					// R2：命中视作该文件的预筛工作量已完成，字节数同样计入，
					// 否则命中越多进度越滞后（总量口径已按预筛读量设定）
					s.tracker.AddBytes(minU64(e.Size, hasher.PrefilterMax))
					continue
				}
			}
			// 未命中：用实际抽样分桶，full 视小文件与否（小文件一趟即得全量）
			var ent cache.Entry
			ent = cache.Entry{Path: e.Path, Size: e.Size, MtimeNs: e.ModTime,
				Head: r.Partial.Head, Tail: r.Partial.Tail,
				Mid1: r.Partial.Mid1, Mid2: r.Partial.Mid2,
				Dev: s.ids[i].Dev, Ino: s.ids[i].Ino, CtimeNs: s.ids[i].CtimeNs}
			if r.Small { // 小文件一趟双哈希：full 一并缓存
				ent.Full = r.Full[:]
			}
			s.preMu.Lock()
			s.pendIdx[i] = len(s.pending) // P0-2：阶段 3 补算后原位更新，不再追加第二条
			s.pending = append(s.pending, ent)
			s.preMu.Unlock()
			s.pre[i] = preEntry{
				sample: sampleKey{
					size: e.Size, head: r.Partial.Head, tail: r.Partial.Tail,
					mid1: r.Partial.Mid1, mid2: r.Partial.Mid2,
				},
				full:      r.Full,
				fullValid: r.Small, // 小文件一趟即得全量
			}
			s.tracker.AddFile()
			s.tracker.AddBytes(minU64(e.Size, hasher.PrefilterMax))
		}
	})
	if s.ctx.Err() != nil {
		if pm := s.workerPanic.Load(); pm != nil { // ②-P：内部故障按 Failed 收口
			s.p.setStatus(model.StatusFailed)
			return fmt.Errorf("扫描内部故障（%s）", *pm)
		}
		s.p.setStatus(model.StatusCancelled)
		return s.ctx.Err()
	}
	return nil
}

// bucketBySample （原 pipeline.go:688-706，语句逐字搬移）
func (s *scanRun) bucketBySample() error {
	// ---------- 分桶：一律按 (size, head, tail) 采样聚合 ----------
	// P0-2：修正前带全量哈希的缓存命中文件会绕过采样分桶、直接进最终分组，
	// 于是同内容的新文件只能在"本轮实算采样的文件"里找同伴，永远碰不到
	// 已缓存的那份 → 自己孤身一桶 → 进不了阶段 3 → 被静默丢弃（漏报）。
	// 实测：三个内容相同的大文件，二次扫描只报出 2 个。
	// 现在命中缓存仅免除阶段 3 的重算，不再改变分组资格。
	s.finalGroups = make(map[finalKey][]*model.FileEntry)
	s.sampleGroups = make(map[sampleKey][]int) // 采样 → s.candidates 下标
	for i := range s.candidates {
		if s.pre[i].skip {
			continue // 预筛失败：已入失败清单，不得进入分组（防零值键假组）
		}
		k := s.pre[i].sample
		s.sampleGroups[k] = append(s.sampleGroups[k], i)
	}
	return nil
}

// stageFullHash （原 pipeline.go:708-822，语句逐字搬移）
func (s *scanRun) stageFullHash() error {
	// ---------- 阶段 3：大文件全量 BLAKE3（仅补算缺失者）----------
	s.p.setStatus(model.StatusHashing)
	s.stage("hash", "全量哈希")
	for _, idxs := range s.sampleGroups {
		if len(idxs) < 2 {
			continue // 采样唯一 → 无同伴，不可能成组
		}
		for _, ci := range idxs {
			if s.pre[ci].fullValid {
				continue // 已有全量哈希（缓存命中或小文件一趟），不重算
			}
			s.hashTargets = append(s.hashTargets, s.candidates[ci])
			s.hashSlot = append(s.hashSlot, ci)
		}
	}
	s.idx.Store(0)
	// R2：切换总量口径——已完成量 + 本阶段待哈希量。
	// 修正前 Phase2 的预筛读量与 Phase3 的全量读量重复累加，
	// BytesDone 可超 BytesTotal，导致进度虚满、ETA 提前归零。
	filesDone, bytesDone := s.tracker.Done()
	s.tracker.SetTotal(filesDone+uint64(len(s.hashTargets)), bytesDone+sumSize(s.hashTargets))
	runWorkers(s.ctx, s.workers, s.stagePanicHandler("hash"), func() error {
		for {
			i := int(s.idx.Add(1) - 1)
			if i >= len(s.hashTargets) {
				return nil
			}
			if err := s.p.gate.Wait(s.ctx); err != nil {
				return err
			}
			e := s.hashTargets[i]
			f, err := os.Open(e.Path)
			if err != nil {
				s.preMu.Lock()
				s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "hash", Err: err.Error()})
				s.preMu.Unlock()
				s.hashTargets[i] = nil // 失败剔除，不参与最终分组（防零哈希假组）
				continue
			}
			ci := s.hashSlot[i]
			// B2（R-算法-2）：阶段 3 是**第二次** open（阶段 2 已 open 取采样与身份
			// s.ids[ci]）。两次 open 之间文件可能被顶替（同步盘落新版、下载器原子改名
			// 进来）：此时 full 算的是新内容，而 s.ids[ci]/采样仍是旧文件的，写回缓存就
			// 存下"旧身份 + 新内容"——日后旧文件回位且 size/mtime 未变，Lookup 四点采样
			// 全过 → 返回新内容的 full → 旧文件被并进新内容的组（假重复组）。故 open
			// 成功后立刻重取句柄身份与 s.ids[ci] 比对：不一致即判"扫描期间被替换"，不写
			// 回、剔除分组、记 s.failed。SameIdentity 任一侧未解析时返回 true（fail-open）
			// ——这正是此处要的方向：FAT/exFAT 等无稳定索引的卷无从判断替换，强行判否
			// 会把这些卷上的大文件全部误剔（漏报）；身份可解析的卷（NTFS/ext4/APFS）
			// 才真正生效。接缝 phase3IdentityFn 仅供测试注入顶替。
			if cur := phase3IdentityFn(f); !cur.SameIdentity(s.ids[ci]) {
				f.Close()
				s.preMu.Lock()
				s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "hash",
					Err: "文件在扫描期间被替换（身份已变化），已跳过"})
				s.preMu.Unlock()
				s.hashTargets[i] = nil
				continue
			}
			var full [32]byte
			if e.Size > 512<<20 {
				// 大文件：分段预取流水线。段缓冲由环形池自管理（Y2），
				// 无需借 pooled 读缓冲——旧实现借出却未使用，纯属浪费
				full, err = hasher.HashFullSegmented(f, int64(e.Size), hasher.LargeSeg, 2, nil)
			} else {
				buf := s.pool.GetStreamBuf()
				full, err = hasher.HashFull(f, int64(e.Size), buf)
				s.pool.PutStreamBuf(buf)
			}
			f.Close()
			if err != nil {
				s.preMu.Lock()
				s.failed = append(s.failed, model.FailedItem{Path: e.Path, Stage: "hash", Err: err.Error()})
				s.preMu.Unlock()
				s.hashTargets[i] = nil
				continue
			}
			// 全量哈希只写进独立的 s.fulls 槽位：s.pre[ci].sample 不得改动，
			// 它仍是本阶段分桶与写回缓存的依据。
			s.preMu.Lock()
			s.fulls[ci] = full
			s.fullsValid[ci] = true
			// P0-2：原位补上 full，保留阶段 2 已写入的采样。
			// 修正前这里 append 一条不含 Head/Tail 的新行，UPSERT 整行覆盖
			// 把缓存里的 partial 清零 → 该文件后续扫描再也无法与新文件同桶。
			if pi := s.pendIdx[ci]; pi >= 0 && pi < len(s.pending) {
				cp := make([]byte, len(full))
				copy(cp, full[:])
				s.pending[pi].Full = cp
			} else {
				cp := make([]byte, len(full))
				copy(cp, full[:])
				s.pending = append(s.pending, cache.Entry{
					Path: e.Path, Size: e.Size, MtimeNs: e.ModTime,
					Head: s.pre[ci].sample.head, Tail: s.pre[ci].sample.tail,
					Mid1: s.pre[ci].sample.mid1, Mid2: s.pre[ci].sample.mid2,
					Dev: s.ids[ci].Dev, Ino: s.ids[ci].Ino, CtimeNs: s.ids[ci].CtimeNs,
					Full: cp,
				})
			}
			s.preMu.Unlock()
			s.tracker.AddFile()
			s.tracker.AddBytes(e.Size)
		}
	})
	if s.ctx.Err() != nil {
		if pm := s.workerPanic.Load(); pm != nil { // ②-P：内部故障按 Failed 收口
			s.p.setStatus(model.StatusFailed)
			return fmt.Errorf("扫描内部故障（%s）", *pm)
		}
		s.p.setStatus(model.StatusCancelled)
		return s.ctx.Err()
	}
	return nil
}

// groupByFullHash （原 pipeline.go:824-856，语句逐字搬移）
func (s *scanRun) groupByFullHash() error {
	// ---------- 最终分组：按全量哈希聚合 ----------
	// s.fulls[] 是阶段 3 的产物，与 s.candidates 下标平行；s.pre[ci].full 覆盖缓存/小文件来源。
	// 先按全量哈希聚合候选下标，再据阶段 2 句柄身份折并同物理文件（B1）。
	s.finalIdx = make(map[finalKey][]int)
	for _, idxs := range s.sampleGroups {
		if len(idxs) < 2 {
			continue
		}
		for _, i := range idxs {
			full := s.pre[i].full
			if !s.pre[i].fullValid {
				if !s.fullsValid[i] {
					continue // 全量哈希失败：已入失败清单，剔除分组（防零哈希假组）
				}
				full = s.fulls[i]
			}
			k := finalKey{size: s.candidates[i].Size, full: full}
			s.finalIdx[k] = append(s.finalIdx[k], i)
		}
	}
	// B1（R-算法-1）：每个全量哈希桶内据 s.ids[]（阶段 2 fsid.FromFile 句柄身份）折并
	// 确信同一物理文件的条目——硬链接 / 同 inode 多路径。这是阶段 1.5 ResolveKey 的
	// 权威兜底：Windows 独占锁等致 ResolveKey 未解析时，同 inode 两路径会双双入桶成
	// 假重复组；此处折并即消除。仅两侧均解析且同 Dev+Ino 才并（见 collapseSameIdentity）。
	for k, idxs := range s.finalIdx {
		entries := make([]*model.FileEntry, len(idxs))
		bucketIDs := make([]fsid.ID, len(idxs))
		for j, i := range idxs {
			entries[j] = s.candidates[i]
			bucketIDs[j] = s.ids[i]
		}
		s.finalGroups[k] = collapseSameIdentity(entries, bucketIDs)
	}
	return nil
}

// emitGroups （原 pipeline.go:859-922，语句逐字搬移）
func (s *scanRun) emitGroups() error {
	// ---------- 阶段 4（可选）：paranoid 逐字节确认 ----------
	// G1：比对缓冲在整趟 paranoid 中只分配一对，组间与文件间复用。
	// 原实现每次比对新分配 2×256KiB（组内文件数-1 次），
	// 万级组场景下成为纯 GC 负担。
	if s.cfg.Paranoid {
		s.stage("verify", "逐字节确认")
		s.ver = newVerifier()
	}

	// ---------- 输出 ----------
	for k, g := range s.finalGroups {
		if len(g) < 2 {
			continue
		}
		sortEntries(g)
		if s.cfg.Paranoid {
			// ②-B：paranoid 是纯 I/O 长阶段——组间过 gate（暂停即时停步），
			// 取消经 s.ctx 在组内逐文件/逐块感知（见 group/equal）。
			if err := s.p.gate.Wait(s.ctx); err != nil {
				s.p.setStatus(model.StatusCancelled)
				return s.ctx.Err()
			}
			g, failedAcc := s.ver.group(s.ctx, g, s.failed)
			// group 的第二个返回值是「累加后的完整失败清单」（其内部直接 append
			// 到传入的 s.failed）。必须整表接管而非再 append：否则每输出一个组
			// 就把既有失败翻一倍（G 组 → 2^G 条），失败统计与历史写入全部失真。
			// 组被 verify 拆散（len(g)<2）时同样要接管，否则该组新增的失败被吞。
			s.failed = failedAcc
			if err := s.ctx.Err(); err != nil {
				s.p.setStatus(model.StatusCancelled)
				return err
			}
			if len(g) < 2 {
				continue
			}
		}
		s.groups = append(s.groups, &model.DuplicateGroup{
			Files:             g,
			Reclaimable:       (uint64(len(g)) - 1) * g[0].Size,
			ReclaimableActual: model.ReclaimActual(g),
			Hash:              k.full, // 组内容哈希（M3 操作前校验依据）
		})
	}
	// 稳定排序：可释放空间降序，其次组大小，再次组内最小路径。
	// M94：三级键缺一个都会把上面那个 map 的随机遍历序漏进输出——改前的次级键写的是
	// GroupID，而 GroupID 那时已由遍历序发放，等于"用一个随机量去消随机"。
	// 最小路径这一级是**全序收尾**：一个路径只属于一个组，故三级用尽后不再有平手。
	sort.Slice(s.groups, func(i, j int) bool {
		if s.groups[i].Reclaimable != s.groups[j].Reclaimable {
			return s.groups[i].Reclaimable > s.groups[j].Reclaimable
		}
		if ni, nj := len(s.groups[i].Files), len(s.groups[j].Files); ni != nj {
			return ni > nj
		}
		return s.groups[i].Files[0].Path < s.groups[j].Files[0].Path
	})
	// 组号在排序**之后**按序号发放（1..N）：同输入两跑的组序与组号因此都一致。
	for i := range s.groups {
		s.groups[i].GroupID = uint64(i + 1)
	}
	// M4：写回与命中续期统一在 defer 中执行（含 Cancelled 路径）
	s.p.setStatus(model.StatusDone)
	return nil
}
