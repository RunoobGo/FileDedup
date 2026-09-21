# M6 实施细化设计（真实工况安全 · 逐项设计段）

- 日期：2026-09-21
- 上位依据：`2026-09-20-scenario-optimization-design.md`（工况总纲）§2.1~§2.4、§4、§5；
  登记处 `docs/04-开发与测试计划.md` §6.7 C 组 1~4 项；§7 另接 04 §6.8.8 M21
  （登记修法即"并入工况 M6 的计数横幅"，故与本稿同源）；§8 另接 04 §6.8.8 M19/M20
  （回滚路径的 TOCTOU，与 §2/§6 的 `claimSlot` 是同一条"不替用户处置不属于本次操作的文件"主线）；
  §9 另接 04 §6.8.8 M22/M25/M26（假账与静默失败：数字/失败/判据三者都必须在目标环境里**成立**）；
  §14 另接用户指令"审查…如有遗漏自动完成开发和修复"——不是新功能，而是**已有声明与实证的
  对齐**（含 M43 兑现、M30 同族漏网 G10、新登记 M46）
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
| 8 | 假账与静默失败（M22 + M25 + M26） | — | §6.8.8 M22/M25/M26 | §9 | ✅（含 §9.0 取证 E1~E17；**推翻 M26 登记的"Linux 主门禁可跑出两棵"**） | ✅（划账见 04 §6.9.9；新增登记 M40/M41/M42/M43，见 §9.5；九条变异真读数含 M-M26-b 本机逃逸） |
| 9 | 遍历键不折叠（M36，方案 C） | — | §6.8.8 M36 | §10 | ✅（含 §10.0 取证 E1~E11；**推翻 I2 当年定的遍历期折叠机制**，折叠只留给"合并用户给的根"） | ✅（划账见 04 §6.9.10；新增登记 M44，见 §10.5-1） |
| 10 | 稀疏卷判据（M28） | — | §6.8.8 M28 | §11 | ✅（含 §11.0 取证 E1~E12；**推翻登记的"`f_type` 白名单 + 每卷写探测"形状**，改用遍历内证据） | ✅（2026-09-21，实施 commit `333a9ba`，划账 04 §6.9.11，追记 §11.6） |
| 11 | TS 类型对齐（M30） | — | §6.8.8 M30 | §12 | ✅（含 §12.0 取证 E1~E7；**缺口面比登记大**：3 个接口 9 个字段，非登记行只记的 `ScanSummary` 5+1） | ✅（2026-09-21，交付 `d4af53f`：比对器 23 对 + 3 豁免，六条变异全杀；追记 §12.6，划账 04 §6.9.12） |
| 12 | CoW 克隆双计（M27） | — | §6.8.8 M27 | §13 | ✅（含 §13.0 取证 E1~E8；**更正登记行的"下限"为"上限"**；夹具成 darwin 门禁的钉子；实施期另发现前提自检盲区 ⇒ 补读出口钉子，见 §13.6） | ✅（2026-09-21，交付 `e013f09`：darwin 克隆夹具 + 读出口钉子，M-M27-a/b/c 真读数见 §13.6；划账 04 §6.9.13） |
| 13 | 本轮全量审查的修复包（G1~G11） | — | §6.8.8 M43（兑现）+ 新增 M45/M46 | §14 | ✅（含 §14.0 取证：**11 条声明与实证不一致**，其中 G10/G11 是取证时新发现；G1~G11 逐条附 file:line 与"矛盾的代码事实"两列） | ✅（2026-09-21，代码面 `0fd9cd7`：M43 兑现 + 方法面钉子（修前 34/32 → 修后 34/34）+ 契约面两处补齐 + 门禁脚本口径改真；文档面与划账 04 §6.10，追记 §14.6） |

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

> **§3.1 的 API 追记（M28，2026-09-21）**：上表的 `From(size, reported, ok)` 已增第四参
> `trustsZero`、`Of` 已被删除（它的语义 = "没有卷级证据"的单文件封装），新增
> `Reported`（原 `reported` 导出）/`VolumeID`/`Tracking`，见 §11.2。原文保留。

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
  （**M28 追记，2026-09-21**：该开放项已实施，但**判据形状被推翻**——既不用 `f_type`
  白名单，也不用每卷一次写探测，改用遍历期本就拿得到的**同卷非零证据**，见 §11 的
  E3~E6。上面那句"要两者兼得需 …"是当时的判断，保留原文以存其真。）
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

**实施后追记（2026-09-21，M36 交付）**：表内方案 C 的"**本轮不做**"是 C1 交付当时的结论；
本项（M36）随后就按它实施了，设计段见 §10。三点随之变化：① 遍历键**一律**不折叠，
`folder` 类型与四个方法整体删除；② `sens` 不再出参（方案 B 留下的那处"已知不美"消失）；
③ 三条直接戳 `folder` 的用例被一条更强的纯断言替换（§10.3 V1）。方案 A 的结论不变
（仍被 E5 推翻），方案 B 单根那一半的结论也不变（仍是"单根不折"的特例）。

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

- **E6 / M36**：≥2 根、且其中某根的子树里嵌着另一卷（大小写敏感）时，按根折叠
  仍会把该子树里的 `alpha`/`ALPHA` 折成一棵。~~修法是方案 C，本轮按"不扩大改动面"只登记。~~
  **（2026-09-21 追记）已按方案 C 实施，见 §10**；本条其余结论（当时只登记、真读数
  `ED-two-roots files_total=2`）不变。
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

---

## 9. M22 + M25 + M26：假账与静默失败（04 §6.8.8 M22、M25、M26）

> 三件同源，都是**事实与呈现分叉**：
> M22 把**还躺在回收站里**的文件报成"已释放"（数字说谎，且与紧挨着的"打开回收站"按钮自相矛盾）；
> M25 把启动期的失败只写进 stderr（GUI 没有终端 ⇒ 等于没说，用户只感到"这软件越用越慢"）；
> M26 把一条**只在 Windows 上失效**的前缀判据当成了跨平台判据（那条分支在 Windows 上等于不存在，
> 而本机恰好看不出来）。
> 共同的判据形状：**只有在目标环境里真成立才算数**——数字必须对应磁盘事实（M22）、
> 失败必须走用户看得见的通道（M25）、判据必须在目标平台上真的命中（M26）。

### 9.0 动手前取证（E1~E17）

**M22（现场 / 后果 / 既有钉法）**

| # | 结论 | 证据（本机读码） |
|---|---|---|
| E1 | 现场：trash 与 delete/move 混在同一栏 | `internal/ops/executor.go:343-350`：`switch op.Kind { case "hardlink": …; case "symlink": …; default: res.Reclaimed += e.Size }`。同处注释（`:334-335`）自述口径是"真正从磁盘上消失的数据量（trash/delete/move 出卷）"——**trash 恰恰不消失** |
| E2 | trash 的真实磁盘效果（逐平台读码） | Linux：`trash_linux.go:83-105` `moveIntoTrash` 同卷 `os.Rename`（数据落 `~/.local/share/Trash`），跨卷退化为"复制+删源"；macOS：Finder `move … to trash`（`trash_darwin.go` 的 AppleScript）；Windows：`$Recycle.Bin`。⇒ 同卷 trash **磁盘占用一分未减**；跨卷 trash 只是把数据搬到另一个卷的回收站，**磁盘总量同样未减**，要等清空才释放 |
| E3 | 钉住旧口径的断言共 3 处，全是 trash 场景 | `ops_test.go:209`（S7"跳过不计入释放空间"）、`integration_test.go:68`（M3 集成）、`executor_ads_test.go:108`（V5 放行对照）。⇒ 本项必须走上那条既定程序：**改断言前先论证它钉错了语义，并在原位写明** |
| E4 | 同类口径在账本侧**独立存在**，本项不覆盖 | `RecordsView.vue:268` 的"回收空间"列读 `OpMeta.Reclaimable`，后者来自 `oplog.go:130` 的 `SUM(size) WHERE state=done`——**不分 kind**。⇒ 结果条修好之后，记录页那一列对 trash 操作仍在说"回收空间 X"。新登记（§9.5-2） |
| E5 | 单列先例现成，照抄即可 | `LinkedBytes`（`model.go:216`）与 `SymlinkedBytes`（`:229`）就在同一个 switch、同一个结构体里，前端各有一段措辞（`ResultView.vue:426-433`、`opdisplay.ts:27-48`）。三者互斥并列，本项加第四个 |
| E6 | 既有前端措辞把 trash 与 delete 混为一谈 | `opdisplay.ts:49-50` 的 `default` 分支 → "共 X 空间可释放"（`ConfirmDialog.vue:68` 传 `props.kind`，trash 会落到 default）；`frontend/tests/opdisplay.test.ts:51-56` 把 `['trash','delete']` 一起断言成"空间可释放" |
| E7 | 同卷 move 是同一族缺陷，但不在登记行内 | 同卷 move 也是改名（`move.go:34-35` 的 `renameFile` 快路径），却照样进 `Reclaimed`。与 trash 的差别在于**要先能分辨**（见 §9.5-1），故本项不动它 |

**M25（现场 / 通道 / 夹具）**

| # | 结论 | 证据（本机读码 + 实测） |
|---|---|---|
| E8 | 现场 | `app.go:288-299`：`cache.Open` 失败只 `fmt.Fprintf(os.Stderr, "[cache] 哈希缓存不可用…")`。GUI 无终端 ⇒ 用户永远看不到；后果不是崩溃而是**永久变慢且无从排查** |
| E9 | 启动期唯一的界面通道是**单个字符串槽** | `app.go:223-227`（`startupNotice string`）+ `:1208-1215`（`GetStartupNotice`）。已占用者是 M12b 的账本隔离重建（`:331` 赋值）⇒ 缓存失败若也直接赋值会**互相挤掉**（登记原文的次生问题） |
| E10 | 可测先例现成 | `openLedger`（`app.go:311-334`）就是同一段"启动前置 + 界面留痕"，M12b 特意从 `startup` 抽出并写明"抽出来是为了可测"（因 `startup` 会解析真实的 `os.UserConfigDir`，单测调它就会动用户机器上的库） |
| E11 | 夹具可行性（本机真读数） | 把 `<cfgDir>/cache.db` 建成**目录**：`cache.Open` 必失败且**不走隔离重建**——`err=缓存库不可用（未改动任何文件）: 初始化失败: unable to open database file (14)`，目录内无 `.broken-*` 残留 ⇒ **不需要新增接缝变量**。（反面：往 `cache.db` 写垃圾会命中"影像损坏→隔离→重建成功"，反而拿不到失败态） |
| E12 | 前端消费面只有一处，且不保留换行 | `stores/scan.ts:833` 把整条串丢给一次 `toast().push(msg,'warn',20000)`；`ToastHost.vue:90` 的 `.msg` 没有 `white-space: pre-line` ⇒ 多条用 `\n` 拼接后**会折成空格连成一段**（§9.5-3） |
| E13 | 同族第三处（本项不修） | `app.go:317-322`（`openLedger` 打开失败）同样只写 stderr，而后果更重：**回收站/移动/硬链接清理会被拒绝执行**（C3 之后）。新登记（§9.5-4） |

**M26（现场 / 推翻 / 定向复现 / 既有正确写法）**

| # | 结论 | 证据（本机读码 + 真读数） |
|---|---|---|
| E14 | 现场 | `scanner.go:565`：`strings.HasPrefix(fr, fk+string(filepath.Separator))`，而 `fr`/`fk` 来自 `fscase.Fold`（`fscase.go:31-43`）——**路径含 `\` 时 Fold 把整串换成 `/`**，Windows 的 `filepath.Separator` 却是 `\` |
| E15 | **登记前提被推翻** | 把 `C:\a` / `C:\a\b` 喂进**生产** `dedupeRoots`，darwin 上读得 `kept=["…/scanner/C:\a"]`（**1 棵**，已合并）——`filepath.Abs`+`Fold` 在本机把 `\` 归成了 `/`，与 `filepath.Separator`（`/`）恰好同值。⇒ 登记的"平台无关，可在 Linux 主门禁跑出两棵"**不成立**：本项在 darwin/Linux 上拿不到生产路径的红读数 |
| E16 | 定向复现（同一次真读数：把 `:565` 的判据逐字参数化后注入平台真值） | `fr="c:/a/b"`、`fk="c:/a"`：`sep="/"` → **true**；`sep="\\"` → **false**（Windows 不敏感卷的真相）；敏感卷形态（Fold 原样保留：`fr="C:\a\b"`、`fk="C:\a"`）→ **true**（此刻靠"两边都带 `\`"成立）。⇒ 缺陷的准确形态：**Windows 不敏感卷（NTFS 默认）上这条分支恒不成立，敏感卷上靠巧合成立**；今日危害限于"同一棵树被重复遍历 + 按根记账的口径出现宽窄根同时留痕"（登记行原文） |
| E17 | 同一包里正确写法已经存在 | `folder.key`（`scanner.go:196-202`，M6-P4 加，注释里**就点名了这条登记**）+ `rootsUnder`（`:206-208`，用 `"/"` 拼前缀）。⇒ 本项不是发明判据，而是把 `dedupeRoots` 拉到同一口径，并顺手消掉 I5 的第二份实现 |

### 9.1 判据

**M22 —— trash 单列；`Reclaimed` 的定义不动（登记原话）：**

1. `model.OpsResult` 增 `TrashedBytes uint64`（第四个字节口径，与 `Reclaimed`/`LinkedBytes`/`SymlinkedBytes`
   **互斥**——同一个成功项只会落进其中一栏）。
2. `aggregate()` 的 switch 增 `case "trash": res.TrashedBytes += e.Size`；`default`（delete/move）**一字不动**。
3. 计数口径按 §1 约束 2 写清：**数的是"进回收站的字节数"，不是"释放的字节数"**——
   回收站里的数据仍在磁盘上，用户清空之后才释放。
4. 呈现按既有两个口径的形状补第四支：`ResultView.vue` 结果条加 `v-else-if TrashedBytes` 分支；
   `opdisplay.ts` 加 `case 'trash'`（措辞取自登记："移入回收站（清空后才释放）"），
   并把文件头那张"口径 → 栏位"对照表补上第四行（那张表正是 M15 立下的"判据只有一份"）。
5. 既有 3 处 trash 断言（E3）改写 + **原位写明**为什么它钉错了；delete 与跨卷 move 的反向钉法是
   **新增**（不是改）。
6. **同卷 move 不在本项**：要分辨必须先让 `MoveFile` 报出"这次是改名还是跨卷"，
   改返回形状会牵动两处调用点 ⇒ 新登记（§9.5-1）。本项**不碰** move 的任何一行。

**M25 —— 启动期失败走界面通道；槽位由"覆盖"改"累积"：**

1. `startupNotice` 保持 `string` 与 `GetStartupNotice` 的绑定形状（登记允许 `+= "\n"`），
   但**只许经 `addStartupNotice(msg)` 写**：空则直赋，非空则 `cur + "\n" + msg`（后写不再挤掉先写）。
2. 新增 `openCache()`（对齐 `openLedger`，E10）：失败时保留既有 stderr 行 **加**一条
   `addStartupNotice(cacheUnavailableNotice(dbPath, err))`；成功路径一字不动
   （`a.cch = cch`、`a.pipe = a.pipe.WithCache(cch)`）。
   它在 M9 白名单上的位置与 `openLedger` 同档（启动前置，那时还没有并发读者）。
3. 文案是纯函数 `cacheUnavailableNotice(dbPath string, err error) string`（照 `ledgerQuarantineNotice`
   的形状），必须说清三件事：**出了什么事**（哈希缓存打不开，本次运行不启用缓存）、
   **对用户意味着什么**（每次扫描都要重新计算，速度变慢；功能不受影响）、**在哪**（缓存文件名）。
   不写"永久变慢"这类无法证伪的话——重启后可能就好了。
4. `openLedger` 的隔离重建分支改走 `addStartupNotice`（同一槽位只留一个写入口）。
5. **不动前端**（E12）：多条的呈现方式属 M8，按 §1 约束 4 登记（§9.5-3）。

**M26 —— 判据搬进 `"/"` 空间，全包只剩一份：**

1. 新增两个纯函数（无 build tag，§1 约束 1）：
   - `keyOf(folded, sep string) string` = `strings.ReplaceAll(folded, sep, "/")`。**sep 是平台分隔符真值**
     （H6 注入），生产传 `string(filepath.Separator)`：不敏感卷上 Fold 已归一（此步无操作），
     **敏感卷上 Fold 原样返回、这一步是真起作用的**（E16 第三行）。
   - `underKey(key, root string) bool` = `key == root || strings.HasPrefix(key, root+"/")`。
     两个参数都必须是 `keyOf` 的产物 ⇒ 前缀恒用 `"/"`，与平台无关。
2. `dedupeRoots` 的两处键构造改走 `keyOf(…, string(filepath.Separator))`，前缀判定改走 `underKey(fr, fk)`。
3. `folder.key` 改为 `keyOf(f.fold(p), string(filepath.Separator))`；
   `rootsUnder` 的 `k == dirKey || HasPrefix(k, dirKey+"/")` 改为 `underKey(k, dirKey)`。
   ⇒ 同一判据在全包**只剩一份**（此前 `dedupeRoots` 与 `rootsUnder` 各写一份，其中一份带缺陷：I5）。
4. `filepath.Separator` 与 `"/"` 的分工写进注释：前者是**输入侧**的归一真值，
   后者是**键空间**的约定；把两者混在一个表达式里正是 M26 的成因。

### 9.2 改动面（6 个生产文件 + 4 个测试文件 + 3 个前端文件）

| 文件 | 改动 |
|---|---|
| `internal/model/model.go` | `OpsResult` 增 `TrashedBytes` 字段（含与另三口径互斥、为什么 trash 不算"释放"的注释） |
| `internal/ops/executor.go` | `aggregate()` 增 `case "trash"`；`:334-335` 的口径注释把 trash 从 `Reclaimed` 的成员里删掉 |
| 新 `internal/ops/executor_account_test.go` | V1~V3 |
| `internal/ops/ops_test.go`、`integration_test.go`、`executor_ads_test.go` | 三处 trash 断言改写（原位写明）+ ads 拒收断言补 `TrashedBytes == 0` |
| `app.go` | 新增 `openCache()`、`addStartupNotice()`、`cacheUnavailableNotice()`；`startup` 的缓存段改为调用；`openLedger` 的两条留痕改走新槽位 |
| 新 `app_m25_test.go` | V4~V6 |
| `internal/scanner/scanner.go` | `keyOf`/`underKey` 两个纯函数 + 三处调用点（`dedupeRoots`×2、`folder.key`、`rootsUnder`） |
| 新 `internal/scanner/scanner_m26_test.go` | V7~V8 |
| `frontend/src/wails.ts` | `OpsResult.TrashedBytes` 镜像 + 口径注释第四行 |
| `frontend/src/utils/opdisplay.ts` | `case 'trash'` + 头部口径表第四行 |
| `frontend/src/views/ResultView.vue` | 结果条第四分支 |
| `frontend/tests/opdisplay.test.ts` | trash 从 `['trash','delete']` 那条用例里拆出（原位写明理由） |

### 9.3 探针（V1~V8）与修前必红

| 探针 | 断言 | 修前读数 |
|---|---|---|
| V1 `TestTrashKindReportsTrashedNotReclaimed`（trash + `mockTrash`） | `Reclaimed == 0` 且 `TrashedBytes == Σ size(OK)`；`OK` 集合本身不变 | 编译红（字段不存在）；**补一步真读数**：只加字段、不改 `aggregate` → `Reclaimed=<dup2.Size>、TrashedBytes=0` |
| V2 `TestDeleteKindStillReclaims`（delete） | `Reclaimed == Σ size`、`TrashedBytes == 0` | 绿（负控制：钉住"没把 delete 一起改掉"） |
| V3 `TestCrossVolumeMoveStillReclaims`（`forceCrossVolumeRename` + move） | 跨卷 move 照旧 `Reclaimed == Σ`、`TrashedBytes == 0` | 绿（负控制，登记明确要求"反向钉住"） |
| V4 `TestOpenCacheUnavailableSurfacesNotice` | `cache.db` 建成目录 → `openCache()` 后 `GetStartupNotice()` 非空且含缓存原因与文件名 | 红（抽出 `openCache` 后行为照旧：notice 为空串） |
| V5 `TestStartupNoticesAccumulate` | 账本隔离重建（垃圾 `history.db`）+ 缓存失败同触 → 两条**都在** | 红（单槽：后写挤掉先写，只剩一条） |
| V6 `TestOpenCacheQuietWhenHealthy` | 健康路径（新建库）不产生任何提示 | 绿（负控制：防假警——每次都弹的提示会被用户脱敏） |
| V7 `TestUnderKeyWindowsTruthIsSubroot`（注入 `sep="\\"`） | 不敏感形态 `("c:/a/b","c:/a")` 与敏感形态 `keyOf("C:\a\b","\\")` 都判出子树；`("C:\ab","C:\a")` 与反向**不**误判 | 编译红（`keyOf`/`underKey` 不存在）；抽出后（旧式逐字保留）→ **false**（E16 真读数） |
| V8 `TestDedupeRootsStillMergesNestedNativeRoots`（真夹具 `tmp/a`、`tmp/a/b`） | 生产 `dedupeRoots` 仍合并为一棵；`sens` 与保留根同序、`all` 仍为去重前的全部规范化根 | 绿（负控制：防"判据搬进 `/` 空间"把本机行为改坏） |

**修前必红**：V1（补一步）、V4、V5、V7 红；V2/V3/V6/V8 是负控制，修前修后都必须绿。
既有 3 处 trash 断言（E3）改的是**口径**（trash 不再进 `Reclaimed`），不是**事实**
（`OK`/`Skipped`/`Failed` 集合一字不动）——这一点由 V1 的集合断言与 `executor_ads_test.go:228` 的补强共同钉住。

### 9.4 变异（九条，逐条改坏 → 应红）

| 编号 | 变异 | 应变红 |
|---|---|---|
| M-M22-a | 删掉 `case "trash"`（回到 `default`） | V1 |
| M-M22-b | trash 同时累加 `TrashedBytes` 与 `Reclaimed`（两栏都记） | V1 |
| M-M22-c | trash 写进 `LinkedBytes`（串栏） | V1 |
| M-M25-a | `openCache` 失败只写 stderr（不调 `addStartupNotice`） | V4 |
| M-M25-b | `addStartupNotice` 改回直接赋值（挤掉前一条） | V5 |
| M-M25-c | 成功路径也留一条提示（假警） | V6 |
| M-M26-a | `keyOf` 不归一（`return folded`） | V7 —— **本机唯一能杀 M26 的变异** |
| M-M26-b | `underKey` 的前缀改用 `string(filepath.Separator)` | **本机绿（变异逃逸）**：darwin 上该常量与修法值同为 `"/"`，不可区分 ⇒ 只有 Windows runner 能抓（§9.5-5 如实记录） |
| M-M26-c | `underKey` 用 `strings.Contains` 代替前缀 | V7 的兄弟用例（`C:\ab` 误判为 `C:\a` 的子树） |

### 9.5 未兑现与边界

1. **同卷 move 仍被算成"已释放"**（E7）：登记只点 trash，而 move 的分辨要先让
   `MoveFile`（`move.go:22`）报出"这次是改名还是跨卷"——改返回形状会牵动 `executor.go:497`
   与 `undo.go` 的 `undoMove` 两处调用点，属独立设计决定。新增登记 **M40**。
2. **记录页"回收空间"列对 trash 操作仍说谎**（E4）：账本 `op_records.reclaimed` 是
   `SUM(size) WHERE state=done`，不分 kind；修它要么加列（schema 迁移）、要么在读侧按 kind 分述。
   新增登记 **M41**。
3. **多条启动提示在 toast 里会连成一段**（E12）：本轮不动前端，`\n` 被折成空格 ⇒
   两条长句读起来是一段话。呈现方式属 M8，新增登记 **M42**。
4. **`openLedger` 打开失败仍只写 stderr**（E13）：后果比 M25 更重（回收站/移动/硬链接清理会被拒），
   修法与 `addStartupNotice` 同形（一行），但不在本项登记行内 ⇒ 按 §1 约束 5 登记 **M43**，
   不改写既有行、也不在本项实施。
5. **M26 的 Windows 真机语义未兑现**（与 M32/M33 同档）：修法在 Windows 上的正确性只有
   "注入真值的等价复现"（E16/V7）与 `GOOS=windows go vet` 两条保证，**没有真机读数**；
   且 M-M26-b 这一变异在本机**不可杀**（如实记录，不写成"已验证"）。
6. **M22 的跨卷 trash 细节**：跨卷 trash 在源卷确实腾了空间（复制+删源），数据搬到另一个卷的
   回收站 ⇒ 磁盘总量未减。措辞"移入回收站（清空后才释放）"对跨卷同样成立，
   故**不**为跨卷另开一栏。
7. **M22 只动 trash 这一格**：hardlink/symlink 的既有口径一字不动；ADS 守卫的拒绝路径也不受影响
   （拒绝项不进任何一栏）。
8. **M25 的 startup 顺序未变**：仍是 缓存 → 账本 → `app:ready`，故提示的累积顺序
   （先缓存、后账本）与发生的先后一致；`app:ready` 之后前端才拉 `GetStartupNotice`，
   不存在"拉取早于写入"的窗口。
9. **M26 的判据面只覆盖 `dedupeRoots` 与既有两个键比较点**：`rootPrefixes`（`scanner.go:582-589`）
   用的仍是"根 + `filepath.Separator`"，那是**原始路径**上的前缀（不是折叠键），
   与 M26 不同族，本项不动（登记 M36 是另一回事：≥2 根时子树里嵌着另一卷的语义，仍留账）。

---

## 10. M36：≥2 根路径上的按根折叠缺口（04 §6.8.8 M36）

> 修的是什么：遍历期的 `visited` 去重键此前是**按根所在卷的语义折叠**过的（`folder.fold`）。
> 而折叠键描述的是**根所在卷**，一棵根的子树却在**任意深度**上都可能嵌着另一卷——
> 把一棵大小写敏感卷挂进任意普通目录即得此构型（本机可自建，E1）。
> 于是"父根在不敏感卷上"这条判定被错误地施加到整棵子树：`alpha/` 与 `ALPHA/` 两棵
> **不同**子树被折成一个键，后遇到的那棵整棵不进语料，**且一条失败都不记**（E3）。
> 这是 C1（§6 单根）的同族缺口，修法就是 §6.2 早已预告并登记的**方案 C**。

### 10.0 动手前取证（2026-09-21，本机实测 + 读码）

| # | 取证项 | 读数 | 出处 |
|---|---|---|---|
| E1 | 构型能不能自建（不需要 root） | 能：`hdiutil create -size 20m -fs "Case-sensitive APFS" -volname FDDSEN` rc=0；`hdiutil attach <dmg> -mountpoint <普通目录>/sen -nobrowse` rc=0，`mount` 回显 `<…>/host/sen (apfs, local, nodev, nosuid, journaled, noowners, nobrowse, mounted by just)` | 本机执行 |
| E2 | 构型的两个卷语义各自确认 | 父卷不敏感：`test -d <EV>/HOST` 为真（`host` 与 `HOST` 是同一目录）；子卷敏感：`<EV>/host/sen/ALPHA/a.txt` **不存在**而 `alpha/a.txt` 存在，且 `ls` 同时列出 `ALPHA` 与 `alpha` | 本机执行 |
| E3 | ★ 修前真读数（两根 = 不敏感父目录 + 另一无关目录；真值 3 = 敏感卷内 2 + 另一根 1） | `files_total=2`、`files_failed=0`、`protected_dirs=1`（= 敏感卷根上的 `.fseventsd`，与本项无关） | `fdd-cli`，本轮重取（与 04 §6.9.6 `ED` 行读数一致） |
| E4 | 单根对照（方案 B 已治） | **同一台机器、同一刻、同一构型**，只给父目录一个根：`files_total=2`（真值 2）——单根已收齐，两根仍漏 | `fdd-cli`，本轮 |
| E5 | 遍历期折叠的消费点到底有几处 | 两处**判重**（`visited` 的发现键 `scanner.go:405`、种子键 `:289`）＋两处**键空间**（逃逸判据的 `rawKeys` `:269`、dirKey `:388`）。除这四处外无消费方；`res.Visited` 只是 `len(visited)` | 读码 |
| E6 | 折叠撤掉后还需不需要它 | 需要，但只剩**合并用户给的根**一处：`dedupeRoots:582-586` 的 `fr/fk` 比较——"同一目录的两种拼写"只在用户手输时出现 | 读码 |
| E7 | `folder` 类型还剩什么 | 只剩 `prefixes`（G2 预计算，`WalkWithGate:253` 直接当 `prefixes` 用）；`roots`/`sens`/`def`/`needFold` 与四个方法在方案 C 下全无消费方 | 读码 |
| E8 | 危害能不能在**主门禁**（Linux CI）上复现，而不只靠挂卷 | 能：`probeCaseSensitive` 是包级接缝，注入"不敏感"就能让父根被判为不敏感，而 Linux 的 tmpfs/ext4 上 `alpha/` 与 `ALPHA/` 可以真并存 ⇒ 与 E3 **同形**（根卷判定 ≠ 子树现实）。本轮落地为 V2 | 读码 + 设计 |
| E9 | 折叠错了的代价不对称（承 §6.0 E9） | 折错 = 静默丢语料、下游补不回来；不折错 = 同一目录走两遍，由阶段 1.5 的物理身份去重吸收 | 读码 |
| E10 | 逃逸判据的键也随之变精确 | `rawKeys`/dirKey 由"折叠键"改为**原样拼写键** ⇒ 用户手输的拼写与盘上拼写**仅大小写不同**时不再匹配 ⇒ 不再放行保护剪枝（剪枝 + `ProtectedDirs` 计数）。这是本项**新引入**的一处行为变化，登记 **M44**（§10.5-1） | 读码 |
| E11 | 既有用例里哪些会被推翻 | 直接戳 `folder` 的三条：`TestFolderSingleRootNeverFolds`、`TestFolderMultiRootStillFolds`、`TestFolderFoldPerRoot`——正是 §6.2 方案 C 预告的代价。**端到端那几条不受影响**（`TestWalkCaseSensitivityIsProbed`、`TestWalkSingleRootSkipsProbe`、`TestWalkSingleRootKeepsCaseVariantSubtrees`、`TestRootInsideProtectedDirStillScanned`），因为 `dedupeRoots` 的合并折叠保留 | 读码 |

### 10.1 判据：折叠的两个用途，只有一个是遍历期的

折叠把路径归一到"同一棵树的同一个键"。它在本代码里只有两个真实用途：

1. **合并用户给的根**（`dedupeRoots`）：用户可能把同一棵树以两种拼写各给一次（手输、
   从两个方向点进来）。这里折叠是**必需**的，且是"探测"存在的唯一理由。**保留。**
2. **遍历期去重**（`visited`）：从根出发，每个子路径都由 `filepath.Join(父, ReadDir 得到的名字)`
   生成，链接/junction 已在 `:294` 被跳过 ⇒ 同一目录在一趟遍历里**只会以唯一拼写出现**，
   没有可并之物。此时任何折叠都只剩一种可能的作用：把大小写不同的两棵**不同**目录
   误并成一棵。**撤掉。**

两种错法的代价不对称（E9）：折错 = 静默丢语料且不可恢复；不折错 = 同一目录走两遍，
结果被阶段 1.5 的物理身份去重吸收。**不确定时倾向不折叠**——这正是 C1 已经采用的口径
（§6.1），M36 只是把它从"单根"推广到"全部遍历键"，让判据不再依赖根本身的卷属性。

一句话：**探测只描述根本身所在的卷，而遍历会走进任意深度的别的卷**（E5/E8 的构型）。
把"根卷"的语义施加到子树上，前提本身就是错的；既然遍历期没有可并之物，最省事的正确
做法就是不折，而不是去猜每一个子目录的卷语义。

### 10.2 改动面（`internal/scanner/scanner.go` + 三个测试文件）

1. **删** `folder` 类型与 `newFolder`/`fold`/`foldRoot`/`key`（`:139-:220` 一带的折叠段）。
2. **增** `visitKey(p string) string { return keyOf(p, string(filepath.Separator)) }`——
   遍历期唯一的键构造入口（只归一分隔符，不动大小写）。`keyOf`/`underKey` 的文档随之改写
   （"折叠后的路径"→"路径"）。
3. **`WalkWithGate` 换键**：
   - `cleaned, all := dedupeRoots(roots)`（sens 不再出参）；`prefixes := rootPrefixes(cleaned)`
   - `rawKeys[i] = visitKey(all[i])`；逃逸判据 `rootsUnder(rawKeys, all, visitKey(full))`
   - `visited[visitKey(cleaned[i])]` 种子；目录发现处 `key := visitKey(full)`
4. **`dedupeRoots` 收窄签名**：返回 `(kept, all)`——`sens` 变成纯内部值（只在合并时用）。
   单根早退分支**保留**（它的作用是不探测，`TestWalkSingleRootSkipsProbe` 钉着它），
   只是不再为出参构造 sens。§6.2 方案 B 留下的那处"已知不美"（sens 是"没测过"的诚实值
   却要随签名传出）随之消失。
5. **不动**：`fscase` 包、`probeCaseSensitive` 接缝、`rootPrefixes`、`sysguard`、
   阶段 1.5 的物理身份去重。
6. **三条 `folder` 用例作废并原位替换**（E11）：它们的断言对象（`newFolder`/`fold`/`foldRoot`）
   在本项被删除，属 §6.2 预告的"推翻 I2 当年定的机制"那一半。替换物是一条约**更强**的
   纯断言（V1：遍历键在任何根数、任何卷语义下都拼写精确），I2 的实质（探测决定**合并**）
   继续由 `TestDedupeRootsFoldsByProbedVolume` 钉住。原位留说明块，不做静默删除。

### 10.3 探针（修前必红清单）

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `scanner`：`TestVisitKeyNeverFolds`（原位替换三条 `folder` 用例） | 遍历键 = 原样拼写 + 分隔符归一：`visitKey(p) == keyOf(p, sep)`、`visitKey(lower) != visitKey(upper)`（不折）、`keyOf` 的 Windows 真值形态（注入 `\`）；新 API 无根数/卷语义参数 ⇒ 与平台无关 | 修前**编译红**（`visitKey` 不存在）＋语义相反：同位置的旧断言钉的是"两根且有一根不敏感时**要**折叠"，与本节判据正相反 |
| V2 | `scanner`：`TestMultiRootWalkKeepsCaseVariantSubtrees`（E8 构型，Linux 主门禁可跑） | 两根 + 注入"不敏感"：父根下 `alpha/`、`ALPHA/` 各 1 文件 + 另一根 1 文件 → 收 **3** 个（修前 2），`Failed` 空；无敏感卷时 skip | 修前遍历键折叠 ⇒ 两棵并成一棵 ⇒ 2 |
| V3 | `scanner`：`TestCaseMismatchedRootUnderProtectedDirIsNotRescued`（E10/M44 判据钉） | 两根：宽根 + 保护清单内目录（拼写与盘上**仅大小写不同**）⇒ 剪枝生效、`ProtectedDirs>=1`、`UnprotectedRoots` 空（fail-closed） | 修前折叠键让两者匹配 ⇒ 逃逸放行、文件进语料 |
| V4 | 既有两条**保持绿**（反向对照，证明不是"取消折叠"） | `TestDedupeRootsFoldsByProbedVolume`（说不敏感 ⇒ 两根并成一棵）、`TestWalkCaseSensitivityIsProbed`（端到端 2 / 4） | 绿 |
| V5 | 既有四条**保持绿**（遍历键换名不得伤及行为） | `TestWalkSingleRootSkipsProbe`（单根零探测）、`TestWalkSingleRootKeepsCaseVariantSubtrees`、`TestRootInsideProtectedDirStillScanned`（拼写一致时逃逸照旧放行）、`TestExplicitProtectedRootIsReported` | 绿 |
| V6 | 真构型复跑（非用例，E1 的 `hdiutil` 构型） | 两根构型 `files_total=3 failed=0`（真值 3）；单根仍 2 | 修前 `files_total=2`（E3 读数） |

### 10.4 变异（逐条改坏 → 应红）

| # | 变异 | 应变红 |
|---|---|---|
| M-M36-a | `visitKey` 内部改回折叠（`keyOf(fscase.Fold(p,false), sep)`） | V1、V2 |
| M-M36-b | 只把目录发现处的键改回折叠（`key := visitKey(full)` 处），V1 的直接靶子不动 | V2（V1 应仍绿——两条各自盯一个面） |
| M-M36-c | 逃逸判据的键改回折叠（`rootsUnder(rawKeys, all, …)` 处） | V3 |
| M-M36-d | 顺带把 `dedupeRoots` 的合并折叠也删掉（**过度删除**方向） | V4（`TestDedupeRootsFoldsByProbedVolume`、`TestWalkCaseSensitivityIsProbed`） |
| M-M36-e | 种子键改回折叠（`visited[visitKey(cleaned[i])]` 处） | **预期不可杀**：合并后根集两两不成嵌套，遍历走不到第二个拼写 ⇒ 无危害。如实记录（与 M-M26-b 同款处理），不写成"已验证" |

### 10.5 未兑现与边界

1. **逃逸判据不再容忍大小写拼写差异**（E10）：不敏感卷上，用户**手输**一个与盘上拼写仅
   大小写不同的保护清单内路径作根时，剪枝不再放行（fail-closed），后果是剪枝 + `ProtectedDirs`
   计数，而不是"他点名的根本被拒"的专门提示。**与 §6.1"不确定时倾向不折叠"同一口径**，
   且方向是安全侧（宁可少放行，不可误放行——误放行会走进用户从没点过的保护目录）。
   新增登记 **M44**，V3 把这条口径钉成断言。
2. **Windows / Linux 真机读数未取**：本机是 darwin，遍历键的精确比较在 Windows 上只是
   "不比大小写"，但**未在真机取读数**（与 §6.5-2 同档，不写作已通过）。
3. **`res.Visited` 口径再变**：多根时不再按折叠键计数。消费方只有取消用例的松断言，无依赖。
4. **E9 那条兜底未实测**：Linux casefold/CIFS 上"该折没折 ⇒ 同一棵收两遍"由阶段 1.5 的
   物理身份去重吸收，属"有理有据但未实测"（与 §6.5-2 同档）。
5. **V2 在 macOS 上 skip**：父卷不敏感 ⇒ 两棵真目录无法并存，兑现环境是 Linux CI
   （与 C1 的 V3 正好互补：那条在 macOS 因不敏感而 skip、在 Linux 兑现；本条同）。
   本机证据由 V6 的 `hdiutil` 真构型读数承担。

### 10.6 实施后追记（2026-09-21，逐条真读数）

改动落地为 commit `693c9b6`（设计段 `816faf3`），划账在 04 §6.9.10。逐条对照本节的预测：

| 预测 | 实测 | 结论 |
|---|---|---|
| V1 红在"编译红 + 旧断言语义相反" | 编译红如期（新 API）；语义相反由 M-M36-a 复现：`visitKey("/X/Docs/Sub/F.TXT") = "/x/docs/sub/f.txt", want "/X/Docs/Sub/F.TXT"` | 一致 |
| V2 本机 skip、Linux 兑现、真读数 2→3 | 默认 `TMPDIR` 下 SKIP（如期）；**把 `TMPDIR` 指向 `hdiutil` 敏感卷后 PASS**（新发现的兑现途径，三条需敏感卷的用例同批通过）；V6 真构型 `files_total=2 → 3` | 一致，且兑现面比预测宽 |
| V3 修前放行、修后 fail-closed | 修后 PASS；M-M36-c 下三条断言全红（含 `UnprotectedRoots = […/LOST+FOUND/inner]`——修前会报出一个用户没点过的根） | 一致 |
| V4/V5 保持绿 | 全包（含敏感卷 `TMPDIR`）`ok`；`TestDedupeRootsFoldsByProbedVolume`、`TestWalkCaseSensitivityIsProbed`、`TestWalkSingleRootSkipsProbe`、`TestRootInsideProtectedDirStillScanned` 均 PASS（后者在敏感卷 `TMPDIR` 下也跑到了真两棵） | 一致 |
| M-M36-e 不可杀 | 如期不可杀（两种 `TMPDIR` 全包 `ok`），如实记录为"无危害" | 一致 |
| 三条 `folder` 用例被推翻 | 原位替换为 `TestVisitKeyNeverFolds`（+3 −3，源级 `^func Test` 仍 588）；`dedupeRoots` 顺带收窄为两返回值，方案 B 的"已知不美"消失 | 一致 |

一处**口径更正**（与实现无关，属划账口径）：此前 §6.9.8/§6.9.9 记的"PASS 608 = 顶层 RUN 612"
把两级并作一个数——608 是含 71 条子用例的锚定 PASS 行数，612 的 `^=== RUN` 里也含那 71 条
（子用例的 RUN 行不带缩进、结果行带缩进）。本项起按顶层/子用例分层记录，详见 04 §6.9.10。

边界里新登记的一条（E10 兑现）：**M44** —— 逃逸判据的键精确化后，不敏感卷上"手输拼写与盘上
仅大小写不同"的保护清单内路径不再放行剪枝（fail-closed），由 V3 钉成断言。

---

## 11. M28：稀疏卷判据（04 §6.8.8 M28）

> 修的是什么：`reported==0 && size>0` 这一类读数（真全洞文件——本机 APFS 上 `truncate` 到
> 1 GiB 而不写一个字节，读数就是 0 blocks，E1）目前被 M6-P2 的规则 2 一律判成"未统计"，
> 退回逻辑大小。代价是一个 256 MiB 全洞重复组仍按 256 MiB 报可释放（E2，端到端真读数）。
> 本项把"**这个 0 可不可信**"用**本卷自己给出的读数**回答：遍历中在本卷见过 `reported>0`
> 的，其 0 采信为真读数；没见过的卷维持原状（fail-closed）。
> **登记行写的判据形状（`statfs` 的 `f_type` 白名单 + 每卷一次写探测）被本节的取证推翻**，
> 理由在 E3~E6——照它做既拿不到"证据"，还要在只读扫描里引入写盘。

### 11.0 动手前取证（2026-09-21，本机实测 + 读码）

| # | 取证项 | 读数 | 出处 |
|---|---|---|---|
| E1 | ★ 这笔代价在本机是不是活的 | 是。`dd if=/dev/zero of=hole2.bin bs=1 count=0 seek=1073741824` ⇒ `size=1073741824 blocks512=0 dev=16777230`；同目录 `small.bin`(1024 B) ⇒ `blocks512=8`。`go test ./internal/scanner -run TestWalkAllHoleFileNeverReportsZeroActual -v` 落到日志分支 `本卷对全洞报 0 blocks：按『未统计』回退逻辑口径`（PASS） | 本机执行 |
| E2 | ★ 端到端修前读数（`fdd-cli`） | 两套夹具读数**一模一样**：`proven`（两枚 256 MiB 全洞 + 一枚 8 KiB 普通文件）与 `unproven`（只有那两枚全洞）都是 `reclaimable_bytes_actual=268435456`（=逻辑回退）、`reclaimable_actual_known=false`。**unproven 的读数就是"恒报 0 的卷"在数字上的形态**——修后它必须**保持**这样 | 本机执行，本轮 |
| E3 | `f_type` 能不能当**卷**键 | **不能**：`/tmp`（`dev=16777230`）与 `/Volumes/fx`（`dev=16777244`）是两个不同的挂载实例，`f_type` **同为 `0x1a`（apfs）**。按 f_type 聚合证据 = 把两卷读数混成一池，方向是**误信** | 本机执行（`.scratch-m28` 临时程序读 `unix.Statfs`） |
| E4 | `f_type` 能不能退而作**家族白名单** | 能分家族（apfs `0x1a` / devfs `0x13`），但它回答的是"这是什么文件系统"，不是"本卷的 `st_blocks` 会不会报非零"——两者只在"驱动器正常"时才重合。白名单另有两个烂尾方向：未知类型只能 fail-closed（收益拿不到），Linux 要枚举 ext4/btrfs/xfs/…、Windows 侧**根本没有对应物**（GCPSW 不是 statfs 家族） | 本机执行 + 读码 |
| E5 | 登记的"每卷一次写探测"代价 | 扫描全程**只读**（本轮唯一写盘探测是 fscase 那次，且已按 C1 收窄到"≥2 根才探"）。为 M28 再引入按卷写盘，会在三类真实工况上出问题：① 同步客户端把新临时文件当用户资产（M6-P1 整节的动机场景）；② 只读挂载/无写权限目录直接失败（fail-closed 等于白探）；③ 崩溃/被杀留残留。**证据**这条路一次盘都不用碰 | 读码 + 设计 |
| E6 | 证据从哪来、要多少成本 | 本卷**已经给出的读数**：`reported>0` 就是"该卷在 `st_blocks` 里报真实分配"的直接证据，同卷任何一个非空普通文件都有（E1 的 `small.bin`、`/etc/hosts` 均为 8 blocks512）。遍历本来就会 `Info()` 每个文件 ⇒ **零额外 syscall、零写盘** | 读码 + E1 |
| E7 | 判定能不能在遍历中做 | **不能**：worker 并发，证据可能来自"还没遍历到的文件" ⇒ 读数取决于线程调度快慢，同一语料两次扫描可能不同（与"计数不许说谎"同族的纪律）。判定放**收尾**（`workerWg.Wait()` 之后），与 `escapedRoots`/`protectedDirs` 的收口同一处 | 读码 |
| E8 | 对手（NFS/FUSE 恒报 0）本机能不能真造 | 不能：本机没有可挂的 NFS/FUSE 服务端（起 `nfsd` 要改系统配置、要 root），**真读数未兑现**（§11.5-2）。但对手的**形态**在主门禁上可以完整复现：「本卷没有任何非零证据」= 只放全洞文件的目录（E2 的 `unproven`） | 读码 |
| E9 | 跨卷对照能不能在测试里造 | 能（本机）：`/dev/null` 在 devfs 上（`dev=-370076309`，`f_type=0x13`），与 `t.TempDir()` 的 apfs 卷**不同 dev**。Linux CI 上要看 `/dev` 是否独立挂载——同卷则 skip（不进隔离清单，§7 约定） | 本机执行 |
| E10 | 目录的 `blocks` | 也是 0：`/` 与 `/System` 都报 `blocks512=0`。**"0 blocks"在 APFS 上不是"洞"的同义词**，也说明目录读数不能充当证据（本项只采信普通文件） | 本机执行 |
| E11 | `mkfile -n 1g` 的顺带读数 | `blocks512=32`（16 KiB），不是 0——与 §3.0 记的 `mkfile -n 8m` 占 8 MiB 不同源（同族命令在不同尺寸下落地策略不同）。与判据无关；**夹具一律用 `os.Create`+`Truncate`**（= E1 的 `dd seek` 同机制），不用 `mkfile` | 本机执行 |
| E12 | 既有用例受影响面 | `TestWalkAllHoleFileNeverReportsZeroActual`（04 §6.9.3 的 U6）的**夹具**（目录里只有全洞文件）正好落在"无证据 ⇒ 未统计"一侧 ⇒ **断言与读数全部不变**，只需改函数注释（"开放项 M28"→"已实施，见 §11"）与 `probeActual`（它用的 `Of` 被删，改走 `Reported`+`From`，语义一字不变）。`TestWalkActualKnownForPlainFile`/`TestWalkRecordsSparseActualBelowSize`/`TestWalkZeroSizeFileStillSkipped` 不受影响 | 读码 |

### 11.1 判据

"一个 0 blocks 是不是真读数"**只对卷有意义**，对单个文件无意义（M6-P2 已论证：单文件不可区分
"全洞"与"该卷恒报 0"）。本项用**本卷的读数**回答这个问题，判据只有一句：

> **本卷是否被这一趟遍历证明会报非零实占**（同卷、读到了、非零，三者同时成立）。

判定表（只动第四行；其余三行逐字保持 M6-P2 的口径）：

| 情形 | M6-P2 处置 | M28 之后 |
|---|---|---|
| `!ok`（平台读不到） | `(size, false)` | **不变**——读不到就是读不到，**卷级证据不翻案**（证据说的是"0 可信"，不是"读得到"） |
| `ok && reported>0` | `(reported, true)` | **不变**；它同时构成该卷的证据 |
| `ok && reported==0 && size>0`，**无**证据 | `(size, false)` | **不变**（fail-closed：恒报 0 的卷挡在这） |
| `ok && reported==0 && size>0`，**有**证据 | `(size, false)` ← 收益丢失 | **`(0, true)`** ← 本项解锁 |
| `size==0` | `(0, true)`（遍历期本就跳过 0 字节） | 不变 |

三条不变式，写死在实现里：

1. **证据不跨卷**：`VolumeID` 不同就不互相作证（E3：同一个 `f_type` 都不算同卷）。
2. **`!ok` 与证据无关**：读失败的条目即便本卷别处有非零证据，仍走 `(size, false)`。
3. **判定在收尾统一做**（E7）：遍历期一律按"无证据"出值（= 今天的行为），收尾只做一件事——
   把有证据的卷上的"未统计 0"重判。**无证据时（今天的所有工况）行为逐字节不变。**

两处口径选择（都不是随手取的）：

- **证据在过滤之前采集**：卷会不会报非零，与用户勾了什么过滤无关；被 `ExcludeExts`/大小/隐藏
  规则挡掉的文件同样作数。放在 `matcher.Apply` 之后就变成"语料里恰好有普通文件才有收益"，
  那是把用户过滤当成了卷属性。
- **只采信普通文件**（`IsRegular` 之后采集）：目录的 blocks 在 APFS 上恒 0（E10），
  设备/管道一类更不该参与；0 字节文件报 0 没有信息量。

收益边界：本项**只解锁** `reported==0 && size>0` 这一类（全洞文件）。`!ok` 一类（平台读不到、
非 NTFS 卷、FUSE 不支持）**不**解锁——没有读数可以采信。

### 11.2 改动面（1 个新语义 + 4 个生产文件 + 2 个测试文件）

1. **`internal/realbytes/realbytes.go`**（无 tag，全部判定与回退仍在这一处）：
   - `From(size, reported uint64, ok bool)` → **`From(size, reported uint64, ok, trustsZero bool)`**：
     第四参只作用于 `reported==0 && size>0` 那一分支（`trustsZero ⇒ (0,true)`）。
   - **删** `Of`：它的语义就是"不带证据的单文件判定"，两个调用点一个要走证据链（scanner）、
     一个要读原始读数（测试前提探针），留着它就是一处"没有证据也能判"的入口。
   - `reported` **改名导出为 `Reported(path, info)`**：平台层唯一的读数入口（只报数不判定）。
   - **新增 `VolumeID(path string, info os.FileInfo) (uint64, bool)`**：卷（挂载实例）标识。
   - **新增 `Tracking`**（纯集合，非并发安全：每 worker 一份、收尾 `Merge`——热路径每文件一次
     `Observe`，刻意不加锁）：`Observe(vid, reported uint64, ok bool)`（`ok && reported>0` 才算
     证据）、`Trust(vid) bool`、`Merge(*Tracking)`。
2. **`internal/realbytes/realbytes_unix.go`**：`reported`→`Reported` 改名；`VolumeID` = `Stat_t.Dev`
   （`info.Sys()` 断言失败 ⇒ `(0,false)`，fail-closed）。
3. **`internal/realbytes/realbytes_windows.go`**：`reported`→`Reported` 改名；`VolumeID` =
   `filepath.VolumeName(path)` 的 FNV-1a 64（`C:` / `\\server\share` / `\\?\Volume{…}` 都给出稳定
   前缀；空串 ⇒ `(0,false)`）。哈希只为把键统一成 `uint64`，冲突方向是"两卷证据混池"（§11.5-6）。
4. **`internal/realbytes/realbytes_other.go`**：`Reported` 恒 `(0,false)`；`VolumeID` 恒 `(0,false)`
   （其他 unix 变体不按猜的字段偏移读，与 `fsid_other.go` 同一处置）。
5. **`internal/scanner/scanner.go`**：worker 装配区加 `tracking := make([]*realbytes.Tracking, workers)`
   与 `pendZero := make([][]zeroCase, workers)`；候选文件处（`:424` 一带）改为
   `rep, rok := realbytes.Reported(full, info)` → `vid, vok := realbytes.VolumeID(full, info)` →
   `if vok { tracking[idx].Observe(vid, rep, rok) }`（**在 `matcher.Apply` 之前**）→ 建条目后
   `if vok && rok && rep == 0 { pendZero[idx] = append(pendZero[idx], zeroCase{e: e, vid: vid}) }` →
   `e.Actual, e.ActualKnown = realbytes.From(size, rep, rok, false)`；收尾合并 tracking 后
   `for _, z := range pendZero 全量 { z.e.Actual, z.e.ActualKnown = realbytes.From(z.e.Size, 0, true, tr.Trust(z.vid)) }`
   （单条重判也走 `From`——I5：一处判定一处实现）。
6. **测试**：`internal/realbytes/realbytes_test.go` 三条既有用例**机械加第四参 `false`**
   （断言一字不动，钉的仍是 M6-P2 规则 1/2/3）+ 新增 V1~V3；`internal/scanner/scanner_realbytes_test.go`
   的 `probeActual` 改走 `Reported`+`From(…,false)`（语义不变）、U6 注释更新、新增 V4~V6。
7. **不动**：`internal/model`（`ActualBytes` 对 `(0,true)` 已正确返回 0）、`hash_cache` 列集
   （U7 钉着"实占不进缓存"）、`ScanSummary`/`fdd-cli` 字段、所有平台读数实现本身的算术。

### 11.3 探针（修前必红清单）

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `realbytes`：`TestFromTrustsZeroOnlyWhenVolumeProven` | 四向：`(1MiB,0,true,false)→(1MiB,false)`（M6-P2 原状）、`(1MiB,0,true,true)→(0,true)`（收益）、`(1MiB,4096,true,true)→(4096,true)`（非零不受影响）、`(1MiB,0,false,true)→(1MiB,false)`（**读不到不因卷证据翻案**，不变式 2） | 修前**编译红**（`From` 无第四参） |
| V2 | `realbytes`：`TestTrackingOnlyRecordsNonZeroEvidence` | `Observe(v,0,true)` 不成证据；`Observe(v,4096,false)` 不成证据（`ok` 是合约的一部分，不因当下平台实现恰好报不出这种组合而省略）；`Observe(v,4096,true)` 成立；**`Trust(另一卷) == false`**（不变式 1，不池化） | 修前编译红（`Tracking` 不存在） |
| V3 | `realbytes`：`TestTrackingMergeKeepsKeysSeparate` | 两份各记一个卷，`Merge` 后两个键各自成立、未记的第三个键不成立 | 同上 |
| V4 | `realbytes`（unix）：`TestVolumeIDSameDirSameID` | 同目录两个文件同 ID 且 `ok`；跨卷用 `/dev/null`（E9）对照——**不同 ID 才断言，同 ID 则 `t.Skipf`**（该环境没有第二个卷，夹具不成立） | 修前编译红（`VolumeID` 不存在） |
| V5 | `scanner`：`TestWalkAllHoleOnProvenVolumeReportsRealZero` | 目录 = 全洞 64 MiB（E1 机制）+ 1 KiB 普通文件 ⇒ 全洞条目 `Actual==0 && ActualKnown==true` 且 `ActualBytes()==0`；普通条目 `ActualKnown && Actual>0`。前提自探：夹具 `Reported != 0` ⇒ `t.Skipf`（该卷把洞落地分配） | 修前**语义红**：`reported==0` 一律回退 ⇒ `Actual=64 MiB`（变异下可复现，M-M28-a/c） |
| V6 | `scanner`：`TestWalkAllHoleOnUnprovenVolumeStaysUnknown` | 目录里**只有**全洞文件 ⇒ 无任何非零证据 ⇒ 条目 `Actual==size && !ActualKnown`（E2 的 `unproven` 形态，fail-closed 的钉） | 修前**绿**（正是今天的口径）——它的作用是让 M-M28-b/c 会红 |
| V7 | `scanner`：`TestWalkEvidenceIgnoresFilters` | 目录 = 全洞文件 + 一枚被 `ExcludeExts` 挡掉的普通文件 ⇒ 全洞条目仍 `(0,true)`（证据与被过滤无关，§11.1 口径选择） | 修前语义红（同 V5 形态） |
| V8 | 既有四条**保持绿** | `TestFromFallsBackWhenPlatformReportsZero`（规则 2 原状）、`TestWalkAllHoleFileNeverReportsZeroActual`（U6，夹具落在无证据侧）、`TestWalkActualKnownForPlainFile`、`TestWalkRecordsSparseActualBelowSize` | 绿（无证据路径逐字节不变） |
| V9 | 真读数（非用例，`fdd-cli` 两套夹具） | `proven`：修前 `reclaimable_bytes_actual=268435456 / known=false` ⇒ 修后 `0 / true`；`unproven`：修前修后**同读数**（`268435456 / false`） | 修前读数已取（E2） |

### 11.4 变异（逐条改坏 → 应红）

| # | 变异 | 应变红 |
|---|---|---|
| M-M28-a | 删掉证据采集（`tracking[idx].Observe(...)` 一行） | V5、V7 |
| M-M28-b | `Trust` 恒真（不看集合） | V6（无证据目录被误采信） |
| M-M28-c | 删掉收尾重判（`pendZero` 整段） | V5、V7 |
| M-M28-d | `Trust` 忽略卷键（`return len(t.seen) > 0`，池化） | V2、V3 |
| M-M28-e | `Observe` 丢掉 `ok` 条件（`if reported > 0` 就记） | V2（`Observe(v,4096,false)` 那条） |
| M-M28-f | 收尾重判时把 `!ok` 的条目也翻案（例如对 `From` 传 `ok=true`） | V1 第四向 |
| M-M28-g | 证据采集挪到 `matcher.Apply` 之后 | V7 |

> M-M28-e 的说明：当下两个平台实现里 `ok=false` 必然伴随 `reported=0`（unix/Windows 都是这样返回的），
> 所以这条变异在**端到端路径上不可杀**；它杀在纯函数层（V2）——那是**合约**的一部分：
> "读不到"与"读到 0"是两件事，将来任何一个平台实现只要破了这个组合，就会被 V2 立刻抓住。

### 11.5 未兑现与边界

1. **Windows 腿未兑现真机**（与 M6-P2 同档）：`VolumeID` 走 `filepath.VolumeName` + FNV、
   读数走 `GetCompressedFileSizeW`，都只有 `GOOS=windows go vet` 的交叉编译保证；
   "GCPSW 对全洞稀疏文件返回 0"只有 MSDN 依据，**没有真机读数**。不写作已通过。
2. **NFS/FUSE "恒报 0" 的真机未兑现**（E8）：本机无服务端可挂。防线本身的形态由 V6 在主门禁上
   钉住（无证据 ⇒ fail-closed），但"真实 sshfs/s3fs 的读数长什么样"本轮没量过。
3. **"偶发报 0"的故障驱动不在防线内**：证据判据回答的是"本卷会不会报非零"。一个对多数文件报
   真值、对个别文件瞎报 0 的驱动，仍会被采信——单文件无从证伪。这是拿"整卷收益"换来的取舍，
   与 M6-P2 当年"整卷放弃"的选择相反，方向已由用户裁定受理（本项即其解锁条件）。
4. **全洞语料卷拿不到收益**：该卷上被 stat 到的普通文件若**全是**全洞（或全被剪枝），就没有
   证据 ⇒ 维持未统计。刻意如此：没有可证伪的证据就不采信。
5. **UI 未呈现**（裁定 ②/③）：本轮只到 `FileEntry.Actual/ActualKnown` 与 `fdd-cli` 的组级聚合；
   "实占未知"与"真 0"在界面上的区分属 M8。划账不得因后端字段齐了就记兑现。
6. **Windows 的 FNV 冲突**：两卷哈希相撞时证据会混池（方向是"更易被采信"）。概率约 2⁻⁶⁴，
   且需要其中一卷先产生非零证据；记在此处，不写进代码。
7. **证据的可见面**：只来自"被 stat 到的普通文件"——目录（E10）、符号链接（跳过）、非普通文件、
   被剪枝/未遍历到的子树都不参与；0 字节文件报 0 无信息量。
8. **计数无变化**：M28 不跳过任何文件，`ScanSummary` 的任何一个计数都不动（收益只体现在
   `Actual`/`ActualKnown` 与组级 `reclaimable_actual`）。

### 11.6 实施后追记（2026-09-21，逐条真读数）

改动落地为 commit `333a9ba`（设计段 `641a7b1`），划账在 04 §6.9.11。逐条对照本节的预测：

| 预测 | 实测 | 结论 |
|---|---|---|
| V1/V2/V3 红在"编译红" | 编译红如期（新 API/新类型不存在）；语义那一面由 M-M28-d/e/f 三条变异复现，报错原文见 04 §6.9.11 | 一致 |
| V5/V7 修前语义红（`reported==0` 一律回退） | M-M28-a/c 下 V5、V7 双红（`全洞条目 ActualKnown = false…`、`全洞条目 = (67108864,false), want (0,true)…`）；这两条变异下 V6 均绿 | 一致 |
| V6 的作用是让"信任判据被放宽"会红 | M-M28-b 下 V6 红（`ActualKnown = true…`、`Actual = 0, want 67108864`），V5/V7 绿 | 一致 |
| M-M28-e 端到端不可杀 | 如期：纯函数层 V2 红、端到端三条全 `ok`（`ok filededup/internal/scanner 0.380s`）——如实记录为"合约层的钉子"，与 M-M36-e / M-M26-b 同款处理，不写作已验证 | 一致 |
| M-M28-f 由 V1 第四向杀 | 一致（`读不到不因卷证据翻案：From(1MiB,0,false,true) = (0,true), want (1MiB,false)`）。变异落在 `From` 的判定顺序上（`trustsZero` 越过 `!ok`）——"`!ok` 不可被卷证据翻案"这条不变式的唯一载体就是它（I5） | 一致 |
| V4 真机跨卷对照 | 本机两条 PASS（`/dev/null` 在 devfs 上，与临时目录 apfs 不同 dev，E9） | 一致 |
| V9 真读数：`proven` 0/true、`unproven` 保持同读数 | 一致：`proven` = `reclaimable_bytes_actual=0 / reclaimable_actual_known=true`（组行 `reclaimable_actual=0`，stderr "可释放 256.0MB（实占 0B）"）；`unproven` = `268435456 / false`（"实占 256.0MB"，与修前逐字同） | 一致 |
| 计数无变化（§11.5-8） | 两组夹具的 `stats` 除实占两项外同值（`files_total` 3/2、`groups` 各 1、`failed` 0） | 一致 |

**实施中新增/更正的事实**：

1. **门禁抓到一处漏改**：`internal/dedup/realbytes_e2e_test.go:149:29` 仍在调用被删的 `realbytes.Of`，
   由首轮 `GOOS=linux go vet` 的编译红抓出（vet 会把全平台测试文件一起编译）。已在同一 commit 内
   改为 `Reported`+`From` 两步调用——这正是 `Of` 删除后"两步合流"口径的最后一个调用点。
2. **用例数**：源级 `^func Test` **596**（+8 = realbytes 5 + scanner 3）；顶层结果行 **549**
   （PASS 545 / SKIP 4 / FAIL 0）；子用例 71（PASS 70 / SKIP 1）。四条顶层 SKIP 是
   `TestMoveFileCrossDeviceReal` 与三条需敏感卷的大小写用例（C1/M36 的），与本项无关；
   本项新增的探针在默认 `TMPDIR` 下**全部实跑**、无 skip。
3. **既有用例的语义不变是逐字节级的**：`probeActual`（`scanner`）与 `requireU4Sparse`（`dedup`）
   都改成"先 `Reported` 再 `From(…, false)`"，与 `Of` 当年的两步完全同义——U6 断言一字未动而仍绿
   即此事的证据。
4. **`realbytes_volume_unix_test.go` 带 tag**（`darwin || linux`）符合 H6 分层：判定在无 tag 文件、
   平台读数在 tag 文件；它钉的是"卷标识是物理量"（同卷同值、跨挂载实例不同值）。
5. **§11.5-1 的开销面已在代码里挂账**：`scanner.go` 采集点注释直接指向 M29（Windows 腿由"每候选"
   扩到"每普通文件"），兑现仍待 Windows 真机。

---

## 12. M30：TS 类型与 Go 下发字段的对齐（04 §6.8.8 M30）

> 本项**没有生产代码的行为变化**：补的是 `frontend/src/wails.ts` 的类型声明，加的是**门禁**。
> 动因是登记行里那句话——"Go 侧 JSON 一律照发，TS 侧不声明就取不到，于是 M8 做呈现时的第一个
> 动作必然是补类型，而那时很难分清这个字段后端到底有没有下发"。补字段本身不难，难的是**让它
> 不再漂移**：所以本项的交付物是一个进根包门禁的静态比对器，补字段只是它抓出来的第一批账。

### 12.0 动手前取证（2026-09-21，读码 + 一次性比对脚本）

| # | 取证项 | 读数 | 出处 |
|---|---|---|---|
| E1 | 镜像面到底有多大 | 只在 `frontend/src/wails.ts`：24 个 `export interface`，另有 `BackendAPI`（方法面）与 `OpKind`（字面量联合）两个非数据镜像。`stores/`、`utils/`、`components/` 里的类型（`Toast`/`ReclaimLine`/markdown 的 `Block`/`Seg`/`IconDef`）纯前端，无 Go 对应物 | 读码 |
| E2 | ★ 修前真读数（24 对逐对双向比对） | **3 个接口、9 个字段缺失**：`ScanSummary` 6（`reclaimableActual`/`protectedDirs`/`protectedFiles`/`skippedCloudFiles`/`skippedWorkTempFiles`/`unprotectedRoots`）、`GroupView` 2（`reclaimableActual`/`actualKnown`）、`PagedResult` 1（`totalReclaimableActual`）。**登记行只记了 `ScanSummary` 那 5+1 条**——M6-P2 同批加给 `GroupView`/`PagedResult` 的两处（共 3 个字段）被漏记，缺口面比登记大 | 一次性脚本，本轮 |
| E3 | 反向（TS 有 Go 无）有几处 | 0 处——但 `OpRecordItem` 的 `isSymlink`/`dangling` 暴露出**映射对错了**：TS 镜像的是 `app.go` 的 `main.OpRecordItem`（匿名嵌入 `history.OpItem` + 两枚标注字段），不是 `history.OpItem` 本身。比对器必须按 `encoding/json` 的规则做**匿名嵌入提升**，否则这对会永远假红 | 脚本 + 读码 |
| E4 | 有没有比对器覆盖不到的对 | 两处，都要**显式豁免且带理由**：`OpsFiltered`（Go 侧是 emit 点的 `map[string]any` 字面量，`app.go:1766`，无结构体可反射）、`OpKind`（字面量联合；Go 侧用字面量 `case` 校验，`executor.go:391`，没有常量集合）。豁免写进比对表，不许在代码里静默 `continue` | 读码 |
| E5 | 两侧"字段名"怎么取才同口径 | Go 侧走 `reflect` 而非解析源码：有效 JSON 名 = 有 tag 取 tag 名（`json:"-"` 跳过、`omitempty` 不改名）、无 tag 取字段名、匿名嵌入提升——与 `encoding/json` 同一套规则（`history.OpItem.Hash` 的 `json:"-"` 就是现成的用例）。TS 侧没有类型系统可依赖，只能解析文本（小纯函数） | 读码 |
| E6 | 比对器放哪 | **根包测试**（`wails_types_test.go`，`package main`）。理由：`ScanSummary`/`FileView`/`OpRecordItem` 定义在 main 包，**任何 `cmd/` 程序都 import 不了**；而根包测试本来就在门禁里（`go test -race -count=4 .`）⇒ 不新增门禁行、不复制类型定义（复制正是要防的漂移） | 读码 |
| E7 | 修前必红的形态 | 比对器写好后**不改 TS 直接跑**即红，逐条列出那 9 个字段（E2 清单）——这就是"修前必红"的直接证据，不需要再做一次人工比对 | 设计 |

### 12.1 判据

1. **比对单位是接口对**（TS interface ↔ Go 类型），两侧字段名统一为**有效 JSON 名**。
2. **两个方向都判红**：缺失（Go 有 TS 无）= 前端按类型取不到；多余（TS 有 Go 无）= 前端读一个
   永远 `undefined` 的字段。两种都是"类型在说谎"，只是方向不同。
3. **覆盖性自证**：`wails.ts` 里每个 `export interface` 必须在映射表或豁免表里——防"新增接口
   忘了登记"；反向也查（映射表里的 TS 名必须在文件里存在），防"接口改名后表里留了个幽灵"。
4. **H6 分层**：解析与比对是**无 IO 的纯函数**（`tsInterfaceFields` / `goJSONNames` /
   `compareFieldSets`），各自有用例；读文件与反射只出现在测试壳里。

### 12.2 改动面

1. `frontend/src/wails.ts`：补 9 个字段（带上与 Go 侧同口径的注释——尤其"可能未统计"那几条）。
2. 新增 `wails_types_test.go`（根包）：映射表（24 对 + 2 条豁免）+ 三个纯函数 + 三条断言。
3. 文档：04 §6.9.12 划账；本稿 §0 行 11。
4. **不动**：Go 侧任何结构体、`app.go`、CI 配置（门禁已有根包测试行）、以及 `stores/` 等纯前端类型。

### 12.3 探针（修前必红清单）

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `TestTSInterfaceFieldsParsesRealFile` | 解析器纯函数：跳过注释与空行、认 `?` 可选、认内联对象类型（`Failed: { Path: … }[]` 里的 `Path` 不算本层字段）、认嵌套 `{}` 不提前收口 | 新函数不存在 ⇒ **编译红** |
| V2 | `TestWailsTypesCoverGoFields`（真文件，双向） | 24 对逐对：缺失集合与多余集合都为空，报错时列出**接口名 + 字段名 + 两侧口径** | **修前语义红**：9 条缺失（E2 清单） |
| V3 | `TestGoJSONNamesFollowsEncodingJSON` | 有效名规则（合成结构体，不依赖真类型）：无 tag→字段名、`-`→跳过、`omitempty`→不改名、匿名嵌入→提升 | 编译红（函数不存在） |
| V4 | `TestWailsInterfacesAreAllMapped` | 覆盖性：文件里每个 `export interface` 在映射表或豁免表里；映射表里每个 TS 名在文件里存在 | 编译红 |
| V5 | 既有门禁**保持绿**（反向对照） | 补字段后 `npm run typecheck`、根包 `go test -race -count=4 .`、`go vet` ×3 | 绿 |

### 12.4 变异（逐条改坏 → 应红）

| # | 变异 | 应变红 |
|---|---|---|
| M-M30-a | 从 TS 的 `ScanSummary` 删一个刚补的字段（如 `protectedDirs`） | V2（缺失侧） |
| M-M30-b | 给 TS 的 `FileView` 加一个 Go 侧没有的字段 | V2（多余侧） |
| M-M30-c | 映射表把 `OpRecordItem` 指回 `history.OpItem`（E3 修前的错映射） | V2（多余侧：`isSymlink`/`dangling`） |
| M-M30-d | 解析器丢掉 `?` 可选字段（正则去掉 `\??`） | V1 + V2（`OpRequest.ProcessDirs?` 等会报缺失） |
| M-M30-e | `goJSONNames` 不处理 `json:"-"`（把 `Hash` 当字段） | V3 + V2（真文件上 `OpRecordItem` 会多出 `Hash`） |
| M-M30-f | `goJSONNames` 不处理 `omitempty`（名取成 `x,omitempty`） | V3 + V2 |

### 12.5 未兑现与边界

1. **两处显式豁免**（E4）：`OpsFiltered` / `OpKind` 没有可反射的 Go 对象，比对器不覆盖；
   豁免理由写在比对表里。若将来把 `ops:filtered` 载荷升级成结构体、或把 kind 收敛成常量集合，
   应随之收编（本项不做）。
2. **只比字段名，不比类型**：`size: number` ↔ `uint64`、`[]string` ↔ `string[]` 这类"类型形状"
   不在判据内——需要一套 Go↔TS 类型映射规则，且 Wails 下发的 JSON 形状与 Go 类型并非一一对应
   （指针、接口、`omitempty` 都会改形状）。本项只保证"字段存在且名字对"。
3. **不覆盖 `wails.ts` 之外的前端文件**：E1 已确认其余类型无 Go 对应物；将来若新增镜像面，
   要加进映射表（覆盖性断言只在 `wails.ts` 内自证）。
4. **UI 未呈现**（裁定 ②/③）：本项只补类型声明，不新增任何界面呈现；补字段 ≠ M8 开工。
5. **`ScanSummary` 的两处口径缺口仍是缺口**：`history.db` 没存 `skippedCloudFiles`/
   `skippedWorkTempFiles`/保护清单三项，从记录页恢复的历史摘要只能显示"未统计"——
   本项只让**类型**如实描述这件事，不改变数据来源。

### 12.6 实施后追记（2026-09-21，交付 `d4af53f`，划账 04 §6.9.12）

**预测 vs 实际**：

| # | 设计段预测 | 实际 |
|---|---|---|
| V1/V3/V4 | "编译红"（函数不存在） | 实现落地后立即转绿；各条的**语义**红由 M-M30-d/e/f 复现（原文进 04 划账 V1/V3 行） |
| V2 修前必红 | 9 条缺失（E2 清单） | 比对器写好后**不改 TS 直接跑**即红，逐字命中（顺序 = Go 声明序），未做第二次人工比对（E7 兑现） |
| 变异 M-M30-a~f | 逐条应红 | **六条全杀**，与预测的变红面一致；无"不可杀"变异 |
| 门禁 12 行 | 全绿（`smoke-symlink.sh` rc=2 属合法 SKIP） | 一致，逐条真读数进 04 §6.9.12 |
| 用例重数 | — | 源级 **600** / 顶层 **553**（PASS 549 + SKIP 4）/ 子用例 **75** / `^=== RUN` **628** |

**新事实（设计段没写到的）**：

1. **比对表实际是 23 对 + 3 条豁免**（§12.2 写的是"24 对 + 2 条豁免"）：E4 点名的两条之外，
   `BackendAPI` 也**显式登记豁免并写理由**（E1 只说它"非数据镜像"）。26 个导出名字——
   25 个 `export interface` + `OpKind` 一个 `export type`——全部有归属，覆盖性断言全绿。
2. **V4 首轮有一处实现自身的假红**：`!declared[p.ts]` 把 map 的**值**当**存在性**判——
   `OpKind` 是 `export type`（值恒 false）于是被报"在 wails.ts 里不存在"，而它明明在。
   改 `if _, ok := declared[p.ts]; !ok` 后转绿。教训与 K 批同款：**新断言自己要先红一次**——
   这里是它自己把假红暴露出来的，若当时顺手删掉这条"看不懂的报错"就丢掉了整条覆盖性防线。
3. **M-M30-e 的报错与预测措辞不同**：预测"`OpRecordItem` 会多出 `Hash`"，实际多出的是 `-`
   ——tag 名胜出，字段名不参与有效名。变异表里那句"（把 `Hash` 当字段）"只在"注释被清空、
   无 tag 名"的假设下成立；真读数进 04 §6.9.12，以真读数为准。
4. **`goJSONNames` 对"未导出类型嵌入"多走了一步**：设计只说"匿名嵌入提升"；实现按
   `encoding/json` 的真实规则——未导出的**非结构体**嵌入才丢，未导出的**结构体**嵌入仍提升
   （其导出字段照常入池）。合成用例的 `jsonNameSynthEmbedded` 恰是未导出类型，V3 因此
   同时钉住了这条"多数人以为会被丢掉"的规则。
5. **补必填字段零构造点报错**：`vue-tsc` 全绿——前端从不构造这些对象字面量（都从后端来）。
   反向的验证同理成立：将来若前端手工构造 `ScanSummary`，`npm run typecheck` 会立刻抓出缺字段。
6. **产物逐字不变**：`dist/assets/index-DQDgG5FN.js 152.55 kB` 与 §6.9.11 记录**同名同大小**
   ——类型声明不进 bundle；`npm run build` 的产物可比性因此不受本项影响。

---

## 13. M27：CoW 克隆的实占双计（04 §6.8.8 M27）

> 本项**没有生产行为变化**。交付三件事：(1) 登记行缺的那个前提——**能进 CI 的克隆夹具**
> 与真读数；(2) 把"系统性高估"从**推断**坐实为算术 + 读数；(3) 把修向定形并挂账。
> 登记行原文"克隆卷上『实占』应显示为**下限**"经 §13.1 的算术更正为**上限**——
> 原行不改写，挂账在 04 §6.9.13 与本稿 §13.6。呈现（含"至多"措辞）属 M8（裁定②/③）。

### 13.0 动手前取证（2026-09-21，本机 darwin/arm64，APFS；夹具 8 MiB）

| # | 取证项 | 读数（真） | 出处 |
|---|---|---|---|
| E1 | 能不能在测试里造出克隆（登记行缺的前提） | 能，且可自证：写 8 MiB 后卷可用空间 Δ=**8,396,800 B**（量具看得见分配）；`/bin/cp -c` rc=0 后 Δ=**0 B**（克隆占零）。两条一起才构成"真的克隆了"的证据 | 一次性程序，本轮 |
| E2 | 双计 | src/dst 各自 `size=8388608`、`st_blocks×512=8388608`（**各自报满额**）；`ino` 各异（74121913/74121914）、`nlink=1` ⇒ M6-P2 的 FileID 去重/硬链接防线**对克隆完全看不见** | 同上 |
| E3 | 删除一份克隆真正释放多少 | **Δ=0 B**（删 dst 前后可用空间一字不差）——"可释放 8 MB"在这个夹具上是 100% 虚报 | 同上 |
| E4 | 产品口径（`fdd-cli` 真跑，非用例） | `reclaimable_bytes_actual=8388608`、`reclaimable_actual_known=true`；组行 `reclaimable_actual=8388608`；stderr：`完成: 1 组 / 可释放 8.0MB（实占 8.0MB）` | 本轮真跑 |
| E5 | 交叉工具同样被欺 | `du -h` 报目录 **16M**（两份各 8.0M）——`du` 与 `st_blocks` 同源，用户拿系统工具自查也查不出 | 本轮真跑 |
| E6 | 有没有用户态 extent 映射（per-file 检测的唯一指望） | 探了三路都不通：libc `fcntl(fd, F_LOG2PHYS, &l2p)` = errno 14（同一 harness 的 `F_GETFL` 对照 r=0 ⇒ 调用形状无误）；`F_LOG2PHYS_EXT` 同；Go `syscall.Syscall(SYS_FCNTL, …)` 形式返回 0 但回填值是垃圾（`contig=-2395422420551860224`）。两路读数**互相矛盾** ⇒ 结论只到"没跑通一条能读 extent 的路"，**不写**"APFS 必然返回 EFAULT"这类更强的表述；本机无 HFS 卷，**没有正对照** | 一次性探针，本轮 |
| E7 | Go 里能不能直接调 `clonefile` | `golang.org/x/sys@v0.48.0` **有** `unix.Clonefile`（darwin amd64/arm64，`zsyscall_darwin_*.go:1093`）——§3.0 那句"Go 无克隆绑定"指的是 **stdlib**（GOROOT 的 `zsysnum_darwin_arm64.go` 实测无 `SYS_CLONEFILE`）；仓库政策是**零第三方依赖**（`internal/ops/winreg_windows.go:12`），故夹具走 `/bin/cp -c`：零依赖，且与 §3.0/#2、#4 的既有读数是同一条路 | 模块缓存 + GOROOT + 读码 |
| E8 | 目标平台面 | darwin：`statfs.Fstypename=="apfs"` 本机直接可读；btrfs/reflink 与 ReFS 本机不可造（无 btrfs 卷、无 Windows）⇒ 这两条腿的本项读数**未兑现** | 读码 |

### 13.1 判据

1. **方向是"上限"不是"下限"**（更正登记行，算术如下）。记 `S` = 克隆对各自报的实占（=整份），
   `T` = 删除其一真正释放的字节：
   - 产品"可释放"报 `S`（E4），而 `T=0`（E3）⇒ `T ≤ S`，所报数值是真相的**上界**；
   - 组内实占合计 `2S`（E2）> 该对**物理占用 `S`**（E1 的 Δ=0）⇒ 合计同样是上界。
   故呈现应写"(至多) S"或"可能明显偏大"，而**不是**"至少 S"。登记行里的"下限"在此更正
   ——这两个字直接决定文案方向，写反了就是把虚报伪装成保守。
2. **per-file 不可检测**（E6）：没有可用的用户态 extent 映射 ⇒ 无法逐文件判"这份是不是克隆"。
   任何"对克隆对折一次"的修法都会退化成"对同卷**所有**重复组打折"——那是**无依据地改写数字**，
   比高估更坏（M22 的同一条主线：数字可以少说，不许编）。
3. **判据形态**：能进 CI 的夹具 + 两条钉子：(a) 克隆对两份经**产品自己的 API**（`Reported`+`From`）
   各自报满额实占；(b) 删除其一真正释放 ≪ 整份。**前提**（真造出克隆）由卷可用空间增量证明，
   证明不了就 skip——fail-closed，不降级成"同卷模拟"（与 `smoke-symlink.sh` 同款纪律）。
4. **H6 分层**：夹具与量具全在 darwin 测试文件里，生产代码**零改动**（除一处注释定界）。

### 13.2 改动面

1. 新增 `internal/realbytes/realbytes_clone_darwin_test.go`（`//go:build darwin`）：
   真夹具 + 两条钉子 + 量具自检 + skip 纪律。
2. `internal/realbytes/realbytes.go` 规则 3 下加一段注释定界（克隆/块级去重 ⇒ 所报数值是上限），
   指向用例与 04 §6.9.13。
3. 文档：04 §6.9.13 划账；本稿 §0 行 12。
4. **不动**：任何生产代码分支、CLI/JSON 字段（呈现属 M8）、`go.mod`（不引 x/sys）。

### 13.3 探针

| # | 用例 | 断言 | 修前为什么红 |
|---|---|---|---|
| V1 | `TestClonePairDoubleCountsActualBlocks` 前提段 | 量具自检：写 8 MiB 后 Δ≥半份；`cp -c` 成功后 Δ<四分之一份 | 用例文件不存在 ⇒ **编译红** |
| V2 | 同上，断言 1（双计） | 两份经 `Reported`+`From` 都得 `(size, true)`——各自报满额 | 同上 |
| V3 | 同上，断言 2（删除不释放） | 删掉克隆后释放 < 四分之一份（本机读数 0 B） | 同上 |
| V4 | 既有门禁**保持绿**（反向对照） | darwin 上本用例真跑不 skip；linux/windows 因 build tag 不参与；`./internal/realbytes` 与根包全绿 | 绿 |

### 13.4 变异

本项的用例钉的是**环境事实**（APFS 对共享块的行为），不是生产分支——变异表如实记这一点，
不凑数：生产腿上的改动只能由**兄弟用例**杀，本项如实记"谁杀的"。

| # | 变异 | 预期与真读数 |
|---|---|---|
| M-M27-a | `Reported` unix 腿改成 `return size, true`（不看 `st_blocks`） | **本项不红**（断言 1 只钉数值不钉来源：两个值恰好都等于 size）；由兄弟用例杀（scanner 的 U6/V5 系列钉"全洞真 0"） |
| M-M27-b | `From` 的 `!ok` 分支改成 `return size, true`（读不到也当确信） | 本项不红（真夹具上 `ok` 恒 true，碰不到该分支）；由 `realbytes` 的 U 系列杀 |
| M-M27-c | 夹具把 `/bin/cp -c` 换成 `/bin/cp`（真实复制） | **不红也不绿——触发 skip**（Δ≈整份 ⇒ 判"本环境造不出克隆"）。如实记：这条变异证明的是 **skip 纪律**，不是断言强度；"skipped"绝不能当"passed"读 |

### 13.5 未兑现与边界

1. **Linux（btrfs/reflink）/ Windows（ReFS）腿无真读数**（E8）：本机造不出这两种卷。
   btrfs 需专门卷（CI 无）、ReFS 需 Windows。本项只兑现 darwin/APFS。
2. **CI 兑现待 macOS job 首跑**：本机真跑通过；GitHub macOS runner 上若 `/tmp` 不可克隆，
   走 skip（读数带出原因）——skip 不算通过（与 `smoke-symlink.sh` 同档）。
3. **per-file 检测不做**（E6）：没有 extent 映射这条路，别再试"读 inode 结构"之类的旁门。
4. **呈现属 M8**：按卷标注 + "至多"措辞都属裁定②的排除面；届时若 M8 需要后端给"按卷 CoW 能力"
   信号（`statfs.Fstypename`/`f_type`/ReFS），**另立 ID**——本项**不预置**未消费的契约字段
   （不制造第二个 M30 式欠账）。
5. **组级合计与 CLI 文案不改**：`reclaimable_actual` 合计在克隆对上是 `2S` vs 物理 `S`，
   本项不动口径、不给 CLI 加注（属 M8）；本项只保证"高估这件事有真读数、有钉子、方向写对"。
6. **§3.0 的一句表述按 E7 读**：那句"Go 无 `clonefile` 绑定"对 stdlib 成立、对 `x/sys` 不成立；
   原句不改写，在此挂账。
7. **`du` 也靠不住**（E5）：用户拿系统自带工具自查同样看到 16M——文案里不能建议"用 du 复核"。

### 13.6 实施后追记（2026-09-21，交付 `e013f09`，划账 04 §6.9.13）

**改动面比 §13.2 多了一条**（如实记）：§13.2-1 只列了克隆夹具一件；实施中 M-M27-a 暴露了一个
**门禁盲区**，当场补了第二件（`realbytes_reported_unix_test.go`）。

1. **M-M27-a 的预判是错的（本条最重要）**：§13.4 预测"由兄弟用例杀（scanner 的 U6/V5 系列钉
   '全洞真 0'）"——实测**无杀手**。把 `Reported` 改成返回逻辑大小后，全仓跑遍 **0 红**，而
   四条本应是杀手的断言用例（scanner U2/V5/V7、dedup U4）集体降级 **skip**（+5 skip）。
   成因：它们的**环境前提自检**都调 `realbytes.Reported`，读出口被改坏被前提读成"本卷不支持稀疏"
   ——不变量在断言与前提**两侧**依赖同一个函数，改动被静默吸收。这正是 K 批"门禁假绿"的同族形态。
2. **当场补钉**：新增 `TestReportedMatchesRawStatBlocks`（`darwin || linux`），用 raw `syscall.Stat`
   的 `st_blocks×512` 与 `Reported` **逐字节对照**，不设任何"卷支不支持稀疏"的门槛（与同目录
   `rawDev` 同一纪律："不经过被测函数读真值"）。补钉后 M-M27-a 被它杀回红（原文：`Reported = 67108864,
   want 16384（raw st_blocks×512）：读出口与平台原始读数不一致——所有以它为前提自检的用例会集体
   降级为 skip，而不是红`；全仓仅此一条红）。⇒ **新登记 M45**（04 §6.8.8 表）。纪律面进两条新用例
   注释：**前提自检（环境探测）必须独立于被测物**。
3. **M-M27-b 与预判一致**：本项不红（真夹具 `ok` 恒 true，碰不到 `!ok` 分支）；杀器就是 realbytes
   U 系列两条，原文已录进 04 §6.9.13 变异表。
4. **M-M27-c 证明的是 skip 纪律**（与预判一致）：`cp -c → cp` 后前提判出"真实复制"、**触发 skip**；
   这条变异不红不绿——如实记，不当作断言强度的证据。
5. **真夹具读数带噪声（新事实）**：两次真跑，克隆步 Δ 一次 `0 B`、一次 `4096 B`；删除步同刻
   `0 B` / `-4096 B`（克隆的元数据块被收回）。阈值取整份四分之一（2 MiB），余量足够；读数照实
   打进 `t.Logf`，不做圆整。E1 那次一次性程序的 `Δ=8396800 B`（写步）与夹具今日的 `8388608 B`
   是**两次不同的真跑**，都真，不合并成一个数。
6. **产物逐字不变**：`dist/assets/index-DQDgG5FN.js 152.55 kB` 与 §6.9.11/§6.9.12 同名同大小
   ——本项前端零改动。
7. **计数**：源级 602 / 顶层 551+4+0 / 子用例 75 / `=== RUN` 630（+2 = 本项两条新用例）。

---

## 14. 本轮全量审查的修复包（G1~G11，2026-09-21）

上位依据是用户指令："审查项目设计、开发以及历次审查文档，确认功能完全实现，审查问题全部
修复，测试全部通过。如有遗漏自动完成开发和修复。"审查路径：**门禁新鲜读数**（全绿，仅
`scripts/smoke-symlink.sh` rc=2 合法跳过）+ **文档声明逐条回代码比对**。本节只收
"文档/注释/脚本在说一件代码并没有做的事"这一类缺口——它们不是新功能，而是**已有声明的真值
问题**，与本稿 §2~§13 的"数字宁可说不知道"是同一条纪律。

**审查中一并核实为"非缺陷"、本轮不动的三类**（写在这里以免下一轮重新考古）：

- Linux 的 `.Trash-$UID` 未进内置清单：04:847 与总纲 :202/:220 已把它**指派给 M7**，
  而 M7 未开工 ⇒ 是指派项不是孤儿项。
- `docs/superpowers/specs/2026-08-05-dedup-formal-spec.md` 的 8 条 `Status: draft`：
  04 §6.8.0.5 表头**明文规定**"specs 只追加、不改状态、不撤回" ⇒ 制度如此。
- `ExportReport` 是 M5 空桩（`app.go:2202` 返回"报告导出将在 M5 提供"），09 手册对
  导出功能的描述与 i18n 现状都是诚实的（:551/:733）⇒ 桩本身是 M9 的活。

### 14.0 动手前取证（逐条：位置 → 原文 → 与之矛盾的代码事实）

| # | 位置 | 声明原文（逐字） | 代码实证 | 结论 |
|---|---|---|---|---|
| G1 | `docs/09-用户手册.md:465`（§6.5 状态码表） | `\| OK \| 执行成功，计入释放空间 \|` | `internal/ops/executor.go:354-363` 按 kind 分四桶：`hardlink→LinkedBytes`、`symlink→SymlinkedBytes`、`trash→TrashedBytes`、`default→Reclaimed`。**OK 本身不进任何桶**，只有永久删除才进"释放空间" | 手册把一个**分四种口径**的计数说成一种，用户照 §6.5 读 `Reclaimed` 会以为回收站清理也算"释放" |
| G2 | `docs/09-用户手册.md:627-628`（§10 FAQ） | "所以默认跳过并**单独报** `已跳过 N 个云端占位文件`（`skipped_cloud_files`）" | `skippedCloudFiles` 在前端只出现 2 次，**都在 `frontend/src/wails.ts`（注释 + 类型字段）**；`--include='*.vue'` 与 `stores/` 命中数为 0 ⇒ 界面没有这句话，只有 JSON/CLI 有该字段（裁定③的边界） | 手册描述了 GUI 提示，实为 CLI/JSON 字段 |
| G3 | `scripts/test-frontend-logic.sh:91` | `printf '…%s 个用例全部通过（含 2 条接线断言）\n' "$count"` | 同脚本 :55 的 `count` 取自 node 计数行；本次真跑读数 `ℹ tests 20 / pass 20`，接线另打 2 行 `✓` ⇒ 实际 **20 node + 2 接线 = 22 项**。`$count` 只有 20，文案却写"含 2" | 计数口径自我矛盾（20 含 2 ⇒ 读者以为 node 侧 18）。门禁脚本自己犯了 §6.6.2"计数不许说谎" |
| G4 | `wails_types_test.go:67` | `// 豁免两条：没有可反射的 Go 对象。` | 其后随 **3** 条 `nil` goType 豁免：`OpsFiltered`、`BackendAPI`、`OpKind` | 注释数错了（不影响判定，只影响读码） |
| G5 | `frontend/src/wails.ts:282-320`（`BackendAPI`） | 32 枚方法声明 | Go 侧 `grep -c '^func (a \*App) [A-Z]' app.go` = **34**；三向差集（`comm` 实测）：Go 有 / TS 无 = `ExportReport`、`PreviewProcessPolicy`；TS 有 / Go 无 = **空**。反射口径已核实：`reflect` 只报导出方法（一次性程序验证：同包 `*S` 一导出未导出两方法，`NumMethod()==1`），`App` 无嵌入字段 ⇒ **零豁免可用** | 方法面**没有任何门禁**（M30 只比字段不比方法），已漂走 2 枚 |
| G6 | `app.go:359-363`（`openLedger` 失败分支） | 原位注释自己承认："（只写 stderr 这一半仍是缺口：GUI 无终端 ⇒ 登记 M43，本项不动。）" | `history.Open` 失败 ⇒ 只有 `fmt.Fprintf(os.Stderr, …)` 后 `return`；而 `GetStartupNotice` 的前端消费者**已在**（`stores/scan.ts:833`），M12b 的隔离通知就走这条路（`app.go:373`） | M43 登记行已写清修法"与 `addStartupNotice` 同形（一行）"；本轮审查既已展开，顺手兑现 |
| G7 | `docs/04-开发与测试计划.md:417`（B 组第 8 条） | "**CI 无 macOS job**（§3.2）：darwin 专属 15 个用例…都不进门禁" | `.github/workflows/ci.yml:215-217` 有 `macos: name: go test (macos) runs-on: macos-latest`（D-2 本轮自己加的，见任务 #14） | 该条已过时；同节第 6/7 条用 `~~删除线~~ + 已闭环` 格式，照抄先例 |
| G8 | 04:1219（M37 登记行位置列） | "`internal/ops/move.go` / `symlink.go` / `merge_guard.go` 的 `backup+".undo"` 停靠名" | `grep -rn '\.undo"' internal/ --include='*.go'`（去测试）只命中 `merge_guard.go:156,159,163,165` 与 `worktemp.go:119` 的 `TrimSuffix`；move/symlink 两份重复舞步已随 §8 的 I5 收归 `rollbackUnverifiedSwap` 一处 | 位置清单过时（结论仍成立）；仿 M39 先例加括注，不改写判据 |
| G9 | `internal/realbytes/realbytes_volume_unix_test.go:54-63` | 注释："Linux CI 上 /dev 可能与 / 同卷——那种环境里夹具不成立，skip 而不是红" | 唯一对照卷是 `/dev/null`；ubuntu runner 上 `/dev` 多为 devtmpfs（与 `/` 不同 dev）**可能**成立，但本机（darwin）无法取证 ⇒ 现状是"这条判据在 CI 上有没有证据"未知 | 加候选卷列表（`/dev/shm`、`/dev`、`/tmp`、`/`…）取第一个真正不同 dev 的，宽度换证据；Linux 腿本机拿不到读数 ⇒ 照实标"代码已改、验证未兑现" |
| G10 | `app.go:1278-1282`（`ProcessPreview`） | Go 侧三枚 json tag：`effectiveIds`/`effectiveCount`/`unmatchedDirs` | `grep -n ProcessPreview frontend/src/wails.ts` = **无命中** ⇒ 整个类型在 TS 没有镜像。M30 的漏网原因清楚：§12.0 E2 是**从 wails.ts 已有的接口往回找字段缺口**，从未枚举"Go 下发但 TS 压根没有的类型" | M30 同族、同方向（Go 有 TS 无）；随 G5 一起补（`PreviewProcessPolicy` 的返回类型） |
| G11 | `frontend/wailsjs/go/main/App.d.ts`（Wails 生成物，32 枚 `export function`） | — | 三向差集：`App.d.ts` 缺 `GetStartupNotice`、`PreviewProcessPolicy`；`App.d.ts` 有 / `BackendAPI` 无 = `ExportReport`。生成物只在 `wails build` 时刷新，而门禁（§3.3 那 13 行）里**没有** `wails build` ⇒ 提交进仓库的生成物会长期陈旧 | **新增登记 M46**，本轮不修（见 §14.5-2）。运行时不受害：Wails 按反射注入 `window.go.main.App.*`，与 `.d.ts` 无关；受影响的只是"照 `.d.ts` 读契约"的人 |

G1~G11 全部由本轮 grep/读码现取，无一条来自上一轮的记忆。另有一处**未成立**的怀疑，
如实记下：审查中怀疑 `cmd/benchgen/main.go` 的引用行号漂移，实测 `04:1805` 指的 `:291`
与当前文件**一致**（"③ .fdd-old 残留…不计入 TotalFiles"正在 291 行），不改。真实漂移只
有一处：`04:1223` 的 `RecordsView.vue:268` → 现为 **269**（`<th>回收空间</th>`）。

### 14.1 判据

1. **方法面的权威源是 Go 导出方法集 ↔ `wails.ts` 的手写 `BackendAPI`**，不是生成物
   `App.d.ts`。理由见 G11 括注（生成物不在门禁面上、且运行时按反射注入）。钉子做成
   **双向、零豁免**：Go 有 TS 无 ⇒ 红（前端取不到）；TS 有 Go 无 ⇒ 红（前端调一个不存在
   的方法）。生命周期钩子（`startup`/`shutdown`/`beforeClose`）刻意小写，反射本来就不报，
   因此**不需要排除表**——将来若要加豁免，必须像 §12.0 E4 那样写理由并显式列出。
2. **M43 的文案必须说后果**，不能只说"历史库不可用"：现状是账本不可用时回收站/移动/
   硬链接清理**被拒绝执行**（`beginJournal`），用户看到的是"点了没反应/报错"。三件事缺
   一不可（照 `cacheUnavailableNotice` 的形状）：出了什么事、对用户意味着什么、在哪个文件上。
3. **纯函数 + 一行接入**（H6/I5）：判定写在 `ledgerUnavailableNotice(err, histPath)`，
   `openLedger` 只负责调它并 `addStartupNotice`；不用 `emit`（startup 早于前端注册监听，
   事件必丢——同 M12b 的理由）。stderr 那一条**保留不动**：跑 CLI/无终端时它仍是唯一出口。
4. **文档类缺口（G1/G2/G3/G7/G8）不新增判据代码**，只把话说对：G7/G8 走既有格式先例
   （删除线 + 已闭环 / 括注更正），不改写既有结论；G1/G2 改手册措辞为代码实际口径；
   G3 改脚本打印为**两个数各自真**（node 20 + 接线 2 = 22）。
5. **skip 不算通过，前提自探独立于被测物**（M45 纪律）：G9 只加宽候选卷，不改判据；
   全部候选同 dev 时仍 skip。Linux CI 上是否兑现，本轮**不预判**。
6. **不扩大改动面**（§1 约束 5）：G11 与"手册其余章节的 i18n/M5/M8 描述"都只登记或只改
   本轮点名的那一句；M46 新 ID 记录生成物陈旧。

### 14.2 改动面

**代码 6 件**

| 文件 | 内容 |
|---|---|
| `frontend/src/wails.ts` | `BackendAPI` 补 `PreviewProcessPolicy(dirs, selectedIDs): Promise<ProcessPreview>` 与 `ExportReport(format, path): Promise<string>` 两枚声明；新增 `export interface ProcessPreview`（三字段，照 G10 的 tag）。`ExportReport` 处必须写明"Go 侧目前是 M5 空桩，调用即报错"，否则 TS 签名比 Go 现状更乐观——那是反向的谎 |
| `wails_types_test.go` | G4 注释改"豁免三条"；映射表加 `ProcessPreview` 一对；新增 `tsInterfaceMethods`（纯函数，抽方法名）+ `goExportedMethods`（纯函数，吃 `[]string` 排序去重）+ `TestBackendAPIMatchesGoExportedMethods`（读 TS 文件 + `reflect.TypeOf(&App{})` 反射，双向比对，零豁免） |
| `app.go` | 新增 `ledgerUnavailableNotice(histPath string, err error) string`（纯函数）；`openLedger` 失败分支加一行 `a.addStartupNotice(...)`，stderr 保留；删掉那句"（只写 stderr 这一半仍是缺口：…本项不动。）"的原位注释（本项就是来兑现它的，留着会指错） |
| `app_m43_test.go`（新） | V1 必失败夹具（`history.db` 造成同名目录）→ `openLedger()` → 断言 `GetStartupNotice()` 非空且含 `history.db` + "拒绝执行"后果；V2 健康库时 `GetStartupNotice()==""`（不打扰）；V3 与 M25 的累积槽共存（两条通知都在、`\n` 分隔） |
| `internal/realbytes/realbytes_volume_unix_test.go` | 跨卷对照改为**候选卷列表**（`/dev/null`、`/dev/shm`、`/dev`、`/run`、`/tmp`），取第一个 dev 与临时目录不同者做对照；全无对照才 skip，并把"试过了哪些"打进 skip 原因 |
| `scripts/test-frontend-logic.sh` | 打印改为 `20 个 node 用例 + 2 条接线断言 = 合计 22 项全部通过`，接线数以实际计数为准（不写死 2） |

**文档 2 个文件（09 两处、04 五处 + 新登记一行）**：`docs/09` §6.5 OK 行按 kind 四分（G1）、§10 FAQ 云端句改口径（G2）；
`docs/04` B 组第 8 条删除线 + 已闭环（G7）、M37 行加位置括注（G8）、M41 行 `:268→:269`
（14.0 末段那处真实漂移）、M43 行末列翻转为"已实施"；新增 **M46 行**（G11）。
另：本稿 §0 进度表加第 13 行。

### 14.3 探针（修前必红）

| 探针 | 形状 | 预期 |
|---|---|---|
| P1 方法面 | 只加 `TestBackendAPIMatchesGoExportedMethods`，不动 `wails.ts` | **红**，且原文要报出 `ExportReport`、`PreviewProcessPolicy` 两枚（这一条同时是 G5 的证据，不必另找） |
| P2 `ProcessPreview` 镜像 | 映射表加一对而 TS 未声明 | **红**（`wails.ts 里找不到 export interface ProcessPreview`，走 §12 已有的失败路径） |
| P3 M43 | 只加 `app_m43_test.go`，不动 `app.go` | **红**在 `GetStartupNotice()` 为空——前提是夹具自证（先断言 `history.Open(该目录)` 返回 error，故"空"只可能是"没上报"，不是"没失败"） |
| P4 G9 | 本机 darwin 现状即绿 ⇒ **无修前红可取**，如实记。收益（Linux CI 有无证据）本轮不可证 | 不充数 |
| P5 文档面（G1/G2/G3/G7/G8） | 注释与文案无红可取 ⇒ 证据形态是"改前原文 + 改后原文"两列，见划账表 | 不充数 |

### 14.4 变异（逐条改坏 → 应红；真读数写进 04 划账，不写本文）

| # | 变异 | 目标 |
|---|---|---|
| M-M14-a | 从 `BackendAPI` 删一枚 `PreviewProcessPolicy` | P1 方向 1 红 |
| M-M14-b | 把 `TestBackendAPIMatchesGoExportedMethods` 改成**单向**（只查 TS→Go） | 反向缺口不红 ⇒ 证明双向是必要的（与 §12.4 M-M30 同族） |
| M-M14-c | `ProcessPreview` 的 json tag 改错一枚（`effectiveCount`→`effectiveCnt`） | 字段镜像比对红 |
| M-M14-d | `ledgerUnavailableNotice` 改成返回 `""` | P3 红 |
| M-M14-e | 把 `addStartupNotice` 那行接入删掉（只留 stderr） | P3 红（回到登记前的缺口形状） |

预测不了实测结果的行不写。**五条实跑结果**（含 M-M14-b 那条"证据是不红"的特殊形态）
与原文报错见 04 §6.10 三与本文 §14.6-4：本轮无一条"杀不掉"，也无需补钉。

### 14.5 未兑现与边界

1. **G9 的 Linux 腿**：本机是 darwin，无法验证 ubuntu runner 上 `/dev/shm` 是否可 stat。
   划账必须写"**代码已改、验证未兑现**"，不得写"已通过"；兑现要等 macOS/Linux runner 读数。
2. **G11（M46）不修**：刷新 `App.d.ts` 要跑 `wails build`（或 `wails generate module`），
   这一步不在 §3.3 门禁 13 行里，且生成物按手写代码重排会产生大 diff。只登记，修法留给
   M9 门禁收尾时定（要么把生成物比对进门禁，要么把生成物移出版本库）。
3. **M45 的 Windows 腿**不变（本轮无 Windows runner），G6 与之无关。
4. **裁定③仍然生效**：G2 只改手册措辞，**不**给前端加"已跳过 N 个云端占位文件"横幅；
   G5 补的 `PreviewProcessPolicy`/`ExportReport` 只是**类型面**，前端零调用点（`grep` 实证
   见 14.0），故 `npm run build` 产物字节数应逐字不变——这是本项的一条反向钉子
   （若产物变了，说明动到了前端运行面）。
5. **`ExportReport` 不做实现**（M5 空桩）：本轮只让它出现在契约面上并注明是桩。
6. **本节不含新功能**：G1~G11 全部是"已有声明 ↔ 实证"的对齐；发现的真缺口若需要新行为
   （如 CoW 按卷标注、记录页按 kind 分述），一律留在 M7/M8/M9，不在本轮顺手做。

### 14.6 实施后追记（2026-09-21，代码面 `0fd9cd7`，划账 04 §6.10）

1. **三条修前红原样命中，一条不需要放宽判据**：P1 报出的差集正是 §14.0 G5 实测的那两枚
   （`ExportReport, PreviewProcessPolicy`，`Go 导出 34 枚 / BackendAPI 声明 32 枚`）；
   P2 走了 §12 已有的失败路径（`wails.ts 里找不到 export interface ProcessPreview`）；
   P3 红在 `GetStartupNotice()` 为空。修后同三条全绿，方法面读数变 **34 / 34**。
2. **"反射只报导出方法"当场取证**（一次性程序：同包类型一导出 + 一未导出方法，
   `NumMethod()==1`），并与 `grep -c '^func (a \*App) [A-Z]'` 的 **34** 交叉核对一致
   ⇒ §14.1-1 的"零豁免"不是乐观假设而是实测结论。若哪天 `App` 加了嵌入字段，
   反射会把提升方法一并报出，那时才会需要豁免表。
3. **改动面比 §14.2 多一处**（如实记）：§14.2 只写"新增 `tsInterfaceMethods`"，实施时把它
   依赖的花括号深度遍历抽成了 `tsInterfaceBody` 供两侧共用——不抽就是**两份**解析器，
   正是本项要防的漂（I5）。抽取的行为等价由 §12.3 那 4 条解析用例（真文件两例 + 合成片段
   + 查无此接口报错）全绿兜住。
4. **M-M14-b 的证据形态特殊，别读成"变异没杀掉"**：它的判据是"把 P1 改成单向 + 注入一枚
   Go 侧不存在的 `NoSuchBackendMethod()` ⇒ `ok filededup 0.445s`（**放过**）"，
   即"少了反向腿就抓不住"，与 §12.4 的 M-M30 同族。这条实验**没有落进仓库**（只存在于
   划账表），因此本项在仓库里留下的强度证据只有 M-M14-a/c 两条红。
5. **G9 本机零新增证据**：改前改后用的都是同一个对照卷 `/dev/null`
   （`-v` 读数：`/dev/null（dev=18446744073339475307）vs 临时目录（dev=16777230）→
   VolumeID 16777230 vs 18446744073339475307`），新增的 6 个候选一个没用上 ⇒ 本条收益
   只在 Linux runner 上才谈得上兑现，04 §6.10 五-1 按裁定写作"**代码已改、验证未兑现**"。
6. **产物逐字不变**：`dist/assets/index-DQDgG5FN.js 152.55 kB`，与 §6.9.11/12/13 同名同大小
   ⇒ 裁定③ 未被越界（本轮 TS 改动全在类型面，运行时包未变）。这是 §14.5-4 预埋的**反向钉子**。
7. **计数**：源级 **606**（上轮 602，+4 = M43 三条 + 方法面一条）/ 顶层 **555 + 4 skip + 0 fail** /
   子用例 **75**（与上轮持平，本轮未加子用例）/ `=== RUN` **634** = 559 顶层 + 75 子用例。
   skip 名单与 §6.9.13 同一批，本轮未新增也未消除任何 skip。
8. **审查阶段被复核推翻的两条结论**（写在这里防止下一轮重犯）：其一"Linux
   `.Trash-$UID` 全交代为零"——实际 04:847 与总纲 :202/:220 已指派给 **M7**，是指派项不是
   孤儿项；其二"§6.9 点名的用例有 4 个对不上"——其中 3 条 folder 用例是 §6.9.10 已声明的
   原位替换、`TestAds*` 那条是 `-run` 前缀模式，均非失配。教训：**子代理的结论必须自己
   逐条 grep/读码复核后才能进划账**（本轮照此执行，才没有把两条错结论写进 04）。
9. **工具面一条事实**（省下一轮时间，与判据无关）：04 的登记表行内含全角引号与 `→` 时，
   精确文本匹配多次失败；整行替换 + 表格列数（`awk -F'|'` 与邻居行对齐）核对是更稳的路径。
   本轮 M37/M41/M43/M46 四行的改动都用这个方式复核过结构未破。


---

## 15. 本轮全量代码审查的实施批次（OPS/SCN/FLT/REAL/MODEL/APP/FE/GATE，2026-09-21）

上位指令：**"进行全量代码审查并测试，按照审查情况实施修订。"**
分区审查由 5 个只读子代理并行完成（未碰工作树、未跑测试），产出 60+ 条候选。
本节是**动手前**的取证与裁定：**每条都自己开码核对过**，核对不过的写成"推翻"或"降级"，
不许照抄子代理结论。判据与 §1 约束一致：不扩大改动面、新增计数不进界面（M8）、
修正既有假话类前端改动沿用 04:2014 的先例许可。

### 15.0 动手前取证（位置 → 实际读到的代码 → 裁定）

#### A. 复核为真、本批实施

| ID | 位置与实测到的代码 | 裁定与修法 |
|---|---|---|
| **OPS-1**（Critical） | `move.go:124` `hardlinkRename(tmp, dup)` **成功**后 tmp 这个名字已被改名消耗；`:135-138` 的 `removeOwnBackup` 失败分支紧接着执行 `_ = os.Remove(tmp)`，注释写"确保临时硬链接不残留"。此刻 `tmp` 路径上若被第三方落了文件，这一行就是在**无按路径证据**地删别人的东西，错误还被 `_ =` 吞掉、结果记成"成功 + 残留告警"。同形位置 `symlink.go:129-131` **没有**这一行（`removeOwnBackup` 失败直接 `return err`）⇒ 两条合并路径不对称，硬链接侧多一枚无证删除 | 实施：删掉该 `_ = os.Remove(tmp)`；探针 = swap 成功后在 tmp 位第三方落文件 + `workTempRemove` 注错，断言第三方仍在 |
| **OPS-7** | `executor.go:401-409` trash 的 pre-pass 逐个 `identityStill` 后**批量** `trash(paths)`；批量失败退化为逐文件分支 `:457-485`，该分支只做 `os.Stat` + `known[p]` 查表，`if m, err := trash([]string{p})`（:482）**之前不再复核身份**。trash 是全仓唯一"复核不紧贴动作"的 kind（delete/hardlink/symlink/move 都在动手前一行复核）⇒ 批量失败到逐个重试之间的延迟（整批 I/O + 并发排队）构成窗口 | 实施：回退分支每次单独 trash 前补一次 `identityStill(p, procIDs[i])`，不合规记 Failed/stage=verify，与 :405-407 同一措辞 |
| **OPS-9**（I5） | 同一个 Win32 错误码在 ops 包内有两套命名：`regstatus.go:24` `regErrNotFound = syscall.Errno(2)` + `:32` **裸** `syscall.Errno(3)`；`symlink_windows.go:51-57` 一张具名表（`errFileNotFound`=2 / `errPathNotFound`=3 …）。`ads/ads.go:33-35` 是第三套（跨包，且 int 非 Errno） | 实施（包内）：把 `symlink_windows.go` 的具名表整体挪进无 tag 的 `winerrno.go`，`regstatus.go` 改引用 `errFileNotFound`/`errPathNotFound`，消灭裸 3；ads 侧收归需新建叶子包 ⇒ 扩面，登记不实施 |
| **OPS-10** | `move.go:277-305` `claimDst` 的 `for i := 0; ; i++` 无上限、不看 ctx。同包 `trash_linux.go:104-107` 对同形状循环已给过结论并设 `xdgNameMaxTry = 10000`，注释原文："永远查不出 NotExist 会让全部并发 goroutine 一起挂死"。同一条理由在本包已经成立过一次 ⇒ 本处属漏改不是取舍 | 实施：`claimDst` 复用同一个包级上限常量，超限返回显式错误 |
| **CACHE-1** | `HashHeadTail`（hasher.go:101-190）的采样分支按**声明 size** 算 `sampleOffsets`，追加的尾巴落在四个采样点之外；AS-H2 的 `rejectGrowthBeyond` 此前只挂在小文件一趟与两条全量路径上，**采样分支不在场**。真实盲区是"遍历 stat 之后、哈希之前"变长：此时缓存四点采样仍相符 ⇒ pipeline 跳过阶段 3（唯一调 HashFull 处），paranoid 又按 size 截断 ⇒ 假重复组（AS-H2 的第四入口）。**严重度必须按此措辞**：下游 executor 的 VerifyFile 内容复核会拦住删除，故这是"两道声明防线各有一处盲区"，不是已兑现的错删路径 | 已实施（本批起点）：采样分支 `r.Short = short` 之后补 `rejectGrowthBeyond`；导出 `RejectGrowthBeyond` 供 dedup 复用，避免 I5 第二份实现 |
| **PARA-1** | `pipeline.go:813-835` `equal` 只比 `size` 字节就 `return true`；`size = int64(g[0].Size)` 来自更早的 stat。文件在比对期间被追加时，前 size 字节相同即判"逐字节一致"⇒ paranoid 这道**最后防线同样切尾**。另有 `size < 0`（坏卷/畸形挂载报出）时 `remain > 0` 不成立，函数**一次都不读就返回 true** | 实施：`equal` 收尾调 `hasher.RejectGrowthBeyond(a/b, size)`；入口 `size < 0` fail-closed 报错 |
| **HASH-1** | `HashHeadTail`/`HashFull`/`HashFullSegmented` 无 size 下界守卫：负 size 走 `size <= SmallFileMax` 分支，`buf[:size]` 直接 panic（`slice bounds out of range [:-1]`）。worker panic 会让**整轮扫描**作废（pipeline 的 workerPanic 收口）⇒ 一个坏卷打死全部结果 | 已实施：三入口 `if size < 0 { return declaredSizeErr(size) }`，错误自证"声明长度不可信"而非"恰好被计数分支挡下" |
| **DOC-1** | `hasher.go:174-178` 注释原文称"pipeline 的短读纠偏会在**阶段 3** 以实际读量再校正"。实际纠偏发生在阶段 2 拿到 `r.Short` 之后（`pipeline.go:486-487` `e.Size = actual`）；阶段 3 只按已纠正的 `e.Size` 调 HashFull | 实施：就地更正注释（属"修正既有假话"，非新增呈现） |
| **SCN-3** | `scanner.go:596-602` `rootPrefixes` 无条件 `out[i] = r + sep`。根为 `/`（或任何已带尾分隔符的根）⇒ 前缀变成 `//`；`:608-615` `relativeTo` 对 `/Users/x` 的前缀匹配全部失败 ⇒ 兜底 `filepath.Base(full)` ⇒ 任何含 `/` 的 `ExcludePaths`（如 `a/b/*`）**静默永不命中**，方向是放行（该排的没排）。基准实现 `scanner_test.go:164-171` 同病 ⇒ 等价性用例在结构上抓不到这条 | 实施：根已以分隔符结尾则不追加；探针走 `rootPrefixes`+`relativeTo` 单元级断言（绕开等价性基准的传染性） |
| **FLT-1** | `filter.go:45-61` `newExtSet` 只做 `strings.ToLower`；`has(ext)` 的调用方传入 `filepath.Ext` 产物（**带点**）。用户在 GUI（`ScanView.vue:84-85`，只 trim 不补点）或 CLI 里写 `tmp` ⇒ `"tmp" != ".tmp"` ⇒ 该条排除**整条 fail-open**，且界面不会报错 | 实施：在 `newExtSet` 一处归一（TrimSpace + 补点 + ToLower）。一处判定一处实现，GUI/CLI 不必各自改 |
| **REAL-1** | `pipeline.go:486-487` 短读纠偏只 `e.Size = actual`。`e.Actual`/`e.ActualKnown` 仍是**截断前那份**的实占读数 ⇒ 逻辑栏已纠正、实占栏没纠正，双口径互相冒充（违 I6）。跨卷/回退情形下实占还会大于新逻辑长度 | 实施：同一处补 `e.ActualKnown = false; e.Actual = 0`，让实占栏显式落回"未统计"而不是冒充 |
| **MODEL-1** | `model.go:137-144` `AnyActualKnown` 遍历 `g.Files` **全部成员**（含 files[0] 保留项）；`:149-159` `ReclaimActual` 只加 `files[1:]`。两函数的口径必须一致，因为 `AnyActualKnown` 是那个数字的"已统计"标志（调用点 `app.go:963`、`cmd/fdd-cli/main.go:141`）。保留项读到过实占而冗余项都没有时，标志为真、数字却纯是逻辑口径回退值 | 实施：`AnyActualKnown` 只看 `files[1:]`，与 `ReclaimActual` 逐字对齐 |
| **APP-1** | `app.go:1384`/`1397` 的 `ApplyKeepPolicy` 全程持 `a.mu`，内部经 `keep.go:145` 调 `fscase.Sensitive(dir)`；`fscase.go:75-85` 注释原文可引：它**要往用户目录写探测文件**。策略侧已有 `WarmSensitivity` + `ApplyProcessPolicyWith`（AS-R3 的形状），保留侧既无 With 变体也无测试钉子（`app_process_lock_test.go:43` 只钉了 `PreviewProcessPolicy`）⇒ AS-R3 **修了一半** | 实施：照 AS-R3 形状补 `ApplyKeepPolicyWith` + 锁外预热，探针成对（锁内不得出现写盘探测） |
| **APP-2** | `app.go:2220-2232`：`CacheStats`/`CacheClear` 各**锁外两次**解引用 `a.cch`（`if a.cch == nil` 后再 `a.cch.GetStats()`）；`shutdown` 在锁内置空（`:458-461`）。与 M9 的 `histSnapshot`（`:1626-1637`）逐字同形，而 M9 的 AST 门禁白名单只管 `a.hist` ⇒ 同一类缺陷的第二例 | 实施：加 `cchSnapshot()`，AST 门禁扩到 `a.cch` |
| **APP-3** | `app.go:1878-1882` `PruneScanFiles` 失败只 `fmt.Fprintf(os.Stderr, ...)`，注释原文"属可忽略的陈旧关联，留痕即可"；`app.go:451-455` shutdown 的 drain 超时同样只 stderr。M7 的 `warnLedger`（`:1641-1646`）是这类"账本没写进去"的统一出口 ⇒ 这是同族**第 5、6 处**漏网 | 实施：两处改走 `a.warnLedger(...)`（打包 GUI 无控制台，只写 stderr 等于没写） |
| **APP-4** | `app.go:2049-2052` 批量回撤的 `todo` 只收 `StateDone`；而 `:2004-2007` 的函数文档承诺"回撤失败的项保持 undo_failed，用户可修正后再次回撤"，单项通道 `:2171-2174` 也确实允许 `done \|\| undo_failed`。`undoing` 状态只在 `history.Open` 收口（`history.go:185-190`）。后果：部分失败后再点"全部回撤"→ todo 为空 → 前端弹"已回撤或未实际执行"（`scan.ts:775-776`），与事实不符 | 实施：`todo` 过滤改 `done \|\| undo_failed`，结果文案带"另有 N 项此前失败" |
| **APP-6** | "哪些操作可回撤"有**两份内联实现**：`app.go:1674` `undoable := op.Kind != "delete" && !(op.Kind == "trash" && runtime.GOOS == "windows")`（写库真值 `OpMeta.Undoable`）与 `:1617-1622` `undoableReason` 里的 `if kind == "trash" && runtime.GOOS == "windows"`（文案）。两处判据必须同源，否则出现"落库说可撤、文案说不可撤"；且 `runtime.GOOS` 直接落在 RPC 层同时违 I5 与 H6 | 实施：抽无 tag 纯函数 `undoableFor(kind, goos)`，reason 分支改用它，两侧同源 |
| **APP-7** | `sysguard.go:239-261` `isReservedName` 的名单是 CON/PRN/AUX/NUL + COM1-9 + LPT1-9；`:239` 的说明文案同口径。**缺 `CLOCK$`**——Microsoft 命名规范在册的保留设备名 | 实施：补一支 + 用例（含 `CLOCK$.txt` 取词干形态） |
| **APP-9** | `sysguard.go:92-100` `hits`：prefix/suffix 比对是"把 name ToLower 后与**手写小写的** e.prefix 比"。表内当前两条恰好是小写（`.com.apple.timemachine-` / `.snapshots`）⇒ 今天没坏；但任何人新增一条带大写的 prefix/suffix 会**静默永不命中**（整卷 TM 快照挡不住，方向是放行）。`:26` 注释还写着"大小写不敏感"，是承诺与实现脱节 | 实施：装配时统一 `strings.ToLower`，把"永不命中"变成不可能 |
| **APP-10** | `cmd/fdd-cli/main.go:132` `r := report{... Failed: failed}`，`failed` 为 nil 时 JSON 输出 `"failed": null`。仓内已有两处裁定"空列表必须是 []"（`oplog.go` 侧、`scan.go` 侧），且 `smoke-cli.sh:71` 已被迫 `or []` 兜底——**脚本的兜底就是这个缺陷存在证据** | 实施：`Failed: append([]model.FailedItem{}, failed...)` |
| **FE-1** | `ResultView.vue:438` 「打开回收站」按钮在 OK 块内**无条件**显示。delete/hardlink/symlink 运行时 `TrashedBytes` 必为 0，按钮仍在暗示"去回收站看看"——正是 M22 立项理由的后半句（"数字改了、按钮没改"）。属"修正既有假话"类，非新增计数呈现 ⇒ 有 04:2014 先例许可 | 实施：store 记 `lastOpKind`，按钮按 `=== 'trash'` 显示 |
| **FE-2** | `opdisplay.ts:59-61` 与 `ConfirmDialog.vue:36` 都用 `default:` 兜底返回硬编码文案，全仓无 `never` 检查。`scan.ts:679-681` 注释承诺"新增 kind 会在编译期报错"**不成立**——真新增一个 kind 会静默走 default 拿错文案。`OpKind` 又被 `wails_types_test.go:74` exempt ⇒ 类型面也不管 | 实施：两处 default 前加 `const _exhaustive: never = kind`，把假承诺变成真门禁 |
| **FE-3** | `RecordsView.vue:249` 「重扫」按钮 `:disabled` 只看 `store.scanning \|\| store.opsRunning`，**不看本视图的 `loading`**；而 `scan.ts:326-377` 的 `rescan` 会先发起在途 `openHistory`。两者交错 ⇒ 统计条与列表来自不同代（正是 B3-2 自己点名的危险形状） | 实施：`:disabled` 补 `\|\| loading` + 回写后代际复核 |
| **GATE-1** | `scripts/test-frontend-logic.sh:84-87` 只有 2 条 wiring 锚，且都不在 `views/`。实测：删掉 `ResultView` 的 `v-else-if TrashedBytes` 分支，**13 行门禁全绿** ⇒ 本轮 FE-1 这类改动的回归无人守 | 实施：补 2 条锚（只锚标识符，不锚中文文案，避免改字就红） |
| **GATE-2** | `wails_types_test.go:357-385` 字段面：`goJSONNames` 若解析失效返回空、TS 侧也空 ⇒ `missing`/`extra` 都为 0，**两侧同时为空即静默通过**。同文件方法面 `:427-428` 已有 `len(tsMethods)==0 ⇒ t.Fatal` 的 fail-closed 口径 ⇒ 同一文件内两条标准不一致 | 实施：字段面补 `len(goNames)==0` 下界 + compared 计数下界 |
| **GATE-3** | `docs/04:378` 门禁表仍写 `SKIP=''`，脚本实际用 `QUARANTINE=()` ⇒ 文档与脚本不符（照文档改脚本会改坏） | 实施：文档更正（括注式，不改写既有结论） |
| **DOC-H2** | `docs/04:248` §3 H2 行写"用 (dev,ino,**ctime**) 再验一次"，而 `fsid.SameIdentity` 注释明写**不含 ctime**（chmod/xattr 会推进 ctime，含了就误拦正常改写），`identity_still_test.go:68-74` 把"原地重写仍放行"钉成预期 ⇒ **文档与代码不一致，代码是对的** | 实施：只更正文档，括注说明裁定依据 |

#### B. 复核后推翻 / 降级（不得照抄子代理结论）

| 项 | 子代理说法 | 开码后的事实 | 处置 |
|---|---|---|---|
| **SCN-2** | "隐藏规则抵消逃逸放行 ⇒ 被点名根下的子树一个文件都不扫" | `scanner.go:253-255` 把**全部根**预置进 `visited`/队列；逃逸成立意味着某根 R 在被剪目录 D 之下，而 R 自己那棵树在队列里，D 作为 W 的子项被隐藏规则跳过**不影响 R 的扫描** ⇒ 不丢文件 | **推翻**，留证：本批未据此改任何代码 |
| **OPS-4** | "Windows trash 的 dst map 一次都没写（`trash_windows.go:238-280` 只 return），整批失败时'源消失+无落点'必判失败" | 事实成立，但这是 `executor.go:436-476` 注释里**明文裁定**的取向（"宁可让用户看到一条需要核实的错误"）。代价（TrashedBytes 少计、账本记 failed、结果集不清理）如实登记 | **降级**为登记项，不当缺陷修 |
| **OPS-6** | 数据缺陷 | 是 §3 H2 行的文档与代码不符 ⇒ 见上表 **DOC-H2** | **改性质**：只改文档 |
| **SCN-1** | "用户点名的根位于 `eAbsPath` 型保护条目内部时，整棵子树被逐层剪枝 ⇒ 扫出 0 文件、`ProtectedDirs` 虚计、`UnprotectedRoots` 谎称未保护" | 开码核对到的是**两条已存在的放行通道**：`scanner.go:238-241`（根自身命中清单时记 `startUnprot`，且剪枝不参与）与 `:356-362`（`guard.Dir` 命中后先问 `rootsUnder(rawKeys, all, visitKey(full))`，有根在其下即放行下潜）。**本轮未能构造出可复现的剪枝路径** ⇒ 不写成缺陷 | **待取证**：登记为 M47 系列一条，判据要求一次真夹具（根在 `/System` 型 absPath 条目内部）复跑，取证前不改扫描核心
| **FE-9** | "`format.ts` 以 1024 计算却标 KB/MB，手册用 KiB/MiB" | 属实，但改单位会让现有前端断言变红。按"不许改测试断言让门禁变绿"⇒ 本批不动 | **登记** |

#### C. 登记为 M47 起独立 ID、本批不实施（含现象与最小修法）

登记表逐条给"位置 → 现象 → 证据 → 触发条件 → 最小修法 → 本机可验证性"，正文进 04 §6.11 附表。此处摘要：

- **OPS-2**：`trash_linux.go:83-102` 跨卷复制完成后按**路径**盲删源；失败分支删 `.trashinfo` 留下无主孤儿条目（AS-H4 同形状）。linux 腿本机造不出第二挂载卷 ⇒ 无法红→绿。
- **OPS-3**：`undo.go:389-399`、`:279`、`merge_guard.go:113-117` 先查后用（复核与动作之间无抢占），`claimExact` 未覆盖这三处 ⇒ 需连带更正 04 §6.9.8"兑现边界 5：各自已有防线"的判定依据，属裁定面。
- **OPS-4**（由降级而来）：Windows `defaultTrash` 任何路径都返回空 dstMap（`trash_windows.go:237-280` 从声明到 return 一次未写），批量整批报错时"源消失 + 无落点"必落 `strict && !known[p]` ⇒ 记 Failed + 数据丢失警报。这是 `executor.go:436-476` 注释里**明文裁定**的取向，代价（TrashedBytes 少计、账本记 failed、结果集不清理）如实登记，不改判据。
- **OPS-5**：executor 四栏用 `e.Size` 而非 `ActualBytes()`，与扫描页两口径并存 ⇒ **产品裁定**，非缺陷。
- **OPS-8**：darwin `osascript` 整段跑完才输出，批量失败丢全部 dstMap ⇒ C2 修复在 darwin 腿失效。需二分重试改造，改动面大。
- **OPS-11**：`verify.go:43-64` 把"打不开/读不了"（`os.Open` 非 ENOENT、`f.Stat()` 失败、`HashFull` I/O 错）折进 `VerdictFailed`，`executor.go:239` 一律套"文件在扫描后被修改"文案 ⇒ 三类"无从判定"与一类"确实变了"共用文案。
- **OPS-12**：`trash_windows.go:441-448` `verifyRecycled` 用 `os.Stat` 判"源是否还在"，任何错误都被当成"已消失"（ACL/EACCES/EIO 会让仍在盘上的文件通过判据 1）。应走 `Lstat` 并把非 ENOENT 按"仍在"处理。
- **OPS-13**：`verify.go:101-104`（err → false）× `executor.go:405/493/518/538/598`：`identityStill` 分不出"文件已消失"与"被替换"，用户自己删掉 dup 会得到"已被替换、已拦截"且记 Failed ⇒ 不进 app 的 `gone` 集合，结果集留一个不存在的路径。
- **OPS-14**：`merge_guard.go:96-102` 的 `slotProvesSymlink` 走 `verifySymlinked`，后者在 `!tid.Resolved || !kid.Resolved` 时只比"能打开 + 大小一致"（`symlink.go:174-186`）⇒ 删侧取证在 NFS/FUSE 卷上可能把用户自己的链接当我们的残留删掉，与 `slotProvesHardlink` 的 fail-closed 口径相反。
- **OPS-15**：`undo.go:186-198` 复用 `MoveFile(it.DestPath, filepath.Dir(it.OrigPath))`，而正向移动因重名递增过 `photo_1.jpg` 的文件回撤后停在 `photo_1.jpg`，不是 `OrigPath`。
- **APP-5**：`GetOpRecord` 无分页且逐条 stat。**APP-8**：`app.go:1188-1194` `startCmd` 丢弃退出状态（`go func() { _ = cmd.Wait() }()`）⇒ `RevealInFolder`/`OpenTrash` 起得来但随后失败时完全不可见。**APP-11**：`history.go:115-148` 会话级 PRAGMA（`foreign_keys`/`busy_timeout`）只在开池时发一次，`database/sql` 遇 `ErrBadConn` 重建那条唯一连接后不重放。**APP-12**：`app.go:279-287`+`:1198` `cfgDir` 取不到时 `settingsPath()` 仍返回相对路径 ⇒ `SaveSettings` 写进程 CWD。**APP-13**：`app.go:1803` `beginJournal` 早于 S4 校验（`executor.go:187-190`）⇒ 未获批的 `delete` 也在账本留一条记录（`FinalizeOp` 收口为 cancelled），且 `FailedItem` 无 Path。
- **FC-1**：`fscase` 探测失败时兜底"不敏感"，与"不确定时倾向不折"相反 ⇒ 会让 `dedupeRoots` **丢弃**一个真不同的根（方向是少扫，不是错删）。**FC-2**：`Fold` 在所有平台把 `\` 当分隔符。
- **I5-1**：`\`→`/` 归一在 `keyOf`/`fscase.Fold`/`sysguard.normalize`/`toSlashPat` 四处实现且规则互相冲突；`sysguard` 有"不得 import internal 包"的自设分层约定 ⇒ 收归单一实现**需用户裁定优先级**。同时 M26 划账"四处只剩一份"须加限定：scanner 包内成立、全仓不成立。
- **SCN-4**：`scanner.go:560`（`sort.Strings` 排**原样**串）+ `:570-588`（只回看 `kept`）⇒ 不敏感卷上宽根 `/data/b` 与子根 `/data/B/A` 因 `'B'(0x42) < 'b'(0x62)` 使子根先入 `kept`，宽根不被判重 ⇒ 同一棵树走两遍，`UnprotectedRoots` 跟着多计（M26 修后的残留形态）。
- **SCN-5**：`res.Visited = len(visited)`（`:495`）偏高——`:253-255` 先把全部根预置、`:374-383` 在 **submit 时**登记而非读成功后、`:302-310` 取消后排空与 `ReadDir` 失败的目录同样留痕。与字段注释"实际访问目录数"不符；目前只被诊断/测试读取（`scanner_test.go:256`、`gate_cancel_test.go:127`）。
- **SCN-6**：`scanner.go:554-558` 只做 `Abs`+`Clean`（无 `EvalSymlinks`）+ `:240` `guard.Dir(r, filepath.Base(r))` ⇒ 目录符号链接作根时 `ReadDir` 会跟随，而保护/留痕判据按**链接名**判（与 AS-H3 修正前同形）。
- **PRG-1**：`progress.go:58-67`（取快照后**先释锁再 cb**）与 `:74-86`：ticker 取 S1 后释锁，`Stop` 取 S2(≥S1) 并 cb，随后 ticker 才 cb(S1) ⇒ "节流不丢终值"可被回退一次。`progress_test.go` 只测终值包含全部计数，未测这个交错。
- **FLT-2**：`filter.go:83-88` 文档承诺"`Compile(nil)` 时 Apply 恒 true，调用方可直接调"，但 `scanner.go:364`（`f.IncludeHidden`）、`:409`（`f.AllowCloudHydration`）无条件解引用 `*model.Filters` ⇒ `Walk(ctx, roots, nil, n)` 一遇子目录即 panic（被 I3 的逐目录 recover 吞成整目录漏扫）。
- **STATE-1**：`pipeline.go:234-257` `ValidateTransition` 在 mu 内**只判不写**，写入在锁外的 `setStatus`（`:257`）⇒ 窗口内第二个 `Run` 也能过检查（今天靠 `app.go:590 scanInFlight` 从外部挡），且该窗口内 `Pause()`/`Cancel()` 见 `Idle` 直接回错（`:129`）。
- **WIN-1**：`GetCompressedFileSizeW` 调用前未 `SetLastError(0)`（返回值 0xFFFFFFFF 才是失败信号，脏的旧 lastError 会误判）。
- **CACHE-2**：`cache.go:168-206` `openDB` 既无 `busy_timeout` 也不 `SetMaxOpenConns(1)`，而同仓 `history.go:120/135` 两样都做了 ⇒ 连带 `PRAGMA synchronous=NORMAL` 只落在一条连接上。
- **CACHE-3**：`cache.go:220-222` `Lookup` 把一切 DB 错误折叠成"未命中"（含运行期损坏、满盘），而损坏判定只在 `Open` 做过一次 ⇒ 库中途坏了永不自愈，每轮只刷 `Store` 失败条目。修法：`Store`/`Touch` 错误经 `dbfile.IsCorruption` 命中时置一次 disabled 标志、只上报一条（与 M25 的 `addStartupNotice` 同槽位）。
- **CACHE-4**：`cache.go:261-265 / 285` 两笔小账实不符：(a) `Store` 已 Commit 成功后 `evictLocked` 才失败，返回值仍被 `pipeline.go:403` 写成"缓存写回失败"；(b) UPSERT 的 `full=excluded.full` 无条件覆盖，而缓存失效判据含 ctime（`cache.go:230`），`chmod`/xattr/`rename` 这类只动元数据的操作会推进 ctime ⇒ 与 `fsid.SameIdentity` 刻意不含 ctime 的裁定（DOC-H2）方向相反，需一并裁定。
- **MEDIA-1**：`pipeline.go:271-273`（配 `scan.ts:373`）G6 的介质自适应只在 `cfg.Threads<1` 时生效，而载入一条历史会把 `threads.value` 回填成历史行的显式线程数 ⇒ 此后介质探测静默不参与。另 `media/probe_darwin.go:146-149` 的 `diskutilCache` 是进程内缓存。
- **FE-4**：`scan.ts:688-693` `guard` 与上锁之间隔了一次 `await`。**FE-5**：`scan.ts:365-372` `rescanHistory` 逐字段回灌，`Filters` 共 7 位此处只赋 6 位 ⇒ `AllowCloudHydration` 丢失（当前 GUI 无该开关，风险是"按此配置重新扫描"这句承诺对未来新增字段失效）。**FE-6**：`RecordsView.vue:72-80` 前端复刻后端 `undoableReason` 却少了平台条件（注释自称"与后端保持同一口径"⇒ 同一判据两份实现，I5 形态）；今天被存量数据遮蔽（`m.undoable` 取自落账列，macOS/Linux 的 trash 记录不显示该徽标）。修法：后端把 reason 文本随 `OpRecord` 下发，前端只渲染——属接口扩面，与裁定③（新增呈现归 M8）相邻，故登记。**FE-7**：`ResultView.vue:63-69` 一次最多投 7 条 toast，池上限 5 且溢出丢最旧 ⇒ 摘要行会被自己挤掉。**FE-8**：`FailedDrawer.vue:39` × `ResultView.vue:451` 同一入口两个"失败"数不同源。**FE-9**：`format.ts:5-10` 以 1024 计算却标 `KB/MB/GB`，而手册 09:113-141 用 KiB/MiB ⇒ 属实但改单位会让现有前端断言变红，按"不许改断言让门禁变绿"改为登记。**FE-10**：`FailedDrawer.vue:41` × `:18-24` 无剪贴板时「复制全部」静默无反馈。

### 15.1 判据

1. **无证删除一律消灭**：任何 `_ = os.Remove(path)` 必须能回答"凭什么说这个名字还是我们的"。
   OPS-1 的判据不是"会不会真删到"，而是"证据链在哪一步断的"——swap 成功即宣告 tmp 名字已交还命名空间，
   此后按名删除无据。
2. **复核必须紧贴动作**：pre-pass 与动作之间的批量 I/O 时长不是零。OPS-7 用"同包其他 kind 都紧贴"作基准。
3. **一处判定一处实现（I5）**：OPS-9/APP-6/FLT-1 都按此收敛到单一出口；跨包收归需新建叶子包的，先登记。
4. **循环必须有界**：同包已给过结论的形状不再重犯（OPS-10 复用 `xdgNameMaxTry` 的裁定，而不是新发明一个数）。
5. **两口径不得互相冒充（I6）**：REAL-1、MODEL-1 都是"标志与数字来自不同总体"，修法统一为"标志与数字同总体"。
6. **fail-closed 优先于放行**：SCN-3、FLT-1、FC-1 三条缺陷的共同方向是"判据失效时放行"，
   修复后判据失效必须落在"少扫/不排"侧而不是"多删/多扫"侧。
7. **门禁的下界也是判据**：GATE-1/2 修的是"判据面可以被删空而全绿"，与 AS-K 批同源。
8. **诚实措辞优先于严重度**：CACHE-1 明确写成"两道声明防线各有一处盲区"，不写成"会错删"——
   executor 的内容复核是第三道，本批不夸大。

### 15.2 改动面（生产文件）

- `internal/hasher/hasher.go`（采样分支变长守卫 + 三入口负 size + 导出 `RejectGrowthBeyond` + DOC-1 注释更正）
- `internal/dedup/pipeline.go`（`equal` 变长守卫与负 size；REAL-1 实占栏复位）
- `internal/model/model.go`（`AnyActualKnown` 只看 `files[1:]`）
- `internal/scanner/scanner.go`（`rootPrefixes` 尾分隔符判尾）
- `internal/filter/filter.go`（`newExtSet` 扩展名归一）
- `internal/ops/move.go`（OPS-1 删无证 Remove、OPS-10 `claimDst` 上界）
- `internal/ops/executor.go`（OPS-7 回退分支身份复核）
- `internal/ops/winerrno.go`（新，无 tag）+ `symlink_windows.go`（删本文件内表）+ `regstatus.go`（引用具名表）
- `internal/sysguard/sysguard.go`（APP-7 `CLOCK$`、APP-9 prefix/suffix 装配归一）
- `app.go`（APP-1 With 变体 + 锁外预热、APP-2 `cchSnapshot`、APP-3 两处走 `warnLedger`、APP-4 todo 口径、APP-6 `undoableFor`）
- `cmd/fdd-cli/main.go`（APP-10 `failed: []`）
- `frontend/src/stores/scan.ts`（`lastOpKind`）、`frontend/src/views/ResultView.vue`（FE-1）、
  `frontend/src/utils/opdisplay.ts` + `components/ConfirmDialog.vue`（FE-2 never 钉）、
  `frontend/src/views/RecordsView.vue`（FE-3 disabled + 代际）
- 门禁与测试：`scripts/test-frontend-logic.sh`（GATE-1 两条锚）、`wails_types_test.go`（GATE-2 下界）
- 文档：`docs/04-开发与测试计划.md`（GATE-3 `SKIP=''`→`QUARANTINE=()`、DOC-H2 §3 H2 行括注）、04 新增 §6.11

### 15.3 探针（修前必红）

1. `internal/hasher/growth_sampling_test.go` 4 条：采样路径变长未被拒 / 负 size panic / 错误自证入参不可信 / 负控制（size 相符时采样结果逐位不变）。
2. `internal/ops/`：swap 成功后 tmp 位第三方文件必须存活（OPS-1）；回退逐个 trash 前身份变化必须拦截且不删除（OPS-7）；`claimDst` 在占位持续被占时须在有界次数内返回错误（OPS-10）。
3. `internal/dedup/`：paranoid `equal` 在文件变长时须判不一致（PARA-1）；负 size 须报错而非返回 true。
4. `internal/scanner/`：根为 `string(filepath.Separator)` 时 `relativeTo` 须给出真实相对路径（SCN-3）。
5. `internal/filter/`：`ExcludeExtensions = ["tmp"]`（无点）须真的排除 `.tmp` 文件（FLT-1）。
6. `internal/model/`：仅保留项 `ActualKnown` 时 `AnyActualKnown()` 须为 false（MODEL-1）。
7. `app.go`：`CacheStats`/`CacheClear` 的句柄读取须经快照（AST 门禁扩项即探针，APP-2）；批量回撤须吃到 `undo_failed` 项（APP-4）；回撤真值与文案须同源（APP-6 表驱动）。
8. `internal/sysguard/`：`CLOCK$`/`CLOCK$.txt` 须判保留名（APP-7）；大写形态的 prefix/suffix 条目须命中（APP-9）。
9. CLI：无失败项时 JSON 须为 `"failed": []`（APP-10）。
10. 前端：`lastOpKind` 非 trash 时按钮不显示（FE-1，node 用例）；`opdisplay`/`ConfirmDialog` 的 never 钉使新增 kind 在 `typecheck` 报错（FE-2）。

### 15.4 变异

每条实施后逐条改坏并抄真读数：删守卫→应红；把上限常量改 0→应红；`AnyActualKnown` 改回全遍历→应红；
`newExtSet` 去掉补点→应红；`rootPrefixes` 恢复无条件追加→应红；删 `cchSnapshot` 改回裸解引用→AST 门禁应红；
`undoableFor` 的 trash/windows 支改回 true→表驱动应红。全部读数写进 04 §6.11，不写本文。

### 15.5 未兑现与边界

- linux 腿（OPS-2/OPS-7 的跨卷部分）与 darwin osascript 腿（OPS-8）本机不可构造 ⇒ 平台相关改动一律写
  "**代码已改、验证未兑现**"，绝不写"已通过"。
- `smoke-symlink.sh` 需 root 挂独立文件系统，本机 rc=2 是**合法跳过**，不得读成通过（本轮复跑仍 rc=2）。
- 本批不动 `format.ts` 单位（FE-9）、不动 executor 四栏口径（OPS-5）、不收归 ads 与 sysguard 的跨包常量
  （OPS-9 包内部分 / I5-1），这些都需要用户裁定或新叶子包，属扩面。
- CACHE-1 的修复把"采样分支无变长守卫"这一处盲区补上，但**不声称**"变长假重复已被彻底排除"：
  缓存 full 的可信度仍建立在四点采样 + 变长探测 + paranoid 内容比对三者之上。

### 15.6 实施后追记（2026-09-21，代码面 `8c89d9e`，划账 04 §6.11）

1. **A 表 27 条全部落地**（OPS-1/7/9/10、CACHE-1、PARA-1、HASH-1、DOC-1、SCN-3、FLT-1、
   REAL-1、MODEL-1、APP-1/2/3/4/6/7/9/10、FE-1/2/3、GATE-1/2/3、DOC-H2）。探针共
   探针文件按 `git show --name-status 8c89d9e` 数：**新增 13 个测试文件**（12 个 Go +
   `frontend/tests/scan-history-race.test.ts`，另有 1 个新生产文件 `internal/ops/winerrno.go`）
   + **5 个既有用例文件扩写**（`app_hist_race_test.go` / `app_ledger_warn_test.go` /
   `app_undo_test.go` / `internal/ops/process_test.go` / `wails_types_test.go`），
   合计 14 A + 27 M = 41 个文件。修前红读数逐条抄进 04 §6.11 二。

2. **偏离一（必须如实记，不得当成已完成）：APP-4 的结果文案项未实施。** §15.0-A 的修法写的是
   "`todo` 过滤改 `done || undo_failed`，**结果文案带「另有 N 项此前失败」**"。前半句已实施并
   有红→绿；后半句**不做**：那个 N 是一项新的计数呈现，直接撞裁定③（新增计数的界面呈现归 M8）。
   用户报的那个症状（部分失败后再点"全部回撤"得到"已回撤或未实际执行"这句假话）由前半句修掉：
   失败项现在真的进入重试集合，措辞与事实重新对齐，不需要新数字也已经不说谎。

3. **偏离二：OPS-10 的上限常量改了名、搬了家。** §15.2 原文只写"复用同一个包级上限常量"，
   实际是 `trash_linux.go` 的 `xdgNameMaxTry` 变成 `move.go:375-380` 的 `nameMaxTry`。
   原因是硬的：原常量住在 `//go:build linux` 文件里，darwin 上**根本不可见**，
   `claimDst`（无 tag）引用它会使非 Linux 腿编译失败——"共用一个数"这句话在原地无法兑现。
   改名是因为它不再只服务 XDG 一条腿。`trash_linux.go:135/145` 两处引用同步，取值 10000 一字未动。

4. **偏离三（形态更优，但与设计稿不同）：FE-1 的判据取自后端实测字节，不取自前端自记 kind。**
   §15.2 写的是"`stores/scan.ts`（`lastOpKind`）"，实施改为 `v-if="store.opsResult.TrashedBytes"`
   （`ResultView.vue:445`）。理由：`lastOpKind` 是前端自己记的第二个真值源，一旦某次操作
   实际未回收任何字节（全部 skip / 全失败），它仍会显示按钮——正是本项要防的"暗示去回收站看看"。
   取后端字节数则**不需要新增 store 状态**，与裁定③ 的边界也更干净。`grep -rn lastOpKind frontend/src/`
   本轮实测为空。

5. **三条"改前无红可读"的性质说明（不得冒充修前必红）**：OPS-9 是两套命名的收归，
   行为逐字不变（取证 = `regstatus.go:24` 现为 `const regErrNotFound = errFileNotFound`、
   全包裸 `Errno(3)` 归零，加三平台 `go vet` 各 rc=0）；APP-6 是两份**同语义**实现收归一份，
   改前两侧恰好一致 ⇒ 没有可红的分歧，红-绿全靠两次变异（04 §6.11 三）；
   DOC-1 / GATE-3 / DOC-H2 是文档与注释更正，证据形态是"改前原文 ↔ 与之矛盾的代码事实"。
   另有一条**改前即 PASS**：`TestApplyKeepPolicyProbeIsCacheHitAfterWarm` 是 APP-1 的正对照
   （钉"预热确实生效"），不是缺陷探针，把它算作"修前必红"就是说谎。

6. **门禁面撞出一条既有用例钉错了载体**（就地论证后更正，未放宽）：APP-3 让
   `ExecuteOperation` 的告警由 1 条变 5 条，`app_ledger_warn_test.go:133` 的
   "末条告警应指向 FinalizeOp" 当场红。原因是 `eventRecorder.payloads` 按事件名**只存最后一条**
   载荷，"末条"这个说法本身把断言钉在了错误载体上。改后为集合断言（`captureAppErrors` 收全部
   `app:error`）+ `countEvent >= 2`，并**新增**"收尾那条必须在场"这一层约束：覆盖面是收紧不是放宽。
   这与"不许改测试断言让门禁变绿"不冲突——该许可的适用条件是"断言钉错了语义"，本条正是。

7. **产物面：本轮前端是运行时改动，§14.5-6 那条反向钉子在本轮不适用。**
   bundle 由 `index-DQDgG5FN.js 152.55 kB` 变 `index-CGxl9jq2.js 152.84 kB | gzip 59.27 kB`
   （FE-1/2/3 都进运行时包）。因此 04 §6.10 里"`npm run build` 产物逐字不变 ⇒ 裁定③ 未越界"
   这类论证**不能**再被下一轮引用来为本轮辩护；本轮对裁定③ 的遵守改由第 2 条（明确不做 N 项文案）
   与"三处前端改动全部是修正既有假话/既有失效承诺"来支撑。

8. **计数**：源级 **645**（上轮 606，净增 **39**）/ 顶层 **594 PASS + 4 SKIP + 0 FAIL** /
   子用例 **74 PASS + 1 SKIP = 75** / `=== RUN` **673** = 598 顶层 + 75 子用例。
   ★ 口径钉死：裸 `grep -rh '^func Test' --include='*_test.go' .` 本轮读出 **662**，多出的 **17**
   个是已 gitignore 的 `.workbuddy/` 母本副本；排除该目录后的 **645** 与
   `git grep -h '^func Test' HEAD -- '*_test.go'` 逐字相同，同一命令在 `f5ae910`（设计段提交）上
   是 **606** ⇒ 与 §6.10 记的 606 同口径，+39 可直接引用（不必再猜"上轮有没有把副本数进去"）。
   skip 名单与 §6.9.13/§6.10 同一批（4 顶层 + 1 子用例），本轮未新增也未消除任何 skip。
9. **B 表复核（防止下一轮照抄子代理）**：SCN-2 **推翻**（`scanner.go:253-255` 把全部根预置进队列，
   隐藏规则剪掉中间目录不丢被点名根的文件）；OPS-4 **降级**为登记（`executor.go:436-476`〔本批后为 `:440-474`〕注释里
   是明文裁定的取向，不是缺陷）；OPS-6 **改性质**为 DOC-H2（文档与代码不符，代码是对的）；
   SCN-1 **改判待取证**（开码找到两条已存在的放行通道 `:238-241` / `:356-362`，本轮构造不出
   可复现的剪枝路径，登记为 M65 并要求真夹具）；FE-9 **属实但不做**（改单位会让现有前端断言变红）。

10. **GATE-1 的条目数与设计稿不同（多于，不是少于）**：§15.0-A 写"补 2 条锚"，实际补 **4 条**
    （`scripts/test-frontend-logic.sh:92/97/99/104`），接线断言由 2 → **6**。多出的两条是 FE-2
    的两处 switch 各占一条（`opdisplay.ts` 与 `ConfirmDialog.vue` 是两个独立失效点，合起来锚
    等于漏锚），以及 FE-3 那条需要 `forbid` 位——它禁掉的是被替换掉的旧写法
    `store.scanning || store.opsRunning`，只锚新写法的话旧写法复活也不会红。


---

## 16. M64（I5-1）：路径归一实现的收归（04 §6.11 登记表；2026-09-21 用户裁定"优先收归，分层约定后补"）

### 16.0 动手前取证（位置 → 实际读到的代码 → 裁定）

**归一实现（登记时说"四处"，逐条开码读到的实际形态）**

| # | 实现 | 位置 | 对 `\` 的处理 | 尾斜杠 | 大小写 | 生产调用点 |
|---|---|---|---|---|---|---|
| 1 | `keyOf(p, sep)` | `scanner.go:167-169` | 只换 `sep` 传入的那个字符（H6：平台真值由参数注入） | 不动 | 不折 | `visitKey:153`、`dedupeRoots:575/578` |
| 2 | `fscase.Fold(p, sensitive)` | `fscase.go:31-41` | **恒**换 `\`→`/`（含 unix） | 不动 | `sensitive=false` 时 `ToLower` | `ops/keep.go:161/267/284/322`、`scanner:575/578` |
| 3 | `sysguard.normalize(p)` | `sysguard.go:285-293` | **恒**换（:282-284 注释写明理由："不用 filepath.ToSlash/Clean：它们在 Linux 上把 `\` 当普通字符，而本包必须在任一 GOOS 上判定 Windows 风格路径"） | **去全部尾 `/`，根 `/` 保留** | 不折 | `:202/203/234` |
| 4 | `filter.toSlashPat(s)` | `filter.go:139-144` | **恒**换，带"无 `\` 即原样返回"的零分配 fast path | 不动 | 不折 | 模式侧 `:125`、rel 侧 `:175/:198` |

三条**改变判据形状**的实测发现（不是照抄登记条目）：

1. **`keyOf` 在 `dedupeRoots` 里是空操作。** `:575` 写的是 `keyOf(fscase.Fold(r, sens[i]), string(filepath.Separator))`，
   而 `Fold` 已恒把 `\` 换成 `/` ⇒ Windows 上串里已无 `\`（sep 换不到东西）、unix 上 sep 就是 `/`（换了等于没换）。
   故 `keyOf` 的**真实作用面只有 `visitKey` 一条**（遍历期键空间，M36 决定不折 ⇒ 它是那条路上唯一的归一）。
   这个空操作不是 bug，但它是"四处各自长出来"能长期不被发现的形状证据。
2. **`filter` 的两侧同函数**（模式侧 `:125` 与 rel 侧 `:175/:198` 都过 `toSlashPat`）⇒ unix 上把合法文件名
   `a\b` 换成 `a/b` 是**对称**的，匹配关系不变。所以 FC-2（M63）的实际风险面只在 `fscase.Fold`
   （`dedupeRoots` 的合并判据），**不在** `filter`——登记条目里"影响折叠与排除判据"这半句是扩大了的表述，本节按实测收窄。
3. **分层约定是文档级、无门禁强制。** `sysguard.go:15-19` 写"不 import 任何 internal 包"，但全仓
   grep 无任何用例/脚本检查 import 面（AST 门禁只有 `app_hist_race_test.go:95/176` 那类，检查的是锁内字段访问）
   ⇒ 新增一个叶子依赖**不会**让任何门禁变红，"后补"补的是**说法**而不是**判据**。
   同时实测 `go list ./internal/...` = **14 个包**（`ads cache cloudfile dbfile dedup filter fscase fsid hasher
   history media model ops progress realbytes scanner sysguard worktemp`）⇒ 新增 pathnorm 后 04 §7 交付物清单
   里那句"14 包"是要一起改的真值。

**同一"/"键空间里的第二条判据（前缀）也长了四份**——登记时只数了归一，开码发现这才是更该收的那一半：

| 位置 | 表达式 | 调用前的归一 / 守卫 |
|---|---|---|
| `scanner.go:173-175` `underKey` | `key == root \|\| HasPrefix(key, root+"/")` | 两侧都是 `visitKey`/`keyOf` 产物 |
| `sysguard.go:296-301` `under` | `p == e \|\| HasPrefix(p, e+"/")` | 两侧过 `normalize` |
| `ops/keep.go:320-323` `inDirFold` | `foldedPath == prefix \|\| HasPrefix(foldedPath, prefix+"/")` | `TrimSuffix(Fold(Clean(dir)),"/")`；**prefix 为空串 ⇒ 整卷命中**（注释写明是刻意的） |
| `filter.go:271-273` | `prefix != "" && (rel == prefix \|\| HasPrefix(rel, prefix+"/"))` | 多一条"空模式不得命中一切"守卫 |

四份的**表达式同形**，差别只在调用前的归一和空串守卫 ⇒ `Under(child, parent)` 一份可覆盖全部四处
（`Under(x, "")` 展开后正好是 keep.go 想要的"空前缀即整卷"，而 filter 那条守卫本就在调用侧）。

### 16.1 判据与裁定落地方式

1. **单一实现落 `internal/pathnorm`**（新叶子包，import 面只允许 `strings`，无 build tag）。
   为什么不塞进现有包：`fscase` import `os`+`worktemp` 且要写盘探测（会把副作用带进 sysguard 的纯字符串判据面）；
   `model` 是数据类型包；`sysguard` 自己是被依赖方。
2. **API 两个，都是"一处判定一处实现"的收口**：
   - `func Slash(p, sep string) string`：把 `sep` 那一个字符换成 `/`；`sep` 由**调用点**给
     （平台真值 `string(filepath.Separator)` 或字面 `"\"`），不含该字符时原样返回（零分配 fast path 收成一份）。
   - `func Under(child, parent string) bool`：`"/"` 键空间的前缀判据一份实现。
3. **行为逐字不变**：每个调用点传它今天用的那个字符，故本轮**不产生任何语义变化**——
   `Slash(p, "\\")` 与今天的 `normalize`/`toSlashPat`/`Fold` 的替换腿逐字同，
   `Slash(p, string(filepath.Separator))` 与今天的 `keyOf` 逐字同。
   **FC-2（M63）因此本轮不修**：它是行为变化（unix 上 `\` 算不算分隔符），登记仍在 M63；
   本条的收益正是"改它只需要动一处"。
4. **不收的三样**（防过度收归）：大小写折叠（`fscase` 的卷语义判据，不是路径归一）；
   尾斜杠规则（`sysguard` 的"保根 `/`"与 `keep.go` 的"剥成空串即整卷"是**两种刻意不同**的语义，
   且各自只有一份实现 ⇒ 无重复可收，保持就地）；`keep.hidden`（`keep.go:114`）的按段切分走 stdlib
   `filepath.ToSlash`，单实现、且它是"平台真值"腿的正确用法示例。
5. **收归要被门禁守住**，否则下一轮还会长出第五份：新增根包用例扫描仓内全部非测试 `*.go`，
   断言"`\`→`/` 这一形态只允许出现在 `internal/pathnorm`"。

### 16.2 改动面（生产文件）

| 文件 | 改法 |
|---|---|
| `internal/pathnorm/pathnorm.go`（新） | `Slash` + `Under`，含"哪一字符算分隔符由调用点负责"的分歧说明 |
| `internal/pathnorm/pathnorm_test.go`（新） | 等价性锁（语料对拍四份旧 body）+ 空串/根/连续分隔符边界 |
| `internal/scanner/scanner.go` | 删 `keyOf`（:167-169）与 `underKey`（:173-175）；`visitKey` 改调 `pathnorm.Slash(p, string(filepath.Separator))`；`rootsUnder`/`dedupeRoots` 改调 `pathnorm.Under`；`:575/:578` 的空操作腿删除（保留 `Fold` 腿），并就地写明"为什么删得掉" |
| `internal/sysguard/sysguard.go` | `normalize` 的替换腿改调 `pathnorm.Slash(p, "\\")`（尾斜杠循环保持就地）；`under` 改调 `pathnorm.Under`；包注释 :15-19 的分层约定按裁定"后补" |
| `internal/filter/filter.go` | 删 `toSlashPat`，三处改调 `pathnorm.Slash(s, "\\")`；`:272` 前缀改调 `pathnorm.Under`（`prefix != ""` 守卫留在调用侧） |
| `internal/fscase/fscase.go` | `Fold` 的替换腿改调 `pathnorm.Slash(p, "\\")`，`ToLower` 腿保持就地 |
| `internal/ops/keep.go` | `inDirFold` 前缀改调 `pathnorm.Under` |
| `app_pathnorm_gate_test.go`（新，根包） | 16.1-5 的 I5 门禁 + "五个旧名字不得再有函数体"的退场断言 |
| `docs/04` | §6.8.0 约束 5（分层）与 §6.9.9 的 M26"四处只剩一份"表述按实测更正；§7 交付物清单 14 包 → 15 包；§6.11 登记表 M64 行标注已实施；新增 §6.12 划账 |

### 16.3 探针（修前必红）

| 探针 | 预期首轮读数 | 性质 |
|---|---|---|
| P-1 I5 门禁（仓内 `\`→`/` 只许一处） | **红**，逐条点名 `scanner.go` / `sysguard.go` / `filter.go` / `fscase.go` 四处 | 真红-真绿对；这条是本轮收归能否守住判据的那条腿 |
| P-2 旧名字退场断言 | **红**，点名 `keyOf` / `underKey` / `normalize` / `under` / `toSlashPat` 五个 body 仍在 | 真红-真绿对（收归后只剩调用，不残留第二实现） |
| P-3 等价性锁（`pathnorm` 语料对拍） | **无红可取**：新函数在改前不存在，编译期即无此路径 | 诚实标注：性质同 OPS-9"命名收归、行为不变"，强度证据走 16.4 变异 |
| P-4 既有 M26 钉子（`scanner_m26_test.go:29-33`、`scanner_i2_i3_test.go:237`） | 改前绿；改后必须**仍绿且断言一字不动**（只把 `keyOf(...)` 换个写法） | 反向钉子：若收归把 M26 的语义弄丢，这几条会红 |

### 16.4 变异（每条：改坏 → 跑目标用例 → 抄原文 → 还原 → `shasum -c`）

| # | 变异 | 目标 | 预期 |
|---|---|---|---|
| M-M64-a | `Slash` 删掉零分配 fast path（直接 `ReplaceAll`） | 全量 | 行为等价 ⇒ **不红**；本条的价值是把"fast path 是分配优化而非判据"写成读数，不许当成"变异被杀"记账 |
| M-M64-b | `Slash` 改成 `filepath.ToSlash`（平台语义） | sysguard / filter / pathnorm 的跨平台用例 | 红（unix 上不再换 `\` ⇒ Windows 风格清单与模式在 Linux 主门禁失效） |
| M-M64-c | `Under` 写成 `HasPrefix(child, parent)`（少一条 `/`） | scanner / filter / pathnorm 前缀判据 | 红（`/data/ab` 被判为 `/data/a` 的后代） |
| M-M64-d | 在 `scanner.go` 里手写第五份 `\`→`/` 本地实现 | P-1 门禁 | 红 ⇒ 证明这条门禁真的在守，而不是恒过 |

### 16.5 边界与未兑现

1. **本轮零生产语义变化**，因此不声称修掉任何一条既有缺陷；M63（FC-2）/M65（SCN-1）等仍在登记。
2. **Windows 真机腿不新增**：`Slash(p, string(filepath.Separator))` 在 darwin 上就是恒等映射，
   该差异只由"参数注入 `\\`"的用例面覆盖（既有 M26 用例形状不变）+ 三平台 `go vet`/`build` 各 rc=0。
   不写"Windows 路径语义已验证"。
3. **"分层约定后补"的实际内容**：改的是 `sysguard` 包注释与 04 §6.8.0 约束 5 的**表述**
   （"不 import 任何 internal 包"→"不 import 任何有依赖/有副作用的 internal 包；唯一例外是只 import
   `strings` 的叶子包"），sysguard 的真实 import 面以 `go list` 读数为准（仍为纯字符串判据）。
4. **`keyOf` 在 `dedupeRoots` 的空操作腿被删除**是行为等价的，但它是"读代码时以为它在做某件事、其实没有"
   的形状，故在 16.0-1 留了取证记录，并在改后代码注释里写明理由，避免下一轮有人"补回来"。

### 16.6 实施后追记（只追加，不回改上面各节原文）

实施 commit `f4e1ab8`；划账见 04 §6.12。本节记下**动手后才发现或才被推翻的事**，
其中三处是对本节上文（16.0/16.1/16.4/16.5）的更正，按"不改写登记原文"的规矩写在这里。

1. **16.1-4 说"尾斜杠不收"，实现最后收了第三条腿 `TrimTailKeepRoot`。**
   偏离原因不是设计变更，而是 P-2 那条退场断言（`normalize` 不得再有函数体）与
   "sysguard 的尾斜杠规则保持就地"直接冲突：`normalize` 的 body 里除了替换腿就是去尾循环，
   既不残留 body、又不把这条规则原地复制一份，只剩"把它也做成一份具名判据"这一条路。
   代价：`TrimTailKeepRoot` 的调用点在 `sysguard.Dir:211`，即**遍历期每个目录一次**（不是只在装配时跑），
   多出的是一条 `HasSuffix` 循环——短字符串、无分配，与它换掉的旧 `normalize` 内联循环同形同量。
   收益是"保根去尾"这条规则从此也只有一份。
   ⇒ 16.1 的 API 是**两个**，实际交付**三个函数**。
2. **`sysguard` 的 `n := normalize(dirName)` 改成了 `pathnorm.Slash(dirName, "\\")`，去尾腿没了。**
   这是一处**真实的收窄**，不是等价改写，所以 16.1-3 那句"行为逐字不变"在本调用点上不成立。
   可达性证据：`guard.Dir`/`guard.File` 的全部生产调用点给的"名字"参只有
   `filepath.Base(r)`（`scanner.go:225`）与 `de.Name()`（`:341`、`:376`）两种，
   两者都不可能带尾分隔符 ⇒ 那条去尾在这两个输入上不可观察。
   处置：**不复加**。加回去等于把一条不可达的循环留在每目录一次的热路径上，
   并且"P-2 断言旧名字退场"与"原地保留旧 body"会再次互斥。
   ⇒ 本轮的准确表述是"**零生产可达语义变化**"，不是"零语义变化"（16.5-1 按此读）。
3. **16.0-3 的"14 个包"是把 04 §7 的旧表述当成了实测，真读数如下。**
   `go list ./internal/...` 改前 = **18**，改后 = **19**（新增 `pathnorm`）；而 04 §7 那行"14 包"
   的名字列表自 **M6 批次起就已过期**——缺 `ads` `cloudfile` `realbytes` `sysguard` 四个
   （四个都是 M6-P1~P4 交付时新增的），加上 `pathnorm` 共缺五个。
   ⇒ 本轮连带把 §7 两处（表格行与交付物清单）改为 19 包并补齐名单，见 §6.12。
4. **16.5-3 的坐标是错的**：04 §6.8.0 约束 5 讲的是**平台边界**（`*_windows.go` 只能 build/vet），
   全仓 grep 不到"不 import 任何 internal 包"这句话的文档落点——它**只存在于 `sysguard.go`
   自己的包注释里**（且该注释带着同一个错误交叉引用，写作"04 §6.8.0 约束 5"）。
    ⇒ "分层约定后补"的实际内容因此比上文写的更轻也更重：更轻是无需改任何文档既有行；
   更重是这条约定**从来没有文档落点、也没有门禁**，改完包注释只是把一条口口相传的规矩
   挪到了它唯一存在的地方。本轮按裁定只更新 `sysguard.go` 的注释（含"谁改了 pathnorm 的
   import 面，本段即为失效声明"），并在 §6.12 把这条约定的**现行表述**写进 04。
5. **16.4 的两条预期被实测推翻，按原样记下**：
   - **M-M64-b 首轮只红 4 包（`filter` `pathnorm` `scanner` `sysguard`），`fscase` 与 `ops` 全绿**
     ⇒ 暴露的不是变异问题而是**覆盖缺口**：`internal/fscase` 此前**一条 `Fold` 用例都没有**，
     而 `sysguard` 的内置 `eAbsPath` 六条全是 POSIX 形（`/proc` `/sys` `/dev` `/run` `/private` `/System`），
     没有任何用例需要"恒换 `\`"成立。两条补强用例（`TestFoldSwapLegIsPlatformIndependent`、
     `TestAbsPathEntryReliesOnConstantBackslashSwap`）都是**在看到这条红绿差之后才写的**，
     写回同一变异下复跑取红、还原取绿，读数见 §6.12 表三。
   - **M-M64-c 首轮 `internal/filter` 全绿**（预期"filter 前缀判据红"未兑现）⇒ 同为覆盖缺口：
     既有用例只喂深层后代形态（`a/b/c/d`），没有"只差一个分隔符的邻居"（`a/bc/d`）那一格。
     补两条表项后 `filter` 亦红。
   ⇒ 本节把 16.4 的价值重述一遍：**变异的作用是让"预期的红"和"实际的包名表"对账**，
   对不上时缺口即缺陷；四条变异里三条的实测集合与预测不完全一致，这个差集本身就是本项的产出。
6. **P-1 门禁存在一处已知的形状局限**（写下来，防止被当成更强的守卫）：它检测的是
   `strings.ReplaceAll(_, _, "/")` 这一**表达式形态**。16.4 的 M-M64-b 恰好说明代价——
   变异把整条腿换成 `filepath.ToSlash` 后，P-1 依然"违规 0 处"（表达式没了，门禁空洞变绿）。
   ⇒ 它守的是"别再手写一份"，不守"这一份不许被 stdlib 顶掉"；后者的守卫是 16.6-5 那批
   跨平台用例，不是 P-1。本轮不加"必须恰好一处"的下界断言（那会把 pathnorm 内部的实现
   细节钉成门禁，改一次写法就要改门禁），改为在此登记该取舍。
7. **等价性锁（P-3）无红可取这一点照旧成立**，且其价值在 M-M64-b 下被反向兑现：
   `TestSlashBackslashLegMatchesOldThreeCopies` 与 `TestTrimTailKeepRootMatchesSysguardNormalize`
   正是那批先红的用例——锁里的 `gold*` 独立基准不依赖被测实现，所以实现一变形它必然红。

## 17. 第六批：登记表内五条本机可验证项（M66/M68 根侧/M70/M52/M54）+ 两条复核后转待裁定（M62/M56；2026-09-21）

范围来源：04 §6.11 登记表里标"✓ 本机可验证"且**未标"需裁定"**的后端项。挑了七条开码复核，
结果是 **五条实施、两条转待裁定、一条拆半**——拆与转的理由全部写在 17.0，不在实施后补。

### 17.0 动手前取证（登记原文 → 实际读到的代码 → 判定）

| 登记 | 登记原文的关键断言 | 开码读到的现状 | 判定 |
|---|---|---|---|
| M66（SCN-4） | `sort.Strings` 排原样串、去重只回看 `kept` | `scanner.go:545` 排原样串，`:549-552` 的 `sens` 在排序**之后**算（所以按下标对齐没坏），`:553-577` 单向回看 `kept` | **成立**，且形状比登记多一步：排序早于 sens 计算 ⇒ 修法必须把 sens 上移，不能只换排序键 |
| M68（SCN-6） | 根只 `Abs`+`Clean`，`ReadDir` 会跟随，"保护清单挡不住" | `:539-543` 确实无 `EvalSymlinks`；**但遍历内符号链接一律 `continue`（`:303-305`）⇒ 能被跟随的只有"根"这一处**。真后果只有两条：(a) `startUnprot:224-228` 按链接名判 ⇒ 界面**不警示**"这一片已脱离系统保护"；(b) 清单里 `eAbsPath` 型条目（按绝对路径匹配）在链接根下永不命中 | **拆半**：(a) 本轮修（一次 `EvalSymlinks`/根，零热路径）；(b) 不修 ⇒ 新登记 **M84** |
| M70（FLT-2） | "`Compile(nil)` 承诺 Apply 恒 true"与 `f` 无条件解引用矛盾 | 承诺兑现得好好的：`filter.go:119-121` 有 nil 分支，`Apply:141`、`ExcludeDir:187` 各自先 `if m == nil` 短路 ⇒ **Matcher 侧没说谎**。真凶是 `scanner.go:349 !f.IncludeHidden` 与 `:394 !f.AllowCloudHydration` 解引用 `f *model.Filters`，而 `WalkWithGate` 的签名**从没承诺**可空；panic 被 `:273-281` 逐目录 recover 吞成一条 Failed | **成立但归因要改**：不是"文档与实现冲突"，是"遍历器自己吃下 nil 又靠 recover 兜"。修法因此落在 `WalkWithGate` 起首，不动 filter 的承诺 |
| M52（OPS-11） | 三类"无从判定"被折进 `VerdictFailed`，一律套"文件在扫描后被修改" | `verify.go:43-56` open 非 ENOENT / `f.Stat` 失败 / **非普通文件与 stat 失败同在 `:52` 一个分支** / `HashFull` I/O 错 `:63` ⇒ 四处全折 Failed；"确实变了"只有 `:55`（size）与 `:71`（哈希）；文案在 `executor.go:239-240` | **成立且多一处分类**（非普通文件）。并发现一条**必须一起做的加固**：`executor.go:242` 的 `default:` 是"通过"分支，`:559`/`:613` 两个 keep 源 switch **无 default** ⇒ 新增枚举值只要有一处忘列，"无从判定"会被当"通过"放行破坏性动作 |
| M54（OPS-13） | `identityStill` 分不出"已消失"与"被替换" | `verify.go:101-104` 的 `err != nil → false` 确实不分；六个调用点 `:403/:483/:502/:526/:547/:606` 逐字吻合；`gone` 集合在 `app.go:1898-1901`，**`res.Skipped` 与 `res.OK` 同路进 gone** ⇒ 改记 Skipped 就能同时清结果集与 `a.byID`；`delete` 分支 `:507-513` 已有 `os.IsNotExist → ocSkipped` 的可抄形状 | **成立** |
| M62（FC-1） | "兜底不敏感" ⇒ 探测失败按敏感处理 | `probe` 的 `:134`/`:146` 返回的是 `Default()` = `defaultSensitive`，**只在 darwin/windows 为 false**（`default_insensitive.go:7`）⇒ "兜底不敏感"不是全平台性质。更要命的是反向危害：不可写**且真的不敏感**的卷（只读挂载的 exFAT/FAT 移动盘、无写权限的共享目录）改判"敏感"后，`/A` 与 `/a` 两种拼写不再合并 ⇒ 同一棵树走两遍 ⇒ 重复组与可释放空间虚高，正是包注释 `:7-9` 立项目标要防的方向 | **转待裁定**（不是缺陷修复而是取向选择）：两条路各有真危害，且修法必然改两条既有断言（`fscase_test.go:57`、`:124` 都断言 `== Default()`）。反向危害登记为 **M85** |
| M56（OPS-14） | 回撤走 `claimExact(OrigPath)` | `undo.go:192` 走 `MoveFile(dst, dir(OrigPath))`：**原位空闲时本来就精确落回 OrigPath**，唯一差别在"原位被占时"的落点选择；而现有递增名是**安全**的（宁另名不覆盖），问题只是"记录显示的 OrigPath ≠ 实际落点"。登记的 `claimExact` 只解决"抢名"，抢不到仍要三选一（保持递增 / 改 `.fdd-restored` 与 `undoTrash:148-155` 同形 / 显式失败）。附带读到：`app.go:2166-2171` 见 `uerr != nil` 即丢弃 `restored` ⇒ 回撤侧没有"成功但告警"出口，而 `undo.go:173-176` 的 EXDEV 分支**已经在**返回 `(target, err)` 双值——那条告警同样把路径丢了 | **转待裁定**（落点契约）；"成功但告警丢路径"是新缺陷 ⇒ 登记 **M86** |

**M66 的可测性取证（决定探针形状，比登记乐观）**：`dedupeRoots` 只做字符串工作 + `probeCaseSensitive`
（已是包级变量注入点，`scanner.go:138`），**不碰盘**。所以探针直接调
`dedupeRoots([]string{"/data/B/A", "/data/b"})` 并扮演"两卷都不敏感"，在 darwin 与 Linux CI
上给出**同一个确定读数**——不需要真造两棵只差大小写的目录（那是 M36 V2 只能 `t.Skipf` 的形状）。

**M54 的跨平台判据取证**：`fsid.FromPathNoFollow` unix 腿（`fsid_unix.go:27-33`）返回 `os.Lstat`
的 `*PathError`，Windows 腿（`fsid_windows.go:128-130`）返回 `syscall.Errno`；两侧
`errors.Is(err, os.ErrNotExist)` 都成立（Errno 自带 `Is`）。⇒ "已消失"判据是**一个纯函数、
参数注入错误值**的 H6 形状，不需要真机。

**M68(b) 为什么不做（M84 的理由）**：修它要在遍历期同时携带"展示路径"与"真实路径"两个键空间
（队列元素从 `string` 变结构体、`visited`、`rootsUnder` 判据都要分叉）。而"干脆把 emitted
Path 换成真实路径"这一条看似省事，实测代价是本机**全部** `t.TempDir()` 夹具：darwin 上
`/var` 是 `/private/var` 的链接，`EvalSymlinks` 会把每个测试路径都改写 ⇒ 数十条既有断言变红。
⇒ 属改动面裁定，登记不实施。

### 17.1 判据（本批立的五条）

1. **判重集合的入集顺序必须按比较键排序**（M66）。折叠后 `X` 是 `Y` 的前缀 ⇒ `fold(X) < fold(Y)`
   恒成立，所以"按折叠键排序 + 单向回看"是充分的；按原样串排序则把判据交给码位（`'B' < 'b'`）。
2. **根级"已脱离系统保护"的留痕判据要看穿符号链接**（M68-a），但**不改写 emitted 路径**：
   留痕与展示是两件事，本轮只补前者。
3. **`WalkWithGate` 的 `f == nil` 等于"全默认"**（M70），与 `filter.Compile(nil)` 的语义对齐；
   兜底放在解引用侧而不是承诺侧，因为承诺已经兑现且被两处 nil 短路守着。
4. **"无从判定"与"确认已变"分开成文，未知枚举值一律落 Failed**（M52）。第二半是第一半的
   防线：`switch v` 的 `default:` 当前是放行分支，任何新增结论都会先命中它。
5. **复核失败要分"已消失"与"被替换"**（M54）：前者按 S8 记 Skipped（与 `VerifyFile` 的
   `VerdictSkipped`、`delete` 分支的 `os.IsNotExist` 同一口径），后者按 S1 记 Failed 拦截。
   `identityStill` 的签名与**其余十个**调用点**一个都不动**〔§17.7-4 更正：此处原写 15 个是错口径——实测改前全仓非测试调用点为 **16 处**（executor 6 + 其余 10），15 漏算了 `move.go:317` 又把注释行算了进来〕，改为走 `identityStatus`（I5：一处判定）。

### 17.2 改动面（生产文件）

| 文件 | 改法 |
|---|---|
| `internal/scanner/scanner.go` | **M66**：`sens` 计算上移到排序之前，排序键改 `fscase.Fold`（同键再按原样串定序），`out`/`sens` 一并置换；判重循环 `:553-577` 一字不动。**M70**：`WalkWithGate` 起首 `if f == nil { f = &model.Filters{} }`。**M68-a**：`startUnprot` 循环内对每个根做一次 `filepath.EvalSymlinks`，解析成功且原样串不同 ⇒ 用**真实路径**再过一次 `guard.Dir`，命中则把**用户给的那条路径**记进 `startUnprot`（展示与警示仍用原样串） |
| `internal/ops/verify.go` | **M52**：新增 `VerdictUnverifiable`；`verify.go:48`（open 非 ENOENT）/`:52`（stat 失败、非普通文件）/`:63`（`HashFull` I/O 错）三处改判它；`VerdictFailed` **语义与名字都不动**（= 确认内容已变）。**M54**：新增 `identityStatus(path, id) (still, gone bool)`，`identityStill` 改为 `still, _ := identityStatus(...)` |
| `internal/ops/executor.go` | **M52**：`:233` switch 的 `default:` 换成显式 `case VerdictPass:` + 兜底 `default:`（记 Failed "未知校验结论"）；新增 `case VerdictUnverifiable:` 文案"无从判定（打不开/读不了/不是普通文件），已拦截"；`:559`/`:613` 各补 `case VerdictUnverifiable:`。**M54**：六处 `identityStill` 改 `identityStatus`，`gone` 走 `settle(ocSkipped)`；新增 `var verifyFileFn = VerifyFile` 测试接缝（与 `symlinkCreateFn`、`volumeIDOf` 同族） |
| 新登记 | **M84**（M68-b：双键空间）、**M85**（M62 的反向危害）、**M86**（回撤侧成功但告警丢 `restored`）；M62/M56 两行加"转待裁定"括注（**原结论不改写**） |
| `docs/04` | §6.11 登记表：M66/M68/M70/M52/M54 行标注本批处置，M62/M56 行加括注；新增 **§6.13** 划账 |

测试文件新增（不改任何既有断言）：`internal/scanner/scanner_m66_m70_m68_test.go`、
`internal/ops/verify_m52_m54_test.go`。

### 17.3 探针（修前必红）

| 探针 | 首轮预期 | 说明 |
|---|---|---|
| P-1 M66 排序键 | **红**：`dedupeRoots(["/data/B/A","/data/b"])` 在"两卷不敏感"扮演下返回 2 条 | 修后 1 条（宽根 `/data/b` 胜）。纯字符串 + 注入，两端同读数 |
| P-2 M70 nil Filters | **红**：`Walk(ctx, []string{dir}, nil, 2)`（dir 内一子目录 + 一文件）得到 `len(res.Failed)==1`、`len(res.Files)==0` | 修后 Failed 0、Files 全收。这条同时钉住"panic 被 recover 吞成整目录漏扫"的形状 |
| P-3 M68-a 链接根留痕 | **红**：根 = `base/link` → `base/lost+found`，`res.UnprotectedRoots` 为空 | 修后含 `base/link` 一条（原样串）。真夹具、真 `EvalSymlinks`，darwin/Linux 都能造 |
| P-4 M52 三类分开成文 | **红**：目录当 `e.Path` 传入 → 现为 `VerdictFailed`；`chmod 0` 的文件 → 现为 `VerdictFailed`（后者需 `Geteuid()!=0` 守卫，照 `fscase_test.go:48` 形状） | 修后两条均为 `VerdictUnverifiable`；端到端断言 `res.Failed[0].Err` **不含**"被修改"二字 |
| P-5 M52 未知枚举兜底 | **红**：把 `verifyFileFn` 接缝换成返回一个越界 `Verdict(99)` ⇒ 现状**落进 `default:` 放行**，文件被真删 | 修后记 Failed "未知校验结论"。这条是 17.1-4 第二半的门禁，不修则 M52 的加固等于没做 |
| P-6 M54 已消失 vs 被替换 | **红**：接缝内 `VerifyFile` 返回 Pass 后把文件删掉 ⇒ 现状 `res.Failed` 含"已被替换、已拦截"，`res.Skipped` 空 | 修后 `res.Skipped` 命中、`res.Failed` 空；另两条 `identityStatus` 直测（删除 → `gone=true`；rename 顶替 → `gone=false`），并钉住"被替换"仍记 Failed |

**既有钉子反向要求（改后必须仍绿、断言一字不动）**：`identity_still_test.go:153`
（"路径已不存在 → identityStill 判否"）、`ops_test.go:346`/`:381`（篡改与 size 变化 → `VerdictFailed`）、
`scanner_m36_test.go` 两条（遍历键不折）、`scanner_i2_i3_test.go` 的 M26 钉子。

### 17.4 变异（每条：改坏 → 跑目标用例 → 抄原文 → 还原 → `shasum -c`）

| # | 变异 | 目标 | 预期 |
|---|---|---|---|
| M17-a | 排序键换回 `sort.Strings`（sens 仍前置） | P-1 | 红（回到 M66 原状） |
| M17-b | `WalkWithGate` 的 nil 兜底删掉 | P-2 | 红（回到 panic→Failed） |
| M17-c | 根侧 `guard.Dir` 只用原样串（即修前形态） | P-3 | 红 |
| M17-d | `executor.go` 的 `case VerdictPass:` 换回 `default:` 放行 | P-5 | 红 |
| M17-e | 六处 `identityStatus` 里任一处把 `gone` 当"未消失"处理（只判 `!still`） | P-6 | 红（该处所在 op.Kind 的用例） |
| M17-f | `identityStill` 改成"still 为真**或** gone 为真都算没被动过"（把"已消失"放行） | 全量 `go test ./internal/ops` | 红——必须被 `identity_still_test.go:153` 与 `symlink_test.go` 那批钉子杀掉；这条测的是"新函数没有悄悄放松旧判据" |

预测允许被实测推翻；推翻时按 §16.6 的做法把"预测的包名表 vs 实测的包名表"的差集写进 17.6。

### 17.5 交付判据

每条一个"修前必红"真读数 + 修后真绿；全套 15 行门禁 rc 记录；不新增计数、不动前端
（P-4 的文案改只影响 `FailedItem.Err` 字符串，前端 `FailedDrawer.vue:55` 原样渲染，无需配套改动）。

### 17.6 边界与未兑现（写在动手前）

1. **M68 只修一半**：链接根下的 `eAbsPath` 型条目依旧不命中（M84），本批不改遍历核心。
2. **M62/M56 不实施**：取向与契约裁定（17.0 最后两行），等裁定后另起设计段。
3. **Windows 腿零新增真机读数**：M52/M54 的分类判据走 `errors.Is`，Windows 侧只有
   `go vet windows` rc=0 + 既有用例面的形态覆盖，**不写"Windows 已验证"**。
4. **`identityStatus` 只服务 executor 六处**：`move.go:60/102/106/317`、`symlink.go:80/84/117`、
   `merge_guard.go:44/90`、`undo.go:173` 十处**继续用 `identityStill`**——它们各自的处置语义
   与"要不要区分已消失"并不一致（如 `merge_guard.go:90` 是刻意 fail-closed 的删文件侧），
   统一改判属扩大改动面，不做。
5. **P-4 的 `chmod 0` 用例在 root 下自动 skip**（`Geteuid()==0` 无权限拒绝语义），
   非 root 腿才是真读数；目录那条不依赖权限，两端都跑。

### 17.7 实测读数（变异取证与三处"预测被推翻"；2026-09-21 实施后补）

基线锚点（`cp` 备份 + `shasum -a 256 -c` 全程复核，五条变异跑完三个文件仍 OK）：

```
7379977dd3fbecf1d69446596e3e14282a26602745fe4257ee0866304df05109  internal/ops/executor.go
e44bd5366bd71bc7b7d79d19568bbd713660662ff304f011dbbcda139c989339  internal/ops/verify.go
d14aed302d27dbafcc46cef410492c1f475e3b3e5063c48618e903a3b1e9e6ce  internal/scanner/scanner.go
```

| # | 变异（实测执行形态） | 目标 | 实测 | 全包名表 |
|---|---|---|---|---|
| M17-a | `fkeys[i] = fscase.Fold(r, sens[i])` 改为 `fkeys[i] = r`（排序键换回原样串，sens 仍前置） | P-1 | **红**：`kept = [/data/B/A /data/b], want [/data/b]` | 全仓 `go test ./...` 只有 `filededup/internal/scanner` 一个 FAIL，其内只有 `TestDedupeRootsSortsByFoldKey` 一条 ⇒ 与预测**同集**，无差集 |
| M17-b | 删掉 `WalkWithGate` 起首的 `if f == nil { f = &model.Filters{} }` | P-2 | **红**：`Failed = [{… Stage:scan Err:遍历异常已隔离，该目录已跳过: runtime error: invalid memory address or nil pointer dereference}]` | 同上，只有 `internal/scanner` / `TestWalkWithNilFiltersIsAllDefault` |
| M17-c | 根侧 `guard.Dir` 只用原样串 | P-3 | **N/A（未跑）** | M68-a 实现后**回退**（见下 2），P-3 已改成 `t.Skipf`，没有可杀的存活实现。不写"已验证" |
| M17-d | `case VerdictPass:` 换回 `default:`（未知结论即"通过"），并删掉加固腿那段兜底 | P-5 | **红**：`越界校验结论被 default 放行并真删了文件（M52 加固腿失效）` | 只有 `internal/ops` / `TestUnknownVerdictFailsClosed` |
| M17-d′ | 追加一条设计段没列的**弱变异**：只删兜底 `default:`、保留 `case VerdictPass:` | P-5 | **红在另一格**：`Failed = [], want 1 条「未知校验结论」`（文件不再被删，但静默不入队） | 同上。⇒ P-5 的三条断言各自咬住一种失效形态，不是只咬"真删文件"那一种 |
| M17-e | `guardIdentity` 里 `gone` 分支从 `ocSkipped` 改为 `ocFailed` | P-6b | **红**：`Skipped = [], want [ …/dup1.bin ]（S8：目标已达成，与 VerdictSkipped 同一口径）` | 只有 `internal/ops` / `TestIdentityGoneBeforeActionRecordsSkipped` |
| M17-f | `identityStill` 改为 `return still \|\| gone`（把"已消失"也当没被动过） | 全量 `go test ./internal/ops` | **红**：`路径已不存在，identityStill 必须判否` | **只有 `TestIdentityStillRejectsMissingPath` 一条**（`identity_still_test.go:140`，断言在 `:153`）。预测里还写了"`symlink_test.go` 那批钉子"——**实测未开火**：那批钉子走的是"被顶替"分支，与 `gone` 无涉。差集如实记为"预测多于实测"，不代表覆盖有洞 |

**四处必须写进划账的偏差（不按 §16.6 的做法掩掉）**：

1. **P-2 的 panic 首落点不是预测的那条腿。** 设计段 17.0 预测在目录分支（`matcher.Apply`
   之前的 `f.IncludeHidden`），实测在**文件分支**：改后基线里首帧是 `scanner.go:400`
   （`cloudCheck` 之前那道 `f.AllowCloudHydration` 判定），变异体内因删了三行而行号前移。
   ⇒ 缺陷成立、判据成立，但"第一落点"的**位置**预测错了；两者都是"整目录被 recover 吞掉"，
   所以修法的落点（入口兜底）不受影响。
2. **M68-a 实现过又回退，本批交付的是"取证 + 转登记"，不是修复。** 按"根侧解析真实路径后
   再问一次清单"实现后，负控制钉子 P-3b 立刻变红：darwin 上 `t.TempDir()` 落在
   `/var/folders/…`，其真身是 `/private/var/folders/…`，而 `sysguard.go:145` 有条目
   `{kind: eAbsPath, plat: pDarwin, name: "/private", why: "var/tmp/folders 等运行时目录的真身"}`
   **按前缀命中** ⇒ 用户从没点过的普通根全部被判成"脱离保护"并写进 `UnprotectedRoots`。
   这不是实现写错，而是"链接根换真实路径再判"这条判据**本身**会把"真身在保护前缀下"
   误伤成"脱离保护"：清单里同时存在 `/private` 这类**祖先级**条目时，解析后的路径比
   链接名更容易命中清单，方向与逃逸判据相反。⇒ 回退生产改动，P-3 保留为 `t.Skipf`
   （**不得读作通过**），剩余一半转登记 **M84**。
3. **P-6a 无红可取。** `TestIdentityStatusSeparatesGoneFromReplaced` 是新判据的定义性用例，
   `identityStatus` 与它同时落地，改前编译期就没有这条路径——性质同 §16.6 的 P-3 等价性锁。
   M54 真正的"修前必红"是 **P-6b**（`Skipped = []`，抄录见上），不拿 P-6a 冒充。

4. **17.1-5 的"15 个调用点"是错口径，实测 16 处。** 判据 5 写"`identityStill` 的签名与 15 个
   调用点一个都不动"，`git grep` 改前（`ed95ef7`）非测试调用点实际是 **16 处** ——
   executor 6 + 其余 10（`move.go:60/102/106/317` 四处、`symlink.go:80/84/117` 三处、
   `merge_guard.go:44/90` 两处、`undo.go:173` 一处）。15 这个数漏了 `move.go:317`
   （`claimedDst.stillOurs`，写成一行 `return` 的方法，grep 时被当成定义混掉了），
   又把 `merge_guard.go:8`、`symlink.go:49/78` 三条**注释里的** `identityStill` 算了进去。
   ⇒ "一个都不动"这件事本身成立（那十处签名与语义都没改），错的是**计数**，
   已在 17.1-5 就地括注更正、未改写原句其余部分。

**加固腿自己引入的错（必须记，不掩）**：给两处 keep 源 switch 加 returning `default:` 之后，
`go vet` 报 `unreachable code`（`executor.go:614`、`:677`）——因为那两处原本以
`case VerdictPass:` 结尾，加了兜底后 `VerdictPass` 自己掉进 `default:` 被拦死。
修法不是删兜底，而是**补一条显式空体 `case VerdictPass:`**（注释写明"少了这一格，合并全被拦死"）。
教训：`default` 兜底只能加在"成功分支已显式列全"的 switch 上，否则它咬的是成功腿。

---

## 18. 第七批：cache / state / progress 面（M71 / M73 / M74 / M75(a) / M69 / M78；2026-09-21）

登记来源：04 §6.11 登记表内**本机可验证、不需裁定**的六条。M75(b)、M62、M56、M48、M82、M84
仍留在待裁定队列，本批一律不动。

### 18.0 动手前取证（现跑现取，含一次工作树异常）

**0. 先记一件与代码无关但影响读数的事故。** 本批取证开始时（22:51）`internal/dedup/` 全部 15 个
`.go` 被同时覆写成**同一内容**：49152 B（恰好 48 KiB）、同一 mtime、跨文件相同 MD5
（`b2d1ca5d…`），内容是一段 206 行的 `./path:N:…` grep 输出。`grep`/`Read` 读不到任何符号，
看起来像"包被删空"。逐条查实：

- `git status --short` 恰好 15 行 ` M internal/dedup/…`，全仓 341 个跟踪文件里**只有这一包**受损
  （用 python 按 `(md5, len)` 分组扫全部跟踪文件，重复组只有 1 个）；
- `git cat-file -s HEAD:internal/dedup/<f>` 逐个读数 1120~37148 B，**HEAD 侧 15 个 blob 全部完好**
  ⇒ 纯工作树事故，不是提交造成的；`git log`/`reflog` 显示 HEAD 仍是 `36a2a5f`（22:41），未动。
- 处置（不用破坏性 git）：`cp -p` 把垃圾留档到 `/tmp/dedup_corrupt_backup/`（sha256
  `90167c7d…`），再 `git cat-file blob HEAD:<path> > <path>` 逐个写回。
- 恢复后真读数：`git status --short` **空输出**；`go build ./...` `rc=0`；`go vet ./internal/dedup/` `rc=0`；
  `go test ./internal/dedup/` → `ok filededup/internal/dedup 3.287s`。
- **连带更正**：§6.13 第四节那套"15 行门禁 rc=0 / `run=693`"读数取自 ~22:4x，**早于 22:51**。
  它们描述的是"与 HEAD 一致的那棵树"，而现在这棵树是恢复出来的——内容按 git 判定与 HEAD 相同，
  但"读数时刻"这件事不成立，故本批交付时会重跑全套门禁并另报新读数，不拿 §6.13 的旧数充当第七批的门禁。

下面 1~10 是在恢复后的树上重取的本批取证。

| # | 取证 | 真读数 |
|---|---|---|
| 1 | M71 窗口形状 | `pipeline.go:231-239` 在 `p.mu` 内做终态复位 + `ValidateTransition(p.status, StatusScanning)`，**只判不写**；`:246` 释锁；`:247-252` 六个原子计数归零 + `:255 gate.Resume()`；`:257 p.setStatus(model.StatusScanning)` 才第二次取锁写入。同窗口 `Pause()`（`:126-142`）/`Cancel()`（`:166-177`）读到的仍是 `Idle` ⇒ 一个回"当前不在扫描中"、一个 `running=false` 回"没有进行中的扫描任务"。今天**唯一的**拦截在 app 层：`app.go:591-595` 的 `a.mu` + `scanInFlight` |
| 2 | M73 现状（对照组） | 临时探针 `TestZZScratchPlainDSN`（现状形状：裸 path + `db.Exec` 设 pragma）：池上先 `PRAGMA synchronous=NORMAL`+`busy_timeout=5000`，随后取到的第二条连接读数 **`busy=0 sync=2`**（2=FULL，即 SQLite 默认）。⇒ 登记原文"synchronous=NORMAL 只落在一条连接上"由读数控实，不是推测 |
| 3 | M73 修法 A（DSN URI）被真读数否决 | 同探针两种 DSN：`url.URL{Scheme:"file", Opaque:path}` ⇒ 三条连接都拿到 `busy=5000 sync=1`，**但库没落在目标路径**（`os.Stat` 报 no such file，目录里多出名为 `a b` 的文件：`#` 被当成 URI fragment）。`url.URL{Path: filepath.ToSlash(path)}` ⇒ darwin 下落点正确且三连接全 `5000/1`；但同一构造喂 Windows 路径产出 `file://C:%5CUsers%5C…`，`//` 之后的 `C:` 被吃成 authority。本机无 Windows 真机 ⇒ **DSN 形整体否决**（缓存库建到别处 = 每轮白算 + `永不自愈`，且这一半无法验证） |
| 4 | M73 修法 B（照抄 history 的 `SetMaxOpenConns(1)`）被测量否决 | 基准（Apple M4，4000 条库，8 worker × 3000 次主键点查 = 24000 次）：默认池 `139.1 ms`、**单连接 `289.8 ms`（2.08×）**、4 连接 `124.6 ms`。`cache.go:65-66` 的注释"Lookup 用 RLock 并行（阶段 2 多 worker 同时点查）"是被测出来的，不是装饰 ⇒ 登记建议的"与 history 同形"**有害**，否决；顺带把这三行读数写进代码注释，供后人复用 |
| 5 | M73 采修法 C 的可行性 | `go doc modernc.org/sqlite`：`func NewConnector(dsn string) (driver.Connector, error)` 存在 ⇒ 用它取基础 connector、自己包一层 `Connect` 后跑 pragma，**路径仍以纯文件名交给驱动**（不引入 URI），每条新连接都带 pragma。另一条 `RegisterConnectionHook(fn ConnectionHookFn)` 是**进程级**钩子、按 dsn 区分，会波及同进程的 `history.db` ⇒ 不用 |
| 6 | M74 折叠点 | `cache.go:211 Lookup` → `:220-222 row.Scan(...) err != nil ⇒ return Entry{}, false, false`，`sql.ErrNoRows` 与"库坏了"走同一条出口。全仓 `IsCorruption` 只有两处 consulted（`cache.go:92`、`history.go:161`，grep 读数）⇒ 打开之后运行期的损坏没有任何判定 |
| 7 | M74 提示槽位（推翻登记原文的修法） | 登记写"只上报一条（与 M25 的 `addStartupNotice` 同槽位）"。实测槽位消费方式：`scan.ts:851-856` 的 `api.getStartupNotice()` 在 **store init 的"初始拉取"里调一次**（`.then(msg => toast().push(...))`），全仓再无第二个消费者（`grep StartupNotice` 读数：只有 `wails.ts:316/389` 的类型与包装 + `scan.ts:852` 这一处调用）；`app.go:1267-1270 GetStartupNotice` 的注释自己写明"前端在 store.init 的'初始拉取'里调一次即可"。⇒ 扫描**运行中**写进去的提示永远不会显示 = 又造一句假话。改走已在显示的通道：`scan:done` 的失败清单（`FailedItem{Stage:"cache"}`，前端每轮 `refreshFailed`）。**这是对登记原文的偏离，理由就是这条读数** |
| 8 | M75(a) 文案与事实错位 | `cache.go:285` 在 `tx.Commit()` 成功（`:282`）之后 `return c.evictLocked()`；`pipeline.go:401-403` 把 `Store` 的任何错误一律写成 `"缓存写回失败: " + e.Error()` ⇒ 哈希**已经写进去了**却被报成写回失败 |
| 9 | M69 回退窗口 | `progress.go:59-67` ticker 分支"mu 内取快照 → 释锁 → cb"；`:76-84 Stop` 同形。两条路径互不排斥 ⇒ ticker 取 S1、释锁，`Stop` 取 S2(≥S1) 并 cb(S2)，ticker 才 cb(S1)，此后 goroutine 已退出 ⇒ **界面最后一条进度是较旧的那条**。既有 `TestTrackerThrottle`（`progress_test.go:32`）只断言"终值包含全部计数"，没测**顺序** |
| 10 | M78 字段位差 | `scan.ts:384-391 rescanHistory` 逐字段回灌 6 位，`emptyFilters()`（`scan.ts:13-24`）有 7 位，缺的正是 `AllowCloudHydration`。历史侧键名对得上：`HistoryMeta.Filters` 是 `model.Filters` 内联 marshal（`app.go` 结构体 + `model.Filters` **无 json tag**）⇒ JSON 键就是 Go 字段名 `AllowCloudHydration`，前端 `m.filters?.AllowCloudHydration` 读得到。`HistoryMeta` 的 TS 侧 `filters: Filters`（`wails.ts:71`）已含该字段 |

### 18.1 判据（六条，每条都可证伪）

1. **M71**：状态机的"认领"必须是一次动作——判与写在**同一个** `p.mu` 临界区内完成。抽出
   `claimRunLocked()`（复位终态 → `ValidateTransition` → 写 `StatusScanning` → 存 `p.cancel`），
   `Run` 只调它一次；`:257` 那句 `setStatus(StatusScanning)` 保留，但语义退化成"认领后若被
   `Pause` 抢走则记 `afterResume`"这一格，不再是写入的唯一落点。
   判据的可观测形式：认领成功后 `Status()` 立刻是 `Scanning`，第二次认领必被拒。
2. **M73**：`busy_timeout=5000` 与 `synchronous=NORMAL` 必须对**每一条**连接生效，且连接池
   不得为了绕这个问题缩到 1；路径交给驱动的形式不得改变（不引入 URI）。
3. **M74**：`Lookup` 只有 `sql.ErrNoRows` 才允许判"未命中"；其余 DB 错误必须留下证据（计数），
   其中被 `dbfile.IsCorruption` 确证者置一次 `corrupt` 标志，置位后 `Lookup`/`Store`/`Touch`
   不再向库发 SQL；一轮扫描至多**一条**FailedItem 说明这件事，且不得说"已自愈/已重建"。
4. **M75(a)**：写回已 Commit 而淘汰失败时，用户看到的必须是"哈希已写回、LRU 淘汰未做（下次
   扫描会再试）"，不得写成"缓存写回失败"。判据用哨兵错误 `cache.ErrEvictFailed` + `errors.Is`，
   不在调用方重新解释 SQLite 文本。
5. **M69**：`Stop()` 返回之后，不得再有更早的快照被外发。实现取向：**先等节流 goroutine 收口**
   （`WaitGroup`）再取终值外发，cb 全程不在锁内（不把 Wails emit 拉进临界区）。
6. **M78**：`rescanHistory` 不得再逐字段手抄——以 `emptyFilters()` 为底、整体透传历史 `filters`，
   只对需要换算的两字段（Min/MaxSize 字节→KB）做覆盖。将来 `Filters` 加字段时自动透传。

### 18.2 改动面（只这些文件）

| 文件 | 动作 |
|---|---|
| `internal/dedup/pipeline.go` | 抽 `claimRunLocked()` 并在 `Run` 起首单次调用（M71）；`Store`/`Touch` 失败文案按 `errors.Is` 分岔（M75a）；轮末按 `cch.Corrupted()` 追加**至多一条** FailedItem（M74） |
| `internal/cache/cache.go` | `openDB` 走 `sql.OpenDB(pragmaConnector)`，每连接两 pragma（M73）；`Lookup` 错误三分类 + `dbErrs`/`corrupt` 原子位 + `Corrupted()`，`Store`/`Touch` 在 corrupt 时短路（M74）；`evictLocked` 失败包 `ErrEvictFailed`（M75a） |
| `internal/progress/progress.go` | `Start` 记 `WaitGroup`，`Stop` 先 `Wait()` 再取终值外发（M69） |
| `frontend/src/stores/scan.ts` | `rescanHistory` 改整体透传（M78） |
| 新增测试 | `internal/cache/cache_m73_m74_m75_test.go`、`internal/dedup/pipeline_m71_test.go`、`internal/progress/progress_m69_test.go`、`frontend/tests/scan-rescan-history.test.ts` |
| `docs/04`、本文件 | §6.14 划账 + §18.7 实施后追记（下一提交） |

### 18.3 探针（每项都要先看它红过一次，失败原因须与预测一致）

| 探针 | 钉的判据 | 预测的"修前红法" |
|---|---|---|
| P-18-1 `TestClaimRunLockedIsJudgeAndWrite` | 18.1-1 | 认领后 `Status()` 仍为 `Idle`（旧形状只判不写）；第二次认领返回"可认领" ⇒ `--- FAIL: … 认领后状态应为 Scanning` |
| P-18-2 `TestPragmasApplyToEveryPooledConn` | 18.1-2 | 第二条连接 `busy_timeout=0`、`synchronous=2`（取证 #2 同形，但走真实 `Open`）⇒ 红在断言，不是 skip |
| P-18-3 `TestDBErrorIsNotSilentlyAMiss` | 18.1-3 | `Lookup` 在库被写坏后返回 `hit=false` 且**无任何证据**：`DBErrors()` 不存在即编译不过 ⇒ 改为对旧形状写"计数为 0 而 Store 报错"的可编译形态，红法须实测抄录，不许预测当读数 |
| P-18-4 `TestStoreEvictFailureKeepsCommittedRows` | 18.1-4 | 淘汰失败被折成 `Store` 返回裸错误，`errors.Is(e, ErrEvictFailed)` 不成立；且已 Commit 的行必须查得到 |
| P-18-5 `TestRunReportsEvictAsNotWriteBackFailure` | 18.1-4 | `FailedItem[0].Err` 以"缓存写回失败"开头 ⇒ 与"已写回"相反，红在该字符串 |
| P-18-6 `TestStopNeverFollowedByOlderSnapshot` | 18.1-5 | cb 序列最后一条 < 序列最大值（ticker 的旧快照压在终值之后）⇒ 红在"末值必须等于最大值" |
| P-18-7 前端 `rescanHistory 透传未来字段` | 18.1-6 | 造一条 `filters` 含 `AllowCloudHydration: true` 的历史记录，重扫下发的 payload 里该位为 `false`/缺省 ⇒ 红 |
| P-18-8 `TestCorruptStopsIssuingSQL` | 18.1-3 | corrupt 置位后仍继续发 SQL（每轮再错一次），`Lookup` 计数持续上涨 ⇒ 红 |

### 18.4 变异计划（预测"该红的包名表"，跑完与实测表相减）

| 变异 | 动作 | 预测被杀于 |
|---|---|---|
| M18-a | `claimRunLocked` 退回"只判不写" | `internal/dedup` P-18-1 |
| M18-b | 删掉 connector 的 per-conn pragma（回到 `db.Exec` 一次） | `internal/cache` P-18-2 |
| M18-c | `Lookup` 把所有 `Scan` 错误折回"未命中"（去掉三分类） | `internal/cache` P-18-3/P-18-8 |
| M18-d | `Store` 末尾 `return c.evictLocked()` 改 `return nil`（吞掉淘汰失败） | `internal/cache` P-18-4 或 `internal/dedup` P-18-5（**两处都算合理落点，记实测**） |
| M18-e | 去掉 `errors.Is` 分岔，文案统一"缓存写回失败" | `internal/dedup` P-18-5 |
| M18-f | `Stop` 不再 `Wait()` 直接取终值 | `internal/progress` P-18-6 |
| M18-g | `rescanHistory` 改回逐字段列举（少一位） | `frontend/tests` P-18-7 |

### 18.5 交付判据

六项各有"修前必红"抄录 + 变异实测表（预测集 − 实测集的差集必须逐条解释）；全套 15 行门禁重跑
（含 §18.0-0 之后必须重取这一条）；`go test -race` 腿不得出现 `data_race` 命中；前端 `typecheck`
与 `test-frontend-logic` 绿（无 node 或 node<22.18 时按 SKIP 报，不得读作通过）。

### 18.6 本批不做 / 转登记

- **M74 的"自愈"那一半不做**：登记原文暗示"确证损坏→隔离重建"。运行期隔离需要 close 现库、
  改名、重开、并把新句柄换回 `p.cch`（app 层持有、扫描在途），跨三层且无真故障可全验。
  本批只做到"确证损坏 ⇒ 停止发 SQL + 说一条真话"，剩下的隔离重建**新登记 M87**。
- **界面新增呈现位不做**（裁定 ③）：M74 只借用已有的失败清单，不新增计数、不加控件。
- **M75(b)（UPSERT 无条件覆盖 `full` + ctime）继续等裁定**，本批一字不动。
- Windows 侧本批不引入任何 URI/DSN 形式（取证 #3），故不存在"代码已改、验证未兑现"的新面；
  若 P-18-2 的实现在 Windows 上改变连接建立方式，划账时如实标注。
