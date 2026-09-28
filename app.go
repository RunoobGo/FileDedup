// app.go Wails 绑定服务：01 §3.2 API 契约实现（04 M2-T02）。
//
// M336（2026-09-28 第七轮审查批）：本文件只留**跨簇共享**的东西——契约视图类型、
// 包级常量/变量、非方法的辅助函数。67 个 `func (a *App)` 绑定方法已按职责簇移到：
//
//	app_lifecycle.go  生命周期与进程内务（startup/shutdown/…）
//	app_scan.go       扫描控制与目录选择
//	app_result.go     结果集查询、授权与保留策略
//	app_reveal.go     预览与系统揭示
//	app_ops.go        清理执行与回撤
//	app_history.go    扫描历史与操作记录
//	app_settings.go   设置、版本、导出与缓存维护
//
// 移动是**纯机械**的：方法名与签名一字未改。Wails 绑定按方法名解析（与所在文件
// 无关），所以这次拆分对前后端契约、调用方与既有测试都是零影响。
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// cacheQuarantineNotice 把"哈希缓存影像损坏已隔离重建"翻成界面文案（纯函数）。
//
// 与 cacheUnavailableNotice 同形（出了什么事 / 在哪个文件上 / 对用户意味着什么），
// 但两者互斥：那一条是"根本没开成"，这一条是"开成了、旧表整表作废"。
// 后果层必须说准——丢失的是**缓存行**不是用户数据：去重结果不受影响，
// 变的只是本次运行的速度（每条候选都要重算一遍，首扫尤其明显）。
func cacheQuarantineNotice(quarantined string) string {
	return "哈希缓存影像损坏，已隔离为 " + filepath.Base(quarantined) + " 并重建（旧缓存整表作废），" +
		"本次运行的每次扫描都要重新计算，速度会变慢；不影响去重结果。" +
		"旧文件仍在配置目录，需要时可手工查看。"
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

// ---------- 生命周期（§5.5 语义） ----------

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

// ---------- 结果查询 ----------

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

// execRevealCmd 是路径类外部命令的执行缝（M155 卷型注入缝同族）。
//
// 抽出来只为一个理由：RevealPath/OpenPath 的用例不许真的弹出 Finder/文件管理器
// 窗口——那是劫持用户的桌面。生产值恒为 startCmd，测试期整只替换成"只记录不启动"。
// 卷型缝收在 mu 里面（M155 的教训：注入点必须自身线程安全），这里没有那个问题：
// 缝只在绑定方法入口处读一次，读与用之间不跨锁、不跨 goroutine。
var execRevealCmd = startCmd

// windowsPathBase 按 **Windows 的分隔符语义**取路径末段：`/` 与 `\` 同为分隔符，
// 尾部多余的分隔符先剥掉（`C:\a\explorer\` → `explorer`，与 Windows 上的 `filepath.Base` 同值）。
//
// ★ 为什么不用 `filepath.Base`：它认的是**宿主机**的分隔符。这里服务的判据是"注入
// goos=windows 时按 Windows 语义裁决"，而 unix 宿主上 `filepath.Base` 对
// `C:\Windows\explorer.exe` 是 no-op（整串原样返回）⇒ 那一格在 darwin/linux 腿恒读
// false，只有 Windows 主机读得到 true，注入平台参数换来的"三平台同测"就成了空话。
func windowsPathBase(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// revealExitSilent 报告「这条外部定位命令的非零退出能不能当作成功」。
//
// M288（04 §6.50 W4-5 真机定案）：Windows 上 `explorer.exe` **成功也返回 1**。
// 真机四个定位动作（三条路径 + 一次回收站）误弹 4/4、真失败 0/4，而 OS 侧
// （四扇 CabinetWClass 顶层窗口同时在场、标题逐条对得上）证明窗口真的开起来了。
// ⇒ 在这条通道上 explorer 的退出码**不含成败信息**（失败回 1，成功也回 1），
// 拿它当判据造出的正是"狼来了"：真失败时那条 toast 与假告警逐字同形。
//
// 平台真值经参数注入（APP-6 同族）：判据在任一主机上都能把三平台各跑一遍，
// 不必等 Windows CI。⇒ 取基名**不许**用 filepath.Base：它在非 Windows 平台
// 不认 `\`，`C:\Windows\explorer.exe` 会被整串留下（M312：unix CI 腿上
// "生产形态：解析后的绝对路径" 这一臂当场红）。这里显式按两种分隔符切，
// Windows 真机上的解析结果（exec.Command 的 LookPath 产物）行为不变。
func revealExitSilent(goos string, cmd *exec.Cmd) bool {
	if goos != "windows" {
		return false
	}
	// 先转小写再剥 .exe：TrimSuffix 本身大小写敏感，而 Windows 上
	// `C:\Windows\EXPLORER.EXE` 与裸名 `explorer` 是同一个程序。
	// ★ 取末段走 windowsPathBase 而不是 filepath.Base——后者认宿主机分隔符。
	return strings.TrimSuffix(strings.ToLower(windowsPathBase(cmd.Path)), ".exe") == "explorer"
}

// checkRevealPath 校验前端回送的路径，返回规整后的绝对化入参与 stat 结果。
//
// 失败清单里的路径是"后端算出 → 前端展示 → 原样送回"的一圈往返，属于新的信任
// 边界（ID 走不到它们：失败项压根没进 byID）。所以两条硬规则先于任何装配执行：
//   - 空白路径拒掉：空的 open 调用在 darwin 上会打开"当前目录"的 Finder，
//     属于用户没要求的跳转；
//   - stat 不到的路径拒掉：失败清单里躺着的多半正是"访问不了"的东西，
//     把 ENOENT 原样抛回去，比弹一个空窗或让外部工具静默失败有用。
func checkRevealPath(raw string) (string, os.FileInfo, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", nil, fmt.Errorf("路径为空")
	}
	info, err := os.Lstat(p)
	if err != nil {
		return "", nil, fmt.Errorf("路径不存在或无法访问（%s）：%v", p, err)
	}
	return p, info, nil
}

// openCmd 组装"打开这个路径本身"的命令（文件交给默认应用，目录开窗口）。
// 与 revealCmd 的区别只有一条：不带选中参数。Linux 同样要探测，否则
// 命令不存在时报的是"exec 失败"，用户分不清是没装工具还是操作失败。
func openCmd(path string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path), nil
	case "windows":
		return exec.Command("explorer", path), nil
	}
	for _, c := range []struct {
		exe  string
		args []string
	}{
		{"xdg-open", []string{path}},
		{"gio", []string{"open", path}},
	} {
		if _, err := exec.LookPath(c.exe); err == nil {
			return exec.Command(c.exe, c.args...), nil
		}
	}
	return nil, fmt.Errorf("未找到可用的打开命令（xdg-open/gio），无法打开 %s", path)
}

// ---------- 设置（单一事实源，01 §7.3） ----------

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
