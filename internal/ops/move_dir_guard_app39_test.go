package ops

// APP-39（2026-10-03 审查 P1-1）：move 腿的落点复审必须**早于 MkdirAll**，
// 否则"授权树之外被创建出目录树"这一格没被算进 M204 的自觉代价。
//
// 形状：改前 `moveFileDetailed` 的顺序是
//
//	srcID 取底片 → os.MkdirAll(targetDir) → claimDst → checkLanding(dst.path)
//
// 而 M204 那段注释的自觉代价只写了"外部会先多出一个**零字节**占位、随即由
// release 清掉；数据一个字节都不出去"——**目录树不在那句代价里**。
//
// 触发：用户授权 D:\Data、目标填 D:\Data\sub（尚不存在），入口
// moveTargetAllowed 通过（resolveTargetPath 上溯到已存在的 D:\Data 再接回 sub）；
// 派发窗口内第三方把 D:\Data\sub 换成指向别处的链接 ⇒ MkdirAll 顺着链接在授权树
// 之外创建出目录，claimDst 造出占位，复审拒绝、release 删掉占位 —— **目录留着**。
//
// 判据落在"被拒之后授权树之外有没有多出目录"，不是"有没有报错"：
// 改前那条错误消息一模一样，只有盘上的残留物能区分两者。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

// P-52 复审拒掉 targetDir 时，**不得**在它下面创建出任何目录。
// 这是本组的核心一格：改前 checkLanding 只作用于 claimDst 之后的 dst.path，
// 而 MkdirAll(targetDir) 已经先跑完了。
func TestAPP39LandingRecheckPrecedesMkdirAll(t *testing.T) {
	fx := newFixture(t)
	base := t.TempDir()
	// 授权根 = base/authorized；目标 = base/authorized/sub（**尚不存在**）
	auth := filepath.Join(base, "authorized")
	if err := os.MkdirAll(auth, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(auth, "sub")

	res := Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
		MoveLandingAllowed: func(landing string) error {
			return errNotAuthorized // 恒拒：模拟"复审发现落点越界"
		},
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	if len(res.Failed) != 1 {
		t.Fatalf("复审恒拒时整项应失败：ok=%d failed=%d", len(res.OK), len(res.Failed))
	}
	// ★ 本组真正要钉的一格：被拒之后 targetDir **不该存在**。
	// 改前 MkdirAll 先跑，复审在后面才拒绝 ⇒ 目录已经建出来了（且不会被 release 收走，
	// 因为 release 只管 claimDst 占位那个文件）。
	if _, err := os.Lstat(target); err == nil {
		t.Errorf("APP-39：复审拒绝了落点，目标目录 %s 却仍然被创建出来 —— "+
			"授权树之外凭空多出一棵目录树，而用户没授权创建它、它也不会自动清掉", target)
	} else if !os.IsNotExist(err) {
		t.Errorf("APP-39：查目标目录时拿到非「不存在」的错误 %v", err)
	}
}

// P-53 负控制：复审恒放行时，目标目录**照常被创建**且条目搬成功。
// 这一格钉的是"闸不许把正常用法一起打死"——APP-39 只挪了顺序，没加"不许建目录"。
func TestAPP39LandingAcceptStillCreatesTargetDir(t *testing.T) {
	fx := newFixture(t)
	base := t.TempDir()
	target := filepath.Join(base, "moved") // 尚不存在

	res := Execute(Options{
		Groups:             []*model.DuplicateGroup{fx.group},
		MoveLandingAllowed: func(string) error { return nil },
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})

	if len(res.OK) != 1 {
		t.Fatalf("复审恒放行时应搬成功：ok=%d failed=%d %v", len(res.OK), len(res.Failed), res.Failed)
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		t.Errorf("APP-39 负控制：复审放行时目标目录没有被创建（err=%v）", err)
	}
}

// P-54 复审**必须**问一次 targetDir 本身（不只是落点）。
// 这一格是 APP-39 的行为判据本体：改前 checker 只会收到 dst.path（targetDir 之下的
// 某个文件），收不到 targetDir —— 于是"目标树在不在授权内"这件事从头到尾没人问过。
func TestAPP39LandingCheckerIsAskedAboutTargetDirItself(t *testing.T) {
	fx := newFixture(t)
	base := t.TempDir()
	target := filepath.Join(base, "moved")

	var asked []string
	Execute(Options{
		Groups: []*model.DuplicateGroup{fx.group},
		MoveLandingAllowed: func(landing string) error {
			asked = append(asked, landing)
			return nil
		},
	}, model.OpRequest{Kind: "move", FileIDs: []uint64{fx.dup1.ID}, TargetDir: target})
	for _, p := range asked {
		if p == target {
			return // 问到了 targetDir 本身
		}
	}
	t.Errorf("APP-39：checker 从头到尾没收到 targetDir 本身（收到的：%v）⇒ "+
		"「目标树在不在授权内」这件事仍然没人问过", asked)
}

// errNotAuthorized 是本组复审桩的固定拒绝理由。
// 刻意用包级 sentinel 而不是 fmt.Errorf：判据只关心"被拒"这个事实，
// 而 M204 那侧的真拒绝文案由 app 层的 moveTargetAllowed 给，两边不共用。
var errNotAuthorized = errors.New("落点不在授权范围内")
