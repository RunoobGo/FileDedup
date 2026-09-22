package main

// P3 回归：imageMime 表与已注册解码器必须一致。
// 修正前 .webp/.bmp/.svg 列在表内却无解码器，预览只会得到"图片解码失败"。

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"filededup/internal/model"
	"golang.org/x/image/bmp"
	_ "golang.org/x/image/webp" // 确认 webp 解码器可注册
)

// webpFixtureB64：x/image@v0.46.0/testdata/gopher-doc.1bpp.lossless.webp（442 B，
// 该目录里最小的一个）的 base64。x/image/webp **没有编码器**，标准库也没有，
// 所以这一项只能带真字节夹具（原因见 TestImageMimeAllDecodable 的注释）。
// 许可：BSD-3-Clause，与 golang.org/x/image 同。
const webpFixtureB64 = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF" +
	"8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyP" +
	"L+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN" +
	"34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func previewFor(t *testing.T, name string, data []byte) PreviewData {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.byID = map[uint64]*model.FileEntry{
		1: {ID: 1, Path: p, Size: uint64(len(data)), Ext: filepath.Ext(name)},
	}
	got, err := a.PreviewFile(1)
	if err != nil {
		t.Fatalf("PreviewFile(%s): %v", name, err)
	}
	return got
}

// 每个 imageMime 项都必须能被解码为 image（否则应从表中移除）。
//
// ★ M143（04 §6.11 TST-5，第 3 轮 §24.4）：本条改前硬写 `exts := []string{".png", ".bmp"}`，
// 而生产表有 6 项 ⇒ 表与被测对象**脱钩**。往 imageMime 加一项却忘了注册解码器，
// 本条照绿——而这正是 app.go:1039-1041 那句 P3 承诺要防的事（当年 .webp/.bmp/.svg
// 就是这么漏进去的）。现在改由生产表驱动，两侧键集合必须**相等**：
//   - 表里加一项没有夹具 ⇒ 红（"夹具没跟上"就是"解码器可能没跟上"的同一种漏法）；
//   - 表里删一项而夹具还留着 ⇒ 也红（否则本表会随时间悄悄缩水）。
//
// webp 那段字节是 x/image/testdata 里最小的一个（gopher-doc.1bpp.lossless.webp，442 B）
// 转成 base64 内联：**不能**用 RIFF 头桩——实测 image.DecodeConfig 对桩返回
// format="webp" 但 err="vp8: invalid format"，PreviewFile 于是给 kind="binary"
// （"图片解码失败"），那样这条腿就成了假绿。
func TestImageMimeAllDecodable(t *testing.T) {
	fixtures := map[string]struct {
		data   []byte
		format string // image.DecodeConfig 应回报的格式名
	}{
		".png":  {encodeFixture(t, "png"), "png"},
		".jpg":  {encodeFixture(t, "jpeg"), "jpeg"},
		".jpeg": {encodeFixture(t, "jpeg"), "jpeg"},
		".gif":  {encodeFixture(t, "gif"), "gif"},
		".bmp":  {encodeFixture(t, "bmp"), "bmp"},
		".webp": {mustDecodeB64(t, webpFixtureB64), "webp"},
	}

	// ---- 集合相等（两个方向都要判）----
	for e := range fixtures {
		if _, ok := imageMime[e]; !ok {
			t.Errorf("夹具里的 %s 不在 imageMime 中：表缩了而本条没跟着缩 ⇒ 下次加回来时没人核", e)
		}
	}
	for e := range imageMime {
		if _, ok := fixtures[e]; !ok {
			t.Errorf("imageMime 有 %s 却没有可解码夹具 ⇒ 本条从未验过它（M143 的原始缺陷形状）", e)
		}
	}
	// 条数下界：集合相等在"两边同时被删空"时仍成立（M146 同一课）。
	if len(imageMime) < 6 {
		t.Fatalf("imageMime 只剩 %d 项（本轮取证时为 6）⇒ 表被删空式的通过不算通过", len(imageMime))
	}

	// ---- 逐格式判据：MIME 值、解码器注册、端到端预览 ----
	for e, fx := range fixtures {
		if got := imageMime[e]; got != "image/"+fx.format {
			t.Errorf("%s 的 MIME 表值 = %q，want \"image/%s\"（夹具与表项对不上）", e, got, fx.format)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(fx.data))
		if err != nil {
			t.Errorf("%s 夹具 DecodeConfig 失败: %v ⇒ 解码器未注册或夹具坏了", e, err)
			continue
		}
		if format != fx.format {
			t.Errorf("%s DecodeConfig 回报格式 %q，want %q（注册的是另一个解码器？）", e, format, fx.format)
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			t.Errorf("%s 夹具尺寸异常: %dx%d", e, cfg.Width, cfg.Height)
		}
		if got := previewFor(t, "x"+e, fx.data); got.Kind != "image" {
			t.Errorf("%s 预览 kind=%s content=%q，期望 image（解码器未注册？）", e, got.Kind, firstChars(got.Content, 48))
		}
	}
}

// TestImageMimeDecoderImportsAreInApp 是 M143 的**第二条腿**，因为第一条腿单独不 decisive。
//
// 真读数（第 3 轮，driver dr_m143.py）：把 app.go:27 的 `_ "image/gif"` 删掉，
// TestImageMimeAllDecodable **仍然全绿**（rc=0）。原因不是判据写错，而是它在验一件
// 本文件自己造成的事实——上面那条用例为了造 GIF 夹具而 `import "image/gif"`，
// 而 `image.RegisterFormat` 是**进程级**全局注册表：测试二进制里任何一处导入都会
// 把格式注册上。⇒ "DecodeConfig 认得 gif" 这件事被被测物之外的力量保证了。
//
// 所以 P3 那句承诺（"表内每一项都必须在下方有对应解码器"）必须**按字面**钉：
// 直接读 app.go 的 import 块，要求每个表项的解码器包都在里面。这一腿删一行就红。
func TestImageMimeDecoderImportsAreInApp(t *testing.T) {
	// 包名由 MIME 去掉 "image/" 前缀得到，但注册位置分两处：标准库与 golang.org/x/image。
	// 这张映射是本条自己的知识，不复用 imageMime 的值（否则表错了这里也错，等于没验）。
	pkgFor := map[string]string{
		"png":  "image/png",
		"jpeg": "image/jpeg",
		"gif":  "image/gif",
		"webp": "golang.org/x/image/webp",
		"bmp":  "golang.org/x/image/bmp",
	}
	file, err := parser.ParseFile(token.NewFileSet(), "app.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("读不出 app.go 的 import 块（文件改名？工作目录不对？）: %v", err)
	}
	imported := map[string]bool{}
	for _, imp := range file.Imports {
		if p, uerr := strconv.Unquote(imp.Path.Value); uerr == nil {
			imported[p] = true
		}
	}
	if len(imported) == 0 {
		t.Fatal("import 块解析出 0 条 ⇒ 本条等于没跑")
	}
	checked := 0
	for ext, mime := range imageMime {
		name := strings.TrimPrefix(mime, "image/")
		pkg, ok := pkgFor[name]
		if !ok {
			t.Errorf("表项 %s 的 MIME %q 认不出对应的解码器包 ⇒ 新增格式时要把包名补进本条的映射", ext, mime)
			continue
		}
		checked++
		if !imported[pkg] {
			t.Errorf("表项 %s（%s）承诺了 %q 的解码器，而 app.go 没有导入它 ⇒ PreviewFile 会对这个扩展名报「图片解码失败」（P3/M143）",
				ext, mime, pkg)
		}
	}
	if checked != len(imageMime) {
		t.Fatalf("只核了 %d 项而表有 %d 项 ⇒ 有表项没被走到", checked, len(imageMime))
	}
}

// encodeFixture 为每种位图各生成一段**真能解码**的小图（8x8 渐变，非随机噪声：
// 噪声会让 gif/bmp 的夹具体积没必要地大）。
func encodeFixture(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 30), uint8(y * 30), 128, 255})
		}
	}
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "bmp":
		err = bmp.Encode(&buf, img)
	default:
		t.Fatalf("没有 %s 的编码器（webp 无编码器，用内联夹具）", format)
	}
	if err != nil {
		t.Fatalf("%s 编码失败: %v", format, err)
	}
	return buf.Bytes()
}

func mustDecodeB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("内联夹具不是合法 base64: %v", err)
	}
	return b
}

func firstChars(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// webp 解码器必须可注册（无编码器的标准实现，仅验证 Decode 能识别头部）
func TestWebpDecoderRegistered(t *testing.T) {
	// RIFF <len> WEBP VP8 <chunklen> —— 长度字段需自洽，否则解码器在
	// "识别格式"之前就因短数据报错，测不到注册与否。
	var w bytes.Buffer
	w.WriteString("RIFF")
	binary.Write(&w, binary.LittleEndian, uint32(4+8+10))
	w.WriteString("WEBP")
	w.WriteString("VP8 ")
	binary.Write(&w, binary.LittleEndian, uint32(10))
	w.Write(make([]byte, 10))
	if _, _, err := image.Decode(bytes.NewReader(w.Bytes())); err != nil {
		// 关键判据：不得是 "unknown format"（未注册），格式相关的报错说明解码器已接管
		if strings.Contains(err.Error(), "unknown format") {
			t.Fatalf("webp 解码器未注册: %v", err)
		}
		t.Logf("webp 已注册（数据不完整导致解码失败，属预期）: %v", err)
	}
	if _, ok := imageMime[".webp"]; !ok {
		t.Error(".webp 应在 imageMime 中")
	}
}

// SVG 已移出位图表 → 落到文本分支显示源码（前端不再以 data: URL 注入，避免脚本执行）
func TestSvgFallsBackToText(t *testing.T) {
	if _, ok := imageMime[".svg"]; ok {
		t.Fatal(".svg 不应留在 imageMime（无位图解码器）")
	}
	src := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`
	got := previewFor(t, "logo.svg", []byte(src))
	if got.Kind != "text" {
		t.Fatalf("SVG 应以文本预览, kind=%s content=%q", got.Kind, got.Content)
	}
	if !strings.Contains(got.Content, "<svg") {
		t.Fatal("SVG 源码内容丢失")
	}
}

// 极窄长图不得触发除零 panic（maxPixels 放行但缩放比会退化为 0）
func TestThumbnailExtremeAspectNoPanic(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 1, 100000)) // 10 万像素，低于上限
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := thumbnail(buf.Bytes(), 512)
	if err != nil {
		// 明确报错可以接受，panic 不行（本函数在扫描 goroutine 之外但仍在主进程）
		t.Logf("返回错误（可接受）: %v", err)
		return
	}
	if len(out) == 0 {
		t.Fatal("缩略图为空")
	}
	// 校验输出可解码且尺寸合理
	nimg, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("输出不可解码: %v", err)
	}
	w, h := nimg.Bounds().Dx(), nimg.Bounds().Dy()
	if w <= 0 || h <= 0 || w > 512 || h > 512 {
		t.Fatalf("缩略图尺寸异常: %dx%d", w, h)
	}
}

// 解压炸弹经 PreviewFile 出口时，用户必须看到"尺寸过大"而非无信息量的失败提示。
// （像素预检本身由 app_test.go:TestThumbnailPixelBomb 覆盖，此处补契约层）
func TestThumbnailBombRejectedViaPreview(t *testing.T) {
	got := previewFor(t, "bomb.png", buildPng(100000, 100000))
	if got.Kind == "image" {
		t.Fatal("解压炸弹图片不应生成缩略图")
	}
	if !strings.Contains(got.Content, "尺寸过大") {
		t.Fatalf("应给出尺寸过大提示, got kind=%s content=%q", got.Kind, got.Content)
	}
}
