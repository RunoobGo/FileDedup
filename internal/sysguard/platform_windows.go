//go:build windows

package sysguard

// Current 本包唯一按 GOOS 分流的地方：保护清单的平台位由此注入判据，
// 其余全部逻辑无 build tag（可在 Linux 主门禁上断言 Windows 规则）。
const Current = PlatformWindows
