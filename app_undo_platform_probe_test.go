package main

// APP-6（2026-09-21 全量审查，I5 + H6）的门禁：把「这类操作在这个平台上能不能
// 应用内回撤」与「不可撤时该说哪句」钉成**同一条判据的两侧**。
//
// 改前的现场（两份内联判据，各自读 runtime.GOOS）：
//   - beginJournal 落库 `OpMeta.Undoable`：`kind != "delete" && !(kind=="trash" && GOOS=="windows")`
//   - undoableReason 文案分流：`if kind == "trash" && GOOS == "windows"`
// 两份漂移的后果是"账本说可撤、界面说不可撤"（或反过来），而这类矛盾从任何一次
// 单平台运行里都看不出来——本机永远是 darwin，Windows 那条腿只能 t.Skip。
//
// 平台真值进参数后换来的是**新覆盖面**：三平台 × 五类操作在同一份代码上一次性断言。
// 本文件就是兑现这份覆盖：
//   ① 真值表：undoableFor 在 windows/darwin/linux 下的取值逐格钉死（字面表，不重算公式）；
//   ② 分流互斥：非 Windows 的 trash 文案不得出现 Windows 专属引导（去掉 `!undoableFor`
//      守卫即红），Windows 的 trash 文案不得换成永久删除那套（判据与文案串台即红）；
//   ③ 同源：导出入口 undoableReason 必须与 undoableReasonFor(kind, 本机) 逐字相同。
//
// ★ 取证性质（必须如实登记，不得冒充"改前必红"的第一手读数）：APP-6 是把两份
// 语义相同的实现收归一份，**行为不变**，所以改前没有红可读。本文件的红-绿靠
// 变异取证：把 undoableFor 的 Windows 支删掉、把 undoableReasonFor 的守卫删掉，
// 两个方向各红一次，读数见 04 §6.11。

import (
	"runtime"
	"strings"
	"testing"
)

// undoableTruthTable 是判据的**独立真值表**：格子里写的是"应该是什么"，
// 不是从生产实现重算出来的公式——否则测试与实现同源，实现错了它跟着错，
// 等于没测（本仓"前提自检必须独立于被测物"不变式）。
//
// kind 全集取自 ops/executor.go:391 那台操作类型的分发 switch，
// 一处增类即需在此同步增行（漏行由 TestUndoableForTruthTable 的覆盖率断言拦）。
var undoableTruthTable = []struct {
	kind string
	goos string
	want bool
}{
	{"delete", "windows", false}, {"delete", "darwin", false}, {"delete", "linux", false},
	{"trash", "windows", false}, {"trash", "darwin", true}, {"trash", "linux", true},
	{"move", "windows", true}, {"move", "darwin", true}, {"move", "linux", true},
	{"hardlink", "windows", true}, {"hardlink", "darwin", true}, {"hardlink", "linux", true},
	{"symlink", "windows", true}, {"symlink", "darwin", true}, {"symlink", "linux", true},
}

// allOpKinds 与真值表同源维护：新增操作类型时，分发型 switch、真值表、这张表三处
// 都要动，任何一处漏了都有用例红（而不是静默少测一类）。
var allOpKinds = []string{"delete", "trash", "move", "hardlink", "symlink"}

func allGooses() []string { return []string{"windows", "darwin", "linux"} }

// TestUndoableForTruthTable 逐格钉死 undoableFor 的三平台取值。
func TestUndoableForTruthTable(t *testing.T) {
	// 前置自检：真值表必须把 kind × goos 铺满，漏一格就是"这一格没人测"。
	covered := map[string]bool{}
	for _, c := range undoableTruthTable {
		covered[c.kind+"\x00"+c.goos] = true
	}
	for _, k := range allOpKinds {
		for _, g := range allGooses() {
			if !covered[k+"\x00"+g] {
				t.Fatalf("真值表缺格 kind=%s goos=%s（新增操作类型或平台时要同步补格）", k, g)
			}
		}
	}
	for _, c := range undoableTruthTable {
		if got := undoableFor(c.kind, c.goos); got != c.want {
			t.Errorf("undoableFor(%q, %q) = %v，应为 %v", c.kind, c.goos, got, c.want)
		}
	}
}

// TestUndoableReasonForCoversEveryNonUndoableCell 每一条"不可撤"都必须有**属于它自己
// 这一类**的解释文案，不得串台。
//
// 这条是 APP-6 收归判据后新拿到的防护：undoableFor 若将来把某一类新操作判为不可撤
// （例如 symlink 在某平台也拿不到落点映射），undoableReasonFor 会把它兜进"永久删除"
// 那段文案里——落库的 undoable=false 配上一句物理上说不通的理由，就是假话。
// 串台一旦发生在 delete/trash 之外的 kind 上，本用例即红。
func TestUndoableReasonForCoversEveryNonUndoableCell(t *testing.T) {
	// 每个 kind 的文案必须出现的关键字（自述其类）与禁止出现的关键字（别人的类）。
	type copyRule struct {
		mustHave    []string
		mustNotHave []string
	}
	rules := map[string]copyRule{
		"delete": {mustHave: []string{"永久删除"}, mustNotHave: []string{"回收站不支持"}},
		"trash":  {mustHave: []string{"回收站"}, mustNotHave: []string{"永久删除"}},
	}
	checked := 0
	for _, k := range allOpKinds {
		for _, g := range allGooses() {
			if undoableFor(k, g) {
				continue // 可撤的格子在生产里根本不会被问文案
			}
			checked++
			msg := undoableReasonFor(k, g)
			if msg == "" {
				t.Fatalf("kind=%s goos=%s 判为不可撤却给了空文案", k, g)
			}
			ru, ok := rules[k]
			if !ok {
				t.Fatalf("kind=%s 被 undoableFor 判为不可撤，但 undoableReasonFor 没有为它写文案："+
					"它会兜进别人的解释里（假话）。请为新 kind 补分支并登记进本用例。\n文案：%s", k, msg)
			}
			for _, want := range ru.mustHave {
				if !strings.Contains(msg, want) {
					t.Errorf("kind=%s goos=%s 文案缺少 %q：%s", k, g, want, msg)
				}
			}
			for _, bad := range ru.mustNotHave {
				if strings.Contains(msg, bad) {
					t.Errorf("kind=%s goos=%s 文案串到了别的类别（含 %q）：%s", k, g, bad, msg)
				}
			}
		}
	}
	if checked != 4 { // delete×3 + trash@windows：格子数一变即说明判据集合变了
		t.Fatalf("不可撤格子数 = %d，应为 4（delete×3 + trash@windows）；判据集合变了要同步复核文案分支", checked)
	}
}

// TestUndoableReasonForBranchesByPlatformNotJustKind 钉住"文案分流问过 undoableFor"：
// 同样 kind=trash，Windows 与非 Windows 必须给出**不同**的解释。
//
// 为什么单列一条：若分流退化成只看 kind（`if kind == "trash"`），非 Windows 的 trash
// 也会拿到"打开系统回收站"那段——而本机 trash 本就可撤，生产永远不问它，于是这个
// 错误在本机完全观测不到。真值表钉的是判据，这一条钉的是判据与文案**之间的连线**。
func TestUndoableReasonForBranchesByPlatformNotJustKind(t *testing.T) {
	win := undoableReasonFor("trash", "windows")
	lin := undoableReasonFor("trash", "linux")
	dar := undoableReasonFor("trash", "darwin")
	if win == lin || win == dar {
		t.Fatalf("Windows 与 Linux/Darwin 的 trash 文案逐字相同，说明文案分流没问平台判据：%s", win)
	}
	for _, g := range []string{"linux", "darwin"} {
		if strings.Contains(undoableReasonFor("trash", g), "打开系统回收站") {
			t.Fatalf("%s 的 trash 给出了 Windows 专属引导：%s", g, undoableReasonFor("trash", g))
		}
	}
}

// TestUndoableReasonIsRuntimeWrapperOfFor 导出入口只是"补上本机平台真值"的一层薄壳：
// 若它自己另写一套分流，undoableFor 的门禁就管不到实际发给界面的那句。
func TestUndoableReasonIsRuntimeWrapperOfFor(t *testing.T) {
	for _, k := range append(allOpKinds, "some-future-kind") {
		if got, want := undoableReason(k), undoableReasonFor(k, runtime.GOOS); got != want {
			t.Fatalf("undoableReason(%q) 与 undoableReasonFor(%q, 本机) 不同源：\n got=%s\nwant=%s",
				k, k, got, want)
		}
	}
}
