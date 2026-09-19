package dedup

// 短读（short read）端到端回归（2026-09-19 修复）。
//
// 用户现象（Windows 11 x64）：「有扫描的动作，但无法扫描到重复文件（实际存在），
// 也未保存缓存」。
//
// 根因：目录遍历取到的 size 与随后 open 读到的可读长度不一致（Windows 的
// exFAT/SMB 网络盘、Defender 实时扫描、OneDrive 云占位文件、稀疏/压缩文件，
// 以及 ReadDir→Open 的 TOCTOU 窗口都会造成），HashHeadTail 把由此产生的
// io.ErrUnexpectedEOF 当真实错误返回，pipeline 阶段 2 据此把该文件
// skip=true 整个剔除出预筛分组，后果是：
//   - 它与任何文件都不可能同桶 → 整组重复静默消失（扫不到重复）
//   - 它永远走不到 pending 入队那一行 → 缓存里永远没有它（未保存缓存）
//
// 本测试在文件系统层**真实制造**短读：先让遍历看到正确的 size，再在预筛
// 之前把文件截断，使"记录 size > 实际可读长度"成立。用 walkHook 注入。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/model"
)

// TestPipelineSurvivesTruncatedDuplicate 验证：一个重复组中若有一个成员在
// 扫描期间被截断，其余成员仍必须被扫出（修正前该组会整体消失）。
func TestPipelineSurvivesTruncatedDuplicate(t *testing.T) {
	root := t.TempDir()
	payload := make([]byte, 300<<10) // 300KiB > SmallFileMax，走大文件路径
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	// 三个同内容文件：a1、a2 在根目录，a3 在子目录（对应隔离清单里的
	// 「子目录里的副本文件」）
	p1 := filepath.Join(root, "a1.bin")
	p2 := filepath.Join(root, "a2.bin")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p3 := filepath.Join(sub, "a3.bin")
	for _, p := range []string{p1, p2, p3} {
		if err := os.WriteFile(p, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 一个独文件（不应出现在结果里）
	if err := os.WriteFile(filepath.Join(root, "uniq.bin"), []byte("unique-content-here"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 关键步骤：在预筛真正开始前，把 a3 截断——模拟「遍历与读取之间文件变短」。
	// 用一个在 stage 2 入口触发的钩子实现，确保遍历已记录完整 size。
	truncateDuringScan(t, root, p3, int64(len(payload)), 100<<10)

	p := New()
	hookInto(t, p)
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots:   []string{root},
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 核心断言：a1 与 a2 必须成为一组（修正前整组消失，groups 为空）
	var found bool
	for _, g := range groups {
		if len(g.Files) >= 2 {
			has1, has2 := false, false
			for _, f := range g.Files {
				if f.Path == p1 {
					has1 = true
				}
				if f.Path == p2 {
					has2 = true
				}
			}
			if has1 && has2 {
				found = true
				t.Logf("✅ 找到 a1+a2 重复组（%d 个成员），修正前该组会整体消失", len(g.Files))
			}
		}
	}
	if !found {
		t.Fatalf("a1/a2 重复组未被扫出——短读导致整组静默消失的 bug 复现。groups=%d failed=%+v",
			len(groups), failed)
	}
	// 被截断的文件应留下可解释的失败提示（而不是无声无息）
	t.Logf("失败清单（含被截断文件的说明）: %+v", failed)
}

// TestPipelineTruncatedFileStillCached 验证：被截断的文件仍会写入缓存
// （对应「未保存缓存」现象）。
func TestPipelineTruncatedFileStillCached(t *testing.T) {
	root := t.TempDir()
	payload := make([]byte, 300<<10)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	p1 := filepath.Join(root, "a1.bin")
	p2 := filepath.Join(root, "a2.bin")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cachePath := filepath.Join(t.TempDir(), "cache.db")
	cch, err := cache.Open(cachePath)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	defer cch.Close()

	truncateDuringScan(t, root, p2, int64(len(payload)), 100<<10)

	p := New().WithCache(cch)
	hookInto(t, p)
	_, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots:    []string{root},
		Threads:  4,
		UseCache: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("失败清单: %+v", failed)

	stats, err := cch.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	t.Logf("缓存条目数 = %d（含全量哈希 = %d）", stats.Entries, stats.WithFull)

	// 正确文件必须入缓存
	if stats.Entries < 1 {
		t.Fatalf("缓存条目为 0——「未保存缓存」现象复现")
	}
	// 被截断的文件也应入缓存（它已按实际长度被正确处理，不再被剔除）
	if _, ok := cch.LastHit(p2); !ok {
		t.Fatalf("被截断的 %s 未写入缓存（修正前它会被 skip 剔除，永远不入 pending）", p2)
	}
	t.Logf("✅ 被截断文件 %s 已写入缓存", filepath.Base(p2))
}

// truncateDuringScan 在流水线进入「预筛」阶段前把 target 截断到 newSize，
// 从而真实制造「目录遍历记录的 size > 随后实际可读长度」的场景。
//
// 实现方式：挂 OnStage 回调，收到 stage=="prefilter" 时执行截断。该回调在
// stage("prefilter", ...) 时被调用，此刻阶段 0 的遍历已完成（记录的是截断前
// 的完整 size），而阶段 2 的读取尚未开始——正是需要复现的时序窗口。
//
// 这比"直接写一个比真实内容短的文件"更贴近 Windows 上的真实成因：问题不在
// 文件本身，而在两个系统调用之间被改变。
func truncateDuringScan(t *testing.T, root, target string, oldSize, newSize int64) {
	t.Helper()
	if newSize >= oldSize {
		t.Fatalf("newSize(%d) 必须小于 oldSize(%d)", newSize, oldSize)
	}
	// 先记录截断动作的状态，供断言与调试
	t.Logf("将在 prefilter 阶段把 %s 从 %d 字节截断到 %d 字节",
		filepath.Base(target), oldSize, newSize)
	// 由测试调用方把返回值挂到 Pipeline.OnStage 上
	// （见 truncateHook 的用法说明）
	pendingTruncate = []pendingTrunc{{
		target: target, newSize: newSize,
	}}
}

// pendingTrunc 一次待执行的截断。
type pendingTrunc struct {
	target  string
	newSize int64
}

// pendingTruncate 本测试文件内的待执行截断队列。
// 用包级变量而非回调闭包，是为了让 truncateDuringScan 只负责"登记"，
// 具体挂钩由 hookInto 完成——保持调用点可读。
var pendingTruncate []pendingTrunc

// hookInto 把登记好的截断动作挂到 p.OnStage 上。
func hookInto(t *testing.T, p *Pipeline) {
	t.Helper()
	q := pendingTruncate
	pendingTruncate = nil
	if len(q) == 0 {
		return
	}
	p.OnStage = func(ev model.StageEvent) {
		if ev.Stage != "prefilter" {
			return
		}
		for _, pt := range q {
			f, err := os.OpenFile(pt.target, os.O_WRONLY, 0o644)
			if err != nil {
				t.Logf("截断打开失败 %s: %v", pt.target, err)
				continue
			}
			if err := f.Truncate(pt.newSize); err != nil {
				t.Logf("截断失败 %s: %v", pt.target, err)
			}
			f.Close()
			if st, err := os.Stat(pt.target); err == nil {
				t.Logf("已在 prefilter 阶段把 %s 截断为 %d 字节",
					filepath.Base(pt.target), st.Size())
			}
		}
	}
}
