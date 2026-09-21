package ops

// 第六批探针（2026-09-21，04 §6.11 登记表 M52 / M54；设计稿 §17.3 P-4~P-6）。
//
//   P-4 M52 —— "打不开/读不了/不是普通文件"这三类**无从判定**被折进 VerdictFailed，
//              于是失败抽屉统一写着"文件在扫描后被修改"。修前红。
//   P-5 M52 —— executor.go 的 `switch v` 里 default 是**放行**分支，另两处 keep 源
//              switch 无 default ⇒ 任何新增/越界结论都会静默进 toProcess 挨一刀。
//              这条修前真的把文件删了（不只是文案难看）。
//   P-6 M54 —— 复核失败分不清"用户自己删了"与"被第三方顶替"：前者其实目标已达成
//              （与 VerdictSkipped、delete 分支的 os.IsNotExist→Skipped 同形状），
//              却记 Failed 且不进 gone 集合 ⇒ 结果集留一个盘上不存在的路径。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/hasher"
	"filededup/internal/model"
)

// needResolvedID 取路径身份；卷不提供稳定索引时跳过（与 newMergeFixture 同一前置）。
func needResolvedID(t *testing.T, path string) fsid.ID {
	t.Helper()
	id, err := fsid.FromPathNoFollow(path)
	if err != nil {
		t.Fatalf("取身份失败: %v", err)
	}
	if !id.Resolved {
		t.Skipf("本卷不提供稳定索引，identityStatus 无从比对（放行分支由既有钉子覆盖）")
	}
	return id
}

// P-4a（M52）：非普通文件不是"被修改"，是"无从判定"。
func TestVerifyNonRegularIsUnverifiable(t *testing.T) {
	dir := t.TempDir()
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &model.FileEntry{ID: 1, Path: dir, Size: uint64(st.Size())}
	v, _ := VerifyFile(e, [32]byte{}, hasher.NewPool())
	if v != VerdictUnverifiable {
		t.Fatalf("目录作校验对象 = %d, want VerdictUnverifiable(%d)；"+
			"实得 VerdictFailed 即 M52：盘上从没说过它被改过", v, VerdictUnverifiable)
	}
}

// P-4b（M52）：EACCES 打不开不是"被修改"。
//
// ★ 前置自检（2026-09-22，§21.3 W3）：`os.Chmod(path, 0)` 造"打不开"只在
// 认权限位的卷上成立。Windows 的 chmod 只翻**只读属性**、不拒绝读，于是
// `VerifyFile` 打得开、哈希相符 → 返回 VerdictPass，红的是夹具前提而不是判据
// （windows 腿实测 `= 0`）。本卷读得动就 Skip 并写明原因，按 04 §6.8.0 约束 5
// 记"Windows 侧这一条未验证"，不许拿 unix 的绿冒充。
func TestVerifyUnopenableIsUnverifiable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户无权限拒绝语义")
	}
	fx := newFixture(t)
	path := fx.dup1.Path
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		t.Skipf("本平台的 chmod 不拒绝读（Windows 只翻只读位）：「打不开」这一前提造不出来，" +
			"M52 的无从判定分支在本卷上未验证（§21.3 W3）")
	}

	v, _ := VerifyFile(fx.dup1, fx.group.Hash, hasher.NewPool())
	if v != VerdictUnverifiable {
		t.Fatalf("打不开的文件 = %d, want VerdictUnverifiable(%d)", v, VerdictUnverifiable)
	}

	// 端到端：文案不得再写"被修改"（用户按这句话去重扫是白跑，真因是权限）。
	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}, KeepIDs: map[uint64]bool{fx.orig.ID: true}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %+v, want 1 条", res.Failed)
	}
	if strings.Contains(res.Failed[0].Err, "被修改") {
		t.Fatalf("权限不可读被报成「文件在扫描后被修改」（M52）: %q", res.Failed[0].Err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("无从判定的文件必须原样不动: %v", err)
	}
}

// P-5（M52 的加固腿）：未知/越界校验结论一律落 Failed，不得走 default 放行。
func TestUnknownVerdictFailsClosed(t *testing.T) {
	fx := newFixture(t)
	prev := verifyFileFn
	verifyFileFn = func(e *model.FileEntry, h [32]byte, p *hasher.Pool) (Verdict, fsid.ID) {
		if e.ID == fx.dup1.ID {
			return Verdict(99), fsid.ID{} // 扮演"以后有人加了枚举值而这里忘了列 case"
		}
		return prev(e, h, p)
	}
	t.Cleanup(func() { verifyFileFn = prev })

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}, KeepIDs: map[uint64]bool{fx.orig.ID: true}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})
	if len(res.OK) != 0 {
		t.Fatalf("越界校验结论被 default 放行并真删了文件（M52 加固腿失效）: %+v", res.OK)
	}
	if _, err := os.Stat(fx.dup1.Path); err != nil {
		t.Fatal("文件被删：default 分支必须改成兜底 Failed")
	}
	if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "未知") {
		t.Fatalf("Failed = %+v, want 1 条「未知校验结论」", res.Failed)
	}
}

// P-6a（M54）：identityStatus 分得清"已消失"与"被替换"。
func TestIdentityStatusSeparatesGoneFromReplaced(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(p, []byte("AAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := needResolvedID(t, p)

	if still, gone := identityStatus(p, id); !still || gone {
		t.Fatalf("路径未被动过 = (%v,%v), want (true,false)", still, gone)
	}

	// 被另一个文件顶替（rename 进来）⇒ 必须仍是"非 gone"，否则放行就是错删第三方。
	other := filepath.Join(dir, "other.bin")
	if err := os.WriteFile(other, []byte("BBBBBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	replaced := filepath.Join(dir, "replaced.bin")
	if err := os.WriteFile(replaced, []byte("CCC"), 0o644); err != nil {
		t.Fatal(err)
	}
	origID := needResolvedID(t, replaced)
	if err := os.Rename(other, replaced); err != nil {
		t.Fatal(err)
	}
	if still, gone := identityStatus(replaced, origID); still || gone {
		t.Fatalf("被顶替 = (%v,%v), want (false,false)：gone 为真会把顶替当成就绪跳过", still, gone)
	}

	// 真的消失 ⇒ gone。
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if still, gone := identityStatus(p, id); still || !gone {
		t.Fatalf("已消失 = (%v,%v), want (false,true)（实得 false,false 即 M54）", still, gone)
	}
}

// P-6b（M54）：端到端——校验通过后、动手前被删，应记 Skipped 并进 gone 语义。
func TestIdentityGoneBeforeActionRecordsSkipped(t *testing.T) {
	fx := newFixture(t)
	prev := verifyFileFn
	victim := fx.dup1.Path
	verifyFileFn = func(e *model.FileEntry, h [32]byte, p *hasher.Pool) (Verdict, fsid.ID) {
		v, id := prev(e, h, p)
		if e.ID == fx.dup1.ID && v == VerdictPass {
			// 确定性复现"用户自己把这份重复文件删了"：校验已过、复核未跑。
			if err := os.Remove(victim); err != nil {
				t.Errorf("制造窗口失败: %v", err)
			}
		}
		return v, id
	}
	t.Cleanup(func() { verifyFileFn = prev })

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}, KeepIDs: map[uint64]bool{fx.orig.ID: true}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})
	if len(res.Skipped) != 1 || res.Skipped[0] != victim {
		t.Fatalf("Skipped = %v, want [%s]（S8：目标已达成，与 VerdictSkipped 同一口径）", res.Skipped, victim)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("Failed = %+v, want 空（修前这里是一条「已被替换、已拦截」）", res.Failed)
	}
}

// P-6c（M54 的反向钉子）：被顶替时**不得**跟着变 Skipped。
func TestIdentityReplacedBeforeActionStillFails(t *testing.T) {
	fx := newFixture(t)
	prev := verifyFileFn
	victim := fx.dup1.Path
	verifyFileFn = func(e *model.FileEntry, h [32]byte, p *hasher.Pool) (Verdict, fsid.ID) {
		v, id := prev(e, h, p)
		if e.ID == fx.dup1.ID && v == VerdictPass {
			// 第三方把同名新文件放进 dup 位：路径在、身份已换。
			// M115（第 2 轮 §23.4）：改前这里是 WriteFile → Remove → Rename 三步，
			// 中间有一瞬路径上什么都没有 ⇒ 在会还号的卷上（CI 实测）顶替者可能拿到
			// 同一个 inode 号，用例就退化成在测"文件系统的还号策略"而不是测守卫。
			// 现走本包唯一实现 swapInAt（先写旁边、原子改名，末尾自带
			// assertDistinctIdentity 自检）。断言一字未动。
			swapInAt(t, victim, id, []byte("THIRD-PARTY"))
		}
		return v, id
	}
	t.Cleanup(func() { verifyFileFn = prev })

	res := Execute(Options{Groups: []*model.DuplicateGroup{fx.group}, KeepIDs: map[uint64]bool{fx.orig.ID: true}},
		model.OpRequest{Kind: "delete", FileIDs: []uint64{fx.dup1.ID}, ConfirmDanger: true})
	if len(res.Skipped) != 0 {
		t.Fatalf("被顶替不得按 Skipped 处置（那是「已消失」的口径）: %v", res.Skipped)
	}
	if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "被替换") {
		t.Fatalf("Failed = %+v, want 1 条「已被替换、已拦截」", res.Failed)
	}
	// ★ M114 第一步（补强，不动判据）：把"确证顶替"那一格的文案整个钉死。
	// 下面 identity_unknown_m114_test.go 断的是"读不动"那一格**不得**用这套说法，
	// 两条对照才有意义：没有这条钉子，把两句改成同一句话仍然全绿。
	if !strings.Contains(res.Failed[0].Err, "inode 已变化") {
		t.Errorf("确证顶替的措辞必须带身份依据「inode 已变化」，否则「被替换」成了无凭据的断言: %q", res.Failed[0].Err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("第三方文件被我们删了（数据丢失）: %v", err)
	}
}
