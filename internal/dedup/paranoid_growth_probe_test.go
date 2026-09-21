package dedup

// 2026-09-21 全量审查 PARA-1 / HASH-1（设计稿 §15.0-A）。
//
// paranoid 逐字节比对是"假重复"的**最后一道**防线，但 equal 原先只比
// 声明 size 个字节就 return true——声明 size 来自更早的 stat，比对期间
// 文件被追加时，前 size 字节相同即判"逐字节一致"，这道防线与它本应兜住的
// 预筛层犯同一个错（切尾）。
//
// 另一条更硬的：size 为负（坏卷 / 畸形 FUSE-SMB 挂载报出的 st_size）时
// `remain > 0` 一次都不成立，函数**一个字节都不读就返回 true** ⇒ 任意两个
// 文件都被判成一致，paranoid 反而成了唯一的放行口。
//
// 两条都要求在 return true 之前有"声明长度之后确实没有内容"的证据。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

func writeBytes(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// PARA-1 主探针：前缀相同、尾部被追加 ⇒ 必须判不一致（或报错），不得放行。
func TestVerifyEqualRejectsGrowthBeyondDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	head := strings.Repeat("A", 300*1024) // 跨过 verifyBufSize，走多块循环
	a := writeBytes(t, dir, "a.bin", []byte(head))
	// b 的前 300KiB 与 a 逐字节相同，之后多出内容：声明 size 取 a 的长度，
	// 这正是"stat 之后、比对之前/之中被追加"的形状。
	b := writeBytes(t, dir, "b.bin", []byte(head+"APPENDED-AFTER-THE-STAT"))

	fa, err := os.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()

	same, err := newVerifier().equal(context.Background(), fa, fb, int64(len(head)))
	if err == nil && same {
		t.Fatalf("变长守卫缺位：声明 %d 字节、b 实际 %d 字节可读，equal 仍给出「逐字节一致」结论"+
			"⇒ paranoid 这道最后防线与预筛层犯同一个切尾错（设计稿 §15.0-A PARA-1）", len(head), len(head)+23)
	}
	if err == nil {
		t.Fatalf("equal 判不一致但未说明原因（应为 error：入参不可信，而不是「内容不同」）")
	}
	t.Logf("如实拒绝：%v", err)
}

// HASH-1 主探针：负数声明长度必须 fail-closed，绝不"一次不读就返回 true"。
func TestVerifyEqualRejectsNegativeDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	a := writeBytes(t, dir, "a.bin", []byte("aaa"))
	b := writeBytes(t, dir, "b.bin", []byte("zzz")) // 与 a 内容完全不同

	fa, _ := os.Open(a)
	defer fa.Close()
	fb, _ := os.Open(b)
	defer fb.Close()

	same, err := newVerifier().equal(context.Background(), fa, fb, -1)
	if err == nil {
		t.Fatalf("负数声明长度下 equal 返回 same=%v 且无错误：内容完全不同的两个文件被放行，"+
			"说明它在 remain<=0 时**一个字节都没读**就给了结论", same)
	}
	if same {
		t.Fatal("报错的同时不得返回 same=true")
	}
	if !strings.Contains(err.Error(), "不可信") && !strings.Contains(err.Error(), "长度") {
		t.Errorf("错误应自证「声明长度不可信」，实际：%v", err)
	}
}

// 负控制：等长且内容相同必须仍判一致——守卫不得把正常路径一并拦掉。
func TestVerifyEqualExactMatchStillPasses(t *testing.T) {
	dir := t.TempDir()
	content := []byte(strings.Repeat("same-", 100*1024))
	a := writeBytes(t, dir, "a.bin", content)
	b := writeBytes(t, dir, "b.bin", content)
	fa, _ := os.Open(a)
	defer fa.Close()
	fb, _ := os.Open(b)
	defer fb.Close()

	same, err := newVerifier().equal(context.Background(), fa, fb, int64(len(content)))
	if err != nil || !same {
		t.Fatalf("真重复必须仍判一致（守卫误伤），got same=%v err=%v", same, err)
	}
}

// REAL-1：短读纠偏只改逻辑 size、不碰实占栏 ⇒ 两个口径互相冒充。
//
// 走真夹具。阶段 1 会淘汰"独 size"，所以 p3 需要一个同尺寸的伴file（p4）才进得了
// 候选；截断后它纠正成 40KiB，正好落进 p1/p1b 那一组，于是同一个组里同时出现
// "被纠正过的条目"（p3）与"未被纠正的条目"（p1）——后者就是本卷实占可读的前置自检。
func TestShortReadCorrectionInvalidatesActualColumn(t *testing.T) {
	root := t.TempDir()
	head := make([]byte, 40<<10) // 40KiB：小文件路径，ActualSize 为精确读量
	for i := range head {
		head[i] = byte(i % 197)
	}
	extra := make([]byte, 60<<10)
	for i := range extra {
		extra[i] = byte(200 + i%50)
	}
	other := make([]byte, 100<<10)
	for i := range other {
		other[i] = byte(i % 91)
	}
	p1 := filepath.Join(root, "p1.bin")
	p1b := filepath.Join(root, "p1b.bin")
	p3 := filepath.Join(root, "p3.bin")
	p4 := filepath.Join(root, "p4.bin")
	if err := os.WriteFile(p1, head, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1b, head, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p3, append(append([]byte{}, head...), extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p4, other, 0o644); err != nil {
		t.Fatal(err)
	}
	truncateDuringScan(t, root, p3, 100<<10, 40<<10)

	pl := New()
	hookInto(t, pl)
	groups, _, err := pl.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	var corrected, plain *model.FileEntry
	for _, g := range groups {
		for _, f := range g.Files {
			switch f.Path {
			case p1:
				plain = f
			case p3:
				corrected = f
			}
		}
	}
	if plain == nil || corrected == nil {
		t.Fatalf("夹具未成立：p1=%v p3=%v 未同时出现在结果组里，本用例没有验证任何东西",
			plain != nil, corrected != nil)
	}
	if corrected.Size != uint64(len(head)) {
		t.Fatalf("夹具未成立：p3 的逻辑 size 未被纠正为 %d（实际 %d）", len(head), corrected.Size)
	}
	// 前置自检（独立于被测的那一行）：同一趟扫描里未被纠正的条目确实拿到了实占读数，
	// 说明本卷会把 ActualKnown 置真 ⇒ 下面要求被纠正的那条作废，不是"本来就没人统计"。
	if !plain.ActualKnown {
		t.Fatal("前置不成立：本卷连普通文件都没读到实占（p1.ActualKnown=false），REAL-1 无从断言")
	}
	if corrected.ActualKnown {
		t.Fatalf("p3 的逻辑 size 已从 %d 纠正为 %d，但 ActualKnown 仍为 true、Actual=%d："+
			"实占栏仍是**截断前那份**的读数 ⇒ 逻辑栏说了真话、实占栏没有（§15.0-A REAL-1）",
			100<<10, corrected.Size, corrected.Actual)
	}
}
