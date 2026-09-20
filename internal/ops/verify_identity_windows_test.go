//go:build windows

package ops

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// AS-H1（2026-09-20 全仓审计，第 7 篇 §三）：Windows 上整条「动作前身份复核」空转。
//
// 修正前 VerifyFile 用 fsid.FromFileInfo(f.Stat()) 产出参照身份，而
// fsid_windows.go 的 fromInfo 在 Windows **恒返回未解析 ID**（卷号与 64 位
// 文件索引只在 BY_HANDLE_FILE_INFORMATION 句柄查询里有，Lstat 产物拿不到）。
// 参照身份恒为 fsid.ID{} 时，identityStill 第一行 `if !id.Resolved { return true }`
// 直接放行 → executor 五处破坏性动作前复核与 symlink 的 keepID 守卫在本平台
// 全部平凡通过：扫描后第三方以 rename 顶替该路径，应用照删不误并记 done。
//
// 这两条用例是**本机（unix）恒绿、只在 windows job 上兑现 RED** 的那一类
// （04 §6.8.0 约束 5）：unix 的 FromFileInfo 本来就解析，缺陷不显现。

func writeVerifyTarget(t *testing.T, dir, name string, payload []byte) *model.FileEntry {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return &model.FileEntry{Path: p, Size: uint64(len(payload))}
}

// TestVerifyFileIdentityIsResolved 参照身份必须真的解析出来。
//
// 修复前在 Windows 上 id.Resolved 恒 false——那正是「复核看起来跑了、实际什么都没查」的形态。
func TestVerifyFileIdentityIsResolved(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("AS-H1-IDENTITY-PAYLOAD-0123456789abcdef")
	e := writeVerifyTarget(t, dir, "a.bin", payload)

	v, id := VerifyFile(e, hashOfFileT(t, e.Path), hasher.NewPool())
	if v != VerdictPass {
		t.Fatalf("未改动的文件应通过校验，实际 %v", v)
	}
	if !id.Resolved {
		t.Fatal("VerifyFile 返回的参照身份未解析：Windows 上 FromFileInfo 恒缺卷号/索引，" +
			"identityStill 因此平凡通过，整条动作前复核空转（AS-H1）——应改用句柄查询 FromFile(f)")
	}
	f, err := os.Open(e.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if want := fsid.FromFile(f); !id.SameIdentity(want) {
		t.Fatalf("参照身份与句柄查询不一致：%+v vs %+v", id, want)
	}
}

// TestVerifyFileIdentityCatchesRenameSwap 是上一条的**行为**证明，也是本缺陷的
// 实际危害形态：VerifyFile 通过后、破坏性动作前，第三方把该路径 rename 顶替成
// 另一个文件，identityStill 必须判否。
//
// 修复前 Windows 上判是（未解析 → 放行）；unix 上因为 lstat 带 dev/ino 本就判否，
// 所以这条回归只有 windows 腿能抓到。
func TestVerifyFileIdentityCatchesRenameSwap(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("AS-H1-SWAP-PAYLOAD-0123456789abcdefgh")
	e := writeVerifyTarget(t, dir, "victim.bin", payload)

	_, id := VerifyFile(e, hashOfFileT(t, e.Path), hasher.NewPool())
	if !id.Resolved {
		t.Skipf("该卷不提供稳定文件索引（FAT/exFAT），身份复核无从谈起: %+v", id)
	}

	// 第三方顶替：原文件挪走，同路径放一个不同内容的文件（模拟同步盘/下载器的原子改名）
	other := filepath.Join(dir, "third-party.bin")
	if err := os.WriteFile(other, []byte("someone else's brand new file!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(e.Path, filepath.Join(dir, "moved-away.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, e.Path); err != nil {
		t.Fatal(err)
	}

	if identityStill(e.Path, id) {
		t.Fatal("路径已被 rename 顶替，identityStill 必须判否（放行即错删第三方新文件）")
	}
}
