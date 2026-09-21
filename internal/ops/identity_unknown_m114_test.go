package ops

// M114（第 2 轮 §23.3，04 §6.11 OPS-13b）：identityStatus 的第三种情形不得冒充"被替换"。
//
// (false, false) 有**两条来路**（verify.go）：
//   - fsidFromPathFn 报错且**不是** ENOENT（父目录 EACCES、网络盘 EIO/ESTALE、路径形状 ENOTDIR）；
//   - 原先能解析、现在解析不出（cur.Resolved 为假）。
// 两条都是"我们不知道"，而 executor.go 的 guardIdentity 把 still=false 一律记
// "文件在扫描后被替换（inode 已变化），已拦截"。拦下是对的（fail-closed，本轮不改处置），
// 说的是假话：用户据此以为文件被人换过，去找那个根本不存在的"新文件"，
// 而真正该做的动作是重扫或查挂载。同一条判据的第四种情形（gone → Skipped）
// 上一批已经分出去了，剩下这一格是 M54 的收尾。
//
// 判据落在文案而不是处置：改前必须是"拦住了、但说成被替换"，红在措辞那一格。
// 读身份失败这一格在无 root 的本机上造不出真文件系统形状，因此走 fsidFromPathFn
// 接缝注入（该接缝只为这件事存在，生产路径就是 fsid.FromPathNoFollow 本身）。

import (
	"errors"
	"os"
	"strings"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/model"
)

// errSimulatedUnreadable 是一条**平台中立**的"读不动"哨兵：普通 error 在三条腿上
// 都不会被 errors.Is 认成 ENOENT，也就不会掉进 gone 那一格。
// ★ 不用 syscall.EACCES 造：Windows 上 Errno(13).Error() 走 FormatMessage，
// 拿到的既不是 "permission denied" 也不保证 errors.Is 到 os.ErrPermission
// ——把本机 errno 文案当普适判据是本项目犯过两次的同类错（M126 的首跑、CI 的 Windows 腿）。
var errSimulatedUnreadable = errors.New("模拟：身份读取失败（卷不可达，非 ENOENT）")

func TestIdentityUnreadableIsNotReportedAsReplaced(t *testing.T) {
	// ---- 夹具前提自检（不经判据本体，I5）----
	if errors.Is(errSimulatedUnreadable, os.ErrNotExist) {
		t.Fatal("夹具前提不成立：注入的错误被认成了 ENOENT，测不到 M114 那一格")
	}
	if errors.Is(errSimulatedUnreadable, os.ErrExist) {
		t.Fatalf("夹具前提不成立：注入的错误被认成了别的 errno 家族（%v）", errSimulatedUnreadable)
	}

	fx := newFixture(t)
	victim := fx.dup1.Path
	var hits int
	prev := fsidFromPathFn
	fsidFromPathFn = func(p string) (fsid.ID, error) {
		if p == victim {
			hits++
			return fsid.ID{}, errSimulatedUnreadable
		}
		return prev(p)
	}
	t.Cleanup(func() { fsidFromPathFn = prev })

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}, KeepIDs: map[uint64]bool{fx.orig.ID: true}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})

	// ---- 前提自检：注入点真的被走到了 ----
	if hits == 0 {
		t.Fatal("前提自检：身份读取从未落到被测路径上 ⇒ 探针打在空处，本条没有取到读数")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("前提自检：文件应还在盘上（我们注入的是「读不动」，不是「不存在」）: %v", err)
	}

	// ---- 处置不许回退：无从判定仍然 fail-closed ----
	if len(res.Skipped) != 0 {
		t.Errorf("读不动不得按 Skipped 处置（那是「确实已消失」的口径）: %v", res.Skipped)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %+v, want 1 条（拦下这件事本身是对的，本批只修说法）", res.Failed)
	}
	msg := res.Failed[0].Err

	// ---- 判据格：不能把"不知道"说成"看到了顶替" ----
	if strings.Contains(msg, "被替换") {
		t.Errorf("读身份失败被说成了确证的顶替（M114）：%q\n"+
			"该格的实际来路是 %v：既不是「仍是那个对象」，也不是「换成了另一个」，更不是「已经没了」，"+
			"只有「无从判定」这一种说法是真话", msg, errSimulatedUnreadable)
	}
	if !strings.Contains(msg, "无法确认") {
		t.Errorf("文案应明说「无法确认」，让用户去重扫/查挂载而不是找一个不存在的新文件: %q", msg)
	}
	if !strings.Contains(msg, "模拟：身份读取失败") {
		t.Errorf("底层原因必须原样出现在文案里（用户据此判断是权限、断线还是形状问题）: %q", msg)
	}
}
