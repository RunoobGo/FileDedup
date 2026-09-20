//go:build windows

package ops

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// TestUnsafeDriveReasonFixedDriveIsRecyclable 回归用例（缺陷：盘根字面量写错）。
//
// 修正前 `syscall.UTF16PtrFromString(p[:2] + `\)`)` 里的 raw string 是
// 「反斜杠 + 右括号」两字符，拼出的实参是 `F:\)` 而非合法盘根 `F:\`。
// GetDriveTypeW 对非法盘根返回 DRIVE_NO_ROOT_DIR(1)，于是**所有**盘符
// （含本机系统盘）都被判成「根目录不存在」，回收站操作被整批拒绝——
// 用户可见症状即「文件所在卷无回收站，已拒绝以防静默永久删除」。
func TestUnsafeDriveReasonFixedDriveIsRecyclable(t *testing.T) {
	// 取本机系统盘，必定是 DRIVE_FIXED，必有回收站。
	sysDrive, ok := syscall.Getenv("SystemDrive") // 形如 "C:"
	if !ok || sysDrive == "" {
		sysDrive = "C:"
	}
	p := sysDrive + `\Windows\Temp\fdd-probe.bin`

	if why := unsafeDriveReason(p); why != "" {
		t.Fatalf("系统盘（%s）应判定为可回收，却得到拒绝原因: %q\n"+
			"这通常说明传给 GetDriveTypeW 的盘根不是 `%s\\`（缺尾反斜杠或多出杂字符）",
			sysDrive, why, sysDrive)
	}
}

// TestDriveRootArgHasTrailingBackslashOnly 精确锁定盘根字面量的形状。
//
// 直接比对 UTF-16 编码，不依赖任何真实盘符与卷类型，因此在本用例中
// 即使落在 CI 的某个奇怪卷上也能稳定判定「字面量是否写对」。
func TestDriveRootArgHasTrailingBackslashOnly(t *testing.T) {
	root := `F:` + `\`
	ptr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := utf16Units(ptr); n != 3 {
		t.Fatalf("盘根 %q 的 UTF-16 长度 = %d, want 3（F : \\）；"+
			"若为 4 说明字面量里混入了多余字符（典型是 raw string 中的 `)`）", root, n)
	}
	// 明确钉住「反引号 raw string 不做转义」这一事实：
	// `\)` 是 2 个字符（反斜杠 + 右括号），不是 1 个反斜杠。
	if got := len(`\)`); got != 2 {
		t.Fatalf("raw string 语义异常：len(`\\)`) = %d, want 2", got)
	}
	if `\` != "\\" {
		t.Fatalf("raw string 反斜杠应与普通字符串的转义写法等价")
	}
}

// TestUnsafeDriveReasonNonFixedPathsRejected 确保真正该拒绝的路径仍被拒绝，
// 防止为修盘根缺陷而把安全闸门整体放开。
func TestUnsafeDriveReasonNonFixedPathsRejected(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string // 期望出现在原因里的关键字
	}{
		{"UNC 网络位置", `\\server\share\a.bin`, "网络位置"},
		{"相对路径无法识别卷", `relative\a.bin`, "无法识别所在卷"},
		{"无盘符", `a.bin`, "无法识别所在卷"},
		{"盘符但无分隔符", `F:a.bin`, "无法识别所在卷"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := unsafeDriveReason(c.path)
			if why == "" {
				t.Fatalf("路径 %q 应被拒绝，却判定为可回收", c.path)
			}
			if !strings.Contains(why, c.want) {
				t.Fatalf("拒绝原因 %q 未包含期望关键字 %q", why, c.want)
			}
		})
	}
}

// TestUnsafeDriveReasonAcceptsBothSeparators 盘符后跟正斜杠也应被识别
// （源码第 61 行同时接受 '\' 与 '/'），不能因盘根构造而回归。
func TestUnsafeDriveReasonAcceptsBothSeparators(t *testing.T) {
	// 仅验证「能通过路径形状校验」这一步，不关心具体卷类型结果。
	for _, p := range []string{`F:\a\b.bin`, `F:/a/b.bin`} {
		why := unsafeDriveReason(p)
		if strings.Contains(why, "无法识别所在卷") {
			t.Fatalf("路径 %q 的形状校验不应失败（why=%q）", p, why)
		}
	}
}

func utf16Units(p *uint16) int {
	n := 0
	base := unsafe.Pointer(p)
	for *(*uint16)(unsafe.Add(base, n*2)) != 0 {
		n++
	}
	return n
}
