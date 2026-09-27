//go:build windows

package ops

// M265 / M270 的 **Windows 接线腿**——判据本体在 `recycle_policy.go`（无 tag，
// Linux CI 主门禁跑它），本文件钉的是"Windows 这一侧到底有没有去用"：
//
//	① 预检必须真的调用长度判据（M265：>259 码元的路径交给 Shell 会被静默永久删除，
//	   且 `LongPathsEnabled` 开了也一样——§6.45 二·一九档同读数）。
//	② 事后复核必须真的消费一次扫描的两份读数（M270：`SHQueryRecycleBinW` 本机四卷
//	   恒 `ok=false` ⇒ 判据 2 每卷都被 `continue` 旁路，"报成功而复核一声未响"）。
//	③ 一次枚举、两处消费：`$Recycle.Bin` 只能扫一遍，dst 填充和降级判据共用同一份
//	   读数。扫两遍不只是慢（M197 那批判据不接受并发正是因为它跑在真实卷上），
//	   更会因为两趟之间被别的进程清理而给出**互相矛盾**的读数。
//
// ★ 引用纪律：本文件是 Windows 真机上的判据接线，不折算 §6.45 那些梯度的真机读数。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// m265LongTempPath 造一条**只靠长度**触发的路径：卷根用本机临时目录（盘符不写死，
// 否则换一台只有 C 盘的机器就误判），后面拼到超过 260 码元。
func m265LongTempPath(t *testing.T, units int) string {
	t.Helper()
	dir := t.TempDir()
	need := units - pathUnits(dir) - 1 - 5 // 减去目录前缀、一个分隔符、"x.bin"
	if need <= 0 {
		t.Skipf("临时目录本身已经很长（%d 码元），造不出 %d 码元的对照夹具", pathUnits(dir), units)
	}
	// 全用 ASCII：让"字节 = 码元"，夹具长度可手算复核。
	return filepath.Join(dir, strings.Repeat("a", need)+"x.bin")
}

// ① 预检层：超长路径必须在**交给 Shell 之前**就被拒。
func TestM265WindowsPreflightRejectsOverlongPath(t *testing.T) {
	long := m265LongTempPath(t, 260)
	if got := pathUnits(long); got < 260 {
		t.Fatalf("夹具长度错：%d 码元，未到真机失效点", got)
	}
	why := recyclableReason(long)
	if why == "" {
		t.Fatal("260 码元路径通过了预检 ⇒ 会被 Shell 静默永久删除（M265 原样回来）")
	}
	if !strings.Contains(why, "静默永久删除") {
		t.Fatalf("拒绝原因没说清后果：%s", why)
	}

	// 反向钉：边界内侧不得误拒。真机 259 码元那一格是**能进站**的。
	short := m265LongTempPath(t, 259)
	if got := pathUnits(short); got > 259 {
		t.Fatalf("对照夹具长度错：%d 码元", got)
	}
	if why := longPathDropReasonFor(short); why != "" {
		t.Fatalf("259 码元被预检拒了（真机能进站）：%s", why)
	}
}

// ① 的接线锚：`recyclableReason` 必须真的喊到长度判据。
// 只测行为不够——预检有卷型/策略/容量三层，我把长度那层整段删掉，
// 短路径的行为测试仍然全绿，而超长路径又只在真机才暴露（M265 的原始发现方式）。
func TestM265PreflightIsWiredIntoRecyclableReason(t *testing.T) {
	raw, err := os.ReadFile("trash_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "func recyclableReason(")
	if i < 0 {
		t.Fatal("找不到 recyclableReason ⇒ 锚的扫描面对不上，先重读结构")
	}
	body := src[i:]
	if j := strings.Index(body, "\n// recycleBinCapacityReason"); j > 0 {
		body = body[:j]
	}
	if !strings.Contains(body, "longPathDropReasonFor(") {
		t.Fatal("recyclableReason 里没有长度判据 ⇒ M265 的预检层又只剩卷型/策略/容量三层")
	}
	if !strings.Contains(body, "M265") {
		t.Fatal("接线处没有了 M265 的锚 ⇒ 下一位读者无从知道这层判据为什么不能删")
	}
}

// ② 降级腿：源已消失、`SHQueryRecycleBinW` 给不出基准、而 `$Recycle.Bin` 里
// **有**本轮进站痕迹 → 必须判通过（这正是 M270 在本机的常态：四卷恒读不到条目数）。
func TestM270VerifyPassesOnEvidenceWithoutBaseline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gone-but-recycled.bin") // 从未创建 = 源已消失
	exp := expectedRecycledPerVolume([]string{p})
	if exp[toWinRoot(p)] != 1 {
		t.Fatalf("夹具的按卷预期错：%v", exp)
	}
	ev := recycleEvidence{
		found:   map[string]string{p: `C:\$Recycle.Bin\S-1-5-21-1\$Rwhatever.bin`},
		scanned: map[string]bool{toWinRoot(p): true},
	}
	if err := verifyRecycled([]string{p}, exp, RBState{}, ev); err != nil {
		t.Fatalf("逐条对上了账却仍报错：%v", err)
	}
}

// ② 降级腿的反面：源已消失、无基准、**一条进站痕迹都没有** → 必须报错。
//
// ★ 这一条推翻了 `TestVerifyRecycledGoneSourceNoSnapshotOK`（2026-09-20 写下）的期望：
// 那条测试把"无快照 → 判据 2 跳过 → 不报错"当成保守正确行为，而那**正是 M270 的事故本体**
// （真机形状：`err=<nil>` + `before=map[] after=map[] expected=map[F:\:3]`）。
// 保守的方向搞错了：拿不到证据不等于有证据，静默放行才是最大的风险。
func TestM270VerifyReportsUnusableReviewWhenNoEvidenceChannel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gone-and-blind.bin")
	exp := expectedRecycledPerVolume([]string{p})
	err := verifyRecycled([]string{p}, exp, RBState{}, recycleEvidence{})
	if err == nil {
		t.Fatal("既无条目数基准、也无进站痕迹却判通过 ⇒ 又是一次静默旁路（M270 原形状）")
	}
	if !strings.Contains(err.Error(), "复核不可用") {
		t.Fatalf("取证通道本身失效时必须说「复核不可用」：%v", err)
	}
	// ★ 不许把它报成数据已丢：我们只是看不见证据（M279 那类假警报的成因）。
	if strings.Contains(err.Error(), "永久删除") {
		t.Fatalf("把「取不到证据」报成「数据已丢」：%v", err)
	}
}

// ② 降级腿的第三条臂：目录读得到、账却对不上 → 报"静默永久删除"，并说清缺几条。
func TestM270VerifyReportsPartialLossPerEntry(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	exp := expectedRecycledPerVolume([]string{a, b})
	root := toWinRoot(a)
	if exp[root] != 2 {
		t.Fatalf("夹具的按卷预期错：%v", exp)
	}
	// 两处都必须报不出"仍存在于原路径"：判据 1 与降级腿是两条独立的话。
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("夹具文件必须不存在（源已消失）：%v", err)
		}
	}
	ev := recycleEvidence{
		found:   map[string]string{a: `C:\$Recycle.Bin\S-1-5-21-1\$Ra.bin`},
		scanned: map[string]bool{root: true},
	}
	err := verifyRecycled([]string{a, b}, exp, RBState{}, ev)
	if err == nil {
		t.Fatal("2 条预期只有 1 条有痕迹 ⇒ 必须报错")
	}
	if !strings.Contains(err.Error(), "永久删除") || !strings.Contains(err.Error(), "缺 1 条") {
		t.Fatalf("报错没说清后果与缺口：%v", err)
	}
	// 判据 1 不得抢话：源都没了，不该出现"仍存在于原路径"。
	if strings.Contains(err.Error(), "仍存在于原路径") {
		t.Fatalf("降级腿混进了判据 1 的措辞：%v", err)
	}
}

// ③ 接线锚：`$Recycle.Bin` 的枚举在 `defaultTrashLocked` 里**恰好一次**，
// 且两处消费（dst 合并、事后复核）都吃同一份读数。
func TestM270ScansRecycleBinExactlyOnce(t *testing.T) {
	raw, err := os.ReadFile("trash_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "func defaultTrashLocked(")
	if i < 0 {
		t.Fatal("找不到 defaultTrashLocked")
	}
	body := src[i:]
	if j := strings.Index(body, "\n// recyclableReason"); j > 0 {
		body = body[:j]
	}
	if n := strings.Count(body, "scanRecycleBin("); n != 1 {
		t.Fatalf("scanRecycleBin 在 defaultTrashLocked 里出现 %d 次，期望恰好 1 次（一次枚举两处消费）", n)
	}
	if !strings.Contains(body, "verifyRecycled(") {
		t.Fatal("事后复核不再消费扫描读数 ⇒ 降级腿又回到了没有输入的状态")
	}
	// 扫描必须发生在 Shell 那一腿**之后**：在它之前扫得到的痕迹属于上一轮。
	shell := strings.Index(body, "procSHFileOperation.Call")
	scan := strings.Index(body, "scanRecycleBin(")
	if shell < 0 || scan < 0 || scan < shell {
		t.Fatalf("扫描未在 SHFileOperation 之后（shell=%d scan=%d）", shell, scan)
	}
}

// ③ 的一次枚举必须是**幂等**的：同一批路径被两处消费时不得出现两个数。
// 这里用一份临时"回收站"目录跑 `scanRecycleBin`，只验读数形状，不碰真回收站。
func TestM270ScanReportsPerVolumeReadability(t *testing.T) {
	root := t.TempDir()
	sep := string(filepath.Separator)
	now := time.Now()
	cand := filepath.Join(root, "dup", "a.bin")
	_, dst := putIndexFile(t, filepath.Join(root, recycleBinDirName, "S-1-5-21-mine"), "SCAN111", cand, now)
	blind := t.TempDir() // 没有 $Recycle.Bin 的卷根

	found, scanned := scanRecycleBin([]string{root + sep, blind + sep}, []string{cand}, now)
	if found[cand] != dst {
		t.Fatalf("扫描没交回进站证据：%v", found)
	}
	// `scanned` 的键**就是调用方交进来的那个卷根串**：Windows 侧先用 toWinRoot 规范化再传进来，
	// 而 `expected` 的键出自同一把尺子——换键形状会让降级腿查不到卷，把有证据的卷报成盲点。
	if !scanned[root+sep] {
		t.Fatalf("读到了目录却没记为可复核：%v", scanned)
	}
	if scanned[blind+sep] {
		t.Fatal("没有 $Recycle.Bin 的卷被记成「可复核」⇒ 降级腿会把盲点当证据")
	}
}
