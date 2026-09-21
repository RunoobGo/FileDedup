package ops

// 2026-09-21 全量审查 OPS 批（设计稿 §15.0-A）。三条都在"复核与动手之间"这条
// 同一条纪律上，只是位置不同：
//
//   OPS-1（Critical）合并**成功**后按路径盲删临时名 —— 名字已被改名消耗，
//         此刻该位置上是谁的东西，本函数一无所知。
//   OPS-7 回收站批量失败退化成逐个执行时**不再复核身份** —— trash 是全仓唯一
//         "复核不紧贴动作"的 kind，中间夹着整批 I/O 与并发排队。
//   OPS-10 claimDst 的重名递增无上限、不看取消 —— 同包 trash_linux.go 对同形状
//         循环已给过结论（"永远查不出 NotExist 会让全部并发 goroutine 一起挂死"）。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/model"
)

// OPS-1 主探针：swap 成功之后 tmp 名字已交还命名空间，第三方在此落子时不得删除。
func TestHardlinkMergeSwapSuccessDoesNotRemoveForeignTempName(t *testing.T) {
	f := newMergeFixture(t)
	tmp := f.dup + FddTempSuffix

	orig := hardlinkRename
	var planted bool
	hardlinkRename = func(oldp, newp string) error {
		err := orig(oldp, newp)
		// 钉在「tmp→dup 改名成功」之后：这一行执行完，tmp 这个名字就不再属于我们。
		if err == nil && !planted && oldp == tmp && newp == f.dup {
			planted = true
			if werr := os.WriteFile(tmp, []byte(victimData), 0o644); werr != nil {
				t.Errorf("放置第三方文件失败: %v", werr)
			}
		}
		return err
	}
	t.Cleanup(func() { hardlinkRename = orig })

	// 让收尾删备份失败 ⇒ 走进那条 `_ = os.Remove(tmp)` 分支（与 symlink.go 同位置
	// 的写法不对称：软链接侧没有这一行）。
	origRemove := workTempRemove
	workTempRemove = func(string) error { return errors.New("simulated sharing violation") }
	t.Cleanup(func() { workTempRemove = origRemove })

	err := HardlinkMerge(f.keep, f.dup, f.keepID, f.dupID)
	var residue *ResidueError
	if !errors.As(err, &residue) {
		t.Fatalf("合并已到位、只是备份没删掉，应回报 ResidueError（成功+残留），实际 %T: %v", err, err)
	}
	if !planted {
		t.Fatal("前置条件未触发：tmp→dup 的改名从未成功，本用例没有验证任何东西")
	}
	data, rerr := os.ReadFile(tmp)
	if rerr != nil || string(data) != victimData {
		t.Fatalf("合并成功后按路径删了不属于本次操作的文件：读 %s 得到 %v / %q。"+
			"tmp 已被改名消耗，此后该名字归命名空间，本函数对它没有任何所有权证据（§15.0-A OPS-1）",
			tmp, rerr, data)
	}
}

// OPS-7 主探针：批量 trash 失败后的逐个回退，每次动手前必须再复核一次身份。
func TestTrashFallbackRechecksIdentityBeforeEachAttempt(t *testing.T) {
	dir := t.TempDir()
	content := []byte("trash-fallback-identity-window")
	keep := filepath.Join(dir, "keep.bin")
	dup := filepath.Join(dir, "dup.bin")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := hashOfContent(t, content)
	ek := &model.FileEntry{ID: 1, Path: keep, Size: uint64(len(content)), Ext: ".bin"}
	ed := &model.FileEntry{ID: 2, Path: dup, Size: uint64(len(content)), Ext: ".bin"}
	group := &model.DuplicateGroup{GroupID: 1, Hash: h, Files: []*model.FileEntry{ek, ed}}

	var calls int32
	var planted bool
	trashFn := func(paths []string) (map[string]string, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// 批量失败之后、逐个回退之前：第三方把 dup 位置换成另一个 inode
			// （内容逐字节相同，所以任何只看 size/Stat 的判据都发现不了）。
			tmp := dup + ".swap-in"
			if err := os.WriteFile(tmp, content, 0o644); err != nil {
				t.Errorf("放置顶替文件失败: %v", err)
				return nil, fmt.Errorf("setup: %w", err)
			}
			if err := os.Rename(tmp, dup); err != nil {
				t.Errorf("顶替失败: %v", err)
				return nil, fmt.Errorf("setup: %w", err)
			}
			planted = true
			return nil, errors.New("simulated batch trash failure")
		}
		// 走到这里就说明回退分支**没有**重新复核身份，直接把顶替者派给了回收站。
		return nil, errors.New("回退分支动手了：身份复核缺位")
	}

	res := Execute(
		Options{Groups: []*model.DuplicateGroup{group}, KeepIDs: map[uint64]bool{1: true},
			TrashFn: trashFn},
		model.OpRequest{Kind: "trash", FileIDs: []uint64{2}},
	)
	if !planted {
		t.Fatal("前置条件未触发：批量 trash 从未被调用，本用例没有验证任何东西")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("回退分支在 dup 已被顶替后仍动手 %d 次（应为 1 次批量派发）："+
			"trash 是全仓唯一「复核不紧贴动作」的 kind（§15.0-A OPS-7）", n)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("身份变化必须记 1 条失败，实际 Failed=%+v", res.Failed)
	}
	if !strings.Contains(res.Failed[0].Err, "替换") {
		t.Errorf("失败原因应是身份拦截，实际 %q", res.Failed[0].Err)
	}
	// 顶替者仍在原位：它不是本次操作的对象，应用从未获授权处置它。
	if data, err := os.ReadFile(dup); err != nil || string(data) != string(content) {
		t.Fatalf("顶替文件被处置了（err=%v data=%q）", err, data)
	}
}

// OPS-10 主探针：候选名持续被占时 claimDst 必须在有界次数内退出，不得挂死。
func TestClaimDstIsBounded(t *testing.T) {
	orig := openExclusive
	var attempts int32
	openExclusive = func(string, int, os.FileMode) (*os.File, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, fs.ErrExist // 名字"永远查不出 NotExist"的环境（截断式文件系统等）
	}
	t.Cleanup(func() { openExclusive = orig })

	type outcome struct {
		path string
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		c, err := claimDst(t.TempDir(), "photo.jpg")
		ch <- outcome{c.path, err}
	}()
	select {
	case o := <-ch:
		n := atomic.LoadInt32(&attempts)
		if o.err == nil {
			t.Fatalf("每一次抢占都返回 ErrExist，claimDst 却成功了（%s）", o.path)
		}
		if n > nameMaxTry {
			t.Fatalf("尝试 %d 次仍在上限 %d 之外：上限没有生效", n, nameMaxTry)
		}
		if !strings.Contains(o.err.Error(), "上限") {
			t.Errorf("错误应说明是到达递增上限，实际：%v", o.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("claimDst 在候选名持续被占时不收敛（挂死）：同包 trash_linux.go:104 对同形状循环" +
			"已给出结论——不设上限会让全部并发 goroutine 一起挂死（§15.0-A OPS-10）")
	}
}
