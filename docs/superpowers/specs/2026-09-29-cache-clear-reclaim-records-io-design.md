# 2026-09-29 缓存清空真回收 + 记录导出/导入 · 细化设计段

## 0. 起跑状态与本批来源

起跑点：`origin/main = 3938ac5`（§6.64 收口批之后），本地干净、无在途批次。
上一自由号 **M347**（docs/04 现读最大号 M346）。

来源是用户报的一个**症状**加四条裁定，不是审查台账里的在册项：

> 清除缓存后，缓存统计条目显示为 0，但占用未变。修复此问题，清除时正确清除数据，
> 同时增加备份数据功能，防止误删除

### 0.1 裁定总表（四条都问过了，逐条按用户原话登记）

| # | 岔口 | 用户裁定 | 与本设计的关系 |
|---|------|---------|---------------|
| R-1 | 备份做到哪一层 | 「清空时，移除哈希缓存，保留记录（账本），记录尽在记录页删除」+ 追加确认「是说清缓存不能连坐账本」 | 清缓存的作用域被**钉死在 `hash_cache` 一张表**；账本四张表不碰，并且要有守卫测试（M350）。记录页原有逐条删除/清空两个出口**一律不动** |
| R-2 | 怎么把备份用回去 | 「手动导出导入，导入时以增量的形式扩展本地记录」 | 恢复手段不是"覆盖回原文件"，而是**记录导出/导入**这条新功能（M351~M353）；导入语义=增量合并，不覆盖本地 |
| R-3 | 快照保留策略 | 「仅保留最近一次快照防止误删除」 | 固定名单份、覆盖式，不建备份目录、不做轮转计数（M349） |
| R-4 | 「占用」口径 | 「把 -wal 计入 DBSizeBytes」 | `dbSizeBytes` 从"主库"改为"主库 + `-wal`"（M348），契约面与 09/10 口径同步改 |

★ R-2 推翻了我原设想的两条路（"只报快照路径 + 手册手动恢复"和"应用内备份管理器"都作废）。
本批**不做**应用内恢复入口；恢复能力由"导入记录"承担，而导入的是 `.db` 影像。

### 0.2 分工总表（本批 7 个号，两批提交）

| 号 | 标题 | 落位 | 批 |
|----|------|------|----|
| M347 | `cache.Clear()` 只发 DELETE ⇒ 空间永不回收（用户报的症状本体） | `internal/cache/cache.go` | 1 |
| M348 | `DBSizeBytes` 不含 `-wal` ⇒ 占用常态少报约一倍 | `internal/cache/cache.go` | 1 |
| M349 | 清缓存前不落最近一次快照；`CacheClear` 无返回契约（用户看不出清没清） | `internal/cache` + `app_settings.go` + `frontend` | 1 |
| M350 | 「清缓存不连坐账本」缺结构性守卫 | 根包测试 | 1 |
| M351 | 导出记录：`.db` 影像 + `.json` 只读镜像 | `internal/history` + `app_history.go` | 2 |
| M352 | 导入记录：ATTACH 增量合并（自然键去重 + id/hist_id 重映） | `internal/history` + `app_history.go` | 2 |
| M353 | 记录页导出/导入 UI + 在途互斥守卫 + 前后端契约注册 | `frontend` + `wails.ts` | 2 |

---

# 第一部分 批 1：清空的真实回收与边界

## 1. M347（CACHE-13）`Clear()` 只发 `DELETE`，磁盘占用不降

### 1.1 取证（本机实跑读数，非推断）

探针文件 `internal/cache/probe_clear_test.go` / `probe_clear2_test.go`（临时探针，随批删除，
读数全部转录如下）。夹具：20 000 条带 32 B full 的条目，`page_size=4096`。

| 时点 | 主库 `cache.db` | `-wal` | `-shm` | `GetStats().dbSizeBytes` | `Entries` |
|------|----------------|--------|--------|--------------------------|-----------|
| 填满 | 4,300,800 | 4,375,472 | 32,768 | 4,300,800 | 20,000 |
| `Clear()`（= `DELETE FROM hash_cache`）后 | **4,300,800** | 4,375,472 | 32,768 | **4,300,800** | **0** |
| `VACUUM` 后 | **4,300,800** | 4,375,472 | 32,768 | 4,300,800 | 0 |
| `PRAGMA wal_checkpoint(TRUNCATE)` 后 | **24,576** | **0** | 32,768 | 24,576 | 0 |

同一次 `DELETE` 后读 `pragma_freelist_count`：**1044 页 × 4096 = 4,276,224 B 挂在 freelist 上**
（占文件 99.4%）；`VACUUM` 后 `freelist_count` 归 **0**，但主库字节数**一格没少**。

★ 第三条读数是本段最要紧的一条，它挡掉了一个"显然正确"的修法：
**WAL 模式下 `VACUUM` 只重写 WAL，不缩主库文件**。若按直觉只加 `VACUUM`，
症状一分不差地留在原地（探针会一路绿到 checkpoint 那格才红），
这就是"看起来改了、其实没改"的形状。

另取两条与本修法相关的前提（同探针实测）：
- `VACUUM` 对**空表**库不报错、安全；`cache_meta` 的 `algo_version` 在 VACUUM 后仍为
  `blake3-256+xxh64-4pt+id-v4`（⇒ 不会因回收触发 `enforceAlgoVersion` 的整表作废分支，
  也就不会把"清缓存"变成"清缓存 + 顺带改语义版本"）。
- `VACUUM INTO ?` 产出的是一份**自包含单文件**影像（实测 4,120,576 B）：能独立 `Open`、
  `GetStats` 读到 20,000 条、`Lookup` 命中且 `head=42` 正确 ⇒ 快照不需要 `-wal` 伴随文件。
  ★ 该语句在**目标文件已存在时失败**，这一条被 M349 的落地顺序直接用到。

### 1.2 缺陷形状（三层，缺一层就修不对）

1. **SQLite 语义层**：`DELETE` 只把页挂进 freelist，文件长度是 SQLite 从不主动归还的
   （`auto_vacuum` 本仓未开，也不该开——它把回收耦合进每次写入）。
2. **WAL 层**：`journal_mode=WAL` 下新页先落 `-wal`，主库只在 checkpoint 时前进；
   所以 `VACUUM` 的效果**必须**跟一次 checkpoint 才在盘上可见（实测第 3、4 行的分岔）。
3. **展示层**：`GetStats()` 的 `DBSizeBytes = os.Stat(c.path).Size()`，只看主库。
   三层叠起来，界面就成了"条目 0 · 4.1 MB"——用户据此判断"没清掉"，
   而**数据其实确实已经删了**（`Entries`/`WithFull` 归 0 是真的）。

★ 措辞纪律：本条**不是**"清除没生效"，是"清除生效了但盘上看不出来"。
两者修法不同，登记行与手册都不许写成前者。

### 1.3 契约违背的一格（为什么这条够 P0 而非展示瑕疵）

`docs/09` §缓存维护第 9 条原文：**「想立即回收空间可在设置里清空缓存」**。
实现一条都没回收 ⇒ 这是**已文档化承诺**未兑现，不是口径未定。
所以 M347 的修法必须兑现"立即回收"这四个字，探针的判据也照这四个字定。

### 1.4 修法

`internal/cache/cache.go`，`Clear()` 改为三步，**顺序即判据**：

```go
// ErrReclaimFailed 哈希条目已删、只有空间回收没走完（M347）。
// 上游必须据此分岔文案——把它报成"清空失败"是一句假话（条目确实已经没了）。
var ErrReclaimFailed = errors.New("哈希缓存已清空，但磁盘空间未能回收")

func (c *Cache) Clear() error {
	// 1) DELETE 失败 ⇒ 原样返回（既没删也没回收，错误语义与改前同形）
	// 2) DELETE 成功 ⇒ cntValid=false / lastEvicted=0 立即失效（改前就在的位置，不动）
	// 3) reclaimLocked() 失败 ⇒ 包成 ErrReclaimFailed，原文进括号作 detail
}

func (c *Cache) reclaimLocked() error {
	if _, err := c.db.Exec(`VACUUM`); err != nil { return err }
	var busy, logN, ckptN int
	if err := c.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).
		Scan(&busy, &logN, &ckptN); err != nil { return err }
	if busy != 0 { // ★ busy=1 不是 error，是"没做成"，必须自己认
		return fmt.Errorf("WAL 正被其他连接占用（checkpoint busy），主库体积暂未回落")
	}
	return nil
}
```

三条不显然的取舍，都写进代码注释：

- **为什么 `VACUUM` 不进事务**：SQLite 明文禁止，而 `Clear()` 现在就没有事务 ⇒
  不"顺手"给它包一层（包了就红）。
- **为什么必须读 `PRAGMA` 的三个返回列而不是 `Exec` 了事**：`wal_checkpoint` 撞占用时
  **不返回 error**，它返回一行 `busy=1`。用 `Exec` 会把"没回收成功"读成"回收成功"，
  那就把本条的假话从"字节没减"升级成"报告说减了"。这与 `IsBusy`（M20/M281）
  同一条纪律：**认不准就不说成功**。
- **为什么失败不 rollback**：`DELETE` 已 autocommit，回滚无从谈起；唯一诚实的动作是
  说清"哪半截做完了"。判据与文案分岔形状照 `ErrEvictFailed`（M75(a)）那条在册先例走。

`shellRPCError` 侧不需要新签名规则：中文哨兵含 CJK 走 B 档原文照出（`app_error_shell.go` 已证）。

### 1.5 判据

**修前必红探针**（新文件 `internal/cache/cache_reclaim_m347_test.go`）：

- `TestClearReclaimsMainFileBytes`：填满 → `Clear()` → 断言
  `os.Stat(main)` **且** `GetStats().DBSizeBytes` 都 `< 填满时的 1/8`。
  改前必红（改后 4,300,800 → 24,576，比值 0.0057，判据取 1/8 留足平台余量）。
- `TestClearTruncatesWal`：`Clear()` 后断言 `-wal` 字节为 0（钉住"checkpoint 那一步真的跑了"，
  只有 `VACUUM` 没 checkpoint 时这条红）。
- `TestClearReclaimBusyIsNotSilent`：注入点（见下）伪造 `busy=1` ⇒ 断言 `errors.Is(err, ErrReclaimFailed)`
  且**错误串里点名"暂未回落"**；再断言 `Entries==0`（条目已删这半截不许被连带否认）。
- `TestClearKeepsAlgoVersion`：`Clear()` 后再 `Open` 同一文件 ⇒ 条目 0、`algo_version` 仍是当前常量、
  不出现第二次隔离。钉住"回收没把语义版本搅进来"。

**变异验证**（改坏必须变红，逐条落格）：

| 变异 | 预测变红的格子 |
|------|---------------|
| 删掉 `VACUUM`（只 DELETE + checkpoint） | `TestClearReclaimsMainFileBytes` |
| 删掉 `wal_checkpoint` 那一腿（只 DELETE + VACUUM） | `TestClearTruncatesWal`、`TestClearReclaimsMainFileBytes` |
| checkpoint 改用 `Exec` 不读 busy 列 | `TestClearReclaimBusyIsNotSilent` |
| 把 `ErrReclaimFailed` 文案改成"清空缓存失败" | 文案锚（同文件里的串断言） |

★ 第二行是**本批最重要的一格变异**：它证明"只加 VACUUM"这版修法在本仓门禁里能蒙混过关的
只有 `TestClearReclaimsMainFileBytes` 之外的那些格子——如果我只写一条探针，
这条修法就会以"已修复"的名义留下原症状。所以两条探针必须分开写、各钉一层。

**注入点**：`var checkpointFn = func(db *sql.DB) (int, int, int, error) {...}`
（惯例同 `evictFn`/`renameFile`）。造 `busy=1` 的真实路径要第二个进程写锁，
比一条包级接缝贵得多；真机那一格进 05 清单（见 §4）。

### 1.6 边界（不得读成什么）

- 不消除"清空后仍需一次全量重算"的既有代价，只兑现"立即回收空间"。
- `-shm`（实测 32 KB）不进任何统计，见 M348 的理由。
- `VACUUM` 期间写锁不释放：一次 50 万条量级的清空会让界面等一趟磁盘（不是 bug，
  是代价），手册要写明"清空可能需要几秒"。本批**不**把它挪到后台任务里——挪进 goroutine
  就要新做在途状态与二次点击抑制，那是另一个号。
- 跨进程 `fdd-cli`：现读 `cmd/fdd-cli/main.go` **没有**任何 `Clear` 调用点（只有 `cache.Open`），
  所以 busy 分支在今天的代码里只可能由"另一个 GUI 实例/其他连接"触发；M196 已有 OS 级单实例保护。

## 2. M348（CACHE-14）`dbSizeBytes` 不含 `-wal`，占用常态少报

### 2.1 取证

`GetStats()` 现读只取 `os.Stat(c.path).Size()`（`cache.go:561-563`）。
填满 20 000 条时实测主库 **4,300,800**、`-wal` **4,375,472** ⇒ 盘上真实占用 8,676,272 B，
界面报 4,300,800 B（**少报 50.4%**）。这不是清空才有的问题，是常态扫描期就一直错。

### 2.2 修法

```go
// DBSizeBytes 主库 + -wal 的字节和（M348）。用户在这里看到的是"占用"，
// 而 WAL 模式下一条都没 checkpoint 的改动全在 -wal 里，只报主库就少报一半。
// -shm 不计：它是 32 KB 恒定的共享内存索引，随最后一个连接退出即消失，不承载数据。
```

`-wal` 不存在时按 0 计入（`os.Stat` 失败且非 ErrNotExist 也不报错——这一格是展示量，
不值得为它把整个 `CacheStats` 变成 error）。
**`corrupt` 停用期仍返回零值**（M214 的既有形状不动，含 `DBSizeBytes`）：
那条分支返回的本来就是"说谎的零"，由 `Corrupted()` 兜语义，本条不翻它的案。

### 2.3 判据与变异

- `TestStatsIncludesWalBytes`：Store 后**不** checkpoint，断言 `DBSizeBytes >= 主库+wal 实测和`
  且 `> 主库单独字节数`。改前必红（改前它恰等于主库）。
- `TestStatsNoWalSidecarIsFine`：只 `DELETE` 文件不留 wal 的形态下不报错、值=主库。
- 变异：把 wal 项去掉 ⇒ 前者红；wal 用 `st.Size()` 不判 `err` ⇒ 后者红。

### 2.4 边界

- 契约字段名 `dbSizeBytes` **不改**（改名的代价要摊到生成绑定、既有测试、09/10 三处，
  收益只是措辞）。**含义改了** ⇒ 09 §4.7 体积参考那句（现写"主库约 16 KB 起"）与 10 同步改写，
  并写明含 WAL。这是 R-4 裁定的直接后果，不许只改代码不改口径。

## 3. M349（CACHE-15）清空前不落最近一次快照，且 `CacheClear` 不回报任何数

### 3.1 取证

- `app_settings.go:103-109` `CacheClear()` 返回 `error`：成功时界面只能写死一句
  「哈希缓存已清空」（`SettingsView.vue:30`），**没有任何数**——用户无从判断清了 0 条还是 2 万条、
  回收了多少字节、备份去了哪。这次的"占用未变"之所以被当成"没清"，界面拿不出反证是共因。
- 全仓 grep `VACUUM|wal_checkpoint|checkpoint`：生产代码**零命中**（只有 docs 里三处散文）
  ⇒ 仓内今天没有任何"库快照"能力可复用；唯一相近机制是 `dbfile.Quarantine`，
  但它是**改名隔离**（把文件挪走），语义是"这库已坏、别再用"，不是"复制一份还能回来"。
  ★ 所以快照走 `VACUUM INTO`，不走 Quarantine，也不自己 `io.Copy`
  （`io.Copy` 主库会漏掉未 checkpoint 的 `-wal`，拷出来的是旧影像——M6 手册与
  §6.48 都吃过"主库 4096 B、垃圾全在 wal"的亏）。

### 3.2 修法

`internal/cache` 新增：

```go
// Snapshot 用 VACUUM INTO 把当前库导成一份自包含单文件（M349）。
// dest 必须不存在（SQLite 硬约束）⇒ 落笔方先写 dest+".tmp-<nano>"，成功后 rename 覆盖到 dest。
func (c *Cache) Snapshot(dest string) error
```

`app_settings.go` 的 `CacheClear()` 改为**先快照、后清空**，且返回结果：

```go
type CacheClearResult struct {
	EntriesCleared int    `json:"entriesCleared"`
	ReclaimedBytes int64  `json:"reclaimedBytes"`
	SnapshotPath   string `json:"snapshotPath"`
}
func (a *App) CacheClear() (CacheClearResult, error)
```

顺序与失败面（逐条都是 fail-closed，与本仓既有纪律同形）：

1. 取 `before := GetStats()`（拿条数与占用，用于回报和"回收了多少"）。
2. `snapshotPath = filepath.Dir(DBPath()) + "/cache-backup.db"`（**固定名 ⇒ 天然只留最近一次**，
   这是 R-3 的字面落地；不建 `cache-backups/` 目录，与 `*.broken-*` 同放配置目录，手册好指）。
3. `Snapshot()` 失败 ⇒ **直接返回错误，一个字节都不删**（"要清但没备份成"不是"清不了"，
   是把风险说清楚；与 dbfile「要么完整隔离、要么原地不动」、C4「绝不拿误判赌用户数据」同一条取向）。
4. 成功 ⇒ `Clear()`；`Clear()` 返 `ErrReclaimFailed` 时**仍算清空成功**：
   回报条目数与快照路径，回收字节按实测（可能为 0）给，文案分岔见 §3.4。
5. `after := GetStats()`，`ReclaimedBytes = max(0, before.DBSizeBytes - after.DBSizeBytes)`。

### 3.3 判据

- `TestCacheClearAbortsWhenSnapshotFails`（根包）：注入快照失败 ⇒ 断言
  ①返回 error ②`GetStats().Entries` **仍等于**清空前的条数（一条没少）③错误串含"未删除任何缓存"。
  ★ 这条是 R-1/R-3 的承重探针：它证明"防误删"不是事后补救，而是**前置门槛**。
- `TestCacheClearKeepsSingleSnapshot`：连续清两次 ⇒ 目录内 `cache-backup.db*` 只有一份、
  固定名被第二次覆盖、无 `.tmp-` 残留。
- `TestCacheClearReturnsCounts`：20 000 条场景断言 `EntriesCleared==20000`、
  `ReclaimedBytes > 1<<20`、`SnapshotPath` 存在且能被 `cache.Open` 打开读回 20 000 条
  （**快照可用性**必须是断言，不能只是"文件生成了"）。
- 变异：把快照挪到 `Clear()` 之后 ⇒ 第一条红（条目已为 0）；
  快照名改成带时间戳 ⇒ 第二条红（留了两份）；
  只 `os.Create` 空文件当快照 ⇒ 第三条红（打不开/读不到条目）。

### 3.4 前端与契约（M353 之前，批 1 只动这一处）

- `wails.ts`：`cacheClear: () => Promise<CacheClearResult>`，并新增 `export interface CacheClearResult`
  + `wailsMirrors` 注册（`TestWailsInterfacesAreAllMapped`/`TestBackendAPIMatchesGoExportedMethods`
  零豁免，不注册必红）。
- 成功 toast 改为报数：`已清空 20,000 条 · 回收 4.1 MB · 快照在 cache-backup.db`；
  `ErrReclaimFailed` 腿单独走 warn 档（"已清空 N 条，但空间未回收：<原因>"），
  **绝不允许**在这一腿说"清空失败"。
- 两段式就地确认的形状不动（R3-5 的既有裁定），只把"确认清空"那行说明补一句
  "会先把现有缓存快照到 `cache-backup.db`（覆盖上一次）"——预览后果，不是加第三段确认。

### 3.5 边界

- 快照**只有一份**且会被下次清空覆盖 ⇒ 它防的是"这次手滑"，不防"三个月前那次手滑"。
  手册 §缓存维护要按这个尺度写，不许写成"备份机制"。
- 快照落在**配置目录**，与主库同盘同卷 ⇒ 它不防卷损坏/误删整个目录；
  要防那一档得走 M351 的导出记录（可选到别的盘）。手册两条要互相指一句。
- 快照是**哈希缓存**的，不含账本 ⇒ 与 R-1 不冲突，但这句话必须写出来，
  否则用户会以为 `cache-backup.db` 能找回清理记录。

## 4. M350（APP-16）「清缓存不连坐账本」缺结构守卫

### 4.1 为什么单独占一个号

R-1 是用户这次的原话要求，而它在今天的代码里**只是巧合成立**——
`CacheClear` 没去碰 `a.hist` 纯属没写，没有任何一条测试拦着"以后有人在清缓存里顺手清历史"。
没有守卫的承诺下一批就会被改坏，所以要一条**结构测试**，不是注释。

### 4.2 判据

`app_cache_boundary_m350_test.go`（根包，走 `newHistApp` 同款真账本夹具）：

1. 种 1 条扫描历史 + 1 条清理记录（含 `op_items`）+ 若干缓存条目；
2. 记下 `history.db` 的 size/mtime 与 `ListScanHistory()`/`ListOpRecords()` 两条清单；
3. `CacheClear()`；
4. 断言：①缓存 `Entries==0` ②两个清单**逐条等值**（不是"非空"，是等值）③账本文件的回撤条目状态、
   `hist_files` 计数不变（经公开查询法读，不裸 SQL，遵 §5 测试惯例）⑤ 快照文件名不落在账本目录。

变异：在 `CacheClear()` 里临时加一句 `hs.ClearScans()` ⇒ 本条必须红。
★ 这条探针**不提供任何修复**，它是 R-1 的执行合同；写在批 1 里，红→绿一次即可。

---

# 第二部分 批 2：记录导出 / 导入（增量合并）

## 5. M351（HIST-xx）导出记录：`.db` 影像 + `.json` 只读镜像

### 5.1 取证与既有面

- 账本四张表 DDL 现读在 `internal/history/history.go:62-117`：
  `scan_history`(12 列) / `hist_groups`(6) / `hist_files`(6) / `op_records`(8) / `op_items`(9)。
  ★ 是**五张**表（四张历史 + `op_items`），设计稿上一律按五张写。
- 根包已有出口：`ListScanHistory` `LoadScanHistory` `DeleteScanHistory` `ClearScanHistory`
  `ListOpRecords` `GetOpRecord` `ClearOpRecords`（`app_history.go:22/46/119/136/245/266/290`）。
  **没有**任何导出/导入。
- 对话框先例：全仓只有一处 `wruntime.OpenDirectoryDialog`（`app_scan.go:23` `SelectDirectory`），
  **后端起对话框**是本仓既有惯例 ⇒ 新 RPC 沿用。已核 v2.16.0 `pkg/runtime/dialog.go`：
  `OpenFileDialog:44`、`SaveFileDialog:66` 都在册可用。
- `ExportReport(format, path)`（`app_settings.go:88`）是**报告**导出的空桩（返回"将在 M5 提供"），
  与"记录导出"是两件事。★ 本批**不接管、不实现、不改名**它，只在手册里隔一句防止混读。

### 5.2 修法

- 选目录用既有 `OpenDirectoryDialog`（不引入 `SaveFileDialog`：要一次写**两个**文件，
  选目录比选两个文件名更贴合，且复用已证明可用那条腿）。
- `func (a *App) ExportRecords() (RecordsExportResult, error)`（后端起对话框 + 落两文件）：
  - `filededup-records-<20060102-150405>.db` —— `VACUUM INTO`，自包含一致快照（M349 已实测该语句可用）。
  - 同名 `.json` —— 只读镜像：`{schemaVersion, exportedAt, scans:[…groups/files], ops:[…items]}`，
    BLOB 以 hex 串呈现。★ **不参与导入**（R-2 走的是 `.db`），只供人核对与 diff。
  - 文件名带时间戳 ⇒ 天然不撞 `VACUUM INTO` 的"目标必须不存在"硬约束；仍先落 `.tmp` 再 rename。
  - 返回 `{dir, dbPath, jsonPath, scans, ops, bytes}` 给界面报数。
- 在途守卫：与 `ClearOpRecords` 同一条 `opsRunning`（B3-1 先例）——执行中途导出会得到
  半本账（`planned` 未收口），宁可拒绝。扫描在途**不拦**（扫描只读账本，导出不改文件语义）。

### 5.3 判据

- `TestExportRecordsWritesBothFiles`：导后两文件都在、`.db` 能被 `history.Open` 打开且
  `ListScans`/`ListOps` 条数与源库**逐条等值**；`.json` 能被 `json.Unmarshal` 且 `scans` 数一致。
- `TestExportRefusesWhileOpsRunning`：`opsRunning` 置位 ⇒ 拒绝、**两个文件都不生成**
  （半本账不该被固化成一个看起来完整的文件）。
- `TestExportIntoNonWritableDir`：目标目录不可写 ⇒ 返回中文错、无 `.tmp` 残留。
- 变异：只导 `.db` 不导 `.json` ⇒ 第一条红；不查 `opsRunning` ⇒ 第二条红；
  失败路径不删 `.tmp` ⇒ 第三条红。

## 6. M352（HIST-xx）导入记录：ATTACH 增量合并

### 6.1 语义定义（"增量"必须先有可执行的定义，否则合并无从判据）

R-2 原话「导入时以增量的形式扩展本地记录」⇒ 三条硬约束：

1. **不删不改本地任何已有行**（导入不是恢复，是合并）。
2. 同一份文件**导两次不翻倍** ⇒ 需要**自然键**判"这条已经在本地了"：
   - `scan_history`：`(saved_at, roots, filters, groups_count, files_count)`
   - `op_records`：`(created_at, op_kind, target_dir, done_count, reclaimed)`
   ★ 为什么不能只比 `saved_at`：同一秒内两次扫描（自动化/重试）会撞键，误判成"已有"
   就静默吞掉一条真记录；roots/filters 入键后误吞概率才够低。
   ★ 为什么不认 `id`：`id` 是各库自己的 AUTOINCREMENT，跨库无意义，照 `id` 插会撞主键或错关联。
3. 返回 `{scansAdded, scansSkipped, opsAdded, opsSkipped}`，界面如实报"新增 N 条 / 跳过 M 条（本地已有）"。

**外键重映**（本条最难的一格，做错就是错关联而不是报错）：

- 父行进本地后拿 `last_insert_rowid()`，**子行按父行的源库 id 批量带上新 `hist_id`** 插入；
  `hist_files` 再套一层（同时需要新 `hist_id` 和新 `group_id`）。
- `op_records.hist_id` **不是外键**（DDL 现读 `INTEGER NOT NULL DEFAULT 0`，无 `REFERENCES`），
  它是溯源指针 ⇒ 必须同样过一遍 `源 id → 本地 id` 映射；
  ★ 命中的源 scan 若**因自然键被跳过**，映射表里也必须给它指到**本地那一条**
  （`roots`+`saved_at` 回查），否则导入的记录会指向一个不存在的 hist_id，
  记录页"联动裁剪/恢复"那条腿会静默失效。
- 找不到映射的 `hist_id`（导出的库本身缺该行，或用户手删过）⇒ **置 0**（= 无关联），
  并在返回里记一条 `opsOrphaned`，不猜、不硬塞。

### 6.2 实现形状

`internal/history` 新增 `func (s *Store) ImportFrom(srcPath string) (ImportSummary, error)`：

1. 前置校验（**在任何写之前**）：`history.Open(srcPath)` 走一遍 ⇒ 拿到
   ①不是数据库/损坏（`dbfile.IsCorruption` 分类，中文错，本地不动）
   ②五张表齐全 + 必需列在位（`PRAGMA table_info` 比对；缺列直接拒，别静默丢字段）
   ③关掉它。**不**用 ATTACH 一个未知文件当校验手段（ATTACH 失败点分散、难回干净）。
2. 事务内 `ATTACH DATABASE ? AS src` → 逐表合并 → `DETACH` → `COMMIT`。
   整体要么全成要么全不动（中途 error ⇒ `tx.Rollback()`，本地零变化）。
3. `foreign_keys` 在本库是**逐连接 ON**（M59 已收归 `sqlconn`），先父后子的顺序天然满足；
   不要用 `PRAGMA defer_foreign_keys` 绕过——那是把约束关掉当省事用。
4. 合并走 `INSERT … SELECT … WHERE NOT EXISTS(自然键)`，一条 SQL 判重 + 插一条，
   避免 N+1 往返；子表用"按父 id join 已插入行"的批量语句。

### 6.3 判据

- `TestImportMergesIncrementally`：本地 2 条历史 + 文件 3 条（其中 1 条与本地自然键相同）
  ⇒ 结果 4 条（**本地那 2 条一行没动**，逐字段等值断言），`scansAdded=2 / scansSkipped=1`。
- `TestImportIsIdempotent`：同一文件连导两次 ⇒ 第二次 `added=0`，总数不变。
- `TestImportRemapsChildIds`：导入后新 `hist_files.group_id` **必须**指向新 `hist_groups.id`
  （断言 `hist_id`/`group_id` 关联完整、`ON DELETE CASCADE` 实测生效：删掉新父 → 子行清零）。
- `TestImportRemapsOpHistIDToExistingLocal`：源里那条 op 指向的 scan 被自然键判重跳过 ⇒
  op 的 `hist_id` 指到**本地已有**那条，而不是 0、也不是源 id。
- `TestImportOrphanHistIDAzeroed`：源 op 指向源库里不存在的 scan ⇒ `hist_id=0` 且 `opsOrphaned=1`。
- `TestImportRejectsCorruptAndKeepsLocalUntouched`：喂一个截断/非数据库文件 ⇒ 中文错误、
  本地表条数与内容**逐字节不变**。
- `TestImportRejectsMissingTable`：合法 SQLite 但少 `op_items` ⇒ 拒、点名缺哪张表。
- `TestImportRefusesWhileOpsRunning`：同 M351 的 `opsRunning` 腿。
- 变异（每条都要落预测格）：把自然键只留 `saved_at` ⇒ `TestImportMergesIncrementally`
  的"同秒两条"子格红；`op_records.hist_id` 不重映 ⇒ `TestImportRemapsOpHistIDToExistingLocal` 红；
  前置校验改成直接 ATTACH ⇒ `TestImportRejectsMissingTable` 红（缺表在 SQL 层才炸，
  且此时可能已插了半截父行）；不做事务 ⇒ `TestImportRejectsCorrupt…` 红。

### 6.4 ★ 导入的记录能不能回撤（口径必须先取证再写手册）

现读 `internal/ops/undo.go:22-23`：trash/move/hardlink 三条回撤路径**都做全量 BLAKE3 比对**，
外加 `identityStill` 删前复核（:225）与 `restoreInPlace` 的"还是不是刚哈希那一份"（:88）。
⇒ 导入来的记录不会比本地记录多拿到一分删除权限：内容或身份不符时它**回撤失败并报错**，
而不是按外来账本盲删。这一条允许手册写"导入的记录同样逐文件校验"。
★ 但**不得**写成"导入外来账本绝对安全"：`op_items` 的 `dest_path`/`link_src` 是回撤的**复制源**，
指向谁就从谁那里复制——本批**不**新增"只导入元信息不信任路径"的防线。
这条让渡写进登记行与手册，并进 05 真机清单一条（用户自己的盘、自己导出的文件这一档场景实测）。

## 7. M353（FE-xx）记录页 UI + 契约注册

- `RecordsView.vue` 工具行（:212-231 两个清空块旁边）加「导出记录」「导入记录」两按钮：
  - 导出走**单击**（不覆盖任何东西 ⇒ 无需两段式确认；R-1 的边界由 M350 守卫而非确认框）。
  - 导入走**两段式就地确认**（复用 `confirmClear`/`clearAll` 的形状 :26/:50-54）：
    第一下把后果写在原处——"只新增本地没有的记录，不修改也不删除已有 N 条"，
    给「确认导入 / 取消」；第二下才执行。★ 与本仓 R3-5 裁定一致：不用浏览器原生 `confirm()`。
  - 在途置灰沿用 `store.busy` + `store.busyTip`（与清空按钮同一双腿）。
- 结果 toast 报数：导入 `新增 2 条历史 / 跳过 1 条（本地已有）`；导出给目录路径。
  导入完成后调 `store.refreshHistory()` + `store.refreshOps()`（两个都刷：
  合并同时动了两张清单，只刷一个会显示成"导入没生效"，正是本批要消灭的那类误读）。
- 契约注册（零豁免闸）：`wails.ts` `BackendAPI` 加 `exportRecords`/`importRecords`；
  `RecordsExportResult`/`RecordsImportResult` 两接口 + `wailsMirrors` 各一行。
- `frontend/wailsjs/go/main/App.d.ts` 是生成物、随 `wails build` 重生成（M46/M205 在册口径），
  本批手改**不是**判据；真机跑一次 build 后顺手带走。

---

# 第三部分 门禁、文档回写与真机欠账

## 8. 文档双回写（memory 硬约束：09 真源、10 派生，功能变更须双回写）

| 文件 | 改哪 |
|------|------|
| `docs/09-用户手册.md` §4.7 | 「手动清空」补快照一步与"只留最近一次"；体积参考那句改写 `dbSizeBytes` **含 WAL**；「清空可能需要几秒」写明 |
| `docs/09` §缓存维护 9 | 兑现「想立即回收空间」——现写的是承诺，本批让它成真，措辞按实测读数重写 |
| `docs/09` §6.6/6.7 或新 §6.8 | 导出/导入记录：两文件含义、`.json` 不参与导入、增量语义、同文件二次导入不翻倍、导入记录的回撤口径与 §6.4 的让渡 |
| `docs/09` §术语/FAQ | 加一条「为什么条目 0 但以前占用没变」——这次症状的正面回答 |
| `docs/10-…用户手册` | 上述四节逐条派生转写（同一口径、两种话术） |
| `docs/02` §4.2 / `docs/01` §7.3 | `dbSizeBytes` 口径一句、清空三步（DELETE→VACUUM→checkpoint）进设计口径 |
| `docs/04` §6.65 | M347~M353 登记行 + 全套门禁读数 + 未兑现格 |
| `docs/05` 真机清单 | 新增格：macOS/Windows 的**系统对话框**（导出选目录、导入选文件）CI 无从驱动；Windows/Linux 腿 VACUUM+checkpoint 的字节读数（本机 macOS 已有实测，另两腿只能真机） |

## 9. 门禁与交付节奏（按 memory `feedback-workflow-rules` 的三提交）

1. **设计段单独提交**（本文件）。
2. 批 1 实施提交（M347~M350 + 前端 M349 那一小段）：先提探针红，再修到绿。
   全套本地门禁按 `scripts/` 15 行跑，读数逐条抄进 §6.65，不写结论式总结。
3. 批 2 实施提交（M351~M353）。
4. 划账提交 + push + `gh run watch` 盯三腿；红了走复批，复批回填只增不删。
5 ★ 凡"代码已改、验证未兑现"（系统对话框、Windows/Linux 的体积读数）一律显式写未兑现，
不得写作已通过。

## 10. 本批明确**不做**的（防止范围自我膨胀）

- 不做应用内备份管理器（列表/恢复/删除快照）——R-2 已把恢复手段定为"导入记录"。
- 不动记录页两个「清空」出口（R-1 追加裁定确认：那是"在记录页删"的合法入口）。
- 不接管 `ExportReport`（M5 的报告导出空桩原样留着）。
- 不给 `history.db` 加 VACUUM/checkpoint（本仓不向 UI 报账本体积，症状不存在；
  要清账本磁盘是另一格，登记为后续观察项，不混进本批）。
- 不做快照的多份轮转（R-3 定一份）。

## 11. 批 1 实施后追记（只增不删：本节是设计段落笔时**没有**的读数）

### 11.1 `-wal` 在"只读打开"下就存在 —— §2.3 那条判据的前提被现读否掉

设计段原本打算钉"没有侧文件时 `dbSizeBytes` 仍等于主库"。现读否掉了这个夹具：
**只要有一条活连接，`-wal` 就在场**——新建库、只 `Open` 不写任何东西，读数就是
主库 4,096 B / `-wal` 57,712 B。所以"无侧文件"不是一个能自然出现的运行态，
把它写成用例等于钉一句永远不会被走到的话。

拆成两条真判据：
- `TestStatsWalEmptyEqualsMain`：清空（走完整 checkpoint/TRUNCATE）之后 `-wal` 确实
  是 0 字节 ⇒ 此时 `dbSizeBytes == 主库`。这才是"侧文件为 0 时不多算"的真照片。
- `TestDiskUsageToleratesMissingFiles`：直接对 `diskUsageLocked` 造缺失文件，钉的是
  helper 的容忍性（展示量不值得为一条 `os.Stat` 失败把整个 `CacheStats` 变成 error）。

### 11.2 ★ 探针缺陷（自纠，必须留痕）：第一版 busy 用例是**假造的**，变异当场空转

MU-c 第一次跑是 **全绿**。原因是我在探针里覆盖了 `checkpointFn` 接缝、直接返回
`busy=1`——于是"把真函数换成 `db.Exec` 并忽略 busy 列"这个变异改的正是被我覆盖掉的那段，
用例看不见任何差别。这不是变异没杀伤力，是**用例没有对真实现取证**。

修法：删掉假接缝，改用**第二个真实连接**复现——`sql.Open("sqlite", cch.DBPath())` 另开一腿，
持一个未消费完的 `rows` 游标，再对本库写 200~300 条 ⇒ `PRAGMA wal_checkpoint(TRUNCATE)`
自己回 `busy=1`，`-wal` 实测留在 86,552 B 不截。此时 MU-c 才落红。
接缝本身保留在 `reclaimLocked` 里（它是这条路径可测的唯一手段），但**判据不许建立在接缝的
返回值上**；这条约束对以后所有 `*Fn` 接缝通用。

MU-d 同样逃逸过一次：锚点断言的是"暂未回落"，那句话在**内层** busy 串里，重写外层哨兵时
它照样存活。补的是 `TestErrReclaimFailedTextDoesNotDenyTheDeletion`——直接断言哨兵自身
含「已清空」且**不含**「失败」，把"文案纪律"钉在文案自己身上而不是钉在拼装处。

### 11.3 busy 那一档的**代价**读数：约 5 秒

`busy_timeout=5000` 是每条连接都吃的会话级 PRAGMA（`connPragmas`），所以 checkpoint 撞上
持锁方时不是立刻返回，而是**先等满 5 s 再回 `busy=1`**。实测 `TestClearBusyDoesNotClaimSuccess`
单条 5.04 s、`TestCacheClearReclaimFailStillReports` 5.04 s——这两格的时长本身就是这条通道的
照片。后果要如实说：清空缓存这个动作在他进程持写锁时**会卡住界面约 5 秒**，然后才报出
"已清空但空间未回收"。这比修前（立刻返回、但字节没减）慢，换来的是那句话是真话。
本批**不**调小超时：5 s 是全库共用的让路窗口，为这一格单独收紧会把 M20 那批的取向改掉。

### 11.4 前端两条接线锚的变异读数（批 1 交付证据补齐）

- MU-k（catch 臂改回 `toast.notifyError('清空缓存失败', e)`）⇒ 门禁 rc=1，红在预测的那一条：
  「回收失败那一档走 warn 且不得写「清空缓存失败」」，理由是"未引用 `toast.push(toast.errText(e), 'warn'`"。
- MU-l（成功臂改成写死的 `notifySuccess('哈希缓存已清空')`）⇒ 门禁 rc=1，红在
  「清空回执把"回收了多少字节"报给用户」，理由"未引用 `res.reclaimedBytes`"。
- 还原后基线：`node 用例 156 项 + 接线断言 40 项 = 合计 196 项全部通过`（接线 38 → 40 即本批两枚）。

### 11.5 一处需要更正的自我引用

`CacheClear` 的注释最初指向 `app_cache_boundary_m350_test.go`——该文件从未存在，M350 的守卫
实际落在 `app_cache_clear_m349_test.go` 的 `TestCacheClearLeavesLedgerUntouched`。已就地改正。
登记在这里的理由：注释指向一个不存在的文件名，和文档写"已在 docs/05 挂格"是同一类说谎，
只是它藏在代码里、没有门禁会替我抓。

## 12. 批 2 实施后追记（只增不删：设计段落笔时**没有**的读数）

编号规则：§11 的 MU-* 是批 1 的；本节从 MU-o 起是批 2 我这轮**逐条亲手跑过**的，
没有一条是"听上一轮说跑过"。跑法一律是"打坏→跑判据→记红→还原→复跑记绿"，
还原后 rc 全为 0（读数见 §12.6 末行）。

### 12.1 ATTACH 那一形状被现读判死 —— §6 落笔时的设计不成立

设计段 §6 写的增量合并是 `ATTACH DATABASE ? AS srcdb` + 一句 `INSERT ... SELECT`。
实测（一次性探针，跑完即删）：

- 事务内 `ATTACH` 与跨库 `SELECT` 都成功；
- 事务内 `DETACH DATABASE srcdb2` ⇒ **`database srcdb2 is locked (1)`**；
- 同一连接在事务**外** ATTACH 再 DETACH ⇒ 两者都成功。

⇒ ATTACH 形状要么把 DETACH 推到事务外（源库在整个导入期间挂在写连接上），
要么让连接一直带着一个外来 schema。两条都比"另开只读连接 + 本地逐行事务"更含混，
所以实施走后者（`openReadOnly` / `ImportFrom`）。这不是偏好，是 DETACH 那条错误判的。

### 12.2 只读通道与影像的四条实测（`export.go` 页首引的就是这四条）

| # | 读数 |
|---|---|
| (1) | `VACUUM INTO` 产物 `journal_mode="delete"`、单文件 **40,960 B**、`page_count=10`，目录里**没有**它的 `-wal`/`-shm`；同一时刻源库是 main 4,096 B + `-wal` 74,192 B ⇒ "自包含"是可以数的：未 checkpoint 的内容被一并收进那 10 页 |
| (2) | `PRAGMA query_only=ON` 下 `UPDATE op_records SET done_count = done_count WHERE 1=0` ⇒ **`attempt to write a readonly database (8)`**（零行语句也被拒：拒的是权限，不是影响行数） |
| (3) | 只读连接打开前后，影像字节 **40,960 → 40,960**，`os.ReadFile` 全等 |
| (4) | 事务内 `DETACH` ⇒ `database srcdb2 is locked (1)`（§12.1） |

读数 (2)(3) 是"读外来库不许走 `history.Open`"这条约束的证据面：`Open` 会执行
`PRAGMA journal_mode=WAL` 与两条 `UPDATE op_items` 收口语句，确证损坏时还会改名隔离——
四件事全在**改动用户刚选中的那个文件**。判据落在 `TestImportLeavesSourceFileUntouched`
（字节全等 + 目录里不许出现 `image-*` 侧文件）。

### 12.3 ★ 影像的权限实测是 0644 —— 注释许了诺、代码没做的一格（本轮新发现）

`exportRecordsTo` 的注释写着"0o600 而不是 0o644：账本里是用户机器上的**全部文件路径**"，
而实测两件产物：

```
产物 filededup-records-20231115-061320.db   权限=0644
产物 filededup-records-20231115-061320.json 权限=0600
```

只有 `os.WriteFile` 那件吃了 0o600；`.db` 跟的是 SQLite 自己的默认。可那句话讲的隐私
对两件**同样**成立（影像里就是那五张表逐行）。⇒ 补 `os.Chmod(tmpDB, 0o600)`，
设不上就停止导出（M61 的在册取向：不确定的写入要在落账前拒绝；静默留一份 0644 的
账本影像在"很可能是外置盘/共享目录"的导出目录里，比这次没导成更难看）。
判据 `TestExportArtifactsAreOwnerOnly`，变异 MU-s 见 §12.6。

登记这一条的理由：没有任何门禁会替我抓"注释说了、代码没做"，它和 §11.5 那条
"注释指向不存在的文件"是同一类，只是这次是权限位。

### 12.4 `MarshalIndent` 会重排内层 `RawMessage` —— §5.3 的"字节一致"判据不成立

设计段 §5.3 想让镜像与库里的 TEXT 列**逐字节相同**（理由是"镜像的存在理由就是 diff"）。
实测 `json.MarshalIndent(mirror, "", "  ")` 会把内层 `json.RawMessage`（roots 那一段）
重新缩进——于是任何"整份镜像 == 库里字节"的断言必然假红。改后的判据：`json.Compact`
归一化之后比**内容**（`TestExportMirrorIsParseableJSON`）。搬运这件事仍然成立
（字段序、空数组表示都不重编），只是它的可断言形状是内容等值而不是字节等值。

### 12.5 正向类型面闸：G10 那一族被照出来，而且本仓真有一枚

批 2 前段（本轮上下文早段）跑 MU-o 时发现：删掉 `wailsMirrors` 里新加的行，
`TestWailsTypesCoverGoFields` 读作绿。这轮把 MU-o 做成两个形状，读数不同，分开记：

- **形状 A**——TS 有命名接口 `RecordsImportResult`、表里删掉那一行：
  `TestWailsInterfacesAreAllMapped` 与新增的正向闸**双双落红**。
  ⇒ 这一格老闸本来就兜得住（"TS 有而表里没有"正是它的判据），先前记的"两项都绿"
  只对 `TestWailsTypesCoverGoFields` 成立，这里更正，不沿用那句过头的话。
- **形状 B**——Go 下发一枚**入参**结构体，TS 侧只写内联对象类型、表里也不登记：
  两项老闸（`TestWailsTypesCoverGoFields`、`TestWailsInterfacesAreAllMapped`）**全绿**，
  只有新增的 `TestGoBindingTypesAreAllRegistered` 红在
  `Go 侧下发 1 枚结构体没登记进 wailsMirrors…：model.KeepPolicy`。

形状 B 不是假想的夹具：`model.KeepPolicy` 在本仓的真实形状就是
`wails.ts` 里的内联 `{ Kind: string; Directories: string[] }`——Go 侧改了字段名而 TS 不跟，
**没有一把尺子量得到**。修法是把它升成命名接口 `KeepPolicy` + 登记一行
（顺带让字段比对第一次覆盖到这枚入参），而不是在判据代码里给它开洞。

新闸的边界（写进注释，防止下一个人以为它管全部）：只扫**方法签名上直接出现**的
本仓结构体（入参与返回，解包指针/切片/映射），**不递归字段**——字段是父类型的一部分，
TS 侧把它们摊平进父接口，给它们各开一枚镜像反而是假契约面。
另带一条防空转的下限：`len(seen) < 20` 直接红（本轮实际 31 枚）。

### 12.6 批 2 变异读数（逐条，含两条自纠）

| 变异 | 打坏的守卫 | 读数 |
|---|---|---|
| MU-o(A/B) | 类型面登记 | 见 §12.5：形状 B 只有新闸红 |
| MU-r | `rawColumn` 的 `json.Valid` 拦截整段删掉 | **首次全绿**（夹具没有一列是坏的）⇒ 补 `TestExportRejectsCorruptColumn`（四列逐列循环）后复跑：roots / filters / failed_json / keep_paths **四子项全红** |
| MU-r2 | `ExportTo` 失败分支不再 `cleanup()` | 红在「留下了残留 `filededup-records-20231115-061320.db.tmp`」——残留正是那件**已经生成**的影像，这条同时证明用例非空转 |
| MU-s | 删掉影像的 `os.Chmod(0600)` | 红在「权限是 0644，要求 0600」（§12.3） |
| MU-t | 结构不符的哨兵文案换成通用「导入失败」 | 红在 `TestImportRejectsMissingTable` |
| MU-u | 子行合并失败被 `_ = err` 吞掉、继续提交 | 红在 `TestImportMidChildFailureRollsBack` |
| MU-n1 | 自库守卫 `srcPath == s.path` 改成永不相等 | 红在 `TestImportRefusesSelf` |
| MU-n2 | 存在性守卫失效 | **第一次跑的是假红**：整行换掉后 `"os" imported and not used` 编译失败，rc=1 但判据根本没跑到。改成保留 `os.Stat`、只中和条件（`err != nil && false`）⇒ 真红，且红得与 §6 的预言逐字对上：「不存在的文件报成了结构错误：表 scan_history 整张不存在…」（SQLite 对不存在的库会**建出**空库） |
| MU-n3 | `sum.OpsOrphaned++` 删掉 | 红在 `TestImportOrphanHistIDIsZeroed` |
| MU-n4 | 子行归位改用源库 id | 红在 `TestImportRemapsChildIds` |
| MU-n5 | 取消被报成 `error` | 红在 `TestExportCancelIsNotFailure` |
| MU-n6 | 导入后只重取一张清单 | 门禁 rc=1，红在接线锚「未引用 `store.refreshHistory(), store.refreshOps()`」 |

★ 与 §11.2 同一条自纠记法：MU-r 与 MU-n2 第一次都是**假通过/假红**——前者是判据没覆盖
（夹具缺坏列），后者是变异改不动（编译挡路）。两次都不是"实现没问题"的证据，
都是"我这轮还没取证"的证据。
批 1 的 MU-c（checkpoint 的空清理）维持**无判据覆盖**：本机造不出那条分支（§11.2）。
本节没有任何一条变异是"锚点找不到、于是没执行"。

### 12.7 交付读数（批 2）

- 新增用例 **30 项**：`internal/history/export_m351_test.go` 5（含 4 个坏列子项）、
  `import_m352_test.go` 12、`app_records_io_m351_test.go` 13。
- `go build ./...` OK；`go test ./... -count=1` 全绿（无一包 FAIL）。
- 四项 Wails 契约闸全绿（含新加的正向那一把），`wailsMirrors` 现有 31 枚 Go 类型。
- 前端：`scripts/test-frontend-logic.sh` = node 156 项 + 接线 45 项 = **201 项全部通过**，
  `vue-tsc --noEmit` 无输出。
