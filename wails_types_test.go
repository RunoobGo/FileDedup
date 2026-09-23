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
	"sort"
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
	// M44/G10（2026-09-21 审查）：Go 下发但 TS 压根没有镜像的类型，M30 的枚举方向
	// 看不到（§14.0 G10）——它是 PreviewProcessPolicy 的返回类型。
	{"ProcessPreview", reflect.TypeOf(ProcessPreview{}), ""},
	{"PendingQuery", reflect.TypeOf(PendingQuery{}), ""},
	{"PendingRow", reflect.TypeOf(PendingRow{}), ""},
	{"PendingPage", reflect.TypeOf(PendingPage{}), ""},
	// 豁免三条：没有可反射的 Go 对象。
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
	body, err := tsInterfaceBody(src, name)
	if err != nil {
		return nil, err
	}
	var fields []string
	for _, code := range body {
		if f, ok := tsFieldName(code); ok {
			fields = append(fields, f)
		}
	}
	return fields, nil
}

// tsInterfaceBody 抽出 interface 本层（花括号深度 1）的代码行，注释已剥掉。
// tsInterfaceFields / tsInterfaceMethods 共用这一份遍历（I5：两处解析必然漂）。
func tsInterfaceBody(src, name string) ([]string, error) {
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
	var body []string
	depth := 0
	for _, l := range lines[start:] {
		code := l
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = code[:idx] // 注释里的花括号与大括号都不算
		}
		if depth == 1 {
			body = append(body, code)
		}
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if depth <= 0 {
			break
		}
	}
	return body, nil
}

// tsInterfaceMethods 同一个 body 里挑出**方法声明**（`名字(` 开头），顺序即声明顺序。
func tsInterfaceMethods(src, name string) ([]string, error) {
	body, err := tsInterfaceBody(src, name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, code := range body {
		if m, ok := tsMethodName(code); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// tsMethodName 一行是不是方法声明；是则返回方法名。纯函数。
// 判据 = "`(` 之前那一段全是标识符字符"：字段声明（`kind: string`）的冒号前不含括号，
// 内联对象类型（`nested: {`）的冒号前也不是标识符，两条都进不来。
func tsMethodName(line string) (string, bool) {
	s := strings.TrimSpace(line)
	paren := strings.Index(s, "(")
	if paren <= 0 {
		return "", false
	}
	name := s[:paren]
	for _, r := range name {
		if r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		return "", false
	}
	return name, true
}

// goExportedMethods 反射取一个类型的方法集名字（升序）。reflect 只报**导出**方法，
// 因此生命周期钩子（startup/shutdown/beforeClose 刻意小写）自然不在面里——
// 本项因此不需要排除表（设计稿 §14.1-1；一次性程序实测同包两方法类型 NumMethod()==1）。
func goExportedMethods(t reflect.Type) []string {
	out := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		out = append(out, t.Method(i).Name)
	}
	sort.Strings(out)
	return out
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
		want := []string{"Kind", "FileIDs", "TargetDir", "ConfirmDanger", "ProcessDirs", "ExcludeDirs"}
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

// compareMirrorPair 比对一对镜像面：Go 有效 JSON 名 ↔ TS 接口字段名。
//
// GATE-2（2026-09-21 全量审查）：第三个返回值 vacuous 标的是「这一对其实一个名字
// 都没比」。missing / extra 两条判据在**两侧皆空**时同时静默通过——字段面门禁可以
// 绿得一次比对都没发生。同一把锁在方法面上早就有了（本文件
// TestBackendAPIMatchesGoExportedMethods 里那句 len(tsMethods)==0 ⇒ t.Fatal，
// 理由是「解析器失效会让本项静默通过」），字段面漏了对称的那一份。
func compareMirrorPair(p mirrorPair, src string) (missing, extra, vacuous []string, compared int) {
	tsFields, err := tsInterfaceFields(src, p.ts)
	if err != nil {
		// 接口体取不到也算空转：它同样导致「比了 0 个字段却报绿」。
		return nil, nil, []string{p.ts + "（接口体解析失败：" + err.Error() + "）"}, 0
	}
	goNames := goJSONNames(p.goType)
	switch {
	case len(goNames) == 0:
		vacuous = append(vacuous, p.ts+"（Go 侧 "+p.goType.String()+" 反射出 0 个 JSON 字段名）")
	case len(tsFields) == 0:
		vacuous = append(vacuous, p.ts+"（TS 侧接口体解析出 0 个字段）")
	}
	miss, ext := compareFieldSets(goNames, tsFields)
	for _, f := range miss {
		missing = append(missing, p.ts+"."+f)
	}
	for _, f := range ext {
		extra = append(extra, p.ts+"."+f)
	}
	return missing, extra, vacuous, len(goNames)
}

// TestWailsTypesCoverGoFields 逐对双向比对（§12.3 V2）+ 空转自检（GATE-2）。
func TestWailsTypesCoverGoFields(t *testing.T) {
	src := readWailsTS(t)
	var missing, extra, vacuous []string
	pairs, compared := 0, 0
	for _, p := range wailsMirrors {
		if p.exempt != "" {
			continue
		}
		pairs++
		m, e, v, n := compareMirrorPair(p, src)
		missing = append(missing, m...)
		extra = append(extra, e...)
		vacuous = append(vacuous, v...)
		compared += n
	}
	// 两条下界的取值依据（本轮实测）：**24 对 / 130 个名字**。
	// 留的是"字段增减不误报、表面塌陷必报"的余量——少一两个字段由 missing/extra
	// 负责，这里只挡"整片比对没了"这一档，所以地板远低于实测、又高于任何合理子集。
	if len(vacuous) > 0 {
		t.Errorf("有 %d 对镜像面空转（一个字段都没比，却不会让本项变红）：%s",
			len(vacuous), strings.Join(vacuous, "; "))
	}
	if pairs < 20 {
		t.Errorf("字段面只登记了 %d 对参与比对（基线 24 对）：映射表被清空时本项不得读作通过", pairs)
	}
	if compared < 100 {
		t.Errorf("字段面合计只比了 %d 个 Go 字段名（基线 130 个）：解析器或反射失效会让本项静默通过", compared)
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

// TestCompareMirrorPairFlagsVacuous 空转自检本身要被测到：
// 判据若不报警，GATE-2 就只是句注释。这里喂一对"Go 侧没有任何导出字段"的镜像面，
// 它必须落在 vacuous 里（而不是因为"两边都空所以没差异"而通过）。
type noExportedJSONFields struct {
	hidden     int
	alsoHidden string //nolint:unused // 刻意造一个无导出字段的类型，供 vacuous 自检用
}

func TestCompareMirrorPairFlagsVacuous(t *testing.T) {
	src := readWailsTS(t)
	p := mirrorPair{ts: "FailedItem", goType: reflect.TypeOf(noExportedJSONFields{})}
	_, _, vacuous, compared := compareMirrorPair(p, src)
	if len(vacuous) != 1 {
		t.Fatalf("Go 侧 0 字段名却未被标为空转（vacuous=%v）——GATE-2 的锁是空的", vacuous)
	}
	if !strings.Contains(vacuous[0], "FailedItem") || !strings.Contains(vacuous[0], "0 个") {
		t.Errorf("空转报告没点名是哪一对、为什么：%q", vacuous[0])
	}
	if compared != 0 {
		t.Errorf("compared = %d，应为 0（Go 侧本来就没名字）", compared)
	}
	// 正对照：真表里的 FailedItem 一侧不许被标空转。
	real := mirrorPair{ts: "FailedItem", goType: reflect.TypeOf(model.FailedItem{})}
	if _, _, v, n := compareMirrorPair(real, src); len(v) != 0 || n == 0 {
		t.Errorf("正常的 FailedItem 对被误标为空转（vacuous=%v compared=%d）", v, n)
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

// TestBackendAPIMatchesGoExportedMethods 方法面双向比对（G5/P1，设计稿 §14.1-1）。
//
// 为什么源是 Go 的反射方法集而不是生成物 `wailsjs/go/main/App.d.ts`：Wails 运行时按反射
// 注入 `window.go.main.App.*`，手写声明才是前端真正依赖的契约面，而生成物只在 `wails build`
// 时刷新——那一步不在 §3.3 门禁 13 行里，所以它已经在漂移（本轮实测缺两枚，登记 M46）。
//
// **零豁免**：Go 侧反射本来就只报导出方法（生命周期钩子刻意小写，见 goExportedMethods
// 注释），TS 侧声明的每个名字都必须真有后端。将来确实需要豁免时，必须像 wailsMirrors 的
// exempt 那样把理由写在数据里，不许在判据代码里开洞。
func TestBackendAPIMatchesGoExportedMethods(t *testing.T) {
	src := readWailsTS(t)
	tsMethods, err := tsInterfaceMethods(src, "BackendAPI")
	if err != nil {
		t.Fatal(err)
	}
	if len(tsMethods) == 0 {
		t.Fatal("BackendAPI 解析出 0 枚方法声明：解析器失效会让本项静默通过")
	}
	goMethods := goExportedMethods(reflect.TypeOf(&App{}))

	missing, extra := compareFieldSets(goMethods, tsMethods)
	if len(missing) > 0 {
		t.Errorf("BackendAPI 缺 %d 枚 Go 导出方法（后端在、前端按类型调不到）：%s",
			len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("BackendAPI 声明了 %d 枚 Go 侧不存在的方法（运行期必然 reject，是反向的谎）：%s",
			len(extra), strings.Join(extra, ", "))
	}
	// 计数一并打出：漂移发生时除了名字还要知道规模（划账要抄这个数）。
	t.Logf("方法面：Go 导出 %d 枚 / BackendAPI 声明 %d 枚", len(goMethods), len(tsMethods))
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
