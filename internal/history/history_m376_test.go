package history

// M376（第八轮裁定回收 / 设计段 §2.1）：**存量**历史账本的档位。
//
// 与 cache 侧同一条判据、同一种形状，但这一份的代价更重：`history.db` 装的是
// 用户机器上的**全部文件路径**（含回收站落位路径）与回撤账本（见 app_records_io.go:188 那三行），
// 而盘上现读（设计段 §1 第 1 条）是 0644、所在目录 0755。
// ★ 两包各一枚接缝、不抽公共 helper：cache 与 history 今天互不依赖，
//   为一行 Chmod 新增跨包耦合是把加固做成结构改动的开始。

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// assertStoreMode 与 openTwiceAtStore 的口径逐字同 internal/cache/cache_m376_test.go
// （分平台措辞那段的理由见那边；两包各写一份是刻意的，不为共用而跨包）。
func assertStoreMode(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("账本影像读不到（%s）：%v", path, err)
	}
	perm := st.Mode().Perm()
	if runtime.GOOS == "windows" {
		if perm != 0o666 {
			t.Fatalf("windows 腿的读数必须仍是 0666（M354 现读：POSIX 位在那条腿上不可表达），实得 %04o", perm)
		}
		return
	}
	if perm != 0o600 {
		t.Fatalf("存量账本档位未收紧：期望 0600，实得 %04o（同机其他账户仍可读）", perm)
	}
}

func openTwiceAtStore(t *testing.T, path string, existingMode os.FileMode) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("首趟建账本失败：%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("首趟关句柄失败：%v", err)
	}
	if existingMode != 0 {
		if err := os.Chmod(path, existingMode); err != nil {
			t.Fatalf("桩存量档位失败：%v", err)
		}
		if st, err := os.Stat(path); err != nil {
			t.Fatalf("桩完读不到：%v", err)
		} else if runtime.GOOS != "windows" && st.Mode().Perm() != existingMode {
			t.Fatalf("夹具桩位未生效：期望 %04o，实得 %04o", existingMode, st.Mode().Perm())
		}
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("第二趟打开失败：%v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	return s2
}

// TestM376OpenTightensExistingStoreMode 是 P-36 本体：盘上已有的 0644 账本，Open 后必须收成 0600。
func TestM376OpenTightensExistingStoreMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	openTwiceAtStore(t, path, 0o644)
	assertStoreMode(t, path)
}

// TestM376HardenFailureStaysOpenAndIsLoud 是 P-37 在账本侧的那一格。
// ★ 如实标注同 cache 侧：改前这条分支不存在，不是"修前必红"探针，红由变异 MU-w 代取。
// 这一格比 cache 侧更要紧：账本不在场用户丢的是**回撤能力**，所以"收紧失败 ⇒ Open 失败"
// 这个看起来更严格的写法，恰恰是本批禁止的修法。
func TestM376HardenFailureStaysOpenAndIsLoud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	probe, err := Open(path)
	if err != nil {
		t.Fatalf("建账本失败：%v", err)
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
	s, err := Open(path)
	_ = w.Close()
	os.Stderr = oldErr
	out := <-got

	if err != nil {
		t.Fatalf("档位收紧失败不该让账本打不开：%v", err)
	}
	if _, err := s.ListScans(); err != nil {
		t.Fatalf("收紧失败后账本必须照常可用：ListScans 报 %v", err)
	}
	_ = s.Close()
	if !strings.Contains(out, "档位收紧失败") || !strings.Contains(out, "系统原文：") {
		t.Fatalf("收紧失败不得静默，stderr 要含中文外壳与系统原文，实得 %q", out)
	}
}

// TestM376FreshlyCreatedStoreIsTightened 是 P-38 在新库那一格的 history 侧（同 umask 条件，
// 见 cache 侧那条注释：umask 022 宿主改前红、077 宿主改前即绿）。
func TestM376FreshlyCreatedStoreIsTightened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("建账本失败：%v", err)
	}
	_ = s.Close()
	assertStoreMode(t, path)
}

// TestM376StoreReopenIsIdempotent 是防过修钉子（不断 0600，只断读数不变 + 三次打开都成功）：
// 账本一旦因加固失败而打不开，用户丢的是**回撤能力**——比留着 0644 更糟，故这条必须钉住。
func TestM376StoreReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s := openTwiceAtStore(t, path, 0o644)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("第二次打开后读不到档位：%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("关句柄失败：%v", err)
	}
	s3, err := Open(path)
	if err != nil {
		t.Fatalf("第三趟打开失败（档位收紧不该影响可用性）：%v", err)
	}
	_ = s3.Close()
	second, err := os.Stat(path)
	if err != nil {
		t.Fatalf("第三次打开后读不到档位：%v", err)
	}
	if first.Mode().Perm() != second.Mode().Perm() {
		t.Fatalf("重复 Open 改变了档位读数：%04o → %04o（收紧必须幂等）", first.Mode().Perm(), second.Mode().Perm())
	}
}
