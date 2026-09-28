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
	"filededup/internal/cache"
	"filededup/internal/dedup"
	"fmt"
	"os"
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

// CacheClear 清空缓存（M4-T01）。同上（APP-2）。
func (a *App) CacheClear() error {
	cch := a.cchSnapshot()
	if cch == nil {
		return fmt.Errorf("缓存不可用")
	}
	return shellRPCError(cch.Clear()) // M214：DELETE 原始错误套壳；中文哨兵（ErrCorruptDisabled）原样保身份
}
