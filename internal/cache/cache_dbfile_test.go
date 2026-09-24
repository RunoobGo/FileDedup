package cache

// 2026-09-18 审查 C4 回归：损坏自愈的边界。只有 SQLite 明确指证影像损坏时才隔离重建；
// BUSY / 只读 / 满盘等暂时性故障必须原样报错，一个字节都不能动。

import (
	"os"
	"path/filepath"
	"strings"
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

// M213 消费腿（cache 级）：Quarantine 返回错误 ⇒ Open 必须原样上抛
// "放弃重建"，旧影像一个字节都不动、不被新空库顶掉。
// 真实失败源：损坏库放在**只读目录**里 ⇒ 改名（无论主/侧）必 EACCES，
// 对应在册场景"另一进程锁住文件致隔离失败"。
// 判据不是"隔离能成功"而是"隔离不成功时绝不重建"——改前 _ = 吞侧错只影响
// 有侧文件的形状，主文件改名失败改前改后都上抛，故本条两棵树皆绿，
// 它的定位是消费腿契约钉子（防未来有人把 qerr 分支改成"照常重建"）。
//
// 〔2026-09-24 CI 复批〕：只读目录是 **Unix-only** 前提——Windows 忽略目录的
// 写/执行位，改名照常成功 ⇒ windows 腿红在前提而非判据。Windows 上唯一便携的
// "改不动名"是文件的**只读属性**（去 owner 写位 → Go 映射 FILE_ATTRIBUTE_READONLY
// → rename 需 DELETE 访问被拒 ACCESS_DENIED），故此处两把锁叠加：
//   - 目录去写位（拦 unix 的 rename），
//   - 主文件去写位（拦 windows 的 rename，对 unix 无害）。
//
// darwin/linux 上文件只读不影响 rename，目录那把仍生效，故本机仍当场取红。
func TestQuarantineFailureAbortsRebuild(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")
	junk := []byte("this is not a sqlite database garbage")
	if err := os.WriteFile(dbPath, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dbPath, 0o644)
		_ = os.Chmod(dir, 0o755)
	})

	_, err := Open(dbPath)
	if err == nil {
		t.Fatal("隔离失败时 Open 必须报错（不得静默降级为空缓存）")
	}
	if !strings.Contains(err.Error(), "放弃重建") || !strings.Contains(err.Error(), "隔离失败") {
		t.Errorf("错误必须点名'隔离失败 ⇒ 放弃重建'：%v", err)
	}
	// 现场核验：旧影像仍在原地且内容未动，也没有任何 .broken-* 或被重建的新库。
	after, rerr := os.ReadFile(dbPath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(after) != string(junk) {
		t.Error("隔离失败路径改动了旧影像")
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
