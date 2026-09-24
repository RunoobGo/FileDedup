# 2026-09-24 第五轮全仓审查修订计划

> 依据：五路只读并行审查（核心算法域 / 操作执行域 / 扫描缓存接线 / 前端 / 门禁CI-CLI），
> 高危与中危发现已由主代理开码核实（下文逐条标注核实状态）。
> 基线：HEAD = `462e0ef`，工作树干净，M 台账至 M222（04 §6.40）。
> 本轮 M 编号从 **M223** 起。

## Global Constraints

- **禁提交/禁推送**：所有批次只改工作树；每批完成 + 回归全绿后向用户汇报，提交与推送另行授权（feedback-review-fix-workflow 既定约束，覆盖本计划所有任务里的"commit"语义）。
- **RED 先行**：每条行为修复先落一个会红的测试（写明测试名），修完转绿；防线类修复配**变异负控制**（把防线改反，测试必须红）。
- **回归口径**（regression-verification-standard）：gofmt、build、vet×3（darwin/windows/linux）、`go test -race -count=2 ./...`、`go test -race -count=4 .`、`cd frontend && npm run build`（含 vue-tsc）、`bash scripts/smoke-cli.sh`；linux-only 测试仅 GOOS=linux vet 编译核证并在报告中声明。
- **文档口径**（docs-source-of-truth）：行为变更只改 09 + README；01–03 冻结；测试计数变动同步 04 §3.1/§3.2/§3.3/§7 与 README「测试与回归」；版本 6 取值位/5 文件由 check-version-sync.sh 管。
- **待裁定项未拍板前，对应任务不得开工**（见下）。
- 实施时对"代理报告坐标"必须**现读复核**再动刀——本计划行号是 2026-09-24 现读快照，可能随批次内前序任务漂移。

## 待裁定清单（J 项，开工前需用户逐条拍板）

| # | 裁定点 | 选项 |
|---|---|---|
## 裁定清单（J 项，2026-09-24 用户已逐条拍板 — 全部取推荐项）

| # | 裁定点 | 裁定 |
|---|---|---|
| J-1 | **R-门禁-1 发布腿**：build.yml release 无 draft/prerelease，v* tag 即不可逆公开；本地躺错位 tag `v1.0.0`（指向 AppVersion=0.4.0-m4 旧提交，**未推远端**，远端只有 v0.5.0） | **(a)** release 加 `draft: true` + 删除本地 v1.0.0（删 tag 属破坏性 git 操作，执行前再确认一次） |
| J-2 | **R-门禁-2 发布门槛**：tag push 不触发 ci.yml，build.yml 回归面远弱于 CI | **(b)** 新增轻量校验腿：`gh api` 查 tag 所指 commit 的 ci.yml 最近 run 结论，非 success 即红（不在四腿重复全回归） |
| J-3 | **R-操作-3 reclaimed 口径**：FinalizeOp `SUM(size) WHERE done` 把 hardlink/symlink/同卷 move 的 size 全计入历史页"回收空间" | **(a)** 改 SUM 按 kind 过滤（只计 trash/delete/跨卷 move，对齐执行器 model.go:240-267 口径）。★ 裁定前现读补证：执行器侧 Reclaimed 早已 kind 化（同卷 move 不进、hardlink→LinkedBytes、symlink→SymlinkedBytes），oplog.go:106-118 注释"天然为零/设计如此"与执行器实现**直接矛盾**，属 M40 账本侧漏改而非"两处口径不同"。旧记录 reclaimed 会重算（注释的审计链担忧建立在假前提上，不予采纳）；同步改注释 + 09:672 + 04 划账 |
| J-4 | **低危批处置**（约 20 条，见 F 批清单） | **(b)** 挑选随批修（一行级无争议项），其余台账登记。随批修：R-根包-1（a.ctx 竞态）、R-扫描-2（队列内存）、R-算法-6、R-缓存-1、R-前端-3/4/6、R-门禁-7/8、R-操作-4；仅登记（动操作腿/需设计决策）：R-操作-5/6、R-算法-3/4/5、R-扫描-3 |
| J-5 | **R-根包-2 取消路径 failed**：扫描取消不写回取消前已积累的 ReadDir 失败清单 | **(b)** 维持丢弃 + 09:722 取消行补明文「取消后失败清单一并不提供（含取消前已读不了的目录）」+ 代码注释补理由 + 台账登记（取消=用户不要部分结果，failed 写回缺消费方） |
| J-6 | **R-门禁-5 fdd-cli 退出码**：failed 非空仍 exit 0 | **(a)** failed>0 → exit 3（区别于 0=全成、既有 1/2 错误码）；同步 smoke-cli.sh（绿路径 failed=0 不受影响，但断言需认 3）+ 09/README 的 CLI 段 |

## 审查结论总览

- **高危 1 条**（门禁域）：build.yml 发布腿——全仓唯一不可逆对外动作持最弱门槛且无人工闸。
- **中危 10 条**：算法域 2（Windows 硬链接假重复组、阶段 3 缓存写回身份嵌合体）、操作域 3（undoSymlink 备份无内容复核、XDG 跨卷孤儿副本、reclaimed 假账）、扫描域 1（DT_UNKNOWN 目录静默漏扫）、前端 2（filtered 文案假归因、快捷键漏模态门）、门禁域 2（发布门槛弱、smoke-cli 失败现场销毁）、CLI 1（fdd-cli cache/out 自指污染）。
- **低危约 20 条**：见 F 批。
- 五路代理一致结论：四轮修复后核心防线（身份复核、变长防护、取消链路、判据唯一性）成熟度高，本轮**未发现新的直接数据销毁路径**；风险重心在"跨阶段/跨平台一致性缝隙"与"发布/账目口径"。

---

## A 批：高危（J-1=(a)、J-2=(b) 已裁定，可开工）

### Task A1: build.yml 发布腿加人工闸 + 错位 tag 拆雷（R-门禁-1，已开码核实；J-1=(a)）

坐标：`.github/workflows/build.yml` release job（softprops/action-gh-release@v3，无 draft/prerelease 参数）；本地 `git tag -l` = v0.5.0 + v1.0.0，`ls-remote --tags` 仅 v0.5.0。（★ 本 workflow 已于 2026-09-24 因 R-门禁-9 改动过 Version Sync Check 位置，实施前现读 release job 行号。）

- [x] **Step 1（J-1=(a)）:** release 腿 `with:` 补 `draft: true`（人工在 GitHub 确认后发布），checksum 步保留。✅ Create Release `with` 现 = `{files, generate_release_notes, draft:true}`（ruby job-graph 核证）。
- [x] **Step 2（J-1=(a)，需用户单独授权）:** 删除本地错位 tag v1.0.0。✅ 用户 AskUserQuestion 确认后 `git tag -d v1.0.0`（was 09a4929）；本地 tag 现仅 v0.5.0；恢复命令 `git tag v1.0.0 09a4929`。
- [x] **Step 3:** YAML 语法核证。✅ 本机无 actionlint/pyyaml，用 `ruby -ryaml` 解析通过 + job-graph dump 核证；无本地 runner，真机验收登记入 05（下次 tag/dispatch 演练观察 draft Release + verify-ci 拦截）。

### Task A2: 发布门槛对齐 CI（R-门禁-2，代理报告+主代理 grep 核证；J-2=(b)）

坐标：`build.yml` build job（version-sync + `go test -count=1` + smoke-cli）vs `ci.yml:61-182`。

- [x] **Step 1（J-2=(b)）:** 新增校验 job `verify-ci`——`gh api repos/{repo}/actions/runs?head_sha={sha}` 取 `.name=="CI"` 最近 run 的 conclusion，非 success（或查无）即 `exit 1`。挂 `needs` 链最前：`build.needs=verify-ci`、`release.needs=build`（ruby 核证）。事件字段经 `env:` 传入 shell（不插值），遵 AS-K4。
- [x] **Step 2:** 不在四腿重复全回归（J-2 否掉 (a)）；`verify-ci` 用 `permissions: actions:read + contents:read`、`timeout-minutes:5`。dry-run 本机对 HEAD(73d0b73，CI run 36017945038 绿) 跑同款 jq → 返回 `success`，门槛逻辑得证。非 tag 触发（dispatch）下 `if:` 为 false → step 跳过 → job 绿 → 下游 build 照跑。

## B 批：后端中危（J-3=(a) 已裁定，B1–B6 全部可开工）

### Task B1: 分组前组内同身份互查（R-算法-1，已开码核实阶段 1.5 仅在 Resolved 时折并）

坐标：`internal/dedup/pipeline.go:383-408`（阶段 1.5）、阶段 2 已持有 `ids[i]`（fsid.FromFile）。Windows 上 ResolveKey 标准 open 与 C4 兜底全失败（独占锁文件）时返回未解析 Key → 同 inode 两路径双双入候选 → 内容逐字节同 → 假重复组、reclaimable 虚高。

- [x] **Step 1（RED）:** `TestCollapseSameIdentityUnresolvedKey`（collapse_identity_b1_test.go）——构造两条 Key.Resolved=false 但 ids 同 Dev+Ino 的候选，断言折并为 1 条、保留较短路径、ID 不被丢弃项覆盖（C3）。RED 得证（stub 下 len=2 want 1）。
- [x] **Step 2:** 抽出纯函数 `collapseSameIdentity(entries, ids)`（pipeline.go refilterUnique 旁），最终分组改为先按全量哈希聚合下标 `finalIdx`、再逐桶据 `ids[]` 折并后物化 `finalGroups`。**确信判据**：仅两侧 ids 均 Resolved 且 Dev+Ino 同才并（不含 ctime）；任一侧未解析保持独立——刻意不用 fsid.SameIdentity（其未解析即 true，用于分组会漏报）。配套 `TestCollapseSameIdentityConservative` 四子例钉住口径。
- [x] **Step 3（负控制）:** 把 `seen[k]; dup` 改反 → 主测试 + 三保守子例全红；已还原。
- [x] **Step 4:** 现读 ops 侧：identityCheck/identityStill/slotProvesHardlink/backupOwnershipStill 检测的是**操作中途 TOCTOU 顶替**，非"组内两成员同 inode"⇒ ops 无假硬链接组熔断，本折并是唯一防线（互补不重复：漏过也不销毁数据，危害止于结果集失真）。结论写入测试头注释。
- 验收：gofmt 净、go build ./... 净、go vet ./internal/dedup 净、`go test ./internal/dedup -count=1` 全绿（含既有 hardlink/group-order/paranoid 回归）。

### Task B2: 阶段 3 缓存写回前身份复核（R-算法-2，已开码核实）

坐标：`internal/dedup/pipeline.go:685-735`——pending 行把阶段 2 句柄身份（`ids[ci]` 的 Dev/Ino/CtimeNs）与阶段 3 新句柄的 full 哈希拼在一行；两次 open 间文件被顶替 → 缓存存下"旧身份+新内容"，日后旧文件回位且 size/mtime 未变时 Lookup 全过 → 假重复组（ops 前线能拦删除，但结果集已失真）。

- [x] **Step 1（RED）:** `TestPhase3WritebackIdentityMismatch`（phase3_identity_b2_test.go）——新增包级接缝 `phase3IdentityFn`（默认 fsid.FromFile，仿 ops fsidFromPathFn），测试注入"victim 阶段 3 身份≠阶段 2 记录"。断言 ① failed 含 victim（Stage=hash、Err 含「替换」）② 无组含 victim、len(groups)==0 ③ 缓存 WithFull==1（victim 全量不写回）。RED 得证。配套 `TestPhase3WritebackIdentityMatchStillGroups` 钉住身份一致时不误剔（1 组 2 文件、WithFull==2）。
- [x] **Step 2:** 阶段 3 `os.Open` 成功后、哈希前重取 `phase3IdentityFn(f)` 与 `ids[ci]` 比 `SameIdentity`；不一致 → `f.Close()` + `hashTargets[i]=nil` + failed「文件在扫描期间被替换（身份已变化），已跳过」+ continue（跳过 full 写回）。`ci := hashSlot[i]` 上移至 open 后。**fail-open 方向正确**：SameIdentity 任一侧未解析即 true ⇒ FAT/exFAT 不误剔（与 B1 分组折并的 fail-closed 刻意相反，各自威胁不同）。
- [x] **Step 3（负控制）:** `!cur.SameIdentity` 改反 → 两测试全红（match 腿 a/b/survivor 被误判"被替换"）；已还原。
- 验收：gofmt 净、build 净、vet 净、dedup 全绿。★ failed 新增"扫描期间被替换"类别：smoke 绿色路径无顶替 ⇒ failed==0 硬断言不受影响；09 文案同步入 E 批。

### Task B3: undoSymlink 备份内容复核（R-操作-1，已开码核实：防线②仅 Lstat 存在性）

坐标：`internal/ops/undo.go:456-490`——与 undoHardlink 防线②（全量 BLAKE3 对 it.Hash）不对称；`.fdd-old` 名被扫描忽略，顶替不可见，回撤会把顶替者当原文件恢复并拨回 mtime。

- [x] **Step 1（RED）:** `TestUndoSymlinkBackupContentRecheck`（undo_symlink_content_b3_test.go）——合法链接（dup→keep）+ 备份先写真原内容算出 it.Hash、再被第三方顶替；断言回撤被拦截（err 含「不符/顶替」）、链接仍是链接、备份内容未被改动。RED 得证。配套 `TestUndoSymlinkBackupZeroHashLegacyRestores` 钉住零值哈希 fail-open 照常还原。
- [x] **Step 2:** undo.go 防线②（备份存在性 Lstat）后补 ②b：`it.Hash != 零值` 时 `hashFile(backup)` 全量 BLAKE3 对 it.Hash，读不出或不符即拦截「链接与备份均未动」；零值跳过（与 undoHardlink 防线②/undoSourceCheck/alreadyRestored 同判据）。同步**改正函数 docstring**：旧注释宣称备份"内容天然正确"是错的（.fdd-old 被扫描忽略、可被顶替），现写明 ②b 与 undoHardlink 对称。
- [x] **Step 3（负控制）:** `h != it.Hash` 改反 → 内容复核测试红（顶替备份未被拦）；已还原。
- 验收：gofmt 净、build 净、vet 净、`go test ./internal/ops -count=1` 全绿（含既有 symlink-undo 真哈希路径回归）。

### Task B4: XDG 跨卷腿 removeSrc 失败的孤儿处置（R-操作-2，方向已核证 moveIntoTrash:118 裸返回错误；调用方 trashinfo 处理细节实施时现读）

坐标：`internal/ops/trash_xdg.go:100-120` + trashXDG:55-63——复制成功但删源失败 → trashinfo 被删 → Trash/files 留不可见孤儿副本；executor C6 逐文件重试会再造一份。

- [x] **Step 1（RED）:** `TestTrashXDGRemoveSrcFailNoOrphan`——removeSrc 注入失败，断言 dst 副本被清掉（归属可证明：本函数刚创建）或 trashinfo 被保留，二选一按 Step 2 裁定口径。✅ `internal/ops/trash_xdg_orphan_b4_test.go`；RED 已证（修前 files/ 残留 `victim.bin`）。裁定口径＝**清 dst**（非保留 trashinfo）：源仍在原处、字节未动，副本纯多余。断言五格：①源原封不动 ②files/ 空 ③info/ 空（与调用方回滚闭合）④错误含「源未动/无残留」且 %w 包出底层删源原因 ⑤dstMap 不收该项。
- [x] **Step 2:** 修复取向：removeSrc 失败时 `os.Remove(dst)`（与 copy 失败分支同形），错误文本说明"源未动、无残留"。✅ `trash_xdg.go` moveIntoTrash 末尾 `return removeSrc(src)` 改为 if err 分支：`os.Remove(dst)` + `fmt.Errorf("跨卷入回收站时删源失败（源未动、回收站无残留副本，原件仍在 %s）: %w", src, err)`。注释点明与紧邻 preRemoveRecheck 失败分支**取向相反**（被顶替留 dst／删源失败清 dst），区别在源的身份。变异负控制（摘掉 `os.Remove(dst)`）已证红。
- [x] **Step 3:** 现读 trashXDG 调用方对错误分支的 trashinfo 处置，确认与 Step 2 口径闭合；GOOS=linux vet 核证（本机跑不了 linux trash 实测，登记 05）。✅ 现读 trashXDG:55-63——错误非 errCopiedSrcSwapped ⇒ 删 trashinfo，与 Step 2 的 `os.Remove(dst)` 闭合（两份皆清、零孤儿）。gofmt/build/vet×3（darwin/windows/linux）全绿，ops 全套 `-race` 绿。真机层登记 05 LNX-2 第二条「可选加测」（chmod 500 父目录造真 EACCES；与 §31 守卫取向相反）。

### Task B5: DT_UNKNOWN 目录静默漏扫（R-扫描-1，已开码核实 scanner.go:505 `!IsRegular → continue` 无留痕）

坐标：`internal/scanner/scanner.go:478-510`——DirEntry 类型未知（FUSE/SMB/部分网络挂载）时目录走文件腿，被 `!info.Mode().IsRegular()` 静默 continue：整棵子树漏扫、不记 Failed、不计数，违背 M21"跳过必留痕"纪律。

- [x] **Step 1（RED）:** `TestScannerDirUnknownTypeNotSilentlySkipped`——注入 Type() 返回未知（fs.FileMode(0) DirEntry）的目录项，断言子树被遍历（或至少留痕计数）。✅ `internal/scanner/scanner_dtunknown_b5_test.go`；新增 `readDirFn` 接缝（同 guard/cloudCheck 手法）注入 `unknownTypeEntry{fs.DirEntry}`（Type()→0、Info() 委托真实项）。RED 已证（修前 `files=[]`，整棵 sub 子树无声消失）。断言：①inner.txt 被收到 ②sub 不记 Failed（漏扫是静默的，修后也不该凭空记失败）。
- [x] **Step 2:** 文件腿兜底处补 `info.IsDir()` 分支：按目录路径过门禁（guard/符号链接口径与正常目录腿一致）后 submit。✅ 关键发现：`de.Type()` 对**常规文件**也返回 0（`FileMode.Type()` 只留类型位），派发前无法区分 DT_UNKNOWN 与常规文件 ⇒ 兜底只能落在文件腿 `de.Info()` 之后（计划坐标正确）。为保「与正常目录腿**完全一致**」，把目录门禁（guard.Dir/escapedRoots/隐藏/ExcludeDir/ExcludeDirPath/去重/submit）抽成 worker 内闭包 `enterDir(full, de)`，正常腿与 DT_UNKNOWN 兜底腿共用（杜绝两处漂移）。兜底分支排在 cloudCheck/IsRegular **之前**。变异负控制（保留 IsDir 检测但摘掉 enterDir 路由）已证红。gofmt/build/vet×3 全绿，scanner 全套 `-race` 绿（enterDir 抽取未回归任何既有目录门禁测试）。
- [ ] **Step 3:** 补 09 手册"网络卷/FUSE 扫描"一句（若 J-5 同批则合并改）。⏸ **顺延 E 批**：计划本句即标注"若 J-5 同批则合并改"，而 J-5=(b) 的 09 补明文正在 E 批（docs sync）。为避免 09 编辑碎片化、与 E 批合并一次改。B 批只交付代码修复（用户指令"处理 B1–B6 的修复"）。

### Task B6: 历史页 reclaimed 口径（R-操作-3，已开码核实 planOpItems 全 kind 记 e.Size、oplog 注释与执行器实现互斥）——J-3=(a)

坐标：`internal/history/oplog.go:98-132`（FinalizeOp SUM）、`app.go:2563-2586`（planOpItems）、`frontend/src/views/RecordsView.vue:269`。执行器侧真值口径参照 `internal/model/model.go:240-267` + `executor.go:496-532/:736`（Reclaimed 只收 delete+跨卷 move；hardlink→LinkedBytes、symlink→SymlinkedBytes、trash→TrashedBytes、同卷 move 不进）。

> **裁定修正（本批实施时用户二次拍板）**：J-3 原字面「改 SUM 按 kind 过滤（只计 trash/delete/跨卷 move）」与执行器实现冲突两处——① 执行器把 **trash 记入 TrashedBytes 而非 Reclaimed**（M22），故 trash 不该进 reclaimed；② **跨卷 move 无法用纯 SQL kind 过滤表达**（`cross_vol` 是每条目运行期信息，op_items 未持久化该列）。经 AskUserQuestion，用户选择「**对齐执行器 res.Reclaimed**」：FinalizeOp 不再库内重算，改由调用方（app.go 收尾处）把执行器已算好的 `res.Reclaimed` 传入并原样落库。kind 口径的唯一真值仍在执行器（已由 M22/M40 执行器测试覆盖），账本侧只忠实持久化。副作用：trash/hardlink/symlink/同卷 move 的历史「回收空间」显示 0 —— 这是修正后的真话。

- [x] **Step 1（RED）:** `internal/history/oplog_b6_test.go`——`TestFinalizeOpPersistsCallerReclaimed`（trash 条目 size=1000，传 reclaimed=0 → 断言 Reclaimable==0，旧 SUM 会得 1000）+ `TestFinalizeOpPersistsNonZeroReclaimed`（delete，传 4096 ≠ SUM(6000) → 断言原样落 4096）。首跑因签名 1→2 参编译失败（RED 确证）。
- [x] **Step 2（对齐执行器）:** `FinalizeOp(opID int64)` → `FinalizeOp(opID int64, reclaimed uint64)`；SQL 由 `reclaimed = (SELECT SUM(size)...)` 改为 `reclaimed = ?`（done_count 仍库内 COUNT，无口径歧义）。`app.go:2505` 收尾改传 `hs.FinalizeOp(journalID, res.Reclaimed)`。8 处测试调用同步补参；`oplog_i6_test.go` seedExecutedOp 由 trash 改 delete（6000 与执行器口径自洽）、`app_undo_test.go:124` hardlink 断言由「Reclaimable!=0」翻正为「==0」。
- [x] **Step 3:** 重写 oplog.go:98-118 注释——删掉「hardlink/symlink 天然为零/两处口径不同是设计如此」的假前提，如实描述「reclaimed 由调用方传入、与执行器同口径；trash/链接类/同卷 move 显示 0 是真话」；保留「补链接类统计请新增列、勿动此处」的告诫。
- [x] **Step 4（负控制）:** 把 SQL 退回 `SUM(size)` 并 `_ = reclaimed` 忽略入参 → 两条 B6 测试均红（trash 得 1000、delete 得 6000），确证防线生效；随后还原。
- [ ] **Step 5:** 09:672「回收空间」措辞现读复核是否需补口径说明（trash/链接类/同卷 move 记 0）—— ⏸ **顺延 E 批**（docs sync），与 J-5=(b) 的 09 补明文、B5 顺延的网络卷/FUSE 句合并一次改，避免 09 编辑碎片化。旧记录 reclaimed 不自动迁移（FinalizeOp 是收尾写死快照，非读取时动态算）：历史记录一经 Finalize 即冻结，旧假账数字留存于既有记录、新操作起记真值——E1 划账说明。

## C 批：前端中危

### Task C1: ops:filtered 文案假归因（R-前端-1，已开码核实 app.go:2404 `filtered = ProcessDirs>0 || ExcludeDirs>0`）

坐标：`frontend/src/stores/scan.ts:951`——只配黑名单时文案仍说"M 项在优先文件夹内"，而界面根本不存在优先文件夹；ConfirmDialog proctip 已按两把过滤器分支，此处是同判据第二份文案实现且已漂移。

- [x] **Step 1（RED）:** node 用例——仅 excludeDirs 非空时收到 ops:filtered，断言文案不出现"优先文件夹"。（scan-filtered-wording.test.ts，2 用例绿）
- [x] **Step 2:** 收口为中性句「其中 M 项在本次处理范围内」（与 ResultView.vue:315/:464 现读字面对齐，判据唯一性纪律）。scan.ts ops:filtered 主句已改。

### Task C2: 全局快捷键补模态门（R-前端-2，已开码核实 App.vue:32 条件缺 pendingOpen/failedOpen）

坐标：`frontend/src/App.vue:32`——抽屉开着时 Space 被劫持、Cmd+A 在模态背后改全局勾选（拟处理抽屉是快照，画面与勾选脱钩）、Delete 静默清空勾选；confirmOpen 拦截的立项理由正是防误删，两抽屉同形漏网。

- [x] **Step 1（RED）:** App.vue 打不进 node --test（M116/M118 同族限制），改走接线锚：test-frontend-logic.sh 两条 `wiring_window 'src/App.vue' '!store.preview && !store.confirmOpen' 1 '!store.pendingOpen'/'!store.failedOpen'`（含变异反证：删锚字符串→红）。
- [x] **Step 2:** App.vue keydown 门条件补 `!store.pendingOpen && !store.failedOpen`（App.vue:36-37）。

### Task C3: 前端低危三连（R-前端-3/4/6，依 J-4）— 已完成（2026-09-24）

- [x] **R-前端-3:** busyReason 增第三支 `if (histLoading.value) return '历史结果载入中，请等待完成'`（scan.ts:~161）。node 用例 scan-busy-histloading.test.ts 3 项绿 + 变异反证（删 histLoading 支→3 红→还原）。
- [x] **R-前端-4:** 分界数学抽进 `utils/pending.ts` 纯函数 `firstExcludedIndex`（修 `idx<0` 整页排除段返 -1 → 现返 0 置顶标题），PendingDrawer.vue 改为消费。node 用例 pending-first-excluded.test.ts 6 项绿 + 变异反证（还原旧 `idx>=0&&...` 逻辑→1 红→复原）+ 接线锚（PendingDrawer→firstExcludedIndex）。
- [x] **R-前端-6:** scan.ts:30-33 注释如实化——两处**反向**换算点（startScan KB→字节、rescanHistory `Math.round(.../1024)` 字节→KB），删去"settings.filtersDefault 有消费者"的假陈述（实无消费者）。
- [x] **R-前端-5（复核→仅登记）:** 现读 Go 侧 StartScan 对文件 root 的实际行为：拖入的文件 root → ReadDir 得 ENOTDIR → 记为 `FailedItem{Stage:"scan"}`（**留痕**、非静默、无数据丢失）。与 F 批表 line 206 处置一致（本轮不改，仅台账登记），E1 手册补一句说明。

## D 批：门禁与 CLI

### Task D1: smoke-cli 失败现场保留（R-门禁-3，已开码核实：set -e + EXIT trap rm -rf 销毁 .err）

坐标：`scripts/smoke-cli.sh:27,34-36,50`。

- [x] **Step 1:** `cleanup()` 按退出码分流（rc=0 才 rm -rf，rc≠0 保留 $WORK + 打印路径 + ls）；`run()` 接住 fdd-cli 退出码，失败时 `cat` **整段** `.err`（旧版只 sed 第 1 行，且失败时那行根本轮不到执行）再 exit rc。
- [x] **Step 2:** `bash -n` 绿；本地跑一次 smoke 三跑一致绿路径不受影响（205 组 / 复扫命中 532 / OK）；构造人为失败（临时改坏 `run cold` 传 `-bogus-flag`）验证完整 stderr 可见 + $WORK 保留 + rc=2 传出，验完删除临时副本与保留目录。

### Task D2: fdd-cli cache/out 自指污染守卫（R-门禁-4，已开码核实 main.go:93-97 roots 校验后无守卫）— 已完成（2026-09-24）

坐标：`cmd/fdd-cli/main.go`——`-cache` 落在扫描根内时 cache.db/-wal/-shm 被当语料扫进 files_total；`-o` os.Create 截断任意既有路径（M222 已登记"并非零写操作"，本条是新危害面：自指污染会让 smoke 对账失真）。

- [x] **Step 1（RED）:** Go 测试 `cmd/fdd-cli/d2_selfref_test.go`——`TestEnclosingRootDetectsTargetInsideRoot`（8 分支：根内/深层子目录/out/同级/异树/多根命中第二/多根都不命中/空）+ `TestEnclosingRootRespectsSeparatorBoundary`（/tmp/bench vs /tmp/benchmark 前缀重叠负控制）+ `TestSelfRefGuardIsWiredBeforeRun`（源码锚：两守卫各 1 次且都在 p.Run 之前）。RED 得证（enclosingRoot undefined → 编译失败）。
- [x] **Step 2:** 新增纯函数 `enclosingRoot(target, roots)`（abs+Clean+pathnorm.Slash 归一 → pathnorm.Under 边界比较）+ `absKey` 辅助；main() 在 roots 校验后、cache.Open/p.Run 之前对 `-cache`/`-o` 各拦一次，命中即 `os.Exit(2)`，错误文本给出**两个路径**。行为验证：建二进制跑三例（cache 在根内 rc=2 / out 在根内 rc=2 / 冒烟同级摆位 rc=0 且 bench 内无 cache.db 泄漏）。变异反证：把 `Under(keyTarget,keyR)` 两参对调 → 5 子测红 → 复原。

### Task D3: 门禁低危四连（R-门禁-5/6/7/8）— 代码侧已完成（2026-09-24）；文档表留 E1

- [x] **R-门禁-5（J-6=(a)）:** fdd-cli `failed>0 → exit 3`（区别 0=全成、1=运行崩溃、2=用法/自指守卫错）。main() 末尾在报告编码**之后**判 `r.Stats.FilesFailed > 0` → closeOut+closeCache+os.Exit(3)。① smoke-cli.sh 的 `run()` 认 rc=3 为"有失败项、报告有效"→ 放行到 python 比对块由 `failed!=0` 断言判红（保留三跑互比诊断），rc=1/2 才就地 cat stderr 停；绿路径 failed=0→rc=0 不受影响。RED：`cmd/fdd-cli/d3_exitcode_test.go` 的 `TestCLIExitThreeOnFailedItems`（build+run，必失败语料=把普通文件当扫描根→ENOTDIR→FailedItem，断言 rc=3 + JSON failed 非空，不依赖权限）+ `TestExitThreeIsWiredAfterReport`（源码锚：FilesFailed>0 判据 + os.Exit(3) 在场且在 enc.Encode 之后）。RED 得证（rc=0 / 缺判据）→ 实现 → 绿。② **09/README 退出码表留 E1**（与手册 CLI 段一并写）。
- [x] **R-门禁-6:** build.yml 两处。① Package Artifact 的 `find|head -1`（多命中静默取首个）改为显式断言"恰好 1 个二进制"（0 或 >1 都 fail）；② release 的 checksum 步加 `shell: bash` + 点数断言：`FileDedup-*` 产物数必须 == 矩阵腿数（4），且 SHA256SUMS.txt 行数 == 4，缺腿即 `::error::` 退出。本机验证：YAML 经 ruby/psych 解析通过；两段 shell 逻辑独立跑验证（exactly-one：0/1/2 文件分别 not-found/ok/multiple；checksum：3/4/5 腿分别 exit1/OK/exit1）。★ runner 相关格无本机 runner，同 M120 约束待 tag/手动触发兑现。
- [x] **R-门禁-7:** benchgen 补 `TestSameSeedProducesByteIdenticalManifest`（cmd/benchgen/main_test.go）——同 seed 两次 `generate()` 的 manifest 经 `json.MarshalIndent` 后 `bytes.Equal` 逐字节比对（比既有 corpus-digest 更严：钉每组 Size/相对路径集合/Shapes）；含非空守卫（`"seed": 42` + 组数>0）。变异反证：generate() 末尾按 map 迭代序追加组 → 首处差异偏移 776、-count=3 中 2 红 → 复原 → -count=2 绿。
- [x] **R-门禁-8:** smoke-symlink-assert.sh:316 `for f in $scan_list`（不加引号词分割，REPO_ROOT 含空格时路径被撕碎→假红）改为 `while IFS= read -r f; do … done <<< "$scan_list"`（scan_list 本就是 `printf '%s\n'` 换行分隔）。验证：bash -n 绿 + 全脚本 18 断言 0 失败（E 组仍扫 8 文件）；独立对照证明含空格路径下旧写法解析出 0 个 -f、新写法解析出 2 个。

### Task D4: Version Sync Check 门禁编排修复（R-门禁-9，**已修 + 已提交推送 + CI 已验证，2026-09-24**）

实际爆红发现（非本轮审查查出，是 build.yml workflow_dispatch run 36013091891 三腿红倒逼）：该步原排在 Setup Go **之前**且四腿都跑，但脚本 B3/§7 真值通道要 `go list`——macOS runner 无预装 go（`command not found`）、windows 腿 Git Bash 下 go list 空 stdout（成因未钉死：模块按 GOOS 分别下载证明包图加载过，却无 stdout 无 stderr；需带 set -x 的复现腿才能诊断）、linux 腿靠 runner 预装 go 侥幸绿。

- [x] **Step 1:** 该步移到 Setup Go 之后（用 go-version-file 的 go，不赌 runner 预装），加 `if: matrix.platform == 'linux'` 只在 linux 腿跑一次（与 CLI Smoke 同款先例、口径对齐 ci.yml:72-73 ubuntu 单跑）；注释显式认领收窄理由（SKIP/收窄必认领纪律）。门禁语义不减：linux 是发布产物腿，tag 触发照样拦。
- [x] **Step 2:** 本机 `bash scripts/check-version-sync.sh` 全绿（A 组 6 取值位 0.5.0 + B 组 9 锚点 61 格）。
- [x] **Step 3:** 已提交推送（commit `73d0b73`）；workflow_dispatch run 36018021905 **四腿全 success**（linux 2m10s / macos-amd64 1m47s / macos-arm64 1m46s / windows 3m45s，release 腿正确跳过），push CI run 36017945038 亦绿。E1 划账时登记 M 编号。
- 遗留登记：windows Git Bash 下 go list 取空的成因未诊断（现已绕开——该脚本不再上 windows 腿）；若日后要在 windows CI 跑此脚本，先复现钉死。

## E 批：文档一致性 + 划账（收口批）

### Task E1: 文档同步

- [ ] 09 手册：B5（网络卷 DT_UNKNOWN 行为）、J-5 裁定结果、J-3 裁定的 reclaimed 口径句、D2 的 CLI 守卫行为。
- [ ] README：CLI 段如 J-6 改退出码则同步。
- [ ] 04 台账：本轮全部 M 编号登记（M223 起），§6.41 划账；§3.2 测试计数如变动四处同步（04 §3.1/§3.2/§3.3/§7 + README）。
- [ ] 05 真机清单：A1 tag 演练、B4 linux trash 实测、C 批 UI 现读。

### Task E2: 全量回归 + 汇报

- [ ] Global Constraints 回归口径全跑；报告声明本机已知缺口（linux-only ops 测试仅编译核证、GUI 手测项）。

## F 批：低危清单（J-4=(b) 挑选随批修；每条实施前先现读复核坐标）

**随批修（一行级/无行为争议）** —— 各配 RED 或断言，批末并入回归：

| # | 坐标 | 一句话 |
|---|---|---|
| R-算法-6 | pipeline.go:576-590 | 缓存命中采样不符时丢弃本轮已算 full 重算：直接采用 |
| R-操作-4 | executor.go:272 | Execute 不按 fid 去重，重复 id 使已成功腿被 last-wins 覆写为 failed：入口去重（对齐 planOpItems 的 seen） |
| R-扫描-2 | scanner.go:59 | 出队不释放底层数组，百万目录级内存峰值：置零或环形队列 |
| R-根包-1 | app_single_instance.go:66 vs app.go:351 | onSecondInstance 无同步读 a.ctx（linux 腿可早于 OnStartup）：atomic.Pointer 或经 a.mu |
| R-缓存-1 | cache.go:347-353 | UPSERT 无条件 `full=excluded.full` 可把有效 full 抹 NULL：比照 partial 加 CASE（性能向，方向安全） |
| R-前端-3/4/6 | 见 C3 | busyReason 并 histLoading / PendingDrawer 整页排除标题 / scan.ts:32 注释如实化（在 C3 内实施） |
| R-门禁-7/8 | 见 D3 | benchgen 同 seed 逐字节钉测 / smoke-symlink-assert.sh:316 引号分词（在 D3 内实施） |

**仅台账登记（动操作腿 or 需设计决策，本轮不改）**：

| # | 坐标 | 一句话 | 登记理由 |
|---|---|---|---|
| R-算法-3 | pathnorm.go:44 + filter.go:145-155 | ExcludeDirs 卷根键静默失效 | 需先定"卷根键"语义（拒绝 or 整卷），属设计决策 |
| R-算法-4 | hasher.go:297-302 | 大文件分段缓冲 ≈3GiB 峰值无背压 | 需内存自适应设计 + benchmark，非一行级 |
| R-算法-5 | filter.go:404-423 | 多 `**` 回溯无记忆化 O(n^k) | 需记忆化设计 + 性能验证 |
| R-操作-5 | move.go / trash_xdg.go / undo.go | 跨卷复制身份底片与内容不同句柄 | 动三条操作腿，风险面大，独立批处理 |
| R-操作-6 | executor.go:571-590 + trash_darwin.go | darwin 批量 trash 复核不紧贴 + OPS-7 注释失实 | 动操作腿 + 注释校正需连带核证"全仓唯一"断言 |
| R-扫描-3 | scanner.go:302-315 | covered 根符号链接无真身二次问 | 触及保护门禁边界，需专门测试设计 |
| R-根包-2 | app.go:765-777 | 取消路径不写回 failed | J-5=(b) 维持丢弃，仅文档（E1）+ 登记 |
| R-前端-5 | ScanView.vue:66-68 | OnFileDrop 文件当目录 | 先现读 Go 侧 StartScan 行为再定，归 C3 复核 |

## 执行顺序与回归节奏

1. **裁定已完成**（2026-09-24，J-1~J-6 全部取推荐项）：全部批次可开工。建议顺序 A（高危发布腿，纯 workflow 改动，风险最低）→ B（后端中危防线）→ C（前端）→ D（门禁/CLI）→ E（文档+划账收口）。B/C/D 无相互依赖，可按用户节奏分批。
2. **B 批**（B1→B2→B3→B4→B5→B6）：算法/操作防线，独立可交付，每条 RED→修→负控制；批末跑回归 1–2 项。B6 涉及 history 账目重算，改完单独跑 history 包全测 + smoke（reclaimed 不在 smoke 断言面，但要确认不回归）。
3. **C 批**：与 B 并行无冲突（前后端不交叠；C1 若涉 ResultView 字面注意与 04 §6.40 M220 现读对齐）。
4. **A/D 批**：CI/脚本改动，绿色路径本机可验的部分先验；tag 演练入 05 真机清单。A2 的校验 job 逻辑本机可用 `gh api` 干跑核证。
5. **E 批**：全部实施完成后统一划账 + 第十一次测试计数核对 + 全量回归 + 汇报（避免多次回填）。
6. 每批完成即向用户汇报，**不提交不推送**（提交推送另行授权，同 R-门禁-9 那次的流程）。

## 风险与边界（不写成的话）

- B1 的同身份互查若与既有"阶段 1.5 折并"叠加，注意保持组序三级键确定性（有专测钉）与 ID 引用稳定（C3 判据：指针原地替换不得覆盖 ID/Key）。
- B2 会给"扫描期间文件被替换"新增 failed 类别，failed 计数面变化需同步 09 与 smoke 断言（smoke 有 `failed==0` 硬断言，绿色路径不受影响，但要确认）。
- B4 的 linux 实腿本机不可测：GOOS=linux vet + CI linux runner 兜底，真机项入 05。
- B6（J-3=(a)）会重算历史记录 reclaimed：用户已在知情下拍板接受（旧数字虚高本就是缺陷，重算是修正而非破坏）；实施时确认 FinalizeOp 是"每次操作收尾时写死快照"还是"读取时动态算"——若是写死快照，旧记录不会自动变，需评估是否补一次性迁移（E1 划账说明）。
- A 批动 workflow 无法本机全验：改动保守化（只加参数/只加腿），下次 tag 演练收口。A1 Step 2 删 tag 属破坏性 git 操作，执行前单独再确认。
- C 批会动前端 node 用例数（现 55+接线 17，第四轮后口径实施时现读），E2 的计数核对放最后统一做。
