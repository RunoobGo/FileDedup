package dedup

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	kept, failed := newVerifier().group(context.Background(), g, failed)
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

// TestPipelineHardlinkRepIDStable C3 回归：硬链接多路径去重保留短路径时，
// 只允许替换路径派生字段（Path/Ext），不得把被丢弃项的 ID 覆盖到代表体上——
// 下游按 ID 关联选中态/保留决策/预览，覆盖会让 ID 与结果集错位。
//
// 确定性来源：Threads=1 + 单目录 → scanner 按目录项（字典序）顺序分配 ID：
// aaa-longer-name.bin=1, mid.bin=2, zz.bin=3。
// 阶段 1.5 中 zz 与 aaa 同 inode（key 相同）且路径更短 → 替换代表体字段。
//
//	修复前：*prev = *e → 代表体 ID 被覆盖为 3（zz 的）。
//	修复后：仅换 Path/Ext → 代表体保持 ID=1（aaa 的），Path=zz.bin。
func TestPipelineHardlinkRepIDStable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows FileKey 需开句柄，ID 顺序同样依赖单 worker，跳过")
	}
	root := t.TempDir()
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte(i)
	}
	long := filepath.Join(root, "aaa-longer-name.bin")
	if err := os.WriteFile(long, content, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "zz.bin") // 短路径硬链接（同 inode）
	if err := os.Link(long, link); err != nil {
		t.Skipf("硬链接创建失败: %v", err)
	}
	mid := filepath.Join(root, "mid.bin") // 同内容独立副本（不同 inode）
	if err := os.WriteFile(mid, content, 0o644); err != nil {
		t.Fatal(err)
	}

	p := New()
	groups, _, err := p.Run(context.Background(), model.ScanConfig{
		Roots: []string{root}, Threads: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("组数 = %d, want 1（硬链接对计一次 + 独立副本）", len(groups))
	}
	g := groups[0]
	if len(g.Files) != 2 {
		t.Fatalf("组内文件数 = %d, want 2: %+v", len(g.Files), g.Files)
	}
	for _, f := range g.Files {
		if strings.HasSuffix(f.Path, "zz.bin") {
			// 修复前此处为 3（被 zz 自身 ID 覆盖）；修复后应保持 aaa 的 ID=1
			if f.ID != 1 {
				t.Fatalf("C3 回归：短路径代表体 ID = %d, want 1（长路径项 ID）；Path=%s",
					f.ID, f.Path)
			}
			return
		}
	}
	t.Fatalf("结果组中未找到短路径硬链接 zz.bin: %+v", g.Files)
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

// TestPipelineCancelRace Y1：Cancel 与 Run 并发（Run 持锁写 p.cancel）。
// 该测试配合 -race 运行：修复前 Cancel 无锁读取会触发数据竞争报告。
func TestPipelineCancelRace(t *testing.T) {
	root := t.TempDir()
	genDataset(t, root)
	p := New()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				p.Cancel() // 与 Run 的 p.cancel 赋值并发读写
			}
		}
	}()
	_, _, _ = p.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Threads: 2})
	close(stop)
	wg.Wait()
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

// P0-1 回归：终态不得永久锁死流水线。
// 修正前 Done 状态下的第二次 Run 永远被拒（无人置回 Idle），
// 而 app 层已清空旧结果集 → 界面永久卡在「扫描中」，增量缓存特性不可达。
func TestPipelineRerunAfterTerminalStates(t *testing.T) {
	root := t.TempDir()
	genDataset(t, root)

	t.Run("Done后可再扫", func(t *testing.T) {
		p := New()
		g1, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		if p.Status() != model.StatusDone {
			t.Fatalf("终态 = %s, want Done", p.Status())
		}
		g2, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}})
		if err != nil {
			t.Fatalf("第二次 Run 被拒: %v", err)
		}
		if len(g2) != len(g1) {
			t.Fatalf("两次扫描组数应一致: %d vs %d", len(g1), len(g2))
		}
		if p.Status() != model.StatusDone {
			t.Fatalf("第二次终态 = %s, want Done", p.Status())
		}
	})

	t.Run("Cancelled后可再扫", func(t *testing.T) {
		// 较大数据集 + 等待进入运行态再取消（同 TestPipelineCancelNoLeak 手法），
		// 避免小数据集在取消前就跑完、终态落在 Done 的竞态。
		croot := t.TempDir()
		for i := 0; i < 4000; i++ {
			p := filepath.Join(croot, "f"+strconv.Itoa(i)+".bin")
			b := make([]byte, 64)
			rand.Read(b)
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		p := New()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for p.Status() == model.StatusIdle {
				time.Sleep(time.Millisecond)
			}
			cancel()
		}()
		_, _, err := p.Run(ctx, model.ScanConfig{Roots: []string{croot}, Threads: 4})
		if err == nil {
			t.Skip("数据集过小，取消前已完成（不构成失败）")
		}
		if p.Status() != model.StatusCancelled {
			t.Fatalf("终态 = %s, want Cancelled", p.Status())
		}
		if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{croot}}); err != nil {
			t.Fatalf("取消后再次 Run 被拒: %v", err)
		}
	})

	// 运行中重入仍须被拒——复位逻辑只能作用于终态，不得把「并发双开」也放行。
	// app 层有 scanInFlight 互斥，此处是引擎自身的第二道防线。
	// 白盒注入运行态，避免依赖时序（数据集大小/调度）造成竞态。
	t.Run("运行中重入仍被拒", func(t *testing.T) {
		for _, s := range []model.TaskStatus{model.StatusScanning, model.StatusPrefiltering, model.StatusHashing} {
			p := New()
			p.mu.Lock()
			p.status = s
			p.mu.Unlock()
			if _, _, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}}); err == nil {
				t.Fatalf("%s 状态下的第二次 Run 必须被拒绝", s)
			}
			if p.Status() != s {
				t.Fatalf("%s 被误复位为 %s（复位只允许发生在终态）", s, p.Status())
			}
		}
		// Paused 按既有语义可恢复到 Scanning，此处仅锁定不被复位逻辑改变
		p := New()
		p.mu.Lock()
		p.status = model.StatusPaused
		p.mu.Unlock()
		if p.isTerminalLocked() {
			t.Fatal("Paused 不是终态，不得被复位")
		}
	})
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

// TestProgressAccountingConsistent R2 回归：进度总量口径必须与实际计入量一致。
// 修正前预筛读量（min(size,128KiB)）与全量哈希读量（size）对同一文件重复累加，
// BytesDone 可超过 BytesTotal → 进度条虚满、ETA 提前归零。
func TestProgressAccountingConsistent(t *testing.T) {
	root := t.TempDir()
	genDataset(t, root) // 含 200KiB 大文件重复组：走「预筛 + 全量」两阶段
	var mu sync.Mutex
	var evs []model.ProgressEvent
	p := New()
	p.OnProgress = func(ev model.ProgressEvent) {
		mu.Lock()
		evs = append(evs, ev)
		mu.Unlock()
	}
	_, failed, err := p.Run(context.Background(), model.ScanConfig{Roots: []string{root}, Threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("不应有失败项: %+v", failed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(evs) == 0 {
		t.Fatal("无进度事件")
	}
	for i, ev := range evs {
		if ev.BytesTotal > 0 && ev.BytesDone > ev.BytesTotal {
			t.Fatalf("事件 #%d: BytesDone(%d) > BytesTotal(%d)，口径重复累加（R2 回归）",
				i, ev.BytesDone, ev.BytesTotal)
		}
		if ev.FilesDone > ev.FilesTotal {
			t.Fatalf("事件 #%d: FilesDone(%d) > FilesTotal(%d)", i, ev.FilesDone, ev.FilesTotal)
		}
	}
	// 无失败场景下终值须精确收敛（Stop 推终值）
	last := evs[len(evs)-1]
	t.Logf("进度终值: files=%d/%d bytes=%d/%d events=%d",
		last.FilesDone, last.FilesTotal, last.BytesDone, last.BytesTotal, len(evs))
	if last.FilesDone != last.FilesTotal || last.BytesDone != last.BytesTotal {
		t.Fatalf("终值未收敛: files %d/%d, bytes %d/%d",
			last.FilesDone, last.FilesTotal, last.BytesDone, last.BytesTotal)
	}
}

// G1 回归：比对缓冲必须在组内复用——比对分配量不得随组内文件数线性增长。
func TestVerifierReusesBuffers(t *testing.T) {
	root := t.TempDir()
	size := verifyBufSize*2 + 4096 // 跨 3 个分块，确保走满多轮循环
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	var g []*model.FileEntry
	const n = 6
	for i := 0; i < n; i++ {
		p := filepath.Join(root, fmt.Sprintf("f%d.bin", i))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		g = append(g, &model.FileEntry{Path: p, Size: uint64(size)})
	}

	ver := newVerifier() // 缓冲先分配好，排除建缓冲本身的影响
	var failed []model.FailedItem

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	kept, _ := ver.group(context.Background(), g, failed)
	runtime.ReadMemStats(&after)

	if len(kept) != n {
		t.Fatalf("保留 = %d, want %d", len(kept), n)
	}
	// 不复用时：n-1 = 5 次比对 × 2 × 256KiB = 2.5MiB
	// 复用后只应有极少量杂项分配，阈值取 512KiB 留足余量
	alloc := after.TotalAlloc - before.TotalAlloc
	const naiveAlloc = (n - 1) * 2 * verifyBufSize
	if alloc > 512<<10 {
		t.Fatalf("比对分配 %d 字节，疑似未复用缓冲（不复用应为 %d 字节）", alloc, naiveAlloc)
	}
	t.Logf("比对分配 %d 字节（不复用应为 %d 字节，省 %.1f%%）",
		alloc, naiveAlloc, 100*(1-float64(alloc)/float64(naiveAlloc)))
}

// G1 等价性：跨多个 256KiB 分块的比对必须正确，含仅首字节 / 仅末块不一致。
func TestVerifierMultiChunkEquivalence(t *testing.T) {
	root := t.TempDir()
	size := verifyBufSize*2 + 12345
	base := make([]byte, size)
	if _, err := rand.Read(base); err != nil {
		t.Fatal(err)
	}

	write := func(name string, b []byte) string {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	same := append([]byte{}, base...)

	headDiff := append([]byte{}, base...)
	headDiff[0] ^= 0xFF // 第 1 块不一致

	tailDiff := append([]byte{}, base...)
	tailDiff[size-1] ^= 0xFF // 最后一个不完整块不一致

	pRep := write("rep.bin", base)
	pSame := write("same.bin", same)
	pHead := write("head.bin", headDiff)
	pTail := write("tail.bin", tailDiff)

	g := []*model.FileEntry{
		{Path: pRep, Size: uint64(size)},
		{Path: pSame, Size: uint64(size)},
		{Path: pHead, Size: uint64(size)},
		{Path: pTail, Size: uint64(size)},
	}
	var failed []model.FailedItem
	kept, failed := newVerifier().group(context.Background(), g, failed)

	if len(kept) != 2 {
		t.Fatalf("保留 = %d, want 2 (rep + same)", len(kept))
	}
	if kept[0].Path != pRep || kept[1].Path != pSame {
		t.Fatalf("保留集错误: %v, %v", kept[0].Path, kept[1].Path)
	}
	if len(failed) != 2 {
		t.Fatalf("失败条目 = %d, want 2", len(failed))
	}
	got := map[string]bool{failed[0].Path: true, failed[1].Path: true}
	if !got[pHead] || !got[pTail] {
		t.Fatalf("失败集应含 head/tail 两处差异: %+v", failed)
	}
}

// G1 等价性：代表文件句柄在每次比对前复位，多轮比对结果不受顺序影响。
func TestVerifierSeekResetBetweenFiles(t *testing.T) {
	root := t.TempDir()
	size := verifyBufSize * 2
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	var g []*model.FileEntry
	for i := 0; i < 4; i++ {
		p := filepath.Join(root, fmt.Sprintf("s%d.bin", i))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		g = append(g, &model.FileEntry{Path: p, Size: uint64(size)})
	}
	var failed []model.FailedItem
	kept, _ := newVerifier().group(context.Background(), g, failed)
	if len(kept) != 4 {
		t.Fatalf("保留 = %d, want 4（若句柄未复位，第 3 个起会读到 EOF 而误判）", len(kept))
	}
}

// G6 安全契约：自动并发度必须在 [1, 核数-1] 区间内——只降级不升级，
// 且任何探测结果都不会算出非法的并发数。
func TestAutoThreadsWithinBounds(t *testing.T) {
	base := defaultThreads()
	roots := []string{t.TempDir()}
	got := autoThreads(roots)
	if got < 1 {
		t.Fatalf("自动并发度必须 >= 1，实际 %d", got)
	}
	if got > base {
		t.Fatalf("自动并发度 %d 超过核数-1(%d)，违反「只降级不升级」", got, base)
	}
	// 空 roots 不得 panic，且维持默认
	if empty := autoThreads(nil); empty != base {
		t.Fatalf("空 roots 应维持默认 %d，实际 %d", base, empty)
	}
	// 不存在的根：探测失败必须维持默认，绝不降级
	if bad := autoThreads([]string{"/definitely/not/here/xyz"}); bad != base {
		t.Fatalf("探测失败应维持默认 %d，实际 %d", base, bad)
	}
	t.Logf("核数-1 = %d，实测自动并发度 = %d（根 %v）", base, got, roots)
}
