package ops

import (
	"errors"
	"fmt"
)

// 本文件承载 `SHFileOperationW` 返回码的**中文归因表**（M280）。
//
// 为什么在无 build tag 的文件里：表本身是"数字 → 文案"的纯映射，不含任何平台调用；
// 按本仓库既定纪律（recycle_policy.go / winerrno.go 同一条），纯判定一律下沉，
// 让它进 Linux CI 主门禁。平台真值（那个返回码）由 `trash_windows.go` 的调用点供给。
//
// ★ 收录标准（比"多覆盖几个码"重要）：只收**本仓有据**的码——真机读数（32 那档）
// 或 Win32 错误码表与 Shell 文档一致、且不存在数值重叠的。
// 理由是 `FO_E_*` 与 Win32 码**共用同一批数字**：`FO_E_FILENOTFOUND` 就是 124，
// 而 124 在 Win32 表里是 `ERROR_LEVELS_TOO_DEEP`，两回事；Vista+ 的 Shell 还常改交
// HRESULT（0x8027xxxx）。给说不清的码配上自信的归因，用户就会照着假原因去排查
// ——这与 M279 那次"把已进站的文件报成可能永久删除"是同一种错，只是方向相反。

// shFileOperationReason 把 SHFileOperationW 的返回码翻成可读中文。
//
// 返回串**末尾自带裸码括弧**：本项目所有真机读数都按数字归档（§6.30 / §6.44），
// 译文随版本改，数字是唯一的对表凭据。
func shFileOperationReason(code int64) string {
	switch code {
	case 2:
		return "系统找不到要操作的文件（它可能在本轮扫描之后已被移走或删除，错误码 2）"
	case 3:
		return "系统找不到文件所在的路径（目录可能已被改名或删掉，错误码 3）"
	case 5:
		return "系统拒绝访问：多半是权限不足或文件被设为只读（错误码 5）"
	case 32:
		// 本机最常见的一档：真机读数 80 项混批里 15 项被别的进程占用 ⇒ Shell 交回 32。
		return "文件正被其他程序占用（杀毒扫描、索引器或预览窗口最常见）：" +
			"请关闭正在使用它的程序，或稍后重试（错误码 32）"
	case 33:
		return "文件被其他程序锁定，暂时无法移动（错误码 33）"
	case 112:
		return "目标卷磁盘空间不足，回收站放不下这批文件（错误码 112）"
	default:
		return fmt.Sprintf("系统交回的失败码未收录在本项目的译码表内，因此不猜原因"+
			"（错误码 %d / 0x%X）", code, code)
	}
}

// shFileOperationError 是调用点用的包装（只在返回码非 0 时进这里）。
func shFileOperationError(code int64) error {
	return errors.New(shFileOperationReason(code))
}
