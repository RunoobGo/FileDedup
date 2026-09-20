package ops

import (
	"strings"
	"testing"
)

// 2026-09-20 缺陷回归：跨硬盘「移入回收站」后文件被**静默永久删除**。
//
// 根因回顾：SHFileOperationW + FOF_ALLOWUNDO 是"尽力而为"，不是保证。
// 当文件无法进入回收站时（回收站被策略禁用、单文件超出回收站配额、
// 该卷 $Recycle.Bin 不可用），Shell 会**永久删除该文件并返回 0（成功）**。
// 原实现只看 r0==0 就记为"成功移入回收站"，于是一批文件从磁盘上彻底
// 消失、回收站里却什么都没有——数据丢失级缺陷。
//
// 本文件测试的是**平台无关的判定逻辑**（recycle_policy.go），
// 因此能在 Linux CI 上执行——上一轮的教训是"只写在 windows 文件里的
// 防线从来没被自动化门禁跑过"，这次刻意把纯逻辑下沉出来。

// TestCheckRecycledSilentPermanentDelete 核心回归：文件已消失、
// 但回收站条目数没增加 —— 这正是"疑似被直接删除"的根因判据。
//
// 修正前该函数不存在（判定只看 r0==0），故本用例在修正前**无法编译**；
// 这是"缺陷必然被这道测试拦住"的最强形式。
func TestCheckRecycledSilentPermanentDelete(t *testing.T) {
	cases := []struct {
		name      string
		still     []string
		expected  RBState
		before    RBState
		after     RBState
		wantErr   bool
		wantInErr []string // 错误信息必须包含的关键字
	}{
		{
			name:     "正常回收_条目数增加",
			still:    nil,
			expected: RBState{`F:\`: 2},
			before:   RBState{`F:\`: 3},
			after:    RBState{`F:\`: 5},
			wantErr:  false,
		},
		{
			name:     "正常回收_跨盘各自增加",
			still:    nil,
			expected: RBState{`F:\`: 1, `E:\`: 2},
			before:   RBState{`F:\`: 3, `E:\`: 7},
			after:    RBState{`F:\`: 4, `E:\`: 9},
			wantErr:  false,
		},
		{
			name:     "★静默永久删除_文件消失但条目未增加",
			still:    nil,
			expected: RBState{`F:\`: 1},
			before:   RBState{`F:\`: 3},
			after:    RBState{`F:\`: 3}, // 没变
			wantErr:  true,
			wantInErr: []string{
				"静默永久删除", "F:\\", "3 → 3",
			},
		},
		{
			name:     "★静默永久删除_条目数反而减少",
			still:    nil,
			expected: RBState{`F:\`: 1},
			before:   RBState{`F:\`: 5},
			after:    RBState{`F:\`: 2}, // 被别处清理了
			wantErr:  true,
			wantInErr: []string{
				"静默永久删除", "5 → 2",
			},
		},
		{
			name:      "★源仍在_返回成功是假的",
			still:     []string{`F:\dup\a.bin`},
			expected:  RBState{`F:\`: 1},
			before:    RBState{`F:\`: 3},
			after:     RBState{`F:\`: 3},
			wantErr:   true,
			wantInErr: []string{"仍存在于原路径", `F:\dup\a.bin`},
		},
		{
			name:     "回收站不可查询_跳过判据2只信判据1",
			still:    nil,
			expected: RBState{}, // 事前就没查到 → 不参与
			before:   RBState{},
			after:    RBState{`F:\`: 9},
			wantErr:  false,
		},
		{
			name:     "事后不可查询_跳过该卷",
			still:    nil,
			expected: RBState{`F:\`: 1},
			before:   RBState{`F:\`: 3},
			after:    RBState{}, // 事后查不到 → 跳过
			wantErr:  false,
		},
		{
			name: "★部分丢失_同卷3个只进了2个",
			// 这是 2026-09-20 加固的场景：旧判据只查 "nAfter > nBefore"，
			// 3 → 4 的真值使旧实现**通过**，而实际少了 1 个文件。
			still:    nil,
			expected: RBState{`F:\`: 3},
			before:   RBState{`F:\`: 10},
			after:    RBState{`F:\`: 12}, // 只 +2，少了 1 个
			wantErr:  true,
			wantInErr: []string{
				"静默永久删除", "应有 3 个", "只增加了 2", "缺失 1 个",
			},
		},
		{
			name: "★部分丢失_大文件超配额被丢其余入站",
			// 触发本缺陷的真实路径：同卷批量 [50GiB 视频, 1MiB 文本]，
			// 回收站配额 10GiB → 视频被静默永久删除、文本正常入站。
			still:     nil,
			expected:  RBState{`F:\`: 2},
			before:    RBState{`F:\`: 5},
			after:     RBState{`F:\`: 6}, // 视频那份没进 → 只 +1
			wantErr:   true,
			wantInErr: []string{"静默永久删除", "缺失 1 个"},
		},
		{
			name:      "跨盘_其中一盘部分丢失",
			still:     nil,
			expected:  RBState{`F:\`: 1, `E:\`: 3},
			before:    RBState{`F:\`: 1, `E:\`: 1},
			after:     RBState{`F:\`: 2, `E:\`: 2}, // E 盘只 +1，应 +3
			wantErr:   true,
			wantInErr: []string{`E:\`, "应有 3 个", "缺失 2 个"},
		},
		{
			name: "预期数未知_退化为旧行为不误报",
			// expected 里没有该卷（调用方统计不到卷根）时，只保留
			// "条目不得减少"这一半判据，避免误报。
			still:    nil,
			expected: RBState{},
			before:   RBState{`F:\`: 3},
			after:    RBState{`F:\`: 4},
			wantErr:  false,
		},
		{
			name:      "预期数未知_但条目零增长仍报错",
			still:     nil,
			expected:  RBState{},
			before:    RBState{`F:\`: 3},
			after:     RBState{`F:\`: 3},
			wantErr:   true,
			wantInErr: []string{"静默永久删除"},
		},
		{
			name: "★增量超出预期_不报错",
			// 回收站条目可能因其它进程同时删除文件而多增；
			// 增量 ≥ 预期即为满足，多出来的不是我们该管的。
			still:    nil,
			expected: RBState{`F:\`: 2},
			before:   RBState{`F:\`: 3},
			after:    RBState{`F:\`: 9},
			wantErr:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkRecycled(c.still, c.expected, c.before, c.after)
			if c.wantErr && err == nil {
				t.Fatalf("应当检出问题却返回 nil —— 这就是「疑似被直接删除」漏网的路径")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("不应报错却得到: %v", err)
			}
			for _, kw := range c.wantInErr {
				if err == nil || !strings.Contains(err.Error(), kw) {
					t.Fatalf("错误信息 %v 未包含关键字 %q", err, kw)
				}
			}
		})
	}
}

// TestCheckRecycledMultiVolumePicksSortedFirst 多卷同时违约时，
// 错误信息必须稳定指向按卷根排序后的第一个——否则用户与测试
// 看到的"首个违约卷"会随 map 遍历顺序漂移（Go map 顺序是随机的）。
func TestCheckRecycledMultiVolumePicksSortedFirst(t *testing.T) {
	expected := RBState{`F:\`: 1, `E:\`: 1, `D:\`: 1}
	before := RBState{`F:\`: 1, `E:\`: 1, `D:\`: 1}
	after := RBState{`F:\`: 1, `E:\`: 1, `D:\`: 1} // 三卷全未增加
	// 跑多次，断言错误信息里的卷根恒为字典序最小的 `D:\`
	for i := 0; i < 50; i++ {
		err := checkRecycled(nil, expected, before, after)
		if err == nil {
			t.Fatal("三卷均未增加，应检出静默删除")
		}
		if !strings.Contains(err.Error(), `D:\`) {
			t.Fatalf("第 %d 次：错误信息未指向字典序最小的卷 D:\\ —— %v", i, err)
		}
	}
}

// TestCheckRecycledPartialLossIsDetected 2026-09-20 加固的**核心**回归：
// 同卷部分文件被静默永久删除时，旧判据（只查 nAfter > nBefore）会放行，
// 新判据（增量 ≥ 该卷预期入站数）必须拦住。
//
// 这条用例的价值在于它**在旧实现上会失败**：expected 参数不存在时无法编译，
// 而把 expected 全填 1（旧行为等价）就会让本例的 wantErr 落空。
func TestCheckRecycledPartialLossIsDetected(t *testing.T) {
	// 5 个文件同卷，回收站只多了 4 个 → 1 个被静默永久删除。
	// before=7, after=11 → 增量 4 > 0，旧判据通过；新判据 4 < 5 拦下。
	err := checkRecycled(nil, RBState{`F:\`: 5}, RBState{`F:\`: 7}, RBState{`F:\`: 11})
	if err == nil {
		t.Fatal("同卷 5 个文件只入站 4 个，必须检出——旧判据（只看增量>0）在此漏检")
	}
	for _, kw := range []string{"静默永久删除", "应有 5 个", "只增加了 4", "缺失 1 个", "数据恢复工具"} {
		if !strings.Contains(err.Error(), kw) {
			t.Fatalf("错误信息 %v 未包含 %q", err, kw)
		}
	}
}

// TestExpectedRecycledPerVolume 预期数统计的口径：每源路径按其卷各计 1，
// 拿不到卷根的路径不计入。
//
// 本测试在任意平台都要能跑，因此临时注入一个与 Windows driveRoot 同形态的
// 解析器（`X:\…` → `X:\`），跑完立刻还原——真实的 driveRoot 只在 Windows
// 侧由 init 挂载（见 trash_windows.go），Linux CI 上本来就没有实现。
func TestExpectedRecycledPerVolume(t *testing.T) {
	orig := volRootFn
	volRootFn = func(p string) string {
		if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
			return ""
		}
		return p[:2] + `\`
	}
	t.Cleanup(func() { volRootFn = orig })

	got := expectedRecycledPerVolume([]string{
		`F:\dup\a.bin`, `F:\dup\b.bin`, `F:\dup\sub\c.bin`,
		`E:\other\d.bin`,
		`relative\no\drive.bin`, // 无盘符 → 返回 "" → 不计入
	})
	want := RBState{`F:\`: 3, `E:\`: 1}
	if len(got) != len(want) {
		t.Fatalf("卷数不符: got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("卷 %s 预期数 got %d, want %d（全部: %v）", k, got[k], v, got)
		}
	}
}

// TestExpectedRecycledPerVolumeWithoutRootFn 未挂载卷根解析器时
// （非 Windows 平台），返回空表而非 panic——判据 2 应退化为"不得减少"。
func TestExpectedRecycledPerVolumeWithoutRootFn(t *testing.T) {
	orig := volRootFn
	volRootFn = nil
	t.Cleanup(func() { volRootFn = orig })

	got := expectedRecycledPerVolume([]string{`F:\a.bin`, `E:\b.bin`})
	if len(got) != 0 {
		t.Fatalf("无卷根解析器时应返回空表，got %v", got)
	}
}

// TestCheckRecycledUnionNoBaselineIsNotMisreported 判据 2 的"卷没有增量"分支
// **必须要求 before 里有该卷**。
//
// 这是一个假阳性回归测试：E 盘事前查不到（before 缺 E）→ nBefore 取值 0 →
// 若把"条目数没增加"直接拿去判，会把"事前不可查、事后恢复可查"的卷误判成
// 静默永久删除，让用户收到一条纯属虚构的数据丢失告警。没有基准就谈不上
// "增量不足"，此时只能退回到判据 1（源是否消失）。
//
// 场景取自真实可复现场景：同事的移动硬盘 E: 在操作期间被拔掉/休眠，
// SHQueryRecycleBinW 事前返回失败，操作后又插上了。
func TestCheckRecycledUnionNoBaselineIsNotMisreported(t *testing.T) {
	err := checkRecycled(nil,
		RBState{`F:\`: 1, `E:\`: 2},
		RBState{`F:\`: 5},             // 事前只查到 F（E 未挂载）
		RBState{`F:\`: 6, `E:\`: 100}, // F 正常 +1；E 事后才可查
	)
	if err != nil {
		t.Fatalf("E 盘无事前基准，不得凭 0 基准报数据丢失（假阳性）：%v", err)
	}
}

// TestCheckRecycledUnionIncludesExpectedOnlyVolumes 反过来，when **有基准**时，
// 预期数已知的卷即使条目数丝毫未增，也必须被检出——这是 D2 修复的核心：
// "整卷静默永久删除"（回收站被策略禁用）在旧判据下也是漏检的。
func TestCheckRecycledUnionIncludesExpectedOnlyVolumes(t *testing.T) {
	// E 盘预期 2 个入站，事前 7、事后仍是 7 → 增量 0 < 2 → 检出，且指向 E 盘。
	err := checkRecycled(nil,
		RBState{`F:\`: 1, `E:\`: 2},
		RBState{`F:\`: 5, `E:\`: 7},
		RBState{`F:\`: 6, `E:\`: 7},
	)
	if err == nil {
		t.Fatal("E 盘预期 2 个入站但条目零增长（有基准）→ 必须检出")
	}
	if !strings.Contains(err.Error(), `E:\`) {
		t.Fatalf("错误信息未指向 E 盘: %v", err)
	}
	if !strings.Contains(err.Error(), "应有 2 个") {
		t.Fatalf("错误信息应点明预期数: %v", err)
	}
	// F 盘预期 1、实际 +1，属于正常，不该被误报。
	if strings.Contains(err.Error(), `F:\`) {
		t.Fatalf("F 盘增量达标，不应出现在错误信息里: %v", err)
	}
}

// TestCapacityReason 单文件 vs 回收站容量上限。
func TestCapacityReason(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		name     string
		fileSize int64
		capBytes int64
		wantErr  bool
	}{
		{"小文件装得下", 10 << 20, 50 * gib, false},
		{"恰好等于上限_放行", 50 * gib, 50 * gib, false},
		{"★超出上限_正是静默删除的场景", 60 * gib, 50 * gib, true},
		{"远超上限", 200 * gib, 5 * gib, true},
		{"上限未知_放行交事后复核", 60 * gib, 0, false},
		{"上限为负_视为未知", 60 * gib, -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := capacityReason(c.fileSize, c.capBytes)
			if c.wantErr && why == "" {
				t.Fatalf("文件 %s 超过上限 %s，应当拒绝", humanSize(c.fileSize), humanSize(c.capBytes))
			}
			if !c.wantErr && why != "" {
				t.Fatalf("不应拒绝却得到: %q", why)
			}
		})
	}
}

// TestCapacityReasonMessageIsActionable 拒绝原因必须**说清楚**，
// 而不是只丢一个数字。用户需要知道"为什么"和"怎么办"。
func TestCapacityReasonMessageIsActionable(t *testing.T) {
	why := capacityReason(60<<30, 50<<30)
	for _, kw := range []string{"60.00 GiB", "50.00 GiB", "静默永久删除"} {
		if !strings.Contains(why, kw) {
			t.Fatalf("拒绝原因 %q 缺少关键信息 %q", why, kw)
		}
	}
}

// TestHumanSize 边界与格式。
func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1 << 20, "1.0 MiB"},
		{int64(1) << 30, "1.00 GiB"},
		{int64(1) << 40, "1.00 TiB"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Fatalf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSortedKeysIsDeterministic 锁死排序契约——checkRecycled 的
// 错误信息稳定性完全依赖它。
func TestSortedKeysIsDeterministic(t *testing.T) {
	m := RBState{`F:\`: 1, `C:\`: 1, `E:\`: 1, `D:\`: 1}
	want := []string{`C:\`, `D:\`, `E:\`, `F:\`}
	for i := 0; i < 20; i++ {
		got := sortedKeys(m)
		if len(got) != len(want) {
			t.Fatalf("长度不符: %v", got)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("第 %d 次：got %v, want %v", i, got, want)
			}
		}
	}
}
