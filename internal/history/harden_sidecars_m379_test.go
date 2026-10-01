package history

// M379（第九轮批 C / 2026-10-01 裁-1「写入腿各补收紧」）：**账本侧**的写入期伴随文件档位。
//
// 这一侧泄露的东西比缓存那一侧重：`-wal` 里未 checkpoint 的内容是用户机器上的**全部文件路径**
// （含回收站落位路径），0644 就是同机其他账户可读。cache 侧的同名判据见
// `internal/cache/cache_m379_sidecar_test.go`；本文件只补账本这一侧的三格，外加一条
// **接线锚**（写腿清单）——裁-1 说的是"各"，而本包有十一条写腿，漏一条就是半个闸。
//
// ★ Windows 腿不兑现（M354），措辞与 assertDBMode 同源；**不得**写"三平台已验"。

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// m379Mode 读一个文件的档位（不存在时返回 -1 与 err）。
func m379Mode(t *testing.T, path string) (os.FileMode, error) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Mode().Perm(), nil
}

// assertSidecar0600 钉"伴随文件已是仅所有者可读写"，分平台措辞（windows 腿只能读回 0666）。
func assertSidecar0600(t *testing.T, path string) {
	t.Helper()
	perm, err := m379Mode(t, path)
	if err != nil {
		t.Fatalf("伴随文件读不到（%s）：%v", path, err)
	}
	if runtime.GOOS == "windows" {
		if perm != 0o666 {
			t.Fatalf("windows 腿读数必须仍是 0666（M354），实得 %04o（%s）", perm, filepath.Base(path))
		}
		return
	}
	if perm != 0o600 {
		t.Fatalf("%s 档位未收紧：期望 0600，实得 %04o（同机其他账户仍可读）", filepath.Base(path), perm)
	}
}

// captureStderr 收一段代码写到 os.Stderr 的内容（与 cache 侧同形；两包各一份是因为
// 测试夹具不跨包导出）。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stderr = old
	return <-done
}

// TestM379HistoryWriteLegsTightenWal 是 P-55 的账本半边：SaveScan 与 FinalizeOp 两条腿
// 在飞时，`-wal` 的读数必须是 0600（主库那格由 M376 管，这里顺带读一次做参照）。
func TestM379HistoryWriteLegsTightenWal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")
	s := newStoreAt(t, dir, "history.db")

	if _, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil); err != nil {
		t.Fatalf("SaveScan 失败：%v", err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("前提不成立：写回之后 -wal 不在场，本用例没东西可收：%v", err)
	}
	assertSidecar0600(t, path+"-wal")
	if _, err := os.Stat(path + "-shm"); err == nil {
		assertSidecar0600(t, path+"-shm")
	}

	// 第二条腿：账本写入（BeginOp → FinishItem → FinalizeOp）走的是另一条事务，
	// 只钉 SaveScan 就等于"清理这一路没人管"。
	histID, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil)
	if err != nil {
		t.Fatalf("SaveScan 失败：%v", err)
	}
	var h [32]byte
	opID, err := s.BeginOp("move", "/tmp/dst", histID, true, []OpItemPlan{{
		OrigPath: "/root/a/0.bin", Hash: h, Size: 1000, MtimeNs: 1234567}})
	if err != nil {
		t.Fatalf("BeginOp 失败：%v", err)
	}
	if err := s.FinishItem(opID, "/root/a/0.bin", "/tmp/dst/0.bin", "", StateDone, ""); err != nil {
		t.Fatalf("FinishItem 失败：%v", err)
	}
	if err := s.FinalizeOp(opID, 1000); err != nil {
		t.Fatalf("FinalizeOp 失败：%v", err)
	}
	assertSidecar0600(t, path+"-wal")
	assertSidecar0600(t, path) // 主库参照格
}

// TestM379HistoryPreFixShapeIsSixHundredFortyFour 是 P-55 的前提自检：把补档接缝桩成空操作
// （= 改前形状），同一条写腿产出的 -wal 就是 0644。
// ★ umask 条件与 cache 侧那一格逐字同：077 宿主上这一格改前即绿，取的是"没坏"。
func TestM379HistoryPreFixShapeIsSixHundredFortyFour(t *testing.T) {
	old := hardenSidecarMode
	hardenSidecarMode = func(string) error { return nil }
	t.Cleanup(func() { hardenSidecarMode = old })

	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	defer s.Close()
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil); err != nil {
		t.Fatalf("SaveScan 失败：%v", err)
	}
	perm, err := m379Mode(t, path+"-wal")
	if err != nil {
		t.Fatalf("空桩下 -wal 应仍在场：%v", err)
	}
	switch {
	case runtime.GOOS == "windows":
		t.Logf("windows 腿读数恒 %04o（M354）⇒ 改前形状在这一侧无法表达", perm)
	case perm == 0o644:
		t.Logf("改前形状已钉住：补档缺席时 -wal 读数 = %04o（同机其他账户可读）", perm)
	default:
		t.Logf("宿主 umask 不是 022：SQLite 直接落 %04o ⇒ 本机这一格改前即绿，取的是「没坏」而不是「修好了」", perm)
	}
}

// TestM379HistoryAbsentSidecarsAreSilent 是 P-56 的账本半边（负控制，改前即绿）：
// 伴随文件不在场是常态，不得出声、不得让写腿失败。
func TestM379HistoryAbsentSidecarsAreSilent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatalf("SaveScan 失败：%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("关句柄失败：%v", err)
	}

	out := captureStderr(t, func() {
		s2, err := Open(path) // 这一趟面对的可能是"两个伴随文件都不存在"
		if err != nil {
			t.Errorf("重新打开失败：%v", err)
			return
		}
		if _, err := s2.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
			t.Errorf("SaveScan 失败：%v", err)
		}
		_ = s2.Close()
	})
	if strings.Contains(out, "收紧失败") {
		t.Fatalf("伴随文件缺席或正常收档时不得出声，stderr 实得 %q", out)
	}
}

// TestM379HistorySidecarFailureIsLoudButNotFatal 钉失败处置：真失败要出声（含系统原文），
// 但不得把写腿或 Open 变成失败（口径沿用 M376）。
// ★ 这一格改前不存在（改前没有这条分支），红由变异代取，见划账。
func TestM379HistorySidecarFailureIsLoudButNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 腿的 POSIX 位不表达访问权（M354），本格的失败形状在这一侧不成立")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatalf("SaveScan 失败：%v", err)
	}
	_ = s.Close()

	old := hardenSidecarMode
	hardenSidecarMode = func(string) error { return errors.New("chmod denied by external tool") }
	t.Cleanup(func() { hardenSidecarMode = old })

	var saveErr error
	out := captureStderr(t, func() {
		s2, err := Open(path)
		if err != nil {
			t.Errorf("伴随文件收档失败不得让 Open 失败（用户丢的是回撤能力）：%v", err)
			return
		}
		defer s2.Close()
		_, saveErr = s2.SaveScan(mkScanCfg(), mkGroups(1), nil)
	})
	if saveErr != nil {
		t.Fatalf("收档失败不得上升为写腿失败：%v", saveErr)
	}
	if !strings.Contains(out, "WAL 伴随文件档位收紧失败") || !strings.Contains(out, "系统原文：") {
		t.Fatalf("真失败不得静默，stderr 要含中文外壳与系统原文，实得 %q", out)
	}
	if !strings.Contains(out, "chmod denied by external tool") {
		t.Fatalf("系统原文必须原样带出，实得 %q", out)
	}
}

// TestM379EveryHistoryWriteLegHardens 是**接线锚**（静态，不是行为探针）：
// 本包每一条含写 SQL（INSERT/UPDATE/DELETE）的方法，体内必须出现 hardenSidecars。
//
// 为什么要这条：裁-1 的原话是"写入腿**各**补收紧"，而账本侧有十一条写腿（SaveScan、
// PruneScanFiles、UpdateKeepPaths、DeleteScan、ClearScans、BeginOp、FinishItem、
// FinalizeOp、MarkItemUndo、ClearOps、ImportFrom）。只靠 P-55 点两格，第 12 条写腿
// 新增时照样漏 —— 这一格钉的是**清单本身是判据**（与 M380 的临界区锚同一族）。
//
// ★ 它钉的是形状（函数体里有没有那一行），不是时序；时序那半边由 P-55 的读数管。
//
// ★ 两半各管一件事，都要跑：
//   - **点名清单**：这 11 条腿逐条查，漏一条即红。清单是写死的，所以"改前有几条"这件事
//     本身被钉住了 —— ImportFrom 的写语句住在包级辅助函数里（insertScanRow / mergeOpItems），
//     形状扫描看不见它，只有点名清单能看见。
//   - **形状扫描**：任何 `(*Store)` 方法体内出现 `tx.Exec|s.db.Exec` + INSERT/UPDATE/DELETE，
//     就必须出现补档调用 ⇒ 第 12 条写腿新增时不需要谁来提醒。
func TestM379EveryHistoryWriteLegHardens(t *testing.T) {
	var b strings.Builder
	for _, name := range []string{"scan.go", "oplog.go", "import.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		b.WriteString("\n")
		b.Write(src)
	}
	s := b.String()

	body := func(fn string) (string, bool) {
		m := regexp.MustCompile(`(?ms)^func \(s \*Store\) ` + fn + `\(.*?\n\}`).FindString(s)
		return m, m != ""
	}

	// ① 点名清单（2026-10-01 现读的 11 条写腿）。
	for _, fn := range []string{
		"SaveScan", "UpdateKeepPaths", "PruneScanFiles", "DeleteScan", "ClearScans",
		"BeginOp", "FinishItem", "FinalizeOp", "MarkItemUndo", "ClearOps", "ImportFrom",
	} {
		got, ok := body(fn)
		if !ok {
			t.Fatalf("点名清单里的写腿 %s 在本包源码里找不到 ⇒ 这条锚已经空转", fn)
		}
		if !strings.Contains(got, "hardenSidecars(s.path)") {
			t.Errorf("M379 接线锚：写腿 %s 体内没有 hardenSidecars(s.path) ⇒ 这条腿产出的 -wal 仍以 0644 落盘", fn)
		}
	}

	// ② 形状扫描：兜住清单之外的第 12 条。
	fns := regexp.MustCompile(`(?m)^func \(s \*Store\) ([A-Z]\w+)\(`).FindAllStringSubmatchIndex(s, -1)
	writeStmt := regexp.MustCompile("(tx\\.Exec|s\\.db\\.Exec)\\(\\s*`(INSERT|UPDATE|DELETE)")
	var swept int
	for _, m := range fns {
		fn := s[m[2]:m[3]]
		got, ok := body(fn)
		if !ok {
			continue
		}
		if !writeStmt.MatchString(got) {
			continue // 只读方法：不在本锚的覆盖面里
		}
		swept++
		if !strings.Contains(got, "hardenSidecars(s.path)") {
			t.Errorf("M379 接线锚（形状扫描）：写腿 %s 含写 SQL 却没补档", fn)
		}
	}
	if swept < 10 {
		t.Fatalf("M379 接线锚的形状扫描面本身要自检：只认出 %d 条含写 SQL 的方法"+
			"（2026-10-01 现读是 10 条；ImportFrom 的写语句在包级辅助函数里，本就更少一条），"+
			"说明正则与代码形状脱节了", swept)
	}
	t.Logf("接线锚：点名清单 11 条 + 形状扫描 %d 条含写 SQL 的方法，全部带补档调用", swept)
}
