package ops

// M317（OPS-45，2026-09-28 第六轮全量审查）：`undoHardlink` 的②号防线是本函数
// 五条零值处置里**唯一**漏了 `it.Hash != [32]byte{}` 守卫的一条。
//
// 后果两条：
//   - v0.5.0 之前写入的账本行没有内容证据（Hash 全零），硬链接合并条目**永久撤不回**；
//   - 文案把原因说成"保留源内容与扫描记录不一致（已被修改）"——假归因，用户会以为
//     自己的文件被动过手脚，实际是我们手上根本没有可比对的证据。
//
// 在册口径本来就有：undoSourceCheck、undoSymlink 的备份腿②b 都明写
// "零值时跳过而不是拦死：一律拦会让历史记录全都撤不回"，undoSourceCheck 那段注释还写着
// "三条回撤路径的判据必须一致"——而 M4 交付段（docs/04:1421）声称 undoHardlink
// 已与另两条对齐，**外延没做到**。M4 当时的探针
// `TestUndoLegacyLedgerWithoutHashStillRestores` 只造了 `Kind:"trash"` 一条，
// hardlink 腿的零值格从未被断言过（本文件补上，并按约束 (1) 由登记表追记而非改写旧行）。
//
// ★ 第一条用例是**真·改前红**（不需要变异顶）：它只引用改前就有的符号
// （UndoOne / UndoItem / HardlinkMerge / pastNs），改前跑必然红在"被拦死"那一格，
// 改后绿。变异 M317-a（把守卫去掉）红在同一条；M317-b（把"零值跳过"扩到大小腿）
// 红在第二条——设计稿 §3.3 原本以为有一条现成的大小腿用例可当红靶，现读**没有**
// （default 分支要"句柄身份取不动"才进得去，改前的断言里无人构造过），故本文件补了第二条。
// 无 build tag：判据与平台无关（身份①在 unix 走 (dev,ino)、Windows 走句柄查询）。
// ★ 但第二条用例的**夹具**（chmod 000 造"打不开"）只在认权限位的卷上成立 ⇒ 那一格
// 按 M335 走"问卷不问系统名"的前置自检，造不出来时 Skip 并写明未验证，不是 Fail。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

func TestM317LegacyLedgerWithoutHashStillUnlinksHardlink(t *testing.T) {
	dir := t.TempDir()
	content := []byte("LEGACY-HARDLINK-LEDGER-WITHOUT-CONTENT-EVIDENCE")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "sub", "dup.bin")
	if err := os.MkdirAll(filepath.Dir(dup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatal(err)
	}

	mtime := pastNs()
	// Hash 刻意留零值：升级前写入的账本没有内容证据。
	it := UndoItem{Kind: "hardlink", OrigPath: dup, LinkSrc: keep,
		Hash: [32]byte{}, Size: uint64(len(content)), MtimeNs: mtime}

	dst, err := UndoOne(it)
	if err != nil {
		t.Fatalf("无内容证据的旧账本被拦死（应放行，与 undoSourceCheck/undoTrash 同判据）: %v", err)
	}
	if dst != dup {
		t.Fatalf("落点不符: %s", dst)
	}
	ki, di := statT(t, keep), statT(t, dup)
	if os.SameFile(ki, di) {
		t.Fatal("dup 仍与 keep 共享 inode，未拆链")
	}
	if got, _ := os.ReadFile(dup); string(got) != string(content) {
		t.Fatalf("dup 还原内容与保留源不符: %q", got)
	}
	if got, _ := os.ReadFile(keep); string(got) != string(content) {
		t.Fatalf("keep 被改动: %q", got)
	}
	if di.ModTime().UnixNano() != mtime {
		t.Fatalf("mtime 未还原: %v", di.ModTime())
	}
	if tmpLeft, _ := filepath.Glob(dup + FddUndoSuffix); len(tmpLeft) > 0 {
		t.Fatalf("回撤暂存残留: %v", tmpLeft)
	}
}

// M317 的边界格（红靶：变异 M317-b——把"零值跳过"扩到连大小都不比）：
// 放行只放行②号自校验，default 分支（句柄身份取不动）的大小一条必须照旧拦。
//
// 造 default 分支：合并后 chmod 000。dup 与 keep 是同一 inode，一次 chmod 两条路径
// 同时不可读，identityByHandle 的 os.Open 必撞 EACCES ⇒ 落 default；
// 而 Lstat 不需要读权限，大小腿照常可比对。root 忽略权限位 ⇒ 跳过。
//
// ★ 这条夹具的前提是"**本卷认 POSIX 权限位**"，而它比原设想窄得多（M335，2026-09-28
// CI run 36340988848 的 windows 腿唯一一条红就出在这里）。两条读数把旧注释推翻：
//   - Windows 上 `os.Geteuid()` **返回 -1 而不是 0**（GOROOT 的 syscall_windows.go 里
//     `func Geteuid() (euid int) { return -1 }`）⇒ 上一版注释"Windows 上恒为 0、这一格
//     会跳过"不成立，那条 Skip 从未在 windows 腿生效过；
//   - Windows 的 chmod 只翻 `FILE_ATTRIBUTE_READONLY`、不拒绝读 ⇒ "身份取不动"根本造不出来。
//
// 处置沿用同包两条先例的口径（M52 的 TestVerifyUnopenableIsUnverifiable、M91 的
// TestM91UnreadableDupIsUnverifiableNotModified）：**问卷而不问系统名**——先探一次
// `os.Open`，打得开就 Skip 并写明原因。★ 探针句柄**必须关掉**：CI 那次跟着报的第二条错
// （TempDir RemoveAll：dup.bin "被另一进程占用"）正是没关的探针挡住清理，不是判据红。
// 按 04 §6.8.0 约束 5，这一格在 Windows 侧记"未验证"，不许拿 unix 的绿冒充。
func TestM317ZeroHashStillChecksSize(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 忽略文件权限位，造不出\"身份取不动\"的现场")
	}
	dir := t.TempDir()
	content := []byte("SIZE-LEG-STILL-APPLIES-EVEN-WITHOUT-HASH")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	if err := os.WriteFile(keep, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := HardlinkMerge(keep, dup, fsid.ID{}, fsid.ID{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(keep, 0o644)
		_ = os.Chmod(dup, 0o644)
	})
	if err := os.Chmod(keep, 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(dup); err == nil {
		_ = f.Close()
		t.Skipf("本平台 chmod 不拒绝读（Windows 只翻只读位，exFAT/FAT 上权限位整个不存在）：" +
			"\"身份取不动\"这一前提造不出来 ⇒ default 分支的大小腿在本卷未验证（M335）")
	}

	// Size 故意与现场不符：只有大小腿能识破，内容自校验此时本就无从比对。
	it := UndoItem{Kind: "hardlink", OrigPath: dup, LinkSrc: keep,
		Hash: [32]byte{}, Size: uint64(len(content)) + 7, MtimeNs: pastNs()}

	if _, err := UndoOne(it); err == nil || !strings.Contains(err.Error(), "大小与记录不一致") {
		t.Fatalf("零值 Hash 时大小防线必须照旧拦（M317 只放内容自校验），实得: %v", err)
	}
}
