# 拟处理文件清单查询（2026-09-23）设计段

## 0. 需求与裁定记录

用户原话："增加待处理文件清单查询功能。" 经两轮确认（AskUserQuestion，非推定）：

- **口径**：执行前拟处理集 —— "如果现在点执行，会处理哪些文件"（勾选 ∩ 结果集内非保留 ∩ 处理策略目录）。
  不是"扫描跳过/失败项"（`GetFailedItems` 已有），也不是账本"中断"遗留条目。
- **交付面**：后端查询绑定 + UI 清单视图（抽屉）。**不做导出**（那是 M9 的面）、
  不做订阅式实时刷新、不加扩展名筛选（组视图已有）。

## 1. 现状取证（落笔前逐项开码核对）

1. **判据内核已有一份**：`PreviewProcessPolicy`（app.go:1398-1450）求
   勾选 ∩ 生效范围的交点：`dirs` 非空走 `ops.ApplyProcessPolicyWith`（内部已剔保留项，
   keep.go:372-404），空走 `byID` 存在性 + `keepIDs` 过滤；两条分支共用同一求交循环，
   按 selectedIDs 顺序去重（M10c 修齐的口径）。执行侧真值是
   `planOpItems`（app.go:2074，"在结果集内且未被标为保留"）+ 执行器对保留项的硬拒绝。
2. **它只给计数与 ID，不给可读明细**：`ProcessPreview{EffectiveIDs, EffectiveCount, UnmatchedDirs}`；
   且前端今天**没有任何调用点**（wails.ts:310 注释明写"呈现属 M8"）——
   界面上的"其中 M 项在优先文件夹内"走的是另一条腿：`FilterInDirs(dirs, paths)`
   拿命中下标（scan.ts:604-627），它**不剔保留项**（纯路径判据，输入由前端给）。
   ⇒ 缺口正是"清单明细"这一格，与既有计数腿并存不冲突。
3. **AS-R3 教训适用于本绑定**：`WarmSensitivity(dirs)` 要往用户目录写探测文件，
   必须在取 `a.mu` **之前**做完（app.go:1401-1408 原样照抄），否则死挂载能把
   全部 Wails 绑定连锁一起卡住。锁内只剩纯内存计算。
4. **分页钳位有先例**：PageSize 默认 100 / 上限 500、`math.MaxInt` 溢出安全起点
   （M10a，app.go:866-911）；排序 tie-break 有 M144 教训（同值必须落到唯一键）。
5. **TS 类型比对器**（M30）：根包 `wails_types_test.go` 的 `wailsMirrors` 表逐字段双向比对，
   新结构体进表即受管，不新增门禁行。

## 2. 后端契约

```go
// PendingQuery GetPendingFiles 入参。
type PendingQuery struct {
    SelectedIDs []uint64 `json:"selectedIds"` // 前端当前勾选（含被排除项，用于算 reason）
    Dirs        []string `json:"dirs"`         // 处理策略"优先文件夹"；空=未启用
    Page        int      `json:"page"`
    PageSize    int      `json:"pageSize"`
    Sort        string   `json:"sort"`         // group(默认)/size/path
}

// PendingRow 一行清单项；pending=false 时 reason 说明为什么不会动它。
type PendingRow struct {
    ID      uint64 `json:"id"`
    Path    string `json:"path"`
    Name    string `json:"name"`
    Size    uint64 `json:"size"`
    GroupID uint64 `json:"groupId"`
    Pending bool   `json:"pending"`
    Reason  string `json:"reason"` // ""/keep/outside/gone（gone=已不在结果集）
}

type PendingPage struct {
    Total        int          `json:"total"` // = len(Rows)，= 勾选去重后总数
    Page         int          `json:"page"`
    PageSize     int          `json:"pageSize"`
    PendingCount int          `json:"pendingCount"` // 前段（会被处理）条数
    KeepCount    int          `json:"keepCount"`
    OutsideCount int          `json:"outsideCount"`
    GoneCount    int          `json:"goneCount"`
    Rows         []PendingRow `json:"rows"`
}
```

- **排序**：先 pending 段后 excluded 段（分界 = `PendingCount`，前端据此插分区标题）；
  pending 段按 Sort——`group`=结果集组序（遍历 `a.groups` 产出，天然稳定）、
  `size`=降序、tie-break GroupID→ID、`path`=升序、tie-break ID；
  excluded 段按 selectedIDs 原序（去重后，稳定）。
- **判据唯一性**（本项的结构性承诺）：pending 成员资格 = `pendingIDsLocked` 的输出集合，
  该内核由 `PreviewProcessPolicy` 与新绑定**共用同一份代码**（§3），
  不允许出现"清单说有 N 项、执行只动 M 项"的两套账。
- **锁语义**：只读、持 `a.mu` 全程、不置 `opsRunning`、不受其互斥限制（与预览同类，
  app.go:1410-1416 的注释理由原样适用）；`WarmSensitivity` 在锁外预热。
- 无结果集/空勾选 ⇒ 空页非错误（与预览一致：计数为 0 是真话，报错是谎话）。

## 3. 内核收归（先收归、后加功能，两个动作同一提交）

把 app.go:1418-1443 的求交段提成

```go
// pendingIDsLocked 求"勾选 ∩ 结果集内非保留 ∩（若启用）优先目录"，
// 返回按 selectedIDs 顺序去重的生效 ID + 逐 id 排除原因。须持 a.mu。
func (a *App) pendingIDsLocked(dirs []string, resolve ops.SensResolver,
    selectedIDs []uint64) (ids []uint64, reason map[uint64]string)
```

- `PreviewProcessPolicy` 改为调用它再包 `ProcessPreview`——**行为零变化**，
  由既有 `TestPreviewProcessPolicy*`（含并发 race 用例）钉住；收归后 reason 分类
  （keep/outside/gone）在内核里顺手给出，两分支的语义分岔点就是既有代码的
  `if len(dirs)==0 / else` 两格，不新增判断。
- `GetPendingFiles` = `pendingIDsLocked` + 组序/视图行装配 + 排序分页。

## 4. 前端

- `stores/scan.ts`：`pendingOpen`/`pendingPage`/`pendingSort` 状态 +
  `openPending()`（打开时拉一次，page/sort 变化重拉；勾选与 procDirs 变化**不**做订阅式
  预取——清单是点开才看的快照，重开即重取，避免与 150ms debounce 命中数腿互相踩）；
  请求序号防回包错乱（同 `procReqSeq` 手法）。
- `PendingDrawer.vue`：仿 `FailedDrawer.vue`（列表抽屉、Esc 关闭、复用现有表样式），
  两段渲染：分界索引 = `pendingCount`，表头行"将处理 N 项"／"不会处理 M 项（原因见行内）"；
  分页按钮上下页 + 排序下拉。
- 入口：结果页 sel-info 行（"已选 N 项…其中 M 项在优先文件夹内"）旁加
  「查看清单」按钮，仅 `selectedFiles.length>0` 且非 `procCountPending` 时有意义——
  但清单本身不依赖 debounce 腿（自己问后端拿真值），故不灰、点了就拉。
- `wails.ts`：`PendingQuery`/`PendingRow`/`PendingPage` 接口 +
  `BackendAPI.GetPendingFiles(q): Promise<PendingPage>` 声明；
  根包比对器 `wailsMirrors` 补三对。

## 5. 探针、测试与变异（修前必红口径）

- **改前红的形态**：新绑定/新内核在改前树编译不过 ⇒ 按第二批口径 1，
  行为级红**全部由变异提供**，预测包名表与实测包名表对账（M64 口径）。
- Go 用例（根包，无 build tag，进 Linux CI）：
  1. 一致性：同输入下 `GetPendingFiles` 的 pending 集合 ≡ `PreviewProcessPolicy` 的
     `EffectiveIDs`（两腿必须互为镜像）；
  2. reason 三分正确性（保留项 / 目录外 / 结果集外，含 dirs 空非空两分支）；
  3. 分页：PageSize 钳位、越界页返回空页不 panic（M10a 同形）、Total 恒全量口径；
  4. 排序稳定：size/path 同值时 GroupID→ID tie-break，两次调用逐行相等（M144 同形）；
  5. 重复勾选只计一次；
  6. 并发：与结果集清理并跑 -race（`TestPreviewProcessPolicyConcurrentWithCleanupNoRace` 同形）。
- 变异（预告，跑完对账）：V-1 内核忘剔 keepIDs；V-2 分页起点漏溢出判；
  V-3 reason 把 outside 误归 keep；V-4 tie-break 删成 ID 单键；V-5 pending 段用
  selectedIDs 序冒充组序（group 排序腿失效）。
- 前端：node 用例钉 scan.ts 的清单状态机（回包错乱、page 复位）；
  接线锚点钉 ResultView 入口与 wails.ts 声明（静态锚，如实称锚点不称行为红——
  §23 第 4 条口径）。

## 6. 边界与未兑现（落笔即声明，划账不得反悔）

- 清单是**只读快照**：打开后文件被外部改动、或另一路 keep 决策刚应用，清单不自动重取；
  真值在执行侧另有复核（planOpItems 派发时快照 + 执行器硬拒绝 + M91 读复核），
  清单不承诺与"点执行那一刻"逐字节一致，承诺的是**与同输入下预览内核逐 id 相等**。
- Windows 真机行为（路径大小写、UNC）沿用 `ops` 内核既有判定，本项**不新增平台断言**；
  无 Windows 真机 ⇒ 该格不兑现，CI 三腿读数为限。
- 端到端 UI 手点不在本机兑现（headless 环境不拉 Wails 窗口）；前端交付证据 =
  node 用例 + 接线锚点 + `vue-tsc` 类型面，实机操作由用户验收。
