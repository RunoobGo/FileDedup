//go:build darwin || linux

package realbytes

// M27 追补（设计稿 §13.6）：`Reported` 这个平台读出口自身的钉子。
//
// 为什么需要它：稀疏系用例（scanner 的 U2/V5/V6/V7、dedup 的 U4）**前提自检**都走
// `realbytes.Reported`——前提要问的正是"平台报了什么"，而这个函数就是平台读出口
// 本身。于是它一旦被改坏，前提会把"读出口坏了"读成"本卷不支持稀疏" ⇒ 那些断言
// 用例全部降级 skip、全仓零红（M-M27-a 实测：新增 5 条 skip、0 条红）。
// 前提读环境必须**独立于被测物**（与同目录 VolumeID 的 rawDev 同一纪律），
// 所以这里用 raw syscall 直接读 st_blocks 与 Reported 对照——它不设任何
// "本卷支不支持稀疏"的门槛，读数不符就是红。

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// readoutProbeSize 64 MiB + 尾部 1 KiB：与 scanner 的 sparseSize 同量级——
// 稀疏与否按数量级可分辨（真稀疏 ≈ 8 个 512 块，逻辑是 131072 个）。
const readoutProbeSize = 64 << 20

// TestReportedMatchesRawStatBlocks 钉 `Reported(path, info)` 与 raw st_blocks×512
// 一字不差。夹具用 WriteAt 而非 seek 后灌零：后者会把洞填实，夹具就不再是稀疏的
// （同 scanner mkSparseFile 的注释）。尾部写保证文件非全洞——全洞落在
// "报 0 该不该采信"的判定分支上，那是 From 的事，不是读出口的事。
func TestReportedMatchesRawStatBlocks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sparse.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(readoutProbeSize); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("READOUT-MARKER"), readoutProbeSize-1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("拿不到 Stat_t：本用例的对照读数不可得")
	}
	raw := uint64(st.Blocks) * BlockSize
	if raw*4 >= readoutProbeSize {
		t.Skipf("本卷把稀疏文件落地分配（raw 报 %d B）：对照夹具不成立", raw)
	}

	rep, ok := Reported(p, fi)
	if !ok {
		t.Fatalf("Reported 报读不到（ok=false），但 raw st_blocks 躺着 %d：unix 读出口不该 fail-closed", raw)
	}
	if rep != raw {
		t.Errorf("Reported = %d, want %d（raw st_blocks×%d）：读出口与平台原始读数不一致——"+
			"所有以它为前提自检的用例会集体降级为 skip，而不是红", rep, raw, BlockSize)
	}
}
