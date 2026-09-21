package scanner

// M21（2026-09-21，设计稿 §7.3）工作临时名跳过计数的**可见性**用例。
//
// 与 scanner_worktemp_test.go 的分工：那里钉"哪些名字该被忽略、哪些用户文件不许
// 被误伤"（判定本身，一字未动）；这里钉"忽略这件事**看得见**"——数得准、不随
// 用户的过滤设置漂移、只数文件、数不到的地方不编数。
//
// 为什么值得单独一组：这类名字是三类"跳过"里唯一无声的一类（保护清单、云端占位
// 早有计数），且判据是名字形态、分不出"我们的残留"与"用户恰好这样命名的文件"。
// 计数是它唯一的可见面（GUI 无控制台）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// writeSized 造一个指定大小的文件（大小是 V4 的判据之一）。
func writeSized(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// V1：五种**已注册形态**各一个 → 计数恰为 5，用户文件照常收齐。
// 形态清单与 worktemp.forms 同源（后缀三式 + 插入式 + 插入式的 _N 递增）。
func TestWalkCountsWorkTempSkipped(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, root,
		"dup.bin.fdd-old",              // 后缀式：合并前的完整副本（缺陷 6 的现场）
		"dup.bin.fdd-tmp",              // 后缀式：临时硬链接
		"dup.bin.fdd-old.undo",         // 后缀式：回滚失败暂存的二次后缀
		"photo.jpg.fdd-restored.jpg",   // 插入式：回收站回撤的"原位被占"标记
		"photo.jpg.fdd-restored_2.jpg", // 插入式 + claimDst 的 _N 递增（AS-R1）
		"keep.bin", "real1.bin", "real2.bin",
	)

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 4)

	if got, want := res.SkippedWorkTempFiles, 5; got != want {
		t.Errorf("SkippedWorkTempFiles = %d, want %d（五种已注册形态各一）", got, want)
	}
	names := namesIn(res)
	for _, n := range []string{"keep.bin", "real1.bin", "real2.bin"} {
		if !names[n] {
			t.Errorf("用户文件 %q 未进语料（判定或计数路径误伤）", n)
		}
	}
	if len(res.Files) != 3 {
		t.Errorf("应恰好收集 3 个用户文件，实际 %d", len(res.Files))
	}
	// 同 M6-P4/M6-P1 的纪律：命中不是失败——计数与失败清单分属两个抽屉。
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want 空（跳过不是失败）", res.Failed)
	}
}

// V2：反向守卫 —— 只是**内嵌**标记的用户名，计数必须为 0，且全部照常收集。
// 判定一旦放宽成 Contains，这里先红（M1 的漏扫现场：漏扫比残留更难发现）。
func TestWorkTempCountIgnoresEmbeddedMarkerUserNames(t *testing.T) {
	root := t.TempDir()
	mkDirFiles(t, root,
		"notes.fdd-old-summary.txt", // 内嵌 .fdd-old，但既不结尾也不成插入式
		"build.fdd-tmp-dir.so",      // 内嵌 .fdd-tmp
		"x.fdd-case-probe-notes",    // 前缀式的形，但尾部不是纯数字
	)

	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)

	if res.SkippedWorkTempFiles != 0 {
		t.Errorf("SkippedWorkTempFiles = %d, want 0（这些是用户名，不是我们的产物）",
			res.SkippedWorkTempFiles)
	}
	if len(res.Files) != 3 {
		t.Errorf("应收集 3 个用户文件，实际 %d：%v", len(res.Files), namesIn(res))
	}
}

// V3：计数不随 IncludeHidden 漂移。
//
// 这钉的是**顺序**：自增在 scanner.go 的文件侧隐藏规则之前（该规则住在
// filter.Apply 里）。若把自增挪到它之后，用户勾一下"包含隐藏文件"就会看到这个数
// 变化，读起来像"这次残留比上次少"——而盘上一模一样。
func TestWorkTempCountDoesNotDriftWithIncludeHidden(t *testing.T) {
	root := t.TempDir()
	// 崩溃残片：探测文件以 "." 开头（平时被隐藏规则挡着），但形态上仍属登记表。
	mkDirFiles(t, root, ".fdd-case-probe-7", "movie.mkv.fdd-old", ".plain-hidden")

	off := Walk(context.Background(), []string{root}, &model.Filters{}, 2)
	on := Walk(context.Background(), []string{root}, &model.Filters{IncludeHidden: true}, 2)

	if off.SkippedWorkTempFiles != 2 || on.SkippedWorkTempFiles != 2 {
		t.Errorf("SkippedWorkTempFiles 随 IncludeHidden 漂移：hidden=false 得 %d，true 得 %d，want 均为 2",
			off.SkippedWorkTempFiles, on.SkippedWorkTempFiles)
	}
	// 反向对照：隐藏的**普通**文件确实随该开关进出语料。没有这一条，
	// "两档计数相等"也可能只是开关压根没生效。
	if hasPathWith(off, "/.plain-hidden") || !hasPathWith(on, "/.plain-hidden") {
		t.Errorf("IncludeHidden 对普通隐藏文件的既有语义变了（off=%v on=%v），上面的相等断言不成立",
			namesIn(off), namesIn(on))
	}
	// 探测残片两档都不进语料：它是我们的中间产物，从来不该参与分组。
	if hasPathWith(off, "/.fdd-case-probe-7") || hasPathWith(on, "/.fdd-case-probe-7") {
		t.Error("探测残片进了语料（判定失效）")
	}
}

// V4：计数不随扩展名/大小/0 字节三条普通过滤漂移。
//
// 这钉的是**位置**：自增在 matcher.Apply 之前。挪到之后，一个 .log 的残留会被
// 扩展名过滤静默吃掉，这个数就开始说谎（设计稿 §7.1 的纪律原文）。
func TestWorkTempCountDoesNotDriftWithFilters(t *testing.T) {
	root := t.TempDir()
	writeSized(t, root, "report.fdd-restored.log", 200) // 插入式 → 扩展名排除命中
	writeSized(t, root, "tiny.txt.fdd-tmp", 10)         // 低于 MinSize
	writeSized(t, root, "zero.bin.fdd-old", 0)          // 0 字节内置跳过
	writeSized(t, root, "keep.txt", 200)                // 正常收集

	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludeExts: []string{".log"}, MinSize: 100}, 2)

	if got, want := res.SkippedWorkTempFiles, 3; got != want {
		t.Errorf("SkippedWorkTempFiles = %d, want %d（三条过滤各挡下一个临时名，计数须全数照收）",
			got, want)
	}
	if !hasPathWith(res, "/keep.txt") {
		t.Error("keep.txt 未进语料（夹具整体没生效，上面的计数断言不成立）")
	}
	for _, n := range []string{"report.fdd-restored.log", "tiny.txt.fdd-tmp", "zero.bin.fdd-old"} {
		if hasPathWith(res, "/"+n) {
			t.Errorf("%s 进了语料：普通过滤没挡住它", n)
		}
	}
}

// V5：口径边界 —— 只数**文件**，且只数**到达过**的。
//
//   - 临时命名的目录从 M1 起不跳过（我们从不生成这种目录），它的内容逐项判定：
//     目录本身不进数，里面的**普通**文件照常进语料；
//   - 被 ExcludePaths 剪掉的子树没有下潜，其中的临时名文件**数不到**——
//     与 ProtectedDirs"只数目录、不估文件"同一条纪律：不知道的不许估。
func TestWorkTempCountIsFilesOnlyAndReachedOnly(t *testing.T) {
	root := t.TempDir()
	// 目录名恰为临时名：目录不跳、不计，内容逐项判定。
	mkDirFiles(t, filepath.Join(root, "a.fdd-old"), "inner.bin", "inner2.bin.fdd-old")
	// 被排除的子树：里面那个残留不该进数（没走到那儿）。
	mkDirFiles(t, filepath.Join(root, "skipme"), "buried.fdd-old")

	res := Walk(context.Background(), []string{root},
		&model.Filters{ExcludePaths: []string{"skipme"}}, 2)

	if got, want := res.SkippedWorkTempFiles, 1; got != want {
		t.Errorf("SkippedWorkTempFiles = %d, want %d（只有 a.fdd-old/inner2.bin.fdd-old 被数到；"+
			"目录本身不计、被排除子树里的数不到）", got, want)
	}
	if !hasPathWith(res, "/a.fdd-old/inner.bin") {
		t.Errorf("临时命名目录里的普通文件未进语料（M1 的漏扫缺陷复发）：%v", namesIn(res))
	}
	if hasPathWith(res, "/skipme/buried.fdd-old") {
		t.Error("被排除子树里的残留进了语料")
	}
}
