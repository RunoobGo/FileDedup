package main

// 缓存快照的落位（M361，第八轮审查批 2）。
//
// CacheClear 的承诺是"清空前把现有缓存快照到 cache-backup.db，只留最近一次"。
// "只留最近一次"意味着**必须覆盖**固定名上那份——所以记录导出那条腿"动手前 exists 就拒"
// 的写法不能照抄过来。但无条件 os.Rename 也不对：Go 没有 O_EXCL 语义的改名原语
// （internal/ops/merge_guard.go 把这句话写在注释里），rename 一律替换，而
// "落位改名静默覆盖第三方文件"在本仓是记过的 P0 形状（§6.63 / M344）。
//
// 于是取 merge_guard 那套三档认领，第三档改成"另落"：
//
//	固定名空着            ⇒ 直接落位（承诺如常）
//	证明得了是我们的      ⇒ 原位覆盖（承诺如常，盘上仍只一份）
//	证明不了              ⇒ **绝不碰它**，另落到一个 O_EXCL 抢到的时间戳名，并把真实落点
//	                         如实交回回执
//
// ★ 为什么第三档不是"显式失败"（merge_guard 的原第三档）：走到这一步缓存**已经清空**了，
//   取消就等于让用户既没有缓存也没有快照；而改名一个字节都不销毁 ⇒ 另落是两边都不输的那一步。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// snapshotProvablyOurs 判断固定名上那份对象能不能被认领为"本应用上一轮留下的影像"。
// 判据本体按平台分档（provablyOwnedOnThisPlatform，见同目录两枚带 tag 的文件）；
// 这里留成接缝是因为"属主不同的常规文件"这一臂在没有 root 的机器上造不出来。
var snapshotProvablyOurs = provablyOwnedOnThisPlatform

// asideLandingAttempts 是另落名的试名上限：占名一律 O_EXCL，撞名就换下一个时间戳。
const asideLandingAttempts = 8

// landCacheSnapshot 把 tmpPath 落成快照，返回**真实落点**、一句只在未按承诺落在固定名时
// 非空的说明、以及错误。任何返回 err != nil 的路径都不留下半截文件（tmp 与被占住的
// 认领名一起收回）。
func landCacheSnapshot(tmpPath, snapPath string) (string, string, error) {
	st, err := os.Lstat(snapPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// 空位：直接落。
		if rerr := os.Rename(tmpPath, snapPath); rerr != nil {
			_ = os.Remove(tmpPath)
			return "", "", rerr
		}
		return snapPath, "", nil
	case err != nil:
		// 连"那里有没有东西"都问不出来 ⇒ 不猜、不覆盖（merge_guard 同口径）。
		_ = os.Remove(tmpPath)
		return "", "", fmt.Errorf("无法确认 %s 是否空闲，已放弃覆盖它（本应用不会删除不属于它的文件）: %w",
			filepath.Base(snapPath), err)
	case snapshotProvablyOurs(st):
		// 上一份自己的影像 ⇒ 覆盖正是承诺。
		if rerr := os.Rename(tmpPath, snapPath); rerr != nil {
			_ = os.Remove(tmpPath)
			return "", "", rerr
		}
		return snapPath, "", nil
	}

	// 证明不了：非常规文件（符号链接/目录/设备）或 unix 腿属主不是本机用户。
	aside, claimErr := claimAsideName(snapPath)
	if claimErr != nil {
		_ = os.Remove(tmpPath)
		return "", "", claimErr
	}
	if rerr := os.Rename(tmpPath, aside); rerr != nil {
		// 刚 O_EXCL 占住的名字是自己三秒前建的，删掉不丢任何人的数据。
		_ = os.Remove(aside)
		_ = os.Remove(tmpPath)
		return "", "", rerr
	}
	return aside, fmt.Sprintf(
		"固定名 %s 上装的是一份不属于本应用的对象（不是常规文件，或属主不是当前用户），"+
			"本应用没有碰它；本次快照另落在 %s", filepath.Base(snapPath), filepath.Base(aside)), nil
}

// claimAsideName 用 O_EXCL 抢一个另落名：**问一次 + 占一次**是同一步，
// 所以不存在"判断它不存在、然后被别人先建了"的窗口（ops 的 claimExact 同一件事）。
func claimAsideName(snapPath string) (string, error) {
	stamp := time.Now().Format("20060102-150405.000")
	var lastErr error
	for attempt := 0; attempt < asideLandingAttempts; attempt++ {
		name := fmt.Sprintf("%s.aside-%s", snapPath, stamp)
		if attempt > 0 {
			name = fmt.Sprintf("%s.aside-%s-%d", snapPath, stamp, attempt+1)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_ = f.Close()
			return name, nil
		}
		lastErr = err
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("占不住另落名 %s（固定名上那份不属于本应用的文件没有被碰，快照这次没落成）: %w",
				filepath.Base(name), err)
		}
	}
	return "", fmt.Errorf("另落名连撞 %d 次（%v），已放弃：固定名上那份不属于本应用的文件没有被碰，快照这次没落成",
		asideLandingAttempts, lastErr)
}
