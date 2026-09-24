package scanner

// M212（2026-09-24 第五轮审查批）：被宽根覆盖的**具名根**必须自己作为根被遍历。
// 修前形状：dedupeRoots 丢弃被覆盖子根 ⇒ 它既不被 submit，宽根遍历又要在隐藏/保护
// 剪枝处停下 ⇒ "用户明明点名了，整棵子树一个文件都扫不到、零留痕"。
// 本机复现（fdd-cli）：`T T/.hidden/inner` → files_total=2；单指 inner → 1。
// 修法口径（联合语义）：扫 cleaned 全集 ∪ 每个被覆盖具名根作为根各扫一遍；
// **未**被点名的隐藏兄弟维持剪枝（开放是"根级"的，不是"祖先目录级"的）。

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
	"filededup/internal/sysguard"
)

func countFilesWith(res *Result, suffix string) int {
	n := 0
	for _, f := range res.Files {
		if strings.HasSuffix(filepath.ToSlash(f.Path), suffix) {
			n++
		}
	}
	return n
}

// P-212-a：隐藏具名根（B 格）+ 负控制 + 防双扫。修前红在第一格。
func TestCoveredHiddenRootIsScanned(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, ".hidden", "inner")
	mkDirFiles(t, inner, "x.bin")
	mkDirFiles(t, filepath.Join(root, ".hidden"), "sibling.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")

	res := Walk(context.Background(), []string{root, inner}, &model.Filters{}, 2)
	if !hasPathWith(res, "/.hidden/inner/x.bin") {
		t.Error("被宽根覆盖的隐藏具名根整棵漏扫（修前形状）——用户点名的根必须自己作为根被遍历")
	}
	if hasPathWith(res, "/.hidden/sibling.bin") {
		t.Error("未被点名的隐藏兄弟被连带扫入——联合语义不是「放弃全部隐藏剪枝」")
	}
	if n := countFilesWith(res, "/x.bin"); n != 1 {
		t.Errorf("x.bin 计 %d 次，want 1（visited 预登记必须防住双扫）", n)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want 空", res.Failed)
	}
}

// P-212-a2：非隐藏的可见具名根——改前经宽根下降已能扫到，本批防的是
// "covered 根另行 submit 后 visited 去重失守 ⇒ 双扫双计"。
func TestCoveredVisibleRootScannedOnce(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "visible", "sub")
	mkDirFiles(t, sub, "y.bin")

	res := Walk(context.Background(), []string{root, sub}, &model.Filters{}, 2)
	if n := countFilesWith(res, "/y.bin"); n != 1 {
		t.Errorf("y.bin 计 %d 次，want 1（根提交与父下降互斥）", n)
	}
}

// P-212-b：保护+隐藏叠加（A 格，假警示）。.Spotlight-V100 是 darwin 清单的
// **点开头**目录名条目 ⇒ 修前逃逸分支记了"已脱离系统保护"，隐藏规则却照样剪枝，
// 警示与事实相反。清单以 PlatformDarwin 注入（惯例同 scanner_guard_test.go 的
// PlatformWindows 注入），三条 CI 腿都能跑——比设计段 §2.3 预定的 darwin build tag
// 更宽，偏差记 §6.40。
func TestCoveredProtectedHiddenRootWarningIsTrue(t *testing.T) {
	setGuard(t, sysguard.New(sysguard.PlatformDarwin))
	root := t.TempDir()
	inner := filepath.Join(root, ".Spotlight-V100", "inner")
	mkDirFiles(t, inner, "z.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")

	res := Walk(context.Background(), []string{root, inner}, &model.Filters{}, 2)
	if !hasPathWith(res, "/.Spotlight-V100/inner/z.bin") {
		t.Error("警示记了「已脱离系统保护」却没真扫——修前假警示形状")
	}
	if len(res.UnprotectedRoots) != 1 || filepath.ToSlash(res.UnprotectedRoots[0]) != filepath.ToSlash(inner) {
		t.Errorf("UnprotectedRoots = %v, want [%s]", res.UnprotectedRoots, inner)
	}
}

// covered 分类真值（设计段 §2.3 变异 M-212-2 的靶）：
// "被宽根严格覆盖"与"同树另一拼写（折叠键相等）"必须分家——后者进 covered
// 就等于把 M66 防住的"两种拼写各走一遍"放回来。
func TestDedupeRootsCoveredClassification(t *testing.T) {
	base := t.TempDir()
	sens := func(v bool) {
		probeCaseVerdict = func(context.Context, string) (fscase.Result, error) {
			return fscase.Result{Sensitive: v, Proven: true}, nil
		}
	}
	t.Cleanup(func() { probeCaseVerdict = fscase.VerdictCtx })

	// 0）不敏感卷、同树两拼写：kept=1，covered **必须空**（拼写重复不作二次提交）
	sens(false)
	aLower := filepath.Join(base, "a")
	aUpper := filepath.Join(base, "A")
	kept, _, _, _, covered, _ := dedupeRoots(context.Background(), []string{aLower, aUpper}, false)
	if len(kept) != 1 || len(covered) != 0 {
		t.Fatalf("拼写重复：kept=%v covered=%v, want 1/0", kept, covered)
	}

	// 1）宽根 + 两个折叠等价的具名子根：covered 只收先到的一个（coveredFkeys 互斥）
	sens(false)
	kept, _, _, _, covered, _ = dedupeRoots(context.Background(),
		[]string{base, aLower, aUpper}, false)
	// 两条折叠等价拼写中**恰一条**进 covered（平局按原样串排序，'A'<'a' ⇒ 先到者可能是 A）。
	if len(kept) != 1 || len(covered) != 1 {
		t.Fatalf("折叠等价子根：kept=%v covered=%v, want 1/1", kept, covered)
	}
	if b := filepath.Base(covered[0]); b != "a" && b != "A" {
		t.Fatalf("covered[0]=%v, want base/a 或 base/A", covered[0])
	}

	// 2）敏感卷：两拼写是两棵真树，各自进 covered
	sens(true)
	_, _, _, _, covered, _ = dedupeRoots(context.Background(),
		[]string{base, aLower, aUpper}, false)
	if len(covered) != 2 {
		t.Fatalf("敏感卷双拼写：covered=%v, want 2 条", covered)
	}

	// 3）单根早退腿：covered 恒空
	sens(false)
	_, _, _, _, covered, _ = dedupeRoots(context.Background(), []string{base}, false)
	if len(covered) != 0 {
		t.Fatalf("单根：covered=%v, want 空", covered)
	}
}
