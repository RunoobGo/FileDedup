package main

// M196（登记表 APP-23）：OS 级单实例保护 + 第二实例聚焦已有窗口。
//
// 危害面（登记行原话）：`opsRunning` 互斥门、写前账本、XDG guard 全是**进程内**防线，
// 两个实例同时把清理打在重叠文件集上时两边各自都"正确"，合起来就是重复移动/重复删除。
// 所以本文件的判据不是"有没有把窗口带到前面"（那格真机才可证，见 §6.36 的"未兑现"清单），
// 而是**装配真的接上了、两个前置原语真的都被调、事件真的按原样交出去**。
//
// 设计段：docs/superpowers/specs/2026-09-24-single-instance-m196-design.md
// ★ 其中 §1.3 那张表是本文件的承重墙：v2.16 公开面无 focus/bring-to-front API，
//   "前置"这件事在三条腿上由**不同**原语提供——linux 只有 WindowUnminimise 走
//   gtk_window_present，darwin/windows 只有 WindowShow 走 makeKeyAndOrderFront /
//   SetForegroundWindow。漏调任一个，就有一条腿"第二实例发来了信号、窗口却没到前面"。
//   P-2 因此钉的是"两个都被调 + 顺序"，不是"调了某个 raise"。

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

// seqLog 把"原生原语"与"事件出口"记进**同一条**序列，于是 raise 与 emit 的相对顺序
// 是可断言的事实而不是注释。包级替换而不给每个 App 加接缝：要测的正是 raiseMainWindow
// **真身**的调用集合与顺序，换成 App 字段就只能测"回调调了字段"，测不到本体漏没漏
// 一个原语（变异 Vb 那一格）。
type seqLog struct {
	mu       sync.Mutex
	steps    []string
	payloads []map[string]any
}

func (s *seqLog) add(step string) {
	s.mu.Lock()
	s.steps = append(s.steps, step)
	s.mu.Unlock()
}

func (s *seqLog) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// swapWindowPrimitives 换上记录器，t.Cleanup 还原（变异跑完后必须能干净还原）。
func swapWindowPrimitives(t *testing.T, s *seqLog) {
	t.Helper()
	origUn, origShow := windowUnminimise, windowShow
	windowUnminimise = func(context.Context) { s.add("unminimise") }
	windowShow = func(context.Context) { s.add("show") }
	t.Cleanup(func() { windowUnminimise, windowShow = origUn, origShow })
}

func newSeqApp(t *testing.T, s *seqLog) *App {
	t.Helper()
	a := NewApp()
	a.ctx = context.Background() // emit 已替换，不再要求 Wails 内部 context
	a.emit = func(_ context.Context, name string, args ...interface{}) {
		s.add("emit:" + name)
		if len(args) > 0 {
			if m, ok := args[0].(map[string]any); ok {
				s.mu.Lock()
				s.payloads = append(s.payloads, m)
				s.mu.Unlock()
			}
		}
	}
	return a
}

// P-1 装配契约：开关真的接在 wails.Run 的入参上。
// 这条专杀"实现了回调却忘了接线"——本批最像 bug 的失败模式，
// 且一旦漏接，其余三条用例全都会绿（它们测的是回调本体，不碰装配）。
func TestM196SingleInstanceWiredIntoAppOptions(t *testing.T) {
	a := NewApp()
	opts := buildAppOptions(a)

	sil := opts.SingleInstanceLock
	if sil == nil {
		t.Fatal("options.App.SingleInstanceLock 为 nil ⇒ 双开两个实例照样各起各的，M196 一格都没修上")
	}
	if sil.UniqueId != singleInstanceUniqueID {
		t.Errorf("UniqueId=%q 与常量 %q 不符 ⇒ 锁的标识漂移了（三腿各自拿它拼 dbus 名/锁文件名/互斥体名）",
			sil.UniqueId, singleInstanceUniqueID)
	}
	if sil.OnSecondInstanceLaunch == nil {
		t.Fatal("回调为 nil ⇒ 第二实例会静默退出且现窗口毫无反应，用户只看到'双击没反应'")
	}

	// 回调**得是指向本 App 的那一个**：只断"非 nil"的话，接成别人的函数也照样绿。
	var s seqLog
	swapWindowPrimitives(t, &s)
	a.ctx = context.Background()
	a.emit = func(context.Context, string, ...interface{}) {}
	sil.OnSecondInstanceLaunch(options.SecondInstanceData{
		Args:             []string{"--from-second-instance"},
		WorkingDirectory: "/tmp/whatever",
	})
	if got := s.snapshot(); len(got) == 0 {
		t.Fatal("接上的回调没有触发窗口前置 ⇒ 它不是 a.onSecondInstance，或内部把 raise 丢了")
	}
}

// P-2 两个前置原语都被调、且 Unminimise 在前（§1.3 那张表的唯一机器可查证据）。
func TestM196RaiseCallsBothPrimitivesInOrder(t *testing.T) {
	var s seqLog
	swapWindowPrimitives(t, &s)

	raiseMainWindow(context.Background())

	got := s.snapshot()
	if len(got) != 2 || got[0] != "unminimise" || got[1] != "show" {
		t.Errorf("前置必须是 [unminimise show]，实得 %v ⇒ 少一个就有一条腿带不起窗口："+
			"linux 的前置只来自 Unminimise(gtk_window_present)，darwin/windows 的只来自 Show", got)
	}
}

// P-3 ctx 尚未就绪时不得崩、不得半做。
// 这不是假想的格子：linux 腿 Wails 先起 OnStartup 协程（frontend.go:295）
// 再 SetupSingleInstance（:301），回调因此可能早于 a.ctx 赋值到达；
// 而 wruntime 的 WindowShow/EventsEmit 在拿不到前端时走的是 **log.Fatalf**
// （pkg/runtime/runtime.go:16-20,25-27）⇒ 没守卫就是现窗口被自己的处理器带走。
func TestM196CallbackWithoutContextIsNoOp(t *testing.T) {
	a := NewApp() // ctx 保持零值
	var s seqLog
	a.emit = func(_ context.Context, name string, _ ...interface{}) { s.add("emit:" + name) }
	swapWindowPrimitives(t, &s)

	a.onSecondInstance(options.SecondInstanceData{Args: []string{"x"}, WorkingDirectory: "/y"})

	if got := s.snapshot(); len(got) != 0 {
		t.Errorf("ctx 未就绪却动了窗口或事件，实得 %v ⇒ 窗口原语那两下会撞 wruntime 的 log.Fatalf", got)
	}
}

// P-4 载荷原样交回 + 顺序 + 连发两次各自成立（第二实例可能连着开）。
func TestM196SecondInstancePayloadPassedThrough(t *testing.T) {
	var s seqLog
	a := newSeqApp(t, &s)
	swapWindowPrimitives(t, &s) // ★ 不换就直接打真 wruntime：普通 ctx 下它是 log.Fatalf，测试进程会被带走

	first := options.SecondInstanceData{Args: []string{"--a", "--b"}, WorkingDirectory: "/tmp/one"}
	a.onSecondInstance(first)
	a.onSecondInstance(first)

	got := s.snapshot()
	want := []string{"unminimise", "show", "emit:app:second-instance", "unminimise", "show", "emit:app:second-instance"}
	if len(got) != len(want) {
		t.Fatalf("应记到 %d 步 %v，实得 %v", len(want), want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 步应是 %s（raise 必须在 emit 之前：窗口先到位，提示才有落点），实得 %v",
				i, want[i], got)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.payloads) != 2 {
		t.Fatalf("两条事件都该带载荷，实得 %d", len(s.payloads))
	}
	payload := s.payloads[0]
	args, _ := payload["args"].([]string)
	if len(args) != 2 || args[0] != "--a" || args[1] != "--b" {
		t.Errorf("args 未原样带出，实得 %v", payload["args"])
	}
	if wd, _ := payload["workingDirectory"].(string); wd != "/tmp/one" {
		t.Errorf("workingDirectory 未原样带出，实得 %v", payload["workingDirectory"])
	}
}

// P-6 标识形状：三腿各自把 singleInstanceUniqueID 拼进**文件名 / D-Bus 名 / 内核对象名**，
// 这三处的合法字符集不一样，而 P-1 只比"等于常量"——常量本身写成非法形状时它无从分辨。
// 变异 Ve（把 ID 改成 "FileDedup/sub"）实测正是这样活下来的，本格是它的补账。
//
// 判据取自三腿的拼接式（设计段 §1.2 坐标）：
//
//	darwin  single_instance.go:27-28  → 文件名 `<id>.lock` ⇒ '/' 直接变成"写到别的目录去"，
//	        前导点会让它落进隐藏段，空串则锁到目录本身；
//	linux   single_instance.go:23-26  → `org.wails_app_<id>.SingleInstance`（'-'/'.' 被换成 '_'）
//	        ⇒ dbus 名要求每段是 [A-Za-z_][A-Za-z0-9_]*，空格与 '/' 都不行；
//	windows single_instance.go:38-42  → `wails-app-<id>sim` 等内核对象名 ⇒ 不能含 '\\' 与 '/'。
//
// 取三条的**交集**，只断这一条交集，不多禁（免得把合法的改名空间钉死）。
func TestM196UniqueIDShapeLegalOnAllThreeLegs(t *testing.T) {
	id := singleInstanceUniqueID
	if id == "" {
		t.Fatal("UniqueId 为空 ⇒ darwin 的锁文件变成 `.lock`（锁到目录），三腿标识同时失效")
	}
	for _, bad := range []string{"/", "\\", " ", "\t", "\n", ":", ";", "*", "?"} {
		if strings.Contains(id, bad) {
			t.Errorf("UniqueId %q 含 %q ⇒ 至少一条腿的锁标识会失效或漂到别处", id, bad)
		}
	}
	if strings.HasPrefix(id, ".") {
		t.Errorf("UniqueId %q 以前导点开头 ⇒ darwin 侧锁文件落进隐藏段、linux 侧 dbus 段非法", id)
	}
}
