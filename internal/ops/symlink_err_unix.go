//go:build !windows

package ops

import "errors"

// ErrSymlinkNeedsPrivilege 表示"环境不具备创建符号链接的权限"。
//
// 定义在**非 Windows** 侧（而不是公共文件里）是为了让 Windows 侧能保留
// 完整的错误映射说明；unix 上创建符号链接没有任何特权要求，此哨兵不会被
// 任何实现命中，仅作为跨平台存在的类型供调用方 errors.Is 使用。
var ErrSymlinkNeedsPrivilege = errors.New("创建符号链接需要权限")
