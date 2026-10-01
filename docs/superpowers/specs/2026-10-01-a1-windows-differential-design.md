# A1 细化设计段：把 windows 腿的差分实数取出来，销掉 `docs/05` §0.2 A1 那一格

日期：2026-10-01　依据：用户「这些任务是否完成：两件事是把 grep 截断那格补实（属 ci-go-test.sh 小改）与真的执行一次 05 的 A1 差分——要不要占号、动哪格，等你定。」
本轮范围：**一项**——`docs/05` §0.2 **A1** 那一格的"差分读数"。**零代码改动、零门禁脚本改动、不占新号**。

---

## 0. 这一项的字面边界（先把"占号与否"这个我该定的事定死）

用户把裁定权交给我（「要不要占号、动哪格，等你定」）。裁定：

1. **不占新号。** 理由：A1 不是缺陷，是**一份清单里一格待办**。号表（M-系）记的是"发现过的缺陷及其兑现状态"；把"做掉一件已登记的待办"再开一个号，等于给同一件事记两遍账，且会让"下一自由号"的语义从"新发现的缺口数"退化成"干过的活数"。⇒ 本批只**覆盖既有行的状态**（M100 的"② 仍无读数"），不开新行。
2. **不占号 ≠ 不记账。** 兑现动作本身要落到三处：`docs/05:28`（A1 格销格）、`docs/04:3178`（M100 行 append-only 追记）、`docs/04` 新增 **§6.73**（划账）。
3. **"12 条"这个话术不精确，就地更正，不登记为缺陷。** 现读：`docs/05` A1 原文写"直接产出 V8b、`fsid_path`、`trash_windows_recycle` 等 **12 条**『绿但不知是否真跑』用例的差分"。实测这个 12 是**文件数**（12 个 windows-only 测试文件），不是用例数（同一批文件里有 **52** 个 `Test` 函数），且 `fsid_path_test.go` **不在这个面里**（它没有 `//go:build windows`、文件名也不带 `_windows` 后缀 ⇒ 跨平台文件，靠运行期 `runtime.GOOS` 分流）。⇒ 属"活文档话术与取证坐标收归"那一族（同 §6.70 的 E 组），按本仓规矩改口不改号。
4. **不碰的**：M405（`run-gates.sh` 行 6 留住 SKIP 名单）、M401/M402、M177/M334、M120、A2、A3——一律不在本批。M405 的动机（本机 harness 只报数不报名单）与本批**无关**：本批的取数走的是 runner 日志，不是 `run-gates.sh`。

---

## 1. 取数口径（四条命令，全部可照抄复取）

### 1.1 windows-only 面 = 12 个测试文件

```
find internal -name '*_test.go' -print0 | xargs -0 grep -l '^//go:build windows'
```

⇒ 12 行。**两种口径的并集也是 12**（`find internal cmd -name '*_windows_test.go'` 得 8 个，全部落在这 12 之内）⇒ 这一格不存在"漏了只在文件名上 windows-only 的文件"这种缺口。逐文件（含每文件的 `Test` 函数数与 `t.Skip` **文本**命中次数）：

| 文件 | cases | `t.Skip` 文本命中 |
|---|---|---|
| `internal/ads/ads_windows_test.go` | 2 | 2 |
| `internal/fsid/fsid_ctime_m271_test.go` | 2 | 4 |
| `internal/fsid/fsid_windows_test.go` | 6 | 5 |
| `internal/ops/crossdevice_windows_test.go` | 4 | 0 |
| `internal/ops/hardlinkcause_windows_test.go` | 4 | 0 |
| `internal/ops/shfileop_windows_test.go` | 1 | 0 |
| `internal/ops/trash_windows_list_test.go` | 5 | 0 |
| `internal/ops/trash_windows_m270_test.go` | 7 | 1 |
| `internal/ops/trash_windows_recycle_test.go` | 12 | 6 |
| `internal/ops/trash_windows_test.go` | 4 | 0 |
| `internal/ops/verify_identity_windows_test.go` | 2 | 1 |
| `internal/realbytes/realbytes_reported_windows_test.go` | 3 | 0 |
| **合计** | **52** | **19** |

★ 19 处文本命中 ≠ 19 个用例：一个用例可以有多条 Skip 出口（`ads_windows_test.go` 的 2 处**全在** `TestDetectsRealNamedStreamOnNTFS` 一个函数里），也有命中落在 `Test` 函数体之外（helper／`if runtime.GOOS` 分支）。⇒ 判"哪些用例**能** Skip"必须按**函数体**归属，不能按文件计数。

### 1.2 分类器只是**辅助口径**，判据不建立在它身上（本批一条自纠，见 §1.2b）

按函数体归属把 52 条分成"13 条带 Skip 出口／39 条不带"（一次性 awk 落在未跟踪的 `build/m7scratch/a1_enum.awk`）。★ **这一格设计段初稿把它当判据用，是错的**，见 §1.2b。

### 1.2b 分类器**漏 helper 里的 Skip** ⇒ 它不能当判据（本段自纠，落笔前撞出来的）

`internal/ops/trash_windows_m270_test.go` 里 `t.Skip` 文本命中 = 1，但那唯一一处住在 helper：

```
28:func m265LongTempPath(t *testing.T, units int) string {
33:    t.Skipf("临时目录本身已经很长（%d 码元），造不出 %d 码元的对照夹具", …)
41:    long := m265LongTempPath(t, 260)     ← TestM265WindowsPreflightRejectsOverlongPath 调用
54:    short := m265LongTempPath(t, 259)    ← TestM265PreflightIsWiredIntoRecyclableReason 调用
```

⇒ 这两条用例**能** Skip，v2 分类器却把它们判成 clean（它只扫 `func Test` 自己的体内文字）。所以"13 条 SKIPABLE"是个**下界**、"39 条 clean"**不是"结构上不可能 Skip"的证明**。⇒ 本批的差分一律改成**不依赖源码分类**的形状（§1.4）：名单齐了之后，"跑没跑"直接从名单读，不需要预判"谁能 Skip"。
★ 顺带保留的一条自纠：v1 更差——它按"最后见到的用例名"归属，把 helper 的 Skip 记到**前面那个** `Test` 上（`TestBuildPathListRoundTripsUTF16` 被这样错标，而它自己体内零命中）；v2 限定函数体后修掉了错标，但**引入了漏标**。⇒ 教训：**用源码分类当判据，两头都会错（错标＋漏标）；能用运行期名单就不该用分类。**

### 1.3 windows 腿 SKIP 名单（29 个唯一名 = 28 顶层 + 1 子测试）

```
gh run view 36847449767 --log --job 110321125757 \
  | grep -oE -- '--- SKIP: [^ ]+' | sed -E 's/--- SKIP: //; s|/.*||' | sort -u
```

同一条腿的 READOUT 行（`M404` 之后的形状，名单与计数同一把尺）：

```
M331_READOUT rc=0 pkgs_ok=23 skip_top=28 skip_sub=1 roster_top=28 roster_sub=1 fail_top=0 fail_sub=0
```

### 1.4 交集与差集（差分的本体，**不依赖 §1.2 的分类**）

```
grep -hoE '^func (Test|Benchmark)[A-Za-z0-9_]+' $(cat <12 文件清单>) | awk '{print $2}' | sort > /tmp/a1_all52.txt
comm -12 /tmp/a1_all52.txt <29 条名单>      # 面内实际 Skip
comm -23 /tmp/a1_all52.txt <29 条名单> | wc -l   # 面内真跑且绿
comm -13 /tmp/a1_all52.txt <29 条名单> | wc -l   # 面外 Skip（跨平台文件的运行期分流）
```

- 52 条 ∩ 29 条名单 = **1 条**：`TestQueryRecycleBinSystemDrive`（`ops/trash_windows_recycle_test.go`）。
- ⇒ 面内 **51 条真跑且绿**：它们所在的四个包（`internal/ads`／`internal/fsid`／`internal/ops`／`internal/realbytes`）在同一条腿日志里**逐包报 `ok`**（`ok` 行 23 枚 ≡ `pkgs_ok=23`），且 `fail_top=0 fail_sub=0`、`rc=0`。
- 名单 − 52 = **28 条**，按定义就是"不在这 12 个文件里的用例"（跨平台文件靠运行期分流）。★ 本批**不逐枚归因这 28 条**（它们与 A1 无关），只抽查两枚确认形状：`TestFromPathRegularFileMatchesHandle`（`fsid_path_test.go:139` 的 `runtime.GOOS == "windows"` 分流）、`TestMoveFileCrossDeviceReal`（§6.71 P-61-b 在册六条里的跨卷那一族，windows runner 上同样没第二卷）。
- 自洽式：**29 = 1（面内）+ 28（面外）**，且 **51 + 1 = 52**。⇒ 这两个等式就是本批的差分，任何一格不成立差分不成立。

**判据链（写死形状）**：`-v` 日志里一条用例只有 PASS／SKIP／FAIL 三种终局。⇒ **包报 `ok` ＋ `fail_* = 0` ＋ 名字不在完整 SKIP 名单 ⇒ 该用例真跑且绿**。★ 这条链**不需要**预判"谁能 Skip"，所以 §1.2b 那两类错标／漏标都伤不到它；它同时是 §6.21 七 那条"结构上不可能 Skip"规则的**替代**——以后不必再读源码断言没有 Skip 分支。

面内 52 条不逐枚点名（名单只有 1 条命中，逐枚等于抄 52 行）；只点 `docs/05` A1 原句**亲自命名过的三处**，让"那格的待办被这一跑答上了"可当面验：

| `docs/05` A1 原句命名 | 现读 | windows 腿判定 |
|---|---|---|
| **V8b** | `TestDetectsRealNamedStreamOnNTFS`（`ads/ads_windows_test.go:49`，两条 Skip 出口都在它体内） | **跑且绿**（不在名单，`ok filededup/internal/ads`） |
| **`trash_windows_recycle`** | 该文件 12 条用例 | **11 条跑且绿** ＋ 1 条 Skip（`TestQueryRecycleBinSystemDrive`，原因见 §2·3：这台 runner 缺 Shell 回收站能力） |
| **`fsid_path`** | `fsid_path_test.go` 4 条用例，**不在这个面里**（跨平台文件，0 处 `go:build`） | 3 条跑且绿 ＋ 1 条**按设计**在 windows Skip（`TestFromPathRegularFileMatchesHandle`，`:139` 文案"Windows 的 Stat 产物不带卷号/索引"） |

附：§1.2 辅助口径的 13 条（★ ＝ 本批点名要兑现的那一枚 V8b）。**只作对照、不进判据**——这 13 条是下界（§1.2b 的漏标），且"跑且绿"这一列的成立与它无关，成立与否只看上面的两个自洽式。

| 用例 | 文件 | windows 腿判定 |
|---|---|---|
| ★ `TestDetectsRealNamedStreamOnNTFS` | `ads/ads_windows_test.go` | **跑且绿**（不在名单，`ok internal/ads`） |
| `TestWindowsChangeTimeAdvancesOnWrite` | `fsid/fsid_windows_test.go` | 跑且绿 |
| `TestWindowsDistinctFilesDiffer` | `fsid/fsid_windows_test.go` | 跑且绿 |
| `TestWindowsFromFileResolves` | `fsid/fsid_windows_test.go` | 跑且绿 |
| `TestWindowsIdentitySurvivesRename` | `fsid/fsid_windows_test.go` | 跑且绿 |
| `TestWindowsChangeTimeIsResolvedAndPlausible` | `fsid/fsid_ctime_m271_test.go` | 跑且绿 |
| `TestWindowsChangeTimeMatchesBasicInfo` | `fsid/fsid_ctime_m271_test.go` | 跑且绿 |
| `TestRecycleBinCapBytesSystemDrive` | `ops/trash_windows_recycle_test.go` | 跑且绿 |
| `TestRegistryGetDWORDRejectsWrongType` | `ops/trash_windows_recycle_test.go` | 跑且绿 |
| `TestVolumeGUIDShape` | `ops/trash_windows_recycle_test.go` | 跑且绿 |
| `TestWinNukeStatusOfDistinguishesAbsentFromUnreadable` | `ops/trash_windows_recycle_test.go` | 跑且绿 |
| `TestVerifyFileIdentityCatchesRenameSwap` | `ops/verify_identity_windows_test.go` | 跑且绿 |
| `TestQueryRecycleBinSystemDrive` | `ops/trash_windows_recycle_test.go` | **Skip**（在册名单里；文案见 §2·3 ⇒ 这台 runner 缺 Shell 回收站能力）⇒ 这一格**不算兑现**，归 `docs/05` 的 W 系列真机面 |

---

## 2. 兑现边界（写在前头）

1. **用哪一跑的读数、以及为什么可以用**：本批取的是 run `36847449767`（HEAD `39c0363`，即 §6.72 那批自己触发的那一跑）。⇒ 按 §6.21 七 的回填边界，"本批自己触发的 run 不回写"里的"本批"指**§6.72**；从本批（§6.73）看它是**上一批**那一跑，与 §6.72 引用 run `36826343884`（§6.71 那批）同构 ⇒ 可以写进账。★ 本批自己推送后那一跑的读数同样**只报给用户、不回写 §6.73**。
2. **两跑互验**：§6.72 用的是 run `36826343884`（名单被二进制判定截断，只剩 10 条打印名，完整名单靠 `--- SKIP:` 从 `go test` 自己的输出段复原）；本批用的是修复后的 `36847449767`（`roster_top=28 ≡ skip_top=28`，脚本自报完整）。两跑的唯一名集合**逐字相同**（29 条，差别只在子测试那条的截取形状）⇒ 差分不依赖某一跑的截断形状。
3. **本批证明的是"跑没跑、绿没绿"，不是"断言对不对"**：`TestQueryRecycleBinSystemDrive` 这一条 Skip **不是缺陷**。★〔本段初稿写的原因是"它要真系统盘的回收站"，那是**我编的**——现读同一份日志就有它自己的文案：`trash_windows_recycle_test.go:117: SHQueryRecycleBinW(C:\) 调用失败（CI 沙箱可能禁用了 Shell API），跳过`，同日志 `:312`/`:314` 另有 `app_m294_fakebin_test.go` 两处报"Windows 的 `SHQueryRecycleBinW` 在本机不可用"⇒ 真实形状是**这台 runner 缺 Shell 回收站能力**，与本仓 05 里"回收站面 W 系列"未兑现的原因同一根。⇒ 划账 §6.73〕本批不替它编兑现；`docs/05` 的 W 系列（真机面）照旧留着。
4. **`docs/05` 其余格不动**：只销 A1。W6-1 的"前置：A1 的 `-v` 跑法证明该用例没 Skip"这一句，本批的 12 条点名结果**当场满足它**（V8b 不在名单）⇒ 那一格的前置注需要一次跟手的措辞对齐（它写的是"前置"，不是"结论"），不改 W6-1 的实测内容本身。`:367` 那句"三个先行项建议在本轮开测前合入"里 A1 已合入 ⇒ 只追记状态，不重写句子。
5. **零代码 ⇒ 门禁只跑一次终态取证**：本批改动面只有两份 markdown ⇒ 行 6（`top_*` 计数）、行 9–11（前端三格）、行 13–14 应与 §6.72 终态同值；不同值就要逐格归因，不许含糊。
