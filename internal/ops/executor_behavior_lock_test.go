package ops

// M337（2026-09-28 第七轮审查批）：`Execute` 分支提取前的**行为锁**。
//
// 背景：`Execute` 是约 754 行的单体函数（executor.go:193-946），五种操作
// （trash/delete/move/hardlink/symlink）的完整分支都在函数体内。把它按 kind
// 提取成五个函数是**结构改动**，风险不在逻辑写错，而在"搬的时候漏带了一个
// 变量、或传参顺序错位"——那种错误编译得过、测试也未必见得着，却会让某一腿
// 的终态悄悄变掉。
//
// 所以**先锁行为、再动结构**：本文件的断言值取自提取前的实测（不是设计意图）。
// 提取后本文件必须**逐字仍绿**；红了就说明提取改变了行为，那是提取的错，
// 不是本文件的错——不得为了让测试变绿而回改这里的断言。
//
// 覆盖取舍：symlink 那一腿的复杂场景（跨卷、悬空、权限）已由
// executor_symlink_test.go 与 symlink_test.go 覆盖，本文件不为它重复造夹具；
// 提取时那一腿同样要过那些既有用例。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// TestExecBehaviorLockSuccessLegs 五种 kind 各跑一次成功腿，钉住
// 「OK 条数 + 四态互斥 + 字节口径」这组形状。
func TestExecBehaviorLockSuccessLegs(t *testing.T) {
	t.Run("trash", func(t *testing.T) {
		fx := newFixture(t)
		res := Execute(Options{
			Groups:  []*model.DuplicateGroup{fx.group},
			TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
		}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

		assertCounts(t, res, 2, 0, 0, 0)
		if res.TrashedBytes != fx.dup1.Size+fx.dup2.Size {
			t.Fatalf("TrashedBytes = %d, want %d", res.TrashedBytes, fx.dup1.Size+fx.dup2.Size)
		}
		if res.Reclaimed != 0 || res.LinkedBytes != 0 || res.SymlinkedBytes != 0 {
			t.Fatalf("trash 只准进 TrashedBytes：reclaimed=%d linked=%d symlinked=%d",
				res.Reclaimed, res.LinkedBytes, res.SymlinkedBytes)
		}
	})

	t.Run("delete", func(t *testing.T) {
		fx := newFixture(t)
		res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
			model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}, ConfirmDanger: true})

		assertCounts(t, res, 2, 0, 0, 0)
		if res.Reclaimed != fx.dup1.Size+fx.dup2.Size {
			t.Fatalf("Reclaimed = %d, want %d", res.Reclaimed, fx.dup1.Size+fx.dup2.Size)
		}
		if res.TrashedBytes != 0 {
			t.Fatalf("delete 不进回收站：TrashedBytes=%d", res.TrashedBytes)
		}
	})

	t.Run("move_cross_volume", func(t *testing.T) {
		fx := newFixture(t)
		forceCrossVolumeRename(t)
		res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
			model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID},
				TargetDir: filepath.Join(t.TempDir(), "moved")})

		assertCounts(t, res, 1, 0, 0, 0)
		if res.Reclaimed != fx.dup1.Size {
			t.Fatalf("跨卷 move 的 Reclaimed = %d, want %d", res.Reclaimed, fx.dup1.Size)
		}
	})

	t.Run("hardlink", func(t *testing.T) {
		fx := newFixture(t)
		res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
			model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

		assertCounts(t, res, 2, 0, 0, 0)
		if res.LinkedBytes != fx.dup1.Size+fx.dup2.Size {
			t.Fatalf("LinkedBytes = %d, want %d", res.LinkedBytes, fx.dup1.Size+fx.dup2.Size)
		}
		if res.Reclaimed != 0 || res.TrashedBytes != 0 || res.SymlinkedBytes != 0 {
			t.Fatalf("hardlink 只准进 LinkedBytes：reclaimed=%d trashed=%d symlinked=%d",
				res.Reclaimed, res.TrashedBytes, res.SymlinkedBytes)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		fx := newFixture(t)
		// 同卷软链接只有净损失（ops/symlink.go 头注释）：这里的锁只钉
		// 「不崩、且给出可归因的结果」这一层，成功语义由 symlink 专项用例守。
		res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
			model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup1.ID}})
		if len(res.OK)+len(res.Failed)+len(res.Skipped)+len(res.Cancelled) != 1 {
			t.Fatalf("symlink 一腿的四态合计 = %d, want 1（ok=%v failed=%v skipped=%v cancelled=%v）",
				len(res.OK)+len(res.Failed)+len(res.Skipped)+len(res.Cancelled),
				res.OK, res.Failed, res.Skipped, res.Cancelled)
		}
	})
}

// TestExecBehaviorLockDeleteNeedsConfirm delete 缺 ConfirmDanger 必须在
// **动任何文件之前**被拒（S4：后端是最后防线）。
func TestExecBehaviorLockDeleteNeedsConfirm(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}}) // 无 ConfirmDanger

	assertCounts(t, res, 0, 1, 0, 0)
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatalf("被拒的 delete 不许动磁盘上的文件：%v", err)
	}
}

// TestExecBehaviorLockEmptyRequest 空请求与"文件不在结果集"两格的形状。
func TestExecBehaviorLockEmptyRequest(t *testing.T) {
	fx := newFixture(t)

	if res := (Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "trash", FileIDs: nil})); len(res.OK) != 0 ||
		len(res.Failed) != 0 || len(res.Skipped) != 0 || len(res.Cancelled) != 0 {
		t.Fatalf("空 FileIDs 应四态全空，实得 ok=%v failed=%v skipped=%v cancelled=%v",
			res.OK, res.Failed, res.Skipped, res.Cancelled)
	}

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "trash", FileIDs: []uint64{99999}})
	assertCounts(t, res, 0, 1, 0, 0)
}

// TestExecBehaviorLockCancelledContext ctx 已取消时不得有任何一项走到成功。
func TestExecBehaviorLockCancelledContext(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := Execute(Options{
		Ctx:     ctx,
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.OK) != 0 {
		t.Fatalf("ctx 已取消却仍有成功项：ok=%v", res.OK)
	}
	// 钉"每一项都有归属"，不是钉具体归到哪一态（取消与跳过的边界随分支而异，
	// 把它钉死反而会把一次合规的重构打成红）。
	if len(res.Cancelled)+len(res.Skipped)+len(res.Failed) != 2 {
		t.Fatalf("取消态下两项须各有归属，实得 cancelled=%v skipped=%v failed=%v",
			res.Cancelled, res.Skipped, res.Failed)
	}
}

// assertCounts 四态计数断言：ok / failed / skipped / cancelled。
func assertCounts(t *testing.T, res model.OpsResult, ok, failed, skipped, cancelled int) {
	t.Helper()
	if len(res.OK) != ok || len(res.Failed) != failed ||
		len(res.Skipped) != skipped || len(res.Cancelled) != cancelled {
		t.Fatalf("四态 = ok:%d failed:%d skipped:%d cancelled:%d，want %d/%d/%d/%d（failed=%v）",
			len(res.OK), len(res.Failed), len(res.Skipped), len(res.Cancelled),
			ok, failed, skipped, cancelled, res.Failed)
	}
}
