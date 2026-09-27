//go:build !darwin && !linux && !windows

package ops

import "errors"

// defaultTrash 其他平台（freebsd 等 unix 变体）：本平台没有实现，如实报错，不碰任何文件。
//
// M301（取向：统一成兜底式，兜底腿显式 fail-closed，别把归因推给编译器）。
// 改前本包只有 `trash_windows.go` / `trash_darwin.go` / `trash_linux.go` **三份枚举式
// tag**，而无 tag 的 `trash.go:14` 直接引用 `defaultTrash` ⇒ 第四平台上
// `GOOS=freebsd GOARCH=amd64 go build ./internal/...` 交回的是
// `internal\ops\trash.go:14:61: undefined: defaultTrash`——一条编译器口径的
// 链接期错误，既没告诉用户"这台机器上回收站功能不可用"，也没告诉维护者"缺的是哪条腿"。
// 本腿把它换成一句人话（与 `internal/fsid/fsid_other.go` 的兜底式风格同形）。
//
// 取向 = fail-closed，**绝不静默成功**：调用方（含回撤账本、批量删除）把
// "空映射 + nil error"读成"这些路径已经进回收站了"。在本平台上那是假话，
// 而假话的下游后果很硬——用户以为还能回撤，实际一个文件都没动，或者被别的
// 删除路径按永久删除处理。所以这里选择"什么都不做 + 明确报错"。
//
// 若本平台将来要支持：按 `trash_xdg.go` 的 FreeDesktop 规范自研，或接平台原生
// 回收站接口，另开一条 `//go:build freebsd` 的枚举腿；**不要**把本腿改成静默成功。
func defaultTrash(paths []string) (map[string]string, error) {
	return nil, errors.New(
		"回收站操作在本平台尚未实现（当前支持 windows / darwin / linux），已取消：没有删除任何文件",
	)
}
