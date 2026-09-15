package dedup

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filededup/internal/model"
)

// genDataset 生成确定性测试数据（E2E 断言依据）：
// 返回根目录与期望重复组（每组路径集合）。
func genDataset(t *testing.T, root string) map[string][]string {
	t.Helper()
	mustWrite := func(rel string, b []byte) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rnd := func(n int) []byte {
		b := make([]byte, n)
		rand.Read(b)
		return b
	}
	expect := map[string][]string{}
	// 3 组重复：2 副本 / 3 副本 / 小文件重复
	g1 := mustWrite("u/a1.bin", rnd(3000))
	expect["g1"] = []string{g1, mustWrite("u/a2.bin", readFile(t, g1))}
	g2 := mustWrite("v/b1.bin", rnd(200<<10))
	expect["g2"] = []string{g2, mustWrite("v/b2.bin", readFile(t, g2)), mustWrite("v/b3.bin", readFile(t, g2))}
	g3 := mustWrite("w/c1.txt", rnd(1000))
	expect["g3"] = []string{g3, mustWrite("w/sub/c2.txt", readFile(t, g3))}
	// 独文件（不同内容同 size 也不同）
	mustWrite("u/unique1.bin", rnd(3000))
	mustWrite("u/unique2.bin", rnd(3000))
	mustWrite("u/unique3.bin", rnd(3000))
	// 0 字节与隐藏文件（默认跳过）
	mustWrite("u/zero.bin", nil)
	mustWrite("u/.h.txt", rnd(10))
	return expect
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPipelineE2E(t *testing.T) {
	root := t.TempDir()
	expect := genDataset(t, root)

	p := New()
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots:   []string{root},
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("失败清单非空: %+v", failed)
	}
	if len(groups) != len(expect) {
		t.Fatalf("重复组数 = %d, want %d; groups=%+v", len(groups), len(expect), groups)
	}
	totalReclaim := uint64(0)
	matched := map[string]bool{}
	for _, g := range groups {
		if len(g.Files) < 2 {
			t.Fatalf("组 %d 文件数 < 2", g.GroupID)
		}
		if g.Reclaimable != (uint64(len(g.Files))-1)*g.Files[0].Size {
			t.Fatalf("组 %d 可释放空间计算错误", g.GroupID)
		}
		totalReclaim += g.Reclaimable
		for _, want := range expect {
			if matched[string(g.Files[0].Path)] {
				continue
			}
			if sameSet(g, want) {
				matched[string(g.Files[0].Path)] = true
			}
		}
	}
	if len(matched) != len(expect) {
		t.Fatalf("匹配期望组 = %d, want %d", len(matched), len(expect))
	}
	if p.Status() != model.StatusDone {
		t.Fatalf("终态 = %s, want Done", p.Status())
	}
}

func sameSet(g *model.DuplicateGroup, want []string) bool {
	if len(g.Files) != len(want) {
		return false
	}
	set := map[string]bool{}
	for _, f := range g.Files {
		set[f.Path] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// TestVerifyGroupTamper 单元测试 paranoid 核心：哈希相同但逐字节不同（模拟哈希后被篡改）→ 移出组并入失败清单。
func TestVerifyGroupTamper(t *testing.T) {
	root := t.TempDir()
	rnd := func(n int) []byte { b := make([]byte, n); rand.Read(b); return b }
	base := rnd(3000)
	p1 := filepath.Join(root, "1.bin")
	p2 := filepath.Join(root, "2.bin") // 中间篡改一字节，size 不变
	p3 := filepath.Join(root, "3.bin") // 完全相同副本
	for _, x := range []struct {
		p string
		b []byte
	}{{p1, base}, {p3, base}} {
		if err := os.WriteFile(x.p, x.b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tam := append([]byte{}, base...)
	tam[len(tam)/2] ^= 0xFF
	if err := os.WriteFile(p2, tam, 0o644); err != nil {
		t.Fatal(err)
	}

	g := []*model.FileEntry{
		{Path: p1, Size: uint64(len(base))},
		{Path: p2, Size: uint64(len(base))},
		{Path: p3, Size: uint64(len(base))},
	}
	var failed []model.FailedItem
	kept, failed := verifyGroup(g, failed)
	if len(kept) != 2 {
		t.Fatalf("保留 = %d, want 2", len(kept))
	}
	if len(failed) != 1 || failed[0].Path != p2 {
		t.Fatalf("篡改文件应计入失败清单: %+v", failed)
	}
	if failed[0].Stage != "verify" {
		t.Fatalf("失败阶段应为 verify: %+v", failed[0])
	}
}

// TestPipelineParanoidNoFalsePositive paranoid 模式对真实重复组无误伤（结果与非 paranoid 一致）。
func TestPipelineParanoidNoFalsePositive(t *testing.T) {
	root := t.TempDir()
	expect := genDataset(t, root)

	p1 := New()
	g1, _, err := p1.Run(context.Background(), model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	p2 := New()
	g2, _, err := p2.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Paranoid: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g1) != len(g2) || len(g1) != len(expect) {
		t.Fatalf("paranoid 组数 = %d, 普通 = %d, want %d", len(g2), len(g1), len(expect))
	}
	for i := range g1 {
		if len(g1[i].Files) != len(g2[i].Files) {
			t.Fatalf("组 %d 文件数不一致: paranoid=%d 普通=%d", i, len(g2[i].Files), len(g1[i].Files))
		}
	}
}

// TestPipelineHashFailureNoFalseGroup 回归：同尺寸但预筛/哈希均失败（不可读）的
// 不同文件不得形成重复组——零哈希假组会导致用户误删。
func TestPipelineHashFailureNoFalseGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 权限语义不适用")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 不受文件读取权限限制")
	}
	root := t.TempDir()
	p1 := filepath.Join(root, "a.bin")
	p2 := filepath.Join(root, "b.bin")
	if err := os.WriteFile(p1, []byte("AAAA-content-x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("BBBB-content-y"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{p1, p2} {
		if err := os.Chmod(p, 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(p, 0o644) // 保障 TempDir 清理
	}
	p := New()
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("哈希失败文件不得形成重复组: %+v", groups)
	}
	if len(failed) != 2 {
		t.Fatalf("失败清单 = %d, want 2: %+v", len(failed), failed)
	}
	for _, f := range failed {
		if f.Stage != "prefilter" {
			t.Fatalf("失败阶段 = %s, want prefilter: %+v", f.Stage, f)
		}
	}
}

func TestPipelineHardlinkNotDuplicate(t *testing.T) {
	// 硬链接（同物理文件多路径）：不计为重复、不占可释放空间
	if runtime.GOOS == "windows" {
		t.Skip("Windows 硬链接语义由 FileKey 集成测试覆盖")
	}
	root := t.TempDir()
	src := filepath.Join(root, "orig.bin")
	if err := os.WriteFile(src, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.bin")
	if err := os.Link(src, link); err != nil {
		t.Skipf("硬链接创建失败: %v", err)
	}
	p := New()
	groups, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		for _, f := range g.Files {
			if strings.HasSuffix(f.Path, "orig.bin") || strings.HasSuffix(f.Path, "link.bin") {
				t.Fatal("硬链接被误判为重复")
			}
		}
	}
}

func TestPipelineCancelNoLeak(t *testing.T) {
	// 较大数据集保证任务窗口：等待进入运行态后立即取消
	root := t.TempDir()
	for i := 0; i < 4000; i++ {
		p := filepath.Join(root, "bulk", "f"+strconv.Itoa(i)+".bin")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 64)
		rand.Read(b)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	p := New()
	go func() {
		// 等待进入运行态（避免小数据集在取消前完成的竞态）
		for p.Status() == model.StatusIdle {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	_, _, err := p.Run(ctx, model.ScanConfig{Roots: []string{root}, Threads: 4})
	if err == nil {
		t.Fatal("取消后 Run 应返回错误")
	}
	if p.Status() != model.StatusCancelled {
		t.Fatalf("终态 = %s, want Cancelled", p.Status())
	}
	// goroutine 泄漏检测：等待回收后对比
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("疑似 goroutine 泄漏: before=%d after=%d", before, runtime.NumGoroutine())
}

func TestPipelinePauseResume(t *testing.T) {
	// 较大数据集保证任务窗口覆盖预筛/哈希阶段
	root := t.TempDir()
	for i := 0; i < 4000; i++ {
		p := filepath.Join(root, "bulk", "f"+strconv.Itoa(i)+".bin")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 64)
		rand.Read(b) // 内容各异，避免 bulk 文件自身形成重复组
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expect := genDataset(t, root)
	p := New()
	finished := make(chan struct{})
	var groups []*model.DuplicateGroup
	go func() {
		defer close(finished)
		g, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Threads: 2})
		if err != nil {
			t.Errorf("Run: %v", err)
			return
		}
		groups = g
	}()
	time.Sleep(5 * time.Millisecond) // 进入运行态后暂停
	p.Pause()
	// 挂起稳定：paused 状态保持，任务未完成
	deadline := time.Now().Add(2 * time.Second)
	for p.Status() != model.StatusPaused && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.Status() != model.StatusPaused {
		t.Fatalf("应进入 Paused，got %s", p.Status())
	}
	select {
	case <-finished:
		t.Fatal("暂停期间任务不应完成")
	default:
	}
	p.Resume()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("恢复后任务未完成")
	}
	if len(groups) != len(expect) {
		t.Fatalf("恢复后组数 = %d, want %d", len(groups), len(expect))
	}
	if p.Status() != model.StatusDone {
		t.Fatalf("终态 = %s, want Done", p.Status())
	}
}

func TestPipelineIllegalStart(t *testing.T) {
	// Done → Scanning 非法转换应被拒绝
	p := New()
	root := t.TempDir()
	genDataset(t, root)
	if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}}); err == nil {
		t.Fatal("Done 状态下重复 Run 应被状态机拒绝")
	}
}

func TestProgressCallbackFired(t *testing.T) {
	root := t.TempDir()
	genDataset(t, root)
	var fired atomic.Int64
	p := New()
	p.OnProgress = func(ev model.ProgressEvent) {
		fired.Add(1)
	}
	if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Threads: 2}); err != nil {
		t.Fatal(err)
	}
	if fired.Load() == 0 {
		t.Fatal("进度回调未触发")
	}
}
