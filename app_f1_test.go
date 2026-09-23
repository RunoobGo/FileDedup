package main

// Task F1（2026-09-23 第四轮全仓审查，J-6 裁定「一行级六条随批修」）——六条里**有行为面**的两条。
//
// ① `StartScan` 的锁内清态漏了 `a.lastEvs`：`GetScanProgress` 的注释自称"断线重连语义"，
//   但重扫之后、本轮第一条进度事件到达之前，它回吐的是**上一轮的终值**（百分比/语料数都是旧的）。
//   ★ 一条如实的收窄：这条绑定目前**前端没有消费者**（`wails.ts` 有包装函数、`frontend/src` 无调用点，
//   界面上的进度读的是事件流，`startScan` 自己把 `progress.value` 置了 null）⇒
//   本项修的是**绑定层契约**，不是界面上看得见的缺陷，划账不许写成"修好一处显示错误"。
//
// ② `superseded()` 的早退挪到了 `SaveScan` 之前。改前的顺序是"落库（锁外，慢）→ 再判取代 → 弃写"，
//   于是被取代的那一轮照样占一格 `history.MaxScanHistory`，把最旧的**有效**记录挤掉（CASCADE 连子行删）。
//   ★ 两条用例的"改前红"都不存在：卡点 `scanAboutToSaveHook` 与清态那一句都是新符号，
//   改前树只会得到"没有这个导出/没有这一句"——红在错误的格子上。修对必红的证据由**变异**提供
//   （Fa：把早退挪回 SaveScan 之后 ⇒ ②红；Fb：删掉 `a.lastEvs = ProgressEvent{}` ⇒ ①红），逐字读数见 §30.11。
//
// ③④⑤⑥ 是注释与退出路径，本机不可行为级取红 ⇒ 由文件末尾那枚静态钉守住"改过的地方不许悄悄退回去"。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filededup/internal/model"
)

// mkScanRoot 造一个必产生重复组的扫描根（两份同内容 + 另两份同内容）。
func mkScanRoot(t *testing.T, tag string) string {
	t.Helper()
	root := t.TempDir()
	for i, pair := range [][]string{{"a1.bin", "a2.bin"}, {"b1.bin", "b2.bin"}} {
		payload := []byte(fmt.Sprintf("%s-PAYLOAD-%d-DUPLICATE", tag, i))
		for _, name := range pair {
			if err := os.WriteFile(filepath.Join(root, name), payload, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// ①：重扫之后、本轮第一条进度事件到达之前，GetScanProgress 必须是"本轮还没数据"，
// 而不是上一轮的终值。卡点放在进度回调上（它正是 `lastEvs` 的唯一写者）。
func TestStartScanClearsStaleProgress(t *testing.T) {
	root := mkScanRoot(t, "PROGRESS")
	a, rec := newHistApp(t)

	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "第一轮"); ev != "scan:done" {
		t.Fatalf("第一轮终止事件 = %s", ev)
	}
	origOnProgress := a.pipe.OnProgress
	t.Cleanup(func() { a.pipe.OnProgress = origOnProgress })

	a.mu.Lock()
	stale := a.lastEvs
	a.mu.Unlock()
	// 前提自检放在断言之前（M91 的教训）：夹具没造出读数时，红要落在"前提"那一格。
	if stale.FilesDone == 0 && stale.FilesTotal == 0 {
		t.Fatalf("用例前提不成立：第一轮没在 lastEvs 里留下任何计数（%+v）", stale)
	}

	// 第二轮：第一条进度事件卡在写 lastEvs **之前**，于是这一段读到的只可能是 StartScan 留下的值。
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	a.pipe.OnProgress = func(ev model.ProgressEvent) {
		once.Do(func() { close(entered) })
		<-release
		origOnProgress(ev)
	}
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		close(release)
		t.Fatal("用例前提不成立：第二轮一条进度事件都没发出，卡点从未生效")
	}
	got := a.GetScanProgress()
	close(release) // 无论断言结果如何都放行，避免把 goroutine 永久挂着

	if got != (model.ProgressEvent{}) {
		t.Fatalf("重扫后 GetScanProgress 仍回吐上一轮终值：%+v（旧值 %+v）", got, stale)
	}
	if ev := rec.waitTerminal(t, "第二轮"); ev != "scan:done" {
		t.Fatalf("第二轮终止事件 = %s（放行卡点后必须能收尾）", ev)
	}
}

// ②：被新扫描取代的那一轮**不得**留下历史行。
// 时序全部由 scanAboutToSaveHook 钉住：A 停在"即将落库"这一点，B 起跑（代际当场 +1），
// 放行后 A 必须判为已被取代并直接返回。
func TestSupersededScanWritesNoHistoryRow(t *testing.T) {
	root := mkScanRoot(t, "SUPERSEDED")
	a, rec := newHistApp(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	scanAboutToSaveHook = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	t.Cleanup(func() { scanAboutToSaveHook = nil })

	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("用例前提不成立：A 没走到落库卡点（收尾顺序被改动了？）")
	}

	// B 起跑：StartScan 在锁内 resultGen.Add(1)，返回时 A 的代际已确定过期。
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		close(release)
		t.Fatalf("A 停在卡点期间 B 起不来（说明在途互斥把这段也占了）：%v", err)
	}
	close(release)

	if ev := rec.waitTerminal(t, "B"); ev != "scan:done" {
		t.Fatalf("B 终止事件 = %s", ev)
	}
	// A 的判定在放行后只是一次原子比较，B 的 scan:done 远晚于它；这里再让出一小段，
	// 免得"改前会多写一行"那条路径因调度而假绿。
	time.Sleep(100 * time.Millisecond)

	ms, err := a.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("历史行数 = %d, want 1：被取代的扫描不得占一格 MaxScanHistory"+
			"（多出的那一行来自 A，它会把最旧的有效记录挤掉）：%s", len(ms), historyIDs(ms))
	}
}

func historyIDs(ms []HistoryMeta) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, fmt.Sprintf("%d", m.ID))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ---------- 静态钉（③④⑤⑥）----------

// 四条本机取不到行为级红的改动：注释与 CLI 退出路径。这里钉"改对的形状不许退回去"。
// 手法先例：ops 侧读 filter.go 源文件的 P-1 钉。★ 只锚标识符与判据句，不锚整段措辞。
func TestF1StaticAnchors(t *testing.T) {
	appSrc := readSource(t, "app.go")
	cacheSrc := readSourceAt(t, filepath.Join("internal", "cache", "cache.go"))
	cliSrc := readSourceAt(t, filepath.Join("cmd", "fdd-cli", "main.go"))

	// ①：清态那一句必须在 StartScan 的锁内段里（与 groups 置 nil 同一临界区）。
	if !strings.Contains(appSrc, "a.lastEvs = model.ProgressEvent{") {
		t.Fatal("①：StartScan 不再重置 lastEvs，GetScanProgress 会回吐上一轮终值")
	}

	// ③a：结构体字段注释不许退回"goroutine 在途"那种过宽说法。
	if !strings.Contains(appSrc, `scanInFlight bool               // 扫描"结果集尚未写回"的在途标志`) {
		t.Fatal(`③a：scanInFlight 的字段注释退回了"goroutine 在途"——复位点在写回那一拍，不是整条 goroutine`)
	}

	// ④：cache 的 Dev 注释不许再写"Windows 恒 0"。
	if strings.Contains(cacheSrc, "Windows 恒 0") {
		t.Fatal("④：cache 的 Dev 注释又出现\"Windows 恒 0\"——fsid I7 起 Windows 从句柄取卷序列号")
	}
	if !strings.Contains(cacheSrc, "fsid_windows.go:201") {
		t.Fatal("④：Dev 注释失去指向 fsid 句柄腿的锚，后来人无法复核\"不再恒 0\"的依据")
	}
	// ④b：同一个错处在 Lookup 的文档注释里还有第二处（把"未解析"括注成 Windows 专属）。
	if strings.Contains(cacheSrc, "id 未解析（Windows）") {
		t.Fatal("④b：Lookup 注释又把\"未解析\"缩回 Windows 专属——跳过身份比较的条件是 !id.Resolved，与平台无关")
	}

	// ⑤：CLI 的三条失败退出路径都要显式收口；旧的裸 defer 不许回来。
	// ★ 只钉**代码行**：F1⑤ 的说明注释本身就引用了那个旧写法，用全文 Contains 会把
	//   "解释为什么禁止"当成"违反了禁止"，红得没有道理（④ 同一课，见下方 ④b）。
	for _, line := range strings.Split(cliSrc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "defer cch.Close()") {
			t.Fatal("⑤：`defer cch.Close()` 退回来了——os.Exit 路径不会跑 defer")
		}
	}
	if n := strings.Count(cliSrc, "closeCache()"); n < 4 {
		t.Fatalf("⑤：closeCache() 显式调用 = %d 处, want ≥4（defer + 三条 os.Exit 路径）", n)
	}

	// ⑥：GetOpRecord 的网络卷风险登记不许被删。
	if !strings.Contains(appSrc, "F1⑥") {
		t.Fatal("⑥：GetOpRecord 的逐条同步 Lstat 风险说明不见了（该条按 J-6 只登记、不改行为）")
	}
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	return readSourceAt(t, name)
}

func readSourceAt(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读不到 %s（静态钉无法执行，不得读作通过）：%v", rel, err)
	}
	return string(b)
}
