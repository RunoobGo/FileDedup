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
