// Package dedup 多阶段过滤流水线编排 + 任务状态机（04 M1-T09，01 §4）。
package dedup

import (
	"bytes"
	"context"
	"errors"
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
)

// phase3IdentityFn 是阶段 3「重取句柄身份」的包级接缝，生产恒为 fsid.FromFile，
// 仅供测试注入"阶段 2 与阶段 3 两次 open 之间文件被顶替"的情形（与 ops 的
// fsidFromPathFn 同族，见 internal/ops/verify.go:103）。
var phase3IdentityFn = fsid.FromFile

// Pipeline 一次扫描任务。
type Pipeline struct {
	mu          sync.Mutex
	status      model.TaskStatus
	afterResume model.TaskStatus // 暂停期间到达的阶段，Resume 时恢复
	gate        *Gate
	cancel      context.CancelFunc
	cch         *cache.Cache // 可选：哈希缓存（M4）

	// scannedFiles 阶段 0 采集到的语料文件总数。进度事件的 FilesTotal 按设计
	// 是"当前阶段"口径（R2：阶段切换后重设为该阶段处理量），缓存命中扫尤其明显，
	// 因此需要"本轮共扫描多少文件"这一稳定口径的调用方（fdd-cli 报告）走这里。
	scannedFiles atomic.Uint64

	// cacheHits 本轮预筛阶段在缓存里查到记录的次数（AS-K1）。
	// 单独立一个数的理由：冒烟脚本原先只把三跑结论互比，那证明不了"缓存真的
	// 被用上"——UseCache 一旦断线，三跑就等价于三次冷扫，门禁照样全绿。
	// 有了这个正向信号，"没命中"才第一次会变成红。
	cacheHits atomic.Uint64

	// M6-P4（2026-09-21，04 §6.7 C 组 4）系统保护清单的可见计数，与 scannedFiles
	// 同为按轮归零。为什么要经 Pipeline 而不是只留在 scanner.Result：绑定层的
	// scan:done 载荷与 CLI 报告都从 Pipeline 取数，而扫描结果在流水线里会被
	// 过滤/分组改写，事后拿不到"剪了多少"这件事。
	protectedDirs  atomic.Uint64
	protectedFiles atomic.Uint64
	// cloudSkipped 是 M6-P1 跳过的云端占位文件数。与 protected* 同一套按轮归零
	// 的下发路径：占位数影响"这次到底算了多少文件"，必须在 scan:done 里跟着走，
	// 不能只留在 scanner.Result（流水线后面的阶段会改写结果集，事后拿不到）。
	cloudSkipped atomic.Uint64
	// workTempSkipped 是 M21 按 worktemp.IsTempName 跳过的工作临时名文件数
	// （应用自己的 .fdd-* 残留，04 §6.8.8 M21）。走与 protected*/cloudSkipped
	// 完全相同的下发路径：绑定的 scan:done 载荷与 CLI 报告都从这里取数。
	workTempSkipped atomic.Uint64
	// caseProbeUnproven 是 M62+M85 的"本轮有多少个根的卷语义是从平台默认来的"计数，
	// 与 protected*/cloudSkipped/workTempSkipped 同一套按轮归零的下发路径（设计稿 §28.2 ②）。
	// 计数从 scanner.Result 转存而不是让绑定层去 import scanner：流水线后面的阶段会改写
	// 结果集，事后拿不到"当初问卷问到了什么"。
	caseProbeUnproven atomic.Uint64
	// unprotectedRoots 是"因用户显式指定而脱离系统保护"的根，界面须警示。
	// 它是字符串集合而非计数，故不塞进 atomic；只在扫描收尾写一次，读在 Run 之后。
	unprotectedRoots []string

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

// ScannedFiles 返回本轮 Run 阶段 0 采集到的语料文件总数（Run 结束后读取）。
// 不复用进度事件的 FilesTotal：那是阶段口径（R2），进入预筛/哈希阶段后会被
// 重设为该阶段的处理量，缓存命中扫下远小于语料数，作为报告统计会误导。
func (p *Pipeline) ScannedFiles() uint64 { return p.scannedFiles.Load() }

// CacheHits 返回本轮 Run 在哈希缓存里查到记录的候选文件数（Run 结束后读取）。
// 与 ScannedFiles 同为"按轮归零"的口径：跨轮累加会让"这一跑到底有没有吃到
// 缓存"看不出来（AS-K1）。
func (p *Pipeline) CacheHits() uint64 { return p.cacheHits.Load() }

// ProtectedDirs 返回本轮被系统保护清单剪掉的**目录**数（Run 结束后读取）。
// 只数目录：剪枝没有下潜，报成"跳过了 N 个文件"就是编数。
func (p *Pipeline) ProtectedDirs() uint64 { return p.protectedDirs.Load() }

// ProtectedFiles 返回本轮被保护清单跳过的文件数（盘根伪文件、Windows 保留名）。
func (p *Pipeline) ProtectedFiles() uint64 { return p.protectedFiles.Load() }

// CloudSkipped 返回本轮因"云端占位"跳过的文件数（M6-P1）。
// AllowCloudHydration=true 时恒为 0：那一档下占位文件照常参与，没有跳过这件事。
func (p *Pipeline) CloudSkipped() uint64 { return p.cloudSkipped.Load() }

// WorkTempSkipped 返回本轮按 worktemp.IsTempName 跳过的工作临时名文件数（M21）。
// 口径是"到达的文件项里名字命中"，与扩展名/大小/隐藏设置无关；不含目录
// （临时命名的目录不会被跳过，见 scanner.go 的 M1 注释）。
func (p *Pipeline) WorkTempSkipped() uint64 { return p.workTempSkipped.Load() }

// CaseProbeUnproven 返回本轮**问卷过、但卷大小写语义来自平台默认**的扫描根数（M62+M85）。
// 0 有两种成因，别当成"这一卷实测过"：要么所有根都拿到了读数，要么单根一趟压根没问卷
// （C1，见 scanner.dedupeRoots）。口径与限制都写在 scanner.Result 同名字段上。
func (p *Pipeline) CaseProbeUnproven() uint64 { return p.caseProbeUnproven.Load() }

// UnprotectedRoots 返回"因用户显式指定而脱离系统保护"的扫描根（Run 结束后读取）。
// 非空即意味着这一轮有一部分扫描是在保护清单之外跑的，界面必须警示：
// 用户可能是故意的，也可能是误选了系统目录。
func (p *Pipeline) UnprotectedRoots() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.unprotectedRoots
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

// claimRunLocked 认领这一轮扫描：终态复位 → 状态机校验 → **当场写入** Scanning → 存 cancel。
//
// M71：判与写必须在同一个 p.mu 临界区内。改前这一段只判不写，Scanning 要等到
// 释锁、六个原子计数归零、gate.Resume() 之后由 setStatus 第二次取锁才写进去；
// 那个窗口里 Pause() 读到 Idle 便回"当前不在扫描中"、Cancel() 读到 running=false
// 便回"没有进行中的扫描任务"——两句都是假话，用户按字面理解会以为按钮没反应。
// app 层的 scanInFlight 挡板（app.go:591）只是把这条路堵在界面侧，判据本身仍不闭合。
//
// 调用方须持 p.mu。返回非 nil 即未认领成功，状态与 cancel 一律不留痕（可安全重试）。
func (p *Pipeline) claimRunLocked(cancel context.CancelFunc) error {
	// P0-1：终态（Done/Cancelled/Failed）自动经 Idle 复位，使「再次扫描」成为
	// 受支持的路径。状态机表本身不动（仍只允许 Idle→Scanning 进入运行态），
	// 因此 Done→Scanning 依旧非法——复位是一次显式的 Done→Idle→Scanning。
	// 不这样做的话：Run 结束只会置终态、无人置 Idle，第二次 Run 永久被拒，
	// 而 app 层已清空旧结果集 → 界面永久卡在「扫描中」（增量缓存特性完全不可达）。
	if p.isTerminalLocked() {
		p.status = model.StatusIdle
	}
	if !model.ValidateTransition(p.status, model.StatusScanning) {
		return fmt.Errorf("非法状态转换: %s → Scanning", p.status)
	}
	p.status = model.StatusScanning
	p.cancel = cancel
	p.afterResume = ""
	// M6-P4：与 scannedFiles/cacheHits 同口径按轮归零。上一轮的逃逸根若不清，
	// 本轮即便什么都没逃逸，界面也会继续挂着上一条扫描的"已脱离系统保护"警示。
	// 切片归零与其余状态一样在 mu 内完成（访问器同一把锁读）。
	p.unprotectedRoots = nil
	return nil
}

// cacheOpErrText 把 cache.Store/Touch 的错误分诊成一句**符合事实**的话（M75a / M74）。
// 判据用哨兵错误而不是重新解释 SQLite 文本（I5：分类只在 cache 侧做一次）。
// ok=false 表示这件事已由轮末那条停用说明统一交代，再报一条"失败"反倒成假话。
//
// 〔2026-09-27 M281〕造句本体搬到 cacheWriteBackErrText：这里只留"停用态不造句"这一半，
// 因为 M74 那条边界（ok=false）被两处消费（Run 的 defer 与测试），不该和文案混在一起。
func cacheOpErrText(op string, e error) (string, bool) {
	if errors.Is(e, cache.ErrCorruptDisabled) {
		return "", false
	}
	return cacheWriteBackErrText(op, e), true
}

// cacheWriteBackErrText 是缓存降级那两档的**唯一造句点**（M281）。
//
// 修前的形状（真机读数 W9-7）：`缓存写回失败: database is locked (5) (SQLITE_BUSY)`
// ——SQLite 的英文内部串当主语直达用户，而且句子里读不出"这跟我的文件有没有关系"。
// 三条边界：
//  1. 先讲**降级**（不影响本次去重结果）再讲原因：缓存写回失败在语义上不是"文件没处理成"；
//  2. 系统原文只能待在尾注里（本仓真机读数按原文归档，译文会随版本改，取证要对表）；
//  3. 归因表只收**有读数**的一档（占用），认不出来的错误一律交回原文、不配猜来的原因
//     ——与 M280 的 shFileOperationReason 同一纪律。
//
// op 分岔是因为两档的**后果不同**，共用一句就成了假话：写回失败 ⇒ 这批哈希没进库、
// 下次要重算；续期失败 ⇒ 条目早已在库里（哈希没丢），只是 LRU 的记账没刷新、可能被提前淘汰。
func cacheWriteBackErrText(op string, e error) string {
	if errors.Is(e, cache.ErrEvictFailed) {
		// 这一档哨兵自带"哈希条目已写回，仅 LRU 淘汰未完成"——改前这里一律写成
		// "缓存写回失败"，而 Commit 早就成功了（§18.0 取证 #8）。原文照抄最诚实。
		return "缓存淘汰未完成（不影响本次去重结果）：" + e.Error()
	}
	subject := "缓存写回"
	consequence := "这批哈希没有进库，下次扫描还要重算"
	if op == "缓存命中续期" {
		subject = "缓存命中续期"
		consequence = "条目本身还在库里，只是 LRU 的「最近用过」没刷新，它可能被提前淘汰"
	}
	cause := ""
	if cache.IsBusy(e) {
		cause = "缓存库正被其他程序占用（另一个窗口或命令行版同时打开同一个库最常见），稍后重试即可"
	}
	tail := "系统原文：" + e.Error()
	if cause != "" {
		tail = cause + "。" + tail
	}
	return subject + "未完成（不影响本次去重结果）：" + consequence + "。" + tail
}

// cacheStageItem 造一条 `Stage="cache"` 的失败项（M281：三处造句点共用）。
//
// 为什么 Path 里放的是**库文件**而不是某个用户文件：缓存失败根本没有对应到某个文件，
// 空 `Path` 又不是可定位信息（抽屉里的"显示路径"那一列会是空白，用户无从判断这是哪件事）。
// 没挂库时返回空串，不凭空造路径。
func (p *Pipeline) cacheStageItem(msg string) model.FailedItem {
	item := model.FailedItem{Stage: "cache", Err: msg}
	if p.cch != nil {
		item.Path = p.cch.DBPath()
	}
	return item
}

// corruptCacheNotice 生成轮末那条"库被确证损坏 ⇒ 停用"的说明（M74）。
//
// 措辞边界钉在这里，且**全轮只有这一个造句点、在 Run 的 defer 里**：
// Lookup 侧只计数不造句（几万条点查会刷几万条失败清单），所以"至多一条"
// 是结构保证而不是概率。内容边界：只说"已停用"，不得出现"已隔离/已重建/已自愈"
// ——运行期重建没做（§18.6 → 新登记 M87），说了就是假话。
//
// M95：计数取自 Cache.dbErrs 的 Load()，而它**没有任何轮内重置点**（全仓只有
// Add/Load 两个访问点）⇒ 文案不得称"本轮"。corrupt 是粘滞位，从第 2 轮起每轮末
// 都会把进程启动以来的总数再报一遍；写成"本轮"等于让同一个数字每轮都变大还自称本轮。
func corruptCacheNotice(dbErrs int64, corrupted bool) string {
	if !corrupted {
		return ""
	}
	return fmt.Sprintf("哈希缓存被确证损坏，已停用（进程启动以来累计库错误 %d 次）："+
		"去重结果不受影响，但缓存不再命中，之后每轮都要重算哈希（重启应用才会重新开库）", dbErrs)
}

// Run 执行完整流水线。返回重复组与失败清单；ctx 取消返回 context 错误。
// 命名返回值：defer 中的缓存写回（含 Cancelled 路径，01 §5.5）需修改返回值。
func (p *Pipeline) Run(parent context.Context, cfg model.ScanConfig) (groups []*model.DuplicateGroup, failed []model.FailedItem, err error) {
	ctx, cancel := context.WithCancel(parent)
	p.mu.Lock()
	claimErr := p.claimRunLocked(cancel)
	p.mu.Unlock()
	if claimErr != nil {
		cancel()
		return nil, nil, claimErr
	}
	p.scannedFiles.Store(0)
	p.cacheHits.Store(0) // AS-K1：按轮归零，见 CacheHits 注释
	p.protectedDirs.Store(0)
	p.protectedFiles.Store(0)
	p.cloudSkipped.Store(0)      // M6-P1：同口径按轮归零，否则上一轮的占位数会串进本轮报告
	p.workTempSkipped.Store(0)   // M21：同口径按轮归零
	p.caseProbeUnproven.Store(0) // M62+M85：同口径按轮归零，少这行 = 上一轮的未确证串进本轮
	// 闸门复位：上一轮若在 Paused 下被取消（父 ctx 直接取消、未走 CancelScan），
	// gate 仍处于关闭态；不复位则本轮 worker 的 gate.Wait 会永久阻塞。
	p.gate.Resume()
	defer cancel()
	// M71 后这里不再是"写入 Scanning"的落点（认领时已写），只剩一格真实职责：
	// 若在释锁与本行之间被 Pause() 抢走，setStatus 见 Paused 会把它记进 afterResume，
	// Resume 后回到 Scanning 而不是把暂停态吞掉。
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

	// M341（2026-09-28）：阶段体已整体搬到 pipeline_stages.go（**语句逐字未动**，
	// 只把外层局部变量改写成 s.xxx）。这里构造一趟扫描的共享上下文并依次调用。
	s := &scanRun{
		p:       p,
		cfg:     cfg,
		ctx:     ctx,
		cancel:  cancel,
		tracker: tracker,
		stage:   stage,
		workers: workers,
		pool:    pool,
		cacheOn: p.cch != nil && cfg.UseCache,
	}
	// M4：任务结束单事务批量写回 + 命中续期（含 Cancelled：已算哈希不浪费，01 §5.5）
	// ★ M341 搬移时的重要更正：这个 defer 原本就挂在 **Run** 上（在所有阶段之后
	// 执行），不是挂在阶段 2 上。搬移时它随 stagePrefilter 一起走了，导致缓存
	// 写回提前到阶段 3 之前 ⇒ 阶段 3 补算的 full 不再写回（P0-2 的原位补全会
	// 落空）。第一次搬移后 TestCacheSecondScan 等四条用例当场变红即此事。
	// 现按原语义移回 Run：写回时机与提取前一致。
	defer func() {
		if !s.cacheOn {
			return
		}
		if len(s.pending) > 0 {
			if e := s.p.cch.Store(s.pending); e != nil {
				// 缓存失败不影响结果正确性：计入失败清单提示
				if text, ok := cacheOpErrText("缓存写回", e); ok {
					s.failed = append(s.failed, s.p.cacheStageItem(text))
				}
			}
		}
		if len(s.hitPaths) > 0 {
			if e := s.p.cch.Touch(s.hitPaths); e != nil {
				if text, ok := cacheOpErrText("缓存命中续期", e); ok {
					s.failed = append(s.failed, s.p.cacheStageItem(text))
				}
			}
		}
		// M74：本轮把库确证成损坏 ⇒ 至多**一条**说明（造句点见 corruptCacheNotice）。
		if msg := corruptCacheNotice(s.p.cch.DBErrors(), s.p.cch.Corrupted()); msg != "" {
			s.failed = append(s.failed, s.p.cacheStageItem(msg))
		}
		// ★ M341 搬移时的第二处更正（比第一处更隐蔽）：`failed` 是 **命名返回值**。
		// 原代码 defer 里写的是 `failed = append(failed, …)`，改的是返回值变量本身，
		// 所以在 return 求值**之后**执行也仍然生效；搬进 scanRun 后写成了
		// `s.failed = append(s.failed, …)`，而 `return s.groups, s.failed, nil`
		// 会**先**把当时的 s.failed 拷进返回值，defer 之后的修改就丢了
		// ⇒ TestRunReportsCacheWriteBackFailureOnce / TestM281… 两条当场读不到
		// cache 失败项（实测 0 条）。
		// 修法是保持 defer 内对 s.failed 的追加（与阶段方法同一口径），末尾显式
		// 同步回命名返回值——命名返回值在 defer 中的赋值会覆盖 return 时的求值。
		failed = s.failed
	}()

	for _, step := range []func() error{
		s.stageScan, s.stageSizeGroup, s.stageDedupFileKey, s.stagePrefilter,
		s.bucketBySample, s.stageFullHash, s.groupByFullHash, s.emitGroups,
	} {
		if err := step(); err != nil {
			return nil, s.failed, err
		}
	}
	return s.groups, s.failed, nil

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
// 第二个返回值是把本组新增失败 append 到入参 failed 之后的**完整累加表**：
// 调用方必须整表接管，勿再 append（否则既有失败随组数翻倍）。
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
	// HASH-1（2026-09-21 审查）：负数声明长度下 `remain > 0` 一次都不成立，
	// 修正前的实现会**一个字节都不读就 return true**——内容完全不同的两个文件
	// 被判一致，最后防线反过来成了唯一的放行口。坏卷/畸形 FUSE-SMB 挂载确实会
	// 报出负的 st_size，故入口 fail-closed。
	if size < 0 {
		return false, fmt.Errorf("逐字节比对无法进行：声明长度不可信（%d 字节）", size)
	}
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
	// PARA-1（2026-09-21 审查）：只比 size 字节会在「比对期间文件被追加」时给出
	// 假的一致结论（size 来自更早的 stat，追加的尾巴落在比较范围之外）。
	// 与预筛层共用 hasher 的同一份探测，不再各写一遍（I5）。
	for _, f := range []*os.File{a, b} {
		if gerr := hasher.RejectGrowthBeyond(f, size); gerr != nil {
			return false, gerr
		}
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

// collapseSameIdentity 折并确信指向同一物理文件的条目（硬链接 / 同 inode 多路径），
// 保留路径较短者，与阶段 1.5（候选 FileKey 去重）同口径：仅替换路径派生字段
// Path/Ext，ID/Key 不被丢弃项覆盖（下游按 ID 关联选中态/保留决策/预览，覆盖会让
// ID 与结果集错位——见阶段 1.5 的 C3 注释）。
//
// 仅当两侧 ids 均 Resolved 且 Dev+Ino 相同才折并；判据不含 ctime（chmod/xattr 会
// 推进 ctime 但文件未被替换）。任一侧未解析时无从判断 ⇒ 保持独立（**不**折并）：
// 避免在 FAT/exFAT 等不提供稳定文件索引的卷上把内容相同但物理独立的文件误并为一条
// （漏报，对删除工具比 reclaimable 虚高更危险）。因此这里用确信判据而非 fsid 的
// SameIdentity（后者任一侧未解析即返回 true，用于操作前复核的"存疑从同"方向，
// 拿来分组折并会制造漏报）。
//
// 这是阶段 1.5 ResolveKey 的权威兜底：Windows 独占锁 / 删除挂起等致 ResolveKey
// 标准 open 与 C4 兜底全失败、返回未解析 FileKey 时，同 inode 两路径会双双留在候选
// 并因内容逐字节相同落入同一全量哈希桶，形成假重复组（R-算法-1）。阶段 2 的句柄
// 身份 ids[]（fsid.FromFile）此时仍能确认同 Dev+Ino，据此折并即消除假组。
func collapseSameIdentity(entries []*model.FileEntry, ids []fsid.ID) []*model.FileEntry {
	seen := make(map[model.FileKey]*model.FileEntry, len(entries))
	out := entries[:0] // 原地压缩（写索引 ≤ 读索引），与阶段 1.5 / refilterUnique 同款
	for j, e := range entries {
		id := ids[j]
		if id.Resolved {
			k := model.FileKey{VolumeID: id.Dev, FileIndex: id.Ino, Resolved: true}
			if rep, dup := seen[k]; dup {
				if len(e.Path) < len(rep.Path) {
					rep.Path = e.Path
					rep.Ext = e.Ext
				}
				continue // 折并：不新增组成员
			}
			seen[k] = e
		}
		out = append(out, e)
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
