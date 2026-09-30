package main

// 第八轮批 3（拟 M373）P-33：`beginJournal` 的留痕放行支不许假设 emit 已就位。
//
// 取证在册（§3.1g）：同文件的 `warnBackground` 带 `a.emit != nil && a.ctx != nil` 守卫，
// 而 C3 裁定那条"不可回撤类：留痕放行"用的是裸 `a.emit(a.ctx, …)` 两行。
// 可达路径不是假想：`a.ctx` 要等 `startup` 才接线（`NewApp` 只接 emit、不接 ctx），而这一支是 C3 裁定给"永久删除 / Windows 回收站"留的那条留痕放行——
// 同文件的 `recoverGoroutine` 之所以存在，理由写得很清楚：任何 panic 都会
// **直接带走整个进程**（用户看到的现象是"点了一下，软件消失"）。
//
// ★ 判据只到"不 panic + 仍按 C3 放行"这一格；stderr 那行的**逐字不变**由
//   MU-23 那一侧的对账保证（改法是把两行换成 warnBackground，不是改话术）。

import (
	"testing"

	"filededup/internal/model"
)

func TestM373JournalWarnBranchSurvivesWithoutEmitter(t *testing.T) {
	a := NewApp()
	// 夹具形状：emit 是 `NewApp` 默认接的真 runtime，而 ctx 还没接线（startup 之前、
	// 或测试里裸构造就是这个形状）。改前这一支把裸 `a.ctx` 递给 `wruntime.EventsEmit`。
	a.ctx = nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("M373：留痕放行支在 ctx 未就位时 panic（用户侧=软件消失）：%v", r)
		}
	}()
	groups := []*model.DuplicateGroup{mkGroup(1, 10, "/journal/a.bin", "/journal/b.bin")}
	jid, err := a.beginJournal(nil, 0, model.OpRequest{
		Kind: "delete", FileIDs: []uint64{101},
	}, groups, map[uint64]bool{100: true})
	if err != nil {
		t.Errorf("M373：不可撤类在账本不可用时应按 C3 放行（留痕但不拒），实际拒了：%v", err)
	}
	if jid != 0 {
		t.Errorf("M373：账本不可用却返回了账本号 %d", jid)
	}
}
