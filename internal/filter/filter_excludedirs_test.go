// 扫描排除目录（精确路径通道）：Filters.ExcludeDirs 的匹配判据。
// 与 ExcludePaths（glob、相对扫描根）分属两条独立通道：这里比的是
// 目录的绝对路径键，命中该目录**及整个子树**即剪枝。
package filter

import (
	"testing"

	"filededup/internal/model"
)

func compileDirs(t *testing.T, dirs ...string) *Matcher {
	t.Helper()
	return Compile(&model.Filters{ExcludeDirs: dirs})
}

func TestExcludeDirPathHitsSelfAndSubtree(t *testing.T) {
	m := compileDirs(t, "/vol/data/sync")
	for _, full := range []string{
		"/vol/data/sync",       // 自身
		"/vol/data/sync/a.txt", // 直接后代目录内容无所谓——键语义只看路径前缀
		"/vol/data/sync/x/y/z", // 任意深度
	} {
		if !m.ExcludeDirPath(full) {
			t.Fatalf("ExcludeDirPath(%q) = false, want true（命中自身/子树）", full)
		}
	}
}

func TestExcludeDirPathRejectsLookalikeAndParent(t *testing.T) {
	m := compileDirs(t, "/vol/data/sync")
	for _, full := range []string{
		"/vol/data/syncx", // 前缀形近兄弟：必须以段边界分家
		"/vol/data",       // 父目录
		"/vol/data/other", // 无关目录
	} {
		if m.ExcludeDirPath(full) {
			t.Fatalf("ExcludeDirPath(%q) = true, want false", full)
		}
	}
}

func TestExcludeDirPathNormalizesEntryShape(t *testing.T) {
	// 尾斜杠条目与无尾斜杠条目同义（picker 与手输都可能带尾杠）。
	// 盘根 "/":TrimTailKeepRoot 刻意保留,条目 "/x" 前的根斜杠不许被吃掉。
	m := compileDirs(t, "/vol/data/sync/", " /vol/cache ")
	if !m.ExcludeDirPath("/vol/data/sync/deep") {
		t.Fatal("尾斜杠条目未生效")
	}
	if !m.ExcludeDirPath("/vol/cache") {
		t.Fatal("空白包裹的条目未被 trim 生效")
	}
}

func TestExcludeDirPathInactiveIsAlwaysFalse(t *testing.T) {
	// 未配置 = 恒 false（与 ExcludeDir 的空列表分支同口径）；
	// 空白列表项按未配置处理,不得留下命中一切的键——"Under(x, \"\")" 对
	// 以 "/" 开头的 child 恒真，一条空串就等价于"全盘排除"，方向必须钉死。
	m := compileDirs(t, "", "   ")
	for _, full := range []string{"/anything", "/"} {
		if m.ExcludeDirPath(full) {
			t.Fatalf("空白条目 %q 生效了：空排除目录列表必须恒 false", full)
		}
	}
	var nilM *Matcher
	if nilM.ExcludeDirPath("/vol/data") {
		t.Fatal("nil *Matcher 必须恒 false（与 Compile/Apply/ExcludeDir 同契约）")
	}
}

func TestExcludeDirPathMultipleEntriesAnyHit(t *testing.T) {
	m := compileDirs(t, "/vol/a", "/vol/b")
	if !m.ExcludeDirPath("/vol/b/x") || !m.ExcludeDirPath("/vol/a") {
		t.Fatal("多条目时任一命中即排除")
	}
	if m.ExcludeDirPath("/vol/c") {
		t.Fatal("未列出的目录被误排除")
	}
}
