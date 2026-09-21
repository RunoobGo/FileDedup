package history

// M12（2026-09-21 全仓审计 §五 12）两条同批：
//  a) UpdateKeepPaths 不校验 RowsAffected——历史行刚被删除/裁剪时静默返回 nil，
//     界面上"已保存"，库里那行根本不存在；
//  b) 确证损坏的账本隔离重建后只 fprintf(stderr)，GUI 用户没有终端，
//     看到的只是"历史记录页凭空变空"。新增 QuarantinedTo 出口给应用层。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 行已不存在时必须报错（同文件 FinishItem/FinalizeOp 的 n == 0 口径）。
func TestUpdateKeepPathsRejectsMissingRow(t *testing.T) {
	s := testDB(t)
	id, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"/a/keep.bin"}
	if err := s.UpdateKeepPaths(id, paths); err != nil {
		t.Fatalf("行存在时应保存成功: %v", err)
	}
	if err := s.DeleteScan(id); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateKeepPaths(id, paths); err == nil {
		t.Fatal("历史行已删除仍返回 nil：界面上是「已保存」，库里那行不存在，" +
			"下次恢复该历史时保留项回到上一次的状态而无人知晓")
	} else if !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("错误应说明行不存在，实得: %v", err)
	}
	// 反向：删掉一条不影响另一条的写入（别把判据做成"一律失败"）
	id2, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateKeepPaths(id2, paths); err != nil {
		t.Fatalf("现存行 %d 的写入不该被牵连: %v", id2, err)
	}
	m, groups, err := s.LoadScan(id2)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.KeepPaths) != 1 || m.KeepPaths[0] != paths[0] {
		t.Fatalf("keep_paths 未真正写入: %+v（groups=%d）", m.KeepPaths, len(groups))
	}
}

// 隔离重建要能被应用层问到：QuarantinedTo 在非损坏路径下为空串，
// 损坏重建后为隔离文件名（且文件确实还在——这是"可自行恢复"那句文案的依据）。
func TestQuarantinedToVisibleToCaller(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "history.db")

	// 健康库：没有隔离过，必须是空串，否则每次启动都白报一次"账本丢了"
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.QuarantinedTo(); got != "" {
		t.Fatalf("健康库的 QuarantinedTo = %q，应为空串", got)
	}
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// 覆写成非库影像，触发"确证损坏 → 隔离重建"那一条路
	junk := []byte("garbage where an undo ledger used to live")
	if err := os.WriteFile(p, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("确证损坏应隔离重建: %v", err)
	}
	defer s2.Close()
	q := s2.QuarantinedTo()
	if q == "" {
		t.Fatal("隔离重建后 QuarantinedTo 为空：应用层无从得知，界面依旧只会说「历史记录是空的」")
	}
	if _, err := os.Stat(q); err != nil {
		t.Fatalf("隔离文件应仍可读（文案据此承诺「可自行恢复」）: %v", err)
	}
	payload, err := os.ReadFile(q)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != string(junk) {
		t.Fatalf("隔离内容被改写: %q", payload)
	}
	if !strings.HasPrefix(filepath.Base(q), "history.db.broken-") {
		t.Fatalf("隔离文件名 = %q，期望 history.db.broken-* 形态", q)
	}
	// 重建后的库必须真的可用（否则文案里的"重建"是假的）
	if _, err := s2.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatalf("重建后的库不可写: %v", err)
	}
	ms, err := s2.ListScans()
	if err != nil || len(ms) != 1 {
		t.Fatalf("重建后应有 1 条新历史: %+v err=%v", ms, err)
	}
}
