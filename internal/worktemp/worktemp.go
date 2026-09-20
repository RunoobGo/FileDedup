// Package worktemp 定义应用自身产生的「工作临时文件名」及其唯一判定入口。
//
// 为什么单独成包（2026-09-19）：
// 本判定的两个使用者分处不同层——产生临时名的是 internal/ops（硬链接合并、
// 回撤），而需要忽略临时名的是 internal/scanner（遍历时跳过）。二者若直接
// 互相引用会形成依赖环（scanner → ops → ... → scanner）。因此把这份**纯字符串
// 规则**下沉到不依赖任何内部包的最小叶子包，两边共同引用同一份定义。
//
// 这份单一来源本身就是缺陷 6 的修复要点：此前「产生」与「忽略」各自为政，
// 新增一种临时名时忘记同步忽略规则，残留文件便会被当作正常文件参与
// 重复分组——用户看到「做过的文件重扫又变重复」。
package worktemp

import "strings"

// 各类工作临时文件的后缀/标记。命名规则统一为「用户文件名 + 标记」，
// 因此**不以 "." 开头**，扫描器的隐藏文件跳过规则对它无效——
// 这正是必须显式登记并忽略它们的原因。
const (
	// SuffixTmp 硬链接合并的临时硬链接（指向保留源）。
	SuffixTmp = ".fdd-tmp"
	// SuffixOld 合并前 dup 的原始独立副本。
	//
	// 注意：这是**修复残留问题的关键角色**——它与被保留的文件内容
	// 逐字节相同，一旦残留就必然在下次扫描中构成"重复组"。
	SuffixOld = ".fdd-old"
	// SuffixUndo 回撤（undoHardlink）使用的临时名后缀。
	SuffixUndo = ".fdd-undo-tmp"
	// MarkRestored 回收站回撤「原位被占」时插入的标记
	// （形如 name.fdd-restored.ext，故为"标记"而非"后缀"）。
	MarkRestored = ".fdd-restored"
)

// form 一种工作临时名的**生成形态**。
type form struct {
	// mark 我们写进文件名里的标记（恒为 ".fdd-" 小写前缀）。
	mark string
	// inserted 标记的位置：true 插在扩展名之前（name.fdd-restored.ext），
	// false 追加在整个名字之后（name.ext.fdd-old）。
	inserted bool
}

// forms 是本包**唯一**的注册表：新增一种临时名只在这里登记一次，
// IsTempName 直接由它派生。
//
// ★ 2026-09-20（AS-R2 全仓审计）：此前这里有两份清单——markers（四项，
// 但除测试外无人使用）与 suffixMarkers（三项，判定实际用的那份）。
// "只登记一次"的不变量被自己破了：中枢列表 markers 反成死列表，而它承诺的
// 第四种形态靠另一段代码特判。多一份清单就多一次"登记了却认不出"，
// 与 I5（同一判据两份实现）是同一类事故形态。现在形态与标记同源。
var forms = []form{
	{mark: SuffixTmp},
	{mark: SuffixOld},
	{mark: SuffixUndo},
	{mark: MarkRestored, inserted: true},
}

// IsTempName 判定一个「文件名」（不含目录部分）是否为应用自身的工作临时名。
// 扫描阶段应据此忽略，避免残留污染重复分组。
//
// 2026-09-20（ocr 审查 M1）：由 Contains 收紧为**生成形态**判定。
// Contains 会把 `notes.fdd-old-summary.txt`、`build.fdd-tmp-dir` 这类
// 只是内嵌标记的用户名（含目录名）判为临时名——扫描器在 IsDir 分支之前
// 调用它，一个目录名就能让整棵子树静默漏扫。
//
// 生成形态一共三类（逐条见 forms 与各 case 的注释）：
//   - 后缀式：用户文件名 + .fdd-tmp/.fdd-old/.fdd-undo-tmp（回滚暂存会再叠
//     一层 .undo）；
//   - 插入式：仅 .fdd-restored，插在扩展名之前，即标记之后要么到名尾、
//     要么以 "." 开头接扩展名；
//   - ★ AS-R1：以上形态再经 ops.uniqueDst 的重名递增后，`_N` 会插在扩展名
//     之前（a.fdd-restored.bin → a.fdd-restored_1.bin）。这一档以前漏登记，
//     恢复产物因此重新参与重复分组——缺陷 6 的复发形态。
//
// 判定大小写敏感：我们生成时固定用小写 ".fdd-"；用户若真有 "x.FDD-old"，
// 那不是我们产生的，不应替他忽略。
func IsTempName(name string) bool {
	// 回滚失败暂存的二次后缀：X.fdd-old.undo → 先剥掉 .undo 再按形态判定
	n := strings.TrimSuffix(name, ".undo")
	for _, f := range forms {
		if !f.inserted {
			if strings.HasSuffix(n, f.mark) {
				return true
			}
			continue
		}
		// 插入式：标记之后必须是「名尾 / .扩展名 / _序号.扩展名」之一。
		// 只认这三种收尾，是为了不放宽成 Contains（见上方 M1 说明）。
		i := strings.Index(n, f.mark)
		if i < 0 {
			continue
		}
		if rest := n[i+len(f.mark):]; rest == "" || isExtStart(rest) || isNumberedExt(rest) {
			return true
		}
	}
	return false
}

// isExtStart 标记之后直接接扩展名（".jpg"、无扩展名时为空串）。
func isExtStart(rest string) bool { return rest == "" || rest[0] == '.' }

// isNumberedExt 标记之后是 uniqueDst 的递增序号再接扩展名："_1.jpg"、"_12"。
//
// 必须**纯数字 + 可选的 .扩展名**，否则 "a.fdd-restored_backup.txt"
// 这类用户文件会被连带忽略（漏扫比残留更难被发现）。
func isNumberedExt(rest string) bool {
	if !strings.HasPrefix(rest, "_") {
		return false
	}
	rest = rest[1:]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return false
	}
	tail := rest[digits:]
	return tail == "" || tail[0] == '.'
}
