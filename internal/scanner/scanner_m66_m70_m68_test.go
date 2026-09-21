package scanner

// 第六批探针（2026-09-21，04 §6.11 登记表 M66 / M70 / M68；设计稿 §17.3 P-1~P-3）。
//
// 前两条各自钉住一个登记条目，全部取过"修前必红"：
//   P-1 M66  —— dedupeRoots 的入集顺序按原样串排，而判重按折叠串 ⇒ 宽根被自己的
//                子根抢先入集，同一棵树的两种拼写都留下。
//   P-2 M70  —— WalkWithGate 无条件解引用 f *model.Filters，nil 时 panic 被逐目录
//                recover 吞成一条 Failed ⇒ 现象是"整目录静默漏扫"，不是崩。
//   P-3 M68  —— 链接根指向清单内目录时不警示：**只取证不实施**（t.Skipf），
//                省事修法会被 P-3b 打红，理由与读数见设计稿 §17.7。

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// rootWant 把测试里书写的 unix 风格根路径换算成 dedupeRoots 在**本平台**会产出的
// 归一形式——它第一步就是 filepath.Abs + Clean（scanner.go:564-570）。
//
// ★ 为什么期望值跟着平台走（2026-09-22，设计稿 §21.3 W4）：Windows runner 的工作
// 目录在 D: 上，"/data/b" 归一成 "D:\data\b"；原先把 unix 的产物写死进 want，
// 红的是拼写而不是判据。**谁胜出**（宽根吃掉子根 / 互不相干的两根都留）仍由这两条
// 断言钉住，不随平台变；随平台变的只是那条路径怎么落地。
func rootWant(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("Abs(%q) 失败: %v", p, err)
	}
	return filepath.Clean(abs)
}

// P-1（M66）：判重集合的入集顺序必须按比较键排序。
//
// 走 dedupeRoots 而不是 Walk：它只做字符串工作 + probeCaseSensitive（已是注入点），
// 不碰盘 ⇒ 这条探针在 darwin 与 Linux CI 上给出**同一个确定读数**，
// 不需要 M36 V2 那种"两棵只差大小写的目录"真夹具（本机造不出来，只能 skip）。
func TestDedupeRootsSortsByFoldKey(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	probeCaseSensitive = func(string) bool { return false } // 扮演"两个根都在不敏感卷上"

	// 原样串里 'B'(0x42) < 'b'(0x62) ⇒ 子根先排序在前；折叠后 "/data/b" 才是父根。
	kept, all := dedupeRoots([]string{"/data/B/A", "/data/b"})
	if len(all) != 2 {
		t.Fatalf("去重前的全部根 = %v, want 2 条", all)
	}
	if want := []string{rootWant(t, "/data/b")}; !reflect.DeepEqual(kept, want) {
		t.Fatalf("kept = %v, want %v（宽根必须胜出，子根判重丢弃；"+
			"实得两条即 M66：排序键用了原样串）", kept, want)
	}
}

// P-1b（M66 的反向钉子）：修排序键不得把"本来就互不相干"的两个根折掉一个。
func TestDedupeRootsKeepsUnrelatedRootsAfterSortFix(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	probeCaseSensitive = func(string) bool { return false }
	kept, _ := dedupeRoots([]string{"/data/zz/sub", "/data/b"})
	want := []string{rootWant(t, "/data/b"), rootWant(t, "/data/zz/sub")}
	if !reflect.DeepEqual(kept, want) {
		t.Fatalf("kept = %v, want %v（无父子关系的两根都要留）", kept, want)
	}
}

// P-2（M70）：nil Filters 等于"全默认"，不得让整目录漏扫。
func TestWalkWithNilFiltersIsAllDefault(t *testing.T) {
	dir := t.TempDir()
	mkDirFiles(t, dir, "root.txt")
	mkDirFiles(t, filepath.Join(dir, "sub"), "inner.txt")

	res := Walk(context.Background(), []string{dir}, nil, 2)
	if len(res.Failed) != 0 {
		t.Fatalf("Failed = %+v, want 空（修前：nil *model.Filters 解引用 panic 被 recover 吞成整目录漏扫）",
			res.Failed)
	}
	if len(res.Files) != 2 {
		t.Fatalf("收文件数 = %d, want 2：%v", len(res.Files), filePaths(res.Files))
	}
}

// P-3（M68-a）：根是符号链接时，根级"已脱离系统保护"留痕看不出。
//
// 本条 **skip 不实施**（设计稿 §17.7）：按"真实路径再过一遍 guard.Dir"修它，会让 darwin 上
// /var 与 /tmp 之下的**每一个**普通根都得到一条假的"已脱离系统保护"——清单里
// `eAbsPath /private`（sysguard.go:145，why 原文"var/tmp/folders 等运行时目录的真身"）
// 是按前缀命中的，而 t.TempDir() 的真身就在 /private/var/folders 之下。
// 反向钉子见 P-3b。整条 M68（两半）合并登记为 M84 等裁定。
func TestSymlinkedRootUnderProtectedDirIsReported(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "lost+found") // 清单里的三平台通吃条目（按目录名命中）
	mkDirFiles(t, target, "x.bin")
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("本文件系统不支持符号链接: %v", err)
	}

	res := Walk(context.Background(), []string{link}, &model.Filters{}, 2)
	if len(res.Files) != 1 {
		t.Fatalf("收文件数 = %d, want 1（用户点名的根照扫）：%v", len(res.Files), filePaths(res.Files))
	}
	t.Skipf("M68/M84 待裁定：链接根指向清单内目录时不警示，实得 UnprotectedRoots=%v", res.UnprotectedRoots)
}

// P-3b（M68 的**反向**钉子）：临时目录下的普通根不得报"已脱离系统保护"。
//
// 这条是本批唯一抓到"修复本身制造新假话"的用例：先按"根侧解析真实路径后判定"实现了
// M68-a，它让 P-3 变绿的同时把这条打红（darwin 上 UnprotectedRoots 里冒出
// /var/folders/.../photos 一条）。留成钉子，防止下一轮再用同一个省事修法去动它。
func TestPlainRootStillReportsNoUnprotected(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "photos")
	mkDirFiles(t, dir, "a.txt")
	res := Walk(context.Background(), []string{dir}, &model.Filters{}, 2)
	if len(res.UnprotectedRoots) != 0 {
		t.Fatalf("UnprotectedRoots = %v, want 空（既非清单内路径也不是链接；"+
			"若这里非空，说明有人把根侧判据换成了'按真实路径问清单'——darwin 上 /private 会误伤一切临时目录）",
			res.UnprotectedRoots)
	}
	if len(res.Files) != 1 {
		t.Fatalf("收文件数 = %d, want 1", len(res.Files))
	}
}
