package main

// M10（2026-09-21 全仓审计与整改交接 §五 10）三条同批探针：
//   a) 分页起点溢出（绑定层入参可控 → 切片越界 panic）；
//   b) 设置文件损坏时的静默半吞（坏值回写盖掉证据）；
//   c) PreviewProcessPolicy 空 dirs 分支与执行侧口径相反（保留项被算进生效范围）。
// 三条都是"改前必红"的形状：a 当场 panic，b 拿到 Threads=9 且无事件无证据文件，
// c 的生效数比真正会动的文件多。

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// isDefaultSettings 逐字段比较（Settings 内含切片，== 不可比较）。
func isDefaultSettings(s Settings) bool {
	return reflect.DeepEqual(s, defaultSettings())
}

// mustSettingsPath 跟随 M60 的签名变化（cfgDir 未初始化时不再静默返回相对路径，
// 而是回 error）。这里把 error 直接判死：这几个用例走 newHistApp，cfgDir 必然已设，
// 回 error 就是用例前提塌了。
func mustSettingsPath(t *testing.T, a *App) string {
	t.Helper()
	path, err := a.settingsPath()
	if err != nil {
		t.Fatalf("settingsPath() 报错（本文件用例的 cfgDir 应已初始化）：%v", err)
	}
	return path
}

// ---------- M10a ----------

// pagedNoPanic 把" panic 即失败"写成断言，让 RED 的读数是一句话而不是堆栈。
func pagedNoPanic(t *testing.T, a *App, q ResultQuery) PagedResult {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("分页入参 page=%d pageSize=%d 不该 panic：%v", q.Page, q.PageSize, r)
		}
	}()
	r, err := a.GetResultGroups(q)
	if err != nil {
		t.Fatalf("GetResultGroups(%+v) 报错：%v", q, err)
	}
	return r
}

// 溢出回绕的起点必须判成"空页"，而不是负下标切片。
// 1<<62 × 2 的乘积正好是 MinInt64：修正前 `start >= len(gs)` 对负数恒假，
// 下一步 gs[start:end] 当场越界。
func TestResultGroupsPageOverflow(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	r0 := pagedNoPanic(t, a, ResultQuery{Page: 0, PageSize: 1})
	if r0.Total == 0 || len(r0.Groups) == 0 {
		t.Fatalf("第 0 页应有数据（Total=%d 实得 %d 组），夹具失效", r0.Total, len(r0.Groups))
	}

	for _, q := range []ResultQuery{
		{Page: 1 << 62, PageSize: 2},   // 回绕成 MinInt64（负）
		{Page: 1 << 62, PageSize: 500}, // 钳位上限 500 仍溢出
		{Page: math.MaxInt, PageSize: 2},
		{Page: math.MaxInt/500 + 1, PageSize: math.MaxInt}, // 超大 pageSize 钳到 500 后乘积仍溢出
	} {
		r := pagedNoPanic(t, a, q)
		if len(r.Groups) != 0 {
			t.Fatalf("溢出起点 page=%d pageSize=%d 应给空页，实得 %d 组",
				q.Page, q.PageSize, len(r.Groups))
		}
		// Total/TotalReclaimable 是统计条的数据源，空页时也必须如实。
		if r.Total != r0.Total {
			t.Fatalf("空页仍要回传总数：Total=%d want %d", r.Total, r0.Total)
		}
		if r.TotalReclaimable != r0.TotalReclaimable {
			t.Fatalf("空页仍要回传可释放总量：%d want %d", r.TotalReclaimable, r0.TotalReclaimable)
		}
		if r.Page != q.Page {
			t.Fatalf("页码回显失真：%d want %d", r.Page, q.Page)
		}
	}
}

// 溢出守卫只能挡"真的溢出"：恰好落在边界上的合法翻页不得被一起挡掉。
// 若把守卫写成 `q.Page > N`，这里就会静默少一页，而用户看到的是"最后一组不见了"。
func TestResultGroupsPageBoundaryStillWorks(t *testing.T) {
	a, _, _, _, _ := procFixture(t)
	all := pagedNoPanic(t, a, ResultQuery{Page: 0, PageSize: 500})
	n := len(all.Groups)
	// 末组首页（start = n-1）必须还能拿到 1 组
	last := pagedNoPanic(t, a, ResultQuery{Page: n - 1, PageSize: 1})
	if len(last.Groups) != 1 {
		t.Fatalf("最后一页应剩 1 组，实得 %d", len(last.Groups))
	}
	// 恰好越界的下一页（start = n）给空页
	after := pagedNoPanic(t, a, ResultQuery{Page: n, PageSize: 1})
	if len(after.Groups) != 0 {
		t.Fatalf("start=len 应为空页，实得 %d 组", len(after.Groups))
	}
	// 乘积恰好不溢出的极大页：MaxInt64/2 × 2 = MaxInt64-1，仍走正常判空路径
	big := pagedNoPanic(t, a, ResultQuery{Page: math.MaxInt / 2, PageSize: 2})
	if len(big.Groups) != 0 || big.Total != all.Total {
		t.Fatalf("未溢出的大页应给空页且回传总数：%+v", big)
	}
}

// ---------- M10b ----------

// 损坏的设置文件：回到**纯**默认值、留证、并且让界面知道。
// 两种坏法分别取证（修正前 `_ = json.Unmarshal` 都只回一个无声的结果）：
//   - 语法错：Unmarshal 先整体校验，一个字段都不写 → 静默回默认，设置"凭空丢了"；
//   - 字段类型错：出错之前解到的字段已写进 s → 返回"解析到哪算哪"的半成品
//     （Threads=9、Theme=dark 会一路带到界面上，下一次保存再把它写回磁盘盖掉证据）。
func TestGetSettingsCorruptIsPureDefaultAndVisible(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"语法截断", `{"threads": 9, "theme": "dark", "language":`},
		{"字段类型错", `{"threads": 9, "theme": "dark", "language": 5}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, rec := newHistApp(t)
			path := mustSettingsPath(t, a)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}

			got := a.GetSettings()
			if !isDefaultSettings(got) {
				t.Fatalf("解析失败必须回到纯默认值，实得 %+v（默认 %+v）", got, defaultSettings())
			}
			if countEvent(rec, "app:error") == 0 {
				t.Fatal("设置损坏必须报一条 app:error，否则用户以为自己的设置还在")
			}
			msg := ledgerErrText(t, rec)
			if !strings.Contains(msg, "设置") {
				t.Fatalf("app:error 文案应说明是设置文件：%q", msg)
			}
			// 证据必须保住：改名为 .corrupt，字节原样，用户可自行恢复
			kept, err := os.ReadFile(path + ".corrupt")
			if err != nil {
				t.Fatalf("损坏文件未留证（应改名为 settings.json.corrupt）：%v", err)
			}
			if string(kept) != tc.content {
				t.Fatalf("留证内容被改写：%q", kept)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("原路径应已让位给 .corrupt，实得 err=%v", err)
			}
		})
	}
}

// 反面：正常配置与首次运行都不得被牵连——
// 合法文件要原样读出、不报错、不改名；文件不存在时静默给默认值
// （首次运行是常态，报"设置损坏"是假警）。
func TestGetSettingsValidAndMissingUnaffected(t *testing.T) {
	a, rec := newHistApp(t)
	if err := os.MkdirAll(filepath.Dir(mustSettingsPath(t, a)), 0o755); err != nil {
		t.Fatal(err)
	}

	got := a.GetSettings()
	if !isDefaultSettings(got) {
		t.Fatalf("首次运行应静默给默认值，实得 %+v", got)
	}
	if countEvent(rec, "app:error") != 0 {
		t.Fatal("首次运行不该报设置损坏")
	}

	saved, err := a.SaveSettings(Settings{Threads: 4, Theme: "dark", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	again := a.GetSettings()
	if again.Threads != saved.Threads || again.Theme != saved.Theme || again.Language != saved.Language {
		t.Fatalf("合法配置读回失真：%+v want %+v", again, saved)
	}
	if countEvent(rec, "app:error") != 0 {
		t.Fatalf("合法配置不该报错：%s", ledgerErrText(t, rec))
	}
	if _, err := os.Stat(mustSettingsPath(t, a) + ".corrupt"); err == nil {
		t.Fatal("合法配置被误改名为 .corrupt")
	}
}

// ---------- M10c ----------

// keepAndStaleIDs 从当前结果集取一个保留项与一个冗余项。
func keepAndStaleIDs(t *testing.T, a *App) (keepID, redundantID uint64) {
	t.Helper()
	r, err := a.GetResultGroups(ResultQuery{PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range r.Groups {
		for _, f := range g.Files {
			if f.IsKeep && keepID == 0 {
				keepID = f.ID
			} else if !f.IsKeep && redundantID == 0 {
				redundantID = f.ID
			}
		}
	}
	if keepID == 0 || redundantID == 0 {
		t.Fatalf("夹具需同时含保留项与冗余项，实得 keep=%d redundant=%d", keepID, redundantID)
	}
	return keepID, redundantID
}

// 未启用处理策略时的"生效范围"必须与执行侧同一内核：
// 保留项不入账、不被处理（keep.go 的 `continue` 与 planOpItems 的
// "在结果集内且未被标为保留"），结果集外的 id 同理。
// 修正前这一路直接 `append(EffectiveIDs, selectedIDs...)`，三项全算生效。
func TestPreviewNoDirsExcludesKeepAndStale(t *testing.T) {
	a, _, _, insideDir, _ := procFixture(t)
	keepID, redundantID := keepAndStaleIDs(t, a)
	const staleID = uint64(1 << 40) // 结果集外的 id（历史裁剪后/伪造入参）

	sel := []uint64{keepID, redundantID, staleID}
	pv, err := a.PreviewProcessPolicy(nil, nil, sel)
	if err != nil {
		t.Fatal(err)
	}
	if pv.EffectiveCount != 1 || len(pv.EffectiveIDs) != 1 || pv.EffectiveIDs[0] != redundantID {
		t.Fatalf("未启用策略时生效集应为 {冗余项}，实得 %d/%v（保留项与结果集外 id 不该算进去）",
			pv.EffectiveCount, pv.EffectiveIDs)
	}
	if len(pv.UnmatchedDirs) != 0 {
		t.Fatalf("未启用时不该报未命中：%v", pv.UnmatchedDirs)
	}

	// 重复勾选只算一次：执行侧 planOpItems 用 seen 去重，预览必须同口径，
	// 否则"将处理 N"会大于实际写入账本的条目数。
	dup, err := a.PreviewProcessPolicy(nil, nil, []uint64{redundantID, redundantID})
	if err != nil {
		t.Fatal(err)
	}
	if dup.EffectiveCount != 1 {
		t.Fatalf("重复 id 应去重为 1，实得 %d/%v", dup.EffectiveCount, dup.EffectiveIDs)
	}

	// 启用策略的一路同样去重（保留项那一路由 ApplyProcessPolicyWith 剔除）
	inside := idsInDir(t, a, insideDir)
	both, err := a.PreviewProcessPolicy([]string{insideDir}, nil,
		[]uint64{inside.ids[0], inside.ids[0], keepID})
	if err != nil {
		t.Fatal(err)
	}
	if both.EffectiveCount != 1 || both.EffectiveIDs[0] != inside.ids[0] {
		t.Fatalf("启用策略时重复勾选应去重为 1，实得 %d/%v", both.EffectiveCount, both.EffectiveIDs)
	}
}
