# FileDedup 代码审查报告

**审查范围**：Go 后端全量（app.go、main.go、internal/ 全部 8 个包）、前端全量（src/ 13 个文件）
**审查重点**：算法优化、运行速度、资源占用、UI 设计
**审查日期**：2026-09-16 ｜ 版本基线：0.4.0-m4

---

## 一、总体评价

🎉 **架构质量很高**。五级漏斗流水线（size 分组 → FileKey 硬链接去重 → 首尾 xxHash64 预筛 → 全量 BLAKE3 → paranoid 逐字节确认）是教科书级的去重设计，比 fdupes/jdupes 同类工具更进一步：

- 小文件（≤128KiB）一趟双哈希，避免二次读盘
- SQLite WAL 哈希缓存 + 批量 UPSERT 单事务写回，二次扫描近零读盘
- `sync.Pool` 缓冲复用、大文件分段预取流水线（IO 与哈希重叠）
- 解压炸弹防护（`DecodeConfig` 预检像素数，app.go:695-703）
- 暂停/恢复门（Gate）、操作前 S1 校验、保留项 S2 保护、delete 强制确认 S4
- 每个包都有配套测试，还有 benchgen 基准数据生成器

**结论：算法框架不需要推翻，优化空间集中在缓存淘汰的正确性、IO 计账、并发清理与前端规模化**。以下按严重程度分级列出。

---

## 二、🔴 必须修复（2 项）

### R1. 缓存 LRU 淘汰策略失效——热文件反而最先被逐出

- **位置**：`internal/cache/cache.go:96-114`（Lookup）＋ `internal/dedup/pipeline.go:217-233`（缓存命中路径）
- **问题**：`Lookup` 命中时不刷新 `last_hit`，且命中条目**不会进入** `pending` 写回队列（只有新计算的条目才进，pipeline.go:253-263、345-347）。结果：内容长期未变、每次扫描都命中的"最受益"文件，其 `last_hit` 永远是首次写入的旧值；一旦缓存超 50 万条触发淘汰（cache.go:163-165 按 `last_hit ASC` 删除），**这些高频命中文件被最先清退**，缓存命中率随时间持续劣化，M4 里程碑的核心价值被逐渐侵蚀。
- **修复建议**：阶段 2 worker 命中缓存时本地收集路径，任务结束时与 `pending` 一并提交，批量执行 `UPDATE hash_cache SET last_hit = ? WHERE path IN (...)`（可分批 500 条/语句）。`Store` 签名增加命中路径参数即可，改动面小。

### R2. 进度计账重复累加——进度条虚满、ETA 提前归零

- **位置**：`internal/dedup/pipeline.go:151`（BytesTotal 口径）、`:267`（预筛加 `min(size,128K)`）、`:349`（全量再加 `e.Size`）
- **问题**：`BytesTotal = Σ(全部文件 size)`，但同一候选文件先按预筛计入最多 128KiB，进入全量哈希后又计入完整 size——**预筛读量与全量读量重复计账**。重复率高时 `BytesDone` 可超过 `BytesTotal`：前端百分比被 clamp 到 100%（ScanView.vue:58-62），ETA 提前显示 0 但任务仍在哈希，且"速度"出现假跌（elapsed 继续增长、BytesDone 早已封顶）。
- **修复建议**：统一口径。最简做法：预筛阶段结束时将 `bytesTotal` 修正为「已完成的预筛字节 + Σ(待全量哈希候选的 size)」；或按阶段切换 total（prefilter 阶段用 `Σmin(size,128K)`，hash 阶段重置为候选总量）。`Tracker` 已有 `SetTotal`，可直接复用。

---

## 三、🟡 重要问题（8 项）

### Y1. 数据竞争：`p.cancel` 无锁读

- **位置**：`internal/dedup/pipeline.go:86-91`（Cancel 直接读字段）vs `:117-119`（Run 持锁写）
- **问题**：`Cancel()` 读取 `p.cancel` 未加锁，与 `Run()` 的锁内写入构成数据竞争。`go test -race` 会报；虽有 nil 检查兜底、实际窗口极小，但属于正式缺陷（race detector 拦截级别）。
- **修复建议**：`Cancel` 内 `p.mu.Lock()` 后读取再调用。

### Y2. 大文件分段哈希：每段 16MB 新分配，pool buf 参数被架空

- **位置**：`internal/hasher/hasher.go:89-132`（`HashFullSegmented`）
- **问题**：`depth>0` 时传入的 pool 缓冲完全未用（仅 `depth<=0` 分支走 `HashFull` 才消费），调用方（pipeline.go:328-335）借出的 1MiB 缓冲白白占用；更关键的是每段 `make([]byte, 16MB)`（hasher.go:111）用完即弃——10GB 文件 = 640 次 16MB 分配，多 worker 同时哈希大文件时 GC 压力与内存尖峰叠加（workers × depth × 16MB 瞬时在途）。
- **修复建议**：段缓冲 ring 复用——预分配 `depth+1` 个 16MB 缓冲经 channel 流转，消费方 `h.Write` 完成后归还；顺带清理误导性的 `buf` 参数（或在顺序分支才借用）。

### Y3. `GetResultGroups` 每次分页全量重排 + 持锁过长

- **位置**：`app.go:258-321`
- **问题**：滚动加载每追加一页都触发：全量 ext 筛选（O(组×文件)）+ `sort.Slice` 全量重排（O(n log n)），**全程持有 `a.mu`**——而 `OnProgress` 回调（app.go:120-125）也要抢 `a.mu`，万级组时每次滚动会短暂阻塞进度事件推送。
- **修复建议**：扫描完成/操作清理之间结果集不可变，按 `(sort, ext)` 缓存有序索引（失效条件：ops 清理、keep 变更），分页仅浅拷贝切片。持锁范围同步收窄。

### Y4. 清理操作完全串行执行

- **位置**：`internal/ops/executor.go:100-185`
- **问题**：delete/move/hardlink 逐文件串行；trash 批量 osascript 失败后退化为逐文件也是串行。万级文件操作耗时线性叠加单文件 syscall 延迟（尤其 macOS Finder AppleScript 往返）。
- **修复建议**：有界并发 worker（4-8 个），`report` 改原子计数；trash 批量接口已是好的样板，delete/move/hardlink 天然可并发（不同文件互不依赖）。注意 OnProgress 的 done 单调性用 atomic 保证即可。

### Y5. 图片预览峰值内存 ~300MB

- **位置**：`app.go:403-417`（50MB 全量读入）＋ `:692-734`（maxPixels 64M ≈ 256MB RGBA）
- **问题**：单次预览点击即可让 50MB 源数据 + 64M 像素位图同时存活，峰值约 300MB；而产出只是 512px 缩略图。
- **修复建议**：`maxPixels` 降到 16M（≈64MB RGBA，仍覆盖 4000×4000）；`srcLimit` 降到 20MB。两处常量改动即可，风险极低。

### Y6. 前端错误处理全靠阻塞式 `alert()`

- **位置**：`frontend/src/stores/scan.ts:79, 93, 171, 197, 251, 281, 290` 等 10+ 处
- **问题**：系统级模态对话框与整体精致的 UI 割裂；扫描失败弹窗叠加在进度面板上，连续错误时体验很差。
- **修复建议**：统一 toast 组件（非阻塞、可堆叠、自动消失、失败项可点击跳转失败清单），错误状态入 store。

### Y7. 结果页 DOM 无上限累积

- **位置**：`frontend/src/views/ResultView.vue:128-132`（无限滚动）
- **问题**：每页 +100 组持续 append，万级组时 Vue 组件实例与 DOM 节点线性增长。`content-visibility: auto`（GroupCard.vue:56）已缓解渲染开销，但内存无解。
- **修复建议**：设加载上限（如 2000 组后显示「继续加载」显式按钮），或引入轻量虚拟滚动。结合 Y3 的后端排序缓存一起做收益最大。

### Y8. 默认勾选全部冗余项，误删风险面偏大

- **位置**：`frontend/src/stores/scan.ts:127-137`（resetSelection）＋ `ResultView.vue:98-106`
- **问题**："默认全选冗余项 + 永久删除按钮常驻"意味着一次误点仅隔一个确认框。后端 S4 强制确认与 S2 保留保护都很完善，但默认全选仍是激进的产品选择——大结果集下用户很难逐项复核。
- **修复建议**（可选）：默认不勾选，进入结果页时提示「已为你按可释放空间排序，点击全选选中全部冗余项」；或仅预选前 N 组。

---

## 四、🟢 优化建议（精选）

| # | 位置 | 问题 | 建议 |
|---|------|------|------|
| G1 | pipeline.go:447-450 | paranoid `streamEqual` 每次比较新分配 2×256KiB | 复用 `hasher.Pool` 或每组一次分配 |
| G2 | scanner.go:159, 234-241 | `relativeTo` 每文件线性扫根，`r+sep` 每次重复分配 | 预计算 `rootsSep []string`，百万文件级省大量小分配 |
| G3 | filter.go:31-34, 46-53 | 扩展名列表逐项 `EqualFold` 线性扫 | 扫描前归一化为 `map[string]struct{}` |
| G4 | cache.go:83 | `idx_cache_size` 索引无任何查询使用 | 徒增写放大，确认无规划后删除 |
| G5 | cache.go:96-114 | Lookup 逐文件 SQL 点查 | 显式 `Prepare` 复用语句；进阶：任务开始按 size 预载候选子集进内存 map |
| G6 | scanner.go:135-143 | `visited` 全局互斥锁，每目录一次 | 深目录百万级时可换 `sync.Map` |
| G7 | GroupCard.vue:28 | 「图片组」正则在模板内每次渲染重算 | 后端 `GroupView` 直接加 `isImage` 字段最干净 |
| G8 | ResultView.vue:22-27 | 无限滚动无 in-flight 防抖，快速滚动会排队多拉几页 | 加 isLoading 标志（resultChain 已保证正确性，只是防过量） |
| G9 | ResultView.vue:18-20 | `watch(groups)` 对 push 追加不触发（deep:false），resetSelection 逻辑分散在各调用点 | 把 resetSelection 收敛进 `doLoadResultPage` |
| G10 | ResultView.vue:63-69 | 排序 select / 扩展名 input 无 label、aria-label | a11y 补齐 |
| G11 | cache.go:154-158 | 每次写回后全表 `COUNT(*)` | 改触发式：仅当本次写入量 > 0 才计数；影响小 |
| G12 | frontend/src/.DS_Store | 仓库垃圾文件 | 删除并加 .gitignore |

---

## 五、UI 设计专项评价

**做得好的** 🎉：
- 64px 图标侧栏 + 三视图布局紧凑清晰，信息密度合理
- CSS 变量主题系统（light/dark/system 三态跟随），style.css 统一设计令牌
- 桌面应用正确姿势齐全：拖拽目录（Wails 原生路径）、全局快捷键（Space 预览 / Cmd+A / Del / Esc，App.vue:15-35）、渐进披露高级选项（ScanView.vue:134-142）
- 细节到位：`tabular-nums` 数字对齐、`content-visibility` 长列表优化、失败清单角标、状态点脉冲动画、运行态/配置态视图切换

**待改进的**（除 Y6/Y7/Y8 外）：
- 预览面板基于 base64 传输（`PreviewData.Content`），512px JPEG 约 30-80KB base64 尚可接受；若未来放大预览尺寸建议改 blob URL 或 Wails asset handler
- 结果页筛选只有扩展名输入框，缺"按文件名搜索"——大结果集下的高频需求，建议补齐（后端 ResultQuery 加 name 参数即可）

---

## 六、性能优化路线（按投入产出比排序）

1. **R1 缓存淘汰修复**（半天）：让已有的 M4 投入真正生效，二次扫描加速立竿见影
2. **R2 进度口径修正**（半天）：进度条/ETA 可信度
3. **Y2 段缓冲复用 + Y5 预览降内存**（各 1 小时）：直接减少 GC 压力与内存峰值
4. **Y4 清理操作并发化**（半天）：万级文件操作提速 3-6×
5. **Y3 + Y7 结果查询与渲染规模化**（1 天）：万级组流畅浏览
6. **G2/G3 扫描热路径微优化**（各 1 小时）：百万文件级扫描 CPU 分配减负
7. **Y6 toast 统一**（半天）：体验一致性

**算法层面无需换框架**：当前 xxHash64 预筛（64KiB 头尾）碰撞率对小文件足够，BLAKE3 吞吐（1-3GB/s/核）远超磁盘顺序读，瓶颈在 IO 而非哈希。若要进一步提速，方向是 **IO 并发自适应**：当前哈希 worker 数 = CPU-1 对 SSD 合理，但 HDD 上并发读反而引发寻道抖动——建议暴露"IO 并发数"设置并按介质给默认值（HDD 1-2、SSD = workers）。

---

*审查方法：全文件逐行阅读 + 分级标注。测试与文档（docs/ 设计规范与实现一致性良好）未逐字核对，建议以本报告为索引针对性验证。*
