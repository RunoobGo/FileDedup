package main

// M2-T02 契约实现单测：分页 / 排序 / 筛选 / 保留建议 / 设置回环 / 预览判定。

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.cfgDir = t.TempDir()
	return a
}

func mkGroup(id uint64, size uint64, paths ...string) *model.DuplicateGroup {
	g := &model.DuplicateGroup{GroupID: id, Files: make([]*model.FileEntry, 0, len(paths))}
	for i, p := range paths {
		g.Files = append(g.Files, &model.FileEntry{
			ID: id*100 + uint64(i), Path: p, Size: size,
			Ext: filepath.Ext(p), ModTime: int64(id),
		})
	}
	g.Reclaimable = size * uint64(len(paths)-1)
	return g
}

func TestGetResultGroupsPaging(t *testing.T) {
	a := newTestApp(t)
	for i := 0; i < 250; i++ {
		a.groups = append(a.groups, mkGroup(uint64(i), 100, "a.bin", "b.bin"))
	}
	// 默认页大小 100
	r, err := a.GetResultGroups(ResultQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 250 || len(r.Groups) != 100 || r.Page != 0 {
		t.Fatalf("第 1 页: total=%d len=%d page=%d", r.Total, len(r.Groups), r.Page)
	}
	r, _ = a.GetResultGroups(ResultQuery{Page: 2})
	if len(r.Groups) != 50 {
		t.Fatalf("第 3 页应为 50 组, got %d", len(r.Groups))
	}
	// 越界页返回空而非错误
	r, _ = a.GetResultGroups(ResultQuery{Page: 5})
	if len(r.Groups) != 0 {
		t.Fatalf("越界页应为空")
	}
	// 自定义页大小上限
	r, _ = a.GetResultGroups(ResultQuery{PageSize: 999})
	if len(r.Groups) != 250 {
		t.Fatalf("页大小超限时应取全部 250, got %d", len(r.Groups))
	}
}

func TestGetResultGroupsSort(t *testing.T) {
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{
		mkGroup(1, 100, "a1", "b1", "c1"),       // reclaim 200, count 3
		mkGroup(2, 500, "a2", "b2"),             // reclaim 500, count 2
		mkGroup(3, 300, "a3", "b3", "c3", "d3"), // reclaim 900, count 4
	}
	// 按 reclaimable：900, 500, 200
	r, _ := a.GetResultGroups(ResultQuery{})
	if r.Groups[0].GroupID != 3 || r.Groups[2].GroupID != 1 {
		t.Fatalf("reclaimable 排序错误: %d %d %d", r.Groups[0].GroupID, r.Groups[1].GroupID, r.Groups[2].GroupID)
	}
	// 按 size：500, 300, 100
	r, _ = a.GetResultGroups(ResultQuery{Sort: "size"})
	if r.Groups[0].GroupID != 2 || r.Groups[2].GroupID != 1 {
		t.Fatalf("size 排序错误")
	}
	// 按 count：4, 3, 2
	r, _ = a.GetResultGroups(ResultQuery{Sort: "count"})
	if r.Groups[0].GroupID != 3 || r.Groups[2].GroupID != 2 {
		t.Fatalf("count 排序错误")
	}
}

// BenchmarkGetResultGroupsPaged Y3 收益量化：5000 组的滚动分页查询。
// Cached = 修复后（排序结果复用）；RebuildEach = 修复前（每次翻页全量重排）。
func BenchmarkGetResultGroupsPaged(b *testing.B) {
	a := NewApp()
	a.cfgDir = b.TempDir()
	for i := 0; i < 5000; i++ {
		a.groups = append(a.groups, mkGroup(uint64(i+1), uint64(100+i%97),
			"x/a.dat", "y/b.dat", "z/c.dat"))
	}
	q := func(i int) ResultQuery { return ResultQuery{Page: i % 40, PageSize: 100} }

	b.Run("Cached", func(b *testing.B) {
		if _, err := a.GetResultGroups(q(0)); err != nil { // 预热
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := a.GetResultGroups(q(i)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("RebuildEach", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			a.mu.Lock()
			a.invalidateViewCacheLocked() // 模拟修复前：每次翻页都重新筛选+排序
			a.mu.Unlock()
			if _, err := a.GetResultGroups(q(i)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// TestGetResultGroupsCacheCorrectness Y3：排序缓存复用必须正确，
// 且结果集发生任何结构变更（含原地修改成员数）后旧缓存必须失效。
func TestGetResultGroupsCacheCorrectness(t *testing.T) {
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{
		mkGroup(1, 100, "a", "b"),      // reclaim 100
		mkGroup(2, 500, "c", "d"),      // reclaim 500
		mkGroup(3, 900, "e", "f", "g"), // reclaim 1800
	}
	// 首次查询：建缓存（reclaimable 降序 → g3, g2, g1）
	r1, err := a.GetResultGroups(ResultQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Total != 3 || r1.Groups[0].GroupID != 3 || r1.Groups[2].GroupID != 1 {
		t.Fatalf("首查排序错误: total=%d first=%d", r1.Total, r1.Groups[0].GroupID)
	}
	if len(a.viewCache) != 1 {
		t.Fatalf("缓存条目 = %d, want 1", len(a.viewCache))
	}
	// 同键复用：结果一致且不新增缓存条目
	r2, _ := a.GetResultGroups(ResultQuery{})
	if r2.Total != 3 || r2.Groups[0].GroupID != 3 || len(a.viewCache) != 1 {
		t.Fatalf("缓存复用异常: total=%d cache=%d", r2.Total, len(a.viewCache))
	}
	// 不同键各自缓存
	if _, err := a.GetResultGroups(ResultQuery{Sort: "size"}); err != nil {
		t.Fatal(err)
	}
	if len(a.viewCache) != 2 {
		t.Fatalf("不同键应各建一条缓存, got %d", len(a.viewCache))
	}
	// 故意不显式失效：原地缩减某组成员数 → 指纹须变化使缓存失效
	a.groups[0].Files = a.groups[0].Files[:1] // g1 由 2 文件变 1 文件
	a.groups[0].Reclaimable = 0
	r3, _ := a.GetResultGroups(ResultQuery{})
	if r3.Groups[0].GroupID != 3 {
		t.Fatalf("首组应仍为 g3, got %d", r3.Groups[0].GroupID)
	}
	// g1 现只剩 1 个文件：视图文件数须反映最新状态（陈旧缓存会给出 2）
	found := false
	for _, gv := range r3.Groups {
		if gv.GroupID == 1 {
			found = true
			if len(gv.Files) != 1 {
				t.Fatalf("g1 视图文件数 = %d, want 1（缓存未失效）", len(gv.Files))
			}
		}
	}
	if !found {
		t.Fatal("未找到 g1")
	}
	// 新增组 → 亦须失效
	a.groups = append(a.groups, mkGroup(4, 2000, "h", "i"))
	r4, _ := a.GetResultGroups(ResultQuery{})
	if r4.Total != 4 || r4.Groups[0].GroupID != 4 {
		t.Fatalf("新增组后未失效: total=%d first=%d", r4.Total, r4.Groups[0].GroupID)
	}
	// 空结果集边界
	a.groups = nil
	r5, _ := a.GetResultGroups(ResultQuery{})
	if r5.Total != 0 || len(r5.Groups) != 0 {
		t.Fatalf("空结果集应为 0 组")
	}
}

func TestGetResultGroupsExtFilter(t *testing.T) {
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{
		mkGroup(1, 100, "a.jpg", "b.jpg"),
		mkGroup(2, 100, "c.png", "d.png"),
		mkGroup(3, 100, "e.jpg", "f.txt"),
	}
	r, _ := a.GetResultGroups(ResultQuery{Ext: ".jpg"})
	if r.Total != 2 {
		t.Fatalf("jpg 筛选应为 2 组, got %d", r.Total)
	}
	// 无点后缀自动补
	r, _ = a.GetResultGroups(ResultQuery{Ext: "png"})
	if r.Total != 1 {
		t.Fatalf("png 筛选应为 1 组")
	}
}

func TestToGroupViewKeepShortest(t *testing.T) {
	g := mkGroup(1, 42, "/very/long/path/deep/file.bin", "/a/b.bin", "/mid/file.bin")
	v := toGroupView(g, nil, nil)
	keep := -1
	for i, f := range v.Files {
		if f.IsKeep {
			keep = i
		}
	}
	if keep != 1 {
		t.Fatalf("保留建议应为路径最短（索引 1），got %d", keep)
	}
	if v.Size != 42 || v.Reclaimable != 84 {
		t.Fatalf("视图字段错误: %+v", v)
	}
}

func TestSettingsRoundtrip(t *testing.T) {
	a := newTestApp(t)
	s := Settings{Threads: 8, Theme: "dark", Language: "zh",
		FiltersDefault: model.Filters{MinSize: 1024, IncludeExts: []string{".jpg"}}}
	if _, err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	got := a.GetSettings()
	if got.Threads != 8 || got.Theme != "dark" || got.FiltersDefault.MinSize != 1024 {
		t.Fatalf("设置回读不一致: %+v", got)
	}
	// 非法值修正
	if _, err := a.SaveSettings(Settings{Theme: "hacker"}); err != nil {
		t.Fatal(err)
	}
	if a.GetSettings().Theme != "system" {
		t.Fatal("非法主题应回退 system")
	}
}

func TestPreviewFileTextAndHex(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	txt := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(txt, []byte("hello 文本内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(bin, []byte{0x00, 0x01, 0xFF, 0xFE, 0x80, 0x7F}, 0o644); err != nil {
		t.Fatal(err)
	}
	a.byID[1] = &model.FileEntry{ID: 1, Path: txt, Size: 16, Ext: ".txt"}
	a.byID[2] = &model.FileEntry{ID: 2, Path: bin, Size: 6, Ext: ".bin"}

	p, err := a.PreviewFile(1)
	if err != nil || p.Kind != "text" {
		t.Fatalf("文本判定失败: %+v err=%v", p, err)
	}
	p, err = a.PreviewFile(2)
	if err != nil || p.Kind != "hex" {
		t.Fatalf("HEX 判定失败: %+v err=%v", p, err)
	}
	if _, err := a.PreviewFile(999); err == nil {
		t.Fatal("未知 id 应返回错误")
	}
}

func TestIsLikelyText(t *testing.T) {
	if !isLikelyText([]byte("regular text\nwith lines\r\n\ttabs")) {
		t.Fatal("正常文本应判定为 text")
	}
	if isLikelyText([]byte{0x00, 0xFF, 0x01, 0x02, 0xFE}) {
		t.Fatal("二进制应判定非 text")
	}
}

func TestApplyKeepPolicyReal(t *testing.T) {
	// M3-T01：newest 策略保留 mtime 最新
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{mkGroup(1, 100, "/a/old.bin", "/b/new.bin", "/c/mid.bin")}
	a.groups[0].Files[0].ModTime = 1000
	a.groups[0].Files[1].ModTime = 3000
	a.groups[0].Files[2].ModTime = 2000
	oc, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "newest"})
	if err != nil {
		t.Fatal(err)
	}
	ds := oc.Decisions
	if len(ds) != 1 || ds[0].KeepID != a.groups[0].Files[1].ID {
		t.Fatalf("newest 决策错误: %+v", ds)
	}
	if len(ds[0].RemoveIDs) != 2 {
		t.Fatalf("冗余列表错误: %+v", ds[0])
	}
	// 决策生效于视图
	r, _ := a.GetResultGroups(ResultQuery{})
	if len(r.Groups) != 1 || !r.Groups[0].Files[1].IsKeep {
		t.Fatalf("视图保留标记未生效")
	}
}

func TestExecuteOperationGuards(t *testing.T) {
	// 空结果集 / 未完成任务 → 显式错误；delete 无确认在 ops 层拦截（S4 已测）
	a := newTestApp(t)
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{1}}); err == nil {
		t.Fatal("空结果集应返回错误")
	}
	if _, err := a.CacheStats(); err == nil {
		t.Fatal("CacheStats 存根应返回错误")
	}
	if a.GetVersion() == "" {
		t.Fatal("GetVersion 应返回版本号")
	}
}

func TestExecuteOperationMutualExclusion(t *testing.T) {
	// 操作执行中：拒绝并发操作与保留策略（两组 goroutine 并发写结果集会互相覆盖）
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{mkGroup(1, 100, "a", "b")}
	a.opsRunning = true // 模拟操作执行中
	if _, err := a.ExecuteOperation(model.OpRequest{Kind: "trash", FileIDs: []uint64{101}}); err == nil {
		t.Fatal("操作执行中应拒绝再次操作")
	}
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "shortest"}); err == nil {
		t.Fatal("操作执行中应拒绝保留策略")
	}
	a.opsRunning = false
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "shortest"}); err != nil {
		t.Fatalf("空闲时保留策略应可用: %v", err)
	}
}

// buildPng 构造仅含 IHDR/IEND 的最小 PNG（DecodeConfig 只读头即可判定尺寸）。
func buildPng(w, h uint32) []byte {
	chunk := func(typ string, data []byte) []byte {
		var out bytes.Buffer
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
		out.WriteString(typ)
		out.Write(data)
		crc := crc32.NewIEEE()
		_, _ = crc.Write([]byte(typ))
		_, _ = crc.Write(data)
		_ = binary.Write(&out, binary.BigEndian, crc.Sum32())
		return out.Bytes()
	}
	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 2, 0, 0, 0}) // 8bit RGB，无压缩/过滤/隔行
	var out bytes.Buffer
	out.Write([]byte("\x89PNG\r\n\x1a\n"))
	out.Write(chunk("IHDR", ihdr.Bytes()))
	out.Write(chunk("IEND", nil))
	return out.Bytes()
}

func TestThumbnailPixelBomb(t *testing.T) {
	// 解压炸弹：小体积 PNG 声明巨大尺寸 → 像素预检在 Decode 前拒绝（防 OOM）
	if _, err := thumbnail(buildPng(50000, 50000), 512); err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("解压炸弹应被像素预检拒绝: %v", err)
	}
	// Y5：16M 像素上限边界——25M 像素须拒绝
	if _, err := thumbnail(buildPng(5000, 5000), 512); err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("25M 像素应被拒绝（Y5 收紧后）: %v", err)
	}
	// 恰好 16M 像素仍被接受（通过尺寸预检，随后因缺 IDAT 报解码错误）
	if _, err := thumbnail(buildPng(4000, 4000), 512); err == nil || strings.Contains(err.Error(), "过大") {
		t.Fatalf("16M 像素不应被判为过大: %v", err)
	}
	// 正常尺寸不受影响（真实图片路径由 TestThumbnail 覆盖）
	if _, err := thumbnail(buildPng(64, 64), 512); err == nil {
		t.Fatal("缺 IDAT 的 64x64 应报解码错误而非像素错误")
	}
}

// TestPreviewFileSizeLimits Y5：源文件大小上限收紧到 20MB。
func TestPreviewFileSizeLimits(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "big.png")
	// 真实可解码 PNG（buildPng 仅含 IHDR/IEND，供像素预检用例使用）
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	// 声明体积超过 20MB → 明确提示且不读盘解码
	a.byID[1] = &model.FileEntry{ID: 1, Path: p, Size: 21 << 20, Ext: ".png"}
	got, err := a.PreviewFile(1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "binary" || !strings.Contains(got.Content, "20MB") {
		t.Fatalf("超限图片应返回尺寸提示: %+v", got)
	}
	// 20MB 以内（真实体积）→ 正常走缩略图路径
	info, _ := os.Stat(p)
	a.byID[2] = &model.FileEntry{ID: 2, Path: p, Size: uint64(info.Size()), Ext: ".png"}
	got, err = a.PreviewFile(2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "image" || got.MimeType != "image/jpeg" {
		t.Fatalf("正常图片应返回缩略图: %+v", got)
	}
}

func TestThumbnail(t *testing.T) {
	// M4-T04：缩略图（解码→缩放→JPEG，零第三方依赖）
	src := image.NewRGBA(image.Rect(0, 0, 2048, 1024))
	for y := 0; y < 1024; y++ {
		for x := 0; x < 2048; x++ {
			src.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	thumb, err := thumbnail(buf.Bytes(), 512)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(thumb))
	if err != nil || format != "jpeg" {
		t.Fatalf("缩略图格式: %s err=%v", format, err)
	}
	b := img.Bounds()
	if b.Dx() != 512 || b.Dy() != 256 {
		t.Fatalf("缩略图尺寸 = %dx%d, want 512x256", b.Dx(), b.Dy())
	}
	if len(thumb) > 100<<10 {
		t.Fatalf("缩略图过大: %d", len(thumb))
	}
	// 小图不放大
	small := image.NewRGBA(image.Rect(0, 0, 100, 50))
	buf.Reset()
	png.Encode(&buf, small)
	thumb, _ = thumbnail(buf.Bytes(), 512)
	img, _, _ = image.Decode(bytes.NewReader(thumb))
	if img.Bounds().Dx() != 100 {
		t.Fatal("小图不应放大")
	}
	// 非图片数据 → 明确错误
	if _, err := thumbnail([]byte("not an image"), 512); err == nil {
		t.Fatal("非图片应报错")
	}
}

// TestApplyKeepPolicyDirectoryValidation directory 策略必须至少一个目录。
func TestApplyKeepPolicyDirectoryValidation(t *testing.T) {
	a := newTestApp(t)
	a.groups = []*model.DuplicateGroup{mkGroup(1, 100, "/x/a.bin", "/y/b.bin")}
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "directory"}); err == nil {
		t.Fatal("空目录列表应报错")
	}
	if _, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "directory",
		Directories: []string{"  "}}); err == nil {
		t.Fatal("全空白目录列表应报错")
	}
	ds, err := a.ApplyKeepPolicy(model.KeepPolicy{Kind: "directory",
		Directories: []string{"/x"}})
	if err != nil || len(ds.Decisions) != 1 {
		t.Fatalf("有效目录应产生决策: %+v err=%v", ds, err)
	}
}
