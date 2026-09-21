package main

// P0-1 / P1-1 回归：扫描-操作生命周期与互斥。
// 这些路径跑在 goroutine 里并以事件收尾，此前因无法注入事件出口而完全无测试覆盖。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/model"
	"filededup/internal/ops"
)

// eventRecorder 替换 a.emit，记录终止事件。
type eventRecorder struct {
	mu     sync.Mutex
	events []string
	// payloads 按事件名记录**最后一次**载荷。
	// 只记名字是不够的：`ops:filtered` 这类事件的价值全在载荷里
	// （过滤前后计数、未命中目录），不校验载荷就等于没测。
	payloads map[string]any
	done     chan string
}

func (e *eventRecorder) emit(_ context.Context, name string, args ...interface{}) {
	e.mu.Lock()
	e.events = append(e.events, name)
	if len(args) > 0 {
		if e.payloads == nil {
			e.payloads = map[string]any{}
		}
		e.payloads[name] = args[0]
	}
	e.mu.Unlock()
	switch name {
	case "scan:done", "scan:error", "scan:cancelled", "ops:done", "ops:error", "ops:undo:done":
		select {
		case e.done <- name:
		default:
		}
	}
}

// has 报告某事件是否已发出（不消费终止队列）。
func (e *eventRecorder) has(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, n := range e.events {
		if n == name {
			return true
		}
	}
	return false
}

// lastWith 取出某事件最后一次携带的载荷。
func (e *eventRecorder) lastWith(name string) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, ok := e.payloads[name]
	return p, ok
}

// names 已发出的事件名快照（失败信息里用得到）。
func (e *eventRecorder) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

func (e *eventRecorder) waitTerminal(t *testing.T, tag string) string {
	t.Helper()
	select {
	case ev := <-e.done:
		return ev
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: 未收到扫描终止事件（疑似永久卡住）", tag)
		return ""
	}
}

func newProbeApp(t *testing.T) (*App, *eventRecorder, string) {
	t.Helper()
	root := t.TempDir()
	payload := []byte("SHARED-DUPLICATE-CONTENT-PAYLOAD")
	os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "b.bin"), payload, 0o644)

	// M93：App 构造收归 newHistApp（原先这里自己复制了一份四行构造，
	// 且**从不接 a.hist**。写前账本是 fail-closed 的，无账本的 App 永远走不到
	// 互斥门的下游，于是三条互斥用例的绿全部来自别的门 —— 见 §22.2 变异取证）。
	a, rec := newHistApp(t)
	return a, rec, root
}

// newOperableApp 在 newProbeApp 之上把 App 铺成「结果集就绪、清理请求能一路走到
// 互斥门」的状态，返回一个结果集内可操作文件的 id。
//
// 为什么必须一起铺三件（resultsReady / groups / 账本）：ExecuteOperation 的门禁链
// 是 opsRunning → scanInFlight → resultsReady → len(groups) → 写前账本
// （app.go:1805-1820、:1776-1787）。缺一件，请求就停在互斥门**下游**，
// 把 `if a.scanInFlight` 整条删掉用例也不会红（改前真读数：**-count=6 全绿）。
//
// 组内两个成员都指向不存在的路径：清理必然 ENOENT → Skipped，不必真造文件
// 就能走完受理与收尾路径（app_history_test.go:35 mkHistGroup 的既有手法）。
func newOperableApp(t *testing.T) (*App, string, uint64) {
	t.Helper()
	a, _, root := newProbeApp(t)
	dir := t.TempDir()
	g := mkGroup(1, 100, filepath.Join(dir, "gone-a"), filepath.Join(dir, "gone-b"))
	a.mu.Lock()
	a.resultsReady = true
	a.groups = []*model.DuplicateGroup{g}
	a.byID[g.Files[1].ID] = g.Files[1]
	a.mu.Unlock()
	return a, root, g.Files[1].ID
}

// assertIdleAppAcceptsOps 前提自检（独立于被测门禁）：什么都不钉时，
// 一个可操作请求必须**真的被受理**。
//
// 自检失败即当场红，不允许继续往下断言 —— 那意味着夹具穿不过某道下游门，
// 于是「被拒」这个结果无法归因给互斥门禁，正是 M93 改前那种「恒绿却什么都没测」
// 的形状。判据本身不依赖被测代码的行为，只依赖门禁链的形状。
func assertIdleAppAcceptsOps(t *testing.T) {
	t.Helper()
	a, _, id := newOperableApp(t)
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}}); err != nil {
		t.Fatalf("前提自检失败：空闲 App 的可操作请求应被受理，实际被拒：%v"+
			"（夹具穿不过互斥门下游的某道门 ⇒ 本文件的互斥断言无法归因到被测门禁）", err)
	}
	a.wg.Wait() // 不等到收尾就返回，会让下一段的在途标志不再是「真实空闲」
}

// fireOps / fireScans 并发发 n 个请求，逐格回收结果。
//
// 每个 goroutine 只写 errs[i] 自己那一格，受理计数走 atomic ——
// M92 那次 CI DATA RACE 就是裸 ++（run 35644606017，读/写同指一行）。
func fireOps(t *testing.T, a *App, id uint64, n int) (int64, []error) {
	t.Helper()
	var accepted atomic.Int64
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}})
			if err == nil {
				accepted.Add(1)
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	return accepted.Load(), errs
}

func fireScans(a *App, root string, n int) (int64, []error) {
	var accepted atomic.Int64
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := a.StartScan(model.ScanConfig{Roots: []string{root}})
			if err == nil {
				accepted.Add(1)
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	return accepted.Load(), errs
}

// assertAllRejected 双条件判据：一条都不许受理，**且每条拒因都必须是那句互斥文案**。
// 只断「被拒」是不够的：任何一道别的门替它挡下请求，用例都会绿而产品门禁可以已被拆掉。
func assertAllRejected(t *testing.T, tag string, accepted int64, errs []error, wantReason string) {
	t.Helper()
	if accepted != 0 {
		t.Fatalf("%s: 应零受理，实际受理 %d 次", tag, accepted)
	}
	for i, err := range errs {
		if err == nil || !strings.Contains(err.Error(), wantReason) {
			t.Fatalf("%s: 第 %d 条拒因不是被测互斥门（期望含 %q）：%v", tag, i, wantReason, err)
		}
	}
}

// P0-1：连续两次扫描都必须正常完成（修正前第二次被状态机拒绝，
// 而 StartScan 已清空结果集 → 前端永久卡在「扫描中」且结果为空）。
func TestSecondScanCompletes(t *testing.T) {
	a, rec, root := newProbeApp(t)

	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("StartScan#1: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#1"); ev != "scan:done" {
		t.Fatalf("scan#1 终止事件 = %s", ev)
	}
	r1, _ := a.GetResultGroups(ResultQuery{})
	if r1.Total == 0 {
		t.Fatal("scan#1 应至少找出 1 组")
	}

	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("P0-1: StartScan#2 被拒: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#2"); ev != "scan:done" {
		t.Fatalf("P0-1: scan#2 终止事件 = %s（期望 scan:done）", ev)
	}
	r2, _ := a.GetResultGroups(ResultQuery{})
	if r2.Total != r1.Total {
		t.Fatalf("P0-1: 二次扫描结果不一致: %d vs %d", r2.Total, r1.Total)
	}
}

// P0-1：取消后也能重新扫描。
func TestScanAfterCancel(t *testing.T) {
	a, rec, root := newProbeApp(t)
	// 造足够大的数据集，保证 Cancel 落在运行中而非收尾后
	for i := 0; i < 4000; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("x%04d.bin", i)),
			[]byte(fmt.Sprintf("payload-%d", i%7)), 0o644)
	}
	id1, err := a.StartScan(model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	for a.GetStatus() == string(model.StatusIdle) {
		time.Sleep(2 * time.Millisecond)
	}
	if err := a.CancelScan(); err != nil {
		t.Fatalf("运行中取消应成功: %v", err)
	}
	ev := rec.waitTerminal(t, "scan#1(cancel)")
	if ev != "scan:cancelled" {
		t.Skipf("扫描在取消前已完成（环境过快），跳过：taskID=%s ev=%s", id1, ev)
	}
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("P0-1: 取消后重新扫描被拒: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#2"); ev != "scan:done" && ev != "scan:cancelled" {
		t.Fatalf("scan#2 终止事件 = %s", ev)
	}
	// P3：终态下再取消应报错而不是静默成功
	if err := a.CancelScan(); err == nil {
		t.Error("P3: 任务已结束时 CancelScan 应返回错误")
	}
}

// P1-1：清理操作在途时不得开启新扫描（否则 ops goroutine 收尾会覆盖新结果集）。
//
// M93 补强（不是修死门禁 —— 变异 M-R1-c 下改前这条也 6/6 转红，它一直是活的；
// 这里只把**归因**从侥幸变成保证）：改前只断「err != nil」而不看拒因，并用
// sleep+轮询 Status 等上一次扫描收尾，而 Status 变 Done 早于 scanInFlight 复位
// （复位在 app.go:669）。落在那个窗口里时，拒因来自上游的 scanInFlight 门，
// 删掉 opsRunning 门禁这条也不会红。现在等终止事件（scan:done 必晚于复位）
// 并断拒因，另加一条锁内自检钉住「上游门是开的」。
func TestStartScanRejectedWhileOpsRunning(t *testing.T) {
	a, rec, root := newProbeApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan#1"); ev != "scan:done" {
		t.Fatalf("scan#1 终止事件 = %s", ev)
	}
	a.mu.Lock()
	// 前提自检：拒因归因要求 scanInFlight 这道上游门此刻是开的。
	if a.scanInFlight {
		a.mu.Unlock()
		t.Fatal("前提自检失败：scan:done 之后 scanInFlight 仍为真，无法把拒因归到 opsRunning 门")
	}
	a.opsRunning = true // 模拟 ops goroutine 在途
	a.mu.Unlock()
	_, err := a.StartScan(model.ScanConfig{Roots: []string{root}})
	if err == nil {
		t.Fatal("P1-1: 操作执行中应拒绝新扫描（结果集会被陈旧回写覆盖）")
	}
	if !strings.Contains(err.Error(), "清理操作执行中") {
		t.Fatalf("P1-1: 拒因不是 opsRunning 门（互斥门禁可能已被别的门替挡）：%v", err)
	}
	a.mu.Lock()
	a.opsRunning = false
	a.mu.Unlock()
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatalf("空闲时应可扫描: %v", err)
	}
	if ev := rec.waitTerminal(t, "scan#2"); ev != "scan:done" && ev != "scan:cancelled" {
		t.Fatalf("scan#2 终止事件 = %s", ev)
	}
}

// P1-1 对称（phase A）：扫描在途时不得开启清理操作。
//
// M93 重写。改前判据只有「err == nil ⇒ Fatal」一条，而该夹具因缺 a.hist 与
// resultsReady 根本走不到互斥门：把 `if a.scanInFlight` 整条删掉，
// 用例 -count=6 仍然全绿（§22.2 变异 M-R1-a/M-R1-b 真读数）。
// 现在换成双条件：零受理 **且** 每条拒因含"扫描进行中"，并加两条前提自检
// （空闲时可受理 / 解除钉住后立刻恢复受理），使「被拒」这一结果只能来自互斥门。
func TestExecuteOperationRejectedWhileScanInFlight(t *testing.T) {
	assertIdleAppAcceptsOps(t)
	a, _, id := newOperableApp(t)

	a.mu.Lock()
	a.scanInFlight = true // 模拟扫描 goroutine 收尾在途
	a.mu.Unlock()

	accepted, errs := fireOps(t, a, id, 6)
	assertAllRejected(t, "P1-1 phase A（扫描在途 ⇒ 拒清理）", accepted, errs, "扫描进行中")

	// 前提自检的另一半：放开钉住后同一 App 必须立刻恢复受理。
	// 不放开就直接红 ⇒ 排除「夹具本身就永远拒」这种假绿。
	a.mu.Lock()
	a.scanInFlight = false
	a.mu.Unlock()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}}); err != nil {
		t.Fatalf("P1-1: 解除 scanInFlight 后应恢复受理，实际仍被拒：%v", err)
	}
	a.wg.Wait()
}

// P1-1（phase B）：清理在途时不得开新扫描，双向闭合的另一半。
//
// M93 重写。原用例判据是 `scanOK>0 && opsOK>0`，把两类**先后**各受理一次当成违规；
// 实测三次里两次两侧全 0（整条用例什么都没断言），第三次证明的那格也不是它声称的那一格
// （§22.2 表 4）。改后：用 opsExecuteFn 接缝让一次**真实受理**的清理停在执行器内，
// opsRunning 不再手写，然后要求每条 StartScan 零受理且拒因含"清理操作执行中"。
//
// ★ 设计段 §22.2 另规划的「不钉标志、两侧并发、断受理数 <= 1」那一格
// **未实现**：正确实现下扫描足够快时第二个扫描被先后受理属正常，
// 该判据必然假红（详见 §6.18 的偏离交代）。
func TestScanAndOpsAreMutuallyExclusive(t *testing.T) {
	assertIdleAppAcceptsOps(t)
	a, root, id := newOperableApp(t)

	enter, release := make(chan struct{}), make(chan struct{})
	prev := opsExecuteFn
	opsExecuteFn = func(o ops.Options, op model.OpRequest) model.OpsResult {
		close(enter)
		<-release
		return prev(o, op)
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		opsExecuteFn = prev
	})

	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}}); err != nil {
		t.Fatalf("前提自检失败：清理请求应被受理，实际被拒：%v", err)
	}
	<-enter // 确认真的进到执行器内部（不是被某道门拒掉后空等）

	accepted, errs := fireScans(a, root, 6)
	assertAllRejected(t, "P1-1 phase B（清理在途 ⇒ 拒扫描）", accepted, errs, "清理操作执行中")

	close(release)
	a.wg.Wait()
	a.mu.Lock()
	stillRunning := a.opsRunning
	a.mu.Unlock()
	if stillRunning {
		t.Fatal("前提自检失败：ops goroutine 已收尾而 opsRunning 未复位（后续断言的拒因无从归因）")
	}
}
