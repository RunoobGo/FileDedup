package ops

import "syscall"

// Win32 系统错误码的**全包唯一**命名表（OPS-9，2026-09-21 全量审查，I5：一处判定一处实现）。
//
// 修正前两件事同时存在：
//   - 同一批错误码在 ops 包里有两套命名——本表（原来只住在 symlink_windows.go 里）
//     和 regstatus.go 的 regErrNotFound / **裸** syscall.Errno(3)；
//   - 具名表住在 `//go:build windows` 文件里，于是非 Windows 平台连"这两个数是不是
//     同一个意思"都无从断言，regstatus.go 才被迫另起一套。
//
// 本文件刻意**不带 build tag**：syscall.Errno 在所有平台都存在，数值是 Win32 协议常量
// 而非本机内核值，因此两侧的错误分类可在任意平台被测试覆盖（与 H6 同一条纪律：
// 平台判定写成无 tag 的纯函数/纯常量，平台真值由调用侧注入）。
//
// 注：internal/ads 包另有第三套（同为 2/3/87，但是 int 不是 Errno）。跨包收归需要
// 新建一个叶子包来承载，属扩面，本轮按 §1 约束 5 登记不实施。
const (
	errInvalidParameter    = syscall.Errno(87)   // ERROR_INVALID_PARAMETER
	errPrivilegeNotHeld    = syscall.Errno(1314) // ERROR_PRIVILEGE_NOT_HELD
	errFileNotFound        = syscall.Errno(2)    // ERROR_FILE_NOT_FOUND
	errPathNotFound        = syscall.Errno(3)    // ERROR_PATH_NOT_FOUND
	errAlreadyExists       = syscall.Errno(183)  // ERROR_ALREADY_EXISTS
	errNotSupportedByFS    = syscall.Errno(50)   // ERROR_NOT_SUPPORTED（FAT/exFAT 等）
	errAccessDeniedWindows = syscall.Errno(5)    // ERROR_ACCESS_DENIED
)
