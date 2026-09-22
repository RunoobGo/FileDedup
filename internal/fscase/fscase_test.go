package fscase

// 2026-09-18 审查 I2 回归：大小写语义必须来自实测，且测完不留残渣。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSensitiveAgreesWithIndependentCheck 用与实现无关的方式复核探测结论：
// 自己写一个混合大小写文件，再按另一种大小写去 Lstat，命中即卷不敏感。
// 断言 Sensitive() 与这一现场事实一致（不敏感卷上 false、敏感卷上 true）。
func TestSensitiveAgreesWithIndependentCheck(t *testing.T) {
	dir := t.TempDir()
	const mixed = "FddCaseProbeCheck.dat"
	if err := os.WriteFile(filepath.Join(dir, mixed), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, altErr := os.Lstat(filepath.Join(dir, strings.ToLower(mixed)))
	truthInsensitive := altErr == nil // 换小写仍看得到同一文件 → 卷不敏感
	if got := Sensitive(dir); got == truthInsensitive {
		t.Fatalf("Sensitive(%q) = %v，与现场事实相反（卷不敏感=%v）", dir, got, truthInsensitive)
	}
	if err := os.Remove(filepath.Join(dir, mixed)); err != nil {
		t.Fatal(err)
	}
}

// TestSensitiveLeavesNoTrace 探测用完必须自删：扫描根里留下 .fdd-case-probe-*
// 会被用户当成自己的文件，也会在下次扫描时参与去重判定。
func TestSensitiveLeavesNoTrace(t *testing.T) {
	dir := t.TempDir()
	Sensitive(dir) // 无论命中哪条分支，都不得留文件
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("探测在目录里留下 %d 个条目: %s", len(names), names[0].Name())
	}
}

// TestSensitiveFallsBackToDefaultWhenUnwritable 只读/不可写目录不得瞎猜：
// 退回平台默认（与改动前的硬编码行为一致）。
func TestSensitiveFallsBackToDefaultWhenUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户无权限拒绝语义")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if got := Sensitive(ro); got != Default() {
		t.Fatalf("不可写目录返回 %v, want 默认 %v", got, Default())
	}
}

// TestSensitiveIsCached 同一目录重复询问必须给同一答案（探测结果按目录缓存）。
func TestSensitiveIsCached(t *testing.T) {
	dir := t.TempDir()
	first := Sensitive(dir)
	for i := 0; i < 5; i++ {
		if got := Sensitive(dir); got != first {
			t.Fatalf("第 %d 次询问翻转了结论: %v != %v", i+2, got, first)
		}
	}
}

// staleProbeAt 在 dir 里放下"上一次进程被 kill 在 create 与 remove 之间"
// 留下的探测文件（大写形式），返回其路径。n 是本次探测将要使用的序号。
func staleProbeAt(t *testing.T, dir string, n uint64) string {
	t.Helper()
	_, upper := probeNames(dir, n)
	if err := os.WriteFile(upper, []byte("stale-from-previous-run"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(upper) })
	return upper
}

// TestProbeNotDegradedByStaleCollision（M13，2026-09-20 全仓审计）：
// 修正前 probe 的预检查"另一种大小写形式必须当前不存在"一旦撞上残留就直接
// `return Default()`，而 probeNo 每次进程从 1 重启 → 残留名与首轮探测恒撞名 →
// **该目录的探测永久静默退化为平台默认**。在 macOS 自建的大小写敏感卷上
// Default()=false，折叠会把 A/ 与 a/ 当同一棵目录整棵漏扫——恰是本包立项要修的场景。
//
// 断言用"与干净目录同结论"而不是"等于某个固定值"：真实卷语义在各平台不同，
// 但**残留绝不能改变结论**这一条是平台无关的。
func TestProbeNotDegradedByStaleCollision(t *testing.T) {
	clean := t.TempDir()
	baseline := probe(clean)

	dir := t.TempDir()
	before := probeNo.Load()
	stale := staleProbeAt(t, dir, before+1)
	if got := probe(dir); got != baseline {
		t.Fatalf("撞上残留后结论从 %v 变成 %v：探测被静默降级", baseline, got)
	}
	// ★ 这条才是与卷语义无关的可观测证据：撞名后必须**换号继续测**。
	// 只断言"结论没变"在 CI 上是空的——默认值恰好等于本地卷的真值时，
	// 退化和正确在返回值上看不出差别（本包的受害场景恰恰是"默认 ≠ 现实"的卷）。
	if probeNo.Load() < before+2 {
		t.Fatalf("撞上残留后没有换号重试（序号只走到 %d）：预检查直接 return Default() 即为退化", probeNo.Load())
	}
	// 两种卷上占位文件都必须还在：不敏感卷上 O_EXCL 直接撞 EEXIST（根本没建过文件），
	// 敏感卷上本次只清自己创建的 lower。任何一条路径都不许去删不是本次建的东西。
	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("probe 删掉了不是本次创建的文件: %v", err)
	}
}

// TestProbeGivesUpWithoutTouchingStrangers 钉住重试的上界与"不替陌生人删文件"：
// 连续 probeAttempts 个名字全被占时退回默认值，而且一个占位文件都不许消失。
//
// ★ M62+M85 之后"重试用尽"是**三条**退默认出口之一（另两条：创建失败、upper 读不动），
// 所以这一格的收尾会先问卷型。测试把卷型钉成"读不到"（helper 见 verdict_result_test.go）：
// 不这么做的话，断言就在替 CI 那台机器的卷型背书 —— 真挂一张 FAT 镜像跑测试套件的机器上，
// `Sensitive` 会合理地等于 false 而 `Proven` 合理地等于 true。
func TestProbeGivesUpWithoutTouchingStrangers(t *testing.T) {
	restore := useVolumeType(t, "")
	defer restore()
	dir := t.TempDir()
	// ★ 夹具前提修复（本轮 M62+M85 发现，改前只占住**一个**名字）：循环里传的是
	// `probeNo.Load()+1`，而 Load 在循环内不变 ⇒ 八个迭代建的是同一个文件。probe
	// 第一轮撞名、第二轮就换号成功了，从没走到「重试用尽」那一格。改前这条一直绿：
	// 换号成功读到的 bool 恰好等于 Default()（不敏感卷 false、敏感卷 true），断言在
	// 返回值上看不出差别 —— 与上面那条 M13 用例注释说的「只断言结论等于默认值是空的」
	// 是同一件事，这次由 Proven 那一格把它暴露出来。
	// 这里不改断言、只让前提成立：占住 probe 下一步真正会用到的那八个号（本包无并发
	// 测试，probeNo 在两次调用之间不会被别人推进，故窗口就是 [start+1, start+8]）。
	start := probeNo.Load()
	var stales []string
	for i := 0; i < probeAttempts; i++ {
		stales = append(stales, staleProbeAt(t, dir, start+uint64(i)+1))
	}
	if len(stales) != probeAttempts {
		t.Fatalf("夹具前提不成立：占位名有重复（%d 个 want %d）⇒ 测不到「重试用尽」", len(stales), probeAttempts)
	}
	got := probe(dir)
	// 前提自检：必须真的用掉了八个号，否则本条测的不是「重试用尽」。
	if used := probeNo.Load() - start; used != probeAttempts {
		t.Fatalf("夹具前提不成立：本次只推进了 %d 个探测号（want %d）⇒ 没走到「重试用尽」那一格",
			used, probeAttempts)
	}
	if got.Sensitive != Default() {
		t.Fatalf("重试用尽后应退回平台默认，实得 %+v", got)
	}
	if got.Proven {
		t.Fatalf("重试用尽 + 卷型读不到 ⇒ 这一格没有任何证据，却报了确证：%+v", got)
	}
	for _, p := range stales {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("占位文件 %s 被删除: %v", filepath.Base(p), err)
		}
	}
}

// TestFoldSwapLegIsPlatformIndependent 补强（M64 变异 M-M64-b 暴露的覆盖缺口）。
//
// 现场：本包原先**一条 Fold 用例都没有**。把 Fold 的替换腿换成 filepath.ToSlash
// （unix 上即恒等映射）做变异，fscase 与 ops 的测试一路绿。这不是纸面风险：
// M26（04 §6.8.8）的成因正是"折叠串与前缀串的分隔符口径不一致"，而 dedupeRoots
// 的前缀判据直接吃 Fold 的输出——那条腿断了，Windows 上"同一棵树的两种拼写"
// 就不再合并，同一目录走两遍，重复组数与可释放空间虚高。
//
// 2026-09-22 M63 裁定后本用例**收窄为「大小写腿平台无关」**（设计稿 §27.3）：
// 分隔符腿改成按宿主平台注入，于是它不再能在任一主机上被同一组字面量钉住，
// 改由 fold 的显式 sep 参数承担（Windows 腿传 "\\"、unix 腿传 "/"），
// 宿主绑定那一格另见 TestFoldSeparatorLegFollowsHostPlatform。
// 收窄不等于删断言：下面四格一条都没减，只是把「不吃宿主」这件事从 Fold
// 挪到了 fold 的参数上。原第 157-161 行那条 FC-2 偏差钉（unix 上 Fold 仍折 "\"）
// 随改判据作废，它钉住的正是本次裁定要修掉的错误语义。
func TestFoldSwapLegIsPlatformIndependent(t *testing.T) {
	// 不敏感卷：反斜杠形与斜杠形必须折成同一个键（大小写也一起折）。
	if a, b := fold(`C:\A\b`, false, `\`), fold(`c:/a/B`, false, `\`); a != b {
		t.Errorf("不敏感卷上两种拼写未折成同键：%q vs %q", a, b)
	}
	// 敏感卷：只换分隔符、不动大小写——替换腿与折叠腿是两件事，必须各自可钉。
	if got, want := fold(`C:\A\b`, true, `\`), `C:/A/b`; got != want {
		t.Errorf("fold(%q, true, %q) = %q，want %q", `C:\A\b`, `\`, got, want)
	}
	// 误伤面：敏感卷上大小写不同的两棵目录不得被折成同一棵（M36 的判据前提）。
	if fold(`C:\A\b`, true, `\`) == fold(`C:\A\B`, true, `\`) {
		t.Error("敏感卷上 A\\b 与 A\\B 被折成同键：一棵树会整棵静默不被扫描")
	}
	// ★ 大小写腿必须与 sep 取值无关：同一条输入换分隔符口径，大小写结论不变。
	// 这一格才是本用例在 M63 之后真正要钉的"平台无关"。
	if got, want := fold(`C:\A\b`, false, `/`), `c:\a\b`; got != want {
		t.Errorf("fold(%q, false, %q) = %q，want %q：大小写腿被分隔符口径带跑了", `C:\A\b`, `/`, got, want)
	}
}

// TestFoldSeparatorLegFollowsHostPlatform 正向钉住 M63 改后的判据：
// Windows 折 "\"、非 Windows 不折，且 Fold 与按宿主分隔符注入的 fold 同源。
//
// 改前的 FC-2 偏差（unix 上 "a\b" 是合法文件名却被当结构）在这里第一次有钉子：
// 上一用例的第四格只证明"大小写腿不受 sep 影响"，证明不了"Fold 到底取哪个 sep"。
func TestFoldSeparatorLegFollowsHostPlatform(t *testing.T) {
	// Windows 腿：反斜杠是结构。
	if got, want := fold(`a\b`, true, `\`), "a/b"; got != want {
		t.Errorf("fold(Windows) = %q，want %q", got, want)
	}
	// unix 腿：反斜杠是普通文件名字符，原样保留。
	if got, want := fold(`a\b`, true, `/`), `a\b`; got != want {
		t.Errorf("fold(unix) = %q，want %q：合法文件名里的反斜杠被当成结构（M63 回归）", got, want)
	}
	// 这一格是 M63 的全部代价：改前两者同键 ⇒ unix 上 "a\b" 那棵目录整棵静默不被扫描。
	if fold(`a\b`, false, `/`) == fold(`a/b`, false, `/`) {
		t.Error(`a\b 与 a/b 折成同键：本机上它们不是同一个目录`)
	}
	// 同源钉：Fold 就是「按宿主分隔符注入」那一版，不许各写一份判据。
	for _, c := range []struct {
		p string
		s bool
	}{
		{`a\b`, true}, {`a\b`, false}, {`C:\A\b`, true}, {`C:\A\b`, false},
		{`/data/B/A`, false}, {`plain/path`, true},
	} {
		if got, want := Fold(c.p, c.s), fold(c.p, c.s, string(filepath.Separator)); got != want {
			t.Fatalf("Fold(%q, %v) = %q 与注入宿主分隔符的 %q 分叉", c.p, c.s, got, want)
		}
	}
	// 宿主真值：非 Windows 上 Fold 必须保住字面反斜杠。
	if filepath.Separator != '\\' {
		if got, want := Fold(`a\b`, true), `a\b`; got != want {
			t.Errorf("本机分隔符为 %q，Fold = %q，want %q", string(filepath.Separator), got, want)
		}
	}
}
