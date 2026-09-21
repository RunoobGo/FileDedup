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

	"filededup/internal/cloudfile"
	"filededup/internal/filter"
	"filededup/internal/fscase"
	"filededup/internal/model"
	"filededup/internal/realbytes"
	"filededup/internal/sysguard"
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

	// M6-P4（2026-09-21，04 §6.7 C 组 4）系统保护清单的三类可见计数。
	//
	// 为什么必须独立成数而不是并入 Failed：受保护目录被剪枝是**引擎有意为之**，
	// 混进失败清单会把真正的"你的文件读不了"淹没在系统噪声里（修正前盘根扫描
	// 就是这个样子）。但静默跳过同样不行——用户看到的结果比盘上的东西少，
	// 必须有一个地方解释少了多少、为什么少。
	//
	// ProtectedDirs 只数**目录**：剪枝没有下潜，报"跳过了 N 个文件"就是编数。
	ProtectedDirs  int
	ProtectedFiles int
	// SkippedCloudFiles 是 M6-P1 跳过的云端占位文件数（macOS dataless /
	// Windows RECALL_ON_DATA_ACCESS）。为什么又是"独立计数、不记 Failed"：
	// 占位文件没被读取就没被去重，是引擎**按用户默认意愿**主动放弃的（读了
	// 就等于替用户下载全文），语义上同 ProtectedFiles 一类；但它影响的正是
	// 用户最关心的"能省多少"，所以必须单列一个数，让界面能说清"这次没算的是
	// 云端文件，共 N 个"。允许水合（AllowCloudHydration）时照常读取、**不计数**。
	SkippedCloudFiles int
	// UnprotectedRoots 是"因为用户显式指定了它，所以保护对它失效"的根路径。
	// 界面必须据此警示（M8）：这些根扫出来的东西可能全是系统元数据，
	// 也可能是用户唯一真正想扫的——两种情况下他都该知道自己脱离了保护范围。
	UnprotectedRoots []string
}

// Waiter 暂停闸门（②-S）：dedup.Gate 实现本接口。扫描器不 import dedup
// （依赖方向相反），经窄接口注入；nil 表示不支持暂停。
type Waiter interface {
	Wait(ctx context.Context) error
}

// guard 系统保护清单。抽成包级变量供测试换装别的平台清单（与
// probeCaseSensitive 同一手法）：盘根伪文件与 Windows 保留名只在 Windows 清单里，
// 不能注入就永远只能在 Windows 上才有断言机会，而那是"等于没测"。
var guard = sysguard.New(sysguard.Current)

// cloudCheck 云端占位判据（M6-P1）。抽成包级变量供测试注入假判据：
// 真实的 dataless/RECALL 位要靠云端客户端配合制造，本机与 CI 都造不出来，
// 不能注入就等于把"顺序对不对""计数准不准"这两件真正可测的事留白。
var cloudCheck = cloudfile.Of

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
	// C1（2026-09-21，设计稿 §6.1）：**单根不做折叠**。折叠唯一的真实用途是把"同一棵树
	// 的两种拼写"并成一个键，而单根出发的遍历里每个目录只会以唯一拼写出现（队列只投
	// 原样根，其后每个路径都是 filepath.Join(父, ReadDir 得到的名字)，链接/junction 已
	// 被跳过）——没有可并之物，折叠只可能把大小写不同的两棵**不同**目录误并成一棵。
	// 实测：单根扫大小写敏感卷上的 alpha/ 与 ALPHA/，只收得到一棵子树，且 files_failed=0。
	// 反方向的错法（该折没折 → 同一棵收两遍）由流水线阶段 1.5 的物理身份去重兜底，
	// 代价不对称，故不确定时倾向不折。sens 此处保留不参与折叠：它是"没测过"的诚实值。
	if len(roots) <= 1 {
		return f
	}
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
// 故单独按该根卷的语义折叠。单根时与 fold 同口径地原样返回——否则 visited 的种子键
// 会被平台默认小写化，而其后每个目录的键不折，两套键打架（C1，见 newFolder 注释）。
func (f *folder) foldRoot(i int) string {
	if !f.needFold {
		return f.roots[i]
	}
	return fscase.Fold(f.roots[i], f.sens[i])
}

// key 折叠 + 分隔符统一为 "/" 的**比较键**。
// 不能拿 fold 的结果直接做前缀比较：fold 在不敏感卷上会把 "\" 换成 "/"，
// 在敏感卷上原样返回，两种形态混在一起比较时前缀判定会静默失效
// （04 §6.8.8 登记的 dedupeRoots 同类问题正是这一条）。
func (f *folder) key(p string) string {
	return strings.ReplaceAll(f.fold(p), string(filepath.Separator), "/")
}

// rootsUnder 返回位于 dir（含自身）之下的那些用户原始根。
// dirKey 必须是 folder.key 的产物；paths 与 keys 同序，是要展示给界面的原样路径。
func rootsUnder(keys []string, paths []string, dirKey string) []string {
	var out []string
	for i, k := range keys {
		if k == dirKey || strings.HasPrefix(k, dirKey+"/") {
			out = append(out, paths[i])
		}
	}
	return out
}

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
	cleaned, sens, all := dedupeRoots(roots)
	// I2：折叠按各根所在卷探测；G2：根前缀预计算一次，供每个文件的 relativeTo 复用
	fld := newFolder(cleaned, sens)
	prefixes := fld.prefixes
	// G3：过滤器预编译一次（扩展名集合建 map），供全部 worker 只读复用
	matcher := filter.Compile(f)

	// M6-P4：盘根伪文件的锚点。**原样字符串**比对即可，不必折叠：遍历期的 dir
	// 只有两种来源——用户给的根（就是这份字符串本身）或 filepath.Join(父, 名字)，
	// 后者永远不等于任何根。做折叠反而会引入"根的两种拼写谁赢"的无谓分支。
	scanRoots := make(map[string]bool, len(cleaned))
	for _, r := range cleaned {
		scanRoots[r] = true
	}
	// M6-P4 逃逸判据要用**用户原始指定的全部根**（含被宽根覆盖而丢弃的子根）：
	// "已被宽根覆盖"这条推断在遇到保护剪枝时并不成立——宽根走不进受保护目录里面。
	// 折叠键统一走 folder.key（见它注释里的那条分隔符陷阱）。
	rawKeys := make([]string, len(all))
	for i, r := range all {
		rawKeys[i] = fld.key(r)
	}
	// (1) 用户点名的根本身就是清单内路径：照他的意思扫，但这份"已脱离系统保护"
	// 必须留痕，界面据此警示（M8）。剪枝不参与，故不计入 ProtectedDirs。
	startUnprot := make(map[string]struct{})
	for _, r := range cleaned {
		if guard.Dir(r, filepath.Base(r)).Skip {
			startUnprot[r] = struct{}{}
		}
	}

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
	// 保护计数按 worker 记账、收口时合并（与 locals/fails 同一手法，不加锁）。
	protDirs := make([]int, workers)
	protFiles := make([]int, workers)
	cloudSkipped := make([]int, workers)
	escapedRoots := make([][]string, workers)
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
					//
					// 2026-09-20（ocr 审查 M1）：仅对**文件**生效。我们从不
					// 生成临时命名的目录（ops 的全部临时名都挂在用户文件名
					// 之后），而此处在 IsDir 分支之前执行——修正前一个名为
					// "album.fdd-old-collection" 的用户目录会让整棵子树静默
					// 漏扫。目录名含标记时按其内容逐项判定即可。
					if !typ.IsDir() && worktemp.IsTempName(de.Name()) {
						continue
					}
					if typ.IsDir() {
						// M6-P4（04 §6.7 C 组 4）：系统保护清单**排在隐藏规则之前**。
						// 既属"系统保护"又属"隐藏"的条目（.Spotlight-V100 一类）若先被
						// 隐藏规则拦下，"已保护跳过 N" 会随用户勾选"包含隐藏文件"而变小，
						// 读起来像保护失效；两类计数也就此分不清谁挡的。
						//
						// 命中后唯一的放行通道是"用户显式把该目录（或其内部）设为扫描根"：
						// 保护是为了挡住误伤，不是为了否决专家的指名请求。放行时把涉及的
						// 根记进 UnprotectedRoots，界面必须警示"这一片已脱离系统保护"。
						if d := guard.Dir(full, de.Name()); d.Skip {
							if esc := rootsUnder(rawKeys, all, fld.key(full)); len(esc) > 0 {
								escapedRoots[idx] = append(escapedRoots[idx], esc...)
							} else {
								protDirs[idx]++
								continue
							}
						}
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
					//
					// M6-P4：保护判定放在 Info() **之前**是刻意的——Windows 上
					// 保留名（CON、NUL.dat）的 stat 本身就可能返回设备信息或报错，
					// 先判定就一次盘都不碰。命中同样**不记 Failed**：它不是失败。
					if d := guard.File(de.Name(), scanRoots[dir]); d.Skip {
						protFiles[idx]++
						continue
					}
					info, err := de.Info()
					if err != nil {
						fails[idx] = append(fails[idx], model.FailedItem{Path: full, Stage: "scan", Err: err.Error()})
						continue
					}
					// M6-P1 云端占位：必须排在 IsRegular / 0 字节 / matcher.Apply
					// **三者之前**（04 §6.9.4 判据）。两处理由不同：
					//   - Windows 的 RECALL 位只存在于 Info() 带来的属性里，而占位
					//     文件在部分客户端下 Mode 并不可靠；放 IsRegular 之后就永远看不到。
					//   - 计数说的是"有多少云端文件没参与"，与扩展名/大小过滤无关：
					//     放在 matcher 之后，一个 .txt 的占位会被扩展名过滤静默吃掉，
					//     这个数就开始说谎。
					// 允许水合（AllowCloudHydration）时整段跳过：用户显式要读，
					// 那就按普通文件走，既不跳过也不计数。
					if !f.AllowCloudHydration && cloudCheck(info) {
						cloudSkipped[idx]++
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
					// M6-P2 实占：复用同一个 info（unix 零额外 syscall，读的是
					// Stat_t.Blocks）。放在 matcher.Apply **之后**是刻意的——
					// Windows 腿要按路径查询，被过滤器挡掉的文件不该付这次开销。
					e.Actual, e.ActualKnown = realbytes.Of(full, size, info)
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

	for _, n := range protDirs {
		res.ProtectedDirs += n
	}
	for _, n := range protFiles {
		res.ProtectedFiles += n
	}
	for _, n := range cloudSkipped {
		res.SkippedCloudFiles += n
	}
	// 逃逸根去重后排序：多个 worker 可能各自报同一个根，而 worker 完成顺序
	// 不确定——不排序就是同一份输入两次扫描给出两份清单（回归无法断言，
	// 界面上还会看到顺序抖动）。
	seenUnprot := map[string]struct{}{}
	var unprot []string
	addUnprot := func(r string) {
		if _, ok := seenUnprot[r]; ok {
			return
		}
		seenUnprot[r] = struct{}{}
		unprot = append(unprot, r)
	}
	for r := range startUnprot {
		addUnprot(r)
	}
	for _, es := range escapedRoots {
		for _, r := range es {
			addUnprot(r)
		}
	}
	sort.Strings(unprot)
	res.UnprotectedRoots = unprot
	return res
}

// dedupeRoots 规范化并剔除被其他根包含的子根（a 与 a/b 同扫时丢弃 a/b），
// 返回与保留根同序的「该根所在卷是否区分大小写」，以及**去重前的全部规范化根**。
//
// 第三个返回值单独给出是 M6-P4 的逃逸判据要用：保护清单会把宽根的一条子树剪掉，
// 此时"子根已被宽根覆盖"并不成立（宽根走不进受保护目录里面），被丢弃的子根
// 仍须作为"用户显式指定过"的依据放行。
//
// I2：判重前按各根所在卷的语义折叠。
//
// C1（2026-09-21）：单根不探测，理由**不是**"无从判重所以折叠无关紧要"——单根扫描照样
// 要用折叠键（`visited` 去重），只是它**不需要**折叠（设计稿 §6.1：单根遍历里每个目录
// 只会以唯一拼写出现）。不探测是为了不给最常用的一条路（只选一个目录）平白往用户目录里
// 写探测文件；折叠本身由 newFolder 对单根整个关掉。
func dedupeRoots(roots []string) ([]string, []bool, []string) {
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
		return out, sens, out
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
	return kept, keepSens, out
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
