package ops

// R-操作-4（2026-09-24 审查五轮 F 批，J-4=(b) 随批修）：Execute 的 FileIDs 校验循环
// 不按 fid 去重。重复 id 会让同一文件在 toProcess 里出现两次；执行阶段第二次 rename
// 命中 ENOENT，把一条**已经成功**的腿在结果账里覆写成 failed（用户看到"清理失败"，
// 实际早已清理），并可能重复计入 TrashedBytes/Reclaimed。
//
// 修法与 planOpItems（app.go:2576）/ 选择集去重（app.go:1664）同源：入口按 fid 去重。
// 去重后进度分母仍是 len(op.FileIDs)，故跳过重复项时要 report("") 推一格，
// 与既有"文件不在结果集"分支同款，保证 n 走到 total。

import (
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

func TestExecuteDedupsDuplicateFileIDs(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup1.ID}})

	if len(res.Failed) != 0 {
		t.Fatalf("重复 id 不得制造失败项（同一文件只应处理一次）：failed=%v", res.Failed)
	}
	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("重复 id 应折叠成一次成功：ok=%v", res.OK)
	}
	if res.TrashedBytes != fx.dup1.Size {
		t.Fatalf("重复 id 不得重复计账：TrashedBytes=%d want %d", res.TrashedBytes, fx.dup1.Size)
	}
}

// 进度分母不回归：重复项被跳过时仍要推进进度，n 必须走到 total。
func TestExecuteDuplicateFileIDsProgressReachesTotal(t *testing.T) {
	fx := newFixture(t)
	var lastN, lastTotal int
	Execute(Options{
		Groups:     []*model.DuplicateGroup{fx.group},
		TrashFn:    mockTrash(filepath.Join(t.TempDir(), "t"), false),
		OnProgress: func(n, total int, _ string) { lastN, lastTotal = n, total },
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup1.ID, fx.dup2.ID}})

	if lastTotal != 3 {
		t.Fatalf("进度分母应为 len(FileIDs)=3，实得 %d", lastTotal)
	}
	if lastN != lastTotal {
		t.Fatalf("进度未走满（重复项被跳过却没推进）：n=%d total=%d", lastN, lastTotal)
	}
}
