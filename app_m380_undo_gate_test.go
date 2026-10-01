package main

// 第九轮修订批 A1（拟 M380）：维护闩的覆盖面必须真的包住"回撤"这两条腿。
//
// 报告 P1-1 的现读事实：app.go 的 claimMaintenance 文档写着「双向闭合：维护先占住则
// 扫描/清理被拒」，而两个回撤入口（UndoOperation / UndoOperationItem）在锁内只问
// opsRunning 与 scanInFlight，**零处引用 a.maintaining**。于是"清空清理记录"整表删除
// 在途时，一笔回撤照样能挤进来：它读到的账本随时会被那次整表删除抽走，而文件已经动过。
//
// ★ 本文件不引用任何"修复后才存在"的东西：`maintaining` 字段、claimMaintenance、
//   ledgerClearOps 接缝、undoOneFn 接缝、seedExecutedOp 全是改前就存在的。
//   所以"改前必红"落在行为上，不落在编译错误上。
//
// 判据 P-44：维护在途 ⇒ 两条回撤腿都被拒，且一次文件操作都没发生（undoOneFn 零调用）。
// 前提自检（M93 教训）：同一夹具空闲时两条腿都必须**真的受理**并走到 undoOneFn，
// 否则"被拒"无法归因给被测门禁。

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filededup/internal/ops"
)

// m380UndoableOp 落一条真实可撤的单条回收站账本，返回 (opLogID, itemID)。
func m380UndoableOp(t *testing.T, a *App) (int64, int64) {
	t.Helper()
	dir := t.TempDir()
	content := []byte("M380-UNDO-GATE-PAYLOAD")
	mtime := time.Now().Add(-24 * time.Hour).UnixNano()
	dest := filepath.Join(dir, "trash", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dir, "home", "a.bin")
	opID := seedExecutedOp(t, a, "trash", []string{orig}, []string{dest},
		uint64(len(content)), [][32]byte{blake3Of(t, dest)}, mtime)
	_, items, err := a.hist.GetOp(opID)
	if err != nil || len(items) != 1 {
		t.Fatalf("M380：可撤账本没铺好 n=%d err=%v", len(items), err)
	}
	return opID, items[0].ID
}

// m380CountUndoCalls 把 undoOneFn 换成「只计数、不碰文件系统」的桩。
// 返回读计数的函数——用它断言"回撤一步都没走通"，而不是靠观察真实文件。
func m380CountUndoCalls(t *testing.T) func() int {
	t.Helper()
	prev := undoOneFn
	t.Cleanup(func() { undoOneFn = prev })
	var mu sync.Mutex
	var n int
	undoOneFn = func(ops.UndoItem) (string, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return "M380-桩：未动文件", nil
	}
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// M380 正向判据：清空清理记录在途 ⇒ 两条回撤腿都被拒且零文件动作。
func TestM380UndoRejectedWhileRecordsClearing(t *testing.T) {
	t.Run("批量腿", func(t *testing.T) {
		a, _ := newHistApp(t)
		opID, _ := m380UndoableOp(t, a)
		calls := m380CountUndoCalls(t)

		pauseInLedgerClear(t, a, func() {
			_, err := a.UndoOperation(opID)
			if err == nil {
				a.wg.Wait()
				t.Fatalf("M380：清空清理记录期间批量回撤被受理 —— claimMaintenance 的「双向闭合」话术对回撤腿是假的")
			}
			if !strings.Contains(err.Error(), "清空清理记录") {
				t.Errorf("M380：批量回撤的拒绝文案没点名是哪项维护：%v", err)
			}
		})
		if n := calls(); n != 0 {
			t.Errorf("M380：维护在途期间回撤执行了 %d 次文件操作 —— 账本已被整表删除抽走，文件却动了", n)
		}
	})

	t.Run("单项腿", func(t *testing.T) {
		a, _ := newHistApp(t)
		opID, itemID := m380UndoableOp(t, a)
		calls := m380CountUndoCalls(t)

		pauseInLedgerClear(t, a, func() {
			_, err := a.UndoOperationItem(opID, itemID)
			if err == nil {
				a.wg.Wait()
				t.Fatalf("M380：清空清理记录期间单项回撤被受理 —— 补了批量腿漏了增量腿就是半个闸")
			}
			if !strings.Contains(err.Error(), "清空清理记录") {
				t.Errorf("M380：单项回撤的拒绝文案没点名是哪项维护：%v", err)
			}
		})
		if n := calls(); n != 0 {
			t.Errorf("M380：维护在途期间单项回撤执行了 %d 次文件操作", n)
		}
	})
}

// M380 前提自检 / 负控制：空闲时两条腿照常受理，并确实走到执行入口。
// 这一格改前即绿——它的作用是证明上面那两格的"被拒"归因于维护闩，而不是夹具卡在别处。
func TestM380UndoAcceptedWhenIdle(t *testing.T) {
	t.Run("批量腿", func(t *testing.T) {
		a, _ := newHistApp(t)
		opID, _ := m380UndoableOp(t, a)
		calls := m380CountUndoCalls(t)
		if _, err := a.UndoOperation(opID); err != nil {
			t.Fatalf("前提自检失败：空闲 App 的批量回撤应被受理，实际被拒：%v", err)
		}
		a.wg.Wait()
		if n := calls(); n == 0 {
			t.Fatalf("前提自检失败：批量回撤被受理却没走到执行入口 —— 本文件的「零文件动作」断言无法归因")
		}
	})

	t.Run("单项腿", func(t *testing.T) {
		a, _ := newHistApp(t)
		opID, itemID := m380UndoableOp(t, a)
		calls := m380CountUndoCalls(t)
		if _, err := a.UndoOperationItem(opID, itemID); err != nil {
			t.Fatalf("前提自检失败：空闲 App 的单项回撤应被受理，实际被拒：%v", err)
		}
		a.wg.Wait()
		if n := calls(); n == 0 {
			t.Fatalf("前提自检失败：单项回撤被受理却没走到执行入口")
		}
	})
}

// ★ 第二把尺子（静态锚，形状同 app_f1_test.go 的源码钉）：M380 的承重判据是
// "问与占位在**同一个** a.mu 临界区里"，而这一句**行为探针钉不住** ——
// 变异 MU9-b（把第三问挪到 a.mu.Lock() 之前，做成 M364 那种"先问再占"的两段式）
// 在 pauseInLedgerClear 这个暂停点上照样会被拒，P-44 实测**存活**（读数见 04 §6.70）。
// 竞态窗口要有缝可插才观测得到，而这里没有缝；于是只能直接读源码形状：
// 从 a.mu.Lock() 到 a.opsRunning = true 之间，每一次 a.mu.Unlock() 都必须紧跟 return
// （= 中间没有"解锁了还继续往下走"的第二次进入）。
// 措辞边界：这是**静态锚**，不是行为级红；它钉形状，P-44 钉行为，两把锁互相兜。
func TestM380MaintainingCheckSharesCriticalSection(t *testing.T) {
	src := readSource(t, "app_ops.go")
	for _, sig := range []string{
		"func (a *App) UndoOperation(opLogID int64) (string, error) {",
		"func (a *App) UndoOperationItem(opLogID, itemID int64) (string, error) {",
	} {
		start := strings.Index(src, sig)
		if start < 0 {
			t.Fatalf("M380 静态锚读不到 %s —— 入口签名变了，本锚失效不得读作通过", sig)
		}
		rest := src[start:]
		lockIdx := strings.Index(rest, "a.mu.Lock()")
		claimIdx := strings.Index(rest, "a.opsRunning = true")
		mainIdx := strings.Index(rest, `a.maintaining != ""`)
		if lockIdx < 0 || claimIdx < 0 || mainIdx < 0 {
			t.Fatalf("M380：%s 里找不齐 Lock/占位/maintaining 三处形状，锚失效", sig)
		}
		if !(lockIdx < mainIdx && mainIdx < claimIdx) {
			t.Errorf("M380：%s 的第三问不在「加锁 → 占位」之间（Lock=%d maintaining=%d 占位=%d）—— 退回先问再占的两段式",
				sig, lockIdx, mainIdx, claimIdx)
		}
		for _, unlock := range allIndexes(rest[lockIdx:claimIdx], "a.mu.Unlock()") {
			tail := strings.TrimLeft(rest[lockIdx+unlock+len("a.mu.Unlock()"):], " \t\n")
			if !strings.HasPrefix(tail, "return") {
				t.Errorf("M380：%s 在临界区里出现「解锁后还往下走」的一处（偏移 %d）—— 判与占不在同一段", sig, unlock)
			}
		}
	}
}

func allIndexes(s, needle string) []int {
	var out []int
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return out
		}
		out = append(out, i+j)
		i += j + len(needle)
	}
}
