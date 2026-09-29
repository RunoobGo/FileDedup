package cache

// M347/M348 修前探针（设计段 §1.5 / §2.3）：清空后磁盘占用必须真的回落，
// 且统计口径必须把 -wal 算进「占用」。
//
// ★ 两条体积探针刻意分开：实测证明「只加 VACUUM」这版修法能让「主库字节不变」
//   这件事继续存在（WAL 下 VACUUM 只重写 -wal），合成一条就会放走它。

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const probeEntries = 20000

// fillProbe 灌 probeEntries 条带 full 的条目，返回填满时的主库字节数。
func fillProbe(t *testing.T, c *Cache) int64 {
	t.Helper()
	return fillProbeN(t, c, probeEntries)
}

// fillProbeN 灌 n 条带 full 的条目，返回填满时的主库字节数。
func fillProbeN(t *testing.T, c *Cache, n int) int64 {
	t.Helper()
	entries := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		full := make([]byte, 32)
		entries = append(entries, Entry{
			Path: fmt.Sprintf("/very/long/path/to/file/number/%06d/name.bin", i),
			Size: uint64(i * 1024), MtimeNs: int64(i),
			Head: uint64(i), Tail: uint64(i * 3), Full: full,
		})
	}
	if err := c.Store(entries); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(c.path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// sideSize 读侧文件字节（不存在记 0）。
func sideSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// M347 主判据：Clear() 之后主库文件本身必须缩到填满时的 1/8 以下。
// 修前读数：4,300,800 → 4,300,800（一条字节都没少，比值 1.0）。
func TestClearReclaimsMainFileBytes(t *testing.T) {
	c := openTest(t)
	before := fillProbe(t, c)
	if before < 1<<20 {
		t.Fatalf("夹具没有造出可观测的库体积（%d B），本探针判据失效", before)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear 报错: %v", err)
	}
	if after := sideSize(t, c.path); after > before/8 {
		t.Errorf("M347：清空后主库未回收 —— before=%d after=%d（判据 < %d）", before, after, before/8)
	}
	st, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.DBSizeBytes > before/8 {
		t.Errorf("M347：统计页的「占用」未随清空回落 —— dbSizeBytes=%d before=%d", st.DBSizeBytes, before)
	}
}

// M347 第二层判据：WAL 必须被 checkpoint 截断。
// 只有 VACUUM、没有 wal_checkpoint(TRUNCATE) 时，本条单独红（实测主库仍 4,300,800、
// -wal 仍 4,375,472），所以它不许并进上一条。
func TestClearTruncatesWal(t *testing.T) {
	c := openTest(t)
	fillProbe(t, c)
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear 报错: %v", err)
	}
	if wal := sideSize(t, c.path+"-wal"); wal != 0 {
		t.Errorf("M347：清空后 -wal 未截断，仍占 %d 字节（回收效果整份留在 WAL 里）", wal)
	}
}

// M348：Store 之后不 checkpoint，占用必须把 -wal 计入。
// 修前读数：dbSizeBytes=4,300,800 == 主库单独字节，wal=4,375,472 完全没算。
func TestStatsIncludesWalBytes(t *testing.T) {
	c := openTest(t)
	main := fillProbe(t, c)
	wal := sideSize(t, c.path+"-wal")
	if wal == 0 {
		t.Skip("夹具本次没留下 -wal（WAL 已被自动截断），本格无从判定")
	}
	st, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if want := main + wal; st.DBSizeBytes < want {
		t.Errorf("M348：占用少报了 -wal —— 报 %d，主库 %d + wal %d = %d", st.DBSizeBytes, main, wal, want)
	}
}

// M348 反向格一：-wal 为空时占用恰好等于主库，不因侧文件虚增。
//
// 这一格用的是 Clear 之后的**真实形态**（实测 wal 被 TRUNCATE 成 0 字节、文件仍在）。
// ★ 取证记录：SQLite 在 WAL 模式下只要连接开着就会留下 -wal —— 连"只 Open 不写"都留
//
//	（第一版探针按"重开后就没有 wal"写，读数 57,712 B 直接把它打红）。
//	所以「侧文件根本不存在」对活句柄不是可达状态，那条宽松分支单列在下面直接问 helper。
func TestStatsWalEmptyEqualsMain(t *testing.T) {
	c := openTest(t)
	fillProbe(t, c)
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear 报错: %v", err)
	}
	if wal := sideSize(t, c.path+"-wal"); wal != 0 {
		t.Fatalf("夹具前提走样：Clear 后 -wal 应已截断成 0，实际 %d 字节", wal)
	}
	st, err := c.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if main := sideSize(t, c.path); st.DBSizeBytes != main {
		t.Errorf("M348：wal 为 0 时占用应等于主库 %d，实际 %d", main, st.DBSizeBytes)
	}
}

// M348 反向格二：侧文件缺失按 0 计入、不得报错（helper 单测，绕开"活句柄必有 wal"）。
func TestDiskUsageToleratesMissingFiles(t *testing.T) {
	dir := t.TempDir()
	// 主库都不存在 ⇒ 两档 stat 全失败 ⇒ 0，而不是 error（这一格是展示量，不值得让整个统计翻脸）
	c := &Cache{path: filepath.Join(dir, "absent.db")}
	if got := c.diskUsageLocked(); got != 0 {
		t.Errorf("M348：库族文件全缺时应记 0，实际 %d", got)
	}
	// 主库在、wal 不在 ⇒ 值等于主库
	if err := os.WriteFile(c.path, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := c.diskUsageLocked(); got != 4096 {
		t.Errorf("M348：无 -wal 时应等于主库 4096，实际 %d", got)
	}
}

// M347 假话防线（设计段 §1.5 第三格）：`PRAGMA wal_checkpoint` 撞占用时**不返回 error**，
// 它返回一行 busy=1。用 Exec 一发不管返回值，"没回收成功"就会被读成"回收成功"。
// ★ 这一格刻意**不用接缝伪造**：第二连接持一个不结完的读快照、本库再写入推进 WAL，
//
//	是真实复现出的 busy（实测 busy=1、-wal 停在 86,552 B），比伪造返回值贵一些但值得——
//	第一版探针在这里伪造了接缝，于是"checkpointFn 用 Exec 忽略 busy"这一版变异**全绿**（读数在册）。
func TestClearBusyDoesNotClaimSuccess(t *testing.T) {
	c := openTest(t)
	fillProbeN(t, c, 500)
	db2, err := sql.Open("sqlite", c.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	rows, err := db2.Query(`SELECT path FROM hash_cache`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("夹具前提走样：读快照一个游标都起不来")
	}
	// 本库继续写 ⇒ WAL 前进到读者快照之后，TRUNCATE 就截不动了。
	extra := make([]Entry, 0, 200)
	for i := 0; i < 200; i++ {
		full := make([]byte, 32)
		extra = append(extra, Entry{
			Path: fmt.Sprintf("/second/writer/%06d.bin", i), Size: uint64(i),
			MtimeNs: int64(i), Head: uint64(i), Tail: 1, Full: full,
		})
	}
	if err := c.Store(extra); err != nil {
		t.Fatalf("夹具写入报错: %v", err)
	}
	err = c.Clear()
	if !errors.Is(err, ErrReclaimFailed) {
		t.Errorf("M347：checkpoint 明显没做成（-wal 未截断），Clear 却没说\u201c回收未完成\u201d —— err=%v", err)
	}
	st, serr := c.GetStats()
	if serr != nil {
		t.Fatal(serr)
	}
	if st.Entries != 0 {
		t.Errorf("M347：回收失败被误报成清空失败的另一半 —— 条目本该已删净，实际剩 %d 条", st.Entries)
	}
	if wal := sideSize(t, c.path+"-wal"); wal == 0 {
		t.Log("注：本次 busy 腿竟也截断了 -wal，busy 判据的取证前提需在 CI 复核（不算失败）")
	}
}

// 哨兵文案自身的锚（M75(a) 同一条纪律：失败的那半截不能否认成功的那半截）。
// 变异：把 ErrReclaimFailed 写成"清空缓存失败"⇒ 本条红（改前它是绿的，
// 因为锚错挂在了内层 busy 说明串上，而那一串在改文案时并不会消失）。
func TestErrReclaimFailedTextDoesNotDenyTheDeletion(t *testing.T) {
	msg := ErrReclaimFailed.Error()
	if !strings.Contains(msg, "已清空") {
		t.Errorf("M347：哨兵没承认「条目已删」这一半，上游照抄就成假话：%s", msg)
	}
	if strings.Contains(msg, "失败") {
		t.Errorf("M347：哨兵被写成「失败」档，会把「已删干净、只差空间」说反：%s", msg)
	}
}

// checkpoint 那一步自己报错（只读卷/满盘一类）时同样走 ErrReclaimFailed，不能穿透成裸 SQL 错。
func TestClearReclaimErrorIsWrapped(t *testing.T) {
	restore := checkpointFn
	defer func() { checkpointFn = restore }()
	checkpointFn = func(db *sql.DB) (int, error) { return 0, errors.New("模拟 checkpoint 失败") }

	c := openTest(t)
	fillProbe(t, c)
	err := c.Clear()
	if !errors.Is(err, ErrReclaimFailed) {
		t.Fatalf("M347：回收步骤报错应包成 ErrReclaimFailed，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "模拟 checkpoint 失败") {
		t.Errorf("M347：原文没进 detail，排查时无从复现：%v", err)
	}
}

// 回收不得把语义版本搅进来：VACUUM 若连 cache_meta 一起清掉，下次 Open 就会
// 走 enforceAlgoVersion 的整表作废分支 —— "清一次缓存"就变成"清缓存 + 顺带改库语义"。
func TestClearKeepsAlgoVersion(t *testing.T) {
	c := openTest(t)
	fillProbe(t, c)
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear 报错: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c2, err := Open(c.path)
	if err != nil {
		t.Fatalf("清空后重开报错: %v", err)
	}
	defer c2.Close()
	if q := c2.QuarantinedTo(); q != "" {
		t.Errorf("M347：回收动作触发了隔离重建（不该发生），隔离到 %s", q)
	}
	var got string
	if err := c2.db.QueryRow(`SELECT value FROM cache_meta WHERE key = ?`, metaKey).Scan(&got); err != nil {
		t.Fatalf("cache_meta 读不回: %v", err)
	}
	if got != AlgoVersion {
		t.Errorf("M347：语义版本被回收动作搅动，want %q got %q", AlgoVersion, got)
	}
	st, err := c2.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Entries != 0 {
		t.Errorf("M347：重开后条目应仍为 0，实际 %d", st.Entries)
	}
}

// 账本侧文件绝不能被缓存的回收动作顺手牵走：Clear/Snapshot 只认 c.path 与其 -wal/-shm。
func TestClearLeavesOtherDbFilesAlone(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(filepath.Join(dir, "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fillProbe(t, c)
	// 同目录放一个无关文件（模拟 history.db 与隔离残留），清空后必须原地不动
	stray := filepath.Join(dir, "history.db")
	if err := os.WriteFile(stray, []byte("must-not-be-touched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear 报错: %v", err)
	}
	got, err := os.ReadFile(stray)
	if err != nil || string(got) != "must-not-be-touched" {
		t.Errorf("M347：回收动作动了同目录的其它文件（err=%v content=%q）", err, got)
	}
}
