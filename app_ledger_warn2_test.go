package main

// APP-3（2026-09-21 全量审查，设计稿 §15.0-A）：M7 的统一出口还漏着两处。
//
// M7 的裁定是"账本没写进去必须让用户看见"，出口 warnLedger = stderr + app:error
// 双通道，理由写在函数注释里：**打包后的 GUI 没有控制台，只写 stderr 等于没写**。
// 本轮全量审查在同族里数出第 5、6 处漏网：
//
//   ① ExecuteOperation 收尾的 hs.PruneScanFiles 失败 → 只 fmt.Fprintf(os.Stderr, ...)，
//      注释原话"属可忽略的陈旧关联，留痕即可"。可"留痕"留在了用户看不见的地方，
//      而后果是历史行仍列着已删文件（下次从历史页恢复会得到不存在的路径）。
//   ② shutdown 的在途任务未在 grace 内收口 → 也只 stderr。这一条直接就是
//      "本次落账可能缺失"，与 M7 那四处是同一件事。
//
// ②的探针要把 grace 缩短，否则单测真等 10 秒；因此把 inflightDrainGrace 从
// const 降为 var（生产取值一字未改，只是让"超时"成为可构造的形状）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"filededup/internal/model"
	"filededup/internal/ops"
)

// captureAppErrors 把 a.emit 包一层，收集**全部** app:error 载荷，并返回还原函数。
//
// 为什么不能只看 eventRecorder.payloads：那个 map 按事件名只留最后一条，
// 而这里要断言的恰恰是"某一处漏网"——只看末条等于让前面几处隐身。
func captureAppErrors(a *App) (got *[]string, restore func()) {
	var out []string
	prev := a.emit
	a.emit = func(ctx context.Context, name string, args ...interface{}) {
		if name == "app:error" && len(args) > 0 {
			if m, ok := args[0].(map[string]string); ok {
				out = append(out, m["error"])
			}
		}
		prev(ctx, name, args...)
	}
	return &out, func() { a.emit = prev }
}

// TestPruneScanFilesFailureIsVisible 历史裁剪失败必须走统一出口。
func TestPruneScanFilesFailureIsVisible(t *testing.T) {
	a, rec, _ := linkedHistApp(t)
	got, restore := captureAppErrors(a)
	t.Cleanup(restore)

	prev := opsExecuteFn
	opsExecuteFn = func(o ops.Options, op model.OpRequest) model.OpsResult {
		closeLedger(t, a) // 执行完之后账本不可写 → 收尾的 Prune 必然报错
		return prev(o, op)
	}
	t.Cleanup(func() { opsExecuteFn = prev })

	a.mu.Lock()
	var sel []uint64
	for _, f := range a.groups[0].Files {
		sel = append(sel, f.ID)
	}
	a.mu.Unlock()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: sel}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	var sawPrune bool
	for _, txt := range *got {
		if strings.Contains(txt, "裁剪") {
			sawPrune = true
			if !strings.Contains(txt, "历史") {
				t.Fatalf("裁剪告警没说是哪一笔，用户无从判断影响面: %q", txt)
			}
		}
	}
	if !sawPrune {
		t.Fatalf("历史裁剪失败只写了 stderr（GUI 无控制台 ⇒ 等于没说），未经 app:error 出口。实际告警：%q（§15.0-A APP-3）", strings.Join(*got, " | "))
	}
}

// TestShutdownDrainTimeoutIsVisible 在途任务超时未收口必须走统一出口。
func TestShutdownDrainTimeoutIsVisible(t *testing.T) {
	a, rec := newHistApp(t)
	got, restore := captureAppErrors(a)
	t.Cleanup(restore)
	prevGrace := inflightDrainGrace
	inflightDrainGrace = 30 * time.Millisecond
	t.Cleanup(func() { inflightDrainGrace = prevGrace })

	a.wg.Add(1) // 永不归零：模拟一次不可中断的系统调用把 goroutine 卡在盘上
	t.Cleanup(func() { a.wg.Done() })

	a.shutdown(context.Background())

	if !rec.has("app:error") {
		t.Fatalf("在途超时只写了 stderr，事件序列 %v（§15.0-A APP-3）", rec.names())
	}
	if len(*got) == 0 || !strings.Contains((*got)[len(*got)-1], "落账") {
		t.Fatalf("超时告警未说明后果（本次落账可能缺失）: %q", strings.Join(*got, " | "))
	}
}
