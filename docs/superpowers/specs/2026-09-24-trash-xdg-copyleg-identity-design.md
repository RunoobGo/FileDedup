# 31. Linux 回收站跨卷复制腿删源身份守卫（第五轮审查 P0 修复批）

> 状态：**实施前设计段**（2026-09-24 第五轮全面审查发现台账 P0，用户回复"处理"批准执行）。
> 划账落 §6.29；本文件先单独提交，实施再提交。

## 31.0 取证（改前真读数）

`internal/ops/trash_linux.go:83-101`（`moveIntoTrash`）跨卷退化腿：

```go
err = copyAndSync(f, dst, st.Size())
f.Close()
if err != nil { os.Remove(dst); return err }
return os.Remove(src)          // ← 删源前无任何身份复核
```

对照同仓 `move.go:59-87`（AS-H4，2026-09-20 全仓审计已修）：复制前 `pathIdentity(src)`
取身份底片、复制后 `identityStill(src, srcID)` 复核、不符则**保留源**并交回落点。
`undo.go:214-228` 同型守卫亦在。**本腿是全仓唯一没吃到 AS-H4 的跨卷复制后删源路径**——
复制可持续数秒到数分钟，窗口内第三方以 rename 顶替 src（同步盘/下载器原子落子的时序），
`os.Remove(src)` 删掉的是别人的新文件，且 trashXDG 把该路径记为成功、账本记 done。
（主代理逐行对照亲验，见审查台账 project-full-audit-2026-09。）

相关既有声明：`trash_linux.go:126-127` 注释把"跨进程竞态"建立在"单实例桌面应用语境"
的前提上——而 OS 级单实例锁**全仓不存在**（台账 P1 首条，待裁定）。本批不借此扩面，
只在划账点名该前提仍未兑现。

## 31.1 为什么先把实现腿下沉为无 tag 文件（H6 惯例）

修判据要"修前必红 + 变异"，而 `moveIntoTrash` 钉在 `//go:build linux` 上、本机是
darwin、无 docker/无本地 Linux 运行通道（本批现查：`docker info` 不可用）⇒
行为级判据在改前形状下**本机永远跑不动**，只剩 CI linux 腿的"推上去才知红绿"。
按既有惯例"纯逻辑下沉无 build tag 文件进全平台门禁"（H6）：

- `trashXDG / moveIntoTrash / nameFree / uniqueXDG / writeTrashInfo / xdgEscapePath /
  trashXDGGuard / defaultTrash 的 XDG 路径拼接` 全部是纯 os/path 逻辑，跨平台可编译；
- 下沉后探针/负控制/变异**三件套在本机当场可跑**；CI linux 腿照旧跑
  `trash_linux_test.go`（一字不动），windows 腿不会调用该代码（`defaultTrash` 各有平台实现）。
- 留在 `//go:build linux` 的只剩 `defaultTrash`（XDG_DATA_HOME 取数 + 转发）。
- ★ 不迁 `trash_linux_test.go` 的原因：其中 chmod-0、`/dev/shm` 真跨卷、悬空链接几组
  夹具的前提**不保证在非 linux 平台成立**（§6.17 CI 批口径 1）；让它们继续在 linux 腿跑，
  不给 darwin/windows 新增假红或新 SKIP 行。

## 31.2 修法（形状照抄 move.go AS-H4，落点处置按回收站语义改一处）

1. **行为中性的接缝前置**（同一提交内先落地，保证探针能对着"改前逻辑+新接缝"取红）：
   - `os.Rename` → `renameFile`（move.go:437 既有 var，默认 os.Rename）；
   - 末尾 `os.Remove(src)` → `removeSrc`（move.go:445 既有 var）；
   - `copyAndSync` 调用点 → 新接缝 `var copyAndSyncFile = copyAndSync`
     （惯例同 `copyVerifyFile`：测试在"复制已完成、源尚未删除"这一刻构造顶替）。
2. **守卫**：`os.Stat` 之后、`os.Open` 复制之前取 `srcID, err := pathIdentity(src)`；
   复制成功后 `identityStill(src, srcID)` 不符 ⇒ 返回哨兵
   `errCopiedSrcSwapped` 包裹的中文错误（文案点名 dst 落点与 src，M88 口径）。
3. **落点处置（与 move 腿的唯一差异）**：move 腿两份并存都在用户目录；回收站腿的
   `dst` 半份在 `Trash/files/` 且对应 trashinfo 若不保留就成了"回收站里看不见原路径的
   孤儿"（P2 注释里已论证过的形状）。⇒ `trashXDG` 回滚 trashinfo 前判
   `errors.Is(err, errCopiedSrcSwapped)`：该错误**不删 trashinfo**，副本+info 成对留存
   （用户可从回收站还原；还原撞第三方占位由 DE 自身的重名策略处置）；其余错误照旧回滚。
4. **不扩面**：Windows 回收站增量快照交错（台账 P1）、`uniqueXDG` 先查后用的跨进程窗、
   跨卷目录复制腿对目录源的既有失败形状——全部只登记不改。

## 31.3 判据（修前红 → 绿 → 变异，三格各钉什么）

新增**无 tag** 测试文件 `trash_xdg_identity_test.go`（package ops）：

| 格 | 用例 | 钉的断言 |
|---|---|---|
| 修前红 | `TestTrashXDGCrossVolumeDoesNotDeleteReplacedSource` | ①`trashXDG` 必须报错（改前：报成功）②第三方文件必须仍在 src（改前：被删）③`removeSrc` 对 src 调用数=0（改前：1）④dst 副本+trashinfo 成对留存 |
| 负控制 | `TestTrashXDGCrossVolumeRemovesUnchangedSource` | 未顶替时删源恰好 1 次、src 消失、dst+info 在位（守卫不得过严） |
| 变异 | MU-a：`identityStill(...)` 翻恒真 | 修前用例 ②③ 当场红（红在"第三方被删"格，不红在夹具） |
| 变异 | MU-b：删掉哨兵回滚豁免（trashinfo 无条件删） | ④红（孤儿副本格） |

时序钉死走既有接缝：`forceCrossVolumeRename`（move_crossvolume_identity_test.go:28）
让首次 rename 稳定 EXDEV；`copyAndSyncFile` 钩子里"先写旁边再 rename 顶位"
（§6.17 夹具规矩，不用先删后建）。

**兑现边界（如实）**：以上三件套在 darwin 本机真跑；**linux 真机跨卷（/dev/shm）与
windows 腿行为读数本批不取**——linux 腿由既有 `trash_linux_test.go`（一字未动，CI 真跑）
+ `GOOS=linux go vet`/`go test -c` 编译级证据覆盖；CI 三腿真读数待推送批兑现（不推送则
划账写"未兑现"，不写"已通过"）。

## 31.4 计数与门禁口径

- 取数一律现跑：改前后各跑 `go test ./internal/ops -run 'TrashXDG'`；批末全套
  `bash scripts/run-gates.sh`（15 行），`src_test` 预期 +2（两条新用例），其余基线不动；
  darwin 腿 `top_SKIP` 不得因此批 +1（新用例结构上不可能 Skip：前提造不出即 `t.Fatalf`）。
- 提交节奏：设计段（本文）→ 实施（接缝+守卫+测试）→ 划账 §6.29（与批 B/批 C 合并为
  一次文档收口批，编号占用在划账时统一声明）。
