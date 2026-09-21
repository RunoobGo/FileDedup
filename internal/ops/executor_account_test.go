package ops

// M22（2026-09-21，04 §6.8.8）：**回收站被算进"已释放"**——09-19 硬链接那笔假账的同构复发。
//
// 判据：四个字节口径互斥，同一个成功项只落进其中一栏。
//   hardlink → LinkedBytes / symlink → SymlinkedBytes / **trash → TrashedBytes** /
//   delete 与跨卷 move → Reclaimed（唯二"真的从磁盘消失"的两格）
//
// 为什么 trash 不属于 Reclaimed：同卷进回收站只是一次改名（数据落到
// `$Recycle.Bin` / `~/.Trash` / XDG 目录），跨卷也只是把数据搬到另一个卷的回收站，
// **磁盘总量都一分未减**——要等用户清空回收站才释放。修正前 trash 落进 default，
// 结果条因此写"成功 N（释放 X）"，而紧挨着的按钮是"打开回收站"。

import (
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// V1：trash 的贡献必须落在 TrashedBytes，Reclaimed 必须为 0。
func TestTrashKindReportsTrashedNotReclaimed(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.OK) != 2 || len(res.Failed) != 0 {
		t.Fatalf("前置：本用例要的是两个成功项，实得 ok=%v failed=%v", res.OK, res.Failed)
	}
	if res.Reclaimed != 0 {
		t.Fatalf("回收站里的文件没有离开磁盘（同卷只是改名、跨卷只是搬到另一个卷的回收站），"+
			"不得计入 Reclaimed：Reclaimed=%d", res.Reclaimed)
	}
	want := fx.dup1.Size + fx.dup2.Size
	if res.TrashedBytes != want {
		t.Fatalf("移入回收站的字节数 = %d, want %d", res.TrashedBytes, want)
	}
	// 串栏防护：另两个链接口径也不该被 trash 碰到
	if res.LinkedBytes != 0 || res.SymlinkedBytes != 0 {
		t.Fatalf("trash 不该写进链接类栏位：linked=%d symlinked=%d",
			res.LinkedBytes, res.SymlinkedBytes)
	}
}

// V2：delete 照旧进 Reclaimed（负控制——证明 V1 不是"把整张表改坏"）。
func TestDeleteKindStillReclaims(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}, ConfirmDanger: true})

	if len(res.OK) != 2 {
		t.Fatalf("前置：delete 应成功 2 项，实得 %v", res.OK)
	}
	want := fx.dup1.Size + fx.dup2.Size
	if res.Reclaimed != want {
		t.Fatalf("永久删除确实释放空间，Reclaimed = %d, want %d", res.Reclaimed, want)
	}
	if res.TrashedBytes != 0 {
		t.Fatalf("delete 不进回收站：TrashedBytes=%d", res.TrashedBytes)
	}
}

// V3：跨卷 move 照旧进 Reclaimed（登记明确要求"反向钉住"）。
//
// 为什么只钉跨卷这一半：同卷 move 也只是改名，却仍落在 Reclaimed——那次假账要等
// MoveFile 能报出 EXDEV 才谈得上分辨（登记 M40），本项不动它，也不把已知的假账
// 钉成契约。跨卷这一半是**合乎事实**的（数据离开源卷），必须有断言守着，
// 免得将来"修 M40"时把它一起改坏。
func TestCrossVolumeMoveStillReclaims(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	forceCrossVolumeRename(t)

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	if len(res.OK) != 1 || len(res.Failed) != 0 {
		t.Fatalf("前置：跨卷 move 应成功 1 项，实得 ok=%v failed=%v", res.OK, res.Failed)
	}
	if res.Reclaimed != fx.dup1.Size {
		t.Fatalf("跨卷移出源卷确实腾出了空间：Reclaimed=%d, want %d", res.Reclaimed, fx.dup1.Size)
	}
	if res.TrashedBytes != 0 {
		t.Fatalf("move 不进回收站：TrashedBytes=%d", res.TrashedBytes)
	}
}
