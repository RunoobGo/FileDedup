package ops

// Trash 平台回收站统一入口（M3-T03）：
//
//	darwin: osascript → Finder trash（批量单进程，可还原）
//	windows: SHFileOperationW（FOF_ALLOWUNDO，批量）
//	linux: XDG Trash 规范自研（零外部依赖）
//
// 返回 src→dst 尽力映射（v0.5.0 功能 4：回撤账本需要去向）：
// darwin 从 Finder 输出解析，配对失败即放弃（宁缺勿错配）；
// linux 自研实现精确记录；
// windows 自 **M279** 起**有**映射，但不是从 `SHFileOperationW` 拿的（它确实不给公开出参）：
// 事后**只读反查 `$I` 索引**（`recycle_index.go:164` 的 `collectRecycledDestinations`、
// 取数在 `:192` 的 `scanRecycleBin`），并且在 `defaultTrashLocked` 里把填表 defer 在
// `r0` 判定**之前**（`trash_windows.go:288` 的 `fillRecycledDst` × `:323` 的取数）——
// 因为 `r0=32` 那类"部分已进站即错误返回"的出口，全靠这份先取到的读数才不把已进站项
// 记成数据丢失。★ 本行原写"windows 无公开映射接口，恒为空 map，回撤改由「打开系统回收站」
// 引导完成"，那是 M279 之前的旧话，2026-10-01 第九轮修订批 E6 改口。
// ★ 边界照旧如实：反查是**尽力**而为——`$I` 已被系统清掉、卷不可读或配不到"本轮"条目时
// 交回空表（fail-closed，见 `recycle_index_test.go` 的 M279 格），所以 windows 侧的下落
// 可能在账本上显示为"未记录"，这不等于该文件没进回收站。
// 具体实现见 build tag 分文件。
var Trash func(paths []string) (map[string]string, error) = defaultTrash
