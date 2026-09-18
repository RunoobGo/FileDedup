//go:build !darwin && !windows

package fscase

// 平台默认：Linux/BSD 等的 ext4/xfs/btrfs/UFS 默认大小写敏感。仅在按卷探测
// 不可用（只读卷、无写权限、目录不存在）时才用到这个值。
const defaultSensitive = true
