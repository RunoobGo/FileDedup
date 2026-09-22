package scanner

// M105 根侧传值（设计稿 §28.3 补记二；2026-09-22 裁定「按卷敏感度过渡」）。
//
// filter 那一侧只解决"给到 caseMode 就按卷放宽"；本文件钉的是**caseMode 从哪儿来**，
// §28.3 原文一句没写，是三格真实的判据：
//   1. 值必须**按根**给。混合根（外置 NTFS 盘 + 机内 apfs 目录）时，全局取"任一不敏感"
//      会在敏感卷上放宽（多排除=少扫，方向安全但对用户说谎），全局取"全部敏感"则外置盘
//      那一半 fail-open 原样留着 —— 两个都不是"按卷过渡"。
//   2. `dedupeRoots` 的第四个返回值必须与 kept **同序**：调用点拿 relativeTo 的根下标去取，
//      错位比不给下标更糟（把 A 卷的语义按到 B 卷的根上）。
//   3. C1 的收窄：单根只有在**用户写了排除模式**时才探测（没有消费方时探测结果无人可读，
//      "别往用户目录写探测文件"的原口径继续成立）。
//
// 三条都是纯逻辑 + 真目录夹具（t.TempDir），无 build tag，三条平台都跑；卷语义一律走
// probeCaseVerdict 注入点（H6），不去猜宿主卷的真实大小写敏感度。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// mkFile 造一个非空文件（0 字节被遍历器内置跳过，夹具不能踩那一格）。
func mkFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 判据 1：按根而不是全局。两个根各有一棵盘上拼写为 TEMP 的目录，排除模式写的是 Temp。
// ★ 反向那一格（敏感卷那半必须**照扫**）才是本条的价值所在：把它改成全局读数
// （"任一根不敏感就放宽"）时， insensitive 那半仍然绿，只有这一格会红。
func TestExcludeVerdictIsTakenPerRootNotGlobally(t *testing.T) {
	base := t.TempDir()
	insens := filepath.Join(base, "extdisk") // 扮演外置 NTFS/exFAT
	sens := filepath.Join(base, "internal")  // 扮演机内 apfs
	mkFile(t, filepath.Join(insens, "TEMP", "a.log"))
	mkFile(t, filepath.Join(insens, "keep", "b.log"))
	mkFile(t, filepath.Join(sens, "TEMP", "c.log"))
	mkFile(t, filepath.Join(sens, "keep", "d.log"))

	stubVerdict(t, map[string]fscase.Result{
		"extdisk":  {Sensitive: false, Proven: true},
		"internal": {Sensitive: true, Proven: true},
	})
	res := Walk(context.Background(), []string{insens, sens},
		&model.Filters{ExcludePaths: []string{"Temp"}, IncludeHidden: true}, 2)

	var got []string
	for _, e := range res.Files {
		got = append(got, filepath.Base(filepath.Dir(e.Path))+"/"+filepath.Base(e.Path))
	}
	joined := strings.Join(got, ",")
	if strings.Contains(joined, "TEMP/a.log") {
		t.Fatalf("不敏感卷那半没放宽（fail-open 原样留着）：%v", got)
	}
	if !strings.Contains(joined, "TEMP/c.log") {
		t.Fatalf("敏感卷那半被别的根的读数放宽了（全局取数的症状）：%v", got)
	}
	// 未命中排除的两棵 keep 必须都在，防"整趟都没扫"这种空转过绿。
	if !strings.Contains(joined, "keep/b.log") || !strings.Contains(joined, "keep/d.log") {
		t.Fatalf("keep 目录被牵连少扫：%v", got)
	}
}

// 判据 1 的第二条腿：**目录剪枝**也必须按根。字面前缀式（"Cache/Sessions"）是用户最常写的
// 排除形态，它在 filter 里走 pathnorm.Under 而不是 path.Match，是 §28.3 落点清单漏掉的那条。
func TestExcludeDirPruningFollowsOwningRootVerdict(t *testing.T) {
	base := t.TempDir()
	insens := filepath.Join(base, "usbstick")
	sens := filepath.Join(base, "ssd")
	deep := func(root string) { mkFile(t, filepath.Join(root, "CACHE", "sessions", "x.log")) }
	deep(insens)
	deep(sens)

	vInsensitive := fscase.Result{Sensitive: false, Proven: true}
	vSensitive := fscase.Result{Sensitive: true, Proven: true}
	stubVerdict(t, map[string]fscase.Result{"usbstick": vInsensitive, "ssd": vSensitive})
	res := Walk(context.Background(), []string{insens, sens},
		&model.Filters{ExcludePaths: []string{"Cache/Sessions"}, IncludeHidden: true}, 2)

	if len(res.Files) != 1 {
		var got []string
		for _, e := range res.Files {
			got = append(got, e.Path)
		}
		t.Fatalf("应只剩敏感卷那一条 CACHE/sessions/x.log，实得 %d 项：%v", len(res.Files), got)
	}
	if !strings.Contains(res.Files[0].Path, "ssd") {
		t.Fatalf("留下的那条不该是 %q（放宽的腿接反了）", res.Files[0].Path)
	}
}

// 判据 2：第四个返回值必须与 kept 同序。夹具里**夹一个会被丢弃的子根**（sub 被宽根
// extdisk 覆盖），这样"把排序后全量 verdicts 直接返回"与"返回与 kept 对齐的那份"才不同形。
func TestKeepVerdictsAlignedWithKeptRoots(t *testing.T) {
	base := t.TempDir()
	wide := filepath.Join(base, "wide")
	sub := filepath.Join(wide, "sub")
	sibling := filepath.Join(base, "zeta")
	for _, d := range []string{sub, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// ★ 三根的 Sensitive **必须同值**：M36 之后每根按自己那卷折叠，一卷敏感一卷不敏感时
	// 含大写的 t.TempDir() 路径会把 wide 与 wide/sub 折成不同形 ⇒ sub 根本不会被丢弃，
	// 本用例就红在夹具前提上（m62 测试的 ③ 格踩过同一格）。要"错位可测"只需 Proven 不同形。
	want := map[string]fscase.Result{
		"wide": {Sensitive: true, Proven: true},
		"sub":  {Sensitive: true, Proven: false}, // 与 wide/zeta 差在 Proven，专供错位可测
		"zeta": {Sensitive: true, Proven: true},
	}
	stubVerdict(t, want)
	kept, _, _, verdicts := dedupeRoots([]string{wide, sub, sibling}, false)
	if len(kept) != 2 || len(verdicts) != 2 {
		t.Fatalf("夹具应为 wide+zeta 两根（sub 被覆盖丢弃）、读数两份，实得 kept=%d verdicts=%d",
			len(kept), len(verdicts))
	}
	for i, r := range kept {
		got, exp := verdicts[i], want[filepath.Base(r)]
		if got != exp {
			t.Fatalf("verdicts[%d]（根 %s）= %+v，want %+v ⇒ 与 kept 不同序：下标取到的会是别的根的卷语义",
				i, filepath.Base(r), got, exp)
		}
	}
}

// 判据 3：C1 的收窄两个方向各钉一格。
// 反向那一格（无排除模式 ⇒ 一次都不问）保住 C1 的原意：最常走的那条路不往用户目录写探测文件。
func TestSingleRootProbesOnlyWhenExcludePatternsExist(t *testing.T) {
	root := filepath.Join(t.TempDir(), "solo")
	mkFile(t, filepath.Join(root, "TEMP", "a.log"))
	mkFile(t, filepath.Join(root, "keep", "b.log"))
	stubVerdict(t, map[string]fscase.Result{"solo": {Sensitive: false, Proven: true}})

	// ① 有排除模式：单根也必须探测，否则 caseMode 永远是"未确证" ⇒ 放宽腿接不上。
	//    没有这一格，"单根不探测"（C1）就会把 M105 在最常见形态上原样挡回来。
	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludePaths: []string{"Temp"}, IncludeHidden: true}, 2)
	if len(res.Files) != 1 || filepath.Base(res.Files[0].Path) != "b.log" {
		var got []string
		for _, e := range res.Files {
			got = append(got, e.Path)
		}
		t.Fatalf("单根带排除模式时 TEMP 未被排除（探测没为单根发起？）：%v", got)
	}

	// ② 无排除模式：探测没有消费方 ⇒ 一次都不发起（C1 原口径）。
	//    ★ 计数用的是重新换装的那条缝（calls 从 0 起），① 那次探测记在旧缝上、不算进来。
	calls := stubVerdict(t, map[string]fscase.Result{"solo": {Sensitive: false, Proven: true}})
	_ = Walk(context.Background(), []string{root}, &model.Filters{IncludeHidden: true}, 2)
	if n := calls.Load(); n != 0 {
		t.Fatalf("无排除模式的单根趟发起了 %d 次探测 ⇒ C1（不给最常走的路写探测文件）被破", n)
	}
}
