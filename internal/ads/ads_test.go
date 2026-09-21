package ads

import "testing"

// V1（设计 §5.4）：判据层全部落在无 tag 文件里，所以这些断言在 Linux/darwin
// 主门禁上就执行——Windows 真机只负责证明"胶水递上来的名字是对的"（V8）。
func TestStreamIsNonDefault(t *testing.T) {
	cases := []struct {
		name string
		want bool
		why  string
	}{
		// 默认的无名数据流：三种写法都必须放行（E5 原文 "always the default,
		// unnamed data stream, ::$DATA"）。误判这一档的代价是**整卷每个文件都被拒**。
		{"::$DATA", false, "E5 的规范写法"},
		{"::$data", false, "型别段大小写不敏感"},
		{":$DATA", false, "省略空名段的等价写法"},
		{":$data", false, "同上 + 大小写"},

		// 命名流：一律为真
		{":note:$DATA", true, "普通命名流"},
		{":Zone.Identifier:$DATA", true, "下载标记流（真实场景）"},
		{":Zone.Identifier::$DATA", true, "多一个冒号的畸形串——宁可多拦一个"},
		{":report:$BITMAP", true, "非 $DATA 型别"},
		{":note", true, "无型别段：Windows 允许，仍是命名流"},

		// 无法解析：不判（不误伤）
		{"", false, "空串"},
		{".", false, "无冒号"},
		{"foo:bar", false, "有冒号但不是流记法（无前导冒号）"},
		{":", false, "只有一个冒号"},
	}
	for _, c := range cases {
		if got := StreamIsNonDefault(c.name); got != c.want {
			t.Errorf("StreamIsNonDefault(%q) = %v，want %v（%s）", c.name, got, c.want, c.why)
		}
	}
}

func TestHasNonDefault(t *testing.T) {
	cases := []struct {
		names []string
		want  bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"::$DATA"}, false},
		{[]string{"::$DATA", "::$DATA"}, false},
		{[]string{"::$DATA", ":note:$DATA"}, true},
		{[]string{":note:$DATA", "::$DATA"}, true},
		{[]string{"::"}, false},
	}
	for _, c := range cases {
		if got := HasNonDefault(c.names); got != c.want {
			t.Errorf("HasNonDefault(%q) = %v，want %v", c.names, got, c.want)
		}
	}
}

// V2：errno → ErrKind 的六格分类表。用例一律用**字面量**而非包内常量——
// 常量抄错这件事必须由用例来抓，不能自己证明自己。
func TestClassify(t *testing.T) {
	cases := []struct {
		errno uintptr
		want  ErrKind
		why   string
	}{
		{0, ErrNone, "成功"},
		{38, ErrNoStreams, "ERROR_HANDLE_EOF：没有流可找 / 枚举到头（E4、E8）"},
		{87, ErrFSNoStreams, "ERROR_INVALID_PARAMETER：该文件系统不支持流（E4）"},
		{2, ErrMissing, "ERROR_FILE_NOT_FOUND"},
		{3, ErrMissing, "ERROR_PATH_NOT_FOUND"},
		{5, ErrDenied, "ERROR_ACCESS_DENIED"},
		{997, ErrOther, "ERROR_IO_PENDING 一类：不认识的一律 ErrOther"},
		{123, ErrOther, "任意未知码"},
	}
	for _, c := range cases {
		if got := Classify(c.errno); got != c.want {
			t.Errorf("Classify(%d) = %v，want %v（%s）", c.errno, got, c.want, c.why)
		}
	}
}

// V3：fail-closed 表**逐格**断言（设计 §5.1 的表有 6 行，这里连 names 一起叉乘）。
func TestDecideFailClosedTable(t *testing.T) {
	const (
		reasonNamed   = "文件含备用数据流，去重会丢失备用流内容，已拒绝操作"
		reasonDenied  = "无法确认文件是否存在备用数据流（拒绝访问），为避免丢失其内容已拒绝操作"
		reasonUnknown = "无法确认文件是否存在备用数据流（未知错误），为避免丢失其内容已拒绝操作"
	)
	clean := []string{"::$DATA"}
	named := []string{"::$DATA", ":note:$DATA"}

	cases := []struct {
		names  []string
		kind   ErrKind
		reject bool
		reason string
		why    string
	}{
		// ErrNone：唯一"按 names 判"的一格
		{nil, ErrNone, false, "", "枚举成功且无流（目录等）"},
		{clean, ErrNone, false, "", "枚举成功，只有默认流"},
		{named, ErrNone, true, reasonNamed, "命中备用流"},

		// 38：一个 $DATA 都没有 = 无从谈起备用流
		{nil, ErrNoStreams, false, "", "ERROR_HANDLE_EOF"},
		{named, ErrNoStreams, false, "", "38 时 names 不可信，仍按放行——表里写死的一格"},

		// 87：exFAT/FAT/部分网络盘根本不支持流
		{nil, ErrFSNoStreams, false, "", "卷不支持流"},
		{named, ErrFSNoStreams, false, "", "同上，names 不可信"},

		// 2/3：文件已消失，紧接着的 MoveFile/Trash 会给出自己的真实错误
		{nil, ErrMissing, false, "", "文件不存在"},
		{named, ErrMissing, false, "", "同上"},

		// 5 与其余：无法确定 → 拒绝（数据丢失方向，必须有独立杀手用例 M-P3-e）
		{nil, ErrDenied, true, reasonDenied, "拒绝访问：判不了就不赌"},
		{clean, ErrDenied, true, reasonDenied, "同上（names 干净也不放行）"},
		{nil, ErrOther, true, reasonUnknown, "未知错误：判不了就不赌"},
		{clean, ErrOther, true, reasonUnknown, "同上"},
	}
	for _, c := range cases {
		got := Decide(c.names, c.kind)
		if got.Reject != c.reject {
			t.Errorf("Decide(%q, %v).Reject = %v，want %v（%s）", c.names, c.kind, got.Reject, c.reject, c.why)
		}
		if got.Reason != c.reason {
			t.Errorf("Decide(%q, %v).Reason = %q，want %q（%s）", c.names, c.kind, got.Reason, c.reason, c.why)
		}
	}
}

// V3b（2026-09-21 变异 M-P3-d 读数暴露的缺口后补）：Classify → Decide 的**组合**。
//
// 为什么单靠 V2+V3 不够：V2 只测 Classify 的输出、V3 只测 Decide 的表（喂的是 ErrKind
// 值本身），于是"87 被判成 ErrOther 而不是 ErrFSNoStreams"这种错——exFAT/FAT 卷上
// **每次清理都被拒**——能让两处都绿。变异 M-P3-d/e 的第一轮读数正是如此：只有
// TestClassify 红了，Decide 那一侧毫无反应。这条用例把 errno 一路喂到底。
func TestClassifyThenDecide(t *testing.T) {
	named := []string{"::$DATA", ":note:$DATA"}
	cases := []struct {
		errno  uintptr
		names  []string
		reject bool
		why    string
	}{
		{0, []string{"::$DATA"}, false, "枚举成功、只有默认流"},
		{0, named, true, "枚举成功、有命名流"},
		{38, named, false, "没有流可找 → 放行（names 不可信）"},
		{87, named, false, "卷不支持流 → 放行，否则 exFAT 上每次清理都被拒"},
		{2, named, false, "文件不存在 → 放行"},
		{3, named, false, "路径不存在 → 放行"},
		{5, []string{"::$DATA"}, true, "拒绝访问 → 拦（数据丢失方向）"},
		{997, named, true, "未知码 → 拦"},
	}
	for _, c := range cases {
		got := Decide(c.names, Classify(c.errno))
		if got.Reject != c.reject {
			t.Errorf("Decide(_, Classify(%d)).Reject = %v，want %v（%s）",
				c.errno, got.Reject, c.reject, c.why)
		}
	}
}

// String() 是 ErrKind 唯一的对外说明面（拒绝文案里要嵌它），单独钉一遍，
// 免得将来改表时把"未知错误"这类兜底文案改成空串。
func TestErrKindString(t *testing.T) {
	cases := []struct {
		k    ErrKind
		want string
	}{
		{ErrNone, "枚举成功"},
		{ErrNoStreams, "没有流"},
		{ErrFSNoStreams, "该卷不支持备用数据流"},
		{ErrMissing, "文件已不存在"},
		{ErrDenied, "拒绝访问"},
		{ErrOther, "未知错误"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("ErrKind(%d).String() = %q，want %q", c.k, got, c.want)
		}
	}
}
