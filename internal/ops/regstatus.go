package ops

import (
	"errors"
	"fmt"
)

// 本文件承载注册表读数的**错误分类**，刻意与 winreg_windows.go 分开。
//
// 为什么下沉（AS-R5，2026-09-20 全仓审计）：回收站预检的三态判定
// （nukeOff / nukeOn / nukeUnknown）里，只有 nukeOn 会拒绝操作，
// 所以「读不到」绝不能被归类成「读到并且没设过」。这个分界原本是纯逻辑，
// 却整段住在 `//go:build windows` 文件里——Linux CI 主门禁一行都跑不到，
// 而 winNukeStatusOf 用的是泛化 fall-through：新增一类错误时不会被任何
// 测试提醒。把判据抽到本文件后，分类表可在任意平台断言。

// regErrNotFound 即 ERROR_FILE_NOT_FOUND：RegOpenKeyExW（键不存在）与
// RegQueryValueExW（值不存在）都返回它。★ 这是「用户没设过」这一**正常
// 情形**与「读不到」（ACL 拒绝、类型不符）的唯一分界，必须与后者区分：
// 前者可判 nukeOff，后者只能判 nukeUnknown。
//
// OPS-9（2026-09-21 全量审查，I5）：数值此前在本包**另有两套命名**
// （本文件的 Errno(2)/裸 Errno(3) 与 symlink_windows.go 的具名表）。
// 现在统一指向 winerrno.go（无 build tag），本文件不再自带数字。
const regErrNotFound = errFileNotFound

// errRegTypeMismatch 值类型不是 REG_DWORD（registryGetDWORD 专用）。
var errRegTypeMismatch = errors.New("注册表值类型不是 REG_DWORD")

// regIsNotFound 判定注册表错误是否为「键/值不存在」。
// 注：ERROR_PATH_NOT_FOUND(3) 在父键不存在时出现，同样属于"没有这个设置"。
func regIsNotFound(err error) bool {
	return errors.Is(err, regErrNotFound) || errors.Is(err, errPathNotFound)
}

// regIsMismatch 判定 registryGetDWORD 是否因值类型不符而失败。
func regIsMismatch(err error) bool {
	return errors.Is(err, errRegTypeMismatch)
}

// classifyRegErr 把一次注册表读取的失败映射为三态。入参必须是**非 nil** 的
// 错误（读成功由调用方自行按值判定 nukeOn / nukeOff）。
//
// ★ 类型不符单列，且在 not-found 之前判：regIsMismatch 此前有定义无调用，
// 与 ACL 拒绝一起被泛化分支吞掉。两者返回值恰好相同（都只能 unknown），
// 所以单列不是为了改变行为，而是为了让「哪类错误走哪支」成为被测试钉住的
// 显式契约——若有人把类型不符并进 not-found 那一支，就会把「用户手工写了
// 个字符串值」读成「用户没设过策略」→ nukeOff → 放行 → 整批永久删除。
func classifyRegErr(err error) nukeStatus {
	switch {
	case regIsMismatch(err):
		return nukeUnknown // 值在、类型不对：不能替他猜开还是关
	case regIsNotFound(err):
		return nukeOff // 键/值确实不存在 = 用户没改过设置（全新账户常态）
	default:
		return nukeUnknown // ACL 拒绝、未知错误码：一律按"无法判定"
	}
}

// regTypeErrorf 包装类型不符错误（registryGetDWORD 用）。
// 单独成函数只为让"%w 包装 + regIsMismatch 可判"这一契约留在同一份文件里。
func regTypeErrorf(wantType, gotType uint32) error {
	return fmt.Errorf("%w: 期望 REG_DWORD(%d)，实际类型 %d", errRegTypeMismatch, wantType, gotType)
}
