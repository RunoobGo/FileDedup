package main

// M202 回归（登记 §6.29，设计稿 `2026-09-24-error-shell-m202-design`）：
// 错误事件的中文外壳 + 原文降级 detail。
//
// 缺陷原形：`app.go` 三个发射点把 `err.Error()` 原样 emit ⇒ 界面直接出现
// `open /Users/…: permission denied` 这类英文 OS 错误与内部绝对路径。
//
// 四格的分工（★ P-2 是护栏不是红探针，AS-K2）：
//   P-1 真 OS 错误 → 外壳中文、路径不进外壳、detail 逐字等于原文；
//   P-2 应用自撰中文错误 → 外壳必须**一字不改**（这一格拦的是"一律套壳"这个反向失败）；
//   P-3 认不出的英文 → 必须是显式"未分类"，不许假装知道；
//   P-4 panic 腿走真事件路径 → 外壳是内部异常档，原句（含 kind）整串进 detail。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

// P-1
func TestM202RealOSErrorGetsChineseShell(t *testing.T) {
	// 真取一条 OS 错误（不存在的路径）：三平台的 errno 字面不同（unix 是
	// "no such file or directory"，Windows 折成 "The system cannot find the file/path
	// specified."），故断言按"外壳不含路径分隔符 + 是已分类的中文句"来写，
	// 不把某平台的英文字面钉进断言。
	missing := filepath.Join(t.TempDir(), "definitely-not-here")
	_, err := os.Open(missing)
	if err == nil {
		t.Fatal("前提：打开不存在的路径必须失败，否则本用例取不到 OS 错误")
	}

	shell, detail := errorShell(err)
	if shell == "" {
		t.Fatal("外壳为空：M202 的收口就是给事件载荷一个中文外壳")
	}
	if strings.ContainsAny(shell, `/\`) {
		t.Errorf("外壳里漏了路径（会把内部绝对路径直呈用户）：%q", shell)
	}
	if strings.ContainsAny(shell, "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("外壳仍是英文串，没套上壳：%q", shell)
	}
	if shell == "操作未能完成（未分类的系统错误）" {
		t.Errorf("真实 OS 错误落进未分类档 ⇒ 签名表漏了这一平台的 errno 字面：原文 %q", err)
	}
	if detail != err.Error() {
		t.Errorf("detail 必须逐字等于原文（排查与复制都靠它）：\n got %q\nwant %q", detail, err)
	}

	// detail 与外壳不同时才写键：B 类（原文即中文句）不该多一个空分支。
	ev := errorEvent(err)
	if ev["error"] != shell {
		t.Errorf("载荷 error 键应为外壳：got %q want %q", ev["error"], shell)
	}
	if ev["detail"] != detail {
		t.Errorf("载荷 detail 键应为原文：got %q want %q", ev["detail"], detail)
	}
}

// P-2 护栏：应用自己写的中文句子必须原样透出。
func TestM202AppAuthoredChinesePassesThroughUnchanged(t *testing.T) {
	// 逐字取自回收站拒绝分支的字面（`internal/ops/trash_windows.go`），
	// 以及一条"中文里嵌着英文 OS 原因"的形状（身份拦截句会把底层原因原样带出）。
	cases := []string{
		"3 个文件无法保证进入回收站，已拒绝以防静默永久删除；请改用「移动」或自行确认后再「永久删除」。首个: /tmp/x",
		"无法确认文件仍是扫描时那个对象（open /tmp/y: permission denied），已拦截",
	}
	for _, raw := range cases {
		shell, detail := errorShell(errors.New(raw))
		if shell != raw {
			t.Errorf("应用自撰中文被改写了（信息降级）：\n got %q\nwant %q", shell, raw)
		}
		ev := errorEvent(errors.New(raw))
		if _, ok := ev["detail"]; ok {
			t.Errorf("外壳即原文时不该再写 detail（前端多一个重复分支）：%q", ev["detail"])
		}
		_ = detail
	}
}

// P-3：认不出的一律显式未分类，不假装知道。
func TestM202UnknownEnglishIsExplicitlyUnclassified(t *testing.T) {
	raw := "weird internal thing 42 happened"
	shell, detail := errorShell(errors.New(raw))
	if shell != "操作未能完成（未分类的系统错误）" {
		t.Errorf("未分类档形同虚设（等于把任意英文直呈用户）：%q", shell)
	}
	if detail != raw {
		t.Errorf("detail 丢了原文：got %q want %q", detail, raw)
	}
}

// P-4：panic 腿走**真事件路径**（与 TestScanGoroutinePanicIsContained 同一条注入路），
// 断的是载荷两键，不是事件名。
func TestM202PanicLegEmitsShellWithRawInDetail(t *testing.T) {
	a, rec, root := newProbeApp(t)
	var fired bool
	a.pipe.OnStage = func(model.StageEvent) {
		if !fired {
			fired = true
			panic("模拟流水线内部 panic")
		}
	}
	if _, err := a.StartScan(model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if ev := rec.waitTerminal(t, "panic 后"); ev != "scan:error" {
		t.Fatalf("panic 应发 scan:error，got %s", ev)
	}
	p, ok := rec.lastWith("scan:error")
	if !ok {
		t.Fatal("未记录到 scan:error 载荷")
	}
	payload, ok := p.(map[string]string)
	if !ok {
		t.Fatalf("载荷形状变了（约定是 map[string]string）：%T", p)
	}
	if payload["error"] != panicShell {
		t.Errorf("panic 腿外壳应为内部异常档：got %q want %q", payload["error"], panicShell)
	}
	if !strings.Contains(payload["detail"], "goroutine panic") ||
		!strings.Contains(payload["detail"], "模拟流水线内部 panic") {
		t.Errorf("panic 原句（kind + panic 值）必须整串留在 detail 里供排查：%q", payload["detail"])
	}
}

// 边界自检：nil 错误不该产出载荷（三处发射点都在 err != nil 分支，别让它有机会发出空壳）。
func TestM202NilErrorYieldsEmptyPayload(t *testing.T) {
	shell, detail := errorShell(nil)
	if shell != "" || detail != "" {
		t.Errorf("nil 错误应两值皆空：got (%q,%q)", shell, detail)
	}
	if got := errorEvent(nil); got["error"] != "" {
		t.Errorf("nil 错误不该有外壳：%q", got)
	}
}
