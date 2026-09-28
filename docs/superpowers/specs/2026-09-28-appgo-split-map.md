# app.go 拆分映射表（M336 / M340 配套）

> 用途：`docs/04` 登记表与历批划账里钉着大量 `app.go:NNN` 坐标（如 M169 `app.go:2313`、
> M285 `app.go:385-397`、M288 `app.go:1374-1392`）。2026-09-28 M336 把 `app.go` 按职责簇拆成
> 七个文件后，**这些坐标全部失效**（第 16 行 `OOR` 由 8 涨到 67）。
>
> ★ 但登记表受**约束 (1)** 保护（已登记行不得改写），所以**不改那些锚**，改用本表按**方法名**
> 反查新位置——这正是 M328 立的"认锚点、不钉行号"纪律的用法。
>
> 生成方式：`go/parser` 实测（拆分前一份 AST、拆分后每个文件一份 AST），不是手抄。
> 命令见文末。

## 映射表（67 个 `func (a *App)` 方法）

| 方法 | 拆分前（app.go） | 拆分后 |
|---|---|---|
| `startup` | app.go:349-366 | `app_lifecycle.go:21-38` |
| `setCtx` | app.go:368-374 | `app_lifecycle.go:40-46` |
| `openCache` | app.go:376-405 | `app_lifecycle.go:48-77` |
| `addStartupNotice` | app.go:438-455 | `app_lifecycle.go:79-96` |
| `openLedger` | app.go:457-487 | `app_lifecycle.go:98-128` |
| `beforeClose` | app.go:489-530 | `app_lifecycle.go:130-171` |
| `cancelInFlight` | app.go:532-541 | `app_lifecycle.go:173-182` |
| `shutdown` | app.go:543-567 | `app_lifecycle.go:184-208` |
| `SelectDirectory` | app.go:571-582 | `app_scan.go:19-30` |
| `authorizeDir` | app.go:584-599 | `app_result.go:577-592` |
| `moveTargetAllowed` | app.go:601-631 | `app_result.go:594-624` |
| `authSnapshotLocked` | app.go:633-640 | `app_result.go:568-575` |
| `landingGuard` | app.go:642-665 | `app_result.go:626-649` |
| `resultSuperseded` | app.go:707-711 | `app_result.go:562-566` |
| `StartScan` | app.go:726-884 | `app_scan.go:32-190` |
| `PauseScan` | app.go:886-887 | `app_scan.go:192-193` |
| `ResumeScan` | app.go:889-890 | `app_scan.go:195-196` |
| `CancelOperation` | app.go:892-906 | `app_scan.go:201-215` |
| `CancelScan` | app.go:908-909 | `app_scan.go:198-199` |
| `goTask` | app.go:911-923 | `app_lifecycle.go:235-247` |
| `recoverGoroutine` | app.go:947-970 | `app_lifecycle.go:210-233` |
| `GetScanProgress` | app.go:972-977 | `app_scan.go:217-222` |
| `GetStatus` | app.go:979-980 | `app_scan.go:224-225` |
| `GetResultGroups` | app.go:984-1052 | `app_result.go:20-88` |
| `buildSortedGroupsLocked` | app.go:1054-1097 | `app_result.go:90-133` |
| `invalidateViewCacheLocked` | app.go:1099-1104 | `app_result.go:135-140` |
| `GetFailedItems` | app.go:1210-1226 | `app_result.go:142-158` |
| `PreviewFile` | app.go:1241-1292 | `app_reveal.go:22-73` |
| `RevealInFolder` | app.go:1336-1355 | `app_reveal.go:75-94` |
| `warnRevealExit` | app.go:1461-1480 | `app_reveal.go:96-115` |
| `RevealPath` | app.go:1526-1545 | `app_reveal.go:117-136` |
| `OpenPath` | app.go:1547-1561 | `app_reveal.go:138-152` |
| `RevealKeepSource` | app.go:1563-1588 | `app_reveal.go:154-179` |
| `settingsPath` | app.go:1592-1606 | `app_lifecycle.go:249-263` |
| `GetSettings` | app.go:1608-1634 | `app_settings.go:19-45` |
| `SaveSettings` | app.go:1636-1660 | `app_settings.go:47-71` |
| `GetStartupNotice` | app.go:1677-1684 | `app_settings.go:73-80` |
| `GetVersion` | app.go:1686-1687 | `app_settings.go:82-83` |
| `pendingIDsLocked` | app.go:1749-1814 | `app_result.go:264-329` |
| `pendingSetLocked` | app.go:1816-1839 | `app_result.go:331-354` |
| `PreviewProcessPolicy` | app.go:1841-1881 | `app_result.go:356-396` |
| `GetPendingFiles` | app.go:1883-2045 | `app_result.go:398-560` |
| `FilterInDirs` | app.go:2047-2063 | `app_result.go:160-176` |
| `ApplyKeepPolicy` | app.go:2065-2102 | `app_result.go:178-215` |
| `ClearKeepDecisions` | app.go:2104-2119 | `app_result.go:217-232` |
| `keepPathsLocked` | app.go:2121-2138 | `app_result.go:234-251` |
| `persistKeepPaths` | app.go:2140-2149 | `app_result.go:253-262` |
| `ListScanHistory` | app.go:2168-2188 | `app_history.go:21-41` |
| `LoadScanHistory` | app.go:2190-2262 | `app_history.go:43-115` |
| `DeleteScanHistory` | app.go:2264-2280 | `app_history.go:117-133` |
| `ClearScanHistory` | app.go:2282-2295 | `app_history.go:135-148` |
| `histSnapshot` | app.go:2346-2359 | `app_history.go:150-163` |
| `cchSnapshot` | app.go:2361-2372 | `app_history.go:165-176` |
| `warnBackground` | app.go:2374-2388 | `app_history.go:178-192` |
| `warnLedger` | app.go:2390-2400 | `app_history.go:194-204` |
| `beginJournal` | app.go:2402-2438 | `app_history.go:206-242` |
| `ExecuteOperation` | app.go:2440-2689 | `app_ops.go:24-273` |
| `ListOpRecords` | app.go:2736-2743 | `app_history.go:244-251` |
| `GetOpRecord` | app.go:2760-2791 | `app_history.go:253-284` |
| `ClearOpRecords` | app.go:2793-2809 | `app_history.go:286-302` |
| `UndoOperation` | app.go:2811-2890 | `app_ops.go:275-354` |
| `undoExecuteItem` | app.go:2922-2955 | `app_ops.go:356-389` |
| `UndoOperationItem` | app.go:2957-3032 | `app_ops.go:391-466` |
| `OpenTrash` | app.go:3034-3060 | `app_ops.go:468-494` |
| `ExportReport` | app.go:3062-3067 | `app_settings.go:85-90` |
| `CacheStats` | app.go:3069-3077 | `app_settings.go:92-100` |
| `CacheClear` | app.go:3079-3086 | `app_settings.go:102-109` |

## 不在方法区的坐标怎么换算

`app.go` 里除了方法还有包级类型/常量/辅助函数（它们留在 `app.go`，但删掉 67 个方法块后行号重排）。
换算规则：

```
新行号 = 旧行号 − （旧行号之前被删除的行数）
```

被删除的行 = 上表里所有 `[os, oe]` 区间的并集。需要时按这个式子算，不另列全表。

## 复现命令

```bash
# 1) 拆分前的边界（本仓已无拆分前的 app.go，这张表是唯一的留存证据）
# 2) 拆分后：对每个文件跑一次 AST 定界
cat > /tmp/ast/main.go <<'GO'
// 用 go/parser 输出每个 func (a *App) 的 Doc/Pos/End 行号（TSV）
GO
for f in app_lifecycle app_scan app_result app_reveal app_ops app_history app_settings; do
  go run /tmp/ast/main.go "$f.go"
done
```

★ 不要用手写正则去切分 Go 源码——本轮第一版就是这么把 `app.go` 打成 8 处 syntax error 的
（见 `docs/04` §6.59 三·2）。
