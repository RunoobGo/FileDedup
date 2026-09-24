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
	"fmt"
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

// renameFile 是包级改名接缝（惯例同 cache.evictFn / scanner.probeCaseVerdict）：
// 生产走 os.Rename，测试注入"仅某个侧文件改名失败"来取 M213 的回滚红。
var renameFile = os.Rename

// Quarantine 把库文件连同 -wal/-shm 改名隔离，返回主文件的隔离后路径。
//
// 只改名、不删除：回撤账本一旦被误判清空就是不可恢复的用户数据丢失；
// 改名后原始影像仍在原地，用户与应用都有找回的机会。
// 主文件改名失败即返回错误——调用方应放弃重建，而不是转而删除文件。
//
// M213（2026-09-24 第五轮审查批）：判据是"**要么完整隔离、要么原地不动**"。
// 修正前任一侧文件（-wal/-shm）改名失败都 `_ =` 吞掉、主文件已挪走即返回 nil，
// 于是调用方在原路径重建新库——而旧 -wal 因改名失败留在原地，会被新库照常
// 恢复/撕裂（-wal 只按"同名库"关联，头内无库指纹）。隔离从"保全影像"退化为
// "复制损坏影像 + 在原路径埋活雷"。现读侧文件失败 → 回滚已改名的主/侧文件回原
// 路径 → 返回点名"哪个侧文件、改名错 + 回滚错若有"的 error；回滚本身失败则明写
// "主影像已隔离且无法回原位，勿在原路径重建"，绝不静默。返回 nil 时 -wal/-shm
// 必与主文件同侧。
func Quarantine(path string) (string, error) {
	quarantined := path + ".broken-" + time.Now().Format("20060102-150405.000")
	if err := renameFile(path, quarantined); err != nil {
		return "", err
	}
	// 已成功改名的侧文件（回滚时按逆序挪回原位）。
	moved := make([]string, 0, 2)
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err != nil {
			continue // 侧文件不存在：无需跟随
		}
		if err := renameFile(path+suffix, quarantined+suffix); err != nil {
			// 侧文件改名失败 ⇒ 撤销整次隔离：先把已挪走的侧文件挪回，再把主文件挪回。
			rollbackErrs := rollbackSide(path, quarantined, moved)
			mainRollback := renameFile(quarantined, path)
			if mainRollback != nil {
				// 主影像回滚失败 ⇒ 它已隔离在原地回不去，但绝不能让调用方误以为可重建。
				msg := fmt.Sprintf("侧文件 %s%s 改名失败：%v；主影像已隔离于 %s 且无法挪回原路径（回滚错误：%v）——请勿在原路径重建，须人工核实隔离影像",
					path, suffix, err, quarantined, mainRollback)
				if len(rollbackErrs) > 0 {
					msg += "；另有侧文件回滚失败：" + strings.Join(rollbackErrs, "; ")
				}
				return "", errors.New(msg)
			}
			// 主影像已回到原路径：调用方见 err != nil 即放弃重建（cache/history 现有分支天然接住）。
			msg := fmt.Sprintf("侧文件 %s%s 改名失败：%v；主影像已回滚至原路径，隔离整体撤销，放弃重建",
				path, suffix, err)
			if len(rollbackErrs) > 0 {
				msg += "；注意以下已改名侧文件回滚失败，仍隔离在旧名（主影像已回到原路径，须人工核实）：" + strings.Join(rollbackErrs, "; ")
			}
			return "", errors.New(msg)
		}
		moved = append(moved, suffix)
	}
	return quarantined, nil
}

// rollbackSide 把 moved 里的侧文件从隔离名挪回原路径，返回回滚失败的说明列表。
func rollbackSide(path, quarantined string, moved []string) []string {
	var fails []string
	for i := len(moved) - 1; i >= 0; i-- {
		suffix := moved[i]
		if err := renameFile(quarantined+suffix, path+suffix); err != nil {
			fails = append(fails, fmt.Sprintf("%s%s: %v", quarantined, suffix, err))
		}
	}
	return fails
}
