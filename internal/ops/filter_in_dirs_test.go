package ops

// AS-H6 / 决策 D-1（方案 b，2026-09-20 全仓审计）：
// 前端此前自己实现了一份「路径是否属于目录」的判据（frontend/src/utils/pathpolicy.ts
// 的 dirContains），用来算「将处理 N / 已排除 M」。那是 inDir 之外的**第二份实现**，
// 而且已经实测漂移：它不做 Clean 等价归一（`//`、`./`、`..`），且按路径形状猜
// 大小写语义，在不敏感卷上会**少报**命中——界面承诺不动的文件，后端实际会删。
//
// 修法是把判据收回后端一处（FilterInDirs），前端只显示返回的下标。
// 因此本文件的核心不是"FilterInDirs 算得对不对"，而是
// **"它和引擎用的判据是否严格同一个"**：只要共用同一个内核，前端就再也不可能
// 与后端算出不同的答案；若有人另写一份，下面的口径一致用例会立刻失败。

import (
	"testing"

	"filededup/internal/model"
)

// TestFilterInDirsAgreesWithInDir 逐个 (目录, 路径) 组合比对：
// FilterInDirs 命中 ⟺ inDir 为真。这是"判据只有一份"的直接表达。
func TestFilterInDirsAgreesWithInDir(t *testing.T) {
	paths := []string{
		"/proc/a.bin",
		"/proc/deep/b.bin",
		"/processor/c.bin", // 前缀相近但不是子目录——最易误判
		"/procX/d.bin",
		"/other/e.bin",
		"/proc", // 路径恰好等于目录
		"/outside/f.bin",
	}
	dirs := []string{"/proc", "/proc/", "/other", "/processor", "/", "/nonexistent", "", "   "}

	for _, dir := range dirs {
		got := FilterInDirs([]string{dir}, paths)
		inGot := map[int]bool{}
		for _, i := range got {
			if inGot[i] {
				t.Fatalf("目录 %q：下标 %d 重复出现（%v）——命中一次即应只出一个", dir, i, got)
			}
			inGot[i] = true
		}
		for i, p := range paths {
			if want := inDir(p, dir); want != inGot[i] {
				t.Errorf("口径不一致：路径 %q 目录 %q —— inDir=%v 但 FilterInDirs=%v\n"+
					"两者必须走同一个判据内核（inDir）。若另写了一份路径归属比较，请改回经由 inDir。",
					p, dir, want, inGot[i])
			}
		}
	}
}

// TestFilterInDirsAgreesWithApplyProcessPolicy 与引擎口径一致。
//
// ApplyProcessPolicy 是真正决定执行范围的那一个；预览计数若与它给出不同答案，
// 界面就是在承诺一个不会兑现的数字（I5 的老事故形态）。同一批路径分别喂给两者，
// 命中的下标集合必须与 MatchIDs 完全相同。
func TestFilterInDirsAgreesWithApplyProcessPolicy(t *testing.T) {
	paths := []string{
		"/proc/a.bin", "/proc/deep/b.bin", "/processor/c.bin",
		"/other/d.bin", "/nowhere/e.bin", "/proc",
	}
	corpus := [][]string{
		{"/proc"},
		{"/proc", "/other"},
		{"/proc/", "/proc"}, // 归一后重复：只算一条
		{"/processor"},      // 与 /proc 前缀相近，必须不命中 /proc/*
		{"   ", ""},         // 全空白 = 未启用
		{"/proc/deep", "/nonexistent"},
	}

	for _, dirs := range corpus {
		groups := []*model.DuplicateGroup{dirGroup(1, paths...)}
		out := ApplyProcessPolicy(groups, dirs, nil)

		// dirGroup(id=1) 给第 i 个文件分配的 ID 是 1*100+i，按此还原下标
		want := map[int]bool{}
		for i := range paths {
			if out.MatchIDs[100+uint64(i)] {
				want[i] = true
			}
		}

		got := map[int]bool{}
		for _, i := range FilterInDirs(dirs, paths) {
			got[i] = true
		}

		if len(want) != len(got) {
			t.Fatalf("目录 %q：引擎命中 %d 项，FilterInDirs 命中 %d 项（want %v got %v）",
				dirs, len(want), len(got), wantKeys(want), got)
		}
		for i := range want {
			if !got[i] {
				t.Fatalf("目录 %q：引擎命中下标 %d 但 FilterInDirs 没命中（want %v got %v）",
					dirs, i, wantKeys(want), got)
			}
		}
	}
}

func wantKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestFilterInDirsCleansDirForms AS-H6 的正面：优先目录的各种写法必须归一。
//
// 前端那份的实现漏了这一层（它只剥尾分隔符，不处理 `//`、`./`、`..`），
// 于是 "/data//dups" 在它眼里不是 "/data/dups/a.bin" 的父目录——
// 后端会处理该文件，界面却说"已排除"。
func TestFilterInDirsCleansDirForms(t *testing.T) {
	p := "/data/dups/a.bin"
	for _, dir := range []string{
		"/data//dups",
		"/data/./dups",
		"/data/dups/",
		"/data/dups/../dups",
		"/data/other/../dups",
		"  /data/dups  ",
	} {
		if got := FilterInDirs([]string{dir}, []string{p}); len(got) != 1 || got[0] != 0 {
			t.Errorf("目录 %q 应命中 %q，实得 %v", dir, p, got)
		}
	}

	// 反例：Clean 后确实不相含的目录不得命中（归一不是放宽边界）。
	// "/data" 不在这里——它是 "/data/dups/a.bin" 的真祖先，本来就该命中。
	for _, dir := range []string{"/dat", "/data/dupsx", "/data/dup", "/other", "/dataa"} {
		if got := FilterInDirs([]string{dir}, []string{p}); len(got) != 0 {
			t.Errorf("目录 %q 不应命中 %q，实得 %v", dir, p, got)
		}
	}
}

// TestFilterInDirsUnionAndOrder dirs 是**并集**语义（见 ApplyProcessPolicy 注释），
// 返回下标必须升序且每项只出现一次——前端拿它做 filter，顺序错乱会打乱列表。
func TestFilterInDirsUnionAndOrder(t *testing.T) {
	paths := []string{
		"/a/1.bin", "/b/2.bin", "/c/3.bin", "/a/deep/4.bin", "/b/5.bin",
	}
	got := FilterInDirs([]string{"/b", "/a"}, paths)
	want := []int{0, 1, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("命中 %v，应为 %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("命中 %v，应为 %v（顺序须与入参一致）", got, want)
		}
	}

	// 重复目录（含尾分隔符变体）不得让同一下标出现两次。
	dup := FilterInDirs([]string{"/a", "/a/", " /a "}, []string{"/a/1.bin"})
	if len(dup) != 1 {
		t.Fatalf("重复目录导致同一下标出现 %d 次：%v", len(dup), dup)
	}
}

// TestFilterInDirsDisabled 没有可用目录 → 空结果（不是"全部命中"）。
//
// 这条必须有：未启用处理策略时前端走的是 selectedFiles 原样，
// 但若有人把空目录列表实现成"匹配一切"，确认框会显示一个虚高的数，
// 而引擎那边 dirs 为空时根本不做过滤——两侧仍然一致，所以只能靠用例钉住。
func TestFilterInDirsDisabled(t *testing.T) {
	for _, dirs := range [][]string{nil, {}, {""}, {"   "}, {"\t"}} {
		if got := FilterInDirs(dirs, []string{"/a/1.bin", "/b/2.bin"}); len(got) != 0 {
			t.Fatalf("目录 %q 应视为未启用（空结果），实得 %v", dirs, got)
		}
	}
	// 空路径列表也必须返回空而非 panic。
	if got := FilterInDirs([]string{"/a"}, nil); len(got) != 0 {
		t.Fatalf("空路径应得空结果，实得 %v", got)
	}
}
