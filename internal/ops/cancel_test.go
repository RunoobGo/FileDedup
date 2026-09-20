package ops

// P2 回归：清理操作必须可中止，且中止结果可解释。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/model"
)

// TestExecuteHonoursContextCancel 取消后未派发的条目计入 Cancelled，
// 且不得有任何条目被静默吞掉。
//
// ---------------------------------------------------------------------------
// 2026-09-20 修复「机器有负载时间歇性失败」——**这是用例自身的缺陷，不是被测代码的缺陷**
//
// 症状：CI（或本机并发跑多个 go test 时）间歇性报
//
//	「取消过晚，说明派发未被中止: 42」
//
// 我先按"文件名可能折叠"改过一次（`string(rune('A'+i))` → `x%03d.bin`），
// **结果失败率反而升到 19/20**，说明那个猜测是错的。于是改为实测：
//
//	无负载（单跑十次，逐字相同）：总计 32ms ｜ trash 调用 13 次 ｜ ok=12 cancelled=30
//	有负载（24 路并发）        ：总计 93ms ｜ trash 调用  5 次 ｜ ok= 4 cancelled=38
//	                          极端时                  TrashFn 首次调用 >30ms ｜ ok= 0 cancelled=42
//
// 用同一套 20 路并发对**基线仓库**（不含任何软链接改动）复测：**18/20 失败**，
// 报错信息完全相同 ⇒ 这个用例从来就没稳定过，与本次改动无关。
//
// 根因有两条，都要修：
//
//  1. **断言语义有歧义。** 原断言是 `len(Cancelled) == len(ids)`，但
//     "42 项全部取消"既可能是"取消及时、一项没做"（用例想要的），
//     也可能是"取消发生在首次派发之前、什么都没派发过"（用例一点都不想验证的场景）。
//     这两种情况给出同一个数字，测试却把它们当同一件事处理。
//     负载高时后者更容易发生（首次派发被推迟到 30–60ms，超过了固定的 30ms 取消时延），
//     于是用例失败——但它其实**什么代码缺陷都没发现**。
//
//  2. **取消计时锚点错了。** 计时从"进程开始跑"算起，而真正该被约束的是
//     "首次派发之后多久取消"。首次派发耗时本身受负载影响（40 次文件写入 +
//     42 次 Stat + 逐文件 BLAKE3 复核），把它算进取消窗口就等于把调度抖动
//     直接叠进断言。
//
// 修法（对应上面两条）：
//
//	① 取消计时**锚定在首次派发上**：TrashFn 第一次被调用时通知测试开始计时
//	   （cancelCh 握手），测试再等一段固定时间才 cancel。首次派发多慢都不影响结论。
//	② 断言拆成两条无歧义的：至少完成 1 项（否则用例失去判别力，直接判失败）
//	   + 至少取消 1 项（取消确实拦截了后续派发）。原来那条 42 的相等断言删掉。
//	③ 项内耗时 10ms→20ms，取消延迟 30ms→80ms，给"已完成 1 项以上、
//	   尚有 1 项以上未派发"留出真实裕度，不再卡在临界点。
//
// 断言里**保留** `acc == len(ids)`（条目不得被吞）与 `len(OK) < len(ids)`（取消必须生效），
// 它们没有歧义。文件名也顺手改成纯 ASCII 数字（去掉了 `\` 这类跨平台歧义字符），
// 但那只是清理，不是修复的关键。
// ---------------------------------------------------------------------------
func TestExecuteHonoursContextCancel(t *testing.T) {
	fx := newFixture(t)
	dir := t.TempDir()
	content, _ := os.ReadFile(fx.orig.Path)
	extra := []*model.FileEntry{fx.orig, fx.dup1, fx.dup2}
	for i := 0; i < 40; i++ {
		// 纯数字后缀：跨平台无歧义，且不与 fx 的文件名重叠。
		p := filepath.Join(dir, fmt.Sprintf("x%03d.bin", i))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		extra = append(extra, &model.FileEntry{
			ID: uint64(100 + i), Path: p, Size: uint64(st.Size()),
			ModTime: st.ModTime().UnixNano(), Ext: ".bin",
		})
	}
	fx.group.Files = extra

	ids := make([]uint64, 0, len(extra))
	for _, e := range extra {
		if e.ID != fx.orig.ID {
			ids = append(ids, e.ID)
		}
	}

	// 构造可被取消打断的慢速回收站：
	//   批量调用（len>1）先失败 → 触发执行器退化为逐文件派发（runIndexed），
	//   每项固定耗时 20ms，使 42 项总量（约 840ms）远大于取消时延 → 部分完成可复现。
	// delete/move 同样逐条派发，但小文件删除过快，取消总在派发完毕后才到达。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := filepath.Join(t.TempDir(), "trash")

	// 取消计时锚定在"首次派发"上，而不是进程启动时刻。
	// 见文件头注释根因 2：首次派发自身耗时会随负载漂移，不能算进取消窗口。
	var dispatchCount atomic.Int64
	var firstDispatchAt atomic.Int64
	start := time.Now()
	cancelCh := make(chan struct{})
	trashCh := make(chan struct{})
	go func() {
		<-cancelCh // ← 等 TrashFn 第一次被调用，此刻才是计时的零点
		time.Sleep(80 * time.Millisecond)
		firstDispatchAt.Store(time.Since(start).Milliseconds())
		cancel()
		close(trashCh)
	}()

	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		Ctx:     ctx,
		TrashFn: func(paths []string) (map[string]string, error) {
			if len(paths) > 1 {
				return nil, errFakeBatch // 强制走逐文件隔离路径
			}
			if dispatchCount.Add(1) == 1 {
				close(cancelCh) // 首次派发已发生，放行取消计时
			}
			time.Sleep(20 * time.Millisecond)
			return mockTrash(target, false)(paths)
		},
	}, model.OpRequest{Kind: "trash", FileIDs: ids})
	<-trashCh

	// 前置自检（2026-09-20）：确认构造出来的正是 42 个**互不相同**的路径。
	// 若将来又有人改动文件名生成方式引入折叠，这里会先给出"数据构造有问题"
	// 而不是让读者去猜"为什么取消没生效"——把一个误导性的失败变成一个自解释的失败。
	if len(ids) != 42 {
		t.Fatalf("前置构造错误：应有 42 个待处理 id，实得 %d", len(ids))
	}
	uniq := make(map[string]bool, len(extra))
	for _, e := range extra {
		uniq[e.Path] = true
	}
	if len(uniq) != len(extra) {
		t.Fatalf("前置构造错误：路径有重复（%d 条路径只剩 %d 个唯一值），"+
			"账本以路径为键，重复会让取消断言失去意义", len(extra), len(uniq))
	}

	// 判别力自检：取消必须发生在"至少派发过一次、且尚未派发完"的窗口内。
	// 这两条合起来才排除掉"取消太早、什么都没跑"与"取消太晚、全都跑完"两种失效场景。
	// 旧断言 `len(res.Cancelled) >= len(ids)` 无法区分它们，正是间歇失败的来源。
	if dispatchCount.Load() < 2 {
		t.Fatalf("用例失去判别力：只派发了 %d 次，取消窗口没有覆盖到派发"+
			"（负载异常高时会出现，属测试环境问题，非代码缺陷）", dispatchCount.Load())
	}
	if firstDispatchAt.Load() == 0 {
		t.Fatal("用例失去判别力：取消未按首次派发锚定")
	}

	acc := len(res.OK) + len(res.Failed) + len(res.Skipped) + len(res.Cancelled)
	if acc != len(ids) {
		t.Fatalf("条目被吞: 请求 %d, 归类 %d (ok=%d fail=%d skip=%d cancel=%d)",
			len(ids), acc, len(res.OK), len(res.Failed), len(res.Skipped), len(res.Cancelled))
	}
	// 取消必须真的生效：全部跑完 = 取消没起作用。
	if len(res.OK) == len(ids) {
		t.Fatal("取消未生效：全部条目仍执行完毕")
	}
	// 至少完成一项：否则本用例根本没验证到"取消打断了进行中的工作"。
	// 注意方向与原断言相反——原断言把"一项没做"当成功，这里当失败，
	// 因为"一项没做"意味着取消发生在首次派发之前，用例什么都没证明。
	if len(res.OK) == 0 {
		t.Fatal("一项都没完成：取消发生在首次派发之前，用例失去判别力")
	}
	// 至少取消一项：否则取消到达时派发已全部结束，同样没验证到中止。
	if len(res.Cancelled) == 0 {
		t.Fatal("取消后未派发项应计入 Cancelled：取消到达时派发已全部结束，用例失去判别力")
	}
}

var errFakeBatch = errors.New("批量入口不可用（测试注入）")

// 预先取消 → 一项都不该执行，文件必须原样留在磁盘。
// 这里用 trash 类型验证"批量入口前已取消则整批不派发"。
func TestExecuteAlreadyCancelledDoesNothing(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(t.TempDir(), "t")
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		Ctx:     ctx,
		TrashFn: mockTrash(target, false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})
	if len(res.OK) != 0 {
		t.Fatalf("已取消时不得执行任何操作: %+v", res)
	}
	if len(res.Cancelled) != 2 {
		t.Fatalf("两项都应计入 Cancelled, got %d", len(res.Cancelled))
	}
	for _, p := range []string{fx.dup1.Path, fx.dup2.Path} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("取消后文件被移走: %s", p)
		}
	}
}

// 未取消时行为不变（Cancelled 恒为空，向后兼容）。
func TestExecuteWithoutCtxUnchanged(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
		TrashFn: mockTrash(filepath.Join(t.TempDir(), "t"), false),
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID}})
	if len(res.OK) != 1 || len(res.Cancelled) != 0 {
		t.Fatalf("无 Ctx 时应正常执行: %+v", res)
	}
}
