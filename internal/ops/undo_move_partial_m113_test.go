package ops

// M113（第 2 轮 §23.2，04 §6.11 OPS-14e）：undoMove 必须把 MoveFile 的落点交回去。
//
// MoveFile 有两条"返回 dst 且 err != nil"的部分成功路径（move.go:60-65 复制期间源被顶替、
// :66-72 删源失败）：盘上已经是两份，`dst` 是这份副本在整条链路上唯一的留痕。
// 而 undo.go:197-199 改前是 `return "", err`，把落点丢在函数里 ⇒
// app 层那两格兜子（undoExecuteItem:2255 连带 restored 一起返回、
// undoFailure:2211-2215 见 restored 非空就补"（数据已在 …）"，M86 立的规矩）
// 永远拿不到 target，用户看到的是一条不带落点的纯失败。
//
// 判据落在**返回值**而不是错误文本：MoveFile 的文本本来自带落点，
// 断文本子串会在"undoMove 换了个说法"时误报，也会在"文本带落点但返回空串"（改前的形状）时漏报。
//
// 改前必红：只引用改前就有的符号（undoMove / renameFile 与 removeSrc 接缝 /
// forceCrossVolumeRename），失败原因须落在"第一个返回值为空串"这一格。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUndoMovePartialSuccessHandsBackRestoredPath(t *testing.T) {
	dir := t.TempDir()
	origDir := filepath.Join(dir, "orig") // 回撤的落点（它存在，但原位上不该有文件）
	moveDir := filepath.Join(dir, "moved")
	for _, d := range []string{origDir, moveDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("M113-UNDO-MOVE-CROSS-VOLUME-PAYLOAD")
	dest := filepath.Join(moveDir, "home.bin")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}

	it := UndoItem{
		Kind:     "move",
		OrigPath: filepath.Join(origDir, "home.bin"),
		DestPath: dest,
		Size:     uint64(st.Size()),
	}
	// 原位必须空着，否则 undoSourceCheck 走的是"已还原"那条快路径，探针打不到本格。
	if _, lerr := os.Lstat(it.OrigPath); lerr == nil {
		t.Fatalf("前提自检：原位不该已有文件（夹具没做对）：%s", it.OrigPath)
	}

	forceCrossVolumeRename(t)
	origRemove := removeSrc
	removeSrc = func(string) error { return errSimulatedSrcRemove }
	t.Cleanup(func() { removeSrc = origRemove })

	restored, uerr := undoMove(it)

	// ---- 前提自检：这次确实落在"部分成功"那一格（先证明状态成立，再测判据）----
	if uerr == nil {
		t.Fatal("前提自检：删源失败时 undoMove 必须报错")
	}
	if _, lerr := os.Lstat(dest); lerr != nil {
		t.Fatalf("前提自检：回撤侧的原件应仍在（复制走的是 DestPath）: %v", lerr)
	}
	copied, cerr := os.ReadFile(it.OrigPath)
	if cerr != nil || string(copied) != string(payload) {
		t.Fatalf("前提自检：副本应已完整落在 %s（cerr=%v got=%q）", it.OrigPath, cerr, copied)
	}

	// ---- 判据格：部分成功必须把落点交回调用方 ----
	if restored == "" {
		t.Fatalf("部分成功却回了空落点 ⇒ app 层 undoFailure 的"+
			"「（数据已在 …）」永远补不出来，这份孤儿副本在界面上不存在: %v", uerr)
	}
	if got, werr := filepath.EvalSymlinks(restored); werr == nil {
		restored = got
	}
	want := it.OrigPath
	if rw, err := filepath.EvalSymlinks(want); err == nil {
		want = rw
	}
	if restored != want {
		t.Errorf("交回的落点 = %q, want %q（必须就是副本实际所在，不能是凭据猜的路径）", restored, want)
	}
}
