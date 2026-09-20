//go:build darwin || linux

package fsid

import (
	"os"
	"syscall"
)

func fromInfo(info os.FileInfo) ID {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ID{}
	}
	return ID{
		Dev:      uint64(st.Dev),
		Ino:      uint64(st.Ino),
		CtimeNs:  ctimeNs(st),
		Resolved: true,
	}
}

// FromPathNoFollow 取 path **自身**的物理身份，不跟随符号链接。
//
// unix 上 lstat 的结果就带 (dev, ino)，直接可用——与 Windows 侧行为等价，
// 调用方无需分平台。
func FromPathNoFollow(path string) (ID, error) {
	li, err := os.Lstat(path)
	if err != nil {
		return ID{}, err
	}
	return fromInfo(li), nil
}

// FromPath 取 path **所指向对象**的物理身份，跟随符号链接。
//
// 与 FromPathNoFollow 成对存在，二者用途严格区分：
//   - FromPathNoFollow：想知道"这个路径位置上的对象本身是谁"（识别替换、比对
//     硬链接同族），链接不被穿透；
//   - FromPath：想知道"按这个路径打开会读到谁的数据"（软链接终局复核、悬空
//     检测），链接被穿透。
//
// 2026-09-20（跨卷软链接合并）：软链接合并的终局复核必须用后者——软链接自身
// 是与目标毫无关系的独立对象，拿它自己的 (dev, ino) 去和保留源比永远不等，
// 会把每一次成功的合并都判成失败（详见 internal/ops/symlink.go:verifySymlinked）。
//
// 悬空链接（目标已不存在）：unix 上 stat 返回 ENOENT，如实返回 error；
// Windows 上 CreateFileW 同样失败。调用方需把"打不开"与"打开了但身份未解析"
// 分开处置（后者仍是有效结果，只是该卷不提供稳定索引）。
func FromPath(path string) (ID, error) {
	st, err := os.Stat(path)
	if err != nil {
		return ID{}, err
	}
	return fromInfo(st), nil
}
