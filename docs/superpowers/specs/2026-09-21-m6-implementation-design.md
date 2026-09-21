# M6 实施细化设计（真实工况安全 · 逐项设计段）

- 日期：2026-09-21
- 上位依据：`2026-09-20-scenario-optimization-design.md`（工况总纲）§2.1~§2.4、§4、§5；
  登记处 `docs/04-开发与测试计划.md` §6.7 C 组 1~4 项；§7 另接 04 §6.8.8 M21
  （登记修法即"并入工况 M6 的计数横幅"，故与本稿同源）；§8 另接 04 §6.8.8 M19/M20
  （回滚路径的 TOCTOU，与 §2/§6 的 `claimSlot` 是同一条"不替用户处置不属于本次操作的文件"主线）
- 状态：**按实施顺序逐节追加**。本文只写"动手前必须定死"的判据、接入点与探针设计；
  实施后的兑现记录写在 04 §6.9 划账，不写在本文
- 用户裁定（三条，约束本文全部章节）：① 每项实施前各写一段细化设计（即本文）；
  ② 本轮只做 M6 后端，M8 前端体验项不做；③ 云占位默认跳过 + 可见计数

## 0. 实施顺序与本文进度

| 序 | 项 | 总纲编号 | 04 §6.7 | 本文小节 | 设计 | 实施 |
|---|---|---|---|---|---|---|
| 1 | 系统保护清单 + Windows 保留名 | §2.4 | C 组 4 | §2 | ✅ | ✅（划账见 04 §6.9.1） |
| 2 | 实占口径（稀疏/压缩） | §2.2 | C 组 2 | §3 | ✅（含 §3.0 实测证据） | ✅（划账见 04 §6.9.3） |
| 3 | 云占位检测 | §2.1 | C 组 1 | §4 | ✅（含 §4.0 取证，并**推翻总纲两条判据前提**） | ✅（划账见 04 §6.9.4；§4.4 末尾有本稿首版预测的两处修正） |
| 4 | Windows ADS 防护 | §2.3 | C 组 3 | §5 | ✅（含 §5.0 取证，E7 纠正了本稿动手前的结构布局印象） | ✅（划账见 04 §6.9.5） |
| 5 | fscase 单根探测 | — | C 组 1(04 §6) | §6 | ✅（含 §6.0 取证，**推翻登记的"最小修法：无条件探测"**；真读数取自本机 hdiutil 造的大小写敏感卷） | ✅（划账见 04 §6.9.6；§6.4 末尾有本稿预测的一处修正） |
| 6 | worktemp 跳过计数（M21） | — | §6.8.8 M21 | §7 | ✅（含 §7.0 取证 E1~E8；五层管道照抄、计数落在跳过点） | ✅（划账见 04 §6.9.7；M 表无事后修正——M-d 那一行是**跑之前**改准的） |
| 7 | 回滚路径的 TOCTOU（M19 + M20） | — | §6.8.8 M19/M20 | §8 | ✅（设计：§8.0 取证 E1~E9；实施：六条探针 5 红→全绿 + 变异六条逐条命中，**另补第 7 条变异 M-M20-c′**；顺手消掉 move/symlink 逐字重复的一份） | ✅（划账见 04 §6.9.8；新增登记 M37/M38/M39，见 §8.5） |

## 1. 适用于全部四项的通用约束

1. **判定逻辑必须是无 build tag 的纯函数**，平台差异以**参数注入**而非 `//go:build` 分流
   （H6 分层教训，04 §6.8.0 约束 5 / 总纲 §4.2）。带 tag 的文件只允许出现
   "系统调用胶水 + 常量真值"，其行数越少，进 Linux CI 主门禁的可验证面越大。
2. **计数不许说谎**（§6.6.2 三层计数原则）：新增的每一类"没进语料"都必须有独立计数与
   独立文案，且**计数口径要说明数的是什么**（目录数 vs 文件数 vs 字节数）。
   剪枝掉的目录只能计目录数——我们没有下潜，报"跳过了 N 个文件"就是编数。
3. **修前必红**：每项的每个探针必须先在不改生产代码的前提下红一次，读数落进划账；
   关键判据函数做变异验证（改坏 → 变红），沿用 H6 的六重变异记录表格式。
4. **UI 边界**（裁定 ②）：本轮新计数只到 `ScanSummary` JSON 与 `fdd-cli` 报告为止。
   结果页横幅属 M8。**划账时必须写"UI 未呈现"，不得因后端字段齐了就记兑现**。
5. **不改既有登记行**：实施中发现的新缺陷新增 ID 登记（04 §6.8.0.5），
   不改写 §6.7/§6.8 已有结论。

---

## 2. M6-P4：系统保护清单 + Windows 保留名（总纲 §2.4 / 04 §6.7 C 组 4）

### 2.1 现状与危害（逐条实测/读码确认）

现状是**零内置排除**。遍历期仅有的三道剪枝/跳过：

| 现有规则 | 锚点 | 挡住了什么 | 挡不住什么 |
|---|---|---|---|
| 名字以 `.` 开头的目录不深入 | `internal/scanner/scanner.go:233-235` | macOS `.Trash`/`.Spotlight-V100`（**仅在 `IncludeHidden=false` 时**） | Windows `$Recycle.Bin`（首字符是 `$`）、`System Volume Information`、`WindowsApps`；用户勾选"包含隐藏文件"后 macOS 那几项也漏 |
| 用户 `ExcludePaths` 剪枝 | `scanner.go:240-242` + `internal/filter/filter.go:162-176` | 用户主动写了的那些 | 前端默认 `ExcludePaths: []`（`frontend/src/stores/scan.ts:18`）——**默认配置下等于没有** |
| `worktemp.IsTempName` | `scanner.go:229` | 应用自己的 `.fdd-*` 残留 | 与系统目录无关 |

由此得到三类**具体**危害（不是"可能有噪声"这种含糊话）：

1. **回收站内容被当语料，可导致二次删除**：Windows 移入回收站走 Shell
   `IFileOperation`（`internal/ops/trash_windows.go`），文件实际落到该卷
   `$Recycle.Bin\<SID>\`。该名字不以 `.` 开头，而扫描器**完全不看文件属性**
   （只看 `de.Type()`/`info.Mode()`，没有隐藏位判定）。因此"扫 `D:\` → 移入回收站
   → 再扫 `D:\`"这条完全正常的动线，会把回收站里那份副本重新收进语料、
   和原位残留/其他卷的同内容文件配成重复组。Linux/macOS 侧同一动线落在
   `~/.local/share/Trash/files`（`internal/ops/trash_linux.go:15`）与 `.Trash`，
   今天**只靠 `.` 前缀规则顺带挡住**，用户一旦勾选"包含隐藏文件"即失效。
   补一条实测事实：这条链在 Windows 上**没有第二道防线**——Go 的
   `fs.FileMode` 根本不暴露隐藏位（stdlib 全仓无 `ModeHidden`；
   `os/types_windows.go` 只把 `FILE_ATTRIBUTE_DIRECTORY/DEVICE/REPARSE_POINT/VOLUME`
   与 `ModeIrregular` 映射进 Mode），所以"按属性挡隐藏目录"不是可选方案，
   要么显式 `GetFileAttributesEx`，要么走名字清单（本设计取后者，理由见 §2.9）。
2. **盘根扫描的海量 Failed**：`System Volume Information`、`WindowsApps` 是 ACL
   拒绝遍历的目录，整棵子树每一层都产生一条 `Stage:"scan"` 失败项
   （`scanner.go:200-204`、`:256-260`），失败抽屉被系统性噪声灌满，
   真正的"你的文件读不了"淹没在里面。
3. **Windows 保留名**：从 exFAT/Linux 卷拷来的 `CON.txt`、`NUL.dat` 这类名字，
   Win32 打开时会解析到设备而不是文件；今天它会正常进语料、正常参与分组，
   删除/移动走的是设备路径语义——**操作结果与用户预期完全脱节**。

> **实施时要新增登记**（按 §1 约束 5，不改写 §6.7）：危害 1（`$Recycle.Bin` 进语料）
> 是本轮读码新查出的独立缺陷，严重度高于"噪声"，登记为 §6.8.8 新 ID，
> 并说明它由 M6-P4 顺带闭环。

### 2.2 判据模型：四类条目，不要一个通配引擎

总纲 §2.4 给的是一份平铺清单。平铺实现只有两种收场：要么全部按"路径段名字"匹配
（于是 `/System` 会误伤 `D:\System`、`/mnt/x/system`），要么引入 glob（于是多一套
要测的语义）。这里改成**四类条目、四种判据**，每类的适用面写死，清单里逐项标注归属：

| 类型 | 判据 | 大小写 | 用哪些条目 |
|---|---|---|---|
| `dirName` | 路径**最后一段**等于/前缀命中该名字，出现在任意深度即剪枝 | 不敏感比对（`strings.EqualFold`） | `$Recycle.Bin`、`System Volume Information`、`WindowsApps`、`lost+found`、`.Spotlight-V100`、`.fseventsd`、`.Trashes`、`.DocumentRevisions-V100`、TM 快照目录（前缀+后缀式） |
| `absPath` | 折叠后的**绝对路径**等于该值或是其后代（`p==e \|\| HasPrefix(p, e+"/")`） | **精确**比对 | Linux `/proc`、`/sys`、`/dev`、`/run`；macOS `/private`、`/System` |
| `pseudoFileAtRoot` | 文件名相等 **且** 其父目录恰是本次的某个扫描根 | 不敏感比对 | `pagefile.sys`、`hiberfil.sys`、`swapfile.sys`、`DumpStack.log.tmp` |
| `winReservedName` | 去扩展名后的主体 ∈ {CON,PRN,AUX,NUL,COM1-9,LPT1-9}，仅当平台为 Windows | 不敏感比对 | 保留名 |

四条"为什么这样切"的理由，实现时不得反过来：
- **`dirName` 用不敏感、`absPath` 用精确**，方向不同是刻意的。`dirName` 命中的是
  文件名模式，多匹配一份的代价极低（这些名字不会是用户数据），
  而**少匹配**才是事故（`$RECYCLE.BIN`、`SYSTEM VOLUME INFORMATION` 在 Windows 上
  就是同一个对象）；`absPath` 命中的是"这条绝对路径本身"，放宽成不敏感就会误伤
  真实的用户路径（大小写敏感卷上的 `/system` 镜像根目录）。
- **`pseudoFileAtRoot` 只认"父目录是扫描根"**，不做"是不是卷根"判定。卷根判定在
  Windows 上是真麻烦（`D:\` 的 Clean 形态、UNC、卷 GUID 路径、挂载点目录），
  而危害只发生在"用户把盘根设成了扫描根"这一种情形——用扫描根集合当锚，
  一条 `map[string]bool` 查表就够，且完全可测。
  代价如实写明：嵌套目录里的 `D:\data\backup\pagefile.sys`（虚拟机镜像备份常见）
  **不**被保护，仍按普通文件参与去重。这是本设计的边界，不是漏实现。
- **`winReservedName` 必须以平台参数生效**，绝不在 Linux CI 上按宿主平台判定：
  平台常量由 `platform_windows.go`/`platform_darwin.go`/`platform_linux.go` 三个
  各三行的胶水文件提供，判据本体无 tag，因此"Windows 保留名规则"这件事本身
  在 Linux 上就有回归覆盖（否则它永远只在 Windows 编译，等于没测）。
- **不用"读 Windows 隐藏属性"替代名字清单**（第四条，因为它看起来更"正统"）：
  `fs.FileMode` 无隐藏位（§2.1 末），要拿属性就得为**每个目录**加一次
  `GetFileAttributesEx`——目录级遍历是本项目最热的路径，为一个判据给全盘加 syscall
  不划算；而名字清单是纯字符串、零 syscall、跨平台可在 Linux 门禁断言。
  更根本的理由：隐藏属性属于**用户偏好**（Windows 上大量用户数据被标隐藏），
  把它接进 `IncludeHidden` 会改变既有过滤器语义；受保护清单要的是"不可关闭"，
  两者不是一回事。

### 2.3 清单初版（逐项附"为什么是它"）

**Windows**

| 条目 | 类型 | 理由 |
|---|---|---|
| `$Recycle.Bin` | dirName | 回收站实体（危害 1）；所有版本、所有卷同名 |
| `System Volume Information` | dirName | 卷影复制/索引数据库，ACL 拒绝且全是系统卷元数据 |
| `WindowsApps` | dirName | Store 应用包，受 DACL 保护，进去只产 Failed |
| `pagefile.sys` / `hiberfil.sys` / `swapfile.sys` | pseudoFileAtRoot | 盘根伪文件，逻辑大小巨大且内容随时变 |
| `DumpStack.log.tmp` | pseudoFileAtRoot | 盘根 0 头页文件，恒被占用 |
| `CON`/`PRN`/`AUX`/`NUL`/`COM1-9`/`LPT1-9` | winReservedName | §2.1 危害 3 |

**macOS**

| 条目 | 类型 | 理由 |
|---|---|---|
| `/.Spotlight-V100`、`/.fseventsd`、`/.Trashes`、`/.DocumentRevisions-V100` | dirName | 每卷都有的元数据目录（注意：外接卷根同样有，故按名字而非按 `/` 绝对路径） |
| `System Volume Information` | dirName | 扫 NTFS 外接盘时同一条规则直接复用，不必为 macOS 另写 |
| `/private`、`/System` | absPath | `/System/Volumes/Data` 是 Firmlink 下真实数据的挂载点，**从 `/` 扫会走两遍用户数据**；`/private` 下是 `var/folders` 等运行时垃圾 |
| `.com.apple.TimeMachine-*.snapshots` | dirName（前缀+后缀） | TM 本地快照，整卷的历史副本——不挡的话"重复文件"里全是快照，删了等于删备份 |

**Linux**

| 条目 | 类型 | 理由 |
|---|---|---|
| `/proc`、`/sys`、`/dev`、`/run` | absPath | 伪文件系统。`/proc` 下的 regular + 非 0 size 文件读起来是内核生成物，且 `/proc/self/fd/*` 会牵回真实文件 |
| `/lost+found` | dirName | ext4 修复目录，空或全是 inode 残骸 |

**明确不进清单**（写下来防下一轮加回来）：

- `node_modules`、`.git`、`build`、`dist`：这是**用户偏好**不是系统保护，
  已有 `ExcludePaths` 通道，混进引擎就会变成关不掉的偏执；
- macOS `/Library`、`/Applications`：里面有真实用户数据（App Support 的镜像、
  用户安装的 app），排除它们会造成静默漏扫，比噪声更糟；
- Windows `C:\Windows`：整目录排除会让"扫盘根"这一常见动线的结果少得反常，
  而它的危害已经由上面五条根级条目挡住了绝大部分。**登记为开放项**：
  是否给 `Windows` 目录一个"提示级"处理，留待 M7 真机取证后再定。

### 2.4 包与 API 形状

新增叶子包 `internal/sysguard`（与 `internal/worktemp` 同级同类：不 import 任何
`internal/` 包，scanner 直接引用）。

```go
type Kind int
const (
    KindNone Kind = iota // 不保护，照常遍历
    KindProtectedDir     // 目录被剪枝
    KindProtectedFile    // 盘根伪文件
    KindReservedName     // Windows 保留名
)

type Decision struct {
    Skip   bool
    Kind   Kind
    Reason string // 进日志/报告的短句，不含路径（调用方拼）
}

// Guard 由 New(platform) 编译出清单，只读复用（对齐 filter.Matcher 的做法）。
func New(p Platform) *Guard
func (g *Guard) Dir(absPath, name string) Decision
func (g *Guard) File(absPath, name string, isScanRootChild bool) Decision
```

- `Dir` 不带 `isScanRootChild`：清单里没有任何"根级目录"条目（根级限定只属于
  `pseudoFileAtRoot`，那全是文件），多一个恒不被读的参数只会让"这条判据其实没生效"
  看不出来。

- `Platform` 常量：`PlatformWindows/PlatformDarwin/PlatformLinux`，
  由三个各 3 行的 tagged 文件给出 `const Current = ...`；
  **测试一律显式传 `PlatformWindows`**，这样保留名/`$Recycle.Bin` 规则在
  Linux 门禁里就是可执行断言。
- `Dir`/`File` 分开而不是一个 `Apply`：目录判据不含 `absPath` 之外的文件语义，
  合并会让"伪文件条目不小心写成目录"这类错误没有类型层面的阻力。
- `isScanRootChild` 由调用方给（scanner 持有折叠后的根集合），
  sysguard 自己**不做**任何路径解析——它是纯字符串判定器，这是"能进 Linux CI"的前提。
- 归一化在 sysguard **内部**做（`filepath.Clean` + `\`→`/`），不要求调用方先归一：
  判据的正确性不能依赖"每个调用点都记得先 Clean"。`absPath` 条目按精确比对，
  `dirName`/`pseudoFile`/`winReservedName` 用 `strings.EqualFold`。

### 2.5 接入点与既有规则的先后

`WalkWithGate` 内两处，顺序是设计的一部分：

1. **目录分支**：在 `scanner.go:233` 的隐藏规则**之前**插入保护判定。
   理由：`.Spotlight-V100`、`/System/Volumes/Data` 这些既属"系统保护"又属"隐藏"，
   判定顺序决定它被记进哪个计数。保护在前，计数才不随 `IncludeHidden`
   开关漂移——否则"已保护跳过 N"会在用户勾一下复选框后变小，读起来像保护失效。
2. **文件分支**：在 `scanner.go:229` 的 `IsTempName` **之后**、`Info()` **之前**插入
   `File` 判定。放在 `Info()` 之前是刻意的：保留名文件在 Windows 上
   `de.Info()` 本身可能返回设备信息或报错，先判定就一次盘都不碰。

命中即 `continue`，同时 `local`/`fails` 都不写——**保护跳过不是失败**，
不产生 `FailedItem`（否则又造出一类噪声，与危害 2 同形）。

目录剪枝不进 `visited`：保护判定在 `visited` 之前短路，`Visited` 语义
（"实际访问目录数"，`Result.Visited`）不受影响。

### 2.6 逃逸通道：唯一例外是用户显式设为扫描根

总纲 §2.4 要求"清单不可关闭，唯一例外是用户显式把清单内路径本身设为扫描根时放行"。
实现成一条可判定的规则，而不是一个特判列表：

> **规则**：对被判定为保护的目录 `D`，若本次任一扫描根 `R` 满足
> `R == D` 或 `R` 在 `D` 之下，则**不剪枝 `D`**（必须下潜才够得着 `R`），
> 并把 `D` 记入 `UnprotectedRoots`。

这一条同时覆盖两种情形，且不需要"清单内路径"的可得性判断：

- 用户直接把 `C:\Windows\System Volume Information` 设为根 → `R == D`；
- 用户设在保护目录**内部**（`/System/Volumes/Data`、`/proc/self/task`）→ `R` 在 `D` 下。
  修前的直觉实现"只有根恰好在清单里才放行"在这里会失效：`/System/Volumes/Data`
  不在清单里，于是 `/System` 被剪枝、用户指定的根**一个文件都扫不到**，
  结果页显示"0 组"而没有任何解释——这是必须写进探针的场景。

`pseudoFileAtRoot` 与 `winReservedName` **不参与逃逸**：即使根就是 `D:\`，
`pagefile.sys` 仍然跳过（它不可读，放行只会换回一条 Failed），保留名仍然跳过
（它不是"一个文件"而是一个设备别名）。**清单里没有任何"可关"开关**，
`Settings` 不新增字段，用户配置也无法清空这份清单。

### 2.7 计数与载荷

| 层 | 新增 | 口径 |
|---|---|---|
| `scanner.Result` | `ProtectedDirs int` / `ProtectedFiles int` / `UnprotectedRoots []string` | 目录数、文件数；不估文件总量 |
| `dedup.Pipeline` | `ProtectedDirs()` / `ProtectedFiles()` / `UnprotectedRoots()`（`atomic` + `Load`，与 `ScannedFiles`/`CacheHits` 同族：按轮归零） | 同上 |
| `app.go` `ScanSummary` | `protectedDirs` / `protectedFiles` / `unprotectedRoots`（JSON tag 小驼峰，与既有 `filesFailed` 同风格） | 结果页横幅的数据源（**M8 才呈现**） |
| `cmd/fdd-cli` report `stats` | `protected_dirs` / `protected_files` | 冒烟与脚本可比对 |

`scripts/smoke-cli.sh` 的一致性比较键**不加**这两项（benchgen 语料里没有受保护名字，
加了等于把脚本钉在"永远 0"上，没有信息量）；改为在 §2.8 的 T7 里用真实夹具断言非零。

### 2.8 测试方案（修前必红清单）

纯逻辑用例放在 `internal/sysguard/sysguard_test.go`（无 tag，Linux 门禁全量执行）；
集成用例放在 `internal/scanner/scanner_guard_test.go`（无 tag，靠显式传平台跑 Windows 规则）。

| # | 用例 | 断言 | 为何"修前必红" |
|---|---|---|---|
| T1 | `TestGuardRecycleBinDirNameIsCaseInsensitive` | `Platform=windows` 下 `$RECYCLE.bin`、`$Recycle.Bin`、`system volume information` 全部 `KindProtectedDir` | 包不存在 → 编译红 |
| T2 | `TestGuardAbsPathIsExactCase` | `Platform=linux` 下 `/proc` 命中、`/PROC` 不命中、`/mnt/x/proc` 不命中、`/proc/1/task` 命中（后代规则，纵深防御） | 同上 |
| T3 | `TestGuardPseudoFileOnlyAtScanRoot` | `isScanRootChild=true` 时 `pagefile.sys` 命中；`false`（嵌套 `data/backup/pagefile.sys`）时不命中 | 同上；同时钉住 §2.3 的边界不外溢 |
| T4 | `TestGuardReservedNameWindowsOnly` | `Platform=windows` 时 `CON`、`NUL.txt`、`com1.log`、`LPT9` 命中 `KindReservedName`，`CONTRADICTION.md`、`notes.txt`、`COM0.dat`、`LPT0.dat` 不命中；`Platform=linux` 时**同一批名字全部不命中** | 同上；"Linux 上放行"是平台不外溢的反向断言 |
| T5 | `TestGuardTMAndDotDirsPrunedRegardlessOfHiddenFlag` | `.com.apple.TimeMachine-09-20-2026-010203.snapshots` 命中；`.Spotlight-V100` 命中 | 同上 |
| T6 | `TestWalkPrunesProtectedDirAndCountsIt`（集成，真夹具） | temp 根下建 `$Recycle.Bin/sub/dup.bin` + `keep/a.bin`；扫描后 `Files` 不含前者、`ProtectedDirs>=1`、`Failed` **为空** | 修前 `$Recycle.Bin` 会进语料 → `Files` 含 dup.bin → 红 |
| T7 | `TestWalkCountsRootPseudoFileOnly`（集成） | 根下建 `pagefile.sys`（非 0 字节）→ `ProtectedFiles==1` 且不在 `Files`；`sub/pagefile.sys` → 在 `Files` | 修前 `ProtectedFiles` 字段不存在 → 编译红；同时锁住 T3 的边界在遍历层成立 |
| T8 | `TestRootInsideProtectedDirStillScanned`（§2.6 逃逸，集成） | 把清单内某目录（用夹具版条目，见下）**内部**的子目录设为唯一扫描根 → `Files` 非空、`UnprotectedRoots` 含该祖先、`Failed` 为空 | 修前无逃逸概念；修后若逃逸写坏，这条会在"根在保护目录内部时扫不到东西"上红 |
| T9 | `TestHiddenFlagDoesNotMoveProtectedCount`（集成） | 同一夹具跑 `IncludeHidden=false` 与 `true` 两轮，`ProtectedDirs` 两轮**相等** | 顺序若写反（隐藏规则在前），`true` 轮才第一次把 `.Spotlight-V100` 记成保护 → 两轮不等 → 红。这条是 §2.5 顺序决策的守卫 |

**夹具可移植性**：T6/T7 用的 `$Recycle.Bin`、`pagefile.sys` 在 Linux/macOS 上是
**完全合法的普通名字**，因此这三条不需要任何平台豁免，天然进 Linux 主门禁——
这正是 §2.2 把判据做成纯字符串的直接收益。T8 需要"祖先在清单里、且能在 temp 下造出来"
的名字，用 `lost+found`（dirName，任意平台可造）：根 = `tmp/lost+found/inner`，
断言 `inner` 里的文件仍然被采集。

**实际落地用例名与上表的对应**：`internal/sysguard/sysguard_test.go` 6 例
（T1~T5 加 `TestGuardNoEmptyEntriesAndReasonNonEmpty`：清单不许有空条目、每条必须给得出
理由，Kind 与 Reason 关键词一一对应）；`internal/scanner/scanner_guard_test.go` 7 例
（T6~T9 加 `TestWalkReservedNameWindowsGuardOnly`（保留名在遍历层的正反向）、
`TestExplicitProtectedRootIsReported`（根自身命中清单时只留痕不剪、且不计数）、
`TestProtectedDirsCountIsDirCountNotFileGuess`（钉住 §1 约束 2 的计数口径））。
兑现读数与变异表见 04 §6.9.1。

**变异验证**（改坏判据 → 必须变红，逐条记录读数，格式对齐 H6 六重变异表）：

1. `dirName` 比对改成大小写敏感 → T1 红；
2. `absPath` 放宽为"任意路径段相等" → T2 红（`/mnt/x/proc` 被误伤）；
3. `pseudoFileAtRoot` 去掉 `isScanRootChild` 条件 → T3/T7 红（备份里的 `pagefile.sys` 被吞）；
4. 保留名清单去掉"NUL" → T4 红；
5. 保留名判定不看平台 → T4 的 linux 反向断言红；
6. 逃逸规则反转为"根在保护目录内部也剪枝" → T8 红；
7. 保护判定挪到隐藏规则之后 → T9 红；
8. 命中保护时同时写 `FailedItem` → T6 的 `Failed 为空` 红。

### 2.9 本轮不做（写清边界，防止划账时被当成已交付）

- **结果页横幅"已保护跳过 N 个目录 / M 个文件"与逃逸警示的 UI**：属 M8（裁定 ②）。
  本轮只交付 JSON/CLI 字段；`unprotectedRoots` 非空时用户当前**看不到任何提示**，
  划账须记为"UI 未兑现"。
- `Settings` 新增开关：明确不做（清单不可关闭是本项的立论前提）。
- `ops` 侧的二次防线：语料里既然没有受保护文件，就发不出对它的操作请求；
  唯一残余风险是"扫描后用户在盘根手动建了同名文件再执行操作"，
  该窗口由 `ops/verify.go` 的身份复核兜着，不为它另加判据。
- Windows 保留名的**尾随空格/尾点变体**（`"CON "`、`"NUL."`）与 `$I`/`$R` 回收站
  元数据文件：登记为 M7 真机清单项，本机无法实证其 Win32 行为，不做纸面修复。

---

## 3. M6-P2：实占口径（总纲 §2.2 / 04 §6.7 C 组 2）

### 3.0 前置实验（动手前必须拿到的证据，2026-09-21 本机实测）

环境：macOS 26.6.2（darwin/arm64），夹具在 `/tmp`（APFS 卷）。参照系是 `df -k /tmp`
的 `avail` 逐步差值（真实磁盘占用），被测口径是 `os.stat().st_blocks * 512`。

| # | 夹具 | `st_size` | `st_blocks×512` | `df` 实测差值 | 判读 |
|---|---|---|---|---|---|
| 1 | `dd if=/dev/urandom bs=1m count=32`（实写） | 33,554,432 | 33,554,432 | 与 size 同量级 | 相等 |
| 2 | `cp -c` 克隆 #1 | 33,554,432 | 33,554,432 | **4 KB** | **虚高约 8000 倍**（extents 共享却各自报满额） |
| 3 | `mkfile -n 8m`（不写数据） | 8,388,608 | 8,388,608 | 8,196 KB | 相等（预留即计入） |
| 4 | `cp -c` 克隆 #3 | 8,388,608 | 8,388,608 | **8 KB** | 同 #2 |
| 5 | `mkfile -n 32m` | 33,554,432 | **16,384** | 16 KB | 相等，但**未预留**——与 #3 同工具不同结果 |
| 6 | `dd bs=1m count=1 seek=64`（稀疏 65 MiB） | 68,157,440 | 1,048,576 | ≈1 MiB | 相等（洞不计） |
| 7 | `head -c 1024`（1 KiB 实写） | 1,024 | **4,096** | — | **实占大于逻辑**（4 KiB 块粒度） |
| 8 | 0 字节 | 0 | 0 | — | 扫描器本就跳过 0 字节，不入账 |

由此定下三条，缺一条就不该动手：

1. **`st_blocks×512` 除"共享 extent"一类外都等于真实占用**（#1/#3/#5/#6 全部与 `df` 对得上）。
   所以总纲选它作实占代理成立；而今天用 `Size` 计账在稀疏文件上虚高 **65 倍**（#6：
   报 65 MiB，实际 1 MiB），这一类是实打实的收益。
2. **CoW 克隆是本口径修不掉的残余**（#2/#4）：`st_nlink` 仍为 1、inode 各异，
   所以现有硬链接防线（按 FileID 去重、单列 `LinkedBytes`）**看不见它**，
   `st_blocks` 同样看不见。识别它要读 extent 映射（FIEMAP / `fclist` 一类），
   代价与平台面都不在本轮范围内 → 新增 ID 登记，不假装解决。
3. **实占可以大于逻辑大小**（#7 块粒度）。因此任何"`min(实占, 逻辑)`"式的封顶都是错的，
   会计上必须允许 `Actual > Size`，并在文案里说清"含块对齐"。
   另：#3 与 #5 说明"预分配"在同一系统上都不一致，进一步支持"不许拿 `Size` 当实占"。

**实施期补充实测（2026-09-21，同一台机器同一卷，Go `os.File` + `WriteAt` 造夹具）**：
上表是 shell 工具（`dd`/`mkfile`/`cp`）做的，落到 Go 夹具时又量出两条会影响用例的事实：

| 夹具 | 逻辑 | `st_blocks×512` | 判读 |
|---|---|---|---|
| truncate 到 1/2/4/8/12/16 MiB 后在尾部写 1 KiB | 1 MiB…16 MiB | **等于逻辑大小** | APFS 对 ≤16 MiB 的文件把洞**落地分配**，稀疏要 24 MiB 以上才出现 |
| 同上，24/32/64/128/256 MiB | ≥24 MiB | 16,384 | 真的留洞 |
| truncate 到任意尺寸、**一个字节都不写**（全洞） | 8…256 MiB | **0** | 全洞文件的 0 blocks 与"故障卷恒报 0"在单文件上不可区分 → 见 §3.4 |

两条直接影响实现与用例：① 稀疏夹具必须 ≥ 32 MiB，否则用例会在一台**正常**的 mac 上
正确地红（本轮取 64 MiB）；② 全洞文件的处置必须显式选边，见 §3.4 的取舍记录。

### 3.1 采集分层（对齐 §1 约束 1）

新增叶子包 `internal/realbytes`，形状与 `internal/worktemp`、`internal/sysguard` 同族：

| 文件 | tag | 内容 |
|---|---|---|
| `realbytes.go` | 无 | `From(size, reported uint64, ok bool) (actual uint64, known bool)` 纯函数：`!ok` 或 `reported==0 && size>0` → 回退 `size` 且 `known=false`；否则原样返回。**不做封顶**（§3.0 结论 3）。另给调用方一个入口 `Of(path, size, info)`，把"读一个数"与"判一个数"串起来 |
| `realbytes_unix.go` | `darwin \|\| linux` | 从 `os.FileInfo.Sys().(*syscall.Stat_t)` 取 `Blocks*512`。零额外 syscall：遍历期已经 `de.Info()` 过 |
| `realbytes_other.go` | `!darwin && !linux && !windows` | 恒 `ok=false`（freebsd 等 `Stat_t` 布局不通用）。与 `internal/fsid/fsid_other.go` 同一处置 |
| `realbytes_windows.go` | `windows` | **按路径**调 `GetCompressedFileSizeW`（该 API 本身就收路径，内部以 `FILE_READ_ATTRIBUTES` 打开）；失败（目录、ACL、超 `MAX_PATH` 且未加 `\\?\`）→ `ok=false` 走回退 |

> **§3.1 的一处实施期修正（原设计的前提不成立，留字而不是悄悄改口径）**：
> 原写 Windows 腿"挂在 `internal/fsid` 已打开的句柄上"。**扫描路径上没有那样的句柄**——
> Windows 的遍历阶段刻意不开句柄（每文件开一次代价过高，故身份按需解析，
> 见 `internal/scanner/filekey_windows.go`）。因此改为按路径查询，代价是
> Windows 上每个"通过过滤器的候选文件"多一次元数据查询（采集点排在
> `matcher.Apply` 之后，被过滤掉的文件不付）。这一开销已登记为待评估项（04 §6.9.3
> 的 M29），且"未兑现真机验证"照实记。
> 另：tag 从 `!windows` 收窄为 `darwin || linux` + 一个恒 false 的兜底文件，
> 理由是 `Blocks` 字段在其它 unix 变体上布局不同，宁可不读也不按猜的偏移读。

带 tag 的两个文件只做"读一个数"，判定与回退规则全部在无 tag 层——这样
`From` 的回退分支在 Linux 主门禁里就是可执行断言，Windows 的 NTFS 压缩语义虽然
只能在真机验证，但"读不到就回退并标记"这一条不需要真机就能钉住。

### 3.2 会计改动面

1. `model.FileEntry` 增 `Actual uint64` 与 `ActualKnown bool`（**不进缓存表**，与总纲一致：
   与正确性无关，加列只会让缓存 DB 迁移无谓变大）。
2. `model.DuplicateGroup` 增 `ReclaimableActual uint64`（= 组内冗余成员的 `Actual` 之和），
   **保留** `Reclaimable`（逻辑口径）不动。
   > 为什么不按总纲说的"改用实占"直接把 `Reclaimable` 换掉：它是**已下发过清理的
   > 数字**，也是历史表 `reclaimable` 列的既有语义。同一个字段名在升级前后指向两种
   > 口径，会让"上次扫出 8 GB 这次只剩 300 MB"看起来像回归——而 §6.8 这一整轮
   > 就是在修这类"数字与事实不符"。所以本轮做的是**双口径并存 + 明确谁是权威**：
   > `Reclaimable` 仍是逻辑口径（历史可比），`ReclaimableActual` 是实占（新增、只增不改），
   > M8 的 UI 双数呈现直接读这两个字段。总纲那句"改用实占"在实现层落为
   > "新增实占字段并让界面以它为主"，语义目标一致，风险面小得多。
3. `ScanSummary` 增 `reclaimableActual`；`fdd-cli` 的 `stats` 增 `reclaimable_bytes_actual`。
4. 历史表**不加列**：旧记录没有实占来源，恢复时如实显示"未统计"（与 §2 的
   `ProtectedDirs` 同一处置），不拿逻辑值冒充。

### 3.3 探针（修前必红清单）

| # | 用例（实施后的真名） | 断言 | 修前为什么红 |
|---|---|---|---|
| U1 | `internal/realbytes`：`TestFromFallsBackWhenUnreported` / `TestFromFallsBackWhenPlatformReportsZero` / `TestFromDoesNotCapAtLogicalSize` / `TestFromPassesThroughSparseReported` | `ok=false` → `(size,false)`；`reported=0 && size>0` → 回退；`reported>size` → **原样返回不封顶**；稀疏读数原样通过 | 包不存在 → 编译红（实测读数：`undefined: From` ×6，`[build failed]`） |
| U2 | `TestWalkRecordsSparseActualBelowSize`（遍历腿）+ `TestGroupSparseReclaimsFarLessThanLogical`（组腿） | 64 MiB 洞 + 尾部写 1 KiB → `Actual*4 < Size`、组级 `ReclaimableActual*4 < Reclaimable`，同时 `Size`/`Reclaimable` 一字不动 | 修前无 `Actual` 字段 → 编译红 |
| U3 | `TestWalkActualKnownForPlainFile` | `ActualKnown=true`、`Actual >= Size`、`Actual <= 2×Size + 64 KiB`（只防"读错字段"，不钉块大小） | 同上 |
| U4 | `TestGroupReclaimableActualExcludesKeep` | 3 成员组 → `Reclaimable = 2×Size`、`ReclaimableActual = Σ files[1:].ActualBytes()`，且**不等于**全组成员之和 | 同上 |
| U5 | 平台读数的"只登记不测"边界 | 克隆场景（§3.0 #2/#4）在 CI 上不可造（Go 无 `clonefile` 绑定） → 不写断言，只在 04 记 ID | 不适用 |
| U6 | 实施新增：`TestWalkAllHoleFileNeverReportsZeroActual` | 全洞文件的条目**绝不**以 `Actual=0` 出厂（任何卷上都成立，无需环境探测） | 修前无字段 → 编译红 |
| U7 | 实施新增：`internal/cache`：`TestHashCacheColumnSetIsExactly` | `hash_cache` 列集合逐项等于显式清单（钉住"实占不进缓存表"） | 修前用例不存在 → 不红；它的作用是**让下面第 4 条变异真的会红** |
| U8 | 实施新增：`TestWalkZeroSizeFileStillSkipped` | 0 字节文件不因实占采集而回到语料 | 同上 |

> 稀疏类用例（U2 两腿）带 `requireTailSparse` / `requireU4Sparse` 环境前提核查，
> 不满足时 `t.Skipf`。这**不是**豁免：无稀疏支持的卷上这条断言本身没有意义，
> 而"卷不支持稀疏"不是代码缺陷。按 `scripts/test-windows-quarantine.sh` 头部的既有
> 约定，环境差异在使用点自探，隔离清单只装代码已知缺陷。**留此记录以免将来被当成
> "悄悄跳过"**：本机（APFS）实测为 PASS 而非 SKIP，Linux 门禁同样应 PASS；
> 若哪天它变成 SKIP，要查的是夹具尺寸（§3.0 补充实测的 24 MiB 阈值）。

**变异验证（实施后逐条跑，读数进 04 §6.9.3）**：

| # | 变异（把正确写法改坏） | 应变红的用例 | 实测 |
|---|---|---|---|
| M-P2-a | `From` 里加 `if reported > size { return size, true }`（封顶） | `TestFromDoesNotCapAtLogicalSize` | 红 1 例 |
| M-P2-b | `From` 的 `!ok` 分支改成 `return 0, false`（未知当 0，规则 1） | `TestFromFallsBackWhenUnreported` | 红 1 例 |
| M-P2-b2 | 删掉 `reported==0 && size>0` 整段（规则 2 失效，恒信平台读数） | `TestFromFallsBackWhenPlatformReportsZero`、`TestWalkAllHoleFileNeverReportsZeroActual` | 红 2 例 |
| M-P2-c | `ReclaimActual` 从 `files[0]` 起算（把保留项算进可释放量） | `TestGroupReclaimableActualExcludesKeep` | 红 1 例（`921600` vs `want 614400`） |
| M-P2-d | 给 `hash_cache` 加一列 `actual` | `TestHashCacheColumnSetIsExactly` | 红 1 例 |
| M-P2-e | 把 `e.Actual, e.ActualKnown = realbytes.Of(...)` 从 `matcher.Apply` **之后**挪到**之前** | **预期不红**（Linux 上采集零成本、无语义差异），只作为 Windows 开销的读码约束记录，见 §3.1 修正段 | 不红（三包全 `ok`），预期成立 |

> **本表相对首版的两处修正（留字以免被当成"事后凑红的表"）**：
> 1. 首版把 M-P2-b 的预期写成 3 个用例（含 `TestWalkActualKnownForPlainFile`、
>    `TestWalkAllHoleFileNeverReportsZeroActual`）。实测只红 1 个，**是预期错而非用例弱**：
>    这两个用例的文件走的是 `ok=true` 路径，打 `!ok` 分支（规则 1）根本碰不到它们。
> 2. 顺此暴露出一个真实缺口：`From` 的规则 2（`reported==0 && size>0` → 判不可信）
>    原先**没有任何变异条目覆盖**，即"删掉规则 2"能过全套门禁。补 M-P2-b2 后规则 1
>    与规则 2 各有独立杀手，二者不再互相顶包。

M-P2-e 是这张表里唯一"做不到红"的一条，如实标出而不是删掉：它是本轮设计自我
修正的产物（原前提不成立），约束只能靠代码位置与注释承载。

### 3.4 未兑现与边界

- **Windows 的 NTFS 压缩/稀疏**：`GetCompressedFileSizeW` 那条腿在 darwin/linux 上
  只能到 `go vet` 交叉编译，真机读数一律记为"代码已改、验证未兑现"。
- **APFS/btrfs/ReFS 克隆与块级去重**：本口径系统性高估（§3.0 结论 2），只登记不修。
- **UI 双数呈现属 M8**（裁定 ③）：本轮只到 JSON 与 CLI 字段为止。
- 若某卷 `st_blocks` 语义不可信（部分 FUSE/NFS 报 0）→ 走 `known=false` 回退，
  界面须能区分"实占未知"与"实占=0"，这一点写进字段注释而不是靠猜。
- **全洞文件（一个字节都没写）判为"未统计"，是刻意放弃的一类收益**：`st_blocks==0`
  在"真全洞"与"该卷不跟踪块数"之间单文件不可区分，选边只能选"不新增错误结论"
  这一侧——故障卷上"1 GB 重复组实占 0"是一个自信的错误数字，而退回逻辑口径
  至多是"这一类没拿到收益"。要两者兼得需按卷判 `st_blocks` 可信度
  （`statfs` 的 `f_type` + 每卷一次探测），登记为开放项 M28。
- **APFS 的 16 MiB 落地阈值**（§3.0 补充实测）意味着"小文件稀疏"在本机根本不存在，
  实占=逻辑；这一类不是缺陷，但会让用户在 mac 上看到"两个数一样"，文案须说明。
- **Windows 每候选文件多一次元数据查询**（§3.1 修正段）：真机未测，登记为 M29 待评估。

---

## 4. M6-P1：云占位文件检测（总纲 §2.1 / 04 §6.7 C 组 1）

### 4.0 动手前取证（2026-09-21 本机 + GOROOT/SDK 读数）

**总纲 §2.1 的两条判据前提都不成立**，本稿按实测重写。这不是措辞问题：照原文实施会
引入一次 `getattrlist` 系统调用（本机常量根本不存在）与一次 `DeviceIoControl` 开档
（扫描阶段刻意不开档，与 §3.1 的修正同源）。

| # | 取证 | 读数 | 结论 |
|---|---|---|---|
| E1 | macOS SDK `<sys/attr.h>` 里查 `ATTR_FILE_DATALESS` | 文件存在（27,496 B）、`ATTR_FILE` 命中 **19** 处、`DATALESS` 命中 **0** 处 | 总纲点名的这个属性**不存在** |
| E2 | 同一 SDK `<sys/stat.h>:359` | `#define SF_DATALESS 0x40000000 /* file is dataless object */` | 真身是 **`st_flags` 位**，不是 attribute 查询 |
| E3 | `x/sys@v0.48.0/unix/zerrors_darwin_{amd64,arm64}.go:1286` | `SF_DATALESS = 0x40000000` | 与 E2 独立第二来源相符（值确证）。Go 自带 `syscall`（darwin）**未导出**任何 `SF_` 常量 → 按 `crossdevice_windows.go:10-23` 的先例本地定义并附出处 |
| E4 | `Stat_t`（`ztypes_darwin_{arm64,amd64}.go`） | 有 `Flags uint32` 字段 | 判定值**已在 `de.Info()` 手里**，零额外 syscall |
| E5 | **真机正例**：`~/Library/Mobile Documents/com~apple~CloudDocs/{Desktop,Documents}/.localized` | `flags=0x40008060` → `SF_DATALESS(0x40000000) \| UF_HIDDEN(0x8000) \| UF_TRACKED(0x40) \| UF_COMPRESSED(0x20)`（位名逐条对照 `stat.h:311-359`），`mode=-rw-------`，`size=0`，`blocks=0` | 本机 iCloud Drive 上**确实有 dataless 对象**，且它的 mode 是**普通文件** → `!IsRegular()` 挡不住它（见 E8） |
| E6 | **真机反例**：同一目录里的 `.DS_Store` | `flags=0x00008000`（只有 `UF_HIDDEN`，无 DATALESS 位），`mode=-rw-r--r--`，`size=6148`，`blocks×512=8192` | 同目录同属性形态下位能干净二分；顺带又一个**实占 > 逻辑**的真机样本（佐证 §3.2 不封顶） |
| E7 | 本机普通文件对照：`/etc/hosts`、`go.mod` | `flags=0x0` | 判据不会把本地文件误判为占位 |
| E8 | GOROOT `os/types_windows.go` 读码（非真机） | `newFileStatFromWin32finddata:130-146` 把 `d.FileAttributes` 原样带进 `fileStat`，`Sys():275-286` 回吐 `*syscall.Win32FileAttributeData`；`mode()` 在 `REPARSE_POINT` 置位且 tag 非 symlink/AF_UNIX/DEDUP 时给 **`ModeIrregular`** | Windows 侧标志位同样**零额外 syscall** 可得；且**OneDrive 占位（是 reparse point）今天会被 `!IsRegular()` 静默丢掉**——不是本项目的功劳，是 Go 的 mode 映射顺带 |
| E9 | `x/sys@v0.48.0/windows/types_windows.go:114,122,123` | `FILE_ATTRIBUTE_REPARSE_POINT=0x00000400`、`RECALL_ON_OPEN=0x00040000`、`RECALL_ON_DATA_ACCESS=0x00400000` | Windows 判据取这两个 RECALL 位（MSDN：数据在云端/按需召回）。`REPARSE_TAG_CLOUD` 在该包 **grep 零命中**，也拿不到 tag（`Sys()` 不回吐 `ReparseTag`）→ 总纲的"tag ∈ 0x9000xxxx"这条**实现不了**，改用属性位 |

> **E8 逼出的真危害重述**（比总纲那句"会触发 GB 级下载"更准确，也更难反驳）：
> Windows 上主流占位今天**已经**不会被哈希，但**一个计数都没有**——用户扫 OneDrive
> 看到"0 个重复组"，无从知道是"没有重复"还是"300 个占位文件被静默跳过了"。这与
> §6.7 M21（`.fdd-old` 静默跳过）完全同形，也正是 04 §1 约束 2"计数不许说谎"要修的类。
> macOS 则是**真会下载**：E5 实测 dataless 对象的 mode 是普通文件，一路走到
> `pipeline.go:415 os.Open` + `HashHeadTail` 预筛读（`hasher.go:60-61` 首尾各 64 KiB
> 采样，占位文件会整份召回）。**所以本项的交付重心是"可见 + 可控"，其次才是"省流量"。**

### 4.1 分层与 API 形状（对齐 §1 约束 1，照抄 §3.1 已被验证的形状）

新增叶子包 `internal/cloudfile`，与 `sysguard`/`realbytes` 同族：

| 文件 | tag | 内容 |
|---|---|---|
| `cloudfile.go` | 无 | `type Platform int`（`PlatformOther/PlatformDarwin/PlatformWindows`）；**全部判定** `From(flags uint32, p Platform) bool`；`Of(info os.FileInfo) bool` 只做"取数 + 调 From" |
| `platform_darwin.go` / `platform_windows.go` / `platform_other.go` | 各带 tag | 只有一个 `const Current Platform`（`sysguard/platform_*.go` 的 3 行写法） |
| `flags_darwin.go` | `darwin` | `func flagsOf(info) (uint32, bool)`：`info.Sys().(*syscall.Stat_t).Flags`；断言失败 → `(0,false)`。常量 `sfDataless = 0x40000000` 附 E2/E3 出处 |
| `flags_windows.go` | `windows` | 同上，取 `Win32FileAttributeData.FileAttributes`；本地定义两个 RECALL 位并附 E9 出处 |
| `flags_other.go` | `!darwin && !windows` | 恒 `(0, false)`（Linux 无统一占位语义，见 §4.5 边界） |

`From` 的三条规则（都能在 Linux 主门禁上断言）：

1. `p` 不认识 → `false`。**读不到标志位（`ok=false`）也判 `false`**：与 §3.2 的
   `known=false` 同方向，但这里"保守"意味着**照常扫描**而非跳过——因为错误地
   把一个本地普通文件算成"云端占位跳过"，既虚报计数又吞掉一个真重复候选，
   代价高于"偶尔多下载一份"。这一条选边写进用例注释。
2. darwin：`flags & sfDataless != 0` 才算。**不许写成 `flags != 0`**——E5/E6 显示
   一个**已下载**的 iCloud 文件也带 `UF_HIDDEN`，而 dataless 那个还额外带
   `UF_TRACKED|UF_COMPRESSED`；`flags != 0` 会把整个 iCloud Drive 判成全占位
   （变异 M-P1-b 打这一条，反例正是 E6 的 `0x00008000`）。
3. windows：`flags & (recallOnDataAccess|recallOnOpen) != 0` 才算；单独出现
   `FILE_ATTRIBUTE_REPARSE_POINT`（普通链接/目录 junction 的位）**不算**占位。

### 4.2 接入点与先后顺序（顺序本身就是判据的一部分）

`internal/scanner/scanner.go` 文件分支，**紧跟 `de.Info()` 的错误处理之后**
（现 `:349-353`），即排在 `!info.Mode().IsRegular()`（`:354`）、0 字节跳过（`:358`）、
`matcher.Apply`（`:362`）**三者之前**。三条理由，逐条对应一条变异：

- **在 `IsRegular` 之前**：E8 说明 Windows 占位今天是被 `!IsRegular()` 顺带丢掉的；
  判定放后面就永远数不到它，本项在 Windows 上的主要价值（可见性）直接归零。
- **在 0 字节跳过之前**：E5 的 `.localized` 是 0 字节占位，归到"云占位"比归到
  "0 字节"更有解释力（两者都不进语料，只是账记在哪一类）。
- **在 `matcher.Apply` 之前**：用户用扩展名过滤掉的文件，如果它本身是占位，
  真实原因仍是"在云端未下载"。放后面会让"被过滤"吞掉"被跳过"，计数随过滤规则漂移。

命中处置：`cloudSkipped[idx]++` 后 `continue`；**不记 `FailedItem`**（同 §2.5 与
`scanner.go:344` 的既有纪律："它不是失败"）。接缝 `var cloudCheck = cloudfile.Of`
（与 `var guard = sysguard.New(...)`（`:100`）同一手法），测试注入假判据。

开关：`AllowCloudHydration` 落 `model.Filters`（`model.go:32-39`）。三条理由：
① Go **零值 `false` 恰是安全默认**——旧 `settings.json`、旧历史行（`filters` 是 JSON
blob，`history/scan.go:20`）、前端 `emptyFilters()` 缺字段，三种情形全部解成"跳过占位"，
不需要任何迁移代码；② 复用时序最短（`app.go:507 StartScan` → `pipeline.go:266` →
`scanner.WalkWithGate(&cfg.Filters)`）；③ 语义上 `IncludeHidden` 也是住在这里的行为开关。
`true` 时**完全不判定**（不数、不跳，行为与现状逐字节一致），文案须写"会产生下载流量与耗时"。

### 4.3 计数与下发面（照 ProtectedDirs 的 17 跳表，见 §4.6 V-线程）

`SkippedCloudFiles int`：per-worker 切片（`scanner.go:234` 旁）→ 收口累加（`:411` 旁）
→ `Result` → `pipeline.go` `atomic.Uint64` + 每轮归零 + 访问器 → `app.go` 读入
`ScanSummary.SkippedCloudFiles json:"skippedCloudFiles"` → `scan:done` → `fdd-cli`
`stats.skipped_cloud_files`。**只数文件数**：没有下潜判定，折算字节数就是编数（§1 约束 2）。
历史恢复路径**不填**这个数（`LoadScanHistory` 那三个计数同样没填，`app.go:1488-1491`），
界面显示"未统计"。

### 4.4 探针（修前必红清单）与变异表

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `internal/cloudfile`：darwin 位判定、windows 位判定、`PlatformOther` 恒 false、**组合位不误判**（喂 E5 真读数 `0x40008060` 与 E6 反例 `0x00008000`） | 平台以参数注入，**不看宿主 GOOS**（`sysguard_test.go:3-7` 的既有纪律） | 包不存在 → 编译红 |
| V2 | `scanner`：注入假判据（按路径后缀命中）→ `SkippedCloudFiles==N`、`len(res.Files)` 少 N、`Failed` 为空 | 同 M6-P4 的"命中不是失败" | 无 `cloudCheck` 接缝 → 编译红 |
| V3 | `scanner`：判据排在 `IsRegular` **之前**的顺序不变量。夹具用 `syscall.Mkfifo` 造一个**非普通文件**并让假判据认它 | 该文件被计成"云占位"而不是被 `!IsRegular` 静默丢掉 → 计数 1 | 同上 |
| V4 | `scanner`：`SkippedCloudFiles` 不随 `IncludeHidden` 漂移（§2.8 T9 的同族不变量） | 两次扫描计数相等 | 同上 |
| V5 | `scanner`：`AllowCloudHydration=true` → 计数 0、占位文件正常进语料 | 开关双向生效 | `Filters` 无该字段 → 编译红 |
| V6 | `scanner`：占位判定优先于扩展名过滤（给占位文件一个被 `-exclude` 排除的后缀） | 计数仍为 1 | 同上 |
| V7 | darwin 真机腿（`//go:build darwin`）：对若干真实文件比对 `Of(info)` 与**独立直读** `syscall.Stat` 的 `Flags&sfDataless`，并断言两者一致；找不到 dataless 样本时退化为"读的是不是这个字段"的一致性断言 | 钉住 `flags_darwin.go` 那条"读一个数"的腿读的是正确字段 | 无该文件 → 编译红；无 iCloud Drive 的使用点自探 `t.Skipf`（不进隔离清单，§7 约定） |

| # | 变异 | 应变红的用例 |
|---|---|---|
| M-P1-a | darwin 分支恒 `false`（忽略 SF_DATALESS） | V1 |
| M-P1-b | darwin 判据放宽为 `flags != 0` | V1 组合位用例（E6 反例） |
| M-P1-c | 云判定挪到 `!IsRegular()` **之后** | V3 |
| M-P1-d | 挪到 `matcher.Apply` 之后 | V6 |
| M-P1-e | 命中时同时写 `FailedItem` | V2 |
| M-P1-f | `AllowCloudHydration` 语义反转（true=跳过） | V5 |
| M-P1-g | 计数改在目录分支累加（把"跳过文件数"数成目录数） | V2/V4 的精确值断言 |

**本表相对首版的两处修正**（2026-09-21 实施后按真读数回填，04 §6.9.4 有完整变异表）：

1. **M-P1-i 的预期写错了，补 M-P1-i2**。首版把"接缝接错"写成会红掉 V2~V6；实测
   `var cloudCheck = func(os.FileInfo) bool { return false }` 得到的是
   `FAIL filededup/internal/scanner [build failed]`——`cloudfile` 在 `scanner.go` 里
   只被这一处引用，改成常量闭包就成了未使用 import，编译器先拦下。要测出**行为**红
   必须让变异体仍可编译，故补 **M-P1-i2**（`cloudfile.From(0, cloudfile.Current)`，
   看着在调真判据、实为恒 false）：只有 `TestCloudCheckSeamUsesRealJudgment` 的函数指针
   断言认得它（真读数 `cloudCheck 未接到 cloudfile.Of（got=1044c7f90 want=1044ba320）`，
   其余用例全绿）。这条断言的存在理由与 §3.3 的 M-P2-b2 同源：注入点覆盖一切时，
   出厂接线没人查。
2. **§4.2 承诺"三条顺序理由逐条对应变异"，首版表里只给两条**（M-P1-c、M-P1-d），
   0 字节那条既无断言也无变异。补法：V2 夹具放一个 0 字节占位（机型取自 E5 的
   `.localized`）+ 新增 **M-P1-h**（判定挪到 0 字节跳过之后）。实测它红在
   `SkippedCloudFiles = 3, want 4`，但**不隔离**——这一挪顺带越过了 `IsRegular`，
   所以 V3 同时红；它的独占断言只有那条 4→3。

九条变异（a/b/c/d/e/f/g/h/i2）全部在 detached worktree 上取到真读数，逐条见 04 §6.9.4。

### 4.5 未兑现与边界

- **Windows 真机未兑现**：RECALL 属性位是否出现在 `FindFirstFile` 的
  `dwFileAttributes` 里，是 E8 的读码推论而非读数；无 OneDrive 账号、无 Windows
  runner。兑现方式与 §3.4 第一条同：`GOOS=windows go vet` + V1 的参数化断言，
  划账写"代码已改、验证未兑现"。
- **Linux 恒不跳过**（总纲同口径）：rclone/gvfs/lookback 的占位没有统一语义。
  这不是"覆盖了但没命中"，是**平台未覆盖**，必须在文档里这么写。
- **macOS 真机正例只有 0 字节样本**（E5 是 `.localized`）：本机 iCloud Drive 里
  没有非空 dataless 文件，所以"省下一次真实下载"在本机**未被读数证明**，
  只证明了位可读、可二分、mode 是普通文件。
  **实施后复测确认**（V7 真机腿 `-v` 读数）：`真机取样 3 个文件，其中 dataless 占位 2 个`，
  两个都是 0 字节 `.localized`——首版的这条判断成立，且现在有用例把它变成每次跑都出数的
  常驻断言（M-P1-a/b 两条变异同时红到 V7，说明这条腿真的有鉴别力）。
- **CLI 级端到端只在临时语料上验过字段下发**（实读 `"skipped_cloud_files": 0`）：
  扫真云盘会把目录下**非占位**文件全部预筛采样（`os.Open`+`ReadAt`），等于替用户批量
  拉取云端内容，故**刻意不做** → 登记 04 §6.8.8 **M31**。
- **`wails.ts` 的 `ScanSummary` 缺四个后端字段**（`protectedDirs`/`protectedFiles`/
  `reclaimableActual`/`skippedCloudFiles`）：属 M8 开工时一次对齐的既有欠账，
  登记 **M30**，本轮不因"顺手"而补（补了无人读取，只会把"类型齐全"变成新错觉）。
- **判定不覆盖 ADS/扩展属性、不改 `!IsRegular()` 的既有静默跳过**（除 V3 那一类
  被本项认领的占位外，其余 irregular 文件仍无声丢弃 → 属 M21 同族的既有开放项，不并案）。
- **UI 未呈现**（裁定③）：`skippedCloudFiles` 只到 JSON/CLI。前端仅补
  `Filters` 类型字段与默认值（`wails.ts:4-11`、`scan.ts:13-20`），理由是**防止
  settings 往返静默丢字段**（前端把整个 `filtersDefault` 写回），这不是呈现层改动。
  结果页"已跳过 N 个云端占位文件"横幅与 `AllowCloudHydration` 的复选框属 **M8**。

---

## 5. M6-P3：Windows 备用数据流（ADS）防护（总纲 §2.3 / 04 §6.7 C 组 3）

> 修的是什么：NTFS 上一个文件可以挂多条数据流（`report.txt` + `report.txt:note:$DATA`）。
> 本产品的**全部判据只看默认流**（扫描按逻辑大小、哈希按默认流内容），于是两个"默认流
> 逐字节相同、备用流完全不同"的文件会被判成重复；把其中一份移走/删除/换成链接，它自己
> 那些命名流就**永久消失**。这不是"少省一点空间"，是**静默丢用户数据**。

### 5.0 动手前取证（2026-09-21，MSDN 原文 + GOROOT 读数）

| # | 取证项 | 读数 | 出处 |
|---|---|---|---|
| E1 | 无 cgo / 无 x/sys 的前提下调 Win32 的既有路子 | 仓库已有 **5 个文件**在用 `syscall.NewLazyDLL(...).NewProc(...)`：`internal/fsid/fsid_windows.go:56-59`、`internal/realbytes/realbytes_windows.go:29-30`、`internal/ops/winreg_windows.go:39-45`、`internal/ops/trash_windows.go:48-53`、`internal/ops/symlink_windows.go:34` | 本机 grep |
| E2 | 需要的三个 stdlib 支撑点是否齐 | `syscall.FindClose(handle Handle) error` **已导出**；`syscall.InvalidHandle = ^Handle(0)`；`syscall.UTF16PtrFromString` | GOROOT `src/syscall/zsyscall_windows.go:607`、`syscall_windows.go:265,23,113` |
| E3 | 枚举 API 签名 | `HANDLE FindFirstStreamW(LPCWSTR lpFileName, STREAM_INFO_LEVELS InfoLevel, LPVOID lpFindStreamData, DWORD dwFlags)`；`FindStreamInfoStandard = 0` 是**唯一**取值；`dwFlags` 保留、必须为 0；失败返回 `INVALID_HANDLE_VALUE`，扩展错误看 `GetLastError` | MSDN `fileapi/nf-fileapi-findfirststreamw` |
| E4 | 两个"必须放行"的错误码 | **无流可找 → `ERROR_HANDLE_EOF`(38)**；**文件系统不支持流 → `ERROR_INVALID_PARAMETER`(87)** | 同上（Return value 段两条原文） |
| E5 | 默认流长什么样 | Remarks 原文："For files, this is **always** the default, unnamed data stream, `::$DATA`"；目录没有默认无名流，但可以有命名流 | 同上 |
| E6 | 流名字符串格式 | Members 原文：`The name of the stream. The string name format is ":streamname:$streamtype"` | 同上 + `ns-fileapi-win32_find_stream_data` |
| E7 | ★ 缓冲区结构的**真实**布局（本项最容易写错的一处） | `typedef struct _WIN32_FIND_STREAM_DATA { LARGE_INTEGER StreamSize; WCHAR cStreamName[MAX_PATH + 36]; }` → **8 + 296×2 = 600 字节，名字在 offset 8** | MSDN `ns-fileapi-win32_find_stream_data` |
| E8 | 循环终止条件 | `BOOL FindNextStreamW(HANDLE, LPVOID)`：成功非零、失败零；**"If no more streams can be found, GetLastError returns `ERROR_HANDLE_EOF` (38)"** → 终止靠 **38**，不是 `ERROR_NO_MORE_FILES`(18) | MSDN `nf-fileapi-findnextstreamw` |
| E9 | **接入点只有一处**（总纲 §2.3 写"`ops/verify.go` 执行前"，实测更精确） | 五种 `op.Kind`（`trash`/`delete`/`move`/`hardlink`/`symlink`，`executor.go:359`）**全部**只处理进了 `toProcess` 的项，而 `toProcess` 唯一来源是 `:213-241` 那道校验循环 → 守卫加在这一处即覆盖全部破坏性动作，不必五处各加一遍 | 读码 |
| E10 | 到底哪一侧会丢流（决定"守 dup 还是守 keep"） | 被操作的 dup：`trash`/`delete` 连文件记录一起消失；跨卷 `move` 走"复制+删源"，复制只覆盖默认流；`hardlink`/`symlink` merge 把 dup 路径换成指向 keep 的链接，**dup 自己的文件记录连同其命名流一起被替换**。keep 侧：合并后 dup 路径即 keep 的文件记录，keep 的全部流原样在 → **只守 dup**（`executor.go:516` 那次 `VerifyFile(src,…)` 是内容校验，不涉及销毁） | 读码 |

**E7 值得单独记一笔**：动手前我对这个结构的印象是"内嵌一份 `WIN32_FIND_DATAW`"
（那样 `cStreamName` 在 offset 52，且总尺寸是 8+592=600 的另一套算法碰巧同值）。查文档
才确认它是**扁平**的 `{LARGE_INTEGER; WCHAR[296]}`。两种猜法的差别只在偏移，而**猜错偏移
的表现恰好是最坏的一种**：从 offset 52 读到的是零长字符串 → "没有任何命名流" →
守卫在 Windows 上**恒放行**，全套门禁全绿（判据层是纯函数，测的是名字列表，
不知道胶水递上来的是一串空）。所以本项除尺寸钉（照 `fsid_windows.go:41-53` 的
`_ [600 - unsafe.Sizeof(...)]byte` 手法）之外，还必须有一条"真机上枚举到的第一个名字
必须是 `::$DATA`"的钉（§5.4 V8），否则 E7 这类错在 Linux 上无从暴露。

### 5.1 分层与 API 形状（对齐 §1 约束 1）

`internal/ads/`，与 `sysguard`/`realbytes`/`cloudfile` 同族：**全部判定在无 tag 文件里**，
带 tag 的文件只 `proc.Call` 一次并把 `errno` 交给无 tag 层分类。

```go
// ads.go（无 tag）——判据与分类表，Linux 主门禁里全部可执行
func StreamIsNonDefault(name string) bool          // E6 格式：":<名字>:$<型别>"，中段非空即是
func HasNonDefault(names []string) bool             // 任一为真
type ErrKind int                                     // ErrNone/ErrNoStreams/ErrFSNoStreams/
                                                     // ErrMissing/ErrDenied/ErrOther
func Classify(errno uintptr) ErrKind                 // 38/87/2,3/5/其余——E4 的表在**这一层**
func Decide(names []string, k ErrKind) Outcome       // Allow / Reject(reason)，fail-closed 表在此
const ( errHandleEOF = 38; errInvalidParameter = 87; errFileNotFound = 2
        errPathNotFound = 3; errAccessDenied = 5 )   // 本地抄录并附出处（同 crossdevice_windows.go）

// probe_windows.go（//go:build windows）：FindFirstStreamW + FindNextStreamW 循环，
//   命中非默认流即提前停（不必枚举完）；FindClose 收尾；只把 names 与 errno 交上去。
// probe_other.go（!windows）：不枚举，直接 (nil, ErrNone) + 一条"本平台无 ADS 语义"的注释。
```

三层分离的理由：**"哪些错误码要放行"是安全决策，不是平台细节**。把它放在无 tag 层，
`ErrDenied → Reject` 这条在 Linux CI 里就是可断言的（M-P3-e 变异专门打它）；放在
windows 文件里就等于"这条决策只有 Windows runner 能测"，而本仓没有。

`Decide` 返回带**原因文案**的 `Outcome`，两档文案（总纲给的是第一条）：

- 命中备用流：`文件含备用数据流，去重会丢失备用流内容，已拒绝操作`
- 无法确定（`ErrDenied`/`ErrOther`）：`无法确认文件是否存在备用数据流（%s），为避免丢失其内容已拒绝操作`

**fail-closed 表**（这是本项最需要写清楚的取舍）：

| `ErrKind` | 处置 | 为什么 |
|---|---|---|
| `ErrNone`（枚举成功） | 按 `names` 判 | 正常路径。文件的第一个流恒为 `::$DATA`（E5） |
| `ErrNoStreams`(38) | **Allow** | 一个 `$DATA` 流都没有 = 无从谈起备用流。文件侧罕见（E5 说恒有默认流），主要出现在目录与竞态 |
| `ErrFSNoStreams`(87) | **Allow** | **文件系统不支持流**：exFAT/FAT U 盘、部分网络盘。判拒绝等于让这些卷上"每次清理都被拒"，而它们本来根本不可能有 ADS |
| `ErrMissing`(2/3) | **Allow** | 文件已消失。既有语义里这属 `VerdictSkipped` 的邻居（S8：目标已达成），且紧接着的 `MoveFile`/`Trash` 会给出自己的真实错误。**不放行**就会把"文件不见了"报成"有备用流"，属于说谎 |
| `ErrDenied`(5) | **Reject** | 无法确定。与 `identityStill` 的同族决策一致（`verify.go:104-108` 注释原文："宁可拦一次让用户重扫，也不放行一次可能覆盖他人文件的操作"） |
| `ErrOther` | **Reject** | 未知即不赌。丢数据不可回撤，多拦一次可重扫 |

长路径：`FindFirstStreamW` 的 `lpFileName` 文档写"fully qualified file name"，而
`\\?\` 之外的路径是否受 MAX_PATH 限制**未文档化**（本仓 `realbytes_windows.go:46` 已经把
"超过 MAX_PATH 且未加前缀"列为已知失败形态，且 `longPathAware` 真机未复测，见 01 勘误表）。
处置：**不改判据**，让 2/3 走 `ErrMissing → Allow`，并在划账里明写"长路径上这条守卫可能
静默失效（fail-open），与 M29/`longPathAware` 同一兑现缺口"。刻意**不**在此处加
`\\?\` 重试：那会引入"前缀规范化对不对"这一整块新面积（UNC、相对段、`..`），
而本项的动机场景（NTFS 备用流）与 >260 路径的交集很小，赌错的代价是放行——
与"多加一层未验证的字符串处理"相比，前者更可接受。**这条判断本身登记为开放项**（M33），
不当成已解决。

### 5.2 接入点与计数

`internal/ops/executor.go` 校验循环的 `default:` 分支（`:236` 旁）之前插一道守卫，
即"内容校验已通过、正要进 `toProcess`"的那一刻：

```go
default:
    if oc := adsCheck(e.Path); oc.Reject {      // 接缝 var adsCheck = ads.Check（测试注入）
        res.Failed = append(res.Failed, model.FailedItem{Path: e.Path, Stage: "ads", Err: oc.Reason})
        emitItem(ItemResult{OrigPath: e.Path, State: "failed", Err: oc.Reason})
        report(e.Path)
        continue
    }
    toProcess = append(toProcess, e)
```

- **记 `Failed` 而不是新增计数**：这里与 M6-P1 相反，因为语境不同——扫描期的"跳过"
  是引擎替用户省流量（用户没要求操作），执行期的"拒绝"是**用户已经点了清理、这一项
  没做成**。后者必须进失败抽屉，且 `Stage` 用新值 `"ads"` 而非复用 `"verify"`，
  否则"文件被改过"与"有备用流"两种处置建议完全不同的原因会被并成一条。
- 不 `emitItem` 就等于说谎：写前日志（`OnItem`）是回撤账本的来源，拒绝项不入账。
- `res.Reclaimed` 天然不受影响（没进 `toProcess` 就不参与落账），但**必须有用例钉住**
  （M-P3-h：把拒绝记成 `Skipped` 会污染释放口径）。
- 下发面零改动：`OpsResult.Failed` 与 `ItemResult.Err` 已经透传到界面（M15/M18 的
  `utils/opdisplay` 只格式化字节数，文案原样显示）。
- **不加扫描期计数**（"这个盘上有 N 个文件带备用流"）：`FindFirstStreamW` 每文件一次
  枚举不是免费的（`realbytes` 那次额外查询已经登记为 M29），而 ADS 只在**动手时**才致命。
  登记为 M34，不当成本项已交付。

### 5.3 与既有判据的关系（不改的东西，写清楚免得将来被当漏写）

- **不改哈希判据**：备用流内容不参与 BLAKE3，所以"默认流相同即重复"这一条**今天仍然成立**，
  本项只在动手前拦一道。真要把备用流算进身份属另一个量级的工作（每文件两次枚举 + 
  判据重做），登记 M35。
- **不改 `!IsRegular()`**：NTFS 上目录也可能有命名流（E5），但目录进不了语料，无从操作。
- **macOS 的资源分叉 / Linux 的 xattr 不在本项范围**：HFS 时代的 `._` AppleDouble 是
  **独立文件**（会被当普通文件参与去重，那是另一个问题）；xattr 随 inode 走，硬链接共享、
  删除才消失，与 NTFS"命名流随文件记录一起替换"的语义不同。`probe_other.go` 明确写
  "本平台无此语义"，不猜。

### 5.4 探针（修前必红清单）与变异表

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `ads`：`StreamIsNonDefault` / `HasNonDefault` | `::$DATA`→假；`:note:$DATA`、`:Zone.Identifier::$DATA`、`:report:$BITMAP`→真；空串/无冒号/只有尾部 `:$DATA` 的畸形串→假（**宽判据但不误伤默认流**） | 包不存在 → 编译红 |
| V2 | `ads`：`Classify` 六个 errno | 38→`ErrNoStreams`、87→`ErrFSNoStreams`、2 与 3→`ErrMissing`、5→`ErrDenied`、997→`ErrOther` | 同上 |
| V3 | `ads`：`Decide` 的 fail-closed 表（**逐格**） | Allow/Reject 两两不混；`ErrDenied`/`ErrOther` 必须 Reject 且文案含"已拒绝" | 同上 |
| V4 | `executor`：注入"有备用流"→ 该项 `Failed`、`Stage=="ads"`、文案精确、**文件仍在原位** | 拦得住且真不动手 | 无 `adsCheck` 接缝 → 编译红 |
| V5 | `executor`：注入 `Allow` → 照常执行（反向对照，证明 V4 不是恒拒） | 文件被移走/进回收站 | 同上 |
| V6 | `executor`：**五种 Kind 逐个跑**（`trash`/`delete`/`move`/`hardlink`/`symlink`），每种都必须被拦 | 钉住 E9"接入点只有一处"的结论 | 同上 |
| V7 | `executor`：拒绝项不进 `res.Reclaimed`/`res.LinkedBytes`，且 `OnItem` 收到 `State:"failed"` | 释放口径与回撤账本不被污染 | 同上 |
| V8 | `//go:build windows`：真机 NTFS 上 `StreamIsNonDefault(第一个流名) == false`，且**名字以 `::$DATA` 结尾** | 钉住 E7 的偏移：读错偏移会拿到空串而全绿，这一条专门不让它绿 | 无 runner → 只到 `GOOS=windows go vet`，**未兑现** |

| # | 变异 | 应变红 |
|---|---|---|
| M-P3-a | `StreamIsNonDefault` 恒 false（判据失效） | V1、V4、V6 |
| M-P3-b | 恒 true（默认流也算备用流） | V1、V5 |
| M-P3-c | 默认流识别改成大小写敏感（`::$data` 不再认作默认） | V1 的大小写用例 |
| M-P3-d | `Classify(87)` 归 `ErrOther`（"该卷不支持流"当成异常） | V2、V3（exFAT 上恒拒就是这条） |
| M-P3-e | `Classify(5)` 归 `ErrNoStreams`（拒绝读权限时**放行**） | V2、V3 —— 数据丢失方向，必须有独立杀手用例 |
| M-P3-f | `Decide` 对 `ErrMissing` 改为拒绝 | V3 |
| M-P3-g | 守卫只写在 `case "trash"` 分支里（即"接入点唯一"这个结论是错的） | V6 的 `move`/`hardlink`/`symlink` 三条 |
| M-P3-h | 拒绝记成 `res.Skipped`（把"没做成"报成"已达成"） | V4 的 `Failed` 断言、V7 的落账断言 |

### 5.5 未兑现与边界

- **Windows 真机全部未兑现**（V8 与 `probe_windows.go` 本体）：无 runner，且**造夹具本身
  也需要 NTFS**（`echo x > f.txt:note` 在 APFS/exFAT 上不会创建命名流）。兑现方式与
  §3.4、§4.5 同档：`GOOS=windows go vet` + 无 tag 层的参数化断言，划账写"代码已改、
  验证未兑现" → 登记 **M32**。
- **长路径上可能 fail-open**（§5.1 末段）：>260 且未加 `\\?\` 时枚举报 2/3，按 `ErrMissing`
  放行。刻意接受这个缺口，登记 **M33** 与 `longPathAware`（01 勘误表）同一兑现族。
- **备用流不进身份判据**：本项只拦"动手那一步"，两个默认流相同、备用流不同的文件
  仍会被**报成重复组**（用户看到"重复"，只是清理被拒）。登记 **M35**。
- **不做扫描期 ADS 普查**：理由见 §5.2 末条，登记 **M34**。
- **UI 文案未定制**（裁定③）：`Stage=="ads"` 会随既有失败抽屉原样显示中文原因，
  但界面上没有"什么是备用数据流 / 怎么办"的解释位，属 M8。

---

## 6. C1：fscase 单根探测缺口（04 §6 C 组 1）

> 修的是什么：遍历器用「路径折叠」判断两个目录是不是同一棵（`visited` 去重），
> 而折叠与否来自**按卷探测的大小写语义**。探测只在 **≥2 个根**时执行，单个根直接
> 取平台默认——在 **大小写敏感**的卷上，这会把「`alpha/` 与 `ALPHA/` 两棵不同子树」
> 折叠成一棵，**后遇到的那棵整棵不进语料**，且**一条失败都不记**（`files_failed=0`）。
> 用户看到的是"文件凭空少了一批"，没有任何可追的线索。

### 6.0 动手前取证（2026-09-21，本机实测 + 读码）

**夹具本身是一个取证难点**（大小写敏感卷无法在普通目录里造出来），本节前半段就是
把夹具跑通的记录：

| # | 取证项 | 读数 | 出处 |
|---|---|---|---|
| E1 | 本机能不能造出**真**大小写敏感卷，且不需要 root | 能。`hdiutil create -size 20m -fs "Case-sensitive APFS" -volname FDDSEN` → rc=0；`hdiutil attach … -mountpoint <dir> -nobrowse` → rc=0，`mount` 回显 `(apfs, local, nodev, nosuid, journaled, …, mounted by just)`。且**挂载点可以在任意普通目录下**（挂进一个不敏感目录 → 得到"不敏感树里嵌一棵敏感卷"这个关键构型） | 本机执行 |
| E2 | 夹具放 `/private/tmp` 下会怎样 | **扫到 0 个文件**：`/private` 在 macOS 系统保护清单里（`internal/sysguard`），根自身放行、其下子项全被剪枝。误判为"代码问题"会白查一轮——**做扫描类取证要避开 `/private`、`/System`** | 本机执行（第一次读数 0） |
| E3 | ★ 单根 = 敏感卷的根，含 `c1root/{alpha,ALPHA}` 各 1 个文件（真值 2） | `files_total=1`，`files_failed=0` —— **少了一棵子树且零报错** | `fdd-cli` 真读数 |
| E4 | 对照：把 `alpha` 与 `ALPHA` 作为**两个根**（真值 2） | `files_total=2` —— 探测分支本身是**对的**，缺的只是"单根不走它" | 同上 |
| E5 | ★ 单根 = **不敏感父目录**，其子树里挂着那棵敏感卷（真值 2） | `files_total=1` —— 这个构型是下面推翻方案 A 的关键（探根本身只会得到"不敏感"） | 同上 |
| E6 | 两个根（不敏感父目录 + 另一棵无关目录，真值 3） | `files_total=2` —— 同族缺口在 ≥2 根路径上**依然存在**（父根探测为不敏感 → 其子树里的敏感卷仍被折叠）→ 登记 **M36** | 同上 |
| E7 | 折叠的判据从哪来 | `dedupeRoots`（`scanner.go:479-523`）**单根早退**：`len(out) <= 1` 时 `sens[i] = fscase.Default()`，注释写"单根无从判重，也就不必为它写探测文件"；而 `folder.fold`（`scanner.go:144-154`）与根自身键 `foldRoot`（`:156-158`）**照用这个值** | 读码 |
| E8 | 单根会不会出现"同一目录的两种拼写"（折叠的唯一价值） | 不会。队列只投 `cleaned` 原样根（`scanner.go:420-422`），其后每个路径都由 `filepath.Join(父, ReadDir 得到的名字)` 生成（`:292`）；符号链接与 junction 已被 `:294` 的 `typ&fs.ModeSymlink` 跳过（Go 把 junction 归为 name-surrogate 重解析点并映射成 `ModeSymlink`，GOROOT `src/os/types_windows.go:154-157,204-207`）。**单根出发的遍历里，每个目录只会以唯一拼写出现** | 读码 + GOROOT |
| E9 | 折叠错了的两种后果**不对称** | 折错（把两棵折成一棵）→ 静默少收，**下游没有任何一层能补**；不折错（该折没折）→ 同一目录收两遍，但**阶段 1.5 会按物理身份去重**（`pipeline.go:306-328` 按 `scanner.ResolveKey` 建 `seen` 表，unix 的键来自 `lstat`，`filekey_unix.go:13`）→ 结果集不虚增，只多走一趟目录 | 读码 |
| E10 | 既有用例钉的是哪一侧 | `TestWalkSingleRootSkipsProbe`（`scanner_i2_i3_test.go:187-203`）钉住"单根**不探测**"（`probed==0`）。**这条断言本身是对的**（见 §6.1：单根确实不需要探测），要改的是它背后的理由，不是结论 | 读码 |

**E5 值得单独记一笔**：它同时说明了"按根探测"这件事的天花板——**探测只能描述根本身
所在的卷**，而一棵不敏感的树里完全可以嵌着一棵敏感卷（本机就能造：把敏感卷挂进任意
普通目录）。所以"单根 → 也去探测一下根本身"这个方向即使做了，E5 这个构型仍然是漏的
（探出来是"不敏感"，于是照样折叠、照样丢子树）。这正是下面方案 A 被推翻的地方。

### 6.1 判据：折叠是干什么的，单根为什么不需要它

折叠（`fscase.Fold`）把路径归一到"同一棵树的同一个键"。它在本代码里只有一个真实用途：
**把两种拼写指向同一棵树的路径并成一个键**。而按 E8，单根出发的遍历里不存在第二种拼写。
于是对单根而言折叠**没有可并之物**，只剩一种可能的作用：把**大小写不同的两棵不同目录**
误并成一棵（E3/E5 的读数就是它的后果）。

两种错法的代价不对称（E9）：折错 = 静默丢语料且不可恢复；不折错 = 同一目录多走一遍，
结果被阶段 1.5 的物理身份去重吸收。**因此"不确定时倾向不折叠"才是安全侧**——
这也解释了为什么本项的修法不是"补一次探测"而是"取消单根折叠"。

### 6.2 三个候选方案与取舍

| 方案 | 形状 | 评价 |
|---|---|---|
| **A. 无条件对每个根探测**（04 §6 C 组 1 原登记的最小修法） | 删掉 `dedupeRoots` 的单根早退 | **被 E5 推翻**：探测只描述根本身的卷，"不敏感树里嵌敏感卷"这个构型照旧丢子树。而且它给最常见的场景（只选一个目录）**新增一次往用户目录写探测文件**的副作用，收益却是零——E3/E5 两种构型里它只治得了 E3 |
| **B. 单根不做折叠**（选定） | `newFolder` 对 `len(roots)<=1` 直接返回（`needFold=false`），`foldRoot` 也遵循 `needFold` | 把 E3/E5 **两种**构型一次治好；**零 I/O**（不写探测文件，`TestWalkSingleRootSkipsProbe` 的断言继续成立，只需改注释里的理由）；不引入任何新的"猜卷语义"——它不是猜，而是**认定单根遍历没有可并之物**（E8） |
| **C. 遍历键一律不折叠**（折叠只留给"合并用户给的根"） | `folder.fold/foldRoot` 全去掉折叠，探测只为 `dedupeRoots` 服务 | 更彻底（连 E6/M36 一并治好），但它改的是 I2 当年定下的机制，`TestFolderFoldPerRoot` 那条断言会被整条推翻。**本轮不做**：E6 与 C1 的登记范围不同（它需要 ≥2 根且存在嵌套敏感卷），按 §1 约束 5 登记 **M36** 而不是扩大改动面 |

方案 B 的一处**已知不美**：单根时 `dedupeRoots` 仍会把 `Default()` 填进 `sens` 并交给
`newFolder`，而 `needFold=false` 后这个值不再参与折叠。保留它是因为该返回值与多根路径
共用同一条签名；它是**诚实值**（"我们没测"），不是假装测过。改动处会写明这一点。

### 6.3 改动面（`internal/scanner/scanner.go`，两处 + 一条注释）

1. `newFolder`：`len(roots) <= 1` → 直接返回（不设 `needFold`）。
2. `foldRoot`：`!f.needFold` 时返回根原样（否则单根的 `visited` 种子键仍会被 `Default()`
   折叠，与 `fold()` 的恒等行为打架）。
3. `dedupeRoots` 单根分支的注释：说明"不探测"的理由不是"无从判重"，而是"单根遍历里
   每个目录只会以唯一拼写出现（设计稿 §6.1）"。

不动：`fscase` 包、`dedupeRoots` 的判定逻辑与返回值、`probeCaseSensitive` 接缝、
`TestWalkSingleRootSkipsProbe` 的断言。

### 6.4 探针（修前必红清单）与变异表

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `scanner`：`newFolder` 单根**不折叠**（无 tag、不碰盘、不需要敏感卷） | 单根 + `sens=[false]` 时：`fold(root/子/名)` 原样返回、`foldRoot(0)` 原样返回、`needFold==false` | 修前 `fold` 把小写化当默认行为 → 三条断言全红 |
| V2 | `scanner`：两根时**照旧按探测折叠**（反向对照，证明 B 不是"取消折叠"） | 单根与两根的行为差异只在根数上：两根 + `sens=[false,…]` 时 `fold` 仍折叠 | 绿（回归保护，防 M-C1-c） |
| V3 | `scanner`：单根扫**真**大小写敏感卷，`alpha/` 与 `ALPHA/` 两棵子树都要收齐 | 2 个文件（E3 构型）；无敏感卷时按 E1 的方法自建卷，自建不可行才 skip | 修前只收 1 个（E3 真读数） |
| V4 | `scanner`：`TestWalkSingleRootSkipsProbe` 保持绿 | 单根**仍然不写探测文件**（`probed==0`） | 绿（B 方案必须保持它；方案 A 会把它变红） |

| # | 变异 | 应变红 |
|---|---|---|
| M-C1-a | 去掉 `newFolder` 的单根早退（回到"单根也按 `sens` 折叠"） | V1、V3 |
| M-C1-b | `foldRoot` 不理会 `needFold`（仍走 `fscase.Fold`） | V1 的 `foldRoot` 断言 |
| M-C1-c | 条件写反（`len(roots) > 1` 才早退，即"只有单根折叠"） | V2、`TestFolderFoldPerRoot`、`TestDedupeRootsFoldsByProbedVolume` 中至少一条 |
| M-C1-d | `needFold` 恒为 true（等于回到修前语义） | V1 |

**实施后追记（一处预测修正，2026-09-21）**：V3 的"无敏感卷时按 E1 的方法自建卷，自建不可行
才 skip"**没有落地**，实现是**直接 skip**（`scanner_i2_i3_test.go` 里 `t.Skipf`，判据为 `fscase.Sensitive(root)`）。
理由：自建卷要 `hdiutil` + 挂载 + 清理，把它塞进单元测试会让主门禁依赖"这台机器能不能挂载"
这一外部状态，与 §6.8.3 K 批"门禁不许假绿"的方向相反。代价是**本机变异表里 V3 不参与**
（它在默认不敏感卷上 skip），E3/EC 两种构型的非 skip 证据由 §6.0 的 `hdiutil` + `fdd-cli`
真读数承担；该用例的兑现环境是"临时目录所在卷区分大小写"（Linux CI 默认满足）。
真读数与变异输出见 04 §6.9.6。

### 6.5 未兑现与边界

- **E6 / M36 不修**：≥2 根、且其中某根的子树里嵌着另一卷（大小写敏感）时，按根折叠
  仍会把该子树里的 `alpha`/`ALPHA` 折成一棵。修法是方案 C，本轮按"不扩大改动面"只登记。
- **Windows 与 Linux 两条腿未取真读数**：本机是 darwin。Windows 的"每目录大小写敏感"
  （WSL `fsutil file setCaseSensitiveInfo`）会让**敏感目录嵌在不敏感卷里**——这正是
  E5 的同族构型，单根已被 B 治好；≥2 根的同类缺口并入 M36。Linux 的 casefold/CIFS
  方向（该折没折 → 同一棵收两遍）本机无法构造，且按 E9 由阶段 1.5 兜底，属"有理有据
  但未实测"。
- **探测失败的兜底语义未动**：探不通（只读卷等）仍退回 `Default()`，多根路径上的
  "D 3 层语义猜错"风险与改动前一致；单根已不再依赖探测，故不再受此影响。
- **`res.Visited` 的口径随之变化**（单根时不再按折叠键计数），仅用于取消用例的松断言，
  无消费方依赖具体数值；划账如实记录。

---

## 7. M21：worktemp 跳过计数（04 §6.8.8 M21）

> 本节登记在 04 §6.8.8 而非 §6.7 C 组，但 M21 的登记修法就是"**并入工况 M6 的计数横幅**、
> 不单独造一套 UI"（04:1200）——呈现位与 §2/§4 的三类计数同源，故设计段延续本稿。
> 它是这条"跳过要可见"队列里**最后一块**：保护清单（§2）、云占位（§4）都已可见，
> 只剩应用自己产生的那一类仍然无声。

### 7.0 动手前取证（E1~E8）

| # | 结论 | 证据（本机读码/实跑） |
|---|---|---|
| E1 | 判定入口**全仓唯一** | `worktemp.IsTempName` 的生产调用点只有 `internal/scanner/scanner.go:328` 一处。其余出现处都不是"判定"：`internal/fscase/fscase.go:108` 用 `MarkCaseProbe` **生成**探测名、`internal/ops/worktemp.go:25` 只是转发、`cmd/benchgen/main.go:65` 用它**造**残留语料。⇒ 计数只需一个落点，不存在"漏接第二条路径"的问题 |
| E2 | 落点排在**一切过滤之前** | `scanner.go:310-421` 的全序：符号链接跳过 → **328 命中即 `continue`** → 目录分支（340 保护清单 → 348 目录隐藏规则 → 355 `ExcludePaths` 剪枝 → 358 折叠去重）→ 文件分支（375 保留名 → 379 `Info()` → 393 云端占位 → 397 `IsRegular` → 401 0 字节 → 405 `matcher.Apply` → 408 建条目）。**文件侧的隐藏规则在 `internal/filter/filter.go:130`（`IncludeHidden`），由 405 调用**——比 328 晚 |
| E3 | 目录天然不进数 | `!typ.IsDir()` 已写在 328 的条件里（M1 修正的产物）：临时命名的**目录**既不会被跳过、也不会有自增。计数只需照抄该条件，不需要额外分支 |
| E4 | 计数家族是**五层管道**，照抄即可 | `scanner.Result` 字段（`scanner.go:83-92`）→ 按 worker 的 `[]int` 槽 + 汇总（`scanner.go:263-265` / `:454-462`）→ `dedup.Pipeline` 原子量 + 按轮归零 + `Store` + 访问器（`pipeline.go:50-55` / `:240-242` / `:284-286` / `:87-96`）→ `ScanSummary` JSON + `scan:done` 接线（`app.go:136-162` / `:630-643`）→ `fdd-cli` `Stats`（`cmd/fdd-cli/main.go:41-68` / `:145-157`）。三层口径已在 §2/§4 定死：**不记 Failed**、独立计数、访问器只在同一临界区内取 |
| E5 | 语料里**已有**一个真实样本 | `cmd/benchgen/main.go:65` `residualName = keepName + worktemp.SuffixOld`，:292-296 把它复制成与 `keep.bin` 逐字节相同的残留，且**不计入 `m.TotalFiles`**。⇒ 修后 `smoke-cli.sh` 的语料上必须读到 `skipped_work_temp_files: 1`——这是本机就能拿到的端到端真读数，不需 Windows |
| E6 | 不会有既有断言被新字段打破 | ① `scanner.go:211` 是 `&Result{}` 键值字面量，全仓无 `scanner.Result{...}` 位置字面量；② `scripts/smoke-cli.sh:66-72` 只比**显式列举**的键 + 断言 `cache_hits` 存在，加键不会破坏比对；③ 前端 `frontend/src/wails.ts:36-41` 的 `ScanSummary` 只有 4 个字段，且无任何 TS↔Go 字段静态比对（这正是 M30 的缺口）；④ `scanner_worktemp_test.go` 三条既有用例只断言**收集集合**，不读计数 |
| E7 | 用户可见面现状 | `09-用户手册` §1 能力表第 40 行（"跳过工作临时文件 …不参与分组"）、§5.1 第 256 行（"上表都是**命中即丢弃、不额外报数**的过滤条件"——本项落地后这句对它不再成立）、§6.3.1（"这类文件名已被扫描器**无条件忽略**"）、§9 第 611/623 行两处计数指引。前两处必须随本项改写，§6.3.1 追加计数指引 |
| E8 | 判据的固有边界：分不出"谁的名字" | `IsTempName` 是**名字形态**规则，不是来源标记。一个用户自己命名为 `report.pdf.fdd-old` 的正常文件同样会被跳过——**这正是 M21 登记的伤害本身**（"用户视角是这个文件凭空不参与去重"）。计数能让它可见，不能把它变准。横幅文案（M8）必须同时容纳两种读法："应用自己的残留"与"恰好这样命名的文件"，见 §7.5 边界 1 |

### 7.1 判据

**加一个数**：`SkippedWorkTempFiles`——"本轮遍历到达的**文件**项里，按 `worktemp.IsTempName`
判为应用工作临时名、因此未参与去重的个数"。

- **在 328 的跳过点自增**（与判定同一行位置），**不记 Failed**：命中不是失败，是引擎主动
  放弃（E2/E3；同 §2/§4 的两类计数）。
- **不改判定本身**：`worktemp.IsTempName` 一字不动。本项只让它"被看见"，动判定就会与
  §6.8.8 M1/M13/AS-R1 已收敛的形态规则打架。
- **计数口径 = 到达过 + 名字命中**（E2 的直接推论）：与扩展名/大小/`IncludeHidden`/
  0 字节/云端开关**全都无关**。理由是 §4 那条纪律的原话——"计数说的是盘上有多少这类文件
  没参与，与扩展名/大小过滤无关：放在 matcher 之后，这个数就开始说谎"。
- **只数文件**，目录不计（E3）。
- **数不下来的地方就说不知道**：被剪枝/被排除的目录没有下潜，其中的临时名文件不会进数——
  与 `ProtectedDirs` 只计目录数同一条纪律（不知道的不许估）。

### 7.2 改动面（五层 + 注释 + 手册）

| 文件 | 改动 |
|---|---|
| `internal/scanner/scanner.go` | ① `Result` 增 `SkippedWorkTempFiles int`（放在 `SkippedCloudFiles` 之后，带口径注释）；② 263-265 边增 `wtSkipped := make([]int, workers)`；③ **328 跳过点增 `wtSkipped[idx]++`**；④ 454-462 增汇总 |
| `internal/dedup/pipeline.go` | ① 50-55 增 `workTempSkipped atomic.Uint64`；② 240-242 增按轮归零；③ 284-286 增 `Store`；④ 87-96 增 `WorkTempSkipped()` |
| `app.go` | `ScanSummary` 增 `SkippedWorkTempFiles uint64 \`json:"skippedWorkTempFiles"\`` + 630-643 接线 + **同 `UnprotectedRoots` 的口径缺口注释**（history.db 未存该口径 → 记录页恢复只能整格显示"未统计"） |
| `cmd/fdd-cli/main.go` | `Stats` 增 `SkippedWorkTempFiles int \`json:"skipped_work_temp_files"\`` + 145-157 赋值 |
| `cmd/benchgen/main.go` | :292 注释"**不计数**"随本项过时 → 改为"不计数**入 TotalFiles**（它是被扫描器忽略的残留，且已被 `skipped_work_temp_files` 计数）" |
| `docs/09-用户手册.md` | E7 三处（§1 表 + §5.1 那句"不额外报数" + §6.3.1 追加计数指引） |
| 新测试 | `internal/scanner/scanner_worktemp_count_test.go`（V1~V5）。**既有三条用例一字不动** |

### 7.3 探针（V1~V5，全部落在新文件里）

| 探针 | 断言 | 钉住什么 |
|---|---|---|
| V1 | 复用既有残留形态（`.fdd-old`/`.fdd-tmp`/`.fdd-old.undo`/`.fdd-restored.jpg`/`.fdd-restored_2.jpg` 各一）→ `SkippedWorkTempFiles == 5`，且 `Files` 仍为 3 | 数得准：五种**已注册形态**都在数里，不重不漏 |
| V2 | 反向守卫：`notes.fdd-old-summary.txt`、`album.fdd-old-collection/`、`x.fdd-case-probe-notes` 等**仅内嵌标记**的用户名 → 计数 **0**，且全部照常收集 | 判定不被放宽（H6/M1 回归：漏扫比残留更难发现） |
| V3 | 不随 `IncludeHidden` 漂移：`.fdd-case-probe-7` 残留 + 一个普通临时名，两档设置**同一读数**（且该文件两档都不进 `Files`）；镜像 `scanner_cloud_test.go:96-99` 的同款断言 | 计数在文件侧隐藏规则**之前**（E2）。放之后，读数就会随用户勾选而变 |
| V4 | 不随扩展名/大小过滤漂移：被 `ExcludeExts` 排除的、0 字节的临时名**照数** | 计数在 `matcher.Apply` 之前（§4 那条纪律的直接套用） |
| V5 | 口径边界：剪枝/排除目录里的临时名**不进数**（数下来的才是数）；且目录名恰为临时名时**只有其内部文件被数、目录本身不数** | "不知道的不许估" + 只数文件（E3） |

**修前必红**（两路，缺一不可）：
1. **编译级**：新测试文件先落地、生产代码不动 → `go test ./internal/scanner/ -run TestWalkCountsWorkTempSkipped`
   `[build failed]`（`res.SkippedWorkTempFiles` 未定义）。同 M6 前几项的做法。
2. **CLI 级真读数**：小夹具（`keep.bin` + `keep.bin.fdd-old` + 两个真文件）跑 `fdd-cli`，
   修前 JSON **无** `skipped_work_temp_files` 键；修后 `"skipped_work_temp_files": 1`。
   语料侧另有 E5 的现成样本（机器可复现，不需 Windows）。

### 7.4 变异（M-a..M-e，逐条改坏 → 必须变红）

| 编号 | 变异 | 应红 |
|---|---|---|
| M-a | 删掉 `wtSkipped[idx]++` 整行（回到"静默跳过"） | V1（计数退回 0） |
| M-b | 整条跳过规则**下移**到 `matcher.Apply` 之后（规则与计数同挪，等价于把它当普通过滤的一条） | V4（三条过滤各吃掉一个）；V3 亦红（隐藏档吃掉探测残片） |
| M-c | 判定命中即计数、不看 `typ.IsDir()`（目录也进数，但不跳过） | V5（`a.fdd-old/` 目录名被算进去） |
| M-d | 跳过条件去掉 `!typ.IsDir()`（**M1 缺陷复发**：目录整树漏扫） | V5（其内容与计数同时消失）——注意计数**恰好仍为 1**（目录本身顶上了那个数），靠 `inner.bin` 是否在语料里才逮得住 |
| M-e | `IsTempName` 放宽为 `strings.Contains`（判定与计数同步放宽） | V2 |

（V2 与 V5 各守一段：V2 守"内嵌标记的用户名不许被算/被丢"，V5 守"只数文件、只数到达过的"；
两段的失效模式不同，故 M-d 只由 V5 逮住属预期，不是漏网。）

### 7.5 未兑现与边界

1. **计数分不出"谁的名字"**（E8）：`report.pdf.fdd-old` 若真是用户文件，它在数里与我们的
   残留**不可分辨**。横幅文案（M8）只能中性表述（"已跳过 N 个与应用工作文件同名的文件"），
   不许写成"清理了 N 个残留"——那样会把用户的文件说成我们的垃圾。**本项不做文案**（裁定②）。
2. **UI 未呈现**（裁定②）：本轮只到 `ScanSummary` JSON 与 `fdd-cli` 报告；结果页横幅随 M8。
   划账时不得因"后端字段齐了"记兑现。
3. **history.db 未存该口径**：从记录页恢复历史扫描时只能显示"未统计"，
   **不得用零值冒充"这一轮没有工作临时文件"**（同 `UnprotectedRoots` 的注释口径）。已随
   `app.go` 的结构体注释一并写明。
4. **`benchgen` 语料上恒为 1**：`keep.bin.fdd-old` 是**刻意**造的残留（E5），所以这一项在
   `smoke-cli.sh` 里是**可对账的常数**而不是噪声。若哪天它变成 0 或 2，说明排除规则与语料
   对不上了——这正是缺陷 6 当年被发现的通道，保留其可观测性。
5. **`.fdd-case-probe-<数字>` 形态实际很少进数**：单根不写探测文件（§6），多根写完即删。
   它进数的唯一现实场景是"上次崩溃留下的残片"。计数照数不误（V3 用的就是它），
   但这**不是**新增的清理通道。
6. **Windows 腿无差别**：本项全在无 build tag 的纯逻辑里（判定、计数、管道），
   `go vet` 三条腿覆盖；无需真机读数，也不含平台分支。

---

## 8. M19 + M20：回滚路径的 TOCTOU（04 §6.8.8 M19、M20）

> 两项同源，都是**回滚/恢复动作把不属于本次操作的对象当成自己的来处置**：
> M19 在回收站回撤的**落位**一侧（原位判空后不认领 → `rename` 静默覆盖第三方文件），
> M20 在合并回滚的**还原**一侧（非法搬动 backup 位上的陌生文件，并把它称作"原文件"）。
> 判据形状也同一条：**动手之前先证明那个位置上还是我们的对象；证明不了就一个字节都不碰，只把位置报出来。**
> 这一条正是 §2/§6 的 `claimSlot`（证明不了就显式失败）在**反向动作**上的对偶。
>
> **锚点口径**：本节（§8.0~§8.5）里的 `文件:行号` 是**动手前**的读数，按"只增不改"保留为取证证据。
> 实施后 `undo.go` 因新增注释下移了 10 行（`:250-251` → `:260-261`）、`move.go` 的步骤 5 段落被
> 收归 `merge_guard.go`（该段原 `:129-143` 已不存在），`symlink.go` 同理。**当前锚点与交付读数见
> `docs/04` §6.9.8。**

### 8.0 动手前取证（E1~E9）

| # | 结论 | 证据（本机读码 / GOROOT 读数） |
|---|---|---|
| E1 | M19 现场：**同一函数里一支有抢占、一支没有** | `internal/ops/undo.go:134-147`：`target := it.OrigPath`；`Lstat` 判空 → **不认领**直接 `renameFile(it.DestPath, target)`。另名那一支（原位被占）在 M2 已改成 `claimDst` 原子抢占（:141）。⇒ 不变量"落位前先认领"只覆盖了一半 |
| E2 | 覆盖是**静默**的（不是理论风险） | `go doc os.Rename`："If newpath already exists and is not a directory, Rename replaces it."；Windows 腿 `internal/syscall/windows/syscall_windows.go:366` = `MoveFileEx(from, to, MOVEFILE_REPLACE_EXISTING)`。⇒ 非目录一律**替换**，只有目录/被占用等才会报错 |
| E3 | 同一个问题在 trash 侧**结论相反**，理由要写下来 | `internal/ops/trash_linux.go:128-133` 明确否决占位：`rename(目录 → 已存在的普通文件)` 在 Linux 上返回 ENOTDIR，而移入回收站的源可以是目录。**回撤方向没有这个约束**：`undoSourceCheck` 已要求 DestPath 必须是普通文件（`undo.go:97`），源恒为文件 ⇒ 占位可用。两侧结论不同、各有依据，写在此处以免将来被当成"同一处没修干净" |
| E4 | 抢占工具已在，只缺"不递增"那一档 | `claimDst`（`move.go:288-316`）是"另找可用名 + `_N` 递增"；`claimedDst.stillOurs()/release()`（:319-329）现成。本项要的是**抢占失败即换路**的那一半：`claimExact` |
| E5 | M20 现场，且**逐字重复两份** | `move.go:129-143` 与 `symlink.go:116-132` 是同一段 12 行（连注释都各写一份）——I5（同一判据两份实现）的现成样本。⇒ 本项顺手把这段**上收**到 `merge_guard.go`，此后只剩一份 |
| E6 | 守卫词汇都已在同一个文件 | `backupOwnershipStill`（= `identityStill(backup, dupID)`）、`claimSlot`、`slotProvesHardlink`、`abandonForeignBackup` 全在 `merge_guard.go`；其中 `slotProvesHardlink` 的 `id.Resolved &&` 前缀，是"用于**删除**的判据必须与 `identityStill` 的 fail-open 相反"的既有先例 |
| E7 | 既有断言钉住了哪几件事 | ① `rollback_message_test.go`（2 条）：还原失败的文案必须指向 backup 本尊；② `hardlink_verify_test.go:177` 与 `symlink_test.go:381`：终局复核失败要**回滚成原独立文件**，且 `assertNoResidue` 要求 `.fdd-old.undo` **不残留**；③ `identity_window_test.go:266`（M3 的还原失败报位置）；④ `undo_test.go:83/121`（另名恢复的两档）。⇒ 其中 ②④ 的全部用例都用 `fsid.ID{}`（零值）调合并 ⇒ **回滚前置必须是 fail-open**，见 §8.1 |
| E8 | 步骤 4 的 dup 位**没有**抢占（本次读码新查出） | 步骤 3 把 dup 腾空后，到步骤 4 `hardlinkRename(tmp, dup)` 之间，全流程只复核过 **backup**（`backupOwnershipStill`），**没有任何一处核对 dup 位是否空着**。第三方在这个窗口落子 → 步骤 4 按 E2 静默覆盖。登记 **M38**，不在本项实施（理由见 §8.5-2） |
| E9 | 同一段里的停靠名同样是"无主写 + 无证删" | `hardlinkRename(dup, backup+".undo")`（无抢占写）与 `_ = os.Remove(backup+".undo")`（无证删）；`undoHardlink` 的 `.fdd-undo-tmp` 槽位（`undo.go:250-251` 的无条件 `os.Remove`）同族。登记 **M37 / M39**，不在本项实施（理由见 §8.5-1） |

### 8.1 判据

**M19 —— 空位分支改成"先认领、后使用"：**

1. 新增 `claimExact(path)`：`O_CREATE|O_EXCL` 建 0 字节占位；抢到即这个名字归我们，
   随后的 `renameFile` 替换的是**我们自己的占位**，不再是"赌没人来"。
   与 `claimDst` 的唯一差别是**被占时不递增**（换名即换语义，交给调用方换路）。
2. 抢不到（`EEXIST`：含悬空符号链接与目录）或连"能否占用"都没问出来（其它错误）→
   一律走既有的 `claimDst(name.fdd-restored.ext)` 另名恢复。这与旧口径等价：
   旧代码把 `Lstat` 的非 ENOENT 错误同样当作"已占用"（`undo.go:136-137` 的原注释与写法）。
3. 失败分支照旧 `claim.release()`——新增的这一支也必须能清掉自己的占位。

**M20 —— 两条回滚分支在搬运 backup 之前加同一道前置：**

- `requireOriginalInBackup(backup, dup, dupID, cause)`：`backupOwnershipStill(backup, dupID)`
  不成立 ⇒ **不搬、不挪、不删**，只报位置；文案里**不得**出现"原文件保留在 …"
  （那正是 M20 登记的伤害：把他人的文件说成用户的原文件）。
- 成立 ⇒ 走原路径。**步骤 5 的"停靠—还原—清理"三步一字不动**（保住 E7 全部断言）。
- 两道前置的判据形状与 `slotProvesHardlink` **刻意不同**：这里沿用 `identityStill`
  的 fail-open（`dupID` 未解析时放行）。理由是 E7②④：判据管的是"要不要**放行一次回滚**"，
  判错的代价是回到修前行为（陌生文件被搬回 dup，仍在用户目录里），**不是销毁数据**；
  而 fail-closed 会让所有零值 ID 的回滚永久失效——`TestHardlinkMerge_FailureKeepsOriginalIntact`
  正是零值 ID 且必须回滚成功。

### 8.2 改动面（4 个生产文件 + 2 个新测试文件）

| 文件 | 改动 |
|---|---|
| `internal/ops/move.go` | ① 新增 `claimExact`（紧挨 `claimDst`，含"只差递增与否"的注释）；② 步骤 5 回滚整段（:129-143）→ 一行 `rollbackUnverifiedSwap` 调用；③ `rollbackAfterSwapFailure` 调用处增传 `dupID` |
| `internal/ops/symlink.go` | 同上两处（:116-132 整段 → 一行调用；`rollbackAfterSwapFailure` 增传 `dupID`） |
| `internal/ops/merge_guard.go` | 新增 `requireOriginalInBackup`、`rollbackUnverifiedSwap`（**上收**，消 I5 重复）；`rollbackAfterSwapFailure` 增 `dupID` 参数 + 前置调用 |
| `internal/ops/undo.go` | `undoTrash` 空位分支：`Lstat` → `claimExact`（注释写明抢占点落在哪两个动作之间） |
| 新 `internal/ops/undo_claim_test.go` | V1~V2（M19） |
| 新 `internal/ops/rollback_backup_guard_test.go` | V3~V6（M20，硬链接/软链接各两条） |

既有测试**一条不改**（E7 四条清单即验收条件）。

### 8.3 探针（V1~V6）与修前必红

| 探针 | 断言 | 修前为什么红 |
|---|---|---|
| V1 `TestUndoTrashClaimsOrigNameBeforeRename` | 在 `renameFile` 接缝里让第三方**原子落子**（`O_EXCL` 写自己的文件到 OrigPath）：落子成功 ⇒ 它的字节必须仍在原处；落子被 `EEXIST` 挡下 ⇒ 恢复必须照常到位（`dst == orig`、内容与 mtime 齐全） | 落子成功，随后被 `os.Rename` 静默替换（E2）——第三方文件的字节**当场消失**（改名把它 unlink 了） |
| V2 `TestUndoTrashFailedRenameLeavesNoPlaceholder` | 接缝让改名失败（非 EXDEV）⇒ 报错，且 OrigPath 上**不得留下 0 字节占位** | 绿（回归保护）。它钉的是修法**不许**把崩溃窗口留在磁盘上：占位建立后进程被杀＝用户原名变成 0 字节文件，所以 `claim.release()` 必须在每条失败支路生效 |
| V3 `TestHardlinkMergeRollbackRefusesReplacedBackup` | 步骤 5 复核失败 **+** backup 位被第三方顶替（同一接缝里：先复制式落位、再把 backup 换成陌生文件）⇒ 报错；backup 位那个文件**原封不动**、dup 位仍是本次校验未通过的对象；文案含"已不是本次操作的原文件"、**不含**"原文件保留在 " | 陌生文件被搬进 dup 位、原位置清空，且文案把他人的文件称作"原文件保留在 …"（M20 登记原话） |
| V4 `TestSymlinkMergeRollbackRefusesReplacedBackup` | 同上（软链接腿，`requireSymlinkSupport`） | 同上——`symlink.go` 里那份逐字重复的实现（E5） |
| V5 `TestHardlinkMergeSwapFailureRefusesReplacedBackup` | 步骤 4 失败 **+** backup 被顶替 ⇒ 拒绝；dup 位**不得出现陌生文件**；文案不含"原文件保留在 " | 旧码 `hardlinkRename(backup, dup)` 把陌生文件搬进 dup 位，且只返回裸改名错误（连位置都没报） |
| V6 `TestSymlinkMergeSwapFailureRefusesReplacedBackup` | 同上（软链接腿） | 同上（`symlink.go`） |

接缝构造要点（照 `identity_window_test.go` 的既有惯例）：
- 顶替发生在**同一接缝的同一时刻**——步骤 3 成功之后（`backupOwnershipStill` 已在 :121 复核过），
  所以不会被 M1 的窗口 A 守卫（`abandonForeignBackup`）先拦下；这正是"M20 守卫之间的空档"本身。
- 每次接缝替换都带 `done` 标志与 `t.Cleanup` 里的"前置条件未触发"断言，
  避免用例在没走到目标分支时**静默通过**。

**修前必红（两路，缺一不可）**：
1. 探针文件先落地、生产代码不动 → V1、V3、V4、V5、V6 红（读数照抄进划账）；V2 绿（回归保护）。
2. 既有断言清单（E7）在修前修后都必须全绿——它们同时是"没把回滚功能改坏"的负控制。

### 8.4 变异（六条，逐条改坏 → 必须变红）

| 编号 | 变异 | 应红 |
|---|---|---|
| M-M19-a | `claimExact` 调用换回 `os.Lstat` 先查后用（回到修前语义） | V1 |
| M-M19-b | `claimExact` 的 `O_CREATE\|O_EXCL` 去掉 `O_EXCL` | 既有 `TestUndoTrashOrigOccupiedUsesRestoredName`（原位上的第三方文件被截断，且恢复落错名） |
| M-M19-c | 两条失败支路不调 `claim.release()` | V2（0 字节占位残留） |
| M-M20-a | 删掉 `rollbackUnverifiedSwap` 里的 `requireOriginalInBackup` 调用 | V3、V4 |
| M-M20-b | 步骤 4 分支不加前置（只修步骤 5） | V5、V6 |
| M-M20-c | 前置里的 `backupOwnershipStill(backup, …)` 写成查 **dup** 位（查错了位置） | 既有 `rollback_message_test.go` 两条 + `TestHardlinkMerge_FailureKeepsOriginalIntact` + `TestSymlinkMerge_RollbackWhenVerifyFails`（回滚被自己永久拒掉） |

M-M20-c 用的是"查错位置"而不是"删掉判据"：删判据由 V3/V4 逮住（M-M20-a），
查错位置只有**既有**用例逮得住——两者不是同一失效模式，各自都要有杀手。

### 8.5 未兑现与边界

1. **M37（`.fdd-old.undo` 停靠名：无主写 + 无证删）不修**。判据形状已明：写侧用
   `claimSlot`（被占即拒绝回滚），删侧要"能证明才删"。之所以不在本项动：`hardlink_verify_test.go:228`
   的 `assertNoResidue` **正好钉住了"必须清掉"这一半**，改它等于改一条 2026-09-19 用户判据用例
   的语义；且"清不掉的那一份怎么向用户交代"要先有结论。`undoHardlink` 的 `.fdd-undo-tmp`
   （`undo.go:250-251`）同族 → **M39**。
2. **M38（步骤 4 的 dup 位落子窗口，E8）不修**。修法就是本项落地的 `claimExact` 占位，
   但**它会把一个新的失败形态引进合并主路径**：崩溃时用户的**真名**位置上留下 0 字节文件
   （M19 侧同样有，但那里我们本来就要往这个位置放文件；合并侧 dup 原本是"空着等链接"）。
   要不要接受，需要一个独立的设计决定（含"重跑时怎么看待这个占位"），按 §1 约束 5 登记不扩大。
3. **`claimExact` 的残余窗口**：占位与改名之间（微秒级）第三方若**先删我们的占位再落子**
   （主动针对本次操作），仍会被覆盖。与 `claimDst` 的残余窗口同族、同理由（Go 没有 NOREPLACE
   改名原语，加一道复核也只是把微秒窗口缩成微秒窗口），照 `merge_guard.go:55-60` 的写法
   记在这里，不写无法证伪的守卫。
4. **崩溃窗口（本项新引入，接受）**：占位建立后、改名完成前进程被杀 ⇒ 用户的**原名**位置上
   留一个 0 字节文件；数据仍在回收站/移动目标处，账本条目未落账。**不主动清理**它
   （我们无从与用户自己的 0 字节文件区分），而扫描器内置的 0 字节跳过会让它不进语料
   （`scanner.go` 的 `size==0` 分支）。§8.2 把"认领"与"改名"写成相邻两行，让窗口尽可能短。
5. **Windows 腿未在真机核对一处语义**：`O_CREATE|O_EXCL` 撞上"已存在的**目录**"时返回
   `EEXIST` 还是 `EACCES`，按 E2 的语义两者都会走另名恢复（不覆盖、不报错），
   **无行为差异**，故不单列 ID，只记边界；本项两处新逻辑都在无 build tag 的纯逻辑里，
   `go vet` 三条腿覆盖。
6. **M19 只动 trash 回撤这一支**：`undoMove` 走 `MoveFile`（`claimDst` 递增抢占，天然不覆盖）；
   `undoHardlink`/`undoSymlink` 各自已有防线（后者的原位判据见 `undo.go:310-368`）。
7. **`rollbackAfterSwapFailure` 的既有语义被原样保留**：临时链接仍在我的名字下（步骤 1 建立、
   步骤 2 复核过），所以拒绝搬运时仍照旧清掉 tmp；文案（含"还原"字样）不动，
   `identity_window_test.go` 的 M3 断言因此保持绿。
