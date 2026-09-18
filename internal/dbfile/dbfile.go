// Package dbfile 界定 SQLite 库文件「损坏自愈」的安全边界。
//
// 背景（2026-09-18 全量审查 C4）：cache.Open 与 history.Open 原先对
// **任何**打开错误都无条件 os.Remove 主库与 -wal 后重建。而 SQLITE_BUSY、
// 磁盘满、目录只读、权限不足同样会走到这条分支——一次误判就把用户的
// 回撤账本整体清空；删掉另一进程正在写的 -wal 本身即可损坏主库。
//
// 本包把「判定」与「处置」分开：判定保守（认不准就当作暂时性故障），
// 处置只改名不删除（误判也留得回）。
package dbfile

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
)

// corruptionMarkers 是 SQLite 明确指向「该文件不是一份可用数据库影像」的
// 错误原文片段（modernc.org/sqlite 直接透传 SQLite 消息，无错误码可断言）。
// 刻意只列强指征：暂时性故障（busy/readonly/full disk/permission）一律不在此列。
var corruptionMarkers = []string{
	"file is not a database",           // SQLITE_NOTADB
	"is not a database",                // 驱动包装变体
	"database disk image is malformed", // SQLITE_CORRUPT
	"malformed database schema",        // SQLITE_CORRUPT（schema 路径）
	"is never used",                    // SQLITE_CORRUPT："database page N is never used"
	"database may be corrupt",          // SQLITE_CORRUPT（vtab/索引路径）
	"sqlite_corrupt",
	"sqlite_notadb",
}

// IsCorruption 判定错误是否确指库文件本身损坏（需隔离后重建）。
// 认不准一律返回 false——调用方因此选择「报错不删」，
// 宁可功能暂时不可用，也不能拿用户的账本赌一次误判。
func IsCorruption(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range corruptionMarkers {
		if strings.Contains(s, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// Exists 报告库文件是否存在（不存在时的打开失败必然不是「损坏」，
// 而是目录不可写/权限等环境故障，绝不能走隔离分支）。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// Quarantine 把库文件连同 -wal/-shm 改名隔离，返回主文件的隔离后路径。
//
// 只改名、不删除：回撤账本一旦被误判清空就是不可恢复的用户数据丢失；
// 改名后原始影像仍在原地，用户与应用都有找回的机会。
// 主文件改名失败即返回错误——调用方应放弃重建，而不是转而删除文件。
func Quarantine(path string) (string, error) {
	quarantined := path + ".broken-" + time.Now().Format("20060102-150405.000")
	if err := os.Rename(path, quarantined); err != nil {
		return "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			// 侧文件尽力跟随；失败不影响主影像已隔离的事实
			_ = os.Rename(path+suffix, quarantined+suffix)
		}
	}
	return quarantined, nil
}
