package ops

import (
	"os"
	"path/filepath"
	"strconv"

	"filededup/internal/fsid"
)

// sameVolume 判定两个路径是否落在同一卷上。
//
// 用途：软链接合并虽然**允许**跨卷，但同卷时硬链接是严格更优的选择
// （零权限门槛、引用计数保护、无悬空风险）。执行器据此在"同卷却用了软链接"
// 时记一条 Warnings——**只提示不阻断**：用户可能出于某种自身理由坚持用软链接，
// 应用不该替他把这个选择权拿走。
//
// **契约：本函数只服务于提示文案的选句，禁止参与任何执行判据。**
// 它是"尽力而为 + 判不了就保守返回 true"的判据，两侧都可能出错（卷标识取不到
// 时一律回落同卷）。需要"真的跨卷就绝不能删源"那种安全职责的是 move.go 的
// 复制-校验-身份复核链（AS-H4），不是这里。（2026-09-20 全仓审计 M5 登记此契约，
// 并由 TestSameVolumeNotUsedForExecutionDecisions 钉住唯一调用点。）
//
// 主判据：就近已存在祖先的**平台卷标识**（volumeIDOf，unix 为 st_dev、Windows
// 为卷序列号）。M5 修的就是"unix 上只看根名，恒为 / → 跨挂载点恒判同卷"。
// 卷标识取不到时才回落到根卷名（volumeRoot）；仍判不了则保守返回 true。
// 宁可多提示，不可少提示：少提示的后果是用户不知道有更安全的选项。
func sameVolume(a, b string) bool {
	ia, oka := volumeIDOfExisting(a)
	ib, okb := volumeIDOfExisting(b)
	if oka && okb {
		return ia == ib
	}
	ra := existingRoot(a)
	rb := existingRoot(b)
	if ra == "" || rb == "" {
		return true // 无法判定 → 保守认为同卷（只影响提示文案）
	}
	return ra == rb
}

// volumeIDOf 是平台卷标识的测试接缝：复用 fsid 而不是在本包再写一份 stat/syscall。
// 本仓对"平台物理身份怎么取"只允许有一份实现（I5 那类"两份实现在边界上选出不同
// 结果"的事故形态），fsid 已把 Windows 的句柄查询与 FAT/exFAT 的"卷不提供稳定
// 索引"一起处理好了。
//
// 用 FromPath（跟随链接）而非 FromPathNoFollow：要的是"按这条路径打开会落到哪个
// 卷"，链接自身所在的卷没有意义。
//
// 未解析（FAT/exFAT、freebsd 等）按"取不到"处理、交给上层回落——拿恒等于 0 的
// Dev 去比会把所有文件判成同卷，恰好复刻 M5。
// 接缝必须可注入：CI 上造不出第二块磁盘，跨挂载点的正例只能靠注入。
var volumeIDOf = func(p string) (string, bool) {
	id, err := fsid.FromPath(p)
	if err != nil || !id.Resolved {
		return "", false
	}
	return strconv.FormatUint(id.Dev, 16), true
}

// volumeIDOfExisting 从 p 向上找第一个能取到**已解析**卷标识的路径并返回该标识。
//
// 逐级向上是因为 dup/keep 通常已存在、但调用方也可能传入刚被删掉的路径；
// 与 existingRoot 同一套宽容策略，免得一个已消失的路径让判定 panic。
func volumeIDOfExisting(p string) (string, bool) {
	cur := filepath.Clean(p)
	for {
		if id, ok := volumeIDOf(cur); ok {
			return id, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

// existingRoot 从 p 开始向上找第一个**确实存在**的路径，返回其所在卷根。
//
// 返回 "" 表示一路走到头都没找到已存在的路径（几乎只可能发生在两个路径
// 都是凭空构造的场景）。
//
// 仅作 sameVolume 的回落分支使用：卷标识拿不到时才用这里的粗判据。
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
// unix：**恒为 "/"**——这正是 M5 的成因：单靠根名分不出挂载点，所以跨卷判定
// 必须走 volumeIDOf（st_dev），不能走这里。
// Windows：盘符形式 "C:"。UNC 路径走 VolumeName 的 "\\host\share" 形式。
//
// 注意这里刻意**不**使用 os.SameFile 或 fsid：本函数只在卷标识拿不到时兜底，
// 不承担任何安全职责（见 sameVolume 的契约段）。
func volumeRoot(p string) string {
	if v := filepath.VolumeName(p); v != "" {
		return v
	}
	// unix 无盘符：根卷统一是 "/"
	return string(filepath.Separator)
}
