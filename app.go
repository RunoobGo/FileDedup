// app.go Wails 绑定服务：01 §3.2 API 契约实现（04 M2-T02）。
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/png"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"filededup/internal/cache"
	"filededup/internal/dedup"
	"filededup/internal/history"
	"filededup/internal/model"
	"filededup/internal/ops"
)

// AppVersion 当前版本（GetVersion 契约，04 附录 A）：与 wails.json productVersion、
// frontend/package.json version 统一口径，发布时三处同改。
const AppVersion = "0.4.0"

// ---------- 契约视图类型（01 §7.2） ----------

// FileView 结果文件视图。
type FileView struct {
	ID      uint64 `json:"id"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    uint64 `json:"size"`
	ModTime int64  `json:"mtime"`
	IsKeep  bool   `json:"isKeep"` // 保留建议（路径最短）
}

// GroupView 重复组视图。
type GroupView struct {
	GroupID     uint64     `json:"groupID"`
	Reclaimable uint64     `json:"reclaimable"`
	Size        uint64     `json:"size"`
	Files       []FileView `json:"files"`
}

// ResultQuery 结果查询参数（GetResultGroups）。
type ResultQuery struct {
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Sort     string `json:"sort"` // reclaimable(默认)/size/count
	Ext      string `json:"ext"`  // 扩展名筛选（含点，空=全部）
}

// PagedResult 分页结果（01 §7.2）。
type PagedResult struct {
	Total            uint64      `json:"total"`
	Page             int         `json:"page"`
	TotalReclaimable uint64      `json:"totalReclaimable"` // 全量口径（含未加载页，M4 审查修订）
	Groups           []GroupView `json:"groups"`
}

// sortKey 结果视图缓存键（Y3）：排序方式 + 归一化扩展名筛选。
type sortKey struct {
	sort string
	ext  string
}

// viewCacheEntry 缓存的有序组列表 + 生成时的结果集指纹。
// 指纹自校验：a.groups 发生任何结构性变更（新增/清理/替换）都会使旧缓存失效，
// 避免返回陈旧排序结果（显式失效之外的第二道防线）。
type viewCacheEntry struct {
	groups []*model.DuplicateGroup
	guard  uint64
}

// ScanSummary scan:done / scan:cancelled 事件载荷。
type ScanSummary struct {
	Groups      int    `json:"groups"`
	Reclaimable uint64 `json:"reclaimable"`
	FilesFailed int    `json:"filesFailed"`
	Elapsed     string `json:"elapsed"`
}

// PreviewData PreviewFile 返回（M2：text/hex/image-base64；M4 缩略图）。
type PreviewData struct {
	Kind     string `json:"kind"`    // text/hex/image/binary
	Content  string `json:"content"` // 文本 / HEX / 图片 base64
	MimeType string `json:"mimeType,omitempty"`
}

// Settings 设置（01 §7.2，后端单一事实源）。
type Settings struct {
	Threads        int           `json:"threads"`
	FiltersDefault model.Filters `json:"filtersDefault"`
	Theme          string        `json:"theme"` // light/dark/system
	Language       string        `json:"language"`
}

// App Wails 绑定结构。
type App struct {
	ctx  context.Context
	mu   sync.Mutex
	pipe *dedup.Pipeline

	groups  []*model.DuplicateGroup
	byID    map[uint64]*model.FileEntry
	keepIDs map[uint64]bool // 当前保留决策（S2）
	failed  []model.FailedItem
	lastEvs model.ProgressEvent

	// Y3：结果视图按 (sort, ext) 缓存有序组列表，避免每次翻页全量重排
	viewCache map[sortKey]viewCacheEntry

	opsRunning   bool               // 清理操作执行中（互斥：拒绝并发操作/保留策略，与 goroutine 写结果集互斥）
	scanInFlight bool               // 扫描 goroutine 在途（互斥新扫描：防旧任务收尾写结果集覆盖新任务）
	opsCancel    context.CancelFunc // 当前清理操作的取消函数（P2：可中止）
	taskSeq      atomic.Uint64      // 任务/操作序号（P3：taskID 唯一性）

	// H5：本会话内经原生目录选择器（SelectDirectory）明确授权过的目录集合
	// （Clean 后的绝对路径）。move 的 TargetDir 必须落在其内——此前该参数是
	// 前端任意字符串，绑定层可直接决定任意写位置。
	authDirs map[string]bool

	cfgDir string
	cch    *cache.Cache

	// v0.5.0 功能 3：扫描历史。hist 访问一律经 history.Store 自身锁，
	// 绝不与 a.mu 嵌套（快照 → 放锁 → 调 hist）。
	hist         *history.Store
	curHistID    int64 // 当前结果集对应的历史行 ID（0=无/未保存）
	resultsReady bool  // 结果集可用门槛：扫描完成或历史结果已恢复

	// emit 事件出口（默认 wruntime.EventsEmit）。
	// 可测性：包级 EventsEmit 要求 Wails 前端注入的内部 context，
	// 单元测试里会直接 log.Fatalf 退出进程——StartScan / ExecuteOperation
	// 的 goroutine 收尾路径因此完全无法覆盖，P0-1、P1-1 正是这样漏网的。
	// 这里留一个窄接缝，测试替换后即可断言终止事件。
	emit func(ctx context.Context, event string, data ...interface{})
}

// NewApp 创建绑定服务。
func NewApp() *App {
	a := &App{
		pipe:      dedup.New(),
		byID:      make(map[uint64]*model.FileEntry),
		viewCache: make(map[sortKey]viewCacheEntry),
		authDirs:  make(map[string]bool),
		emit:      wruntime.EventsEmit,
	}
	// 引擎回调 → 事件桥（M2-T03）
	a.pipe.OnProgress = func(ev model.ProgressEvent) {
		a.mu.Lock()
		a.lastEvs = ev
		a.mu.Unlock()
		a.emit(a.ctx, "scan:progress", ev)
	}
	a.pipe.OnStage = func(ev model.StageEvent) {
		a.emit(a.ctx, "scan:stage", ev)
	}
	return a
}

// startup Wails 生命周期。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if dir, err := os.UserConfigDir(); err == nil {
		a.cfgDir = filepath.Join(dir, "FileDedup")
	} else if home, herr := os.UserHomeDir(); herr == nil {
		// 极端环境回退：配置目录不可用时退到家目录，避免静默落到（通常只读的）CWD
		a.cfgDir = filepath.Join(home, ".filededup")
	}
	if a.cfgDir != "" {
		_ = os.MkdirAll(a.cfgDir, 0o755)
	}
	// M4：哈希缓存（损坏自愈；失败不阻塞应用）
	if a.cfgDir != "" {
		dbPath := filepath.Join(a.cfgDir, "cache.db")
		if cch, err := cache.Open(dbPath); err == nil {
			a.cch = cch
			a.pipe = a.pipe.WithCache(cch)
		} else {
			// 缓存不可用只影响二次扫描速度（功能不受损），但静默吞掉会让"为何每次都要重新
			// 哈希"无从排查——这里显式留痕。不去重、不降级退出，只在启动时说一次。
			fmt.Fprintf(os.Stderr, "[cache] 哈希缓存不可用，本次运行将全量重算: %v (path=%s)\n", err, dbPath)
		}
	}
	// v0.5.0：扫描历史/清理账本（独立 history.db，损坏自愈；失败不阻塞应用）
	if a.cfgDir != "" {
		histPath := filepath.Join(a.cfgDir, "history.db")
		if hs, err := history.Open(histPath); err == nil {
			a.hist = hs
		} else {
			// 历史不可用只失去"历史恢复/回撤"能力，扫描与清理不受影响；同 cache 留痕风格。
			fmt.Fprintf(os.Stderr, "[history] 历史库不可用，本次运行不保存历史与清理记录: %v (path=%s)\n", err, histPath)
		}
	}
	a.emit(a.ctx, "app:ready", AppVersion)
}

// shutdown Wails 生命周期：释放缓存与历史库句柄。
func (a *App) shutdown(ctx context.Context) {
	if a.cch != nil {
		_ = a.cch.Close()
		a.cch = nil
	}
	if a.hist != nil {
		_ = a.hist.Close()
		a.hist = nil
	}
}

// ---------- 生命周期（§5.5 语义） ----------

// SelectDirectory 调起系统目录选择器。
// 注：Wails v2 目录选择为单选（平台对话框限制），前端可连续多次添加。
// H5：选择结果同时登记为本会话授权目录（move 目标的白名单来源）。
func (a *App) SelectDirectory() (string, error) {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择目录",
	})
	if err == nil && dir != "" {
		a.authorizeDir(dir)
	}
	return dir, err
}

// authorizeDir 登记授权目录（Clean 绝对路径；符号链接解析变体一并登记）。
func (a *App) authorizeDir(dir string) {
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authDirs == nil {
		a.authDirs = make(map[string]bool)
	}
	a.authDirs[abs] = true
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		a.authDirs[filepath.Clean(ev)] = true
	}
}

// moveTargetAllowed 校验 move 目标：必须位于某个授权目录内（含授权目录本身）。
// 对已存在的路径同时校验符号链接解析后的形态，防经由链接逃逸。
// 调用方须持 a.mu。
func (a *App) moveTargetAllowed(dir string) bool {
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	candidates := []string{abs}
	if ev, err := filepath.EvalSymlinks(abs); err == nil && ev != abs {
		candidates = append(candidates, filepath.Clean(ev))
	}
	for auth := range a.authDirs {
		for _, c := range candidates {
			if withinDir(c, auth) {
				return true
			}
		}
	}
	return false
}

// withinDir target 是否等于 dir 或位于 dir 之下（按路径段边界比较）。
func withinDir(target, dir string) bool {
	if target == dir {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(dir, sep) {
		dir += sep
	}
	return strings.HasPrefix(target, dir)
}

// StartScan 创建扫描任务，返回 taskID；运行中重复调用返回错误。
func (a *App) StartScan(cfg model.ScanConfig) (string, error) {
	if len(cfg.Roots) == 0 {
		return "", fmt.Errorf("请至少添加一个扫描目录")
	}
	if s := a.pipe.Status(); s != model.StatusIdle && s != model.StatusDone &&
		s != model.StatusCancelled && s != model.StatusFailed {
		return "", fmt.Errorf("任务进行中（%s），请先暂停或取消", s)
	}
	// 在途互斥：pipe 状态与 goroutine 收尾之间存在窗口（如 Run 已置 Done 但
	// 结果集未写完），此时放行新扫描会让旧 goroutine 的陈旧结果覆盖新任务
	a.mu.Lock()
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("上一个扫描任务尚未收尾，请稍候")
	}
	// P1-1：清理操作 goroutine 在途时禁止开新扫描。ExecuteOperation 在锁外捕获
	// groups 快照、执行完在锁内回写 a.groups；若此间开新扫描并清空结果集，
	// 旧操作的收尾会用「基于旧结果集清理后的列表」整个覆盖新扫描结果，
	// 新结果静默丢失且无任何提示。
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("清理操作执行中，请等待完成后再扫描")
	}
	a.scanInFlight = true
	a.groups = nil
	a.byID = make(map[uint64]*model.FileEntry)
	a.keepIDs = nil
	a.failed = nil
	a.resultsReady = false // 旧结果集作废（历史恢复/上次扫描均不再可操作）
	a.curHistID = 0
	a.invalidateViewCacheLocked() // Y3：新任务清空旧视图
	a.mu.Unlock()

	// P3：时间戳秒级精度不是唯一 ID——同一秒内重扫（取消后立刻重试很常见）会
	// 产生重复 taskID，前端若据此关联事件就会串台。追加原子序号保证单调唯一。
	taskID := fmt.Sprintf("scan-%s-%d", time.Now().Format("20060102-150405"), a.taskSeq.Add(1))
	a.goTask("scan", func() {
		a.mu.Lock()
		a.scanInFlight = false
		a.mu.Unlock()
	}, func() {
		start := time.Now()
		groups, failed, err := a.pipe.Run(a.ctx, cfg)
		elapsed := time.Since(start)
		// 复位必须先于终止事件发出：前端（与单测）收到 done/cancelled/error 后
		// 可能立刻重扫，若此刻在途标志仍为 true 会被误拒。goTask 的 reset
		// 保留为 panic 展开路径的兜底（其 LIFO 顺序本就先于 recover 发事件）。
		resetInFlight := func() {
			a.mu.Lock()
			a.scanInFlight = false
			a.mu.Unlock()
		}
		if err != nil {
			if a.pipe.Status() == model.StatusCancelled {
				resetInFlight()
				a.emit(a.ctx, "scan:cancelled", ScanSummary{Elapsed: elapsed.String()})
			} else {
				resetInFlight()
				a.emit(a.ctx, "scan:error", map[string]string{"error": err.Error()})
			}
			return
		}
		a.mu.Lock()
		a.groups = groups
		a.failed = failed
		a.invalidateViewCacheLocked() // Y3：新结果集使旧排序缓存失效
		for _, g := range groups {
			for _, f := range g.Files {
				a.byID[f.ID] = f
			}
		}
		var reclaim uint64
		for _, g := range groups {
			reclaim += g.Reclaimable
		}
		a.scanInFlight = false
		a.mu.Unlock()
		// v0.5.0 功能 3：扫描成功收尾自动写历史（锁外调用，见 App.hist 注释）。
		// 保存失败不影响结果集可用，只失去本次记录的恢复/裁剪联动。
		var histID int64
		if a.hist != nil {
			id, serr := a.hist.SaveScan(cfg, groups, failed)
			if serr != nil {
				fmt.Fprintf(os.Stderr, "[history] 扫描历史保存失败: %v\n", serr)
				a.emit(a.ctx, "app:error", map[string]string{"error": "历史保存失败（不影响当前结果）：" + serr.Error()})
			} else {
				histID = id
			}
		}
		a.mu.Lock()
		a.resultsReady = true
		a.curHistID = histID
		a.mu.Unlock()
		a.emit(a.ctx, "scan:done", ScanSummary{
			Groups:      len(groups),
			Reclaimable: reclaim,
			FilesFailed: len(failed),
			Elapsed:     elapsed.String(),
		})
	})
	return taskID, nil
}

// PauseScan 暂停（运行态生效）。P3：无任务时返回错误，前端据此提示而非误显示"已暂停"。
func (a *App) PauseScan() error { return a.pipe.Pause() }

// ResumeScan 恢复。
func (a *App) ResumeScan() error { return a.pipe.Resume() }

// CancelOperation 取消进行中的清理操作（P2）。
// 语义：停止派发后续条目，已完成的部分保持完成（不可回滚），
// 未派发的条目计入 OpsResult.Cancelled 且不会从结果集中移除。
// 没有这个出口时，一次卡住的网络卷/回收站调用会让 opsRunning 永不复位，
// 之后所有清理与保留策略都返回「操作执行中」，只能重启应用。
func (a *App) CancelOperation() error {
	a.mu.Lock()
	cancel := a.opsCancel
	a.mu.Unlock()
	if cancel == nil {
		return fmt.Errorf("当前没有进行中的清理操作")
	}
	cancel()
	return nil
}

// CancelScan 取消。
func (a *App) CancelScan() error { return a.pipe.Cancel() }

// goTask 启动绑定层后台任务（P3）：统一挂上 panic 兜底与在途标志复位。
// defer 为 LIFO：先注册 recover（最外层兜底）、后注册 reset，
// 因此 panic 展开时先复位在途标志、再吞掉 panic 发错误事件——
// 前端收到 error 时应用已确定处于"空闲"，不会再看到"扫描中"的残影。
func (a *App) goTask(kind string, reset, body func()) {
	go func() {
		defer a.recoverGoroutine(kind)
		defer reset()
		body()
	}()
}

// recoverGoroutine P3：绑定层 goroutine 的兜底 panic 守卫。
//
// 扫描/清理跑在独立 goroutine 里，任何 panic（越界、nil map、第三方解码器
// 遇到畸形文件）都会直接带走整个进程——用户看到的现象是"点了扫描，软件消失"，
// 而且未清理的在途标志会让重启前的会话一直显示"扫描中"。
// 这里吞掉 panic 但把堆栈写到 stderr 留痕，并向前端发一条错误事件；
// 状态复位交由 goTask 中先前注册的 reset 完成。
func (a *App) recoverGoroutine(kind string) {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "[panic] %s goroutine 已恢复: %v\n%s\n", kind, r, debug.Stack())
		if kind != "ops" {
			a.pipe.Abort() // 状态机停在运行态 → 之后所有扫描都会被"任务进行中"拒绝
		}
		msg := map[string]string{"error": fmt.Sprintf("%s goroutine panic: %v", kind, r)}
		if a.emit != nil && a.ctx != nil {
			if kind == "ops" {
				a.emit(a.ctx, "ops:error", msg)
			} else {
				a.emit(a.ctx, "scan:error", msg)
			}
		}
	}
}

// GetScanProgress 主动拉取当前进度（断线重连语义）。
func (a *App) GetScanProgress() model.ProgressEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastEvs
}

// GetStatus 当前任务状态字符串。
func (a *App) GetStatus() string { return string(a.pipe.Status()) }

// ---------- 结果查询 ----------

// GetResultGroups 分页查询重复组（M2-T07 排序/筛选；Y3 排序缓存）。
func (a *App) GetResultGroups(q ResultQuery) (PagedResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if q.PageSize <= 0 {
		q.PageSize = 100
	} else if q.PageSize > 500 {
		q.PageSize = 500 // 超限钳到上限（而非重置默认，语义更直观）
	}
	if q.Page < 0 {
		q.Page = 0
	}
	// Y3：筛选 + 排序结果按 (sort, ext) 复用。修正前每次翻页都全量重排
	// （O(n log n)）并重扫扩展名（O(组×文件)），且全程持锁阻塞进度事件。
	key := sortKey{sort: q.Sort, ext: normalizeExt(q.Ext)}
	guard := resultGuard(a.groups)
	entry, ok := a.viewCache[key]
	if !ok || entry.guard != guard {
		entry = viewCacheEntry{groups: a.buildSortedGroupsLocked(key), guard: guard}
		a.viewCache[key] = entry
	}
	gs := entry.groups

	total := uint64(len(gs))
	// 全量可释放空间（含未加载页）：统计条与 totalGroups 同口径
	var totalReclaim uint64
	for _, g := range gs {
		totalReclaim += g.Reclaimable
	}
	start := q.Page * q.PageSize
	if start >= len(gs) {
		return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, Groups: []GroupView{}}, nil
	}
	end := start + q.PageSize
	if end > len(gs) {
		end = len(gs)
	}
	views := make([]GroupView, 0, end-start)
	for _, g := range gs[start:end] {
		views = append(views, toGroupView(g, a.keepIDs))
	}
	return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, Groups: views}, nil
}

// buildSortedGroupsLocked 执行一次扩展名筛选 + 排序（调用方须持 a.mu）。
// 返回切片只读复用：调用方不得原地修改其元素顺序。
func (a *App) buildSortedGroupsLocked(key sortKey) []*model.DuplicateGroup {
	var gs []*model.DuplicateGroup
	for _, g := range a.groups {
		if key.ext != "" && !groupHasExt(g, key.ext) {
			continue
		}
		gs = append(gs, g)
	}
	// 排序（均为降序；稳定：组 ID 兜底）
	switch key.sort {
	case "size":
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].Files[0].Size != gs[j].Files[0].Size {
				return gs[i].Files[0].Size > gs[j].Files[0].Size
			}
			return gs[i].GroupID < gs[j].GroupID
		})
	case "count":
		sort.Slice(gs, func(i, j int) bool {
			if len(gs[i].Files) != len(gs[j].Files) {
				return len(gs[i].Files) > len(gs[j].Files)
			}
			return gs[i].GroupID < gs[j].GroupID
		})
	default: // reclaimable
		sort.Slice(gs, func(i, j int) bool {
			if gs[i].Reclaimable != gs[j].Reclaimable {
				return gs[i].Reclaimable > gs[j].Reclaimable
			}
			return gs[i].GroupID < gs[j].GroupID
		})
	}
	return gs
}

// invalidateViewCacheLocked 结果集结构变更后清空视图缓存（调用方须持 a.mu）。
func (a *App) invalidateViewCacheLocked() {
	if a.viewCache != nil {
		clear(a.viewCache)
	}
}

// resultGuard 结果集指纹（Y3 缓存自校验）：长度 + 各组 ID/成员数。
// 组被新增、移除或成员变化都会改变指纹，使陈旧缓存自动失效。
func resultGuard(groups []*model.DuplicateGroup) uint64 {
	h := uint64(len(groups)) * 0x9E3779B97F4A7C15
	for _, g := range groups {
		h ^= g.GroupID*0xD6E8FEB86659FD93 + uint64(len(g.Files))*0x9E3779B97F4A7C15
	}
	return h
}

// normalizeExt 归一化扩展名筛选（小写、去空白、补前导点）。
func normalizeExt(ext string) string {
	e := strings.ToLower(strings.TrimSpace(ext))
	if e != "" && !strings.HasPrefix(e, ".") {
		e = "." + e
	}
	return e
}

// groupHasExt 组内是否存在该扩展名（ext 须已由 normalizeExt 归一化）。
func groupHasExt(g *model.DuplicateGroup, ext string) bool {
	for _, f := range g.Files {
		if strings.EqualFold(f.Ext, ext) {
			return true
		}
	}
	return false
}

// toGroupView 转换为 UI 视图：保留标记 = 当前决策（若有）否则路径最短建议（01 §9）。
func toGroupView(g *model.DuplicateGroup, keepIDs map[uint64]bool) GroupView {
	v := GroupView{
		GroupID:     g.GroupID,
		Reclaimable: g.Reclaimable,
		Size:        g.Files[0].Size,
		Files:       make([]FileView, 0, len(g.Files)),
	}
	keep := -1
	for i, f := range g.Files {
		if keepIDs != nil && keepIDs[f.ID] {
			keep = i
			break
		}
	}
	if keep < 0 {
		keep = 0
		for i, f := range g.Files {
			if len(f.Path) < len(g.Files[keep].Path) {
				keep = i
			}
		}
	}
	for i, f := range g.Files {
		v.Files = append(v.Files, FileView{
			ID:      f.ID,
			Path:    f.Path,
			Name:    filepath.Base(f.Path),
			Size:    f.Size,
			ModTime: f.ModTime,
			IsKeep:  i == keep,
		})
	}
	return v
}

// GetFailedItems 失败清单（扫描失败；操作失败随 M3 并入）。
func (a *App) GetFailedItems() []model.FailedItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed == nil {
		return []model.FailedItem{}
	}
	return a.failed
}

// ---------- 预览与定位 ----------

// imageMime 可缩略图预览的位图扩展名。
// P3：表内每一项都必须在下方有对应解码器，否则 PreviewFile 会走"图片解码失败"
// 分支——修正前 .webp/.bmp/.svg 都在表里却未注册解码器（标准库只有 jpeg/png/gif），
// 用户看到的是无信息量的失败提示。
//   - .svg 是文本矢量格式，不该进位图预览表：移除后自然落到文本分支显示源码。
//   - .webp/.bmp 通过 golang.org/x/image 注册真正的解码器。
var imageMime = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
}

// PreviewFile 预览：图片（≤256KB base64）/ 文本（前 4KB）/ HEX（前 256B）。
func (a *App) PreviewFile(id uint64) (PreviewData, error) {
	a.mu.Lock()
	e, ok := a.byID[id]
	a.mu.Unlock()
	if !ok {
		return PreviewData{}, fmt.Errorf("文件不存在或已过期（id=%d）", id)
	}
	f, err := os.Open(e.Path)
	if err != nil {
		return PreviewData{}, err
	}
	defer f.Close()

	if _, isImg := imageMime[strings.ToLower(e.Ext)]; isImg {
		// M4-T04：任意大小图片 → 缩略图（512px JPEG；超大文件防炸限制）。
		// Y5：源文件上限由 50MB 收紧到 20MB——预览峰值内存 ≈ 源体积 + 解码位图，
		// 20MB 已覆盖绝大多数照片/截图场景，显著降低单次预览的内存尖峰。
		const srcLimit = 20 << 20
		if e.Size > srcLimit {
			return PreviewData{Kind: "binary", Content: "图片超过 20MB，暂不支持预览"}, nil
		}
		b := make([]byte, e.Size)
		if _, err := io.ReadFull(f, b); err != nil {
			return PreviewData{}, err
		}
		thumb, err := thumbnail(b, 512)
		if err != nil {
			return PreviewData{Kind: "binary", Content: "图片解码失败: " + err.Error()}, nil
		}
		return PreviewData{Kind: "image", Content: base64.StdEncoding.EncodeToString(thumb), MimeType: "image/jpeg"}, nil
	}
	// 文本 / HEX
	const textLimit = 4 << 10
	buf := make([]byte, min64(e.Size, textLimit))
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return PreviewData{}, err // 文件短于记录 size：用已读部分
	}
	b := buf[:n]
	if isLikelyText(b) {
		return PreviewData{Kind: "text", Content: string(b)}, nil
	}
	const hexLimit = 256
	m := n
	if m > hexLimit {
		m = hexLimit
	}
	return PreviewData{Kind: "hex", Content: hexDump(b[:m])}, nil
}

func isLikelyText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	printable := 0
	for _, c := range b {
		if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7F) || c >= 0x80 {
			printable++
		}
	}
	return float64(printable)/float64(len(b)) > 0.85
}

func hexDump(b []byte) string {
	var sb strings.Builder
	for i := 0; i < len(b); i += 16 {
		end := i + 16
		if end > len(b) {
			end = len(b)
		}
		row := b[i:end]
		sb.WriteString(fmt.Sprintf("%08x  ", i))
		for j := 0; j < 16; j++ {
			if j < len(row) {
				sb.WriteString(fmt.Sprintf("%02x ", row[j]))
			} else {
				sb.WriteString("   ")
			}
		}
		sb.WriteString(" |")
		for _, c := range row {
			if c >= 0x20 && c < 0x7F {
				sb.WriteByte(c)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}

// RevealInFolder 平台"打开所在文件夹并选中"。
func (a *App) RevealInFolder(id uint64) error {
	a.mu.Lock()
	e, ok := a.byID[id]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("文件不存在或已过期（id=%d）", id)
	}
	cmd, err := revealCmd(e.Path)
	if err != nil {
		return err
	}
	return startCmd(cmd)
}

// revealCmd 组装"定位并选中"命令。
//
// P3：Linux 分支此前只有 xdg-open 目录 = 只打开父目录不选中，用户在几千个
// 同名/相似文件里仍要自己找；而且 xdg-open 不存在时 exec 才报错，用户看不出
// 是"没装支持的工具"还是"操作失败"。这里按 DE 探测带 --select 的命令，
// 全部缺失时明确报错。darwin/windows 行为不变。
func revealCmd(path string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path), nil
	case "windows":
		return exec.Command("explorer", "/select,", path), nil
	}
	dir := filepath.Dir(path)
	// 顺序即优先级：能选中文件 > 只能打开目录
	for _, c := range []struct {
		exe  string
		args []string
	}{
		{"nautilus", []string{"--select", path}},
		{"dolphin", []string{"--select", path}},
		{"thunar", []string{path}},
		{"nemo", []string{"--select", path}},
		{"pcmanfm", []string{"--select", dir}},
		{"gio", []string{"open", filepath.Dir(path)}},
		{"xdg-open", []string{dir}},
	} {
		if _, err := exec.LookPath(c.exe); err == nil {
			return exec.Command(c.exe, c.args...), nil
		}
	}
	return nil, fmt.Errorf("未找到可用的文件管理器命令（nautilus/dolphin/thunar/xdg-open），无法定位文件")
}

// startCmd 启动外部定位命令并异步回收：
// Start 后不 Wait 会累积僵尸进程，Wait 同步阻塞调用方，故后台回收。
func startCmd(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ---------- 设置（单一事实源，01 §7.3） ----------

func (a *App) settingsPath() string { return filepath.Join(a.cfgDir, "settings.json") }

// GetSettings 读取设置。
func (a *App) GetSettings() Settings {
	s := defaultSettings()
	if b, err := os.ReadFile(a.settingsPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// SaveSettings 保存设置到 settings.json。
func (a *App) SaveSettings(s Settings) (Settings, error) {
	// P2：与引擎同一口径钳制（0 = 自动，1..dedup.MaxThreads = 显式指定）。
	// 引擎侧也会钳制，这里钳是为了落盘值与实际生效值一致，界面上不自相矛盾。
	if s.Threads < 0 {
		s.Threads = 0
	} else if s.Threads > dedup.MaxThreads {
		s.Threads = dedup.MaxThreads
	}
	if s.Theme != "light" && s.Theme != "dark" && s.Theme != "system" {
		s.Theme = "system"
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return s, err
	}
	if err := os.WriteFile(a.settingsPath(), b, 0o644); err != nil {
		return s, err
	}
	return s, nil
}

func defaultSettings() Settings {
	return Settings{Threads: 0, Theme: "system", Language: "zh"}
}

// ---------- 版本与存根（M3/M4/M5 范围） ----------

// GetVersion 当前版本。
func (a *App) GetVersion() string { return AppVersion }

// ApplyKeepPolicy 保留策略引擎（M3-T01）：返回决策并记录 keepIDs（S2 保护依据）。
// 遍历阶段全程持锁（须与操作 goroutine 的结果集清理写互斥）；
// 历史持久化放到放锁之后（hist 自有锁，禁止与 a.mu 嵌套）。
func (a *App) ApplyKeepPolicy(policy model.KeepPolicy) ([]ops.KeepDecision, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return nil, fmt.Errorf("清理操作执行中，请稍后再应用保留策略")
	}
	if len(a.groups) == 0 {
		a.mu.Unlock()
		return nil, fmt.Errorf("暂无结果集")
	}
	if policy.Kind == "directory" && !ops.HasUsableDir(policy.Directories) {
		a.mu.Unlock()
		return nil, fmt.Errorf("请至少添加一个保留目录")
	}
	decisions := ops.ApplyKeepPolicy(a.groups, policy)
	a.keepIDs = make(map[uint64]bool, len(decisions))
	for _, d := range decisions {
		a.keepIDs[d.KeepID] = true
	}
	histID, paths := a.curHistID, a.keepPathsLocked()
	a.mu.Unlock()
	a.persistKeepPaths(histID, paths)
	return decisions, nil
}

// ClearKeepDecisions 清除保留决策（回到默认建议）。
func (a *App) ClearKeepDecisions() {
	a.mu.Lock()
	a.keepIDs = nil
	histID := a.curHistID
	a.mu.Unlock()
	a.persistKeepPaths(histID, nil)
}

// keepPathsLocked 当前保留决策对应的路径集合（须持 a.mu 调用）。
// 跨会话只有路径稳定——历史恢复后按 path→rowid 重建 keepIDs。
func (a *App) keepPathsLocked() []string {
	var out []string
	if len(a.keepIDs) == 0 {
		return out
	}
	for _, g := range a.groups {
		for _, f := range g.Files {
			if a.keepIDs[f.ID] {
				out = append(out, f.Path)
			}
		}
	}
	return out
}

// persistKeepPaths 把保留决策同步进当前历史行（锁外调用；无历史联动则跳过）。
func (a *App) persistKeepPaths(histID int64, paths []string) {
	if a.hist == nil || histID == 0 {
		return
	}
	if err := a.hist.UpdateKeepPaths(histID, paths); err != nil {
		fmt.Fprintf(os.Stderr, "[history] 保留决策保存失败: %v\n", err)
	}
}

// ---------- v0.5.0 功能 3：扫描历史绑定 ----------

// HistoryMeta 扫描历史列表项（字段为 history.ScanMeta 的对外投影，
// 不含 failed/keepPaths——恢复时经 LoadScanHistory 全量读取）。
type HistoryMeta struct {
	ID          int64         `json:"id"`
	SavedAt     int64         `json:"savedAt"` // Unix 秒
	Roots       []string      `json:"roots"`
	Filters     model.Filters `json:"filters"`
	Threads     int           `json:"threads"`
	Paranoid    bool          `json:"paranoid"`
	Groups      int           `json:"groups"`
	Files       int           `json:"files"`
	OrigFiles   int           `json:"origFiles"` // 保存时文件数（files 与之差异=已清理量）
	Reclaimable uint64        `json:"reclaimable"`
}

// ListScanHistory 历史列表（新→旧）。
func (a *App) ListScanHistory() ([]HistoryMeta, error) {
	if a.hist == nil {
		return nil, fmt.Errorf("历史库不可用")
	}
	ms, err := a.hist.ListScans()
	if err != nil {
		return nil, err
	}
	out := make([]HistoryMeta, 0, len(ms))
	for _, m := range ms {
		out = append(out, HistoryMeta{
			ID: m.ID, SavedAt: m.SavedAt, Roots: m.Roots, Filters: m.Filters,
			Threads: m.Threads, Paranoid: m.Paranoid,
			Groups: m.Groups, Files: m.Files, OrigFiles: m.OrigFiles,
			Reclaimable: m.Reclaimable,
		})
	}
	return out, nil
}

// LoadScanHistory 恢复一条历史为当前结果集（可直接继续清理）。
// 陈旧文件安全性由操作前的逐文件校验兜底（S1 篡改拦截 / S8 消失即跳过），
// 载入时不做文件系统遍历。
func (a *App) LoadScanHistory(id int64) (ScanSummary, error) {
	if a.hist == nil {
		return ScanSummary{}, fmt.Errorf("历史库不可用")
	}
	a.mu.Lock()
	busy := a.opsRunning || a.scanInFlight
	a.mu.Unlock()
	if busy {
		return ScanSummary{}, fmt.Errorf("扫描/清理进行中，请稍后再打开历史")
	}

	meta, groups, err := a.hist.LoadScan(id)
	if err != nil {
		return ScanSummary{}, err
	}
	byID := make(map[uint64]*model.FileEntry, meta.Files)
	var reclaim uint64
	for _, g := range groups {
		reclaim += g.Reclaimable
		for _, f := range g.Files {
			byID[f.ID] = f
		}
	}
	// 保留决策持久化的是路径（功能 1 语义），恢复时映射回本次载入的行 ID
	keepSet := make(map[string]bool, len(meta.KeepPaths))
	for _, p := range meta.KeepPaths {
		keepSet[p] = true
	}
	var keepIDs map[uint64]bool
	for _, g := range groups {
		for _, f := range g.Files {
			if keepSet[f.Path] {
				if keepIDs == nil {
					keepIDs = make(map[uint64]bool)
				}
				keepIDs[f.ID] = true
			}
		}
	}

	a.mu.Lock()
	// 二次确认：读库期间可能有任务抢占（与 StartScan 的 check-and-set 同锁串行）
	if a.opsRunning || a.scanInFlight {
		a.mu.Unlock()
		return ScanSummary{}, fmt.Errorf("任务已开始，历史未载入")
	}
	a.groups = groups
	a.byID = byID
	a.keepIDs = keepIDs
	a.failed = meta.Failed
	a.invalidateViewCacheLocked()
	a.curHistID = id
	a.resultsReady = true
	a.mu.Unlock()

	return ScanSummary{
		Groups: len(groups), Reclaimable: reclaim, FilesFailed: len(meta.Failed),
	}, nil
}

// DeleteScanHistory 删除一条历史；若正是当前结果集的来源，仅断开联动
// （内存结果仍可看可清，只是后续裁剪不再回写）。
func (a *App) DeleteScanHistory(id int64) error {
	if a.hist == nil {
		return fmt.Errorf("历史库不可用")
	}
	if err := a.hist.DeleteScan(id); err != nil {
		return err
	}
	a.mu.Lock()
	if a.curHistID == id {
		a.curHistID = 0
	}
	a.mu.Unlock()
	return nil
}

// ClearScanHistory 清空全部扫描历史（当前结果集仅断开联动）。
func (a *App) ClearScanHistory() error {
	if a.hist == nil {
		return fmt.Errorf("历史库不可用")
	}
	if err := a.hist.ClearScans(); err != nil {
		return err
	}
	a.mu.Lock()
	a.curHistID = 0
	a.mu.Unlock()
	return nil
}

// ExecuteOperation 操作执行器（M3-T02~T06）：
// 校验 → 执行（回收站/永久删除/移动/硬链接）→ 事件反馈 → 结果集清理。
// 互斥：执行期间拒绝再次操作与再次扫描（两组 goroutine 并发写结果集会互相覆盖）。
func (a *App) ExecuteOperation(op model.OpRequest) (string, error) {
	// P1-1：快照读取与 opsRunning 置位在同一个临界区内完成。
	// 修正前分了两段（读快照 → 释放锁 → 查状态 → 再取锁双检置位），
	// 中间窗口里新扫描可以启动并清空结果集，本操作的 goroutine 随后
	// 会用「基于陈旧结果集清理后的列表」覆盖新扫描结果 → 新结果静默丢失。
	// StartScan 同样在锁内检查 opsRunning，二者互斥因此是双向闭合的。
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("上一个清理操作仍在执行中")
	}
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("扫描进行中，请等待结束后再执行清理")
	}
	// v0.5.0：门槛由「pipe 状态 Done」改为「结果集就绪」。历史恢复出的结果集
	// 引擎处于 Idle，旧判定会永久拒绝清理；resultsReady 在扫描完成/历史载入
	// 时置真，StartScan 入口置假，语义与旧判定在扫描路径上完全等价。
	if !a.resultsReady {
		a.mu.Unlock()
		return "", fmt.Errorf("暂无可操作的结果集（请先完成扫描或打开历史记录）")
	}
	if len(a.groups) == 0 {
		a.mu.Unlock()
		return "", fmt.Errorf("暂无结果集")
	}
	// H5：move 的目标是前端传入的字符串——只允许落在本会话经原生对话框
	// 选择过的目录（或其子目录）内，堵住"绑定层写任意路径"的越权面。
	if op.Kind == "move" && !a.moveTargetAllowed(op.TargetDir) {
		a.mu.Unlock()
		return "", fmt.Errorf("移动目标无效或未经选择目录对话框授权，请重新选择目标目录")
	}
	groups := a.groups
	keepIDs := a.keepIDs
	failed := a.failed
	hs := a.hist // 锁内快照：goroutine 内不得再解引用可变字段
	histID := a.curHistID
	a.opsRunning = true // 置位后新扫描会被拒（StartScan 检 opsRunning）
	opCtx, cancelOp := context.WithCancel(context.Background())
	a.opsCancel = cancelOp
	a.mu.Unlock()

	opID := fmt.Sprintf("ops-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))

	a.goTask("ops", func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}, func() {
		// v0.5.0 功能 4：写前账本——任何文件系统动作之前落盘全部计划。
		// undoable 判定：delete 不可撤；Windows 回收站拿不到 src→dst 映射，
		// 回撤改由「打开系统回收站」引导；其余可撤。
		undoable := op.Kind != "delete" && !(op.Kind == "trash" && runtime.GOOS == "windows")
		var journalID int64
		if hs != nil {
			if plans := planOpItems(groups, keepIDs, op.FileIDs); len(plans) > 0 {
				jid, jerr := hs.BeginOp(op.Kind, op.TargetDir, histID, undoable, plans)
				if jerr != nil {
					fmt.Fprintf(os.Stderr, "[history] 操作账本写入失败: %v\n", jerr)
					a.emit(a.ctx, "app:error", map[string]string{
						"error": "操作账本写入失败，本次清理将无法在「记录」页回撤：" + jerr.Error()})
				} else {
					journalID = jid
				}
			}
		}
		res := ops.Execute(ops.Options{
			Ctx:     opCtx,
			Groups:  groups,
			KeepIDs: keepIDs,
			OnProgress: func(done, total int, current string) {
				a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: done, Total: total, Current: current})
			},
			// v0.5.0：逐条收口写前账本（OnItem 由执行器保证每个进入
			// 校验/执行阶段的文件恰好一次；取消未派发项留给 Finalize 归位）。
			OnItem: func(r ops.ItemResult) {
				if journalID == 0 {
					return
				}
				if err := hs.FinishItem(journalID, r.OrigPath, r.DestPath, r.LinkSrc, r.State, r.Err); err != nil {
					fmt.Fprintf(os.Stderr, "[history] 条目收口失败 %s: %v\n", r.OrigPath, err)
				}
			},
			// C7：worker 内 panic 兜底。runIndexed 已捕获 panic 并标记失败，
			// 此处把异常转成 ops:error 事件，使前端能复位 opsRunning（P2 死锁终防）。
			OnPanic: func(err error) {
				if a.emit != nil && a.ctx != nil {
					a.emit(a.ctx, "ops:error", map[string]string{"error": err.Error()})
				}
			},
		}, op)
		if journalID != 0 {
			// 残留 planned（取消未派发等）→ cancelled，并冻结 done 计数/回收字节
			if err := hs.FinalizeOp(journalID); err != nil {
				fmt.Fprintf(os.Stderr, "[history] 操作账本收尾失败: %v\n", err)
			}
		}
		// 失败/跳过并入统一失败清单
		a.mu.Lock()
		a.failed = append(append([]model.FailedItem{}, failed...), res.Failed...)
		// 结果集清理：OK/Skipped 的文件移出组，组 <2 则移除组
		// （OK 记录源路径：move 成功后源路径即结果集应清理的条目）
		gone := make(map[string]bool, len(res.OK)+len(res.Skipped))
		for _, p := range append(append([]string{}, res.OK...), res.Skipped...) {
			gone[p] = true
		}
		var kept []*model.DuplicateGroup
		for _, g := range groups {
			files := g.Files[:0]
			for _, f := range g.Files {
				if gone[f.Path] {
					delete(a.byID, f.ID)
				} else {
					files = append(files, f)
				}
			}
			if len(files) >= 2 {
				g.Files = files
				g.Reclaimable = (uint64(len(files)) - 1) * files[0].Size
				kept = append(kept, g)
			}
		}
		a.groups = kept
		a.invalidateViewCacheLocked() // Y3：清理后组结构变化，排序缓存失效
		a.mu.Unlock()
		// v0.5.0：历史联动裁剪（锁外）。gone 为本 goroutine 私有 map，
		// 解锁后无并发写；历史行被删（curHistID 已清零）时 Prune 报"不存在"，
		// 属可忽略的陈旧关联，留痕即可。
		if hs != nil && histID != 0 && len(gone) > 0 {
			if perr := hs.PruneScanFiles(histID, gone); perr != nil {
				fmt.Fprintf(os.Stderr, "[history] 历史裁剪失败: %v\n", perr)
			}
		}
		a.emit(a.ctx, "ops:done", res)
	})
	return opID, nil
}

// planOpItems 从结果集快照构造写前计划：仅收录「在结果集内且未被标为保留」
// 的文件（保留项会被 S2 拒绝、从不触及文件系统，不入账；结果集外 id 无哈希，
// 由执行器直接记错）。按 op.FileIDs 顺序去重。
func planOpItems(groups []*model.DuplicateGroup, keepIDs map[uint64]bool, ids []uint64) []history.OpItemPlan {
	type ent struct {
		e    *model.FileEntry
		hash [32]byte
	}
	lookup := make(map[uint64]ent)
	for _, g := range groups {
		for _, f := range g.Files {
			lookup[f.ID] = ent{f, g.Hash}
		}
	}
	seen := make(map[uint64]bool, len(ids))
	plans := make([]history.OpItemPlan, 0, len(ids))
	for _, id := range ids {
		en, ok := lookup[id]
		if !ok || seen[id] || (keepIDs != nil && keepIDs[id]) {
			continue
		}
		seen[id] = true
		plans = append(plans, history.OpItemPlan{
			OrigPath: en.e.Path, Hash: en.hash, Size: en.e.Size, MtimeNs: en.e.ModTime,
		})
	}
	return plans
}

// OpenTrash 打开系统回收站（M3-T06：恢复引导）。
func (a *App) OpenTrash() error {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return startCmd(exec.Command("open", filepath.Join(home, ".Trash")))
	case "windows":
		return startCmd(exec.Command("explorer", "shell:RecycleBinFolder"))
	default:
		root := os.Getenv("XDG_DATA_HOME")
		if root == "" {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, ".local", "share")
		}
		return startCmd(exec.Command("xdg-open", filepath.Join(root, "Trash", "files")))
	}
}

// ExportReport M5-T01 实现。
func (a *App) ExportReport(format, path string) (string, error) {
	return "", fmt.Errorf("报告导出将在 M5 提供")
}

// CacheStats 缓存统计（M4-T01）。
func (a *App) CacheStats() (cache.Stats, error) {
	if a.cch == nil {
		return cache.Stats{}, fmt.Errorf("缓存不可用")
	}
	return a.cch.GetStats()
}

// CacheClear 清空缓存（M4-T01）。
func (a *App) CacheClear() error {
	if a.cch == nil {
		return fmt.Errorf("缓存不可用")
	}
	return a.cch.Clear()
}

// thumbnail 生成缩略图（M4-T04）：解码（jpeg/png/gif 首帧）→ 最近邻缩放 → JPEG。
// 零第三方依赖（标准库 image）。
func thumbnail(data []byte, maxDim int) ([]byte, error) {
	// 解压炸弹防护：编码体积小 ≠ 解码位图小（小体积 PNG 可声明数十亿像素），
	// 先 DecodeConfig 预检像素总数再解码。
	// Y5：上限由 64M 像素收紧到 16M（≈64MB RGBA）——512px 缩略图不需要更高的
	// 中间精度，此前 64M 像素意味着单次预览最高约 256MB 位图常驻。
	const maxPixels = 16 << 20 // 16M 像素（解码后峰值约 64MB RGBA）
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, fmt.Errorf("图片尺寸过大（%dx%d），不支持预览", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("空图片")
	}
	nw, nh := w, h
	if w > h {
		if w > maxDim {
			nw, nh = maxDim, h*maxDim/w
		}
	} else if h > maxDim {
		nw, nh = w*maxDim/h, maxDim
	}
	// 极端长宽比（1×100000 之类）整数除法会把短边归零，
	// 产出 0 尺寸的"合法" JPEG——前端只显示空白，用户以为文件坏了。
	// 缩略图短边至少 1px。
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
