package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// D2（R-门禁-4，2026-09-24 第五轮审查）：-cache / -o 落在任一扫描根内时的自指污染守卫。
//
// 危害面（现读 cmd/fdd-cli/main.go + scripts/smoke-cli.sh 核实）：
//   - -cache 在根内 → cache.db / -wal / -shm 被当语料扫进 files_total，甚至彼此配成"重复组"，
//     smoke 的"三跑逐项一致 + 与 manifest 对账"当场失真；
//   - -o 在根内 → os.Create **静默截断**根内既有文件（M222 已登记"并非零写操作"，本条是新危害面），
//     且报告 JSON 成为下一跑的语料。
//
// 守卫的判据抽成纯函数 enclosingRoot（main() 无可注入接缝，故行为逻辑走单测、接线走源码锚，
// 手法同 TestCLIStatsCaseProbeUnprovenIsWired 的既有先例）。

func TestEnclosingRootDetectsTargetInsideRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()

	cases := []struct {
		name       string
		target     string
		roots      []string
		wantInRoot bool // true=应判为落在某个根内（返回非空）
	}{
		{"cache 就在根内", filepath.Join(root, "cache.db"), []string{root}, true},
		{"cache 在根的深层子目录", filepath.Join(root, "a", "b", "cache.db"), []string{root}, true},
		{"out 报告落在根内", filepath.Join(root, "report.json"), []string{root}, true},
		{"target 与根同级（冒烟的正常摆位）", filepath.Join(other, "cache.db"), []string{root}, false},
		{"target 在另一棵树", filepath.Join(other, "x", "cache.db"), []string{root}, false},
		{"多根，命中第二个", filepath.Join(other, "cache.db"), []string{root, other}, true},
		{"多根，都不命中", filepath.Join(t.TempDir(), "cache.db"), []string{root, other}, false},
		{"空 target（未传该 flag）", "", []string{root}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := enclosingRoot(c.target, c.roots)
			if c.wantInRoot {
				if got == "" {
					t.Fatalf("enclosingRoot(%q, roots) = \"\"，应判为落在根内（守卫会漏放，自指污染照样发生）", c.target)
				}
			} else {
				if got != "" {
					t.Fatalf("enclosingRoot(%q, roots) = %q，不该命中任何根（守卫会误拒合法摆位，冒烟直接跑不起来）", c.target, got)
				}
			}
		})
	}
}

// 前缀陷阱负控制：root="/tmp/bench" 时 target="/tmp/benchmark/x" 不应命中——
// 单纯 HasPrefix("/tmp/benchmark/x", "/tmp/bench") 为真会误判，必须按分隔符边界比。
func TestEnclosingRootRespectsSeparatorBoundary(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "bench")
	sibling := filepath.Join(string(filepath.Separator), "tmp", "benchmark", "cache.db")
	if got := enclosingRoot(sibling, []string{root}); got != "" {
		t.Fatalf("enclosingRoot(%q, [%q]) = %q，前缀重叠但不在其下，不应命中（pathnorm.Under 的分隔符边界被绕过）",
			sibling, root, got)
	}
	inside := filepath.Join(root, "cache.db")
	if got := enclosingRoot(inside, []string{root}); got == "" {
		t.Fatalf("enclosingRoot(%q, [%q]) = \"\"，真在根内却没命中", inside, root)
	}
}

// 源码级接线锚（AS-K6 / M134/M142 同族）：enclosingRoot 定义得再对，main() 没去调也白搭。
// 钉两件事——① -cache 与 -o 各调一次；② 两处都必须在 p.Run（开扫）之前，
// 因为 cache.Open 会创建 db 文件、p.Run 会读目录，守卫晚一步就已经污染了。
func TestSelfRefGuardIsWiredBeforeRun(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读 main.go: %v", err)
	}
	s := string(src)
	const (
		cacheAnchor = "enclosingRoot(*cachePath, roots)"
		outAnchor   = "enclosingRoot(*out, roots)"
		runAnchor   = "p.Run(context.Background(), cfg)"
	)
	for _, a := range []string{cacheAnchor, outAnchor} {
		if n := strings.Count(s, a); n != 1 {
			t.Fatalf("自指守卫锚 %q 必须恰好出现 1 次，实得 %d 次（守卫被删/被搬走/被复制成多处）", a, n)
		}
	}
	runIdx := strings.Index(s, runAnchor)
	if runIdx < 0 {
		t.Fatalf("找不到 p.Run 锚 %q，无法断言自指守卫先于开扫（main.go 结构变了，请同步本锚）", runAnchor)
	}
	for _, a := range []string{cacheAnchor, outAnchor} {
		if idx := strings.Index(s, a); idx > runIdx {
			t.Fatalf("自指守卫 %q 出现在 p.Run 之后——守卫必须在开扫/建缓存文件**之前**拦截，晚一步就已污染", a)
		}
	}
}
