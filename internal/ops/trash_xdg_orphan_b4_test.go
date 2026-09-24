package ops

// B4（R-操作-2，2026-09-24 第五轮审查 P0 批）：XDG 跨卷腿「复制成功、删源失败」
// 的孤儿处置。moveIntoTrash 跨卷分支复制出 dst 副本后，若 removeSrc 失败（Windows
// 杀软/索引器占用句柄、EACCES、EBUSY…），现状是 `return removeSrc(src)` 裸返回——
// dst 副本留在 Trash/files，而调用方 trashXDG 因错误不是 errCopiedSrcSwapped 会删掉
// trashinfo，于是副本退化成**回收站孤儿**（无元数据、DE 看不到、用户无从还原），
// 且 executor C6 逐文件重试会再复制一份，占用翻倍。
//
// 口径（计划 Step 2 裁定）：删源失败即 `os.Remove(dst)`——dst 是本函数刚创建的，
// 归属可证明，清掉它不构成数据损失（源仍在原处、字节未动）。与「复制失败清半成品」
// 分支同形。错误文本说明「源未动、无残留」，并以 %w 包出底层删源原因。
//
// 本文件无 build tag：守卫行为在 CI 三腿与开发机同判据真跑（H6 惯例，同
// trash_xdg_identity_test.go）。时序由包级接缝钉死：renameFile 稳定 EXDEV 进入
// 复制腿；removeSrc 注入失败模拟「复制已成、删源不成」。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errRemoveSrcBoom 是注入的删源失败原因（模拟 EACCES/EBUSY）。
var errRemoveSrcBoom = errors.New("simulated EACCES/EBUSY on removeSrc")

// TestTrashXDGRemoveSrcFailNoOrphan：跨卷复制成功但删源失败时，
// ① 源必须原封不动（未动、内容不变）；
// ② dst 副本必须被清掉（files/ 空）——不得留回收站孤儿；
// ③ trashinfo 必须被清掉（info/ 空）——调用方按非哨兵错误回滚，与 ② 闭合；
// ④ 错误文本点名「源未动/无残留」并 %w 包出底层删源原因；
// ⑤ dstMap 不得收下该失败项。
func TestTrashXDGRemoveSrcFailNoOrphan(t *testing.T) {
	root := t.TempDir()
	trashRoot := filepath.Join(root, "trash")
	src := filepath.Join(root, "victim.bin")
	payload := []byte("ORIGINAL-CONTENT-COPIED-THEN-ORPHANED")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	forceCrossVolumeRename(t) // 稳定进入跨卷复制腿

	origRemove := removeSrc
	removeSrc = func(string) error { return errRemoveSrcBoom }
	t.Cleanup(func() { removeSrc = origRemove })

	m, err := trashXDG(trashRoot, []string{src})

	// ① 源未动
	if err == nil {
		t.Fatal("删源失败时 trashXDG 不得报告成功（否则账本记 done 而源仍在原处）")
	}
	got, rerr := os.ReadFile(src)
	if rerr != nil {
		t.Fatalf("源必须原封不动地留在原处，实际读不出: %v", rerr)
	}
	if string(got) != string(payload) {
		t.Fatalf("源内容被改动: %q", got)
	}

	// ② dst 副本被清掉（无回收站孤儿）——归属可证明：本函数刚创建
	fileEnts, ferr := os.ReadDir(filepath.Join(trashRoot, "files"))
	if ferr != nil {
		t.Fatalf("files/ 读不出: %v", ferr)
	}
	if len(fileEnts) != 0 {
		names := make([]string, len(fileEnts))
		for i, e := range fileEnts {
			names[i] = e.Name()
		}
		t.Fatalf("删源失败后 files/ 不得残留副本（回收站孤儿），实际残留 %v", names)
	}

	// ③ trashinfo 被清掉（调用方按非哨兵错误回滚，与 ② 闭合）
	infoEnts, ierr := os.ReadDir(filepath.Join(trashRoot, "info"))
	if ierr != nil {
		t.Fatalf("info/ 读不出: %v", ierr)
	}
	if len(infoEnts) != 0 {
		names := make([]string, len(infoEnts))
		for i, e := range infoEnts {
			names[i] = e.Name()
		}
		t.Fatalf("删源失败后 info/ 不得残留 trashinfo（与副本清理口径必须闭合），实际残留 %v", names)
	}

	// ④ 错误文本点名「源未动/无残留」并 %w 包出底层原因
	if !errors.Is(err, errRemoveSrcBoom) {
		t.Errorf("错误必须用 %%w 包出底层删源原因，got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "源未动") || !strings.Contains(msg, "无残留") {
		t.Errorf("错误文本必须说明「源未动、无残留」，got %q", msg)
	}

	// ⑤ dstMap 不收失败项
	if _, ok := m[src]; ok {
		t.Errorf("删源失败的一腿不是成功，dstMap 不得收该项: %v", m)
	}
}
