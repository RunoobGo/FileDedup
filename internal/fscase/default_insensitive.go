//go:build darwin || windows

package fscase

// 平台默认：darwin 与 windows 的主流文件系统（APFS/HFS+ 根卷、NTFS）默认
// 大小写不敏感。仅在按卷探测不可用时才用到这个值。
const defaultSensitive = false
