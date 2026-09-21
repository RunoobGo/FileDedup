package fscase

// M126（04 §6.11 FC-3，设计段 §23.10）：verdictFrom 必须把"upper 确实不存在"
// 与"upper 读不动"分开——前者是"卷区分大小写"的**确证结论**，后者是**无从判定**，
// 只能按包注释（fscase.go:11-13、probe:116）退平台默认。
// 改前 `case uerr != nil: return true, true` 把两者混为一谈，且结果被
// Sensitive:55-70 按目录**永久缓存**：一次 EIO/ESTALE 就把整趟扫描的折叠语义钉死。
//
// 三条用例都不依赖被测卷的大小写语义：upper 只是被当作"另一个路径串"来 lstat，
// 所以它们在 darwin/linux 本机与 CI 三条腿上都是真跑，不靠"换个平台碰运气"。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerdictFromUpperAbsentIsConfirmedSensitive(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "payload126.txt")
	if err := os.WriteFile(lower, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lower) })
	upper := filepath.Join(dir, "PAYLOAD126.TXT")

	// ★ 本卷不区分大小写时（默认 APFS/NTFS），"另一种写法"本来就看得见 ⇒
	// ENOENT 那一支在本机上**到不了**，硬断 v=true 等于拿本机语义当普适判据
	// （首跑就是这么红的：实得 v=false，红在夹具前提而不是产品）。
	// 落点因此由 Lstat 自己说：看不见才测得到这一支；看得见就明示未取读数。
	if _, uerr := os.Lstat(upper); uerr == nil {
		t.Skip("本卷不区分大小写（另一种写法命中同一对象）⇒ 本条测不到 ENOENT 那一支，" +
			"不算通过（AS-K2）；该支由 CI 的 linux 腿（区分大小写的 ext4）真跑")
	}

	v, ok := verdictFrom(lower, upper)
	if !ok {
		t.Fatalf("upper 不存在是可归因的（ok=false 会被当成撞名而白换号）：v=%v ok=%v", v, ok)
	}
	if !v {
		t.Fatalf("upper 位置上空着 ⇒ 必须是确证的\"区分大小写\"，实得 v=%v", v)
	}
}

// 卷**不**区分大小写时（Windows/默认 APFS 那一族），另一种写法命中同一个对象。
// 用硬链接造同一个对象，是为了不依赖卷的大小写语义就能走到 SameFile 那一支。
func TestVerdictFromSameObjectIsInsensitive(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "payload126b.txt")
	if err := os.WriteFile(lower, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lower) })
	upper := filepath.Join(dir, "twin126b.txt")
	if err := os.Link(lower, upper); err != nil {
		t.Skipf("该卷不支持硬链接（%v）⇒ 本条未取读数，不算通过（AS-K2）", err)
	}
	t.Cleanup(func() { _ = os.Remove(upper) })

	v, ok := verdictFrom(lower, upper)
	if !ok {
		t.Fatalf("SameFile 是可归因的：v=%v ok=%v", v, ok)
	}
	if v {
		t.Fatalf("两个名字命中同一对象 ⇒ 必须判\"不区分大小写\"，实得 v=%v", v)
	}
}

// ★ 这一条是 M126 的本体：upper **读不动**（不是不存在）时不得给确证结论。
func TestVerdictFromUnreadableUpperFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "payload126c.txt")
	if err := os.WriteFile(lower, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lower) })

	// 把 upper 做成"位于一个普通文件之下"的路径 ⇒ Lstat 报 ENOTDIR（"读不动"），
	// 而不是 ENOENT（"不存在"）。夹具前提自检：判据落不到 ENOTDIR 就别往下测（I5）。
	unreadable := filepath.Join(lower, "x")
	_, ferr := os.Lstat(unreadable)
	if ferr == nil {
		t.Fatalf("夹具前提不成立：Lstat(%q) 竟然成功", unreadable)
	}
	if errors.Is(ferr, os.ErrNotExist) {
		t.Fatalf("夹具前提不成立：本平台把这个串报成 ENOENT 而非\"读不动\"（%v）⇒ 测不到 M126 那一格", ferr)
	}

	v, ok := verdictFrom(lower, unreadable)
	if !ok {
		t.Fatalf("退默认值也是结论（ok=false 会白耗换号重试）：v=%v ok=%v", v, ok)
	}
	if v != Default() {
		t.Fatalf("upper 读不动 ⇒ 必须退平台默认 %v，实得确证值 %v（%v）："+
			"这是把\"无从判定\"报成\"卷区分大小写\"，且会被 Sensitive() 永久缓存", Default(), v, ferr)
	}
}
