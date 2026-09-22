package fscase

// M126（04 §6.11 FC-3，设计段 §23.10）：verdictFrom 必须把"upper 确实不存在"
// 与"upper 读不动"分开——前者是"卷区分大小写"的**确证结论**，后者是**无从判定**，
// 只能按包注释（fscase.go:11-13、probe:116）退平台默认。
// 改前 `case uerr != nil: return true, true` 把两者混为一谈，且结果被
// Sensitive:55-70 按目录**永久缓存**：一次 EIO/ESTALE 就把整趟扫描的折叠语义钉死。
//
// 三条用例都不依赖被测卷的大小写语义：upper 只是被当作"另一个路径串"来 lstat，
// 所以它们在 darwin/linux 本机与 CI 三条腿上都是真跑，不靠"换个平台碰运气"。
//
// ★ 上面那段"三条"是真读数，本文件其后（第 3 轮 §24.4）又补了两条 M139/M140，
// 那两条同样不依赖被测卷的语义、且在 darwin 本机**必须真跑**（一条 Skip 都不新增）；
// 原三条与它们的一条 t.Skip、一条 t.Skipf 一字未动。

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

// ★ M139（§24.4 FC-4，取红方式 = §24.2 存活的变异 R3-MU5）：撞名那一格（fscase.go:171）零覆盖。
//
// 现有三条覆盖的是 upper「看不见 / 命中同一对象 / 读不动」，没有一条把
// upper = **另一个独立存在的文件**（不同 inode）造出来 ⇒ :171 那句 `return false, false`
// 从没被断过。两条真读数：§24.2 记的 R3-MU5 存活（包级 rc=0，逐字为
// `ok filededup/internal/fscase 0.398s`），本轮改前复算也一样——把既有 10 条单独跑一遍
// （1 条 SKIP、9 条 PASS）、rc=0，撞名那一格无人钉。
//
// ★ 为什么 ok=false 这一半是承重的：它是 probe 唯一能听懂「这次结果不可归因」的信号，
// 收到就换下一个名字重试（fscase.go:142-144：只有 ok 为真才 return v）。若这一格误报
// ok=true，v=false 就升格成「卷**不**区分大小写」的确证结论，并被 Sensitive()
// （fscase.go:54-70）按目录永久缓存（缓存不淘汰）——目录里有人放了一个撞名文件，
// 就能把错的卷语义钉死整趟扫描。
// ★ 判错方向也不是中性的，但别把后果读歪：这一格误报的是"敏感卷被判成不敏感"，
// 落的是包注释 :5-8 的第一类场景——真区分大小写的卷上 A 与 a 本是两个对象，
// 折叠后其中一棵**整棵静默不被扫描**（表现为"文件凭空消失"，且没有任何失败记录）。
// 包注释 :8-9 那句"重复收集 ⇒ 多删文件"是**反方向**的错（:173 误报才会走到），
// 两条都致命，但各归各的格子。
//
// v 那一半一并钉住字面值，但别读歪：ok=false 时 probe 根本不读 v，所以「只改坏 v」
// 在调用方不可观测 —— 本条真正的钉子是 ok。
//
// 本条不依赖被测卷的大小写语义（upper 与 lower 之间没有任何大小写关系，就是两个名字），
// 故在 darwin/APFS 上真跑、不 Skip。
func TestVerdictFromOtherFileAtUpperIsNameCollision(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "payload139.txt")
	upper := filepath.Join(dir, "payload139-other-file.txt")
	for _, p := range []string{lower, upper} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = os.Remove(lower)
		_ = os.Remove(upper)
	})

	// 夹具前提自检：两个名字必须命中**两个不同对象**。若命中同一对象，落点是 :169
	// （SameFile 支）而不是 :171，本条就白测了 —— 与上面那条对 ENOTDIR 的自检同一口径（I5）。
	li, lerr := os.Lstat(lower)
	if lerr != nil {
		t.Fatalf("夹具前提不成立：Lstat(%q) 失败 %v", lower, lerr)
	}
	ui, uerr := os.Lstat(upper)
	if uerr != nil {
		t.Fatalf("夹具前提不成立：Lstat(%q) 失败 %v", upper, uerr)
	}
	if os.SameFile(li, ui) {
		t.Fatalf("夹具前提不成立：lower=%q 与 upper=%q 命中同一对象 ⇒ 落不到撞名那一格，本条测不到 M139", lower, upper)
	}

	v, ok := verdictFrom(lower, upper)
	if ok {
		t.Fatalf("upper 位置上是另一个文件 ⇒ 无从归因，必须 ok=false（让 probe 换号），实得 ok=true、v=%v："+
			"这一格报成确证会被 Sensitive() 按目录永久缓存（M139）", v)
	}
	if v {
		t.Fatalf("撞名给不出「区分大小写」的读数，v 必须是 false（结论由换号后的下一轮给出），实得 v=true")
	}
}

// ★ M140（§24.4 FC-5）：本条**不新增判据格** —— 钉的还是 M126 已写下的那一格
// （fscase.go:172-173：`errors.Is(uerr, os.ErrNotExist)` ⇒ `return true, true`）。
// 它做的只有一件事：把那一格从「读数只归 CI 的 linux 腿」变成「本机三条腿都能读到」。
//
// 上面 TestVerdictFromUpperAbsentIsConfirmedSensitive 拿「同一个名字的另一种大小写」当
// upper，在不敏感卷（默认 APFS/NTFS）上 Lstat 必然命中同一对象 ⇒ 它只能自检后 t.Skip
// （就是它体内那句 t.Skip，M126 写下它之后一字未动；本文件行号因追加而后移，故不给坐标）。
// skip 永远不当通过读 —— 本仓叫 AS-K2。
//
// 本条的 upper 是一个**从未以任何大小写存在过的名字**：ENOENT 与卷的大小写语义无关，
// 所以三条腿都能真跑，不需要 Skip。
func TestVerdictFromNeverCreatedUpperIsConfirmedSensitive(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "payload140.txt")
	if err := os.WriteFile(lower, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lower) })
	upper := filepath.Join(dir, "payload140e-never-created.TXT")

	// 夹具前提自检：落点由 Lstat 自己说，不许「我猜它是 ENOENT」（I5 同口径）。
	// 报成别的错（如 ENOTDIR）说明这一卷把路径串归到 :174 那一格，本条就测不到 M140。
	if _, uerr := os.Lstat(upper); uerr == nil {
		t.Fatalf("夹具前提不成立：Lstat(%q) 竟然成功 ⇒ 本条测不到 ENOENT 那一格", upper)
	} else if !errors.Is(uerr, os.ErrNotExist) {
		t.Fatalf("夹具前提不成立：本卷把 %q 报成 %v 而非 ENOENT ⇒ 落的是「读不动」那一格，不是 M126 那格", upper, uerr)
	}

	v, ok := verdictFrom(lower, upper)
	if !ok {
		t.Fatalf("upper 从未存在是可归因的（ok=false 会被当成撞名而白换号）：v=%v ok=%v", v, ok)
	}
	if !v {
		t.Fatalf("换一种写法确实看不见 ⇒ 必须是确证的「区分大小写」，实得 v=%v（M140：这一格在本机也要读到）", v)
	}
}
