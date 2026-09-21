//go:build !windows

package ads

import "testing"

// V9（本轮新增，设计 §5.4 的 V1~V8 之外）：非 Windows 平台上 Check 必须恒放行。
// 钉的是 probe_other.go——它是 Linux 用户的**实际执行路径**（不是死代码：执行器
// 无条件调 adsCheck），一旦被误改成"枚举不了就拒"，Linux 上每次清理都会失败。
func TestCheckAllowsOnNonWindows(t *testing.T) {
	for _, p := range []string{"/", "/tmp", "does-not-exist-at-all", ""} {
		if got := Check(p); got.Reject {
			t.Errorf("Check(%q).Reject = true（%s），非 Windows 平台必须恒放行", p, got.Reason)
		}
	}
}
