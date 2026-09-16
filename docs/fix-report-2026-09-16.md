# FileDedup 缺陷修复报告

**修复范围**：审查报告（`docs/code-review-2026-09-16.md`）中全部 🔴/🟡 级缺陷，共 10 项
**修复方式**：按严重程度从重到轻逐项修复，**每项修复后立即验证**（不批量改完再测）
**日期**：2026-09-16

---

## 一、修复清单总览

| # | 级别 | 问题 | 主要改动文件 | 验证方式 | 结果 |
|---|------|------|--------------|----------|------|
| R1 | 🔴 | 缓存命中不续期 → 热文件反被淘汰 | cache.go / pipeline.go | 单测 + 集成 + 端到端 sqlite | ✅ |
| R2 | 🔴 | 进度重复计账（BytesDone 可超总量） | pipeline.go / progress.go | 单测（终值收敛断言） | ✅ |
| Y1 | 🟡 | `p.cancel` 无锁读数据竞争 | pipeline.go | `-race` + **探针敏感性验证** | ✅ |
| Y2 | 🟡 | 分段哈希每段 16MB 新分配 | hasher.go | 单测（TotalAlloc 断言） | ✅ |
| Y3 | 🟡 | 每次翻页全量重排、持锁过长 | app.go | 单测 + 基准对比 | ✅ 10.7× |
| Y4 | 🟡 | 清理操作全串行 | executor.go | 并发正确性测试 ×3 + `-race` | ✅ |
| Y5 | 🟡 | 预览峰值内存 ~300MB | app.go | 边界测试（像素/体积） | ✅ |
| Y6 | 🟡 | 错误反馈全用阻塞式 alert | toast.ts / ToastHost.vue + 5 文件 | Node 驱动 11 项断言 | ✅ |
| Y7 | 🟡 | 结果页 DOM 无上限累积 | scan.ts / ResultView.vue | Node 驱动 11 项断言 | ✅ |
| Y8 | 🟡 | 默认全选冗余项，误删风险面大 | scan.ts / ResultView.vue | Node 驱动 10 项断言 | ✅ |

---

## 二、逐项说明：严重性依据 · 修复思路 · 验证

### 🔴 R1 — 缓存 LRU 淘汰失效

**严重性依据**：不是崩溃类缺陷，但**使 M4 里程碑的核心投入持续失效**。缓存设计意图是「命中即复用、按 `last_hit` 淘汰最旧」，而命中条目的 `last_hit` 永不刷新（`Lookup` 不更新、也不进 `pending` 写回队列），于是**每次扫描都命中的热文件反而成为"最旧"**，在超 50 万条时被最先逐出。命中率随时间单调劣化，用户感知为"二次扫描越用越慢"。

**修复思路**：
1. `cache.Touch(paths)` 批量续期（单事务，空列表无操作）
2. `Pipeline` 阶段 2 worker **本地累积**命中路径（避免热路径抢锁），退出时合并，任务结束统一 `Touch`
3. 顺带兑现代码注释原有承诺：写回改为 `defer` + 命名返回值，使 **Cancelled 路径也写回已算哈希**（原实现在取消时提前 return，写回从未执行）

**验证**：
- `TestTouchRefreshesLastHit`：人为老化 `last_hit` 后淘汰，**续期条目必须存活、未续期的最旧条目被删**
- `TestCacheSecondScanRefreshesLastHit`：两次真实扫描，`last_hit` 必须前进
- 端到端（2291 文件）：第三轮扫描后 `sqlite3` 查得 `COUNT(DISTINCT last_hit) = 1`、`COUNT(*) = 500`，`last_hit` 由 `1789561460 → 1789561471`（全部条目统一续期）

### 🔴 R2 — 进度计账重复累加

**严重性依据**：影响**任务的可用性判断**。`BytesTotal` 按全部文件 size 预置，但同一候选文件在预筛阶段计入 `min(size,128KiB)`、进入全量哈希后再计入完整 `size` —— 重复计账使 `BytesDone` 可超过 `BytesTotal`：进度条被前端 clamp 到 100% 早满，**ETA 提前归零而任务仍在跑**，速度曲线出现假跌。用户会误判任务卡死。

**修复思路**：改为**分阶段重置总量口径**——
- 预筛阶段：`SetTotal(len(candidates), Σmin(size,128KiB))`（与 `AddFile/AddBytes` 严格同源）
- 全量阶段：`SetTotal(已完成 + 待哈希数, 已完成字节 + 待哈希字节)`
- 缓存命中同样补计字节（视作该文件预筛工作量已完成，否则命中越多进度越滞后）

新增 `Tracker.Done()` 读取已完成量，新增 `prefilterBytes()` 辅助。

**验证**：`TestProgressAccountingConsistent` 断言每个进度事件 `BytesDone ≤ BytesTotal`、`FilesDone ≤ FilesTotal`，且**无失败场景下终值精确收敛**。实测终值 `files=13/13, bytes=1024616/1024616`（修复前该场景会超发约 2 倍）。

### 🟡 Y1 — `p.cancel` 无锁读

**严重性依据**：实践窗口极小且被 nil 检查兜底，不会造成可见故障；但它是 **race detector 会拦截的正式缺陷**，会污染 CI/`-race` 结果，也违背该文件其余部分严谨的并发规范。

**修复思路**：`Cancel()` 与 `Run()` 一样持 `p.mu` 读取/写入 `p.cancel`。

**验证（含探针敏感性）**：新增 `TestPipelineCancelRace`（自旋 `Cancel()` 与 `Run` 并发）。为证明该测试**确实能捕获**该竞争，临时回退修复后运行 `-race` → 输出 3 处 `WARNING: DATA RACE`（pipeline.go:88/89/118/120）；恢复修复后干净通过。

### 🟡 Y2 — 大文件分段哈希每段新分配

**严重性依据**：不是正确性问题，而是**大文件场景的 GC 与内存尖峰**。每段 `make([]byte, 16MiB)` 用完即弃：10GiB 文件 = 640 次 16MiB 分配；多 worker 并发哈希大文件时分配速率叠加。另外调用方借出的 pooled 读缓冲在分段路径下**完全未被使用**（徒增一次池操作）。

**修复思路**：改为 **depth+1 个缓冲的环形池**（channel 流转，消费者写完后归还），把在途缓冲数与段数解耦；生产者从池取、池空即自然背压不阻塞。清理误导性的 `buf` 参数用法（仅顺序退化路径使用），并在注释中说明调用方无需预借缓冲。

**验证**：`TestHashFullSegmentedReusesBuffers` 以 `TotalAlloc` 增量断言——32MiB 文件（1MiB 段）额外分配 **5.6MB**；修复前为 32MB（= 段数 × 段大小）。等价性测试（分段结果 == 顺序结果）与读错误无 goroutine 泄漏回归同时通过。

### 🟡 Y3 — 结果查询每次翻页全量重排

**严重性依据**：属于**交互卡顿 + 锁竞争**。滚动加载每追加一页都执行一次全量 ext 筛选（O(组×文件)）与全量排序（O(n log n)），且全程持有 `a.mu` —— 而进度回调也要抢同一把锁，万级组时滚动会短暂阻塞进度事件推送。

**修复思路**：按 `(sort, ext)` 缓存有序组列表，并加**结果集指纹自校验**（长度 + 各组 ID/成员数的异或散列）作为第二道防线：即使某条变更路径忘记显式失效，指纹变化也会让旧缓存自动作废。显式失效点补在 3 处结果集变更处（新扫描开始 / 扫描完成写入 / 操作后清理）。

**验证**：`TestGetResultGroupsCacheCorrectness` 覆盖缓存复用、不同键各建一条、**原地缩减成员数后必须失效**、新增组失效、空集边界。基准（5000 组，分页 100）：

```
Cached       27.6 µs/op     ← 修复后
RebuildEach 294.4 µs/op     ← 修复前行为
```

### 🟡 Y4 — 清理操作全串行

**严重性依据**：**万级文件操作耗时随文件数线性叠加单文件 syscall 延迟**（尤其 macOS 回收站 AppleScript 往返），是用户可直接感知的等待。但改造涉及破坏性操作，风险等级同样高。

**修复思路**：`runIndexed` 有界并发（4 workers），**结果按下标写入、末尾按序汇总**——并发的同时保证输出顺序确定（与请求顺序一致，上层 `gone` 匹配与前端展示都依赖它）。
**并发安全前置审查发现**：`MoveFile` 的目标重名递增 `uniqueDst` 是「先查后用」（TOCTOU），并发处理同目录同基名文件时两个 worker 可能选中同一目标名并互相覆盖 → **静默数据丢失**。因此 **move 保持串行**并在代码注释写明原因；delete / hardlink（tmp 名按 dup 路径唯一）/ 回收站失败回退 并发执行。安全优先于吞吐。

**顺带修复的隐患**：
- 引入 `ocNone` 非零哨兵：未显式置位的下标视为「未执行」而非成功，杜绝误报 OK
- 未知操作类型改为**入口即拦截**，避免为每个文件误报一条状态
- 进度语义修正为「成功/失败/跳过均计入完成」，使 Done 能走到 Total（原实现失败项不计数，进度永不走满）

**验证**：新增 3 个并发测试——40 文件 delete（**输出顺序 == 请求顺序**、进度走满、释放空间汇总无丢失更新）、24 文件 hardlink（全部指向同一 inode 且内容一致）、混合结果（20 文件半数预删，OK/Skipped 各 10 且 Skipped 保持请求顺序、不计入释放空间）。全部通过 `-race`。原有 S1/S2/S4/S5/S7/S8 安全测试全绿。

### 🟡 Y5 — 图片预览峰值内存 ~300MB

**严重性依据**：单次点击即可触及，属**可被用户直接触发的资源尖峰**（一次预览 = 50MB 源数据 + 最高 256MB 解码位图），而产出只是 512px 缩略图，精度严重过剩。

**修复思路**：源文件上限 50MB → **20MB**；`maxPixels` 64M → **16M**（≈64MB RGBA，仍覆盖 4000×4000）。峰值由约 300MB 降至约 85MB。

**验证**：`TestThumbnailPixelBomb` 增加边界断言（25M 像素拒绝 / 恰好 16M 像素不判过大），`TestPreviewFileSizeLimits` 验证超限提示与正常缩略图路径；原 50000×50000 解压炸弹用例仍被拦截。

### 🟡 Y6 — 错误反馈全用阻塞式 `alert()`

**严重性依据**：不涉及数据安全，但**错误处理是高频路径**，13 处 `alert()` 会中断操作流、无法堆叠多条、与整体精致 UI 割裂；扫描失败时弹窗叠在进度面板上尤其糟糕。

**修复思路**：新增 `stores/toast.ts`（push/dismiss/notifyError/notifySuccess + `errText` 归一化字符串/Error/`{error}` 三种异常形态、上限 5 条防刷屏、自动消失可手动关闭）与 `components/ToastHost.vue`（右下角堆叠、主题变量跟随、`aria-live` 无障碍），替换全部 13 处调用（含 store 8 处、组件/视图 5 处）。

**验证**：用 esbuild 打包 store 后在 Node 中 stub 驱动真实逻辑，**11 项断言全过**（三种异常归一化、无 `undefined` 泄漏、上限裁剪保留最新、手动关闭、超时自动消失、空消息忽略、success kind）；`npm run build` 通过；`alert(` 全仓清零。

### 🟡 Y7 — 结果页 DOM 无上限累积

**严重性依据**：`content-visibility` 只解决**渲染**开销，Vue 组件实例与 DOM 节点的**内存**仍随滚动线性增长；万级组场景下长时间浏览会明显占用内存。

**修复思路**：放量上限 `loadCap`（默认 2000）+ 每档步长 1000；滚动追加受上限约束，达限后按钮变为「继续加载（已加载 N 组，点击再取一档）」并以警示色提示；同时引入 `loadingPage` 在途标志，修复快速滚动重复排队拉取（原 G8）。

**验证**：Node 驱动 **11 项断言全过**——首屏 100、达上限后**零请求**、`loadMore` 放量一档并加载一页、上限内滚动正常、重新查询回到首页、`loadingPage` 复位。

### 🟡 Y8 — 默认勾选全部冗余项

**严重性依据**：纯安全设计问题，但**后果最严重（不可逆删除）**。后端已有 S4 强制确认与 S2 保留项保护，然而「默认全选 + 常驻永久删除按钮」意味着一次误点仅隔一个确认框，万级结果下用户无法复核。

**修复思路**：取消预设勾选，改为**空选择 + 引导**：工具栏在无选择时显示「未选择——勾选文件，或点『全选』选中全部冗余项（保留项不可选）」并弱强调「全选」按钮；列表被替换（排序/筛选/新扫描/操作后刷新）时清空勾选，**滚动追加分页时保留**当前勾选（不打断用户正在进行的操作）。同步移除了因此变为冗余的 `resetSelection` 调用与失效的 watch。

**验证**：Node 驱动 **10 项断言全过**——默认 size=0、全选只选中冗余项（保留项被排除）、selectedFiles/Bytes 汇总正确、点击切换、追加保留勾选、列表替换清空、清除按钮生效。

---

## 三、整体测试结果

### 后端
```
go build ./...          通过
go vet ./...            通过
gofmt -l .              无输出（格式规范）
go test ./...           全部通过（cache/dedup/filter/hasher/ops/progress/scanner）
go test -race ./...     全部通过（无任何 DATA RACE）
```

### 端到端（2291 文件真实数据集，期望 200 重复组）
| 场景 | 结果 | 耗时 |
|------|------|------|
| 冷扫描（无缓存） | 200 组 / 33.0MB / 失败 0 | 44.8ms |
| 热扫描（全命中） | 200 组 / 33.0MB / 失败 0，**结果一致** | 15.1ms（**2.96×**） |
| 第三轮（验证 R1） | `last_hit` 全部续期，条目数 500 不变 | 17.4ms |

### 前端
```
npm run build           通过（dist 111KB JS / 15KB CSS）
toast store 单元断言    11/11
Y7 放量上限断言         11/11
Y8 勾选语义断言         10/10
```

### 集成
```
wails build             通过：绑定生成 → 前端编译 → 应用编译 → 打包 → 自签名
产物 build/bin/FileDedup.app（12.9MB 可执行文件）
```
（唯一警告为 macOS 部署目标链接提示，属既有噪声，与本次改动无关）

---

## 四、修改内容摘要

**后端（9 文件）**
- `internal/cache/cache.go`：新增 `Touch()` 批量续期、`LastHit()` 诊断方法
- `internal/dedup/pipeline.go`：命名返回值 + defer 统一写回（含 Cancelled）；命中路径收集与续期；分阶段进度总量；`Cancel()` 持锁；清理被架空的 buf 借用；新增 `prefilterBytes()`
- `internal/progress/progress.go`：新增 `Done()` 访问器
- `internal/hasher/hasher.go`：`HashFullSegmented` 改环形缓冲复用，签名语义澄清
- `internal/ops/executor.go`：`runIndexed` 有界并发；结果下标化按序汇总；`ocNone` 哨兵；未知类型入口拦截；进度含失败项；move 保持串行（注释说明 TOCTOU 风险）
- `app.go`：视图排序缓存 + 指纹自校验 + 3 处失效；ext 归一化提取；预览内存上限收紧

**前端（7 文件 + 2 新增）**
- 新增 `stores/toast.ts`、`components/ToastHost.vue`
- `stores/scan.ts`：alert→toast、放量上限与 `loadMore`、`loadingPage`、默认不勾选、列表替换清空勾选、暴露 `loadCap/loadingPage/resultPage`
- `views/ResultView.vue`：滚动受上限约束、按钮文案与警示色、「全选」引导、移除失效 watch
- `App.vue` 挂载 ToastHost；`GroupCard.vue` / `ConfirmDialog.vue` / `SettingsView.vue` 接入 toast

**测试（6 文件 + 1 新增）**
- 新增 `internal/ops/executor_concurrency_test.go`（3 个并发正确性测试）
- 各包补充回归测试：缓存续期 ×2、进度收敛、竞态探针、段缓冲复用、排序缓存、预览边界

**规模**：17 文件修改 + 3 文件新增，`797 insertions(+) / 156 deletions(-)`

---

## 五、未修项（🟢 建议级，按需再做）

> **状态更新（同日）**：下列 1~6 项已在后续一轮中**全部实施并逐项验证**，
> 详见 `docs/optimize-report-2026-09-16.md`；第 7 项经复核决定**保留**。
> 其中 3 项（本节 2、3、5）经实测发现原判断不成立，已在优化报告中更正。

这些是优化建议而非缺陷，不影响正确性与稳定性：

1. `pipeline.go` paranoid `streamEqual` 每次比较分配 2×256KiB → 可复用池
2. `scanner.go` `relativeTo` 每文件线性扫根且重复拼接 → 预计算 `rootsSep`
3. `filter.go` 扩展名列表线性 `EqualFold` → 归一化为 map
4. `cache.go` `idx_cache_size` 索引无查询使用 → 确认后可删（减少写放大）
5. `frontend/src/.DS_Store` 仓库垃圾文件 → 删除并加入 .gitignore
6. IO 并发按存储介质自适应（HDD=1~2 / SSD=workers），当前为 CPU 核数-1
7. `SettingsView` 清空缓存的 `confirm()` 仍为原生对话框（属真实确认场景，保留）

> 如需继续推进，建议优先级：4 → 5 → 1 → 2 → 3 → 6。
