package ops

// symlinkCallOutcome 判据的钉子（M289，2026-09-27 实施批）。
//
// ★ 为什么这张表住在**无 tag** 文件里（winerrno.go 同侧）：本包的既定纪律就是
//   "平台判定写成无 tag 的纯函数/纯常量，平台真值由调用侧注入"（见 winerrno.go 头部）。
//   于是 CI 的 linux/darwin 腿也能断言它，不必等 Windows 真机——而 M289 恰恰是
//   "只有 Windows 真机才看得见"的那一类缺陷，判据若能跨平台断言就不该只在真机上撞。
//
// 表里的每一行都来自真机读数，不是设想：
//   · 提权臂 `flags=0x2 | r=1 | gle=0 | after_lstat=exists`（链接建成）
//     ——一次性探针 `b7sym`，输出留存 b7g/symstage2_elevated.txt，跑完即删；
//   · 过滤令牌臂（同账户 CreateRestrictedToken 禁掉 SeCreateSymbolicLinkPrivilege
//     + 开发者模式关）两段全是 `r=1280 | gle=1314 | after_lstat=missing`
//     ——b7g/symstage2_restricted.txt。★ 注意 **r 不是 0**：CreateSymbolicLinkW
//     返回的是 BOOLEAN，失败时 RAX 里是残值 1280（0x500），而链接根本没建。
//     修前的判据 `if r != 0 { return 1, nil }` 把 1280 读成 TRUE ⇒
//     `createSymlink` 交回 nil，调用侧直接进 `verifySymlinked`，用户看到的失败原因
//     成了「保留源在校验后被替换，已拦截（S1）」——一个"没有权限建软链接"的环境事实
//     被报成了"有人在校验瞬间替换了你的源文件"，而 describeSymlinkError 那句
//     已经写好的「以管理员身份运行 / 开启开发者模式」指引一次都没走到。
//   · 87 那一臂是**两段式降级的触发条件**（未开开发者模式的老 Windows），
//     本机真机读数里 87 **从未出现**（gle 是 1314），所以这一行钉的是代码分支的语义
//     而非本机事实——登记行据此削弱了"两段式降级在本机会发生"的说法，不得读成已验证。

import (
	"errors"
	"syscall"
	"testing"
)

func TestSymlinkCallOutcomeTable(t *testing.T) {
	cases := []struct {
		name    string
		r       uintptr
		errno   error
		wantOK  bool
		wantErr error // nil = 成功；非 nil = 失败时必须"errors.Is 得到它"
	}{
		{"提权臂读数：r=1 + gle=0 ⇒ 成功", 1, syscall.Errno(0), true, nil},
		{
			"过滤令牌臂读数：r=1280 + gle=1314 ⇒ **失败**（修前在这一行被读成成功）",
			1280, syscall.Errno(1314), false, errPrivilegeNotHeld,
		},
		{
			"经典 FALSE 臂：r=0 + gle=1314 ⇒ 同一个错误码，同一种失败",
			0, syscall.Errno(1314), false, errPrivilegeNotHeld,
		},
		{
			"两段式降级条件：r=0 + gle=87 ⇒ 交回 87，让调用侧去掉标志再试一次",
			0, syscall.Errno(87), false, errInvalidParameter,
		},
		{
			// 旧写法在这里判的是 `e == nil`，而装箱的 Errno(0) **永不为 nil**
			// （同 M295 的恒真式）⇒ 那一支从来不会走，gle=0 的失败被原样交下去，
			// describeSymlinkError 落到 default 臂，用户只看见裸的"操作成功完成"。
			"返回 FALSE 却没有理由：r=0 + gle=0 ⇒ 按未知失败交 EINVAL，不许原样往下走",
			0, syscall.Errno(0), false, syscall.EINVAL,
		},
		{"返回 FALSE 且 error 为 nil：同上，按未知失败交 EINVAL", 0, nil, false, syscall.EINVAL},
		{
			"非 Errno 的 error：原样交回（让 describeSymlinkError 走它的兜底臂）",
			0, errors.New("胶水层自己报的错"), false, nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := symlinkCallOutcome(c.r, c.errno)
			if got := r == 1; got != c.wantOK {
				t.Fatalf("outcome 判成 ok=%v（r=%d），期望 ok=%v；err=%v", got, r, c.wantOK, err)
			}
			if !c.wantOK && err == nil {
				t.Fatal("失败臂必须带理由（err=nil 会让下游文案拼出空错误）")
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("失败理由 = %v，期望 errors.Is 到 %v", err, c.wantErr)
			}
		})
	}
}

// 这一格钉的是"恒真式"这个**形状**本身，理由与 realbytes 侧同一条：
// M289 与 M295 的成因都是拿装箱后的 error 判 `!= nil`。
// 前提若哪天变了（Go 不再把 Errno(0) 装箱成非 nil），这里先红，
// 而不是让判据悄悄建在一条已经不成立的假设上。
func TestBoxedZeroErrnoIsNotNilTrapOps(t *testing.T) {
	var boxed error = syscall.Errno(0)
	if boxed == nil {
		t.Fatal("前提变了：syscall.Errno(0) 装箱后现在是 nil ⇒ winerrno.go 里 gle 判据的" +
			"注释需一并重读（别直接删，先在当代 Go 上复测 errIsNil 这一读数）")
	}
	if _, err := symlinkCallOutcome(0, boxed); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("r=0 + 装箱 Errno(0) 应判成 EINVAL，实得 %v ⇒ 又回到『gle=0 的失败原样往下走』", err)
	}
}
