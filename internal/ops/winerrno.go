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
	// ERROR_INVALID_FUNCTION = 1：exFAT/FAT32 上 CreateHardLinkW 的实测返回码
	// （M273，docs/05 W5-3）——该卷的文件系统根本不实现"建硬链接"这个请求。
	errInvalidFunction     = syscall.Errno(1)    // ERROR_INVALID_FUNCTION
	errInvalidParameter    = syscall.Errno(87)   // ERROR_INVALID_PARAMETER
	errPrivilegeNotHeld    = syscall.Errno(1314) // ERROR_PRIVILEGE_NOT_HELD
	errFileNotFound        = syscall.Errno(2)    // ERROR_FILE_NOT_FOUND
	errPathNotFound        = syscall.Errno(3)    // ERROR_PATH_NOT_FOUND
	errAlreadyExists       = syscall.Errno(183)  // ERROR_ALREADY_EXISTS
	errNotSupportedByFS    = syscall.Errno(50)   // ERROR_NOT_SUPPORTED（FAT/exFAT 等）
	errAccessDeniedWindows = syscall.Errno(5)    // ERROR_ACCESS_DENIED
)

// gleIsFailure 判一次 Win32 调用交回的 GetLastError 是否表示**真失败**（M295 同族判据）。
//
// ★ 不能写 `err != nil`：`LazyProc.Call` / `syscall.SyscallN` 的第三个返回值是把 gle
// **装箱**成 error，而 Go 在 gle==0 时也给出非 nil 的 `syscall.Errno(0)`（字符串是
// "The operation completed successfully."，真机读数见 winerrno_symlink_outcome_test.go
// 头部引的 b7g 探针记档）⇒ `err != nil` 判失败是**恒真式**，判失败只能取数值。
// 非 Errno 的非 nil error 按失败处理（fail-closed：说不清来历的理由不许当"没失败"）。
//
// ★ 与 `internal/realbytes` 里同名那份**刻意不合并**：合并要么让 ops 依赖 realbytes
// （功能嫉妒——建软链接的代码不该去借"实占字节"那一包），要么新建叶子包（扩面，
// 且与本文件 :17-18 对 internal/ads 第三套的处置同案）。两份之间以本注释互指，
// 改动判据时两侧都要跟上——这也是本仓对"两处同源"的一贯记档法。
func gleIsFailure(err error) bool {
	if err == nil {
		return false
	}
	e, ok := err.(syscall.Errno)
	return !ok || e != 0
}

// symlinkCallOutcome 把 CreateSymbolicLinkW 交回的（返回值, 装箱的 gle）归成两态：
// 成功 ⇒ `(1, nil)`；失败 ⇒ `(0, 理由)`（M289）。
//
// ★ 判成功只能 `== 1`，不能 `!= 0`：该 API 返回的是 BOOLEAN，而**失败时 RAX 里是残值**
// ——真机读数（过滤令牌臂 + 开发者模式关）两段全是 `r=1280 / gle=1314 / after_lstat=missing`
// （链接根本没建）。修前写的是 `if r != 0 { return 1, nil }`，于是 1280 被读成 TRUE，
// `createSymlink` 交回 nil，调用侧直接进 `verifySymlinked`，用户看到的失败原因是
// 「保留源在校验后被替换，已拦截（S1）」——一个"没有权限建软链接"的环境事实被报成了
// "有人在校验瞬间替换了你的源文件"，而 describeSymlinkError 那句已经写好的
// 「以管理员身份运行 / 开启开发者模式」指引一次都没走到。
//
// 失败而 gle 也给不出理由时交 `syscall.EINVAL`：这一支替掉了修前的 `if e == nil`——
// 装箱后的 Errno(0) 永不为 nil，那一支从来不会走，gle=0 的失败被原样交下去，
// describeSymlinkError 落到兜底臂、用户只看见一句"操作成功完成"。
func symlinkCallOutcome(r uintptr, errno error) (uintptr, error) {
	if r == 1 {
		return 1, nil
	}
	if !gleIsFailure(errno) {
		return 0, syscall.EINVAL
	}
	return 0, errno
}
