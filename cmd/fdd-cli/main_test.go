package main

import "testing"

// M82 防分叉钉（设计稿 §27.2，2026-09-22 裁定「全仓字节单位统一成二进制命名」）：
// CLI 这份 humanBytes 与 internal/ops 的 humanSize（recycle_policy_test.go 的 TestHumanSize
// 已钉住）必须报同一派单位。两侧**各自**钉同一批字面量，不做跨包调用——humanSize 是
// 包内函数，为一条测试导出它属于反向扩大生产面。
// ★ 精度两侧本来就不同（这里 %.1f 到底，ops 那侧 GiB/TiB 用 %.2f），所以钉的是
// 「同一个字节数落在哪个单位档、后缀写法」，不是小数位。
func TestHumanBytesUsesBinaryUnitNames(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{1 << 20, "1.0MiB"},
		{1 << 30, "1.0GiB"},
		{1 << 40, "1.0TiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Fatalf("humanBytes(%d) = %q, want %q（十进制写法 KB/MB/GB 与 internal/ops 及手册 09 的 KiB/MiB 分叉）",
				c.in, got, c.want)
		}
	}
}
