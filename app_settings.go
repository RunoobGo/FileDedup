// app_settings.go —— 设置、版本、导出与缓存维护。
//
// M336（2026-09-28 第七轮审查批）：由 app.go 按职责簇拆分而来。
// **方法名与签名一字未改**——Wails 绑定按方法名解析，与所在文件无关，
// 因此这是零行为改动的纯移动：前后端契约、调用方、既有测试都不受影响。
//
// 包级类型/常量/变量与非方法函数仍留在 app.go：它们被多簇共用，
// 拆开只会让「这个类型在哪」变成第二次查找。
package main

import (
	"encoding/json"
	"errors"
	"filededup/internal/cache"
	"filededup/internal/dedup"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// GetSettings 读取设置：要么完整解析的配置，要么**纯**默认值。
func (a *App) GetSettings() Settings {
	path, perr := a.settingsPath()
	if perr != nil {
		return defaultSettings() // 目录不可用：磁盘上没有属于本应用的证据
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultSettings() // 首次运行/读不到：磁盘上没有证据，静默回默认
	}
	s := defaultSettings()
	if jerr := json.Unmarshal(b, &s); jerr != nil {
		// M10b（2026-09-21 全仓审计 §五 10）：修正前是 `_ = json.Unmarshal`，
		// 坏文件被无声吞掉、返回一份"解析到哪算哪"的半成品配置；界面上看不出
		// 异常，下一次保存又把它写回磁盘，原始证据就此消失。
		// 现在改名为 .corrupt 留证（用户仍可手工恢复），并把失败原因报给界面。
		msg := "设置文件无法解析，本次使用默认设置"
		// E8 / 拟 M397（2026-10-01 第九轮 P2-2）：留证这一步**自己**也会销毁证据——
		// os.Rename 覆盖目标，于是第二次损坏会把第一次那份（唯一能解释"为什么坏"的物证）抹掉。
		// 落点因此改成"撞名就让位"（判据见 corruptBackupPath），且提示那句写的是**真实落点**
		// （与 M285 缓存隔离件点名落点同一形状），否则用户按 settings.json.corrupt 去找会扑空。
		target := corruptBackupPath(path, time.Now())
		if rerr := os.Rename(path, target); rerr == nil {
			msg += "，损坏文件已留证为 " + filepath.Base(target)
		}
		if a.emit != nil {
			a.emit(a.ctx, "app:error", map[string]string{"error": msg + "（" + jerr.Error() + "）"})
		}
		s = defaultSettings()
	}
	return s
}

// corruptBackupPath 为损坏的设置件挑一个**不会覆盖上一次证据**的落点（E8 / 拟 M397）。
//
// 取向（为什么不选"覆盖最新"）：这份文件是唯一能解释"设置为什么坏"的物证，而 M10b 立留证
// 这条分支防的正是"证据消失"——让加固动作自己销毁证据不成立。
//
// ★ 时钟由参数注入而不是内部调 time.Now：同一秒内连续第二次损坏时秒级时间戳必然撞名，
//
//	这一格只有把时钟钉进那一秒才测得到，否则判据是摆设。
//
// ★ Stat 报**任何**错都不算"落点空闲"，只有 ErrNotExist 才算：目录权限出问题的时候，
//
//	把"问不动"读成"没有"会让重命名撞在一个真实存在、只是统计失败的文件上。
func corruptBackupPath(path string, now time.Time) string {
	base := path + ".corrupt"
	if _, err := os.Stat(base); errors.Is(err, fs.ErrNotExist) {
		return base // 盘上还没有旧证据：沿用 M10b 起那个固定名（既有恢复习惯与手册都认它）
	}
	sec := now.Unix()
	for i := int64(0); i < 60; i++ {
		cand := fmt.Sprintf("%s-%d", base, sec+i)
		if _, err := os.Stat(cand); errors.Is(err, fs.ErrNotExist) {
			return cand
		}
	}
	// 60 格全占（同一秒内第 61 次损坏）：退到纳秒位。仍然宁可多留一份，不覆盖。
	return fmt.Sprintf("%s-%d", base, now.UnixNano())
}

// SaveSettings 保存设置到 settings.json。
func (a *App) SaveSettings(s Settings) (Settings, error) {
	// P2：与引擎同一口径钳制（0 = 自动，1..dedup.MaxThreads = 显式指定）。
	// 引擎侧也会钳制，这里钳是为了落盘值与实际生效值一致，界面上不自相矛盾。
	if s.Threads < 0 {
		s.Threads = 0
	} else if s.Threads > dedup.MaxThreads {
		s.Threads = dedup.MaxThreads
	}
	if s.Theme != "light" && s.Theme != "dark" && s.Theme != "system" {
		s.Theme = "system"
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return s, err
	}
	path, perr := a.settingsPath()
	if perr != nil {
		return s, perr
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return s, shellRPCError(err) // M214：SaveSettings 写文件失败不再直通英文 OS 错误
	}
	return s, nil
}

// GetStartupNotice 取启动阶段的一次性提示（无则空串）。
// 为什么不是事件：见 App.startupNotice 的注释——startup 早于前端注册监听，
// 发出去的事件必然丢；前端在 store.init 的"初始拉取"里调一次即可。
func (a *App) GetStartupNotice() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupNotice
}

// GetVersion 当前版本。
func (a *App) GetVersion() string { return AppVersion }

// ExportReport M5-T01 **未交付**：这是空桩，调用只会拿到下面那句固定错误。
// 登记表 04 §2 与手册都按"未交付"记（M300 立账的就是这一度写反的注释）；
// 契约面仍列它，是因为前端 `api` 层刻意不包装无实现的方法（35/38 之差由此而来）。
func (a *App) ExportReport(format, path string) (string, error) {
	return "", fmt.Errorf("报告导出将在 M5 提供")
}

// CacheStats 缓存统计（M4-T01）。句柄经 cchSnapshot 取，锁外只读快照。
func (a *App) CacheStats() (cache.Stats, error) {
	cch := a.cchSnapshot()
	if cch == nil {
		return cache.Stats{}, fmt.Errorf("缓存不可用")
	}
	st, err := cch.GetStats()
	return st, shellRPCError(err) // M214：透传的 SQL 腿错误套壳（nil 原样穿过）
}

// CacheClearResult 是「清空缓存」的回执（M349）。
//
// 为什么要回执：改前 CacheClear 只返回 error，界面上那格「占用」又是从另一条 RPC
// （CacheStats）取来的，于是"条目 0 · 占用 4.1 MB"这种自相矛盾的读数没有任何反证可给——
// 用户只能判断"没清掉"。回执把三件事实一次性交回前端：清了几条、拿回多少字节、快照落在哪。
type CacheClearResult struct {
	EntriesCleared int    `json:"entriesCleared"`
	ReclaimedBytes int64  `json:"reclaimedBytes"`
	SnapshotPath   string `json:"snapshotPath"`
	// SnapshotNote 只在"落位没有按承诺落在固定名上"时非空（M361）。
	//
	// 为什么单开一句而不是塞进 SnapshotPath：确认框里已经告诉用户快照会落在
	// cache-backup.db，那是**承诺**；固定名被一份不属于本应用的对象占着时，本次影像
	// 另落在一个带时间戳的名字上，用户必须知道"这次和你说好的那个名字不是一回事"，
	// 否则他下次去找回档找的是错文件。正常覆盖上一份时这一格留空，界面不多说话。
	SnapshotNote string `json:"snapshotNote"`
}

// CacheClearSnapshotName 是缓存快照的**固定**文件名（落在 cache.db 同目录）。
//
// 固定名 = 只保留最近一次（2026-09-29 用户裁定 R-3）。它防的是"这次手滑"，
// 不是备份系统：没有轮转、没有多份，下次清空就把它覆盖掉。要防卷损坏/整目录误删
// 那一档，走记录页的「导出记录」（可选到别的盘，M351）。
const CacheClearSnapshotName = "cache-backup.db"

// cacheClearSnapshot 是"落一张**仅所有者可读写**的影像"这一步的注入点（测试接缝，
// 惯例同 a.emit 字段、cache.evictFn、dbfile.renameFile）。
//
// 为什么要接缝：M349 的承重判据是**编排顺序**（快照不成就不删），要在根包里断言；
// 而真实造出 `VACUUM INTO` 失败的手段（只读目录、满盘）在本仓既有读数里全是平台条件
// ——§6.40 的 Windows 教训："只读目录造改名失败"在 Windows 上不成立。
// "这条语句真能产出可用影像"由不碰接缝的成功格（读回条目数并逐条命中）与包内探针负责，
// 两边合起来才把这条覆盖完整。
//
// ★ M360 把 Chmod 收进接缝**里面**而不是留在调用方：档位是"这张影像成形"的一部分，
//
//	不是一个独立的后续动作。分开写时，任何"改名之后才补 chmod"的实现都能通过只看
//	最终落点档位的断言，而 SQLite 手里那段 0644 的时间窗（P-15b 钉的就是它）没人看；
//	收进来之后调用方**没有**忘记补档位这条路可走。设不上档位即整步失败 ⇒ 调用方那条
//	「清空缓存已取消：快照未能生成，未删除任何缓存数据」仍然是一句实话（tmp 会被收回，
//	盘上没留下一份可用的影像，缓存也一条没动）。
var cacheClearSnapshot = func(c *cache.Cache, dest string) error {
	if err := c.Snapshot(dest); err != nil {
		return err
	}
	// ★ 这句"仅所有者可读写"只在 unix 腿兑现：Windows 的模式位只表达"只读属性"，
	//   0600 与 0644 在那条腿上读回同一个 0666（M354 的现读），真正的访问权由所在目录
	//   的 NTFS ACL 继承 ⇒ 手册与测试都按分平台措辞写，不拿这句冒称跨平台保证。
	return os.Chmod(dest, 0o600)
}

// cacheClearStats 是"读缓存统计"的注入点（接缝惯例同 cacheClearSnapshot）。
//
// 为什么这条也要接缝：清缓存的回执跨两次取统计——删之前拿条数、删之后算回收字节。
// 后一次失败时**清空已经成立**，用户该拿到的那半句真话（"已清空 N 条 · 快照在 X"）
// 只能从错误串里到达（Wails 在 err != nil 时丢掉结构体，M349 已记过一次），
// 而"后一次失败"这一档在真实机器上要靠满盘/句柄被关去撞，CI 造不出来。
// 有了接缝，这一档就是一条可以钉死的判据（M362）。
var cacheClearStats = func(c *cache.Cache) (cache.Stats, error) { return c.GetStats() }

// CacheClear 清空哈希缓存（M4-T01；回收与快照见 M347/M349）。
//
// 顺序是**门槛**而不是礼貌：先快照、快照成功才删。快照写不成就直接返回错误、
// 一条缓存都不动 —— 与 dbfile「要么完整隔离、要么原地不动」、审查 C4「绝不拿误判赌
// 用户数据」同一条取向。失败面分三档，措辞各不同：
//
//	快照失败      ⇒ 取消清空，未删除任何数据（tmp 清掉，上一份快照原样留着）。
//	DELETE 失败   ⇒ 同上：语句是 autocommit 单条，要么整表清空要么整表还在。
//	只有回收失败  ⇒ **清空已经成立**，走 cache.ErrReclaimFailed 分岔（M347 的文案纪律）。
//
// 只动 hash_cache 一张表：账本（history.db 五张表）由进程里另一条句柄管，
// 这里一次都不引用；那条边界有结构守卫（app_cache_clear_m349_test.go 的
// TestCacheClearLeavesLedgerUntouched，M350）。
func (a *App) CacheClear() (CacheClearResult, error) {
	var res CacheClearResult
	// M359（第八轮批 2）：这一句是整个函数的第一道门槛。原先 app_settings.go 里
	// opsRunning / scanInFlight 的命中数是 0——清缓存既能在扫描写回 hash_cache 时插进来，
	// 也能被"清缓存期间点清理"撞出去；StartScan 与 ExecuteOperation 早已互相双向闭合，
	// 唯独这两个维护口一条闸都不接（不对称本身是缺陷）。占位与三问同临界区，
	// 与那两处的 check-and-set 同锁串行 ⇒ 双向闭合。
	if err := a.claimMaintenance("清空缓存", "清空缓存"); err != nil {
		return res, err
	}
	defer a.releaseMaintenance()

	cch := a.cchSnapshot()
	if cch == nil {
		return res, fmt.Errorf("缓存不可用")
	}
	before, err := cacheClearStats(cch)
	if err != nil {
		return res, shellRPCError(err)
	}
	// 1) 快照先落 tmp，成功后才 rename 覆盖到固定名：失败时上一份快照不受牵连
	//    （VACUUM INTO 拒绝写进一个已存在的文件，实测撞名即错，所以不能直接写目标名）。
	snapPath := filepath.Join(filepath.Dir(cch.DBPath()), CacheClearSnapshotName)
	tmpPath := fmt.Sprintf("%s.tmp-%d", snapPath, time.Now().UnixNano())
	if err := cacheClearSnapshot(cch, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		// ★ %w 不是笔误：停用期的 ErrCorruptDisabled 必须保住 errors.Is 身份
		//   （app_error_shell_test.go 的 B 档护栏专门盯这条透传）。
		return res, fmt.Errorf("清空缓存已取消：快照未能生成，未删除任何缓存数据（%w）", err)
	}
	// M360：档位（0600）收在 cacheClearSnapshot 里面，见那一段的注释——
	// 改名会把模式位一起带过去，所以收 tmp 一处就够；两条落位臂（原位与另落）都跟着走。
	// 2) 清空
	clearErr := cch.Clear()
	reclaimOnly := errors.Is(clearErr, cache.ErrReclaimFailed)
	if clearErr != nil && !reclaimOnly {
		// DELETE 那一步就失败了 ⇒ 数据一条没少，快照也不必留（旧的那份原样不动）
		_ = os.Remove(tmpPath)
		return res, shellRPCError(clearErr) // M214：中文哨兵原样保身份，英文 SQL 腿套壳
	}
	// 3) 清空成立（含"只有回收失败"那一档）⇒ 新快照才配顶掉旧快照。
	//    ★ 落位要先证明固定名上那份（若有）是本应用自己的影像（M361）：
	//      os.Rename 一律替换，而"覆盖掉一份不属于我们的文件"在本仓是记过的 P0 形状
	//      （§6.63 / M344）。证明不了就另落并如实报出真实落点——缓存此刻已经清空，
	//      这一腿**不能**取消，否则用户拿不到任何快照。
	landed, note, err := landCacheSnapshot(tmpPath, snapPath)
	if err != nil {
		return res, fmt.Errorf("缓存已清空 %d 条，但快照落位失败（保留的仍是旧快照或无快照）：%w", before.Entries, err)
	}
	res.SnapshotPath = landed
	res.SnapshotNote = note
	res.EntriesCleared = before.Entries
	after, err := cacheClearStats(cch)
	if err != nil {
		// M362：清空与快照**都已经成立**，缺的只是"回收字节没能核对"。这一句必须同时
		// 把条数与快照落点带进错误串——Wails 在 err != nil 时丢掉结构体，前端 catch 到的
		// 只有这句话；只回一句壳错误等于把已经做成的两件事一起说没了（对照下面 reclaimOnly
		// 那一腿，同一条契约在同一函数里已经处理过一次）。
		return res, fmt.Errorf("缓存已清空 %d 条，快照在 %s，但磁盘占用未能核对（回收字节暂报 0）：%w",
			res.EntriesCleared, landed, err)
	}
	if d := before.DBSizeBytes - after.DBSizeBytes; d > 0 {
		res.ReclaimedBytes = d
	}
	if reclaimOnly {
		// ★ 这一腿不能说"清空失败"：条目已经删净，缺的只是磁盘字节（M347 文案纪律）。
		//   数字必须进错误串而不是只留在 res 里：Wails 在 err != nil 时把结构体丢掉，
		//   前端 catch 到的只有这句话（wails.ts 的 reject 形状是字符串/Error message，
		//   见 SettingsView 那条 catch），所以"已清空 N 条"这一半真话只能靠它到达用户。
		return res, fmt.Errorf("%w；已清空 %d 条、拿回 %d 字节，快照在 %s",
			clearErr, res.EntriesCleared, res.ReclaimedBytes, landed)
	}
	return res, nil
}
