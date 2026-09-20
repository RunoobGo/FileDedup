//go:build windows

package ops

// trashVerifiesRecycle 报告本平台是否为 Windows。
//
// Windows 的 defaultTrash 在 SHFileOperation 返回 0 之后会用 verifyRecycled
// 独立复核「源已消失」且「该卷回收站条目数增加」（trash_windows.go）。
// 因此当回退逻辑看到"源消失但平台未报告落点"时，可以确信文件**没有**
// 可靠地进入回收站，必须按失败上报，而不是记 Skipped（那会把
// 静默永久删除伪装成无害跳过）。
//
// darwin/linux 保持 C6/S8 旧契约，见 trashcap_other.go。
const trashVerifiesRecycle = true

// TrashVerifiesRecycle 导出本平台能力（供 app 层填入 Options）。
func TrashVerifiesRecycle() bool { return trashVerifiesRecycle }
