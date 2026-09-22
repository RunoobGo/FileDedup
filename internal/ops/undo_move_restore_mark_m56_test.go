package ops

// M56（2026-09-22 裁定"与 undoTrash 同形"，设计稿 §27.4）：回撤成功时回的是**实际落点**，
// 而命名规则必须与回收站回撤那一条共用同一份判据。
//
// 改前 undoMove 走 MoveFile(dest, dir(OrigPath))，它的重名处理是**递增改名**
// （home.bin → home-1.bin 这类兄弟名）；改后判据是"确切名字能占就回原名，占不了才落
// name.fdd-restored.ext"，与 undoTrash 共用同一条链（同一段代码，不是同一套文字）。
// 本探针红在名字上，不需要任何 seam：原位放一个第三方文件即可。
//
// ★ 为什么这条判据值得单独钉：递增名把"回撤"变成了"往旁边放一份"，用户按账本回
// OrigPath 找不到文件；而 .fdd-restored 是 worktemp 认得的家族（扫描侧据此跳过暂存），
// 递增名不在那族里 ⇒ 下一次扫描会把刚撤出来的副本当用户数据再参与一轮去重。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/worktemp"
)

func TestUndoMoveOccupiedOriginalUsesRestoreMark(t *testing.T) {
	dir := t.TempDir()
	origDir := filepath.Join(dir, "orig")
	moveDir := filepath.Join(dir, "moved")
	for _, d := range []string{origDir, moveDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("M56-UNDO-MOVE-RESTORE-MARK-PAYLOAD")
	dest := filepath.Join(moveDir, "home.bin")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	// 原位被一个陌生文件占住：回撤不得覆盖它，也不得躲成递增的兄弟名。
	stranger := []byte("stranger")
	if err := os.WriteFile(filepath.Join(origDir, "home.bin"), stranger, 0o644); err != nil {
		t.Fatal(err)
	}

	it := UndoItem{
		Kind:     "move",
		OrigPath: filepath.Join(origDir, "home.bin"),
		DestPath: dest,
		Size:     uint64(st.Size()),
	}
	restored, uerr := undoMove(it)
	if uerr != nil {
		t.Fatalf("undoMove 失败: %v", uerr)
	}

	want := filepath.Join(origDir, "home"+FddRestoreMark+".bin")
	if restored != want {
		t.Errorf("落点 = %q, want %q（原位被占时必须走 .fdd-restored 家族，"+
			"实得递增兄弟名即 M56 未改；实得族外名即标记被换：回撤成了'往旁边放一份'，"+
			"而那份产物不在 worktemp 的暂存族里）", restored, want)
	}
	// ★ 落点契约（§27.4）：返回值必须就是字节实际所在，不能是凭推断拼出来的路径。
	if back, rerr := os.ReadFile(restored); rerr != nil || string(back) != string(payload) {
		t.Fatalf("返回值 %q 处读不回原内容（rerr=%v got=%q）：界面上的「数据已在 …」就是假话", restored, rerr, back)
	}
	if kept, rerr := os.ReadFile(it.OrigPath); rerr != nil || string(kept) != string(stranger) {
		t.Errorf("第三方文件被改动（rerr=%v got=%q）：宁另名不覆盖是这两条回撤路径共同的底线", rerr, kept)
	}
	if _, lerr := os.Lstat(dest); !os.IsNotExist(lerr) {
		t.Errorf("回撤后移动目标侧仍留着原件: %v", lerr)
	}
	// 反向格：落点名必须落在 worktemp 认得的暂存族里。这条才是"往旁边放一份"与
	// "回撤产物"的分工线：递增兄弟名在族外，下一次扫描会把它当用户数据再吃一轮。
	if !worktemp.IsTempName(filepath.Base(restored)) {
		t.Errorf("落点 %q 不在 worktemp 的暂存族内（族外名字不会被扫描侧跳过）", restored)
	}
}

// ★ 原位**空闲**那一格不在这里重复：undo_test.go 的 TestUndoMoveReturnsToOrigDir
// 已经钉住"回确切原名 + 内容一致 + 目标侧不残留 + mtime 还原"，M56 改判据后它仍绿
// （抢占成功路径与改前的 MoveFile 成功路径给同一个落点）。本文件只补它覆盖不到的
// "原位被占"那一腿。
