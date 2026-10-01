package main

// 第九轮修订批 A2 后端腿（拟 M381）：删除/清空扫描历史必须接上互斥闸。
//
// 报告 P1-2 的现读事实：「确认清空」在清理记录那条腿上是四层防线（后端 claimMaintenance
// + store guard() + 按钮 :disabled + 复位时机），而在扫描历史这两条腿上层一都没有 ——
// DeleteScanHistory / ClearScanHistory 体内零个门禁，扫描写回、清理落账、回撤、
// 乃至另一项维护都能与它的整表删除交错。
//
// ★ 本文件只用改前就存在的东西（setScanInFlight / setOpsRunning / pauseInLedgerClear /
//   hist.SaveScan），判据落在行为上而不是编译错误上。
//
// P-45：闸在途 ⇒ 两条历史腿被拒，并且**库里一行都没少**（拒绝必须发生在 SQL 之前）。
// P-46（负控制，改前即绿）：空闲时两条腿照常生效，联动按既有语义断开。

import (
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// m381SeedScans 铺 n 条历史，返回最新一条的 id。
func m381SeedScans(t *testing.T, a *App, n int) int64 {
	t.Helper()
	var last int64
	for i := 0; i < n; i++ {
		g := mkHistGroup(uint64(i+1), 1000,
			filepath.Join("/m381", string(rune('a'+i)), "x.bin"),
			filepath.Join("/m381", string(rune('a'+i)), "y.bin"))
		id, err := a.hist.SaveScan(model.ScanConfig{Roots: []string{"/m381"}},
			[]*model.DuplicateGroup{g}, nil)
		if err != nil {
			t.Fatal(err)
		}
		last = id
	}
	return last
}

// m381CountScans 读当前历史行数（用来证明"被拒 = SQL 根本没跑"）。
func m381CountScans(t *testing.T, a *App) int {
	t.Helper()
	ms, err := a.ListScanHistory()
	if err != nil {
		t.Fatal(err)
	}
	return len(ms)
}

// assertRejectedAndUntouched 三条被拒判据共用：话术点名 + 行数不减。
func assertRejectedAndUntouched(t *testing.T, a *App, want string, err error, before int) {
	t.Helper()
	if err == nil {
		t.Fatalf("M381：请求被受理 —— 整表删除与在途任务交错，界面上只会显示「清空成功」")
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("M381：拒绝话术没点名在等什么（期望含「%s」）：%v", want, err)
	}
	if after := m381CountScans(t, a); after != before {
		t.Errorf("M381：被拒的一腿仍然动了库 %d→%d 行 —— 闸必须落在 SQL 之前", before, after)
	}
}

func TestM381HistoryOpsRejectedWhenBusy(t *testing.T) {
	t.Run("扫描写回在途-清空全部历史", func(t *testing.T) {
		a, _ := newHistApp(t)
		m381SeedScans(t, a, 3)
		before := m381CountScans(t, a)
		setScanInFlight(a, true)
		t.Cleanup(func() { setScanInFlight(a, false) })
		assertRejectedAndUntouched(t, a, "扫描进行中", a.ClearScanHistory(), before)
	})

	t.Run("清理落账在途-删除单条", func(t *testing.T) {
		a, _ := newHistApp(t)
		id := m381SeedScans(t, a, 2)
		before := m381CountScans(t, a)
		setOpsRunning(a, true)
		t.Cleanup(func() { setOpsRunning(a, false) })
		assertRejectedAndUntouched(t, a, "清理/回撤操作执行中", a.DeleteScanHistory(id), before)
	})

	t.Run("另一项维护在途-清空全部历史", func(t *testing.T) {
		a, _ := newHistApp(t)
		m381SeedScans(t, a, 2)
		before := m381CountScans(t, a)
		// 复用 M364 的暂停缝：清空清理记录的整表删除正在里面执行。
		pauseInLedgerClear(t, a, func() {
			assertRejectedAndUntouched(t, a, "清空清理记录", a.ClearScanHistory(), before)
		})
	})
}

// P-46 负控制：空闲时两条腿照常生效（防"加了闸就判死"）。
func TestM381HistoryOpsAcceptedWhenIdle(t *testing.T) {
	t.Run("清空全部历史", func(t *testing.T) {
		a, _ := newHistApp(t)
		id := m381SeedScans(t, a, 3)
		if _, err := a.LoadScanHistory(id); err != nil {
			t.Fatal(err)
		}
		if err := a.ClearScanHistory(); err != nil {
			t.Fatalf("M381 负控制：空闲时清空全部历史被拒：%v", err)
		}
		if n := m381CountScans(t, a); n != 0 {
			t.Errorf("M381 负控制：清空后仍剩 %d 行", n)
		}
		a.mu.Lock()
		cur := a.curHistID
		a.mu.Unlock()
		if cur != 0 {
			t.Errorf("M381 负控制：清空后 curHistID 没断开（仍为 %d）—— 后续裁剪会回写已删行", cur)
		}
	})

	t.Run("删除单条", func(t *testing.T) {
		a, _ := newHistApp(t)
		id := m381SeedScans(t, a, 2)
		if _, err := a.LoadScanHistory(id); err != nil {
			t.Fatal(err)
		}
		if err := a.DeleteScanHistory(id); err != nil {
			t.Fatalf("M381 负控制：空闲时删除历史被拒：%v", err)
		}
		if n := m381CountScans(t, a); n != 1 {
			t.Errorf("M381 负控制：删除单条后剩 %d 行（期望 1）", n)
		}
		a.mu.Lock()
		cur := a.curHistID
		a.mu.Unlock()
		if cur != 0 {
			t.Errorf("M381 负控制：删除当前来源后 curHistID 没断开（仍为 %d）", cur)
		}
	})
}
