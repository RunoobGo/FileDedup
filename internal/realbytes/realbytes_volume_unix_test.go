//go:build darwin || linux

package realbytes

// M28（设计稿 §11.3 V4）：卷标识是"证据只在本卷内成立"这一不变式的物理依据，
// 因此它至少要在真机上钉住两条关系：同卷恒同值、不同挂载实例给不同的值。
// 断言不依赖具体数值（st_dev 的数值各机器不同），只依赖关系。

import (
	"os"
	"path/filepath"
	"strings"
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

// rawDevQuiet 与 rawDev 同一个独立读法，但不致命：候选列表里允许有本机不存在的挂载点。
func rawDevQuiet(p string) (uint64, bool) {
	var st syscall.Stat_t
	if err := syscall.Stat(p, &st); err != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

// crossVolumeCandidates 跨卷对照的候选挂载点（G9，设计稿 §14.2）。
//
// 为什么要列表而不是只押一个：原先只押 `/dev/null`，darwin 上是 devfs（与临时目录
// 必不同 dev，本机实测），但 Linux 上 `/dev` 常常就是挂在 `/` 之下的 devtmpfs、
// 与根同 dev——那种环境里唯一对照失效，整条判据退成 skip，CI 上等于没有证据。
// 逐个试到第一个"dev 真不同"的为止；全试完仍无对照才 skip（与 requireTailSparse
// 同款纪律：环境不满足在使用点自探，skip 不算通过）。
var crossVolumeCandidates = []string{
	"/dev/null",            // darwin devfs / linux 通常在 devtmpfs
	"/dev/shm",             // linux tmpfs
	"/dev",                 // linux devtmpfs / darwin devfs
	"/run",                 // linux tmpfs
	"/tmp",                 // 独立 tmpfs 的机器（含容器）
	"/proc",                // linux procfs：伪文件系统，dev 与磁盘卷必不同
	"/System/Volumes/Data", // darwin 数据卷：与只读系统卷 / 不同 dev
}

func TestVolumeIDDistinguishesMounts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawA := rawDev(t, p)

	var cand string
	var rawB uint64
	var tried []string
	found := false
	for _, c := range crossVolumeCandidates {
		d, ok := rawDevQuiet(c)
		if !ok {
			continue // 本机没有这个挂载点，不算"试过了"
		}
		tried = append(tried, c)
		if d == rawA {
			continue // 同卷，不构成对照
		}
		cand, rawB, found = c, d, true
		break
	}
	if !found {
		t.Skipf("候选挂载点全部与临时目录同卷（试：%s，临时目录 dev=%d），跨卷夹具不成立",
			strings.Join(tried, " "), rawA)
	}
	va, _ := VolumeID(p, statOf(t, p))
	vb, _ := VolumeID(cand, statOf(t, cand))
	if va == vb {
		t.Errorf("两个不同挂载实例（raw dev %d/%d，对照 %s）给了同一个 VolumeID %d：证据会跨卷池化",
			rawA, rawB, cand, va)
	}
	// 对照卷打进日志：划账要抄"这台机器用的是哪个对照"，不能只写"通过"。
	t.Logf("跨卷对照成立：%s（dev=%d）vs 临时目录（dev=%d）→ VolumeID %d vs %d",
		cand, rawB, rawA, va, vb)
}
