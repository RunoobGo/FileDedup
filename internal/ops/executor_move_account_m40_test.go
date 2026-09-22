package ops

// M40（04 §6.11 OPS-2 家族，设计段 §24.3.2）：**同卷 move 被算进"已释放"**。
//
// 这是同一笔假账的第三次同型复发：
//   - 2026-09-19 硬链接（数据块只是转为共享）→ 修成单列 LinkedBytes；
//   - 2026-09-21 回收站（同卷只是一次改名）→ 修成单列 TrashedBytes（M22）；
//   - 同卷 move 一直落在 `default: res.Reclaimed += e.Size`，把一次 rename 报成"释放 X"。
//     登记时（§20.6-2）写的是"要先能分辨改名与跨卷"——分辨的依据只有 MoveFile 知道
//     （EXDEV 只在 rename 那一刻出现），故本轮给它加一个只在成功时有意义的
//     crossVol（move.go:moveFileDetailed），不改公开签名、不动 undo.go 与六处测试。
//
// ★ 本用例与既有 V3（executor_account_test.go 的 TestCrossVolumeMoveStillReclaims）
// 是一对：V3 经 renameFile 缝造 EXDEV 钉住"跨卷仍进 Reclaimed"，本条钉
// "同卷一分都不进"。两条都不需要真第二个卷 ⇒ darwin/linux/windows 三条腿都能真跑，
// 不是"代码已改、验证未兑现"那一类。少了本条，把 move 整栏清零也照样全绿。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

func TestSameVolumeMoveDoesNotReclaim(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	var got []ItemResult
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
		OnItem: func(r ItemResult) { got = append(got, r) },
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	// ---- 前提自检：这次是真的搬走了（否则"没释放"会因为"没发生"而 vacuous）----
	if len(res.OK) != 1 || len(res.Failed) != 0 {
		t.Fatalf("前置：同卷 move 应成功 1 项，实得 ok=%v failed=%v", res.OK, res.Failed)
	}
	if _, err := os.Stat(fx.dup1.Path); !os.IsNotExist(err) {
		t.Fatalf("前置：源文件应已离开原位置（err=%v）⇒ 本条没测到改名那一格", err)
	}
	dst := ""
	for _, r := range got {
		if r.OrigPath == fx.dup1.Path && r.State == "done" {
			dst = r.DestPath
		}
	}
	if dst == "" {
		t.Fatal("前置：成功项没带回落点 ⇒ 无法核对数据是否真的搬走")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("前置：落点上的文件读不出: %v", err)
	}

	// ---- 判据格：同卷改名一分都没从磁盘上消失 ----
	if res.Reclaimed != 0 {
		t.Fatalf("同卷 move 是一次 rename，磁盘总量未减 ⇒ 不得计入 Reclaimed，实得 %d（M40）", res.Reclaimed)
	}
	// 串栏防护：也不许偷偷跑到别的口径里去（四栏互斥是 M22 立下的规矩）
	if res.TrashedBytes != 0 || res.LinkedBytes != 0 || res.SymlinkedBytes != 0 {
		t.Fatalf("同卷 move 不该写进任何其他字节栏位：trashed=%d linked=%d symlinked=%d",
			res.TrashedBytes, res.LinkedBytes, res.SymlinkedBytes)
	}
}
