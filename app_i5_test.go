package main

// 2026-09-18 审查 I5 回归：结果页星标（默认建议）与 "shortest" 保留策略
// 必须选出同一个文件。修正前 app.go 另写了一份「只比路径长度」的规则，
// 在含隐藏目录的组上与 ops.pickShortest（先非隐藏、再最短）结论相反——
// 界面上打星的那份正是应用策略后被清掉的那份。

import (
	"testing"

	"filededup/internal/model"
	"filededup/internal/ops"
)

func TestStarSuggestionMatchesShortestPolicy(t *testing.T) {
	// 隐藏目录里的那份路径更短：纯长度规则会选中它，非隐藏优先规则不会。
	g := mkGroup(1, 100, "/h/.k/a", "/vis/aa.bin")

	v := toGroupView(g, nil) // 无保留决策 → 走默认建议
	var starred string
	for _, f := range v.Files {
		if f.IsKeep {
			starred = f.Path
		}
	}
	dec, unmatched := ops.ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{Kind: "shortest"})
	if len(unmatched) != 0 {
		t.Fatalf("shortest 策略不该有未命中: %v", unmatched)
	}
	if len(dec) != 1 {
		t.Fatalf("决策数 = %d, want 1", len(dec))
	}
	var kept string
	for _, f := range g.Files {
		if f.ID == dec[0].KeepID {
			kept = f.Path
		}
	}
	if starred != kept {
		t.Fatalf("星标与 shortest 策略不一致：星标 %q，实际保留 %q", starred, kept)
	}
	if starred != "/vis/aa.bin" {
		t.Fatalf("默认建议应保非隐藏的那份，得到 %q", starred)
	}
}

// TestStarSuggestionFollowsExplicitKeep 已有保留决策时星标以决策为准（不被建议覆盖）。
func TestStarSuggestionFollowsExplicitKeep(t *testing.T) {
	g := mkGroup(1, 100, "/h/.k/a", "/vis/aa.bin")
	v := toGroupView(g, map[uint64]bool{g.Files[0].ID: true})
	n := 0
	for _, f := range v.Files {
		if f.IsKeep {
			n++
			if f.Path != "/h/.k/a" {
				t.Fatalf("星标应跟随显式决策，得到 %q", f.Path)
			}
		}
	}
	if n != 1 {
		t.Fatalf("星标数 = %d, want 1", n)
	}
}

// TestSuggestKeepIndexEmpty 空组不得返回 0（会把不存在的成员标成保留者）。
func TestSuggestKeepIndexEmpty(t *testing.T) {
	if got := ops.SuggestKeepIndex(&model.DuplicateGroup{}); got != -1 {
		t.Fatalf("空组建议索引 = %d, want -1", got)
	}
	if got := ops.SuggestKeepIndex(&model.DuplicateGroup{Files: []*model.FileEntry{{ID: 7}}}); got != 0 {
		t.Fatalf("单成员组建议索引 = %d, want 0", got)
	}
}
