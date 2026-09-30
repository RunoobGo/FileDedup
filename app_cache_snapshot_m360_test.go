package main

// 第八轮审查批 2（拟 M360 / M361）：缓存快照的档位与落位。
//
// ★ 前半批用例只断言**改前就能跑起来**的行为（模式位读数、固定名上那份对象还在不在、
// 回执里的落点），不引用本批新增的 SnapshotNote / landCacheSnapshot ⇒ "修前必红"
// 落在行为上。改后补进来的那一格（P-18 的 note 必须留空）单独标出，取的是
// 「改前该格不存在这条断言」这一最弱的口径——它的红由 MU-9 实测代取。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"filededup/internal/cache"
)

// snapshotPermReading 把"快照文件的模式位读数"这件事只写一遍。
//
// ★ 分平台措辞（M354 同一条纪律）：Windows 的模式位只表达"只读属性"，0600 与 0644
// 在那条腿上读回同一个 0666 ⇒ 本批那句"仅所有者可读写"**只在 unix 腿兑现**，
// windows 腿的断言是"读数必须仍是 0666"（即那条腿根本没有这件事可断）。
// 用 runtime.GOOS 分支而不是第二枚平台文件：格数方程三腿同平移。
func assertSnapshotMode(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("快照读不到（%s）：%v", path, err)
	}
	perm := st.Mode().Perm()
	if runtime.GOOS == "windows" {
		if perm != 0o666 {
			t.Errorf("M360（windows 腿）：模式位读数不是 0666 而是 %o——这条腿的表达方式和记下来的不一样，划账那格要重写", perm)
		}
		return
	}
	if perm != 0o600 {
		t.Errorf("M360：快照里装的是用户机器上的全部路径，模式位却是 %o（承诺是仅所有者可读写 0600）", perm)
	}
}

// P-15：清空成功后，快照的模式位必须是 0600（unix 腿）。
func TestM360CacheSnapshotIsOwnerOnly(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 60)
	res, err := a.CacheClear()
	if err != nil {
		t.Fatalf("清空报错：%v", err)
	}
	if res.SnapshotPath == "" {
		t.Fatal("M360：回执没给快照落点，本用例无从断言")
	}
	assertSnapshotMode(t, res.SnapshotPath)
}

// P-15b：tmp 影像在改名之前就得是 0600——落位不靠 rename 之后补 chmod。
//
// 这一格防的是"把 Chmod 挪到 rename 之后"那种写法：那条路上 tmp 在 SQLite 手里
// 存在过一段时间（共享目录里可读），而 rename 之后再补设会把"设不上"这一档推到
// "固定名已经被覆盖之后"才发现。接缝里先走真实快照、再当场看一眼 tmp。
func TestM360SnapshotTmpIsOwnerOnlyBeforeLanding(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 40)

	restore := cacheClearSnapshot
	var permAtLanding os.FileMode = 0o777
	var sawTmp bool
	cacheClearSnapshot = func(c *cache.Cache, dest string) error {
		if err := restore(c, dest); err != nil {
			return err
		}
		st, err := os.Stat(dest)
		if err != nil {
			t.Errorf("M360：快照接缝里读不到 tmp（%s）：%v", dest, err)
			return nil
		}
		permAtLanding, sawTmp = st.Mode().Perm(), true
		return nil
	}
	t.Cleanup(func() { cacheClearSnapshot = restore })

	if _, err := a.CacheClear(); err != nil {
		t.Fatalf("清空报错：%v", err)
	}
	if !sawTmp {
		t.Fatal("M360：夹具没看到 tmp（接缝没被走到），本用例前提走样")
	}
	if runtime.GOOS == "windows" {
		if permAtLanding != 0o666 {
			t.Errorf("M360（windows 腿）：tmp 读数不是 0666 而是 %o", permAtLanding)
		}
		return
	}
	if permAtLanding != 0o600 {
		t.Errorf("M360：tmp 在落位前是 %o——档位只在改名之后补，SQLite 手里那段 0644 的时间窗没关掉", permAtLanding)
	}
}

// P-18（正控制）：固定名上是上一份自己的快照 ⇒ 原位覆盖、盘上仍只 1 份。
func TestM361OwnPreviousSnapshotIsReplacedInPlace(t *testing.T) {
	a, cch := clearFixture(t)
	for round := 1; round <= 2; round++ {
		seedCache(t, cch, 50)
		res, err := a.CacheClear()
		if err != nil {
			t.Fatalf("第 %d 轮清空报错：%v", round, err)
		}
		if want := filepath.Join(a.cfgDir, CacheClearSnapshotName); res.SnapshotPath != want {
			t.Fatalf("第 %d 轮落点 = %s，want %s（上一份是我们自己的影像时必须原位覆盖，否则用户的固定名承诺没兑）", round, res.SnapshotPath, want)
		}
		// 改后补的一格：正常覆盖上一份时不许多说话——每次都冒一句"快照另落了"
		// 会把用户的注意力从真正需要他的那一格（固定名被占）上引开。
		if res.SnapshotNote != "" {
			t.Errorf("第 %d 轮：原位覆盖却带了另落说明（%q）", round, res.SnapshotNote)
		}
		matches, _ := filepath.Glob(filepath.Join(a.cfgDir, "cache-backup.db*"))
		if len(matches) != 1 {
			t.Fatalf("第 %d 轮后快照目录里多出东西：%v（只留最近一次的承诺被踩穿）", round, matches)
		}
	}
}
