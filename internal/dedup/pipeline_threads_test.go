package dedup

// M410（04 §6.80 B 态 → §6.81 销格）：`MaxThreads = 64` 的上钳两层都在位、两层都零判据。
//
// 改前现读（复取见 §6.81）：
//   - `grep -rn "MaxThreads" --include='*_test.go' .` 零命中 ⇒ 全仓没有一枚测试提到这枚常量；
//   - 测试里 `Threads:` 的取值集合 = {1,2,4}（本包）／{1,2,3,4,8}（根包）
//     ⇒ 六档里的 `0`／`64`／`65`／`10000`／`-1` 从未进过任何判据，唯一进过的 `1` 也只是"能跑"而不是"被断言"。
//
// 本文件只加判据。被检的 `normalizeThreads` 是从 `Scan` 语句逐字抽出的等价提取
// （抽的理由：钳后值原先是 Scan 的局部变量，不动实现就没有观察通道），判定逻辑零改动。
//
// ★ 期望值一律写成**字面量 64** 而不是 `MaxThreads`：钉的是"64 这个具体阈值"，
//   不是"有个上限"这件事——写符号的话，把常量本身改成 128 用例照样绿，那正是 M410
//   要防的漂移之一（变异 V-h）。

import "testing"

func TestNormalizeThreadsClampTable(t *testing.T) {
	roots := []string{t.TempDir()}
	auto := autoThreads(roots)

	cases := []struct {
		why  string
		in   int
		want int
		// bounded=false 的档位走自动腿：上钳管的是**显式并发度**，
		// 自动腿的界由 autoThreads 自己的契约（G6「只降级不升级」）管，这里不越权断言。
		bounded bool
	}{
		{"零走自动腿", 0, auto, false},
		{"下界原样", 1, 1, true},
		{"阈值上沿原样", 64, 64, true},
		{"刚越界钳住", 65, 64, true},
		{"离谱值钳住", 10000, 64, true},
		{"负数走自动腿", -1, auto, false},
	}
	for _, c := range cases {
		got := normalizeThreads(c.in, roots)
		if got != c.want {
			t.Errorf("%s: normalizeThreads(%d) = %d, want %d", c.why, c.in, got, c.want)
		}
		if c.bounded && (got < 1 || got > 64) {
			t.Errorf("%s: 生效值 %d 越出显式档的 [1..64]", c.why, got)
		}
	}

	// 0 与 -1 必须落进同一档（都走自动腿）：钉住"负数不是报错、也不是钳到 1"。
	if a, b := normalizeThreads(0, roots), normalizeThreads(-1, roots); a != b {
		t.Errorf("0 与 -1 应同走自动腿：实际 %d vs %d", a, b)
	}
}
