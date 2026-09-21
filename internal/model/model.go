// Package model 定义 FileDedup 全部核心数据结构（与 docs/01 §7 一致）。
package model

// FileKey 平台物理文件标识抽象（硬链接识别）。
// unix 平台遍历时由 lstat 顺带填充；Windows 按需解析（仅候选组内）。
type FileKey struct {
	VolumeID  uint64 // 设备号 / 卷序列号
	FileIndex uint64 // inode / NTFS FileId
	Resolved  bool   // 是否已解析（未解析时不参与硬链接比较）
}

// FileEntry 单个文件条目。
type FileEntry struct {
	ID   uint64 // 扫描任务内自增
	Path string
	Size uint64 // 逻辑大小（st_size）：缓存键、预筛分桶、扩展名/大小过滤的口径
	// Actual/ActualKnown 是磁盘实占（M6-P2）。与实际内容的正确性无关，
	// 因此**不进缓存表**——它只是"清掉能腾多少"的计量，不参与任何判定。
	//
	// ActualKnown=false 表示平台读不到实占（非 NTFS 卷、FUSE 报 0、句柄失效），
	// 此时 Actual 回退为 Size 作下限参考。界面**必须**区分"实占未知"与
	// "实占恰好等于/为 0"，否则一个未知文件会被显示成不占空间。
	// Actual 可以大于 Size（块粒度对齐、预分配），不得封顶。
	Actual      uint64
	ActualKnown bool
	ModTime     int64 // UnixNano，缓存校验用
	Key         FileKey
	Ext         string // 小写扩展名（含点，如 ".jpg"；无扩展名为空）
}

// Filters 扫描过滤器。
//
// 没有 JSON tag：字段名就是线名（前端 wails.ts 的 Filters 按 PascalCase 镜像）。
// 新增布尔位一律把**安全侧留成零值**——AllowCloudHydration 因此是"允许"而非"禁止"：
// 旧 settings.json、旧历史行、以及前端尚未跟进的 emptyFilters() 三种情形都解成
// false＝跳过占位，不需要任何迁移代码（设计稿 §4.2）。
type Filters struct {
	IncludeExts   []string // 空 = 全部，如 ".jpg"
	ExcludeExts   []string
	MinSize       uint64   // 0 = 不限（默认 0）
	MaxSize       uint64   // 0 = 不限
	ExcludePaths  []string // glob：无 "/" 匹配任意路径段；有 "/" 匹配相对路径前缀
	IncludeHidden bool
	// AllowCloudHydration=true 时**照常读取云端占位文件**（即隐式触发按需下载），
	// 且不计数。默认 false＝跳过并计 SkippedCloudFiles。
	AllowCloudHydration bool
}

// TaskStatus 任务状态机取值。
type TaskStatus string

const (
	StatusIdle         TaskStatus = "Idle"
	StatusScanning     TaskStatus = "Scanning"     // 阶段 0：元数据扫描
	StatusPrefiltering TaskStatus = "Prefiltering" // 阶段 1/1.5/2：分组与预筛
	StatusHashing      TaskStatus = "Hashing"      // 阶段 3：全量哈希
	StatusDone         TaskStatus = "Done"
	StatusPaused       TaskStatus = "Paused"
	StatusCancelled    TaskStatus = "Cancelled"
	StatusFailed       TaskStatus = "Failed"
)

// legalTransitions 状态机合法转换表（01 §3.1）。
var legalTransitions = map[TaskStatus][]TaskStatus{
	StatusIdle:         {StatusScanning},
	StatusScanning:     {StatusPrefiltering, StatusPaused, StatusCancelled, StatusFailed},
	StatusPrefiltering: {StatusHashing, StatusPaused, StatusCancelled, StatusFailed},
	StatusHashing:      {StatusDone, StatusPaused, StatusCancelled, StatusFailed},
	StatusPaused:       {StatusScanning, StatusPrefiltering, StatusHashing, StatusCancelled}, // 恢复回原阶段
	StatusDone:         {StatusIdle},
	StatusCancelled:    {StatusIdle},
	StatusFailed:       {StatusIdle},
}

// ValidateTransition 校验 from→to 是否合法（测试矩阵依据）。
func ValidateTransition(from, to TaskStatus) bool {
	for _, s := range legalTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// ProgressEvent scan:progress 事件载荷（01 §7.1）。
type ProgressEvent struct {
	Stage      string // scan / prefilter / hash / verify
	FilesDone  uint64
	FilesTotal uint64 // 预估，未知为 0
	BytesDone  uint64
	BytesTotal uint64
	SpeedBps   float64
	ETASeconds int64 // 未知为 -1
}

// StageEvent 阶段切换事件载荷。
type StageEvent struct {
	Stage string
	Desc  string
}

// DuplicateGroup 最终重复组。
type DuplicateGroup struct {
	GroupID uint64
	Files   []*FileEntry
	// Reclaimable 逻辑口径：(n-1)*size，硬链接已在阶段 1.5 剔除。
	// 这是**历史表 reclaimable 列的既有语义**，也是既往清理数字的口径，
	// 因此 M6-P2 引入实占后**不换语义**，改为并列新增 ReclaimableActual
	// （理由见 docs/superpowers/specs/2026-09-21-m6-implementation-design.md §3.2）。
	Reclaimable uint64
	// ReclaimableActual 实占口径：组内**冗余成员**（不含保留项）的 Actual 之和。
	// 成员实占未知时该项按逻辑大小计入（保守，不虚报为 0）——因此
	// ReclaimableActual 与 Reclaimable 在"全部未知"时相等，而不是更小。
	ReclaimableActual uint64
	Hash              [32]byte // 组内容 BLAKE3-256（操作前校验依据，M3）
}

// ActualBytes 该条目在实占口径下的计账字节数（M6-P2）。
//
// 未知（ActualKnown=false）一律退回逻辑大小，**绝不按 0 计**：按 0 计会让
// "平台读不到实占"的重复组显示成不占空间，而这正是本轮在修的那类数字失真。
// 历史恢复的条目、以及未经扫描构造的条目（Actual 为零值）都走这条回退。
func (e *FileEntry) ActualBytes() uint64 {
	if e.ActualKnown {
		return e.Actual
	}
	if e.Actual > 0 {
		return e.Actual
	}
	return e.Size
}

// AnyActualKnown 组内是否**至少有一个**成员读到过实占（M6-P2）。
// 全 false 时实占数字纯为逻辑口径回退，展示层必须显示"未统计"而不是那个数。
// 定义在此而非各调用点：界面、CLI、历史三处若各写一遍，"未知"与"为 0"
// 迟早会在一处被混掉。
func (g *DuplicateGroup) AnyActualKnown() bool {
	for _, f := range g.Files {
		if f.ActualKnown {
			return true
		}
	}
	return false
}

// ReclaimActual 组内**冗余成员**的实占之和。约定 files[0] 为保留项，与
// Reclaimable 的 (n-1) 口径逐字对齐——把保留项计进来会把可释放空间凭空
// 多报一整份文件（变异 M-P2-c 钉住这一点）。
func ReclaimActual(files []*FileEntry) uint64 {
	if len(files) < 2 {
		return 0
	}
	var sum uint64
	for _, f := range files[1:] {
		sum += f.ActualBytes()
	}
	return sum
}

// FailedItem 失败清单条目（扫描与操作共用）。
type FailedItem struct {
	Path  string
	Stage string // scan/prefilter/hash/verify/ads/ops
	Err   string
}

// KeepPolicy 一键保留策略。
// directory 策略的 Directories 为有序优先级列表：靠前目录优先保留。
type KeepPolicy struct {
	Kind        string // newest/oldest/shortest/directory/manual
	Directories []string
}

// OpRequest 清理操作请求。
type OpRequest struct {
	Kind          string // trash/delete/move/hardlink/symlink
	FileIDs       []uint64
	TargetDir     string // move 时生效
	ConfirmDanger bool   // delete 必须显式确认（S4：后端强制）

	// ProcessDirs 处理策略：仅本次操作生效的「优先处理的文件夹」过滤器。
	//
	// 非空时，实际处理范围 = FileIDs ∩ {位于 ProcessDirs 任一目录下的文件}。
	// 空 = 未启用，走与新增本字段之前完全一致的代码路径（向后兼容）。
	//
	// 与保留策略的关系：处理策略只能让操作**做得更少**，不会让保留项被动。
	// 保留项在执行器里是硬拒绝的（"保留文件不可操作"），所以两者不可能冲突——
	// 冲突时"保留策略优先"是结构性的，不依赖额外裁决逻辑。
	//
	// 为什么放在请求里而不是 App 的状态字段：它是"这一次操作的限定条件"，
	// 不是会话级状态。随请求传参天然同步，不需要额外的 set/get 配对调用，
	// 也就不会出现"前端设了但没生效"或"上次设的还在"这类状态漂移。
	ProcessDirs []string
}

// OpsProgress 操作进度事件载荷。
type OpsProgress struct {
	Done    int
	Total   int
	Current string
}

// OpsResult 操作结果事件载荷。
type OpsResult struct {
	OK        []string
	Failed    []FailedItem
	Skipped   []string // 操作时文件已不存在（ENOENT）
	Cancelled []string // 取消后未派发（P2：使操作可中止且结果可解释）
	Reclaimed uint64
	// LinkedBytes 硬链接合并涉及的字节数（2026-09-19 新增，与 Reclaimed 互斥）。
	//
	// 语义区分很重要：Reclaimed 是**已经**从磁盘释放的字节（trash/delete/
	// move 出卷）；而硬链接只是把数据块变为多路径共享，**当期并不释放空间**，
	// 真正释放发生在最后一个链接被删除时。混在一起会让 UI 报出"释放 X"，
	// 而用户查看文件夹占用发现毫无变化——自相矛盾且像是操作失败。
	LinkedBytes uint64
	// SymlinkedBytes 软链接合并涉及的字节数（2026-09-20 新增）。
	//
	// 软链接与硬链接的磁盘效果**不同**，故必须与 LinkedBytes 分开：
	//   - 硬链接：数据块转为共享，当期**不释放**空间（Reclaimed 里不该有其贡献）；
	//   - 软链接：dup 位置只剩一个很小的链接对象，被 dup 占用的那**整份数据**
	//     确实从磁盘上消失了（只有保留项那一份还在）——所以从"占用减少了多少"
	//     的角度看它更像 Reclaimed。
	//
	// 之所以仍不并入 Reclaimed：软链接是"指向路径的替身"，一旦保留项被删除/
	// 移动/所在磁盘被拔出，原路径就失效（悬空）。把它与"文件真正被清掉、
	// 空间确实腾出来了"记在同一栏，会让 UI 无从提示悬空风险。
	// 前端据此分别表述（见 frontend/src/views/ResultView.vue）。
	SymlinkedBytes uint64
	// Warnings 为「操作已成功、但有需要用户知晓的情况」的文字说明
	// （2026-09-19 新增）。首个使用场景：硬链接合并完成后，原独立副本
	// (*.fdd-old) 因被杀软/索引器占用而删除失败，残留于用户目录。
	// 这类情况不应把操作计为失败（链接确实已建立），但也绝不能静默——
	// 残留文件若不告知，用户下次扫描会看到莫名多出的"重复文件"。
	//
	// 2026-09-20 追加场景：软链接合并（同卷建议改用硬链接；权限不足摘要）。
	Warnings []string
}

// ScanConfig 扫描任务配置。
type ScanConfig struct {
	Roots    []string
	Filters  Filters
	Threads  int
	Paranoid bool // 逐字节确认
	UseCache bool
}
