package scanner

// M36 探针（2026-09-21，04 §6.8.8 M36）：遍历期的比较键必须**不折叠**。
//
// 缺陷的准确形态：遍历键此前按「该路径所属扫描根所在卷」的语义折叠，而折叠键描述的
// 是**根所在卷**——一棵根的子树在任意深度上都可能嵌着另一卷。构型可以真造出来
// （本机 `hdiutil` 挂一棵 Case-sensitive APFS 进普通目录，读数见设计稿 §10.0 E1~E4）：
// 父根被判为不敏感 ⇒ 子树里 `alpha/` 与 `ALPHA/` 两棵**不同**目录被折成一个键 ⇒
// 后遇到的那棵整棵不进语料，且 `files_failed=0`（静默）。
//
// 本机（darwin）的临时目录一律在不敏感卷上，两棵大小写不同的目录**无法并存**，
// 所以下面两条走**注入**：`probeCaseSensitive` 扮演"这些根在不敏感卷上"（E8）——
// 复现的正是缺陷的要害（根卷判定 ≠ 子树现实），只是把"子树在另一卷上"换成了
// Linux 本来就大小写敏感的临时目录。V3 与卷语义无关，两端都能跑；
// V2 在 Linux CI 上跑真两棵（macOS 上 skip）。

import (
	"context"
	"path/filepath"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// V2（设计稿 §10.3）：两个根扫"父根 + 另一无关目录"，父根子树里两棵大小写不同的
// 目录都要收齐。修前 2（一棵被折掉，零失败记录），修后 3。
func TestMultiRootWalkKeepsCaseVariantSubtrees(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	base := t.TempDir()
	if !fscase.Sensitive(base) {
		t.Skipf("临时目录所在卷不区分大小写（%s）：alpha/ 与 ALPHA/ 无法并存", base)
	}
	parent := filepath.Join(base, "parent")
	other := filepath.Join(base, "other")
	mkDirFiles(t, filepath.Join(parent, "alpha"), "a.txt")
	mkDirFiles(t, filepath.Join(parent, "ALPHA"), "b.txt")
	mkDirFiles(t, other, "c.txt")

	// 扮演"两个根都在不敏感卷上"。真值 3 与卷语义无关——它测的是
	// "遍历不该拿根卷的判定去折叠子树的拼写"。
	probeCaseSensitive = func(string) bool { return false }

	res := Walk(context.Background(), []string{parent, other}, &model.Filters{}, 2)
	if len(res.Files) != 3 {
		t.Fatalf("两根收文件数 = %d, want 3（真值：alpha/a.txt + ALPHA/b.txt + other/c.txt；修前 2）：%v",
			len(res.Files), filePaths(res.Files))
	}
	if len(res.Failed) != 0 {
		t.Fatalf("不应有失败项（修前也是 0——这正是本项「静默」的地方）: %+v", res.Failed)
	}
}

// V3（设计稿 §10.3、边界 §10.5-1 / M44）：逃逸判据的键随之变精确。
// 宽根 + 「拼写与盘上仅大小写不同」的保护清单内路径作根：剪枝必须照旧生效
// （fail-closed），只记 ProtectedDirs，不把用户从没点过的目录放行成"已脱离保护"。
// 修前折叠键让两者匹配 ⇒ 逃逸放行 + UnprotectedRoots 报出一个他没点过的根。
func TestCaseMismatchedRootUnderProtectedDirIsNotRescued(t *testing.T) {
	t.Cleanup(func() { probeCaseSensitive = fscase.Sensitive })
	root := t.TempDir()
	mkDirFiles(t, filepath.Join(root, "lost+found", "inner"), "x.bin")
	mkDirFiles(t, filepath.Join(root, "keep"), "a.bin")
	// 用户手输的大小写变体：在不敏感卷上 OS 认它（指向同一个目录），
	// 而遍历里的拼写来自 ReadDir ⇒ 两者就此不同。
	mismatch := filepath.Join(root, "LOST+FOUND", "inner")
	probeCaseSensitive = func(string) bool { return false } // 扮演"该卷不敏感"

	res := Walk(context.Background(), []string{root, mismatch}, &model.Filters{}, 2)
	if hasPathWith(res, "/lost+found/") {
		t.Error("保护清单内目录被放行——拼写差异不该构成「用户指定过」")
	}
	if res.ProtectedDirs < 1 {
		t.Errorf("ProtectedDirs = %d, want >=1（剪枝要可见地计数）", res.ProtectedDirs)
	}
	if len(res.UnprotectedRoots) != 0 {
		t.Errorf("UnprotectedRoots = %v, want 空（没放行就不该报告有根脱离保护）", res.UnprotectedRoots)
	}
}
