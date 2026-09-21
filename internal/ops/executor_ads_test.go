package ops

// M6-P3（2026-09-21，设计稿 §5.4）备用数据流守卫在**执行器侧**的后果：
// 拦得住、记账不乱、真不动手、五种操作全覆盖。
//
// 分层：流名与 errno 的判定由 internal/ads 的参数化用例钉住（V1~V3/V9），
// 这里把接缝整个换成假判据——真夹具（NTFS 命名流）在 APFS/exFAT 上造不出来
// （`echo x > f.txt:note` 不会创建流），于是"拦没拦住""账记到哪一栏"这两件
// 真正属于执行器的事，在 Linux/darwin 门禁上就是真夹具真断言
// （与 setCloudCheck 同一手法）。

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"filededup/internal/ads"
	"filededup/internal/model"
)

func setAdsCheck(t *testing.T, fn func(string) ads.Outcome) {
	t.Helper()
	prev := adsCheck
	adsCheck = fn
	t.Cleanup(func() { adsCheck = prev })
}

// rejectAll 模拟"这个文件挂了命名流"。文案用 ads 包里那条真文案，
// 免得 V4 断言的"文案精确"变成对一个测试自造字符串的断言。
func rejectAll(string) ads.Outcome {
	return ads.Outcome{Reject: true, Reason: "文件含备用数据流，去重会丢失备用流内容，已拒绝操作"}
}

func allowAll(string) ads.Outcome { return ads.Outcome{} }

// countingTrash 包一层 mockTrash 并记录调用次数：V4 要证明的不只是"文件还在"，
// 而是"回收站实现**一次都没被调用**"。
func countingTrash(target string) (fn func([]string) (map[string]string, error), calls *int) {
	n := 0
	inner := mockTrash(target, false)
	return func(paths []string) (map[string]string, error) {
		n++
		return inner(paths)
	}, &n
}

// V4：注入"有备用流" → 该项 Failed、Stage=="ads"、文案精确、文件仍在原位、
// 回收站一次没被调用。
func TestAdsGuardBlocksAndTouchesNothing(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "t")
	trash, calls := countingTrash(target)
	setAdsCheck(t, rejectAll)

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: trash,
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})

	if len(res.OK) != 0 {
		t.Fatalf("被拦截项不得进 OK: %+v", res.OK)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("应恰好一条失败: %+v", res.Failed)
	}
	f := res.Failed[0]
	if f.Path != fx.dup1.Path {
		t.Errorf("失败项路径 = %q，want %q", f.Path, fx.dup1.Path)
	}
	if f.Stage != "ads" {
		t.Errorf("Stage = %q，want \"ads\"（复用 \"verify\" 会把两种处置建议完全不同的原因并成一条）", f.Stage)
	}
	if f.Err != "文件含备用数据流，去重会丢失备用流内容，已拒绝操作" {
		t.Errorf("文案 = %q", f.Err)
	}
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatalf("被拦截的文件不应被动过：%v", err)
	}
	if *calls != 0 {
		t.Errorf("回收站被调用了 %d 次，应为 0（拦截必须发生在动手之前）", *calls)
	}
	if _, err := os.Stat(filepath.Join(target, "dup1.bin")); !os.IsNotExist(err) {
		t.Errorf("被拦截项不该出现在回收站里")
	}
}

// V5：反向对照——判据放行时照常执行，证明 V4 不是"恒拒"。
func TestAdsGuardAllowsWhenClean(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "t")
	setAdsCheck(t, allowAll)

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(target, false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})

	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("放行时应照常执行: %+v", res)
	}
	if _, err := os.Stat(fx.dup1.Path); !os.IsNotExist(err) {
		t.Fatalf("放行时源文件应已被移走")
	}
	if _, err := os.Stat(filepath.Join(target, "dup1.bin")); err != nil {
		t.Fatalf("放行时文件应进了回收站：%v", err)
	}
	if res.Reclaimed != fx.dup1.Size {
		t.Errorf("放行项应计入 Reclaimed: got %d want %d", res.Reclaimed, fx.dup1.Size)
	}
}

// V6：五种 op.Kind 逐个跑。钉的是设计稿 E9 的结论——"接入点只有一处"。
// 每一格都跑两臂：先证明"无守卫时这一格本来会成功"（否则红是别的毛病造成的），
// 再证明"有守卫时恰好被 ads 拦下"。
func TestAdsGuardCoversEveryOpKind(t *testing.T) {
	kinds := []struct {
		kind  string
		setup func(t *testing.T, fx *fixture) (Options, model.OpRequest)
	}{
		{"trash", func(t *testing.T, fx *fixture) (Options, model.OpRequest) {
			return Options{Groups: []*model.DuplicateGroup{fx.group},
					TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false)},
				model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}}
		}},
		{"delete", func(t *testing.T, fx *fixture) (Options, model.OpRequest) {
			return Options{Groups: []*model.DuplicateGroup{fx.group}},
				model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true}
		}},
		{"move", func(t *testing.T, fx *fixture) (Options, model.OpRequest) {
			return Options{Groups: []*model.DuplicateGroup{fx.group}},
				model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID},
					TargetDir: filepath.Join(t.TempDir(), "moved")}
		}},
		{"hardlink", func(t *testing.T, fx *fixture) (Options, model.OpRequest) {
			return Options{Groups: []*model.DuplicateGroup{fx.group},
					KeepIDs: map[uint64]bool{fx.orig.ID: true}},
				model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID}}
		}},
		{"symlink", func(t *testing.T, fx *fixture) (Options, model.OpRequest) {
			requireSymlinkSupport(t, fx.dir)
			return Options{Groups: []*model.DuplicateGroup{fx.group},
					KeepIDs: map[uint64]bool{fx.orig.ID: true}},
				model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup1.ID}}
		}},
	}

	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			// 第一臂：无守卫时应成功
			fx := newFixture(t)
			setAdsCheck(t, allowAll)
			opts, req := k.setup(t, fx)
			res := Execute(opts, req)
			if len(res.OK) != 1 {
				t.Fatalf("前提不成立：%s 在无守卫时本应成功，实际 %+v（失败=%+v）",
					k.kind, res.OK, res.Failed)
			}

			// 第二臂：重建夹具，在守卫下必须被拦。
			// "什么都不该发生"的判据用**物理身份**而非路径存在性：hardlink/symlink
			// 合并成功后 dup 路径依然在（只是换了 inode / 变成链接），只看
			// os.Stat 成功会把它误判成"没动过"。
			fx2 := newFixture(t)
			setAdsCheck(t, rejectAll)
			opts2, req2 := k.setup(t, fx2)
			res2 := Execute(opts2, req2)

			if len(res2.OK) != 0 || len(res2.Skipped) != 0 {
				t.Fatalf("%s 未被拦下: ok=%+v skipped=%+v", k.kind, res2.OK, res2.Skipped)
			}
			if len(res2.Failed) != 1 || res2.Failed[0].Stage != "ads" {
				t.Fatalf("%s 的失败未记在 ads 阶段: %+v", k.kind, res2.Failed)
			}
			dupInfo, err := os.Lstat(fx2.dup1.Path)
			if err != nil {
				t.Fatalf("%s 被拦截后源文件应仍在原位：%v", k.kind, err)
			}
			if !dupInfo.Mode().IsRegular() {
				t.Errorf("%s 被拦截后 dup 变成了 %v（symlink 合并动过手）", k.kind, dupInfo.Mode())
			}
			origInfo, err := os.Stat(fx2.orig.Path)
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(origInfo, dupInfo) {
				t.Errorf("%s 被拦截后 dup 与 keep 成了同一个物理文件（hardlink 合并动过手）", k.kind)
			}
			// 内容未变：与组内另一份没被点名的 dup2 逐字节比（同组即同内容）
			raw, err := os.ReadFile(fx2.dup1.Path)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := os.ReadFile(fx2.dup2.Path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != string(ref) {
				t.Errorf("%s 被拦截后 dup 内容变了", k.kind)
			}
		})
	}
}

// V7：拒绝项的会计口径与回调状态。两件事在别的用例里都不成立性：
//   - 释放口径：没进 toProcess 就不该贡献任何字节（M-P3-h 的靶子是把拒绝记成
//     Skipped——Skipped 同样不进 Reclaimed，但会被上层当成"已达成"）；
//   - 写前日志：拒绝项必须回调 failed，否则回撤账本里没有它，
//     而用户明明在结果里看到这一项。
func TestAdsRejectAccountingAndCallback(t *testing.T) {
	fx := newFixture(t)
	setAdsCheck(t, rejectAll)

	var mu sync.Mutex
	var got []ItemResult
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
		OnItem:  func(r ItemResult) { mu.Lock(); got = append(got, r); mu.Unlock() },
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.Failed) != 2 {
		t.Fatalf("两条都该被拒: %+v", res.Failed)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("拒绝不得记成 Skipped（把\"没做成\"报成\"已达成\"）: %+v", res.Skipped)
	}
	if res.Reclaimed != 0 || res.LinkedBytes != 0 || res.SymlinkedBytes != 0 {
		t.Fatalf("拒绝项污染了会计口径: reclaimed=%d linked=%d symlinked=%d",
			res.Reclaimed, res.LinkedBytes, res.SymlinkedBytes)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("回调 %d 次，期望 2（每个被拒项恰一次）: %+v", len(got), got)
	}
	for _, r := range got {
		if r.State != "failed" {
			t.Errorf("%s 回调状态 = %q，want \"failed\"", r.OrigPath, r.State)
		}
		if r.Err == "" {
			t.Errorf("%s 回调未带原因（用户无从知道为什么没做成）", r.OrigPath)
		}
	}
}
