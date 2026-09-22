package scanner

// R2-4（Task B4，设计段 §30.6）：语法无效的排除模式必须**出现在用户看得见的那张清单里**。
//
// 为什么是失败清单：它是扫描侧唯一已经到得了用户的明细通道。开码核实过
// ScanSummary 那六个"跳过类"计数在 frontend/src 里只有类型声明、零消费者
// （裁定②③明写"界面不呈现"），而 FailedDrawer 三列原样渲染 res.Failed。
//
// 本文件钉三格，缺任何一格都是假绿：
//   ① 坏模式 ⇒ 有且仅有一条 Stage=="exclude" 的失败项，Path 是用户原文；
//   ② fail-open 不变 ⇒ 那条模式本想排除的文件**依然被扫到**（本项只报告，不改判）；
//   ③ 无坏模式 ⇒ 失败清单零新增，且正常模式照常生效（否则 ① 只是"什么都报"）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// badPatFixture 造 build/x/f.o 与 keep/f.o 两棵真目录，返回根路径。
func badPatFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{
		filepath.Join("build", "x", "f.o"),
		filepath.Join("keep", "f.o"),
	} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("PAYLOAD-FOR-R2-4"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// excludeFailed 挑出失败清单里的排除模式条目。
func excludeFailed(res *Result) []model.FailedItem {
	var out []model.FailedItem
	for _, f := range res.Failed {
		if f.Stage == "exclude" {
			out = append(out, f)
		}
	}
	return out
}

func scannedPaths(res *Result) []string {
	var out []string
	for _, e := range res.Files {
		out = append(out, filepath.ToSlash(e.Path))
	}
	return out
}

func TestWalkReportsInvalidExcludePattern(t *testing.T) {
	root := badPatFixture(t)
	stubVerdict(t, nil) // 卷语义按"确证敏感"回答，本条不往用户目录写探测文件

	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludePaths: []string{"build/["}}, 2)

	got := excludeFailed(res)
	if len(got) != 1 {
		t.Fatalf("失败清单里的排除条目 = %d 条 %v，want 1 条（多报会把正常模式也说成坏的）", len(got), got)
	}
	if got[0].Path != "build/[" {
		t.Fatalf("Path = %q, want 用户原文 %q（回显归一后的形态用户认不出自己敲的是哪条）",
			got[0].Path, "build/[")
	}
	if !strings.Contains(got[0].Err, "语法无效") {
		t.Fatalf("Err = %q，want 说明语法无效且已按未排除处理", got[0].Err)
	}
	// ② fail-open：那条模式本想排掉 build/x/f.o，坏语法 ⇒ 一个都没排掉。
	if !containsPrefix(scannedPaths(res), filepath.Join(root, "build", "x", "f.o")) {
		t.Fatalf("坏模式把文件排除了（本项只许报告、不许改判）：files=%v", scannedPaths(res))
	}
	if !containsPrefix(scannedPaths(res), filepath.Join(root, "keep", "f.o")) {
		t.Fatalf("夹具反了：keep/f.o 也该在（两棵都扫才算走到遍历）：files=%v", scannedPaths(res))
	}
}

func TestWalkCleanPatternsReportNothing(t *testing.T) {
	root := badPatFixture(t)
	stubVerdict(t, nil)

	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludePaths: []string{"build/**", "keep"}}, 2)

	if got := excludeFailed(res); len(got) != 0 {
		t.Fatalf("正常模式被报成语法无效：%v（负控制失效）", got)
	}
	// 同一批模式确实生效（否则上面那个"零上报"只是过滤器整体没跑）。
	if paths := scannedPaths(res); len(paths) != 0 {
		t.Fatalf("两个正常排除模式都没排掉任何东西：%v", paths)
	}
}

// TestWalkCancelledProbeLegEmitsNoExcludeNotice 折叠判据没问全那一趟，整条 Result 都必须是
// 空的——排除模式那条报告也**不例外**。它排在 dedupeRoots 的早退之后，位置本身就是一条约定：
// 那一趟什么都没扫，就不该留下任何"这次扫描的产出"。
func TestWalkCancelledProbeLegEmitsNoExcludeNotice(t *testing.T) {
	root := badPatFixture(t)
	restoreProbeSeam(t)
	probeCaseVerdict = func(context.Context, string) (fscase.Result, error) {
		return fscase.Result{}, context.Canceled
	}

	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludePaths: []string{"build/["}}, 2)

	if len(res.Failed) != 0 {
		t.Fatalf("探测被中断那一趟仍产出了失败项 %v（应与 R2-2 同一口径：空 Result）", res.Failed)
	}
}

func containsPrefix(paths []string, want string) bool {
	w := filepath.ToSlash(want)
	for _, p := range paths {
		if p == w {
			return true
		}
	}
	return false
}
