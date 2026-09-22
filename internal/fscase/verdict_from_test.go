package fscase

// M126（04 §6.11 FC-3，设计段 §23.10）：verdictFrom 必须把"upper 确实不存在"
// 与"upper 读不动"分开——前者是"卷区分大小写"的**确证结论**，后者是**无从判定**，
// 只能按包注释（fscase.go:11-13、probe:116）退平台默认。
// 改前 `case uerr != nil: return true, true` 把两者混为一谈，且结果被
// Sensitive:55-70 按目录**永久缓存**：一次 EIO/ESTALE 就把整趟扫描的折叠语义钉死。
//
// 三条用例都不依赖被测卷的大小写语义；★ 但"不依赖语义"不等于"每条腿都造得出夹具"：
// CI 首跑的 Windows 腿把下面那个"读不动"形状报成 ENOENT（登记 M151，§25），红在前提
// 自检而不是产品判据 ⇒ 本条改按候选形状取"本平台确实读不动"的那一格，三条腿一律不 Skip。
//
// ★ 上面那段"三条"是真读数，本文件其后（第 3 轮 §24.4）又补了两条 M139/M140，那两条
// 同样不依赖被测卷的语义、且在 darwin 本机**必须真跑**；既有的那句 t.Skip 与 t.Skipf 一字未动。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	v, proven, ok := verdictFrom(lower, upper)
	if !ok {
		t.Fatalf("upper 不存在是可归因的（ok=false 会被当成撞名而白换号）：v=%v ok=%v", v, ok)
	}
	if !v {
		t.Fatalf("upper 位置上空着 ⇒ 必须是确证的\"区分大小写\"，实得 v=%v", v)
	}
	// M62+M85 补强：这一支是「换一种写法确实看不见」= 文件系统给的读数，必须标确证。
	// 它掉成 proven=false 不只是账目问题：probe 会因此丢掉①这一格实测，改走②卷型表
	// （fromVolumeType），而 ntfs/exfat 恰好在表里 ⇒ 一个「实测区分大小写」的卷被卷型
	// 猜成「不区分」，方向相反。
	if !proven {
		t.Fatalf("ENOENT 是实测读数，必须确证：实得 proven=false（v=%v）", v)
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

	v, proven, ok := verdictFrom(lower, upper)
	if !ok {
		t.Fatalf("SameFile 是可归因的：v=%v ok=%v", v, ok)
	}
	if v {
		t.Fatalf("两个名字命中同一对象 ⇒ 必须判\"不区分大小写\"，实得 v=%v", v)
	}
	// M62+M85 补强：命中同一对象同样是文件系统给的读数（这一格正是「这卷实测不区分」的
	// 实测形态），必须确证。它掉成 proven=false 的后果分两层：在 Default() 与真卷型
	// 相反的主机上（linux 上 Default=true，而这一格读到的是不敏感）结论会被翻成
	// 「区分大小写」⇒ 同一棵树的两种拼写各走一遍，重复组数与可释放空间虚高、据此下发
	// 的清理会多删文件；即便在 darwin 上值碰巧一致，这格也从「有读数」降为「没读数」，
	// M105 少一条可放宽的依据、计数链多一笔虚增。
	if !proven {
		t.Fatalf("SameFile 是实测读数，必须确证：实得 proven=false（v=%v）", v)
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

	// upper 必须是"读不动"而不是"不存在"。★ 形状按候选依次试（M151，§25）：同一形状在
	// 不同平台报的不是同一格 —— Windows 把 `dir/file/x` 报成 ERROR_PATH_NOT_FOUND，
	// 在 errors.Is(err, os.ErrNotExist) 眼里那就是"不存在"，本条就白测了。取第一个
	// "Lstat 失败且失败不是 ErrNotExist"的形状；一个都没有就硬红并逐条打印实测错误，
	// ★ 不许退成 t.Skip —— 那等于把该平台的读数来源从一条降为零条（AS-K2）。
	var unreadable string
	var ferr error
	var shape string
	var readings []string
	for _, cand := range []struct{ name, path string }{
		// ① 穿过一个普通文件：darwin/linux 报 ENOTDIR，是文件系统真给的"读不动"。
		{"under-regular-file", filepath.Join(lower, "x")},
		// ② 串里带 NUL：Go 在 UTF-16 转换层就拒绝（Windows 连 syscall 都不进，
		//    syscall_windows.go:39-44 直接 return EINVAL），三平台一律非 ErrNotExist。
		//    ★ Windows 腿走的是这一格：形状由 Go 拒绝而非文件系统给的 ENOTDIR，对
		//    verdictFrom 是同一个 default: 支，对"平台差异"账目不是同一件事。
		{"embedded-NUL", lower + "\x00x"},
	} {
		_, err := os.Lstat(cand.path)
		switch {
		case err == nil:
			readings = append(readings, fmt.Sprintf("%s：Lstat 竟然成功 ⇒ 不合格", cand.name))
		case errors.Is(err, os.ErrNotExist):
			readings = append(readings, fmt.Sprintf("%s：%v ⇒ 落 ENOENT 那一格，不合格", cand.name, err))
		default:
			readings = append(readings, fmt.Sprintf("%s：%v ⇒ 合格", cand.name, err))
			if unreadable == "" {
				unreadable, ferr, shape = cand.path, err, cand.name
			}
		}
	}
	if unreadable == "" {
		t.Fatalf("夹具前提不成立：两种「读不动」形状在本平台都造不出来 ⇒ 测不到 M126 那一格\n%s",
			strings.Join(readings, "\n"))
	}
	t.Logf("本平台钉「读不动」那一格用的形状：%s（%v）", shape, ferr)

	v, proven, ok := verdictFrom(lower, unreadable)
	if !ok {
		t.Fatalf("退默认值也是结论（ok=false 会白耗换号重试）：v=%v ok=%v", v, ok)
	}
	if v != Default() {
		t.Fatalf("upper 读不动 ⇒ 必须退平台默认 %v，实得确证值 %v（%v）："+
			"这是把\"无从判定\"报成\"卷区分大小写\"，且会被 Sensitive() 永久缓存", Default(), v, ferr)
	}
	// M62+M85 补强：这一格是「读不动」而不是「看不见」，proven 必须是 false。
	// 它是 M62 那一档存在的理由：只有这一位为假，probe 才会去问卷型（M85 的落点），
	// 并在卷型也读不到时如实记为未确证。若这一格写成 true，「一次 EIO 钉死整趟扫描」
	// 的老问题只是换了个值继续存在，而计数链永远报 0。
	if proven {
		t.Fatalf("upper 读不动给不出证据，proven 必须是 false（%v），实得 true", ferr)
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

	v, proven, ok := verdictFrom(lower, upper)
	if ok {
		t.Fatalf("upper 位置上是另一个文件 ⇒ 无从归因，必须 ok=false（让 probe 换号），实得 ok=true、v=%v："+
			"这一格报成确证会被 Sensitive() 按目录永久缓存（M139）", v)
	}
	if v {
		t.Fatalf("撞名给不出「区分大小写」的读数，v 必须是 false（结论由换号后的下一轮给出），实得 v=true")
	}
	// M62+M85 补强：撞名同样没有任何证据 ⇒ proven 必须为假。这一格 ok 已为假，
	// probe 走的是换号而不是问卷型，所以它是三态里「两个 bool 都假」的那一种形状；
	// 若把 proven 误置 true，probing 会在不可归因的读数上直接返回（本轮不钉这个分支，
	// 只保证 verdictFrom 自己不撒谎）。
	if proven {
		t.Fatalf("撞名读数不可归因 ⇒ proven 必须为 false，实得 true（v=%v）", v)
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

	v, proven, ok := verdictFrom(lower, upper)
	if !ok {
		t.Fatalf("upper 从未存在是可归因的（ok=false 会被当成撞名而白换号）：v=%v ok=%v", v, ok)
	}
	if !v {
		t.Fatalf("换一种写法确实看不见 ⇒ 必须是确证的「区分大小写」，实得 v=%v（M140：这一格在本机也要读到）", v)
	}
	// M62+M85 补强。★ 与上面第一条同一格（ENOENT ⇒ 确证），但第一条在不敏感卷上会
	// t.Skip（本文件体内那句），这条不会 ⇒ 三平台都能读到 proven=true 这一格。
	// 这不只是重复钉一遍：把 ENOENT 那一支写成 proven=false 的变异，在 Windows 腿上
	// 只有这一条测得到（第一条在那里 Skip）。
	if !proven {
		t.Fatalf("ENOENT 是实测读数，必须确证：实得 proven=false（v=%v）", v)
	}
}
