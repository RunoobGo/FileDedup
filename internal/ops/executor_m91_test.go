package ops

// ============================================================================
// M91（2026-09-22 裁定 A 案，设计稿 §28.4）：**会销毁 dup 原内容**的三条腿
// （delete / hardlink / symlink）在动手前对 dup 再做一次内容级复核。
//
// 症状不是「没复核」，而是「复核的口径不对」：
//
//	校验循环（executor.go:266）✅ → [用户看结果 / 勾选项 / 点执行：**无界时间**] →
//	  guardIdentity ✅ → guardContent（修前不存在）→ os.Remove / HardlinkMerge / SymlinkMerge
//
// guardIdentity 读的是 `(dev,ino)`。这个索引的定义域只到「同时存活的对象」
// （swap_fixture_test.go 头部把这条讲透了），而**就地改写同一个 inode**（编辑器
// truncate+write、`dd conv=notrunc`、数据库追加）根本不产生新对象：号不变、
// size 也可以不变 ⇒ 身份复核恒放行 ⇒ 动作把那份新内容销毁掉。delete 永久删除；
// hardlink/symlink 用 keep 覆盖 dup 位置。两边都不可逆，丢的还是「扫描之后才出现的
// 那份数据」——本应用从未获授权处置它。
//
// ★ 与 M91 立项时那条缺口（§21.2「第三方先删后建、在会还号的卷上骗过 (dev,ino)」）
// 的关系要说准，不许借本文件拔高：这里造的是**同 inode 就地改写**，这一格本机可
// 复现、修前必红；而「删后建 + inode 还号」在 darwin/APFS 上造不出来（内核不复用
// ino），本批**没有**把它测掉，也不许写成「M91 已消除」。内容级复核顺带也覆盖那一格
// （还来的号上内容必与组哈希不同 ⇒ Failed），但那条判据本机不可测，所以不钉。
//
// ★ 取红方式的诚实说明：`beforeActContentRecheck` 这道缝本身就是本批新增的，
// 「修前」的形态是**缝不存在 ⇒ 钩子无处落脚 ⇒ 改写压根没发生**，那样的红会落在
// 前置自检上而不是数据丢失上（第一次试 M25-f 正是这个形状）。所以本文件的修前红
// 靠变异取：保留钩子、去掉判据（M25-f 恒放行 / M25-e 只比 size）。
// ============================================================================

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// m91ForeignMarker 是「扫描之后被别人写进来的新内容」。默认与被改写的 dup **等长**
// （fixture 的 4096 字节），为的是让红只可能来自内容这一维：如果顺手改了 size，
// 红就有两种读法（「尺寸守卫生效」与「内容守卫生效」），而 M91 要钉的只有后者。
// 变长那一格单独有一条用例，见 TestM91DeleteBlocksSizeChangingRewrite。
var m91ForeignMarker = []byte(strings.Repeat("M91-FOREIGN", 400)[:4096])

// recheckProbe 是钩子的现场记录：跑了几次、改前改后的身份与 mtime，以及**改写当下**
// 盘上的字节。快照必须在钩子里取：Execute 返回之后再读盘，读到的是「守卫有没有生效」
// 的结果，而不是「这一格的前提成不成立」——两者混在一处，守卫掉线时会把红报成夹具
// 问题（M25-f 第一次跑正是这个形状）。
type recheckProbe struct {
	fired     int
	prevIno   fsid.ID
	postIno   fsid.ID
	prevMod   time.Time
	postMod   time.Time
	snapBytes []byte
	wantBytes []byte
}

// sameInode 报告「改写前后是否同一个物理对象」。两侧任一未解析时返回 false，
// 由调用方说明「前提无法自检」——**不许**因为自检不了就跳过整条断言。
func (r *recheckProbe) sameInode() bool {
	return r.prevIno.Resolved && r.postIno.Resolved &&
		r.prevIno.Dev == r.postIno.Dev && r.prevIno.Ino == r.postIno.Ino
}

// installAtContentRecheck 换装 guardContent 入口的缝（生产恒 nil），在「身份复核已过、
// 内容复核未发」这一刻对 dup 动手。fn 动手后**返回它认为盘上应该是什么字节**，
// 由钩子与紧随其后的读数对账——这样"等长改写"和"变长改写"两种夹具共用一份自检。
//
// expectFire 是这条用例声明的期望次数：破坏性腿传 1；move/trash 的负控制传 0
// （那两条腿**不该**接这道复核，传 0 让「顺手补一致」的多付一倍读盘当场红）。
func installAtContentRecheck(t *testing.T, dup string, expectFire int, fn func(string) ([]byte, error)) *recheckProbe {
	t.Helper()
	probe := &recheckProbe{}
	orig := beforeActContentRecheck
	beforeActContentRecheck = func(p string) {
		if p != dup {
			return
		}
		// 钩子跑在 worker goroutine 里：只许 t.Errorf，用 t.Fatal 会带走别的用例。
		prev, err := fsid.FromPathNoFollow(p)
		if err != nil {
			t.Errorf("钩子取改写前身份失败: %v", err)
			return
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Errorf("钩子取改写前 mtime 失败: %v", err)
			return
		}
		want, err := fn(p)
		if err != nil {
			t.Errorf("钩子改写 %s 失败: %v", p, err)
			return
		}
		if want == nil {
			// fn 明说"这一刻之后不该再按内容读得到它"（已消失 / 读不动两格）：
			// 身份与字节快照都无从取，前提由各用例自己钉，这里只数次数。
			probe.fired++
			return
		}
		after, err := fsid.FromPathNoFollow(p)
		if err != nil {
			t.Errorf("钩子取改写后身份失败: %v", err)
			return
		}
		st2, err := os.Stat(p)
		if err != nil {
			t.Errorf("钩子取改写后 mtime 失败: %v", err)
			return
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("钩子改写后 %s 立刻读不动: %v", p, err)
			return
		}
		probe.prevIno, probe.postIno = prev, after
		probe.prevMod, probe.postMod = st.ModTime(), st2.ModTime()
		probe.snapBytes, probe.wantBytes = b, want
		probe.fired++
	}
	t.Cleanup(func() {
		beforeActContentRecheck = orig
		if probe.fired != expectFire {
			t.Errorf("前置条件未成立：%s 上 guardContent 入口钩子跑了 %d 次，期望 %d 次"+
				"（0 次 = 这条用例什么都没验证；多于期望 = 复核被挂进了别的腿）",
				dup, probe.fired, expectFire)
		}
	})
	return probe
}

// rewriteTo 就地改写：同一个 fd 上 Truncate + 写入 ⇒ dev/ino 不变。
// 这正是编辑器保存、`dd conv=notrunc`、数据库追加的形状，也是 (dev,ino) 原理上
// 看不见的唯一一种改写（不需要 inode 还号这一前提，见文件头）。
func rewriteTo(data []byte) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		f, err := os.OpenFile(p, os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if err := f.Truncate(int64(len(data))); err != nil {
			return nil, err
		}
		if _, err := f.Write(data); err != nil {
			return nil, err
		}
		return data, nil
	}
}

func rewriteInPlace(p string) ([]byte, error) { return rewriteTo(m91ForeignMarker)(p) }

// touchOnly 只推进 mtime，内容一字不动——guardContent 最容易误伤的形状。
// 判据必须只看内容；若有人把判据退化成「元数据未变才放行」（或 §28.4 里被否决的
// CtimeNs 比较），TestM91LetsTouchedButUnchangedDupThrough 当场红。
func touchOnly(p string) ([]byte, error) {
	before, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	fut := st.ModTime().Add(time.Hour)
	if err := os.Chtimes(p, fut, fut); err != nil {
		return nil, err
	}
	return before, nil
}

// assertM91Premise 钉住「红的是不是那一格」：改写必须真的落在同一个 inode 上，
// 且改写当下盘上的字节确实是那份新内容。这两条任一不成立，后面的拦截断言就是白测
// （它可能只是在庆祝「改写根本没发生」）。
func assertM91Premise(t *testing.T, dup string, probe *recheckProbe) {
	t.Helper()
	if !probe.prevIno.Resolved || !probe.postIno.Resolved {
		// 卷不给稳定索引：身份层本来就无从比对（guardIdentity 在这种卷上恒放行），
		// 这一格的前提自检不了。不 Skip——内容级判据根本不依赖身份，照测。
		t.Logf("本卷不给稳定索引（prev=%+v post=%+v）：同 inode 前提无法自检，"+
			"但 M91 的判据是内容级、不依赖身份，断言照常成立", probe.prevIno, probe.postIno)
	} else if !probe.sameInode() {
		t.Fatalf("夹具前提不成立：改写后 %s 的身份从 (dev=%x,ino=%d) 变成 (dev=%x,ino=%d)，"+
			"这已经不是「就地改写」那一格了（红的是夹具，不是守卫）",
			dup, probe.prevIno.Dev, probe.prevIno.Ino, probe.postIno.Dev, probe.postIno.Ino)
	}
	if string(probe.snapBytes) != string(probe.wantBytes) {
		t.Fatalf("夹具前提不成立：%s 在钩子里改写完立刻读回长度 %d，期望长度 %d",
			dup, len(probe.snapBytes), len(probe.wantBytes))
	}
}

// assertM91Blocked 是三条破坏性腿共用的判据。
func assertM91Blocked(t *testing.T, dup string, probe *recheckProbe, res model.OpsResult) {
	t.Helper()
	if len(res.OK) != 0 {
		t.Fatalf("被就地改写的 dup 仍然被处理了（OK=%v）：M91 的守卫没生效，那份新内容已经销毁", res.OK)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("内容级拦截不得记成 Skipped（那不是「目标已达成」）: %+v", res.Skipped)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %+v，want 恰好 1 条", res.Failed)
	}
	fi := res.Failed[0]
	if fi.Path != dup {
		t.Fatalf("失败项路径不对（%q，期望 %q）", fi.Path, dup)
	}
	if fi.Stage != "verify" {
		t.Fatalf("Stage = %q，期望 verify：内容级复核与校验循环同属校验阶段，"+
			"记成 ops 会让上层以为「动过盘、要按失败回滚」（M52/M54 的同一族口径）", fi.Stage)
	}
	// ★ 「动作前复核」这四个字是这一格的身份证。校验循环（:266）那句是
	// 「校验失败：文件在扫描后被修改」，两者处置相同、来源完全不同：如果这里的
	// 文案与循环那侧混同，本次红就可能只是「改写发生在 Execute 之前」，
	// 与要钉的那道窗口无关。
	if !strings.Contains(fi.Err, "动作前复核") {
		t.Fatalf("错误未标明来自「动作前复核」（无法与扫描期校验循环区分）: %q", fi.Err)
	}
	if !strings.Contains(fi.Err, "被修改") {
		t.Fatalf("内容确实变了，这一格可以说「被修改」: %q", fi.Err)
	}
	if !strings.Contains(fi.Err, "盘上未做任何改动") {
		t.Fatalf("错误未声明盘上未动手（用户会以为改了一半）: %q", fi.Err)
	}
	// 账目必须全零：拦下来的项不许进任何一栏。
	if res.Reclaimed != 0 || res.LinkedBytes != 0 || res.SymlinkedBytes != 0 || res.TrashedBytes != 0 {
		t.Fatalf("拦截项仍进了账（reclaimed=%d linked=%d symlinked=%d trashed=%d）",
			res.Reclaimed, res.LinkedBytes, res.SymlinkedBytes, res.TrashedBytes)
	}
	// ★ M91 的判据本体（修前红在这里）：那份新写入的字节必须还在盘上。
	if got, err := os.ReadFile(dup); err != nil || string(got) != string(probe.wantBytes) {
		t.Fatalf("dup 的新字节没保住（读回长度 %d err=%v，期望长度 %d）：数据丢失",
			len(got), err, len(probe.wantBytes))
	}
	assertNoResidue(t, dup)
}

// ---- 三条破坏性腿：delete / hardlink / symlink ----

func TestM91DeleteBlocksInPlaceRewrite(t *testing.T) {
	fx := newFixture(t)
	probe := installAtContentRecheck(t, fx.dup1.Path, 1, rewriteInPlace)

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	assertM91Premise(t, fx.dup1.Path, probe)
	assertM91Blocked(t, fx.dup1.Path, probe, res)
}

func TestM91HardlinkBlocksInPlaceRewrite(t *testing.T) {
	fx := newFixture(t)
	probe := installAtContentRecheck(t, fx.dup1.Path, 1, rewriteInPlace)

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup1.ID}})

	assertM91Premise(t, fx.dup1.Path, probe)
	assertM91Blocked(t, fx.dup1.Path, probe, res)

	// 合并必须**没有**发生：dup 位置上还是那份独立的新内容，没被 keep 覆盖成共享块。
	ki, err1 := os.Stat(fx.orig.Path)
	di, err2 := os.Stat(fx.dup1.Path)
	if err1 != nil || err2 != nil {
		t.Fatalf("两个路径都得还在（keep err=%v dup err=%v）", err1, err2)
	}
	if os.SameFile(ki, di) {
		t.Fatal("dup 已被硬链接到 keep：拦截却仍然覆盖了 dup 位置上的新内容")
	}
	// keep 一字未动
	if b, err := os.ReadFile(fx.orig.Path); err != nil || string(b) == string(m91ForeignMarker) {
		t.Fatalf("保留源被改动或读数异常: %v", err)
	}
}

func TestM91SymlinkBlocksInPlaceRewrite(t *testing.T) {
	fx := newFixture(t)
	requireSymlinkSupport(t, fx.dir)
	probe := installAtContentRecheck(t, fx.dup1.Path, 1, rewriteInPlace)

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "symlink", FileIDs: []uint64{fx.dup1.ID}})

	assertM91Premise(t, fx.dup1.Path, probe)
	assertM91Blocked(t, fx.dup1.Path, probe, res)

	li, err := os.Lstat(fx.dup1.Path)
	if err != nil {
		t.Fatalf("dup 路径消失了: %v", err)
	}
	if li.Mode()&os.ModeSymlink != 0 {
		t.Fatal("dup 被换成了符号链接：那份新内容已经没了（守卫未生效）")
	}
}

// TestM91DeleteBlocksSizeChangingRewrite 变长那一格（追加/截断后长度不同）。
//
// 为什么单独立一条，而不是觉得"等长那一格已经够了"：这条是给**变异甄别**用的。
// M25-f（判据退成恒放行）在这里红，M25-e（退成只比 size）在这里绿——两条腿的
// 指纹由此分开；否则两个变异红在同一格同一句上，读起来像只做了一个变异。
// 现实形状也不是编的：编辑器追加一段、`dd` 尾部补数据，都会让 size 变。
func TestM91DeleteBlocksSizeChangingRewrite(t *testing.T) {
	fx := newFixture(t)
	short := []byte(strings.Repeat("M91-SHORT", 300)[:2048])
	probe := installAtContentRecheck(t, fx.dup1.Path, 1, rewriteTo(short))

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	assertM91Premise(t, fx.dup1.Path, probe)
	if len(probe.snapBytes) == len(short) && len(short) == int(fx.dup1.Size) {
		t.Fatalf("夹具前提不成立：变长用例改出来的长度与记录相同（%d），本条没有新增任何一格", len(short))
	}
	assertM91Blocked(t, fx.dup1.Path, probe, res)
}

// ---- §28.4 设计的另外两格处置：已消失 / 无从判定 ----

// removeAtRecheck 让 dup 在"身份复核已过、内容复核未发"这一刻消失（用户或另一个程序
// 把它删了）。返回 nil = 本夹具不预期之后还能按内容读到它。
func removeAtRecheck(p string) ([]byte, error) { return nil, os.Remove(p) }

// TestM91CountsVanishedDupAsSkipped 已消失记 Skipped，**不记** Failed。
//
// 为什么单独钉一格：这一格与"内容被改过"的处置方向完全不同——用户自己把那份 dup 删了，
// 目标其实**已达成**，与校验循环的 VerdictSkipped、delete 分支撞 ENOENT 是同一个形状。
// 记成 Failed 就不只是文案难看：Skipped 与 OK 同路进 app.go 的 gone 集合去清结果集，
// 记 Failed 会在结果集里留一个盘上已经没有的路径（M54/OPS-13 的原始论证）。
func TestM91CountsVanishedDupAsSkipped(t *testing.T) {
	fx := newFixture(t)
	installAtContentRecheck(t, fx.dup1.Path, 1, removeAtRecheck)

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	if _, err := os.Lstat(fx.dup1.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("夹具前提不成立：dup 还在（err=%v），本用例没验到「已消失」那一格", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != fx.dup1.Path {
		t.Fatalf("动作前发现 dup 已消失应记 Skipped（目标已达成）: skipped=%+v", res.Skipped)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("已消失不得记 Failed（会在结果集里留一个盘上不存在的路径）: %+v", res.Failed)
	}
	if len(res.OK) != 0 || res.Reclaimed != 0 {
		t.Fatalf("Skipped 不进任何一栏（S7）: ok=%+v reclaimed=%d", res.OK, res.Reclaimed)
	}
}

// TestM91UnreadableDupIsUnverifiableNotModified 读不动不是"被改过"。
//
// 与 M52 同一族口径，但落在**动作前**这一格：EACCES 没有任何内容级证据，
// 说"被修改"是把我们的无能为力写成用户的行为，处置建议也跟着错（修权限 vs 重扫）。
func TestM91UnreadableDupIsUnverifiableNotModified(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户无权限拒绝语义")
	}
	fx := newFixture(t)
	chmodZero := func(p string) ([]byte, error) { return nil, os.Chmod(p, 0) }
	installAtContentRecheck(t, fx.dup1.Path, 1, chmodZero)

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	// ★ 前置自检（§21.3 W3 同一把尺子）：chmod 0 造"读不动"只在认权限位的卷上成立。
	// Windows 的 chmod 只翻只读位、不拒绝读，那一腿这一格造不出来 ⇒ 明写未验证，
	// 不许拿 darwin 的绿冒充（AS-K2：Skip 不是通过）。
	if f, err := os.Open(fx.dup1.Path); err == nil {
		_ = f.Close()
		t.Skipf("本平台 chmod 不拒绝读：「读不动」这一前提造不出来，M91 的无从判定格在本卷未验证")
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %+v，want 恰好 1 条", res.Failed)
	}
	fi := res.Failed[0]
	if !strings.Contains(fi.Err, "无从判定") {
		t.Fatalf("读不动被说成别的（用户会去重扫一个根本没改过的文件）: %q", fi.Err)
	}
	if !strings.Contains(fi.Err, "动作前复核") {
		t.Fatalf("错误未标明来自「动作前复核」（无法与扫描期校验循环区分）: %q", fi.Err)
	}
	if strings.Contains(fi.Err, "被修改") {
		t.Fatalf("EACCES 被报成「文件在扫描后被修改」（M52 的老坑，只是换了位置）: %q", fi.Err)
	}
	if fi.Stage != "verify" || len(res.OK) != 0 || res.Reclaimed != 0 {
		t.Fatalf("处置形状不对: stage=%q ok=%+v reclaimed=%d", fi.Stage, res.OK, res.Reclaimed)
	}
	// 权限还回去再验内容：拦下必须**没动过盘**，那 4096 字节还得原样在。
	if err := os.Chmod(fx.dup1.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(fx.dup1.Path); err != nil || len(b) != 4096 {
		t.Fatalf("拦下后 dup 应当原样在盘上（len=%d err=%v）", len(b), err)
	}
}

// ---- 负控制：move / trash 不接这道复核（§28.4 不扩大改动面） ----

// 这两条腿把文件**整体搬走**，内容不丢，扫到的哈希对不上也不会销毁任何东西；
// 给它们加复核是拿两倍读盘换零收益。钩子期望次数写 0：一旦有人「顺手补一致」，
// 本用例红在 fired 上，而不是红在某条断言上——一眼就能看出是多接了一道复核。
func TestM91DoesNotWrapMove(t *testing.T) {
	fx := newFixture(t)
	probe := installAtContentRecheck(t, fx.dup1.Path, 0, rewriteInPlace)
	target := filepath.Join(t.TempDir(), "moved")

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	if probe.fired != 0 {
		t.Fatalf("move 这条腿接了 M91 的内容复核（跑 %d 次）：§28.4 裁定不做", probe.fired)
	}
	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("move 应照常成功: %+v", res)
	}
	if _, err := os.Stat(fx.dup1.Path); !os.IsNotExist(err) {
		t.Fatalf("源文件没搬走: %v", err)
	}
	// 搬走的那份内容完好——这正是「move 不需要 M91」的实证。
	if b, err := os.ReadFile(filepath.Join(target, "dup1.bin")); err != nil || len(b) != 4096 ||
		strings.Contains(string(b), "M91-FOREIGN") {
		t.Fatalf("移动后的副本内容异常（len=%d err=%v）", len(b), err)
	}
}

func TestM91DoesNotWrapTrash(t *testing.T) {
	fx := newFixture(t)
	probe := installAtContentRecheck(t, fx.dup1.Path, 0, rewriteInPlace)
	trashDir := filepath.Join(t.TempDir(), "trash")

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		TrashFn: mockTrash(trashDir, false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})

	if probe.fired != 0 {
		t.Fatalf("trash 这条腿接了 M91 的内容复核（跑 %d 次）：§28.4 裁定不做", probe.fired)
	}
	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("trash 应照常成功: %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(trashDir, "dup1.bin")); err != nil || len(b) != 4096 ||
		strings.Contains(string(b), "M91-FOREIGN") {
		t.Fatalf("回收站里的副本内容异常（len=%d err=%v）", len(b), err)
	}
}

// ---- 防误伤钉子：mtime 变、内容没变，必须放行 ----

func TestM91LetsTouchedButUnchangedDupThrough(t *testing.T) {
	fx := newFixture(t)
	probe := installAtContentRecheck(t, fx.dup1.Path, 1, touchOnly)

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	if !probe.postMod.After(probe.prevMod) {
		t.Fatalf("夹具前提不成立：mtime 没被推进（prev=%v post=%v），本用例没验到误伤那一格",
			probe.prevMod, probe.postMod)
	}
	if len(probe.wantBytes) != 4096 || string(probe.snapBytes) != string(probe.wantBytes) {
		t.Fatalf("夹具前提不成立：touch 之外还动了内容（snap=%d want=%d）",
			len(probe.snapBytes), len(probe.wantBytes))
	}
	if len(res.OK) != 1 || res.OK[0] != fx.dup1.Path {
		t.Fatalf("只 touch 过的 dup 被拦下了（Failed=%+v）：判据把元数据当成了内容", res.Failed)
	}
	if _, err := os.Stat(fx.dup1.Path); !os.IsNotExist(err) {
		t.Fatal("放行后没有真的删除")
	}
	if res.Reclaimed != fx.dup1.Size {
		t.Fatalf("放行项应计入 Reclaimed=%d，实得 %d", fx.dup1.Size, res.Reclaimed)
	}
}

// ---- 三腿同判据：不许只接一条，也不许多接 ----

// TestM91DestructiveLegWiringCount 用一条静态计数钉住接线面：将来新增会销毁
// dup 内容的腿时，必须在这里登记并附一条同判据的用例，否则本用例红。
// 为什么要有它：guardContent 只在三处接线，逐条用例各测自己那一条腿，
// **没有任何一处**能读出「是不是漏了新腿 / 是不是多接了旧腿」。
func TestM91DestructiveLegWiringCount(t *testing.T) {
	src, err := os.ReadFile("executor.go")
	if err != nil {
		t.Fatal(err)
	}
	const want = "if guardContent(i, e) {"
	if n := strings.Count(string(src), want); n != 3 {
		t.Fatalf("guardContent 接线处 = %d，期望 3（delete/hardlink/symlink）。\n"+
			"多于 3：新腿需要复核 ⇒ 请同时补用例并把本判据改掉；\n"+
			"少于 3：某条腿的复核掉了 ⇒ M91 的防线破口（§28.4）", n)
	}
}

// ---- §28.4 承诺的代价读数 ----

// BenchmarkM91ExtraContentRecheck 把「M91 多花多少」量成可复跑的数，而不是注释里的
// 一句"约两倍"。两个 arm 就是这道复核的全部新增工作：同一个 dup 过一遍 VerifyFile
// （扫描期校验循环那一次）与过两遍（加上动作前那一次）。
//
// ★ 为什么 bench 里不直接跑 Execute：delete 会把文件删掉，每次迭代都得重建 32 MiB
// 数据 ⇒ 写盘成本盖过哈希成本，两臂之差埋在噪声里。这里量的正是那道复核新增的
// 唯一一件事（多读多算一遍），口径干净。
//
// 读法：MB/s 那一栏直接换算成"清理 N GiB 多花多少秒"。冷/热页缓存会影响绝对值，
// 两臂在同一进程里相邻跑、读的是同一份文件，比值比绝对值可靠。
func BenchmarkM91ExtraContentRecheck(b *testing.B) {
	const size = 32 << 20 // 32 MiB：单文件够大，open/fstat 不在计时里占可见比重
	dir := b.TempDir()
	p := filepath.Join(dir, "dup.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i * 7)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		b.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		b.Fatal(err)
	}
	pool := hasher.NewPool()
	f, err := os.Open(p)
	if err != nil {
		b.Fatal(err)
	}
	buf := pool.GetStreamBuf()
	hash, err := hasher.HashFull(f, st.Size(), buf)
	pool.PutStreamBuf(buf)
	_ = f.Close()
	if err != nil {
		b.Fatal(err)
	}
	e := &model.FileEntry{ID: 1, Path: p, Size: uint64(st.Size()),
		ModTime: st.ModTime().UnixNano(), Ext: ".bin"}

	one := func(b *testing.B, times int) {
		b.ReportAllocs()
		b.SetBytes(int64(size) * int64(times))
		for i := 0; i < b.N; i++ {
			for k := 0; k < times; k++ {
				if v, _ := VerifyFile(e, hash, pool); v != VerdictPass {
					b.Fatalf("前置不成立：未改动的文件校验返回 %d，计时测的就不是复核成本了", v)
				}
			}
		}
	}
	b.Run("VerifyOnce_修前", func(b *testing.B) { one(b, 1) })
	b.Run("VerifyTwice_M91", func(b *testing.B) { one(b, 2) })
}
