package main

// M30（2026-09-21，设计稿 §12）：`frontend/src/wails.ts` 的类型声明必须与 Go 侧下发的
// JSON 字段一致——**两个方向都算不一致**：
//   - Go 有而 TS 无：前端按类型取不到这个字段（M6-P1~M21 期间积了 9 个，见 §12.0 E2）；
//   - TS 有而 Go 无：前端读一个永远 undefined 的字段，是反向的谎。
//
// 为什么放在根包测试而不是 `cmd/` 下的独立程序：`ScanSummary`/`FileView`/`OpRecordItem`
// 定义在 main 包，任何 `cmd/` 程序都 import 不了；而根包测试本来就在门禁里
// （`go test -race -count=4 .`），不新增门禁行、也不必把类型定义抄一份出来——
// 抄一份正是本项要防的漂移（§12.0 E6）。
//
// 分层：解析与比对是**无 IO 的纯函数**（tsInterfaceFields / goJSONNames /
// compareFieldSets），读文件与反射只出现在测试壳里（§12.1-4）。

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/history"
	"filededup/internal/model"
	"filededup/internal/ops"
)

const wailsTSPath = "frontend/src/wails.ts"

// mirrorPair 一对镜像：TS 接口 ↔ Go 类型。
//
// exempt 非空表示**显式豁免**（不做字段比对）——理由必须写清，不许静默跳过（§12.0 E4）。
// goType 为 nil 且 exempt 为空会被 TestWailsInterfacesAreAllMapped 判红。
type mirrorPair struct {
	ts     string
	goType reflect.Type
	exempt string
}

var wailsMirrors = []mirrorPair{
	{"Filters", reflect.TypeOf(model.Filters{}), ""},
	{"ScanConfig", reflect.TypeOf(model.ScanConfig{}), ""},
	{"ProgressEvent", reflect.TypeOf(model.ProgressEvent{}), ""},
	{"StageEvent", reflect.TypeOf(model.StageEvent{}), ""},
	{"ScanSummary", reflect.TypeOf(ScanSummary{}), ""},
	{"HistoryMeta", reflect.TypeOf(HistoryMeta{}), ""},
	{"FileView", reflect.TypeOf(FileView{}), ""},
	{"GroupView", reflect.TypeOf(GroupView{}), ""},
	{"ResultQuery", reflect.TypeOf(ResultQuery{}), ""},
	{"PagedResult", reflect.TypeOf(PagedResult{}), ""},
	{"FailedItem", reflect.TypeOf(model.FailedItem{}), ""},
	{"Settings", reflect.TypeOf(Settings{}), ""},
	{"KeepDecision", reflect.TypeOf(ops.KeepDecision{}), ""},
	{"KeepOutcome", reflect.TypeOf(KeepOutcome{}), ""},
	{"OpsProgress", reflect.TypeOf(model.OpsProgress{}), ""},
	{"OpsResult", reflect.TypeOf(model.OpsResult{}), ""},
	{"OpRequest", reflect.TypeOf(model.OpRequest{}), ""},
	{"OpRecord", reflect.TypeOf(history.OpMeta{}), ""},
	// OpRecordItem 镜像的是 main 包这一个（匿名嵌入 history.OpItem + 两枚标注字段），
	// **不是** history.OpItem 本身——映射指错时 isSymlink/dangling 会报成"多余"（§12.0 E3）。
	{"OpRecordItem", reflect.TypeOf(OpRecordItem{}), ""},
	{"OpRecordDetail", reflect.TypeOf(OpRecordDetail{}), ""},
	{"UndoResult", reflect.TypeOf(UndoResult{}), ""},
	{"CacheStats", reflect.TypeOf(cache.Stats{}), ""},
	{"PreviewData", reflect.TypeOf(PreviewData{}), ""},
	// 豁免两条：没有可反射的 Go 对象。
	{"OpsFiltered", nil, "Go 侧是 ops:filtered 的 map[string]any 字面量（app.go 的 emit 点），无结构体可反射；载荷要升级成结构体时随之收编"},
	{"BackendAPI", nil, "方法面（Promise 签名），不是数据镜像"},
	{"OpKind", nil, "字面量联合（export type）；Go 侧用字面量 case 校验（internal/ops/executor.go），没有常量集合"},
}

// tsInterfaceFields 从 wails.ts 源码里抽出一个 interface 的字段名（纯函数，无 IO）。
//
// 只认**本层**字段：内联对象类型里的键（如 `Failed: { Path: string }[]` 的 `Path`）不算。
// 解析规则刻意保守——这个文件是手写的，不是编译器前端：
//   - 逐行找 `export interface <name> {`，按花括号深度收口；
//   - 深度 1 的行去掉 `//` 注释后匹配 `名字 [?] :`；
//   - 空行、注释行、方法签名（没有冒号的）一律跳过。
func tsInterfaceFields(src, name string) ([]string, error) {
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "export interface "+name+" {") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("wails.ts 里找不到 export interface %s", name)
	}
	var fields []string
	depth := 0
	for _, l := range lines[start:] {
		code := l
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = code[:idx] // 注释里的花括号与大括号都不算
		}
		if depth == 1 {
			if f, ok := tsFieldName(code); ok {
				fields = append(fields, f)
			}
		}
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if depth <= 0 {
			break
		}
	}
	return fields, nil
}

// tsFieldName 一行是不是字段声明；是则返回字段名。纯函数。
func tsFieldName(line string) (string, bool) {
	s := strings.TrimSpace(line)
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return "", false
	}
	name := strings.TrimSpace(s[:colon])
	name = strings.TrimSuffix(name, "?")
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " {}\t") {
		return "", false
	}
	return name, true
}

// tsExportedNames 列出 wails.ts 里所有 `export interface X` / `export type X` 的名字，
// 值表示它是不是 interface（覆盖性断言只对 interface 要求"必须登记"）。
func tsExportedNames(src string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(src, "\n") {
		s := strings.TrimSpace(l)
		for _, kw := range []string{"export interface ", "export type "} {
			if !strings.HasPrefix(s, kw) {
				continue
			}
			rest := strings.TrimPrefix(s, kw)
			end := strings.IndexAny(rest, " {")
			if end < 0 {
				continue
			}
			out[rest[:end]] = kw == "export interface "
		}
	}
	return out
}

// goJSONNames 取一个 struct 类型的**有效 JSON 字段名**（纯函数，与 encoding/json 同规则）：
// 有 tag 取 tag 名、`json:"-"` 跳过、`omitempty` 不改名、无 tag 取 Go 字段名、
// 匿名嵌入结构体**提升**其字段（递归）。
func goJSONNames(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		embedsStruct := f.Anonymous && ft.Kind() == reflect.Struct
		// 未导出字段一律跳过，**唯一例外**是嵌入的结构体：encoding/json 不忽略
		// 「未导出类型嵌入」（其导出字段照样提升），只有未导出的非结构体嵌入才丢。
		if f.PkgPath != "" && !embedsStruct {
			continue
		}
		tag, hasTag := f.Tag.Lookup("json")
		name := strings.Split(tag, ",")[0]
		if embedsStruct && (!hasTag || name == "") {
			out = append(out, goJSONNames(ft)...)
			continue
		}
		if name == "-" {
			continue
		}
		if !hasTag || name == "" {
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}

// compareFieldSets 双向比对（纯函数）：missing = Go 有 TS 无；extra = TS 有 Go 无。
func compareFieldSets(goNames, tsNames []string) (missing, extra []string) {
	inTS := map[string]bool{}
	for _, n := range tsNames {
		inTS[n] = true
	}
	inGo := map[string]bool{}
	for _, n := range goNames {
		inGo[n] = true
	}
	for _, n := range goNames {
		if !inTS[n] {
			missing = append(missing, n)
		}
	}
	for _, n := range tsNames {
		if !inGo[n] {
			extra = append(extra, n)
		}
	}
	return missing, extra
}

func readWailsTS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(wailsTSPath)
	if err != nil {
		t.Fatalf("读不到 %s：%v（根包测试的工作目录即仓库根）", wailsTSPath, err)
	}
	return string(b)
}

// TestTSInterfaceFieldsParsesRealFile 解析器对着真文件与合成片段各跑一遍（§12.3 V1）。
func TestTSInterfaceFieldsParsesRealFile(t *testing.T) {
	src := readWailsTS(t)

	t.Run("真文件：可选字段与内联对象类型", func(t *testing.T) {
		got, err := tsInterfaceFields(src, "OpRequest")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Kind", "FileIDs", "TargetDir", "ConfirmDanger", "ProcessDirs"}
		assertSameOrder(t, "OpRequest", want, got)
	})
	t.Run("真文件：内联对象元素里的键不算本层字段", func(t *testing.T) {
		got, err := tsInterfaceFields(src, "OpsResult")
		if err != nil {
			t.Fatal(err)
		}
		// Failed 的元素是内联对象 { Path; Stage; Err }——那三个键不是 OpsResult 的字段。
		want := []string{"OK", "Failed", "Skipped", "Cancelled", "Reclaimed",
			"TrashedBytes", "LinkedBytes", "SymlinkedBytes", "Warnings"}
		assertSameOrder(t, "OpsResult", want, got)
	})
	t.Run("合成片段：注释、嵌套花括号、空行", func(t *testing.T) {
		snippet := strings.Join([]string{
			"export interface Demo {",
			"  // 注释里有 } 花括号",
			"  first: number",
			"",
			"  nested: {",
			"    inner: string",
			"  }",
			"  second?: string // 行尾注释",
			"}",
			"export interface Other {",
			"  notMe: number",
			"}",
		}, "\n")
		got, err := tsInterfaceFields(snippet, "Demo")
		if err != nil {
			t.Fatal(err)
		}
		assertSameOrder(t, "Demo", []string{"first", "nested", "second"}, got)
	})
	t.Run("不存在的接口报错而不是空集", func(t *testing.T) {
		if _, err := tsInterfaceFields(src, "NoSuchInterface"); err == nil {
			t.Fatal("查无此接口时必须报错——空集会让覆盖性断言静默通过")
		}
	})
}

// TestGoJSONNamesFollowsEncodingJSON 有效 JSON 名规则（§12.3 V3，合成类型）。
func TestGoJSONNamesFollowsEncodingJSON(t *testing.T) {
	got := goJSONNames(reflect.TypeOf(jsonNameSynth{}))
	// 顺序即字段声明顺序：两个嵌入（一个未导出类型、一个指针）都提升其导出字段。
	want := []string{"NoTag", "taggedName", "omitMe", "UntaggedPtr", "promoted", "PromotedPtr"}
	assertSameOrder(t, "jsonNameSynth", want, got)
}

type jsonNameSynthEmbedded struct {
	Promoted string `json:"promoted"`
	// Hash 这一类：json:"-" 的字段**不得**出现在有效名里（真文件里 history.OpItem.Hash 就是它）。
	Skipped []byte `json:"-"`
}

type jsonNameSynth struct {
	NoTag       string
	Tagged      int    `json:"taggedName"`
	Omit        string `json:"omitMe,omitempty"`
	UntaggedPtr *int
	jsonNameSynthEmbedded
	*jsonNameSynthEmbeddedPtr
}

type jsonNameSynthEmbeddedPtr struct {
	PromotedPtr string `json:"PromotedPtr"`
}

// TestWailsTypesCoverGoFields 逐对双向比对（§12.3 V2）。
func TestWailsTypesCoverGoFields(t *testing.T) {
	src := readWailsTS(t)
	var missing, extra []string
	for _, p := range wailsMirrors {
		if p.exempt != "" {
			continue
		}
		tsFields, err := tsInterfaceFields(src, p.ts)
		if err != nil {
			t.Errorf("TS 接口 %s：%v", p.ts, err)
			continue
		}
		miss, ext := compareFieldSets(goJSONNames(p.goType), tsFields)
		for _, f := range miss {
			missing = append(missing, p.ts+"."+f)
		}
		for _, f := range ext {
			extra = append(extra, p.ts+"."+f)
		}
	}
	if len(missing) > 0 {
		t.Errorf("TS 侧缺 %d 个 Go 下发字段（前端按类型取不到）：%s",
			len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("TS 侧多 %d 个 Go 侧不存在的字段（前端读到的是永远 undefined）：%s",
			len(extra), strings.Join(extra, ", "))
	}
}

// TestWailsInterfacesAreAllMapped 覆盖性自证（§12.3 V4）：
//   - 文件里每个 interface 必须在映射表里（新增接口不许静默漏登记）；
//   - 映射表里每个 TS 名必须在文件里存在（接口改名后不许留幽灵）；
//   - 表里的豁免项必须写了理由（不许静默跳过）。
func TestWailsInterfacesAreAllMapped(t *testing.T) {
	src := readWailsTS(t)
	declared := tsExportedNames(src)

	table := map[string]bool{}
	for _, p := range wailsMirrors {
		table[p.ts] = true
		if p.exempt == "" && p.goType == nil {
			t.Errorf("映射表 %s：既没有 Go 类型也没有豁免理由", p.ts)
		}
		if _, ok := declared[p.ts]; !ok {
			t.Errorf("映射表里的 %s 在 wails.ts 里不存在（接口改名了？）", p.ts)
		}
	}
	for name, isInterface := range declared {
		if isInterface && !table[name] {
			t.Errorf("wails.ts 的 export interface %s 未登记进映射表：新增的镜像面必须登记，或写明豁免理由", name)
		}
	}
}

func assertSameOrder(t *testing.T, what string, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s 字段数 = %d, want %d\n got = %v\nwant = %v", what, len(got), len(want), got, want)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("%s 字段[%d] = %q, want %q\n got = %v\nwant = %v", what, i, got[i], want[i], got, want)
		}
	}
}
