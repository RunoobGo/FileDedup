//go:build !windows

package ops

// trashVerifiesRecycle 报告本平台是否会独立复核回收站落位。
//
// darwin 走 Finder（osascript），落点解析失败时返回空 dstMap 但文件
// **确实**已进回收站；linux 自研 XDG Trash 精确记录落点。
// 两者都不做"回收站条目数"复核，因此"源消失 + 无落点"不能断言失败，
// 维持 C6/S8 旧契约记 Skipped。
//
// 真正做复核的 Windows 见 trashcap_windows.go。
const trashVerifiesRecycle = false

// TrashVerifiesRecycle 导出本平台能力（供 app 层填入 Options）。
func TrashVerifiesRecycle() bool { return trashVerifiesRecycle }
