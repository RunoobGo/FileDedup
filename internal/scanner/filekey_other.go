//go:build !darwin && !linux && !windows

package scanner

import (
	"os"

	"filededup/internal/model"
)

// filekey 的其他平台腿（freebsd 等 unix 变体）：保守交回「未解析」。
//
// M301（取向：统一成兜底式，兜底腿显式 fail-closed 并带注释，别把归因推给编译器）。
// 改前本包只有 `filekey_windows.go`（//go:build windows）与 `filekey_unix.go`
// （//go:build darwin || linux）**两份枚举**，而无 tag 的 `scanner.go:566` 直接调用
// `keyFromInfo`、无 tag 的 `dedup/pipeline.go:438` 直接调用 `scanner.ResolveKey`
// ⇒ 第四平台上 `GOOS=freebsd GOARCH=amd64 go build ./internal/...` 交回
// `internal\scanner\scanner.go:566:14: undefined: keyFromInfo`。本腿补上这条缺失的腿。
//
// 取向 = 未解析，**不是**"猜一个 key"，也不是报错中断扫描：
//   - 与 windows 扫描期的既有形状同形（`filekey_windows.go` 的 `keyFromInfo` 同样
//     返回未解析，真正的身份按需到 `ResolveKey` 里查）⇒ 本平台的差异只是"按需查
//     也没有可靠接口"，不改变上层判据；
//   - pipeline 的阶段 1.5 只在 `k.Resolved` 为真时才用 key 去重（见
//     `dedup/pipeline.go` 的 `if k.Resolved`），所以未解析的后果是**认不出硬链接**
//     ——重复计数会虚高（删硬链接兄弟不实际释放空间），属"报多不报少"的一侧；
//   - 反过来，若在零信息下擅自把已解析标志置真，`seen[k]` 会把所有文件收进同一个
//     键 ⇒ 不同文件被判成硬链接、只保留一个、其余进回收站。那是误删，不是精度损失。
//     所以本文件**任何位置（注释也算）**都不许写出那个"置真"的代码字面量——
//     `filekey_other_m301_test.go` 的反面断言 grep 的是文件原文，注释里提它也会红
//     （M309 现场验证过一次：把字面量写进本注释，scanner 包当场红），
//     因此这里只能用中文指称它。
//
// 将来要支持：另开一条 `//go:build freebsd` 的枚举腿，从该平台的 Stat_t 取
// dev/ino（参照 `filekey_unix.go`），或接平台原生的文件身份接口。
func keyFromInfo(path string, info os.FileInfo) model.FileKey {
	return model.FileKey{}
}

// ResolveKey 其他平台：没有可靠的身份来源，原样交回（保持未解析）。
func ResolveKey(e *model.FileEntry) model.FileKey {
	return e.Key
}
