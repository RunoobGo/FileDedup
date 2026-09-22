package dedup

// M62+M85 计数链第二跳（设计稿 §28.2 ②）：Pipeline 必须**按轮**转存这个计数。
//
// 为什么要单独钉这一跳：绑定层的 scan:done 载荷与 CLI 报告都从 Pipeline 取数，
// 而不是从 scanner.Result（流水线后面的阶段会改写结果集）。所以 scanner 那边
// 数得再准，这里少一行 `Store` 就是"引擎有数、界面零值"；少一行 `Store(0)` 归零
// 则是"上一轮的未确证串进本轮报告"——两条都是 M21（workTempSkipped）已经警告过的
// 形状。★ 三轮的**次序**是判据的一部分（轮 3 前面必须留一个非 0 的轮 2），理由写在原地。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

func TestPipelineCaseProbeUnprovenIsPerRound(t *testing.T) {
	base := t.TempDir()
	missing := []string{filepath.Join(base, "gone-a"), filepath.Join(base, "gone-b")}

	// 夹具前提自检（I5 同口径）：不许"我猜它读不到卷型"。不存在的目录上探针必失败，
	// 若哪天这条不成立（例如卷型改成按父目录解析），本条会红在前提而不是判据上。
	if v := fscase.Verdict(missing[0]); v.Proven {
		t.Fatalf("夹具前提不成立：不存在的目录 %q 竟被确证（%+v）⇒ 测不到「未确证」那一格", missing[0], v)
	}

	p := New()

	// 轮 1：两根**可写**真目录 ⇒ 探针①成功 ⇒ 读数确证 ⇒ 0。
	// ★ 用两根而不是单根：单根走 C1 提前 return，压根不问卷，0 那个值在两种成因上同形。
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
			t.Fatalf("夹具前提不成立：可写目录 %q 竟未确证（%+v）⇒ 测不到「确证归零」那一格", dir, v)
		}
	}
	if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: live, Threads: 2}); err != nil {
		t.Fatalf("Run（真根）: %v", err)
	}
	if got := p.CaseProbeUnproven(); got != 0 {
		t.Fatalf("两根都确证 ⇒ CaseProbeUnproven 应为 0，实得 %d", got)
	}

	// 轮 2：换成两个不存在的根 ⇒ 计数必须被本轮的 2 **覆盖**掉（少一行 Store 就停在 0）。
	if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: missing, Threads: 2}); err != nil {
		t.Fatalf("Run（不存在的根）: %v", err)
	}
	if got := p.CaseProbeUnproven(); got != 2 {
		t.Fatalf("两根都退默认 ⇒ 应为 2，实得 %d（少一行 Store 就是这一格红）", got)
	}

	// 轮 3：★ 一个已取消的 ctx ⇒ Run 在 Walk 之后、那一组 Store **之前**就 return。
	// 这一格才是"按轮归零"那行真正的受众：本轮压根走不到赋值，只有 `Store(0)` 能保证
	// 读到 0 而不是上一轮留下的 2。
	// ★ 为什么它必须排在轮 2 之后：初版把取消轮排在"真根→0"之后，于是进轮 3 时值本来就是
	// 0，删掉归零行也看不出差别 —— 变异 M25-h 当场存活（实测 rc=0）。要让归零行有受众，
	// 前一轮必须**留下非 0**。三格的次序就是本用例的判据的一部分。
	if _, _, err := p.Run(canceledCtx(), model.ScanConfig{Roots: live, Threads: 2}); err == nil {
		t.Fatal("夹具前提不成立：已取消的 ctx 下 Run 竟正常返回 ⇒ 测不到「本轮没走到赋值」那一格")
	}
	if got := p.CaseProbeUnproven(); got != 0 {
		t.Fatalf("本轮在赋值之前就返回 ⇒ 计数必须是归零后的 0，实得 %d（上一轮的 2 串进本轮）", got)
	}
}

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
