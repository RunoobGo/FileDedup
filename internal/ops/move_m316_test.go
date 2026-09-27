package ops

// M316（OPS-44，2026-09-28 第六轮全量审查）：同卷 move 快路径在 rename 前没有源身份复核。
//
// 缺陷形状：moveFileDetailed 只在**跨卷腿**装了 AS-H4 那道复核（复制前取底片、
// 删源前比对），同卷腿则是从入口一路走到 renameFile——中间隔着 os.MkdirAll 和
// claimDst（重名时最坏一万次 O_EXCL 占名）。窗口内第三方以原子改名顶替 src 时，
// rename 会原子地把落点上的占位顶掉、并把**当时挂在 src 这个名字上的那个对象**
// 搬走：不销毁数据，却是替用户做了一次他没点过的移动，账本还记 done。
// 执行器外面那层 guardIdentity 覆盖不到这段——它离动手点隔着整批的排队与 I/O
// （OPS-7 记的就是这个形状，M316 把 move 这条腿补齐）。
//
// 时序由包级接缝 preRenameRecheck 钉死，夹具口径照抄 swap_fixture_test.go
// （先挪走原件、新对象从旁边 rename 顶位；不"先删后建"，会还号的卷上 inode
// 复用会骗过 (dev,ino)，那种用例测的是文件系统而不是守卫——M91）。
// 与 trash_xdg_identity_test.go 同款约束：顶替不钩在复制进行中（Windows 的
// sharemode 不带 FILE_SHARE_DELETE，run 35931760089 教训）；本用例走的是同卷腿，
// 复核点前后没有任何句柄外溢，三腿同判据真跑，故**无 build tag**（H6 惯例）。
//
// 改前必红：新缝 preRenameRecheck 在改前树不存在 ⇒ 本文件编译不过，行为级红
// **全部由变异提供**（M316-a 缝换恒 true ⇒ 用例 1 红；M316-b 换恒 false ⇒ 用例 2 红；
// M316-c 只放行不 dst.release() ⇒ 用例 1 的零残留格红），见 §6.40/M207 先例。
// 本文件不冒领红探针（AS-K2）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// m316State 承接钩子内才确定的信息。hijackErr 让"顶替本身没做成"不会被
// 误读成"守卫没触发"（与 xdgHijackState 同形）。
type m316State struct {
	parked    string  // 被第三方挪走的原件所在路径
	seenID    fsid.ID // 钩子收到的底片（应等于入口那一刻 src 的身份）
	calls     int     // preRenameRecheck 实际被调用的次数
	hijackErr error   // 钩子内构造顶替自身的失败
}

// hijackAtRenameBoundary 在"名字已定、数据未动一步"的复核那一刻把 src 顶替成
// 第三方的新文件，随后转调原实现——守卫若在，此时必须识破 inode 已换。
func hijackAtRenameBoundary(t *testing.T, src, thirdPartyContent string) *m316State {
	t.Helper()
	st := &m316State{}
	orig := preRenameRecheck
	preRenameRecheck = func(p string, id fsid.ID) bool {
		st.calls++
		st.seenID = id
		st.parked = src + ".parked-original"
		before, err := pathIdentity(src)
		if err != nil {
			st.hijackErr = err
			return orig(p, id)
		}
		if err := os.Rename(src, st.parked); err != nil {
			st.hijackErr = err
			return orig(p, id)
		}
		swapInAt(t, src, before, []byte(thirdPartyContent))
		return orig(p, id)
	}
	t.Cleanup(func() { preRenameRecheck = orig })
	return st
}

func TestM316SameVolumeMoveAbortsWhenSourceSwappedAtBoundary(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "victim.bin")
	const original = "ORIGINAL-CONTENT-THAT-MUST-NOT-BE-MOVED"
	if err := os.WriteFile(src, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "moved")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	wantID, err := pathIdentity(src)
	if err != nil {
		t.Fatal(err)
	}

	const thirdParty = "someone else's freshly renamed file"
	st := hijackAtRenameBoundary(t, src, thirdParty)

	dst, err := MoveFile(src, target)
	if st.hijackErr != nil {
		t.Fatalf("顶替时序本身没构造成（钩位错了或平台句柄占用）: %v", st.hijackErr)
	}
	if st.calls != 1 {
		t.Fatalf("同卷快路径必须在 rename 前恰好复核一次源，实得 %d 次", st.calls)
	}
	if !wantID.Resolved {
		t.Skipf("本卷不提供稳定索引（%+v），顶替原理上无从比对，用例前提不成立", wantID)
	}
	if st.seenID.Dev != wantID.Dev || st.seenID.Ino != wantID.Ino {
		t.Fatalf("钩子收到的底片不是入口那份源身份（M316 要求入口取片、紧贴 rename 比对）："+
			"got dev=%x ino=%d want dev=%x ino=%d", st.seenID.Dev, st.seenID.Ino, wantID.Dev, wantID.Ino)
	}
	if err == nil {
		t.Fatalf("源在改名那一刻已被第三方顶替，MoveFile 却报告成功：%q —— 搬走的是第三方的文件，账本还会记 done（M316）", dst)
	}
	if !strings.Contains(err.Error(), "移动前复核未通过") {
		t.Errorf("错误应指明是移动前复核拦下，实得: %v", err)
	}
	if dst != "" {
		t.Errorf("拦截腿不得返回落点（未做任何改动）: %q", dst)
	}

	// 三份数据一个都不能少：第三方仍在原位、原件仍在暂存位、内容一字未动。
	if b, e := os.ReadFile(src); e != nil || string(b) != thirdParty {
		t.Errorf("第三方文件在 src 上应原样留存，读得 %q err=%v", b, e)
	}
	if b, e := os.ReadFile(st.parked); e != nil || string(b) != original {
		t.Errorf("被顶替的原件应留在 %s 且内容不变，读得 %q err=%v", st.parked, b, e)
	}
	ents, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("拦截之后目标目录残留 %d 项（%s）：claimDst 的零字节占位必须被 release 清掉（M316-c）",
			len(ents), strings.Join(names, ", "))
	}
}

// 正常同卷 move 必须照常成功——M316-b（缝换成恒 false）的红靶：
// 复核点若把"身份可比对且未变"也判否，整条同卷快路径就废了。
func TestM316NormalSameVolumeMoveStillSucceeds(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "keep.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "moved")

	dst, err := MoveFile(src, target)
	if err != nil {
		t.Fatalf("身份未变时同卷 move 必须成功（M316 只补缝，不拦正常腿）: %v", err)
	}
	if b, e := os.ReadFile(dst); e != nil || string(b) != "payload" {
		t.Errorf("落点内容不符: %q err=%v", b, e)
	}
	if _, e := os.Stat(src); !os.IsNotExist(e) {
		t.Errorf("同卷快路径成功后源路径不应存在: %v", e)
	}
}

// 负控制（方向而非红靶）：FAT/exFAT 那类"入口取不到稳定索引"的卷上，
// 底片是未解析零值，此时**无从比对**——强行判否会让这些卷上完全无法移动文件
// （在册口径见 verify.go 的 identityStill 注释）。这里用真 identityStill
// 喂一个未解析 ID，钉住"未解析 ⇒ 放行"这条方向在 move 的复核点上同样成立。
func TestM316UnresolvedIdentityDoesNotBlockMove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "fatlike.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := preRenameRecheck
	preRenameRecheck = func(p string, id fsid.ID) bool { return orig(p, fsid.ID{}) }
	t.Cleanup(func() { preRenameRecheck = orig })

	if _, err := MoveFile(src, filepath.Join(root, "moved")); err != nil {
		t.Fatalf("底片未解析时必须放行（无法比对不是被顶替的证据）: %v", err)
	}
}
