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

// ★ 2026-09-20（ocr M4）身份查询只要求**属性读**权限。
//
// 修正前 FromPath / FromPathNoFollow 都带 GENERIC_READ(0x80000000)：它额外
// 要求 FILE_READ_DATA。于是「另一进程正以FILE_SHARE_READ|WRITE（不含 READ）
// 持有该文件」「ACL/EFS 允许读属性但拒绝读数据」这两类路径上 CreateFileW
// 本可成功拿到 (卷号, 索引)，却因权限要得过多而失败。更糟的是调用方把
// 这个 error 解读成"路径变了/链接悬空"（identityStill 一律拒绝、
// verifySymlinked 报「目标不可达」），把一个权限事实说成了存在性事实。
func TestIdentityAccessMaskIsMinimal(t *testing.T) {
	const fileReadAttributes = 0x80 // 唯一需要的权限：读属性
	if m := identityAccessMask; m != fileReadAttributes {
		t.Fatalf("身份查询的 DesiredAccess = %#x，应恰为 FILE_READ_ATTRIBUTES(%#x)；"+
			"含 GENERIC_READ 会在「数据被拒读/被无共享读占用」的路径上误报不可达",
			m, fileReadAttributes)
	}
}

// TestIdentitySurvivesWriteLockedFile 上面的契约在真机上的体现：
// 另一进程以「只允许写、不允许再有人读数据」的方式占用文件时，
// 身份查询仍须成功——因为只有属性读是必需的。
//
// 这是回归的**行为**证明；上一用例是常量契约（本机无法构造占用时兜底）。
func TestIdentitySurvivesWriteLockedFile(t *testing.T) {
	dir := t.TempDir()
	p := mkTempFile(t, dir, "locked.bin")
	f, err := os.OpenFile(p, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Go 的 O_WRONLY 映射为 GENERIC_WRITE | FILE_SHARE_READ|WRITE|DELETE，
	// 恰好**不**授予后来者 FILE_READ_DATA 的共享权。
	if _, err := FromPathNoFollow(p); err != nil {
		t.Fatalf("写占用下的身份查询失败（修正前 GENERIC_READ 即在此误报）: %v", err)
	}
	if _, err := FromPath(p); err != nil {
		t.Fatalf("FromPath 同样只需属性读，失败: %v", err)
	}
}
