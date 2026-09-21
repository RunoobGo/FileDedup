//go:build !windows

package ads

// enumerateStreams：非 Windows 平台上**没有** ADS 语义，恒报"枚举成功但没有流"。
//
// 明确写出来，是因为"本平台无此语义"与"有语义但没实现"是两件事：
//
//   - macOS 的资源分叉：HFS 时代的 `._` AppleDouble 是**独立文件**（会被当普通文件
//     参与去重，那是另一个问题），现代 APFS 走 xattr 而不是"文件内的第二条流"；
//   - Linux 的 xattr：随 inode 走，硬链接共享、删除才消失——语义与 NTFS"命名流随
//     文件记录一起被替换"根本不同，硬套一条判据只会把普通文件拦下来。
//
// 所以这里不猜（与 internal/cloudfile/cloudfile_other.go 的 Linux 处置同向）。
// 这条路径不是死代码：执行器无条件调 ads.Check，Linux 用户的每次清理都会走到这里；
// ads_other_test.go 的 V9 钉住它必须放行。
func enumerateStreams(path string) ([]string, uintptr) {
	return nil, 0
}
