package dedup

// R-算法-6（2026-09-24 审查五轮 F 批，J-4=(b) 随批修，纯效率/方向安全）：
// 缓存命中但四点采样不符（内容已变）、且本轮是小文件（一趟已算出真 full）时，
// 修前把 r.Full 丢弃、令 fullValid=false，逼阶段 3 对同一文件再做一次全量读+哈希；
// 修后直接采用 r.Full，结果逐字节等价，只省掉那次重算。
//
// 为什么用源码锚而非行为 RED：① 分组输出与修前完全一致，无外部可观测差异；
// ② "命中缓存 + 采样不符"要求 size/mtime 不变而内容变，在 ctime 身份门禁
// （cache.go:309）下难以确定性构造。故与 D2/D3 同源用源码锚钉住接线，
// 正确性由 dedup 包既有回归兜底。

import (
	"os"
	"strings"
	"testing"
)

func TestCacheHitSampleMismatchAdoptsRecomputedFullForSmallFiles(t *testing.T) {
	src, err := os.ReadFile("pipeline.go")
	if err != nil {
		t.Fatalf("读 pipeline.go: %v", err)
	}
	s := string(src)

	// 采样一致分支的锚：确保下面的 else 分支就挂在缓存命中路径上。
	const matchAnchor = "fullValidNow = fullValid"
	matchIdx := strings.Index(s, matchAnchor)
	if matchIdx < 0 {
		t.Fatalf("找不到 %q 锚（缓存命中分支结构变了，请同步本锚）", matchAnchor)
	}
	tail := s[matchIdx:]

	if !strings.Contains(tail, "} else if r.Small {") {
		t.Fatal("缓存命中分支缺少 `else if r.Small`——采样不符时小文件本轮已算的 r.Full 被丢弃、阶段 3 重算")
	}
	adoptIdx := strings.Index(tail, "full = r.Full")
	flagIdx := strings.Index(tail, "fullValidNow = true")
	if adoptIdx < 0 {
		t.Fatal("缺少 `full = r.Full`——没有采用本轮已算的全量哈希")
	}
	if flagIdx < 0 {
		t.Fatal("缺少 `fullValidNow = true`——采样不符的小文件不会被标记为已有 full，仍进阶段 3 重算")
	}
	if adoptIdx > flagIdx {
		t.Fatalf("应先采用 r.Full 再置 fullValidNow=true（adoptIdx=%d flagIdx=%d）", adoptIdx, flagIdx)
	}
}
