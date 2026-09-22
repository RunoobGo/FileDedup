package ops

// M89（04 §6.11 OPS-14d，设计段 §24.3.1）：跨卷 move **部分成功**时执行器把落点丢掉。
//
// 部分成功 = move.go 那两条"数据已经在盘上了、只是这一步没成"的返回：
// 复制完成但删源失败（本用例走的那条），以及复制期间源被第三方顶替（AS-H4 那条）。
// 改前 executor.go 的 move 分支写的是 `settle(i, outcome{code: ocFailed, err: err.Error()})`
// ——dst 出现在 MoveFile 的返回值里却没进 outcome，于是
//   - 发射的失败 ItemResult 的 DestPath 为空 ⇒ 写前日志里这份多出来的副本**没有任何条目**；
//   - 历史页与"核对后自行删去其一"的指引对不上：用户被告知有两份，账上只有一份。
//
// §24.2 的 R3-MU3 实测：把 move.go:85 的 `return dst.path, ...` 改回 `return "", ...`
// 全仓没有一个用例变红 ⇒ 这条通道从来没有被测到。本文件补两面：
// 执行器侧（本条）与 MoveFile 侧（move_crossvolume_identity_test.go 的 M138 断言）。
//
// 负控制方向相反：move.go 的错误文本**本来就自带落点**，所以 DestHint 在这里必须是
// 幂等 no-op ⇒ 末条断言钉住"不许出现第二次（数据已在 …）"，防止两处各补一次。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

func TestMovePartialSuccessFailedItemCarriesDest(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	forceCrossVolumeRename(t)
	origRemove := removeSrc
	removeCalls := 0
	removeSrc = func(p string) error {
		removeCalls++
		return fmt.Errorf("删除源失败（模拟权限/占用）: %s", p)
	}
	t.Cleanup(func() { removeSrc = origRemove })

	var got []ItemResult
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
		OnItem: func(r ItemResult) { got = append(got, r) },
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	// ---- 前提自检：确实走到了"已复制、删源失败"这一格 ----
	if removeCalls != 1 {
		t.Fatalf("前置：删源应恰好尝试 1 次，实得 %d ⇒ 没走进跨卷腿，本条测的不是 M89 那一格", removeCalls)
	}
	if len(res.OK) != 0 || len(res.Failed) != 1 {
		t.Fatalf("部分成功应记 1 项失败、0 项成功，实得 ok=%v failed=%v", res.OK, res.Failed)
	}
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatalf("前置：删源失败时源必须还在（两份并存的前提），实得 %v", err)
	}

	// ---- 判据格：失败项必须把落点交回账本 ----
	var failed *ItemResult
	for i, r := range got {
		if r.OrigPath == fx.dup1.Path {
			failed = &got[i]
		}
	}
	if failed == nil {
		t.Fatalf("执行器没有为 %s 发射终态回调，实得 %+v", fx.dup1.Path, got)
	}
	if failed.State != "failed" {
		t.Fatalf("部分成功的终态应是 failed（它没成功），实得 %q", failed.State)
	}
	if failed.DestPath == "" {
		t.Fatalf("失败项没带回落点 ⇒ 盘上多出来的这份副本在账本里不存在，用户无从核对（M89）")
	}
	copyBytes, err := os.ReadFile(failed.DestPath)
	if err != nil {
		t.Fatalf("落点上的副本读不出: %v", err)
	}
	srcBytes, err := os.ReadFile(fx.dup1.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(copyBytes) != string(srcBytes) {
		t.Errorf("落点上的副本与源内容不符（副本 %d 字节 / 源 %d 字节）", len(copyBytes), len(srcBytes))
	}

	// ---- 幂等格：move.go 的文本已经写着落点，executor 不得再补一次 ----
	if n := strings.Count(failed.Err, failed.DestPath); n != 1 {
		t.Errorf("错误文本里的落点应恰好出现 1 次，实得 %d 次: %s", n, failed.Err)
	}
	if !strings.Contains(failed.Err, "两份并存") {
		t.Errorf("错误文本应保留 move.go 原本的处置指引，实得: %s", failed.Err)
	}

	// ---- 串栏防护：失败项一分都不进任何字节栏 ----
	if res.Reclaimed != 0 || res.TrashedBytes != 0 || res.LinkedBytes != 0 {
		t.Errorf("失败的部分成功不得计入任何栏位：reclaimed=%d trashed=%d linked=%d",
			res.Reclaimed, res.TrashedBytes, res.LinkedBytes)
	}
}

// TestDestHintThreeCells 钉住 DestHint 的三格行为（它是"补落点"规则的唯一实现，
// executor 与 app.go/undoFailure 共用；I5）。
func TestDestHintThreeCells(t *testing.T) {
	// 格 1：有落点、文本里还没有 ⇒ 补一次
	got := DestHint("删源失败", "/a/b/c.bin")
	if got != "删源失败（数据已在 /a/b/c.bin）" {
		t.Errorf("格1 缺落点时应补上，实得 %q", got)
	}
	// 格 2：文本里已经写着同一个路径 ⇒ 原样交回（幂等，防两处各补一次）
	if again := DestHint(got, "/a/b/c.bin"); again != got {
		t.Errorf("格2 已有落点时必须幂等，实得 %q", again)
	}
	// 格 3：没有落点 ⇒ 什么都不动（不产出"（数据已在 ）"这种空落点）
	if empty := DestHint("纯粹的失败", ""); empty != "纯粹的失败" {
		t.Errorf("格3 空落点时不得追加任何文本，实得 %q", empty)
	}
}
