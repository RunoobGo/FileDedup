package dedup

// 验证：短读导致的 size 高估**不会**造出假重复组。
// 场景：一个被截断的文件（其纠正后的 size 是被高估的）与另一个真实大小
// 恰好等于该高估值的文件——两者可能落进同一个 size 桶。最终分组按全量
// 哈希聚合，因此必须分开。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/model"
)

func TestNoFalsePositiveFromShortRead(t *testing.T) {
	root := t.TempDir()
	payload := make([]byte, 300<<10)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	// truncated.bin：300KiB，稍后被截断
	trunc := filepath.Join(root, "truncated.bin")
	if err := os.WriteFile(trunc, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// other.bin：内容完全不同，但大小恰好落在"高估后的 size"上
	otherContent := make([]byte, 150<<10)
	for i := range otherContent {
		otherContent[i] = byte((i * 7) % 253)
	}
	other := filepath.Join(root, "other.bin")
	if err := os.WriteFile(other, otherContent, 0o644); err != nil {
		t.Fatal(err)
	}
	// 同时放两个真正互为副本的文件，确保流水线确实在正常工作
	dupA := filepath.Join(root, "dupA.bin")
	dupB := filepath.Join(root, "dupB.bin")
	dup := []byte("genuine-duplicate-content-payload")
	for _, p := range []string{dupA, dupB} {
		if err := os.WriteFile(p, dup, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	truncateDuringScan(t, root, trunc, int64(len(payload)), 100<<10)
	p := New()
	hookInto(t, p)
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots:   []string{root},
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("失败清单: %+v", failed)

	for _, g := range groups {
		var hasTrunc, hasOther bool
		for _, f := range g.Files {
			if f.Path == trunc {
				hasTrunc = true
			}
			if f.Path == other {
				hasOther = true
			}
		}
		if hasTrunc && hasOther {
			t.Fatalf("❌ 假重复组：被截断文件与无关文件被判为重复（全量哈希未能区分）")
		}
	}
	// 真正的副本对必须被找到
	var foundGenuine bool
	for _, g := range groups {
		var a, b bool
		for _, f := range g.Files {
			if f.Path == dupA {
				a = true
			}
			if f.Path == dupB {
				b = true
			}
		}
		if a && b {
			foundGenuine = true
		}
	}
	if !foundGenuine {
		t.Fatalf("真正的重复对被漏报（流水线基本功能受损）")
	}
	t.Logf("✅ 无假重复组，且真重复对正常检出（共 %d 组）", len(groups))
}
