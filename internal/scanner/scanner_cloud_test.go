package scanner

// M6-P1（2026-09-21，设计稿 §4.4）云端占位在**遍历层**的后果：跳过、计数、
// 与相邻三条规则的先后顺序、开关双向。
//
// 分层：位语义本身（哪些 flags 算占位）由 internal/cloudfile 的参数化用例覆盖，
// 这里刻意不看宿主平台——判据整个换成"按文件名后缀命中"的假函数，于是
// "顺序对不对""计数准不准""开关走没走通"这三件真正属于 scanner 的事，
// 在 Linux 门禁上就是真夹具真断言（与 setGuard 同一手法）。

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"filededup/internal/cloudfile"
	"filededup/internal/model"
)

func setCloudCheck(t *testing.T, fn func(os.FileInfo) bool) {
	t.Helper()
	prev := cloudCheck
	cloudCheck = fn
	t.Cleanup(func() { cloudCheck = prev })
}

// suffixJudge 返回"文件名以 ext 结尾即占位"的假判据。用名字而非真实标志位，
// 是为了让夹具在任何平台、任何卷上都可复现——真占位需要云端客户端配合制造。
func suffixJudge(ext string) func(os.FileInfo) bool {
	return func(fi os.FileInfo) bool {
		return fi != nil && strings.HasSuffix(fi.Name(), ext)
	}
}

// namesIn 结果集文件名集合，便于逐条断言"谁进来了/谁没进来"。
func namesIn(res *Result) map[string]bool {
	set := map[string]bool{}
	for _, e := range res.Files {
		set[filepath.Base(e.Path)] = true
	}
	return set
}

func TestWalkSkipsCloudPlaceholderAndCountsIt(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, root, "a.docx", "b.docx", "set.docx")
	// 0 字节占位（设计稿 §4.0 E5 的真机型：iCloud 的 .localized 就是 0 字节 dataless）。
	// 它必须记在"云占位"这一类而不是"0 字节"：两条 continue 谁在前，决定这个数
	// 落在哪个抽屉，而"你盘上有 4 个文件在云端"远比"有一个是空的"有解释力。
	if err := os.WriteFile(filepath.Join(root, "empty.docx"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mkDirFiles(t, root, "plain.txt")
	if err := os.WriteFile(filepath.Join(root, "plain-zero.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	setCloudCheck(t, suffixJudge(".docx"))
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 4)

	if got, want := res.SkippedCloudFiles, 4; got != want {
		t.Errorf("SkippedCloudFiles = %d, want %d（含 1 个 0 字节占位）", got, want)
	}
	names := namesIn(res)
	for _, n := range []string{"a.docx", "b.docx", "set.docx", "empty.docx"} {
		if names[n] {
			t.Errorf("%s 进了语料：占位文件一旦被哈希就会走预筛采样，那是真下载", n)
		}
	}
	// 非占位文件不受影响，其中 0 字节那条仍按既有规则被跳过（但不进本项的账）。
	if !names["plain.txt"] {
		t.Error("plain.txt 未进语料（判据过宽）")
	}
	if names["plain-zero.txt"] {
		t.Error("0 字节普通文件进了语料")
	}
	// 与 M6-P4 同一条纪律：命中不是失败。
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want 空", res.Failed)
	}
}

func TestWalkCloudCountDoesNotDriftWithIncludeHidden(t *testing.T) {
	root := t.TempDir()
	// 隐藏的**文件**（不是隐藏目录：目录级剪枝发生在判据之前，那是另一条账）。
	mkDirFiles(t, root, ".secret.docx", "open.docx", "note.txt")
	mkDirFiles(t, root, ".plain-hidden") // 隐藏的普通文件，用作反向对照

	setCloudCheck(t, suffixJudge(".docx"))
	off := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	on := Walk(context.Background(), []string{root}, &model.Filters{IncludeHidden: true}, 2)

	if off.SkippedCloudFiles != 2 || on.SkippedCloudFiles != 2 {
		t.Errorf("SkippedCloudFiles 随 IncludeHidden 漂移：hidden=false 得 %d，true 得 %d，want 均为 2",
			off.SkippedCloudFiles, on.SkippedCloudFiles)
	}
	// 反向对照：隐藏的**普通**文件确实随该开关进出语料。没有这一条，"两档计数相等"
	// 也可能只是夹具整体没生效。
	if hasPathWith(off, "/.plain-hidden") || !hasPathWith(on, "/.plain-hidden") {
		t.Errorf("IncludeHidden 对普通隐藏文件的既有语义变了（off=%v on=%v），上面的相等断言不成立",
			namesIn(off), namesIn(on))
	}
}

func TestWalkAllowCloudHydrationBypassesJudgment(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, root, "a.docx", "b.docx", "plain.txt")
	setCloudCheck(t, suffixJudge(".docx"))

	// 默认档：跳过并计数。
	skip := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if skip.SkippedCloudFiles != 2 {
		t.Fatalf("默认档 SkippedCloudFiles = %d, want 2", skip.SkippedCloudFiles)
	}

	// 显式允许水合：既照常入语料，也**不计数**（这一档下没有"跳过"这件事）。
	hydrate := Walk(context.Background(), []string{root},
		&model.Filters{AllowCloudHydration: true}, 2)
	if hydrate.SkippedCloudFiles != 0 {
		t.Errorf("AllowCloudHydration=true 时 SkippedCloudFiles = %d, want 0（计数语义反转会让横幅说反话）",
			hydrate.SkippedCloudFiles)
	}
	names := namesIn(hydrate)
	for _, n := range []string{"a.docx", "b.docx", "plain.txt"} {
		if !names[n] {
			t.Errorf("%s 未进语料：开关语义被反转了", n)
		}
	}
	if len(hydrate.Files) != len(skip.Files)+2 {
		t.Errorf("语料数 = %d，默认档 %d，want 恰好多 2", len(hydrate.Files), len(skip.Files))
	}
}

func TestWalkCloudJudgmentBeatsExtensionFilter(t *testing.T) {
	root := t.TempDir()
	// 一个被扩展名排除、同时是占位的文件；以及一个只被扩展名排除的普通文件。
	mkDirFiles(t, root, "clip.log", "other.log", "keep.txt")
	setCloudCheck(t, suffixJudge("clip.log"))

	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludeExts: []string{".log"}}, 2)

	if res.SkippedCloudFiles != 1 {
		t.Errorf("SkippedCloudFiles = %d, want 1（占位判定须优先于扩展名过滤，否则计数随过滤规则漂移）",
			res.SkippedCloudFiles)
	}
	if !hasPathWith(res, "/keep.txt") {
		t.Error("keep.txt 未进语料")
	}
	if hasPathWith(res, "/clip.log") || hasPathWith(res, "/other.log") {
		t.Error(".log 文件未被扩展名过滤挡住")
	}
}

// TestCloudCheckSeamUsesRealJudgment 钉住**出厂接线**：不注入时 cloudCheck 必须
// 就是 cloudfile.Of。上面每条用例都在测换装后的假判据，若不单独断言这一条，
// "把 cloudCheck 接成恒 false"这种变异在所有用例里都是绿的（M6-P2 的实占接缝
// 踩过同一次，见 04 §6.9.3 的 M-P2-b2）。
//
// 用函数指针比对而非行为比对：行为比对要造真占位文件（本机造不出来），
// 而"接的是哪个函数"是可静态判定的事实，reflect 在这里给的就是那个事实。
func TestCloudCheckSeamUsesRealJudgment(t *testing.T) {
	got := reflect.ValueOf(cloudCheck).Pointer()
	want := reflect.ValueOf(cloudfile.Of).Pointer()
	if got != want {
		t.Errorf("cloudCheck 未接到 cloudfile.Of（got=%x want=%x）", got, want)
	}
	// 行为侧兜底：真实判据在本机临时目录的普通文件上必须给 false，
	// 否则位读错或判据过宽，整片语料会被吞掉。
	root := t.TempDir()
	mkDirFiles(t, root, "plain.txt")
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	if res.SkippedCloudFiles != 0 {
		t.Errorf("真实判据在普通文件上 SkippedCloudFiles = %d, want 0", res.SkippedCloudFiles)
	}
	if !hasPathWith(res, "/plain.txt") {
		t.Error("真实判据把普通文件挡在语料外")
	}
}
