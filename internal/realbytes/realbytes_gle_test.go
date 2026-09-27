package realbytes

// gleIsFailure 判据的钉子（M295，2026-09-27 实施批）。
//
// ★ 为什么这一格必须是**无 tag** 的：本包的缺陷形状不在"读不到"，而在
//   **"怎么判『读不到』"**——那是一个纯算术判定，按本包的分层约定（见 realbytes.go
//   头部：判定与回退规则全住在无 tag 层，带 tag 的文件只负责"读一个数"）就该放在
//   无 tag 层，于是 Linux 主门禁也能跑它，不必等 Windows 真机腿。
//   （修前本包 Windows 腿零测试 = M104 同族形状，登记行点名的正是这条结构漏网。）
//
// 真机读数来源（一次性探针 `b7g/errno_probe.txt`，跑完即删；Windows 10.0.26100 / amd64）：
//   · 成功臂（读 kernel32.dll 的压缩尺寸）交回 r1=0xcc288、GetLastError=Errno(0)，
//     而 **errIsNil=false** ⇒ Go 把 `syscall.Errno(0)` 装箱进 `error` 接口，
//     **成功时那个 error 也非 nil**；
//   · 失败臂（不存在的路径）交回 r1=0xffffffff、GetLastError=Errno(2)。
// ⇒ 所以 `errno != nil` 判失败是**恒真式**，注释宣称的"必须靠 GetLastError 二选一，
//   不能只看返回值"从来没成立过——守卫实际退化成只看返回值。
//   后果：低 32 位恰好全是 1 的合法尺寸（2^32·k + 4294967295，即 4 GiB−1 / 8 GiB−1 …）
//   被判"读不到"，静默回退逻辑大小并标 known=false ⇒ 实占口径在这类文件上系统性偏保守。

import (
	"errors"
	"syscall"
	"testing"
)

func TestGleIsFailureDistinguishesBoxedZero(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// 这一行就是 M295 的全部要点：装箱后的 Errno(0) **不是**失败。
		{"成功臂：装箱的 Errno(0) 非 nil，但不是失败", syscall.Errno(0), false},
		{"失败臂：Errno(2) ERROR_FILE_NOT_FOUND", syscall.Errno(2), true},
		{"失败臂：Errno(5) ERROR_ACCESS_DENIED", syscall.Errno(5), true},
		// 理论臂：调用侧没给 error（真机读不到这种形状，但判据要自洽）。
		{"nil 视为无失败理由", nil, false},
		// ★ fail-closed：不是 Errno 的非 nil error 判"失败"。
		//   宁可回退逻辑大小 + known=false（界面会标"实占未知"），
		//   也不能把一个说不清来历的返回值当可信实占写进账面。
		{"非 Errno 的 error：按失败处理（fail-closed）", errors.New("不是 errno"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gleIsFailure(c.err); got != c.want {
				t.Fatalf("gleIsFailure(%v) = %v，期望 %v", c.err, got, c.want)
			}
		})
	}
}

// 这一格钉的是"恒真式"这个**形状**本身：`err != nil` 与判据结论必须不同。
// 写成断言而不是注释，是因为 M295 的成因就是有人在判据里用了 `!= nil`——
// 将来同族改动（把 gle 换成别的装箱 error 来源）也能在这里被拦下。
func TestBoxedZeroErrnoIsNotNilTrap(t *testing.T) {
	var boxed error = syscall.Errno(0)
	if boxed == nil {
		t.Fatal("前提变了：syscall.Errno(0) 装箱后现在是 nil ⇒ M295 的恒真式前提已消，" +
			"本文件与 gleIsFailure 的注释需一并重读（别直接删，先确认探针读数在当代 Go 上是否还是 errIsNil=false）")
	}
	if gleIsFailure(boxed) {
		t.Fatal("gleIsFailure 又把装箱的 Errno(0) 判成失败 ⇒ M295 的缺陷回来了")
	}
}
