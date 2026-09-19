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
	ID      uint64 // 扫描任务内自增
	Path    string
	Size    uint64
	ModTime int64 // UnixNano，缓存校验用
	Key     FileKey
	Ext     string // 小写扩展名（含点，如 ".jpg"；无扩展名为空）
}

// Filters 扫描过滤器。
type Filters struct {
	IncludeExts   []string // 空 = 全部，如 ".jpg"
	ExcludeExts   []string
	MinSize       uint64   // 0 = 不限（默认 0）
	MaxSize       uint64   // 0 = 不限
	ExcludePaths  []string // glob：无 "/" 匹配任意路径段；有 "/" 匹配相对路径前缀
	IncludeHidden bool
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
	GroupID     uint64
	Files       []*FileEntry
	Reclaimable uint64   // (n-1)*size，硬链接已在阶段 1.5 剔除
	Hash        [32]byte // 组内容 BLAKE3-256（操作前校验依据，M3）
}

// FailedItem 失败清单条目（扫描与操作共用）。
type FailedItem struct {
	Path  string
	Stage string // scan/prefilter/hash/verify/ops
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
	Kind          string // trash/delete/move/hardlink
	FileIDs       []uint64
	TargetDir     string // move 时生效
	ConfirmDanger bool   // delete 必须显式确认（S4：后端强制）
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
}

// ScanConfig 扫描任务配置。
type ScanConfig struct {
	Roots    []string
	Filters  Filters
	Threads  int
	Paranoid bool // 逐字节确认
	UseCache bool
}
