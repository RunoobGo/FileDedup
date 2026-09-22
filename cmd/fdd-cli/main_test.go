package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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

// M62+M85 计数链第四跳（设计稿 §28.2 ④）的**线上名字**：GUI 的载荷有 M30 比对器管，
// CLI 这条 JSON 没有，字段名写错或漏加就是"报告里根本没这一键"。
//
// ★ 本条只钉得到 tag（marshal 出来的键名 + 值能落进去）；`main` 里那一行赋值删掉不会
// 编译失败、只会让这一键恒为 0 ⇒ 值连线由下一条的源码级钉负责，两条合起来才是第四跳。
func TestReportStatsExposesCaseProbeUnproven(t *testing.T) {
	var r report
	r.Stats.CaseProbeUnproven = 7
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"case_probe_unproven":7`) {
		t.Fatalf("stats 里必须有 \"case_probe_unproven\":7，实得 %s", b)
	}
}

// 负控制（AS-K6 同族）：上一条用的是 Contains，键名打错一位就查不出来。
// 这里喂一个必然不存在的键名，断言同一套查法**报缺** —— 否则 Contains 写成永远为真
// （比如把模式串误写成空串）时，上一条会假绿。
func TestReportStatsKeyCheckActuallyFails(t *testing.T) {
	var r report
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"case_probe_unproven_zzz":0`) {
		t.Fatal("负控制失效：不存在的键名也查得到 ⇒ Contains 那条断言是空的")
	}
}

// TestCLIStatsCaseProbeUnprovenIsWired 钉第四跳的**值连线**（设计稿 §28.2 ④）。
//
// 为什么是源码级而不是行为级：那一行赋值在 `main()` 里，没有可注入的接缝 ⇒ 删掉它
// 既不编译失败也不改 JSON 键名，只让这一键恒为 0，而 0 是合法值。本轮实测：变异
// M25-k（删行）后 `go test ./cmd/fdd-cli` 仍报 ok —— 上面两条都测不到这一格。
// ⇒ 手法照既有先例（M30 的比对器读 TS 源文件、M134/M142 静态查门禁脚本）：锚点
// 含等号两侧，故「删行」「搬走」「写成常量 0」三种改法都红。
//
// ★ 同族那六枚计数器（FilesTotal / CacheHits / ProtectedDirs / ProtectedFiles /
// SkippedCloudFiles / SkippedWorkTempFiles）没有这一层，是既有缺口；本条只补 M62+M85
// 自己新加的这一跳，不替它们补（约束 7：不扩大改动面）。
func TestCLIStatsCaseProbeUnprovenIsWired(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读 main.go: %v", err)
	}
	const anchor = "r.Stats.CaseProbeUnproven = int(p.CaseProbeUnproven())"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("第四跳的赋值必须恰好一行、原样在场（%s），实得 %d 次 ⇒ 这行被删、被搬走或被写成常量（CLI 有键但恒 0）", anchor, n)
	}
}
