//go:build windows

package realbytes

// Reported 的 Windows 真机三臂（M295，2026-09-27 实施批）。
//
// ★ 这三臂本身修前修后都绿——它们的价值不是证明修复生效，而是**把真机读数钉进仓库**：
//   M295 的登记行点名"本包 Windows 腿零测试（同 M104 形状），无 tag 层只钉住
//   『读不到⇒回退且 known=false』，钉不住**该不该**判成读不到"。
//   "该不该判成读不到"这一半由 realbytes_gle_test.go 的判据表钉（那才是修前必红的格子）；
//   这里钉的是另外两侧：合法文件不许被误判读不到、真失败必须判读不到、
//   以及旧注释声称"目录是常见失败之一"这件事**在真机上不成立**。
//   有了这三臂，将来任何人改动这里的成功判据都会立刻撞上真机数字，而不是撞上注释。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReportedRegularFileArm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	payload := make([]byte, 8192)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	reported, known := Reported(path, info)
	// 合法文件**必须**判"读到了"。这一臂就是 M295 的正面：判据不许只看返回值。
	if !known {
		t.Fatalf("普通文件被判读不到（known=false）⇒ 实占将回退成逻辑大小并标未知：%s", path)
	}
	if reported == 0 {
		t.Fatalf("普通文件的实占读成 0（known=true）：%s", path)
	}
	t.Logf("真机读数：逻辑 %d B / 实占 %d B（簇对齐与压缩都体现在这个数里）", len(payload), reported)
}

func TestReportedMissingPathArm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.bin")
	info, err := os.Stat(path)
	if err == nil {
		t.Fatal("夹具前提不成立：不存在的路径居然 stat 成功")
	}

	// Reported 在源不存在时 stat 都拿不到，调用方不会走到这里；
	// 这一臂钉的是**API 层**那一侧：真失败必须判失败（旧写法与恒真式撞在同一个读数上，
	// 正因为失败值 0xFFFFFFFF 与合法值重叠，只有 gle 能分开）。
	reported, known := Reported(path, info)
	if known {
		t.Fatalf("不存在的路径被判 known=true（reported=%d）⇒ 二选一判据失效", reported)
	}
	if reported != 0 {
		t.Fatalf("失败臂必须交回 0，实得 %d", reported)
	}
}

// 目录臂：M295 登记行点名"旧注释把目录列为常见失败之一，真机读数不然"。
//
// ★ 这里钉的是**运行时事实**，不是我们希望的语义：目录交回 low=0x0 + gle=Errno(0)，
// 于是本函数对它读到 (0, true)。之所以可以接受：目录在遍历阶段就被 internal/scanner
// 的 IsDir / !IsRegular 两道挡在 Reported 之前，所以这条臂在生产路径上不可达。
// 把它钉下来，是为了让"注释说目录是失败臂"这件事不能再悄悄回来——
// 注释与运行时不符正是 M295 的一半成因。
func TestReportedDirectoryArm(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	reported, known := Reported(dir, info)
	t.Logf("真机读数：目录臂 reported=%d known=%v（旧注释声称这是失败臂之一，现读不然）", reported, known)
	if known != true || reported != 0 {
		t.Fatalf("目录臂读数漂了：现钉 (0,true)，实得 (%d,%v) ⇒ 平台语义变化，"+
			"需同步重读 realbytes_windows.go 里那段〔更正一处与运行时不符的旧列举〕注释", reported, known)
	}
}
