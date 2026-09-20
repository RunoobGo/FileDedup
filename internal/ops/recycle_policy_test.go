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
		before    RBState
		after     RBState
		wantErr   bool
		wantInErr []string // 错误信息必须包含的关键字
	}{
		{
			name:    "正常回收_条目数增加",
			still:   nil,
			before:  RBState{`F:\`: 3},
			after:   RBState{`F:\`: 5},
			wantErr: false,
		},
		{
			name:    "正常回收_跨盘各自增加",
			still:   nil,
			before:  RBState{`F:\`: 3, `E:\`: 7},
			after:   RBState{`F:\`: 4, `E:\`: 9},
			wantErr: false,
		},
		{
			name:    "★静默永久删除_文件消失但条目未增加",
			still:   nil,
			before:  RBState{`F:\`: 3},
			after:   RBState{`F:\`: 3}, // 没变
			wantErr: true,
			wantInErr: []string{
				"静默永久删除", "F:\\", "3 → 3",
			},
		},
		{
			name:    "★静默永久删除_条目数反而减少",
			still:   nil,
			before:  RBState{`F:\`: 5},
			after:   RBState{`F:\`: 2}, // 被别处清理了
			wantErr: true,
			wantInErr: []string{
				"静默永久删除", "5 → 2",
			},
		},
		{
			name:      "★源仍在_返回成功是假的",
			still:     []string{`F:\dup\a.bin`},
			before:    RBState{`F:\`: 3},
			after:     RBState{`F:\`: 3},
			wantErr:   true,
			wantInErr: []string{"仍存在于原路径", `F:\dup\a.bin`},
		},
		{
			name:    "回收站不可查询_跳过判据2只信判据1",
			still:   nil,
			before:  RBState{}, // 事前就没查到 → 不参与
			after:   RBState{`F:\`: 9},
			wantErr: false,
		},
		{
			name:    "事后不可查询_跳过该卷",
			still:   nil,
			before:  RBState{`F:\`: 3},
			after:   RBState{}, // 事后查不到 → 跳过
			wantErr: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkRecycled(c.still, c.before, c.after)
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
	before := RBState{`F:\`: 1, `E:\`: 1, `D:\`: 1}
	after := RBState{`F:\`: 1, `E:\`: 1, `D:\`: 1} // 三卷全未增加
	// 跑多次，断言错误信息里的卷根恒为字典序最小的 `D:\`
	for i := 0; i < 50; i++ {
		err := checkRecycled(nil, before, after)
		if err == nil {
			t.Fatal("三卷均未增加，应检出静默删除")
		}
		if !strings.Contains(err.Error(), `D:\`) {
			t.Fatalf("第 %d 次：错误信息未指向字典序最小的卷 D:\\ —— %v", i, err)
		}
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
