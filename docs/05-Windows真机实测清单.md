# 05 · Windows 真机实测清单（M7 平台证据包）

> 定位：本文是 **M7（平台证据包）** 的执行清单（04 §6.11 登记表尾注登记的"下一批开工项"，原话"下一批开工只能是 M7（平台证据包）"）。
> 汇总自 `docs/04` §3.5 / §6.5 / §6.6.3 / §6.6.4 / §6.9.8 / §6.11 与后端代码逐文件盘点（域 A~R）。
> 每项给出：来源编号 → 操作方式 → 结果验证（判据）→ 需记录的读数。
> 状态口径：只记 **PASS / FAIL / SKIP(原因)**，不许把"CI windows 腿绿"读成"真机验证过"（见 §0.1）。

---

## 0. 总则

### 0.1 两条基础事实（决定本清单为何必须存在）

1. **CI 有 windows 真机腿，但日志分不清"真跑"与"整包 Skip"**：`scripts/test-windows-quarantine.sh` 在 `QUARANTINE` 空清单分支里的 `exec go test -count=1 ./...` 与 CI 三条腿都不带 `-v`（04 以短语"三条腿都不带 `-v`"多处自认，登记行为 §6.11 表内 **M100**）。因此凡标注"CI windows 腿已绿"的项目，其 Skip 出口项一律按"无读数"处理。
2. **判定逻辑住在无 tag 文件里，Linux 主门禁只证明判据表，不证明 Windows 胶水真读到过正确的字节**（ads / sysguard / realbytes / cloudfile / fscase / recycle_policy 同族分层纪律）。

### 0.2 上真机之前可先做的三件事（不占真机时间）

| # | 事项 | 落点 |
|---|---|---|
| A1 | windows 腿补一次带 `-v` 的跑法，把 `--- SKIP` 计数纳入读数 | `test-windows-quarantine.sh` 的 `QUARANTINE` 空清单分支（`exec go test -count=1 ./...`，M100 遗留动作）→ 直接产出 V8b、`fsid_path`、`trash_windows_recycle` 等 12 条"绿但不知是否真跑"用例的差分 |
| A2 | 修 `TestUndoHardlinkSelfHealsAfterUnlinkCrash`（`internal/ops/undo_i6_test.go`）里的**假前提 Skip**（Skip 文案"Windows 上 inode 身份未解析，走的是大小降级分支"，已被 fsid I7 推翻）→ 让 undoHardlink 强校验路径在 windows 腿重新暴露 | 域 E |
| A3 | 前端 pathpolicy 混例（盘符 / UNC / 尾分隔符 / 相对路径）是 node 用例，不必真机 | `plans/2026-09-22-full-review-revision.md` Task C2「高危判据测试缺口补齐（R3-2）」Step 1 `isGroupCrossVolume 用例`（〔2026-09-24 M205 勘误〕此处原文引用的编号"AS-H6·D-1"在计划文件里零命中，引用本身错） |

### 0.3 环境要求

- **主机**：Win10 1607+ 或 Win11（longPathAware / 开发者模式 / UAC 都依赖版本）；建议 Win11。
- **权限**：同一台机器上既能以**管理员**跑一次，也能以**普通账户（开发者模式开/关两态）**跑（§6.5 E 组四环境）。
- **卷**：NTFS 系统盘 + 至少一个第二物理卷（跨卷腿）+ 一个 **exFAT 或 FAT32 U 盘**（保留名夹具、`index==0` 卷、`CreateHardLinkW` 拒绝路径都靠它）+ 可选 SMB/网络卷、ReFS、BitLocker 卷。
- **注册表可写**：`HKCU\...\Explorer\BitBucket`（NukeOnDelete 场景）、`HKLM\...\FileSystem\LongPathsEnabled`（长路径两态）。
- **云客户端**：一个已配置 OneDrive 账户（占位文件场景 M31）。
- **杀软现实性**：至少一轮在**带第三方 AV / Defender 实时防护开启**的机器上跑（04 §6.6.3 拒绝原因表点名的"北信源防病毒接管/清理 `$Recycle.Bin`"、§6.6.4 点名的"目标环境（本机 Win11 + 北信源）`LongPathsEnabled` 取值未确认"两类干扰面：句柄占用、Minifilter 卷重定向、回收站目录接管）。
- **产物**：用 `wails build -platform windows/amd64` 或 build.yml 产出的 zip 裸 exe（未签名）；**不要**用 `GOOS=windows go test` 交叉跑代替 GUI 验收。
- **读数留档**：每项记录 OS 版本 / 卷文件系统 / 是否提权 / 开发者模式态 / AV 状态，按对应 M 编号回填 04。

---

## P0 · 回收站防线（H6 / §6.6.3 E 组）——最高优先级

失败模式是**静默永久删除**，且三层防线（预检 fail-open → 事后复核可 Skip → executor 严格回退只有注入测试）**没有任何一层有真机读数**（域 A/T-5）。

| # | 场景 | 操作方式 | 结果验证（判据） |
|---|---|---|---|
| W1-1 | 正常入站 | 对 NTFS 卷上一批小文件（含中文名、含 >1000 项混合列表）执行"移入回收站" | ① 源路径消失；② 文件出现在**该卷** `$Recycle.Bin`（系统回收站可见、可"还原"回原位）；③ 应用历史里 kind=trash、`DestPath` 恒空（结构性缺失，记录读数即可）、徽标/title 走 `undoableFor(trash,windows)`＝不可应用内回撤；④ 三判据事后复核（源不在 + 该卷条目增量==预期 + 入站时间基线）全部通过，无「可能已被直接删除，请立即到回收站核实」误报（`executor.go` trash 回退分支文案，见 W1-3 勘误） |
| W1-2 | 超配额拒绝 | 把测试卷回收站属性容量设为 **1MB**，对单个 >1MB 文件执行移入回收站 | 见**含容量数字的拒绝提示**（`capacityReason`），文件仍在原位；不得出现"报成功但文件消失" |
| W1-3 | 批量部分超限 | 混合"能进 / 不能进"的多文件一批执行 | **整批拒绝**（`internal/ops/trash_windows.go` 的 `defaultTrash` 预检注释原话"任一不满足即**整批拒绝**"，判据函数 `recyclableReason`/`recycleBinCapacityReason`）；若走 executor 逐个回退，被 Shell 静默丢弃的条目必须记 Failed（`Execute` 的 trash 回退分支，文案「可能已被直接删除，请立即到回收站核实」——〔2026-09-24 M205 勘误〕此清单旧版把该文案写作"疑似已被直接删除"，与码内字面有别，grep 时以 `executor.go` 现文为准），而非 Skipped。此语义 Windows 独有、至今零真机观测 |
| W1-4 | NukeOnDelete 全局 | `reg add HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket /v NukeOnDelete /t REG_DWORD /d 1` 后执行 | 预检**拒绝并指名策略**（nukeStatus=on）；删除键后恢复放行 |
| W1-5 | NukeOnDelete 按卷 | 在 `BitBucket\Volume\{GUID}` 下按卷设 1（GUID 来自 `GetVolumeNameForVolumeMountPointW`，顺带证 `volumeGUID` 解析） | **按卷覆盖全局**的 MSDN 优先级成立：该卷拒绝、他卷放行 |
| W1-6 | HKLM 机器级策略 | 若条件允许，在组策略禁用回收站的机器上执行 | 当前代码**只读 HKCU**（域 B）→ 预期行为是"预检放行、事后复核兜住"；记录兜住与否的真实读数（这是已登记缺口，测出什么都算收账） |
| W1-7 | SHQueryRecycleBinW / 卷 GUID | 让带 `-v` 的 windows 腿（或本机 `go test ./internal/ops -run Recycle -v`，需 GTest 环境）真跑 `trash_windows_recycle_test.go` | 四组可 Skip 用例（`TestVolumeGUIDShape` / `TestRecycleBinCapBytesSystemDrive` / `TestQueryRecycleBinSystemDrive` / Nuke 三态两条：`TestWinNukeStatusOfDistinguishesAbsentFromUnreadable` + `TestRecycleBinVolumeNukeSystemDrive`）**逐条报 PASS 而非 SKIP**；`shQueryRBInfoSize==20` 布局钉在真 Shell 上语义正确 |
| W1-8 | 长路径回收站 | >260 深度文件执行移入回收站 | `SHFileOperationW` 不接受 `\\?\`（`defaultTrash` 内注释原话"SHFileOperation 不支持 \\?\ 前缀，使用普通路径"）且不在 longPathAware 解除清单（04 §6.6.4"回收站路径（跨盘移入回收站）在超长路径上仍可能失败"段）→ 预期**显式失败并保留源文件**，不得静默永久删除 |
| W1-9 | Shell 静默降级 | 沙箱/被安全软件接管 `$Recycle.Bin` 的环境（或第三方清理工具在场）执行 | Shell 返回 0 但未进站时，事后复核必须报「可能已被直接删除，请立即到回收站核实」——这条防线自身是本清单的被测对象 |

---

## P1 · 长路径链路（T04 / §3.5#4 / §6.6.4 / M33）

manifest 的 `ws2:longPathAware` 元素已声明（`build/windows/wails.exe.manifest`）但**注册表前提、双条件生效、进程缓存**全部未复测。

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W2-1 | 长路径扫描/移动/回撤 | 构造 >260 字符深度的重复文件若干，`LongPathsEnabled=1` 态下扫描→移入回收站→（非 trash 类）回撤 | 扫描可见、操作不报 `ERROR_PATH_NOT_FOUND`；回撤成功 |
| W2-2 | 注册表未开启对照 | `LongPathsEnabled=0` + 重启后重复 W2-1 | 退化为 MAX_PATH 行为；确认失败被**报成显式 Failed** 而非静默丢条目 |
| W2-3 | ADS 守卫长路径 fail-open（**M33 开放项**） | 在接近/超过 260 深度造一条带命名流的文件：`type nul > "长路径文件.txt:note"`，对其执行去重动作 | 判据应仍 **Reject**（守卫不因 `FindFirstStreamW` 报 2/3 落进 ErrMissing→Allow）。若放行＝M33 从"推断"升级为"真机读数"，回到代码侧补 `\\?\` 重试 |
| W2-4 | 实占口径退化面 | 深目录下观察 realbytes 列 | `GetCompressedFileSizeW` 失败时 `ActualKnown=false`，界面须显示"未知"而非 0（域 H） |

---

## P2 · 软链接 / 重解析点（§6.5 E 组四环境 + junction）

`CreateSymbolicLinkW` 两段式与错误码翻译（1314/87/50/5）至今只过 `go vet`，`test-windows-quarantine.sh` 头注原话"在 Linux 上**只经过 go vet 的交叉编译检查，未经真实调用**"（开放项 E）。

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W3-1 | 提权 + 跨卷 | 管理员运行应用，对两个 NTFS 卷的重复内容执行软链接合并 | 成功；`VolumeResolved` 为真（`app.go` 的 `FileView.VolumeResolved` 字段，`toGroupView` 赋值）、前端显示跨卷入口（`frontend/src/utils/pathpolicy.ts` 的 `isGroupCrossVolume`） |
| W3-2 | 非提权 + 开发者模式**关** | 普通账户、开发者模式关闭 | **整体失败 + 恰一条**环境级权限指引（AggregateWarnings 折叠为一条，条目数不变），且**无任何文件被改动**；错误码文案指向"提权或开开发者模式"而非"环境不支持"（两段式降级顺序：先 0x2 标志、遇 87 再去标志重试——顺序反了会把环境不支持报成权限不足） |
| W3-3 | 非提权 + 开发者模式**开** | 开开发者模式后重复 | 成功（0x2 标志路径走通） |
| W3-4 | 同卷 | 同卷组执行 | 成功 + 提示建议改用硬链接 |
| W3-5 | 悬空链接 | 合并成功后拔掉目标盘/删除目标 | 记录页标红「链接已失效」（`SymlinkStatus`）；**回撤仍应成功**；若失效未标红 → §6.5 C 组补的 ACL/杀软致 Lstat 失败嫌疑点成立 |
| W3-6 | verifySymlinked 强校验档位 | 合并后手动把链接指向第三个文件再回撤 | Windows 上身份解析成功时应走强校验（`fsid.FromPath`）判否；若恒走"只比大小"弱校验＝I7 叙述在真机不成立 |
| W3-7 | junction / 卷挂载点回环 | 扫描含 `C:\Users\All Users`、手工 `mklink /J` 目录挂载点、`SUBST` 盘的根 | 扫描**不回环**、不死循环（代码零覆盖，见域 F 末行）；Go `Lstat` 对 mount-point 重解析点的 `Mode` 归属当场读数并回填注释 |

---

## P3 · 跨卷移动 / 回撤 / 错误码（域 C）

`isCrossDeviceExtra` 认 error 17 的分支**没有一条用例真跨卷 rename 过**；跨卷腿的旧欠账"本机 uid=501 挂不了独立卷"已被 04 §6.17 七更新口径——Linux/loop 卷侧已有 CI 读数，**Windows 侧仍无真机读数**（该脚本不在 windows job 里）。

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W4-1 | 跨卷 move 一次 | 两物理卷间对 ~200 个文件执行移动（`TestAggregateWarningsCollapsesPrivilegeFailures` 头注点名的欠账） | 走"复制+校验+identityStill+删源"腿；字节/条目记账正确（M40 假账族核账）；不出现"源没删/目标缺" |
| W4-2 | 跨卷回撤 | W4-1 之后执行回撤 | `restoreInPlace` 跨卷分支成功；目标卷同名被占时报错而非覆盖（Windows `os.Rename`＝MOVEFILE_REPLACE_EXISTING 静默替换风险，M19 抢占在 Windows 的残余 TOCTOU 记录读数） |
| W4-3 | 杀软持句柄 | 对正被预览/索引/同步占用的文件执行移动与暂存清理 | `ERROR_SHARING_VIOLATION(32)` 路径（`workTempRemove`）：重试/残留/报错三者实际行为记录，暂存目录不得静默堆积 |
| W4-4 | 盘符≠卷 | `SUBST X: D:\somefolder` 后跨该"卷"操作；同一目录先后挂两个不同卷 | 记录盘符键分裂/混池的实际症状（`TestFileVolumePrefersResolvedIdentity` 注释原话"Windows 上一个盘符未必是一个卷（挂载点会把别的卷挂到 C:\Mount\X）"，`app_volume_test.go`）；回收站判据 2 是否按 GUID 而非盘符正确归卷 |
| W4-5 | explorer 假告警 | 逐条点"打开所在文件夹"（Reveal，#6），路径含空格、逗号、中文各一 | `explorer.exe /select,` 常**成功也返回非零** → 观察 M58 的 `onExit` 报警是否每次误弹（域 N，高概率缺陷）；`OpenTrash`（`shell:RecycleBinFolder`）打开正确 SID 的回收站 |

---

## P4 · 物理身份 / 硬链接 / 卷型（域 D/E/H/J）

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W5-1 | fsid 逐卷读数 | 在 NTFS / exFAT / FAT32 / SMB / ReFS / BitLocker（可得者）上各解析一次文件身份 | NTFS：`Resolved=true` 且 FileId 稳定；FAT/exFAT：应落 `index==0→未解析` 分支；逐卷记"解析成功/失败"原始读数（域 T-3，不许只记用例绿） |
| W5-2 | 硬链接合并端到端（**L4 #1 欠账**） | 同 NTFS 卷上对重复文件执行硬链接合并→回撤 | `CreateHardLinkW` 成功且 `verifyHardlinked` 的 `os.SameFile` 判真；回撤恢复两文件。**带 AV 的机器上特别注意 Minifilter 骗过 SameFile 的可能**（域 E），必要时用 `fsutil hardlink list` 外部核对 |
| W5-3 | 硬链接在 exFAT | 对 exFAT U 盘执行合并 | `CreateHardLinkW` 应返回 50 并被翻译成可读失败，不是 panic/静默 |
| W5-4 | 写锁定文件身份 | 一进程独占写打开文件时执行 `identityStill` 五处复核 | `TestIdentitySurvivesWriteLockedFile` 的契约（FILE_READ_ATTRIBUTES only）真机 PASS 非 Skip |
| W5-5 | 大小写敏感目录（**M36 同族**） | `fsutil file setCaseSensitiveInfo C:\test enable`，目录内放 `a.txt`/`A.txt` 两个不同内容文件后扫描 | **不得折成同一文件**；卷默认判不敏感时，目录级敏感是否被探针捕获→记录（当前 `default_insensitive.go` 的假设风险正是这个形状） |
| W5-6 | 根去重折叠（M26 变异逃逸） | 同时以 `C:\a` 与 `C:\A\B` 为根扫描 | 可重叠根正确折叠、`c:/a/b` 与 `C:\A\B` 折成同键；这是 M26 修向在真机的核对项 |
| W5-7 | ExcludePaths 大小写（**结构性缺口取证**） | `ExcludePaths: ["Cache/Sessions"]`，盘上实存 `cache/sessions` | 当前预期：**不命中**（Windows 上 `Proven` 恒 false → 排除永远区分大小写，域 T-1）。测出即坐实"`GetVolumeInformationW` 补读数（§28.6）应排期" |
| W5-8 | NTFS 压缩/稀疏实占 | 对启用压缩的卷与稀疏文件（`fsutil sparse setflag`）扫描 | `GetCompressedFileSizeW` 给出实占<逻辑；`INVALID_FILE_SIZE` 双向判据不误判 |
| W5-9 | **WIN-1（M72）补码后复测** | 当前代码未 `SetLastError(0)` → 先修（04 §6.11 登记表行首 `\| M72 \| WIN-1 \|` 那一行 + 表尾约束(2)"标'✗'的四条开工即须写"），再复测 W5-8 | 干净 lastError 下不误读 0xFFFFFFFF |
| W5-10 | 占位/缓存第③重证据 | 同文件两种拼写路径（`FILE.TXT`/`file.txt`）重复扫描 + 缓存命中 | 记录 `CtimeNs==0` 是否真机普遍（`internal/fsid/fsid_windows.go` 注释"change time 取不到只丢这一重证据（CtimeNs=0）"、09 §4.3 对用户口径"取不到时只丢这一重证据、记为 0"）、缓存主键两行的实际后果（域 O） |

---

## P5 · ADS 命名流（M32 / M35）

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W6-1 | V8b 端到端 | 从 Internet Explorer/Edge 下载一批文件（自动带 `Zone.Identifier`），执行扫描+动作 | 带命名流文件被守卫**拒绝处理**且报 `Stage "ads"` Failed；症状面确认"大量拒绝"还是"全部放行"（域 G）。前置：A1 的 `-v` 跑法证明该用例没 Skip |
| W6-2 | 默认流放过 | 普通 NTFS 文件（仅 `::$DATA`） | 正常放行，不被误拦 |
| W6-3 | 枚举开销（M34 依赖） | 大语料扫描时记录 ADS 普查前后耗时 | 给 M34"扫描期普查做不做"提供真实开销数 |
| W6-4 | 非 NTFS 卷 | exFAT U 盘上扫描 | `FindFirstStreamW` errno 87 → 分类落 `ErrUnsupported`，不崩不误拦 |
| W6-5 | M35 症状面 | 两个默认流相同、命名流不同的文件 | 当前设计**仍报重复组但动作被拒**——确认界面表现与 09 §6.4 边界注口径一致（"两个默认流相同、命名流不同的文件**仍会被报成重复组**（只是清理被拒，不是没被报重复）"），用户可读 |

---

## P6 · 云端占位文件（OneDrive，M31/M36 族）

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W7-1 | RECALL 位真读数 | OneDrive 设"按需释放空间"，全盘占位状态下扫描 | `skipped_cloud_files` 计数 **>0 且数目==占位数**（若恒 0＝属性位推断错误，整包意义消失）；占位文件不参与哈希 |
| W7-2 | 水合成本 | 打开 `AllowCloudHydration` 扫 OneDrive 目录 | 记录实际下载流量/耗时量级（"扫全盘=拉全云端"风险，域 I），确认文案是否足够劝退 |
| W7-3 | 探针在云目录 | 对占位目录执行大小写探针 | 探针失败→全退默认 + `CaseProbeUnproven` 只报计数不改判（不中止整轮扫描）；若整轮"扫不动"则是域 J 的真机新缺陷 |

---

## P7 · 扫描器 / 性能 / 盘根（#2、M29、域 L/M/Q）

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W8-1 | 盘根扫描保护 | 以 `C:\` 为根扫描 | `pagefile.sys`/`hiberfil.sys`/`swapfile.sys`/`DumpStack.log.tmp`/`$Recycle.Bin`/"System Volume Information"/`WindowsApps` 全部被挡；`WindowsApps` 特权失败产生的 FailedItem 噪声量可接受（域 L）；**ExcludePaths 在盘根下不静默失效**（`internal/scanner/rootprefix_probe_test.go` 中 `TestExcludePathsWithTrailingSeparatorRoot` 头注自认："盘根那一支属 §15.5 的未兑现边界"） |
| W8-2 | 句柄数抽样（#2） | 大语料扫描中记录句柄峰值 | 非候选文件不开句柄（`keyFromInfo` 刻意不解析的取舍成立）；`resolveByHandle` 退化腿只在候选组内出现 |
| W8-3 | realbytes 开销 A/B（M29） | 同一语料分别在带/不带 `GetCompressedFileSizeW` 调用的构建下跑 benchgen，比对阶段 1 耗时 | 差值构成"是否可感知回归"的终判 |
| W8-4 | 介质并发（域 Q） | HDD 外置盘 / SMB 卷上整盘扫描 | Windows `Probe` 恒 Unknown → 维持 NumCPU-1 并发；观察是否出现寻道抖动/超时雪崩，决定是否补 `GetDriveTypeW` 降并发 |
| W8-5 | 中文/特殊字符（#7） | 文件名含中文、emoji、代理对、`\/*?"<>|` 之外的合法特殊组合走全链路（扫→哈希→移动→回撤） | 全链路无损；`buildPathList` 双 NUL 表经真 Shell 消费一次（域 A 遗留：5 条列表用例是纯 Go 编解码，无一条真调 API） |
| W8-6 | 保留名真实触发面 | NTFS 上造不出 `CON.txt`；用 FAT32 U 盘/网络卷/旧数据制造实存保留名文件 | 守卫拦截计数（`protectedFiles`）；确认"根级才挡"的边界不误伤嵌套同名合法文件 |
| W8-7 | `$I`/`$R` 元数据 | 盘根扫描时回收站元数据文件 | 不误入分组（m6 设计稿 `specs/2026-09-21-m6-implementation-design.md` §2.9"本轮不做"登记项："登记为 M7 真机清单项，本机无法实证其 Win32 行为，不做纸面修复"） |
| W8-8 | 短读成因清点 | 压缩卷/稀疏/OneDrive 水合中/HDD 慢读下大批量扫描 | 首扫不再复现"0 组/未保存缓存"（hasher 2026-09-19 事故的同类现象）；`ActualKnown` 作废口径正确 |

---

## P8 · UI / 系统集成 / 数据层（#6、M8、域 N/O/P）

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W9-1 | WebView2 真运行时 | 在 Wails 窗口内（非浏览器 dev）验证：拖拽入库、事件流刷新、原生目录选择对话框 | 三项全部可用（`frontend/src/wails.ts` 的 `backendOrNull` 取不到后端时抛错文案「后端不可用（浏览器开发模式下请使用 Wails 运行）」，明示浏览器不等价） |
| W9-2 | 不可回撤文案 | trash 一条记录，看徽标/title/toast | `undo-code-windows-trash` 文案渲染正确、裸码不漏出（`scan-undo-error` 契约）；"打开系统回收站"落到当前用户 SID 的回收站 |
| W9-3 | M8 呈现位 | 结果页横幅（`unprotectedRoots`）、保留名计数（`protectedFiles`） | 09 §1 产品概述"结果页横幅属后续版本，界面当前不呈现"与云端文件段"界面上目前没有『已跳过 N 个云端占位文件』这条横幅——新增计数的呈现属 M8（前端体验）"两处口径是否仍未呈现；若借本轮开显示，同步改文档 |
| W9-4 | Win32 错误本地化 | 收集一轮真实失败：`FailedItem.Err` 下发内容 | 记录用户实际看到的是 `FormatMessage` 英文/本地化文本还是中文（域 P，全仓零收敛，测出即立账） |
| W9-5 | 注册表三态 | 正常 / HKCU 键被设怪类型（`REG_SZ "1"`）/ ACL 拒绝 三态下预检 | off/unknown/mismatch 分类与真机返回码一致（Linux 上 Errno 2/5 是**数值巧合**，本轮是第一次语义级取证） |
| W9-6 | 配置目录 | 检查 `os.UserConfigDir()` 实际落点；若 `%APPDATA%` 被 OneDrive 重定向 | `cache.db`/`history.db` 不得落在云同步目录（WAL 不可靠风险，域 N）；重定向机器上给出告警或迁移 |
| W9-7 | SQLite 文件锁 | 双开应用 / 杀软扫过 db 文件时读写 | `busy_timeout=5000` 是否够；不出现"database is locked"裸错直达用户 |
| W9-8 | O_EXCL 撞目录 | `claimDst` 抢占目标为"已存在目录"时 | 记录真机返回 EEXIST(183) 还是 EACCES(5)（m6 设计稿 §8.5"未兑现与边界"原话：两者"都会走另名恢复（不覆盖、不报错），**无行为差异**，故不单列 ID，只记边界"；04 §6.9.8 有同文转录，读数回填注释即可） |
| W9-9 | 启动失败痕迹 | 故意制造启动失败（db 损坏等），GUI 子系统下运行 | `main.go` 的 `main()` 启动失败出口（注释原话"P3：println 在无控制台附加的发布版（Windows GUI 子系统）里等于丢弃错误"+ 文案「FileDedup 启动失败」）→ 确认真机上是否留下任何可诊断痕迹，不留则立发布风险账 |

---

## P9 · 发布体系（#9、M120）

| # | 场景 | 操作方式 | 结果验证 |
|---|---|---|---|
| W10-1 | 产物冒烟 | 下载 build.yml 的 windows zip，解压裸 exe 直接运行 | SmartScreen/未签名告警下的首次启动路径；`info.json`/manifest 生效（`check-version-sync` UTF-8 修复后的发布腿首跑复账，M120） |
| W10-2 | 安装/卸载（#9） | 若引入安装器则全链路；当前为裸 exe——至少验证数据目录残留与"卸载"=删目录的行为 | 记录残留清单；同时裁定 04 §6"开放项与风险"**A 组 — 发布前必须由用户决策**第 2 条"是否接受当前不可回撤面"（发布说明是否需前置强调） |
| W10-3 | CI windows 腿超时 | 一次 tag 触发观察 build.yml windows job | timeout-minutes 改动生效（M120 唯一兑现途径） |

---

## 附：回填约定

- 每条读数按来源编号回填 04（M7 总账 ⊃ E 组回收站/软链接、T04、M26/M29/M31/M32/M33/M35/M36/M45/M72/M100、§3.5 #2/#4/#6/#7/#9、AS-R4）。〔2026-09-24 M205 勘误：旧版此处另有三个裸行号 `:575`/`:1058`/`:1127`，在 04/设计稿中已随批增行号漂移、逐条现读不可达，改为本清单 W5-10（`fsid_windows.go` 注释 + 09 §4.3）、W1-8（04 §6.6.4"仍可能失败"段）、W9-8（m6 设计稿 §8.5 / 04 §6.9.8）三处锚点引用。〕
- SKIP 必须带原因与解除条件；**"清单项目全部执行完"的定义是每条都有读数（PASS/FAIL/SKIP+原因），不是"看起来绿"**。
- 与真机无关的三个先行项（§0.2 A1/A2/A3）建议在本轮开测前合入，否则 P0/P5 半数项目拿不到差分读数。
