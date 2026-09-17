package main

// P3 回归：imageMime 表与已注册解码器必须一致。
// 修正前 .webp/.bmp/.svg 列在表内却无解码器，预览只会得到"图片解码失败"。

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
	"golang.org/x/image/bmp"
	_ "golang.org/x/image/webp" // 确认 webp 解码器可注册
)

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

// 每个 imageMime 项都必须能被解码为 image（否则应从表中移除）
func TestImageMimeAllDecodable(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	encode := func(t *testing.T, format string) []byte {
		t.Helper()
		var buf bytes.Buffer
		switch format {
		case "png":
			if err := png.Encode(&buf, img); err != nil {
				t.Fatal(err)
			}
		case "bmp":
			if err := bmp.Encode(&buf, img); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("未覆盖的格式 %s", format)
		}
		return buf.Bytes()
	}
	exts := []string{".png", ".bmp"}
	for _, e := range exts {
		if _, ok := imageMime[e]; !ok {
			t.Errorf("%s 应仍在 imageMime 中", e)
		}
		got := previewFor(t, "x"+e, encode(t, strings.TrimPrefix(e, ".")))
		if got.Kind != "image" {
			t.Errorf("%s 预览 kind=%s content=%q，期望 image（解码器未注册？）", e, got.Kind, got.Content)
		}
	}
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
