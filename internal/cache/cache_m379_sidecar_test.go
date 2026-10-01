package cache

// M379（第九轮批 C / 2026-10-01 裁-1「写入腿各补收紧」）：**写入期** WAL 伴随文件的档位。
//
// 现读取证（§6.69 第八轮那次一次性探针，设计段 §1 第 8 条）：`hardenFileMode` 只在 `Open`
// 里用一次、只改得到 `<base>` 本身，于是主库收成 0600 之后 `-wal` 仍按进程 umask 落 0644
// ——而 WAL 模式下尚未 checkpoint 的哈希条目**全在 -wal 里**（含扫描根下的文件路径），
// 同机其他账户可读。M376 收的是"存量影像"那一侧，本条收的是"写入期"这一侧。
//
// ★ 判据只打在"读数"上（M376 同一条纪律）：P-55 改前真红，不是红在编译错误上；
//   它的红另由变异 MU9-f（把 hardenSidecars 改成空函数）代取，两者都跑、读数分别记。
// ★ Windows 腿不兑现（M354：POSIX 位在那条腿上恒读回 0666）⇒ 分平台措辞沿用 assertDBMode
//   的写法；**不得**据此写"三平台已验"。

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// assertSidecarMode 钉"这个伴随文件的档位读数已是仅所有者可读写"，返回实得读数。
// 期望值与 assertDBMode 同源，但话术点名是哪个文件——读数错了要说清错在哪一格。
func assertSidecarMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("伴随文件读不到（%s）：%v", path, err)
	}
	perm := st.Mode().Perm()
	if runtime.GOOS == "windows" {
		// M354：这条腿只能读到 0666，POSIX 位不表达真实访问权（由所在目录的 ACL 决定）。
		if perm != 0o666 {
			t.Fatalf("windows 腿的读数必须仍是 0666，实得 %04o（%s）", perm, path)
		}
		return perm
	}
	if perm != 0o600 {
		t.Fatalf("%s 档位未收紧：期望 0600，实得 %04o（同机其他账户仍可读）", filepath.Base(path), perm)
	}
	return perm
}

// captureStderr 收一段代码写到 os.Stderr 的内容（M376 的 P-37 同形，抽成 helper 是因为
// 本批两格都要听 stderr）。
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

// m379Entries 造 n 条采样非全零的条目（全零行会被 Lookup 当脏行，与本条无关，别混进来）。
func m379Entries(n int) []Entry {
	out := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Entry{
			Path: filepath.Join("/m379", itoa(i)), Size: uint64(10 + i), MtimeNs: int64(1000 + i),
			Head: uint64(i + 1), Tail: uint64(i + 2), Mid1: uint64(i + 3), Mid2: uint64(i + 4),
			Dev: 7, Ino: uint64(i + 1), CtimeNs: int64(i),
		})
	}
	return out
}

// TestM379StoreLegTightensWalSidecar 是 P-55 本体：写腿在飞时 `-wal` 的读数必须是 0600。
// 改前必红（同一夹具下 -wal 读回 0644，那件事由下面那格用接缝空桩钉成读数）。
func TestM379StoreLegTightensWalSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Store(m379Entries(400)); err != nil {
		t.Fatalf("写回失败：%v", err)
	}
	// 前提自检：这格必须有 -wal 可收（写回不自动 checkpoint，正常必在场）。
	// 夹具哪天把它 checkpoint 掉了，要说"前提不成立"，不许让断言空过。
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("前提不成立：写回 400 条后 -wal 不在场，本用例没东西可收：%v", err)
	}
	assertSidecarMode(t, path+"-wal")
	if _, err := os.Stat(path + "-shm"); err == nil {
		assertSidecarMode(t, path+"-shm")
	} else if runtime.GOOS != "windows" {
		t.Logf("-shm 不在场（只读打开或无共享内存时 SQLite 不建它），本格只钉 -wal")
	}
	// 主库那一格由 M376 的 P-35/P-38 管；这里再读一次，是为了"同一次写回之后三者一致"这句话有据。
	assertDBMode(t, path)
}

// TestM379PreFixShapeIsSixHundredFortyFour 是 P-55 的**前提自检**：把补档接缝桩成"什么都不做"
// （= 改前的形状，这一步骤当时不存在），同一条写腿产出的 `-wal` 读数就是改前那个值。
//
// ★ 为什么单独一格："改前 -wal 是 0644"这句话不能只写在注释里，要有夹具把它钉成读数。
//
//	这一格跑在改后的代码上也不会红——它桩的是接缝，不是判据。
//
// ★ umask 条件如实记（M376 的 P-38 同一句纪律）：宿主 umask 为 022（本机与 CI 三条腿的现读）
//
//	时空桩落 0644 ⇒ "改前必红"成立；若宿主 umask 是 077，SQLite 建出来本就是 0600 ⇒ 该宿主上
//	这一格拿到的是"没坏"，不是"修好了"，所以下面只记日志、不判红。
func TestM379PreFixShapeIsSixHundredFortyFour(t *testing.T) {
	old := hardenSidecarMode
	hardenSidecarMode = func(string) error { return nil }
	t.Cleanup(func() { hardenSidecarMode = old })

	path := filepath.Join(t.TempDir(), "cache.db")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	defer c.Close()
	if err := c.Store(m379Entries(50)); err != nil {
		t.Fatalf("写回失败：%v", err)
	}
	st, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatalf("空桩下 -wal 应仍在场（写回不自动 checkpoint）：%v", err)
	}
	perm := st.Mode().Perm()
	if runtime.GOOS == "windows" {
		t.Logf("windows 腿读数恒 %04o（M354）⇒ 改前形状在这一侧无法表达，本批的 unix 腿读数才算数", perm)
		return
	}
	if perm == 0o644 {
		t.Logf("改前形状已钉住：补档缺席时 -wal 读数 = %04o（同机其他账户可读），P-55 断的就是这一格被修掉", perm)
		return
	}
	t.Logf("宿主 umask 不是 022：SQLite 直接落 %04o ⇒ 本机这一格改前即绿，取的是「没坏」而不是「修好了」", perm)
}

// TestM379AbsentSidecarsAreSilent 是 P-56（负控制，改前即绿）：
// 伴随文件不在场是**常态**（`-shm` 在只读/无共享内存时不建；`-wal` 在 checkpoint 后随最后一个
// 连接退出被 SQLite 删除），这类缺席不得出声、更不得报错。
//
// ★ 这一格钉的是 `dbfile.HardenSidecars` 里的 ErrNotExist 滤除：少了那道滤除，
//
//	"打开一份干净的库"就会朝界面吐一行读起来像加固失败的噪声。
func TestM379AbsentSidecarsAreSilent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")

	c, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	if err := c.Store(m379Entries(10)); err != nil {
		t.Fatalf("写回失败：%v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("关句柄失败：%v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			// 关闭时没删它（取决于 checkpoint 状态）⇒ 这一格退化为"在场就必须对"。
			assertSidecarMode(t, path+suffix)
		}
	}

	out := captureStderr(t, func() {
		c2, err := Open(path)
		if err != nil {
			t.Errorf("重新打开失败：%v", err)
			return
		}
		if err := c2.Store(m379Entries(3)); err != nil {
			t.Errorf("写回失败：%v", err)
		}
		_ = c2.Close()
	})
	if strings.Contains(out, "收紧失败") {
		t.Fatalf("伴随文件缺席或正常收档时不得出声，stderr 实得 %q", out)
	}
}

// TestM379SidecarFailureIsLoudButNotFatal 钉失败处置两半（口径逐字沿用 M376 的 P-37）：
// ① 真·收档失败**不得**把 Open 或写腿变成失败；② 但不得静默，stderr 那行要同时含中文外壳
// 与「系统原文」。
//
// ★ 如实标注：这一格**改前不存在**（改前没有这条分支），所以它不是"修前必红"探针——按 M360/M376
//
//	先例取最弱口径"改前该断言不存在"，其红由变异代取（把出声改成 `return err`，本用例必须红）。
func TestM379SidecarFailureIsLoudButNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 腿的 POSIX 位不表达访问权（M354），本格的失败形状在这一侧不成立")
	}
	path := filepath.Join(t.TempDir(), "cache.db")
	probe, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	if err := probe.Store(m379Entries(5)); err != nil {
		t.Fatalf("写回失败：%v", err)
	}
	_ = probe.Close()

	old := hardenSidecarMode
	hardenSidecarMode = func(string) error { return errors.New("chmod denied by external tool") }
	t.Cleanup(func() { hardenSidecarMode = old })

	var storeErr error
	out := captureStderr(t, func() {
		c, err := Open(path)
		if err != nil {
			t.Errorf("伴随文件收档失败不得让 Open 失败：%v", err)
			return
		}
		storeErr = c.Store(m379Entries(5))
		if _, hit, _ := c.Lookup("/m379/0", 10, 1000, fsid.ID{}); !hit {
			t.Error("收档失败之后缓存必须照常可用")
		}
		_ = c.Close()
	})
	if storeErr != nil {
		t.Fatalf("收档失败不得上升为写腿失败：%v", storeErr)
	}
	if !strings.Contains(out, "WAL 伴随文件档位收紧失败") || !strings.Contains(out, "系统原文：") {
		t.Fatalf("真失败不得静默，stderr 要含中文外壳与系统原文，实得 %q", out)
	}
	if !strings.Contains(out, "chmod denied by external tool") {
		t.Fatalf("系统原文必须原样带出，实得 %q", out)
	}
}
