// Package pathnorm 是"把路径变成可比较的键"这一判据的唯一实现处。
//
// 立这个包的直接原因是 M64（I5-1）：`\`→`/` 的交换在本仓曾有四份实现
// （scanner.keyOf、fscase.Fold、sysguard.normalize、filter.toSlashPat），
// 四份对"`\` 在非 Windows 上算不算分隔符"取径并不一致。同一判据多份实现的实际
// 代价已经发生过一次：M26（04 §6.8.8）就是"折叠串与前缀串的分隔符口径不统一"
// 导致 Windows 上判重分支恒不成立，而 darwin/Linux 上两种写法恰好同值、测不出来。
// 根包的 TestSlashSwapHasSingleImplementation 钉住本包是唯一实现处，防第五份长出来。
//
// 本包只 import strings、无 build tag。sysguard 因此仍然满足"纯字符串判据、
// 在 Linux 主门禁上可执行 Windows 保留名断言"——分层约定允许的唯一例外，
// 就是这类无依赖、无副作用的叶子包（04 §6.8.0 约束 5）。
package pathnorm

import "strings"

// Slash 把 sep 这一个字符统一换成 "/"，返回可直接做前缀比较的键。
//
// sep 由**调用点**负责给，这是本包唯一的分歧口，两种给法各有成立理由：
//   - string(filepath.Separator)：遍历器产生的路径确实按平台分隔符拼接，
//     非 Windows 上 "\" 是合法文件名字符，不能当成结构。
//   - 字面 "\\"：跨平台清单（sysguard 的保护条目）与用户手输的排除模式必须在
//     任一 GOOS 上都认 Windows 写法，否则 Linux CI 里的 Windows 保护断言会
//     静默失效（sysguard 改动前的注释就写明了这一条，现由参数显形）。
//
// 传错的后果不是 panic 而是"判据在某个平台上悄悄换了作用面"，所以调用点要写明理由。
// sep 为空串时原样返回：strings.ReplaceAll(p, "", "/") 会在每个字符间插入 "/"，
// 那是一个没人会想要的结果，这一条守卫是必须的而不是防御性的。
//
// 无命中时原样返回：这是分配优化（visitKey 在遍历热路径上每个目录走一次），不是判据。
func Slash(p, sep string) string {
	if sep == "" || !strings.Contains(p, sep) {
		return p
	}
	return strings.ReplaceAll(p, sep, "/")
}

// Under 判断 child 是否就是 parent 本身、或位于其下。两个参数都必须是 Slash 的产物。
// 键空间里恒为 "/"，故前缀一律用 "/" 拼——**这里不许出现平台分隔符**。
//
// parent 为空串时按"整卷/全盘"理解（以 "/" 开头的 child 即为真）。ops.keep 的
// 保留目录判据依赖这一条：它把 Clean 过的目录再剥掉唯一那个 "/"，盘根就成空串，
// 语义是"该卷下全部"。改动这条等于改那个判据。
func Under(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// TrimTailKeepRoot 去掉键尾部的 "/"，但把盘根 "/" 本身留着。
//
// 为什么需要它：清单条目 "/System/" 与 "/System" 必须同义，否则 Under 会判否；
// 而一律 TrimRight 会把盘根 "/x" 前的 "/" 吃光、把根条目变成空串，
// 于是"保护盘根"与"什么都不保护"长得一模一样。
//
// 注意与 ops.keep 的 TrimSuffix(...,"/") **不是**同一条判据：那边的输入已过
// filepath.Clean（至多一个尾斜杠，且盘根要故意剥成空串），这边输入是用户原样条目。
// 两种前置条件不同的规则不该合并成一份。
func TrimTailKeepRoot(p string) string {
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}
