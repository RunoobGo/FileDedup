package ops

// Trash 平台回收站统一入口（M3-T03）：
//   darwin: osascript → Finder trash（批量单进程，可还原）
//   windows: SHFileOperationW（FOF_ALLOWUNDO，批量）
//   linux: XDG Trash 规范自研（零外部依赖）
// 具体实现见 build tag 分文件。
var Trash = defaultTrash
