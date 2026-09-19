// FileDedup Wails v2 入口。
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// assets 前端产物（生产构建）。
// G5：此处**不能**用 all: 前缀——all: 会把 .DS_Store 等点开头文件一并编入，
// 使 macOS 的 Finder 元数据（6KB / 每个目录一份）被打进发布二进制。
// 去掉 all: 后 Go 的 embed 规则天然排除 "." 与 "_" 开头的文件，
// 构成编译期保证：无论 dist 被谁怎么污染，元数据都不可能进包。
// 注意：若将来 dist 需要正常发布点开头文件（如 .well-known），须改用其他方案。
//
//go:embed frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "FileDedup",
		Width:            1180,
		Height:           780,
		MinWidth:         920,
		MinHeight:        620,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 251, A: 255},
		DragAndDrop: &options.DragAndDrop{
			// 扫描目录拖入：原生层递送绝对路径（前端 window.runtime.OnFileDrop），
			// 比 dataTransfer File.path（WKWebView 不可靠）跨平台稳定
			EnableFileDrop: true,
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose, // 2026-09-18 审查 C5：在途清理/扫描时拦下关窗
		Bind:          []interface{}{app},
	})
	if err != nil {
		// P3：println 在无控制台附加的发布版（Windows GUI 子系统）里等于丢弃错误，
		// 用户只会看到"双击没反应"。写 stderr + 非零退出码，供快捷方式/日志排查。
		fmt.Fprintf(os.Stderr, "FileDedup 启动失败: %v\n", err)
		os.Exit(1)
	}
}
