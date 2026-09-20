package worktemp

import "testing"

// TestIsTempNameRecognizesEveryRegisteredMarker 逐个标记验证：
// markers 里登记的每一项都必须能以**实际生成形态**被认出来。
//
// 存在意义：本包是「产生临时名」（internal/ops）与「忽略临时名」
// （internal/scanner）的**唯一共享定义**。缺陷 6 的成因正是两处各自为政。
// 若日后新增一种临时名却只加在 ops 侧，本测试会指出来。
//
// 2026-09-20（ocr 审查 M1）：判定从 Contains 收紧为"生成形态"后，
// 各标记的合法形态不同——三个后缀标记只会**追加**在名尾（可再带 .undo），
// 插入式只属于 .fdd-restored（插在扩展名之前）。旧测试对后缀标记也断言
// 插入式，等于把误伤用户名的形状钉成了契约，此处按真实生成规则分形断言。
func TestIsTempNameRecognizesEveryRegisteredMarker(t *testing.T) {
	for _, m := range []string{SuffixTmp, SuffixOld, SuffixUndo} {
		names := []string{
			"file.bin" + m,           // 追加式
			"中文名.docx" + m,           // 非 ASCII 文件名
			"no_ext" + m,             // 无扩展名
			"file.bin" + m + ".undo", // 回滚暂存产生的二次后缀
		}
		for _, n := range names {
			if !IsTempName(n) {
				t.Errorf("标记 %q 的形态 %q 未被识别", m, n)
			}
		}
	}
	for _, n := range []string{
		"a" + MarkRestored + ".jpg", // 插在扩展名前（实际形态）
		"a" + MarkRestored,          // 无扩展名文件
	} {
		if !IsTempName(n) {
			t.Errorf("标记 %q 的形态 %q 未被识别", MarkRestored, n)
		}
	}
}

// TestIsTempNameRejectsUserNamesEmbeddingMarkers 用户名只是**包含**标记、
// 但形状并非任何一种生成形态时，不得判为临时名。
//
// 修正前 Contains 一刀切：notes.fdd-old-summary.txt 这类正常文件被扫描器
// 静默跳过；更糟的是同名**目录**会在 IsDir 分支之前被剪掉整棵子树。
func TestIsTempNameRejectsUserNamesEmbeddingMarkers(t *testing.T) {
	negatives := []string{
		"notes.fdd-old-summary.txt", // 标记在中间，非 .fdd-restored 家族
		"report.fdd-old2.xlsx",      // 标记后紧跟其他字符
		"build.fdd-tmp-dir",         // 目录/文件名内嵌后缀标记
		"my.fdd-undo-tmp-files",
		"x.fdd-restoredbackup.txt", // 标记后不是扩展名（不以 "." 开头）
	}
	for _, n := range negatives {
		if IsTempName(n) {
			t.Errorf("%q 不是任何生成形态，不应判为工作临时名", n)
		}
	}
}

// TestIsTempNameDoesNotTouchUserFiles 反向保护：正常用户文件名绝不能被忽略，
// 否则会漏扫用户文件（比残留更严重的错误）。
func TestIsTempNameDoesNotTouchUserFiles(t *testing.T) {
	negatives := []string{
		"a.bin", "报告.docx", "fdd-cli.exe", "fdd备份.bin",
		"fdd.bin", "my.fdd-backup.bin",
		"a.FDD-old", // 大写：不是我们生成的（我们固定小写）
		"temp.txt", ".hidden",
	}
	for _, n := range negatives {
		if IsTempName(n) {
			t.Errorf("%q 是正常文件名，不应被忽略（会导致漏扫）", n)
		}
	}
}

// TestMarkersAreAllLowercaseFddPrefixed 钉住命名约定：我们只生成
// ".fdd-" 小写前缀。判定的大小写敏感性依赖此约定。
func TestMarkersAreAllLowercaseFddPrefixed(t *testing.T) {
	for _, m := range markers {
		if len(m) < 5 || m[:5] != ".fdd-" {
			t.Errorf("标记 %q 应形如 \".fdd-xxx\"（小写前缀）", m)
		}
	}
}
