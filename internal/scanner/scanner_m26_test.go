package scanner

// M26 探针（2026-09-21，04 §6.8.8 M26）：前缀判据必须在**目标平台的真相**下成立。
//
// 缺陷的准确形态（E16 真读数）：修前 dedupeRoots 拿 fscase.Fold 的结果直接做前缀比较，
// 前缀用 filepath.Separator 拼。而 Fold 只在**不敏感卷**上把 "\" 归一成 "/"，
// 敏感卷上原样返回 ⇒ Windows 不敏感卷（NTFS 默认）上这条分支**恒不成立**
// （"同一棵树被重复遍历"），敏感卷上则靠"两边都带 \" 的巧合"成立。
// darwin/Linux 上 filepath.Separator 恰好也是 "/"，与 Fold 的产物同值，
// 所以本机**看不出问题**（登记里"平台无关，可在 Linux 主门禁跑出两棵"的前提已被
// E15 推翻）。⇒ 本文件的 V7 用 sep 注入 Windows 真值来复现真相。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/pathnorm"
)

func TestUnderKeyWindowsTruthIsSubroot(t *testing.T) {
	const sep = `\` // Windows 的分隔符真值（H6：参数注入，不靠 build tag）

	// 1) 不敏感卷形态：Fold 已把整串归一成 "/"，键天然在 "/" 空间里。
	if !pathnorm.Under("c:/a/b", "c:/a") {
		t.Fatal("不敏感卷形态（c:/a/b 在 c:/a 下）未判出子树")
	}

	// 2) 敏感卷形态：Fold 原样返回带 "\" 的串，归一必须由 Slash 完成。
	//    M-M26-a（Slash 不归一）的靶子就是这两条等值断言。
	if got := pathnorm.Slash(`C:\a\b`, sep); got != "C:/a/b" {
		t.Fatalf("Slash 未把分隔符归一到键空间：%q", got)
	}
	if got := pathnorm.Slash(`C:\a`, sep); got != "C:/a" {
		t.Fatalf("Slash 未把分隔符归一到键空间：%q", got)
	}
	if !pathnorm.Under(pathnorm.Slash(`C:\a\b`, sep), pathnorm.Slash(`C:\a`, sep)) {
		t.Fatal("Windows 敏感卷形态（C:\\a\\b 在 C:\\a 下）未判出子树——这就是 M26 的现场")
	}
	// 自身也算"在其下"（rootsUnder 的语义：含 dir 自身）
	if !pathnorm.Under(pathnorm.Slash(`C:\a`, sep), pathnorm.Slash(`C:\a`, sep)) {
		t.Fatal("键等于根时应判为真（rootsUnder 含自身）")
	}

	// 3) 兄弟用例：只是名字前缀相同 ≠ 子树。
	//    M-M26-c（用 strings.Contains 代替前缀）的靶子。
	if pathnorm.Under(pathnorm.Slash(`C:\ab`, sep), pathnorm.Slash(`C:\a`, sep)) {
		t.Fatal("C:\\ab 被误判成 C:\\a 的子树（前缀判据退化成包含判据）")
	}
	if pathnorm.Under(pathnorm.Slash(`C:\a`, sep), pathnorm.Slash(`C:\ab`, sep)) {
		t.Fatal("反向也误判：C:\\a 不该落在 C:\\ab 之下")
	}
	if pathnorm.Under("c:/ab", "c:/a") {
		t.Fatal("不敏感卷形态同样不许把 c:/ab 当成 c:/a 的子树")
	}
}

// 负控制：判据搬进 "/" 空间之后，本机**真实的**嵌套根必须照旧并成一棵。
// 这条挡的是"修 M26 把 darwin/Linux 的行为改坏"（键空间换了写法，重跑一遍真夹具）。
func TestDedupeRootsStillMergesNestedNativeRoots(t *testing.T) {
	base := t.TempDir()
	wide := filepath.Join(base, "a")
	narrow := filepath.Join(base, "a", "b")
	if err := os.MkdirAll(narrow, 0o755); err != nil {
		t.Fatal(err)
	}

	// 故意倒序传入：合并结果与入参顺序无关（dedupeRoots 内部先排序）
	kept, all, _, _, _, _ := dedupeRoots(context.Background(), []string{narrow, wide}, false)

	if len(kept) != 1 || kept[0] != wide {
		t.Fatalf("本机真实嵌套根未并成一棵（只该留宽根）：kept=%v want [%s]", kept, wide)
	}
	// all 是**去重前**的全部规范化根（M6-P4 的逃逸判据要用它），顺序为排序后
	if len(all) != 2 || all[0] != wide || all[1] != narrow {
		t.Fatalf("all 应为去重前的全部规范化根：%v", all)
	}
}
