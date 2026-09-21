package ops

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

// 关联隐患修复回归（2026-09-19）：
// 「身份校验在 Windows 上静默失效」——FromFileInfo(Lstat(...)) 在 Windows
// 恒返回未解析 ID，于是 identityStill 恒返回 true、undoHardlink 的防线①
// 退化为只比大小。此处逐条钉住修复后的行为。
//
// 注意：Windows 句柄路径无法在 unix 上执行；但**平台无关的契约**可以在此
// 完整验证：identityStill 对「被替换的路径」必须判否、对「未动的路径」必须判是。
// 修复前的实现恰好也满足这两条（unix 的 lstat 带 dev/ino），因此本文件主要防
// **回归**：若日后有人把 identityStill 改回 FromFileInfo 或改成跟随链接的 os.Open，
// 下面 TestIdentityStillDetectsSymlinkSwap 会立刻失败。
//
// ★ 更正一处旧注释（2026-09-22，设计稿 §21.1）：本文件此前写"本机为 Linux"，
// 并据此说"这两条在 unix 上恒可满足"。linux CI 腿把它证伪了——`(dev,ino)` 只在
// **两个对象同时存活**时保证不同，而"删掉再原地建"会让会还号的卷（GitHub
// ubuntu runner 的 /tmp）把同一个 inode 号发给新对象。顶替因此一律改成
// "先写旁边、再 rename 顶位"（swap_fixture_test.go），断言一字未动。

// TestIdentityStillDetectsRegularReplacement 路径被换成另一个文件 → 判否。
func TestIdentityStillDetectsRegularReplacement(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, err := fsid.FromPathNoFollow(p)
	if err != nil {
		t.Fatal(err)
	}
	if !orig.Resolved {
		t.Skip("本平台无法解析文件身份，跳过（Windows 句柄路径需在 Windows 上验证）")
	}

	// 替换：把另一个文件原子改名顶进该路径。刻意不用 Remove + WriteFile
	// （那会让新对象拿到刚被回收的 inode 号，身份层原理上识破不了；
	// 2026-09-22 linux CI 就是红在这一格上，见 §21.1 与 swap_fixture_test.go）。
	// 内容逐字节相同是**故意的**：这一格钉的必须是"只换了对象、没换内容"
	// 也能识破，否则等于把判据退化成比内容。
	swapInAt(t, p, orig, []byte("original"))

	if identityStill(p, orig) {
		t.Fatal("路径已被换成另一个文件，identityStill 必须判否（否则会覆盖第三方文件）")
	}
}

// TestIdentityStillAcceptsUntouchedPath 路径未被替换 → 判是。
func TestIdentityStillAcceptsUntouchedPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := fsid.FromPathNoFollow(p)
	if err != nil {
		t.Fatal(err)
	}
	if !id.Resolved {
		t.Skip("本平台无法解析文件身份")
	}
	if !identityStill(p, id) {
		t.Fatal("路径未被动过，identityStill 不应判否")
	}
	// 内容变更（仅重写内容）不应改变物理身份 → 仍应判是
	if err := os.WriteFile(p, []byte("different-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !identityStill(p, id) {
		t.Fatal("仅内容变更不应改变物理身份（原地重写不换 inode）")
	}
}

// TestIdentityStillDetectsSymlinkSwap 路径被换成指向别处的符号链接 → 判否。
//
// 这是**不跟随链接**的专项保护：若实现改用 os.Open/FromFile（会跟随链接），
// 这里会把"被换成链接"误判为"原文件仍在"，从而放行覆盖操作。
func TestIdentityStillDetectsSymlinkSwap(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.bin")
	at := filepath.Join(dir, "at.bin")
	if err := os.WriteFile(real, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(at, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := fsid.FromPathNoFollow(at)
	if err != nil {
		t.Fatal(err)
	}
	if !id.Resolved {
		t.Skip("本平台无法解析文件身份")
	}

	// 把 at.bin 换成指向 real.bin 的符号链接。链接先建在旁边再原子顶位：
	// 它的 inode 在 at.bin 仍存活时就已经分配，两个号必然不同（§21.1）。
	// 若实现改用跟随链接的 os.Open，这里会解出 real.bin 的号，同样与 at 不等
	// ⇒ 本格的"不跟随链接"钉子不受影响。
	swapSymlinkOnto(t, at, real, id)

	if identityStill(at, id) {
		t.Fatal("路径已被换成符号链接，identityStill 必须判否（不得跟随链接）")
	}
}

// TestIdentityStillPassesWhenVolumeLacksStableIndex **卷不提供稳定文件索引**时放行。
//
// 适用范围要收窄（2026-09-20 全仓审计 AS-H1，04 §6.8.1）：本用例钉的是
// FAT/exFAT 这类**卷本身给不出 (卷号, 文件索引)** 的场景——此时"原身份"本就
// 未知，无从比对，强行判否会让这些卷上完全无法执行清理；实际防线退回内容级
// 校验（VerifyFile 每次全量重算 BLAKE3）。
//
// ★ 它**不是**"未解析就放行"的通用许可证。NTFS/APFS/EXT4 等提供稳定索引的卷上
// 参照身份必须解析得动：若因取身份的口径不对而恒为未解析，本放行分支就会把
// 整条动作前复核变成空转（修正前 Windows 正是这个形态——FromFileInfo 在 Windows
// 恒未解析）。那一侧的守卫由 verify_identity_windows_test.go 的
// TestVerifyFileIdentityIsResolved / TestVerifyFileIdentityCatchesRenameSwap 钉住，
// 二者配对成立，本用例只负责"卷确实给不出索引"这一条。
//
// 依 04 §6.8.0 约束 2，这是被允许的那类改动：论证"断言本身钉住了过宽的语义"
// 后收窄其适用范围，并在断言旁写清理由与配对的用例位置。
func TestIdentityStillPassesWhenVolumeLacksStableIndex(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !identityStill(p, fsid.ID{}) {
		t.Fatal("原身份未解析时应放行（否则 FAT/exFAT 卷上无法操作）")
	}
}

// TestIdentityStillRejectsMissingPath 路径已消失 → 判否。
func TestIdentityStillRejectsMissingPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gone.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _ := fsid.FromPathNoFollow(p)
	if !id.Resolved {
		t.Skip("本平台无法解析文件身份")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if identityStill(p, id) {
		t.Fatal("路径已不存在，identityStill 必须判否")
	}
}

// TestUndoHardlinkBlocksSwappedTargetSameSize 端到端守卫：
// 目标被换成**另一个同样大小**的文件时，回撤必须拦截，绝不覆盖。
//
// 这正是修复前 Windows 上的漏洞形态：身份校验退化为只比大小，
// 同大小的第三方文件可以长驱直入被覆盖。修复后走句柄身份，必须拦住。
func TestUndoHardlinkBlocksSwappedTargetSameSize(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	payload := []byte("AAAA-payload-AAAA") // 固定长度
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := hashOfContent(t, payload)

	// 合并：dup 成为 keep 的硬链接
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatalf("合并应成功: %v", err)
	}

	// 用户把 dup 换成另一个**同样大小、内容不同**的文件（模拟"路径被换掉"）
	if err := os.Remove(dup); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("BBBB-REPLACED-BBB") // 同为 16 字节
	if len(replacement) != len(payload) {
		t.Fatalf("测试构造错误：替换内容需与原内容等长（%d vs %d）",
			len(replacement), len(payload))
	}
	if err := os.WriteFile(dup, replacement, 0o644); err != nil {
		t.Fatal(err)
	}

	it := UndoItem{Kind: "hardlink", OrigPath: dup, DestPath: "", LinkSrc: keep,
		Hash: h, Size: uint64(len(payload)), MtimeNs: 0}

	if _, err := UndoOne(it); err == nil {
		t.Fatal("目标已被换成同大小的第三方文件，回撤必须拦截（否则会覆盖用户文件）")
	} else {
		t.Logf("正确拦截：%v", err)
	}

	// 第三方文件必须原样保留
	got, err := os.ReadFile(dup)
	if err != nil {
		t.Fatalf("第三方文件不应消失: %v", err)
	}
	if string(got) != string(replacement) {
		t.Fatalf("第三方文件内容被改动: %q", got)
	}
}
