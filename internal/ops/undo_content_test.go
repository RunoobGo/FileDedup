package ops

// M4（2026-09-21，第 7 篇 §五 4）：`undoSourceCheck` 对 trash/move 只比 size，
// 从不比对内容哈希——而 `UndoItem` 的注释自述"Hash/Size/MtimeNs 为扫描时的内容证据，
// 回撤前据此复核现状未被第三方改动"。同尺寸不同内容的顶替（回收站目录被写工具
// 原地改过、移动目标被同名文件替换）今天就"校验通过"，然后把**错内容**放回家；
// 更糟的是 `applyMtime` 随即把 mtime 拨回记录值，事后从时间戳也看不出动过。
//
// undoHardlink 一直做全量 BLAKE3（`:244-250`），trash/move 与它同属
// "把文件放回原位"的动作，判据没有理由不一致。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hashBytesT 用与账本完全相同的算法哈希一段字节。
// 经临时文件走 hashOfFileT，因为 hasher.HashFull 收的是 *os.File。
func hashBytesT(t *testing.T, b []byte) [32]byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hashme")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return hashOfFileT(t, p)
}

// undoDestFixture 铺一个"回收站/移动目标侧有文件"的最小现场。
func undoDestFixture(t *testing.T, subdir, content string) (dest, orig string) {
	t.Helper()
	dir := t.TempDir()
	dest = filepath.Join(dir, subdir, "a.bin")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dest, filepath.Join(dir, "orig", "a.bin")
}

func TestUndoTrashRejectsSameSizeTamperedContent(t *testing.T) {
	const original = "ORIGINAL-PAYLOAD-AAAAAAAAAAAA"
	// 逐字节不同、长度完全相同：只有"等长改写"才能证明校验靠的是内容而非尺寸。
	tampered := strings.Repeat("B", len(original))
	dest, orig := undoDestFixture(t, ".Trash", tampered)
	// 账本记的是**原件**的哈希（回收站里那份已被同长度改写）
	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: hashBytesT(t, []byte(original)), Size: uint64(len(original)), MtimeNs: pastNs()}

	if _, err := UndoOne(it); err == nil {
		t.Fatal("同尺寸顶替内容被放行（返回 nil）：回撤校验只看 size，M4 未修")
	} else if !strings.Contains(err.Error(), "内容") && !strings.Contains(err.Error(), "哈希") {
		t.Fatalf("报错未说明是内容/哈希不符（用户会以为是路径问题）: %v", err)
	}
	// 拦截必须**不动现场**：错内容不许回家，回收站侧那份保持原样让用户自己核对
	if got, _ := os.ReadFile(orig); len(got) != 0 {
		t.Fatalf("原位被写入了东西（拦截失败）: %q", got)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != tampered {
		t.Fatalf("回收站侧现场被动过: %q %v", got, err)
	}
}

func TestUndoMoveRejectsSameSizeTamperedContent(t *testing.T) {
	const original = "ORIGINAL-MOVE-PAYLOAD-CCCCCCCC"
	tampered := strings.Repeat("D", len(original))
	dest, orig := undoDestFixture(t, "movedir", tampered)
	it := UndoItem{Kind: "move", OrigPath: orig, DestPath: dest,
		Hash: hashBytesT(t, []byte(original)), Size: uint64(len(original)), MtimeNs: pastNs()}

	if _, err := UndoOne(it); err == nil {
		t.Fatal("移动回撤同样只比 size：错内容被放回原目录")
	} else if !strings.Contains(err.Error(), "内容") && !strings.Contains(err.Error(), "哈希") {
		t.Fatalf("报错未说明是内容不符: %v", err)
	}
	if _, err := os.Lstat(orig); !os.IsNotExist(err) {
		t.Fatalf("原位不该出现文件（err=%v）", err)
	}
}

// TestUndoLegacyLedgerWithoutHashStillRestores 把"无内容证据时不猜、照常放行"
// 钉成显式语义：旧账本（升级前写入）没有哈希，若把它当"校验失败"，
// 用户的历史记录就全都撤不回来——那比 M4 本身的危害更大。
func TestUndoLegacyLedgerWithoutHashStillRestores(t *testing.T) {
	dest, orig := undoDestFixture(t, ".Trash", "LEGACY-NO-HASH")
	it := UndoItem{Kind: "trash", OrigPath: orig, DestPath: dest,
		Hash: [32]byte{}, Size: uint64(len("LEGACY-NO-HASH")), MtimeNs: pastNs()}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatalf("无哈希的旧账本被拦死（应放行）: %v", err)
	}
	if dst != orig {
		t.Fatalf("落点不符: %s", dst)
	}
}
