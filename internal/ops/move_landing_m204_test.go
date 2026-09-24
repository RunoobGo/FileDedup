package ops

// M204（2026-09-24 裁定「rename 前按实际落点复审 withinDir」，设计稿
// `2026-09-24-move-landing-recheck-m204`）：`Options.MoveLandingAllowed` 的机制面。
//
// 越权那一格的真改前红在根包（`app_move_landing_m204_test.go`，只用改前就有的公开面）。
// 本文件测的是**执行器这一侧的三条契约**，它们都需要新符号才能表达，
// 所以"改前必红"由变异提供（Va/Vb/Vc，见 §6.35 划账），本文件不冒领红探针（AS-K2）。
//
//   P-2 拒绝即失败：两项都不动、占位不残留；
//   P-3 传参形状：checker 收到的是**实际落点全路径**（含 `name_1.ext` 递增后的名字），
//      且每个条目恰好被问一次——拦"复审接到名义目录上"与"整批只问一次"两种做歪的形；
//   P-4 nil ⇒ 与改前完全同形（护栏，不是红探针）。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

func TestM204LandingRejectFailsItemsAndKeepsSources(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
		MoveLandingAllowed: func(string) error {
			calls++
			return errors.New("实际落点已不在授权目录内（模拟复审拒绝）")
		},
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}, TargetDir: target})

	if calls != 2 {
		t.Fatalf("落点复审应恰好问 2 次（两个条目各一次），实得 %d", calls)
	}
	if len(res.OK) != 0 || len(res.Failed) != 2 {
		t.Fatalf("复审拒绝必须整项失败：ok=%d failed=%d", len(res.OK), len(res.Failed))
	}
	for _, p := range []string{fx.dup1.Path, fx.dup2.Path} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("源文件 %s 在复审被拒时被搬走了: %v", p, err)
		}
	}
	ents, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("拒绝之后目标目录里残留 %d 项（%s）：claimDst 的零字节占位必须被 release 清掉",
			len(ents), strings.Join(names, ", "))
	}
	// 失败一分不进任何字节栏（与 M89 那格同一口径）。
	if res.Reclaimed != 0 {
		t.Errorf("整项失败不得计入 Reclaimed，实得 %d", res.Reclaimed)
	}
}

func TestM204LandingCheckerSeesActualLandingOncePerItem(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// 抢先把 dup1.bin 这个名字占掉 ⇒ claimDst 必须递增；复审要问的就是**递增之后**那个。
	occupied := filepath.Join(target, "dup1.bin")
	if err := os.WriteFile(occupied, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	res := Execute(Options{
		Groups:             []*model.DuplicateGroup{fx.group},
		MoveLandingAllowed: func(landing string) error { got = append(got, landing); return nil },
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}, TargetDir: target})

	if len(res.OK) != 2 {
		t.Fatalf("checker 恒放行时应照常成功：ok=%d failed=%d", len(res.OK), len(res.Failed))
	}
	if len(got) != 2 {
		t.Fatalf("两个条目应各问一次，实得 %d 次: %v", len(got), got)
	}
	if got[0] == occupied || filepath.Base(got[0]) == "dup1.bin" {
		t.Errorf("第 1 次问的是名义名 %q 而不是递增后的实际落点（递增形 = dup1_1.bin）", got[0])
	}
	if !strings.HasPrefix(got[0], target+string(filepath.Separator)) {
		t.Errorf("参数不是 targetDir 之下的完整落点路径: %q", got[0])
	}
	if got[1] != filepath.Join(target, "dup2.bin") {
		t.Errorf("第 2 次落点 = %q，want %q", got[1], filepath.Join(target, "dup2.bin"))
	}
	if _, err := os.Stat(occupied); err != nil {
		t.Errorf("占位用的第三方文件被吞掉了: %v", err)
	}
}

// TestM204NilLandingKeepsLegacyBehavior ★ 护栏：不注入 checker 时行为与改前一字不差。
func TestM204NilLandingKeepsLegacyBehavior(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	res := Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	if len(res.OK) != 1 || len(res.Failed) != 0 {
		t.Fatalf("nil checker 不得改变行为：ok=%d failed=%v", len(res.OK), res.Failed)
	}
	if _, err := os.Stat(filepath.Join(target, "dup1.bin")); err != nil {
		t.Errorf("文件没落到目标目录: %v", err)
	}
	if _, err := os.Stat(fx.dup1.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("同卷 move 成功后源应已不在，实得 %v", err)
	}
}
