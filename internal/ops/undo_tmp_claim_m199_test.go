package ops

// M199（OPS-27，登记 §6.29，设计稿
// `specs/2026-09-24-undo-tmp-claim-slot-m199-design.md`）：
// undoHardlink 的暂存名 `原名.fdd-undo-tmp` 按可预测规则拼出、动手前**无条件盲删**
// （`_ = os.Remove(tmp)`，错误被吞）——第三方恰好持有这个名字的普通文件会先被删掉，
// 而该后缀在 worktemp 忽略清单里，被吞后连扫描痕迹都没有。
// 合并/软链腿的四处同形槽位（move.go ×2、symlink.go ×2）在 M6 已改 `claimSlot`
// 三档"先取证再处置"，回撤腿是最后一处漏改。
//
// 三条用例的取证定位（AS-K2 话术，改前真读数见 §6.32）：
//   P-1 第三方文件占位 → 显式失败 + 一字不碰（修前两格全红：被吞 + 回撤"成功"）；
//   P-2 撕裂残留（尺寸小于记录）占位 → 同 P-1（证明不了归属就不删；修前红）；
//   P-3 完整残留（内容==记录哈希）占位 → 取证放行、回撤照常成功（**改前改后都绿**，
//       身份是护栏：防修法把"自己崩溃残留的正当自愈"修坏，不冒领红探针）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lukechampine.com/blake3"
)

// m199HardlinkFixture 造一条"dup 是指向 keep 的硬链接"的回撤现场，
// 返回 (dir, keep, dup, undoItem)。文件系统不支持硬链接时 Skip（与全仓同口径）。
func m199HardlinkFixture(t *testing.T, payload []byte) (dir, keep, dup string, it UndoItem) {
	t.Helper()
	dir = t.TempDir()
	keep = filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(keep, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dup = filepath.Join(dir, "dup.bin")
	if err := os.Link(keep, dup); err != nil {
		t.Skipf("本文件系统不支持硬链接: %v", err)
	}
	it = UndoItem{Kind: "hardlink", OrigPath: dup, LinkSrc: keep,
		Hash: blake3.Sum256(payload), Size: uint64(len(payload))}
	return dir, keep, dup, it
}

// assertFileUntouched 钉"占位文件存在且内容与字节都属于第三方"。
func assertFileUntouched(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	switch {
	case err != nil:
		t.Errorf("占位文件 %s 消失或被改动（读取失败: %v）⇒ 盲删回来了（M199）", path, err)
	case string(got) != string(want):
		t.Errorf("占位文件内容被改写: %q, want %q", got, want)
	}
}

func TestM199UndoHardlinkDoesNotSwallowForeignTmp(t *testing.T) {
	payload := []byte("M199-ORIGINAL-CONTENT")
	_, _, dup, it := m199HardlinkFixture(t, payload)

	tmp := dup + FddUndoSuffix
	foreign := []byte("FOREIGN-BYTES-BY-SOMEONE-ELSE")
	if err := os.WriteFile(tmp, foreign, 0o644); err != nil {
		t.Fatal(err)
	}

	restored, err := undoHardlink(it)
	// 格 1：必须显式失败（改前：盲删掉第三方文件后回撤"成功" ⇒ 此格红）。
	if err == nil {
		t.Errorf("tmp 槽位被第三方文件占着，回撤却报成功（落点 %q）⇒ 抢位未做归属取证（M199）", restored)
	} else if !strings.Contains(err.Error(), tmp) {
		t.Errorf("失败文案必须点名被占的槽位路径，让用户知道要处置哪个文件；实际: %v", err)
	}
	// 格 2：第三方文件一字不碰（改前：文件已被 os.Remove 吞掉 ⇒ 此格红）。
	assertFileUntouched(t, tmp, foreign)
}

func TestM199UndoHardlinkDoesNotSwallowTornResidue(t *testing.T) {
	payload := []byte("M199-ORIGINAL-CONTENT-ABCDEFGHIJ")
	_, _, dup, it := m199HardlinkFixture(t, payload)

	// 上一次 copyAndSync 中途被杀的形态：同前缀名、内容只是记录的前缀、尺寸必小于记录。
	tmp := dup + FddUndoSuffix
	torn := payload[:len(payload)/2]
	if err := os.WriteFile(tmp, torn, 0o644); err != nil {
		t.Fatal(err)
	}

	restored, err := undoHardlink(it)
	if err == nil {
		t.Errorf("撕裂残留无法证明归属，回撤仍须显式失败而不是覆盖重来（落点 %q）（M199）", restored)
	}
	assertFileUntouched(t, tmp, torn)
}

func TestM199UndoHardlinkReclaimsOwnCompleteResidue(t *testing.T) {
	payload := []byte("M199-ORIGINAL-CONTENT")
	_, _, dup, it := m199HardlinkFixture(t, payload)

	// 上一次运行 copy 完成、rename 前崩溃的形态：tmp 内容逐字节等于记录内容。
	tmp := dup + FddUndoSuffix
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	restored, err := undoHardlink(it)
	if err != nil {
		t.Fatalf("完整残留（内容==记录哈希）应被取证放行、回撤自愈: %v", err)
	}
	if restored != dup {
		t.Errorf("落点 = %q, want %q", restored, dup)
	}
	if got, rerr := os.ReadFile(dup); rerr != nil || string(got) != string(payload) {
		t.Errorf("OrigPath 未恢复出记录内容: %q err=%v", got, rerr)
	}
	if _, serr := os.Stat(tmp); serr == nil {
		t.Errorf("回撤成功后 tmp 占位应被改名消化，仍在原地说明流程没走完整")
	}
}
