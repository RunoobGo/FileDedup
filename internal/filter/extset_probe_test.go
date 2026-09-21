package filter

// FLT-1（2026-09-21 全量审查，设计稿 §15.0-A）。
//
// newExtSet 原先只做 ToLower，而 has() 拿到的是 `filepath.Ext` 的产物（**带点**）。
// 用户在界面上写 "tmp"（GUI 与 CLI 都只 trim 空格、不补点）⇒ "tmp" != ".tmp"
// ⇒ 这条排除**整条 fail-open**：界面上登记着、盘上一个都没排、也不报错。
// 承诺与结果相反，且方向是"多留文件"，比多删更安全但同样是不真话。
//
// 修法落点在**用户输入侧**的 normalizeExtList（由 Compile 调用），不在 newExtSet：
// 后者的契约是"入参已是 .ext 形态，只做小写与选表"，在它里面补点会让
// filter_test.go 的 TestExtSetEquivalence 那份逐位等价基准反过来钉住错误语义
// （同一说法见 filter.go:39-46）。一侧收口 ⇒ GUI/CLI 不必各自补点，也不会两边规则分叉（I5）。
// ★ M128（第 2 轮 §23.10）：本段此前写的是"修法只在 newExtSet 一处"，与同包
// filter.go 的说法正好相反；代码是对的、话术是旧的，故只改话术、不动判据。

import (
	"testing"

	"filededup/internal/model"
)

func TestExtSetNormalizesMissingDot(t *testing.T) {
	cases := []struct {
		name  string
		list  []string
		input string
		want  bool // Apply 的期望值（false = 被排除）
	}{
		{"排除写法无点", []string{"tmp"}, "a.tmp", false},
		{"排除写法有点", []string{".tmp"}, "a.tmp", false},
		{"排除带空格", []string{"  tmp  "}, "a.tmp", false},
		{"大写输入", []string{"TMP"}, "a.tmP", false},
		{"未列出的扩展名仍放行", []string{"tmp"}, "a.log", true},
	}
	for _, c := range cases {
		f := &model.Filters{ExcludeExts: c.list, IncludeHidden: true}
		if got := Compile(f).Apply(c.input, c.input, 10); got != c.want {
			t.Errorf("%s：Apply(%q) = %v, want %v（排除列表 %q）", c.name, c.input, got, c.want, c.list)
		}
	}
}

func TestExtSetNormalizesIncludeList(t *testing.T) {
	f := &model.Filters{IncludeExts: []string{"jpg"}, IncludeHidden: true}
	m := Compile(f)
	if !m.Apply("a.jpg", "a.jpg", 10) {
		t.Error("包含列表归一后必须仍能放行 .jpg")
	}
	if m.Apply("a.png", "a.png", 10) {
		t.Error("包含列表外的一条必须仍被排除（归一不得放宽判定）")
	}
}

// 负控制：全空/全空格的列表按"未配置"处理，不得退化成"一个都不放行"。
func TestExtSetBlankEntriesAreInactive(t *testing.T) {
	f := &model.Filters{IncludeExts: []string{"  ", ""}, IncludeHidden: true}
	m := Compile(f)
	if !m.incExt.inactive() {
		t.Error("空白项应视为未配置（inactive），否则包含列表会把全部文件挡掉")
	}
	if !m.Apply("a.jpg", "a.jpg", 10) {
		t.Error("未配置包含列表时必须放行任意扩展名")
	}
}

// 负控制：多点输入（archive.tar.gz）取 filepath.Ext 的末段，不得被前缀误伤。
func TestExtSetDoesNotMatchInnerDots(t *testing.T) {
	f := &model.Filters{ExcludeExts: []string{"gz"}, IncludeHidden: true}
	m := Compile(f)
	if m.Apply("archive.tar.gz", "archive.tar.gz", 10) {
		t.Error(".tar.gz 的末段是 .gz，应当被排除")
	}
	if !m.Apply("archive.tar.bz2", "archive.tar.bz2", 10) {
		t.Error("模式 gz 不得匹配 .bz2（归一不得放宽成子串匹配）")
	}
}
