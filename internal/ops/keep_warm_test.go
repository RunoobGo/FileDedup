package ops

// AS-R3 的配套：预热表与就地实测必须给出逐位一致的判定。
//
// 持锁调用方走 WarmSensitivity，非持锁调用方（FilterInDirs）走就地实测。
// 两条路一旦因键归一不一致而分叉，锁内那次的目录语义就会**静默**取到别的值
// （命中/不命中翻转），而这正是本函数想避免的那类"两处实现漂移"。

import (
	"testing"

	"filededup/internal/fscase"
)

func TestWarmSensitivityMatchesDirectProbe(t *testing.T) {
	clean := t.TempDir()
	dirs := []string{clean, "  " + clean + "  "}
	resolve := WarmSensitivity(dirs)
	for _, d := range dirs {
		if got, want := resolve(d), fscase.Sensitive(d); got != want {
			t.Errorf("目录 %q：预热表给出 %v，就地实测给出 %v（两者必须一致）", d, got, want)
		}
	}
	// 带尾空白与不带的必须算同一个目录（否则表命中不了，静默退回逐个实测）。
	if got, want := resolve("  "+clean+"  "), resolve(clean); got != want {
		t.Errorf("同一目录带/不带尾空白结果不同：%v vs %v（dirKey 归一失效）", got, want)
	}
}
