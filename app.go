// app.go Wails 绑定服务：01 §3.2 API 契约实现（04 M2-T02）。
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"filededup/internal/cache"
	"filededup/internal/dedup"
	"filededup/internal/model"
	"filededup/internal/ops"
)

// AppVersion 当前版本（GetVersion 契约，04 附录 A：随里程碑递增）。
const AppVersion = "0.4.0-m4"

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

	opsRunning   bool // 清理操作执行中（互斥：拒绝并发操作/保留策略，与 goroutine 写结果集互斥）
	scanInFlight bool // 扫描 goroutine 在途（互斥新扫描：防旧任务收尾写结果集覆盖新任务）

	cfgDir string
	cch    *cache.Cache
}

// NewApp 创建绑定服务。
func NewApp() *App {
	a := &App{
		pipe:      dedup.New(),
		byID:      make(map[uint64]*model.FileEntry),
		viewCache: make(map[sortKey]viewCacheEntry),
	}
	// 引擎回调 → 事件桥（M2-T03）
	a.pipe.OnProgress = func(ev model.ProgressEvent) {
		a.mu.Lock()
		a.lastEvs = ev
		a.mu.Unlock()
		wruntime.EventsEmit(a.ctx, "scan:progress", ev)
	}
	a.pipe.OnStage = func(ev model.StageEvent) {
		wruntime.EventsEmit(a.ctx, "scan:stage", ev)
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
		if cch, err := cache.Open(filepath.Join(a.cfgDir, "cache.db")); err == nil {
			a.cch = cch
			a.pipe = a.pipe.WithCache(cch)
		}
	}
	wruntime.EventsEmit(a.ctx, "app:ready", AppVersion)
}

// shutdown Wails 生命周期：释放缓存句柄。
func (a *App) shutdown(ctx context.Context) {
	if a.cch != nil {
		_ = a.cch.Close()
		a.cch = nil
	}
}

// ---------- 生命周期（§5.5 语义） ----------

// SelectDirectory 调起系统目录选择器。
// 注：Wails v2 目录选择为单选（平台对话框限制），前端可连续多次添加。
func (a *App) SelectDirectory() (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择扫描目录",
	})
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
	a.scanInFlight = true
	a.groups = nil
	a.byID = make(map[uint64]*model.FileEntry)
	a.keepIDs = nil
	a.failed = nil
	a.invalidateViewCacheLocked() // Y3：新任务清空旧视图
	a.mu.Unlock()

	taskID := time.Now().Format("20060102-150405")
	go func() {
		defer func() {
			a.mu.Lock()
			a.scanInFlight = false
			a.mu.Unlock()
		}()
		start := time.Now()
		groups, failed, err := a.pipe.Run(a.ctx, cfg)
		elapsed := time.Since(start)
		if err != nil {
			if a.pipe.Status() == model.StatusCancelled {
				wruntime.EventsEmit(a.ctx, "scan:cancelled", ScanSummary{Elapsed: elapsed.String()})
			} else {
				wruntime.EventsEmit(a.ctx, "scan:error", map[string]string{"error": err.Error()})
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
		a.mu.Unlock()
		wruntime.EventsEmit(a.ctx, "scan:done", ScanSummary{
			Groups:      len(groups),
			Reclaimable: reclaim,
			FilesFailed: len(failed),
			Elapsed:     elapsed.String(),
		})
	}()
	return taskID, nil
}

// PauseScan 暂停（运行态生效）。
func (a *App) PauseScan() error { a.pipe.Pause(); return nil }

// ResumeScan 恢复。
func (a *App) ResumeScan() error { a.pipe.Resume(); return nil }

// CancelScan 取消。
func (a *App) CancelScan() error { a.pipe.Cancel(); return nil }

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

var imageMime = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp", ".svg": "image/svg+xml",
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
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", e.Path)
	case "windows":
		cmd = exec.Command("explorer", "/select,", e.Path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(e.Path))
	}
	return startCmd(cmd)
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
	if s.Threads < 0 {
		s.Threads = 0
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
// 全程持锁：遍历 groups 须与操作 goroutine 的结果集清理写互斥。
func (a *App) ApplyKeepPolicy(policy model.KeepPolicy) ([]ops.KeepDecision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.opsRunning {
		return nil, fmt.Errorf("清理操作执行中，请稍后再应用保留策略")
	}
	if len(a.groups) == 0 {
		return nil, fmt.Errorf("暂无结果集")
	}
	decisions := ops.ApplyKeepPolicy(a.groups, policy)
	a.keepIDs = make(map[uint64]bool, len(decisions))
	for _, d := range decisions {
		a.keepIDs[d.KeepID] = true
	}
	return decisions, nil
}

// ClearKeepDecisions 清除保留决策（回到默认建议）。
func (a *App) ClearKeepDecisions() {
	a.mu.Lock()
	a.keepIDs = nil
	a.mu.Unlock()
}

// ExecuteOperation 操作执行器（M3-T02~T06）：
// 校验 → 执行（回收站/永久删除/移动/硬链接）→ 事件反馈 → 结果集清理。
// 互斥：执行期间拒绝再次操作（两组 goroutine 并发写结果集会互相覆盖）。
func (a *App) ExecuteOperation(op model.OpRequest) (string, error) {
	a.mu.Lock()
	if a.opsRunning {
		a.mu.Unlock()
		return "", fmt.Errorf("上一个清理操作仍在执行中")
	}
	groups := a.groups
	keepIDs := a.keepIDs
	failed := a.failed
	a.mu.Unlock()
	if len(groups) == 0 {
		return "", fmt.Errorf("暂无结果集")
	}
	if s := a.pipe.Status(); s != model.StatusDone {
		return "", fmt.Errorf("任务未完成（当前 %s）", s)
	}
	// 状态与快照校验通过后置位（goroutine 结束时复位）
	a.mu.Lock()
	if a.opsRunning { // 双检：并发窗口内另一调用已置位
		a.mu.Unlock()
		return "", fmt.Errorf("上一个清理操作仍在执行中")
	}
	a.opsRunning = true
	a.mu.Unlock()

	opID := time.Now().Format("ops-150405")

	go func() {
		defer func() {
			a.mu.Lock()
			a.opsRunning = false
			a.mu.Unlock()
		}()
		res := ops.Execute(ops.Options{
			Groups:  groups,
			KeepIDs: keepIDs,
			OnProgress: func(done, total int, current string) {
				wruntime.EventsEmit(a.ctx, "ops:progress", model.OpsProgress{Done: done, Total: total, Current: current})
			},
		}, op)
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
		wruntime.EventsEmit(a.ctx, "ops:done", res)
	}()
	return opID, nil
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
