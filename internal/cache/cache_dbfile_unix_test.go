//go:build !windows

package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M213 消费腿（cache 级）：Quarantine 返回错误 ⇒ Open 必须原样上抛
// "放弃重建"，旧影像一个字节都不动、不被新空库顶掉。
//
// 真实失败源：损坏库放在**只读目录**里 ⇒ 改名（无论主/侧）必 EACCES。
// 这是一个 **Unix-only** 前提：Windows 忽略目录的写/执行位，也允许改名带
// 只读属性的文件 ⇒ 同一夹具在 windows 腿上"隔离照常成功"，红会落在前提而非判据。
// 因此本文件按 `!windows` 门控（linux/darwin 两条 CI 腿当场取红）。
//
// Windows 腿的对应格不是没有，而是刻意落到真机清单：见 docs/05 §P11 的
// W12-1「真锁 -wal 下的隔离-回滚全链」（另起进程写打开 -wal 不放制造
// sharing 拒绝）。CI 里没有 Windows 真锁夹具 ⇒ 不在这里补一条会假绿的近似。
//
// 判据不是"隔离能成功"而是"隔离不成功时绝不重建"——改前 `_ =` 吞侧错只影响
// 有侧文件的形状，主文件改名失败改前改后都上抛，故本条两棵树皆绿，
// 它的定位是消费腿契约钉子（防未来有人把 qerr 分支改成"照常重建"）。
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
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

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
