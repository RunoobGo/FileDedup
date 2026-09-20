// fdd-cli CLI 冒烟工具（04 M1-T11）：扫描指定目录输出 JSON 报告。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"filededup/internal/cache"
	"filededup/internal/dedup"
	"filededup/internal/model"
)

type fileJSON struct {
	Path    string `json:"path"`
	Size    uint64 `json:"size"`
	ModTime int64  `json:"mtime"`
}

type groupJSON struct {
	Reclaimable uint64     `json:"reclaimable"`
	Files       []fileJSON `json:"files"`
}

type report struct {
	Scan struct {
		Roots    []string `json:"roots"`
		Threads  int      `json:"threads"`
		Paranoid bool     `json:"paranoid"`
		Elapsed  string   `json:"elapsed"`
	} `json:"scan"`
	Stats struct {
		FilesTotal     int    `json:"files_total"`
		FilesFailed    int    `json:"files_failed"`
		Groups         int    `json:"groups"`
		ReclaimableSum uint64 `json:"reclaimable_bytes"`
		DuplicateFiles int    `json:"duplicate_files"`
		// CacheHits：本轮在哈希缓存里查到记录的候选文件数（AS-K1）。
		// 冒烟脚本据此断言"复扫必须真命中"——三跑结论一致并不能证明缓存生效。
		CacheHits int `json:"cache_hits"`
	} `json:"stats"`
	Groups []*groupJSON       `json:"groups"`
	Failed []model.FailedItem `json:"failed"`
}

func main() {
	threads := flag.Int("threads", 0, "worker 数（0 = 默认 核数-1）")
	paranoid := flag.Bool("paranoid", false, "逐字节确认")
	minSize := flag.Uint64("min-size", 0, "最小文件大小字节")
	maxSize := flag.Uint64("max-size", 0, "最大文件大小字节（0=不限）")
	exclude := flag.String("exclude", "", "扩展名排除，逗号分隔，如 .tmp,.log")
	includeHidden := flag.Bool("hidden", false, "包含隐藏文件")
	out := flag.String("o", "", "输出到文件（默认 stdout）")
	cachePath := flag.String("cache", "", "哈希缓存 DB 路径（空 = 禁用缓存）")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: fdd-cli [选项] 目录...\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	cfg := model.ScanConfig{
		Roots:    roots,
		Threads:  *threads,
		Paranoid: *paranoid,
		Filters: model.Filters{
			MinSize:       *minSize,
			MaxSize:       *maxSize,
			IncludeHidden: *includeHidden,
		},
	}
	for _, e := range strings.Split(*exclude, ",") {
		if e = strings.TrimSpace(e); e != "" {
			cfg.Filters.ExcludeExts = append(cfg.Filters.ExcludeExts, e)
		}
	}

	p := dedup.New()
	if *cachePath != "" {
		cch, err := cache.Open(*cachePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "缓存打开失败: %v\n", err)
			os.Exit(1)
		}
		defer cch.Close()
		p = p.WithCache(cch)
		cfg.UseCache = true
	}
	start := time.Now()
	groups, failed, err := p.Run(context.Background(), cfg)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描失败: %v\n", err)
		os.Exit(1)
	}

	r := report{Groups: make([]*groupJSON, 0, len(groups)), Failed: failed}
	r.Scan.Roots = roots
	r.Scan.Threads = cfg.Threads
	r.Scan.Paranoid = *paranoid
	r.Scan.Elapsed = elapsed.String()
	for _, g := range groups {
		gj := &groupJSON{Reclaimable: g.Reclaimable}
		for _, f := range g.Files {
			gj.Files = append(gj.Files, fileJSON{Path: f.Path, Size: f.Size, ModTime: f.ModTime})
			r.Stats.DuplicateFiles++
		}
		r.Groups = append(r.Groups, gj)
		r.Stats.ReclaimableSum += g.Reclaimable
	}
	r.Stats.Groups = len(groups)
	r.Stats.FilesFailed = len(failed)
	// 语料口径走 Pipeline.ScannedFiles()，不再从进度事件反推：进度事件的
	// FilesTotal 是阶段口径（预筛/哈希阶段会重设为该阶段处理量），缓存命中
	// 复扫时远小于语料数，双跑比对会误报漂移。
	r.Stats.FilesTotal = int(p.ScannedFiles())
	r.Stats.CacheHits = int(p.CacheHits())

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "创建输出失败: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&r); err != nil {
		fmt.Fprintf(os.Stderr, "编码失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "完成: %d 组 / 可释放 %s / 失败 %d / 耗时 %s\n",
		r.Stats.Groups, humanBytes(r.Stats.ReclaimableSum), r.Stats.FilesFailed, elapsed)
}

func humanBytes(n uint64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(u), 0
	for m := n / u; m >= u; m /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
