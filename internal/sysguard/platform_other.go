//go:build !windows && !darwin

package sysguard

// Current 见 platform_windows.go 的同名说明。
// 与 internal/fscase 同一口径：非 windows/darwin 一律按 Linux 清单处理。
const Current = PlatformLinux
