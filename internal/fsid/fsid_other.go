//go:build !darwin && !linux && !windows

package fsid

import "os"

// 其他 unix 变体（freebsd 等）：Stat_t 字段布局不通用，保守返回未解析。
func fromInfo(info os.FileInfo) ID { return ID{} }

// FromPathNoFollow 取 path 自身身份，不跟随链接。本平台不支持 → 未解析。
func FromPathNoFollow(path string) (ID, error) {
	if _, err := os.Lstat(path); err != nil {
		return ID{}, err
	}
	return ID{}, nil
}

// FromPath 取 path 指向对象的身份，跟随链接。本平台不支持 → 未解析。
//
// 仍做一次 Stat：让"路径不可达"（悬空链接/不存在）能如实报错，
// 调用方靠这个错误区分"目标没了"与"目标在但查不出身份"。
func FromPath(path string) (ID, error) {
	if _, err := os.Stat(path); err != nil {
		return ID{}, err
	}
	return ID{}, nil
}
