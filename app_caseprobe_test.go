package main

// M62+M85 计数链第三跳（设计稿 §28.2 ③）：Pipeline 的取值必须真的进到 scan:done 载荷。
//
// 为什么这一跳要单独钉：绑定层的载荷是**手写字段名**的结构体字面量，漏掉一行赋值不会
// 编译失败，只会让界面与 CLI 看到一个永久的 0 —— 而 0 恰好是"一切正常"的形状，
// 假账因此无声（M21 那族同口径的账目当年就是靠 grep 而非测试保住的，本轮补上真读数）。
// ★ 同时跑两轮：第二轮的 0 只有在第一轮报过非 0 之后才有意义，单看第二轮分不清
// "赋值了且归零正确"与"从没赋值"。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

func TestScanSummaryCarriesUnprovenVerdictCount(t *testing.T) {
	a, rec := newHistApp(t)
	base := t.TempDir()
	missing := []string{filepath.Join(base, "gone-a"), filepath.Join(base, "gone-b")}
	for _, dir := range missing {
		if v := fscase.Verdict(dir); v.Proven {
			t.Fatalf("夹具前提不成立：不存在的目录 %q 竟被确证（%+v）", dir, v)
		}
	}

	live := make([]string, 0, 2)
	for _, name := range []string{"live-a", "live-b"} {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
		live = append(live, dir)
	}
	for _, dir := range live {
		if v := fscase.Verdict(dir); !v.Proven {
			t.Fatalf("夹具前提不成立：可写目录 %q 竟未确证（%+v）", dir, v)
		}
	}

	scan := func(roots []string) ScanSummary {
		t.Helper()
		if _, err := a.StartScan(model.ScanConfig{Roots: roots, Threads: 2}); err != nil {
			t.Fatalf("StartScan: %v", err)
		}
		if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
			t.Fatalf("scan 终止事件 = %s", ev)
		}
		p, ok := rec.lastWith("scan:done")
		if !ok {
			t.Fatal("找不到 scan:done 载荷")
		}
		s, ok := p.(ScanSummary)
		if !ok {
			t.Fatalf("scan:done 载荷类型 = %T，want ScanSummary", p)
		}
		return s
	}

	if got := scan(missing).CaseProbeUnproven; got != 2 {
		t.Fatalf("ScanSummary.CaseProbeUnproven = %d，want 2（漏一行赋值不会编译失败，只会永久报 0）", got)
	}
	if got := scan(live).CaseProbeUnproven; got != 0 {
		t.Fatalf("第二轮两根都确证 ⇒ 载荷必须是本轮的 0，实得 %d（上一轮串进本轮）", got)
	}
}

// ★ 载荷的**线上名字**不在这里钉：M30 的比对器（wails_types_test.go 的 wailsMirrors
// 表里有 ScanSummary 这一对）两个方向都查 —— Go 加了字段而 TS 没镜像、或 tag 与 TS
// 字段名不一致，都当场红。再写一份反射断言就是第二份判据（I5）。
