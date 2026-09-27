//go:build windows

package ops

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Win32 建硬链接失败码 → 中文归因。
//
// 真值取自 Win32 错误码表：
//   - ERROR_INVALID_FUNCTION = 1：卷的文件系统**根本不实现**"建硬链接"这个请求。
//     ★ 这是 exFAT / FAT32 上的**实测值**（M273，W5-3），而 docs/05 那一格原本预期的是 50；
//   - ERROR_NOT_SUPPORTED  = 50：预期里的那一个，一并接住（ReFS 与部分过滤驱动走这支）；
//   - ERROR_NOT_SAME_DEVICE= 17：跨卷（同一常数已住在 crossdevice_windows.go，复用它）；
//   - ERROR_ACCESS_DENIED  = 5：权限/安全软件拦下。
const (
	winErrorInvalidFunction = syscall.Errno(1)
	winErrorNotSupported    = syscall.Errno(50)
	winErrorAccessDenied    = syscall.Errno(5)
)

// hardlinkLinkError 把 `os.Link` 的失败翻成"归因覆盖卷型"的错误。
//
// ★ 为什么要单独一个函数（而不是在调用点拼一句了事）：改前的括号里只有「可能跨卷或权限」，
// 在**同卷、非 NTFS**这一形状下是误导，而"哪种卷支持硬链接"这一句真话当时挂在
// `verifyHardlinked`（`move.go` 收尾复核）那一支——失败发生得更早，那一支根本走不到。
// 现在把真话搬到产生处，并按 errno 分派，避免"三种可能全说一遍"的敷衍。
//
// ★ 兜底必须**承认不知道**：认不出的码把三个候选都列出来，不许挑一个说死
// （与 M289/M270 的"三态如实"同族——把猜测当读数，比标"未知"更坏）。
func hardlinkLinkError(err error) error {
	var le *os.LinkError
	if errors.As(err, &le) {
		if errno, ok := le.Err.(syscall.Errno); ok {
			switch errno {
			case winErrorInvalidFunction, winErrorNotSupported:
				return fmt.Errorf("硬链接失败（该卷不支持硬链接：Windows 上只有 NTFS 提供，"+
					"exFAT/FAT/ReFS 一律拒绝，与跨卷和权限无关）: %w", err)
			case winErrorNotSameDevice:
				return fmt.Errorf("硬链接失败（两个路径不在同一卷，硬链接只能连同卷文件）: %w", err)
			case winErrorAccessDenied:
				return fmt.Errorf("硬链接失败（权限不足或被安全软件拦截）: %w", err)
			}
		}
	}
	return fmt.Errorf("硬链接失败（可能是两者不在同一卷、该卷不支持硬链接（Windows 仅 NTFS 支持）、或权限被拦）: %w", err)
}
