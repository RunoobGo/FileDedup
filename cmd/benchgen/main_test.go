// AS-K6（2026-09-20 全仓审计）的回归：语料必须**由 -seed 决定**，且必须覆盖
// 扫描器的边界形态。
//
// 登记的原症状有两条，本文件逐条钉住：
//  1. 内容走 crypto/rand，`-seed` 只影响尺寸/副本数，"固定种子"名不副实：
//     同一 seed 两次生成的语料内容不同 → 冒烟里的"分组路径集合摘要"其实没有
//     跨运行的可比性，历史基线无法复算。
//  2. 生成集只有普通文件：硬链接、符号链接、`.fdd-old` 残留、隐藏文件、
//     大小写同名这些"扫描器有专门规则"的形态，在整条 CI 里一个都没出现过。
//     规则失效（缺陷 6 那类）时冒烟毫无反应。
//
// 第 2 条的钉法分两层：本文件断言"形态真的按预期建出来了"（文件系统语义），
// scripts/smoke-cli.sh 断言"真扫描器对这些形态的结论与 manifest 一致"
// （规则漂移时组数/语料口径立刻变红）。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"filededup/internal/worktemp"
)

// 冒烟量级：A 数据集 100000×scale → 50 个小文件，副本约 5 组。
const testScale = 0.0005

func buildCorpus(t *testing.T, dataset string, scale float64, seed int64) (string, manifest) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "bench")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return out, generate(out, dataset, scale, seed)
}

// corpusDigest 压平目录内容：相对路径 + 类型 + 内容摘要（符号链接取其目标串）。
//
// 只取**相对**路径：t.TempDir() 每次不同，绝对路径会把无关差异算进摘要。
// 符号链接不跟随（记目标字符串），否则同一条链接的目标内容会算两次，
// 摘要对"链接被换成实体文件"这类变化反而不敏感。
// rootRelative 把符号链接目标折成语料根内的相对路径。
//
// 必要而非洁癖：目标写的是绝对路径，而每个用例的 t.TempDir() 都不同——
// 不折算的话"同 seed 两次生成"会因无关的前缀差异必然失败，
// 断言就退化成永远红的噪声（真回归反而看不出来）。
func rootRelative(root, target string) string {
	abs, err := filepath.Abs(target)
	if err != nil {
		return target
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return target // 指向语料之外：绝对路径本身就是有意义的差异
	}
	return filepath.ToSlash(rel)
}

func corpusDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info() // DirEntry.Info 走 Lstat：符号链接不会被解引用
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			lines = append(lines, rel+"\x00link\x00"+rootRelative(root, target))
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, os.ErrPermission) { // 权限类形态（M 批另有覆盖）不入摘要
				lines = append(lines, rel+"\x00unreadable")
				return nil
			}
			return err
		}
		sum := sha256.Sum256(b)
		lines = append(lines, fmt.Sprintf("%s\x00file\x00%d\x00%s", rel, len(b), hex.EncodeToString(sum[:])))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	all := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(all[:])
}

// ---- 1. -seed 名副其实 ----

func TestSameSeedProducesIdenticalCorpus(t *testing.T) {
	dirA, mA := buildCorpus(t, "A", testScale, 42)
	dirB, mB := buildCorpus(t, "A", testScale, 42)

	dgA, dgB := corpusDigest(t, dirA), corpusDigest(t, dirB)
	if dgA != dgB {
		t.Fatalf("同 seed 两次生成的语料内容不同（A=%s B=%s）：内容源不受 seed 控制，"+
			"冒烟的分组摘要基线无法复算", dgA[:16], dgB[:16])
	}
	if mA.TotalFiles != mB.TotalFiles || len(mA.Groups) != len(mB.Groups) {
		t.Fatalf("同 seed 两次生成的 manifest 不一致：%+v vs %+v", mA, mB)
	}
}

func TestSeedActuallyDrivesContent(t *testing.T) {
	// 防"改过头"：若把内容源写成常量流，上一条测试会以另一种方式假绿
	// （摘要永远相同，seed 依然是装饰品）。
	dirA, _ := buildCorpus(t, "A", testScale, 42)
	dirB, _ := buildCorpus(t, "A", testScale, 43)
	if corpusDigest(t, dirA) == corpusDigest(t, dirB) {
		t.Fatal("不同 seed 生成的语料内容完全相同：seed 没有进入内容")
	}
}

// ---- 2. 边界形态真的存在 ----

func TestShapesProvideScannerEdgeCases(t *testing.T) {
	out, m := buildCorpus(t, "A", testScale, 42)
	in := func(name string) string { return filepath.Join(out, shapeSubdir, name) }

	// 硬链接对：两条路径都在，且是同一 inode，内容一致。
	ha, hb := in(hardAName), in(hardBName)
	sa, err := os.Lstat(ha)
	if err != nil {
		t.Fatalf("缺少硬链接源 %s: %v", ha, err)
	}
	sb, err := os.Lstat(hb)
	if err != nil {
		t.Fatalf("缺少硬链接对 %s: %v", hb, err)
	}
	if !os.SameFile(sa, sb) {
		t.Fatalf("%s 与 %s 不是同一 inode：硬链接形态没建出来，"+
			"阶段 1.5 的硬链接去重规则在 CI 里从未被真正跑过", ha, hb)
	}

	// 符号链接：链接本身不是普通文件（扫描器不跟随、不计数），目标可读。
	// 是否覆盖以 manifest.Shapes 为准，不按"Lstat 失败就算跳过"自证：
	// 声称建出来了却没建出来，正是必须变红的那种漂移。
	sl := in(symLinkName)
	li, linkErr := os.Lstat(sl)
	switch {
	case m.Shapes.Symlink && linkErr != nil:
		t.Fatalf("manifest 声称已建立符号链接，但 %s 不存在（%v）：门禁会把没覆盖当成覆盖了", sl, linkErr)
	case m.Shapes.Symlink:
		if li.Mode()&fs.ModeSymlink == 0 {
			t.Fatalf("%s 不是符号链接（mode=%v）", sl, li.Mode())
		}
		if target, err := os.Readlink(sl); err != nil || target != in(symTargetName) {
			t.Fatalf("符号链接目标异常：target=%q err=%v", target, err)
		}
		if _, err := os.Stat(sl); err != nil {
			t.Fatalf("链接目标不可读: %v", err)
		}
	case linkErr == nil:
		t.Fatalf("%s 存在但 manifest 记 shapes.symlink=false：口径与磁盘不一致", sl)
	default:
		t.Logf("本环境不支持符号链接（%v）：该形态本轮未覆盖，已由 manifest 如实登记", linkErr)
	}

	// .fdd-old 残留：与被保留文件逐字节相同，且判定入口认得它。
	// 这条把"缺陷 6"钉进语料：忽略规则一旦失效，重扫必然多出一个重复组。
	residual := in(residualName)
	if !worktemp.IsTempName(residualName) {
		t.Fatalf("worktemp.IsTempName(%q)=false：语料造出了扫描器会忽略、判定却不认的形态", residualName)
	}
	if !sameContent(t, in(keepName), residual) {
		t.Fatalf("%s 与 %s 内容不一致：残留形态失去意义（必须与被保留文件逐字节相同）",
			residual, in(keepName))
	}

	// 隐藏文件：内容刻意与某个已有组成员相同。IncludeHidden 规则失效时，
	// 它会并入既有组 → 冒烟的分组路径集合摘要立刻变化。
	hidden := in(hiddenName)
	hb2, err := os.ReadFile(hidden)
	if err != nil {
		t.Fatalf("缺少隐藏文件 %s: %v", hidden, err)
	}
	if len(m.Groups) == 0 {
		t.Fatal("manifest 没有任何重复组，隐藏变体的对账无从做起")
	}
	first := filepath.Join(out, m.Groups[0].Files[0])
	if hashBytes(hb2) != hashBytes(readFile(t, first)) {
		t.Fatalf("隐藏文件 %s 的内容未复用组 %s 的成员：规则失效时无法通过组变化暴露", hidden, first)
	}
	if !strings.HasPrefix(hiddenName, ".") {
		t.Fatalf("%q 不是隐藏命名，测不到 IncludeHidden 规则", hiddenName)
	}

	// 同名大小写对：只在大小写敏感卷上存在（不敏感卷上它俩是同一个文件，
	// 造出来的是一个假形态）。判据取 manifest.Shapes 而非重新问一次卷语义，
	// 并反向核对登记与磁盘一致——"声称没造却造出来了"同样是口径漂移。
	lower, upper := in(caseLowerName), in(caseUpperName)
	li, lerr := os.Lstat(lower)
	ui, uerr := os.Lstat(upper)
	switch {
	case m.Shapes.CasePair:
		if lerr != nil || uerr != nil {
			t.Fatalf("manifest 记 case_pair=true 但缺少文件（%s:%v %s:%v）", lower, lerr, upper, uerr)
		}
		if os.SameFile(li, ui) {
			t.Fatalf("%s 与 %s 是同一 inode：卷并不区分大小写，这个形态是假的", lower, upper)
		}
		if !sameContent(t, lower, upper) {
			t.Fatalf("%s 与 %s 内容不同：大小写同名对必须是同一份数据", lower, upper)
		}
		if !containsGroupPair(m, caseLowerName, caseUpperName) {
			t.Fatal("同名大小写对没有被记进 manifest 重复组：冒烟对账会把它当成误报")
		}
	case lerr == nil && uerr == nil && !os.SameFile(li, ui):
		t.Fatalf("两个仅大小写不同的名字已是两个不同 inode，manifest 却记 case_pair=false：卷语义与登记不符")
	}
}

// ---- 3. manifest 的 total_files 口径 ----

func TestManifestCountsOnlyCollectibleFiles(t *testing.T) {
	out, m := buildCorpus(t, "A", testScale, 42)

	// 独立复算"扫描器应收集到的文件数"（口径与 scanner+filter 对齐：
	// 非符号链接、非隐藏名、非工作临时名、非 0 字节、非 manifest.json）。
	got := 0
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil
		}
		name := d.Name()
		if name == "manifest.json" || strings.HasPrefix(name, ".") || worktemp.IsTempName(name) {
			return nil
		}
		if info.Size() == 0 {
			return nil
		}
		got++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == 0 {
		t.Fatal("语料为空")
	}
	if got != m.TotalFiles {
		t.Fatalf("manifest.total_files=%d，按扫描器口径应为 %d："+
			"benchgen 把不该计数的形态（链接/临时名/隐藏）算进了语料，或漏算了", m.TotalFiles, got)
	}
}

// ---- 辅助 ----

func sameContent(t *testing.T, a, b string) bool {
	t.Helper()
	ta, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	tb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return hashBytes(ta) == hashBytes(tb)
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 %s: %v", p, err)
	}
	return b
}

func hashBytes(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// containsGroupPair 判断 manifest 是否把这两个名字记成了**同一组**的两个成员。
// Files 存的是相对 out 的路径（如 "shapes/case_pair.bin"）。
func containsGroupPair(m manifest, lowerName, upperName string) bool {
	has := func(files []string, name string) bool {
		for _, f := range files {
			if filepath.ToSlash(filepath.Base(f)) == name {
				return true
			}
		}
		return false
	}
	for _, g := range m.Groups {
		if has(g.Files, lowerName) && has(g.Files, upperName) {
			return true
		}
	}
	return false
}
