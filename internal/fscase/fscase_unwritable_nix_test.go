//go:build !windows

package fscase

// 第八轮批 3（拟 M369）：只读目录夹具**只在 unix 上有前提**。
//
// 2026-09-29 第八轮审查 §3.1c 的取证：这一格原来住在无 build tag 的 `fscase_test.go` 里，
// 唯一的自保是 `os.Geteuid() == 0` 那句 Skip——而 Windows 上 `Geteuid()` 恒为 **-1**，
// 那道闸永不开 ⇒ windows 腿用 `os.Mkdir(ro, 0o500)` 造不出"写不进去的目录"
// （模式位在 Windows 只表达只读属性，见 M354 同族读数），断言比的又是 `Default()`，
// 于是**摘掉 `fscase.go` 那条"探测失败退回默认"的守卫，这一格照绿**。
// 本仓第四次撞同一形状（M213 / M335 / M354 一脉；§6.40 / §6.66 同族），
// 修法照先例：把 Unix-only 前提那一格放进带 `//go:build !windows` 的文件，
// Windows 那一格转 docs/05 真机清单（W6 组），**不拿 CI 近似读数冒充**。
//
// ★ 代价如实：windows 腿的用例清单比另两条腿**少一格**，三条腿自此不等量平移；
//   本机 darwin 只能证明"加了 tag 后 unix 腿读数不变"，证明不了 Windows 那一格——
//   那句"Windows 上不可写目录退回默认"在本批是**未兑现**，不是已通过。

import (
	"os"
	"path/filepath"
	"testing"
)

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
