//go:build darwin || linux

package realbytes

// M28（设计稿 §11.3 V4）：卷标识是"证据只在本卷内成立"这一不变式的物理依据，
// 因此它至少要在真机上钉住两条关系：同卷恒同值、不同挂载实例给不同的值。
// 断言不依赖具体数值（st_dev 的数值各机器不同），只依赖关系。

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func statOf(t *testing.T, p string) os.FileInfo {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return st
}

// rawDev 独立读真值：不经过被测函数，避免"用被测物验证被测物"。
func rawDev(t *testing.T, p string) uint64 {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(p, &st); err != nil {
		t.Fatalf("syscall.Stat %s: %v", p, err)
	}
	return uint64(st.Dev)
}

func TestVolumeIDSameDirSameID(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ia, oka := VolumeID(a, statOf(t, a))
	ib, okb := VolumeID(b, statOf(t, b))
	if !oka || !okb {
		t.Fatalf("VolumeID 报拿不到卷标识（%v/%v）：unix 上 st_dev 恒在，不该走 fail-closed", oka, okb)
	}
	if ia != ib {
		t.Errorf("同一目录两个文件的 VolumeID = %d/%d, want 相等", ia, ib)
	}
}

func TestVolumeIDDistinguishesMounts(t *testing.T) {
	// 跨卷对照用 /dev/null（devfs）：本机实测它与临时目录不同 dev（设计稿 E9）。
	// Linux CI 上 /dev 可能与 / 同卷——那种环境里夹具不成立，skip 而不是红
	// （与 requireTailSparse 同款：环境不满足在使用点自探）。
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawA := rawDev(t, p)
	rawB := rawDev(t, "/dev/null")
	if rawA == rawB {
		t.Skipf("本环境 /dev/null 与临时目录同卷（dev=%d），跨卷夹具不成立", rawA)
	}
	va, _ := VolumeID(p, statOf(t, p))
	vb, _ := VolumeID("/dev/null", statOf(t, "/dev/null"))
	if va == vb {
		t.Errorf("两个不同挂载实例（raw dev %d/%d）给了同一个 VolumeID %d：证据会跨卷池化",
			rawA, rawB, va)
	}
}
