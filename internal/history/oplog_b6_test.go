package history

// B6（R-操作-3）：FinalizeOp 必须**原样落**执行器已算好的 reclaimed，
// 不得在库内用 SUM(size) 重算。
//
// 缺陷根源：旧实现 reclaimed = SUM(size) WHERE state=done，是 kind-blind 的——
// 对 hardlink/symlink/trash/同卷 move 这些"源字节并未真正从磁盘移除"的操作，
// 也会按条目 size 累加成"回收空间"，历史页于是显示一笔**假账**。
// 执行器早已按 kind 精确算好 res.Reclaimed（仅 delete + 跨卷 move），
// FinalizeOp 的职责只是忠实地把这个数落库，与执行器同口径。

import "testing"

// trash 操作：条目 size 非零，但执行器判定 reclaimed=0（回收站并未真正腾出磁盘）。
// 旧 SUM 实现会算出 1000，新实现必须落 0。
func TestFinalizeOpPersistsCallerReclaimed(t *testing.T) {
	s := testDB(t)
	opID, err := s.BeginOp("trash", "", 0, true, opPlans()[:1]) // a.bin size=1000
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishItem(opID, "/op/a.bin", "/Trash/a.bin", "", StateDone, ""); err != nil {
		t.Fatal(err)
	}
	// 执行器对 trash 判定 reclaimed=0（口径见 executor.go：trash→TrashedBytes，不计入 Reclaimed）。
	if err := s.FinalizeOp(opID, 0); err != nil {
		t.Fatal(err)
	}
	meta, _, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Reclaimable != 0 {
		t.Fatalf("trash 的回收空间应为执行器判定的 0（不得按 SUM(size)=1000 记假账），实得 %d", meta.Reclaimable)
	}
	if meta.Done != 1 {
		t.Fatalf("done 计数仍应由库内 COUNT 得出，期望 1，实得 %d", meta.Done)
	}
}

// delete 操作：执行器判定 reclaimed = 被删字节。FinalizeOp 必须原样落这个非零值，
// 且与条目 SUM 恰好不等时也要以入参为准（证明它落的是入参而非重算）。
func TestFinalizeOpPersistsNonZeroReclaimed(t *testing.T) {
	s := testDB(t)
	opID, err := s.BeginOp("delete", "", 0, false, opPlans()) // 1000+2000+3000
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/op/a.bin", "/op/b.bin", "/op/c.bin"} {
		if err := s.FinishItem(opID, p, "", "", StateDone, ""); err != nil {
			t.Fatal(err)
		}
	}
	// 传一个与 SUM(6000) 明显不同的值，确认落库的是入参本身。
	const want = uint64(4096)
	if err := s.FinalizeOp(opID, want); err != nil {
		t.Fatal(err)
	}
	meta, _, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Reclaimable != want {
		t.Fatalf("FinalizeOp 应原样落执行器的 reclaimed=%d（不是库内 SUM(size)），实得 %d", want, meta.Reclaimable)
	}
}
