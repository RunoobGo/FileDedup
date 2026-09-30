package cache

// M376（第八轮裁定回收 / 设计段 §2.1）：**存量**缓存库影像的档位。
//
// 现读取证（设计段 §1 第 1 条）：`~/Library/Application Support/FileDedup/cache.db` 是 0644，
// 而所在目录 0755 ⇒ 同机其他账户可读。批 2 的 M360 只把**新产影像**收成 0600，
// 打开路径（cache.go:183）一处 chmod 都没有，所以盘上那份老库一直没人管。
//
// ★ 判据只打在"读数"上，不引用本批新增的接缝标识符：这样改前是真的跑起来红，
//   而不是红在编译错误上（§2.2 第 6 条 M364 那条纪律）。P-37 那格（收紧失败必须出声）
//   改前分支根本不存在，按 M360 先例如实标注、由变异 MU-w 代取其红，见下面那段。

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// assertDBMode 是"这份库影像的档位读数"这一格的唯一写法（P-35/P-36/P-38 共用）。
//
// ★ 分平台措辞（M354/M360 同一条纪律）：Windows 的模式位只表达"只读属性"，
//
//	0600 与 0644 在那条腿上读回同一个 0666 ⇒ "仅所有者可读写"只在 unix 腿兑现，
//	windows 腿的断言是"读数必须仍是 0666"（那条腿没有这件事可断）。
//	用 runtime.GOOS 分支而不是第二枚平台文件：格数方程三腿同平移。
func assertDBMode(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("库影像读不到（%s）：%v", path, err)
	}
	perm := st.Mode().Perm()
	if runtime.GOOS == "windows" {
		if perm != 0o666 {
			t.Fatalf("windows 腿的读数必须仍是 0666（M354 现读：POSIX 位在那条腿上不可表达），实得 %04o", perm)
		}
		return
	}
	if perm != 0o600 {
		t.Fatalf("存量库档位未收紧：期望 0600，实得 %04o（同机其他账户仍可读）", perm)
	}
}

// openTwiceAt 按指定路径开→关→（桩成存量档位）→再开，返回第二次的句柄。
// 第二次打开才是本批判据落的地方：第一趟只负责造出一份真实的库影像。
func openTwiceAt(t *testing.T, path string, existingMode os.FileMode) *Cache {
	t.Helper()
	c, err := Open(path)
	if err != nil {
		t.Fatalf("首趟建库失败：%v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("首趟关句柄失败：%v", err)
	}
	if existingMode != 0 {
		if err := os.Chmod(path, existingMode); err != nil {
			t.Fatalf("桩存量档位失败：%v", err)
		}
		if st, err := os.Stat(path); err != nil {
			t.Fatalf("桩完读不到：%v", err)
		} else if runtime.GOOS != "windows" && st.Mode().Perm() != existingMode {
			// ★ 夹具自证：桩没生效时断言会假绿，所以这里先把"改前确实是 0644"钉死。
			t.Fatalf("夹具桩位未生效：期望 %04o，实得 %04o", existingMode, st.Mode().Perm())
		}
	}
	c2, err := Open(path)
	if err != nil {
		t.Fatalf("第二趟打开失败：%v", err)
	}
	t.Cleanup(func() { _ = c2.Close() })
	return c2
}

// TestM376OpenTightensExistingDBMode 是 P-35 本体：盘上已有的 0644 库，Open 之后读数必须是 0600。
// 改前必红（现读 0644；夹具自证那一格同时证明"改前确实是 0644"）。
func TestM376OpenTightensExistingDBMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	openTwiceAt(t, path, 0o644)
	assertDBMode(t, path)
}

// TestM376HardenFailureStaysOpenAndIsLoud 是 P-37（失败处置本体）。
//
// ★ 如实标注：这一格**改前不存在**（改前没有这条分支），所以它不是"修前必红"探针——
//
//	按 M360 先例取的是"改前该格不存在这条断言"这一最弱口径，它的红由变异 MU-w 代取
//	（MU-w = 把出声改成 `return nil, err`，本用例必须变红）。
//
// 判据两半：① 收紧失败**不得**让 Open 失败、库必须照常可用；② 但不得静默，stderr 那行
//
//	要同时含中文外壳与「系统原文」（09「错误文案」已立的话术同形）。
func TestM376HardenFailureStaysOpenAndIsLoud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	probe, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	_ = probe.Close()

	old := hardenFileMode
	hardenFileMode = func(string) error { return errors.New("chmod denied") }
	t.Cleanup(func() { hardenFileMode = old })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldErr := os.Stderr
	os.Stderr = w
	got := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		got <- string(b)
	}()
	c, err := Open(path)
	_ = w.Close()
	os.Stderr = oldErr
	out := <-got

	if err != nil {
		t.Fatalf("档位收紧失败不该让 Open 失败（用户丢缓存比留着 0644 更糟）：%v", err)
	}
	if _, err := c.GetStats(); err != nil {
		t.Fatalf("收紧失败后库必须照常可用：GetStats 报 %v", err)
	}
	_ = c.Close()
	if !strings.Contains(out, "档位收紧失败") || !strings.Contains(out, "系统原文：") {
		t.Fatalf("收紧失败不得静默，stderr 要含中文外壳与系统原文，实得 %q", out)
	}
	// ★ 盘上文件一字不动：失败处置不许把影像改成别的形状。
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读不到库影像：%v", err)
	}
	if runtime.GOOS == "windows" && st.Mode().Perm() != 0o666 {
		t.Fatalf("windows 腿读数必须仍是 0666，实得 %04o", st.Mode().Perm())
	}
}

// TestM376FreshlyCreatedDBIsTightened 是 P-38 前格：首次 Open 造出来的新库同样要 0600
// （同一条腿不分支）。★ umask 条件如实记：本格在 umask 022 宿主（本机与 CI 三条腿的现读）改前红；
// 若宿主 umask 是 077，SQLite 建出来本就 0600 ⇒ 该格改前即绿，取的是"没坏"而不是"修好了"。
func TestM376FreshlyCreatedDBIsTightened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	_ = c.Close()
	assertDBMode(t, path)
}

// TestM376ReopenIsIdempotent 是 P-38 后格（防过修钉子）：已经收过的库反复 Open，
// 读数必须**不再变化**、库照常可用，且不得出现"第二次把文件挡在门外"的形状。
// ★ 本格刻意**不断 0600**（那件事由 P-35 管）：它只断"两次读数相同 + 三次打开都成功"，
//
//	所以改前即绿——它的价值是让"顺手把 Open 改成收紧失败即拒绝"的实现必须红在这里。
func TestM376ReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	c := openTwiceAt(t, path, 0o644)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("第二次打开后读不到档位：%v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("关句柄失败：%v", err)
	}
	c3, err := Open(path)
	if err != nil {
		t.Fatalf("第三趟打开失败（档位收紧不该影响可用性）：%v", err)
	}
	_ = c3.Close()
	second, err := os.Stat(path)
	if err != nil {
		t.Fatalf("第三次打开后读不到档位：%v", err)
	}
	if first.Mode().Perm() != second.Mode().Perm() {
		t.Fatalf("重复 Open 改变了档位读数：%04o → %04o（收紧必须幂等）", first.Mode().Perm(), second.Mode().Perm())
	}
}
