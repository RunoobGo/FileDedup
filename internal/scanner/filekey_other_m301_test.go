package scanner

// M301 的兜底腿判据（scanner 侧）：`keyFromInfo` / `ResolveKey` 的平台实现必须闭合。
//
// 缺陷形状（登记原文，04 §6.51 二·5）：本包只有 `filekey_windows.go`（`//go:build windows`）
// 与 `filekey_unix.go`（`//go:build darwin || linux`）**两份**，而无 tag 的
// `scanner.go:566` 直接调用 `keyFromInfo` ⇒ 第四平台（freebsd）上
// `GOOS=freebsd GOARCH=amd64 go build ./internal/...` 交回
// `internal\scanner\scanner.go:566:14: undefined: keyFromInfo`，
// 归因被推给了编译器。取向：补一枚 `!darwin && !linux && !windows` 的兜底腿，
// 让"本平台没有可靠 inode 来源"落成一个**读得懂的保守值**，而不是一条链接期错误。
//
// 兜底腿为什么交回 `Resolved:false` 而不是报错：这与 windows 扫描期的既有形状同形
// （`keyFromInfo` 在 windows 腿上同样返回未解析）⇒ 最坏后果是"认不出硬链接"
// （重复计数虚高），绝不是"把两个不同文件认成同一个"（那会误删）。这一条取向写进
// 注释，是因为它就是本文件那两条反面断言的全部理由。
//
// ★ 为什么读源文件而不是调用函数：兜底腿带着 `!darwin && !linux && !windows`，
//   在本仓三个目标平台上根本不参与编译，运行期断言无从执行（与 ops 侧
//   `trash_other_m301_test.go`、以及 M270/M280 的接线锚同一手法）。
//   "能编过"由交叉编译读数承担（docs/05 复测清单）。

import (
	"os"
	"strings"
	"testing"
)

// 兜底 tag 的**逐字**形状：与 `internal/fsid/fsid_other.go` 同形（本仓多数包用这一种，
// 顺序也是仓内已有的那一种）。
const otherFilekeyTag = "//go:build !darwin && !linux && !windows"

func TestM301FileKeyHasCatchAllLeg(t *testing.T) {
	raw, err := os.ReadFile("filekey_other.go")
	if err != nil {
		t.Fatalf("兜底腿不存在（M301 的原形状：只有两份枚举）：%v", err)
	}
	src := string(raw)
	if first := strings.SplitN(src, "\n", 2)[0]; !strings.HasPrefix(first, otherFilekeyTag) {
		t.Errorf("兜底腿的 tag 不是仓内那一种风格：首行 %q，期望前缀 %q", first, otherFilekeyTag)
	}
	// 两个入口都要有：`keyFromInfo` 被无 tag 的 scanner.go 调用，
	// `ResolveKey` 被无 tag 的 dedup/pipeline.go 调用——少一个就还是 undefined。
	for _, fn := range []string{"func keyFromInfo(", "func ResolveKey("} {
		if !strings.Contains(src, fn) {
			t.Errorf("兜底腿没定义 %s ⇒ 无 tag 文件在第四平台上仍然 undefined", fn)
		}
	}
	// ★ 反面断言（本行的实质）：兜底腿**不得声称已解析**。
	//   `Resolved:true` 配上零值 key，会让 pipeline 的 `seen[k]` 把所有未解析文件
	//   收进同一个键 ⇒ 第四平台上"不同文件被判成硬链接、只保留一个、其余进回收站"。
	if strings.Contains(src, "Resolved: true") || strings.Contains(src, "Resolved:true") {
		t.Error("兜底腿交回 Resolved:true ⇒ 零值 key 会冒充身份，把不同文件认成同一个（误删面）")
	}
	if !strings.Contains(src, "model.FileKey{}") && !strings.Contains(src, "Resolved: false") {
		t.Error("兜底腿没有交出「未解析」的形状 ⇒ 保守取向没落地")
	}
	// 那句注释要点名支持面，读者才知道"这台机器为什么是保守值"。
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if !strings.Contains(src, goos) {
			t.Errorf("兜底腿的声明没点名已支持平台 %s", goos)
		}
	}
}

// 两份既有腿一字未动：修 M301 只许**加**兜底，不许顺手把 windows 腿改成兜底式，
// 也不许把 unix 腿的 tag 改成更宽的写法（那会让 darwin/linux 落到保守值上）。
func TestM301FileKeyLegsStillSpecific(t *testing.T) {
	for name, want := range map[string]string{
		"filekey_windows.go": "//go:build windows",
		"filekey_unix.go":    "//go:build darwin || linux",
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

// 扫描期在三个目标平台上都**已有** keyFromInfo（谁调用它的接线锚）。
//
// 为什么需要这一条：上面两枚用例只问"文件在不在、tag 对不对"，而本行的缺陷面是
// "无 tag 的调用点没被任何腿覆盖" ⇒ 若哪天 `scanner.go` 的调用被删掉，兜底腿就成了
// 无主代码，而本文件一声不响（与 event-channel-names 的扫描面自证同一族）。
func TestM301CallSitesStillThere(t *testing.T) {
	scan, err := os.ReadFile("scanner.go")
	if err != nil {
		t.Fatalf("scanner.go 读不到：%v", err)
	}
	if !strings.Contains(string(scan), "keyFromInfo(") {
		t.Error("scanner.go 里的 keyFromInfo 调用点不见了 ⇒ 本文件的兜底腿判据已无的放矢，请连带更新这两枚用例")
	}
	pipeline, err := os.ReadFile("../dedup/pipeline.go")
	if err != nil {
		t.Fatalf("dedup/pipeline.go 读不到：%v", err)
	}
	if !strings.Contains(string(pipeline), "scanner.ResolveKey(") {
		t.Error("pipeline.go 里的 scanner.ResolveKey 调用点不见了 ⇒ 同上，判据失去对象")
	}
}
