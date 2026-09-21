package ops

// M19（2026-09-21，设计稿 §8）：回收站回撤的**落位**一侧。
//
// 症状不是"没做校验"，而是**认领与使用不是同一个动作**：
//
//	if Lstat(OrigPath) 报 ENOENT → renameFile(DestPath, OrigPath)
//
// 两步之间第三方把文件放进 OrigPath（同步盘落一个同名文件、下载器原子改名进来，
// 都是这个时序），而 os.Rename 对已存在的普通文件是**静默替换**
// （Windows 腿是 MoveFileEx + MOVEFILE_REPLACE_EXISTING）——那个文件连名字带
// inode 一起消失，账本却记"已还原"。另名那一支（原位已被占）早在 M2 就用
// claimDst 原子抢占，只剩"原位看着空"这一支还是先查后用。
//
// 两条判据：
//  ① 落位前必须先认领那个确切名字（O_EXCL 占位）——第三方要么被挡在门外，
//     要么它落子后我们绝不静默替换掉它；
//  ② 失败支路必须清掉占位，不许把"崩溃后用户原名变成 0 字节文件"的窗口
//     留在磁盘上（设计稿 §8.5-4）。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const undoVictimData = "THIRD-PARTY-WRITTEN-IN-THE-WINDOW"

// undoTrashFixture 铺出"回收站里一份、原位空着"的标准现场。
func undoTrashFixture(t *testing.T) (UndoItem, string) {
	t.Helper()
	dir := t.TempDir()
	payload := []byte("TRASHED-PAYLOAD-必须原样回家")
	dest := filepath.Join(dir, "trash", "a.bin")
	orig := filepath.Join(dir, "orig", "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// 原位的父目录先建出来：本用例验证的是**名字**的抢占，不是 MkdirAll。
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		t.Fatal(err)
	}
	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashOfFileT(t, dest), Size: uint64(len(payload)), MtimeNs: pastNs()}
	return it, string(payload)
}

// TestUndoTrashClaimsOrigNameBeforeRename 钉住"认领与使用是同一个动作"。
//
// 接缝落在改名入口，也就是 Lstat 与 rename 之间**那个窗口本身**（不是近似）：
// 第三方在这里原子落子（O_EXCL 写自己的文件，正是下载器/同步盘的正常写法）。
// 落子成功 → 它的字节必须还在；落子被 EEXIST 挡下 → 说明名字已被我们抢占，
// 恢复必须照常回到原名。
func TestUndoTrashClaimsOrigNameBeforeRename(t *testing.T) {
	it, payload := undoTrashFixture(t)
	origRename := renameFile
	defer func() { renameFile = origRename }()

	var landed bool
	renameFile = func(oldp, newp string) error {
		if newp == it.OrigPath {
			f, err := os.OpenFile(newp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			switch {
			case err == nil:
				if _, werr := f.WriteString(undoVictimData); werr != nil {
					f.Close()
					t.Fatalf("写入第三方文件失败: %v", werr)
				}
				f.Close()
				landed = true
			case errors.Is(err, os.ErrExist):
				// 名字已被认领（修后行为）：第三方的写入被 EEXIST 挡在门外，
				// 与它自己"发现重名就换名/报错"的语义一致。
			default:
				t.Fatalf("在 %s 上落子时遇到意外错误: %v", newp, err)
			}
		}
		return origRename(oldp, newp)
	}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatalf("恢复本身应当成功（回收站侧与原位都未被改动）: %v", err)
	}

	if landed {
		// 修前走这一支：第三方文件落子成功，却被随后的 rename 静默替换。
		if b, rerr := os.ReadFile(it.OrigPath); rerr != nil || string(b) != undoVictimData {
			t.Fatalf("第三方文件在落位窗口里被静默覆盖（M19）：read=%q err=%v", b, rerr)
		}
		return
	}

	if dst != it.OrigPath {
		t.Fatalf("名字已被我们认领时，恢复必须回到原名 %s，实得 %s", it.OrigPath, dst)
	}
	if b, rerr := os.ReadFile(it.OrigPath); rerr != nil || string(b) != payload {
		t.Fatalf("恢复内容不符: %q err=%v", b, rerr)
	}
	if st := statT(t, it.OrigPath); st.ModTime().UnixNano() != it.MtimeNs {
		t.Fatalf("mtime 未还原: %v want %v", st.ModTime(), it.MtimeNs)
	}
}

// TestUndoTrashFailedRenameLeavesNoPlaceholder 反向守卫：认领留下的 0 字节占位
// 必须在失败支路被清掉——否则进程恰在此刻被杀，用户的**原名**位置上就留下一个
// 0 字节文件（本项新引入的崩溃窗口，必须让窗口尽可能短且不留痕）。
func TestUndoTrashFailedRenameLeavesNoPlaceholder(t *testing.T) {
	it, _ := undoTrashFixture(t)
	origRename := renameFile
	defer func() { renameFile = origRename }()
	renameFile = func(oldp, newp string) error {
		if newp == it.OrigPath {
			return errors.New("模拟：改名失败（非跨卷）")
		}
		return origRename(oldp, newp)
	}

	if _, err := UndoOne(it); err == nil {
		t.Fatal("改名失败必须上报")
	}
	if _, err := os.Lstat(it.OrigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失败后 %s 仍留有占位（0 字节文件）: err=%v", it.OrigPath, err)
	}
	if b, err := os.ReadFile(it.DestPath); err != nil || len(b) == 0 {
		t.Fatalf("回收站侧被牵连: %q err=%v", b, err)
	}
}
