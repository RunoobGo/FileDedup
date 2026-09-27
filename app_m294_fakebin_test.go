package main

// M294：根包测试**不得**往本机真实回收站落条目。
//
// 登记的原话（docs/04 M294 行）是形状而不是猜测：在回收站可用的开发机上，
// `go test ./...` 每轮留 ≈25 条**永久**条目，一次 `run-gates.sh` 全量（7 轮包级跑）
// 留 ≈350 条，`t.TempDir()` 的 `os.RemoveAll` 删不掉 `$I`/`$R` 配对。两层危害里更值钱
// 的那层是取证侧：`verifyRecycled` 的判据是"该卷回收站条目数增量 ≥ 预期入站数"，
// 历次真机批次的收尾对账也以回收站基线为分母 ⇒ 跑测试本身把分母单调抬高，
// 恰好与 M265 那一族"静默永久删除"所需的**负向**敏感度相反。
//
// 本文件的三条判据各自钉住修复的一个半边：
//   (1) 默认走假回收站（这一条就是修 M294 的那一条）；
//   (2) 显式选真回收站的用例结束后必须**交回**假的（否则一个用例泄漏到整包）；
//   (3) 真回收站这条腿仍有一格在跑（假回收站不能顺手把平台实现的全部覆盖删掉）。
//
// ★ 沙箱而不是清账：取向②（TestMain 收尾按 `$I` 定向删）仍然要在用户机器上真的
//   建条目再删，且删错了就是用户的条目；取向①（注入假回收站）让条目根本不产生。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"filededup/internal/model"
	"filededup/internal/ops"
)

// realPairApp 备好"磁盘上真有一组重复文件"的 App，返回该组里**唯一那个非保留项**的路径。
//
// 为什么不用 procFixture：那个夹具刻意摆四个文件（要能独立控制"谁在优先目录内"和
// "谁是冗余项"）。显式走真回收站的那一格每轮只该付一条回收站条目，用最小夹具。
func realPairApp(t *testing.T) (a *App, rec *eventRecorder, dupPath string, dupID uint64) {
	t.Helper()
	root := t.TempDir()
	payload := []byte("REAL-TRASH-LEG-PAYLOAD-0123456789")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dupPath = filepath.Join(root, "sub", "b.bin")
	if err := os.WriteFile(dupPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	a, rec = newHistApp(t)
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "scan"); ev != "scan:done" {
		t.Fatalf("scan 终止事件 = %s", ev)
	}
	r, err := a.GetResultGroups(ResultQuery{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range r.Groups {
		for _, f := range g.Files {
			if !f.IsKeep {
				return a, rec, f.Path, f.ID
			}
		}
	}
	t.Fatal("夹具里没有非保留项（重复组未成立）")
	return nil, nil, "", 0
}

// trashOne 用应用绑定执行一次「移入回收站」并等它收尾。
func trashOne(t *testing.T, a *App, rec *eventRecorder, id uint64) {
	t.Helper()
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{id}}); err != nil {
		t.Fatalf("trash 请求被拒: %v", err)
	}
	waitOpsDone(t, rec)
}

// (1) 应用层的回收站请求必须由测试自造的假回收站承接。
//
// 断言的是**落点**，不是"我们换了个函数指针"：文件必须出现在假回收站目录里、
// 且原路径确实没了。改前必红——那时落点是 `C:\$Recycle.Bin\…`（Windows）或
// `~/.local/share/Trash`（linux），假回收站目录里一个文件都不会有。
func TestExecuteOperationTrashIsServedByFakeBin(t *testing.T) {
	a, rec, _, insideDir, _ := procFixture(t)
	ids := idsInDir(t, a, insideDir)
	if len(ids.ids) == 0 {
		t.Fatalf("夹具前提不成立：%s 下没有非保留项", insideDir)
	}
	if _, err := a.ExecuteOperation(model.OpRequest{
		Kind: "trash", FileIDs: ids.ids,
	}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	for _, p := range ids.paths {
		dst := fakeTrashedDest(p)
		if dst == "" {
			t.Errorf("%s 没有落进测试假回收站 ⇒ 这一轮的应用层回收站请求走的是本机真实回收站"+
				"（M294 的污染形状：条目永久留在用户机器上，t.TempDir() 的 RemoveAll 删不掉）", p)
			continue
		}
		if !dirContains(fakeTrashDir, dst) {
			t.Errorf("假回收站的落点 %s 不在沙箱根 %s 之下", dst, fakeTrashDir)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("假回收站里读不到该文件 %s: %v", dst, err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("源 %s 仍在原处（回收站请求没做成）", p)
		}
	}
}

// (2) 显式选真回收站的用例结束后，必须把假回收站装回来。
//
// 为什么单独钉这一条：`requireRealTrash` 换的是**包级变量**。只换不回，等于把污染
// 从"默认行为"改成"谁先跑到谁倒霉"——而 Go 按源文件名字序跑，`app_m294_*` 之后的
// 每个文件都会跟着往真回收站落条目，且**换在哪个用例上完全看不出症状**。
func TestRequireRealTrashHandsBackTheFakeBin(t *testing.T) {
	before := fakeCallCount()

	t.Run("显式选真回收站期间假回收站不收请求", func(t *testing.T) {
		requireRealTrash(t)
		if n := fakeCallCount() - before; n != 0 {
			t.Fatalf("requireRealTrash 之后假回收站仍被调用 %d 次 ⇒ 它没有把平台实现装回去", n)
		}
	})

	t.Run("子用例结束后交回假回收站", func(t *testing.T) {
		a, rec, dupPath, dupID := realPairApp(t)
		trashOne(t, a, rec, dupID)
		if fakeCallCount() == before {
			t.Fatalf("假回收站一次都没被调用 ⇒ 上一个用例把平台实现留在了原地（M294 的污染会从这里回来）")
		}
		if dst := fakeTrashedDest(dupPath); dst == "" {
			t.Fatalf("%s 没落进假回收站，落点 = %q", dupPath, dst)
		} else if !dirContains(fakeTrashDir, dst) {
			t.Fatalf("落点 %s 不在沙箱根 %s 之下", dst, fakeTrashDir)
		}
	})
}

// (3) 真回收站这条腿必须还有一格在跑。
//
// 全仓唯一真正执行到平台 `defaultTrash`（Windows 的 SHFileOperationW + `verifyRecycled`、
// darwin 的 osascript、linux 的自研 XDG）的地方，就是 `Kind:"trash"` 的端到端用例——
// `internal/ops` 侧一律注入 `TrashFn`，从不碰平台实现。把根包整体换成假回收站时，
// 如果不同时留下这一格，**"静默永久删除"那条数据安全性会整条失去集成覆盖**，
// 而那正是 M265/H6-b 一族判据要守的东西。
//
// ★ 付费量现读（04 §6.52 五）：**每轮根包跑测 3 条**——本用例 1 条探测 + 上面
// `TestRequireRealTrashHandsBackTheFakeBin` 里 `requireRealTrash` 自带的 1 条探测 +
// 这一格 1 条载荷；一次全量门禁 7 轮（test -v ×1 + race-count2 ×2 + race-count4 ×4）
// = **21 条**（修前同一轮是 350 条）。它是显式选择的结果，不是默认行为——
// 这两件事的区别就是本文件的全部主题。
func TestExecuteOperationTrashThroughRealRecycleBin(t *testing.T) {
	requireRealTrash(t)
	a, rec, dupPath, dupID := realPairApp(t)

	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{dupID}}); err != nil {
		t.Fatal(err)
	}
	waitOpsDone(t, rec)

	if _, err := os.Stat(dupPath); !os.IsNotExist(err) {
		t.Errorf("真回收站应当把文件搬走: %s err=%v", dupPath, err)
	}
	if dst := fakeTrashedDest(dupPath); dst != "" {
		t.Errorf("这一格刻意走平台实现，却落进了假回收站 %s ⇒ requireRealTrash 没换实现", dst)
	}
	t.Logf("本轮真回收站条目增量来自这一格（探测 1 + 载荷 1）；平台 %s", runtime.GOOS)
}

// 静态约束：沙箱靠"换包级变量 + 逐用例换回"实现，这条链在并行用例下不成立。
//
// 与上面三条不同，本条**改前即绿**——它钉的是新夹具依赖的一项既有性质（根包 0 处
// `t.Parallel()`），不是新行为。之所以值得写成测试而不是注释：将来谁给根包加并行用例，
// 症状会是"某几个用例偶发往真回收站落条目"，那比一条红字难查得多。
// parallelNeedle 刻意写成两截拼接：判据那一行本身是**代码行**，跳过注释也躲不过它，
// 整条字面量一出现就会被本条自己扫出来（实测第一版正是这样红的）。
// 这不是取巧，是这类"扫源码的测试"必然要付的形状税。
const parallelNeedle = "t.Parallel" + "()"

func TestRootPackageHasNoParallelTests(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") { // 注释里提到这个调用不算（本文件自己的说明就写着）
				continue
			}
			if strings.Contains(trimmed, parallelNeedle) {
				hits = append(hits, fmt.Sprintf("%s:%d", e.Name(), i+1))
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("根包出现了并行用例 %v：回收站沙箱换的是包级变量 ops.Trash，"+
			"并行下「谁看到哪个实现」取决于调度顺序 ⇒ 污染会偶发回归。"+
			"要真回收站就在用例里显式 requireRealTrash(t)，别把整包改成并行", hits)
	}
}

// ---- 沙箱本体：假回收站 + TestMain 装配 ----

// realTrashFn 在包初始化时捕获平台实现。Go 保证依赖包（internal/ops）先于本包初始化，
// 所以这里拿到的一定是 defaultTrash 本身，不是任何测试换过的值。
var realTrashFn = ops.Trash

var (
	// fakeTrashDir 由 TestMain 建、TestMain 删；收尾用 os.RemoveAll，不经 Shell，
	// 因此这一整个目录不会在回收站里留下任何东西。
	fakeTrashDir string
	fakeMu       sync.Mutex
	fakeLog      = map[string]string{} // 原路径 → 沙箱落点（与返回给执行器的映射分开保存，见下）
	fakeCalls    int
	fakeSeq      int
)

// fakeTrashedDest 返回原路径在假回收站里的落点；没接过这一笔则为 ""。
func fakeTrashedDest(src string) string {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return fakeLog[src]
}

// fakeCallCount 返回假回收站被调用的次数（判据 (2) 用它看"实现有没有交回来"）。
func fakeCallCount() int {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return fakeCalls
}

// fakeTrash 是 ops.Trash 的测试替身：把源文件搬进沙箱目录，落点记进 fakeLog。
//
// ★ 返回给执行器的映射刻意**与本平台真实实现的形状一致**：
//
//	windows → 恒为空 map（SHFileOperationW 没有公开的 src→dst 接口，见 internal/ops/trash.go 顶部），
//	其他平台 → 精确的 src→dst。
//
// 为什么值得为这件事多写两行：假回收站如果替 Windows 凭空造出落点，账本里就会出现
// 生产永远不可能产生的形状，测试于是可以去断言用户拿不到的信息（而 Windows 回收站
// 拿不到映射、只能引导用户自己开回收站，正是 M79/M290 那一族文案判据的前提）。
// 落点仍然记在 fakeLog 里，本文件的判据读它，不污染执行器看到的世界。
func fakeTrash(paths []string) (map[string]string, error) {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	fakeCalls++

	dst := make(map[string]string, len(paths))
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			// 与平台实现同形：源不在就报错，不静默成功。执行器本该已把消失项按 S8
			// 滤掉（见 executor.go 的 guardIdentity），真走到这里说明上游判据变了——
			// 让它在测试里响，而不是被一个恒成功的替身吞掉。
			return nil, fmt.Errorf("假回收站：源不可读 %s（%v）", p, err)
		}
		fakeSeq++
		to := filepath.Join(fakeTrashDir, fmt.Sprintf("%04d-%s", fakeSeq, filepath.Base(p)))
		if err := moveIntoFakeBin(p, to); err != nil {
			return nil, fmt.Errorf("假回收站：搬运失败 %s → %s（%v）", p, to, err)
		}
		dst[p] = to
		fakeLog[p] = to
	}
	if runtime.GOOS == "windows" {
		return map[string]string{}, nil
	}
	return dst, nil
}

// moveIntoFakeBin 先把源"入站"到沙箱：同卷改名，跨卷退化为复制后删源。
//
// 为什么要跨卷那条退路：沙箱根取自 TMPDIR，而用例的文件来自 t.TempDir()；两者在
// 常规机器上同卷，但 CI 与自定义 TMPDIR 下不保证。Rename 跨卷会 EINVAL/EXDEV，
// 那时若不退化成复制，整包测试会因为夹具落点选择而转红——那是环境，不是产品。
func moveIntoFakeBin(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil || closeErr != nil {
		_ = os.Remove(to)
		if cpErr != nil {
			return cpErr
		}
		return closeErr
	}
	return os.Remove(from)
}

// TestMain 把整包的回收站入口换成沙箱，跑完连目录一起删。
//
// 为什么装在 TestMain 而不是逐个用例注入：根包用的是应用绑定 `ExecuteOperation`，
// 它自己组装 ops.Options（不给调用方留 TrashFn），逐用例注入等于把执行器接缝顶到
// 应用层去改产品代码——M294 的两条取向里没有任何一条要求动产品侧。
//
// ★ 这条装配依赖"根包没有并行用例"，那条性质由 TestRootPackageHasNoParallelTests 钉住。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fdd-test-recycle-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法创建测试假回收站，测试中止: %v\n", err)
		os.Exit(1)
	}
	fakeTrashDir = dir
	ops.Trash = fakeTrash

	code := m.Run()

	ops.Trash = realTrashFn
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "测试假回收站 %s 未清空: %v\n"+
			"（这些条目不经 Shell，请手工删该目录；绝不要用 Clear-RecycleBin 之类整箱清空善后）\n", dir, err)
	}
	os.Exit(code)
}
