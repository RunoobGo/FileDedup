package ops

// ============================================================================
// R1-1（第四轮全仓审查·高危，设计稿 §30.1）：delete 腿在**内容复核之后、删除之前**
// 还有一道身份复核。
//
// 这条腿的守卫顺序与其余四腿相反（现读 executor.go）：
//
//	校验循环(:266) → [无界时间] → guardIdentity(:647) → guardContent(:651)
//	  → [窗口] → os.Remove(:654) 按**路径**删
//
// guardContent 那次全量 BLAKE3 对大文件是秒到分钟级，而哈希绑的是 open 那一刻的 fd：
// 重算期间第三方把路径 rename 顶替掉，哈希照样 VerdictPass，随后删的是**顶替者**，
// 账本记 done + 把 dup 的 size 计进 Reclaimed。delete 没有回收站、没有回撤，
// 是全管线唯一不可逆腿。
//
// ★ 顶替必须发生在 guardContent **之后**那一格，用两道既有缝配合钉住时点：
//   - beforeActContentRecheck 跑在 guardContent 入口 ⇒ 只用来"装弹"（记一个 flag）。
//     若在这一刻顶替，VerifyFile 读到的就是顶替者内容 ⇒ 红落在 M91 那格（"被修改"），
//     不是本条那一格；
//   - 换装的 verifyFileFn 先跑真校验，在**返回之前**按 flag 顶替，并把"哈希绑住的那个
//     fd 的身份"原样交回——那正是现状 guardContent 用 `v, _ :=` 丢掉的东西。
//
// ★ 修前红的面貌（§30.1 预测，跑前写明）：改前树里没有任何一道守卫接得住这一刻的顶替，
//   于是 `os.Remove` 删掉顶替者、条目记 OK ⇒ 红在 `Failed = []，want 恰好 1 条`，
//   而不是红在夹具前提上。
// ============================================================================

import (
	"os"
	"strings"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// r11Foreign 是"扫描之后才被 rename 顶进 dup 位置"的第三方内容。刻意与组内容
// 不同长度：长度相同会让"删掉的是哪一份"只能靠字节判，长度不同则连尺寸都对不上，
// 但两种读法都不影响本条判据（判据看的是身份，不是内容）。
var r11Foreign = []byte(strings.Repeat("R1-1-INTRUDER", 150)[:1950])

func TestDeleteLegRechecksIdentityAfterContentRecheck(t *testing.T) {
	fx := newFixture(t)
	victim := fx.dup1.Path

	// ★ 前提自检问在 Execute **之前**（§29.2 立的规矩）：本条要的是"顶替者带着另一个号"，
	//   而这一刻的顶替只在卷给稳定索引时才可能被识破。放在断言之后，"造不出前提"与
	//   "守卫放行删了文件"两种成因在同一个错误上分不开。
	needResolvedID(t, victim)

	// 夹具：第三方文件先把内容铺在 dup 之外（swapInAt 自己走"先写旁边、原子改名"）。
	origHook := beforeActContentRecheck
	var armed bool
	beforeActContentRecheck = func(p string) {
		if p == victim {
			armed = true // guardContent 入口：身份复核已过、内容复核未发
		}
	}
	t.Cleanup(func() { beforeActContentRecheck = origHook })

	type probeT struct {
		swaps    int
		intruder fsid.ID
		boundID  fsid.ID
		states   []string
	}
	probe := &probeT{}
	prevVerify := verifyFileFn
	verifyFileFn = func(e *model.FileEntry, h [32]byte, p *hasher.Pool) (Verdict, fsid.ID) {
		v, id := prevVerify(e, h, p)
		if e.Path != victim || !armed || v != VerdictPass {
			return v, id
		}
		armed = false
		// 顶替发生在"内容已经算完、复核函数即将返回"这一刻：绑住的 fd 还是原对象，
		// 路径已经指向另一个对象。返回的 id **不改**——它就是 guardContent 该交回调用方的那份身份。
		probe.boundID = id
		probe.intruder = swapInAt(t, victim, id, r11Foreign)
		probe.swaps++
		return v, id
	}
	t.Cleanup(func() { verifyFileFn = prevVerify })

	res := Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
		OnItem: func(r ItemResult) { probe.states = append(probe.states, r.State) },
	}, model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	// ---- 前提读数：顶替真的发生了，且真的换成了另一个号 ----
	if probe.swaps != 1 {
		t.Fatalf("前置条件未成立：dup 在内容复核之后被顶替 %d 次，期望 1 次"+
			"（0 次 = 本条什么都没验证；flag 没落到 guardContent 这一侧）", probe.swaps)
	}
	if !probe.boundID.Resolved || !probe.intruder.Resolved ||
		probe.boundID.Dev == probe.intruder.Dev && probe.boundID.Ino == probe.intruder.Ino {
		t.Fatalf("夹具前提不成立：绑定的 (dev=%x,ino=%d) 与顶替者 (dev=%x,ino=%d) 未区分开，"+
			"本卷会回收 inode 号，身份层原理上识破不了（红的是夹具，不是守卫）",
			probe.boundID.Dev, probe.boundID.Ino, probe.intruder.Dev, probe.intruder.Ino)
	}

	// ---- 判据格：拦下，且说的是"内容复核之后被替换"这一格 ----
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %+v，want 恰好 1 条（改前这里是一条都没拦住：顶替者被删、条目记 OK）", res.Failed)
	}
	fi := res.Failed[0]
	if fi.Path != victim {
		t.Fatalf("失败项路径不对（%q，期望 %q）", fi.Path, victim)
	}
	if fi.Stage != "verify" {
		t.Fatalf("Stage = %q，期望 verify：身份复核没过属校验阶段，记成 ops 会让上层以为"+"'动过盘、要按失败回滚'（M52/M54 的同一族口径）", fi.Stage)
	}
	if !strings.Contains(fi.Err, "内容复核通过后") {
		t.Fatalf("错误未标明来自「内容复核之后」那道复核（无法与 guardIdentity 那句区分）: %q", fi.Err)
	}
	if !strings.Contains(fi.Err, "被替换") {
		t.Fatalf("确证换成了另一个对象，这一格可以说「被替换」: %q", fi.Err)
	}
	if !strings.Contains(fi.Err, "盘上未做任何改动") {
		t.Fatalf("错误未声明盘上未动手（用户会以为删了一半）: %q", fi.Err)
	}
	// ---- 账目：拦下的项一栏都不许进 ----
	if len(res.OK) != 0 || res.Reclaimed != 0 {
		t.Fatalf("顶替者被删掉了（ok=%+v reclaimed=%d）：错删第三方 + 假账", res.OK, res.Reclaimed)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("被顶替不得按 Skipped 处置（那是「已消失」的口径，M54）: %+v", res.Skipped)
	}
	for _, s := range probe.states {
		if s == "done" {
			t.Fatalf("账本被写进了 done（%+v）：上层据此提供回撤，而盘上动过的其实是顶替者", probe.states)
		}
	}
	// ---- 顶替者必须原样在盘上 ----
	if got, err := os.ReadFile(victim); err != nil || string(got) != string(r11Foreign) {
		t.Fatalf("顶替者没保住（读回 len=%d err=%v，期望 len=%d）：数据丢失",
			len(got), err, len(r11Foreign))
	}
	// 保留源一字未动
	if got, err := os.ReadFile(fx.orig.Path); err != nil || len(got) == len(r11Foreign) && string(got) == string(r11Foreign) {
		t.Fatalf("保留源被波及（len=%d err=%v）", len(got), err)
	}
	assertNoResidue(t, victim)
}

// TestDeleteLegRecheckCountsVanishedDupAsSkipped 同一道守卫的第二格：内容复核之后、
// 删除之前 dup **消失**（用户自己删了它，或另一个清理进程抢先了）⇒ 记 Skipped，
// 不记"被替换"。
//
// 为什么单独钉一格：§30.1 偏差 2 就是这一格。设计片段把三种结论统一写成
// "内容复核通过后、删除前文件被替换"，而 M54 明令禁止那种写法——"已消失"是目标已达成，
// 记 Failed 会在结果集里留一个盘上不存在的路径（Skipped 与 OK 同路进 app.go 的 gone 集合）。
func TestDeleteLegRecheckCountsVanishedDupAsSkipped(t *testing.T) {
	fx := newFixture(t)
	victim := fx.dup1.Path
	needResolvedID(t, victim)

	origHook := beforeActContentRecheck
	var armed bool
	beforeActContentRecheck = func(p string) {
		if p == victim {
			armed = true
		}
	}
	t.Cleanup(func() { beforeActContentRecheck = origHook })

	var fires int
	prevVerify := verifyFileFn
	verifyFileFn = func(e *model.FileEntry, h [32]byte, p *hasher.Pool) (Verdict, fsid.ID) {
		v, id := prevVerify(e, h, p)
		if e.Path == victim && armed && v == VerdictPass {
			armed = false
			// 复核已过、动作未发：对象整个消失了。返回 nil 之前先把它 unlink。
			if err := os.Remove(victim); err != nil {
				t.Errorf("制造消失窗口失败: %v", err)
			}
			fires++
		}
		return v, id
	}
	t.Cleanup(func() { verifyFileFn = prevVerify })

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	if fires != 1 {
		t.Fatalf("前置条件未成立：消失窗口造了 %d 次，期望 1 次", fires)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != victim {
		t.Fatalf("动作前发现 dup 已消失应记 Skipped（skipped=%+v failed=%+v）", res.Skipped, res.Failed)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("已消失被说成别的（M54：那会在结果集里留一个盘上不存在的路径）: %+v", res.Failed)
	}
	if len(res.OK) != 0 || res.Reclaimed != 0 {
		t.Fatalf("Skipped 不进任何一栏: ok=%+v reclaimed=%d", res.OK, res.Reclaimed)
	}
}
