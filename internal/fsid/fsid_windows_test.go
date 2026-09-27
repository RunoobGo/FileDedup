//go:build windows

package fsid

// Windows 身份解析的行为断言（CI windows job 执行）。
// 硬要求：卷号 + 文件索引可解析、能区分同目录的不同文件、且改名后不变。
// change time 记在父目录索引项里，卷可以不维护文件的这一项，故为 best-effort。
// 〔2026-09-27 M271 更正〕上面这句 best-effort 的解释**在 NTFS 上不成立**：当时的
// 0 来自 `fileBasicInfoClass` 写错（1=FILE_STANDARD_INFO），不是卷不维护。常数已改对，
// change time 现为**硬要求**，判据在 `fsid_ctime_m271_test.go`（下面两处宽松断言留作
// 非 NTFS 卷的兜底，不再是唯一通道）。
// FAT/exFAT 等不给稳定索引的卷上 fromFile 会退回未解析（本主机造不出这类卷，
// 交由上层的内容级采样兜底）。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// 索引随文件走：改名后**物理身份**（卷号 + 文件索引）不变。
// ★ 2026-09-27（M306）删掉了本行原来的半句「缓存键是 path，靠这一条容忍重命名后复用」：
//
//	hash_cache 的主键就是 path（cache.go:215），改名后是新行，不存在"复用旧行"这条路径。
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
	after := identityOfPath(t, to)
	// ★ 2026-09-27（M306）：这里原本是 `after != before`（**整结构**相等，含 CtimeNs），
	//   M271 把 change time 真正读出来之后它开始红。现读归因（探针读数同批落盘
	//   `build/m7scratch/m306_ctime_rename_readings.txt`）：**NTFS 改名会推进 change time**
	//   （第一次改名实测 +514200 ns；写内容同样推进；两次纯查询之间恒定为 0，
	//   所以不是读数竞态）。⇒ "改名后 ctime 不变"这条前提本来就不成立，
	//   要钉的不变量是**身份两腿（卷号 + 文件索引）随文件走**，也就是 `SameIdentity`。
	//   改前这条用例为什么一直是绿的：错常数下 CtimeNs 恒 0，两侧都 0 ⇒ 整结构相等
	//   **平凡成立**——判据全绿掩盖平台腿失明，正是 M271 判据文件开头写的那个形状。
	//   这里刻意**不**改成"顺手放宽到只比 Dev/Ino 就完事"：见下面那条独立断言，
	//   改名后的 ctime 仍要落在可信区间（否则就是又读错了字段）。
	if !after.SameIdentity(before) {
		t.Fatalf("改名后身份（卷号+文件索引）改变: %+v → %+v", before, after)
	}
	if after.Resolved != before.Resolved {
		t.Fatalf("改名后解析状态改变: %+v → %+v", before, after)
	}
	// 身份两腿之外的 ctime 允许推进（NTFS 语义），但推进后的值仍必须是可信时间——
	// 这条把"改名把 ctime 读成了别的字段"与"改名推进了 ctime"分开：前者是缺陷，后者是事实。
	if after.CtimeNs != 0 {
		lower := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
		if after.CtimeNs < lower || after.CtimeNs > time.Now().Add(24*time.Hour).UnixNano() {
			t.Fatalf("改名后的 CtimeNs 不在可信时间区间（读到的不是 change time）：%d（%s）",
				after.CtimeNs, time.Unix(0, after.CtimeNs).UTC().Format(time.RFC3339))
		}
	}
	if after.CtimeNs < before.CtimeNs {
		t.Logf("改名后 ctime 未推进（本卷/此刻的读数，非缺陷）：%d → %d", before.CtimeNs, after.CtimeNs)
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

// 原地改写内容：文件索引必须不变，且 change time 必须推进。
//
// 〔2026-09-27 M271 更正〕本用例开头原先写着"change time 不设为必查项——Windows 把它
// 记在父目录索引项里，可以不更新文件的这一项"。那句**解释**在 NTFS 上不成立（0 来自
// `fileBasicInfoClass` 写错，不是卷不维护），推进与否的真机读数见下面 M306 那段。
//
// ★ 2026-09-27（M306）：改前这里是"写一次 → 现读 → 必须不同"，**在连续跑全量时会偶发红**
//
//	（实测 8 次里 4 次未推进；单跑 `go test ./internal/fsid/` 则是绿的——这就是它躲过
//	本轮前面所有绿的原因）。真机读数（探针 `$TEMP/fdd_probe_tick`，同批落盘
//	`build/m7scratch/m306_ctime_rename_readings.txt`）：
//	  A 无间隔直接写  ：未推进 4/8      ← NTFS 的时间戳有刻度，同一刻度内的两次操作读出同一个值
//	  B 间隔 1ms 后写 ：未推进 0/8
//	另测：两次纯查询之间差值恒为 0 ⇒ 读数本身稳定，不是竞态也不是缓存。
//	（探针还有一组"把 ctime 设到过去再写"，`SetFileTime` 在只持 FILE_READ_ATTRIBUTES 的
//	句柄上 8/8 交回 Access is denied —— 那一臂**没有读数**，别引成依据。）
//	⇒ 这是**刻度**问题，不是产品回归，也不是"该卷不维护 change time"。
//	修法取"轮询直到推进"而不是 `time.Sleep(1ms)`：后者是把刻度猜成一个数，
//	换一台机器/runner 就可能又红；轮询保留的是真命题——**写入终会推进 ctime**，
//	常数若漂回 1（永远读出 0）则 40 次轮询后照样红，判据强度没有被稀释。
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
	// 轮询到推进为止（上限 40 次 × 2ms）：见函数头 M306 的读数。
	// 每次都用**不同的内容**改写，保证"没推进"不是因为写入了同样的东西。
	if after.CtimeNs == before.CtimeNs {
		for i := 0; i < 40 && after.CtimeNs == before.CtimeNs; i++ {
			time.Sleep(2 * time.Millisecond)
			body := []byte("FSID-IDENTITY-CHANGED! retry")
			if err := os.WriteFile(p, append(body, byte(i)), 0o644); err != nil {
				t.Fatal(err)
			}
			after = identityOfPath(t, p)
			if after.Ino != before.Ino || after.Dev != before.Dev {
				t.Fatalf("轮询写入过程中物理身份变了: %+v → %+v", before, after)
			}
		}
	}
	if after.CtimeNs == before.CtimeNs {
		t.Fatalf("原地写入未推进 change time（轮询 40 次仍相同，M271 的形状）: %+v", after)
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
