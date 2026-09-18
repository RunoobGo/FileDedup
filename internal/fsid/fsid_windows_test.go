//go:build windows

package fsid

// Windows 身份解析的行为断言（CI windows job 执行）。
// 覆盖 NTFS 上的三条性质；FAT/exFAT 等不给稳定索引的卷上 fromFile 会退回
// 未解析（本主机造不出这类卷，交由上层的内容级采样兜底）。

import (
	"os"
	"path/filepath"
	"testing"
)

// 卷号 + 文件索引 + change time 三者齐备。
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
		t.Fatalf("change time 未取到: %+v", id)
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

// 原地改写内容 → change time 推进（无 ctime 语义的 Windows 上唯一的时间证据）。
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
	if after.CtimeNs == before.CtimeNs {
		t.Fatalf("原地写入未推进 change time: %+v", after)
	}
	if after.Ino != before.Ino {
		t.Fatalf("写入后文件索引变了: %+v → %+v", before, after)
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
