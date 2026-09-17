# FileDedup 优化项执行报告

**范围**：审查报告（`docs/code-review-2026-09-16.md`）与修复报告（`docs/fix-report-2026-09-16.md`）第五节列出的 🟢 优化建议，共 7 项
> **编号说明（2026-09-17 补记）**：原审查报告的 🟢 清单实为 **G1–G12 共 12 项**，本报告只执行了其中筛选出的 6+1 项，且编号与前述报告不一一对应（如本报告「G5 `.DS_Store`」= 原审查 G12）。完整 12 项的落地状态见文末「附：G1–G12 三态台账」。
**执行顺序**：按报告建议优先级 4 → 5 → 1 → 2 → 3 → 6（第 7 项经复核决定保留，见文末）
**方式**：与上一轮一致——**改一项 → 立即验证一项**，不批量改完再测；性能类改动均做「回退对照」以证明探针有效
**日期**：2026-09-16

---

## 一、总览

| # | 优化项 | 性质 | 关键量化结果 | 结果 |
|---|--------|------|--------------|------|
| G4 | `idx_cache_size` 死索引 | 写放大 / 库体积 | 库体积 **-11.4%**，插入 **-23.9%**，覆盖 **-19.4%** | ✅ |
| G5 | `.DS_Store` 仓库垃圾 | **实为发布缺陷** | 元数据不再进二进制（内嵌表 1 → 0） | ✅ |
| G1 | paranoid 比对缓冲 | GC 压力 | 比对分配 2,621,440 → **1,272 字节（-99.95%）** | ✅ |
| G2 | `relativeTo` 重复拼接 | CPU | **57.0 → 10.8 ns/op（5.3×）** | ✅ |
| G3 | 扩展名匹配 | CPU | **0.68 ~ 4.5 ns/op**，长列表最大 **29.8×** | ✅ |
| G6 | IO 并发介质自适应 | 吞吐（HDD/网络卷） | 判定开销：内置盘 **5µs** / 外置卷 93ms（缓存） | ✅ |
| G7 | `SettingsView` 原生 confirm | — | 复核后**保留**（真实破坏性操作确认） | ⏸️ |

> 本轮同时**更正了原审查报告中 3 条不成立的判断**，见第五节。

---

## 二、逐项：依据 · 修法 · 验证

### G4 — `idx_cache_size` 死索引

**性质**：不是缺陷，是纯粹的写放大。`size` 从未出现在任何 `WHERE` / `ORDER BY` 中——`Lookup`/`Touch` 走 `path` 主键，淘汰走 `idx_cache_last_hit`，`GetStats` 走全表聚合。因此该索引**只付维护成本、无任何查询收益**。

**修法**：从建表语句中移除，并加 `DROP INDEX IF EXISTS idx_cache_size` 完成旧库迁移（已装用户无需重建缓存）。

**验证**：

| 指标（10 万行） | 修复前 | 修复后 | 变化 |
|---|---|---|---|
| 库体积 | 10,375,168 B（103.8 B/行） | 9,199,616 B（92.0 B/行） | **-11.4%** |
| 首次写入 | 332.6 ms | 253.0 ms | **-23.9%** |
| 全量覆盖写入 | 424.3 ms | 342.0 ms | **-19.4%** |
| 索引清单 | `last_hit` / `size` / autoindex | `last_hit` / autoindex | 已清除 |

- 新增 `TestOpenDropsLegacyUnusedIndex`：手工重建含 `idx_cache_size` 的旧库 → `Open` 后索引消失、`last_hit` 索引仍在、既有记录仍可命中且 `last_hit` 未被篡改
- **回退对照**：把 `DROP` 换回 `CREATE` → 测试 `FAIL`（`idx_cache_size 未被清除`），探针有效
- **真实旧库迁移**：用 `sqlite3` 向端到端缓存库手工植入该索引 → 一次热扫描后索引消失，200 组结果与冷扫描完全一致

### G5 — `.DS_Store`（发现比报告描述更严重的问题）

**原报告判断**：「`frontend/src/.DS_Store` 仓库垃圾文件 → 删除并加入 `.gitignore`」

**复核后的事实**（两点与原报告不符）：

1. `.gitignore` **早已有** `.DS_Store` 规则，且 `git check-ignore` 对 `.DS_Store` / `frontend/src/.DS_Store` / `frontend/dist/.DS_Store` / `foo/bar/.DS_Store` 全部命中；`git ls-files` 显示**没有任何 `.DS_Store` 被跟踪**。→ 「加入 .gitignore」这条已无必要。
2. 但存在一个**更严重、原报告未发现**的问题：`main.go` 的 `//go:embed all:frontend/dist` 中的 `all:` 前缀会连点开头的文件一起嵌入，因此 **`frontend/dist/.DS_Store`（6148 字节 Finder 元数据）被打进了发布二进制**。

**取证**：在旧产物中直接命中内嵌路径表 —— `strings build/bin/.../FileDedup | grep DS_Store` 输出 `frontend/dist/.DS_Store`。

**修法**：去掉 `all:` 前缀。Go 的 embed 规则本身即排除 `.` / `_` 开头的文件，从而形成**编译期保证**：无论 `dist` 被谁怎么污染，元数据都进不去。

**验证（保留污染的对照实验）**：在 `frontend/dist/.DS_Store` **仍存在**的前提下重新编译 ——

| 二进制 | `frontend/dist/.DS_Store` | `frontend/dist/index.html` |
|---|---|---|
| 修复前（`all:`） | **1 次命中** | 1 次 |
| 修复后（无 `all:`） | **0 次** | 1 次（正常目录不受影响） |

`wails build` 产物同样为 0 次命中，集成无回归。

**关于磁盘清理的诚实说明**：删除了 12 处 `.DS_Store`，其中项目根与 `frontend/` 两处被 Finder **在数秒内立即重建**（macOS 正常行为，只要 Finder 开着该目录）。这说明「删文件」本身不可持续——**真正有效的是编译期保证**。因此本项的交付价值在 embed 指令，而非磁盘清理。

### G1 — paranoid 逐字节比对缓冲复用

**性质**：非正确性问题，是万级组场景下纯粹的 GC 负担。原实现在 `streamEqual` 内部**每次比对**新分配 2×256KiB，一组 N 个文件即 N-1 次；且调用点是串行循环，这些短命大对象毫无必要。

**修法**：引入 `verifier` 持有可复用缓冲对，整趟 paranoid **只分配一对**，组间与文件间复用；分配次数从 O(Σ 组内文件数) 降到 **O(1)**。

**验证**：`TestVerifierReusesBuffers` 以 `TotalAlloc` 增量断言（2MiB 文件 × 6，跨 3 个分块）：

```
修复后： 1,272 字节
不复用： 2,621,440 字节（5 次比对 × 2 × 256KiB）
```

**回退对照**：改回「每次比对新分配」→ 实测 `2,622,712 字节` 并 `FAIL`，探针有效。

**等价性**：新增 `TestVerifierMultiChunkEquivalence`（跨 3 个 256KiB 分块，含"仅首字节不同"与"仅最后不完整块不同"两种差异）与 `TestVerifierSeekResetBetweenFiles`（代表文件句柄必须在每次比对前复位，否则第 3 个文件起会误读 EOF）。原有 `TestVerifyGroupTamper` / `TestPipelineParanoidNoFalsePositive` 全绿。

### G2 — `relativeTo` 根前缀预计算

**原报告判断**：「每文件线性扫根且重复拼接 → 预计算 `rootsSep []string`，百万文件级**省大量小分配**」

**修法**（与报告建议一致）：预计算「根 + 分隔符」前缀，`full[len(prefix):]` 切片返回，零分配。

**验证**：新增 `BenchmarkRelativeTo` 内置旧实现作对照 ——

```
Precomputed    10.78 / 10.73 / 10.83 ns/op    0 B/op   0 allocs/op
NaiveConcat    57.18 / 57.00 / 56.79 ns/op    0 B/op   0 allocs/op
```

**⚠️ 更正报告的收益归因**：`allocs/op` 改造前后**都是 0**。Go 编译器对这类短字符串拼接会做栈分配（结果不逃逸），所以原报告「省大量小分配」的**前提并不成立**。真实收益全部在 CPU：省去每个文件对每个根的拼接调用与重复长度计算，**5.3×**。等价性由 `TestRelativeToEquivalence` 覆盖 10 例（多根 / 直接子文件 / 根自身 / 根外路径回退 / 前缀相似根 `/a` vs `/aa`）。

### G3 — 扩展名匹配：**不能一律改 map**

**原报告判断**：「扩展名列表线性 `EqualFold` → 归一化为 map」

**先用纯 map 实现并实测，发现这是性能倒退**：

```
列表 3 项：  线性 6.6 ns  vs  map 12.0 ns   ← map 慢 1.8×
列表 64 项： 线性 121.6 ns vs map 11.3 ns  ← map 快 10.8×
```

map 的哈希开销对短列表是净负担，而**常见配置恰恰就是 1~5 项**（如 `.tmp,.log,.DS_Store`）。照报告原样实施会让默认场景变慢。

**修法**：混合策略 —— 顺带在编译期把列表元素预小写（旧实现每文件都在做 `EqualFold`），然后按长度选择表示：

- `< 12` 项 → 保留切片线性比较（元素已小写，直接 `==`，比旧 `EqualFold` 更快）
- `>= 12` 项 → 建 map，做到与列表长度无关

阈值 12 由 `BenchmarkExtMatch` 逐档实测（1/2/4/6/8/12/16/32/64）确定，**交叉点落在 8 与 12 之间**（N08 线性 4.70 < map 6.19；N12 线性 7.10 > map 4.60）。

**验证**（命中项置于列表末位 = 线性最坏情况）：

| 列表长度 | 旧 `EqualFold` | 新实现 | 提升 |
|---|---|---|---|
| 1 | 2.96 ns | **0.68 ns** | 4.4× |
| 4 | 10.06 ns | **2.00 ns** | 5.0× |
| 8 | 18.40 ns | **4.70 ns** | 3.9× |
| 12 | 26.36 ns | **4.53 ns** | 5.8× |
| 16 | 34.34 ns | **4.50 ns** | 7.6× |
| 64 | 134.5 ns | **4.51 ns** | **29.8×** |

全部 `0 B/op 0 allocs/op`。语义等价由 `TestExtSetEquivalence` 覆盖 11 组列表 × 14 个扩展名（含空列表、空串项、重复项、无点写法、大小写混杂），并校验两条分支各自被正确选中；`TestExtMatchNoAllocs` 锁定零分配契约。

### G6 — IO 并发按存储介质自适应

**性质**：并发度恒为「核数-1」对 SSD 合理，但机械盘上并发随机读会引发寻道抖动、网络卷上会因往返排队与带宽争抢互相拖累，**两者都是"并发越高越慢"**。

**修法**：新增 `internal/media` 包，按开销从低到高三级判定：

1. **网络文件系统 → Network**：读 statfs 的 `f_fstypename`（约 0.7µs，免费）
2. **与启动卷同物理盘 → SSD**：比较设备名物理盘标识（免费）。**这一步是整个设计的成本关键**——macOS 用户主目录在 `/System/Volumes/Data`（`disk3s5`），与系统卷 `/`（`disk3s1s1`）是不同卷宗但同一物理盘（`disk3`），若只按挂载点比较就会漏掉最常见场景而白付 90ms
3. **其余卷 → 调 `diskutil`**：约 90ms，结果按挂载点缓存

并发度取 `min(核数-1, 2)`（仅机械盘/网络卷），**只降级不升级**；探测失败/平台不支持一律返回 `Unknown` → 维持原值，故任何环境下最坏等价于改造前行为。

**实测判定链路**：

| 路径 | 判定 | 挂载点 | 设备 | 同启动盘 | 耗时 |
|---|---|---|---|---|---|
| `/` | ssd | `/` | `/dev/disk3s1s1` | true | **5.3 µs** |
| `/tmp` | ssd | `/System/Volumes/Data` | `/dev/disk3s5` | true | **5.7 µs** |
| `/Users/just` | ssd | `/System/Volumes/Data` | `/dev/disk3s5` | true | **1.7 µs** |
| `/Volumes/fx` | ssd | `/Volumes/fx` | `/dev/disk7s1` | false | 93.2 ms（缓存） |

**过程中踩到并已处理的两个 macOS 陷阱**：

- `diskutil` 失败时**退出码仍为 0**，错误写在 plist 里（`<key>Error</key><true/>`）→ 必须解析内容判成败，不能只看退出码。已加 `isDiskutilError` + 以真实错误 plist 为样本的 `TestIsDiskutilError`
- `diskutil info -plist <任意路径>` 对 `/var/folders/...` 这类合成路径返回 `Could not find disk` → 必须传**卷挂载点**（取自 statfs 的 `f_mntonname`）

**安全性用例**：`TestDiskKey` 覆盖设备名解析边界（`disk3s1s1`/`disk3s5` 须归为同一盘；`disk3` 无分区后缀时**不能被名字里的首个 `s` 切坏**；`disk1` 不得与 `disk10` 混淆）；`TestClassifyMissingFields` 保证字段缺失一律 `Unknown` 而**绝不猜测**；`TestClassifyExternal` 保证 `isInternal` 为 nil 时不得短路（否则会把外置盘误判成内置固态）；`TestAutoThreadsWithinBounds` 锁定「自动并发度 ∈ [1, 核数-1]」且空根/不存在根均维持默认。

**诚实说明**：本机 `/Volumes/fx` 是**外置 USB 固态**（`SolidState=true`），因此判定为 SSD、并发度维持 9 —— 该特性对**本机不改变行为**，价值在机械盘与网络卷用户。另：非 darwin 平台暂不探测（返回 `Unknown` = 维持原行为），接入点已在 `probe_other.go` 注释中说明。

---

## 三、整体测试结果

### 后端
```
go build ./...              通过
go vet ./...                通过
gofmt -l .                  无输出（格式规范）
go test ./...               全部通过（10 个包，含新增 internal/media）
go test -race -count=1 ./... 全部通过（无任何 DATA RACE）
```

### 端到端（2285 文件真实数据集）

| 场景 | 结果 | 耗时 |
|------|------|------|
| 冷扫描（内置盘，无缓存） | 200 组 / 已释放 38,124,097 | 0.52 s |
| 热扫描（全命中） | 200 组，**与冷扫描完全一致** | 0.01 s |
| 外置卷扫描（走 diskutil 慢路径） | 正常产出 | 0.07 s |

> 冷扫描耗时与上一轮（0.50s）持平 —— 因为内置盘走免子进程快路径，本项**未给常见场景引入额外开销**。

### 集成
```
wails build             通过：绑定生成 → 前端编译 → 应用编译 → 打包 → 自签名
产物 build/bin/FileDedup.app（12,967,008 字节）
```
（唯一警告为 macOS 部署目标链接提示，属既有噪声）

### 本轮新增测试
**22 个测试 + 2 个基准**：media 包 13 项、scanner 2+1、filter 2+1、dedup 4、cache 1。全部通过。

---

## 四、变更摘要

**新增包（5 文件 / 664 行）**
- `internal/media/media.go`：`Class` 分类、`AutoWorkers`（只降级不升级）、`Classify`（多根取最受限）、`RootsClass`
- `internal/media/probe_darwin.go`：三级判定链、`diskKey` 物理盘解析、`diskutil` 调用（超时 + 挂载点缓存 + 错误 plist 识别）
- `internal/media/probe_other.go`：非 darwin 返回 `Unknown`（= 不介入）
- `internal/media/media_test.go`、`probe_darwin_test.go`：13 项用例

**修改文件**
- `main.go`：embed 去掉 `all:` 前缀，杜绝点开头文件（macOS 元数据）进包
- `internal/cache/cache.go`：移除 `idx_cache_size`，加 `DROP INDEX IF EXISTS` 迁移旧库
- `internal/scanner/scanner.go`：预计算根前缀 `rootPrefixes`；预编译过滤器 `filter.Compile`
- `internal/filter/filter.go`：重写为 `Matcher`（预编译 + 短列表线性/长列表 map 混合）
- `internal/dedup/pipeline.go`：新增 `verifier`（缓冲复用）替换 `verifyGroup`/`streamEqual`；新增 `autoThreads` 接入介质自适应
- 对应 6 个测试文件补充用例

**累计规模**（含上一轮 10 项缺陷修复）：`git diff --stat` = **22 文件修改，1397 insertions(+) / 192 deletions(-)**

---

## 五、对原审查报告的更正

执行过程中发现原报告有 3 条判断不成立，已按实测结论实施并在此备案：

| 条目 | 原报告结论 | 实测结论 | 处理 |
|---|---|---|---|
| G5 | `.DS_Store` 需「加入 .gitignore」 | `.gitignore` **早已覆盖**，且无文件被跟踪；真正的风险是 `all:frontend/dist` 把 dist 里的元数据**编进了二进制** | 改为修 embed 指令（收益更大），.gitignore 未改动 |
| G2 | 预计算可「省大量小分配」 | 改造前后均为 **0 allocs/op**（Go 对短字符串拼接做栈分配），前提不成立 | 仍实施，但收益归因更正为 CPU **5.3×** |
| G3 | 扩展名列表「归一化为 map」 | 纯 map 会让 **1~8 项的常见配置倒退约 1.8×** | 改为**按长度混合**策略（阈值 12，实测交叉点） |

---

## 六、未做项

- **G7** `SettingsView` 清空缓存仍用原生 `confirm()`：复核后**维持原判保留**。这是真实的破坏性操作确认场景，原生模态对话框在语义与可靠性上都恰当，改用应用内 toast/dialog 反而削弱确认强度。

---

## 七、结论

7 项 🟢 建议中 6 项已实施并逐项验证，1 项经复核保留。三点值得强调：

1. **最有价值的一项不在原报告的量化预期里**：G5 原本只是「清理仓库垃圾」，实际查出发布二进制内嵌 macOS 元数据，并在编译期根治。
2. **两项的原始前提经实测不成立**（G2 的分配归因、G3 的 map 方案），若照报告原样实施，G3 会**让默认场景变慢**。这说明「优化建议」同样需要量化验证，不能凭直觉落地。
3. **G6 的成本设计比功能本身更关键**：90ms 的子进程探测若放在最常见的内部盘扫描上，会让 45ms 级的扫描翻三倍；靠「同物理盘」判定把常见场景压到 5µs，才使这项优化在净收益上成立。

---

## 附：G1–G12 三态台账（2026-09-17 补记）

原审查报告（`code-review-2026-09-16.md` §第五节）的 🟢 清单共 **12 项**，本报告仅执行了筛选出的子集且编号错位。此处按**原报告编号**逐一登记最终状态，消除「未做也未声明」的悬置项：

| 原编号 | 优化项 | 状态 | 落地去向 / 现状 |
|---|---|---|---|
| G1 | paranoid 比对缓冲复用 | ✅ 已闭环 | 本报告 G1（分配 -99.95%） |
| G2 | `relativeTo` 根前缀预计算 | ✅ 已闭环 | 本报告 G2（5.3×；原「省大量小分配」归因经实测不成立，已更正） |
| G3 | 扩展名匹配 | ✅ 已闭环（方案变更） | 本报告 G3：未采纳纯 map（实测默认配置倒退 ~1.8×），改按长度混合策略 |
| G4 | 删除 `idx_cache_size` 死索引 | ✅ 已闭环 | 本报告 G4（库体积 -11.4%） |
| G5 | Lookup 逐文件点查 → Prepare/预载 | ⏸️ 未实施 | `Lookup` 保持 `QueryRow` 点查（走主键索引）；仅批量写路径在事务内 `Prepare`。如未来出现万级命中场景，可重估任务级预载 |
| G6 | scanner `visited` 锁 → `sync.Map` | ⏸️ 未实施 | 仍为 map + 互斥锁（每目录一次）；原报告即标注「深目录百万级时」才需要，当前无实测热点证据 |
| G7 | GroupCard 模板内「图片组」正则 | ⏸️ 未实施 | 模板内仍为 `group.files.some(f => /\.(png\|jpe?g\|...)$/i.test(f.path))` 内联正则（2026-09-17 复核确认）；后端 `GroupView` 加 `isImage` 字段的方案未实施 |
| G8 | 无限滚动 in-flight 防抖 | ✅ 已顺带闭环 | 随 Y7 放量上限落地 `loadingPage` 在途标志（见 `fix-report-2026-09-16.md`） |
| G9 | `resetSelection` 收敛进加载入口 | ✅ 已顺带闭环 | Y8：列表被替换（排序/筛选/刷新）时在 `loadResultPage` 内统一清空 |
| G10 | 排序 select / 扩展名 input 的 a11y | ✅ 已闭环 | 两控件均已带 `aria-label`（见 `ui-fix-2026-09-16.md`） |
| G11 | 写回后全表 `COUNT(*)` | ⏸️ 未实施 | `Store` 的淘汰判定仍全表计数（cache.go `SELECT COUNT(*)`）；如需优化可与 `idx_cache_last_hit` 聚合合并重估 |
| G12 | `.DS_Store` 仓库垃圾 | ✅ 已闭环（升级实施） | 本报告 G5：不止删文件/加 ignore，改为修 embed 指令根治二进制内嵌元数据 |

**合计**：已闭环（含顺带/方案变更）8 项，明确未实施 4 项（G5 / G6 / G7 / G11，均为「无当前热点证据、留待重估」类，非缺陷）。
