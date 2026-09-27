package main

// M285 应用侧探针（2026-09-27 实施批；04 §6.51 立账）。
//
// 现场：W9-9 那半格真机读数——把 <cfgDir>/cache.db 覆写成非库影像后启动，
// 缓存确实被隔离重建了（stderr 有句子），但界面上**一个字都没有**。GUI 用户没有终端，
// 看到的只是"这软件每次扫描都很慢"，而原因（整表作废）永远说不出口。
//
// 与 app_m25_test.go 的分工：M25 管的是**打开失败**（拿不到句柄，无缓存），
// M285 管的是**打开成功但代价是整表作废**（句柄在、速度掉了）。
// 两条出口互斥，所以各自都要断言"另一条不许出现"——同一次启动里既报"不可用"
// 又报"已重建"是一句自相矛盾的话。
//
// 这里刻意不调 cacheQuarantineNotice：判据走真实链路（openCache → GetStartupNotice），
// 测的是"用户看得见"，不是"某个纯函数返回了某个字符串"。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCacheQuarantineSurfacesNotice(t *testing.T) {
	a := newTestApp(t)
	junk := []byte("garbage where a hash cache used to live")
	if err := os.WriteFile(filepath.Join(a.cfgDir, "cache.db"), junk, 0o644); err != nil {
		t.Fatal(err)
	}

	a.openCache()

	a.mu.Lock()
	cch := a.cch
	a.mu.Unlock()
	if cch == nil {
		t.Fatal("夹具失效：确证损坏应隔离重建并留下句柄，本用例没在测隔离路径")
	}
	defer cch.Close()

	notice := a.GetStartupNotice()
	if notice == "" {
		t.Fatal("缓存整表作废只写 stderr：GUI 无终端 ⇒ 用户只看到「每次扫描都很慢」，无从自查（M285）")
	}
	// 四层信息缺一不可：出了什么事 / 在哪个文件上 / 后果 / 边界（结果不受影响）。
	for _, want := range []string{"影像损坏", "cache.db.broken-", "整表作废", "重新计算", "不影响去重结果"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("提示缺少「%s」这一层信息：%q", want, notice)
		}
	}
	// 互斥性：隔离成功不是"不可用"，两条文案不许同时在场。
	if strings.Contains(notice, "哈希缓存不可用") {
		t.Fatalf("已隔离重建成功，却又报「不可用」——同一次启动两条自相矛盾的提示：%q", notice)
	}
}

// 负控制不必另写：app_m25_test.go 的 TestOpenCacheQuietWhenHealthy 已经守着
// "健康路径 openCache 不许留提示"（新建 + 重开两格都跑），新增的隔离分支一旦
// 漏了 `q != ""` 判据，那条既有用例当场转红。重复写一遍只会让两处以后不同步。
