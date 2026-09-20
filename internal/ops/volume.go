package ops

import (
	"os"
	"path/filepath"
)

// sameVolume 判定两个路径是否落在同一卷上。
//
// 用途：软链接合并虽然**允许**跨卷，但同卷时硬链接是严格更优的选择
// （零权限门槛、引用计数保护、无悬空风险）。执行器据此在"同卷却用了软链接"
// 时记一条 Warnings——**只提示不阻断**：用户可能出于某种自身理由坚持用软链接，
// 应用不该替他把这个选择权拿走。
//
// 实现：比较两个路径解析后的根所在位置。做法不是去问平台"卷序列号"
// （那需要 Windows 句柄查询 / unix stat，且两个文件都必须已存在），而是用
// 一个更朴素也更稳的判据：
//
//	把两侧都取到"最近的已存在祖先目录"，再看它们是否同根。
//
// 之所以取祖先目录：软链接合并的场景里 dup 一定存在，keep 也一定存在
// （执行前刚校验过），但为了对调用方宽容，仍做了"向上找已存在目录"的处理，
// 免得一个已消失的路径让整个判定 panic 或误报。
//
// 判定失败（拿不到任何已存在祖先）时返回 true——**保守地认为同卷**，
// 于是只会多打一条"建议用硬链接"的提示，不会把跨卷误判导致漏提示。
// 宁可多提示，不可少提示：少提示的后果是用户不知道有更安全的选项。
func sameVolume(a, b string) bool {
	ra := existingRoot(a)
	rb := existingRoot(b)
	if ra == "" || rb == "" {
		return true // 无法判定 → 保守认为同卷（只影响提示文案）
	}
	return ra == rb
}

// existingRoot 从 p 开始向上找第一个**确实存在**的路径，返回其所在卷根。
//
// 返回 "" 表示一路走到头都没找到已存在的路径（几乎只可能发生在两个路径
// 都是凭空构造的场景）。
func existingRoot(p string) string {
	cur := filepath.Clean(p)
	for {
		if _, err := os.Lstat(cur); err == nil {
			return volumeRoot(cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// volumeRoot 返回路径所在的卷根。
//
// unix："/"（filepath.VolumeName 返回空 → 回退到根分隔符）。
// Windows：盘符形式 "C:"。UNC 路径走 VolumeName 的 "\\host\share" 形式。
//
// 注意这里刻意**不**使用 os.SameFile 或 fsid：本判定只服务于"提示文案选哪句"，
// 不承担任何安全职责。用最朴素、最不依赖平台 API 的方式即可，出错也只影响提示。
func volumeRoot(p string) string {
	if v := filepath.VolumeName(p); v != "" {
		return v
	}
	// unix 无盘符：根卷统一是 "/"
	return string(filepath.Separator)
}
