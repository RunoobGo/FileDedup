package main

// R-根包-1（2026-09-24 审查五轮 F 批，J-4=(b) 随批修）：a.ctx 是裸字段，写点在
// startup()（app.go），读点之一是 onSecondInstance()（app_single_instance.go）。
// linux 腿 Wails 先起 OnStartup 协程再 SetupSingleInstance（见 app_single_instance.go
// 文件头 §1.3），第二实例回调可能早于/并发于 a.ctx 赋值到达 ⇒ 一写一读无同步 = 数据竞争。
// 修法：读写都经 a.mu（写走 setCtx，读在 onSecondInstance 内快照）。
//
// 本用例靠 `go test -race` 判定：竞争存在时 race detector 会让测试二进制失败。
// 复用 m196 测试的 swapWindowPrimitives，避免 onSecondInstance 打到真 wruntime（log.Fatalf）。

import (
	"context"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestAppCtxReadWriteIsSynchronized(t *testing.T) {
	a := NewApp()
	var s seqLog
	swapWindowPrimitives(t, &s)
	a.emit = func(context.Context, string, ...interface{}) {}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			a.setCtx(context.Background())
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			a.onSecondInstance(options.SecondInstanceData{Args: []string{"x"}, WorkingDirectory: "/y"})
		}
	}()
	wg.Wait()
}
