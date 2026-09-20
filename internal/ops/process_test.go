package ops

// 处理策略（优先处理的文件夹）引擎测试。
//
// 语义回顾：与保留策略的 directory 完全不同——
//   - 保留策略要在多个目录里挑出**一个**保留者，所以目录有优先级、必须排序；
//   - 处理策略是"这些目录里的都算"，取**并集**，无优先级。
//
// 本文件里最关键的是最后两条"防 I5 重演"的属性化用例：它们确保
// inDir（唯一的路径归属判据）与 pickInDirectory 的口径永远一致。
// 若将来有人新增第三个路径匹配点而没走 inDir，这两条会报警。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// idsOf 把 MatchIDs 转成便于比对的集合。
func idsOf(o ProcessPolicyOutcome) map[uint64]bool { return o.MatchIDs }

// TestApplyProcessPolicyMatchesAllInDir 目录下的**多份**文件必须全部命中。
//
// 这是与 pickInDirectory 的核心差异：后者返回"最深的那一个索引"（单值），
// 而处理策略要的是"这个目录里的全部"。若误用 pickInDirectory 实现，
// 一个目录下只会命中 1 份，其余静默漏掉——用户限定 3 个文件却只处理 1 个。
func TestApplyProcessPolicyMatchesAllInDir(t *testing.T) {
	g := dirGroup(1, "/proc/a.bin", "/proc/b.bin", "/proc/c.bin", "/other/d.bin")
	out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, []string{"/proc"}, nil)

	if out.MatchedFiles != 3 {
		t.Fatalf("命中数 = %d, want 3（/proc 下三份都要命中）", out.MatchedFiles)
	}
	for i := 0; i < 3; i++ {
		if !out.MatchIDs[g.Files[i].ID] {
			t.Errorf("应命中 %s（id=%d）", g.Files[i].Path, g.Files[i].ID)
		}
	}
	if out.MatchIDs[g.Files[3].ID] {
		t.Errorf("不该命中 /other/d.bin（在限定目录外）")
	}
}

// TestApplyProcessPolicyUnionSemantics 多个目录取并集，且顺序不影响结果。
// 与保留策略的"靠前目录优先"形成对照。
func TestApplyProcessPolicyUnionSemantics(t *testing.T) {
	g := dirGroup(1, "/a/x.bin", "/b/y.bin", "/c/z.bin")

	// 同一组目录、两种顺序，结果必须一致
	for _, dirs := range [][]string{
		{"/a", "/b"},
		{"/b", "/a"},
	} {
		out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, dirs, nil)
		if out.MatchedFiles != 2 {
			t.Fatalf("dirs=%v 命中数 = %d, want 2", dirs, out.MatchedFiles)
		}
		if !out.MatchIDs[g.Files[0].ID] || !out.MatchIDs[g.Files[1].ID] {
			t.Errorf("dirs=%v 应命中 /a/x.bin 与 /b/y.bin（%+v）", dirs, out.MatchIDs)
		}
		if out.MatchIDs[g.Files[2].ID] {
			t.Errorf("dirs=%v 不该命中 /c/z.bin", dirs)
		}
	}
}

// TestApplyProcessPolicyExcludesKeepIDs 保留项即使位于限定目录内也必须被剔除。
//
// 执行器本身会硬拒保留项（"保留文件不可操作"），但那是最后一道防线；
// 引擎层若不剔除，"将处理 N 项"的计数会虚高——用户看到 N，实际处理 < N。
// 这条断言的就是"计数必须诚实"。
func TestApplyProcessPolicyExcludesKeepIDs(t *testing.T) {
	g := dirGroup(1, "/proc/a.bin", "/proc/b.bin", "/proc/c.bin")
	keep := map[uint64]bool{g.Files[1].ID: true} // b 是本组保留者

	out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, []string{"/proc"}, keep)

	if out.MatchedFiles != 2 {
		t.Fatalf("命中数 = %d, want 2（保留项要剔除，不能虚报成 3）", out.MatchedFiles)
	}
	if out.MatchIDs[g.Files[1].ID] {
		t.Fatal("保留项不该出现在 MatchIDs 里（会让计数虚高）")
	}
	if !out.MatchIDs[g.Files[0].ID] || !out.MatchIDs[g.Files[2].ID] {
		t.Errorf("非保留项应命中: %+v", out.MatchIDs)
	}
}

// TestApplyProcessPolicyUnmatchedDirs 未命中的目录要被点名回传。
// 部分命中时只报未命中的那些——用户需要知道"我加的这一条没起作用"。
func TestApplyProcessPolicyUnmatchedDirs(t *testing.T) {
	g := dirGroup(1, "/proc/a.bin", "/proc/b.bin")

	out := ApplyProcessPolicy([]*model.DuplicateGroup{g},
		[]string{"/proc", "/nonexistent", "/alsonone"}, nil)

	if len(out.UnmatchedDirs) != 2 {
		t.Fatalf("未命中目录 = %v, want 2 条", out.UnmatchedDirs)
	}
	// 必须回显**用户输入的原文**，否则他找不到自己加的是哪条
	if out.UnmatchedDirs[0] != "/nonexistent" || out.UnmatchedDirs[1] != "/alsonone" {
		t.Fatalf("未命中目录 = %v, want [/nonexistent /alsonone]", out.UnmatchedDirs)
	}
}

// TestApplyProcessPolicyUnmatchedOnlyWhenNoProcessableFile 目录"存在但里面
// 只有保留项"时也应算未命中——那条目录实际起不到任何限定作用。
func TestApplyProcessPolicyUnmatchedOnlyWhenNoProcessableFile(t *testing.T) {
	g := dirGroup(1, "/only/a.bin", "/other/b.bin")
	keep := map[uint64]bool{g.Files[0].ID: true} // /only 下唯一那份是保留项

	out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, []string{"/only"}, keep)
	if out.MatchedFiles != 0 {
		t.Fatalf("命中数 = %d, want 0（唯一候选是保留项）", out.MatchedFiles)
	}
	if len(out.UnmatchedDirs) != 1 || out.UnmatchedDirs[0] != "/only" {
		t.Fatalf("应报 /only 未命中（那条目录没有可处理的文件），实得 %v", out.UnmatchedDirs)
	}
}

// TestApplyProcessPolicyEmptyDirs 空目录列表 = 未启用处理策略。
// 此时不该把"所有目录"都当未命中（那会在界面上弹一堆无意义的提示）。
func TestApplyProcessPolicyEmptyDirs(t *testing.T) {
	g := dirGroup(1, "/a/x.bin", "/b/y.bin")
	for _, dirs := range [][]string{nil, {}} {
		out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, dirs, nil)
		if out.MatchedFiles != 0 || len(out.MatchIDs) != 0 {
			t.Errorf("dirs=%v 应为空结果", dirs)
		}
		if len(out.UnmatchedDirs) != 0 {
			t.Errorf("dirs=%v 不该报未命中（那不是用户配错了，是没启用）: %v", dirs, out.UnmatchedDirs)
		}
	}
}

// TestApplyProcessPolicyBlankDirsIgnored 空白项忽略，且不计入未命中。
// 与 HasUsableDir 同口径。
func TestApplyProcessPolicyBlankDirsIgnored(t *testing.T) {
	g := dirGroup(1, "/proc/a.bin", "/proc/b.bin")
	out := ApplyProcessPolicy([]*model.DuplicateGroup{g},
		[]string{"  ", "", "\t", "/proc"}, nil)

	if out.MatchedFiles != 2 {
		t.Fatalf("命中数 = %d, want 2（空白项应被忽略，不影响 /proc 生效）", out.MatchedFiles)
	}
	if len(out.UnmatchedDirs) != 0 {
		t.Fatalf("空白项不该计入未命中: %v", out.UnmatchedDirs)
	}

	// 全是空白 = 等同于未启用
	out2 := ApplyProcessPolicy([]*model.DuplicateGroup{g}, []string{"  ", ""}, nil)
	if out2.MatchedFiles != 0 || len(out2.UnmatchedDirs) != 0 {
		t.Fatalf("全空白应等同于未启用: %+v", out2)
	}
}

// TestApplyProcessPolicyDuplicateDirs 重复目录不导致未命中提示重复。
func TestApplyProcessPolicyDuplicateDirs(t *testing.T) {
	g := dirGroup(1, "/proc/a.bin", "/proc/b.bin")
	out := ApplyProcessPolicy([]*model.DuplicateGroup{g},
		[]string{"/proc", "/proc", "/proc/"}, nil)

	if out.MatchedFiles != 2 {
		t.Fatalf("命中数 = %d, want 2", out.MatchedFiles)
	}
	if len(out.UnmatchedDirs) != 0 {
		t.Fatalf("已命中的目录不该出现在未命中里: %v", out.UnmatchedDirs)
	}

	// 未命中的重复项也只报一次
	out2 := ApplyProcessPolicy([]*model.DuplicateGroup{g},
		[]string{"/nope", "/nope"}, nil)
	if len(out2.UnmatchedDirs) != 1 {
		t.Fatalf("未命中目录应去重，实得 %v", out2.UnmatchedDirs)
	}
}

// TestApplyProcessPolicyEmptyGroups 空结果集不 panic，且所有目录都算未命中
// （用户加了目录但当前没有重复文件——正是需要提示的场景）。
func TestApplyProcessPolicyEmptyGroups(t *testing.T) {
	out := ApplyProcessPolicy(nil, []string{"/proc"}, nil)
	if out.MatchedFiles != 0 || len(out.MatchIDs) != 0 {
		t.Fatalf("空结果集应为空命中: %+v", out)
	}
	if len(out.UnmatchedDirs) != 1 {
		t.Fatalf("空结果集下 /proc 应报未命中，实得 %v", out.UnmatchedDirs)
	}
}

// TestApplyProcessPolicyRootDir 目录为卷根时命中该卷全部（含根下深层）。
// 与 pickInDirectory 对根目录的处理保持一致。
func TestApplyProcessPolicyRootDir(t *testing.T) {
	g := dirGroup(1, "/a.bin", "/x/y/c.bin", "/z.bin")
	out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, []string{"/"}, nil)
	if out.MatchedFiles != 3 {
		t.Fatalf("根目录应命中全部 3 份，实得 %d", out.MatchedFiles)
	}
	if len(out.UnmatchedDirs) != 0 {
		t.Fatalf("根目录命中后不该报未命中: %v", out.UnmatchedDirs)
	}
}

// TestApplyProcessPolicyCaseFold 不敏感卷上，目录写法的大小写与尾分隔符
// 都不该让限定失效。
//
// ★ 这条必须与保留策略给出**同一個**命中集：两份策略若在同一批文件上
// 判定不一致，用户会看到"保留策略认为文件在目录内、处理策略认为不在"。
func TestApplyProcessPolicyCaseFold(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "Proc", "Photo")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	onDisk := filepath.Join(real, "a.bin")
	if err := os.WriteFile(onDisk, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fscase.Sensitive(real) {
		t.Skip("该主机临时目录区分大小写，本例（大小写写法差异）不适用")
	}

	g := dirGroup(1, onDisk, filepath.Join(tmp, "Else", "b.bin"))

	cases := []struct{ name, dir string }{
		{"全小写写法", filepath.Join(tmp, "proc", "photo")},
		{"全大写写法", filepath.Join(tmp, "PROC", "PHOTO")},
		{"带尾分隔符", real + string(filepath.Separator)},
		{"正写法", real},
	}
	for _, c := range cases {
		out := ApplyProcessPolicy([]*model.DuplicateGroup{g}, []string{c.dir}, nil)
		if out.MatchedFiles != 1 {
			t.Errorf("%s: 命中数 = %d, want 1（磁盘上那份）", c.name, out.MatchedFiles)
		}
		if !out.MatchIDs[g.Files[0].ID] {
			t.Errorf("%s: 应命中 %s", c.name, onDisk)
		}

		// 交叉校验：保留策略对同一组、同一目录也必须选中同一个文件
		ds, _ := ApplyKeepPolicy([]*model.DuplicateGroup{g},
			model.KeepPolicy{Kind: "directory", Directories: []string{c.dir}})
		if len(ds) != 1 || ds[0].KeepID != g.Files[0].ID {
			t.Errorf("%s: 保留策略选中 %+v，与处理策略口径不一致", c.name, ds)
		}
	}
}

// ---------------------------------------------------------------------------
// 防 I5 重演：inDir 必须是全包唯一的路径归属判据。
// ---------------------------------------------------------------------------

// TestInDirAgreesWithPickInDirectory inDir 与 pickInDirectory 的口径必须一致。
//
// 这是属性化守卫：遍历一组 (路径, 目录) 组合，凡是 inDir 判定为真的路径，
// 只要它在该组内是唯一命中者，pickInDirectory 就必须选中它；反之亦然。
//
// 为什么必须有这条：项目曾因"两份独立的路径比较实现"导致保留策略与默认建议
// 选出不同文件（I5）。现在 inDir 是唯一判据，本用例把这条约定钉死——
// 将来若有人绕过 inDir 另写一份比较，这里会立刻不一致。
func TestInDirAgreesWithPickInDirectory(t *testing.T) {
	paths := []string{
		"/proc/a.bin",
		"/proc/deep/b.bin",
		"/processor/c.bin", // 前缀相近但不是子目录——最易误判
		"/procX/d.bin",
		"/other/e.bin",
		"/proc", // 路径恰好等于目录
		"/PROC/f.bin",
	}
	dirs := []string{"/proc", "/proc/", "/other", "/processor", "/", "/nonexistent"}

	for _, dir := range dirs {
		for _, p := range paths {
			want := inDir(p, dir)

			// 构造"只有这一个文件"的组：pickInDirectory 命中 ⟺ inDir 为真
			g := dirGroup(1, p)
			got := pickInDirectory(g, dir) >= 0

			if want != got {
				t.Errorf("口径不一致：路径 %q 目录 %q —— inDir=%v 但 pickInDirectory=%v\n"+
					"这两者必须一致（inDir 是全包唯一的路径归属判据）。"+
					"若新增了第三条路径比较实现，请改为经由 inDir。",
					p, dir, want, got)
			}
		}
	}
}

// TestPickInDirectoryPrefersDeepest 同组内多份命中同一目录时取**路径最深**者。
//
// 这条以前只在 pickInDirectory 的注释里承诺过，没有用例钉死。
// 深层匹配更精确——保留策略要保的是"用户明确指定的那个更深的目录下的那份"。
func TestPickInDirectoryPrefersDeepest(t *testing.T) {
	// shallow 在前、deep 在后；两者都在 /proc 下，但 deep 路径更长
	shallow := "/proc/a.bin"
	deep := "/proc/sub/dir/verydeep/b.bin"
	g := dirGroup(1, shallow, deep)

	idx := pickInDirectory(g, "/proc")
	if idx < 0 {
		t.Fatal("/proc 应命中，实得 -1")
	}
	if g.Files[idx].Path != deep {
		t.Fatalf("应取路径最深的 %q，实得 %q", deep, g.Files[idx].Path)
	}

	// 与保留策略联动：directory 策略也保最深的那个
	ds, _ := ApplyKeepPolicy([]*model.DuplicateGroup{g},
		model.KeepPolicy{Kind: "directory", Directories: []string{"/proc"}})
	if len(ds) != 1 || ds[0].KeepID != g.Files[1].ID {
		t.Fatalf("保留策略应选中最深的那份，实得 %+v", ds)
	}
}

// TestInDirEdgeCases inDir 的边界：空目录、空白、目录等于路径、
// 前缀相近但不是子目录（"/processor" 不是 "/proc" 的子目录）。
func TestInDirEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		p    string
		dir  string
		want bool
	}{
		{"目录为空", "/a/b.bin", "", false},
		{"目录纯空白", "/a/b.bin", "   ", false},
		{"路径等于目录", "/proc", "/proc", true},
		{"直接子文件", "/proc/a.bin", "/proc", true},
		{"深层后代", "/proc/x/y/z.bin", "/proc", true},
		{"前缀相近但非子目录", "/processor/c.bin", "/proc", false},
		{"多一个字符的目录名", "/procX/d.bin", "/proc", false},
		{"完全无关", "/other/e.bin", "/proc", false},
		{"尾分隔符等价", "/proc/a.bin", "/proc/", true},
		{"根目录命中绝对路径", "/anywhere/a.bin", "/", true},
	}
	for _, c := range cases {
		if got := inDir(c.p, c.dir); got != c.want {
			t.Errorf("%s: inDir(%q, %q) = %v, want %v", c.name, c.p, c.dir, got, c.want)
		}
	}
}
