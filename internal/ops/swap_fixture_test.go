package ops

// 「顶替夹具」的唯一实现（I5）：把某个路径位置换成另一个对象。
//
// 为什么必须是"先写旁边、再 rename 顶位"，而不是看起来更直白的
// `os.Remove(path)` + `os.WriteFile(path, ...)`：
//
// 后者让原对象 unlink 到零链接，inode 号进空闲表，紧随其后创建的新对象
// **可以拿到同一个号**（2026-09-22 CI 实测：GitHub ubuntu runner 的 /tmp 就这样，
// 11 条 internal/ops 用例同时红；设计稿 §21.1）。而 `(dev,ino)` 只能区分
// **同时存活**的对象，那种情形身份层原理上识破不了——把守卫换成任何
// (dev,ino) 实现都一样绿不了。
//
// rename 进来的对象带着它在"原对象尚存活时"就已分配的号，两个号必然不同
// （号只在对象存活期间唯一）。于是这些用例真正检验的是它们声称要检验的那道
// 守卫，而不是文件系统的还号策略；且"原子改名顶位"正是这些用例头部写明的
// 现实时序（同步盘落子、下载器把临时文件改名进来，都没有名字消失的空档）。
//
// 残留缺口不遮掩，已登记 M91（§21.2）：第三方"先删后建"在会还号的卷上仍可
// 骗过 (dev,ino)，闭合它需要内容级兜底或硬链接锚点，属裁定项。

import (
	"os"
	"testing"

	"filededup/internal/fsid"
)

// intruderSuffix 只是夹具的中间名，改名完成后不残留。刻意避开 FddTempSuffix /
// FddOldSuffix，免得与合并流程自己要认领的槽位撞名。
const intruderSuffix = ".fdd-intruder"

// swapInAt 把 data 作为一个**新对象**原子顶替到 path 上，返回顶替者的身份。
//
// before 必须是 path 上原对象的身份（由调用方在顶替前取好）；本函数末尾会
// 自检"顶替者是否真的带着另一个号"——自检独立于被测物（不用 identityStill），
// 失败时报的是"夹具造不出可区分的顶替"，而不是"守卫没生效"，免得两种红同形。
func swapInAt(t *testing.T, path string, before fsid.ID, data []byte) fsid.ID {
	t.Helper()
	tmp := path + intruderSuffix
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("铺第三方文件失败: %v", err)
	}
	after, err := fsid.FromPathNoFollow(tmp)
	if err != nil {
		t.Fatalf("取第三方文件身份失败: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("顶替改名失败: %v", err)
	}
	assertDistinctIdentity(t, path, before, after)
	return after
}

// swapOutAt 与 swapInAt 同一件事，只是"原对象的身份"由本函数就地从 path 取
// （调用方手里没有 dupID 的场合）。
func swapOutAt(t *testing.T, path string, data []byte) fsid.ID {
	t.Helper()
	prev, err := fsid.FromPathNoFollow(path)
	if err != nil {
		t.Fatalf("取原对象身份失败: %v", err)
	}
	return swapInAt(t, path, prev, data)
}

// swapSymlinkOnto 把"指向 target 的符号链接"作为新对象原子顶替到 path 上。
// 链接自身的号与 path 上原对象的号必然不同（同样靠"先建在旁边"保证）。
func swapSymlinkOnto(t *testing.T, path, target string, before fsid.ID) {
	t.Helper()
	tmp := path + intruderSuffix
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		t.Skipf("符号链接创建失败（平台限制）: %v", err)
	}
	after, err := fsid.FromPathNoFollow(tmp)
	if err != nil {
		t.Fatalf("取链接身份失败: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("顶替改名失败: %v", err)
	}
	assertDistinctIdentity(t, path, before, after)
}

// assertDistinctIdentity 钉住夹具前提：顶替者必须与原对象不同号。
func assertDistinctIdentity(t *testing.T, path string, before, after fsid.ID) {
	t.Helper()
	if !before.Resolved || !after.Resolved {
		return // 卷不给稳定索引时前提无从自检，交由用例各自的 Resolved 前置处理
	}
	if before.Dev == after.Dev && before.Ino == after.Ino {
		t.Fatalf("夹具前提不成立：%s 上的顶替者拿到了原对象的 (dev=%x, ino=%d)，"+
			"本卷会回收 inode 号，(dev,ino) 身份层原理上识破不了这一情形（见 M91）",
			path, after.Dev, after.Ino)
	}
}
