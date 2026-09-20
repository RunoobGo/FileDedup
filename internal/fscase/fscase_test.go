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
func TestProbeGivesUpWithoutTouchingStrangers(t *testing.T) {
	dir := t.TempDir()
	var stales []string
	for i := 0; i < probeAttempts; i++ {
		stales = append(stales, staleProbeAt(t, dir, probeNo.Load()+1))
	}
	if got := probe(dir); got != Default() {
		t.Fatalf("重试用尽后应退回平台默认，实得 %v", got)
	}
	for _, p := range stales {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("占位文件 %s 被删除: %v", filepath.Base(p), err)
		}
	}
}
