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
	"math"
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

	// IsPending 表示"策略上这一项可被处理"（功能 3，2026-09-23「隐藏非拟处理项」）。
	// 口径 = 拟处理内核（pendingIDsLocked）在**全量结果集**上的投影：
	// 非保留 ∩（若启用）优先目录 −（若启用）黑名单目录，**与勾选无关**
	// （GetResultGroups 看不到勾选上下文，勾选是显示层的另一层过滤）。
	//
	// ★ 为什么由后端填而不是前端自算：前端手里只有 isKeep 和路径，路径归属
	// 判据（大小写折叠、Clean 归一、卷语义）全在后端——前端重算就是第二份
	// 实现，AS-H6/I5 两类事故（界面对"不会动的文件"做承诺 / 藏错行）的温床。
	// 前端只许消费这个布尔值。
	// 注意 ResultQuery 不带 dirs/excludeDirs 时它只反映"非保留"——白/黑名单
	// 没上送就没有那个维度的信息，界面若开着"隐藏"必须把它们一起上送。
	IsPending bool `json:"isPending"`
}

// GroupView 重复组视图。
type GroupView struct {
	GroupID     uint64 `json:"groupID"`
	Reclaimable uint64 `json:"reclaimable"`
	// ReclaimableActual/ActualKnown 实占口径（M6-P2）。ActualKnown=false 表示
	// 组内**没有任一成员**读到过实占（历史恢复、或该卷不提供 st_blocks/
	// 压缩尺寸），此时 ReclaimableActual 完全来自逻辑回退，界面必须显示
	// "实占未统计"而不是这个数。部分成员 unknown 时按逻辑大小计入，
	// 数字仍是可信下界（宁可少说，不把未知算成 0）。
	ReclaimableActual uint64     `json:"reclaimableActual"`
	ActualKnown       bool       `json:"actualKnown"`
	Size              uint64     `json:"size"`
	Files             []FileView `json:"files"`
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
	// Dirs/ExcludeDirs 「优先处理的文件夹」白名单与「不处理的文件夹」黑名单（功能 3）。
	// **只服务 FileView.IsPending 这一个投影**——不改分组、不改排序、不改聚合口径
	// （TestIsPendingDoesNotChangeAggregates 钉死）。不送 = 投影只反映"非保留"。
	// ★ 开着"隐藏非拟处理项"的每一次分页都必须带上，否则藏行用的是陈旧策略。
	// 副作用提醒：非空时后端要预热卷语义（会写探测文件），所以前端只在开关
	// 打开时才上送，别把它变成每次翻页的固定 I/O。
	Dirs        []string `json:"dirs"`
	ExcludeDirs []string `json:"excludeDirs"`
}

// PagedResult 分页结果（01 §7.2）。
type PagedResult struct {
	Total            uint64 `json:"total"`
	Page             int    `json:"page"`
	TotalReclaimable uint64 `json:"totalReclaimable"` // 全量口径（含未加载页，M4 审查修订）
	// TotalReclaimableActual 实占口径的全量合计（M6-P2），口径与上一行一致。
	// 注意它**可能大于** TotalReclaimable：实占含块对齐与预分配，逻辑大小不含。
	TotalReclaimableActual uint64      `json:"totalReclaimableActual"`
	Groups                 []GroupView `json:"groups"`
}

// sortKey 结果视图缓存键（Y3）：排序方式 + 归一化扩展名筛选。
type sortKey struct {
	sort string
	ext  string
}

// ---------- 拟处理清单查询（2026-09-23，设计段 2026-09-23-pending-files-query §2） ----------

// PendingQuery GetPendingFiles 查询参数。SelectedIDs/Dirs 与 PreviewProcessPolicy
// 同源（前端勾选 + 处理策略目录），判据共用 pendingIDsLocked，见那里。
type PendingQuery struct {
	SelectedIDs []uint64 `json:"selectedIds"` // 前端当前勾选（含会被排除项，用于逐行 reason）
	Dirs        []string `json:"dirs"`        // 「优先处理的文件夹」；空=未启用处理策略
	// ExcludeDirs 「不处理的文件夹」黑名单（功能 2）；空=未启用。
	// 落在此列表内的非保留项永不进拟处理集，归类 reason=excluded。
	ExcludeDirs []string `json:"excludeDirs"`
	Page        int      `json:"page"`
	PageSize    int      `json:"pageSize"`
	Sort        string   `json:"sort"` // group(默认，结果集组序)/size/path
}

// PendingRow 清单一行。Pending=false 时 Reason 说明为什么"点了执行也不会动它"。
type PendingRow struct {
	ID      uint64 `json:"id"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    uint64 `json:"size"`
	GroupID uint64 `json:"groupId"`
	Pending bool   `json:"pending"`
	Reason  string `json:"reason"` // ""/keep/outside/gone/excluded，见 Pending* 常量
}

// PendingPage 拟处理清单分页结果。计数一律全量口径（跨页），与 PagedResult 的
// TotalReclaimable 同一纪律（M4"部分当全部"教训）。
type PendingPage struct {
	Total        int `json:"total"` // = pending+excluded 去重后总行数
	Page         int `json:"page"`
	PageSize     int `json:"pageSize"`
	PendingCount int `json:"pendingCount"` // 前段行数=分界索引
	KeepCount    int `json:"keepCount"`
	OutsideCount int `json:"outsideCount"`
	GoneCount    int `json:"goneCount"`
	// ExcludedCount 落在「不处理的文件夹」黑名单内的行数（功能 2）。
	ExcludedCount int          `json:"excludedCount"`
	Rows          []PendingRow `json:"rows"`
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
	// ReclaimableActual 实占口径合计（M6-P2）。与 Reclaimable 并列而非替换：
	// 后者是历史表与既往清理数字的口径，换语义会让"上次 8 GB 这次 300 MB"
	// 看起来像回归。两数之差就是稀疏/压缩文件被逻辑口径虚报的部分。
	ReclaimableActual uint64 `json:"reclaimableActual"`
	FilesFailed       int    `json:"filesFailed"`
	Elapsed           string `json:"elapsed"`

	// M6-P4（2026-09-21）系统保护清单的可见计数：被剪枝的目录数、被跳过的
	// 盘根伪文件与 Windows 保留名文件数。引擎内置的排除**不许静默**——
	// 用户看到的结果比盘上少，就得有一个地方说明少掉的是什么、为什么。
	ProtectedDirs  uint64 `json:"protectedDirs"`
	ProtectedFiles uint64 `json:"protectedFiles"`
	// SkippedCloudFiles 本轮因"云端占位"跳过的文件数（M6-P1）。计数与
	// ProtectedFiles 同理：命中不是失败，但静默放弃会让用户以为盘上就只有这些
	// 可去重文件。AllowCloudHydration=true 时恒为 0。
	//
	// ★ 同 UnprotectedRoots 的口径缺口：history.db 没存这个数，从记录页恢复时
	// 只能显示"未统计"，不得用零值冒充"这一轮没有云端文件"。
	SkippedCloudFiles uint64 `json:"skippedCloudFiles"`
	// SkippedWorkTempFiles 本轮按 worktemp.IsTempName 跳过的工作临时名文件数
	// （M21，04 §6.8.8）。这是三类"跳过"里最后被点亮的一类：保护清单与云端占位
	// 早已有数，而应用自己的 .fdd-* 残留此前不计语料、不记日志、不进任何计数。
	//
	// ★ 判据是**名字形态**，分不出"我们的残留"与"用户恰好这样命名的文件"——
	// 横幅文案（M8）不得写成"清理了 N 个残留"，只能中性表述。本条只到 JSON
	// 与 CLI 报告，界面不呈现（裁定②）。
	//
	// ★ 同上面两条的口径缺口：history.db 没存这个数，从记录页恢复时只能显示
	// "未统计"，不得用零值冒充"这一轮没有工作临时文件"。
	SkippedWorkTempFiles uint64 `json:"skippedWorkTempFiles"`
	// CaseProbeUnproven 本轮**问卷过、但卷大小写语义取自平台默认**的扫描根数（M62+M85）。
	//
	// 为什么要有这个数：`fscase` 三态之前只回 bool，"实测"与"猜的（平台默认）"同形，
	// 上层既无从警示、也无从收窄判据（M105 的放宽前置条件就是"确证不敏感"）。
	//
	// ★ 0 有两种成因（口径全在 scanner.Result.CaseProbeUnproven 与 dedupeRoots 注释里）：
	// 所有根都拿到读数，或单根一趟按 C1 压根没问卷 —— 别把它读成"这一卷实测过"。
	//
	// ★ 同上面三条的口径缺口：history.db 没存这个数，从记录页恢复时只能显示"未统计"，
	// 不得用零值冒充"这一轮的卷语义都实测过"。界面不呈现（裁定③），故该缺口暂不可见。
	CaseProbeUnproven uint64 `json:"caseProbeUnproven"`
	// UnprotectedRoots 非空即表示"这一轮有扫描根脱离了系统保护"（用户显式点名
	// 了清单内的路径或其内部）。警示文案属 M8（本轮只有 JSON 与 CLI 报告）。
	//
	// ★ 口径：从记录页恢复历史扫描走的是 OpenHistory，那条路径上这三项
	// **没有可信来源**（history.db 未存该口径，见 04 §6.9 的 M6-P4 划账）：
	// 届时只能整格显示"未统计"，不得沿用零值冒充"这一轮没跳过任何东西"。
	UnprotectedRoots []string `json:"unprotectedRoots,omitempty"`
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
	scanInFlight bool               // 扫描"结果集尚未写回"的在途标志（互斥新扫描）。★ F1③：复位点在写回那一拍（本 goroutine 内），不是整条 goroutine 的生命周期；写回之后还剩"落历史 → 认领 → 发事件"三拍，那一段由 resultGen 判取代
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

	// startupNotice 启动阶段产生、但事件送不达的提示（M12b）。
	// startup 跑在前端注册监听之前，emit 出去也没人接（app:ready 之所以能用，
	// 是因为它只是给界面变个版本号，丢了无所谓；本条丢了就等于没修），
	// 因此改由前端初始化时主动拉一次，见 GetStartupNotice。
	startupNotice string

	forceExit func(code int) // 可测接缝：单测里不能真把测试进程带走

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
	a.openCache()
	// v0.5.0：扫描历史/清理账本（独立 history.db，确证损坏时隔离重建）
	a.openLedger()
	a.emit(a.ctx, "app:ready", AppVersion)
}

// openCache 打开哈希缓存；失败时除 stderr 外**还必须留一条界面提示**（M25）。
//
// 从 startup 里抽出来与 openLedger 同理（见其注释）：startup 会解析真实的
// os.UserConfigDir，单测调用它就会动到用户机器上的 cache.db。
//
// 2026-09-21（M25，04 §6.8.8）：修正前这里只写 stderr。GUI 没有终端 ⇒ 等于没说，
// 而后果不是崩溃而是**永久变慢且无从排查**：缓存没开成，每次扫描都全量重算，
// 用户只知道"这软件越来越慢"。与 M7（账本落账失败只写 stderr）同病。
// 本方法对 a.cch / a.pipe 的直接写在 M9 白名单同档（启动前置，无并发读者）。
func (a *App) openCache() {
	if a.cfgDir == "" {
		return
	}
	dbPath := filepath.Join(a.cfgDir, "cache.db")
	cch, err := cache.Open(dbPath)
	if err != nil {
		// 不去重、不降级退出，只在启动时说一次：功能不受损，受损的是速度。
		fmt.Fprintf(os.Stderr, "[cache] 哈希缓存不可用，本次运行将全量重算: %v (path=%s)\n", err, dbPath)
		a.addStartupNotice(cacheUnavailableNotice(dbPath, err))
		return
	}
	a.cch = cch
	a.pipe = a.pipe.WithCache(cch)
}

// cacheUnavailableNotice 把"哈希缓存打不开"翻成界面文案（纯函数）。
// 三件事缺一不可：出了什么事、对用户意味着什么（每次扫描重新算，慢；功能不受损）、
// 在哪个文件上。不写"永久变慢"——重启后可能就好了，说死就成了另一句无法证伪的话。
func cacheUnavailableNotice(dbPath string, err error) string {
	return "哈希缓存不可用（" + filepath.Base(dbPath) + "），本次运行的每次扫描都要重新计算，速度会变慢；" +
		"不影响去重结果。原因：" + err.Error()
}

// ledgerUnavailableNotice 把"账本库打不开"翻成界面文案（纯函数，M43）。
//
// 与 cacheUnavailableNotice 同形（出了什么事 / 在哪个文件上 / 对用户意味着什么），
// 但后果那一层完全不同、也更重：缓存打不开只是慢，账本打不开会让回收站/移动/硬链接
// 清理**被拒绝执行**（beginJournal 拿不到写前账本就不动手，仅永久删除照常）。
// 不写"历史全没了"这种吓人的话——库没打开，本来就没有本次运行的历史可丢。
func ledgerUnavailableNotice(histPath string, err error) string {
	return "历史库不可用（" + filepath.Base(histPath) + "），本次运行不保存历史记录，" +
		"且回收站/移动/硬链接清理会被拒绝执行（仅永久删除不受影响）。原因：" + err.Error()
}

// addStartupNotice 追加一条启动期提示（空串忽略）。
//
// 2026-09-21（M25）：槽位原来是"后写覆盖先写"，于是两个启动期问题同时发生时，
// 界面只显示后一个（登记原文的次生问题）。改为累积：先写的不再被挤掉，
// GetStartupNotice 的绑定形状与前端消费方式都不变（多条以 \n 分隔）。
// 调用点是"启动前置"（startup → openCache/openLedger），与既有写者同档。
func (a *App) addStartupNotice(msg string) {
	if msg == "" {
		return
	}
	a.mu.Lock()
	if a.startupNotice == "" {
		a.startupNotice = msg
	} else {
		a.startupNotice += "\n" + msg
	}
	a.mu.Unlock()
}

// openLedger 打开账本库，并在"确证损坏→隔离重建"发生时把原因留给界面。
//
// 从 startup 里抽出来是为了可测：startup 会去解析真实的 os.UserConfigDir，
// 单测调用它就会动到用户机器上的 history.db（隔离逻辑甚至会把它改名）。
// 这里对 a.hist 的直接写在 M9 的白名单内（startup / openLedger / shutdown
// 同属"启动前置"，那时还没有任何并发读者）。
func (a *App) openLedger() {
	if a.cfgDir == "" {
		return
	}
	histPath := filepath.Join(a.cfgDir, "history.db")
	hs, err := history.Open(histPath)
	if err != nil {
		// 2026-09-18 审查 C3 之后这里不再只是"失去历史/回撤能力"：账本不可用时
		// 回收站/移动/硬链接会被拒绝执行（仅永久删除照常），见 beginJournal。
		// M43（2026-09-21 审查 §14 兑现）：stderr 那一条留着给 CLI/无终端场景，
		// 但 GUI 用户没有终端——后果必须同时进 startupNotice，否则用户只看到
		// "清理莫名被拒绝"而不知所以然。不用 emit：此刻前端监听器还没注册（同 M12b）。
		fmt.Fprintf(os.Stderr, "[history] 历史库不可用：本次运行不保存历史，且回收站/移动/硬链接清理将被拒绝执行: %v (path=%s)\n", err, histPath)
		a.addStartupNotice(ledgerUnavailableNotice(histPath, err))
		return
	}
	a.hist = hs
	// M12b（2026-09-21 全仓审计 §五 12）：确证损坏的账本被隔离重建后，原先只
	// fprintf(stderr)，GUI 用户没有终端，看到的只是"历史记录页凭空变空"。
	// 不能用 emit：此刻前端的监听器还没注册（bind 在 store.init 里，早于挂载的
	// 事件都会丢），所以存进 startupNotice，由前端初始拉取取走。
	if q := hs.QuarantinedTo(); q != "" {
		a.addStartupNotice(ledgerQuarantineNotice(q))
	}
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
		// APP-3：同样走统一出口。这一条说的直接就是"本次落账可能缺失"，
		// 与 M7 那四处是同一件事，没有理由只留在看不见的 stderr 里。
		a.warnLedger(fmt.Sprintf("在途任务未在 %s 内收口，句柄先行释放（本次落账可能缺失，历史记录未必完整）", inflightDrainGrace))
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
//
// AS-H3（2026-09-20 全仓审计）：修正前把 `abs` 与 `EvalSymlinks(abs)` 并列成候选，
// **任一**候选落在**任一**授权目录内即放行——与注释自述的"防经由链接逃逸"正好相反。
// 授权目录内部的一个链接（root/esc → 外部）让 abs 命中授权，解析出的越界结果被无视，
// 随后 move.go 的 MkdirAll + Rename 就经这条链接写到用户从未授权的位置。
// 现在**只认解析后的真实路径**：目标不存在时对最近的已存在祖先求解，再接回尾段。
//
// 授权侧无需再解析：authorizeDir 已把 Clean 绝对路径与其 EvalSymlinks 变体一并登记，
// 两者都是操作系统给出的同一真实大小写形态。
//
// 调用方须持 a.mu。
func (a *App) moveTargetAllowed(dir string) bool {
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	resolved, err := resolveTargetPath(abs)
	if err != nil {
		return false
	}
	for auth := range a.authDirs {
		if withinDir(resolved, auth) {
			return true
		}
	}
	return false
}

// resolveTargetPath 返回 path 的真实物理位置：对**最近的已存在祖先**求
// EvalSymlinks，再把尚不存在的那段尾段原样接回（不存在的段谈不上链接）。
//
// 为什么不能只 EvalSymlinks(path)：move 目标常常还没创建（用户输入新目录名，
// 或由 move.go 的 MkdirAll 建出来），整条路径直接求解会失败——若据此判否就是
// 误拒合法目标，若据此放行"反正解析不了"就是本次要修的洞。逐级上溯到已存在的
// 祖先才是两者都对的答案。
//
// 连根都解析不动（异常）时返回 error，调用方按拒绝处理（fail-closed）。
func resolveTargetPath(path string) (string, error) {
	var tail []string
	cur := path
	for {
		if ev, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				ev = filepath.Join(ev, tail[i])
			}
			return filepath.Clean(ev), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("路径 %q 不存在任何可解析的祖先", path)
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
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

// scanAboutToSaveHook 是扫描收尾"即将落历史库"那一点的测试接缝
// （F1②，2026-09-23）。**生产恒 nil**，调用点见 StartScan 的收尾 goroutine。
//
// 为什么需要它：②要证的性质是"被取代的那一轮不落历史行"，而这要求"在 A 的判代际
// 与 SaveScan 之间插入一次 B 的 StartScan"。现有码在那一段没有任何卡点
// （a.emit 在 SaveScan 之后，a.hist 是具体结构体、没有可换装的函数字段），
// 光靠调度时序拿不到可复跑的读数。手法先例：fscase.fireProbeHook、
// ops.beforeActContentRecheck。
//
// ★ 代价如实：钩子本身是新符号 ⇒ ②的"改前红"不存在（改前树 import 不到它），
// 修对必红的证据由变异提供（把 superseded 早退挪回 SaveScan 之后 ⇒ 用例当场红）。
var scanAboutToSaveHook func()

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
	// ★ F1①（2026-09-23 第四轮全仓审查）：lastEvs 也在这里作废。它是进度回调的唯一写者，
	//   不复位就等于：新扫描已把 groups 清空、GetScanProgress 却仍回吐上一轮的终值——
	//   对本轮进度的一次**谎报**（注释自称的"断线重连语义"因此不成立）。零值是诚实答案：
	//   本轮还没有任何进度。（前端目前无消费者，见 §30.11 的如实收窄。）
	a.lastEvs = model.ProgressEvent{}
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
		var reclaimActual uint64
		for _, g := range groups {
			reclaim += g.Reclaimable
			reclaimActual += g.ReclaimableActual
		}
		a.scanInFlight = false
		a.mu.Unlock()
		// F1（②，2026-09-23 第四轮全仓审查）：判代际必须**早于**落库。改前的顺序是
		// SaveScan（锁外，慢）→ 再判 superseded → 弃写，于是被取代的那一轮照样占一格
		// history.MaxScanHistory，把最旧的**有效**记录挤掉（外键 CASCADE 连子行一起删）。
		// ★ 前移只是把窗口从"SaveScan + 两拍"收窄到"本行到 SaveScan 起跑之间"，**不消除**：
		//   SaveScan 依 App.hist 的既有约束必须在锁外，判代际与落库之间天然让一次锁。
		if scanAboutToSaveHook != nil {
			scanAboutToSaveHook()
		}
		if superseded() {
			fmt.Fprintf(os.Stderr, "[scan] 第 %d 代扫描已被新任务取代，历史与结果集均弃写\n", myGen)
			return
		}
		// v0.5.0 功能 3：扫描成功收尾自动写历史（锁外调用，见 App.hist 注释）。
		// 保存失败不影响结果集可用，只失去本次记录的恢复/裁剪联动。
		var histID int64
		if hs := a.histSnapshot(); hs != nil {
			id, serr := hs.SaveScan(cfg, groups, failed)
			if serr != nil {
				// M7：本行曾是全仓唯一"双通道留痕"的写法，其余四处只写 stderr。
				// 现在统一走 warnLedger（本函数即由此抽出）。
				a.warnLedger(fmt.Sprintf("扫描历史保存失败（不影响当前结果）：%v", serr))
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
		// M6-P4 / M21 / M62+M85：五个"按轮计数"口径在与 superseded() 判定同一临界区内取。
		// 为什么不能留到锁外的 emit 里现取：Run 一开始就把按轮计数器归零，
		// 而 a.scanInFlight 在"结果集写回"那一拍就复位（紧接其后的落库/认领这段全程为 false，
		// 新扫描因此可通过在途检查，只靠 resultGen 判取代）。锁内取数等于把结论钉死成"未被取代 ⇒
		// 没有新的 StartScan ⇒ 没有新一轮 Run ⇒ 这几个值仍是本轮的"。
		// ★ F1③：这里原写的是"在扫描体第一行就复位"——那个位置没有复位语句，
		//   真复位点在上面的 `a.scanInFlight = false`（与结果集写回同一个临界区）。
		pDirs := a.pipe.ProtectedDirs()
		pFiles := a.pipe.ProtectedFiles()
		cloudSkipped := a.pipe.CloudSkipped()
		wtSkipped := a.pipe.WorkTempSkipped()
		caseUnproven := a.pipe.CaseProbeUnproven()
		unprot := a.pipe.UnprotectedRoots()
		a.mu.Unlock()
		a.emit(a.ctx, "scan:done", ScanSummary{
			Groups:               len(groups),
			Reclaimable:          reclaim,
			ReclaimableActual:    reclaimActual,
			FilesFailed:          len(failed),
			Elapsed:              elapsed.String(),
			ProtectedDirs:        pDirs,
			ProtectedFiles:       pFiles,
			SkippedCloudFiles:    cloudSkipped,
			SkippedWorkTempFiles: wtSkipped,
			CaseProbeUnproven:    caseUnproven,
			UnprotectedRoots:     unprot,
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
//
// var 而非常量（APP-3 探针）：取值一字未改，只是让"超时真的发生"成为单测里
// 可构造的形状——否则那条留痕路径要拿 10 秒的真实等待去换一次断言。
var inflightDrainGrace = 10 * time.Second

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
//
// 功能 3（2026-09-23）：q.Dirs/q.ExcludeDirs 非空时逐行填 FileView.IsPending，
// 投影与执行判据同收归 pendingIDsLocked（见 pendingSetLocked），这里只是把
// 全量结果集的 ID 喂进内核。预热（写探测文件）必须保持在取锁**之前**（AS-R3），
// 且仅在有策略目录时才发生——翻页是高频动作，无策略时零额外 I/O。
func (a *App) GetResultGroups(q ResultQuery) (PagedResult, error) {
	var resolve ops.SensResolver
	if all := warmDirs(q.Dirs, q.ExcludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}
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
	// 全量可释放空间（含未加载页）：统计条与 totalGroups 同口径。
	// M6-P2：实占口径必须同样走全量——只给当页之和会让统计条在翻页时数字
	// 跳动，正是 M4 审查修过的那类"部分当全部"。
	var totalReclaim, totalReclaimActual uint64
	for _, g := range gs {
		totalReclaim += g.Reclaimable
		totalReclaimActual += g.ReclaimableActual
	}
	// M10a（2026-09-21 全仓审计 §五 10）：起点必须**溢出安全**。
	// q.Page/q.PageSize 是绑定层入参（int），前端能给任意值：修正前直接
	// `q.Page * q.PageSize`，乘积回绕成负数会让下面的 `start >= len(gs)`
	// 判不出来、`gs[start:end]` 当场 panic；回绕成正数则会拿出错误的一页。
	// 溢出只有一种含义（起点远在结果集之外），先用除法查出来。
	// 用 math.MaxInt 而非 MaxInt64：切片下标是平台 int，32 位目标上
	// 天花板是 MaxInt32，按 64 位判会把"已经回绕"的乘积放过去。
	if q.Page > 0 && q.PageSize > math.MaxInt/q.Page {
		return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, TotalReclaimableActual: totalReclaimActual, Groups: []GroupView{}}, nil
	}
	start := q.Page * q.PageSize
	if start >= len(gs) {
		return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, TotalReclaimableActual: totalReclaimActual, Groups: []GroupView{}}, nil
	}
	end := start + q.PageSize
	if end > len(gs) {
		end = len(gs)
	}
	views := make([]GroupView, 0, end-start)
	// IsPending 投影：只在这里算一次（本页所有组共用同一份集合）。
	// 放在切片判空之后：空页不必为"没有行的结果"遍历整个 byID。
	pending := a.pendingSetLocked(q.Dirs, q.ExcludeDirs, resolve)
	for _, g := range gs[start:end] {
		views = append(views, toGroupView(g, a.keepIDs, pending))
	}
	return PagedResult{Total: total, Page: q.Page, TotalReclaimable: totalReclaim, TotalReclaimableActual: totalReclaimActual, Groups: views}, nil
}

// buildSortedGroupsLocked 执行一次扩展名筛选 + 排序（调用方须持 a.mu）。
// 返回切片只读复用：调用方不得原地修改其元素顺序。
func (a *App) buildSortedGroupsLocked(key sortKey) []*model.DuplicateGroup {
	var gs []*model.DuplicateGroup
	for _, g := range a.groups {
		// 防御：空组直接剔除，不进入排序与分页。
		// size 排序分支读 gs[i].Files[0].Size，空组会越界 panic；count 与
		// reclaimable 分支虽不读 Files[0]，但把空组展示成"0 个文件"的行同样
		// 无意义（既不能勾选也没有可释放空间）。畸形组只可能来自历史库被
		// 外部改写（扫描流水线输出前有 len<2 过滤），这里统一挡掉。
		if len(g.Files) == 0 {
			continue
		}
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
// pending 为拟处理投影集合（见 pendingSetLocked）；nil 表示本次不投影
// （单元测试直调路径），届时 IsPending 一律 false——生产路径（GetResultGroups）
// 永远传内核算出的集合，不存在"忘了传"这一态可被界面读到。
func toGroupView(g *model.DuplicateGroup, keepIDs map[uint64]bool, pending map[uint64]bool) GroupView {
	// 防御：空组没有成员可展示，`g.Files[0].Size` 会越界 panic。走到这里就说明
	// 上游已经把畸形组放进了结果集（扫描流水线输出前有 len<2 过滤，故只可能来自
	// history.LoadScan 读到被外部改写/损坏的历史库）。视图层不该因此崩掉整页
	// ——返回一个成员为空的视图，前端 `g.files.length` 为 0，不显示任何行。
	if len(g.Files) == 0 {
		return GroupView{GroupID: g.GroupID, Files: []FileView{}}
	}
	v := GroupView{
		GroupID:           g.GroupID,
		Reclaimable:       g.Reclaimable,
		ReclaimableActual: g.ReclaimableActual,
		ActualKnown:       g.AnyActualKnown(),
		Size:              g.Files[0].Size,
		Files:             make([]FileView, 0, len(g.Files)),
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
			IsPending:      pending[f.ID],
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

// PreviewFile 预览：图片（512px / q85 JPEG 缩略图的 base64，体积随内容而变，函数里没有
// 字节上限常量）/ 文本（前 4KB）/ HEX（前 256B）。旧注释写的「图片 ≤256KB base64」在
// 高频细节图上是假的，真读数与登记见 04 §6.11 M148。
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
	// M58：定位命令"起得来但立刻非零退出"过去完全静默，现象是点一下没反应。
	return startCmd(cmd, func(werr error) {
		a.warnBackground("reveal", fmt.Sprintf("打开所在文件夹失败（%s）：%v", e.Path, werr))
	})
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
//
// M58（04 §6.11 APP-8）：改前的 goroutine 写的是 `_ = cmd.Wait()`，退出状态整个
// 丢掉。而"Start 成功、子进程随即非零退出"恰是这类命令最常见的失败形态
// （Finder/Dolphin/Finder AppleScript 拒绝、xdg-open 没有 handler、
// 回收站后端脚本缺失），现象就是**点一下没任何反应**——界面没说失败，
// stderr 上也没有痕迹。现在非零退出经 onExit 上报（出口见 warnBackground）。
//
// onExit 只在"启动成功但没成"时调用；Start 本身失败仍走 error 返回值，
// 两条通道不重复报同一件事。退出码 0 时**一次都不调**（反面钉见 P-19-2b：
// 否则每开一次 Finder 就弹一条提示）。
func startCmd(cmd *exec.Cmd, onExit func(error)) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil && onExit != nil {
			onExit(err)
		}
	}()
	return nil
}

// ---------- 设置（单一事实源，01 §7.3） ----------

// settingsPath 给出 settings.json 的路径；配置目录不可用时以错误收口。
//
// M60（04 §6.11 APP-12）：cfgDir 在 startup 取不到用户配置目录时留空串
// （app.go:278-288）。改前无条件 filepath.Join(a.cfgDir, "settings.json")，
// 空串时返回的是**相对路径** "settings.json" ⇒ 配置被写进进程 CWD。
// GUI 打包后 CWD 通常是只读目录或 "/"：写失败静默，而读又会把同目录下
// **别的程序**留下的同名文件当成本应用的用户配置（实测：CWD 里放一份
// {"theme":"dark"} 就会被读回来）。openCache:305 / openLedger:365 同档都有
// `if a.cfgDir == ""` 的兜底，唯独这一条漏了。
func (a *App) settingsPath() (string, error) {
	if a.cfgDir == "" {
		return "", fmt.Errorf("配置目录不可用（拿不到用户配置目录），本次不读写 settings.json")
	}
	return filepath.Join(a.cfgDir, "settings.json"), nil
}

// GetSettings 读取设置：要么完整解析的配置，要么**纯**默认值。
func (a *App) GetSettings() Settings {
	path, perr := a.settingsPath()
	if perr != nil {
		return defaultSettings() // 目录不可用：磁盘上没有属于本应用的证据
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultSettings() // 首次运行/读不到：磁盘上没有证据，静默回默认
	}
	s := defaultSettings()
	if jerr := json.Unmarshal(b, &s); jerr != nil {
		// M10b（2026-09-21 全仓审计 §五 10）：修正前是 `_ = json.Unmarshal`，
		// 坏文件被无声吞掉、返回一份"解析到哪算哪"的半成品配置；界面上看不出
		// 异常，下一次保存又把它写回磁盘，原始证据就此消失。
		// 现在改名为 .corrupt 留证（用户仍可手工恢复），并把失败原因报给界面。
		msg := "设置文件无法解析，本次使用默认设置"
		if rerr := os.Rename(path, path+".corrupt"); rerr == nil {
			msg += "，损坏文件已留证为 settings.json.corrupt"
		}
		if a.emit != nil {
			a.emit(a.ctx, "app:error", map[string]string{"error": msg + "（" + jerr.Error() + "）"})
		}
		s = defaultSettings()
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
	path, perr := a.settingsPath()
	if perr != nil {
		return s, perr
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return s, err
	}
	return s, nil
}

func defaultSettings() Settings {
	return Settings{Threads: 0, Theme: "system", Language: "zh"}
}

// ---------- 版本与存根（M3/M4/M5 范围） ----------

// ledgerQuarantineNotice 把"账本影像损坏已隔离重建"翻成界面文案（纯函数）。
// 必须说清三件事：为什么历史记录空了、旧文件还在不在（在，可自行恢复）、
// 后果是什么（此前记录的清理操作不再有回撤依据）。
func ledgerQuarantineNotice(quarantined string) string {
	return "历史记录与回撤账本已清空：历史库影像损坏，已隔离为 " +
		filepath.Base(quarantined) + " 并重建。旧文件仍在配置目录，需要时可手工查看或恢复；" +
		"本次运行起，此前记录的清理操作无法再回撤。"
}

// GetStartupNotice 取启动阶段的一次性提示（无则空串）。
// 为什么不是事件：见 App.startupNotice 的注释——startup 早于前端注册监听，
// 发出去的事件必然丢；前端在 store.init 的"初始拉取"里调一次即可。
func (a *App) GetStartupNotice() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupNotice
}

// GetVersion 当前版本。
func (a *App) GetVersion() string { return AppVersion }

// emptyIntersectionMsg 过滤器（白名单/黑名单）把勾选全部拦下时的拒绝文案。
//
// 措辞要点：
//   - 说清"发生了什么"（已选 N、命中 0）——让用户自己能判断是不是范围配错了；
//   - 说"已取消操作"而不是"请重试"——用户最怕"以为做了其实没做"，这句直接消歧义；
//   - 顺带提示未命中的文件夹数——那通常就是配错的那一条；
//   - hasDirs/hasExcludes 决定点名哪把尺子，只说真的生效的那把——
//     功能 2 前此函数只有白名单一路，文案逐字保留（FC 类回归钉）。
func emptyIntersectionMsg(selected, unmatchedDirs int, hasDirs, hasExcludes bool) string {
	extra := ""
	if unmatchedDirs > 0 {
		extra = fmt.Sprintf("；其中 %d 个文件夹内没有任何可处理的重复文件（可能写错了路径）", unmatchedDirs)
	}
	if hasDirs && !hasExcludes {
		return fmt.Sprintf("所选文件均不在「优先处理的文件夹」范围内（已选 %d 项，命中 0 项%s），"+
			"已取消操作，未改动任何文件。请调整优先文件夹，或清空该设置后重试。",
			selected, extra)
	}
	scope := "「不处理的文件夹」黑名单内"
	advice := "请调整不处理目录，或清空该设置后重试。"
	if hasDirs {
		scope = "本次处理范围内（受「优先处理的文件夹」与「不处理的文件夹」共同限定）"
		advice = "请调整这两项文件夹设置，或清空后重试。"
	}
	return fmt.Sprintf("所选文件均在%s（已选 %d 项，命中 0 项%s），已取消操作，未改动任何文件。%s",
		scope, selected, extra, advice)
}

// ProcessPreview 处理策略的实际生效范围预览（供 UI 在执行前展示真实数量）。
type ProcessPreview struct {
	EffectiveIDs   []uint64 `json:"effectiveIds"`   // 勾选中位于优先文件夹内的 ID
	EffectiveCount int      `json:"effectiveCount"` // = len(EffectiveIDs)
	UnmatchedDirs  []string `json:"unmatchedDirs"`  // 一个可处理文件都没命中的目录
}

// PendingKeep/PendingOutside/PendingGone/PendingExcluded 拟处理清单的排除原因
// （PendingRow.Reason 的四值）。
const (
	PendingKeep     = "keep"     // 保留项：执行器硬拒绝，勾选了也不会动
	PendingOutside  = "outside"  // 不在任何「优先处理的文件夹」内（处理策略把它滤掉）
	PendingGone     = "gone"     // 已不在当前结果集（扫描收尾清理、切换历史后勾选残留）
	PendingExcluded = "excluded" // 落在「不处理的文件夹」黑名单内（功能 2）
)

// warmDirs 合并两批待预热目录（白名单 + 黑名单）。必须新建切片——
// append(dirs, excludeDirs...) 在 dirs 有余量时会**改写调用方**（前端状态里
// 的 procDirs），那是"预览顺手改了用户设置"级别的事敌。
func warmDirs(dirs, excludeDirs []string) []string {
	if len(excludeDirs) == 0 {
		return dirs
	}
	if len(dirs) == 0 {
		return excludeDirs
	}
	all := make([]string, 0, len(dirs)+len(excludeDirs))
	all = append(all, dirs...)
	all = append(all, excludeDirs...)
	return all
}

// pendingIDsLocked 是「拟处理集」判据的唯一内核：
// 勾选 ∩ 结果集内非保留 ∩（若启用）优先目录 −（若启用）黑名单目录。
//
// 为什么必须收归一份（设计段 §3）：预览计数、拟处理清单、执行派发三处都要问
// "哪些文件真的会被动"。此前预览与执行已按 M10c 修齐口径但仍是两段相似代码，
// 清单再加一段就是三套判据——AS-H6/I5 两类事故（判据漂移 ⇒ 界面对"不会动的
// 文件"做承诺）的温床。现在三处只有这一份实现。
//
// 返回：ids 按 selectedIDs 顺序去重（同 planOpItems 的 seen）；reason 给每个
// 被排除的勾选 id 归类（keep/outside/gone/excluded，见 Pending* 常量）；unmatched 是
// 一个可处理文件都没命中的优先目录（用户原文，回显定位用）。
//
// 须持 a.mu 调用；resolve 由调用方在**取锁之前**用 ops.WarmSensitivity 预热（AS-R3：
// 卷语义探测要写用户目录探测文件，锁内做 I/O 会被死挂载连坐卡死全部绑定）。
// 预热必须覆盖 dirs 与 excludeDirs **两批**目录——黑名单漏预热就是锁内写盘。
//
// 分岔语义与修正前逐格一致（M10c）：dirs 为空走 byID 存在性 + keepIDs 过滤；
// 非空走引擎 MatchIDs（引擎已剔保留项）。排除归类按 gone→keep→outside→excluded
// 四个判据**短路**——最先拦住它的是谁就记谁：白名单没放行谈不上黑名单，
// 保留项在执行器里本来就是硬拒绝，excluded 的说法会夸大黑名单的能力。
// 对结果集外的陈旧 id，两分支归类相同（gone），生效集合不变。
//
// excludeDirs 匹配走与 ProcessDirs 完全相同的引擎（ApplyProcessPolicyWith 的
// inAnyDir 判据，含卷大小写敏感性折叠）：黑名单判错的后果是"说了不处理却
// 处理了"（fail-dangerous），必须与白名单同一把尺子。keepIDs 传 nil——这里只要
// "路径归属"，保留项已在上一格短路，不该被引擎再吞一次归类信息。
func (a *App) pendingIDsLocked(dirs, excludeDirs []string, resolve ops.SensResolver,
	selectedIDs []uint64) (ids []uint64, reason map[uint64]string, unmatched []string) {
	var match map[uint64]bool
	if len(dirs) > 0 {
		out := ops.ApplyProcessPolicyWith(a.groups, dirs, a.keepIDs, resolve)
		match = out.MatchIDs
		unmatched = out.UnmatchedDirs
	}
	var excluded map[uint64]bool
	if len(excludeDirs) > 0 {
		excluded = ops.ApplyProcessPolicyWith(a.groups, excludeDirs, nil, resolve).MatchIDs
	}
	ids = []uint64{} // 空集合回 [] 不回 nil：这条线直达 JSON，nil 会序列化成 null
	reason = make(map[uint64]string, len(selectedIDs))
	seen := make(map[uint64]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		if seen[id] {
			continue // 重复勾选只算一次（同 planOpItems 的 seen）
		}
		seen[id] = true
		if _, ok := a.byID[id]; !ok {
			reason[id] = PendingGone
			continue
		}
		if a.keepIDs[id] {
			reason[id] = PendingKeep
			continue
		}
		if match != nil && !match[id] {
			reason[id] = PendingOutside
			continue
		}
		if excluded[id] {
			reason[id] = PendingExcluded
			continue
		}
		ids = append(ids, id)
	}
	return ids, reason, unmatched
}

// pendingSetLocked 结果页投影用的「策略可处理」集合（功能 3）：把 byID 全量
// 当候选集喂进 pendingIDsLocked，返回 ID 集合。
//
// ★ 为什么不另写一份"非保留 ∩ 白 − 黑"：那会是拟处理判据的第二实现——
// AS-H6（前端自算命中）与 I5（视图自选保留者）两类事故都是同一个形状：
// 第二份实现漂移后，界面对"不会动的文件"做承诺、或把该留的藏了。
// 这里付出的 O(结果文件数) 每次翻页一遍，换"藏的行 == 引擎会动的行"这个不变式。
//
// gone 归类天然不会命中（候选集就是结果集全量）；勾选上下文不在口径里——
// IsPending 说"策略上可处理"，"用户勾没勾"是显示层的另一层，见 FileView.IsPending。
//
// 须持 a.mu；resolve 由调用方在取锁前预热（AS-R3，与 pendingIDsLocked 同一约定）。
func (a *App) pendingSetLocked(dirs, excludeDirs []string, resolve ops.SensResolver) map[uint64]bool {
	all := make([]uint64, 0, len(a.byID))
	for id := range a.byID {
		all = append(all, id)
	}
	ids, _, _ := a.pendingIDsLocked(dirs, excludeDirs, resolve, all)
	set := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// PreviewProcessPolicy 在**不执行任何操作**的前提下，算出处理策略的实际生效范围。
//
// 为什么需要它：处理策略只做执行时过滤、不改动用户勾选，所以"实际会处理哪些"
// 在界面上天然不可见。若不预览，用户会看到"已勾选 40 项"点下去却只处理 12 项，
// 然后以为操作失败了。有了它，按钮计数、选择区说明、确认框三处都能显示真实数量。
//
// 参数带上 selectedIDs 并在此处求交，是为了让"交集"只有一份实现——
// 若让前端自己算，就又出现两处独立实现，正是 I5 那类事故的温床。
// 2026-09-23 起该求交收进 pendingIDsLocked，与 GetPendingFiles 共用；
// 同日功能 2 加 excludeDirs（「不处理的文件夹」黑名单），判据仍只有那一份。
//
// 无副作用：只读 groups/keepIDs 快照，不置 opsRunning、不写账本、不动文件系统。
// 因此它**不受 opsRunning 互斥限制**（预览是只读的，不该被正在进行的清理挡住）。
func (a *App) PreviewProcessPolicy(dirs, excludeDirs []string, selectedIDs []uint64) (ProcessPreview, error) {
	// ★ AS-R3（2026-09-20 全仓审计）：卷语义探测要**往用户目录写探测文件**，
	// 属于 I/O，必须在取锁之前做完——死挂载（网络盘拔走后 stat 挂死）时，
	// 锁内一次写盘就能把 a.mu 连同全部 Wails 绑定一起卡住，而预览本应是只读操作。
	// 预热之后（结论按目录缓存），锁内那次调用是纯查表 + 纯比较。
	// 黑名单与白名单走同一引擎，预热也必须一并覆盖（功能 2）。
	var resolve ops.SensResolver
	if all := warmDirs(dirs, excludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}

	// 全程持锁：这里只剩纯内存计算（无 I/O、不回调整个 App），
	// 耗时与组数成正比，锁住它不伤害交互。此前"锁内浅快照、锁外遍历"的写法
	// 挡不住**元素级**竞争——a.groups 是 []*DuplicateGroup，浅拷贝共享指针，
	// 执行器收尾会在锁内就地改写 g.Files（app.go 结果集清理段 g.Files = files），
	// 无锁遍历即构成数据竞争（2026-09-20 -race 实测复现，TestPreviewProcessPolicyConcurrentWithCleanupNoRace）。
	a.mu.Lock()
	defer a.mu.Unlock()

	ids, _, unmatched := a.pendingIDsLocked(dirs, excludeDirs, resolve, selectedIDs)
	pv := ProcessPreview{EffectiveIDs: ids, EffectiveCount: len(ids), UnmatchedDirs: []string{}}
	// 空结果集也要算出未命中目录（用户加了目录但当前没有重复文件，
	// 正是最需要提示的场景），所以不在这里提前返回。
	if unmatched != nil {
		pv.UnmatchedDirs = append(pv.UnmatchedDirs, unmatched...)
	}
	return pv, nil
}

// GetPendingFiles 分页查询「执行前拟处理清单」：勾选里哪些真的会被处理、
// 哪些不会（及原因）。2026-09-23 新增（设计段 2026-09-23-pending-files-query）。
//
// 与 PreviewProcessPolicy 的关系：同一内核（pendingIDsLocked）的两个投影——
// 预览给计数（按钮禁用、确认框用），清单给明细（用户核对"到底动哪几个文件"）。
// 二者对同一输入必须逐 id 相等，由 TestPendingFilesMirrorsPreviewKernel 钉住。
//
// 行序：先「将处理」段（按 Sort：group=结果集组序、size 降序、path 升序，
// tie-break 一律落到 GroupID→ID——M144 教训：同值不落地就会翻页跳行），
// 后「不会处理」段（勾选原序）。分界索引 = PendingCount，前端据此插分区标题。
//
// 锁与副作用语义与 PreviewProcessPolicy 逐条相同（只读、持 a.mu、锁外预热、
// 不受 opsRunning 互斥限制）。分页钳位与溢出安全同 GetResultGroups（M10a）。
func (a *App) GetPendingFiles(q PendingQuery) (PendingPage, error) {
	var resolve ops.SensResolver
	if all := warmDirs(q.Dirs, q.ExcludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if q.PageSize <= 0 {
		q.PageSize = 100
	} else if q.PageSize > 500 {
		q.PageSize = 500
	}
	if q.Page < 0 {
		q.Page = 0
	}

	ids, reason, _ := a.pendingIDsLocked(q.Dirs, q.ExcludeDirs, resolve, q.SelectedIDs)
	pending := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		pending[id] = true
	}

	// 组内位置一次遍历拿齐：GroupID 展示用，(组序, 组内序) 是 group 排序键。
	// 覆盖所有勾选（含被排除项）——"属于哪个组"对排除行同样是有效信息。
	type loc struct {
		gid     uint64
		grpIdx  int
		fileIdx int
	}
	sel := make(map[uint64]bool, len(q.SelectedIDs))
	for _, id := range q.SelectedIDs {
		sel[id] = true
	}
	pos := make(map[uint64]loc, len(sel))
	for gi, g := range a.groups {
		for fi, f := range g.Files {
			if sel[f.ID] {
				pos[f.ID] = loc{g.GroupID, gi, fi}
			}
		}
	}

	rows := make([]PendingRow, 0, len(ids))
	excludedRows := make([]PendingRow, 0, len(q.SelectedIDs)-len(ids))
	var keepN, outsideN, goneN, excludedN int
	emitted := make(map[uint64]bool, len(q.SelectedIDs)) // 重复勾选只出一行（与内核 seen 同口径）
	for _, id := range q.SelectedIDs {
		if emitted[id] {
			continue
		}
		emitted[id] = true
		if pending[id] {
			e := a.byID[id]
			p := pos[id]
			rows = append(rows, PendingRow{
				ID: id, Path: e.Path, Name: filepath.Base(e.Path),
				Size: e.Size, GroupID: p.gid, Pending: true,
			})
			continue
		}
		r := reason[id]
		if r == "" {
			// 理论不可达：pendingIDsLocked 对每个被排除 id 必归三类之一。
			// 兜成 gone（最保守的说法——"它不在会被动的清单里"为大方向真），
			// 而不是 panic 或漏行：漏行会让 Total 说谎。
			r = PendingGone
		}
		switch r {
		case PendingKeep:
			keepN++
		case PendingOutside:
			outsideN++
		case PendingExcluded:
			excludedN++
		default:
			goneN++
		}
		// gone 的 id 已不在 byID，路径无从得知——行里只有 ID+原因。
		// 这是能力的边界不是实现的偷懒：给它编一个"上一条已知路径"就是说谎。
		var e *model.FileEntry
		if r != PendingGone {
			e = a.byID[id]
		}
		row := PendingRow{ID: id, Reason: r, GroupID: pos[id].gid} // 组已消散时零值 0，仅展示用
		if e != nil {
			row.Path = e.Path
			row.Name = filepath.Base(e.Path)
			row.Size = e.Size
		}
		excludedRows = append(excludedRows, row)
	}
	pendingRows := rows
	// 「将处理」段按 Sort 重排；「不会处理」段保持勾选原序（它只是备注，
	// 给它排序只会让人怀疑"排除也有优先级"——处理策略没有优先级，见 keep.go）。
	switch q.Sort {
	case "size":
		sort.SliceStable(pendingRows, func(i, j int) bool {
			if pendingRows[i].Size != pendingRows[j].Size {
				return pendingRows[i].Size > pendingRows[j].Size
			}
			if pendingRows[i].GroupID != pendingRows[j].GroupID {
				return pendingRows[i].GroupID < pendingRows[j].GroupID
			}
			return pendingRows[i].ID < pendingRows[j].ID
		})
	case "path":
		sort.SliceStable(pendingRows, func(i, j int) bool {
			if pendingRows[i].Path != pendingRows[j].Path {
				return pendingRows[i].Path < pendingRows[j].Path
			}
			return pendingRows[i].ID < pendingRows[j].ID
		})
	default: // group：结果集组序 → 组内成员序 → ID 兜底（三键全序，翻页必稳定）
		sort.SliceStable(pendingRows, func(i, j int) bool {
			pi, pj := pos[pendingRows[i].ID], pos[pendingRows[j].ID]
			if pi.grpIdx != pj.grpIdx {
				return pi.grpIdx < pj.grpIdx
			}
			if pi.fileIdx != pj.fileIdx {
				return pi.fileIdx < pj.fileIdx
			}
			return pendingRows[i].ID < pendingRows[j].ID
		})
	}
	all := append(pendingRows, excludedRows...)
	total := len(all)

	page := PendingPage{
		Total: total, Page: q.Page, PageSize: q.PageSize,
		PendingCount: len(pendingRows), KeepCount: keepN, OutsideCount: outsideN,
		GoneCount: goneN, ExcludedCount: excludedN,
		Rows: []PendingRow{},
	}
	// M10a 同形的溢出安全起点：Page/PageSize 是绑定层入参，乘积回绕会把
	// "远在集合外"读成合法页或直接 panic。溢出只有一种含义——空页但计数照给。
	if q.Page > 0 && q.PageSize > math.MaxInt/q.Page {
		return page, nil
	}
	start := q.Page * q.PageSize
	if start >= total {
		return page, nil
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	page.Rows = all[start:end]
	return page, nil
}

// FilterInDirs 批量判定「哪些路径位于任一优先目录下」，返回 paths 的下标（升序、不重复）。
//
// ★ 2026-09-20（全仓审计 AS-H6 / 决策 D-1 方案 b）：界面上的「将处理 N / 已排除 M」
// 此前由前端自己实现一份路径归属判据算出来，而后端执行范围由 ops.inDir 决定——
// 同一判据两份实现，且前端那份已经实测漂移（不做 Clean 归一、按路径形状猜大小写
// 语义），漂移方向是**少报命中**：界面说"已排除"的那一项后端其实会处理，
// 等于对"不会动的文件"做了假承诺。现在前端只显示这里返回的下标。
//
// 与 PreviewProcessPolicy 的分工：那个绑定求的是「勾选 ID ∩ 后端结果集」，
// 需要 a.mu；本绑定是纯字符串判据，输入完全由前端给出（前端手上有当前
// 展示的路径），因此**不加锁、不受 opsRunning 互斥限制**，可以在清理进行中调用。
//
// 语义与执行侧严格一致：并集、无优先级、空白目录忽略；dirs 全空白时返回空列表
// （= 未启用处理策略，前端据此走"不过滤"的原路径）。
func (a *App) FilterInDirs(dirs []string, paths []string) []int {
	return ops.FilterInDirs(dirs, paths)
}

// ApplyKeepPolicy 保留策略引擎（M3-T01）：返回决策并记录 keepIDs（S2 保护依据）。
// 遍历阶段全程持锁（须与操作 goroutine 的结果集清理写互斥）；
// 历史持久化放到放锁之后（hist 自有锁，禁止与 a.mu 嵌套）。
func (a *App) ApplyKeepPolicy(policy model.KeepPolicy) (KeepOutcome, error) {
	// ★ APP-1（2026-09-21 全量审查）：AS-R3 的同一件事在处理策略侧已修，
	// 保留策略侧漏了。directory 分支要按卷问 fscase.Sensitive，而它**要往用户
	// 目录写探测文件**——落在下面的持锁窗口里，一个死挂载就能把 a.mu 连同
	// 全部 Wails 绑定一起卡住。预热之后锁内只剩纯查表 + 纯比较。
	var resolve ops.SensResolver
	if policy.Kind == "directory" && len(policy.Directories) > 0 {
		resolve = ops.WarmSensitivity(policy.Directories)
	}

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
	decisions, unmatched := ops.ApplyKeepPolicyWith(a.groups, policy, resolve)
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
	hs := a.histSnapshot()
	if hs == nil || histID == 0 {
		return
	}
	if err := hs.UpdateKeepPaths(histID, paths); err != nil {
		a.warnLedger(fmt.Sprintf("保留决策保存失败：%v，恢复该条历史时保留项会回到上一次成功保存的状态", err))
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
	hs := a.histSnapshot()
	if hs == nil {
		return nil, fmt.Errorf("历史库不可用")
	}
	ms, err := hs.ListScans()
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
	hs := a.histSnapshot()
	if hs == nil {
		return ScanSummary{}, fmt.Errorf("历史库不可用")
	}
	a.mu.Lock()
	busy := a.opsRunning || a.scanInFlight
	a.mu.Unlock()
	if busy {
		return ScanSummary{}, fmt.Errorf("扫描/清理进行中，请稍后再打开历史")
	}

	meta, groups, err := hs.LoadScan(id)
	if err != nil {
		return ScanSummary{}, err
	}
	byID := make(map[uint64]*model.FileEntry, meta.Files)
	var reclaim uint64
	var reclaimActual uint64
	for _, g := range groups {
		reclaim += g.Reclaimable
		// M6-P2：历史库没有实占口径，LoadScan 按"成员全部 unknown"回填，
		// 所以这里两数**必然相等**。相等不是"实占恰好等于逻辑大小"的结论，
		// 而是"没统计过"——界面须靠成员的 ActualKnown 判定并显示"未统计"，
		// 不得把这个数当成实占播报（与 M6-P4 三项计数同一处置）。
		reclaimActual += g.ReclaimableActual
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
		Groups: len(groups), Reclaimable: reclaim, ReclaimableActual: reclaimActual,
		FilesFailed: len(meta.Failed),
	}, nil
}

// DeleteScanHistory 删除一条历史；若正是当前结果集的来源，仅断开联动
// （内存结果仍可看可清，只是后续裁剪不再回写）。
func (a *App) DeleteScanHistory(id int64) error {
	hs := a.histSnapshot()
	if hs == nil {
		return fmt.Errorf("历史库不可用")
	}
	if err := hs.DeleteScan(id); err != nil {
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
	hs := a.histSnapshot()
	if hs == nil {
		return fmt.Errorf("历史库不可用")
	}
	if err := hs.ClearScans(); err != nil {
		return err
	}
	a.mu.Lock()
	a.curHistID = 0
	a.mu.Unlock()
	return nil
}

// undoableFor 报告「这类操作在这个平台上能不能应用内回撤」——全包唯一实现。
//
// APP-6（2026-09-21 全量审查，I5 + H6）：这条判据原先有**两份内联写法**：
// beginJournal 落库的 `OpMeta.Undoable` 一处、原因分流（现名 `undoReasonCodeFor`）一处。
// 两份必须同源，否则会出现"账本说可撤、界面说不可撤"（或反过来）。
// 更要紧的是两处都直接读 `runtime.GOOS`，Windows 那条腿在本机永远断言不到；
// 现在平台真值经参数注入，纯函数在三平台同一份代码上可测。
func undoableFor(kind, goos string) bool {
	return kind != "delete" && !(kind == "trash" && goos == "windows")
}

// 回撤失败原因码。字符串本身是**前后端契约**：前端 `utils/undoReason.ts` 按它查文案，
// 改一个字节就等于把用户看到的说明换成空白兜底（M79）。
const (
	undoCodeWindowsTrash    = "undo-code-windows-trash"
	undoCodePermanentDelete = "undo-code-permanent-delete"
)

// undoReasonCode 给出「这条记录为什么不可回撤」的**原因码**，全包唯一出口。
//
// M79（2026-09-22 裁定"判据归后端、文案归前端"）：这里原本直接吐中文正文，
// 而同一份解释在 `RecordsView.vue` 里另有一份短体 ⇒ 两处措辞已经各自漂移，
// 后端每改一次文案就得重发一次二进制。现在后端只下发稳定码，中文由
// `frontend/src/utils/undoReason.ts` 独家提供。
//
// 为什么必须是两码而不是一码（原中文文案留下的理由，随文案搬到前端）：
//   - 永久删除：文件已不在磁盘上，**物理上无从恢复**（应用内绝无可能）。
//   - Windows 回收站：文件**好好地躺在回收站里**，只是 SHFileOperation
//     不返回「哪个文件落到了哪个 $Recycle.Bin 路径」的映射，应用无法
//     自己算回去向。**手动还原完全可行**，出口是系统回收站。
//
// 二者都「不可应用内回撤」，但用户的可行动作截然不同，故分别成码。
func undoReasonCode(kind string) string {
	return undoReasonCodeFor(kind, runtime.GOOS)
}

// undoReasonCodeFor 是 undoReasonCode 的平台参数注入版（APP-6 + H6）。
//
// 为什么再拆一层：原因分流若直接读 runtime.GOOS，于是"Windows 才给回收站那一码"
// 这条分支在本机**只能 t.Skip**。平台真值进参数后，三平台的码与判据是否自相矛盾，
// 在任何一台机器上都能一次性断言。
func undoReasonCodeFor(kind, goos string) string {
	// 判据本身问 undoableFor（APP-6）；这里只决定"不可撤的原因是哪一种"。
	if kind == "trash" && !undoableFor(kind, goos) {
		return undoCodeWindowsTrash
	}
	return undoCodePermanentDelete
}

// histSnapshot 在锁内取账本句柄（M9，2026-09-21 全仓审计 §五 9）。
//
// `a.hist` 是受 `a.mu` 保护的字段：`shutdown` 在锁内把它置 nil（Close 之后置空），
// 而绑定层的 RPC 与扫描 goroutine 随时可能正在读它。直接 `if a.hist == nil` 再
// `a.hist.X()` 是**解引用两次**且都在锁外——前者是数据竞争，后者还可能拿到刚被
// 置空的值。取一次快照后所有调用都用 hs，字段本身只读一次、且在锁内读。
//
// 已经在大临界区里取过快照的调用点（ExecuteOperation / UndoOperation 等）不必
// 走这里，注释里的「锁内快照」与此同源。
func (a *App) histSnapshot() *history.Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hist
}

// cchSnapshot 在锁内取哈希缓存句柄（APP-2，2026-09-21 全量审查）。
//
// 与 M9 的 histSnapshot 逐字同形、同一条理由：`a.cch` 由 `a.mu` 保护
// （shutdown 在锁内 Close 并置空），而绑定层入口原先写的是
// `if a.cch == nil {...}; return a.cch.GetStats()`——锁外解引用两次，
// -race 下是一次真竞争（本仓已用探针复现，见 app_cch_race_test.go 的改前读数）。
// M9 的 AST 门禁白名单只管 `a.hist`，所以同一类缺陷的第二例一路漏到本轮。
func (a *App) cchSnapshot() *cache.Cache {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cch
}

// warnBackground 是"后台动作没成，但不影响主流程结论"的统一出口：
// stderr + app:error 双通道，同一句话。
//
// 为什么必须发事件：打包后的 GUI 没有控制台，只写 stderr 等于没写。
// 出口本身只保证一件事：**绝不静默**；"哪一笔、后果是什么"由调用方写进 userMsg。
//
// M58 前只有 warnLedger 一个出口，M58（定位命令失败）复用同一条通道时才发现它
// 把 "[history]" 与"账本写入失败："写死了 —— 直接复用会造出一句假话
// （一条 Finder 启动失败被报成"账本写入失败"）。故抽出本函数，warnLedger 退居一行包装。
func (a *App) warnBackground(tag, userMsg string) {
	fmt.Fprintf(os.Stderr, "[%s] %s\n", tag, userMsg)
	if a.emit != nil && a.ctx != nil {
		a.emit(a.ctx, "app:error", map[string]string{"error": userMsg})
	}
}

// warnLedger 是"账本没写进去"的统一出口（M7，2026-09-21）。
//
// 修正前 FinishItem/FinalizeOp/persistKeepPaths/MarkItemUndo 四处落账失败都只写 stderr，
// 磁盘满时条目停在 planned、`ops:done` 横幅照报"成功"，用户按 09 §6.7
// 「动手之前先写账本」预期可回撤，实际无账本可撤。
//
// 口径与 SaveScan 一致（本函数即从那里抽出），失败原因逐点由调用方写清"哪一笔、
// 后果是什么"。
func (a *App) warnLedger(msg string) {
	a.warnBackground("history", "账本写入失败："+msg)
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
	undoable := undoableFor(op.Kind, runtime.GOOS)
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
	// ★ R2-3（2026-09-23 第四轮全仓审查，设计段 §30.5）：卷语义探测要**往用户目录写探测
	// 文件**，是 I/O，必须落在 opsRunning 置位**之前**。这是这一族的第三处漏网——
	// 预览（AS-R3，:1372）与保留策略（APP-1，:1448）都已是"锁外预热、锁内纯比较"，
	// 而下面的 ApplyProcessPolicy 改前就地实测。它卡住（死挂载）不会锁住 a.mu，
	// 却会把 opsRunning 永久钉成"清理操作执行中"：resetOps 在那条永不返回的 I/O 之后，
	// 此后每次清理（:1823）与每次新扫描（:611）都被拒，CancelOperation 也解不了。
	var resolve ops.SensResolver
	if all := warmDirs(op.ProcessDirs, op.ExcludeDirs); len(all) > 0 {
		resolve = ops.WarmSensitivity(all)
	}
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
	// M61（04 §6.11 APP-13）：S4「永久删除须显式确认」原先只在执行器里
	//（internal/ops/executor.go 的 delete 分支），而本函数的写前账本 beginJournal
	// 在它**上游**。delete 走 undoable=false 那一档，beginJournal 不会因账本问题
	// 拒绝 ⇒ 未确认的删除照样先在 op_records 落一条，执行器整批拒掉后再由
	// FinalizeOp 收成一个"什么都没动"的记录，最后仍走完 ops:done 横幅。
	// 用户从历史页读到的是"做过一次删除"。
	// 现在把这道判断前移到置 opsRunning 之前：拒绝时既不落账、不派发、也不占互斥。
	// 执行器那道**照旧保留** —— Execute 是导出 API，任何调用方都可能绕过 app 层
	// 直接进（既有钉子 TestS4DeleteRequiresConfirm 钉的正是那道）。
	if op.Kind == "delete" && !op.ConfirmDanger {
		a.mu.Unlock()
		return "", fmt.Errorf("永久删除需要显式确认（ConfirmDanger），已拒绝且未写入历史记录")
	}
	// 过滤器收窄（2026-09-20 白名单 / 2026-09-23 功能 2 黑名单）：
	// 实际处理范围 = FileIDs ∩ 优先文件夹 −（若启用）不处理目录。
	//
	// ★ 判据收进 pendingIDsLocked（功能 2 起）：此前这里自带一段
	// ApplyProcessPolicyWith 求交，与预览/清单是三份相似代码——AS-H6 的
	// 温床形状。现在执行/预览/清单同一内核，"将处理 N"与实际处理 N 不可能漂移。
	//
	// 为什么在**这里**取交集，而不是下沉到 ops.Execute：
	//   ① ops.Execute 是纯执行器，不该感知"处理策略"这个业务概念；
	//   ② 它在 len(FileIDs)==0 时会早退——若把过滤下沉，勾选全部落空时
	//      会**静默执行 0 个文件**，用户以为做了其实什么都没做。这正是要避免的。
	//   ③ 写前账本（beginJournal → planOpItems）也用 op.FileIDs，必须在此之前
	//      收窄，否则账本记录的范围超出实际执行范围，账本失真。
	//
	// 为什么挪进锁内（相对 2026-09-20 的位置）：pendingIDsLocked 要求持 a.mu；
	// 且拒绝路径因此落在 opsRunning 置位**之前**——不再有"先占闸再撤闸"的
	// 复位舞蹈，P2 死锁形状从结构上不存在了。resolve 是入口那次锁外预热的
	// 查表版（R2-3）：预热覆盖 dirs+excludeDirs 同一批目录，表必命中，
	// 锁内这一段没有任何写盘 I/O。
	//
	// op 是值传递：op.FileIDs = kept 只改本地副本，前端勾选状态不受影响。
	filtered := len(op.ProcessDirs) > 0 || len(op.ExcludeDirs) > 0
	var filteredMatched, filteredSelected int
	var filteredUnmatched []string
	if filtered {
		filteredSelected = len(op.FileIDs) // 过滤前的勾选数，用于事件与拒绝文案
		kept, _, um := a.pendingIDsLocked(op.ProcessDirs, op.ExcludeDirs, resolve, op.FileIDs)
		if len(kept) == 0 {
			// 交集为空必须**明确拒绝**，不能静默执行 0 项。此刻还没置
			// opsRunning、没建 opCtx——直接放锁返回即可，无需任何复位。
			a.mu.Unlock()
			return "", fmt.Errorf("%s", emptyIntersectionMsg(
				filteredSelected, len(um), len(op.ProcessDirs) > 0, len(op.ExcludeDirs) > 0))
		}
		op.FileIDs = kept
		filteredMatched = len(kept)
		filteredUnmatched = um
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

	// 收窄已在锁内完成（见上）。通知前端本次过滤的实际范围——用事件而不是
	// 改返回值：ExecuteOperation 的返回值是 opID，改签名会波及 Wails 绑定、
	// TS 签名与一批后端测试。事件在派发前发出，前端据此在执行完成横幅里
	// 说明"已选 M 项中 K 项未处理"。
	if filtered {
		if a.emit != nil && a.ctx != nil {
			a.emit(a.ctx, "ops:filtered", map[string]any{
				"matched":   filteredMatched,
				"selected":  filteredSelected,
				"unmatched": filteredUnmatched,
			})
		}
	}

	// 2026-09-18 审查 C3：写前账本改为 fail-closed。原先 BeginOp 失败只发一条事件、
	// journalID 保持 0 后照常移动文件（hist 为 nil 时更是零提示），与手册
	// 09 §6.7「动手之前先把完整计划写入账本」的承诺相反——用户按界面预期
	// 可回撤，实际账本上什么都没有。此处在派发前落盘计划，失败即拒绝执行。
	// 放在锁外是因为 hist 访问绝不与 a.mu 嵌套（见 App.hist 注释）；
	// opsRunning 已在锁内置位，新扫描与新操作在此期间都被拒。
	// resetOps 复位在途标志并释放取消函数。M8：它在下面被调用**两次**——
	// 一次在终止事件之前，一次留给 goTask 的 defer。
	// defer 那一次一定晚于 body 内的 emit（`defer reset()` 注册在 `body()` 之前），
	// 于是前端在 ops:done 回调里立刻 StartScan / ApplyKeepPolicy 会被
	// 「清理操作执行中」误拒。与扫描路径的 resetInFlight 同构（见 :496 注释）。
	// 保留 defer 那份是 panic 展开路径的兜底；两次调用均为幂等。
	resetOps := func() {
		a.mu.Lock()
		a.opsRunning = false
		a.opsCancel = nil
		a.mu.Unlock()
		cancelOp()
	}
	journalID, jerr := a.beginJournal(hs, histID, op, groups, keepIDs)
	if jerr != nil {
		resetOps()
		return "", jerr
	}

	a.goTask("ops", resetOps, func() {
		res := opsExecuteFn(ops.Options{
			Ctx:     opCtx,
			Groups:  groups,
			KeepIDs: keepIDs,
			// 平台能力声明：Windows 的回收站实现会独立复核落位，
			// 故"源消失但无落点"必须按失败上报（见 ops.TrashVerifiesRecycle）。
			TrashVerifiesRecycle: ops.TrashVerifiesRecycle(),
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
					a.warnLedger(fmt.Sprintf("条目收口失败 %s：%v，该条在历史记录中可能仍是「计划中」，回撤入口不完整", r.OrigPath, err))
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
				a.warnLedger(fmt.Sprintf("操作账本收尾失败：%v，残留的「计划中」条目未归位，回撤范围以历史记录为准", err))
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
				// 实占必须同步重算：不清零/不沿用旧值的话，清理掉一半成员后
				// 结果页仍显示清理前的可释放量（同一类"数字比盘上多"）。
				g.ReclaimableActual = model.ReclaimActual(files)
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
				// APP-3（2026-09-21 全量审查）：原先只写 stderr。M7 已裁定过
				// "打包 GUI 没有控制台，只写 stderr 等于没写"，本处是同族漏网。
				// 后果不是崩溃而是账本与盘上不一致：历史行仍列着已删文件，
				// 下次从历史页恢复会得到一批不存在的路径。
				a.warnLedger(fmt.Sprintf("历史裁剪失败（记录 %d）：%v，该条历史仍可能列出已清理的文件，从历史页恢复前请先重扫", histID, perr))
			}
		}
		// M8：终止事件必须在复位之后发。结果集回写已完成（上面解锁那一刻起
		// 本 goroutine 不再改写 a.groups），此后任何新扫描/新操作都与它无关。
		resetOps()
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
	hs := a.histSnapshot()
	if hs == nil {
		return nil, fmt.Errorf("历史库不可用")
	}
	return hs.ListOps()
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
//
// ★ F1⑥（2026-09-23 登记，**不改行为**）：下面那个循环是**逐条同步 Lstat**，
// 条目数没有上限（一笔大批量清理可上千条），且路径取自账本、可能是当初那个
// 网络卷/外置盘。死挂载与拔走的 SMB 上 Lstat 可以长时间不返回 ⇒ 这条 RPC 会拖着
// 记录页一起不返回，而界面上没有取消入口。这与 B2（R2-2）收的"探测听取消"同族，
// 差别在这里要收的是**读侧的逐条 stat**：要么加"只检测前 N 条"、要么给整条 RPC 配
// ctx 与超时（= 改 OpRecordItem 的检测时机/契约），两条都超出一行级小修的范围 ⇒
// 本批只把风险写进注释，改法随批登记为欠账（J-6 取向）。
func (a *App) GetOpRecord(opID int64) (OpRecordDetail, error) {
	hs := a.histSnapshot()
	if hs == nil {
		return OpRecordDetail{}, fmt.Errorf("历史库不可用")
	}
	m, items, err := hs.GetOp(opID)
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
	hs := a.histSnapshot()
	if hs == nil {
		return fmt.Errorf("历史库不可用")
	}
	return hs.ClearOps()
}

// UndoOperation 回撤一条清理记录：入口同步校验（历史库/互斥/记录可撤性），
// 通过后异步逐项执行，进度复用 ops:progress，终止发 ops:undo:done。
// 处理 state=done 与 state=undo_failed 的条目——失败/跳过/取消项本就没动过
// 文件系统，不在此列；而回撤失败的项确实动过、只是没撤成，必须留在批量范围里
// 供用户修正后重试（APP-4；原先只收 done，把文档承诺的那条路堵死了）。
// done 项撤完转 undone，天然幂等。结果集不动：恢复的文件需要重新扫描确认状态。
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
		return "", fmt.Errorf("%s", undoReasonCode(meta.Kind))
	}

	undoID := fmt.Sprintf("undo-%s-%d", time.Now().Format("150405"), a.taskSeq.Add(1))
	a.goTask("ops", release, func() {
		var todo []history.OpItem
		for _, it := range items {
			// APP-4（2026-09-21 全量审查）：undo_failed 也要收。函数文档一直写着
			// "回撤失败的项保持 undo_failed，用户可修正后再次回撤"，单项通道也确实
			// 放行 done || undo_failed，只有这里把失败项永久排除在批量之外——
			// 于是"修好问题再点一次全部回撤"一个条目都不动。
			// undone / undoing / 未执行态仍然排除：那三类要么已撤成、要么没动过文件。
			if it.State == history.StateDone || it.State == history.StateUndoFailed {
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
			restored, uerr := a.undoExecuteItem(hs, meta.Kind, it)
			if uerr != nil {
				res.Failed = append(res.Failed, undoFailure(it, restored, uerr))
				continue
			}
			res.OK++
			res.Restored = append(res.Restored, restored)
		}
		a.emit(a.ctx, "ops:progress", model.OpsProgress{Done: total, Total: total, Current: ""})
		// M8：同 ExecuteOperation——终止事件前必须先复位（release 也留给
		// goTask 的 defer 兜 panic 路径，两次调用幂等）。
		release()
		a.emit(a.ctx, "ops:undo:done", res)
	})
	return undoID, nil
}

// undoFailure 组装一条回撤失败项（批量与单项两条通道共用，I5）。
//
// M86（04 §6.11 OPS-14b）：restored 非空表示"数据已经回到盘上、只是收尾失败"
// （两份并存那一类）。ops 层的三条部分成功路径各自把落点写进了错误文本，
// 但那是**约定**而不是**强制** —— 漏一条就又变成"报错了却不知道数据在哪"。
// 这里兜一层：落点没出现在文本里就补上，app 层不再丢第二次。
//
// Path 一格**保持 OrigPath**：它标识"哪一条账目失败"，换成落点会让失败清单
// 对不上记录明细；落点走 Err（FailedDrawer.vue 原样渲染 Err）。
//
// ★ 第 3 轮 M89（§24.3.1）：这条"落点没出现在文本里就补上、出现了就不重复"的规则
// 收归到 ops.DestHint——执行器的 move 分支要写同一句话，两处各写一遍是 I5 的漂移面。
// 输出串逐字不变（这里就是那两条既有断言的落点），改的只是实现只有一份。
func undoFailure(it history.OpItem, restored string, uerr error) model.FailedItem {
	return model.FailedItem{
		Path:  it.OrigPath,
		Stage: "undo",
		Err:   ops.DestHint(uerr.Error(), restored),
	}
}

// undoOneFn 回撤执行入口的间接引用：测试据此断言"账本先落、文件后动"的先后顺序。
var undoOneFn = ops.UndoOne

// opsExecuteFn 执行器入口的间接引用（与 undoOneFn 同理由）。
// M7 用它把"操作执行到一半账本变得不可写"这一现场（磁盘满 / 句柄失效）做成
// 确定性场景：测试在 body 内先关掉 *history.Store，于是 OnItem 的 FinishItem
// 与随后的 FinalizeOp 必然报错，从返回值上却完全观测不到。
var opsExecuteFn = ops.Execute

// undoExecuteItem 执行单条回撤并落账（批量/单项共用）。写前落账（2026-09-18
// 审查 I6）：先把条目置为 undoing 再动文件系统——原先先移动后落账，中途被杀会让
// 账本永久停在 done，文件其实已回家却显示"未回撤"，重试还必报"已不存在"。
// 收口：成功置 undone，失败置 undo_failed 并保留原因供排查与重试；
// 写前落账失败则拒绝执行（与 C3「无账本不动文件」同口径），调用方据 error 汇总成败。
func (a *App) undoExecuteItem(hs *history.Store, kind string, it history.OpItem) (string, error) {
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
			a.warnLedger(fmt.Sprintf("回撤失败态落库出错 %s：%v，该条可能停留在「回撤中」，重试前请核对文件实际状态", it.OrigPath, merr))
		}
		// M86（04 §6.11 OPS-14b）：restored 非空 = 数据已经回到盘上、只是收尾动作失败
		//（两份并存那一类）。改前这里 `return "", uerr` 把落点就地丢掉，调用方只剩
		// OrigPath 可报，用户不知道文件现在在哪。结构上保留，展示走 FailedItem.Err。
		return restored, uerr
	}
	if merr := hs.MarkItemUndo(it.ID, history.StateUndone, ""); merr != nil {
		a.warnLedger(fmt.Sprintf("回撤成功态落库出错 %s：%v，文件已还原但记录未更新，历史页可能仍显示为可回撤", it.OrigPath, merr))
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
		return "", fmt.Errorf("%s", undoReasonCode(meta.Kind))
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
			restored, uerr := a.undoExecuteItem(hs, meta.Kind, it)
			if uerr != nil {
				res.Failed = append(res.Failed, undoFailure(it, restored, uerr))
			} else {
				res.OK++
				res.Restored = append(res.Restored, restored)
			}
		}
		// M8：单项回撤与批量回撤同构——终止事件前必须先复位。
		release()
		a.emit(a.ctx, "ops:undo:done", res)
	})
	return undoID, nil
}

// OpenTrash 打开系统回收站（M3-T06：恢复引导）。
func (a *App) OpenTrash() error {
	// M58：三平台共用一句失败文案与同一条出口。回收站打不开时用户正需要它
	//（清理完想找回东西），静默等于把恢复引导变成"点了没反应"。
	onExit := func(werr error) {
		a.warnBackground("trash", fmt.Sprintf("打开系统回收站失败：%v", werr))
	}
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return startCmd(exec.Command("open", filepath.Join(home, ".Trash")), onExit)
	case "windows":
		return startCmd(exec.Command("explorer", "shell:RecycleBinFolder"), onExit)
	default:
		root := os.Getenv("XDG_DATA_HOME")
		if root == "" {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, ".local", "share")
		}
		return startCmd(exec.Command("xdg-open", filepath.Join(root, "Trash", "files")), onExit)
	}
}

// ExportReport M5-T01 实现。
func (a *App) ExportReport(format, path string) (string, error) {
	return "", fmt.Errorf("报告导出将在 M5 提供")
}

// CacheStats 缓存统计（M4-T01）。句柄经 cchSnapshot 取，锁外只读快照。
func (a *App) CacheStats() (cache.Stats, error) {
	cch := a.cchSnapshot()
	if cch == nil {
		return cache.Stats{}, fmt.Errorf("缓存不可用")
	}
	return cch.GetStats()
}

// CacheClear 清空缓存（M4-T01）。同上（APP-2）。
func (a *App) CacheClear() error {
	cch := a.cchSnapshot()
	if cch == nil {
		return fmt.Errorf("缓存不可用")
	}
	return cch.Clear()
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
