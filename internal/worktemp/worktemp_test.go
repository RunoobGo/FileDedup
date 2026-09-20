package worktemp

import (
	"strings"
	"testing"
)

// TestIsTempNameRecognizesEveryRegisteredMarker 逐个**注册表条目**验证：
// forms 里登记的每一种形态，都必须能以实际生成的名字认出来。
//
// 存在意义：本包是「产生临时名」（internal/ops）与「忽略临时名」
// （internal/scanner）的**唯一共享定义**。缺陷 6 的成因正是两处各自为政。
//
// ★ 2026-09-20（AS-R2 全仓审计）：本用例此前把标记清单**又抄了一遍**
// （硬编码三项后缀 + 单独一段插入式），于是注册表漏登记一种形态时测试照样绿——
// 一份只在自己内部自洽的清单，证明不了实现覆盖了登记项。
// 现在改为遍历 forms：新增形态若没被 IsTempName 认出来，这里立刻红。
func TestIsTempNameRecognizesEveryRegisteredMarker(t *testing.T) {
	stems := []string{"file.bin", "中文名.docx", "no_ext"}
	for _, f := range forms {
		for _, stem := range stems {
			var names []string
			if f.inserted {
				// 插入式：标记在扩展名之前，并覆盖 claimDst 的 _N 递增档
				ext := extOf(stem)
				base := strings.TrimSuffix(stem, ext)
				names = []string{
					base + f.mark + ext,
					base + f.mark + "_1" + ext,
					base + f.mark + ext + ".undo", // 回撤暂存叠在扩展名之后
				}
			} else {
				names = []string{
					stem + f.mark,           // 追加式
					stem + f.mark + ".undo", // 回滚暂存产生的二次后缀
				}
			}
			for _, n := range names {
				if !IsTempName(n) {
					t.Errorf("注册表条目 %+v 的生成形态 %q 未被识别——登记与判定已经分叉", f, n)
				}
			}
		}
	}
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i:]
	}
	return ""
}

// TestIsTempNameRecognizesDedupNumberedForms AS-R1（2026-09-20 全仓审计）：
// claimDst（原 uniqueDst）的重名递增会把 `_N` 插在扩展名**之前**，于是
// `a.fdd-restored.bin` 的第二次恢复落成 `a.fdd-restored_1.bin`——
// 判定收紧为"生成形态"后这一档不再被认出，恢复产物重新参与重复分组，
// 正是缺陷 6 的复发形态（用户看到"刚恢复的文件又变重复"）。
func TestIsTempNameRecognizesDedupNumberedForms(t *testing.T) {
	positives := []string{
		"a.fdd-restored_1.bin",         // claimDst 对 a.fdd-restored.bin 递增
		"a.fdd-restored_12.jpg",        // 序号多位
		"中文名.fdd-restored_3.docx",      // 非 ASCII
		"a_1.fdd-restored",             // 无扩展名时序号落在标记之前（现形即认得）
		"photo.jpg.fdd-restored_2.jpg", // 保留源自身带点号
	}
	for _, n := range positives {
		if !IsTempName(n) {
			t.Errorf("%q 是 undoTrash 实际能产出的名字，必须判为工作临时名（否则残留会重新参与重复分组）", n)
		}
	}

	// 负例：序号形态不得把普通用户名卷进来。
	negatives := []string{
		"a.fdd-restoredx_1.bin", // 标记后紧跟其他字母，不是扩展名
		"a.fdd-restored_1x.bin", // 序号不是纯数字
		"a.fdd-restored_.bin",   // 有下划线无序号
		"photo_1.jpg",           // 完全无关的带序号用户文件
	}
	for _, n := range negatives {
		if IsTempName(n) {
			t.Errorf("%q 不是生成形态，不应判为工作临时名（会漏扫用户文件）", n)
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
//
// 遍历 forms（唯一注册表）而不是另一份手抄清单——AS-R2 的教训：
// 清单抄第二遍的时候，第二份就会开始漂移。
func TestMarkersAreAllLowercaseFddPrefixed(t *testing.T) {
	for _, f := range forms {
		m := f.mark
		if len(m) < 5 || m[:5] != ".fdd-" {
			t.Errorf("标记 %q 应形如 \".fdd-xxx\"（小写前缀）", m)
		}
	}
}
