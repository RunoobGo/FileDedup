//go:build windows

package fsid

// M271：Windows 上 change time 从未取到过——`fileBasicInfoClass` 写成 1
// （那是 FILE_STANDARD_INFO），真 FILE_BASIC_INFO 是 0。错常量下按 basicInfo 布局
// 解释 FILE_STANDARD_INFO 的字节，偏移 24~31 落在 Directory 标志 + 补齐上，
// 于是 CtimeNs 恒 0，`ticksToUnixNs(0)` 又走 `if ticks == 0 return 0`。
//
// 为什么这一格必须是 Fatalf 而不是 Logf：带着错常数活到本轮的全部原因，就是
// `fsid_windows_test.go` 对 0 只 t.Logf、对 0 时直接 t.Skip——判据层全绿、平台腿
// 失明。改完常数若不把断言钉上，下一次漂回去照样没人知道。

import (
	"os"
	"testing"
	"time"
	"unsafe"
)

// win32FileBasicInfo 在本文件里**另写一遍**、不引用被测常数，是刻意的：
// 用它自己的常数验它自己的常数，恒真、抓不到任何东西。
const win32FileBasicInfo = 0

func basicInfoAt(t *testing.T, f *os.File, class uintptr) basicInfo {
	t.Helper()
	var bi basicInfo
	r, _, e := procGetFileInformationByHandleEx.Call(f.Fd(), class,
		uintptr(unsafe.Pointer(&bi)), unsafe.Sizeof(bi))
	if r == 0 {
		t.Skipf("该卷拒绝 class=%d 的查询（%v），无从断言", class, e)
	}
	return bi
}

// change time 在能给出稳定索引的卷上必须**非零**，且落在可信时间区间内。
//
// 第二条断言不是多余的：错常量那种形状是"非零但全是别的字段"（把 allocSize
// 当时间戳读），只判 `!= 0` 等于把"读对了"与"读错了字段"放在一起放行。
func TestWindowsChangeTimeIsResolvedAndPlausible(t *testing.T) {
	dir := t.TempDir()
	p := mkTempFile(t, dir, "a.bin")
	id := identityOfPath(t, p)
	if !id.Resolved {
		t.Skipf("该卷不提供稳定文件索引，change time 无对照意义: %+v", id)
	}
	if id.CtimeNs == 0 {
		t.Fatalf("CtimeNs=0 ⇒ change time 从未取到（M271 的形状）：%+v", id)
	}
	lower := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	if id.CtimeNs < lower {
		t.Fatalf("CtimeNs 早于 2000-01-01，读到的不是 change time：%d（%s）",
			id.CtimeNs, time.Unix(0, id.CtimeNs).UTC().Format(time.RFC3339))
	}
	if upper := time.Now().Add(24 * time.Hour).UnixNano(); id.CtimeNs > upper {
		t.Fatalf("CtimeNs 晚于此刻 +24h，读到的不是 change time：%d（%s）",
			id.CtimeNs, time.Unix(0, id.CtimeNs).UTC().Format(time.RFC3339))
	}
}

// FromFile 交回的 change time 必须与**独立发起**的 FILE_BASIC_INFO(class=0) 查询
// 逐位相同，而且那次查询里四个时间不能出现"只有 change 为 0"的形状。
//
// ★ 后半句正是 M271 与旧解释（"change time 记在父目录索引项里，卷可以不维护文件的
// 这一项"）的分界：旧解释在 NTFS 上预言"create/write 有值、change 为 0"，实读是
// 四个全有值 ⇒ 归因是常数错，不是卷行为。
func TestWindowsChangeTimeMatchesBasicInfo(t *testing.T) {
	dir := t.TempDir()
	p := mkTempFile(t, dir, "a.bin")
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id := FromFile(f)
	if !id.Resolved {
		t.Skipf("该卷无稳定索引: %+v", id)
	}
	bi := basicInfoAt(t, f, win32FileBasicInfo)
	if bi.ChangeTime == 0 && bi.CreationTime != 0 && bi.LastWriteTime != 0 {
		t.Fatalf("class=0 也交出「create/write 有值而 change 为 0」⇒ 布局错位：%+v", bi)
	}
	if got := ticksToUnixNs(bi.ChangeTime); got != id.CtimeNs {
		t.Fatalf("FromFile 的 change time 与 class=0 现读不符：%d vs %d ⇒ 被测常数不是 FILE_BASIC_INFO",
			id.CtimeNs, got)
	}
}
