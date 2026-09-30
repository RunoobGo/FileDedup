# 第八轮全面审查实施设计段（2026-09-30）

本轮来源：2026-09-29 夜的第八轮全面审查（八路只读审查代理 + 主代理逐条开码复核）。
取证时刻的仓库锚点：`HEAD == origin/main == 91782a4`，划账至 §6.66，登记号至 M354。
本机交付前门禁读数（pinned 同一提交、跑前跑后 `git rev-parse HEAD` 同值）：
`rows=15 PASS=14 SKIP=1 FAIL=0 REPORT=1`、`src_test=1029`、`top_PASS=958`、`run=1145`、
`sub_PASS=181`、`ok_pkgs=23`、两条 race 行 `data_race=0`；日志 `/tmp/gates-audit8-235502.log`。

## §0 用户裁定（原话摘自本轮 AskUserQuestion 选项，非代理推定）

- **R-8-1（P0-1 取向）：「只在导入侧 fail-closed，不加列」**——外来账本里"回撤要吃到的条目"若没有
  内容证据，整单拒绝导入；**不**新增 `imported` 溯源列，**不**改回撤腿的放行分支。
- **R-8-2（P1 清缓存取向）：「两个都收紧」**——`CacheClear` 补扫描/操作双闸 + 快照 `0600` +
  落位带所有权证明；同批另四条 P1 按现读代码直接修（`ImportFrom` 补锁、`ClearOpRecords`
  判+写同临界区、前端两把锁、回执不被吞）。
- **R-8-3（P2 范围）：「能本机验证的全修，scanKey 登记待裁」**。

拟号规则照旧：**拟号不等于分配**，分配在复核之后（§6.26 六）。本段用 **M355/M356/M357** 作
占位，最终号以 §6.67 登记表为准。裸号（`MU-…`/`P-…`）只在"变异/探针"二字之后出现。

---

## §1 批 1：导入侧内容证据闸（拟 M355）+ 裁剪话术更正（拟 M356）

### 1.1 取证（四条，全部为现读，逐字抄自工作树）

**(a) 导入只问列名、不问值。** `internal/history` 的 `validateForeignLedger` 逐表跑
`SELECT name FROM pragma_table_info(?)`，把缺的**列名**拼进错误串；它对值一个字都不问。
`importRequired` 里 `op_items` 的必需列包含 `hash`，但"列在"不等于"列里有内容证据"。

**(b) hash 逐字搬运，短/空静默过关。** `mergeOpItems` 的读与写都是原样透传：

```go
if err := rows.Scan(&it.origPath, &it.destPath, &it.linkSrc, &it.hash,
    &it.size, &it.mtimeNs, &it.state, &it.errMsg); err != nil {
```

本仓建表语句里 `hash BLOB NOT NULL` 只挡 NULL，`X''`（零长度）照样入库。
读侧 `GetOp` 折回数组时同样不问长度：

```go
copy(it.Hash[:], hb)
```

`copy` 对短切片是**静默零填充**，于是"空 blob"与"全零哈希"在 Go 侧是同一个值。

**(c) 零值哈希恰好是回撤腿的两条豁免开关。** `internal/ops` 的 `undoSourceCheck`：

```go
// it.Hash 为零值时**跳过而不是拦死**：升级前写入的旧账本没有内容证据，
// "无从比对"不等于"校验失败"，一律拦会让历史记录全都撤不回。
if it.Hash != ([32]byte{}) {
```

不进这个 if 就走 `return st, fsid.ID{}, nil`——**身份也拿不到**。紧接着 `restoreInPlace`
的注释把这格的后果写得很清楚（同文件 `identityCheck` 那一段上方）：

```go
// verID 未解析（FAT/exFAT、旧账本无内容证据）时 identityCheck
// 返回 vSame 放行，处置与改前逐格相同。
```

而 `restoreInPlace` 在两条复核都放行之后做两件破坏性动作：先按外来记录建目录
（`os.MkdirAll(filepath.Dir(it.OrigPath), 0o755)`），再 `renameFile(it.DestPath, target)`。
**只剩 size 一格**（`uint64(st.Size()) != it.Size` 报错拦截），而 size 是外来账本自己填的。

**(d) 可达性（本批的关键一格，逐条追到底）：**
- `loadSrcOps`/`insertOpRow` 原样搬运 `undoable`，全仓 `grep -rn 'imported' internal/history`
  在非测试代码里**零命中** ⇒ 导入行与本地行在账本里无法区分。
- 回撤的条目选择集在 `app_ops.go` 的 `UndoOperation` 里：

```go
if it.State == history.StateDone || it.State == history.StateUndoFailed {
    todo = append(todo, it)
}
```

`state` 同样是逐字搬运的。⇒ 一份外来 `.db` 只要给出 `undoable=1` + `state='done'` +
`hash=X''` + `size=目标文件真实字节数`，用户点一次「全部回撤」就得到"按外来账本指定的
路径搬走一个本机真实文件"，且这一趟**内容比对与身份复核双双缺席**。
- 手册侧的相反断言：派生手册（docs/10 §2.11 信任边界）写着"导入的记录在回撤时
  **不会比本地记录多拿到一分权限**：回撤逐文件做全量内容比对与身份复核"；真源手册
  （docs/09 §6.8 同段）`grep -n '导入的记录'` **零命中**——派生册单独断言了真源没有的话，
  双回写规则被**反向**破坏（与 M354 同族：拿我以为的平台/判据语义当承诺）。

**(e) 第二条取证（拟 M356）：三处话术把裁剪对象说反了。** 插入外来扫描的行不带 `id`
（`insertScanRow` 的列表里就没有 `id`），所以外来行必然拿到**最高**自增号；而收尾裁剪
按 `ORDER BY id DESC LIMIT -1 OFFSET ?` 保新删旧——被淘汰的是**号最小那一侧**，
即用户自己的本地最旧记录。真源手册、派生手册、`ImportFrom` 的函数注释三处都写成
"刚导进来的记录若排在最旧一侧，会在那一步被清掉"，方向反了。

### 1.2 判据（修法）

**位置：** `ImportFrom` 里 `validateForeignLedger` 之后、`tx.Begin()` 之前，新增一步
`checkUndoEvidence(src)`。放在这个位置的理由：① 它只需要源侧只读连接，不需要本地自然键，
不必等到 `localScanKeys` 之后；② 与"结构不符即停止"同一档，错误文案可以复用
"（本地记录未改动）"的口径；③ 事务一旦开建，回滚虽然干净，但判据本身会变成"写过一半才发现不该写"。

**判据本体（一条集合式语句，不逐行往返）：** 只问**回撤真会吃到的那一集合**——

- 关联到 `op_records.undoable = 1` 的 `op_items`；
- 且 `state` 属于 `{done, undo_failed}`（与 1.1(d) 那段选择集**逐字同一集合**，
  由 Go 侧常量拼参数位，不在 SQL 里另写一遍字符串字面量）；
- 且 `hash IS NULL OR length(hash) <> 32`。

任一条 ⇒ **整单拒绝**，中文文案点名"几条条目缺内容证据"＋"回撤会跳过内容比对与身份复核"
＋"本地记录未改动"＋"请重新导出或只导入新版导出的记录"。

**不多不少的两端：**
- 不放宽到全表：`undoable=0` 的记录、`state` 是 `undone`/`failed`/`skipped`/`planned`
  的条目一律放行——它们不进 1.1(d) 那个选择集，拦它们是**过度拒绝**，会把"只搬历史台账看数据"
  这一档正常用法打掉。为此并排写两条负控制探针（P-4/P-5）。
- 不加 `imported` 列（裁定 R-8-1），因此本地老账本里本就存在的零哈希行行为**一字不动**：
  豁免仍是"升级前旧记录"的形状，只是这条豁免**不再能由外来文件选中**。

**代价（必须写进手册，不许藏）：** 用户自己的"升级前老账本 → 新版导出 → 导进另一台机"
这一条路被本闸挡住（那本老账的条目没有内容证据）。这是 R-8-1 的必然后果，
在 09/10 两处各补一句，并在 §6.67 划账里记为**取舍**而不是缺陷。

**拟 M356 只做话术：** 更正 09/10 两处与 `ImportFrom` 注释一处，并补一条**真值钉子**
用例（钉住"外来行不会被自己那一步淘汰、被淘汰的是本地最小 id"这个现读行为）。
★ 这条用例**改前就绿**，它是防漂的钉子，**不是**修前红探针——按 §6.11 那批的口径如实标注，
不得写作"取红"。裁剪取向本身（要不要按 `saved_at` 淘汰、要不要豁免外来行）要动判据，
另登 **拟 M357 登记待裁**，本批不自行选边。

### 1.3 探针（五条 + 一条钉子，全部在 `internal/history`）

| 编号 | 判据格 | 修前预期 |
| --- | --- | --- |
| P-1 | `undoable=1` + `state='done'` + `hash=X''` ⇒ 拒，且 `ledgerUnchanged` 断本地五表零变化 | **红**：改前 `ImportFrom` 返回 `OpsAdded=1`、err=nil |
| P-2 | 同上但 `hash` 给 31 字节 ⇒ 拒（长度而非"是不是全零"才是判据） | **红**：改前静默零填充后入库 |
| P-3 | 同上但 `hash` 给满 32 字节 ⇒ 放行且条目数正确（正控制，防"全拒"假绿） | 绿（改前改后都绿，它钉的是新闸不过头） |
| P-4 | `undoable=0` + 空 `hash` ⇒ 放行（负控制） | 绿／改后仍绿 |
| P-5 | `undoable=1` 但 `state='undone'` + 空 `hash` ⇒ 放行（负控制：不在回撤选择集） | 绿／改后仍绿 |
| P-6 | 裁剪方向真值钉子：本地 3 条 + 导入 2 条 → 再 `SaveScan` 一次 → 断"导入的两条仍在、本地最小 id 那条被淘汰" | 绿（**钉子，不是探针**） |

修前红的取法：新建 `import_m355_test.go` 后先在**未改生产码**的树上跑，把失败原文
（哪一格、`OpsAdded` 实值）抄进 §6.67，再动 `import.go`。
夹具走 `exportImage` + 直接 `sql.Open` 改影像（沿用 `import_m352_test.go` 里
`TestImportMidChildFailureRollsBack` 那一族的写法），★ 新开的句柄每条路径都要 `Close`
（M335 的 `TempDir RemoveAll … 被另一进程占用` 教训，本轮普查又抓到两处同类）。

### 1.4 变异（两条，预测集与实测集要对账，别只记"杀了几条"）

- **MU-1** 摘掉 `checkUndoEvidence` 里 `length(hash) <> 32` 这一支（保留函数与调用）⇒
  预测 P-1、P-2 变红，实测包名表须等于预测表。
- **MU-2** 把 state 集合放宽成"全表条目都要求证据"⇒ 预测 P-4、P-5 变红（这一条防的是
  "用过度拒绝换绿"，与 §22 那批"只断言结论等于默认值的判据是空的"同族）。
- 还原一律 `cp`+`diff`，**不用** `git checkout --`（§6.23 那批的教训：会把未提交的同批改动一起冲掉）。

### 1.5 边界与未兑现（划账时照抄，不许读成已通过）

1. 本机只有 darwin：`length()` 对 BLOB 的字节数语义在 linux/windows 腿由 CI 三腿现读兑现；
   CI 三腿 `go test` 不带 `-v` ⇒ 只有**体内不含 `t.Skip`** 的用例其 `ok` 才可读作 PASS
   （P-1~P-6 全部结构上不含 Skip，需在交付时逐文件 grep 自证）。
2. 「导入的记录同样逐文件校验」的**端到端闭环**仍挂真机清单（docs/05 的 MAC-8 那一格），
   本批只把"外来文件拿不到豁免"这一格落到 CI 可跑的判据上；系统文件选择框那两格照旧未兑现。
3. 本批**不改**回撤腿的放行分支（R-8-1 明写"不加列、不改回撤判据"）：一份**本地**老账本
   里的零哈希条目依旧可回撤——这格与 M317 的既有裁定同口径，不是本批的新增缺口。
4. 裁剪取向（拟 M357）待裁：本批只更正话术与钉住现读行为。

### 1.6 实施期追记（判据被实测推翻的一处、取证更正三处、变异对账四条）

★ 按本仓规矩，**预测被推翻就写在这里，不改 §1.2 的原判据**——原预判照旧留在上面供对账。

**(1) 设计判据漏档：`length(hash) <> 32` 拦不住"满 32 字节全零"，而那才是唯一活着的洞。**
§1.2 写的 `hash IS NULL OR length(hash) <> 32` 按长度判，对 `X'00'×32` 判成"有证据"；
而读腿 `copy(it.Hash[:], hb)` 恰好把它折成 `[32]byte{}` ⇒ 走的正是 undo 那条零值豁免。
P-7 的改前真读数：`ImportFrom` 返回 `err=nil`、回执 `{ScansAdded:1 OpsAdded:1}`，条目整条入库。
SQL 层取证（scratch 用例，跑完即删）：`typeof= blob / length=32 / h = 32 零字节` 三条全成立 ⇒
参数位绑定零长 BLOB 的比较是可行的，最终判据落在
`COALESCE(length(i.hash), -1) <> ? OR i.hash = ?`（COALESCE 那一支防的是三值逻辑：
`length(NULL) <> 32` 判成 NULL 而非真，只写长度比较会被 NULL 绕过——单测这条用命中数=2 反证过）。

**(2) 取证更正：X'' 与 NULL 今天**不是**"照样入库"，而是撞在本地列约束上。**
本轮审查报告 P0-1 的原话是"零哈希条目照样入库"，实测不成立：
`X''` 经 modernc 驱动**读回成 nil**（`len=0 nil=true`），`nil` 再写出去是 NULL，
而本地 `op_items.hash` / `hist_groups.hash` 都是 NOT NULL ⇒ 整单在事务里撞驱动串、回滚干净。
所以闸对这两档买到的不是"从放行变拒绝"，而是**"由判据决定、且给出中文原因"**
（P-1 改前红在回执与文案、P-8 改前红在"拒绝理由走偏"）。安全意义上的**活洞只有全零满长那一档**，
划账与手册都按这个口径写，不许再写"外来文件今天能随便导进来"。

**(3) 新缺陷（往返不对称）与它的半修。**
上面那条不对称本身是要登记的缺陷：一条本来合法的外来记录（零长 BLOB 出现在**不可回撤**的记录里）
会被整单撞死，且原因是英文驱动串。本批只在 `op_items` 那条腿修（`blobArg`：nil → 零长切片，
落地仍是 `typeof=blob`），**扫描侧 `hist_groups` 那条腿刻意不修**：
它是 `TestImportMidChildFailureRollsBack` 的夹具支点（那条用例靠"外来 hist_groups.hash 给 NULL
⇒ 撞本地 NOT NULL"构造"子行插到一半失败"），一起归一化就把那条回滚用例的前提抽掉，
而重造它的失败形状等于改既有测试的夹具 ⇒ 与本批"只加判据"的动面不同，转单独一批。
现读行为由 P-9 钉住（修的时候这一条要**反写**，届时它就是自检）。

**(4) 探针编号重排**（§1.3 表里的编号是下笔时的规划，交付按文件顺序重排）：
P-1 X''/done 拒 · P-2 31 字节拒 · P-3 满长放行（正控制）· P-4 undoable=0 放行 ·
P-5 undone 放行 · P-6 undo_failed 拒 · **P-7 满长全零拒（新增，设计原判据拦不住的那档）** ·
**P-8 NULL 拒（新增，三值逻辑那格）** · **P-9 扫描侧零长 hash 现读钉子（新增，记录"登记不修"）** ·
P-10 = §1.3 表里编号 P-6 的裁剪真值钉子。

**(5) 变异对账（预测集 vs 实测集，别只记"杀了几条"）**

| 变异 | 预测变红 | 实测变红 | 差集 |
| --- | --- | --- | --- |
| MU-1 判据退回 §1.2 原判据（无全零支） | P-1、P-2 | **P-7** | 预测**全错**：设计判据拦得住 P-1/P-2，漏的恰是唯一活洞 |
| MU-2 要求全表条目都有证据（过度拒绝） | P-4、P-5 | P-4、P-5 | 空 |
| MU-3 摘掉 blobArg（回归往返缺陷） | P-4、P-5 | P-4、P-5 | 空 |
| MU-4 摘掉闸的调用（不判据） | P-1、P-2、P-6、P-7、P-8 | 同五条 | 空（其余 17 格无损） |

★ MU-2 与 MU-3 杀的是**同两格**：只看"哪几条红"分不开"闸过度拒绝"与"搬运把 X'' 变成 NULL"两种坏，
必须靠错误文本区分 ⇒ P-4/P-5 的失败消息刻意把两个成因都点名，不许写成单一归因。

**(6) 夹具教训两条（可复用）**
1. **SQL 里写 `X''` 字面量、别用 `?` 传 `[]byte{}`**，且**占位符个数变了实参必须同步变**：
   首版把 hash 换成字面量却仍传 4 个实参，多出的 nil 把 `state` 顶成 NULL，
   四条用例一起红在 `op_items.state` 约束上——红在夹具，判据一格没碰。
2. **前提自检要比"预期的那个数"，不要自己算偏移**：P-10 首版写 `importedMin <= localOldest+20`
   把 off-by-one 判在 21 上，钉子红在"夹具前提不成立"；改成直接取 `MAX(id)` 比大小才对。

---

## §2 批 2：P1 六条（R-8-2「两个都收紧」全套）—— 拟 M359~M366

### 2.1 取证（七条，全部为现读，逐字抄自工作树）

**(a) 清缓存一条闸都不接。** `app_settings.go` 里 `opsRunning` / `scanInFlight` 的 `grep -c` 现读是
**0**，`CacheClear` 的第一句实质动作就是 `cch := a.cchSnapshot()`。对照本仓已经立好的两处形状：
`StartScan` 在**一个** `a.mu` 临界区里查 `scanInFlight`、查 `opsRunning`、再置 `scanInFlight`；
`ExecuteOperation` 同锁双检（含 `resultsReady`）后再置 `opsRunning`。⇒ 清缓存既能撞进扫描/清理在途，
也没有任何东西挡得住"清缓存期间开新扫描或点清理"。★ 本批把这一格按**不对称**取证（M285 立的法：
`openLedger` 有、`openCache` 没有），不主张"交错会造成哪一种具体损坏"（见 2.5 第 3 条）。

**(b) 快照跟 SQLite 默认档位走，全程没有 Chmod。** scratch `main` 实测（跑完即删，未留文件）：

```
snapshotMode=644 cacheMode=644 euid=501
```

与 M353 在 `app_records_io.go` 注释里记的那条同值（同一条 `VACUUM INTO`）。全仓非测试代码的
`os.Chmod` 只有两处命中，都在记录导出那条腿。而快照装的是 `hash_cache` 全表 =
用户机器上的**全部路径** + size + mtime。

**(c) 落位是一次无条件改名。**

```go
if err := os.Rename(tmpPath, snapPath); err != nil {
```

本仓对这句话已经有三条在册口径：`internal/ops/merge_guard.go` 的注释写死了
「Go 没有 O_EXCL 语义的改名原语（os.Rename 一律替换）」；§6.63 把"合并落位改名静默覆盖第三方文件"
记为 **P0**（M344）；同一序列给了三档形状的 `claimSlot`——不存在 ⇒ 放行、`ours()` 证明是自己的 ⇒ 处置、
证明不了 ⇒ **显式失败并说清文件在哪**，原话「宁可显式失败并说清文件在哪，也不替第三方销毁文件」。
记录导出那条腿的做法更硬：动手前 `exists(dbPath) || exists(jsonPath)` 就直接拒。
★ 固定名快照**不能**照抄"存在就拒"：覆盖上一份正是用户裁定 R-3 承诺的内容。也**不能**照抄
`claimSlot` 的第三档"显式失败"：缓存此刻已经清空，失败就等于让刚清空的缓存失去快照保护，
而改名本身一个字节都不丢 ⇒ 第三档改成"另落 + 如实报真实落点"。

**(d) 回执被第二次 GetStats 吞掉。** `CacheClear` 末尾：

```go
after, err := cch.GetStats()
if err != nil {
	return res, shellRPCError(err)
}
```

清空**已经成立**、快照**已经落位**，只差"回收字节没能核对"这一腿；而 Wails 在 `err != nil` 时把结构体
丢掉（这条函数注释里自己写过），于是"已清空 N 条 · 快照在 X"这两半真话一起没了。对照同一函数里的
`reclaimOnly` 那一腿——同一种形状已经明确处理过（条数刻意拼进错误串）。⇒ 一个函数、两条臂、
同一契约两种兑现，这是本仓记过一次的不对称。

**(e) `ImportFrom` 不取 `s.mu`。** `internal/history` 非测试代码里 `s.mu.Lock()` 现读 **16** 处，
`import.go` 命中 **0**。同包另一条写路径 `ExportTo` 是 `s.mu.Lock()` + `defer s.mu.Unlock()` 整段包住。
而 `ImportFrom` 的形状是"集合式读本地键（`localScanKeys` / `localOpKeys`）→ 开 tx → 逐行插"，
读与写之间没有任何串行化保证 ⇒ 两个并发 `ImportFrom` 同一份影像会各自判成"本地没有"，双双插到底，
把在册承诺「连点两次不翻倍」踩穿。（`s.db` 只有 1 条连接，所以坏的不是字节，是**判据的时点**。）

**(f) `ClearOpRecords` 判与写分两段。**

```go
a.mu.Lock()
busy := a.opsRunning
a.mu.Unlock()
if busy { ... }
hs := a.histSnapshot()
...
return hs.ClearOps()
```

`opsRunning` 的**置位**发生在 `ExecuteOperation` 自己的临界区里，而这里的**检查**在另一段临界区里，
中间窗口能放行一笔新清理，随后 `DELETE FROM op_records` 抽走它刚落的账。
★ 修法不能是"把 `a.mu` 一路握到 `ClearOps` 之后"：`app_history.go` 明写着
「hist 访问不与 `a.mu` 嵌套」，而 `hs.ClearOps()` 内部要拿 history 包自己的 `s.mu`。

**(g) 前端两把锁缺的是同一件东西。** `SettingsView` 的 `clearCache` 没有任何 in-flight ref
（同仓 `RecordsView` 的 `exportRecords` / `importRecords` 各有 `exporting` / `importing` + `finally` 复位），
且确认态在 `await` **之前**就回到 `false`（本页两个「清空」同形）：

```js
async function clearAllOps() {
  if (!confirmClearOps.value) { confirmClearOps.value = true; return }
  confirmClearOps.value = false
  await store.clearOps()
}
```

⇒ RPC 在途期间按钮恢复成初态、也不带 `:disabled`，"确认清空"再点两下就并发发出第二、第三次
`CacheClear` / `ClearOpRecords`——正好撞在 (a)、(f) 那两条没有闸的窗口上。

### 2.2 判据（七条修法 + 一条登记待裁）

1. **M359 维护在途标记（双向闸）**。`App` 增字段 `maintaining string`（空即无维护在途），配
   `claimMaintenance(what string) error`：**同一个** `a.mu` 临界区内查 `scanInFlight`、`opsRunning`、
   `maintaining` 三件，全空才占位；配 `releaseMaintenance()`。`CacheClear`、`ClearOpRecords` 各自
   开头认领、`defer` 释放；`StartScan` 与 `ExecuteOperation` 的既有临界区各加一支
   `a.maintaining != ""` ⇒ 拒，文案点名是哪一项维护。
   - ★ 用 `string` 而不是 `bool`：拒绝的话必须说得出"是清缓存还是清记录"，否则用户在扫描页只看到
     一句无主的"维护进行中"。
   - ★ 标记是**闩**不是锁：占位与释放各在一次 `a.mu` 临界区内，真正的库操作在锁外 ⇒ 不违反 (f) 里
     那条嵌套惯例。
   - ★ 既有两支的文案与**顺序**一字不动（既有断言直接抄了「上一个扫描任务尚未收尾」这类原话）。
2. **M360 快照 0600**。tmp 影像一成形即 `os.Chmod(tmpPath, 0o600)`；设不上即停止并走既有
   「清空缓存已取消：快照未能生成，未删除任何缓存数据」那一档的形状（此时一条缓存都还没删，
   取消是实话）。注释按 M354 的双回写口径写：这句"仅所有者可读写"只在 unix 腿兑现，
   Windows 的模式位只表达只读属性。
3. **M361 落位带所有权证明**。新增 `landCacheSnapshot(tmp, snap string) (landed, note string, err error)`：
   - `os.Lstat(snap)` 报不存在 ⇒ 原位改名，note 空；
   - 存在且**证明得了是本应用自己的**⇒ 原位覆盖，note 空；
   - 存在但证明不了（不是常规文件：symlink / 目录 / 设备；或 unix 腿属主 ≠ 当前 euid）⇒ **绝不碰它**，
     把新快照落到 O_EXCL 抢占的时间戳名（`cache-backup.db.aside-<ts>`，撞名递增），note 写明
     「固定名被一份不属于本应用的对象占着，本次快照另存在此」；
   - 连撞若干次都占不到名 ⇒ 返回错误，措辞沿用 §6.63 的「本应用不会删除它」。
   属主证明按平台分档，两枚带 tag 的文件（惯例同 `internal/ads`、`internal/fsid`）：`!windows` 走
   `st.Sys().(*syscall.Stat_t).Uid == os.Geteuid()`；windows 腿只主张到"常规文件"这一档，注释与本段
   同时写明属主证明在 Windows 未兑现（M354 同族：不拿弱腿冒称强腿）。
   `CacheClearResult` 增 `SnapshotNote string`；`SnapshotPath` 一律填**真实落点**。
4. **M362 统计失败不吞回执**。加 `cacheClearStats` 接缝（惯例同 `cacheClearSnapshot`）包住两次取统计；
   第二腿失败 ⇒ `EntriesCleared` / `SnapshotPath` 保持已填，错误串写成
   「缓存已清空 N 条，快照在 X，但磁盘占用未能核对（回收字节暂报 0）：<原错误>」，
   `ReclaimedBytes` 如实留 0——不许把"没核对上"说成"回收了 0 字节"以外的任何断言。
5. **M363 `ImportFrom` 补锁**。`s.mu` 从 `localScanKeys()` 之前一路握到 `tx.Commit()`；
   **不开新事务、不改判据**，只把"读集合 → 写"并进同一临界区。死锁自证：区间内调的
   `insertScanRow` / `mergeScanChildren` / `insertOpRow` / `mergeOpItems` / `blobArg` 全是自由函数
   （`grep '^func '` 现读，无一处取 `s.mu`），`src` 是另一条 `*sql.DB`。
6. **M364 `ClearOpRecords` 判+写同临界区**。走 1) 的 `claimMaintenance("清空清理记录")`，
   对 `opsRunning` 那一支**沿用既有原话**（免得既有断言要改）；另在 `hs.ClearOps()` 外面加一层
   `ledgerClearOps` 接缝——★ 接缝本身是**纯重构**，先落地再写探针，这样"改前必红"才落在行为上
   而不是落在编译错误上。
7. **M365 前端两把锁**。`clearCache` 加 `clearing` ref（`try/finally` 复位）+ 两个按钮 `:disabled` +
   确认态只在 RPC 落定后关；`clearAllOps` 加 `clearingOps` ref，同形。判据走
   `scripts/test-frontend-logic.sh` 的 `wiring` / `wiring_window` 锚点（正侧钉新形状、负侧钉
   "await 之前关确认"这个旧形状不许回来）。
8. **M366 登记待裁（本批不动）**：同一次实测里 `cache.db` 自身也是 0644（`history.db` 同档）。
   本批把**新产的影像**收成 0600，而**存量库文件**的档位是另一件事——要动 `Open` 路径、要处置
   盘上既有文件、跨平台还牵扯 NTFS ACL。R-8-2 的字面范围只到"快照 0600"，故只登记不实施。

### 2.3 探针（P-11 ~ P-24）

| 编号 | 判据格 | 修前预期 |
| --- | --- | --- |
| P-11 | `opsRunning=true` ⇒ `CacheClear` 拒，且缓存条目一条不少 | **红**：改前 `err=nil` 且真清走 |
| P-12 | `scanInFlight=true` ⇒ 同上 | **红** |
| P-13 | 反向闸：清缓存在途（快照接缝里暂停）⇒ `StartScan` 拒 | **红**：改前受理并起跑 |
| P-14 | 反向闸：同一暂停点 ⇒ `ExecuteOperation` 拒 | **红** |
| P-15 | 快照模式位：unix 腿硬断 `0600`；windows 腿断"读数必须仍是 `0666`"（M354 同形） | **红**：实测 644 |
| P-16 | 固定名上是 symlink ⇒ 链接自身与其目标一字不动、快照另落、note 非空、`SnapshotPath`=另落名 | **红**：`os.Rename` 顶掉 symlink |
| P-17 | 固定名上是"属主不是本机"的常规文件（属主证明接缝给 false）⇒ 不覆盖、另落 | **红**：覆盖 |
| P-18 | 正控制：固定名上是上一份自己的快照 ⇒ 原位覆盖、note 空、盘上仍只 1 份 | 绿／改后绿（防"全另落"） |
| P-19 | 另落名已被占 ⇒ 走下一个 O_EXCL 名，那个文件内容不变 | **红**：改前根本没有另落这条路 |
| P-20 | 第二腿统计失败 ⇒ 错误串含「已清空 N 条」与快照落点，`res` 两字段仍填好 | **红**：只剩一句壳错误 |
| P-21 | 两个并发 `ImportFrom` 同一影像 ⇒ 行数不翻倍 | **红（概率性）**：复现不出即按 2.5 第 4 条如实记账 |
| P-22 | `ClearOpRecords` 在途（`ledgerClearOps` 暂停点）⇒ `StartScan` 与 `ExecuteOperation` 都拒，且 `maintaining` 已占住 | **红** |
| P-23 | 前端锚点正侧：两个 handler 都有 in-flight ref + `:disabled` + 落定后关确认 | **红**：锚点缺失 |
| P-24 | 前端锚点负侧：`await` 之前关确认态的旧形状不得回来 | 绿（钉子） |

★ P-13 / P-14 / P-22 必须先跑**前提自检**（M93 的教训：夹具穿不过门禁链下游某道门时，"被拒"
无法归因给被测门禁）：同一夹具在不占维护标记时必须**真的受理**，自检不过即当场红。

### 2.4 变异（预测集先写死，交付时对账）

| 变异 | 预测变红 |
| --- | --- |
| MU-5 摘掉 `CacheClear` 的 `claimMaintenance` 调用（函数留着） | P-11、P-12、P-13、P-14 |
| MU-6 只摘 `claimMaintenance` 里 `opsRunning` 一支 | 只有 P-11 |
| MU-7 摘掉 `os.Chmod(tmpPath, 0o600)` | P-15（unix 腿） |
| MU-8 属主证明恒 true | P-17；★ 预测 P-16 **仍绿**（symlink 那支走模式位判定，不经属主） |
| MU-9 所有权判定退化成"存在就另落" | P-18（过度拒绝，防"用多拒绝换绿"） |
| MU-10 第二腿统计失败改成 `_ = err` 继续 | P-20 |
| MU-11 摘掉 `ImportFrom` 的 `s.mu` | P-21（概率性，实测单独记） |
| MU-12 前端确认态改回 `await` 之前关 | P-23 |

还原一律 `cp`+`diff`，**不用** `git checkout --`（§1.4 同一条）。

### 2.5 边界与未兑现（划账时照抄，不许读成已通过）

1. 本机只有 darwin：M360 的 windows 臂只断"读数仍是 0666"，**"Windows 上快照仅所有者可读写"
   这句不成立**；手册与注释按分平台措辞写（M354 同口径）。
2. **M361 的属主那一档在 Windows 腿未兑现**（那条腿的 `os.Stat` 不表达属主，访问权由所在目录的
   NTFS ACL 继承）⇒ 挂 docs/05 真机清单。P-16 需要 `os.Symlink`，Windows 无特权会失败 ⇒ 该格按
   `internal/ads` 惯例放进带 `//go:build !windows` 的文件，windows 腿**不进**这一格 ⇒
   §6.67 的三条平台腿格数必须逐腿重取，不许照抄"合计 +N"。
3. 双闸钉的是"互斥拒绝"这一格；本批**不主张**清缓存与扫描交错会造成哪一种具体损坏
   （那需要真机时序取证）。判据只到"同一时刻只许有一个动 `hash_cache` / `history.db` 的主体"。
4. P-21 是概率性探针：若改前复现不出红，按「代码已改、探针未复现」如实记，另以结构钉子
   （读集合与写 tx 同临界区）钉住，**不许**把"改后也绿"当成改前就该绿。
5. M366（存量 `cache.db` / `history.db` 的 0644）登记待裁，本批未动。
6. 前端两把锁的判据是**静态锚点**（node 侧不能 import `.vue`）⇒「双击确实只发一次 RPC」
   这一格仍挂 docs/05 真机清单。

### 2.6 实施期追记（判据被实测改动七处、变异对账十二条、测量口径自纠一条）

**(1) 档位收回点从 `CacheClear` 主体挪进 `cacheClearSnapshot` 接缝**。设计段把 `os.Chmod` 写在主体里，
而主体拿到的是接缝产出的文件——P-15b 首版就红在「接缝返回时 tmp 仍是 0644」。改名会把模式位一路带到
**两条落位臂**（原位覆盖与另落），所以收在 tmp 一处才够；顺带删掉同一件事的第二个接缝
`snapshotCacheForClear`（两个实现必然分叉）和与之重复的 `TestM359SnapshotFileModeIsOwnerOnly`
——删除处留了理由注释，**没有**改成弱断言蒙过去。

**(2) P-19 推翻原规划**。原设计要测「另落名已存在 ⇒ 换下一个名」，本机要么钉死纳秒时间戳要么引入时钟
接缝，而 `claimAsideName` 的 O_EXCL 循环本身已经兜住撞名那一臂 ⇒ 改钉成本机能证伪的那条：**另落的那份
同样是 0600、盘上只多这一份、不留 tmp 兄弟**。

**(3) P-17 拆成两格**。"属主不是本机的常规文件"在没有 root 的机器上造不出来 ⇒ **端到端那一格未兑现**
（挂 docs/05）。改成两条本机可证伪：接缝返回 false 时固定名上那份一字不动、快照另落（P-17），
以及把判据本体做成表驱动（P-17b `TestM361OwnershipProofArms`：常规文件 + 同 uid 才认领，symlink、
目录、异 uid 都不认领）。P-17b 是 MU-8a 逼出来的，见对账表。

**(4) `claimMaintenance` 收两个参数**（设计段 §2.2 写的是「沿用既有原话」）。一个串担不起两个方向：
ClearOpRecords 撞在 `opsRunning` 上那句话是批 2 之前就立好的文案（「清理/回撤操作执行中，请等待结束后
再清空记录」，一字不许动），而它作为「进行中」主语时该叫「清空清理记录」才与「清空缓存」对称 ⇒
`name` 进维护标记、`action` 进拒绝话术。探针同步改回期待「清空清理记录」。

**(5) 前端窗锚从 8 行收到 1 行，并追加一条计数锚**。见对账表 MU-12：8 行窗口把 catch 腿那句
`confirmClearCache.value = false` 一起算进去，变异照样假绿。收紧后 span=1 的代价是** await 与落定句
之间不许插注释行**，所以判据说明写成行尾注释，位置即文案。

**(6) 批 1 遗留两条红 + 一条空转自纠**。M355 的证据闸落地后，`TestImportRecordsMergesAndReports` 与
它的 Twice 姊妹用全零 hash 夹具被按设计拒掉 ⇒ 夹具改成带 32 字节真证据，**不是**放宽闸。同族的
`TestImportRefusesWhileOpsRunning` 那时已经空转（对任何错误都算通过）⇒ 补归因自检：拒绝串必须是
「操作执行中」那一档。

**(7) windows 腿缺件是跨编译暴露的，不是 review 看出来的**。`GOOS=windows go build` 报 undefined 才补出
`app_snapshot_owner_windows.go`：unix 腿用了 `syscall.Stat_t`，不给另一条腿整个包编译不过。那条腿的
`os.Stat` 不表达属主 ⇒ 认领条件退到「常规文件」为止，注释按 M354 口径写明「这不是已证明属于本应用，
而是能证明的到此为止」。

**(8) 测量口径自纠（可复用）**。首轮 MU-5~MU-11 用的 `-run` 窄集里没有 P-20 的 `TestM362...` 与 P-22 的
`TestM364...`，也没有历史包 ⇒ **窄集测出的差集不可信**。本轮统一到宽集重取：根包
`TestM359|TestM360|TestM361|TestM362|TestM363|TestM364|TestCacheClear|TestClearOpRecords|TestImport`
加 `internal/history` 的 `TestImport|TestM363`，基线 0 红后才逐条施加。下表即宽集读数。

**(9) 变异对账（预测集 vs 实测集，别只记「杀了几条」）**

| 变异 | 预测变红 | 实测变红 | 差集 |
| --- | --- | --- | --- |
| MU-5 摘 `CacheClear` 的 claimMaintenance 调用 | P-11~P-14 | 同四条 | 空 |
| MU-6 只摘 `opsRunning` 一支 | 只有 P-11 | P-11 + B3 | +1：清记录的拒绝话术同样由 `claimMaintenance` 产生 ⇒ 共用一支闸被两处复用；这一格顺带证明 B3 不空转 |
| MU-7 摘掉 `os.Chmod` | P-15 | P-15、P-15b、P-19 | +2：档位随改名带到两条落位臂 ⇒ tmp 格与另落格都靠这一句 |
| MU-8a 属主证明恒真（判据侧） | P-17 | 首版**编译失败**（`os` 成未使用导入）；改写成 `|| true` 后**无既有格红** | ★ 实测把设计判据打成「本机不可证伪」⇒ 补 P-17b，重取后恰好红 P-17b（差集空） |
| MU-8b 落位侧恒认 ours | P-16、P-17 | P-16、P-17、P-19 | +1：另落那条臂的存在性由 P-19 钉 |
| MU-9 所有权判定退化成「存在就另落」 | P-18 | P-18、P-17、`TestCacheClearKeepsSingleSnapshot` | +2：短路把接缝调用一起摘掉，P-17 的前提自检（接缝必须被走到）先红；M349 的「盘上只留一份」随另落一起倒 |
| MU-10 第二腿统计失败改成继续 | P-20 | P-20 | 空 |
| MU-11 摘 `ImportFrom` 的 `s.mu` | P-21（概率性） | P-21，连测五轮**稳定复现** | 概率性预期被推翻：本机竞态窗口足够宽 |
| MU-12 前端确认态移回 `await` 之前 | P-23 | 首版 8 行窗锚**没红**；收到 1 行并追加计数锚后恰好红那一条窗锚 | ★ 原锚点被实测证伪（catch 腿落在窗口内＝假绿），已收紧 |
| MU-13（追加）摘 `StartScan` 侧的 `maintaining` 支 | P-13、P-22 | 同两条 | 空 |
| MU-14（追加）摘 `ExecuteOperation` 侧的同一支 | P-14、P-22 | 同两条 | 空 |
| MU-15（追加）摘 `ClearOpRecords` 的 claim | P-22 | P-22 + B3 | +1：与 MU-6 同一成因 |

★ MU-13/14/15 是 §2.4 表之外的追加三条：双向闸的三支各自承重这件事，只有分头摘掉才证得出来。
★ 还原一律 `cp`+`diff` 校验，本轮无一处用 `git checkout --`（§1.4 同一条）。
★ 前端侧本轮读数：M365 锚点 7 条、接线断言合计 52 条、node 用例 156 条、合计 208 条全通过。
