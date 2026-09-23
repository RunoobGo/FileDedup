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
	Reclaimable uint64 `json:"reclaimable"`
	// 实占口径（M6-P2）。ActualKnown=false 时 reclaimable_actual 完全来自逻辑
	// 回退（该卷读不到实占），它与 reclaimable 相等**不代表**"实占就是这么多"。
	ReclaimableActual uint64     `json:"reclaimable_actual"`
	ActualKnown       bool       `json:"actual_known"`
	Files             []fileJSON `json:"files"`
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
		// M6-P4（2026-09-21）系统保护清单的剪枝量。口径是"目录数/文件数"，
		// 不折算成被剪走的文件总量——剪枝没下潜，估出来的就是假数。
		ProtectedDirs  int `json:"protected_dirs"`
		ProtectedFiles int `json:"protected_files"`
		// M6-P1（2026-09-21）云端占位跳过数。为 0 有两种含义——盘上确实没有
		// 占位文件，或用户用 -allow-cloud-hydration 显式允许读取——后者由
		// 命令行自身知情，报告里不复述，避免为一个大可不必的状态加字段。
		SkippedCloudFiles int `json:"skipped_cloud_files"`
		// M21（2026-09-21）工作临时名文件跳过数：应用自己的 .fdd-* 残留
		// （硬链接合并/回撤的中间产物与崩溃残片）。它们是三类"跳过"里最容易被
		// 误认成"文件凭空消失"的一类——名字不以 "." 开头，隐藏规则挡不住。
		// 口径同 protected_files：命中不是失败，但必须看得见。判据是名字形态，
		// 无法区分"我们的残留"与"用户恰好这样命名的文件"。
		SkippedWorkTempFiles int `json:"skipped_work_temp_files"`
		// CaseProbeUnproven（M62+M85）：本轮问卷过、但卷大小写语义取自平台默认的根数。
		// 0 有两种成因（都确证 / 单根一趟按 C1 压根没问卷），别读成"这一卷实测过"；
		// 完整口径见 scanner.Result.CaseProbeUnproven。
		CaseProbeUnproven int `json:"case_probe_unproven"`
		// M6-P2（2026-09-21）实占口径合计。reclaimable_bytes 保留逻辑口径不动
		// （它是历史数字与既往报表的参照），实占另起一键，两数之差就是稀疏/
		// 压缩文件此前被虚报的量。
		ReclaimableActualSum uint64 `json:"reclaimable_bytes_actual"`
		ActualKnown          bool   `json:"reclaimable_actual_known"`
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
	allowCloud := flag.Bool("allow-cloud-hydration", false, "允许读取云端占位文件（会触发按需下载）")
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
			// M6-P1：默认 false＝跳过云端占位并计数，与 GUI 同一默认档。
			AllowCloudHydration: *allowCloud,
		},
	}
	for _, e := range strings.Split(*exclude, ",") {
		if e = strings.TrimSpace(e); e != "" {
			cfg.Filters.ExcludeExts = append(cfg.Filters.ExcludeExts, e)
		}
	}

	p := dedup.New()
	// F1⑤（2026-09-23）：`defer cch.Close()` 救不了 `os.Exit` —— defer 只在函数返回时跑，
	// 而 main() 里此后还有三条失败退出路径（扫描失败 / 创建输出失败 / 编码失败），
	// 每一条都绕过它。SQLite 跑在 WAL 上，进程被带走不会丢已提交的数据，但会
	// 跳过 Close() 这条明确的收口约定（应用侧 startup/shutdown 都走它，见 app.go 的 C5），
	// 并把 -wal/-shm 的检查点推到下次开档。改法：Close 收进一个幂等闭包，
	// 三条退出路径各显式调一次，正常路径仍由 defer 兜（database/sql 的 Close 幂等）。
	var closeCache = func() {}
	if *cachePath != "" {
		cch, err := cache.Open(*cachePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "缓存打开失败: %v\n", err)
			os.Exit(1)
		}
		closeCache = func() { _ = cch.Close() }
		defer closeCache()
		p = p.WithCache(cch)
		cfg.UseCache = true
	}
	start := time.Now()
	groups, failed, err := p.Run(context.Background(), cfg)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描失败: %v\n", err)
		closeCache()
		os.Exit(1)
	}

	// 两个列表字段都必须预置成非 nil 切片（APP-10）：`failed` 直接收 Pipeline 的
	// 返回值，无失败时它是 nil，JSON 里就成了 "failed": null。同一条报告里
	// groups 恒为数组、failed 却会为 null，机器读者按 `for x in r["failed"]` 消费
	// 即 TypeError；冒烟此前用 `or []` 把这个差别兜住了，等于替后端藏契约违约。
	r := report{
		Groups: make([]*groupJSON, 0, len(groups)),
		Failed: append([]model.FailedItem{}, failed...),
	}
	r.Scan.Roots = roots
	r.Scan.Threads = cfg.Threads
	r.Scan.Paranoid = *paranoid
	r.Scan.Elapsed = elapsed.String()
	for _, g := range groups {
		gj := &groupJSON{
			Reclaimable:       g.Reclaimable,
			ReclaimableActual: g.ReclaimableActual,
			ActualKnown:       g.AnyActualKnown(),
		}
		for _, f := range g.Files {
			gj.Files = append(gj.Files, fileJSON{Path: f.Path, Size: f.Size, ModTime: f.ModTime})
			r.Stats.DuplicateFiles++
		}
		r.Groups = append(r.Groups, gj)
		r.Stats.ReclaimableSum += g.Reclaimable
		r.Stats.ReclaimableActualSum += g.ReclaimableActual
		if gj.ActualKnown {
			r.Stats.ActualKnown = true
		}
	}
	r.Stats.Groups = len(groups)
	r.Stats.FilesFailed = len(failed)
	// 语料口径走 Pipeline.ScannedFiles()，不再从进度事件反推：进度事件的
	// FilesTotal 是阶段口径（预筛/哈希阶段会重设为该阶段处理量），缓存命中
	// 复扫时远小于语料数，双跑比对会误报漂移。
	r.Stats.FilesTotal = int(p.ScannedFiles())
	r.Stats.CacheHits = int(p.CacheHits())
	r.Stats.ProtectedDirs = int(p.ProtectedDirs())
	r.Stats.ProtectedFiles = int(p.ProtectedFiles())
	r.Stats.SkippedCloudFiles = int(p.CloudSkipped())
	r.Stats.SkippedWorkTempFiles = int(p.WorkTempSkipped())
	r.Stats.CaseProbeUnproven = int(p.CaseProbeUnproven()) // M62+M85

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "创建输出失败: %v\n", err)
			closeCache()
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&r); err != nil {
		fmt.Fprintf(os.Stderr, "编码失败: %v\n", err)
		closeCache()
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "完成: %d 组 / 可释放 %s（实占 %s）/ 失败 %d / 耗时 %s\n",
		r.Stats.Groups, humanBytes(r.Stats.ReclaimableSum),
		humanBytes(r.Stats.ReclaimableActualSum), r.Stats.FilesFailed, elapsed)
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
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
