# M6 实施细化设计（真实工况安全 · 逐项设计段）

- 日期：2026-09-21
- 上位依据：`2026-09-20-scenario-optimization-design.md`（工况总纲）§2.1~§2.4、§4、§5；
  登记处 `docs/04-开发与测试计划.md` §6.7 C 组 1~4 项
- 状态：**按实施顺序逐节追加**。本文只写"动手前必须定死"的判据、接入点与探针设计；
  实施后的兑现记录写在 04 §6.9 划账，不写在本文
- 用户裁定（三条，约束本文全部章节）：① 每项实施前各写一段细化设计（即本文）；
  ② 本轮只做 M6 后端，M8 前端体验项不做；③ 云占位默认跳过 + 可见计数

## 0. 实施顺序与本文进度

| 序 | 项 | 总纲编号 | 04 §6.7 | 本文小节 | 设计 | 实施 |
|---|---|---|---|---|---|---|
| 1 | 系统保护清单 + Windows 保留名 | §2.4 | C 组 4 | §2 | ✅ | ✅（划账见 04 §6.9.1） |
| 2 | 实占口径（稀疏/压缩） | §2.2 | C 组 2 | §3 | ✅（含 §3.0 实测证据） | ✅（划账见 04 §6.9.3） |
| 3 | 云占位检测 | §2.1 | C 组 1 | §4 | ✅（含 §4.0 取证，并**推翻总纲两条判据前提**） | ⬜ |
| 4 | Windows ADS 防护 | §2.3 | C 组 3 | §5 | ⬜ | ⬜ |
| 5 | fscase 单根探测 | — | C 组 1(04 §6) | §6 | ⬜ | ⬜ |

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
- **判定不覆盖 ADS/扩展属性、不改 `!IsRegular()` 的既有静默跳过**（除 V3 那一类
  被本项认领的占位外，其余 irregular 文件仍无声丢弃 → 属 M21 同族的既有开放项，不并案）。
- **UI 未呈现**（裁定③）：`skippedCloudFiles` 只到 JSON/CLI。前端仅补
  `Filters` 类型字段与默认值（`wails.ts:4-11`、`scan.ts:13-20`），理由是**防止
  settings 往返静默丢字段**（前端把整个 `filtersDefault` 写回），这不是呈现层改动。
  结果页"已跳过 N 个云端占位文件"横幅与 `AllowCloudHydration` 的复选框属 **M8**。
