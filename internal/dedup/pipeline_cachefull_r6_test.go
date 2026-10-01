package dedup

// R-算法-6（2026-09-24 审查五轮 F 批，J-4=(b) 随批修，纯效率/方向安全）：
// 缓存命中但四点采样不符（内容已变）、且本轮是小文件（一趟已算出真 full）时，
// 修前把 r.Full 丢弃、令 fullValid=false，逼阶段 3 对同一文件再做一次全量读+哈希；
// 修后直接采用 r.Full，结果逐字节等价，只省掉那次重算。
//
// 为什么用源码锚而非行为 RED：① 分组输出与修前完全一致，无外部可观测差异——
// 这一条是本格用源码锚的主因，与身份判据怎么定无关。
// ②〔2026-10-01 追记，随裁-2 更正〕改前这里还写了第二条理由："『命中缓存 + 采样不符』
//   要求 size/mtime 不变而内容变，在 ctime 身份门禁下难以确定性构造"。**这条已经不成立**：
//   ctime 移出失效判据后，"原地改一个字节 + 把 mtime 拨回"是可确定性构造的
//   （`cache_h1_test.go` 走的就是这一条路）。★ 但本格仍然只能是源码锚：理由 ① 没变，
//   把它改成行为用例要新造一套"命中腿采纳重算 full"的可观测差异，属另一件事。
//   留下这条更正记录，是因为照着旧理由推理会误以为身份门禁还兜得住原地改写。
// 故与 D2/D3 同源用源码锚钉住接线，正确性由 dedup 包既有回归兜底。

import (
	"os"
	"strings"
	"testing"
)

func TestCacheHitSampleMismatchAdoptsRecomputedFullForSmallFiles(t *testing.T) {
	// M341（2026-09-28）：扫描面由 `pipeline.go` 单文件扩到
	// 「pipeline.go + pipeline_stages.go」——阶段体搬进了后者，但被钉的这段
	// 分支结构一字未动。★ 与 ops 侧那两条静态钉（sameVolume 调用点、M91 三腿
	// 接线计数）同一族：钉的是**形状**，不是物理位置。
	var src []byte
	for _, name := range []string{"pipeline.go", "pipeline_stages.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		src = append(src, b...)
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
