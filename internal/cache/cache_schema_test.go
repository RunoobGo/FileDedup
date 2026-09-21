package cache

// M6-P2（2026-09-21）缓存表列集合的钉子用例。
//
// 设计稿 §3.2.1 定的口径是"实占（Actual）**不进缓存表**"：它与内容正确性无关，
// 每加一列都要走一次整库作废（本包的版本策略是无版本标记即作废），换来的只是
// 一个每次遍历都能现读的平台值。这条决定如果没有对应用例，就只是注释里的愿望——
// 下一次"顺手把新字段也缓存一下"的提交会静默通过所有其它测试。
//
// 因此这里把列集合钉成一份显式清单：新增列必须同时在这里出现并写清理由，
// 否则本用例红（§3.3 变异 M-P2-d）。

import (
	"sort"
	"testing"
)

func TestHashCacheColumnSetIsExactly(t *testing.T) {
	c := openTest(t)
	rows, err := c.db.Query(`PRAGMA table_info(hash_cache)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    *string
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"path", "size", "mtime_ns", "partial", "full",
		"last_hit", "dev", "ino", "ctime_ns",
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("hash_cache 列集合变了：got=%v want=%v\n"+
			"加列前先回答：这个字段是不是判定依据？实占（Actual）不是——它是每次遍历\n"+
			"都能现读的平台值，进表只会让缓存版本作废、首扫退化（见本文件头与设计稿 §3.2.1）。",
			got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hash_cache 列集合变了：got=%v want=%v", got, want)
		}
	}
}
