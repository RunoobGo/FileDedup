package ops

// M280：`SHFileOperation` 的裸错误码直达用户。
//
// 缺陷形状（登记原文）：Windows 回收站失败时用户看到的是
// `SHFileOperation 错误码 32` 这样一句话——数字既没有单位也没有归因，
// 而 32 恰恰是这台机器上**最常见**的那一档（真机读数：80 项混批里 15 项被别的
// 进程占用 ⇒ Shell 返回 32）。用户既不知道是自己开着视频播放器，也不知道该干什么。
//
// 判据表刻意放在**无 build tag** 的文件里（与 recycle_policy.go / winerrno.go 同一条
// 纪律：纯逻辑进 Linux CI 主门禁，平台真值由调用侧注入）。
//
// ★ 只收录**有据可查**的码，其余一律报"未收录"并保留裸码：
// `FO_E_*` 与 Win32 错误码在 0x7C~0x7F 这一段**数值重叠**（FO_E_FILENOTFOUND 就是 124，
// 而 124 在 Win32 表里是另一回事），Shell 在 Vista+ 还常改交 HRESULT。
// 给一个说不清的码配上自信的原因，就是 M279 那类假警报的制造法
// （那次让用户拿着"数据可能已永久删除"的提示去跑了数据恢复工具）。

import (
	"strconv"
	"strings"
	"testing"
)

// ① 真机那一档必须被翻出来：32 = 文件被其他程序占用。
func TestSHFileOperationReasonNamesTheMachineReadCode(t *testing.T) {
	got := shFileOperationReason(32)
	for _, want := range []string{"占用", "32"} {
		if !strings.Contains(got, want) {
			t.Fatalf("错误码 32 的译文缺 %q：%s", want, got)
		}
	}
	// 归因之外还要给出**下一步**：这一档是可重试的，不该让用户以为数据丢了。
	if !strings.Contains(got, "重试") && !strings.Contains(got, "关闭") {
		t.Fatalf("占用类失败没有给出可操作出路：%s", got)
	}
	if strings.Contains(got, "永久删除") {
		t.Fatalf("占用与永久删除无关，这样写会造出 M279 型假警报：%s", got)
	}
}

// ② 每一档都必须把**裸码留在带码括弧里**：译文会随版本变，取证时以数字为准
// （本项目全部真机读数都是按数字归档的，§6.30 / §6.44 那批同理）。
func TestSHFileOperationReasonKeepsRawCodeForEveryArm(t *testing.T) {
	for _, code := range []int64{2, 3, 5, 32, 33, 36, 112, 424242, 0x80270024} {
		got := shFileOperationReason(code)
		if got == "" {
			t.Fatalf("错误码 %d 交回空串（上层会当成「没有原因」）", code)
		}
		// 括弧用全角：本仓中文文案一律全角（半角括号在本项目只出现在代码/路径里）。
		if !strings.Contains(got, "（") || !strings.Contains(got, "）") {
			t.Fatalf("错误码 %d 的译文没有带码括弧：%s", code, got)
		}
		if !strings.Contains(got, strconv.FormatInt(code, 10)) {
			t.Fatalf("错误码 %d 的译文里找不到该数字：%s", code, got)
		}
	}
}

// ③ 未收录的码**不许**配原因。这一条是整张表的安全边界：
// 有人为了"让提示更友好"把兜底臂改成一句万能归因，用户就会按假原因去排查。
func TestSHFileOperationReasonRefusesToGuessUnknownCodes(t *testing.T) {
	got := shFileOperationReason(424242)
	if !strings.Contains(got, "未收录") {
		t.Fatalf("未收录的码没有如实说「未收录」：%s", got)
	}
	// 十六进制形状也要给：Shell 改交 HRESULT 时（0x8027xxxx），十进制读数无法与文档对表。
	if !strings.Contains(strings.ToUpper(got), "0X") {
		t.Fatalf("未收录的码没给十六进制形状，HRESULT 无法对表：%s", got)
	}
	// 不许蹭任何一档的具体归因。
	for _, banned := range []string{"占用", "磁盘空间", "拒绝访问"} {
		if strings.Contains(got, banned) {
			t.Fatalf("未收录档写出了猜来的归因 %q：%s", banned, got)
		}
	}
	// 也不许把"失败原因不明"升级成数据丢失警报。
	if strings.Contains(got, "永久删除") {
		t.Fatalf("未收录档不得断言数据已丢：%s", got)
	}
}

// ④ 常见档各自独立、不串台：权限类不许写成"占用"，空间类不许写成"权限"。
// 串台的代价是用户去关掉杀毒软件，而真正的问题是盘满了。
func TestSHFileOperationReasonKeepsCausesSeparate(t *testing.T) {
	denied := shFileOperationReason(5)
	if !strings.Contains(denied, "权限") && !strings.Contains(denied, "拒绝访问") {
		t.Fatalf("错误码 5 没归到权限：%s", denied)
	}
	if strings.Contains(denied, "占用") {
		t.Fatalf("权限失败被写成了占用：%s", denied)
	}
	full := shFileOperationReason(112)
	if !strings.Contains(full, "空间") {
		t.Fatalf("错误码 112 没归到磁盘空间：%s", full)
	}
	if strings.Contains(full, "权限") {
		t.Fatalf("磁盘满被写成了权限：%s", full)
	}
	// 找不到文件/路径这一档必须说清"可能本来就不在了"——它与"我们删掉了"要分开，
	// 执行器的 S8 语义正是按这个区分落账的。
	for _, code := range []int64{2, 3} {
		got := shFileOperationReason(code)
		if !strings.Contains(got, "找不到") {
			t.Fatalf("错误码 %d 没归到「找不到」：%s", code, got)
		}
	}
}

// ⑤ 0 不该到这里：调用点只在 `r0 != 0` 时翻译。兜底臂必须**响亮**，
// 万一将来有人在成功路径上误调，得到的是一句"未收录"而不是一句"成功"。
func TestSHFileOperationReasonHasNoSuccessArm(t *testing.T) {
	if got := shFileOperationReason(0); !strings.Contains(got, "未收录") {
		t.Fatalf("0 被配上了原因（调用点只在失败时才来这里，成功语义会掩盖误用）：%s", got)
	}
}
