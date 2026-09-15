package filter

import (
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
			if got := Apply(c.f, c.file, c.rel, c.size); got != c.want {
				t.Fatalf("Apply(%q) = %v, want %v", c.file, got, c.want)
			}
		})
	}
}
