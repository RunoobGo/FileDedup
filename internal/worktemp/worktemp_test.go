package worktemp

import "testing"

// TestIsTempNameRecognizesEveryRegisteredMarker 逐个标记验证：
// markers 里登记的每一项都必须能被认出来。
//
// 存在意义：本包是「产生临时名」（internal/ops）与「忽略临时名」
// （internal/scanner）的**唯一共享定义**。缺陷 6 的成因正是两处各自为政。
// 若日后新增一种临时名却只加在 ops 侧，本测试会指出来。
func TestIsTempNameRecognizesEveryRegisteredMarker(t *testing.T) {
	for _, m := range markers {
		names := []string{
			"file.bin" + m,           // 追加式
			"中文名.docx" + m,           // 非 ASCII 文件名
			"a" + m + ".jpg",         // 插入式（.fdd-restored 的实际形态）
			"no_ext" + m,             // 无扩展名
			"file.bin" + m + ".undo", // 二次后缀（.fdd-old.undo）
		}
		for _, n := range names {
			if !IsTempName(n) {
				t.Errorf("标记 %q 的形态 %q 未被识别", m, n)
			}
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
