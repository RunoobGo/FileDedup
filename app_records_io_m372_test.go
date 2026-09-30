package main

// M372 落位卫生（2026-09-30 第八轮批 3 / 设计段 §3.2 第六条）。
//
// 两条腿各自钉一件事，都是"只删/只写自己建的那件"：
//
//	P-31：预检之后、创建之前，第三方把 `<stem>.json.tmp` 占了名字 —— 旧写法
//	      `os.WriteFile` 会**静默截断别人的文件**（覆盖第三方文件在 §6.63 是 P0 形状）；
//	P-32：同一个窗口里第三方占的是 `<stem>.db.tmp` —— `VACUUM INTO` 对已存在目标直接报错，
//	      于是本次导出失败并走 `cleanup()`，而旧 `cleanup()` 无条件 unlink 两个 tmp 名
//	      ⇒ 把从没建过的那件（别人的）删掉，也是 P0 形状。
//
// ★ 为什么必须有接缝：`exists()` 预检挡得住"动手前就已被占"的那档（那是给用户的话术），
// 挡不住"预检之后才被占"的那个时间窗 —— 而那个窗人手点不出来（要在两条语句之间插进去），
// 只能留一条"生产恒 nil"的卡点缝。先例是 `app.go` 的 `scanAboutToSaveHook`（那段自陈
// 就是"新符号 ⇒ 改前必红不存在"的记账）。
//
// ★ 缝只用来卡住那一刻，判据全部落在真实文件系统上：文件内容一字不变、目录里不多不少。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportSeam 装上卡点缝：在"预检已过、还没动手"那一刻把 f 跑掉。返回还原函数。
func exportSeam(t *testing.T, f func(tmpDB, tmpJSON string)) func() {
	t.Helper()
	old := exportAboutToCreateHook
	exportAboutToCreateHook = f
	return func() { exportAboutToCreateHook = old }
}

// stemOf 复算导出会用的那个词干（与 exportRecordsTo 同一算法，夹具自己算是为了
// 不依赖被测代码的回执——回执在失败路径上是空的）。
func stemOf() string { return "filededup-records-" + fixedNow().Format("20060102-150405") }

// P-31：窗口里第三方占了 `<stem>.json.tmp` ⇒ 导出失败、别人的文件一字不变、不留半件。
func TestExportDoesNotClobberStrangerJSONTmp(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	stem := stemOf()
	stranger := filepath.Join(dir, stem+".json.tmp")
	const want = "STRANGER-JSON-DO-NOT-TOUCH"
	restore := exportSeam(t, func(tmpDB, tmpJSON string) {
		if tmpJSON != stranger {
			t.Errorf("夹具前提走样：缝拿到的 tmpJSON=%s，与算出的 %s 不一致", tmpJSON, stranger)
		}
		// 预检刚刚放行过 ⇒ 此刻这个名字是空的，写进去就是"第三方在窗口里抢名"。
		if err := os.WriteFile(stranger, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	defer restore()

	_, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err == nil {
		t.Fatal("M372：目标名被第三方占着却报告导出成功")
	}
	got, rerr := os.ReadFile(stranger)
	if rerr != nil {
		t.Fatalf("M372：别人的文件没了（被我们删掉/搬走了）: %v", rerr)
	}
	if string(got) != want {
		t.Errorf("M372：别人的文件被截断/覆盖了，内容 %q want %q", got, want)
	}
	// 我们自己的那件（.db 的 tmp）必须收回，且正式名字一个都不许出现。
	entries, derr := os.ReadDir(dir)
	if derr != nil {
		t.Fatal(derr)
	}
	for _, e := range entries {
		if e.Name() == filepath.Base(stranger) {
			continue
		}
		t.Errorf("M372：失败后留下 %s（半件导出比没有更害人）", e.Name())
	}
}

// P-32：窗口里第三方占了 `<stem>.db.tmp` ⇒ 导出失败，但 `cleanup()` 不得 unlink 它。
func TestExportDoesNotUnlinkStrangerDBTmp(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	stem := stemOf()
	stranger := filepath.Join(dir, stem+".db.tmp")
	const want = "STRANGER-DB-DO-NOT-UNLINK"
	restore := exportSeam(t, func(tmpDB, tmpJSON string) {
		if tmpDB != stranger {
			t.Errorf("夹具前提走样：缝拿到的 tmpDB=%s，与算出的 %s 不一致", tmpDB, stranger)
		}
		if err := os.WriteFile(stranger, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	defer restore()

	_, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err == nil {
		t.Fatal("M372：影像的 tmp 名被占着（VACUUM INTO 必失败）却报告导出成功")
	}
	got, rerr := os.ReadFile(stranger)
	if rerr != nil {
		t.Fatalf("M372：别人的 tmp 被 cleanup 删掉了（我们从没建过它）: %v", rerr)
	}
	if string(got) != want {
		t.Errorf("M372：别人的 tmp 被改写了，内容 %q want %q", got, want)
	}
	entries, derr := os.ReadDir(dir)
	if derr != nil {
		t.Fatal(derr)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("M372：目录里除别人的 tmp 外还多了 %v", names)
	}
}

// 回归：正常导出仍然成对落位（O_EXCL 不得把正常路径也判成撞名）。
// 这一格是"改判据不许顺手把功能关掉"的对照物，判据与 m351 的两件套用例同向。
func TestExportStillPairsUpAfterExclusiveCreate(t *testing.T) {
	a := recordsApp(t)
	dir := t.TempDir()
	res, err := a.exportRecordsTo(a.histSnapshot(), dir, fixedNow())
	if err != nil {
		t.Fatalf("M372：正常导出被 O_EXCL 判成撞名了: %v", err)
	}
	for _, p := range []string{res.DbPath, res.JsonPath} {
		if st, serr := os.Stat(p); serr != nil || st.Size() == 0 {
			t.Fatalf("M372：产物缺失或为空 %s（%v）", p, serr)
		}
	}
	if leftover, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(leftover) != 0 {
		t.Errorf("M372：成功后仍残留临时文件 %v", leftover)
	}
	if !strings.HasPrefix(filepath.Base(res.DbPath), stemOf()) {
		t.Errorf("M372：产物名字不对 %s（种子 %s）", filepath.Base(res.DbPath), stemOf())
	}
}
