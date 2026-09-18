package ops

// Trash 平台回收站统一入口（M3-T03）：
//
//	darwin: osascript → Finder trash（批量单进程，可还原）
//	windows: SHFileOperationW（FOF_ALLOWUNDO，批量）
//	linux: XDG Trash 规范自研（零外部依赖）
//
// 返回 src→dst 尽力映射（v0.5.0 功能 4：回撤账本需要去向）：
// darwin 从 Finder 输出解析，配对失败即放弃（宁缺勿错配）；
// linux 自研实现精确记录；windows 无公开映射接口，恒为空 map，
// 回撤改由「打开系统回收站」引导完成。
// 具体实现见 build tag 分文件。
var Trash func(paths []string) (map[string]string, error) = defaultTrash
