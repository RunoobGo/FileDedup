//go:build !windows

package main

// 第八轮审查批 2（拟 M361）：固定名被"不属于本应用的对象"占着时**不许覆盖**。
//
// ★ 为什么这一格带 `!windows`（§2.5 第 2 条）：判据要造的是"固定名位置上装的
// 不是我们的东西"，本机最省事的形状是符号链接；而 Windows 上 os.Symlink 无特权
// 会失败，硬造出来的读数无法归因。Windows 腿由 CI 跑到"常规文件那一档"为止，
// 真正的"陌生对象占位"这一格挂 docs/05 真机清单。
//
// ★ 另一档本机造不出来的形状（属主不同的常规文件）需要 root 才能 chown ⇒ 那一臂
// 走属主接缝模拟，本机端到端未兑现，同样记进 §6.67 的未兑现表。

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// P-16：固定名上是符号链接 ⇒ 链接自身与其目标一字不动，快照另落并如实报出。
//
// 改前真读数（预测）：os.Rename 把**链接本身**顶掉（目标内容不受影响），
// 用户看到的是一份"看起来是备份"的文件，而那条链接指向的东西还在原地等着被引用；
// 若占位的不是链接而是别人放在共享目录里的真文件，被销毁的就是别人的文件
// ——§6.63 记过一次的同一条 P0 形状。
func TestM361SymlinkOccupantIsNeverClobbered(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 70)

	snapPath := filepath.Join(a.cfgDir, CacheClearSnapshotName)
	elsewhere := filepath.Join(t.TempDir(), "someone-else.db")
	if err := os.WriteFile(elsewhere, []byte("第三方内容，一字不许动"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, snapPath); err != nil {
		t.Fatalf("夹具造符号链接失败（本机不支持？本格读数交不回 CI）：%v", err)
	}

	res, err := a.CacheClear()
	if err != nil {
		t.Fatalf("M361：固定名被陌生对象占着时清空整个失败了 —— 该另落而不是取消：%v", err)
	}
	// ① 链接自身仍在（改名没顶掉它）
	li, lerr := os.Lstat(snapPath)
	if lerr != nil {
		t.Fatalf("M361：固定名上的符号链接被改名顶掉了（本应用不认领不属于它的对象）：%v", lerr)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("M361：固定名上不再是链接而是 %v", li.Mode())
	}
	// ② 链接指向的内容一字未动
	body, rerr := os.ReadFile(elsewhere)
	if rerr != nil || string(body) != "第三方内容，一字不许动" {
		t.Errorf("M361：快照落位改写了链接目标的字节（err=%v body=%q）", rerr, body)
	}
	// ③ 回执报的是**真实落点**，不是那个它根本没占住的名字
	if res.SnapshotPath == snapPath {
		t.Errorf("M361：回执声称快照在固定名，而那份对象根本不是它 —— 用户按回执去找回时只会找回别人的文件")
	}
	if res.SnapshotPath == "" {
		t.Error("M361：回执没给快照落点")
	}
	if _, serr := os.Stat(res.SnapshotPath); serr != nil {
		t.Errorf("M361：回执里的落点读不到（%s）：%v", res.SnapshotPath, serr)
	}
	// ④ 另落的那份必须是可用影像（不是 0 字节占位）
	if st, terr := os.Stat(res.SnapshotPath); terr == nil && st.Size() == 0 {
		t.Errorf("M361：另落的快照是 0 字节占位，防不住任何误删")
	}
	// ⑤ note 必须非空（P-16 判据的这一格）：确认框里跟用户承诺的是 cache-backup.db，
	//    这次没落在那儿，不另说一句就是让回执说谎。
	if res.SnapshotNote == "" {
		t.Error("M361：快照没落在承诺的固定名上，回执却没带一句说明")
	}
}

// P-17：固定名上是**属主不是本机用户**的常规文件 ⇒ 同样不许覆盖，另落。
//
// ★ 为什么这一格走接缝而不是真造：没有 root 就 chown 不出"别人的文件"（本机
//
//	`os.Chown(path, 0, 0)` 直接 EPERM）。把"这份是不是我们的"这一问留成接缝
//	（snapshotProvablyOurs）之后，这一臂能在本机钉住**编排**那一段——判据返回 false 时
//	必须走另落、必须不碰固定名上那份、必须在回执里说清楚；而"真能认出属主不同"
//	那一臂本机未兑现，记进 §6.67 未兑现表（判据本体另有 P-15/P-16/P-18 三格钉着）。
func TestM361ForeignOwnedRegularFileIsNotClobbered(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 55)

	snapPath := filepath.Join(a.cfgDir, CacheClearSnapshotName)
	foreign := "这份文件属于另一个用户，一个字节都不许动"
	if err := os.WriteFile(snapPath, []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := snapshotProvablyOurs
	var sawRegular bool
	snapshotProvablyOurs = func(st fs.FileInfo) bool {
		// 前提自检：夹具给的必须真是常规文件，否则这一格红在夹具而不是判据。
		if !st.Mode().IsRegular() {
			t.Errorf("M361 夹具：交给属主判据的不是常规文件而是 %v", st.Mode())
		}
		sawRegular = true
		return false // 模拟"属主 ≠ 当前 euid"
	}
	t.Cleanup(func() { snapshotProvablyOurs = restore })

	res, err := a.CacheClear()
	if err != nil {
		t.Fatalf("M361（属主臂）：认领失败时清空整个失败了 —— 该另落而不是取消：%v", err)
	}
	if !sawRegular {
		t.Fatal("M361（属主臂）：属主判据没被走到（接缝没被调用），本用例前提走样")
	}
	if res.SnapshotPath == snapPath || res.SnapshotPath == "" {
		t.Errorf("M361（属主臂）：回执落点 = %s，认不出的那份不该被顶掉，另落名也没报回来", res.SnapshotPath)
	}
	if res.SnapshotNote == "" {
		t.Error("M361（属主臂）：另落了却没在回执里说一句")
	}
	body, rerr := os.ReadFile(snapPath)
	if rerr != nil || string(body) != foreign {
		t.Errorf("M361（属主臂）：固定名上那份陌生文件被改写了（err=%v body=%q）", rerr, body)
	}
}

// P-17b：属主判据**本体**的逐臂断言（不经过 CacheClear，直接喂 FileInfo）。
//
// ★ 这一格是 MU-8a 逼出来的：P-16/P-17/P-18 都只经由 landCacheSnapshot 用判据，
//
//	把 `provablyOwnedOnThisPlatform` 里的 uid 比对整条摘掉（MU-8a）三格照样绿——
//	因为 P-17 自己覆盖了接缝、P-16 红在模式位那一支、P-18 的 uid 本来就相等。
//	也就是说"认出属主不同"这句话在端到端那一臂本机造不出来（要 root），
//	但它**可以**在这里被证伪：喂一个 Sys() 里 uid≠euid 的 FileInfo，判据必须说不。
//	补上这一格之后 MU-8a 有格可红；仍未兑现的是"真有一份别人 uid 的文件在盘上"
//	那条端到端路径（记进 §6.67 未兑现表）。
type fixedStat struct {
	mode fs.FileMode
	sys  any
}

func (f fixedStat) Name() string       { return CacheClearSnapshotName }
func (f fixedStat) Size() int64        { return 4096 }
func (f fixedStat) Mode() fs.FileMode  { return f.mode }
func (f fixedStat) ModTime() time.Time { return time.Time{} }
func (f fixedStat) IsDir() bool        { return f.mode.IsDir() }
func (f fixedStat) Sys() any           { return f.sys }

func TestM361OwnershipProofArms(t *testing.T) {
	ours := uint32(os.Geteuid())
	cases := []struct {
		desc string
		in   fixedStat
		want bool
	}{
		{"常规文件 + 属主就是本机用户", fixedStat{mode: 0o600, sys: &syscall.Stat_t{Uid: ours}}, true},
		{"常规文件 + 属主是别人", fixedStat{mode: 0o600, sys: &syscall.Stat_t{Uid: ours + 1}}, false},
		{"常规文件 + 属主是 root", fixedStat{mode: 0o600, sys: &syscall.Stat_t{Uid: 0}}, ours == 0},
		{"符号链接（哪怕属主是我们）", fixedStat{mode: os.ModeSymlink | 0o777, sys: &syscall.Stat_t{Uid: ours}}, false},
		{"目录", fixedStat{mode: fs.ModeDir | 0o755, sys: &syscall.Stat_t{Uid: ours}}, false},
		{"Sys() 不是本平台的 Stat_t", fixedStat{mode: 0o600, sys: "不是 Stat_t"}, false},
		{"Sys() 是空的", fixedStat{mode: 0o600, sys: nil}, false},
	}
	for _, c := range cases {
		if got := provablyOwnedOnThisPlatform(c.in); got != c.want {
			t.Errorf("M361（属主判据）：%s ⇒ %v，want %v", c.desc, got, c.want)
		}
	}
}

// P-19：另落的那一份同样必须是 0600，且盘上只多这一份、不留 tmp 兄弟。
//
// ★ 这一格钉的是"档位收在 tmp 上、随改名一路带过去"这条链：如果实现只在固定名
// 落位之后补 Chmod，另落那条臂就会留下一份 0644 的隐私影像在很可能共享的目录里
// ——M353 记过的"承诺只兑了一半"换一副面孔回来。
// ★ 原设计里这一格写的是"另落名撞车 ⇒ 试下一个名"：本机预测不到纳秒时间戳名，
// 硬造撞名要引入时钟接缝，而 O_EXCL 循环本身已经把那一臂兜住 ⇒ 推翻原规划，
// 改钉成这条本机真能证伪的判据（追记见设计段 §2.6）。
func TestM361AsideSnapshotKeepsOwnerOnlyMode(t *testing.T) {
	a, cch := clearFixture(t)
	seedCache(t, cch, 40)

	snapPath := filepath.Join(a.cfgDir, CacheClearSnapshotName)
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.db"), snapPath); err != nil {
		t.Fatalf("夹具造符号链接失败：%v", err)
	}
	res, err := a.CacheClear()
	if err != nil {
		t.Fatalf("M361：另落那条臂整个失败了：%v", err)
	}
	if res.SnapshotPath == snapPath || res.SnapshotPath == "" {
		t.Fatalf("M361：回执落点不对（%s）", res.SnapshotPath)
	}
	li, lerr := os.Lstat(res.SnapshotPath)
	if lerr != nil {
		t.Fatalf("M361：回执落点读不到：%v", lerr)
	}
	if !li.Mode().IsRegular() {
		t.Errorf("M361：另落的不是常规文件而是 %v", li.Mode())
	}
	if perm := li.Mode().Perm(); perm != 0o600 {
		t.Errorf("M361：另落的快照模式位是 %o（这条臂也得是仅所有者可读写）", perm)
	}
	// 不留半截 tmp：目录里 cache-backup.db* 只许有链接 + 那一份另落影像
	matches, _ := filepath.Glob(filepath.Join(a.cfgDir, CacheClearSnapshotName+"*"))
	if len(matches) != 2 {
		t.Errorf("M361：cache-backup.db* 残留 %v（应为固定名上的链接 + 一份另落影像）", matches)
	}
	body, rerr := os.ReadFile(snapPath)
	_ = body
	if rerr == nil {
		t.Errorf("M361：固定名上的链接被改写成了普通文件（ReadFile 竟然读通了）")
	}
}
