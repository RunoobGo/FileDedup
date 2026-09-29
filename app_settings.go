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
		if rerr := os.Rename(path, path+".corrupt"); rerr == nil {
			msg += "，损坏文件已留证为 settings.json.corrupt"
		}
		if a.emit != nil {
			a.emit(a.ctx, "app:error", map[string]string{"error": msg + "（" + jerr.Error() + "）"})
		}
		s = defaultSettings()
	}
	return s
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
}

// CacheClearSnapshotName 是缓存快照的**固定**文件名（落在 cache.db 同目录）。
//
// 固定名 = 只保留最近一次（2026-09-29 用户裁定 R-3）。它防的是"这次手滑"，
// 不是备份系统：没有轮转、没有多份，下次清空就把它覆盖掉。要防卷损坏/整目录误删
// 那一档，走记录页的「导出记录」（可选到别的盘，M351）。
const CacheClearSnapshotName = "cache-backup.db"

// cacheClearSnapshot 是"落快照"这一步的注入点（测试接缝，惯例同 a.emit 字段、
// cache.evictFn、dbfile.renameFile）。
//
// 为什么要接缝：M349 的承重判据是**编排顺序**（快照不成就不删），要在根包里断言；
// 而真实造出 `VACUUM INTO` 失败的手段（只读目录、满盘）在本仓既有读数里全是平台条件
// ——§6.40 的 Windows 教训："只读目录造改名失败"在 Windows 上不成立。
// "这条语句真能产出可用影像"由不碰接缝的成功格（读回条目数并逐条命中）与包内探针负责，
// 两边合起来才把这条覆盖完整。
var cacheClearSnapshot = func(c *cache.Cache, dest string) error { return c.Snapshot(dest) }

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
	cch := a.cchSnapshot()
	if cch == nil {
		return res, fmt.Errorf("缓存不可用")
	}
	before, err := cch.GetStats()
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
	// 2) 清空
	clearErr := cch.Clear()
	reclaimOnly := errors.Is(clearErr, cache.ErrReclaimFailed)
	if clearErr != nil && !reclaimOnly {
		// DELETE 那一步就失败了 ⇒ 数据一条没少，快照也不必留（旧的那份原样不动）
		_ = os.Remove(tmpPath)
		return res, shellRPCError(clearErr) // M214：中文哨兵原样保身份，英文 SQL 腿套壳
	}
	// 3) 清空成立（含"只有回收失败"那一档）⇒ 新快照才配顶掉旧快照
	if err := os.Rename(tmpPath, snapPath); err != nil {
		_ = os.Remove(tmpPath)
		return res, fmt.Errorf("缓存已清空 %d 条，但快照落位失败（保留的仍是旧快照或无快照）：%w", before.Entries, err)
	}
	res.SnapshotPath = snapPath
	res.EntriesCleared = before.Entries
	after, err := cch.GetStats()
	if err != nil {
		return res, shellRPCError(err)
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
			clearErr, res.EntriesCleared, res.ReclaimedBytes, snapPath)
	}
	return res, nil
}
