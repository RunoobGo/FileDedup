package ops

// M3 安全测试（04 §3.6 S1~S8）+ 保留策略 / 移动 / XDG trashinfo / 硬链接 单测。

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/hasher"
	"filededup/internal/model"
	"lukechampine.com/blake3"
)

// ---------- 测试装置 ----------

type fixture struct {
	dir   string
	group *model.DuplicateGroup
	orig  *model.FileEntry // 保留
	dup1  *model.FileEntry // 冗余1
	dup2  *model.FileEntry // 冗余2
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	content := make([]byte, 4096)
	rand.Read(content)
	g := &model.DuplicateGroup{GroupID: 1, Hash: blake3.Sum256(content)}
	for i, name := range []string{"keep.bin", "dup1.bin", "dup2.bin"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		e := &model.FileEntry{
			ID: uint64(i + 1), Path: p, Size: uint64(st.Size()),
			ModTime: st.ModTime().UnixNano(), Ext: ".bin",
		}
		g.Files = append(g.Files, e)
	}
	g.Reclaimable = 2 * g.Files[0].Size
	return &fixture{
		dir: dir, group: g,
		orig: g.Files[0], dup1: g.Files[1], dup2: g.Files[2],
	}
}

// mockTrash 可控回收站：成功移入指定目录并返回 src→dst 映射 / 可注入失败。
func mockTrash(target string, fail bool) func([]string) (map[string]string, error) {
	return func(paths []string) (map[string]string, error) {
		if fail {
			return nil, fmt.Errorf("回收站写入失败（模拟磁盘满/权限）")
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		dst := map[string]string{}
		for _, p := range paths {
			d := filepath.Join(target, filepath.Base(p))
			if err := os.Rename(p, d); err != nil {
				return dst, err
			}
			dst[p] = d
		}
		return dst, nil
	}
}

// ---------- S1：篡改拦截 ----------

func TestS1TamperBlocked(t *testing.T) {
	fx := newFixture(t)
	// 篡改 dup1：保持 size，改中间字节（触发重算哈希路径）
	raw, _ := os.ReadFile(fx.dup1.Path)
	raw[len(raw)/2] ^= 0xFF
	os.WriteFile(fx.dup1.Path, raw, 0o644)
	// mtime 也会变 → 快速路径失效 → 重算 BLAKE3 → 不一致

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{
		Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID},
	})
	for _, ok := range res.OK {
		if ok == fx.dup1.Path {
			t.Fatal("被篡改文件不应进入 OK")
		}
	}
	blocked := false
	for _, f := range res.Failed {
		if f.Path == fx.dup1.Path && strings.Contains(f.Err, "修改") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("S1 篡改未拦截: %+v", res.Failed)
	}
	// dup2 未篡改：应正常执行
	if len(res.OK) != 1 || res.OK[0] != fx.dup2.Path {
		t.Fatalf("dup2 应正常处理: %+v", res)
	}
	// 篡改文件仍在磁盘
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatal("被拦截文件不应被删除")
	}
}

// ---------- S2：保留项不可操作 ----------

func TestS2KeepProtected(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.orig.ID}})
	if len(res.OK) != 0 {
		t.Fatal("保留文件被执行了删除")
	}
	found := false
	for _, f := range res.Failed {
		if f.Path == fx.orig.Path && strings.Contains(f.Err, "保留") {
			found = true
		}
	}
	if !found {
		t.Fatalf("S2 未产生保护错误: %+v", res.Failed)
	}
	if _, err := os.Stat(fx.orig.Path); err != nil {
		t.Fatal("保留文件被删除")
	}
}

// ---------- S4：永久删除强制确认 ----------

func TestS4DeleteRequiresConfirm(t *testing.T) {
	fx := newFixture(t)
	// 未确认 → 后端拒绝（最后防线）
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 0 {
		t.Fatal("未确认的永久删除被执行")
	}
	if len(res.Failed) == 0 || !strings.Contains(res.Failed[0].Err, "确认") {
		t.Fatalf("应返回显式确认错误: %+v", res.Failed)
	}
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatal("文件被误删")
	}
	// 确认后 → 执行（临时目录非系统回收站场景，直接 os.Remove）
	res = Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})
	if len(res.OK) != 1 {
		t.Fatalf("确认后应执行: %+v", res)
	}
	if _, err := os.Stat(fx.dup1.Path); !os.IsNotExist(err) {
		t.Fatal("确认后未删除")
	}
}

// ---------- S5：回收站失败明确报错（不静默丢文件） ----------

func TestS5TrashFailureExplicit(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash("", true), // 注入失败
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 0 {
		t.Fatal("回收站失败不应计入 OK")
	}
	if len(res.Failed) == 0 {
		t.Fatal("S5 失败未上报（静默丢文件风险）")
	}
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatal("文件消失（严重：失败时文件被移动走了？）")
	}
}

// ---------- S6：0 字节（pipeline 内置跳过，此处验证不进结果集） ----------

func TestS6ZeroByteSkippedByPipeline(t *testing.T) {
	// 0 字节在 scanner.Walk 内置跳过（M1 已测），结果集不可能包含 → 语义联测在 dedup 包
}

// ---------- S7：中途失败其余继续 ----------

func TestS7PartialFailureContinues(t *testing.T) {
	fx := newFixture(t)
	// dup1 不存在（模拟已删）+ dup2 正常：ENOENT → skipped；dup2 → ok
	os.Remove(fx.dup1.Path)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})
	if len(res.Skipped) != 1 || res.Skipped[0] != fx.dup1.Path {
		t.Fatalf("消失文件应计入 Skipped: %+v", res)
	}
	if len(res.OK) != 1 || res.OK[0] != fx.dup2.Path {
		t.Fatalf("其余文件应继续处理: %+v", res)
	}
	// Skipped 不计入任何一栏（本用例的诉求）。
	//
	// 2026-09-21（M22，04 §6.8.8）：断言从 Reclaimed 改到 TrashedBytes，不是为了让门禁
	// 变绿——是本项论证了**原断言钉错了语义**：kind=trash 的文件进了回收站、数据仍在
	// 磁盘上，从来就不该出现在"已释放"栏里。S7 的本意（跳过项不参与累计）一字未改，
	// 只是改到正确的那一栏上验。
	if res.Reclaimed != 0 || res.TrashedBytes != fx.dup2.Size {
		t.Fatalf("跳过项不得参与累计、成功项应计入 TrashedBytes：reclaimed=%d trashed=%d",
			res.Reclaimed, res.TrashedBytes)
	}
}

// ---------- S8：ENOENT 语义 ----------

func TestS8EnoentSkipped(t *testing.T) {
	fx := newFixture(t)
	os.Remove(fx.dup1.Path)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.Failed) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("S8 语义错误: failed=%+v skipped=%+v", res.Failed, res.Skipped)
	}
}

// ---------- S1 扩展：hardlink 的 keep 源校验 ----------

// keep 源被篡改 → 合并必须拦截（否则硬链接会用新内容覆盖 dup 原内容，不可逆）。
func TestS1HardlinkKeepSourceTampered(t *testing.T) {
	fx := newFixture(t)
	// 篡改 keep 源：size 不变、中间字节变化（触发重算哈希 → 与组哈希不一致）
	raw, _ := os.ReadFile(fx.orig.Path)
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(fx.orig.Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 0 {
		t.Fatalf("keep 源被篡改时不应执行合并: %+v", res)
	}
	blocked := false
	for _, f := range res.Failed {
		if f.Path == fx.dup1.Path && strings.Contains(f.Err, "保留源") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("应报告保留源校验失败: %+v", res.Failed)
	}
	// dup1 原内容未被覆盖（与未动的 dup2 仍一致）
	d1, _ := os.ReadFile(fx.dup1.Path)
	d2, _ := os.ReadFile(fx.dup2.Path)
	if string(d1) != string(d2) {
		t.Fatal("dup 内容被篡改源覆盖（数据丢失）")
	}
}

// keep 源消失 → 无法合并，计入失败。
func TestS1HardlinkKeepSourceGone(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.orig.Path); err != nil {
		t.Fatal(err)
	}
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("keep 源消失时不应执行合并: %+v", res)
	}
	if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "保留源") {
		t.Fatalf("应报告保留源消失: %+v", res.Failed)
	}
}

// ---------- move：OK 记录源路径 ----------

// OK 语义 = 结果集中应清理的源路径（上层 gone 匹配依据），不是移动目标。
func TestMoveOKRecordsSourcePath(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(t.TempDir(), "moved")
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})
	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("move 的 OK 应记录源路径（结果集清理依据）: %+v", res.OK)
	}
	if _, err := os.Stat(fx.dup1.Path); !os.IsNotExist(err) {
		t.Fatal("源文件应已移走")
	}
	if _, err := os.Stat(filepath.Join(target, "dup1.bin")); err != nil {
		t.Fatal("目标文件应存在")
	}
}

// ---------- 校验快速路径 ----------

func TestVerifyFastPathAndTouch(t *testing.T) {
	fx := newFixture(t)
	pool := hasher.NewPool()

	// 快速路径：元数据未变
	if v, _ := VerifyFile(fx.dup1, fx.group.Hash, pool); v != VerdictPass {
		t.Fatalf("元数据未变应快速通过, got %d", v)
	}
	// touch（内容未变 mtime 变）→ 重算通过
	future := time.Unix(0, fx.dup1.ModTime).Add(time.Hour)
	os.Chtimes(fx.dup1.Path, future, future)
	if v, _ := VerifyFile(fx.dup1, fx.group.Hash, pool); v != VerdictPass {
		t.Fatalf("仅 touch 应通过（内容未变）, got %d", v)
	}
}

// ---------- P0-3：时间戳无法证明内容未变 ----------

// 原地改写但 mtime/ctime 逐纳秒不变（同一次时钟刻度内、或 cp -p 等保留时间戳的
// 程序化改写、或 FAT32/exFAT/HFS+ 等粗粒度卷）。修正前快速路径直接放行。
func TestS1TamperWithUnchangedMtime(t *testing.T) {
	fx := newFixture(t)
	pool := hasher.NewPool()

	// 记录并强制还原时间戳，模拟"内容变了、时间戳没变"
	st, _ := os.Stat(fx.dup1.Path)
	raw, _ := os.ReadFile(fx.dup1.Path)
	raw[len(raw)/2] ^= 0xFF
	raw[len(raw)/2+1] ^= 0xAA
	if err := os.WriteFile(fx.dup1.Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fx.dup1.Path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	// 前提确认：size 与 mtime 与扫描记录完全一致
	now, _ := os.Stat(fx.dup1.Path)
	if now.Size() != int64(fx.dup1.Size) || now.ModTime().UnixNano() != fx.dup1.ModTime {
		t.Fatalf("测试前提失效：元数据已变化")
	}

	// 内容级校验必须拦截（修正前：size+mtime 双一致 → 快速路径直接放行）
	if v, _ := VerifyFile(fx.dup1, fx.group.Hash, pool); v != VerdictFailed {
		t.Fatalf("元数据未变但内容已篡改，VerifyFile 应 Failed，got %d", v)
	}

	// 端到端：hardlink 合并的 keep 源被如此篡改时必须拦截
	keep := fx.orig
	ks, _ := os.Stat(keep.Path)
	kraw, _ := os.ReadFile(keep.Path)
	kraw[len(kraw)/2] ^= 0x55
	os.WriteFile(keep.Path, kraw, 0o644)
	os.Chtimes(keep.Path, ks.ModTime(), ks.ModTime())

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{keep.ID: true},
	}, model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 0 {
		t.Fatalf("keep 源被原地改写（时间戳未变）时不得执行合并: %+v", res)
	}
	// dup 原内容必须仍在
	after, _ := os.ReadFile(fx.dup1.Path)
	if string(after) != string(raw) {
		t.Fatal("dup 原内容被覆盖：数据丢失")
	}
}

// size 变化是少数仍能凭元数据断定的情形：重复组成员尺寸恒等，尺寸变了即非原物。
func TestVerifySizeMismatchFails(t *testing.T) {
	fx := newFixture(t)
	pool := hasher.NewPool()
	if v, _ := VerifyFile(fx.dup1, fx.group.Hash, pool); v != VerdictPass {
		t.Fatalf("未修改应通过, got %d", v)
	}
	raw, _ := os.ReadFile(fx.dup1.Path)
	os.WriteFile(fx.dup1.Path, append(raw, 'x'), 0o644)
	if v, _ := VerifyFile(fx.dup1, fx.group.Hash, pool); v != VerdictFailed {
		t.Fatalf("size 变化应 Failed, got %d", v)
	}
	// ENOENT 仍为 Skipped（S8）
	os.Remove(fx.dup2.Path)
	if v, _ := VerifyFile(fx.dup2, fx.group.Hash, pool); v != VerdictSkipped {
		t.Fatalf("已消失应 Skipped, got %d", v)
	}
}

// TestKeepPolicyDirectoryPriority 多目录优先级：按序取首个命中目录，
// 同目录多命中沿用最深匹配，全不命中该组跳过（不产生决策）。
func TestKeepPolicyDirectoryPriority(t *testing.T) {
	g := &model.DuplicateGroup{GroupID: 1, Files: []*model.FileEntry{
		{ID: 1, Path: "/keep2/a/photo.jpg", Size: 10},
		{ID: 2, Path: "/keep1/x/photo.jpg", Size: 10},
		{ID: 3, Path: "/other/photo.jpg", Size: 10},
	}}
	ds, _ := ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/keep1", "/keep2"}})
	if len(ds) != 1 || ds[0].KeepID != 2 {
		t.Fatalf("优先级首位命中: got %+v want keep 2", ds)
	}
	ds, _ = ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/miss", "/keep2"}})
	if len(ds) != 1 || ds[0].KeepID != 1 {
		t.Fatalf("次位命中: got %+v want keep 1", ds)
	}
	ds, _ = ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/miss"}})
	if len(ds) != 0 {
		t.Fatalf("全不命中应跳过: got %+v", ds)
	}
	g2 := &model.DuplicateGroup{GroupID: 2, Files: []*model.FileEntry{
		{ID: 4, Path: "/d/a.jpg", Size: 9}, {ID: 5, Path: "/d/sub/a.jpg", Size: 9}}}
	ds, _ = ApplyKeepPolicy([]*model.DuplicateGroup{g2}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"/d"}})
	if len(ds) != 1 || ds[0].KeepID != 5 {
		t.Fatalf("最深匹配: got %+v want keep 5", ds)
	}
	ds, _ = ApplyKeepPolicy([]*model.DuplicateGroup{g}, model.KeepPolicy{
		Kind: "directory", Directories: []string{"", "/keep1"}})
	if len(ds) != 1 || ds[0].KeepID != 2 {
		t.Fatalf("空目录项应忽略: got %+v want keep 2", ds)
	}
}
