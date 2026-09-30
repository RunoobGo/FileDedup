package main

// 第八轮批 3（拟 M367）P-25 / P-26：账本类 RPC 的**失败腿**必须走中文壳。
//
// 取证在册（§3.1a）：`app_history.go` 六条腿与 `app_ops.go` 两条回撤腿直接把底层错误
// 原样返回，而同包的 `app_records_io.go`、`app_reveal.go`、`app_settings.go` 早就在用
// `shellRPCError`。M214 立这条壳的理由是"界面不许把英文原话当结论"，
// 于是 `database is locked` 这类句子在 GUI 用户面前就是一句没法读的乱码级线索。
//
// ★ 失败源用的是**句柄已关**的账本，不注入、不靠时序：底层必然报
//   `sql: database is closed`，八条腿共用同一个失败前提。

import (
	"strings"
	"testing"

	"filededup/internal/model"
)

// closedLedgerApp 交出一只账本句柄已经关掉的 App（其余夹具与 newHistApp 同源）。
func closedLedgerApp(t *testing.T) *App {
	t.Helper()
	a, _ := newHistApp(t)
	if err := a.hist.Close(); err != nil {
		t.Fatalf("夹具关句柄失败: %v", err)
	}
	return a
}

// 八条腿逐条点名：错误必须带中文外壳，而系统原话必须**仍在**（壳不是替换，是加壳）。
func TestM367LedgerLegsCarryChineseShell(t *testing.T) {
	a := closedLedgerApp(t)
	cases := []struct {
		name string
		call func() error
	}{
		{"ListScanHistory", func() error { _, err := a.ListScanHistory(); return err }},
		{"LoadScanHistory", func() error { _, err := a.LoadScanHistory(1); return err }},
		{"DeleteScanHistory", func() error { return a.DeleteScanHistory(1) }},
		{"ClearScanHistory", func() error { return a.ClearScanHistory() }},
		{"ListOpRecords", func() error { _, err := a.ListOpRecords(); return err }},
		{"GetOpRecord", func() error { _, err := a.GetOpRecord(1); return err }},
		{"UndoOperation", func() error { _, err := a.UndoOperation(1); return err }},
		{"UndoOperationItem", func() error { _, err := a.UndoOperationItem(1, 1); return err }},
	}
	raw := "sql: database is closed"
	for _, tc := range cases {
		err := tc.call()
		if err == nil {
			t.Errorf("M367：%s 在账本句柄已关时居然返回了成功", tc.name)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, raw) {
			t.Errorf("M367：%s 把系统原话丢了（排查时无从复现）：%q", tc.name, msg)
		}
		if !strings.Contains(msg, "（系统原文：") {
			t.Errorf("M367：%s 没走中文壳，用户看到的仍是英文原话：%q", tc.name, msg)
		}
		shell := strings.SplitN(msg, "（系统原文：", 2)[0]
		if !hasCJKText(shell) {
			t.Errorf("M367：%s 的壳不是中文句：%q", tc.name, msg)
		}
		// 回撤两条腿必须把在途标志交还，否则一次失败就把应用锁死。
		a.mu.Lock()
		running := a.opsRunning
		a.mu.Unlock()
		if running {
			t.Errorf("M367：%s 失败后 opsRunning 仍占着", tc.name)
		}
	}
}

// 负侧钉子（P-26）：应用自撰的中文拒绝句必须**逐字原样**到界面，壳不许套壳。
//
// `shellRPCError` 自己的判据是 shell==raw ⇒ 原样返回（保留 errors.Is 身份），
// 所以这一格钉的是"将来有人图省事把这两句也包一层"时能当场红。
func TestM367ChineseRejectionsStayVerbatim(t *testing.T) {
	a, _ := newHistApp(t)
	if err := a.claimMaintenance("清空清理记录", "清空记录"); err != nil {
		t.Fatalf("夹具占不住维护标记: %v", err)
	}
	err := a.ClearOpRecords()
	a.releaseMaintenance()
	if err == nil {
		t.Fatal("M367：维护在途时清空记录竟然受理了")
	}
	if strings.Contains(err.Error(), "（系统原文：") {
		t.Errorf("M367：给自撰中文拒绝句套了壳，等于中文（系统原文：中文）：%q", err.Error())
	}

	// 账本不可用时，可回撤类必须拒，且那句自撰中文原样端出。
	// mkGroup(1,…) 给出的两个 id 是 100 与 101：留 100、动 101。
	groups := []*model.DuplicateGroup{mkGroup(1, 10, "/journal/a.bin", "/journal/b.bin")}
	if _, err := a.beginJournal(nil, 0, model.OpRequest{
		Kind: "move", TargetDir: t.TempDir(), FileIDs: []uint64{101},
	}, groups, map[uint64]bool{100: true}); err == nil {
		t.Fatal("M367：账本不可用时可回撤操作被放行了（C3 裁定倒退回去了）")
	} else if strings.Contains(err.Error(), "（系统原文：") {
		t.Errorf("M367：账本不可用的拒绝被套壳：%q", err.Error())
	} else if !strings.Contains(err.Error(), "清理账本不可用") {
		t.Errorf("M367：拒绝话术变了：%q", err.Error())
	}
}
