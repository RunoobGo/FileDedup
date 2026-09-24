# 2026-09-24 第五轮全面审查修订批 · 细化设计段

用户指令："全面审查项目代码以及文档，并进行测试" → 报告交付后 →"逐项执行和测试"。
本段是实施前的取证/判据/探针/变异/边界细化；发现编号 N-x 沿用当日审查报告，登记号拟占
**M211~M222**（现读登记表最大占用 M210；规矩：拟号不等于分配，分配在划账批复核之后）。

- 批次起点：HEAD = origin/main = `f638795`，工作区干净，单 worktree。
- 门禁基线（本段现取，`/tmp/gates-audit-202032.log`）：`rows=15 PASS=14 SKIP=1 FAIL=0`，
  src_test=837 / top_PASS=785 / run=926 / sub_PASS=136 / ok_pkgs=23 / 两 race 行 data_race=0 /
  前端 108+33=141。与 §6.39 收口格逐字相等 ⇒ **本批从全绿起跑**。
- 审查方式：五路只读子代理（ops 执行层 / 扫描核心 / 支撑包 / 前端桥接 / 文档面），
  每条发现由主代理亲手复核后才入台账（子代理坐标可信度纪律，见 04 §6.19 口径一）。

## 1. M211（N-1）darwin osascript argv 无 `--` 终止符 + rc=0 零输出记虚账

### 1.1 取证（全部本机实测，非推断）

- `internal/ops/trash_darwin.go:43-47`：`osascriptArgs` 产出 `[-e, script, paths...]`，paths 前无 `--`。
- 本机三格真值（脚本为无害的 `count of argv`，不触 Finder）：
  1. `osascript -e <s> "-dash.txt"` → `illegal option -- d`，rc=2 ⇒ 整批失败，
     以 `-` 开头的文件在 darwin **永久进不了回收站**，英文 usage 直透用户（executor.go:658 逐文件重试同样必败）；
  2. `osascript -e <s> -i < /dev/null` → **rc=0、stdout 空**（`-i` 是 osascript 唯一无参选项，handler 根本没跑）；
  3. `osascript -e <s> -- "-dash.txt" "n.txt"` → 输出 `2`，rc=0 ⇒ `--` 即修好。
- 危害放大点在 `internal/ops/executor.go:588-591`：`trash(paths)` 返回 `err==nil` 即把**全部** usable
  记 `ocOK`（dst 从 dstMap 取，取不到记空）⇒ 形状 2 下未移动的在盘文件被整批记 done + TrashedBytes 虚账，
  事后回撤报"回收站中的文件已不存在"。与在册 M51（darwin 批量失败丢 dstMap）是**不同失效形状**；
  grep `docs/04` `illegal option|终止符|以 - 开头` 零命中 ⇒ 未登记。
- 既有测试冲突格：`trash_darwin_test.go` `TestTrashScriptIsStatic` 硬断言 `len(args)==4` 且
  `args[2]` 即首个路径——本批把契约改为 `[-e, script, --, paths...]`，该断言**随契约改写**为
  "路径在 `--` 之后原样出现"，这不是"为变绿改断言"：判据本身变了，修前红由新探针提供（§1.2-1）。

### 1.2 修法与判据

1. `osascriptArgs(paths)` 拆为 `osascriptArgsFor(script string, paths []string) []string`
   （`-e script` 后、paths 前插 `--`；paths 为空时不插）；`osascriptArgs = osascriptArgsFor(trashScript, paths)`。
2. 防线（防"rc=0 但脚本没跑"这一整族，不止 `-i`）：`defaultTrash` 每批 parse 之后，
   `len(batch)>0 && len(本批解析出)==0` ⇒ 返回 `(已收 dst, error)`，error 点名"osascript 返回 0 但无任何成对输出
   （脚本未执行？）"。保持 C2 契约：已知 dst 不丢。判定逻辑抽纯函数 `batchOutputGap(parsed, batchLen int) bool`。
3. executor 侧**不改**：批量腿 err!=nil 走既有逐文件回退；逐文件腿同样经 2 的防线，
   `-i` 文件在逐文件腿会以 err 落 Failed（而不是 ocOK+空 dst）。

### 1.3 探针与变异

- P-211-a（修前真红，纯函数级）：`TestOsascriptArgsDashTerminator`——`osascriptArgsFor` 尚不存在时编译不过，
  故红由**改前树等价复刻**提供：直接断言现函数 `osascriptArgs(["-x.txt"])` 的产物含 `--` 段（改前红，改后绿）。
- P-211-b（修前真红，行为级，安全不触 Finder）：`TestOsascriptDashArgFidelity`——
  用 `osascriptArgsFor(countArgvScript, ["-i","-dash.txt","n.txt"])` 真跑 osascript，期望 stdout=="3"。
  改前形状（无 `--`）⇒ rc=2 红；改后 ⇒ 绿。darwin 专属（`//go:build darwin` 文件内）。
- P-211-c（防线判据）：`TestBatchOutputGap` 纯函数三格（0/N、N/N、0/0）。
  新函数无改前红 ⇒ 变异顶：M-211-1 把 `batchOutputGap` 改恒 false，P-211 族中依赖防线的用例必须红。
- 变异计划：M-211-2 删 `--`（P-211-a/b 双红）；M-211-1（上）。

### 1.4 边界

- CI macos 腿会跑 P-211-b：count 脚本不触 Finder/不碰文件 ⇒ 可安全上 CI。
- **不**改 batchTimeout/batchSize；**不**动 linux/windows 腿。
- 未兑现格：darwin 真 Finder 下"以 `-` 开头文件入站成功"仍需用户环境一次读数（05 MAC 格追加一条 MAC-6 判据）。

## 2. M212（N-2）隐藏目录内的具名根被宽根覆盖后整棵静默漏扫

### 2.1 取证

- `dedupeRoots`（scanner.go:655-746）把被宽根覆盖（`pathnorm.Under(fr, fk)`，注意 Under 含相等）的子根从
  `kept` 丢弃；`Scan` 只对 `cleaned` 预登记 visited（:314-316）并 submit（:548-549）。
- 遍历侧：隐藏规则 :435 `if !f.IncludeHidden && HasPrefix(name,".") { continue }` **排在**
  保护逃逸分支 :427-433 之后且不看具名根 ⇒ 两格：
  - **A 格（假警示）**：`T + T/.Spotlight-V100/inner`——逃逸分支记 `escapedRoots`（宣称"已脱离系统保护"），
    :435 照样剪枝 ⇒ 警示与事实相反；且与 04:1605 文档口径"则不剪枝并记入 UnprotectedRoots"矛盾。
  - **B 格（纯漏扫）**：`T + T/.cache/inner`——无任何留痕。
- 本机复现（`/tmp/fdd-cli-audit`，临时目录）：
  `T T/.hidden/inner`（inner 内 1 文件与 keep 同内容）→ `files_total=2, failed=0, protD=0, groups=0`；
  单指 `T/.hidden/inner` → `files_total=1` ⇒ **多加一个父根语义翻转**。
- grep `docs/04` `具名根.*隐藏|逃逸.*隐藏` 零命中；既有钉用例夹具全用非 dot 名（`lost+found`，
  scanner_guard_test.go:130）⇒ 结构性覆盖不到。M36（:275 注释"rawKeys 不折叠"）与本修不冲突：
  rawKeys 已含全部根，本修只是让"用户点名的根"真的被走。

### 2.2 修法与判据（联合语义）

口径：**"扫 cleaned 全集（隐藏/保护照常剪）" ∪ "每个被覆盖具名根各自作为根扫（根本身免一次剪枝）"**——
与"用户单独指名该根"时的行为逐字一致（根级 submit 不经隐藏/保护检查，后代照常受检）。

1. `dedupeRoots` 增第六返回值 `covered []string`：在丢弃分支里，仅当 `fr != fk`（**严格**被某 kept 覆盖）
   时记入；折叠键**相等**的"同树两拼写"拼写重复不算 covered（M66 防重扫语义保持原样）。
2. `Scan`：`walkRoots := cleaned ∪ covered`（顺序：cleaned 在前，covered 按 all 原序，确定性）。
   visited 预登记、根 submit、`startUnprot` 前置判据循环三处一律改走 `walkRoots`。
   `prefixes`/`rootVerdicts`/`scanRoots` **不动**（covered 根的文件 relativeTo 落宽根前缀，同卷同 verdict；
   covered 根自身免检保护，靠 startUnprot 记警示——循环抬到 walkRoots 后 A 格的警示变真）。
3. visited 预登记保证宽根遍历与 covered 根遍历互不重跑（出队处 :455-464 的 seen 判定已在）。

### 2.3 探针与变异

- P-212-a（修前真红，B 格）：`TestCoveredHiddenRootIsScanned`——三根夹具（keep.bin、.hidden/inner/dup、
  .hidden/sibling/only4sibling）+ roots [T, T/.hidden/inner]：断言 inner 的文件**在**、
  `.hidden/sibling` 的文件**不在**（负控制：联合语义不是"放弃全部隐藏剪枝"）、visited 无重复计数。
  改前：inner 文件缺 ⇒ 红在真值格。
- P-212-b（修前真红，A 格，darwin 门）：roots [T, T/.Spotlight-V100/inner] ⇒
  断言 `UnprotectedRoots` 含 `T/.Spotlight-V100/inner` **且** inner 文件被扫到（警示与事实同真）。
  改前：警示有、文件无 ⇒ 第二格红。前提自检：`guard.Platform()==PlatformDarwin`，否则 `t.Fatalf`
  点名"该平台清单无此条"（不 Skip 成静默；按 §6.24 规矩 2 用平台判据而非卷判据——这是清单内容差异，不是卷差异）。
  ★ 当日细化：linux CI 腿必须能跑过该条 ⇒ 用 `runtime.GOOS=="linux"` 下真实存在的 dot 型 dirName 条目？
  现读 `sysguard.go` 表内 linux 位无点开头条目 ⇒ 该条**只能 darwin 兑现**，本机 darwin 有读数、
  CI macos 腿有读数，linux/windows 腿不出现该用例文件（build tag `darwin`），如实记"两腿无此格"。
- 变异：M-212-1 covered 恒 nil（P-212-a/b 红）；M-212-2 把 covered 判定改成 `Under`（含相等）后
  对同树两拼写也 submit ⇒ 配一条"折叠相等拼写不双扫"用例（夹具用大小写两拼写 + 卷前提自检，
  构造不出不敏感卷时按既有 `fscase` 惯例硬红点名——本机 APFS 大小写不敏感默认开，可兑现）。

### 2.4 边界

- **不**改 `IncludeHidden=true` 行为；**不**动 ExcludeDirs/黑名单语义（M191 内核不受影响）。
- covered 根同时命中 ExcludePaths glob 时：**根免检维持现状**（用户指名 > glob），与本修正交，不新增通道。
- CLI 冒烟（smoke-cli.sh）判据不涉及多根，不漂。

## 3. M213（N-3）dbfile.Quarantine 静默吞 -wal/-shm 改名失败

### 3.1 取证

- `internal/dbfile/dbfile.go:67-71`：`_ = os.Rename(path+suffix, quarantined+suffix)`，
  注释"失败不影响主影像已隔离的事实"；主文件改名成功即返回 nil ⇒ 调用方（cache.go:137-145 /
  history.go:189-198）立即在**原路径**重建新库。
- 危害链：SQLite 的 -wal 只按"同名库"关联，头内无库指纹 ⇒ 原路径留下的旧 -wal 会被**新库**照常恢复/
  撕裂；他进程持锁（fdd-cli 与 GUI 并存是在册场景）时 Windows 上侧文件改名必失败。
  隔离从"保全影像"退化为"复制损坏影像 + 埋活雷"。
- 在册核对：04:39/:66 只把"跟随 -wal/-shm"当**已发生**陈述；M87 是运行期不自愈，非本格 ⇒ 未登记。

### 3.2 修法与判据

1. 包级接缝 `var renameFile = os.Rename`（惯例同 cache.evictFn / scanner.probeCaseVerdict）。
2. `Quarantine`：主文件改名 → 侧文件逐个改名；**任一**侧文件失败 ⇒
   a) 尽力把已改名的主文件（及已成功的侧文件）**回滚**到原路径；
   b) 返回点名"哪个侧文件、两个错（改名错 + 回滚错若有）"的 error；调用方现有"失败即放弃重建"分支天然接住。
   c) 回滚本身失败时，error 明写"主影像已隔离且无法回原位，勿在原路径重建"——绝不静默。
3. 判据不是"尽量成功"而是"**要么完整隔离、要么原地不动**"：返回 nil 时 `-wal/-shm` 必与主文件同侧。

### 3.3 探针与变异

- P-213-a（修前真红）：`TestQuarantineSideFailureRollsBackAndErrors`——造 `db` + 假 `-wal`，
  注入"仅 -wal 改名失败"：断言 err!=nil 且 `db` 回到原位、`.broken-*` 主文件不存在。改前 err==nil ⇒ 红。
- P-213-b（负控制）：无侧文件 ⇒ 行为与改前一致（err==nil，主文件隔离）。
- 变异：M-213-1 把回滚腿删掉 ⇒ P-213-a 的回原位格红；M-213-2 `renameFile` 失败仍吞 ⇒ 首格红。
- 消费腿回归：cache/history 在隔离失败路径上的既有行为（不重建）由既有 M74 用例顶住，本批**不改**它们；
  抽一条 cache 级用例证明"Quarantine err ⇒ 句柄保持停用、不新建空库顶掉旧影像"。

### 3.4 边界

- Windows 真机"另一进程锁 wal 致改名失败"那格本机造不出 ⇒ 报"代码已改、该格未兑现"，
  挂 05 W 腿新格（W12-1）。
- 回滚是尽力而为：回滚失败时**不**再试图"删除新库"或"删 wal"（只改名不删除的包级纪律不破）。

## 4. M214（P2）M202 返回腿残余：四条 RPC 直通英文错误 + cache 三触点缺停用位

### 4.1 取证

- 事件通道已被 M202 的中文外壳收编（app_error_shell.go），**RPC 返回面**在册只豁免了事件通道与
  FailedItem 抽屉（§6.34 边界），未点名返回腿。现读四处：
  `app.go:1219`（PreviewFile os.Open err 原样 return）、`:1233`（io.ReadFull err）、
  `:1532`（SaveSettings os.WriteFile err）、`:2946`（CacheClear 透传 `DELETE` 原始错误）；
  前端 `toast.ts:63-71 errText` 原样转字符串、`scan.ts:1090-1093` 直入 toast。
- cache 停用位不齐：`Lookup/Store/…`（:277/:330/:388）都有 `corrupt` 闸，
  `Clear/GetStats/LastHit` 没有 ⇒ 停用后每轮 UI 拉统计仍发 SQL、每轮拿原始错误。

### 4.2 修法与判据

1. `app_error_shell.go` 新增纯函数 `shellRPCError(err error) error`：
   `errorShell` 得 (shell, raw)；shell==raw（B 档自撰中文/A 档已中文）⇒ 原样；
   否则返回 `fmt.Errorf("%s（系统原文：%s）", shell, raw)`——与 09 §"错误文案"已立的"系统原文"话术同形。
2. 上列四处（含 CacheStats 透传腿）包一层 `shellRPCError`；不改 errorEvent 腿。
3. `cache.Clear`：`corrupt ⇒ ErrCorruptDisabled`（哨兵句本就是完整中文，M74 原样上报即可）；
   `GetStats`：`corrupt ⇒ 零值+nil`（不发 SQL；注释点名"停用期的统计是说谎的零，不是空库"）；
   `LastHit`：`corrupt ⇒ 0,false`。

### 4.3 探针与变异

- P-214-a（修前真红，cache 包）：内部测试置 `corrupt` ⇒ `Clear()` 必须返回 `ErrCorruptDisabled`、
  `GetStats()` 必须 err==nil 且不再触库（用已 close 的 db 句柄造"再发 SQL 必炸"的现场——改前 SQL 腿红在错）。
- P-214-b（shellRPCError 为**新函数** ⇒ 无改前红，按 §6.23 规矩由变异顶）：
  变异 = 恒 `return err`（不包裹）⇒ 依赖它的上层用例红；变异还原用 `cp`+`cmp`。
- P-214-c（端到端一格）：app 包 `TestSaveSettingsErrorIsShelled`——把配置目录指到不可写处
  （现读 `settingsPath()` 是否已有注入缝；无缝则经 `t.TempDir` + chmod 只读造 ENOENT 不可行 ⇒
  按"登记给的修法可被真读数否决"规矩换路：直接对 `shellRPCError(os.Open("/nonexistent/x"))`
  断言"中文壳 +（系统原文：…no such file…）"两段俱在，端到端格如实记**未兑现**）。
- 前端 `errText` **不改**（后端已壳化；前端兜底属另一格，维持在册裁定面）。

## 5. M215（P2）scan.ts:185 `getStatus().then` 无 `.catch`

- 取证：`refreshStatus` 从 `scan:stage`(:1008)/`app:ready`(:1043 邻域) 事件回调里调用；
  同文件 :276 自家规矩注释明写"不能让 rejection 逃逸"。`GetStatus` 无 error 返回，危害=传输层 reject 时
  unhandled rejection。修法：`.catch(() => {})` 与 :1043/:1044 两处既有形状同构。
- 判据：接线锚点（test-frontend-logic.sh 静态 grep：`getStatus().then` 命中行 5 行窗口内须见 `.catch`）——
  改前该锚红（等价替换+锚点口径，按 §6.19 四不得称行为级红）。接线计数 33→34，B 组前端格随划账更新。

## 6. M216（P2）sysguard 条目侧归一：注释声称 ≠ 实现，latent fail-open

- 取证：`sysguard.go:307-308` 包尾注释声称条目侧统一走 `Slash`+`TrimTailKeepRoot`；
  `New()`（:195-212）只对 prefix/suffix 折小写，`eAbsPath.name` 原样入桶；归一只发生在查询侧（`Dir` :227-228）。
  现读内置表 15 条 absPath 全为干净 POSIX 形 ⇒ 今日无实害，但按注释形状登记 `"/dev/"` 或反斜杠形即静默失配。
- 修法：`New()` 对 `kind==eAbsPath` 执行 `e.name = pathnorm.TrimTailKeepRoot(pathnorm.Slash(e.name, "\\"))`；
  配两条用例：
  P-216-a（修前真红）：临时往 `table` append 一条 `name:"/audit-probe-dir/"`（全平台位）⇒
  `Dir("/audit-probe-dir/sub","sub").Skip` 必须 true；改前红。
  P-216-b（自检锚）：遍历内置表断言每条 absPath 名 `==` 其归一值（防"注释又跑回实现前面"）。
- 边界：**不**动 M84 裁定过的 `/private` 八条后代条目内容（归一是幂等的，干净形不变 ⇒ 行为零变化）。

## 7. 文档面（M217~M222）

| 号 | 落点 | 修法（原文一字不删，改处带〔2026-09-24 第五批复核〕标记） |
|----|------|----|
| M217 | docs/10 §3.8 邻域 | 补三条用户口径（Win/Linux 静默放行两腿、**不跨账户**边界、macOS 临时目录不可写⇒静默不启动 + FAQ 指针），消除与 10:6-7 自述的矛盾 |
| M218 | docs/04:80 | "绑定方法 34 个"→ 现读 **37**（该行自带 grep 命令锚，随批再复算一次，以复算值落笔） |
| M219 | docs/04:1525（M46 行）/M206 行 | M46"提交进仓库"补**当日更正**（`.gitignore:16` 起效后 wailsjs 从未被跟踪，`git log --all -- frontend/wailsjs` 零提交；:80 行才是对的）；M206/M46 各加"五条缺载已随 09-24 重生成闭合（37/37 对账 + `TestBackendAPIMatchesGoExportedMethods` 绿），仅'无静态锚'取向仍待裁"注记；连带把"锚的对象不在 CI checkout 内"写进 M206 修法行 |
| M220 | docs/09 §6.2.1 两格 | "在优先文件夹内/不在优先文件夹内" → 与 UI 字面一致："在处理范围内 / 不在本次处理范围内"（`ResultView.vue:315`、`ConfirmDialog.vue:147` 现读），并加"10 §2.6 本来就是对的"互指 |
| M221 | docs/09:710 | 删除与 :709 逐字重复的规则行（diff 亲验相等；纯 duplication，非划账史文） |
| M222 | docs/04:6575（§6.36 五） | "只读/唯一命中"补**当日更正**：`cmd/fdd-cli/main.go:189` 有 `os.Create(*out)` 写报告文件；结论（不吃 ops.Execute ⇒ 不加锁）不改 |

## 8. 交付节奏与验证面

- 三提交节奏：**设计段（本文件）单独一笔** → 实施按 M211/M212/M213/M214+M215+M216 各一笔
  （每笔含探针用例）→ 文档批一笔 → 划账一笔（登记表 M211~M222 占号落定、§6.40、
  B 组八格/前端格/逐包行随现读更新、05 追加 MAC-6/W12-1 两格）。
- 每笔实施后跑受影响包 + 全量 `go vet` 三平台；交付态跑全套 `scripts/run-gates.sh`，
  stdout 重进带时间戳新文件（不读旧报告）；race 两行必须 data_race=0。
- 变异清单：M-211-1/2、M-212-1/2、M-213-1/2、M-214(恒等返回)、M-215(锚改写)、M-216(去掉归一)——
  逐格红证原文进划账。
- 收口 = 提交 → push → `gh run view --json status,conclusion` 有界轮询到三腿绿；
  红了走复批。未兑现格（darwin 真 Finder、Windows 锁 wal、端到端 SaveSettings）在划账明写
  "代码已改、验证未兑现"，**不记通过**。
