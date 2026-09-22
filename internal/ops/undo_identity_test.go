package ops

// ============================================================================
// R2-1（第四轮全仓审查，设计段 §30.3）：**回撤的同卷腿**在「内容校验已过」与
// 「改名放回原位」之间没有身份留底，于是回家的可能根本不是我们校验过的那一份。
//
// 改前时序（`undo.go`）：
//
//	undoTrash/undoMove → undoSourceCheck：Lstat → size → 按路径全量 BLAKE3 ✅
//	  → restoreInPlace：MkdirAll → claimExact/claimDst（原位抢 0 字节占位）
//	  → renameFile(it.DestPath, target)      ← 同卷常态分支：中间**零**身份复核
//	  → applyMtime → 上层记「回撤成功」
//
// 跨卷回退分支反倒握着一条完整链（`pathIdentity` → `copyVerifyFile` → `identityStill`
// → 才 `removeSrc`），也就是 AS-H4 那句「复核对象必须与动手对象是同一个」只装在了
// **异常分支**上。M4 修的是判据（size → 内容哈希），没管哈希之后这一段；M48/M56 管的是
// OrigPath 侧的抢占与落点，也不是 DestPath 侧的身份。
//
// ★ 为什么内容哈希拦不住这一格：`hashFile` 按路径 open 一份、哈希完就 close，随后
// `renameFile` 动的是**那一刻路径上的东西**。顶替发生在两者之间时，我们搬回家的是
// 顶替者，而真件在回收站/移动目录侧去向不明。等长**同内容**的顶替（同一份数据的另一个
// 副本被改名进来）连「内容不对」都不会暴露——只有身份看得见。所以本文件有一条专门钉它。
//
// 取红方式：接缝 `undoSourceCheckFn` 是本批新增的，但它只是**同义转接**（生产恒等于
// 原函数），所以修前红是**真红**——接缝存在而守卫不存在，正是改前的形状。这与 M91 那批
// 第一次试错（钩子本身即修法的一部分 ⇒ 红落在夹具前提上）不同。
// ============================================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filededup/internal/fsid"
	"filededup/internal/worktemp"
)

// undoWindowProbe 记录「校验已过」这一刻之后我们对 DestPath 做了什么。
type undoWindowProbe struct {
	fired    int
	post     fsid.ID // 动手**之后** DestPath 上的身份（gone 那一格为零值）
	postSize int64
}

// actAfterUndoCheck 在 undoSourceCheck **成功返回之后**对 DestPath 动手，
// 也就是精确站在「校验已过、改名未发」这段窗口里。fn 返回空 ID = 这一刻之后
// 该路径按设计已经不存在（gone 那一格）。
func actAfterUndoCheck(t *testing.T, dest string, fn func(string) (fsid.ID, error)) *undoWindowProbe {
	t.Helper()
	p := &undoWindowProbe{}
	orig := undoSourceCheckFn
	undoSourceCheckFn = func(it UndoItem, where string) (os.FileInfo, fsid.ID, error) {
		st, verID, err := orig(it, where)
		// 钩子跑在调用方 goroutine 里：只许 t.Errorf，t.Fatal 会带走别的用例。
		if it.DestPath != dest || err != nil {
			return st, verID, err
		}
		p.fired++
		id, ferr := fn(dest)
		if ferr != nil {
			t.Errorf("窗口里对 %s 动手失败: %v", dest, ferr)
			return st, verID, err
		}
		p.post = id
		if st2, serr := os.Lstat(dest); serr == nil {
			p.postSize = st2.Size()
		}
		// verID 必须**原样交回**：它就是"我刚哈希的那一份"的身份，吞掉它等于把
		// 守卫的参照物换成未解析（identityCheck 的 vSame 放行分支）——守卫会静默空转。
		return st, verID, err
	}
	t.Cleanup(func() {
		undoSourceCheckFn = orig
		if p.fired != 1 {
			t.Errorf("前置条件未成立：%s 上「校验已过」出现了 %d 次，期望 1 次"+
				"（0 次 = 本条什么都没验证；>1 = 接缝被挂进了别的腿）", dest, p.fired)
		}
	})
	return p
}

// undoItemFixture 铺一条内容自洽的回撤记录：DestPath 上有那份原件，
// 账本里的 Hash/Size 就是它的。OrigPath 是否存在由调用方决定。
func undoItemFixture(t *testing.T, kind, subdir string, content []byte) (UndoItem, string) {
	t.Helper()
	dest, orig := undoDestFixture(t, subdir, string(content))
	return UndoItem{Kind: kind, OrigPath: orig, DestPath: dest,
		Hash: hashBytesT(t, content), Size: uint64(len(content)), MtimeNs: pastNs()}, dest
}

// assertNothingAt 钉「拦截不许在盘上留下任何东西」：这些路径必须一个都不存在。
// 抢下的 0 字节占位（claimExact / claimDst 两支都要 release）与"顶替者被搬进来"
// 在这一格同形，所以两条支分别由两条用例覆盖（原位空闲 / 原位被占）。
func assertNothingAt(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("%s 上出现了东西（claim 的 0 字节占位没 release，或顶替者被搬了进来）: err=%v", p, err)
		}
	}
}

// assertUndoBlocked 用于「原位本就空闲」那一支：拦下之后原位依旧不存在任何东西。
func assertUndoBlocked(t *testing.T, it UndoItem, extra ...string) {
	t.Helper()
	assertNothingAt(t, append([]string{it.OrigPath}, extra...)...)
}

// ---- 主格：校验已过、改名未发，DestPath 被 rename 顶替 ----

func TestUndoTrashBlocksIntruderAfterContentCheck(t *testing.T) {
	const original = "ORIGINAL-TRASH-PAYLOAD-AAAAAAAAA"
	it, dest := undoItemFixture(t, "trash", ".Trash", []byte(original))
	before := needResolvedID(t, dest)
	probe := actAfterUndoCheck(t, dest, func(p string) (fsid.ID, error) {
		return swapOutAt(t, p, []byte(strings.Repeat("R21-", 400)[:len(original)])), nil
	})

	target, err := UndoOne(it)
	if err == nil {
		t.Fatalf("顶替者被放回原位并记成回撤成功（target=%q）：同卷腿没有身份留底（R2-1）", target)
	}
	if !strings.Contains(err.Error(), "被替换") {
		t.Fatalf("报错未说明是「被替换」（用户会去回收站找一个根本不在这儿的东西）: %v", err)
	}
	if !strings.Contains(err.Error(), "inode") {
		t.Fatalf("报错未给出身份这一维的依据: %v", err)
	}
	assertUndoBlocked(t, it)
	// 拦下不许动第三方那份：DestPath 上仍是顶替内容。
	if b, rerr := os.ReadFile(dest); rerr != nil || !strings.Contains(string(b), "R21-") {
		t.Fatalf("回撤侧读数不是顶替者（len=%d err=%v）", len(b), rerr)
	}
	if probe.post.SameIdentity(before) {
		t.Errorf("夹具前提不成立：顶替者拿到了同一个 inode，本条没验到身份那一格")
	}
}

func TestUndoMoveBlocksIntruderAfterContentCheck(t *testing.T) {
	// ★ 这一条刻意走**另一支**：OrigPath 已被占 ⇒ restoreInPlace 落 name.fdd-restored.ext。
	// 那一支的占位同样要在拦截时 release；两支都要有钉子（只有确切名字那一支被测到 = 半格）。
	const original = "ORIGINAL-MOVE-PAYLOAD-BBBBBBBBBBBB"
	it, dest := undoItemFixture(t, "move", "movedir", []byte(original))
	if err := os.MkdirAll(filepath.Dir(it.OrigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := []byte("third-party occupies the original slot")
	if err := os.WriteFile(it.OrigPath, blocker, 0o644); err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(it.OrigPath)
	alt := filepath.Join(filepath.Dir(it.OrigPath),
		strings.TrimSuffix(base, filepath.Ext(base))+worktemp.MarkRestored+filepath.Ext(base))
	probe := actAfterUndoCheck(t, dest, func(p string) (fsid.ID, error) {
		return swapOutAt(t, p, []byte(strings.Repeat("R21M", 400)[:len(original)])), nil
	})

	target, err := UndoOne(it)
	if err == nil {
		t.Fatalf("move 腿同样把顶替者搬回了家（target=%q）", target)
	}
	if !strings.Contains(err.Error(), "被替换") {
		t.Fatalf("move 腿报错未说明是被替换: %v", err)
	}
	// 这一支 OrigPath 上是**第三方的那份**（本用例刻意占住的），所以判据不是"原位不存在"，
	// 而是"原位还是那份"+另名落点没被写进去。
	assertNothingAt(t, alt)
	if b, rerr := os.ReadFile(it.OrigPath); rerr != nil || string(b) != string(blocker) {
		t.Fatalf("原位上的第三方文件被改动（len=%d err=%v）", len(b), rerr)
	}
	if probe.post.Resolved && probe.postSize != int64(len(original)) {
		t.Errorf("夹具读数异常：顶替者长度 %d，期望 %d", probe.postSize, len(original))
	}
}

// TestUndoBlocksSameContentIntruder 是本文件最硬的一格：**内容逐字节相同**的另一份
// 文件被改名顶进 DestPath。size 相同、BLAKE3 相同 ⇒ M4 那道内容校验恒放行，
// 只有 (dev,ino) 看得见「回家的不是我校验的那一份」。
//
// 为什么值得单立：它证明本批补的不是「哈希的重复品」。若把判据退化成「回家前再哈希一次」，
// 这一格照样绿，而第三方的副本仍然被搬进了用户原位。
func TestUndoBlocksSameContentIntruder(t *testing.T) {
	const original = "SAME-BYTES-DIFFERENT-INODE-CCCCCCCCC"
	it, dest := undoItemFixture(t, "trash", ".Trash", []byte(original))
	before := needResolvedID(t, dest)
	probe := actAfterUndoCheck(t, dest, func(p string) (fsid.ID, error) {
		id := swapOutAt(t, p, []byte(original))
		if id.SameIdentity(before) {
			t.Errorf("夹具前提不成立：顶替者拿到了同一个 inode，本条没验到身份那一格")
		}
		return id, nil
	})

	target, err := UndoOne(it)
	if err == nil {
		t.Fatalf("同内容顶替者被放回原位并记成成功（target=%q）：判据退化成了内容比对", target)
	}
	if !strings.Contains(err.Error(), "被替换") {
		t.Fatalf("报错未说明是被替换: %v", err)
	}
	assertUndoBlocked(t, it)
	// 内容一模一样，所以「盘上还有没有那份数据」这个判据失效——只能看身份：
	// DestPath 必须还是顶替者那个号（没被我们改名搬走）。
	if now, lerr := fsid.FromPathNoFollow(dest); lerr != nil || !now.SameIdentity(probe.post) {
		t.Fatalf("DestPath 上的对象被搬走了（now=%+v probe=%+v err=%v）", now, probe.post, lerr)
	}
}

// ---- 分格钉子：消失不是「被替换」（M54 同族，只是换了位置） ----

func TestUndoVanishedDestIsNotReportedAsReplaced(t *testing.T) {
	const original = "VANISHING-TRASH-PAYLOAD-DDDDDDDDDD"
	it, dest := undoItemFixture(t, "trash", ".Trash", []byte(original))
	actAfterUndoCheck(t, dest, func(p string) (fsid.ID, error) { return fsid.ID{}, os.Remove(p) })

	target, err := UndoOne(it)
	if err == nil {
		t.Fatalf("DestPath 已经没了却返回成功（target=%q）", target)
	}
	if strings.Contains(err.Error(), "被替换") || strings.Contains(err.Error(), "inode") {
		t.Fatalf("「已消失」被说成「被替换（inode 已变化）」——那是宣称看到过一个不存在的新对象: %v", err)
	}
	if !strings.Contains(err.Error(), "消失") {
		t.Fatalf("报错未说明是「消失」（用户会去找一个改名进来的第三方文件）: %v", err)
	}
	// ★ 回撤场景的「消失」**不记成已达成**：与 delete 腿的 Skipped 是两个口径——
	// 那份数据没回家，用户必须知道，所以这里要的是非 nil 错误（上面已断）+ 原位干净。
	assertUndoBlocked(t, it)
}

// ---- 防误伤：mtime 变了、对象没变，必须照常回家 ----

// 与 M91 的 touch 钉子同形。判据一旦退化成「元数据未变才放行」（或有人去比 MtimeNs），
// 本条当场红——回撤**本来就要**把 mtime 拨回记录值，动 mtime 是常态。
func TestUndoTouchedDestStillRestores(t *testing.T) {
	const original = "TOUCHED-BUT-SAME-INODE-EEEEEEEEEEE"
	it, dest := undoItemFixture(t, "trash", ".Trash", []byte(original))
	before := needResolvedID(t, dest)
	probe := actAfterUndoCheck(t, dest, func(p string) (fsid.ID, error) {
		fut := time.Now().Add(time.Hour)
		if err := os.Chtimes(p, fut, fut); err != nil {
			return fsid.ID{}, err
		}
		return needResolvedID(t, p), nil
	})

	target, err := UndoOne(it)
	if err != nil {
		t.Fatalf("只推进了 mtime 的回撤源被拦下: %v", err)
	}
	if target != it.OrigPath {
		t.Fatalf("落点 = %q，期望回到确切原路径 %q", target, it.OrigPath)
	}
	if !probe.post.SameIdentity(before) {
		t.Errorf("夹具前提不成立：touch 换掉了对象（before=%+v after=%+v），本条没验到误伤那一格",
			before, probe.post)
	}
	b, rerr := os.ReadFile(target)
	if rerr != nil || string(b) != original {
		t.Fatalf("回家的内容不对（len=%d err=%v）", len(b), rerr)
	}
	if _, lerr := os.Lstat(dest); !os.IsNotExist(lerr) {
		t.Fatalf("回撤侧没清掉，盘上两份: %v", lerr)
	}
}

// ---- 覆盖面：接线形状（守卫只在同卷那条改名之前，不许多接） ----

// TestUndoSameVolumeGuardWiringCount 与 M91 的 TestM91DestructiveLegWiringCount 同族：
// 逐条用例各测自己那一格，**没有任何一处**能读出「是不是漏了新腿 / 是不是多接了一道」。
// 期望 1：restoreInPlace 里紧贴 renameFile 的那一道。跨卷回退分支用的是 AS-H4 那条
// identityStill（参照物取在复制之前，是另一个时刻），不该被折进这一格。
func TestUndoSameVolumeGuardWiringCount(t *testing.T) {
	src, err := os.ReadFile("undo.go")
	if err != nil {
		t.Fatal(err)
	}
	const want = "identityCheck(it.DestPath, verID)"
	if n := strings.Count(string(src), want); n != 1 {
		t.Fatalf("同卷腿的身份复核接线处 = %d，期望 1（restoreInPlace 紧贴 renameFile）。\n"+
			"多于 1：新腿需要复核 ⇒ 同时补用例并改掉本判据；\n"+
			"0：这道复核掉了 ⇒ R2-1 的防线破口（设计段 §30.3）", n)
	}
}
