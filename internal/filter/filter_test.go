package filter

import (
	"fmt"
	"strings"
	"testing"

	"filededup/internal/model"
)

func TestApply(t *testing.T) {
	cases := []struct {
		name string
		f    *model.Filters
		file string
		rel  string
		size uint64
		want bool
	}{
		{"nil 过滤器=全通过", nil, "a.txt", "a.txt", 100, true},
		{"MinSize 过滤", &model.Filters{MinSize: 200}, "a.txt", "a.txt", 100, false},
		{"MinSize 边界等于", &model.Filters{MinSize: 100}, "a.txt", "a.txt", 100, true},
		{"MaxSize=0 不限", &model.Filters{MaxSize: 0}, "a.txt", "a.txt", 999999, true},
		{"MaxSize 过滤", &model.Filters{MaxSize: 50}, "a.txt", "a.txt", 100, false},
		{"隐藏文件默认跳过", &model.Filters{}, ".DS_Store", ".DS_Store", 10, false},
		{"隐藏文件显式包含", &model.Filters{IncludeHidden: true}, ".hidden", ".hidden", 10, true},
		{"IncludeExts 空=全部", &model.Filters{}, "x.bin", "x.bin", 10, true},
		{"IncludeExts 命中", &model.Filters{IncludeExts: []string{".JPG"}}, "p.jpg", "p.jpg", 10, true},
		{"IncludeExts 未命中", &model.Filters{IncludeExts: []string{".jpg"}}, "p.png", "p.png", 10, false},
		{"ExcludeExts 命中", &model.Filters{ExcludeExts: []string{".tmp"}}, "a.tmp", "a.tmp", 10, false},
		{"ExcludeExts 大小写不敏感", &model.Filters{ExcludeExts: []string{".TMP"}}, "a.tmp", "a.tmp", 10, false},
		{"段 glob 排除", &model.Filters{ExcludePaths: []string{"node_modules"}}, "f.js", "web/node_modules/x/f.js", 10, false},
		{"段 glob 不命中", &model.Filters{ExcludePaths: []string{"target"}}, "f.js", "web/src/f.js", 10, true},
		{"前缀式排除 dir/**", &model.Filters{ExcludePaths: []string{"build/**"}}, "f.o", "build/x/f.o", 10, false},
		{"前缀式不命中", &model.Filters{ExcludePaths: []string{"build/**"}}, "f.o", "src/build/f.o", 10, true}, // src/build 不是根 build
		{"文件名段命中 *.tmp", &model.Filters{ExcludePaths: []string{"*.tmp"}}, "a.tmp", "d/a.tmp", 10, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Compile(c.f).Apply(c.file, c.rel, c.size); got != c.want {
				t.Fatalf("Apply(%q) = %v, want %v", c.file, got, c.want)
			}
		})
	}
}

// ---------- G3：扩展名匹配由线性 EqualFold 改为预编译集合 ----------

// naiveContainsExt 改造前的实现，作为语义基准。
func naiveContainsExt(list []string, ext string) bool {
	for _, e := range list {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}

// G3 等价性：新集合语义必须与旧 EqualFold 线性比较逐例一致，
// 且覆盖「短列表走线性」「长列表走 map」两条分支（阈值 extSetMapMin）。
func TestExtSetEquivalence(t *testing.T) {
	lists := [][]string{
		nil,
		{},
		{""},
		{".jpg"},
		{".JPG"},
		{".jpg", ".png"},
		{".JPG", "PNG", ".Gif"},
		{".tmp", ".tmp"},       // 重复项
		{".a", "", ".b"},       // 含空串
		{"png", ".PNG", "PNG"}, // 无点 + 大小写混杂
	}
	// 长列表：强制走 map 分支
	long := make([]string, 0, 12)
	for i := 0; i < 11; i++ {
		long = append(long, fmt.Sprintf(".ext%02d", i))
	}
	long = append(long, ".JPG")
	lists = append(lists, long)

	exts := []string{"", ".jpg", ".JPG", ".png", ".gif", ".tmp", ".a", ".b", "png", ".tXt", ".x",
		".ext00", ".ext10", ".EXT05"}
	for li, list := range lists {
		s := newExtSet(list)
		branch := "linear"
		if s.set != nil {
			branch = "map"
		}
		if len(list) > 0 && len(list) < extSetMapMin && branch != "linear" {
			t.Fatalf("list[%d] 长度 %d 应走线性分支，实际 %s", li, len(list), branch)
		}
		if len(list) >= extSetMapMin && branch != "map" {
			t.Fatalf("list[%d] 长度 %d 应走 map 分支，实际 %s", li, len(list), branch)
		}
		for _, ext := range exts {
			lower := strings.ToLower(ext) // 与其他调用方一致（Apply 内已 ToLower）
			want := naiveContainsExt(list, lower)
			if got := s.has(lower); got != want {
				t.Fatalf("list[%d]=%v ext=%q: has=%v naive=%v", li, list, ext, got, want)
			}
		}
	}
	// 空列表必须为 inactive（不启用该方向过滤）
	if !newExtSet(nil).inactive() || !newExtSet([]string{}).inactive() {
		t.Fatal("空列表应为 inactive")
	}
	if newExtSet([]string{""}).inactive() {
		t.Fatal("含空串的非空列表不应为 inactive")
	}
}

// G3 回归：扩展名判定不得随列表长度增加而分配。
func TestExtMatchNoAllocs(t *testing.T) {
	long := make([]string, 0, 64)
	for i := 0; i < 63; i++ {
		long = append(long, fmt.Sprintf(".ext%02d", i))
	}
	long = append(long, ".jpg")
	m := Compile(&model.Filters{ExcludeExts: long})
	if m.Apply("p.jpg", "p.jpg", 10) {
		t.Fatal("应被 ExcludeExts 命中而跳过")
	}
	if n := testing.AllocsPerRun(200, func() { _ = m.Apply("p.jpg", "p.jpg", 10) }); n != 0 {
		t.Fatalf("长列表 Apply 每次分配 %.1f 次，期望 0", n)
	}
	// 短列表分支同样零分配
	sm := Compile(&model.Filters{ExcludeExts: []string{".tmp", ".log", ".jpg"}})
	if n := testing.AllocsPerRun(200, func() { _ = sm.Apply("p.jpg", "p.jpg", 10) }); n != 0 {
		t.Fatalf("短列表 Apply 每次分配 %.1f 次，期望 0", n)
	}
}

// BenchmarkExtMatch 为 extSetMapMin 阈值提供依据：
// 逐档对比「新线性」（列表已预小写，直接 == 比较）与「新 map」（哈希查表），
// 取两者交叉点作为阈值。命中项固定在列表末位（线性扫描最坏情况）。
func BenchmarkExtMatch(b *testing.B) {
	for _, n := range []int{1, 2, 4, 6, 8, 12, 16, 32, 64} {
		lower := make([]string, 0, n)
		for i := 0; i < n-1; i++ {
			lower = append(lower, fmt.Sprintf(".ext%02d", i))
		}
		lower = append(lower, ".jpg") // 末位命中

		lin := extSet{list: lower}
		mp := make(map[string]struct{}, n)
		for _, e := range lower {
			mp[e] = struct{}{}
		}
		m := extSet{set: mp}
		old := lower // 旧实现：大小写不敏感比较，元素未预小写

		b.Run(fmt.Sprintf("N%02d/NewLinear", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = lin.has(".jpg")
			}
		})
		b.Run(fmt.Sprintf("N%02d/NewMap", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.has(".jpg")
			}
		})
		b.Run(fmt.Sprintf("N%02d/OldEqualFold", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = naiveContainsExt(old, ".jpg")
			}
		})
	}
}
