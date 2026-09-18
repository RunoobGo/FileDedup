package main

// 2026-09-18 审查 I4 回归（API 层）：directory 策略的未命中组数必须随结果返回，
// 前端才有东西可提示。修正前这类组在 ops 层被静默 continue，调用方无从知晓。

import (
	"testing"

	"filededup/internal/model"
)

func TestApplyKeepPolicySurfacesUnmatchedGroups(t *testing.T) {
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{
		mkGroup(1, 100, "/keep/a.bin", "/else/a.bin"),
		mkGroup(2, 100, "/nowhere/a.bin", "/else/b.bin"),
		mkGroup(3, 100, "/keep/deep/a.bin", "/else/c.bin"),
	}
	oc, err := a.ApplyKeepPolicy(model.KeepPolicy{
		Kind: "directory", Directories: []string{"/keep"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(oc.Decisions) != 2 {
		t.Fatalf("决策数 = %d, want 2（%+v）", len(oc.Decisions), oc.Decisions)
	}
	if oc.UnmatchedGroups != 1 {
		t.Fatalf("未命中组数 = %d, want 1", oc.UnmatchedGroups)
	}
	// 组 2 未被标任何保留者（不受 S2 保护）
	for _, f := range a.groups[1].Files {
		a.mu.Lock()
		protected := a.keepIDs[f.ID]
		a.mu.Unlock()
		if protected {
			t.Fatalf("未命中组的成员被误标为保留者: %s", f.Path)
		}
	}
	// 非 directory 策略不产生未命中
	oc2, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "newest"})
	if err != nil || oc2.UnmatchedGroups != 0 || len(oc2.Decisions) != 3 {
		t.Fatalf("newest 策略结果失真: %+v err=%v", oc2, err)
	}
}
