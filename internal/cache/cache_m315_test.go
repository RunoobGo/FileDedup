package cache

// M315（CACHE-12，2026-09-28 第六轮审查批 P0）：Store 的 full 分支曾经是
// "来行没带 full 就一律保留库存值"（R-缓存-1 为"同代不误清"加的 CASE）。
// 缺的是**代际**这一半：等长原地改写之后那次 `Full=nil` 写回，会把上一代的 full
// 留在新代际的 size/mtime/partial 旁边 ⇒ 命中腿（dedup 阶段 2）四道全过、照单沿用，
// 缓存里出现跨代混排行。本文件钉"守卫只拦跨代、不拦同代"这条判据本身；
// 端到端后果（真重复对因此漏报）见 internal/dedup 的三扫集成用例。
// 同代的保留行为由 cache_preserve_full_r1_test.go 继续钉，两条必须同时在场。

import (
	"bytes"
	"path/filepath"
	"testing"

	"filededup/internal/fsid"
)

func m315Open(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func m315Hash(seed byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = seed
	}
	return b
}

// 判据本体：mtime 与采样都换了一代，来行不带 full ⇒ 库存 full 必须清空。
func TestM315StoreDropsStaleFullWhenGenerationChanged(t *testing.T) {
	c := m315Open(t)
	path := "/gen/a.bin"

	first := Entry{Path: path, Size: 100, MtimeNs: 1, Head: 11, Tail: 22, Mid1: 33, Mid2: 44,
		Full: m315Hash(0x01)}
	if err := c.Store([]Entry{first}); err != nil {
		t.Fatal(err)
	}

	// 第二轮：等长原地改写（size 不变、mtime 与四点采样都变），本轮没算 full。
	second := Entry{Path: path, Size: 100, MtimeNs: 2, Head: 55, Tail: 66, Mid1: 77, Mid2: 88,
		Full: nil}
	if err := c.Store([]Entry{second}); err != nil {
		t.Fatal(err)
	}

	got, hit, fullValid := c.Lookup(path, 100, 2, fsid.ID{})
	if !hit {
		t.Fatal("前提不成立：本轮应按 size/mtime 命中（未命中就绕过了要钉的那一格）")
	}
	if fullValid || len(got.Full) != 0 {
		t.Fatalf("M315：跨代际写回把上一代的 full 留了下来（fullValid=%v len=%d）", fullValid, len(got.Full))
	}
	// 元数据与采样必须是新代际的——守卫只清 full，不许顺手把别的臂也作废。
	if got.MtimeNs != 2 || got.Head != 55 || got.Tail != 66 || got.Mid1 != 77 || got.Mid2 != 88 {
		t.Fatalf("新代际的 mtime/partial 未写回： %+v", got)
	}
}

// 三臂各自单独足以判定跨代：只 mtime 变、只 partial 变（size 始终不变）。
func TestM315GuardArmsAreEachSufficient(t *testing.T) {
	cases := []struct {
		name              string
		newMtime          int64
		newHead           uint64
		keepPartialEqual  bool
		wantFullPreserved bool
	}{
		{name: "mtime 变、采样不变", newMtime: 2, newHead: 11, keepPartialEqual: true, wantFullPreserved: false},
		{name: "采样变、mtime 不变", newMtime: 1, newHead: 99, keepPartialEqual: false, wantFullPreserved: false},
		{name: "两臂都不变（同代）", newMtime: 1, newHead: 11, keepPartialEqual: true, wantFullPreserved: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := m315Open(t)
			path := "/gen/arms.bin"
			full := m315Hash(0x07)
			base := Entry{Path: path, Size: 200, MtimeNs: 1, Head: 11, Tail: 22, Mid1: 33, Mid2: 44, Full: full}
			if err := c.Store([]Entry{base}); err != nil {
				t.Fatal(err)
			}
			next := base
			next.Full = nil
			next.MtimeNs = tc.newMtime
			next.Head = tc.newHead
			if err := c.Store([]Entry{next}); err != nil {
				t.Fatal(err)
			}
			_, hit, fullValid := c.Lookup(path, 200, tc.newMtime, fsid.ID{})
			if !hit {
				t.Fatalf("前提不成立：应命中（hit=%v）", hit)
			}
			if fullValid != tc.wantFullPreserved {
				t.Fatalf("full 保留 = %v，应为 %v（跨代清空 / 同代保留）", fullValid, tc.wantFullPreserved)
			}
		})
	}
}

// 来行"这轮压根没采样"（partial 全零）+ size/mtime 同代 ⇒ 不许因为信息缺失就丢掉 full。
func TestM315AllZeroIncomingPartialKeepsSameGenerationFull(t *testing.T) {
	c := m315Open(t)
	path := "/gen/nosample.bin"
	base := Entry{Path: path, Size: 300, MtimeNs: 5, Head: 11, Tail: 22, Mid1: 33, Mid2: 44,
		Full: m315Hash(0x0A)}
	if err := c.Store([]Entry{base}); err != nil {
		t.Fatal(err)
	}
	next := base
	next.Full = nil
	next.Head, next.Tail, next.Mid1, next.Mid2 = 0, 0, 0, 0 // encodePartial 全零 = 本轮无采样
	if err := c.Store([]Entry{next}); err != nil {
		t.Fatal(err)
	}
	got, hit, fullValid := c.Lookup(path, 300, 5, fsid.ID{})
	if !hit {
		t.Fatal("前提不成立：应命中")
	}
	if !fullValid {
		t.Fatal("同代际 + 本轮无采样 ⇒ full 不该被清（守卫过头会把 R-缓存-1 的战果吐回去）")
	}
	if !bytes.Equal(got.Full, base.Full) {
		t.Fatalf("full 内容不符：%x", got.Full)
	}
	// 全零来行不该把库存采样冲掉（partial 自己的 CASE，R-缓存-1 同批语义）。
	if got.Head != 11 || got.Mid2 != 44 {
		t.Fatalf("库存 partial 被全零来行覆盖： %+v", got)
	}
}

// 来行自带 full ⇒ 一律覆盖，守卫不参与（否则新算出的真值会被旧值挡住）。
func TestM315FreshFullAlwaysWins(t *testing.T) {
	c := m315Open(t)
	path := "/gen/fresh.bin"
	if err := c.Store([]Entry{{Path: path, Size: 400, MtimeNs: 1,
		Head: 1, Tail: 2, Mid1: 3, Mid2: 4, Full: m315Hash(0x0B)}}); err != nil {
		t.Fatal(err)
	}
	fresh := m315Hash(0x0C)
	if err := c.Store([]Entry{{Path: path, Size: 400, MtimeNs: 2,
		Head: 5, Tail: 6, Mid1: 7, Mid2: 8, Full: fresh}}); err != nil {
		t.Fatal(err)
	}
	got, hit, fullValid := c.Lookup(path, 400, 2, fsid.ID{})
	if !hit || !fullValid || !bytes.Equal(got.Full, fresh) {
		t.Fatalf("跨代际但自带新 full ⇒ 必须覆盖：hit=%v fullValid=%v full=%x", hit, fullValid, got.Full)
	}
}
