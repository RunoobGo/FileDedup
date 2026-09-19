package cache

// 2026-09-18 审查 C4 回归：损坏自愈的边界。只有 SQLite 明确指证影像损坏时才隔离重建；
// BUSY / 只读 / 满盘等暂时性故障必须原样报错，一个字节都不能动。

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

// blockWalSidecar 用「-wal 位置被目录占住」制造暂时性打开失败：
// 库文件本身完好、目录可写、权限正常，只是 SQLite 这一刻建不起 WAL 侧文件。
// 真实成因可能是同步盘/备份工具留下同名目录，也可能是他进程正在写库。
// 关键性质：修复前的自愈 os.Remove(主库) 在此**会成功**，所以这条用例能
// 真正区分「修复前把库删了重建」与「修复后原样报错」。
func blockWalSidecar(t *testing.T, p string) {
	t.Helper()
	if err := os.Mkdir(p+"-wal", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(p + "-wal"); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	})
}

func TestCorruptSelfHealKeepsImageQuarantined(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")
	junk := []byte("this is not a sqlite database garbage")
	if err := os.WriteFile(dbPath, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Open(dbPath)
	if err != nil {
		t.Fatalf("确证损坏应隔离重建: %v", err)
	}
	defer c.Close()

	// 旧影像必须仍在原地（改名，不是删除）：误判也要留得回
	matches, _ := filepath.Glob(dbPath + ".broken-*")
	if len(matches) != 1 {
		t.Fatalf("隔离文件 = %v, want 1 个", matches)
	}
	payload, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != string(junk) {
		t.Errorf("隔离后的内容不符: %q", payload)
	}
}

// 暂时性故障：报错，但库文件与既有条目分毫不动。这正是修复前的死法——
// openDB 失败 → 无条件 os.Remove → 重建空库，缓存整表蒸发。
func TestTransientFailureLeavesDBUntouched(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")
	c, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Store([]Entry{{Path: "/keep/me", Size: 7, MtimeNs: 7, Head: 3, Tail: 4}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	blockWalSidecar(t, dbPath)
	_, err = Open(dbPath)
	if err == nil {
		t.Fatal("库暂时打不开时 Open 应失败（不得静默降级）")
	}
	t.Logf("暂时性故障按预期上抛: %v", err)
	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("暂时性故障不得改动库文件")
	}
	if broken, _ := filepath.Glob(dbPath + ".broken-*"); len(broken) != 0 {
		t.Errorf("暂时性故障不得隔离/搬走库文件: %v", broken)
	}
	if err := os.Remove(dbPath + "-wal"); err != nil {
		t.Fatal(err)
	}

	// 冲突解除后重新打开：既有缓存条目仍在（证明没有被「顺手重建」清掉）
	c2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, hit, _ := c2.Lookup("/keep/me", 7, 7, fsid.ID{}); !hit {
		t.Fatal("故障后既有缓存条目丢失（等价于被误删重建）")
	}
}
