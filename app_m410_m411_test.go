package main

// M410 设置腿 ＋ M411 opID 腿（04 §6.80 两格 B 态 → §6.81 销格）。
//
// 两格同族：修正本体在位、判据为零。
//   - M410 设置腿：`app_settings.go` 的 `> dedup.MaxThreads → 64` 与引擎同口径的第二层钳，
//     改前 `grep -rn "MaxThreads" --include='*_test.go' .` 零命中；
//   - M411 opID 腿：`app_ops.go` 的 `ops-<时分秒>-<taskSeq>`，改前 `app_p3_test.go` 里
//     `opID`／`"ops-` 零命中，全仓 37 处 `a.ExecuteOperation(` 测试调用全部丢弃返回串。
//     taskID 半边早有 `TestTaskIDsAreUnique`，opID 半边没有对应物。
//
// ★ 两个文件的期望值都写字面量 64 而不是 `dedup.MaxThreads`（钉阈值本身，理由见
//   internal/dedup/pipeline_threads_test.go 段头）。

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"filededup/internal/model"
)

// TestSaveSettingsThreadsClamp 落盘值必须与实际生效值同口径（P2 的那句"界面上不自相矛盾"）。
func TestSaveSettingsThreadsClamp(t *testing.T) {
	a := newTestApp(t)
	cases := []struct {
		why  string
		in   int
		want int
	}{
		{"负数归零（0 = 自动）", -1, 0},
		{"零档原样", 0, 0},
		{"下界原样", 1, 1},
		{"阈值上沿原样", 64, 64},
		{"刚越界钳住", 65, 64},
		{"离谱值钳住", 10000, 64},
	}
	for _, c := range cases {
		got, err := a.SaveSettings(Settings{Threads: c.in, Theme: "system", Language: "zh"})
		if err != nil {
			t.Fatalf("%s: SaveSettings(Threads=%d): %v", c.why, c.in, err)
		}
		if got.Threads != c.want {
			t.Errorf("%s: 返回的落盘值 = %d, want %d", c.why, got.Threads, c.want)
		}
		// 回读一遍：只钳返回值、磁盘上仍写着越界值，同样是对界面说谎。
		if again := a.GetSettings(); again.Threads != c.want {
			t.Errorf("%s: 回读值 = %d, want %d（磁盘残留越界值）", c.why, again.Threads, c.want)
		}
	}
}

// TestOperationIDsAreUnique 与 TestTaskIDsAreUnique 同形补 opID 半边（M411）。
//
// 三腿，全部不吃挂钟：opID 的时间段只有时分秒（`150405`），同秒两次必同串 ⇒
// 单断"互异"会随运气红或绿。形态腿钉住 `-%d` 这一段的存在，递增腿钉住它真的在涨。
func TestOperationIDsAreUnique(t *testing.T) {
	a, _, _ := newProbeApp(t)
	dir := t.TempDir()

	// 每次请求前把结果集重新铺平：清理收尾会回写 `a.groups`（app_ops.go:260），
	// 组内两个成员都指向不存在的路径 ⇒ 一趟下来全被跳过、`len(a.groups)` 归零，
	// 第二次请求撞 `:69` 的「暂无结果集」就返回空串。那不是本用例要检的事。
	plant := func() uint64 {
		g := mkGroup(1, 100, filepath.Join(dir, "gone-a"), filepath.Join(dir, "gone-b"))
		a.mu.Lock()
		a.resultsReady = true
		a.groups = []*model.DuplicateGroup{g}
		a.byID[g.Files[1].ID] = g.Files[1]
		a.mu.Unlock()
		return g.Files[1].ID
	}

	var ids []string
	for i := 0; i < 3; i++ {
		got, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{plant()}})
		if err != nil {
			t.Fatalf("ExecuteOperation#%d: %v（夹具穿不过受理门 ⇒ 返回串无法归因给被测代码）", i, err)
		}
		ids = append(ids, got)
		a.wg.Wait() // 等在途标志复位，否则下一次请求被互斥门拒掉，拿到的是空串
	}

	seqs := make([]int64, 0, len(ids))
	for i, s := range ids {
		parts := strings.Split(s, "-")
		if len(parts) != 3 || parts[0] != "ops" || len(parts[1]) != 6 {
			t.Errorf("#%d opID 形态异常 %q，want ops-<6位时分秒>-<序号>（退回纯时间戳正是少了尾号那一段）", i, s)
			continue
		}
		if _, terr := strconv.Atoi(parts[1]); terr != nil {
			t.Errorf("#%d opID 时间段不是数字: %q", i, s)
			continue
		}
		n, serr := strconv.Atoi(parts[2])
		if serr != nil {
			t.Errorf("#%d opID 序号段不是数字: %q", i, s)
			continue
		}
		seqs = append(seqs, int64(n))
	}

	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] == ids[j] {
				t.Errorf("opID 重复：第 %d 次与第 %d 次同为 %q（同秒两次请求必须靠序号段区分）", i, j, ids[i])
			}
		}
	}

	// 递增腿：序号若退化成常量，三次拿到的还是三个不同串（跨秒时），互异腿会漏这一格。
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("opID 序号未严格递增: %d → %d（taskSeq.Add(1) 被改成常量就是这个形状）", seqs[i-1], seqs[i])
		}
	}
}
