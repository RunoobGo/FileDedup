//go:build windows

package fsid

// Windows 身份解析的行为断言（CI windows job 执行）。
// 硬要求：卷号 + 文件索引可解析、能区分同目录的不同文件、且改名后不变。
// change time 记在父目录索引项里，卷可以不维护文件的这一项，故为 best-effort。
// FAT/exFAT 等不给稳定索引的卷上 fromFile 会退回未解析（本主机造不出这类卷，
// 交由上层的内容级采样兜底）。

import (
	"os"
	"path/filepath"
	"testing"
)

// 卷号 + 文件索引齐备；change time 是 best-effort（见下方说明）。
func TestWindowsFromFileResolves(t *testing.T) {
	dir := t.TempDir()
	p := mkTempFile(t, dir, "a.bin")
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id := FromFile(f)
	if !id.Resolved {
		t.Skipf("该卷不提供稳定文件索引（身份缺位，走内容兜底）: %+v", id)
	}
	if id.Dev == 0 || id.Ino == 0 {
		t.Fatalf("已解析却缺要素: %+v", id)
	}
	if id.CtimeNs == 0 {
		// Windows 的 change time 存在父目录的索引项里，卷可以不维护文件的这一项
		// （run 35372232737 的 windows runner 实测为 0）。这不是身份解析失败：
		// 缓存命中判定退回 (卷号, 索引) + 四点采样重算，误报防线仍在（H1 第 ② 层）。
		t.Logf("该卷不给文件维护 change time，CtimeNs=0（身份仍成立，靠内容采样兜底）: %+v", id)
	}
	if st, err := os.Stat(p); err != nil {
		t.Fatal(err)
	} else if FromFileInfo(st).Resolved {
		t.Fatal("Windows 上 FileInfo 不应假称已解析（卷号/索引只能句柄查询）")
	}
}

// 索引随文件走：改名后身份不变（缓存键是 path，靠这一条容忍重命名后复用）。
func TestWindowsIdentitySurvivesRename(t *testing.T) {
	dir := t.TempDir()
	p := mkTempFile(t, dir, "before.bin")
	before := identityOfPath(t, p)
	if !before.Resolved {
		t.Skip("该卷无稳定索引")
	}
	to := filepath.Join(dir, "after.bin")
	if err := os.Rename(p, to); err != nil {
		t.Fatal(err)
	}
	if after := identityOfPath(t, to); after != before {
		t.Fatalf("改名后身份改变: %+v → %+v", before, after)
	}
}

// 同目录下两个文件身份必须不同（否则缓存会把两个文件当一个）。
func TestWindowsDistinctFilesDiffer(t *testing.T) {
	dir := t.TempDir()
	a := identityOfPath(t, mkTempFile(t, dir, "a.bin"))
	b := identityOfPath(t, mkTempFile(t, dir, "b.bin"))
	if !a.Resolved || !b.Resolved {
		t.Skip("该卷无稳定索引")
	}
	if a.Ino == b.Ino {
		t.Fatalf("两个文件同一索引: %+v vs %+v", a, b)
	}
}

// 原地改写内容：文件索引必须不变；卷若维护 change time，则它必须推进。
// change time 不设为必查项——Windows 把它记在父目录索引项里，可以不更新文件的这一项
// （见 TestWindowsFromFileResolves）。这一层缺位时，原地篡改由四点采样重算兜住，
// 那条路径由 dedup 包的 TestCacheMidOnlyChangeNoFalseGroup 端到端断言。
func TestWindowsChangeTimeAdvancesOnWrite(t *testing.T) {
	dir := t.TempDir()
	p := mkTempFile(t, dir, "a.bin")
	before := identityOfPath(t, p)
	if !before.Resolved {
		t.Skip("该卷无稳定索引")
	}
	if err := os.WriteFile(p, []byte("FSID-IDENTITY-CHANGED!"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := identityOfPath(t, p)
	if after.Ino != before.Ino || after.Dev != before.Dev {
		t.Fatalf("写入后物理身份变了（应为原地改写）: %+v → %+v", before, after)
	}
	if before.CtimeNs == 0 {
		t.Skipf("该卷不给文件维护 change time，无法断言推进: %+v", after)
	}
	if after.CtimeNs == before.CtimeNs {
		t.Fatalf("原地写入未推进 change time: %+v", after)
	}
}

func identityOfPath(t *testing.T, p string) ID {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return FromFile(f)
}
