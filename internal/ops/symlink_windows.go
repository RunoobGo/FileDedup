//go:build windows

package ops

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// ============================================================================
// Windows 平台层：创建文件符号链接
//
// 为什么需要单独一层（不能直接用 os.Symlink）：
//
//  1. Windows 创建符号链接需要 SeCreateSymbolicLinkPrivilege，普通用户默认
//     没有。SDK 提供了带 SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE 标志的
//     路径，但**仅当系统已开启「开发者模式」**时该标志才被接受；否则调用会
//     失败于 ERROR_INVALID_PARAMETER(87) 而不是给出"你需要特权"的提示。
//     正确做法是：先带标志试（开发者模式可直接成功），遇到 87 就去掉标志重试
//     （走特权路径）。os.Symlink 只做其中一条，无法覆盖两种环境。
//
//  2. 失败于 ERROR_PRIVILEGE_NOT_HELD(1314) 时，原始消息只有一句英文
//     "A required privilege is not held by the client"，用户无从下手。
//     这里翻译成可操作的中文指引（提权运行 / 开启开发者模式）。
//
// 对照：硬链接（CreateHardLinkW）**不需要**任何特权，但**不能跨卷**；
// 符号链接能跨卷，代价是需要特权 + 有悬空风险。二者互补，不互相替代。
// ============================================================================

var (
	modkernel32Symlink      = syscall.NewLazyDLL("kernel32.dll")
	procCreateSymbolicLinkW = modkernel32Symlink.NewProc("CreateSymbolicLinkW")
)

const (
	// SYMBOLIC_LINK_FLAG_FILE：目标为文件（0x0 即"非目录"，是默认值）。
	symlinkFlagFile = 0x0
	// SYMBOLIC_LINK_FLAG_DIRECTORY：目标为目录。本应用不创建目录链接。
	symlinkFlagDirectory = 0x1
	// SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE：允许非提权创建。
	// 需要 Windows 10 1703+ **且已开启开发者模式**；否则整个调用以
	// ERROR_INVALID_PARAMETER(87) 失败（标志本身不被认可）。
	symlinkFlagAllowUnprivilegedCreate = 0x2
)

// Win32 错误码（syscall 未导出这两个，按其数值直接构造）。
const (
	errInvalidParameter    = syscall.Errno(87)   // ERROR_INVALID_PARAMETER
	errPrivilegeNotHeld    = syscall.Errno(1314) // ERROR_PRIVILEGE_NOT_HELD
	errFileNotFound        = syscall.Errno(2)    // ERROR_FILE_NOT_FOUND
	errPathNotFound        = syscall.Errno(3)    // ERROR_PATH_NOT_FOUND
	errAlreadyExists       = syscall.Errno(183)  // ERROR_ALREADY_EXISTS
	errNotSupportedByFS    = syscall.Errno(50)   // ERROR_NOT_SUPPORTED（FAT/exFAT 等）
	errAccessDeniedWindows = syscall.Errno(5)    // ERROR_ACCESS_DENIED
)

// ErrSymlinkNeedsPrivilege 表示"环境不具备创建符号链接的权限"。
//
// 单独成类型（而非只给一段文案）：调用方需要把这种情况与"路径冲突、
// 卷不支持"等其他失败区分开，才能给出对的引导——例如在结果页顶部提示
// "以管理员身份重启后重试"，而不是让用户逐条去猜。
var ErrSymlinkNeedsPrivilege = errors.New("创建符号链接需要权限")

// createSymlink 在 linkPath 处创建指向 target 的文件符号链接。
//
// 两段式尝试（见文件头注释）：
//
//	第一段：带 ALLOW_UNPRIVILEGED_CREATE
//	        → 开启开发者模式的机器上直接成功，无需提权
//	        → 未开启时返回 87，进入第二段
//	第二段：不带该标志（经典路径）
//	        → 已提权运行：成功
//	        → 未提权：1314，映射为 ErrSymlinkNeedsPrivilege
//
// 注意第二段**不是**无条件重试：只有第一段明确报了 87 才重试。
// 其他错误（路径已存在、卷不支持、目标路径非法）直接返回，不掩盖真实原因。
//
// target 保存为字符串（与 unix 一致）。传绝对路径，链接才在移动场景下足够稳定。
func createSymlink(target, linkPath string) error {
	tp, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("目标路径无法转换为 Windows 字符串: %w", err)
	}
	lp, err := syscall.UTF16PtrFromString(linkPath)
	if err != nil {
		return fmt.Errorf("链接路径无法转换为 Windows 字符串: %w", err)
	}

	call := func(flags uintptr) (uintptr, error) {
		r, _, e := procCreateSymbolicLinkW.Call(
			uintptr(unsafe.Pointer(lp)),
			uintptr(unsafe.Pointer(tp)),
			flags,
		)
		if r != 0 {
			return 1, nil
		}
		if e == nil {
			return 0, syscall.EINVAL
		}
		return 0, e
	}

	r, err := call(symlinkFlagFile | symlinkFlagAllowUnprivilegedCreate)
	if r == 0 && errors.Is(err, errInvalidParameter) {
		// 该 Windows/策略不认 ALLOW_UNPRIVILEGED_CREATE（未开开发者模式）。
		// 去掉标志走经典特权路径。
		r, err = call(symlinkFlagFile)
	}
	if r != 0 {
		return nil
	}
	return describeSymlinkError(err, linkPath)
}

// describeSymlinkError 把 Win32 错误码翻译成**可操作**的中文提示。
//
// 逐条对应的处置建议（这也是用户实际遇到的四类失败）：
//
//	1314 → 提权运行 / 开启开发者模式
//	87   → 两段都失败了，说明既没开开发者模式也没提权，或系统版本过旧
//	50   → 目标卷不支持符号链接（FAT/exFAT），换 NTFS/ReFS
//	183  → 链接路径已被占用（理论上不该出现，因为调用方先 Remove 过）
//	5    → 被 ACL/安全软件拦截
func describeSymlinkError(err error, linkPath string) error {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return fmt.Errorf("创建符号链接失败（%s）: %w", linkPath, err)
	}
	switch errno {
	case errPrivilegeNotHeld, errInvalidParameter:
		return fmt.Errorf(
			"%w：Windows 要求 SeCreateSymbolicLinkPrivilege 才能创建符号链接。"+
				"请任选其一后重试——① 右键程序图标选「以管理员身份运行」；"+
				"② 在「设置 → 系统 → 开发者选项」中开启「开发者模式」（开启后普通权限即可创建）。"+
				"同卷文件请改用硬链接（不需要任何权限）。原始错误: %v",
			ErrSymlinkNeedsPrivilege, errno)
	case errNotSupportedByFS:
		return fmt.Errorf(
			"创建符号链接失败：目标位置所在卷不支持符号链接"+
				"（FAT/exFAT 不支持，需 NTFS 或 ReFS）: %s", linkPath)
	case errAlreadyExists:
		return fmt.Errorf("创建符号链接失败：路径已被占用（%s）: %w", linkPath, err)
	case errAccessDeniedWindows:
		return fmt.Errorf(
			"创建符号链接失败：访问被拒绝（%s）。"+
				"除权限不足外，也可能是杀毒软件/安全策略拦截了符号链接创建，"+
				"可将本程序加入白名单后重试: %w", linkPath, err)
	case errFileNotFound, errPathNotFound:
		return fmt.Errorf(
			"创建符号链接失败：路径不存在（%s）。"+
				"若链接所在目录已被移除，请先恢复目录再执行: %w", linkPath, err)
	default:
		return fmt.Errorf("创建符号链接失败（%s）: %w", linkPath, err)
	}
}

// symlinkTarget 读取链接中保存的目标路径字符串。
//
// Windows 上 os.Readlink 已能正确读取（Go 内部即用重解析点查询），
// 这里保持与 unix 同名同语义，调用方无需分平台。
func symlinkTarget(linkPath string) (string, error) {
	return os.Readlink(linkPath)
}

// symlinkIsDanglingPlatform 平台实现：Windows 与 unix 判据一致
// （Lstat 是链接 + Stat 打不开），此处仅为接口对齐保留，不再重复平台逻辑。
