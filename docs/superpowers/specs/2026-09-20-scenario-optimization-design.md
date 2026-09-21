# 设计文档：真实工况与 OS 差异缺口优化（M6/M7/M8/M9 总纲）

- 日期：2026-09-20
- 状态：**已批准、未实施**（本文是调研 + 方案总纲；每个 P0 项实施前需各自展开细化设计）
- 版本目标：交付为新里程碑（版本号由用户发布时明示，本文不预设）
- 调研方法：功能口径以 09 手册对账；UI 以 `frontend/src` 全量走查；OS 边界以
  `internal/` 逐场景 grep + 代码锚点确认（全部结论附 file:line，可复核）

## 0. 需求与已确认决策

用户裁定（2026-09-20）：

| # | 决策点 | 裁定 |
|---|---|---|
| 1 | 云盘占位文件（OneDrive/iCloud 未下载）默认策略 | **默认跳过 + 可见计数**（"已跳过 N 个云占位文件"，不静默、不触发下载；提供可选开关"允许下载后参与"，默认关） |
| 2 | 里程碑推进顺序 | **M6（真实工况安全）与 M8（规模化体验）并行，M7（跨平台证据）随后**，M9（交付完整）收尾 |
| 3 | 文档落档 | 本设计稿 + 开放项登记进 `docs/04` §6（新增 §6.7） |

**明确不做**（本轮范围外，理由见各节）：

- 相似文件/感知哈希去重（与"内容级判定"的产品承诺是两个物种，需独立立项）；
- 增量续扫（M4 已以"缓存 + 历史快照"为替代语义，04 §1.1 结论维持）；
- G5~G11 性能热点（维持"无热点证据不动"）；
- Linux 非 UTF-8 文件名的完整字节序支持（复杂度高、受害面窄，仅做"不白屏"降级）。

## 1. 现状关键事实（调研结论，含代码锚点）

### 1.1 云盘占位文件——全仓零检测（P0，费用与正确性风险）

- 无任何 `IO_REPARSE_TAG_CLOUD` / iCloud dataless / Google Drive 检测（grep 零命中）。
- 扫描阶段仅凭 `IsRegular()`+size 放行（`internal/scanner/scanner.go:256-263`）；
  哈希阶段 `os.Open`+`ReadAt`（`internal/dedup/pipeline.go:365,378,532`）会**直接触发按需下载**。
- Windows 侧仅间接依赖 Go 运行时把部分 reparse point 报为 `ModeIrregular`，无显式保证；
  macOS iCloud 未下载文件（regular + 正常 size）**完全无防护**。
- 已有认知痕迹：`internal/hasher/hasher.go:45` 注释承认占位文件"逻辑大小≠可读长度"短读。

### 1.2 稀疏/压缩文件——可释放空间系统性虚高（P0，会计口径）

- 哈希与 `Reclaimable=(n−1)×size` 全用**逻辑大小**（`pipeline.go:658`、`model/model.go:89`）；
  无 `st_blocks` / `GetCompressedFileSize` 参与（grep 零命中）。
- 后果：NTFS 压缩卷、稀疏文件（虚拟机镜像、`cp --sparse`）场景 UI 承诺的空间是虚数，
  与本项目"计数不许说谎"的既有原则（§6.6.2 三层计数）冲突。

### 1.3 Windows ADS 备用流——去重会丢备用流（P0，数据丢失级）

- 哈希只读默认数据流；两文件默认流相同、备用流不同（备份/杀软把元数据写进 ADS 的真实场景）
  会被判重复，删除/合并后**备用流内容永久丢失**，且事后校验（同样只看默认流）发现不了。

### 1.4 系统目录/伪文件——零硬排除（P0，从盘根扫描必撞）

- `filter.go` / `scanner.go` 无任何平台保护清单；前端默认 `ExcludePaths: []`
  （`frontend/src/stores/scan.ts:14-17`）。实际防护仅剩"`.` 前缀隐藏规则"，挡得住 macOS
  `.Trash`/`.Spotlight-V100`，挡不住 Windows `pagefile.sys`/`System Volume Information`/
  `WindowsApps`、Linux `/proc`/`/sys`/`lost+found`、TM 本地快照。
- 后果：盘根扫描产生海量 Failed 噪声、无谓耗时，还可能触发反病毒软件对抗。
- 连带：Windows 保留名（CON/PRN/NUL…）无任何处理。

### 1.5 已登记项（本文不重复设计，仅排期）

- fscase 单根探测缺口 → 04 §6 C1，修法已定（无条件逐根探测），并入 M6；
- CI 无 macOS job → §6 B8，并入 M7；
- Windows 真机证据包（H6 四场景 / longPathAware 实测 / 软链接权限四环境 / 处理策略清单）
  → §6.5、§6.6.2、§6.6.3 各自的 E 组补项，统一并入 M7；
- 1 万组 ≥50fps 从未验证 → §6 B1，M8 一并闭环；
- 报告导出空桩 / language 死选项 → §6 C3 / A3+C2，M9 决策落地。

### 1.6 UI 侧真实动线缺口（M8 依据）

- 扫描中整页被进度面板替换（`ScanView.vue` v-if/v-else），且 `startScan` 即
  `clearStaleResult()`（`scan.ts:154-170`）——大目录扫描十几分钟时用户无事可做，
  上一次结果还被提前销毁；
- 无虚拟滚动（仅 `content-visibility` 兜底，`GroupCard.vue:95`）、无组内路径搜索、
  无跨放量保留勾选、无 Shift 范围选、无右键菜单（§6.6.2 D 组"右键加入优先文件夹"同源）；
- 无导出：失败清单仅"复制全部"到剪贴板（`FailedDrawer.vue:18-24`）；
- 预览无视频/音频缩略（P3）；设置页原生 `confirm()` 与全站 toast 风格不一致（P3 瑕疵）。

## 2. 方案设计

### 2.1 `internal/cloudfile`：云占位检测（P0-1）

| 平台 | 判据 | 备注 |
|---|---|---|
| Windows | 句柄 `DeviceIoControl(FSCTL_GET_REPARSE_POINT)` 的 reparse tag ∈ CLOUD 段（`0x9000xxxx`，含 CSV 变体） | 复用 `fsid` 已有句柄路径，避免额外开档；Go `ModeIrregular` 只作兜底不作依据 |
| macOS | `getattrlist` 查 `ATTR_FILE_DATALESS`（fileprovider 未下载） | 新增 `probe_cloud_darwin.go`，参照 `media/probe_darwin.go` 的 cgo-free xattr 路子（`unix.Getxattr` 或 getattrlist 直调） |
| Linux | 不处理 | 无统一占位语义；rclone/gvfs 挂载走既有网络卷降并发 |

- 接入点：scanner 阶段 0（`IsRegular` 放行后追加一次判定），判为占位 → **不计入语料、
  单独计数** `SkippedCloudFiles`，进 `scan:done` 统计与结果页横幅。
- 设置项：`AllowCloudHydration`（默认 false）。开启后行为=现状（照常读取，隐式触发下载），
  文案必须写明"会产生下载流量与耗时"。
- 哈希短读兜底不动：现有短读处理（S1 相关回归）继续兜 macOS 判定漏网的情形。

### 2.2 实占口径（P0-2）

- 新增采集：unix `st_blocks×512`（`fstat` 顺带，零额外 syscall）、Windows
  `GetCompressedFileSizeW`（fsid 句柄路径顺带）。
- **判定语义不变**（分组仍按逻辑大小 + 内容哈希），只改会计：`Reclaimable` / 组大小 /
  历史与记录页统计改用实占；UI 双数呈现 `可释放（实占）X · 逻辑 Y`，避免"数字变小了"
  被当成回归。缓存表不加列（与正确性无关）。
- 探针测试方向：稀疏文件上 `Reclaimable(实占) < Reclaimable(逻辑)`，普通文件两数相等。

> **实施回执（2026-09-21，交付于 `ef2b404`，划账见 04 §6.9.3；本节原文按登记当时保留不改）**：
> 上面四条里有**三处与实施不符**，都在这段更正，防止下一轮照抄：
> ① "Windows `GetCompressedFileSizeW`（fsid 句柄路径顺带）"前提不成立——扫描阶段在
> Windows 上**刻意不开句柄**（04 §6.8 硬链接那轮的结论），故实现改**按路径**查询，
> 代价是每个通过过滤器的候选文件多一次元数据调用（登记 **M29**，真机未测）；
> ② "`Reclaimable` …改用实占"落为**双口径并存**（新增 `ReclaimableActual`，
> `Reclaimable` 语义一字不动）——它是历史表 `reclaimable` 列的既有口径，换语义会让
> 既往清理数字与库里旧行对不上；详理由见实施稿 §3.2 注 2；
> ③ "普通文件两数相等"**实测不成立**：块对齐使实占**可以大于**逻辑（1 KiB 实写占
> 4 KiB；benchgen 语料实测 86.9MB vs 86.3MB）。所以判据不是"相等"而是
> "`actualKnown` 决定这两数能不能拿来比较"，且 `From` 明确**不做 `min(实占,逻辑)` 封顶**。
> UI 双数呈现如本条所述属 **M8**，本轮只到 JSON / CLI 字段（用户裁定③）。

### 2.3 ADS 防护（P0-3，仅 Windows 生效）

- `ops/verify.go` 执行前追加 `HasNonDefaultStream(path)`（`FindFirstStreamW`，
  枚举到 `:$DATA` 以外的任意流即认定）；命中 → 该项记 **Failed**，原因文案
  「文件含备用数据流，去重会丢失备用流内容，已拒绝操作」。
- 与云检测同理下沉纯逻辑（判定函数无 build tag，进 Linux CI，沿用 H6 分层教训）。

### 2.4 系统保护清单（P0-4）

- 新增 `internal/filter`（或独立 `internal/sysguard`）内置清单，scanner 目录级剪枝复用
  既有剪枝通道；文件级保护 `pagefile.sys`/`hiberfil.sys`/`swapfile.sys`（盘根、隐藏属性）。
- 初版清单：
  - Windows：`$Recycle.Bin`、`System Volume Information`、`WindowsApps`、`DumpStack.log.tmp`、
    盘根根级 `pagefile.sys`/`hiberfil.sys`/`swapfile.sys`；保留名文件（`CON`、`PRN`、`AUX`、
    `NUL`、`COM1-9`、`LPT1-9` 及其带扩展形态）；
  - macOS：`/.Spotlight-V100`、`/.fseventsd`、`/.Trashes`、`/.DocumentRevisions-V100`、
    `/private`、`/System`、TM 本地快照目录（`/Volumes/*/.com.apple.TimeMachine*.snapshots`）；
  - Linux：`/proc`、`/sys`、`/dev`、`/run`、`/lost+found`。
- 语义（与用户过滤器的关系必须写死）：清单**不可被用户设置关闭**；唯一例外是用户**显式把
  清单内路径本身设为扫描根**时放行并弹一条"已脱离系统保护"警示——保留专家通道，堵死误伤。
  被剪枝的目录计数为"已保护跳过 N"，在结果页横幅可见（与云计数同一呈现位）。

### 2.5 UI 动线包（M8）

| 项 | 方案要点 |
|---|---|
| 扫描中浏览 | `startScan` 不再 `clearStaleResult`；结果页顶部挂"陈旧结果"水印横幅，`scan:done` 后自动刷新为新结果；进度收进全局细条 + 可展开抽屉（保留暂停/恢复/取消入口） |
| 虚拟化 | `GroupCard` 列表自研窗口化（固定行高折叠态 + 展开项单独渲染），不引组件库；完成后把"1 万组 ≥50fps"做成脚本化定量验证入 nightly |
| 搜索与勾选 | 组内路径子串过滤框；勾选集合按文件 id 持久、跨放量/跨排序不丢（切换结果集仍清空）；Shift 范围选 |
| 右键菜单 | 文件行右键：加入优先文件夹 / 加入排除路径 / 复制路径（补 §6.6.2 D 组缺口） |
| 导出（M9） | `ExportReport` 落真：JSON/CSV（结果组 + 失败清单 + 云/保护计数），走系统保存对话框 |

## 3. 操作系统差异矩阵（本方案新增行为的平台适用面）

| 机制 | Windows | macOS | Linux |
|---|---|---|---|
| 云占位检测 | reparse tag（主判据） | `ATTR_FILE_DATALESS` | 不适用（网络卷路径兜底） |
| 实占口径 | `GetCompressedFileSizeW` | `st_blocks`（APFS clone 注意：clone 文件 `st_blocks` 反映共享块，双计风险需探针验证后定文案） | `st_blocks` |
| ADS 防护 | `FindFirstStreamW` | 不适用（xattr 不参与判定——Time Machine 备份工具场景真实受害面评估后如需要另立项） | 不适用 |
| 保护清单 | 盘根伪文件 + 保留名 | 快照/元数据目录 | procfs 系 |
| 真机证据 | **M7 集中补**（当前证据最薄） | 已有 GUI 自动化补验基础 + 新增 CI job | XDG 单测覆盖好；补 per-uid `.Trash-$UID` 解析 |

## 4. 测试与门禁约定（沿用项目惯例）

1. 每项 P0 修复必须先有**修前必红**的探针测试，修复后绿；关键判定函数做**变异验证**并留记录表
   （对齐 H6 的六重变异做法）；
2. 纯逻辑一律下沉无 build tag 文件进 Linux CI 主门禁（H6 分层教训）；
3. 稀疏/ADS/云检测需构造真实夹具：Linux CI 上 `fallocate --create-hole` 造稀疏、
   tmpfs/xattr 造 dataless 不可行处用注入点，平台层单独在 macOS/Windows 真机冒烟；
4. M8 虚拟化以"1 万组 ≥50fps"脚本化定量为验收门禁（补 §6 B1 旧账）；
5. 文档同步：每项交付同步 09（功能口径）+ 04（§1 交付总览、§3.5 checklist、§6 销账）。

## 5. 里程碑与顺序（已裁定：M6 ∥ M8，M7 随后）

| 里程碑 | 内容 | 依赖/风险 | 预估 |
|---|---|---|---|
| **M6 真实工况安全** | §2.1 云检测、§2.2 实占、§2.3 ADS、§2.4 保护清单 + 04 §6 C1 fscase 单根 | `st_blocks` 在 APFS clone 上的语义需先做证据实验再定文案；保护清单需三平台真机各扫一次盘根回归 | ~1.5 周 |
| **M8 规模化体验**（与 M6 并行，不同工作面） | §2.5 扫描中浏览、虚拟化 + 50fps 门禁、搜索/勾选/右键 | 纯前端为主，与 M6 后端改动无冲突；虚拟化须保 `content-visibility` 回退 | ~2 周 |
| **M7 跨平台证据** | Windows 真机验收包（H6 四场景、长路径、软链接权限四环境、处理策略 7 项、unicode/保留名）+ CI macOS job + Linux `.Trash-$UID` | 依赖一台 Windows 真机（含 `LongPathsEnabled` 配置权限）；M6 新平台行为并入同一轮真机清单执行 | ~1 周（真机在场） |
| **M9 交付完整** | 报告导出、i18n 死选项处置（A3 决策）、strip + 体积门禁（B4）、契约 diff（B5）、三平台安装/卸载冒烟 | A3 需用户决策：摘除 or 上 vue-i18n | ~1 周 |

**M6 实施前置实验**（一次性，进设计细化稿）：APFS clone 文件的 `st_blocks` 双计实验、
Windows 真机 OneDrive 占位 tag 采集、`FindFirstStreamW` 在目标环境的开销评估。

## 6. 开放项登记映射（写入 04 §6.7）

| 本文编号 | 登记去向 |
|---|---|
| 1.1 云占位 / 1.2 实占 / 1.3 ADS / 1.4 保护清单+保留名 | §6.7 新增（C 组性质：数据正确性/会计真实性） |
| 1.5 各项 | 原位不动（C1 / B8 / B1 / C3 / A3 + 各 E 组补），仅 §6.7 加指针 |
| 1.6 UI 动线缺口 | §6.7 新增（B/D 组性质） |
| Linux `.Trash-$UID`、非 UTF-8 文件名降级 | §6.7 新增（E/C 组） |
