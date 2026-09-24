package dedup

import (
	"testing"

	"filededup/internal/fsid"
	"filededup/internal/model"
)

// B1（R-算法-1）：阶段 1.5 的硬链接折并依赖 scanner.ResolveKey，而后者在 Windows
// 独占锁 / 删除挂起等情形下标准 open 与 C4 兜底全失败 ⇒ 返回未解析 FileKey ⇒ 同一
// inode 的两条路径双双留在候选里。它们内容逐字节相同 ⇒ 落进同一全量哈希桶 ⇒ 报成
// 一个 2 文件"重复组"，reclaimable 虚高（删硬链接兄弟并不释放空间），且清理会抹掉
// 一条路径引用。阶段 2 的句柄身份 ids[]（fsid.FromFile）是权威兜底：本测试钉住
// collapseSameIdentity 据 ids[] 把这类假组折并回一条。
//
// 在 unix 上无法经全流程复现（遍历时 keyFromInfo 必解析 inode，阶段 1.5 已折并），
// 故直接对折并helper 做单元核证——构造 Key.Resolved=false（模拟 ResolveKey 未解析）
// 但 ids 已解析且同 Dev+Ino 的两条目。
//
// ★ 双层关系（B1 Step 4 现读结论）：ops 侧的身份守卫（verify.go identityCheck /
// identityStill、merge_guard.go slotProvesHardlink / backupOwnershipStill）检测的是
// **操作中途的 TOCTOU 顶替**（某路径的 inode 在 verify 与 act 之间变了），而非"组内
// 两成员本就是同一 inode"。因此 ops 侧**没有**针对假硬链接组的熔断——本折并（分组层）
// 是阻止假组被报出（reclaimable 虚高）并被处置的**唯一**防线。二者互补不重复：即便
// 漏过本层，处置假硬链接组也不会销毁数据（删一条硬链接名另一条仍在；硬链接"合并"是
// 近似空操作的改名舞），危害止于结果集失真与用户预期落空，而这正是本层要消除的。
func TestCollapseSameIdentityUnresolvedKey(t *testing.T) {
	long := &model.FileEntry{
		ID: 1, Path: "/vol/aaa-longer-name.bin", Ext: ".bin", Size: 4096,
		Key: model.FileKey{Resolved: false}, // 阶段 1.5 ResolveKey 未解析
	}
	short := &model.FileEntry{
		ID: 2, Path: "/vol/zz.bin", Ext: ".bin", Size: 4096,
		Key: model.FileKey{Resolved: false},
	}
	sameID := fsid.ID{Dev: 100, Ino: 200, Resolved: true} // 阶段 2 句柄身份：同一物理文件

	got := collapseSameIdentity([]*model.FileEntry{long, short}, []fsid.ID{sameID, sameID})

	if len(got) != 1 {
		t.Fatalf("折并后条目数 = %d, want 1（同 inode 两路径应折并为一条，消除假重复组）", len(got))
	}
	if got[0].Path != "/vol/zz.bin" {
		t.Fatalf("保留路径 = %q, want 较短者 /vol/zz.bin（与阶段 1.5 同口径）", got[0].Path)
	}
	// C3：保留项的 ID 必须是首见项（long, ID=1），不得被丢弃项（short, ID=2）覆盖——
	// 下游按 ID 关联选中态/保留决策/预览，覆盖会让 ID 与结果集错位。
	if got[0].ID != 1 {
		t.Fatalf("C3 回归：保留项 ID = %d, want 1（首见长路径项 ID）", got[0].ID)
	}
}

// 折并必须**保守**：仅在两侧 ids 均确信解析且同 Dev+Ino 时才并。任一侧未解析
// （FAT/exFAT 等无稳定文件索引的卷）或两侧 inode 不同（内容巧合相同的独立文件）
// 都必须保持独立——否则会把物理独立的同内容文件误并成一条，造成漏报（对删除工具
// 而言比 reclaimable 虚高更危险）。这条钉住口径，防止后人误用宽松的 SameIdentity
// （其任一侧未解析即返回 true）来做分组折并。
func TestCollapseSameIdentityConservative(t *testing.T) {
	mk := func(id uint64, path string) *model.FileEntry {
		return &model.FileEntry{ID: id, Path: path, Ext: ".bin", Size: 4096, Key: model.FileKey{Resolved: false}}
	}

	t.Run("两侧未解析不折并", func(t *testing.T) {
		a, b := mk(1, "/vol/a.bin"), mk(2, "/vol/b.bin")
		unresolved := fsid.ID{Resolved: false}
		got := collapseSameIdentity([]*model.FileEntry{a, b}, []fsid.ID{unresolved, unresolved})
		if len(got) != 2 {
			t.Fatalf("未解析身份应保持独立：条目数 = %d, want 2", len(got))
		}
	})

	t.Run("一侧未解析不折并", func(t *testing.T) {
		a, b := mk(1, "/vol/a.bin"), mk(2, "/vol/b.bin")
		resolved := fsid.ID{Dev: 100, Ino: 200, Resolved: true}
		unresolved := fsid.ID{Resolved: false}
		got := collapseSameIdentity([]*model.FileEntry{a, b}, []fsid.ID{resolved, unresolved})
		if len(got) != 2 {
			t.Fatalf("一侧未解析应保持独立：条目数 = %d, want 2", len(got))
		}
	})

	t.Run("inode 不同不折并", func(t *testing.T) {
		a, b := mk(1, "/vol/a.bin"), mk(2, "/vol/b.bin")
		idA := fsid.ID{Dev: 100, Ino: 200, Resolved: true}
		idB := fsid.ID{Dev: 100, Ino: 999, Resolved: true} // 同卷不同 inode
		got := collapseSameIdentity([]*model.FileEntry{a, b}, []fsid.ID{idA, idB})
		if len(got) != 2 {
			t.Fatalf("不同 inode 是独立文件，应保持独立：条目数 = %d, want 2", len(got))
		}
	})

	t.Run("ctime 不同仍折并", func(t *testing.T) {
		// 同 Dev+Ino 即同一物理文件；chmod/xattr 会推进 ctime 但文件未被替换，
		// 折并判据不含 ctime（与 fsid.SameIdentity 同口径）。
		a, b := mk(1, "/vol/aaa.bin"), mk(2, "/vol/z.bin")
		idA := fsid.ID{Dev: 100, Ino: 200, CtimeNs: 1, Resolved: true}
		idB := fsid.ID{Dev: 100, Ino: 200, CtimeNs: 2, Resolved: true}
		got := collapseSameIdentity([]*model.FileEntry{a, b}, []fsid.ID{idA, idB})
		if len(got) != 1 {
			t.Fatalf("同 Dev+Ino 应折并（忽略 ctime）：条目数 = %d, want 1", len(got))
		}
	})
}
