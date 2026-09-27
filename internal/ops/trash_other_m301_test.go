package ops

// M301 的兜底腿判据：`defaultTrash` 的平台实现必须以"三份枚举 + 一枚兜底"闭合。
//
// 缺陷形状（登记原文，04 §6.51 二·5）：改前 `internal/ops` 只有
// `trash_windows.go` / `trash_darwin.go` / `trash_linux.go` **三份枚举式 tag**，
// 而无 tag 的 `trash.go:14` 直接引用 `defaultTrash` ⇒ 第四个平台（freebsd）上
// `go build` 交回的是 `undefined: defaultTrash`。本行的取证方式是**真跑交叉编译**，
// 修前的现读原文是：
//
//	GOOS=freebsd GOARCH=amd64 go build ./internal/...
//	  internal\ops\trash.go:14:61: undefined: defaultTrash
//	  internal\scanner\scanner.go:566:14: undefined: keyFromInfo
//
// ⇒ 归因被推给了编译器。取向（与 M289/M270 的"三态如实"同族）：兜底腿显式 fail-closed
//   并带注释，让"本平台没实现"成为一句**人话**而不是一条链接期错误。
//
// ★ 为什么这里读源文件而不是直接调函数：兜底腿带着 `!windows && !darwin && !linux`，
//   在本仓的三个目标平台上根本不参与编译，运行期断言无从执行（同 M270/M280 的接线锚手法）。
//   真正的"能编过"由上面那条交叉编译读数承担（docs/05 复测清单里也有它）。

import (
	"os"
	"strings"
	"testing"
)

// 兜底 tag 的**逐字**形状：与 `internal/fsid/fsid_other.go` 同形（本仓多数包用这一种）。
// 顺序也钉住——三种写法在 Go 里等价，但仓库只有一种风格是有意的（M301 钉的就是"风格并存"）。
const otherPlatformTag = "//go:build !darwin && !linux && !windows"

func TestM301TrashHasCatchAllLeg(t *testing.T) {
	raw, err := os.ReadFile("trash_other.go")
	if err != nil {
		t.Fatalf("兜底腿不存在（M301 的原形状：只有三份枚举）：%v", err)
	}
	src := string(raw)
	if first := strings.SplitN(src, "\n", 2)[0]; !strings.HasPrefix(first, otherPlatformTag) {
		t.Errorf("兜底腿的 tag 不是仓内那一种风格：首行 %q，期望前缀 %q", first, otherPlatformTag)
	}
	if !strings.Contains(src, "func defaultTrash(") {
		t.Error("兜底腿没有定义 defaultTrash ⇒ 引用它的无 tag 文件仍然 undefined")
	}
	// fail-closed 的形状：必须交回一个 error，且不得是"成功但什么都不做"。
	if strings.Contains(src, "return nil, nil") {
		t.Error("兜底腿 return nil, nil ⇒ 第四平台上'删除进了回收站'是一句假话")
	}
	if !strings.Contains(src, "errors.New") && !strings.Contains(src, "fmt.Errorf") {
		t.Error("兜底腿没有造句 ⇒ 归因仍然推给编译器")
	}
	// 那句声明要点名支持面（用户看得懂"这台机器上为什么不行"）。
	// ★ 只看 errors.New(...) 里的那句话，不看整个文件——变异 Y2 证过：把文案里的
	//   「当前支持 windows / darwin / linux」换成「本平台」后，整文件级的 Contains
	//   仍被文件头注释里的那三个词喂饱 ⇒ 判据存活，等于没钉。用户看得见的那句才是射程。
	msg := errorTextOf(src)
	if msg == "" {
		t.Fatal("兜底腿里找不到 errors.New / fmt.Errorf 的第一参数（字符串字面量）⇒ 上面的造句判据已无实体可查")
	}
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if !strings.Contains(msg, goos) {
			t.Errorf("兜底腿给用户的那句声明没点名已支持平台 %s（文案：%s）", goos, msg)
		}
	}
}

// errorTextOf 取出 errors.New / fmt.Errorf 首个实参里的字符串字面量内容。
// 找第一个引号后的正文，到配对的引号为止（本仓文案里没有转义引号，简单扫描即可）。
func errorTextOf(src string) string {
	i := strings.Index(src, "errors.New(")
	if i < 0 {
		i = strings.Index(src, "fmt.Errorf(")
	}
	if i < 0 {
		return ""
	}
	rest := src[i:]
	open := strings.Index(rest, `"`)
	if open < 0 {
		return ""
	}
	close := strings.Index(rest[open+1:], `"`)
	if close < 0 {
		return ""
	}
	return rest[open+1 : open+1+close]
}

// 枚举腿一字未动：修 M301 只许**加**兜底，不许把 windows 腿改成兜底式。
func TestM301PlatformLegsStillSpecific(t *testing.T) {
	for name, want := range map[string]string{
		"trash_windows.go": "//go:build windows",
		"trash_darwin.go":  "//go:build darwin",
		"trash_linux.go":   "//go:build linux",
	} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s 读不到：%v", name, err)
		}
		if first := strings.SplitN(string(raw), "\n", 2)[0]; first != want {
			t.Errorf("%s 的首行 tag 被改动了：%q（期望 %q）", name, first, want)
		}
	}
}
