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
const AppVersion = "0.5.0"

// ---------- 契约视图类型（01 §7.2） ----------

// FileView 结果文件视图。
type FileView struct {
	ID      uint64 `json:"id"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    uint64 `json:"size"`
	ModTime int64  `json:"mtime"`
	IsKeep  bool   `json:"isKeep"` // 默认建议保留者（非隐藏优先，再路径最短）

	// Volume 所在卷标识（2026-09-20 新增，"symlink" 前置能力）。
	//
	// 为什么必须由后端给出，而不是让前端从路径里自己截：
	//   - 前端要判断"这个重复组是否跨卷"，才决定显不显示「软链接合并」按钮。
	//     硬链接跨卷必失败，软链接在同卷又只有净损失（见 ops/symlink.go 头注释），
	//     所以这个判定直接决定按钮该不该出现，判错等于把用户引向必然失败的路径。
	//   - Windows 上"卷"不等于"盘符"：一个盘符可能是一个卷，也可能是一个挂载点
	//     （如 C:\Mount\Data 指向另一个卷）。而且路径大小写、UNC
	//     （\\server\share）、以及 \\?\ 前缀都会让字符串比较得出一堆假阳性。
	//     只有后端有能力用平台 API（volumeRoot / fsid）给出对的答案。
	//
	// 取值：优先取 FileKey.VolumeID（扫描阶段解析出的卷序列号/设备号），
	// 未解析时退化为路径的卷根（filepath.VolumeName，unix 为 "/"）。
	// 前端只做相等比较，不解释其内容。
	Volume string `json:"volume"`

	// VolumeResolved 表示 Volume 是否来自**真实卷身份**（而非路径推断）。
	// false 时不参与跨卷判定（保守按"同卷"处理，不显示按钮）：
	// 与其猜错让用户点出一个注定失败的按钮，不如不显示。
	VolumeResolved bool `json:"volumeResolved"`
}

// GroupView 重复组视图。
type GroupView struct {
	GroupID     uint64     `json:"groupID"`
	Reclaimable uint64     `json:"reclaimable"`
	Size        uint64     `json:"size"`
	Files       []FileView `json:"files"`
}

// KeepOutcome 保留策略结果（2026-09-18 审查 I4）。
type KeepOutcome struct {
	Decisions []ops.KeepDecision `json:"decisions"`
	// UnmatchedGroups：directory 策略下没有任何成员命中保留目录的组数。
	// 这些组不会被标出保留者、不受 S2 保护，UI 必须明示。
	UnmatchedGroups int `json:"unmatchedGroups"`
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

	// resultGen 结果集代际号（2026-09-18 审查 C7）：StartScan 在锁内自增，
	// 扫描 goroutine 收尾前比对——不等即「本任务已被新扫描取代」，弃写。
	// scanInFlight 挡不住这一段：它在写回结果集时就已复位，而随后还有
	// SaveScan（锁外）→ resultsReady/curHistID → scan:done 三拍。
	resultGen atomic.Uint64

	// 2026-09-18 审查 C5：绑定层后台任务的生命周期。
	// wg 由 goTask 统一记账，shutdown 据此等在途 goroutine 收口后再关句柄；
	// quitPending 记录「用户已按关闭、我们已拦下并请求中止」的次数，
	// 让关闭按钮第一次按下是"中止并等待"，第二次才是"照办退出"。
	wg          sync.WaitGroup
	quitPending int
	forceExit   func(code int) // 可测接缝：单测里不能真把测试进程带走

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
		forceExit: os.Exit,
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
	// M4：哈希缓存（确证损坏时隔离重建；打开失败不阻塞应用）
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
	// v0.5.0：扫描历史/清理账本（独立 history.db，确证损坏时隔离重建）
	if a.cfgDir != "" {
		histPath := filepath.Join(a.cfgDir, "history.db")
		if hs, err := history.Open(histPath); err == nil {
			a.hist = hs
		} else {
			// 2026-09-18 审查 C3 之后这里不再只是"失去历史/回撤能力"：账本不可用时
			// 回收站/移动/硬链接会被拒绝执行（仅永久删除照常），见 beginJournal。
			// 留痕必须说清后果，否则用户只看到"清理报账本不可用"而不知所以然。
			fmt.Fprintf(os.Stderr, "[history] 历史库不可用：本次运行不保存历史，且回收站/移动/硬链接清理将被拒绝执行: %v (path=%s)\n", err, histPath)
		}
	}
	a.emit(a.ctx, "app:ready", AppVersion)
}

// beforeClose 挂到 options.OnBeforeClose：返回 true 表示「这一次先别关窗口」。
// 命名小写与 startup/shutdown 同列——生命周期钩子不该出现在前端绑定面里。
//
// 2026-09-18 审查 C5：扫描/清理跑在 goroutine 里，而关窗口即进程退出，原先
// 毫无拦截——移动被拦腰截断、写前账本停在 planned、句柄被在途 goroutine 继续
// 使用。交互语义：
//   - 第一次关闭：拦下，请求中止在途任务，发 app:quit-blocked 让前端提示；
//   - 第二次关闭（任务仍未收口）：认定用户就是要走，立即结束进程。
//
// 硬退出是可接受的而非理想：账本本来就是写前的，未落账条目留在 planned，
// 下次启动 history.Open 统一标为 interrupted，用户看得到、可追溯。
// 不无限等待是因为一次卡住的回收站/网络卷调用会让窗口永远关不掉。
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	if !a.opsRunning && !a.scanInFlight {
		a.quitPending = 0
		a.mu.Unlock()
		return false
	}
	running := "scan"
	if a.opsRunning {
		running = "ops"
	}
	a.quitPending++
	attempt := a.quitPending
	a.mu.Unlock()

	if attempt >= 2 {
		fmt.Fprintf(os.Stderr, "[app] 第二次关闭：在途任务仍未收口，立即退出（未完成条目由下次启动标记为中断）\n")
		a.forceExit(0)
		return true // forceExit 被测试替换时可达
	}
	a.cancelInFlight()
	msg := "清理进行中，已请求中止——待其停下后再点一次即可退出（中止后已完成的部分不会回滚）"
	if running == "scan" {
		msg = "扫描进行中，已请求中止——待其停下后再点一次即可退出（扫描不涉及文件改动）"
	}
	if a.emit != nil {
		a.emit(ctx, "app:quit-blocked", map[string]string{"message": msg, "running": running})
	}
	return true
}

// cancelInFlight 请求中止在途扫描与清理（幂等；不等待收口）。
func (a *App) cancelInFlight() {
	a.mu.Lock()
	cancel := a.opsCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = a.pipe.Cancel()
}

// shutdown Wails 生命周期：释放缓存与历史库句柄。
//
// 2026-09-18 审查 C5：句柄必须在在途 goroutine 收口后才关——扫描收尾写
// SaveScan、清理收尾写 FinishItem/FinalizeOp，先关句柄会让这些落账变成
// "sql: database is closed"，用户侧表现为历史/账本凭空少一条。
// 等待设上限：单条文件系统调用（网络卷、回收站服务）不可中断，
// 不能让"关不掉"成为代价；超时后按现状放行，并在 stderr 留痕。
func (a *App) shutdown(ctx context.Context) {
	a.cancelInFlight()
	if !waitGroupTimeout(&a.wg, inflightDrainGrace) {
		fmt.Fprintf(os.Stderr, "[app] 在途任务未在 %s 内收口，句柄先行释放（本次落账可能缺失）\n", inflightDrainGrace)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
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

// resultSuperseded 报告 myGen 这一代结果集是否已被更新的任务取走
// （2026-09-18 审查 C7）。收尾 goroutine 据此弃写、弃发终止事件。
func (a *App) resultSuperseded(myGen uint64) bool {
	return myGen != a.resultGen.Load()
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
	myGen := a.resultGen.Add(1) // 代际号：goroutine 收尾据此判断自己是否已被取代
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
		// 代际判定：复位在途标志后本 goroutine 还剩「写回结果集 → SaveScan →
		// resultsReady/curHistID → 终止事件」几拍，其间新扫描完全可能已被用户
		// 发起（StartScan 只看 scanInFlight）。代际不符即弃写、弃发事件，
		// 否则旧任务会把新扫描的 curHistID 换成自己的历史行——之后的清理
		// 裁剪的是上一条记录，当前记录永远残留已删文件。
		superseded := func() bool { return a.resultSuperseded(myGen) }
		if err != nil {
			if a.pipe.Status() == model.StatusCancelled {
				resetInFlight()
				if !superseded() {
					a.emit(a.ctx, "scan:cancelled", ScanSummary{Elapsed: elapsed.String()})
				}
			} else {
				resetInFlight()
				if !superseded() {
					a.emit(a.ctx, "scan:error", map[string]string{"error": err.Error()})
				}
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
		if superseded() {
			a.mu.Unlock()
			fmt.Fprintf(os.Stderr, "[scan] 第 %d 代扫描已被新任务取代，收尾结果弃写\n", myGen)
			return
		}
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
	a.wg.Add(1)
	go func() {
		defer a.wg.Done() // 最先注册 → 最后执行：panic 路径也不会漏记账
		defer a.recoverGoroutine(kind)
		defer reset()
		body()
	}()
}

// inflightDrainGrace 退出前等在途任务收口的上限。超过它说明有不可中断的
// 单次系统调用卡住（网络卷、回收站服务），此时宁可放行退出也不能让窗口关不掉。
const inflightDrainGrace = 10 * time.Second

// waitGroupTimeout 等待 wg 归零，返回是否在期限内完成。
func waitGroupTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
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
		// I5：默认建议与 ApplyKeepPolicy("shortest") 共用 ops.SuggestKeepIndex。
		// 修正前这里另写了一份「只比路径长度」的规则，含隐藏目录的组上两份会
		// 选出不同文件——星标显示的那一份正是被默认策略清掉的那一份。
		keep = ops.SuggestKeepIndex(g)
	}
	for i, f := range g.Files {
		vol, volOK := fileVolume(f)
		v.Files = append(v.Files, FileView{
			ID:             f.ID,
			Path:           f.Path,
			Name:           filepath.Base(f.Path),
			Size:           f.Size,
			ModTime:        f.ModTime,
			IsKeep:         i == keep,
			Volume:         vol,
			VolumeResolved: volOK,
		})
	}
	return v
}

// fileVolume 计算文件所在卷的标识（供 UI 判定跨卷，见 FileView.Volume）。
//
// 两级来源，优先"真实卷身份"：
//
//	① f.Key.VolumeID —— 扫描阶段由平台层解析的卷序列号/设备号。
//	   Windows 上 FileKey 只在候选组内按需解析（scanner 阶段 1.5），
//	   所以这里**可能为未解析**；unix 上遍历时顺带填充，总是有值。
//	② 路径卷根 filepath.VolumeName —— 兜底。Windows 上恒为盘符（"D:"），
//	   unix 上为空串（此时用 "/" 统一表示"唯一根"）。
//
// 返回的第二个值区分二者，因为**可信度不同**：① 是内核给出的卷身份，
// 可直接用于跨卷判定；② 只是路径前缀，在挂载点/UNC 场景下会误判，
// 因此前端应跳过未解析的组而不是拿它去猜（见 FileView.VolumeResolved）。
func fileVolume(f *model.FileEntry) (string, bool) {
	if f.Key.Resolved {
		// 前缀区分来源，避免"恰好等于某盘符字符串"的荒诞碰撞。
		return fmt.Sprintf("vid:%d", f.Key.VolumeID), true
	}
	root := filepath.VolumeName(f.Path)
	if root == "" {
		root = string(filepath.Separator) // unix：全部路径同属一个根
	}
	return "root:" + root, false
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

// emptyIntersectionMsg 处理策略把勾选全部过滤掉时的拒绝文案。
//
// 措辞要点：
//   - 说清"发生了什么"（已选 N、命中 0）——让用户自己能判断是不是范围配错了；
//   - 说"已取消操作"而不是"请重试"——用户最怕"以为做了其实没做"，这句直接消歧义；
//   - 顺带提示未命中的文件夹数——那通常就是配错的那一条。
func emptyIntersectionMsg(selected, unmatchedDirs int) string {
	extra := ""
	if unmatchedDirs > 0 {
		extra = fmt.Sprintf("；其中 %d 个文件夹内没有任何可处理的重复文件（可能写错了路径）", unmatchedDirs)
	}
	return fmt.Sprintf("所选文件均不在「优先处理的文件夹」范围内（已选 %d 项，命中 0 项%s），"+
		"已取消操作，未改动任何文件。请调整优先文件夹，或清空该设置后重试。",
		selected, extra)
}

// ProcessPreview 处理策略的实际生效范围预览（供 UI 在执行前展示真实数量）。
type ProcessPreview struct {
	EffectiveIDs   []uint64 `json:"effectiveIds"`   // 勾选中位于优先文件夹内的 ID
	EffectiveCount int      `json:"effectiveCount"` // = len(EffectiveIDs)
	UnmatchedDirs  []string `json:"unmatchedDirs"`  // 一个可处理文件都没命中的目录
}

// PreviewProcessPolicy 在**不执行任何操作**的前提下，算出处理策略的实际生效范围。
//
// 为什么需要它：处理策略只做执行时过滤、不改动用户勾选，所以"实际会处理哪些"
// 在界面上天然不可见。若不预览，用户会看到"已勾选 40 项"点下去却只处理 12 项，
// 然后以为操作失败了。有了它，按钮计数、选择区说明、确认框三处都能显示真实数量。
//
// 参数带上 selectedIDs 并在此处求交，是为了让"交集"只有一份实现——
// 若让前端自己算，就又出现两处独立实现，正是 I5 那类事故的温床。
//
// 无副作用：只读 groups/keepIDs 快照，不置 opsRunning、不写账本、不动文件系统。
// 因此它**不受 opsRunning 互斥限制**（预览是只读的，不该被正在进行的清理挡住）。
func (a *App) PreviewProcessPolicy(dirs []string, selectedIDs []uint64) (ProcessPreview, error) {
	pv := ProcessPreview{EffectiveIDs: []uint64{}, UnmatchedDirs: []string{}}
	if len(dirs) == 0 {
		// 未启用处理策略：生效范围就是全部勾选。前端据此走原路径。
		pv.EffectiveIDs = append(pv.EffectiveIDs, selectedIDs...)
		pv.EffectiveCount = len(pv.EffectiveIDs)
		return pv, nil
	}

	a.mu.Lock()
	groups := a.groups
	keepIDs := a.keepIDs
	a.mu.Unlock()

	// 空结果集也要算出未命中目录（用户加了目录但当前没有重复文件，
	// 正是最需要提示的场景），所以不在这里提前返回。
	out := ops.ApplyProcessPolicy(groups, dirs, keepIDs)

	for _, id := range selectedIDs {
		if out.MatchIDs[id] {
			pv.EffectiveIDs = append(pv.EffectiveIDs, id)
		}
	}
	pv.EffectiveCount = len(pv.EffectiveIDs)
	if out.UnmatchedDirs != nil {
		pv.UnmatchedDirs = append(pv.UnmatchedDirs, out.UnmatchedDirs...)
	}
	return pv, nil
}

// ApplyKeepPolicy 保留策略引擎（M3-T01）：返回决策并记录 keepIDs（S2 保护依据）。
// 遍历阶段全程持锁（须与操作 goroutine 的结果集清理写互斥）；
// 历史持久化放到放锁之后（hist 自有锁，禁止与 a.mu 嵌套）。
func (a *App) ApplyKeepPolicy(policy model.KeepPolicy) (KeepOutcome, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return KeepOutcome{}, fmt.Errorf("清理操作执行中，请稍后再应用保留策略")
	}
	if len(a.groups) == 0 {
		a.mu.Unlock()
		return KeepOutcome{}, fmt.Errorf("暂无结果集")
	}
	if policy.Kind == "directory" && !ops.HasUsableDir(policy.Directories) {
		a.mu.Unlock()
		return KeepOutcome{}, fmt.Errorf("请至少添加一个保留目录")
	}
	decisions, unmatched := ops.ApplyKeepPolicy(a.groups, policy)
	a.keepIDs = make(map[uint64]bool, len(decisions))
	for _, d := range decisions {
		a.keepIDs[d.KeepID] = true
	}
	histID, paths := a.curHistID, a.keepPathsLocked()
	a.mu.Unlock()
	a.persistKeepPaths(histID, paths)
	// I4：directory 策略下没有任何成员命中保留目录的组，一个保留者都不会被标出来。
	// 这类组不受 S2 保护，必须让 UI 说清楚，不能让用户以为"已应用=已保护"。
	return KeepOutcome{Decisions: decisions, UnmatchedGroups: len(unmatched)}, nil
}

// ClearKeepDecisions 清除保留决策（回到默认建议）。
// B3-1：与 ApplyKeepPolicy 同口径的在途互斥。修正前只有本方法无守卫——清理
// 执行中点「重置」会成功改写 a.keepIDs，而同一次操作实际用的是派发时的快照，
// 于是「界面已重置、文件系统仍按旧保护集执行」两套事实并存。
func (a *App) ClearKeepDecisions() error {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return fmt.Errorf("清理操作执行中，请稍后重置保留策略")
	}
	a.keepIDs = nil
	histID := a.curHistID
	a.mu.Unlock()
	a.persistKeepPaths(histID, nil)
	return nil
}

// keepPathsLocked 当前保留决策对应的路径集合（须持 a.mu 调用）。
// 跨会话只有路径稳定——历史恢复后按 path→rowid 重建 keepIDs。
func (a *App) keepPathsLocked() []string {
	// 空集合用 [] 而非 nil：本函数的返回值语义就是"路径列表"，
	// nil 在这里只是"没数据"的另一种写法，徒增调用方的判空负担。
	out := make([]string, 0, len(a.keepIDs))
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
	// 载入即换代（2026-09-18 审查 C7）：此刻 scanInFlight 可能已是 false 而旧
	// 扫描 goroutine 仍在收尾，不换新代际就会被它把 curHistID 写回自己那行。
	a.resultGen.Add(1)
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

// undoableReason 解释「这条记录为什么不可回撤」，并给出可执行的下一步。
//
// 2026-09-19 改进：原文案是「该记录不可回撤（永久删除与 Windows 回收站不支持
// 应用内回撤）」。用户读完仍然不知道**自己能做什么**——尤其 Windows 回收站
// 这一条，其不可回撤并非「设计取舍」而是 API 层面的客观限制，必须讲清楚，
// 否则容易被理解成「软件偷懒，故意不给撤」。
//
// 区分两类的本质差异：
//   - 永久删除：文件已不在磁盘上，**物理上无从恢复**（应用内绝无可能）。
//   - Windows 回收站：文件**好好地躺在回收站里**，只是 SHFileOperation
//     不返回「哪个文件落到了哪个 $Recycle.Bin 路径」的映射，应用无法
//     自己算回去向。**手动还原完全可行**，出口是系统回收站。
//
// 二者都「不可应用内回撤」，但用户的可行动作截然不同，故分别成文。
func undoableReason(kind string) string {
	if kind == "trash" && runtime.GOOS == "windows" {
		return "Windows 回收站操作不支持应用内回撤：系统 API 不返回" +
			"「每个文件落在回收站的哪个位置」的映射，应用无法定位文件而把它搬回原处。" +
			"文件本身仍在回收站里，请点上方「打开系统回收站」，右键选择「还原」即可取回。"
	}
	return "永久删除不支持回撤：文件已从磁盘移除，没有任何可恢复的来源。" +
		"若还需保留这些文件，请重新扫描后改用「移入回收站」或「移动」。"
}

// beginJournal 在任何文件系统动作之前把本次清理的完整计划落盘（写前账本）。
// undoable 判定：delete 不可撤；Windows 回收站拿不到 src→dst 映射，
// 回撤改由「打开系统回收站」引导；其余可撤。
//
// 2026-09-18 审查 C3（用户裁定：仅可回撤类型拒绝）：账本不可用（hist 为 nil 或 BeginOp
// 失败）时，承诺过可回撤的操作（回收站/移动/硬链接）一律拒绝执行——原先
// 只发一条事件便照常移动文件，用户按手册 09 §6.7 预期可回撤而实际无账本可撤。
// 不承诺回撤的（永久删除、Windows 回收站）仍放行：这类操作故障前后能力等价，
// 若一并拒绝反而会把用户逼向唯一可用的破坏性路径；改为 stderr + app:error
// 显式留痕（绝不静默）。
//
// 必须在 a.mu 之外调用（hist 访问不与 a.mu 嵌套）；调用方已置 opsRunning。
func (a *App) beginJournal(hs *history.Store, histID int64, op model.OpRequest,
	groups []*model.DuplicateGroup, keepIDs map[uint64]bool) (int64, error) {
	plans := planOpItems(groups, keepIDs, op.FileIDs)
	if len(plans) == 0 {
		return 0, fmt.Errorf("无可操作文件（所选 id 均不在当前结果集，或均为保留项）")
	}
	undoable := op.Kind != "delete" && !(op.Kind == "trash" && runtime.GOOS == "windows")
	jid, jerr := func() (int64, error) {
		if hs == nil {
			return 0, fmt.Errorf("history.db 未就绪")
		}
		return hs.BeginOp(op.Kind, op.TargetDir, histID, undoable, plans)
	}()
	if jerr == nil {
		return jid, nil
	}
	if undoable {
		return 0, fmt.Errorf("清理账本不可用，已拒绝执行（无账本即无法回撤与追溯）：%w", jerr)
	}
	// 不可回撤类：留痕放行
	msg := fmt.Sprintf("本次操作未写入清理账本（%v）：该操作本就不支持应用内回撤，已照常执行并在此留痕", jerr)
	fmt.Fprintf(os.Stderr, "[history] %s\n", msg)
	a.emit(a.ctx, "app:error", map[string]string{"error": msg})
	return 0, nil
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

	// 处理策略过滤（2026-09-20）：实际处理范围 = FileIDs ∩ 优先文件夹内。
	//
	// 为什么在**这里**取交集，而不是下沉到 ops.Execute：
	//   ① ops.Execute 是纯执行器，不该感知"处理策略"这个业务概念；
	//   ② 它在 len(FileIDs)==0 时会早退——若把过滤下沉，勾选全部落在范围外时
	//      会**静默执行 0 个文件**，用户以为做了其实什么都没做。这正是要避免的。
	//   ③ 写前账本（beginJournal → planOpItems）也用 op.FileIDs，必须在此之前
	//      收窄，否则账本记录的范围超出实际执行范围，账本失真。
	//
	// op 是值传递：op.FileIDs = kept 只改本地副本，前端勾选状态不受影响。
	if len(op.ProcessDirs) > 0 {
		pout := ops.ApplyProcessPolicy(groups, op.ProcessDirs, keepIDs)
		selectedCount := len(op.FileIDs) // 过滤前的勾选数，用于事件与拒绝文案
		kept := make([]uint64, 0, selectedCount)
		for _, id := range op.FileIDs {
			if pout.MatchIDs[id] {
				kept = append(kept, id)
			}
		}
		if len(kept) == 0 {
			// 交集为空必须**明确拒绝**，不能静默执行 0 项。
			// 复位 opsRunning/opsCancel 照抄下面 beginJournal 失败分支——
			// 漏了会让应用永久卡在"操作执行中"（P2 死锁终防的教训）。
			a.mu.Lock()
			a.opsRunning = false
			a.opsCancel = nil
			a.mu.Unlock()
			cancelOp()
			return "", fmt.Errorf("%s", emptyIntersectionMsg(selectedCount, len(pout.UnmatchedDirs)))
		}
		op.FileIDs = kept
		// 通知前端本次过滤的实际范围。用事件而不是改返回值：ExecuteOperation
		// 的返回值是 opID，改签名会波及 Wails 绑定、TS 签名与一批后端测试。
		// 事件在派发前发出，前端据此在执行完成横幅里说明"已选 M 项中 K 项未处理"。
		if a.emit != nil && a.ctx != nil {
			a.emit(a.ctx, "ops:filtered", map[string]any{
				"matched":   len(kept),
				"selected":  selectedCount,
				"unmatched": pout.UnmatchedDirs,
			})
		}
	}

	// 2026-09-18 审查 C3：写前账本改为 fail-closed。原先 BeginOp 失败只发一条事件、
	// journalID 保持 0 后照常移动文件（hist 为 nil 时更是零提示），与手册
	// 09 §6.7「动手之前先把完整计划写入账本」的承诺相反——用户按界面预期
	// 可回撤，实际账本上什么都没有。此处在派发前落盘计划，失败即拒绝执行。
	// 放在锁外是因为 hist 访问绝不与 a.mu 嵌套（见 App.hist 注释）；
	// opsRunning 已在锁内置位，新扫描与新操作在此期间都被拒。
	journalID, jerr := a.beginJournal(hs, histID, op, groups, keepIDs)
	if jerr != nil {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
		return "", jerr
	}

	a.goTask("ops", func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}, func() {
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

// ---------- v0.5.0 功能 4：清理记录读取与回撤 ----------

// UndoResult ops:undo:done 终止载荷（Restored 为实际落地路径，可能因重名另置）。
type UndoResult struct {
	OpID     int64              `json:"opId"`
	OK       int                `json:"ok"`
	Restored []string           `json:"restored"`
	Failed   []model.FailedItem `json:"failed"`
}

// OpRecordDetail 单条清理记录（摘要 + 条目明细），供前端展开视图。
type OpRecordDetail struct {
	Meta  history.OpMeta `json:"meta"`
	Items []OpRecordItem `json:"list"`
}

// ListOpRecords 清理记录列表（新→旧）。
func (a *App) ListOpRecords() ([]history.OpMeta, error) {
	if a.hist == nil {
		return nil, fmt.Errorf("历史库不可用")
	}
	return a.hist.ListOps()
}

// OpRecordItem 条目明细视图（history.OpItem + 链接状态标注，2026-09-20）。
//
// 为什么在视图层加 IsSymlink/Dangling，而不是让前端自己 stat：
// 前端拿不到文件系统，只能靠 kind 猜；而 kind 是**整笔操作**的属性——
// 一笔 symlink 操作里的某一条可能根本没执行成功（state=failed），
// 原位什么都没有。逐条 Lstat 才知道实情。
//
// 这两个字段是**展示用**的：Dangling 只影响标红提示，不阻断回撤
// （悬空链接的回撤恰恰是最需要的场景，见 ops/undo.go undoSymlink）。
type OpRecordItem struct {
	history.OpItem
	IsSymlink bool `json:"isSymlink"` // 原位当前是一个符号链接
	Dangling  bool `json:"dangling"`  // 是链接且目标不可达（悬空）
}

// GetOpRecord 单条清理记录的条目明细。
//
// 逐条标注链接状态（2026-09-20）：只对**已成功执行**的 done 条目做检测——
// 其余状态（failed/skipped/cancelled）本就没在文件系统上动手，
// 原位可能有意料之外的第三方文件，检测它们只会产出误导性的标红。
func (a *App) GetOpRecord(opID int64) (OpRecordDetail, error) {
	if a.hist == nil {
		return OpRecordDetail{}, fmt.Errorf("历史库不可用")
	}
	m, items, err := a.hist.GetOp(opID)
	if err != nil {
		return OpRecordDetail{}, err
	}
	list := make([]OpRecordItem, 0, len(items))
	for _, it := range items {
		view := OpRecordItem{OpItem: it}
		if it.State == history.StateDone {
			view.IsSymlink, view.Dangling = ops.SymlinkStatus(it.OrigPath)
		}
		list = append(list, view)
	}
	return OpRecordDetail{Meta: *m, Items: list}, nil
}

// ClearOpRecords 删除全部清理账本（不动已回收/已移动的文件本身）。
// B3-1：清理/回撤在途时拒绝——账本此刻正被 FinishItem/FinalizeOp 逐项落账，
// 整表删除会让一个仍在移动文件的操作失去全部记录（事后既无从回撤也无从追溯），
// 而界面上只表现为"清空成功"。
func (a *App) ClearOpRecords() error {
	a.mu.Lock()
	busy := a.opsRunning
	a.mu.Unlock()
	if busy {
		return fmt.Errorf("清理/回撤操作执行中，请等待结束后再清空记录")
	}
	if a.hist == nil {
		return fmt.Errorf("历史库不可用")
	}
	return a.hist.ClearOps()
}

// UndoOperation 回撤一条清理记录：入口同步校验（历史库/互斥/记录可撤性），
// 通过后异步逐项执行，进度复用 ops:progress，终止发 ops:undo:done。
// 只处理 state=done 的条目——失败/跳过/取消项本就没动过文件系统，
// 回撤失败的项保持 undo_failed，用户可修正后再次回撤（done 项已转 undone，
// 天然幂等）。结果集不动：恢复的文件需要重新扫描确认状态。
func (a *App) UndoOperation(opLogID int64) (string, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("清理/回撤操作执行中，请稍候")
	}
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("扫描进行中，请等待结束后再回撤")
	}
	hs := a.hist // 锁内快照：goroutine 内不得再解引用可变字段
	if hs == nil {
		a.mu.Unlock()
		return "", fmt.Errorf("历史库不可用")
	}
	a.opsRunning = true
	opCtx, cancelOp := context.WithCancel(context.Background())
	a.opsCancel = cancelOp
	a.mu.Unlock()

	release := func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}

	meta, items, err := hs.GetOp(opLogID)
	if err != nil {
		release()
		return "", err
	}
	if !meta.Undoable {
		release()
		return "", fmt.Errorf("%s", undoableReason(meta.Kind))
	}

	undoID := fmt.Sprintf("undo-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))
	a.goTask("ops", release, func() {
		var todo []history.OpItem
		for _, it := range items {
			if it.State == history.StateDone {
				todo = append(todo, it)
			}
		}
		res := UndoResult{OpID: opLogID, Restored: []string{}, Failed: []model.FailedItem{}}
		total := len(todo)
		for i, it := range todo {
			if opCtx.Err() != nil {
				break // 剩余项保持 done，可再次回撤
			}
			a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: i, Total: total, Current: it.OrigPath})
			restored, uerr := undoExecuteItem(hs, meta.Kind, it)
			if uerr != nil {
				res.Failed = append(res.Failed, model.FailedItem{Path: it.OrigPath, Stage: "undo", Err: uerr.Error()})
				continue
			}
			res.OK++
			res.Restored = append(res.Restored, restored)
		}
		a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: total, Total: total, Current: ""})
		a.emit(a.ctx, "ops:undo:done", res)
	})
	return undoID, nil
}

// undoOneFn 回撤执行入口的间接引用：测试据此断言"账本先落、文件后动"的先后顺序。
var undoOneFn = ops.UndoOne

// undoExecuteItem 执行单条回撤并落账（批量/单项共用）。写前落账（2026-09-18
// 审查 I6）：先把条目置为 undoing 再动文件系统——原先先移动后落账，中途被杀会让
// 账本永久停在 done，文件其实已回家却显示"未回撤"，重试还必报"已不存在"。
// 收口：成功置 undone，失败置 undo_failed 并保留原因供排查与重试；
// 写前落账失败则拒绝执行（与 C3「无账本不动文件」同口径），调用方据 error 汇总成败。
func undoExecuteItem(hs *history.Store, kind string, it history.OpItem) (string, error) {
	if err := hs.MarkItemUndo(it.ID, history.StateUndoing, ""); err != nil {
		return "", fmt.Errorf("回撤写前落账失败，已放弃执行（文件系统未改动）: %w", err)
	}
	var restored string
	var uerr error
	if kind == "trash" && it.DestPath == "" {
		// darwin 旧版/映射失败时条目没有回收站落点，无法定位
		uerr = fmt.Errorf("无法定位回收站位置，请打开系统回收站手动还原")
	} else {
		restored, uerr = undoOneFn(ops.UndoItem{
			Kind: kind, OrigPath: it.OrigPath, DestPath: it.DestPath,
			LinkSrc: it.LinkSrc, Hash: it.Hash, Size: it.Size, MtimeNs: it.MtimeNs,
		})
	}
	if uerr != nil {
		if merr := hs.MarkItemUndo(it.ID, history.StateUndoFailed, uerr.Error()); merr != nil {
			fmt.Fprintf(os.Stderr, "[history] 回撤失败态落库出错 %s: %v\n", it.OrigPath, merr)
		}
		return "", uerr
	}
	if merr := hs.MarkItemUndo(it.ID, history.StateUndone, ""); merr != nil {
		fmt.Fprintf(os.Stderr, "[history] 回撤成功态落库出错 %s: %v\n", it.OrigPath, merr)
	}
	return restored, nil
}

// UndoOperationItem 回撤记录中的单个条目（全部回撤之外的增量通道）：
// done 与 undo_failed（修正后重试）可撤；互斥/可撤性/条目状态入口同步校验，
// 受理后异步执行，终止事件与批量回撤同构（ops:undo:done）。
// 与批量口径一致：已回撤项不重复处理（同步拒绝），结果集不动。
func (a *App) UndoOperationItem(opLogID, itemID int64) (string, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("清理/回撤操作执行中，请稍候")
	}
	if a.scanInFlight {
		a.mu.Unlock()
		return "", fmt.Errorf("扫描进行中，请等待结束后再回撤")
	}
	hs := a.hist // 锁内快照：goroutine 内不得再解引用可变字段
	if hs == nil {
		a.mu.Unlock()
		return "", fmt.Errorf("历史库不可用")
	}
	a.opsRunning = true
	opCtx, cancelOp := context.WithCancel(context.Background())
	a.opsCancel = cancelOp
	a.mu.Unlock()

	release := func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}

	meta, items, err := hs.GetOp(opLogID)
	if err != nil {
		release()
		return "", err
	}
	if !meta.Undoable {
		release()
		return "", fmt.Errorf("%s", undoableReason(meta.Kind))
	}
	var target *history.OpItem
	for i := range items {
		if items[i].ID == itemID {
			target = &items[i]
			break
		}
	}
	if target == nil {
		release()
		return "", fmt.Errorf("该记录中不存在此条目（itemID=%d）", itemID)
	}
	if target.State != history.StateDone && target.State != history.StateUndoFailed {
		release()
		return "", fmt.Errorf("该项不可回撤（当前状态：%s）", target.State)
	}
	it := *target

	undoID := fmt.Sprintf("undo-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))
	a.goTask("ops", release, func() {
		res := UndoResult{OpID: opLogID, Restored: []string{}, Failed: []model.FailedItem{}}
		if opCtx.Err() == nil {
			restored, uerr := undoExecuteItem(hs, meta.Kind, it)
			if uerr != nil {
				res.Failed = append(res.Failed, model.FailedItem{Path: it.OrigPath, Stage: "undo", Err: uerr.Error()})
			} else {
				res.OK++
				res.Restored = append(res.Restored, restored)
			}
		}
		a.emit(a.ctx, "ops:undo:done", res)
	})
	return undoID, nil
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
