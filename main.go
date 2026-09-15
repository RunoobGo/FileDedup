// FileDedup Wails v2 入口。
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
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
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
