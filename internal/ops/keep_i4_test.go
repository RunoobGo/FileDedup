package ops

// 2026-09-18 审查 I4 回归：directory 保留策略的两个静默失效面。
//   - 组内无一命中保留目录 → 修正前静默跳过，整组不受 S2 保护却无人知晓；
//   - 保留目录写法与实际路径大小写/尾分隔符不同 → 修正前整组匹配不上。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

func dirGroup(id uint64, paths ...string) *model.DuplicateGroup {
	g := &model.DuplicateGroup{GroupID: id, Files: make([]*model.FileEntry, 0, len(paths))}
	for i, p := range paths {
		g.Files = append(g.Files, &model.FileEntry{ID: id*100 + uint64(i), Path: p, Size: 10})
	}
	return g
}

// TestApplyKeepPolicyReportsUnmatchedGroups 未命中组必须回传，而不是静默消失。
func TestApplyKeepPolicyReportsUnmatchedGroups(t *testing.T) {
	g1 := dirGroup(1, "/keep/a.bin", "/else/a.bin")
	g2 := dirGroup(2, "/nowhere/a.bin", "/elsewhere/a.bin")
	g3 := dirGroup(3, "/keep/deep/a.bin", "/else/a.bin")
	ds, unmatched := ApplyKeepPolicy(
		[]*model.DuplicateGroup{g1, g2, g3},
		model.KeepPolicy{Kind: "directory", Directories: []string{"/keep"}})
	if len(ds) != 2 {
		t.Fatalf("决策数 = %d, want 2（%+v）", len(ds), ds)
	}
	if len(unmatched) != 1 || unmatched[0] != 2 {
		t.Fatalf("未命中组 = %v, want [2]", unmatched)
	}
	// 命中组仍要给出正确保留者
	if ds[0].KeepID != g1.Files[0].ID || ds[1].KeepID != g3.Files[0].ID {
		t.Fatalf("保留者错配: %+v", ds)
	}
	// 非 directory 策略不产生未命中
	ds2, un2 := ApplyKeepPolicy([]*model.DuplicateGroup{g2}, model.KeepPolicy{Kind: "shortest"})
	if len(un2) != 0 || len(ds2) != 1 {
		t.Fatalf("shortest 策略不该有未命中: ds=%+v unmatched=%v", ds2, un2)
	}
}

// TestPickInDirectoryTolerantOnInsensitiveVolume 不敏感卷上，用户写的大小写形式
// 与尾分隔符都不该让整组匹配落空（落空 = 整组不受保护 = 可被整组清理）。
func TestPickInDirectoryTolerantOnInsensitiveVolume(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "Keep", "Photo")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	onDisk := filepath.Join(real, "a.bin")
	if err := os.WriteFile(onDisk, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fscase.Sensitive(real) {
		t.Skip("该主机临时目录区分大小写，本例（大小写写法差异）不适用")
	}
	g := dirGroup(1, onDisk, filepath.Join(tmp, "Else", "a.bin"))
	other := dirGroup(2, filepath.Join(tmp, "Else", "b.bin"), filepath.Join(tmp, "Else", "c.bin"))

	cases := []struct{ name, dir string }{
		{"全小写写法", filepath.Join(tmp, "keep", "photo")},
		{"首字母大写写法", filepath.Join(tmp, "KEEP", "PHOTO")},
		{"带尾分隔符", real + string(filepath.Separator)},
		{"正写法", real},
	}
	for _, c := range cases {
		got, unmatched := ApplyKeepPolicy([]*model.DuplicateGroup{g, other},
			model.KeepPolicy{Kind: "directory", Directories: []string{c.dir}})
		if len(got) != 1 {
			t.Fatalf("%s: 决策数 = %d, want 1（%+v）", c.name, len(got), got)
		}
		if got[0].KeepID != g.Files[0].ID {
			t.Errorf("%s: 保留者 = %d, want %d（磁盘上的那份）", c.name, got[0].KeepID, g.Files[0].ID)
		}
		if len(unmatched) != 1 || unmatched[0] != 2 {
			t.Errorf("%s: 未命中组 = %v, want [2]", c.name, unmatched)
		}
	}
}
