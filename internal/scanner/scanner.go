// Package scanner 并行目录遍历 + 平台 FileKey 抽象（04 M1-T03/T04/T05）。
package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"filededup/internal/filter"
	"filededup/internal/fscase"
	"filededup/internal/model"
	"filededup/internal/worktemp"
)

// dirQueue 无界目录队列：worker 既是消费者也是生产者，
// 有界 channel 在"全部 worker 同时投递且队列满"时会死锁，无界队列从根上消除该风险。
type dirQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []string
	closed bool
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *dirQueue) push(d string) {
	q.mu.Lock()
	q.items = append(q.items, d)
	q.mu.Unlock()
	q.cond.Signal()
}

// pop 阻塞取目录；队列关闭且空时返回 false。
func (q *dirQueue) pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return "", false
	}
	d := q.items[0]
	q.items = q.items[1:]
	return d, true
}

// close 关闭队列（唤醒全部等待者）。
func (q *dirQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

// Result 扫描结果。
type Result struct {
	Files   []*model.FileEntry
	Failed  []model.FailedItem
	Visited int // 实际访问目录数（诊断用）
}

// Waiter 暂停闸门（②-S）：dedup.Gate 实现本接口。扫描器不 import dedup
// （依赖方向相反），经窄接口注入；nil 表示不支持暂停。
type Waiter interface {
	Wait(ctx context.Context) error
}

// probeCaseSensitive 按卷探测入口；抽成变量供测试扮演敏感/不敏感卷
// （真实大小写敏感卷需要专门格式化的卷，本机与 CI 都无法现造）。
var probeCaseSensitive = fscase.Sensitive

// folder 路径折叠器：折叠与否由「该路径所属扫描根所在卷」决定（I2）。
// 修正前是包级函数 + 编译目标硬编码，等于对全部卷做一次平台赌博。
type folder struct {
	roots    []string
	prefixes []string // root + sep，与 sens 同序
	sens     []bool   // 各根所在卷是否区分大小写
	def      bool     // 平台默认（路径不属于任何根时）
	needFold bool     // 存在需要折叠的根；全敏感时 fold 直接原样返回
}

func newFolder(roots []string, sens []bool) *folder {
	f := &folder{roots: roots, prefixes: rootPrefixes(roots), sens: sens, def: fscase.Default()}
	for _, s := range f.sens {
		if !s {
			f.needFold = true // 有不敏感卷 → 必须按其语义折叠
			break
		}
	}
	if !f.def {
		f.needFold = true // 兜底分支（路径未命中任何根）落在不敏感平台默认上
	}
	return f
}

// fold 折叠遍历中产生的全路径：按其所属扫描根所在卷的语义。
func (f *folder) fold(p string) string {
	if !f.needFold {
		return p
	}
	for i, rp := range f.prefixes {
		if strings.HasPrefix(p, rp) {
			return fscase.Fold(p, f.sens[i])
		}
	}
	return fscase.Fold(p, f.def)
}

// foldRoot 折叠第 i 个根本身：根路径不带尾分隔符，命中不了自身的 root+sep 前缀，
// 故单独按该根卷的语义折叠。
func (f *folder) foldRoot(i int) string { return fscase.Fold(f.roots[i], f.sens[i]) }

// Walk 并行遍历 roots（无暂停闸门）。
func Walk(ctx context.Context, roots []string, f *model.Filters, workers int) *Result {
	return WalkWithGate(ctx, roots, f, workers, nil)
}

// WalkWithGate 并行遍历 roots：
//   - 跳过符号链接与 0 字节文件（内置行为）
//   - 应用过滤器
//   - 重叠根目录与子目录去重（折叠与否按各根所在卷探测，I2）
//   - unix 平台 FileKey 由 lstat 顺带填充；Windows 留待 ResolveKey 按需解析
//   - ②-S：每目录处理前过 gate（暂停挂起、取消快速排空），取消后不再触盘
func WalkWithGate(ctx context.Context, roots []string, f *model.Filters, workers int, gate Waiter) *Result {
	if workers < 1 {
		workers = 4
	}
	res := &Result{}
	cleaned, sens := dedupeRoots(roots)
	// I2：折叠按各根所在卷探测；G2：根前缀预计算一次，供每个文件的 relativeTo 复用
	fld := newFolder(cleaned, sens)
	prefixes := fld.prefixes
	// G3：过滤器预编译一次（扩展名集合建 map），供全部 worker 只读复用
	matcher := filter.Compile(f)

	var (
		mu      sync.Mutex
		nextID  atomic.Uint64
		visited = make(map[string]struct{})
		q       = newDirQueue()
		pend    sync.WaitGroup // 在途目录计数（队列中 + 处理中）
	)

	for i := range cleaned {
		visited[fld.foldRoot(i)] = struct{}{}
	}

	// submit 投递目录：先计数再入队（保证 closer 的 Wait 不早于 Add）。
	submit := func(dir string) {
		pend.Add(1)
		q.push(dir)
	}

	locals := make([][]*model.FileEntry, workers)
	fails := make([][]model.FailedItem, workers)
	var workerWg sync.WaitGroup

	for i := 0; i < workers; i++ {
		workerWg.Add(1)
		go func(idx int) {
			defer workerWg.Done()
			local := make([]*model.FileEntry, 0, 1024)
			// handle 处理单个目录。I3（2026-09-18 审查）：逐目录 recover——
			// 修正前 worker 无任何兜底，阶段 0 一个 panic（异常 DirEntry 的 Info、
			// 底层 FS 返回的畸形项等）就把整个进程带走，与手册 §7「panic 转 Failed、
			// 不拖垮进程」不符；且进程被杀时正在写的清理任务会停在半路。
			handle := func(dir string) {
				defer pend.Done() // 正常/取消/panic 三条路径都只销一次在途计数
				defer func() {
					if r := recover(); r != nil {
						fmt.Fprintf(os.Stderr, "[scan] 目录 %s 遍历 panic：%v\n%s\n", dir, r, debug.Stack())
						fails[idx] = append(fails[idx], model.FailedItem{
							Path: dir, Stage: "scan",
							Err: fmt.Sprintf("遍历异常已隔离，该目录已跳过: %v", r),
						})
					}
				}()
				// ②-S：暂停挂起在目录处理前（in-flight 目录完成后停步，符合 01 §5.5）。
				// gate.Wait 只在 ctx 结束时返回错误 → 走下方取消排空路径。
				if gate != nil {
					_ = gate.Wait(ctx)
				}
				if ctx.Err() != nil {
					// 取消排空：只销在途计数，绝不再 ReadDir——
					// 修正前取消后仍会把队列里剩余全部目录逐个读一遍才退出。
					return
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					fails[idx] = append(fails[idx], model.FailedItem{Path: dir, Stage: "scan", Err: err.Error()})
					return
				}
				for j, de := range entries {
					if j&1023 == 512 && ctx.Err() != nil {
						break // 单个超大目录内的取消粒度（目录级检查之间最长 1024 项）
					}
					full := filepath.Join(dir, de.Name())
					typ := de.Type()
					if typ&fs.ModeSymlink != 0 {
						continue // 符号链接：不跟随
					}
					// 2026-09-19（缺陷：残留临时文件被当作重复文件）：
					// 应用自身的中间产物以「用户文件名 + .fdd-*」命名，
					// **不以 "." 开头**，因此下面的隐藏文件规则拦不住它。
					// 其中 *.fdd-old 是合并前的完整独立副本，内容与被保留的
					// 文件逐字节相同——若因进程中断/杀软占用句柄而残留，
					// 每次扫描都会把它配成一个"重复组"，用户看到的现象就是
					// 「做过硬链接合并的文件重扫仍被识别为重复」。
					// 判定集中在 worktemp.IsTempName，与产生处（internal/ops）
					// 共用同一份定义，避免日后新增临时名时漏掉忽略规则。
					if worktemp.IsTempName(de.Name()) {
						continue
					}
					if typ.IsDir() {
						if !f.IncludeHidden && strings.HasPrefix(de.Name(), ".") {
							continue
						}
						// P2：目录级剪枝。修正前 ExcludePaths 只作用于文件，
						// 排除 node_modules/.git 时仍会把整棵树走完再逐个过滤，
						// 大目录场景下"排除"只省结果不省时间（实测遍历量不变）。
						// 目录不参与扩展名/大小判定，故走 matchPath-only 的裁剪入口。
						if matcher.ExcludeDir(relativeTo(prefixes, full), de.Name()) {
							continue
						}
						key := fld.fold(full)
						mu.Lock()
						_, seen := visited[key]
						if !seen {
							visited[key] = struct{}{}
						}
						mu.Unlock()
						if !seen {
							submit(full)
						}
						continue
					}
					// 普通文件（Type 未知时用 Info 兜底）
					info, err := de.Info()
					if err != nil {
						fails[idx] = append(fails[idx], model.FailedItem{Path: full, Stage: "scan", Err: err.Error()})
						continue
					}
					if !info.Mode().IsRegular() {
						continue
					}
					size := uint64(info.Size())
					if size == 0 {
						continue // 0 字节内置跳过
					}
					rel := relativeTo(prefixes, full)
					if !matcher.Apply(de.Name(), rel, size) {
						continue
					}
					e := &model.FileEntry{
						ID:      nextID.Add(1),
						Path:    full,
						Size:    size,
						ModTime: info.ModTime().UnixNano(),
						Ext:     strings.ToLower(filepath.Ext(full)),
					}
					e.Key = keyFromInfo(full, info) // unix 填充；win 返回未解析
					local = append(local, e)
				}
			}
			for {
				dir, ok := q.pop()
				if !ok {
					break // 队列关闭且空：全部完成
				}
				handle(dir)
			}
			locals[idx] = local
		}(i)
	}

	// 先投递根目录（完成全部 Add），再启动 closer：
	// 在途计数归零 → 关闭队列 → worker 逐个退出。
	for _, r := range cleaned {
		submit(r)
	}
	go func() {
		pend.Wait()
		q.close()
	}()

	workerWg.Wait()

	for _, l := range locals {
		res.Files = append(res.Files, l...)
	}
	for _, fl := range fails {
		res.Failed = append(res.Failed, fl...)
	}
	res.Visited = len(visited)
	return res
}

// dedupeRoots 规范化并剔除被其他根包含的子根（a 与 a/b 同扫时丢弃 a/b），
// 返回与保留根同序的「该根所在卷是否区分大小写」。
//
// I2：判重前按各根所在卷的语义折叠。单根无从判重，也就不必为它写探测文件。
func dedupeRoots(roots []string) ([]string, []bool) {
	var out []string
	for _, r := range roots {
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = filepath.Clean(r)
		}
		out = append(out, filepath.Clean(abs))
	}
	sort.Strings(out)
	if len(out) <= 1 {
		sens := make([]bool, len(out))
		for i := range sens {
			sens[i] = fscase.Default()
		}
		return out, sens
	}
	sens := make([]bool, len(out))
	for i, r := range out {
		sens[i] = probeCaseSensitive(r)
	}
	var kept []string
	var keepSens []bool
	for i, r := range out {
		dup := false
		fr := fscase.Fold(r, sens[i])
		for j, k := range kept {
			// k 是 r 的前缀目录（各按自身卷的语义折叠后比较）
			fk := fscase.Fold(k, keepSens[j])
			if fr == fk || strings.HasPrefix(fr, fk+string(filepath.Separator)) {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, r)
			keepSens = append(keepSens, sens[i])
		}
	}
	return kept, keepSens
}

// rootPrefixes 预计算「根 + 分隔符」前缀（G2）。
// 原实现在 relativeTo 内部对每个文件、每个根都执行一次 r+string(filepath.Separator)，
// 每次产生一个短命字符串；百万文件级扫描下这一项即数百万次小对象分配。
// 预计算后每文件零分配（HasPrefix 与切片均不分配）。
func rootPrefixes(roots []string) []string {
	sep := string(filepath.Separator)
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = r + sep
	}
	return out
}

// relativeTo 计算 full 相对最近扫描根的路径（过滤 glob 用）。
// rootPrefixes 为 rootPrefixes() 的产物；len(rp) 恰为「根长 + 1」，
// 故用 full[len(rp):] 切片即可，无需再算偏移。
func relativeTo(rootPrefixes []string, full string) string {
	for _, rp := range rootPrefixes {
		if strings.HasPrefix(full, rp) {
			return full[len(rp):]
		}
	}
	return filepath.Base(full)
}
