//go:build windows

package ads

import (
	"syscall"
	"unsafe"
)

// cStreamNameLen = MAX_PATH(260) + 36，来自 MSDN 的结构声明（设计稿 §5.0 E7）。
const cStreamNameLen = 296

// win32FindStreamData 对应 C 侧的 WIN32_FIND_STREAM_DATA。★ 它是**扁平**的：
//
//	typedef struct _WIN32_FIND_STREAM_DATA {
//	  LARGE_INTEGER StreamSize;
//	  WCHAR         cStreamName[MAX_PATH + 36];
//	} WIN32_FIND_STREAM_DATA;
//
// 动手前我对它的印象是"内嵌一份 WIN32_FIND_DATAW"（那样名字在 offset 52），
// 查 MSDN 才确认名字在 **offset 8**。猜错偏移的表现恰好是最坏的一种：从 offset 52
// 读到的是零长字符串 → "没有任何命名流" → 守卫在 Windows 上恒放行，而判据层是纯
// 函数、测的是名字列表，全套门禁照样全绿。所以这里除尺寸钉之外，还必须有
// ads_windows_test.go 的 V8（真机上第一个流名必须是 "::$DATA"）。
type win32FindStreamData struct {
	StreamSize  int64
	cStreamName [cStreamNameLen]uint16
}

// Win32 ABI 契约：尺寸一偏离，系统调用就会朝 Go 变量里多写/少写字节——少写只是
// 读错字段，多写直接越界踩坏栈。8 + 296*2 = 600，与 C 侧逐字节等值。
// 手法照 internal/fsid/fsid_windows.go:41-53（那里也是先踩过一次布局错）。
var (
	_ [600 - unsafe.Sizeof(win32FindStreamData{})]byte
	_ [unsafe.Sizeof(win32FindStreamData{}) - 600]byte
)

var (
	modkernel32          = syscall.NewLazyDLL("kernel32.dll")
	procFindFirstStreamW = modkernel32.NewProc("FindFirstStreamW")
	procFindNextStreamW  = modkernel32.NewProc("FindNextStreamW")
)

// findStreamInfoStandard 即 STREAM_INFO_LEVELS 的 FindStreamInfoStandard，
// 也是该枚举**唯一**合法取值（E3）。dwFlags 保留，必须为 0。
const findStreamInfoStandard = 0

// enumerateStreams 枚举一个文件的全部数据流，把名字列表与**最后一个 errno** 交给
// 无 tag 层去判。本文件不出现任何"该不该拦"的判断。
//
// 用 syscall.SyscallN 而非仓库既有五处 proc.Call 的理由：这里需要的是**数值形态**的
// errno（交给 Classify），而 .Call 返回的是 error 接口、还要断言回落；SyscallN 直接
// 给 syscall.Errno，且变参长度显式传给 runtime（不存在历史上 Proc.Call 的三参截断问题）。
//
// ★ 真机验证状态（M100 更正）：本文件的胶水层**已经在 CI 的 windows 真机腿跑过**
// （run 35642706382：`ok  filededup/internal/ads  0.021s`），凭据是无 Skip 分支的 V8，
// 它钉的正是下面的结构偏移。darwin/linux 主门禁这一侧仍只有
// `GOOS=windows go vet` 的交叉编译检查 + 尺寸钉。
// 端到端那一半（"有一条真命名流被拦住"= V8b）**仍未兑现**：V8b 有两条合法 Skip
// 出口，而 CI 三条腿都不带 -v，从 `ok` 行分不出真绿还是跳过（M32 保持部分兑现）。
func enumerateStreams(path string) ([]string, uintptr) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		// 含 NUL 等非法路径：归类交给无 tag 层，这里只如实报"参数不合法"。
		return nil, errInvalidParameter
	}

	var data win32FindStreamData
	h, _, errno := syscall.SyscallN(
		procFindFirstStreamW.Addr(),
		uintptr(unsafe.Pointer(p)),
		uintptr(findStreamInfoStandard),
		uintptr(unsafe.Pointer(&data)),
		0,
	)
	if syscall.Handle(h) == syscall.InvalidHandle {
		// 无流可找(38) / 卷不支持流(87) / 文件不存在(2,3) / 拒绝访问(5) 都在这一支
		return nil, uintptr(errno)
	}
	defer syscall.FindClose(syscall.Handle(h))

	var names []string
	for {
		name := syscall.UTF16ToString(data.cStreamName[:])
		names = append(names, name)
		if StreamIsNonDefault(name) {
			// 命中即停，不必枚举完（同一个文件的其余流不影响结论）
			return names, 0
		}
		ok, _, errno := syscall.SyscallN(
			procFindNextStreamW.Addr(),
			h,
			uintptr(unsafe.Pointer(&data)),
		)
		if ok == 0 {
			if errno == errHandleEOF {
				return names, 0 // E8：枚举正常结束
			}
			return names, uintptr(errno)
		}
	}
}
