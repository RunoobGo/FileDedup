//go:build !windows

package ops

// S3：硬链接合并（unix；Windows 待真机 checklist L4 #1）。

import (
	"os"
	"runtime"
	"syscall"
	"testing"

	"filededup/internal/model"
)

func TestS3HardlinkMerge(t *testing.T) {
	fx := newFixture(t)
	res := Execute(Options{
		Groups:  []*model.DuplicateGroup{fx.group},
		KeepIDs: map[uint64]bool{fx.orig.ID: true},
	}, model.OpRequest{Kind: "hardlink", FileIDs: []uint64{fx.dup2.ID}})
	if len(res.OK) != 1 {
		t.Fatalf("硬链接合并失败: %+v", res)
	}
	i1, i2 := inodeOf(t, fx.orig.Path), inodeOf(t, fx.dup2.Path)
	if i1 == 0 || i1 != i2 {
		t.Fatalf("合并后 inode 不一致: %d vs %d", i1, i2)
	}
	b1, _ := os.ReadFile(fx.orig.Path)
	b2, _ := os.ReadFile(fx.dup2.Path)
	if len(b1) == 0 || string(b1) != string(b2) {
		t.Fatal("内容不一致")
	}
	if res.Reclaimed != fx.dup2.Size {
		t.Fatalf("释放空间计算错误: %d", res.Reclaimed)
	}
}

func inodeOf(t *testing.T, p string) uint64 {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if si, ok := st.Sys().(*syscall.Stat_t); ok {
		return si.Ino
	}
	t.Fatalf("平台不支持 inode 检查（%s）", runtime.GOOS)
	return 0
}
