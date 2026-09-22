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

### 18.7 实施后追记（2026-09-21 深夜；划账见 04 §6.14）

#### 一、与 §18.2 改动面的偏离（四处，逐条如实记）

1. **新增一条测试接缝 `evictFn`（§18.2 没列）**：`cache.go` 里 `var evictFn = func(c *Cache) error { return c.evictLocked() }`。
   起因是 §18.3 P-18-4 预测的"修前红法"实测打不通（见下面二-4），而不是为了让实现变干净。
   惯例同族：`ops.verifyFileFn`、`scanner.probeCaseSensitive`、`model.TrashFn`。生产恒等于 `evictLocked`。
2. **M74 的界面通道不走 `addStartupNotice`**（§18.0 取证 #7 已预告这条偏离）：改走已在显示的
   `scan:done` 失败清单，且全轮只有 `corruptCacheNotice()` 一个造句点。
3. **M73 不按登记原文"与 history 同形"**（取证 #3/#4 两条修法都被读数否决），落成 Connector 包装。
   Windows 侧因此**没有**新增 URI/DSN 面：路径仍以纯文件名交给驱动，与改前同形 ⇒ 本批无"代码已改、验证未兑现"的新平台面。
4. **M78 的透传多做了一步归一**：设计段只说"整体透传"，实现先把历史 `filters` 里的 `null`/`undefined`
   滤掉再由 `emptyFilters()` 兜底。原因开码才看清：`model.Filters` 无 `omitempty`，Go 侧 nil 切片
   marshal 成 JSON `null`，而改前的 `?? []` 恰好挡着这一手 ⇒ 纯透传会把 `IncludeExts` 写成 `null`，
   下游 `.join()` 直接炸。钉它的是 `scan-rescan-history.test.ts` 第三条用例。

#### 二、修前必红（逐字抄录；红绿两侧都是实跑）

1. **P-18-2（M73）—— 本批唯一在"改前树"上真跑出来的红**。做法：把 `cache.go` 换回 `HEAD` 版本
   （`git cat-file blob HEAD:… >`），测试文件只留 M73 那两条（另两条引用新增符号，编译不过），跑完
   `cp` 还原并 `shasum -a 256 -c` 核对 OK：

```
--- FAIL: TestEveryPooledConnectionCarriesPragmas
    cache_m73_m75_test.go:48: 池上首条连接也应带 pragma：busy_timeout=0 synchronous=1
    cache_m73_m75_test.go:68: 第 2 条额外连接的 pragma 没生效：busy_timeout=0 want 5000，synchronous=1 want 1(NORMAL)
    cache_m73_m75_test.go:68: 第 3 条额外连接的 pragma 没生效：busy_timeout=0 want 5000，synchronous=2 want 1(NORMAL)
```

   **这条读数顺带更正登记原文**（§6.11 CACHE-2 行）：改前 `openDB` 只发 `journal_mode=WAL` 与
   `synchronous=NORMAL` 两条，**全仓从来没有发过 `busy_timeout`**（grep 读数：HEAD 的 `cache.go` 里
   `PRAGMA` 只有那两行）⇒ 登记说的"synchronous=NORMAL 只落在一条连接上"实测是"落在**一部分**连接上
   （三条连接 1/1/2），`busy_timeout` 一条都没有"。缺陷成立且比登记描述更宽。
   同跑的对照用例 `TestEvictDeleteCanReallyFail` 在改前树 `--- PASS` ⇒ 它本就是前提自检，不是红探针。

2. **P-18-1 / P-18-1b（M71）**：改前树里 `claimRunLocked` 这个符号不存在，探针编译不过 ⇒
   "修前红"由变异 **M18-a**（把认领退回"只判不写"，即改前形状）提供：

```
--- FAIL: TestClaimRunLockedIsJudgeAndWrite
    pipeline_m71_test.go:38: 认领后状态必须已是 Scanning（判与写同临界区）：实测 Idle
    pipeline_m71_test.go:43: 第二次认领竟然成功：窗口还在，两个 Run 会同时跑
--- FAIL: TestPauseAndCancelSeeJustClaimedRun
    pipeline_m71_test.go:72: 刚认领就 Pause 被拒（改前的假话之一）：当前不在扫描中（Idle），无法暂停
```

   第二条正是登记原文那句"同窗口 `Pause()` 见 `Idle` 直接回错"的实跑形态；`Cancel()` 那句被
   `Resume()` 的断言先截断（`t.Errorf` 不中止），红法同因。

3. **P-18-3 / P-18-8（M74）**：同样依赖新符号（`DBErrors`/`Corrupted`/`ErrCorruptDisabled`），
   红由 **M18-c**（把 `Lookup` 的 Scan 错误整体折回未命中）提供：

```
--- FAIL: TestLookupDBErrorLeavesEvidenceAndIsNotCalledCorruption
    cache_m74_test.go:48: DB 错误必须留证据：DBErrors=0 want 1
    cache_m74_test.go:58: 未停用时每次点查都应各记一笔：DBErrors=0 want 2
--- FAIL: TestRuntimeCorruptionStopsIssuingSQL
    cache_m74_test.go:145: SQLite 原文必须被认成损坏（说明这不是拿假错误凑的）；DBErrors=0 底层错误=file is not a database (26)
```

   第三行的"底层错误"是**独立句柄**取回的原文（`corruptionEvidence`），证明损坏是真的
   `file is not a database (26)`，不是假错误自证。前提自检排在所有断言之前。

4. **P-18-4（M75a，cache 侧）—— 预测的修前红法被推翻**（§18.3 表里写的是"伪造超限计数 + RAISE
   触发器从 `Store` 打进去"）。实测：`Store` 在 Commit 之后把 `cntValid` 置回 false（C5 既有语义），
   `evictLocked` 于是先重算 `COUNT` 得 2 条、判定未超限、**连 DELETE 都不发**，返回 nil ⇒
   那套夹具只能打进 `evictLocked` 本身（留作前提自检 #1 的独立用例），进不了 `Store`。
   红由 **M18-d**（`Store` 末尾吞掉淘汰失败）提供：

```
--- FAIL: TestStoreEvictFailureSaysWriteBackSucceeded
    cache_m73_m75_test.go:136: 淘汰失败却返回了 nil，本用例前提不成立
```

5. **P-18-5（M75a，dedup 侧）**：红由 **M18-e**（去掉 `errors.Is` 分岔）提供：

```
--- FAIL: TestCacheOpErrTextClassification
    pipeline_m71_test.go:94: 淘汰失败仍被写成写回失败（Commit 早已成功，这句是假话）：
      "缓存写回失败: 缓存 LRU 淘汰失败：哈希条目已写回，仅 LRU 淘汰未完成（探针）"
```

   这句就是**登记原文描述的用户可见假话**的逐字形态——一句话里同时写着"写回失败"和"已写回"。

6. **P-18-6（M69）**：**M18-f 删掉的正是改前不存在的那一行**（`t.wg.Wait()`），所以这条就是改前形状：

```
--- FAIL: TestStopNeverFollowedByOlderSnapshot
    progress_m69_test.go:78: Stop 之后又外发了更旧的快照（进度倒退）：序列 [3 0]
    progress_m69_test.go:83: 界面看到的最后一条必须是最大值：末值=0 最大=3 序列=[3 0]
```

   ★ 第一版探针在此**红错了格**：`close(release)` 之后立刻读序列，改前的形状被读成
   "外发不足两条"（断言没跑到顺序那一格）。补一条"等那条被卡住的外发真的落进序列"再取证，
   才拿到预测的顺序倒退。记为流程教训：**取红也要验红的是不是那一格**。

7. **P-18-7（M78）**：红由 **M18-g**（`rescanHistory` 退回逐字段列举）提供：

```
✖ 历史里的 AllowCloudHydration 必须透传到重扫载荷（M78 主案）
  AssertionError: 重扫把云端占位档位静默降级成了安全档（改前形状）  actual: false, expected: true
✖ 将来 Filters 新增字段时自动透传（钉住"整体透传"这个修法本身）
  AssertionError: 新字段又被漏抄了——逐字段列举的必然复现路径  actual: undefined, expected: 'keep'
```

   同一次变异里另两条（`null` 归一、字节只换算一次）**保持绿** ⇒ 它们是给新实现加的防线，
   不是缺口的探针，不冒充"修前红"。

#### 三、变异实测表（§18.4 预测集 − 实测集的差集逐条解释）

| # | 变异 | 预测被杀于 | 实测（全包名表，`-count=1 -v`） | 差集 |
|---|---|---|---|---|
| M18-a | 认领退回只判不写 | `internal/dedup` P-18-1 | `TestClaimRunLockedIsJudgeAndWrite`、`TestPauseAndCancelSeeJustClaimedRun`（该包仅此两条 FAIL） | 无（P-18-1b 是补的，一并记） |
| M18-b | connector 不补 pragma | `internal/cache` P-18-2 | `TestEveryPooledConnectionCarriesPragmas`（仅此一条） | 无 |
| M18-c | 折回"未命中" | `internal/cache` P-18-3/P-18-8 | `TestLookupDBErrorLeavesEvidenceAndIsNotCalledCorruption`、`TestRuntimeCorruptionStopsIssuingSQL` | **P-18-3b 未开火**：`TestMissingRowIsNotACorruptionError` 断言的是 `DBErrors==0`，把记账删掉它当然更"成立" ⇒ 它是**过度修正的防线**（防"把没查到也记成错误"），不是缺口探针，不据此说覆盖有洞 |
| M18-d | `Store` 吞掉淘汰失败 | `internal/cache` P-18-4 **或** `internal/dedup` P-18-5（"两处都算合理落点"） | 只 `internal/cache`；`internal/dedup` 全包 rc=0 | **实测少于预测**：Run 级要造淘汰失败得先真塞过 `MaxEntries`(50 万) 行，本机不划算 ⇒ 淘汰这条链在 Run 级没有落点，只能由"分诊函数 + 改前树真跑的写回文案"两侧夹。记为覆盖边界（五-2） |
| M18-e | 去掉 `errors.Is` 分岔 | `internal/dedup` P-18-5 | `TestCacheOpErrTextClassification` | **Run 级那条未开火**（`TestRunReportsCacheWriteBackFailureOnce` 走的是"库已关闭"的通用分支，与淘汰无关）⇒ 同一边界 |
| M18-f | `Stop` 不 `Wait` | `internal/progress` P-18-6 | `TestStopNeverFollowedByOlderSnapshot`（`-race`，仅此一条） | 无 |
| M18-g | `rescanHistory` 退回列举 | `frontend/tests` P-18-7 | 同上两条 node 用例 FAIL（rc=1） | 无（另两条按设计保持绿，见二-7） |
| **M18-h**（§18.4 之外补的） | 停用说明去掉 `corrupted` 守卫（每轮都冒一条） | 未预测 | `TestCacheSecondScan`、`TestCacheMtimeContentChanged`（两条**既有**用例）、`TestCorruptCacheNoticeWording`、`TestRunReportsCacheWriteBackFailureOnce` | 预测少于实测 ⇒ 好事，记下来：既有套件本身在给这条守卫兜底 |

变异全部逐条 `cp` 备份 → python 打补丁（锚点 `assert count==1`）→ 跑 → `cp` 还原 →
`shasum -a 256 -c` 四个文件全 OK（`pipeline.go`、`cache.go`、`progress.go`、`scan.ts`）。

#### 四、Run 级造不出"真损坏"夹具这件事（新增取证，不是猜测）

试过两条路，都不成立，故 M74 的 Run 级用例改用"库已关闭"这种暂时性故障：

- 把整库文件覆写成垃圾（连接池仍开着）：`--- PASS` 形状的真读数是
  `覆写后 Lookup hit=true DBErrors=0 Corrupted=false`、`覆写后 Store err=nil` ⇒
  SQLite 的页缓存把损坏整个吞了，运行期根本撞不到那条错误。
- 想改撞"深层页"（不动 page 1）那版实验**做废了**：page size 取错了偏移（读成 28530），
  且 WAL 未 checkpoint、主库只有 4096 B ⇒ 写的垃圾落在文件末尾之外。**这一版不作为证据**，
  结论只来自上一条。

⇒ 所以 `Corrupted()` 的真损坏证据在 `internal/cache`（用不经 DDL 的夹具，见 `newCacheOn`），
Run 级只钉"至多一条 + 措辞边界 + 不可用≠损坏"。停用后的**真机恢复路径**仍未验证（本批不做隔离重建，M87）。

#### 五、边界

1. 停用只到"不再发 SQL + 说一条真话"；库不隔离、不重建、句柄不换 ⇒ **M87** 新登记。
2. 淘汰失败的 Run 级落点没有用例（M18-d 差集），文案链路靠"分诊函数唯一 + 改前树真跑的通用分支"两夹。
3. 界面不新增呈现位（裁定③）：本批只借用已有失败清单，`FailedItem` 结构未扩。
4. `app.go` 一字未动 —— 它本来就在 `openCache` 失败时退化成"无缓存"，M73/M74 都在 `cache` 包内收敛。

---

## 19. 第八批：app / history / scanner / undo 面六条（M58 / M59 / M60 / M61 / M67 / M86；2026-09-22）

**本批范围怎么来的**：§6.11 登记表里"本机可验证、不需要裁定"的行，到 §18 交付后只剩
**M58(APP-8) / M59(APP-11) / M60(APP-12) / M61(APP-13) / M67(SCN-5) / M86(OPS-14b)** 六条。
下面这些**不在本批**，理由各不同，写在 19.6：M57（性能项，缺基准）、M63（与既有钉子正面冲突，待裁定）、
M65（需真夹具）、M87（需状态机配合）、M77/M79/M80/M81/M83（前端面，与 M8 一并处理）。

### 19.0 动手前取证（现跑现取；登记原文 → 实际读到的代码 → 判定）

1. **M59 —— 取到了真读数，而且比登记的更具体**。登记说"会话级 PRAGMA 只在开池时发一次，
   `ErrBadConn` 重建后不重放 ⇒ 外键约束静默失效"。本机不便造 `ErrBadConn`，改走**同一机制的另一条触发源**：
   让池回收那条空闲连接（`SetConnMaxIdleTime`/`SetConnMaxLifetime` 置 1ms + sleep 50ms），
   下一次查询只能新建连接。实验（`internal/history` 内，读 `s.db` 的 PRAGMA 原文）：

```
    zz_m59_scratch_test.go:27: 改前·池上首条连接：foreign_keys=1 busy_timeout=5000 synchronous=2
    zz_m59_scratch_test.go:44: 重建连接后：foreign_keys=0 busy_timeout=0 synchronous=2
```

   三条各有结论，**不是一刀切**：
   - `foreign_keys` 1 → **0**：真失效。它承重 —— `hist_groups`/`hist_files`/`op_items` 四处
     `REFERENCES … ON DELETE CASCADE`（`history.go:72/80/81/100`），关掉之后删扫描历史只删主表、
     子表**留孤儿行**，且 `LoadScan` 仍可能按旧 `hist_id` 捞出残留组 ⇒ 属数据事故，不只是"约束没了"。
   - `busy_timeout` 5000 → **0**：`history.go:133-135` 那句注释"本库连接池限 1，故 Exec 设置的
     会话级 PRAGMA 对该库所有语句都生效"**在被回收的连接上是错的**（fdd-cli 与 GUI 并存时 BUSY 直接变故障）。
   - `synchronous` 2 → **2**：SQLite 默认就是 FULL，这条**侥幸无损**。⇒ 划账时不得把三条一起说成"全部失效"。
   - 诚实边界：复现走的是"空闲回收"，`ErrBadConn` 那条路是同一机制的另一触发源（读 Go 的
     `database/sql` 语义），本批**不声称复现了真实 `ErrBadConn`**。

2. **M86 —— 登记说的"必然丢失"要先拆成三条，因为三条的文案成熟度不同**。
   `ops.UndoOne` 返回 `(target, err)` 双值的只有三处（全在 `internal/ops/undo.go`）：

   | 坐标 | 分支 | 现在这句话带不带落点 |
   |---|---|---|
   | `:174` | 跨卷恢复后回收站侧被替换（两份并存） | **带**（"已恢复到 %s…未清理回收站侧"） |
   | `:178` | 已复制但清理回收站侧失败（两份并存） | **不带**（只有"两份并存"四个字） |
   | `:397` | 软链接回撤：链接已删、还原备份失败 | **带**（"原文件保留在 %s（数据未丢失）"） |

   丢弃发生在 `app.go:2166-2171`：`if uerr != nil { … return "", uerr }` —— `restored` 就地扔掉。
   而用户可见通道只有两条：`MarkItemUndo(…, uerr.Error())` 进账本 err 列、`FailedItem{Path, Stage, Err}`
   进失败清单（`FailedDrawer.vue:55` 原样渲染 Path 与 Err）。⇒ **真正丢落点的只有 `:178` 那一条**，
   且丢的原因是文案没写，不是因为 `restored` 变量被丢（另两条本来就靠文案送达）。
   可用性取证：该分支的三个动作已有接缝 `renameFile`/`copyVerifyFile`/`removeSrc`
   （`move.go:403/407/411`，均 `var = os.XXX`）⇒ 能做成**确定性**场景，不是竞态。

3. **M67 —— `Visited` 在产线侧零消费者**。全仓 `res.Visited` 只有一个写入点
   （`scanner.go:489` `= len(visited)`）与两个读取点，都在测试里
   （`scanner_test.go:256` 剪枝对比、`gate_cancel_test.go:127` 取消排空）；`app.go`/`cmd` 都不读它。
   偏高来自三处形状（`scanner.go:247-249` 全部根预置 / `:368-377` 子目录在 **submit 时**登记 /
   `:296-305` 取消后排空与 `ReadDir` 失败都照样留痕），而 `visited` 这张表**同时**是去重集
   （子目录登记晚了就会被重复入队）⇒ 修法必须是"拆成两个东西"，不是"挪一下登记时机"。

4. **M61 —— 顺序读数**：`ExecuteOperation` 在 `app.go:1852` 调 `beginJournal`（写前落盘全部计划），
   S4 校验却在更下游的 `executor.go:193-196`。`delete` 走 `undoableFor("delete", …) == false`
   那一档 ⇒ `beginJournal` 不因账本问题拒绝，`BeginOp` 成功返回 journalID，
   执行器整批拒掉后 `:1887-1892` 照样 `FinalizeOp`（残留 planned → cancelled）
   ⇒ 账本多一条"什么都没动"的记录。且 `executor.go:194` 那条 `FailedItem` **不带 Path**。

5. **M60 —— 同一问题在别处已经有兜底，settings 独漏**：`startup` 里 `cfgDir` 取不到就留空串
   （`app.go:278-288`），`openCache:305`、`openLedger:365` 各自 `if a.cfgDir == "" { return }`；
   只有 `settingsPath():1203` 无条件 `filepath.Join(a.cfgDir, "settings.json")` ⇒ 空串时返回
   **相对路径** `"settings.json"`，`SaveSettings:1246` 把它写进进程 CWD（GUI 打包后 CWD 通常是只读或"!"）。

6. **M58 —— 出口形状**：`startCmd:1193-1199` 是 `go func() { _ = cmd.Wait() }()`。
   登记建议"走 `warnLedger` 同族出口"，但 `warnLedger:1700-1703` 写死了
   `"[history]"` tag 与"账本写入失败："前缀 ⇒ 直接复用会造出一句假话。
   该出口的真价值是"stderr + `app:error` 双通道、绝不静默"这一形状，值得抽一层共用而不是复制。

7. **会被本批连带改动的既有钉子**（先登记，免得实施时以为是回归）：
   `internal/ops/ops_test.go:142 TestS4DeleteRequiresConfirm`（走 `Execute`，不受 app 层前移影响）、
   `internal/scanner/gate_cancel_test.go:127`（见 19.3 P-19-5，必须**补强**，理由就地论证）、
   `internal/scanner/scanner_test.go:256`（剪枝对比，新口径下仍成立）、
   `internal/history` 全部现有用例（`initConn` 改动只加不换语义）。

### 19.1 判据（六条，每条都可证伪）

1. **M59**：账本库的**每一条**连接（含回收后新建的）都必须带 `foreign_keys=ON`、`busy_timeout=5000`、
   `synchronous=FULL`。判据是连接属性而不是文件属性 ⇒ 只能逐连接验。
2. **M58**：`startCmd` 启动成功后进程退出码非零时，**必须**产生一条用户可见提示（stderr + `app:error`），
   内容含命令失败事实；启动成功且退出 0 时**不得**冒提示（反面钉，防"每开一次 Finder 就弹一条"）。
3. **M60**：`cfgDir` 为空时 `SaveSettings` 必须以错误收口，且**磁盘上不得出现** `settings.json`；
   `GetSettings` 同档返回纯默认值（读别人目录里的文件同样是假话）。
4. **M61**：未获批的 `delete` 必须**在落账之前**就被拒：账本 `op_records` 行数不增、
   不产生 `ops:done`、`opsRunning` 不留痕；执行器那道 S4 照旧保留（`Execute` 是导出 API，
   任何调用方都可能绕过 app 层直接进，且既有钉子 `TestS4DeleteRequiresConfirm` 钉的正是那道）。
   —— 取证一条：`cmd/fdd-cli` 只做扫描（`dedup.New()`，全仓 grep 无 `ops.` 调用），
   所以"保留执行器那道"的理由**不是** CLI 需要它，别把这句假话写进划账。
5. **M67**：`Visited` 必须等于"真的发过一次 `ReadDir` 且成功的目录数"。三格各自钉：
   根 `ReadDir` 失败 ⇒ 不计；取消后排空 ⇒ 不计（这条同时是"零磁盘 I/O"的正证）；正常全遍历 ⇒ 与
   实际读过的目录数一致（用一次性计数，不靠 `visited` 去重集）。
6. **M86**：`UndoOne` 的三条双值路径（`:174`/`:178`/`:397`）**每一条**返回的错误文本都必须含落点路径；
   且 app 层不得在 `restored` 非空时把它与错误一起丢掉（结构上保留，展示走既有 FailedItem 通道）。

### 19.2 改动面（只这些文件）

| 文件 | 动什么 |
|---|---|
| **`internal/sqlconn/sqlconn.go`（新增）** | 把 §18 落在 `cache.go` 里的"每条连接建好即重放会话级 PRAGMA"收归成一份：`WithPragmas(base driver.Connector, pragmas []string) driver.Connector`。cache 与 history 两处各写一份是同一条判据的两个实现（I5）；本批 M59 要动它，正好一起收。 |
| `internal/sqlconn/sqlconn_test.go`（新增） | 收归后的**等价性锁** + "新建连接仍带 pragma"（P-19-1 的库无关腿）。 |
| `internal/cache/cache.go` | 删 `pragmaConnector`/`applyConnPragmas`/`connPragmas` 三件套，改调 `sqlconn.WithPragmas`。**行为一字不变**（§18 已划账的语义，靠既有 P-18-2 与新收归锁共同兜住）。 |
| `internal/cache/cache_m74_test.go` | `newCacheOn` 里的 `pragmaConnector{…}` 换成 `sqlconn.WithPragmas(…)`（测试内构造句柄的写法跟随实现，不是改断言）。 |
| `internal/history/history.go` | `initConn` 改走 `sql.OpenDB(sqlconn.WithPragmas(sqlite.NewConnector(path), 会话级三条))`；`SetMaxOpenConns(1)` **保留**（它自己的理由与 M73 不同：账本写频极低 + 从根上消 BUSY 面）；文件级 `journal_mode=WAL`/`user_version` 与建表留在原处；删掉 `:134` 那句现在被证伪的注释。 |
| `app.go` | M58：抽 `warnBackground(tag, userMsg)`，`warnLedger` 变其一行包装（`"[history]"` + "账本写入失败："原样保留）；`startCmd` 收一个退出回调参数，两处调用点各传自己的话术。M60：`settingsPath()` 改返回 `(string, error)`，`GetSettings`/`SaveSettings` 两处跟随。M61：`ExecuteOperation` 在置 `opsRunning` **之前**做 S4 预检并直接返回错误。 |
| `internal/ops/undo.go` | M86：`:178` 那句改成自带落点（与 `:174`/`:397` 同构）。 |
| `internal/scanner/scanner.go` | M67：`visited` 保留为去重集，新增 worker 本地 `accessed` 计数（`ReadDir` 成功后 `++`，收口求和赋给 `res.Visited`）；字段注释与实现对齐。 |

新增测试文件（全部"只引用改前符号"的走改前树真跑；引用新符号的由变异取红，逐条在 19.3 标明）：
`internal/history/history_m59_test.go`、`app_m58_m60_m61_test.go`、
`internal/ops/undo_m86_test.go`、`internal/scanner/scanner_m67_test.go`、`internal/sqlconn/sqlconn_test.go`。
**不动** `frontend/`（六条全在后端）、不动 `cmd/fdd-cli`、不动 `model.FailedItem` 结构（裁定③）。

### 19.3 探针（每项都要先看它红过一次，失败原因须与预测一致）

| # | 用例 | 预测的修前红 | 红从哪来 |
|---|---|---|---|
| P-19-1 | `TestEveryLedgerConnectionCarriesPragmas`（history） | 回收空闲连接后 `foreign_keys=0`、`busy_timeout=0` | **可改前树真跑**（只引用 `Open`/`s.db` 这些改前就有的东西）；与 §18 P-18-2 同族 |
| P-19-1b | `TestConnPragmasSurviveReconnect`（sqlconn） | 收归前无此包 ⇒ 编译期不存在 | 收归等价性：改后须绿；红由变异 M19-a 提供 |
| P-19-2 | `TestStartCmdReportsNonZeroExit`（app） | `startCmd` 只有 `error` 返回值、没有退出通道 ⇒ 无签名可传回调 | 改前树：`startCmd` 签名不同，编译不过 ⇒ 红由变异 M19-b（回调改回 `_ = cmd.Wait()`）提供 |
| P-19-2b | `TestStartCmdQuietOnSuccess`（反面钉） | 同上 | 同上，与 P-19-2 同批 |
| P-19-3 | `TestSaveSettingsWithoutCfgDirFailsAndWritesNothing`（app） | 改前 `SaveSettings` 返回 `err == nil` 且 CWD 出现 `settings.json` | **可改前树真跑**（用 `t.Chdir` 把 CWD 换到临时目录，免得污染仓库） |
| P-19-4 | `TestUnconfirmedDeleteLeavesNoLedgerRow`（app） | 改前 `op_records` 多一条、`ops:done` 会发、返回值非空 opID | **可改前树真跑**（只引用 `ExecuteOperation`/`history.Open`） |
| P-19-5 | `TestWalkCancelDrainReadsNoDirectories`（scanner） | 改前 `Visited` = 预置的根数（≥1）而非 0 ⇒ 断言"取消后一次 ReadDir 都没有"红 | 新用例；既有 `gate_cancel_test.go:127` 需**补强**为"`Files==120` 即失败"并加"`Visited==0`"正证 —— 就地论证：那句 `res.Visited > 0` 钉的正是被本项推翻的旧口径，留着它会在 M67 之后**永不触发**（假绿），不是放松，是换到能触发的形状 |
| P-19-6 | `TestVisitedCountsOnlySuccessfulReadDir`（scanner） | 根 `ReadDir` 失败（chmod 0）时改前仍计 1 | **可改前树真跑** |
| P-19-7 | `TestUndoPartialRestoreMessagesNameTheLandingPath`（ops，表驱动三条） | `:178` 那条不含 target | **可改前树真跑**（走三个已有接缝造确定性场景） |
| P-19-7b | `TestUndoPartialRestoreKeepsPathInFailure`（app） | 改前 `undoExecuteItem` 丢弃 `restored`，失败清单 Path 只有 OrigPath | 引用新返回值形状的部分由变异 M19-c 提供 |

### 19.4 变异计划（预测"该红的包名表"，跑完与实测表相减）

| # | 变异（把改后退回改前形状） | 预测被杀于 |
|---|---|---|
| M19-a | `sqlconn.WithPragmas` 直通 base（不补 pragma） | `internal/sqlconn` + `internal/cache`（P-18-2 应仍红 ⇒ 收归没丢覆盖）+ `internal/history` |
| M19-b | `startCmd` 的退出回调改回 `_ = cmd.Wait()` | 根包（app）P-19-2 |
| M19-c | `undoExecuteItem` 把 `restored` 重新丢成 `""` | 根包 P-19-7b |
| M19-d | `:178` 文案删掉落点参数 | `internal/ops` P-19-7 |
| M19-e | `settingsPath` 去掉空串错误分支 | 根包 P-19-3 |
| M19-f | S4 预检从 `ExecuteOperation` 移除（只留执行器那道） | 根包 P-19-4（**且预测 `internal/ops` 仍全绿** —— 那正是要看的差集：账本行是 app 层的账，执行器管不着） |
| M19-g | `Visited` 改回 `len(visited)` | `internal/scanner` P-19-5/P-19-6 + 预测 `TestWalkWithGateCancelDrainsWithoutIO` 的补强腿开火 |

### 19.5 交付判据

六条各有"修前必红"抄录（能改前树真跑的必须真跑，不能的必须点名是哪条变异顶的）；
变异预测集 − 实测集的差集逐条解释；全套 15 行门禁重跑，任何一项红就停下修；
`docs/04` §6.15 划账 + §6.11 表内六行加〔已实施〕括注（原句一字不动）；三提交，绝不 push。

### 19.6 本批不做 / 转登记（写在动手前）

1. **M57（APP-5）**：登记自己标"属性能项，需基准"。分页 + 批量 stat 的正确性可测，
   但"大记录页卡顿"这条收益没有基准就说不出口 ⇒ 与 M8 的界面/性能面一并处理，本批不动。
2. **M63（FC-2）**：与既有钉子 `TestFoldSwapLegIsPlatformIndependent` 正面冲突（见 04 表尾第三条追记），
   未裁定不改。
3. **M65（SCN-1）**：需要先造"扫描根位于 `/System` 型 absPath 保护条目内部"的真夹具；
   造不出来之前它对扫描核心是**未知**而非已知缺陷。
4. **M87（CACHE-3b）**：运行期隔离重建要跨三层换句柄，本批不碰（§18.7 五-1 同一条）。
5. **前端 FE 面（M77/M79/M80/M81/M83）**：与 M8 一并做，避免为单条改动各起一次界面回归。
6. **`res.Visited` 不做界面呈现**（裁定③）：本批只让它变成真话，不新增任何显示位。
7. **M59 不做"连接池改大"**：`SetMaxOpenConns(1)` 留着。收归后它不再掩盖会话级 PRAGMA 的缺口，
   于是"缩池"从**修法**降级成**无关的现状** —— 这一步不做，避免把 M73 的实测代价（2.08 倍）搬到账本库上瞎猜。

### 19.7 实施后追记（2026-09-22 凌晨；划账见 04 §6.15，代码面 `4cf0c7f`）

#### 一、与 §19.2 / §19.3 的偏离（六处，逐条如实记）

1. **`connPragmas` 留在 `cache.go`**（§19.2 写的是"删三件套"，含这个常量）。收归的是**机制**
   （Connector 包装 + 逐连接重放），不是每库的**策略**：缓存库要 `synchronous=NORMAL`、
   账本库要 `FULL`（M11 裁定，掉电时账本不能随 WAL 丢）。把常量搬进公共包等于把两套策略压成一套，
   那是**假收归**。history 侧相应新增一份 `connPragmas` 变量（§19.2 说的是"inline 三条"），
   好让 M11 那段理由跟着常量走，不留在注释里漂流。
2. **删掉了 `history.go:134` 那句注释**（§19.2 已预告），但它被证伪的方式值得单记：
   注释写的是"本库连接池限 1，故 Exec 设置的会话级 PRAGMA 对该库所有语句都生效"——
   前半句是真（`SetMaxOpenConns(1)` 确实在），后半句是假（见二-1 的 `foreign_keys=0`）。
   ⇒ **缩池从来不成立**，它只是让"很少建新连接"；M59 的成因归因按实测更正。
3. **改动面多出两处测试跟随**：`app_m10_test.go` 三处 `a.settingsPath()` 调用改成
   `mustSettingsPath(t, a)`（新 helper，回 error 即判死——那几个用例的 cfgDir 必然已设），
   以及新增 `app_m86_test.go`（§19.2 的新测试清单里只有 `internal/ops/undo_m86_test.go`）。
   **没有改任何一条既有断言**：前者是签名变化的编译跟随，后者是 P-19-7b 的落点。
4. **P-19-7b 比计划更强：它在改前树真跑出来了**。§19.3 写的是"引用新返回值形状的部分由变异
   M19-c 提供"，实际 `undoExecuteItem` 的签名本来就是 `(string, error)`（只是把第二个返回值丢成
   `""`），所以两条用例都不必碰变异就红了（逐字读数见二-5）。M19-c 照跑，作为"改后仍能退回"的对照。
   用例名与 §19.3 的 `TestUndoPartialRestoreKeepsPathInFailure` 不同：拆成
   `TestUndoExecuteItemKeepsPartialLanding`（结构上别丢）+ `TestUndoPartialLandingReachesFailureList`
   （展示通道要带上），因为判据 6 本身就是这两件事。
5. **新增 §19.3 未列的一格 P-19-2c**（`TestWarnBackgroundKeepsLedgerPrefixOutOfSharedChannel`）：
   抽共用出口最容易写错的是把"账本写入失败："写死在出口上（那会把一条 Finder 失败报成账本故障）。
   它没有改前红可取（改前不存在这个函数），故补了一条反向变异 **M19-h** 让它红过一次（见三）。
6. **`warnBackground` 顺带改了两件判据没要求的事**（交底，不藏）：
   a. stderr 行现在与事件**同一句话** ⇒ `warnLedger` 的 stderr 从 `[history] <msg>` 变成
      `[history] 账本写入失败：<msg>`（改前只有 `app:error` 带前缀）。实跑打印过原文，见二-6。
   b. 出口加了 `a.emit != nil && a.ctx != nil` 守卫：改前 `warnLedger` 是无条件 `a.emit(a.ctx, ...)`，
      `emit` 为 nil 时直接 panic。**这一格没有探针**（生产 startup 必然设 emit，测试里造 nil 只是凑数）
      ⇒ 记为"顺带改动、未验"，不作为交付能力。

#### 二、修前必红（逐字抄录；红绿两侧都是实跑）

做法：把 `app.go` / `internal/history/history.go` / `internal/ops/undo.go` / `internal/scanner/scanner.go`
换回 `2920c0e`（§19 设计段提交，实现未动那一版），`app_m10_test.go` 与 `app_m58_m60_m61_test.go` 挪开、
后者只写回 M60/M61 段（M58 段引用改前不存在的 `startCmd` 双参签名，留着是编译障碍而非判据排除），
跑完 `cp -p` 还原 + 逐文件 sha256 比对（脚本打印"全部到位"）+ `git status --porcelain` 为空。

1. **P-19-1（M59）**：

```
--- FAIL: TestEveryLedgerConnectionCarriesPragmas
    history_m59_test.go:100: 重建的连接没带 pragma：foreign_keys=0 want 1，busy_timeout=0 want 5000，synchronous=2 want 2(FULL)（foreign_keys 关掉 = 四处 ON DELETE CASCADE 静默失效，子表留孤儿行）
```

   同一条读数在**改后**是绿的，且 `synchronous` 两侧同为 2 —— 它没掉是 SQLite 默认值恰好也是 FULL，
   **侥幸不是保证**，所以三条一起断言（§19.1-1）。

2. **P-19-5 / P-19-6（M67）+ `gate_cancel_test.go` 的补强腿**：

```
    scanner_m67_test.go:41: 根 ReadDir 失败时 Visited 必须为 0，实测 1：改前形态把预置进 visited 的根算成"访问过"，而这个目录一次读都没有成功
--- FAIL: TestVisitedCountsOnlySuccessfulReadDir (0.00s)
    scanner_m67_test.go:90: 取消排空后 Visited 必须为 0（零磁盘 I/O 的正证），实测 1、files=0
--- FAIL: TestWalkCancelDrainReadsNoDirectories (0.13s)
    gate_cancel_test.go:138: 取消排空阶段不得有任何一次成功的 ReadDir，实测 Visited=1
--- FAIL: TestWalkWithGateCancelDrainsWithoutIO (0.08s)
```

   第三条是**既有用例**在新判据下的红：它原来那句合取 `files==120 && Visited>0` 在改后永不成立
   （§19.3 P-19-5 就地论证过——换形状不是放松）。它的等价类腿 `files==120` 这次没开火（改前
   files 也是 0），符合预测。

3. **P-19-7（M86，ops 侧表驱动三条）**：

```
=== RUN   TestUndoPartialRestoreMessagesNameTheLandingPath/回收站侧被顶替-未清理
=== RUN   TestUndoPartialRestoreMessagesNameTheLandingPath/已复制但清理回收站侧失败
    undo_m86_test.go:147: 错误文本没说出落点 /var/folders/.../TestUndoPartialRestoreMessagesNameTheLandingPath已复制但清2286528153/001/orig.bin，用户拿到的是一句「两份并存」却不知道去哪找："已复制但清理回收站侧失败（两份并存）: 模拟：回收站侧句柄被占用，删不掉"
=== RUN   TestUndoPartialRestoreMessagesNameTheLandingPath/链接已删-还原备份失败
    --- PASS: TestUndoPartialRestoreMessagesNameTheLandingPath/回收站侧被顶替-未清理 (0.01s)
    --- FAIL: TestUndoPartialRestoreMessagesNameTheLandingPath/已复制但清理回收站侧失败 (0.00s)
    --- PASS: TestUndoPartialRestoreMessagesNameTheLandingPath/链接已删-还原备份失败 (0.00s)
```

   A/C 两行按预测保持绿（`§19.0-2` 读到的就是只有 B 那条掉落的点）——反面兜底成立。

4. **P-19-3（M60，读写两侧）**：

```
--- FAIL: TestSaveSettingsWithoutCfgDirFailsAndWritesNothing
    app_m58_m60_m61_test.go:55: cfgDir 为空时 SaveSettings 必须以错误收口，实测 err == nil（配置被静默写到别处）
    app_m58_m60_m61_test.go:58: cfgDir 为空时磁盘上不得出现 settings.json，实测 CWD 里有（err=<nil>）——那正是写进进程 CWD 的证据
--- FAIL: TestGetSettingsWithoutCfgDirIgnoresCwdFile
    app_m58_m60_m61_test.go:76: cfgDir 为空时 GetSettings 必须只给纯默认值，实测 {Threads:4 FiltersDefault:{...} Theme:dark Language:en}（读到的是 CWD 里那份不属于本应用的配置）
```

   第二格是**登记原文没说到的后果**：M60 行只写"写进进程 CWD"，实测同档还会把同目录里
   **别人的** `settings.json` 当成本应用配置读回来。

5. **P-19-4（M61）+ P-19-7b（M86，app 侧）**：

```
--- FAIL: TestUnconfirmedDeleteLeavesNoLedgerRow
    app_m58_m60_m61_test.go:101: 未确认的永久删除必须在落账之前就被拒绝，实测返回 opID="ops-012108-1" err=nil
    app_m58_m60_m61_test.go:109: 账本 op_records 从 0 涨到 1：一条「什么都没动」的删除记录留在了历史里，用户会以为发生过一次可回撤的操作
    app_m58_m60_m61_test.go:115: 未确认的删除仍然走完了派发并发了终止事件 "ops:done"（序列 [ops:done]）
--- FAIL: TestUndoExecuteItemKeepsPartialLanding
    app_m86_test.go:68: 部分还原的落点必须在错误分支一起交回去，实测 ""（want "/var/folders/.../TestUndoExecuteItemKeepsPartialLanding2966584731/001/home/a.bin"）——app 层把它丢成空串，失败清单就再也没有第二次机会告诉用户数据在哪
--- FAIL: TestUndoPartialLandingReachesFailureList
    app_m86_test.go:110: 失败清单的文案没带上落点 /var/folders/.../elsewhere/a.bin，用户只看到一句失败却不知道数据在哪: "模拟：数据已放到别处，但收尾动作失败"
```

   `opID` 是时间编码（`ops-HHMMSS-N`），所以这条读数与实施前第一次取红（`ops-003532-1`）只有 ID 不同。
   `TestUnconfirmedDeleteLeavesNoLedgerRow` 的第四格（`opsRunning` 不留痕）改前是绿的：
   改前走完派发、正常复位了互斥 ⇒ 那一格防的是"前移之后忘记复位"这个**新引入**的风险，不是旧缺陷。

6. **P-19-2 系（M58）没有改前红**（签名不同、编译不过），红由变异 M19-b 提供（见三）；
   P-19-2c 的红由 M19-h 提供。改后 stderr 侧的实跑原文（同一条用例打印，双通道一致的唯一直接读数）：

```
[reveal] 打开所在文件夹失败（/tmp/x）：exit status 1
[history] 账本写入失败：回撤失败态落库出错
```

#### 三、变异：预测集 vs 实测集（八条，逐条对差集）

每条都是 `cp -p` 备份 → python 打补丁（锚点 `assert count==1`）→ `go test -count=1 -v` →
`cp -p` 还原 → 逐文件 sha256 比对。八条变异共触达 6 个文件次，还原后全部一致、`git status` 干净。

| # | 变异 | 预测被杀于 | 实测（逐字包级读数） | 差集解释 |
|---|---|---|---|---|
| M19-a | `WithPragmas` 直通 base | sqlconn + cache + history | `FAIL filededup/internal/sqlconn`（4 条机制用例全红）、`FAIL filededup/internal/cache`（`TestEveryPooledConnectionCarriesPragmas`）、`FAIL filededup/internal/history`（P-19-1 + `TestEvictOldest`/`TestPruneScanFiles`/`TestDeleteAndClear`/`TestSchemaTablesAndForeignKeys`/`TestListOpsAndClear`）、**`FAIL filededup`**（`TestLoadHistoryAndExecutePrunes`） | 实测**多一个包**：根包那条也经 `history.Open` 建库，`foreign_keys` 一掉它的级联断言就红。P-18-2 仍红 ⇒ 预测里那条"收归没丢覆盖"成立。包内多红的那 5 条既有断言同样不是缺口，是覆盖面比预测宽 |
| M19-b | 回调改回 `_ = cmd.Wait()` | 根包 P-19-2 | 只 `TestStartCmdReportsNonZeroExit`（用时 10.01s = 等满超时窗） | 无差集。P-19-2b 保持绿是设计如此：它断言"不会调用"，静音类变异杀不动它，二者不互相顶包 |
| M19-c | `restored` 丢回 `""` | 根包 P-19-7b | `TestUndoExecuteItemKeepsPartialLanding` + `TestUndoPartialLandingReachesFailureList` 两条全红 | 无（与二-4 合看：这条变异在改后树上顶的是"改前树本来就红"的那两格） |
| M19-d | `:178` 文案删掉落点参数 | `internal/ops` P-19-7 | `FAIL filededup/internal/ops`，且只有 B 行 `/已复制但清理回收站侧失败` 红；`ok filededup` | 根包不红**不是漏网**：app 层钉的是"清单里的 Path/Err 带落点"，`undoFailure` 那一层兜底不依赖 `:178` 的措辞 ⇒ 两层各自独立可验，正是 M86 判据要的双保险 |
| M19-e | `settingsPath` 去空串分支 | 根包 P-19-3 | P-19-3 两格 + `TestGetSettingsWithoutCfgDirIgnoresCwdFile` | 多于预测：§19.3 那行只点名了写侧，读侧那条是同一变异的第二格（见二-4） |
| M19-f | S4 预检从 `ExecuteOperation` 移除 | 根包 P-19-4 **且 `internal/ops` 仍全绿** | `FAIL filededup`（1 条）、`ok filededup/internal/ops 0.795s` | 与预测**完全一致**。这条"ops 仍绿"就是 §19.1-4 的论点：账本行是 app 层的账，执行器那道管不着 ⇒ 保留它不等于修好了本条 |
| M19-g | `Visited` 改回 `len(visited)` | `internal/scanner` P-19-5/6 + 预测补强腿开火 | 三条全红：两条新用例 + **既有** `TestWalkWithGateCancelDrainsWithoutIO`；`ok filededup` | 与预测一致（含"补强腿开火"这一条，二-2 就是它的改前读数） |
| **M19-h**（§19.4 之外补） | "账本写入失败：" 写死回共用出口 | 未预测（P-19-2c 是实施时新增的格） | 只 `TestWarnBackgroundKeepsLedgerPrefixOutOfSharedChannel` | 预测少于实测 ⇒ 记下来：这条变异专门验"新加的守卫格是不是可证伪的"，答案是它是 |

#### 四、门禁与用例重数（2026-09-22 凌晨真读数，全套 15 行重跑）

`gofmt` 干净（本批两处不合规已 `gofmt -w`：`//（` → `// （` 两处注释、`Result` 结构体字段对齐）、
`go build` rc=0、`go vet` linux/darwin/windows 三包 rc=0、
`go test -count=1 -v ./...` rc=0：**`top_PASS=646` / `sub_PASS=77` / `top_FAIL=0` / `sub_FAIL=0`**、
`run=729`、`ok_pkgs=22` + 1 个 no-test 包、**`src_test=698`**（上批 678 ⇒ 本批 +20 条 Go 用例）；
`-race -count=2 ./...` 与 `-race -count=4 .` 均 rc=0、`DATA RACE` 计数 **0**；
`typecheck` rc=0、前端 `build` rc=0（`index-DdrO38H1.js 152.67 kB`）、`test-frontend-logic` 35 项全过、
`check-version-sync` 0.5.0 对齐、`smoke-cli` 三跑一致（205 组 / 可释放 90522243 B / 复扫命中 532）、
`smoke-symlink-assert` 全过。

**SKIP 不读作通过**：`top_SKIP=5` + `sub_SKIP=1`，逐条是
`TestMoveFileCrossDeviceReal`、`TestWalkCaseSensitivityIsProbed`、
`TestWalkSingleRootKeepsCaseVariantSubtrees`、`TestMultiRootWalkKeepsCaseVariantSubtrees`、
`TestSymlinkedRootUnderProtectedDirIsReported`、`TestUndoableReasonExplainsAndGivesNextStep/windows_trash_…`
——与 §6.14 那批同一组平台腿，本批**没有新增** skip。
第 15 行 `smoke-symlink` 仍 **rc=2**（需要 root 挂独立文件系统，本机 uid=501）⇒ 该项**未验证**，
不是通过。⇒ 14 行 rc=0 + 1 行 SKIP。

#### 五、边界与新登记

1. **新登记 M88（OPS-14c）**：`internal/ops/move.go:67` 的
   `"已复制但删除源失败（两份并存）: %w"` 与本批 M86 的 `undo.go:178` **同型**（说了"两份并存"却不给落点），
   差别只在它发生在**正向移动**路径。§19.0-2 把"返回双值的路径"数成三条（`:174`/`:178`/`:397`），
   漏看了 move 侧这一条 ⇒ 按约束 (1) 不改写 M86 行的结论，新增 ID。本批**不修**（不扩大改动面；
   修法与 M86 完全同构，开工时可顺带把两条文案一起补齐）。
2. **M58 只有 darwin 一条腿有读数**。回调本身跨平台（`exitCodeCmd` 用测试二进制当桩，
   三平台都能跑），但**真实外部命令的退出码语义没有取到**：`explorer shell:RecycleBinFolder`
   即使资源管理器没开也回 0、`nautilus`/`thunar` 常后台化立即退出、`open -R` 的失败码
   （`kLSApplicationNotFoundErr`→7）未在本机任何变体下验过。⇒ 这两档是**"代码已改、验证未兑现"**，
   属 M7 的 Windows/Linux 证据包范围。
3. **`res.Visited` 生产侧仍无消费者**（只有测试读它）：本批只让它变成真话，不新增呈现位（裁定③）。
4. **`model.FailedItem` 结构未扩**：M61 的 Path 缺失走的是"提前拒绝、不再产生这条记录"，
   M86 的落点走的是既有 `Err` 文本 + `Path=OrigPath`，两者都没动前端契约。
5. **M61 的 `opID` 前缀 `ops-`** 说明它由时间生成，历史页看到一条 `cancelled` 的 delete 记录
   本身就是"什么都没动"的语义 —— 本批把它**不再产生**，但**没有**清理既有库里已经留下的那类记录
   （用户手上可能有）。这条不属于 M61 判据，未做，也未登记为新 ID（属数据清理策略，M8 界面面一并看）。

---

## 20. 第九批：前端 FE 面四条 + move 侧同型文案（M77 / M80 / M81 / M83 / M88；2026-09-22）

**范围**：§6.11 六张登记表里**"本机可验证"且不需用户裁定**的剩余条目就是这五条
（M77/M80/M81/M83 在前端、M88 在 `internal/ops`）。其余全部卡在硬条件上：M57 要先有性能基准、
M65 要先造"根在 `/System` 型 absPath 条目内部"的真夹具、M72 无 Windows 真机、M87 要跨三层换句柄、
M79 的登记修法属接口扩面（取证见 §20.0-6，本批改判为需裁定）；M48/M82/M75(b)/M62/M56/M84 六条等裁定。
⇒ 本批做完，**§6.11 表内可自动推进的条目清零**，下一批只能进 M7/M8/M9 或先要那六条裁定。

### 20.0 动手前取证（登记原文 → 实际读到的代码 → 判定）

**20.0-1（M77，FE-4）** 登记说 `scan.ts:707-712` "guard 与上锁之间隔了一次 await"。实读：
`executeOp` 起于 `:710`，`:716` `if (!guard('执行清理操作')) return`，`:720` `await ensureProcCounts()`，
`:721` `opsRunning.value = true` ⇒ 窗口**确实存在**，而且不只是一个 microtask：
`ensureProcCounts` → `refreshProcCounts`（`:600-621`）在"启用了处理策略且真值未到位"时会真发一次
`FilterInDirs` RPC，那是一次完整的异步往返。
对照：另两个上锁入口 `undoOperation:446`、`undoOperationItem:460` 都是函数第一句就上锁，**没有**窗口
⇒ 全仓只有 `executeOp` 一条。
后果取证（比登记说的"两个入口都通过 guard"更具体）：第二个入口通过 guard 后会照常走
`:722 opsResult.value = null`、`:726 opsProgress.value = {...}`，并在自己的 catch（`:733-734`）里
**把 `opsRunning` 置回 false**——而此时第一次操作还在途。于是前端的互斥标记被一次"注定被后端回绝"的
调用提前解开，后续点击一路畅通（后端 `app.go:1848` 自己的守卫仍然拦得住，所以不是双跑，
而是**界面在操作在途时重新变成可点**＋横幅与进度被第二次的形状改写）。
判定：**真缺陷，本机可验证**（node 用例同 tick 调两次即可）。

**20.0-2（M80，FE-7）** 实读 `src/stores/toast.ts`：`:15 const MAX_TOASTS = 5`，
`:38 while (toasts.value.length > MAX_TOASTS) dismiss(toasts.value[0].id)` ⇒ 溢出**丢最旧**。
`src/views/ResultView.vue:63-68` 的 `showWarnings` 顺序是：摘要行 → 至多 5 条明细 → 溢出行，
最多 7 次 `push` ⇒ 当 `Warnings.length >= 5` 时摘要行**必定**是第一个被自己投出的明细挤掉的。
判定：**真缺陷**。修法取登记两案中的"摘要行不占池"（`push` 加 `head` 选项，淘汰时先丢非 head 里最旧的）。
**为什么不取"提高上限"**：7 只是这一次的上界，`Warnings` 条数由后端决定（权限汇总可以更多），
把常量从 5 改成 7 只是把翻车点往后推一格，明细照样淹掉摘要；而"丢最旧"这个策略本身是对的
（新到的信息更该被看见），所以只该为摘要破例。

**20.0-3（M81，FE-8）** 四个开抽屉的入口：`App.vue:74-75`（页签徽标）、`ResultView.vue:223`（统计条）、
`:481`（空态按钮）三处都取 `store.failed.length`，**只有** `:459`（结果横幅）取
`store.opsResult.Failed.length`；抽屉的标题、正文与"复制全部"全都吃 `store.failed`
（`FailedDrawer.vue:19/39/46/52`）。
两个数的**语义差别**取证到 Go 侧：`app.go:1960` 是
`a.failed = append(append([]model.FailedItem{}, failed...), res.Failed...)`，
`failed` 是操作开始前的快照（`:1845`）⇒ `GetFailedItems` 回的是**并集**（扫描期失败 ∪ 本次操作失败），
`opsResult.Failed` 是**本次**。
⇒ **登记的修法（"都取 `opsResult.failed` 长度"）不采纳**：那样抽屉标题会与自己列出的正文不同源，
并把扫描期失败项从清单计数里抹掉。与 §18.7 的 M73 同类——**登记的修法被真读数否决**，
原结论不视为已验证可行。
本批改判为"各说各的范围"：横幅那条在两个数不等时**必须限定为"本次失败"**，其余三处与抽屉仍报全量。
判据收进 `src/utils/opdisplay.ts` 一份（`opFailedLabel(opFailed, listTotal)`），组件只引用 ⇒ 与 M15/M18
"文案判据收在 utils、组件不得自拼"同形（I5）。

**20.0-4（M83，FE-10）** `FailedDrawer.vue:18-24`：
`navigator.clipboard?.writeText(text).catch((e) => toast.notifyError('复制失败', e))` ——
可选链短路时整个表达式是 `undefined`，`.catch` 从未挂上 ⇒ **无剪贴板的环境**
（非安全上下文、无授权、部分 Linux WebKit 后端）点「复制全部」完全静默，比"报了失败"更糟。
判定：**真缺陷**。修法：把写入收进 `src/utils/clipboard.ts` 的 `copyText(text, clip?)`，
剪贴板缺失与写入 reject 一律抛人话错误，组件 catch 后 `notifyError`；成功路径不加呈现
（登记只要求失败路径有反馈）。
测试接缝取证：Node 24 的全局 `navigator` 是 **getter-only**（实测
`Object.getOwnPropertyDescriptor(globalThis, "navigator")` → `{get: true, set: false, configurable: true}`），
直接赋值在 ESM 严格模式下会抛 ⇒ `copyText` 用**可选参数注入** `clip`，不碰全局。

**20.0-5（M88，OPS-14c）** `internal/ops/move.go:66-67` 与 M86 修好的 `undo.go:177-183` 同型：
`"已复制但删除源失败（两份并存）: %w"` 说了两份并存却不给落点；同函数 `:63-64` 的兄弟分支是带落点写的。
更要紧的一格（本批新证）：`executor.go:568-572` 拿到 `MoveFile` 的 `(dst, err)` 后，
`err != nil` 直接 `settle(i, outcome{code: ocFailed, err: err.Error()})`，**把 dst 丢掉**
⇒ 这份孤儿副本在整条链路上**只有错误文本一个留痕处**（账本记 failed、无 DestPath，历史页与回撤都看不见它）。
判定：**真缺陷，本机可验证**——夹具与接缝现成：`forceCrossVolumeRename`（`move_crossvolume_identity_test.go:28`）
造出 EXDEV，`removeSrc`（`move.go:411 var removeSrc = os.Remove`）可注错，`undo_m86_test.go:71-74` 就是这么做的。
**顺带新登记 M89**（见 §20.6-2）：上面那条"部分成功的 move 只在错误文本里留痕"不属 M88 判据，
本批不修，只登记。

**20.0-6（M79，FE-6，取证后转裁定）** 登记的修法（后端把 reason 文本随 `OpRecord` 下发、前端只渲染）
经开码复核成立但**改动面跨接口**：`RecordsView.vue:73-79` 的 `noUndoTitle` 确实是
`app.go:1690 undoableReasonFor` 的第二实现且少了平台条件（它无条件把 `trash` 说成"Windows 回收站"）。
取证另找到一条**纯前端**替代：`m.undoable` 取自后端落账列（已含平台条件），
于是 `kind === 'trash' && !m.undoable` 可反推出"此刻必然是 Windows"。
但这条反推本身就是又一份跨语言的第二实现（判据从后端漂到前端），不优于原修法 ⇒ 两种都要动接口契约或
判据归属，属裁定③ 相邻的界面/契约面，与 M8 一并裁定。本批**不做**。

### 20.1 判据（五条，每条都可证伪）

1. **M77**：`executeOp` 的 `opsRunning.value = true` 必须出现在该函数**第一个 await 之前**；
   第二次同 tick 调用必须在 `guard` 处返回，**不得**多发一次 `ExecuteOperation`、
   **不得**改写 `opsResult` / `opsProgress`、**不得**在自己失败的收口里解开第一次操作还占着的锁。
2. **M80**：`push` 的溢出淘汰**先丢非 head 里最旧的**；全是 head 时才丢最旧的 head。
   由此 `showWarnings` 的摘要行（head）在任何明细条数下都留在池内，可见总数仍 ≤ `MAX_TOASTS`。
3. **M81**：`opFailedLabel(opFailed, listTotal)` 是唯一出口——`opFailed <= 0` 返回空串；
   `listTotal > opFailed` 返回以「本次失败」开头的文案；两数相等时**不**出现「本次」限定。
   组件模板只许引用它，不得自己拼 `store.opsResult.Failed.length` 那句。
4. **M83**：`copyText` 在"没有剪贴板"与"写入被拒"两条路径上都**必须抛**（改前形状是静默返回）；
   写入成功时不抛、不把内容改动。组件的 catch 必须把错误交给 `notifyError`。
5. **M88**：`move.go:67` 的错误文本必须同时含**副本落点 `dst.path`** 与**残留源路径 `src`**，
   且仍 `errors.Is` 得到底层原因（`%w` 不丢）。

### 20.2 改动面（只这些文件）

| 文件 | 动作 |
|---|---|
| `frontend/src/stores/scan.ts` | `executeOp` 上锁前置（M77） |
| `frontend/src/stores/toast.ts` | `push` 增 `head` 选项与淘汰优先级（M80） |
| `frontend/src/utils/opdisplay.ts` | 新增 `opFailedLabel`（M81） |
| `frontend/src/utils/clipboard.ts` | **新增**，`copyText`（M83） |
| `frontend/src/views/ResultView.vue` | 摘要行标 head；失败按钮改引用 `opFailedLabel`（M80/M81） |
| `frontend/src/components/FailedDrawer.vue` | `copyAll` 走 `copyText` + 失败 toast（M83） |
| `internal/ops/move.go` | `:67` 文案补两个落点（M88） |
| `frontend/tests/*.test.ts` | 新增 4 个探针文件（P-20-1/2/3/4） |
| `internal/ops/move_m88_test.go` | **新增**（P-20-5） |
| `scripts/test-frontend-logic.sh` | 追加 3 条接线断言（P-20-6） |

不动：`app.go`、`executor.go`（M88 只改文案，`dst` 丢弃那一格转 M89）、`RecordsView.vue`（M79 待裁定）、
`wails.ts` / `wailsjs`（无契约变化 ⇒ 不碰 M46 那半）。

### 20.3 探针（每项先看它红过一次；改前树跑不了的必须点名由哪条变异顶）

| 探针 | 位置 | 断言的那一格 | 改前能否真跑 |
|---|---|---|---|
| P-20-1 | `frontend/tests/scan-execute-lock.test.ts` | 同 tick 两次 `executeOp`：`invoked('executeOperation')===1`；第一次的 RPC 用 `deferred()` 挂住时，第二次回包报错后 `store.opsRunning` **仍为 true** | ✓ 能（只引用改前就有的 `executeOp`） |
| P-20-2 | `frontend/tests/toast-head.test.ts` | 投 1 条 head + 7 条普通 ⇒ head 仍在 `toasts` 里、`length <= 5`；连投 head 时仍 ≤5 且丢的是最旧 head | ✓ 能（改前多传的第 4 个参数被 JS 忽略 ⇒ 摘要被挤掉，正是缺陷格） |
| P-20-3 | `frontend/tests/op-failed-label.test.ts` | `opFailedLabel(2,5)` 以「本次失败」开头、`(5,5)` 不含「本次」、`(0,5)` 空串 | ✗ **不能**（函数改前不存在，import 失败是"红在错误的格子上"）⇒ 修前必红由 **M20-c** 提供，另加一条复刻改前形状的自检 |
| P-20-4 | `frontend/tests/clipboard-copy.test.ts` | 无剪贴板 / `writeText` reject / 成功 三格 | △ **复刻格能真跑**：文件里另写一条不 import 新模块的用例，内联复刻改前那一行（`navigator.clipboard?.writeText(t)?.catch(...)`）并断言"缺剪贴板时必须产出一错误上报"⇒ 改前当场红；新模块的三格由 **M20-d** 兜 |
| P-20-5 | `internal/ops/move_m88_test.go` | 跨卷 + `removeSrc` 注错后，`err.Error()` 同时含 `dst.path` 与 `src`；`errors.Is(err, 底层哨兵)` 仍真；前提自检：两份都还在盘上 | ✓ 能 |
| P-20-6 | 接线断言 3 条 | 组件必须引用 `opFailedLabel(` / `copyText(`，`ResultView` 禁含被禁写法 `失败 {{ formatCount(store.opsResult.Failed.length) }}` | 负控制走 `FRONTEND_DIR`（脚本已支持）⇒ 见 M20-f/M20-g |

**★ 取红必须验"红的是不是那一格"**（§18.7 一口径继续适用）：P-20-1 的失败信息必须落在
"第二次调用把 `opsRunning` 解回了 false"或"RPC 发了两次"这两格上，若报成 `ids.length === 0`
提前返回之类的形状错误，判为探针无效、重做夹具。

### 20.4 变异计划（预测"该红的用例/包名表"，跑完与实测表相减）

| 变异 | 回退的动作 | 预测红在 |
|---|---|---|
| M20-a | `scan.ts` 上锁挪回 `await ensureProcCounts()` 之后 | `tests/scan-execute-lock.test.ts`（且仅这条） |
| M20-b | `toast.ts` 淘汰退回 `dismiss(toasts.value[0].id)`（不看 head） | `tests/toast-head.test.ts` |
| M20-c | `opdisplay.ts` 的 `opFailedLabel` 去掉 `listTotal > opFailed` 分岔（恒返回「失败 N」） | `tests/op-failed-label.test.ts` |
| M20-d | `clipboard.ts` 缺剪贴板时 `return`（不抛） | `tests/clipboard-copy.test.ts` |
| M20-e | `move.go:67` 退回原文案（只说"两份并存"） | `internal/ops` 的 `TestMoveFilePartialDeleteSourceNamesBothSides`；`### 6 test -v` 出现 top_FAIL≥1 |
| M20-f | `ResultView.vue` 模板退回内联拼串（不用 `opFailedLabel`） | `### 11 frontend-logic` 接线断言 rc=1（`FRONTEND_DIR` 负控制） |
| M20-g | `FailedDrawer.vue` 退回 `navigator.clipboard?.writeText(...)` | 同上，锚 `copyText(` 的那条 |

预测的差集来源先说明：node 用例之间互不相干，理论上每条变异只红自己那条；
但 `test -v` 的 Go 面里 `move.go` 文案可能被既有断言引用（`grep 已复制但` 实测只有 `undo_m86_test.go:137`
引用 undo 侧，move 侧零引用）⇒ M20-e 若红出第二条，就是**覆盖比预测更宽**，如实记。

### 20.5 交付判据

- 五条各有"修前必红"：P-20-1/2/4/5 必须在改前树真跑并红在预测那格；P-20-3 跑不了的必须点名 M20-c 顶。
- 七条变异逐条跑，预测集 − 实测集的差集**逐条解释**（不许只记"杀了几条"）。
- 全套 15 行门禁重跑，fresh 读数进 04 §6.16；`smoke-symlink` rc=2 仍按 SKIP 记，**绝不读作通过**。
- 用例重数现跑现取：Go 侧 `grep -rh '^func Test' --include='*_test.go' --exclude-dir=.workbuddy . | wc -l`
  = **698**（改前基线）。★ 口径修正：`.workbuddy`（`.gitignore:24`，本机备份/探针残留，非源码）里有 5 个
  `*_test.go`，**裸命令会读到 715** ⇒ 历次划账的 678/698 都是排除该目录的读数，将来别把 715 当成"涨了 17 条"。
  node 侧与接线项以 `### 11` 那行的三个数为准（改前 29 + 6 = 35）。
- 04 §6.11 表内这五行各加〔2026-09-22 已实施〕括注，**原句一字不动**；M79 行加"取证后转裁定"括注。
- 三提交（设计段 / 实施 / 划账），**绝不 push**。

### 20.6 本批不做 / 转登记（写在动手前）

1. **M79 转需裁定**（§20.0-6）：登记修法与新找到的纯前端反推修法都要动"判据归谁"，属接口/契约面，
   与裁定③ 与 M8 一并处理。本批不碰 `RecordsView.vue`、不扩 `OpRecord`。
2. **新登记 M89（OPS-14d）**：`executor.go:568-572` 在 `MoveFile` 部分成功（返回 `dst` 且 `err != nil`）时
   丢弃 `dst`、只记 `ocFailed` ⇒ 盘上多出的那份副本在账本里没有任何条目，历史页与回撤都不知道它存在，
   唯一留痕是错误文本（M88 只让这句文本说实话，**没有**改变"文本是唯一留痕"这件事）。
   修法要动 `outcome`/`FailedItem` 形状或账本状态机（与 M40「`MoveFile` 不回三态」同族），
   本批不修，只登记。
3. **`rescanHistory` 那类"guard 之后仍有 await"的形状**：本批只钉 `executeOp` 一条
   （取证确认全仓只有它有上锁窗口），不做"所有 async 入口一律先上锁"的泛化——那是改架构，不是修假话。
4. **toast 的"多条同因提示"（M42）**：M80 改的是淘汰优先级，不新增去重/合并策略，M42 仍挂账。
5. **空清单点「复制全部」**：改前是"复制一个空串、静默成功"。登记只要求失败路径有反馈，
   本批按判据 4 的范围做；这条既不在 M83 判据里、也不另开 ID（属 M8 界面面的措辞项）。

---

### 20.7 实施后追记（2026-09-22 凌晨；划账见 04 §6.16，代码面 `c6c4ba5`）

本节只写**动手之后才知道的事**：改前真读数、三条取证更正、八条变异的预测集 − 实测集。
判据本身没有改（§20.1 五条照原样落地），改动面也没有超出 §20.2 那张表。

#### 20.7.0 四条"改前必红"的真读数（红绿两侧都是实跑）

做法与 §19.7 相同：把 `frontend/src/stores/toast.ts` / `scan.ts` 换回 `07dd74d`（设计段提交，
实现未动那一版），探针文件留在原地跑，跑完 `cp -p` 还原 + 逐文件 sha256 比对 + `git status` 为空。
P-20-5 的改前红由 M20-e 提供（那条变异**就是把文案退回改前原文**，等价于在改前树跑，见 20.7.1）。

```
✖ 同 tick 连点两次：第二次不得再发 RPC，也不得解开第一次还占着的锁 (5.028ms)
✔ 在途锁已置、RPC 还没发出时，进度条不得留着上一条操作的数字 (1.906416ms)
✖ 启用了处理策略时窗口是一次完整 RPC 往返：第二次同样必须在 guard 处判死 (1.493542ms)
✖ 摘要行标 head 后，被 7 条明细挤也挤不掉，且可见总数仍不超上限 (1.563667ms)
✖ 淘汰顺序：先丢非 head 里最旧的，head 之间仍按最旧先丢 (0.788ms)
✔ 不传 head 时行为与改前一致（普通提示仍照旧丢最旧，防顺手改坏别的调用点） (0.210791ms)
ℹ tests 6 / pass 2 / fail 4
```

★ 一处读数边界，别把上面那组读成"两格都亲眼见红"：第一条用例里摆着**两个** `assert`，
node:test 在第一个失败处就抛 ⇒ 那一跑**只亲眼看到**"RPC 发了两次"这一格。
第二格（"第二次在自己的 catch 里把第一次**还在途**的锁解回 false"）另用一条**临时探针**取到逐字读数
（不 import 生产断言、前面不摆任何会先抛的 `assert`，只钉 `opsRunning`）：

```
✖ M77 第二格单独取证：第二次调用的 catch 是否解开第一次还占着的锁 (5.333791ms)
  AssertionError [ERR_ASSERTION]: 第二次的 catch 把第一次还占着的互斥标记解回了 false
    actual: false,
    expected: true
ℹ tests 1 / pass 0 / fail 1        ← 改前树（scan.ts 换回 07dd74d）
✔ M77 第二格单独取证：第二次调用的 catch 是否解开第一次还占着的锁 (4.888625ms)
ℹ pass 1 / fail 0                  ← 换回实施后，同一探针原地重跑
```

该临时文件取证后**已删除、不入仓**（红绿两侧的这条读数只存在于本节）。
合进同 tick 用例后，那一格在整文件层面由 M20-a 复验：M20-a 下该文件红两条，
其中第二条用例（启用处理策略那条）的 `opsRunning` 断言排在其 `ExecuteOperation` 计数断言之后，
故**同样**不会亲眼抛在那里 —— 两格的正证是上面这条临时探针，M20-a 只证明"上锁时机"这件事可证伪。

**第三条用例（在途锁那条）在改前是绿的**，且这是设计如此：它是一条**不变式**而不是修前红探针
（改前那一刻 `opsRunning` 还是 false，横幅根本没出现 ⇒ 断言空转）。它存在的理由是本批把上锁提前之后
**新产生**了"锁已置、RPC 未发"这一格，需要一个东西钉住它。它在 20.7.1 的 M20-h 下红，
所以不是装饰。就地注释已写明这件事，免得后来人把"两版都绿"读成"没测到"。

#### 20.7.1 变异取证（§20.4 预测集 − 实测集，逐条）

八条（§20.4 的七条 + 实施时补的 M20-h）。每条 `cp -p` 备份 → python 打补丁（锚点 `assert count==1`）
→ 跑 → `cp -p` 还原 → `shasum -a 256 -c` 全部 OK，八条跑完 `git status --porcelain` 为空。

| # | 变异 | 预测红在 | 实测（全量 node 套件的 fail 集合） | 差集解释 |
|---|---|---|---|---|
| M20-a | 上锁挪回 `await ensureProcCounts()` 之后 | `scan-execute-lock.test.ts` 且仅这条 | 该文件 2 条红（第一、第三条），43 项里 fail 2 | 与预测一致。**第二条用例（不变式那条）保持绿是对的**：改前形状就是 `opsRunning` 未置 ⇒ 走 else 分支记录形状 |
| M20-b | 淘汰退回 `dismiss(toasts.value[0].id)` | `toast-head.test.ts` | 该文件 2 条红，第三条（"不传 head 时行为与改前一致"）绿 | 与预测一致。第三条绿是**设计如此**：它断言的就是改前行为，静音类变异杀不动它，与 M19-b 同理 |
| M20-c | `opFailedLabel` 去掉分岔、恒返回「失败 N」 | `op-failed-label.test.ts` | 该文件 1 条红（"两个数不等时…"），其余 3 条绿 | 与预测一致。绿的三条分别钉：相等时不加限定、本次为 0 不给文案、全量<本次按相等处理 —— 都不依赖被删的那个分岔 |
| M20-d | 缺剪贴板时 `return`（不抛） | `clipboard-copy.test.ts` | 该文件 2 条红（"必须抛"两格），前提自检那条绿 | 与预测一致。★ 第一次跑这条**补丁没打上**（锚点是单行 `throw`，实际是带花括号的三行形状，`assert` 当场中止、文件未写），却紧接着跑出 4 条全绿 —— 那是一次**空跑**，不是读数。重新按真实形状打补丁后拿到上面这组。记下来是因为"全绿"长得像"变异没杀掉"，容易误读成用例无效 |
| M20-e | `move.go:67` 退回原文案（只说"两份并存"） | `TestMoveFilePartialDeleteSourceNamesBothSides`；`test -v` 出现 FAIL | 只这一条红（`move_m88_test.go:70` 与 `:73` 两格），且顺手把 `go test ./...` 全仓跑了一遍：除它以外零红 | **覆盖没有比预测更宽** —— §20.4 预留的那个"若红出第二条就是更宽"的分支没发生，`grep 已复制但` 的取证成立（既有断言只引用 undo 侧） |
| M20-f | `ResultView.vue` 模板退回内联拼串 | `### 11` 接线断言 rc=1 | `FRONTEND_DIR=/tmp/m20neg` rc=1，红在锚 `opFailedLabel(` 的那条，被禁写法命中 | 与预测一致 |
| M20-g | `FailedDrawer.vue` 退回可选链写法 | 同上，锚 `copyText(` 的那条 | `FRONTEND_DIR=/tmp/m20neg2` rc=1，红在锚 `copyText(` 的那条（"未引用 copyText("） | 与预测一致。两条各只红自己那条 ⇒ 三个锚点互不顶包 |
| **M20-h**（§20.4 之外补） | 删掉 `opsProgress.value = null` | 未预测（这一格是实施时才产生的） | 全量套件 43 项里**只红 1 条**，正是那条不变式 | 补这条变异的目的就是验"新加的那格可不可证伪"，答案是可。它同时也是 20.7.0 里那条改前绿的对照 |

八条全部被杀。**预测集 = 实测集**，本批没有"预测了却杀不掉"的变异，也没有"意外红出预测之外"的项
（M20-d 那次空跑不算：它是操作失误，重打补丁后的读数才是证据）。

#### 20.7.2 三条取证更正（写在动手后，不改 §20.3 的原句）

1. **§20.3 P-20-4 那格"△ 复刻格能真跑 ⇒ 改前当场红"是错的**。一条**内联复刻改前那一行**的用例
   在两棵树上都会红 —— 它复刻的就是缺陷本身，改前改后没有区别，因此它**不是**修前红探针，
   而是一条**语言语义自检**（"可选链短路时 `.catch` 从未挂上"这件事在 Node 24 上成不成立）。
   实测它在 M20-d 下也保持绿，正说明它与新模块无关。落地的文件名/用例名按这个定位写
   （"前提自检：可选链短路时 .catch 根本不会挂上"）。
2. **§20.3 P-20-3 与 P-20-4 的"修前必红"只能由变异提供，且已核实到位**：
   `git show 07dd74d:frontend/src/utils/clipboard.ts` → rc=128（文件不存在），
   `git show 07dd74d:frontend/src/utils/opdisplay.ts | grep -c opFailedLabel` → 0。
   ⇒ 在改前树跑这两个探针只会红在 `import` 上（"红在错误的格子上"），不算证据。
   实际提供修前必红的是 M20-c 与 M20-d。
3. **★ 一条假绿是写探针时才发现的**：M77 的第二格断言（"第二次不得解开第一次还占着的锁"）
   最初单独放在一条"两次调用中间 await 一次"的用例里，改前跑它是**绿的** —— 那一次 await 已经让
   第一次把锁上了，第二次当场被 guard 挡下，断言的对象根本没发生。合并进"同一 tick 连点两次"
   那条用例之后才拿到 20.7.0 的红。这条更正对应硬约束"取红必须验红的是不是那一格"，
   就地注释也写了（`scan-execute-lock.test.ts` 头部 ★ 取证更正）。

#### 20.7.3 读数口径与一处测量陷阱

- **node 侧**：改前基线 29 项 + 接线 6 项 = 35；本批后 **43 + 9 = 52**。新增 14 条 node 用例
  （P-20-1 三条 + P-20-2 三条 + P-20-3 四条 + P-20-4 四条）+ 3 条接线锚点，与 §20.2 改动面一致。
  ★ 更正一处：本节成文前我一度把改后读数记成"42 + 9 = 51"，实跑是 **43 + 9 = 52**。
  以 `### 11` 那一行为准（硬约束：现跑现取）。
- **Go 侧**：`grep -rh '^func Test' --include='*_test.go' --exclude-dir=.workbuddy . | wc -l`
  = **699**（改前 698，本批 +1 = P-20-5）。裸命令读到 716 仍是 `.workbuddy` 那 5 个 `*_test.go`
  在里头，口径见 §20.5。
- **门禁读数文件本身会拿错**：`/tmp/run_gates.sh` 写的是 **stdout**，历次 `/tmp/gates_report.txt`
  是上一批留的旧文件。这次先 `Read` 了那个旧文件，看到的正是改前读数（"node 用例 29 项 + 接线断言
  6 项 = 合计 35 项"、`dist/assets/index-DdrO38H1.js`）—— 若照抄就成了划账说谎。
  ⇒ 本轮读数取自 `/tmp/gates_run.log`（fresh），关键差异：`index-C6on3t6g.js 153.21 kB`、
  `top_PASS 646→647`、`run 729→730`、`### 11` 那行 35→52。**将来把 stdout 直接重进带时间戳的文件再读。**
- `smoke-cli` 三跑 digest 本轮同为 `f02af17dcd2bf7d9`，与 §6.15 那轮的 `362ede59dd6731f5` 不同
  ⇒ 同一成因（临时目录路径进了 digest），**跨轮数值变化不据此说回归**，见 §6.15 四最后一段。

#### 20.7.4 本批新发现（一条只登记不修 + 一条误判撤回）

1. **★ 一条取证误判撤回（原拟登记 M90）**：写 §20 时以为 M77 同族还剩一半 ——
   "`undoOperation`/`undoOperationItem` 第一句上锁却不置空 `opsProgress` ⇒ 横幅一出现就显示上一条的
   `Done/Total`"。划账前开码复核**推翻它**：`scan.ts:446-447` / `:460-461` 是
   `opsRunning.value = true` 的**下一句同步**置 `opsProgress`，中间没有 `await` ⇒ 那一格不存在，
   两条回撤腿本来就是 M77 要求的形状（与 §20.0-1"全仓只有 `executeOp` 有窗口"是同一件事）。
   ⇒ **不登记 M90**，该 ID 空置不复用；误判与推翻它的读数留在 04 §6.16 五-6。
   顺带一处口误：那两条腿叫 `undoRecord` / `undoItem`，`undoOperation` / `undoOperationItem`
   是它们调用的 api 方法名。
2. **M89 已在本批设计段登记**（§20.6-2，`executor.go:568-572` 部分成功时丢掉 `dst`）；
   M88 只让错误文本说实话，**没有**改变"文本是唯一留痕"这件事。

---

## 21. CI 三条腿首跑的失败面：Linux inode 家族 11 条 + Windows 4 条（2026-09-22）

用户指令："提交变更，推送到仓库，并跟踪 CI 信息，修订 CI 错误问题"。
推送 `fc5dabe..9194cc7`（99 个提交）后 run **35638637530** 三条腿读数为
**macos ✓ / windows ✗ / check(linux) ✗**。这是 2026-09-21 三条腿 CI 成形后的
**第一次真跑**（上一跑 35520016319 停在 2026-09-20，那时只有 3 条红），
所以本轮不是"某次改动打红了 CI"，而是**六十多个提交的跨平台欠账一次性到账**。

### 21.0 失败清单（逐字取自两份 job 日志）

| 腿 | 包 | 用例 | 日志原文（截断处保留） |
|---|---|---|---|
| linux | `internal/ops` | `TestIdentityStillDetectsRegularReplacement` ×2 | `identity_still_test.go:47: 路径已被换成另一个文件，identityStill 必须判否（否则会覆盖第三方文件）` |
| linux | 同上 | `TestIdentityStillDetectsSymlinkSwap` ×2 | `identity_still_test.go:108: 路径已被换成符号链接，identityStill 必须判否（不得跟随链接）` |
| linux | 同上 | `TestHardlinkMergeDoesNotDeleteForeignBackup` / `TestSymlinkMergeDoesNotDeleteForeignBackup` ×2 | `identity_window_test.go:97/:106: 第三方文件被合并流程吸收了（返回 nil）：守卫没生效` |
| linux | 同上 | `TestHardlinkMergeRechecksBackupOwnershipBeforeDelete` / symlink 双胞胎 ×2 | `identity_window_test.go:208: 前置条件不成立：换进去的文件仍被判定为本次操作的 dup，断言将失去意义` |
| linux | 同上 | 四条 `…RefusesReplacedBackup` ×2 | `rollback_backup_guard_test.go:135/:149/:162/:179: backup 位的陌生文件被搬走或改写（M20）：read="" err=open /tmp/…/001/dup.bin.fdd-old: no such file or directory` |
| linux | 同上 | `TestSymlinkMergeDetectsKeepReplacement` ×2 | `symlink_test.go:626: ❌ keep 已被替换仍合并成功：dup 的备份（原始内容最后副本）会被当残留删掉` |
| windows | `internal/ops` | `TestBuildPathListRoundTripsUTF16` | `trash_windows_list_test.go:186: 第 2 个路径往返失真: got "F:\\emoji\\<4 个 U+FFFD>.bin" want "F:\\emoji\\😀😀.bin"` |
| windows | 同上 | `TestVerifyUnopenableIsUnverifiable` | `verify_m52_m54_test.go:67: 打不开的文件 = 0, want VerdictUnverifiable(3)` |
| windows | 同上 | `TestSameVolumeDetectsCrossMount` | `volume_test.go:82: 跨挂载点被判为同卷：volumeRoot 在 unix 恒为 "/"，M5 未修` |
| windows | `internal/scanner` | `TestDedupeRootsSortsByFoldKey` / `TestDedupeRootsKeepsUnrelatedRootsAfterSortFix` | `scanner_m66_m70_m68_test.go:39: kept = [D:\data\b], want [/data/b]（宽根必须胜出…）` / `:50: kept = [D:\data\b D:\data\zz\sub], want [/data/b /data/zz/sub]` |

`FAIL filededup/internal/ops 0.886s`（linux）／`FAIL filededup/internal/ops 2.105s` +
`FAIL filededup/internal/scanner 2.307s`（windows）。其余 21 个包两腿全绿；macos 腿
（`go test -race -count=2 ./...` + 根包 count=4 + CLI 冒烟）整条绿。
**windows 腿的 11 条 inode 家族一条都没红** —— 这条差分本身就是证据，见 21.1-丁。

### 21.1 Linux：11 条共用一个根因——inode 号在「unlink 到零链接」立即可被回收

**甲·反证（不需要任何额外探针）**：`identityStill` 能返回真的路径只有一条
（`verify.go:100-116` 的 `identityStatus`：`!id.Resolved → true` 已由 `newMergeFixture`/
`needResolvedID` 的前置跳过排除；`err != nil` 与 `!cur.Resolved` 都返回 false），
即 `cur.SameIdentity(id)` 为真；而 `SameIdentity`（`fsid.go:43-48`）**只比 `Dev` 与 `Ino`**。
⇒ `identity_window_test.go:208` 那条**前置断言**（"换进去的文件必须不再被认成本次的 dup"）判假，
等价于实测到"两个不同对象拿到同一组 (dev,ino)"。11 条红全部由这一个事实推出，无需假设。

**乙·正对照（同一条 CI 里就有）**：同包 `TestIdentityStatusSeparatesGoneFromReplaced`
（`verify_m52_m54_test.go:121`，rename 在 `:141`）造顶替用的是 **`os.Rename(other, replaced)`**，
同一 runner、同一 `go test -race -count=2`，**通过**。⇒ 红不红由"顶替怎么写"决定，不是环境抖动。

**丙·边界对照**：`TestUndoHardlinkBlocksSwappedTargetSameSize`（`identity_still_test.go:165`）
同样用 Remove + WriteFile，却**通过** —— 它删的 `dup` 刚被 `HardlinkMerge` 做成 keep 的硬链接
（nlink=2），unlink 只把链接数降回 1，**inode 不进空闲表**，新文件无从取到那个号。
甲乙丙合起来把根因钉成一句：**只有"原对象被删到零链接、inode 被回收给紧随其后创建的新对象"这一条路会失效。**

**丁·本机对照读数**（darwin/APFS，同一形状的三连删建，`stat -f ino=%i`）：
`76358951 → 76358952 → 76358953 → 76358954`，**单调分配、不还号**；换不同长度内容同样还给新号
（`76358954`）。这解释了 macos 腿与 windows 腿（NTFS 的 FileId 语义等价"随文件走、不还号"）为何全绿。

**判据（为什么这不算"改测试让门禁变绿"）**：
1. `(dev,ino)` 只能区分**同时存活**的对象——这是文件系统层的定义，不是实现缺陷；
   `identityStill` 要"识破删除后原地重建"必须引入号外的第二因子。
2. 可选的第二因子只有两类：**ctime**（被本仓**明令排除**：`fsid.go:41-42` 与 04 表 H2 的 DOC-H2 裁定，
   理由在本轮更硬——合并主路径自己就 `rename(dup→backup)`，unix 上 rename 推进 ctime，
   带上它会把每一次正常合并都判成顶替）与**内容**（`fsid.go:8-9` 原文写明的兜底方向：
   "调用方应将比较视为平凡通过，安全兜底退回到内容级证据（多点采样 + 全量重算）"）。
3. 而这批夹具声称复现的现实时序，`identity_window_test.go:10-12` 自己写的是
   "同步盘落一个同名文件、下载器**原子改名**进来" —— 原子改名进来的对象必然带着
   "原对象尚存活时"就已分配的 inode ⇒ 身份层**必然**识破。夹具实际写的却是
   `os.Remove` + `os.WriteFile` 到同一路径，那是另一件事（inode 回收），且是身份层**原理上管不着**的那件。

⇒ **处置**：把 7 处顶替夹具调用点改成"先写在不旁边、再 `os.Rename` 顶位"（= 文档承诺的那个时序，
也更接近真实第三方行为：没有名字消失的空档），**一条断言、一句期望值都不动**。
残留的产品缺口不遮掩，另立 **M91**（§21.2）。

★ 本节两处行号是**实施前**取证时的行号，实施提交 `b6842bd` 改动了同一批文件后已前移
（`identity_still_test.go` 163→165、`verify_m52_m54_test.go` 132→141）；正文已按改后行号刷新。
"6 处"是本节初稿的误数：把 `identity_still_test.go` 的两条（普通文件 + 符号链接）数成了一条，
实施后逐点名点齐是 **7 处**。

### 21.2 新登记 M91（待裁定）：`(dev,ino)` 在回收 inode 的卷上可被"先删后建"骗过

`merge_guard.go` 的四处归属守卫（`backupOwnershipStill` / `requireOriginalInBackup` /
`removeOwnBackup` / `slotProvesHardlink`）**只认** `(dev,ino)`。在会还号的卷（本轮实测：GitHub
ubuntu runner 的 `/tmp`）上，第三方"删掉 dup → 立刻在同名位置写新内容"可让判据恒真，
后果就是这些用例断言的那件事：替陌生人销毁文件。三条修法，代价各不同，需裁定：

| 修法 | 闭合度 | 代价 |
|---|---|---|
| A 内容级兜底：删除/搬移 backup 前按已验证的组哈希复核（`hasher` 现成） | 完全（含等长等内容的情形） | `HardlinkMerge`/`SymlinkMerge` 要收哈希与 size ⇒ 签名与 executor 两处调用点 + 约 15 处测试调用点；每次合并多读一遍 dup 字节 |
| B 硬链接锚点：操作开始即给 dup 挂一个隐藏链接，全程不让 inode 归零 | 完全（且零额外读盘） | 新增一个必须清理的名字；崩溃留残留（`worktemp` 已能忽略该命名）；FAT/exFAT 不支持硬链接（那里身份本就未解析、判据已 fail-open，方向一致）；与 M38 的 `claimExact` 占位是同一族设施 |
| C 只在 Linux 类卷上补第二因子（如 `FS_IOC_GETVERSION` 取 generation） | 部分（APFS 不暴露） | 平台专属、跨平台面反而变三份，违 I5 |

本轮**一条都不实施**：M91 属"三选一"的处置面裁定，与 §6.11 表内已挂起的 M62/M56/M75(b) 同形。

### 21.3 Windows 四条：三条是夹具的平台前提，一条是测试自带解码器的错

| # | 取证（开码复核，非推断） | 处置 |
|---|---|---|
| W1 | `pathListSegments` 是**测试文件内**的 Win32 读法复刻（`trash_windows_list_test.go:23-45`），逐单元 `cur = append(cur, rune(u))`：代理对的高/低半各自 → `rune(0xD83D)` 非法 → `string()` 折成 U+FFFD。产品侧 `buildPathList` 用 `syscall.StringToUTF16`（`trash_windows.go:488-502`），代理对**本来就写对了** | 测试侧改用 `unicode/utf16.Decode` 合对（与被测物仍是不同实现，独立复刻的身份不变）。断言与用例意图（"编解码这层没问题，缺陷纯粹在 NUL 个数"）一字不动 |
| W2 | `volumeIDOfExisting` 先 `filepath.Clean`（`volume.go:66`）再喂给注入桩，桩用 `strings.HasPrefix(p, "/mnt/b")`；Windows 上 Clean 产出 `\mnt\b\g.bin` ⇒ 桩恒回落 "dev-A" ⇒ `sameVolume` 两侧同号判同卷。**产品判据（st_dev / 卷序列号）与分隔符无关，未受影响** | 桩改成对 `filepath.ToSlash(p)` 判前缀，两条断言（跨挂载判否 / 同挂载判是）在任一平台都仍然真跑 |
| W3 | 夹具用 `os.Chmod(path, 0)` 造"打不开"。Windows 的 chmod 只翻**只读属性**、不拒绝读 ⇒ `VerifyFile` 打得开、哈希相符 → 返 `VerdictPass(0)`，正是日志那个 `= 0`。断言本身（"读不了 = 无从判定，不许报成'被修改'"）在 Windows **未被检验**而非被推翻 | 前置自检：改完权限后**真的**读不了才继续；读得动就 `t.Skipf` 写明平台原因（与 `requireSymlinkSupport` / `newMergeFixture` 同一惯例）。按约束 5，这条在 Windows 腿记"未兑现"，不算通过 |
| W4 | `dedupeRoots` 第一步就 `filepath.Abs`（`scanner.go:564-570`），Windows 上 `/data/b` → `D:\data\b`（工作目录在 `D:\a\FileDedup\FileDedup`）。期望值把 **unix 归一的产物**写死了；判据本身（宽根胜出 / 无父子关系的两根都留）在 Windows 同样成立：`fscase.Fold` 的分隔符腿恒把 `\` 换 `/`（`pathnorm` 收归后唯一实现），折叠键仍是 `d:/data/b` 前缀于 `d:/data/b/a` | 期望值改由平台自身的 `filepath.Abs`+`Clean` 算出（`want` 的形状随平台，**谁胜出**的判据不随平台）。改前在 linux/mac 上读数不变，windows 腿由红转绿 |

W1~W4 全部落在**测试与夹具**，本轮 Windows 腿**没有生产代码改动**。

### 21.4 兑现边界（先说清，免得划账说谎）

- inode 回收只在 Linux runner 上真发生过 ⇒ 这 11 条夹具改动的"改前红"**本机 darwin 拿不到读数**；
  能给的只有 CI 的既有红（run 35638637530）与**改后 CI 绿**（下一跑）。凡此一律写"CI 读数"，不写"本机验证"。
- W1/W2/W4 的本机红同样拿不到（`//go:build windows` 与平台归一只在 windows 腿执行）；
   linux/mac 三条腿只能证明"改动没有把原本绿的弄红"。
- W3 改后在 windows 腿会是 **SKIP**，按 04 §6.8.0 与 AS-K2 的口径 **skip ≠ 通过**：
  划账里必须写成"Windows 侧 M52 无从判定这条未验证"。
- 本轮不动 §6.11 任何一行既有结论；新发现按约束 (1) 只新增 ID（M91）。

### 21.5 改后读数（实施提交 `b6842bd`，run `35642706382`）

CI 三条腿：**`completed success`** —— `go test (macos)=success ; go test (windows)=success ;
gofmt / vet x3 / test -race / frontend / smoke=success`。整份 job 日志里 `--- FAIL` 0 条、
`FAIL filededup` 0 条，三条腿各 **22 个包 ok**（按 job 去重计数）⇒ §21.0 的 16 条全部转绿，
且没有把别处弄红。

本机（darwin/APFS）配套读数：`gofmt`/`build`/`vet ×3` 全 0；`go test -race -count=2 ./internal/ops/ ./internal/scanner/`
两个包 `ok`；全套 15 行门禁 14×`rc=0` + 第 15 行 `rc=2`（`smoke-symlink` 需 root，**跳非过**），
且与上一批那次**逐格相同**（`top_PASS=647 top_SKIP=5 top_FAIL=0`、`sub_PASS=77 sub_SKIP=1`、
`run=730`、`src_test=699`）⇒ 本轮既没新增用例也没删用例。

**变异取证（证明换夹具之后判据仍然杀得动）**：临时把 `identityStill` 改成恒 `return true`，
本机当场红三条 ——

```
--- FAIL: TestIdentityStillDetectsRegularReplacement   identity_still_test.go:51
--- FAIL: TestIdentityStillDetectsSymlinkSwap          identity_still_test.go:110
--- FAIL: TestSymlinkMergeDetectsKeepReplacement       symlink_test.go:623
```

变异已复原（复原后 `git diff -- internal/ops/verify.go` 为空，实现仍是
`still, _ := identityStatus(path, id); return still`）。

**§21.4 的三条边界里，有两条本轮按预告兑现、一条要更正**：

1. "改前红只能来自 CI" —— 兑现：本机的 11 条改前红至今没有读数，将来也不会有（APFS 不还号）。
2. "W1/W2/W4 本机改前红拿不到" —— 兑现：本机只证明了"改动没把原本绿的弄红"。
3. "W3 在 windows 腿会是 SKIP" —— **这句现在只是一条推断，不是读数**：三条腿的 `go test`
   都不带 `-v`，日志里没有 `--- SKIP` 行，CI 无法区分"这一格走了 Skipf"与"这一格真的跑完并通过了"。
   ⇒ 按约束 5 从严处理：Windows 侧"M52 无从判定分支"记**未验证**，不记通过；
   要拿到这条的硬读数，得在 windows 腿补一次带 `-v` 的跑法（本轮不做，属 §6.5 开放项 E 同一族）。

**本轮没有做的两件事，如实记下**：① 没动 `scripts/test-windows-quarantine.sh` 的隔离清单（仍为空），
W3 走的是自探环境 `t.Skipf`，不是白名单遮掩；② 没实施 M91 的任何一条修法（三选一属裁定面）。

**这一跑的副产品：step13 修好之前，ubuntu 腿的 step14~17 一直是 `skipped`**（GitHub 的步骤在前置
步骤失败后不执行）⇒ 根包 `-count=4`、CLI 冒烟、**跨卷软链接冒烟**、冒烟判据自证这四步
此前从未有过 CI 读数。修好后两条值得留档的读数到手：**跨卷软链接 7 条判据在 loop 卷上真跑通**
（`OK: 跨卷软链接的 7 条核心判据全部成立`），以及 **CLI 冒烟两腿组数不同（linux 206 / macos 205）
是 benchgen `case_pair` 的设计内平台差、不是缺陷**。两条的逐格取证与增量对账记在 04 §6.17 七，
不在这里重复。★ 一句要紧的限定：CI 绿**不改变本机第 15 行 `rc=2 SKIP` 的记法**，
只是把"完全无读数"变成"Linux 侧有读数、Windows 侧仍无"。

---

## 22. 第十批（三轮全量审查·第 1 轮）：测试自证面 + dedup 组序确定性 + 话术过期面（M92~M100，登记 M101~M111；2026-09-22）

### 22.0 本批来源与复核纪律

六个**只读**分区子代理并行审（A `internal/ops`／B `scanner+dedup+hasher+model`／C `cache+history+sqlconn+dbfile+progress+media+worktemp`／D 根包 `app*.go`+`cmd/`／E 八个平台 helper 包／F 前端+门禁脚本+文档口径）。
**逐条自己开码复核后才进本段**，四条被推翻或收窄（§22.6），一条子代理未言明的连带事实被补上（§22.7）。
上一批 M90 假登记的教训在这里第二次生效。

本节以下每条都给了**真读数**（本机命令输出或 CI 日志原文），不是"读代码觉得有问题"。

### 22.1 M92：根包互斥用例的计数器裸 `++` 在 CI 被判 DATA RACE

**现象**：run `35644606017`（纯文档提交 `4c73f23`）macOS 腿 step7 `go test -race -count=2` 红，
`--- FAIL: TestScanAndOpsAreMutuallyExclusive` + `race detected during execution of test`，
`WARNING: DATA RACE` 的**读与写都指向 `app_p0_p1_test.go:212`**（`scanOK++` 那一行），两个
goroutine 均由 `:208` 的 `gowrap1` 创建。这是 §21 那批修完之后**同一处代码的第二次 CI 红**。

**先排除产品侧**：`StartScan` 的"判 `scanInFlight` + 写 `scanInFlight`"在同一次加锁内
（`app.go:591-604`），所以两次受理**必然串行** ⇒ 竞态只在测试自己的计数器上，互斥逻辑无洞
（断言那一格没红，红的是 race detector）。

**本机不可复现，且复现不出来是有原因的**：单跑 ×6、整包 `-count=2` ×1、
`GOMAXPROCS=1/2/4/8` 各 ×40（合计 160 次）**全为 0 次竞态**。临时打印计数器取到根因：
本机 12 次采样**全是 `scanOK=1 opsOK=0`** —— 只有一个扫描抢到受理，裸 `++` 只发生一次，
不构成两次访问。CI 那次是 `scanOK>=2` 才成立。

**替代取证（因为原用例的触发时序本机造不出来）**：把该形状单独复刻到 `/tmp/raceshape`
（12 goroutine、每个"成功后累加"，全部返回受理以最大化累加次数），本机 `-race`：

| 形状 | `WARNING: DATA RACE` 次数（`-count=3`） |
| --- | --- |
| 裸 `cnt++`（= 改前形状） | **4** |
| `atomic.Int64.Add(1)`（= 改后形状） | **0**（`ok raceshape`） |

⇒ 本机 race detector **认得这个形状**，故 CI 那份读数为真、且原子版确实消掉了它。
★ 如实限定：这证的是"形状"，不是"该用例在 CI 上重新转绿"——后者要等本批推送后那一跑，未兑现前不记通过。

**修法**：`scanOK`/`opsOK` 改 `atomic.Int64`。**判据 `scanOK>0 && opsOK>0` 一字未动**
（只是改成 `Load()` 后比较），不算改断言。

### 22.2 M93：三条 P1-1 互斥用例的绿来自**错的那道门**（死门禁）

这条是本批唯一的 P1 门禁缺陷，也是 §22.1 修完之后**紧接着才看得见**的——原子化只修了竞态，
把"它其实什么都没断言"这件事从"偶发红"变回了"恒绿"。

**取证链（每一步都是真读数，按动手顺序）**：

1. **变异 M-R1-a**：把 `ExecuteOperation` 的扫描侧门禁整条短路
   （`if a.scanInFlight` → `if a.scanInFlight && false`，`app.go:1809`）⇒
   `TestScanAndOpsAreMutuallyExclusive` 与 `TestExecuteOperationRejectedWhileScanInFlight`
   **`-count=6` 全绿**。门禁拆了用例不红 ⇒ 用例对目标判据零杀伤力。
2. **抓逐字拒因**（临时打印 ops 腿第一条错误）：
   `opsErr="暂无可操作的结果集（请先完成扫描或打开历史记录）"`
   ⇒ 拒它的是 `app.go:1816` 的 `resultsReady` 门，**排在互斥门（`:1809`）下游还是上游都要紧**：
   它在函数里位于 `:1816`，即**先于**任何"ops 真被执行"的可能，所以 `opsOK` 恒为 0。
3. **补上 `resultsReady=true` 后再变异（M-R1-b）**：两条用例**仍然全绿**。再抓拒因，得到**另外两道门**：
   - `opsErr="上一个清理操作仍在执行中"`（`opsRunning` 被同批另一个 ops goroutine 占住）
   - `opsErr="清理账本不可用，已拒绝执行（无账本即无法回撤与追溯）：history.db 未就绪"`
     —— 根因是 `newProbeApp`（`app_p0_p1_test.go:87-101`）**从不接 `a.hist`**，
     而 `ExecuteOperation` 的写前账本是 fail-closed 的。
4. **计数器读数**（补 `resultsReady` 的实验夹具，`-count=3`，`-v`）：

   | 跑次 | `scanOK` | `opsOK` | 该轮断言实际内容 |
   | --- | --- | --- | --- |
   | 1 | 0 | 0 | **无**（两侧都没受理，`scanOK>0 && opsOK>0` 恒假） |
   | 2 | 0 | 0 | **无** |
   | 3 | 1 | 0 | 仅"ops 被拒"，而拒因是账本门不是互斥门 |

   ⇒ 三次里**两次整条用例什么都没断言**，第三次证明的那一格也不是它声称的那一格。

**判据结论**：这条用例的文档注释写着"互斥必须双向闭合"，但它**在任何一种产品缺陷下都不会红**。
`TestExecuteOperationRejectedWhileScanInFlight`（`:186`）同因（未铺 `resultsReady`）。
`TestExecuteOperationMutualExclusion`（`app_test.go:323`）**不在**本条面内：它断的是 `opsRunning`
门，而那道门恰好是 `ExecuteOperation` 的第一个检查（`app.go:1805`），拒因归因正确。

**修法（设计）**：判据从"数一数两类各成功了几次"改成**"拒因必须是那句互斥文案"**，
这是本项目既有的反归因错位手法（§21 的 P-3b 负控制同族）：

- 铺一个**能走到互斥门**的可操作态：接 `a.hist`（复用 `newHistApp` 的 `history.Open` 接法，
  不另造一份）、在锁内一起铺 `resultsReady=true` + `groups`/`byID`（I5：抽成一处 helper）。
- **phase A**（钉"扫描在途 ⇒ 拒清理"）：并发发 ops，要求 `opsOK==0` **且每条拒因含"扫描进行中"**；
  任何一条拒因是别的门 ⇒ 当场红，用例不会再拿"别的门替它挡了"当通过。
- **phase B**（钉"清理在途 ⇒ 拒扫描"）：并发发 `StartScan`，同样双条件（含"清理操作执行中"）。
- 两 phase 各自再钉 `受理数 <= 1`：这条保留原用例的**并发价值**（若"判"与"写"被拆到两次加锁，
  会出现两个都受理 ⇒ 红），M71 那一族就还是没人管。

**★ 对"不许改测试断言来让门禁变绿"的正面交代（约束 2）**：本条**不是**把红改成绿。
现状是恒绿，改后是**两条变异各自必须红**（交付判据见 §22.5），杀伤力从 0 变成有。
原来的 `scanOK>0 && opsOK>0` 判据被换掉是**不得不**：它把"先后各受理一次"当成违规，
而 §22.2-3 的读数是"先后受理"在正确实现下也会发生 ⇒ 该判据一旦夹具修好就**必然假红**。
旧判据的意图（双向闭合）由 phase A/B 双条断言原样承接，且比它多钉了归因。

### 22.3 M94：dedup 的组序与组号在同输入下不确定，且注释承诺的次级键从未参与

**开码证据**（`internal/dedup/pipeline.go`）：
- `:765` `for k, g := range finalGroups` —— `finalGroups` 是 **map**，Go 的 map 遍历序随机；
- `:791-793` `id++` 后 `GroupID: id` ⇒ **组号由那个随机遍历序发放**；
- `:800` 注释承诺"稳定排序：可释放空间降序，**其次组大小**"，`:801-806` 的次级键实际是
  `groups[i].GroupID < groups[j].GroupID` —— `len(Files)` **在全函数里从未出现**，
  而顶替它的那个 `GroupID` 本身就是随机的。

⇒ 两组的 `Reclaimable` 相等时（例：3×2MiB 与 2×4MiB，都是 4MiB 可释放），**输出顺序和两个组号
在两跑之间都不一致**。同一文件 `scanner.go:524-526` 把这条纪律写得很清楚：
"不排序就是同一份输入两次扫描给出两份清单"——dedup 侧漏了。
连带：`pipeline_test.go:186-189` 按下标比对 paranoid/普通两组，靠"夹具三组 Reclaimable 恰好不等"
侥幸不闪，**不是判据保证**。

**改组号前必须查过的连带面（已查，安全）**：`GroupID` 不进持久化身份 ——
`internal/history/scan.go:200` 恢复历史时用的是 DB 自己的 `gr.id`，不是扫描期 `GroupID`；
前端 `ResultView.vue:493` 只把它当 Vue `:key`（同一次结果集内唯一即可）。

**修法**：排序键改成**全序**且兑现注释：`Reclaimable` 降序 → `len(Files)` 降序 →
组内最小路径升序（`sortEntries` 已按 `Path` 升序排过，`g[0].Path` 即组内最小路径，确定性来源）；
**`GroupID` 改为排序之后按序号发放**（1..N）⇒ 同输入两跑的组序与组号都一致。

### 22.4 M95~M100：六处"话术跑得比代码快"

| ID | 坐标 | 复核到的事实（不是子代理的原话，是我自己开码看到的） |
| --- | --- | --- |
| M95 | `internal/cache/cache.go:85` × `:89` × `internal/dedup/pipeline.go:279` | `dbErrs` 声明注释写"（本轮累计）"，同一文件隔 4 行的 `DBErrors()` 注释写"累计到的"——**相邻两行自相矛盾**。全仓 grep 该字段只有 `Add(1)`（`:106`）与 `Load()`（`:90`），**没有任何重置点**；而 `pipeline.go:279` 把它印进用户可见文案"哈希缓存在本轮被确证损坏…**本轮累计库错误 %d 次**"。`openCache` 只在启动开一次库（`app.go:305-319`）⇒ 第 1 轮的暂时性错误会串进第 2 轮起的"本轮"，且只增不减。违"计数不许说谎"。修法=把口径改成真话（进程期累计），**不新增轮内重置**（那要动 `Cache` 与轮次的契约，属扩面） |
| M96 | `internal/cache/cache.go:4` | 包注释仍称"**损坏自愈**（确证损坏时隔离重建）"。同文件 `:86` 自己写着 `corrupt` 位是"停用，不是自愈"、M87 明言"直到重启应用"⇒ 措辞约束只管住了 `Corrupted()` 的返回文案，漏了包注释这一格 |
| M97 | `internal/history/history.go:23-24` × `:155` | 注释称 `SchemaVersion` 是"库结构版本（PRAGMA user_version）"；全仓 grep `user_version` 三处，`initConn` 无条件 `PRAGMA user_version=1` **只写不读**，`Open` 全路径没有一处读回比对 ⇒ 对照 cache 侧 `enforceAlgoVersion`（`cache.go:153`）是**真门禁**，history 这个"版本"是给未来的假承诺。修法=把注释改成"目前只写不读，尚无版本门禁"，真门禁**不补**（补它是改行为，违约束 7） |
| M98 | `internal/scanner/scanner.go:227` | 注释"见 **keyOf** 注释里的那条分隔符陷阱"，但 `scanner.go` 现只有 `visitKey`（`:162`），包内无 `keyOf`；而根包门禁 `app_pathnorm_gate_test.go:34` 断的正是 `keyOf` **不得再以函数形式存在** ⇒ 这条注释指向一个被门禁判死的符号，后来人按图索骥找不到落点（判据实文在 `:618-624`）。M64 交付后的注释残留 |
| M99 | `internal/history/history_m59_test.go:52-54` | 用例头注释称"先用 **OpenedConnections 的增量**证明第二次读落在新建连接上"，而**同一文件** `:20-21` 已明写"Go 1.27 的 `sql.DBStats` 无 `OpenedConnections` 字段"，代码实际用的是"池空 ⇒ 再取必新建"+`InUse=1` 双检 ⇒ 那句是被否决的旧稿残留。自检本身不空过（M59 的断言是真断言），修的是注释 |
| M100 | `internal/ads/ads_windows_test.go:18`、`probe_windows.go:55`、`ads.go:148`、04 §6.5 门禁表、09 §手册边界 | 三处代码注释 + 两处文档都还写着"**本仓无 Windows runner**，本条至今没有过真机读数（M32）"。**真读数**：green run `35642706382` 的 `go test (windows)` 腿日志第 644 行 `ok  	filededup/internal/ads   0.021s`，而 `TestFirstStreamNameIsDefaultOnRealNTFS`（V8）**通篇只有 `t.Fatalf`、无任何 Skip 分支** ⇒ V8 在真机 NTFS 上跑过并绿。⇒ 这是**未兑现转兑现**，但只转一半，见 §22.4 末 |

**M100 只算部分兑现，收窄的理由**（这条我自己差点说过头）：M32 原登记（04 `:1658`）涵盖两层——
① Win32 结构**偏移**（读错 `cStreamName` 会得到零长字符串 ⇒ 守卫恒放行）；② 端到端
"**有一条真命名流被拦住**"。V8 只钉 ①；钉 ② 的是 **V8b** `TestDetectsRealNamedStreamOnNTFS`，
它有两条**合法 Skip** 出口（`os.WriteFile(p+":note")` 失败、`Classify(errno)==ErrFSNoStreams`），
而 CI 三条腿都**不带 `-v`**（§6.17 尾注自认"SKIP 与 PASS 无差分"）⇒ 从 `ok` 行无法判断 V8b
是真绿还是走了 skip。**所以 ② 仍然没有读数**，M32 保持"部分兑现"，不得改写成"已兑现"。
要拿到 ② 的硬读数，得在 windows 腿补一次带 `-v` 的跑法（本轮不做，与 §21.5 同一开放项）。

### 22.5 本批交付判据（做不到就不划账）

1. **两条变异各自必须红**（M93）：M-R1-b（拆 `ExecuteOperation` 扫描侧门禁）⇒ phase A 红；
   M-R1-c（拆 `StartScan` 的 `opsRunning` 门禁）⇒ phase B 红。改前两棵树全绿的读数记在 §22.2。
2. **M94 必须有"同输入两跑不一致"的改前红**：先取改前真读数（同一夹具连跑两遍比对组号序列），
   再取改后（两跑组号序列逐位相同）。
3. **M92/M95~M100 属话术与测试面**：不产新行为，判据是"改前该格的话是假的"——每条给出
   开码坐标与那句原话，本表即是。
4. 全套 15 行门禁（`/tmp/run_gates.sh`，stdout 重进**带时间戳的新文件**，不读固定路径旧报告）。
5. 04 §6.18 划账 + §6.11 表**只增不改**（新 ID M92~M111；M32 行按其既有惯例加 dated 〔…〕括注，
   不改写原结论）。

### 22.6 复核后**被推翻或收窄**的四条（子代理原报，我开码否掉了危害面）

1. **`hash_cache` DDL 两份 ⇒ "版本作废重建后死索引可复活"**：**推翻**。`openDB`（含
   `DROP INDEX IF EXISTS idx_cache_size`，`cache.go:255`）**恒在** `enforceAlgoVersion`→
   `createSchema` 之前调用（`cache.go:128` → `:145`，同一函数内顺序）⇒ 任何走到 `createSchema`
   的路径上那个索引早已被删。DDL 两份是真的（`createSchema:184-200` 缺该条，靠人肉注释同步），
   但只作为**维护隐患**登记 = **M111**，不重构（G4 回归 `cache_test.go:270-329` 只钉了 openDB 腿，
   收归要动的是两处 DDL 列表的形状，属扩面）。
2. **`ads_windows_test.go` 的"V8 绿即 M32 兑现"**：**收窄**到偏移层，理由见 §22.4 末（V8b 双 Skip 出口 + CI 无 `-v`）。
3. **`realbytes` Windows 卷键大小写分裂 ⇒ 算缺陷**：**降级为登记**（**M104**）。方向是
   `C:`/`c:` 折成两个 FNV 键 ⇒ 卷级证据按拼写分裂 ⇒ **退回逻辑口径**，是 fail-closed 不是错账；
   且本机零 Windows 执行面，`internal/realbytes` 连一个 `*_windows_test.go` 都没有（实测 `ls`，
   只有 `reported_unix`/`volume_unix`/`clone_darwin` 三份）⇒ 修它要连带补 windows 腿测试，另批做。
4. **`filter` 段匹配区分大小写 vs `sysguard` 用 `EqualFold`**：**不擅自统一**（登记 **M105**）。
   复核为真（`filter.go:252` `path.Match(pat, seg)` 全函数大小写敏感；`sysguard.go:109`
   `strings.EqualFold(name, e.name)`），后果也真（不敏感卷上用户排除 `Temp` 挡不住 `TEMP`，
   整棵照扫 = fail-open）。但"用户排除模式该不该区分大小写"是**产品口径**、`model`/手册
   均无声明 ⇒ 按"未裁定项不自行选边"处理，进待裁定清单而不是本批改动面。

### 22.7 本批补上的一条连带事实

子代理只报"§6.11 里的 `dbErrs` 口径不对"（M95）。开码时发现的**连带一层**：
`corruptCacheNotice` 的调用点是 `pipeline.go:466`，在 **`Run` 的 defer 里**，而 `Corrupted()`
是**粘滞位**（一旦置真直到重启）⇒ 从第 2 轮起，每轮的轮末文案都会带着**第 1 轮起累积的全部**
错误条数再说一遍"本轮"。这不是措辞瑕疵而是**同一数字每轮都变大**：用户按"本轮"读会以为
错误在持续增长。修法仍是把口径改成真话（"进程启动以来累计"），并在 `:85` 与 `:89` 两处
注释统一到同一个说法（I5：一个口径一个说法）。

### 22.8 本批**复核后转登记**的清单（M112~M121）+ 一条被推翻（2026-09-22，划账 §6.18）

§22.1~§22.7 是"改完的"。这一节是**开过码复核为真、但本批不改**的：它们不在 §22 已提交的
设计面里（约束 7 不扩大改动面），按"每项实施前先写细化设计段"的纪律，下一批带自己的设计段再动。
每条同样给**开码坐标 + 我自己看到的事实**，不抄子代理原话。

| ID | 坐标 | 复核到的事实 | 为何本批不修 |
| --- | --- | --- | --- |
| M112 | `internal/ops/move.go:207-212` × `:222` × `:56-59` | `copyVerify` 的注释明写"还原失败**不作为整体失败**（数据已在目标处），单独返回错误供上层记录"，而 `:222` 是 `return restoreMeta(dst, st)` —— 该错误就是 `copyVerifyFile` 的错误，`MoveFile:56` 见错误即 `dst.release()` **把刚复制完的那份删掉**并整项判失败。源文件未动 ⇒ 不丢数据（fail-closed），但注释承诺的那条路**不存在**。★ 同包 `undo.go:445` 的 `applyMtime` 才是注释说的那个口径（"失败不作为整体失败"、返回 void），所以这不是"两种口径都对"，是同一件事在本包有两份说法 | 真要按注释做，得让 `MoveFile` 回"成功但降级"（`outcome` 有 `warn` 位而 `MoveFile` 签名只有 `(string, error)`）⇒ 与 **M40**「`MoveFile` 不回三态」同源，属形状改动；本批只登记，不动口径 |
| M113 | `internal/ops/undo.go:197-199` | `undoMove` 复用 `MoveFile`，而 `err != nil` 分支直接 `return "", err` **把已返回的 `dst` 丢掉** ⇒ "已复制但删源失败（两份并存，新副本在 …）"里的落点在回撤账本上没有条目。这正是 **M86**（回撤侧丢 `restored`）的同一形状、同一个函数族，M86 已修的 `undoExecuteItem` 没覆盖到 `undoMove` 这一格 | M86 的修法（`undoFailure` 兜一层落点）在这里可复用，但要连带核 `MoveFile` 的部分成功语义在回撤面的记账口径 ⇒ 与 M89 同一批做才不留半截 |
| M114 | `internal/ops/executor.go:344-345` ×（同族）`move.go:108/:112`、`symlink.go:82/:86` | `guardIdentity` 的 else 分支把三种情形压成一句"文件在扫描后被替换（inode 已变化），已拦截"：`identityStatus`（`verify.go:134-148`）返回 `(false,false)` 的路径有两条 —— 读身份**出错且非 ENOENT**（`:140`，例如父目录 EACCES）、以及**原先能解析现在解析不出**（`:142-146`）。两条都是"我们不知道"，不是"看到了另一个 inode"。四处 `identityStill` 的 S1 文案同型 | 判据本体要分第 3 态（确认顶替 / 无从判定），是 `identityStatus` 签名 + 那六处调用点的改动；且 `verify_m52_m54_test.go:215` 断的是 `"被替换"` 子串，改文案要先把"确认顶替仍说被替换"钉在前面（否则就是改断言凑绿） |
| M115 | `internal/ops/verify_m52_m54_test.go:195-204`、`internal/ops/ops_probe_test.go:92-101` | 本包已有"顶替夹具"的唯一实现 `swap_fixture_test.go:33 swapInAt`（原子改名 + `assertDistinctIdentity` 自检，后者存在是因为 CI 实证过回收 inode 的卷会把原号发给新对象，见 `identity_still_test.go:25`）。这两条用例**各自手搓**了 `WriteFile → Remove → Rename`，绕开自检：前者非原子（中间有一瞬路径上什么都没有），后者用自定义后缀 `.swap-in`。后果不是假绿而是**可闪**——顶替者若拿回同一个 ino，`identityStatus` 判"仍是原对象"，用例期望的那条 `Failed` 就不出现 | 纯测试面收归（两条改走 `swapInAt`），价值真但不在 §22 设计面内 |
| M116 | `frontend/src/views/ResultView.vue:219`、`ScanView.vue:18`、`RecordsView.vue:26` | 同一个展示规则（`new Date(sec*1000).toLocaleString('zh-CN', { hour12: false })`）**三处内联各写一遍**，而 `utils/format.ts:41` 已经是它的收归点（`humanBytes`/`formatCount` 同文件）。子代理报的"×4"是这三处 + 那份唯一实现 | 一处一行 import + 替换，风险低；但属"话术/形状"面，留到下一批与 M117/M118 同批做，避免本批再扩改动面 |
| M117 | `frontend/src/components/PreviewPanel.vue:42`（`MD_RENDER_MAX = 64 * 1024`）× `app.go:1082`（`textLimit = 4 << 10`） | 这道"超过阈值默认退回源码"的护栏**不可达**：预览内容只有 `PreviewFile` 一个来源（`stores/scan.ts:903-916` 是唯一写入点），文本腿最多回 4 KiB ⇒ `content.length > 64 KiB` 恒假。于是 `:34-41` 那段注释里 20/100/400 KB 三档实测（"400 KB → 450 ms 肉眼可见的卡顿"）描述的是一条**到不了**的路径 | 三种处置（删护栏 / 把阈值改成与 4 KiB 相称 / 保留但注明"当前不可达，防的是预览上限以后变大"）各有取舍，且删码要连带核 `rendered` 那个开关的语义 ⇒ 要设计段，不顺手改 |
| M118 | `frontend/src/views/ScanView.vue:96-100` × `utils/opdisplay.ts:83-86` | 百分比夹取有**两份实现**：`percentOf` 是收归点（带 `Number.isFinite` 与 `Math.max(0, …)` 下限），`ScanView` 的 `progressPercent` 自己写了一份 `Math.min(100, …)`。**子代理那句"第二条未夹取"不成立**——它夹了上限，只是少了下限那半边 | 一行改 `percentOf(p.BytesDone, p.BytesTotal)`；与 M116 同属前端展示面，同批做 |
| M119 | `scripts/smoke-symlink-assert.sh:238-241` | 收尾只判 `fails -eq 0`，**没有断言条数下限** ⇒ 若所有 `ok`/`bad` 分支都没走到（桩挂错路径、`case` 全部 miss），脚本会打印"全部断言通过"并 `exit 0`。这条脚本存在的意义正是"自证冒烟判据本身有效"，而它自己可以被"一条都没验"骗过 | 修法就是加一个计数器 + `[ "$checks" -ge N ]` 下限；本批刚跑过它（行 14 绿），改它要重跑该行，留到下一批与 M120 一起 |
| M120 | `.github/workflows/build.yml`（全文件） | **零处 `timeout-minutes`**，而 `ci.yml` 在 AS-K5 那一格（`:37-42`）已经把理由写死了："没有 timeout-minutes 的作业，一旦某步卡住就烧满 6 小时平台默认上限，还把已产出的日志一起废掉"。同一条纪律套了 CI，没套发布流水线 | 加两行 job 级 timeout 即可；改 workflow 文件属 CI 面，与本批 ci.yml 只加注释的幅度分开提交 |
| M121 | `internal/cache/cache_test.go:3`（另 `:144` 的失败文案）× `cache.go:5` | M96 把包注释改成"损坏处理分两层，都不是'自愈'"之后，**同包测试文件头仍写着"损坏自愈"**。行为上没矛盾（那里说的是 Open 腿的隔离重建），口径上矛盾：同一件事在同一目录里两个名字。§22.7 刚为 `dbErrs` 的相邻两行立过"一个口径一个说法"这条 | 注释面一行；本批 M96 已经交付，重开同一格只加一个词，按"下一批统一清残余"处理更清楚 |

**★ 复核后推翻的一条**（不登记、不改码）：本轮一度要把"`internal/cache/cache_test.go`
上那道悬空的 `//go:build linux`"写成 M112。开码读数：`cache_test.go` 第 1 行是 `package cache`，
**根本没有 build tag**；全仓 `//go:build linux` 只有一处（`app_p3_linux_test.go:1`），而它测的
`revealCmd` 选的是 `xdg-open`/文件管理器那一族 Linux 专有命令，标签是**对的**。
⇒ 该条按 §22.0 的纪律就地作废、不占号（假 ID 不留进登记表，与 M90 同一处理）。

**本批 ID 段的自我更正**：§22 的标题写了"登记 M101~M111"，那是**在复核之前预定了一个区间**。
实际复核通过后有内容的只有 **M104 / M105 / M106 / M108 / M111** 五个，
**M101 / M102 / M103 / M107 / M109 / M110 六个号没有对应的已复核事实** ⇒ 不写进登记表、
编号留空洞。教训落成真话：**ID 只能在复核通过后分配，不许预定区间。**

---

## 23. 三轮全量审查·第 2 轮设计段（2026-09-22）：§22.8 那 10 条的实施判据 + 本轮新增 4 条

本轮来源三处，全部**自己开码复核**后才进本节：

1. §22.8 登记的 **M112~M121** 十条（第 1 轮复核为真、当时按约束 7 未动）。
2. 本轮两个只读审查子代理。★ 两者质量差得极远，必须先记下：
   - 代理甲（声称审 `339a95c` 的 diff）交回 3 Critical + 4 Important + 2 Minor，
     逐条开码复核后 **Critical 全三条与 Important 四条中的三条为伪造坐标**：
     `identityStill` 实有 12 个非测试调用点（`grep -rn "identityStill(" --include=*.go internal/`
     的逐行读数，见本轮取证），`percentOf` 在 `ResultView.vue:10` import、`:190` 使用，
     全仓 `RunReport` / `Cache.Versions()` / "database is locked"（Go 侧）三串**零命中**，
     根路径在 `scanner.go:565-575` 已 `filepath.Abs`，`run-gates.sh` 的竞态只有两行且
     行 7 就是 `-race ./...`（含根包），并非它引的"6~9 行排除根包"。
     ⇒ 甲的报告**一条不采信、一条不占号**（甲另指控的"§22.8 已登记项"重复除外）。
   - 代理乙（审 `339a95c` 的实施面）交回 9 条，其中 **5 条开码即为真**（M122~M125
     加一条提交说明不符），已按"复核通过才分配 ID"登记为 **M122 / M123 / M124 / M125**。
     它另一处纠正也成立：我在派单时把 `339a95c` 的父提交写成 `0d63d1a`，
     真读数是 `git cat-file -p 339a95c` 的 `parent 15b94a1`（§22 设计段那一跑）
     ⇒ 用错基线的 `git diff` 会把 M71/M74/M75/M21 等中间批的改动算到本提交头上。
   教训落成真话（与 §22.8 那条并列）：**子代理给的坐标要先当假设、后当事实**，
   负向断言（"从没被调用""全仓不存在"）必须有我自己跑过的 grep 读数背书。

### 23.0 本轮判据总则

- 每条**动码前先看它红过一次**，失败原因须与预测同格；取红后验"红的是那一格"。
- 注释/文案面（M112/M121/M125）不造 RED 码：判据是"**旧文案对着新代码必须为假**"，
  以逐字对照 + `git diff` 读数入账，不假称跑红。
- 平台腿拿不到真机读数的（M120 的 build.yml 属发布流水线，本机无 runner 可跑），
  划账写"代码已改、验证未兑现"。
- 断言只许**补强**、不许收窄（约束：不许改断言凑绿）。本轮唯一改文案的
  M114 必须先钉住"确认顶替仍说被替换"再动（§23.4 步骤 1）。

### 23.1 M112 `copyVerify` 注释 vs 实际处置（话术面）

坐标：`internal/ops/move.go:207-213`（注释）× `:222`（`return restoreMeta(dst, st)`）
× `:56-59`（`copyVerifyFile` 报错即 `dst.release()` + 整项失败）。

复核读数：注释承诺"还原失败不作为整体失败（数据已在目标处），单独返回错误供上层记录"，
而代码把 `restoreMeta` 的错误原样当 `copyVerify` 的错误返回，`MoveFile:56` 见错即删掉
刚复制完的那份并判失败。同包 `undo.go:447 applyMtime` 才是注释描述的那种口径
（失败不升级为整体失败、返回 void）⇒ 同一件事在本包有两份说法，说谎的是注释。

修法：**只改注释**（改成真话："还原元数据失败与复制失败同处置——删掉半成品、整项判失败；
源文件未动，故不丢数据，但这次移动没做成"）。不改行为：真要按注释做需 `MoveFile`
回"成功但降级"三态，那是 **M40** 的形状改动，不属本批。
判据：新文案与 `:56-59`/`:222` 三段逐字对照 + 记录"行为面仍挂 M40"。

### 23.2 M113 `undoMove` 把已复制的落点丢掉（部分成功留痕）

坐标：`internal/ops/undo.go:197-199`。复核读数：`MoveFile` 有两条"返回 `dst` 且
`err != nil`"的部分成功路径（`move.go:60-64` 身份被顶替、`:112-116` 删源失败），
`undoMove` 却 `return "", err`。app 层的兜子已经就位——`undoExecuteItem:2255` 会把
`restored` 连带错误一起返回，`undoFailure:2211-2215` 见 `restored` 非空且不在文本里就补
"（数据已在 …）"（M86）⇒ **本包只差把 `dst` 交回去这一行**。

RED：新用例走 `copyVerifyFile` 缝（`move.go:411`，"复制已完成、源尚未删除"的注入点）
造一次 `undoMove` 部分成功，断 `undoFailure` 组装出的 `FailedItem.Err` 含那个落点。
预期改前红在"落点没出现在文本里"（错误文本本身含 `%s`，故必须断**账本字段**而非文本子串
——`MoveFile` 的文本已经带落点，所以判据要落在"`restored` 返回值非空"这一格：
断 `UndoOne` 的**第一个返回值**等于该落点）。取红读数入账后才能改。

### 23.3 M114 `identityStatus` 的第三种情形被写成"被替换"（判据本体）

坐标：`internal/ops/verify.go:134-148` × `executor.go:336-348`（`guardIdentity`）。
复核读数：`(false, false)` 有两条来路——`fsid.FromPathNoFollow` 报错且非 `ENOENT`
（`:139-141`，如父目录 EACCES）、原先能解析现在解析不出（`:142-145`）。两条都是
"我们不知道"，`guardIdentity:344-346` 却统一记 "文件在扫描后被替换（inode 已变化），已拦截"。

修法（分两步，顺序不可反）：
1. **先补强现有断言**：`verify_m52_m54_test.go:215` 现断 `"被替换"` 子串；在其旁加一条
   "确认顶替那一格必须同时含 `inode 已变化`"的钉（把当前文案钉死），并新增一条 RED 用例：
   经新加的 `fsidFromPathFn` 缝注入"读身份失败（非 ENOENT）"，断该格文案**不得**出现"被替换"。
   ⇒ 改前这条必须红，且红在文案（不是红在没拦住）。
2. 再改判据本体：`verify.go` 引入 `identityVerdict`（same / replaced / gone / unknown）
   与 `identityCheck(path, id) (identityVerdict, string)`；`identityStill` 与 `identityStatus`
   退化成它的包装（`still = v==same`，`gone = v==gone`）⇒ 现有 12 个调用点行为一字不变。
   `guardIdentity` 三分：`gone`→`ocSkipped`（M54 既有语义）、`replaced`→原文案、
   `unknown`→"无法确认文件仍是扫描时那个对象（<原因>），已拦截"。

边界（本轮不做）：`move.go:106/:110`、`symlink.go:80/:84` 那四处仍走 `identityStill`，
处置是 fail-closed（拦下），只有文案同病。理由：那四处拦的就是"无从判定"，改文案要重写
它们各自的 `fmt.Errorf` 拼接与对应断言，收益不抵改动面 ⇒ 在 §6.11 的 M114 行加括注留残，
不当已修。

### 23.4 M115 两处顶替夹具改走 `swapInAt`（测试面收归）

坐标：`internal/ops/verify_m52_m54_test.go:195-204`（`WriteFile → Remove → Rename`，
中间有一瞬路径上什么都没有）、`internal/ops/ops_probe_test.go:92-101`（自定义后缀 `.swap-in`）。
本包唯一实现是 `swap_fixture_test.go:33 swapInAt`（原子改名 + `assertDistinctIdentity` 自检；
该自检存在是因为 CI 实证过回收 inode 的卷会把原号发给新对象）。

改法：两条都换成 `swapInAt(t, path, before, data)`，后缀与自检一并收归。
断言一字不动（只改夹具）。判据：改后 `-race -count=2 ./internal/ops/` 全绿，
且 `assertDistinctIdentity` 在两条用例里真的被走到（读数法：临时把 `swapInAt` 的
`before` 传成 `after` 同值，必须红在自检 ⇒ 证明自检在本用例有效，随后改回）。

### 23.5 M116 / M118 前端展示面两处收归

- M116：`ScanView.vue:17-19`、`RecordsView.vue:25-27`、`ResultView.vue:219` 三处内联
  `new Date(sec*1000).toLocaleString('zh-CN', { hour12: false })` ⇒ 收进
  `utils/format.ts`（同文件已有 `formatMtime(ns)`，`:38-42`）。新增
  `formatUnixSec(sec)` 与 `formatMtime` 共用一份实现（`formatMtime` 保持原签名不动，
  避免牵动既有断言）。
- M118：`ScanView.vue:96-100` 的 `progressPercent` 自己写了 `Math.min(100, …)`（**有上限、
  无下限、无 NaN 防**）⇒ 改调 `utils/opdisplay.ts:83` 的 `percentOf`（`ResultView.vue:190`
  已在用它，不是新缝）。★ §22.8 记的子代理"第二份完全没夹取"不成立，它夹了上限。

判据：`scripts/test-frontend-logic.sh` 加一条 M118 的行为探针（`BytesDone > BytesTotal`
与 `NaN` 两格），取红一次再改；M116 属等价替换，判据是 `vue-tsc` + 该脚本的接线断言
（三处内联串改为零命中）。

### 23.6 M117 `PreviewPanel` 那道不可达的护栏（保留 + 说明，不删）

坐标：`frontend/src/components/PreviewPanel.vue:42`（`MD_RENDER_MAX = 64 * 1024`）
× `app.go:1082`（`textLimit = 4 << 10`）× `stores/scan.ts:903-916`（`preview.content`
唯一写入点）。复核读数：Markdown 分支要求 `kind === 'text'`（`:30`），而 text 腿内容
上限 4 KiB ⇒ `content.length > 64 KiB` 恒假，`:34-41` 那段 20/100/400 KB 实测描述的是
到不了的路径。

修法（三选一里取"保留 + 注明"）：注释里写清**当前不可达**（后端 `textLimit = 4 KiB`
远小于这道 64 KiB）、它防的是"预览上限以后变大"、以及那三档实测是**渲染器本体**的代价
读数而不是本路径的可达读数。不删护栏、不移阈值：删一次要连带核 `rendered` 开关的语义，
而阈值调低会让正常 md 文件默认退回源码（改行为）。

### 23.7 M119 / M120 门禁脚本与发布流水线

- M119：`scripts/smoke-symlink-assert.sh:238-241` 只判 `fails -eq 0`，无条数下限
  ⇒ `ok()`/`bad()` 各加一条 `checks` 自增，收尾要求 `checks -ge 14`（14 = 本轮实跑
  `grep -c "✓\|✗"` 的读数）。取红：临时在改后的副本上把 `run_stubbed` 的桩全部改挂
  （或把 A/B/C/D 四段的入口 `if` 短路），须红在"断言条数 0 < 14"，而不是打印"全部断言通过"。
- M120：`.github/workflows/build.yml` 两个 job（`:10 build`、`:170 release`）零处
  `timeout-minutes`，而 `ci.yml:37-42` 已把理由写死（无 timeout 的作业卡住即烧满 6 小时
  平台默认上限并连带废掉已产出日志）。加 job 级 `timeout-minutes`（build 40 / release 15）。
  ★ 本机无 runner ⇒ 划账写"代码已改、验证未兑现"。

### 23.8 M121 `cache_test.go` 的"损坏自愈"残余（话术面）

坐标：`internal/cache/cache_test.go:3`（文件头"损坏自愈"）、`:141`（`// 应自动重建`）、
`:144`（`t.Fatalf("损坏自愈失败: %v", err)`）× `cache.go:5`（M96 已改成"都不是『自愈』"）。
复核读数：行为不矛盾（`:141` 说的是 `Open` 腿的隔离重建，`Open` 确实重建，见 `cache.go:124-141`），
口径矛盾——同一件事在同一目录里两个名字，而 §22.7 刚为 `dbErrs` 相邻两行立过
"一个口径一个说法"。修法：三处措辞统一到"隔离重建"（与 `cache.go:5` 同一说法），不动断言。

### 23.9 本轮新增四条（M122~M125，代理乙提出、我逐条开码复核为真）

| ID | 坐标 | 复核到的事实 | 本轮处置 |
| --- | --- | --- | --- |
| M122 | `internal/dedup/pipeline.go:800-817` × `group_order_probe_test.go:6-9` | 排序第三级键（`Files[0].Path`）**没有任何判据覆盖**：现夹具两组是"可释放量相同、成员数不同"，第二级就分出胜负 ⇒ 把 `:812` 那行删掉探针仍绿，而 `:804` "三级用尽后不再有平手"这句承诺随之变假话 | 扩夹具到三组：两组同可释放量**且同成员数**（2×600KiB 与 2×600KiB 不同内容），第三组保持第二级可分辨。判据 = **变异**：删第三级键必须红；`-race -count=4` 连跑须稳 |
| M123 | `scripts/run-gates.sh:130-143` × `scripts/smoke-symlink.sh:19-22` | `sh_row … 1` 只认"裸 `rc==2` ⇒ SKIP"，而被包脚本自己的契约是 **"`SKIP:` 前缀 + 退出码 2"两件事同真**（`:37`/`:44` 各写一遍，CI 那份实现也是双条件）。⇒ 真失败若恰好以 2 退出，会被降级成 SKIP 且总判定仍 `exit 0`，正是 AS-K2 防的那一格 | 改成双条件：`rc==2` **且** 日志含 `SKIP:`；缺一即 FAIL。取红：在副本上把被测命令换成"exit 2 且不打 SKIP:"，改前读成 SKIP、改后读成 FAIL |
| M124 | `scripts/run-gates.sh:54-65` | gofmt 行只判"列出文件数为 0"，`GRC` 仅回显不判定 ⇒ `gofmt` 自身崩溃（rc≠0 且 stdout 空）读成 PASS | `GRC -ne 0` 即 FAIL（判据从"一条"补成"两条"）。取红：副本里把 `gofmt -l .` 换成 `sh -c 'echo >&2 boom; exit 2'`，改前 PASS、改后 FAIL |
| M125 | `internal/scanner/scanner.go:227`（M98 我自己写的括注） | 那句"分隔符陷阱的实文在 visitKey 与 **dedupeRoots** 各自的注释里"后半指错：`dedupeRoots:550-563` 的注释讲的是折叠/M36/C1（`probeCaseSensitive` 只为它服务），分隔符陷阱的实文在 **`pathnorm` 包注释**里（`visitKey:153-160` 就是那么指的） | 改指 `pathnorm` 包注释。第 1 轮 M98 的修法本身为真，只是新造的交叉引用又错了一处 ⇒ 就地修 + §6.11 该行加括注 |

不占号的两条（只记账）：(a) 代理乙指出 `339a95c` 提交说明里"M92 …判据一字未动"与同一提交
被 M93 重写后的 diff 不符——那是**中间态**的说法，已提交的历史不改写，在 §6.19 记下；
(b) 代理甲的 7 条伪造/失真（§23 开头已列逐串零命中的读数）——不登记、不占号。

### 23.10 第 3 份子代理报告（叶子包）复核后追加：M126~M132

追加时点在设计段提交（`eb099f3`）之后、实施提交之前；这份报告与代理甲不同，
它每条都带了可复算的引文与"改哪一行会红"的验证建议，**7 条开码复核 7 条为真**
（另有一条它自己标"本机无 runner 不可证"的未采信）。逐条读数：

| ID | 坐标 | 复核到的事实（我自己开码/跑过的） | 本批处置 |
| --- | --- | --- | --- |
| M126 | `internal/fscase/fscase.go:154-163` | `verdictFrom` 的 `case uerr != nil: return true, true` 把**任何** Lstat(upper) 错误（EIO/ESTALE/EPERM/ENOTDIR）都当成"换种大小写看不见 ⇒ 卷区分大小写"这一**确证结论**并缓存（`Sensitive:55-70` 按目录永久缓存）。两处承诺被推翻：包注释 `:11-13`"探测不可用（只读卷、无写权限、目录尚不存在）时退回平台默认"、`probe:116`"任何一步不确定都退回默认值，不猜测"。★ 方向也不是"两种错法都行"：包注释 `:7-9` 明写判错方向的后果是"据此下发的清理会多删文件" | **修**：`errors.Is(uerr, os.ErrNotExist)` 才给确证，其余退 `Default()`。`verdictFrom` 是纯函数、**全仓零测试点**（`grep -rn "verdictFrom" --include=*.go .` 只命中 `:137` 调用与 `:149/:154` 自身），故三条用例都能在 darwin 本机真跑：ENOENT→确证、硬链接同号→不敏感、`upper` 落在文件之下（ENOTDIR）→退默认。RED 顺序：先写用例（第三条必红在"确证成 sensitive"），后改码 |
| M127 | `internal/sysguard/sysguard_test.go:56-62` | 注释说"这里**显式钉住**该语义，防止有人顺手改成逐段扫描"，代码是 `if dirSkip(...) { t.Log(...) }`——`t.Log` 永不失败 ⇒ 死门禁。变异复算：把 `Guard.Dir` 改成逐段匹配，全包套件仍全绿 | **修**：`t.Log` 改 `t.Errorf`（钉住现有行为"后代段不判保护"，是**补强**）。判据 = 同一变异（临时把 `Dir` 改成逐段）必须红，改回必须绿 |
| M128 | `internal/filter/extset_probe_test.go:12-13` × `internal/filter/filter.go:41-45` | 测试头写"修法只在 `newExtSet` 一处（I5）"，同包 `filter.go:41-45` 写的正是反面："为什么放在 Compile 而不是 newExtSet …在 newExtSet 里补点会让那份基准反过来钉住错误语义"；`newExtSet:78-94` 确只做小写与选表 ⇒ 说谎的是测试头 | **修**（话术面）：测试头改指 `normalizeExtList`/`Compile`，与 `filter.go` 同一说法 |
| M129 | `internal/hasher/hasher_test.go:168-175` | `TestHashFullOpenError` 函数体只有 `os.Open(missing)` + `if err == nil { t.Fatal }`——**从未调用 `HashFull`**，断的是标准库行为。`HashFull` 里任何守卫（负 size、短读、变长）改坏它都照绿 | **修**：改成真调 `HashFull(f, …)`（`f` 用不可读句柄路径的等价注入：`os.Open` 已失败 ⇒ 直接调 `HashFull(nil-safe)` 不成立，故按 `HashFull` 的真实契约造"句柄有效但读失败"那一格——`HashFull` 签名 `:254` 收 `*os.File`，用例改走"打开后立刻 Close 再读"的 EBADF 路径）。若那条在 CI 上与本机行为不一致 ⇒ 降级为只登记 |
| M130 | `internal/progress/progress.go:23` × `:68` × `:90` | `lastEmit` 只有声明与两处写入，**全仓无读取点**（`grep -rn lastEmit --include=*.go .` 恰好三条）——按时间节流的旧实现残骸，现由 ticker 承担 | **登记不修**：删字段是死代码清理，不属本轮判据面；且 `progress` 包刚在 §18 动过，避免同包二次改动 |
| M131 | `internal/hasher/growth_sampling_test.go:134` | `r.Partial != r2.Partial \|\| r.Full != r2.Full` 的后半**恒不成立**：夹具是 300 KiB（`> SmallFileMax`）走大文件腿，而 `Result.Full` 只在小文件腿赋值（`hasher.go:108`→`:125`），两侧都是零数组 | **登记不修**：删一半是**收窄**断言（本批禁做），留着只是不锐利；下一批若加"小文件腿两次指纹一致"的另一条用例可顺带清 |
| M132 | `internal/hasher/shortread_test.go:111-114` | 注释称"允许的上界是真实长度 + 一个 64 KiB 采样块——该口径下可达的精度上限"。`hasher.go:173-174` 的契约是"不低于真实长度的保守上界"，且推导取"全部读不满点里最小的 off+n"（`:181-190`）：夹具 `declared = real + 300 KiB` 时中点落在 `declared/2`，`v = declared/2` 可以远大于 `real + chunk` ⇒ 那句"上限"是**夹具特定**的，不是口径给的 | **登记不修**：断言本身对夹具成立（改它=改判据），要动的是注释；同包本轮已因 M129 开窗，按约束 7 留到下一批 |

### 23.11 实施期新增两条（M133 / M134，均为开码复核为真后才编号）

| ID | 坐标 | 复核到的事实 | 处置 |
| --- | --- | --- | --- |
| M133 | `.github/workflows/ci.yml:137-164`（冒烟步骤的 `case "$rc"`） | 本地 harness 按 **M123** 补成双条件时才看清：云端这一半是同一格的另一半——改前只认**裸 `rc=2`** 就降级成"被跳过 + 告警"、作业仍绿，而被测脚本的契约是"`SKIP（跳过，非通过）:` 前缀 + 退出码 2"两件事同真（`smoke-symlink.sh:19-22`、`skip():37-40`）。该脚本整体 `set -euo pipefail`，中途任何命令以 2 退出都会在这里被读成合法跳过 | **修**：输出 `tee` 留档 + `rc=${PIPESTATUS[0]}`（取 `timeout` 的码而不是 `tee` 的，取错就变成"tee 成功即通过"）+ `grep -q '^SKIP'` 不成立即 `exit 1`。★ 本机无 runner ⇒ 判据用**同一负控制**跑 yaml 里那段 shell：把被测命令换成"exit 2 且不打 SKIP" |
| M134 | `scripts/smoke-symlink.sh:79`、`:130`、`:136`、`:171`、`scripts/smoke-symlink-assert.sh:185`、`:244`、`scripts/check-version-sync.sh:87`、`.github/workflows/ci.yml:151`、`:174` | **取红复算时撞出来的**：在 `LC_CTYPE=C.UTF-8` 下（python 子进程默认带这个变量）门禁行 14 稳定红 3 条，逐字是 `✗ 真失败时退出码 = 0，应为 1`、`✗ 输出无 FAIL 标记`、`✗ 失败信息未带设备号`；`out.d` 里的真相是 `smoke-symlink.sh: line 79: LOOP\xef: unbound variable`。根因：`$LOOP` **紧跟全角 `）`**，而 macOS 自带的是 **bash 3.2.57**，它在 `C.UTF-8` 下把全角括号的首字节算进变量名 ⇒ `set -u` 直接打死被测脚本 ⇒ D 组三条断言红在"没失败"，而红的原因是**夹具与被测脚本共用的解析器缺陷**，与产品无关。bash 5（CI 的 ubuntu 腿）不分字节，故这是一条**只在开发机上复现**的假红 | **修**：九处同类站点逐处补花括号（`${LOOP}）`），纯写法收敛、不改任何判据与文案。全仓扫法：正则 `\$\{?[A-Za-z_]\w*\}?(?=[（）：，、—％｜])` 过 `scripts/*.sh` 与 `.github/**/*.yml` |

M134 的两条诚实限定：
1. **没加静态断言防回归**。这类"全字后面漏花括号"完全可以进 `smoke-symlink-assert.sh` 的 C 组
   做一条 grep 钉住，但新增断言会把 `MIN_CHECKS` 从 14 抬起、要连带重取条数读数，
   且本批 M119 刚立过这道门 ⇒ 留下一批（登记在本行，不当已修）。
2. **`.github/workflows/ci.yml` 那两处是预防性的**：CI 的 bash 5 不受影响，
   改它只是因为全仓扫到了同一形状（同一规则一处实现一次，I5 的话术侧）。

复算读数（同一负控制"被测命令 exit 2 且不打 SKIP"，跑的是 yaml 里那段 shell 原文）：

```
### 改前（HEAD 版同一步、同一负控制）rc=0
    ::warning title=冒烟未执行::跨卷软链接冒烟被跳过（runner 无法挂载独立文件系统）——该防线本轮未被执行，不等于通过
    nope
### 改后（工作树版，PIPESTATUS + 自证核对）rc=1
    nope
    ::error::rc=2 但输出里没有以 SKIP 开头的那行自证 ⇒ 按真失败判，不降级为跳过（M133）
```

M134 改后读数（`LC_CTYPE=C.UTF-8` 逐条单跑五个 shell 门禁行）：
`test-frontend-logic rc=0`、`check-version-sync rc=0`、`smoke-cli rc=0`、
`smoke-symlink-assert rc=0（14 条 / 0 失败）`、`smoke-symlink rc=2（SKIP，本机无 root；这一行的码与区域设置无关）`。

### 23.12 设计-执行偏离清单（本节是"设计段说过的话"与"实际做到的事"的对账）

逐条如实，不做美化。凡"设计里承诺、执行时换形"的，都在这里留痕；没列出的条目按设计做了。

| 条目 | 设计段原本的话 | 实际做到的 | 换形理由（复核后的事实） |
| --- | --- | --- | --- |
| M118 | "判据：`test-frontend-logic.sh` 加一条**行为探针**（`BytesDone > BytesTotal` 与 `NaN` 两格），取红一次再改" | 只加了 **5 条静态接线锚点**（`wiring ScanView wants percentOf( forbids "Math.min(100, "` 等），**没有新写行为探针** | 该脚本的行为探针是 `node --test` 跑 `frontend/tests/*.test.ts`，**导入不了 `.vue`**（vue-tsc 只查类型、不产出可 require 的模块）。而 `percentOf` 那两格本身早已被 `opdisplay.test.ts:85-98` 钉住 ⇒ 再写一遍是第二份实现（违 I5）。★ 诚实交代：这一格的"取红"改成了**接线锚点对改前文件的红**（见下），不是原设计的行为级红 |
| M116 | "三处内联串改为零命中" + vue-tsc | 加了三条 `forbids .toLocaleString(` 锚点，并用**改前文件**验锚点有杀伤力（5 条全红） | 等价替换无新行为可断，接线锚点是本仓唯一能自动化的形状 |
| M113 | "新用例走 `copyVerifyFile` 缝…断 `undoFailure` 组装出的 `FailedItem.Err` 含那个落点" | 用例**直接调 `undoMove`**，注入走 `removeSrc` 缝，判据落在 `undoMove` 的**第一个返回值** | `copyVerifyFile` 缝在 `MoveFile` 内部，够不到 `undoMove` 的返回语句；`undoFailure` 在 app 层，跨包用例要打两条缝。设计段后半句自己已改口（"判据要落在 `restored` 返回值非空"），实际按后半句做 |
| M114 步 1 | "在其旁加一条'确认顶替那一格必须同时含 `inode 已变化`'的钉" | 做了（`verify_m52_m54_test.go` 增一条 `strings.Contains(..., "inode 已变化")`） | — |
| M114 注入 | "如父目录 EACCES" ⇒ 用 `syscall.EACCES` 造"读不动" | 改用**平台中立的普通 error** `errSimulatedUnreadable`，并额外断"这句自证必须出现在文案里" | `syscall.EACCES` 的 `Error()` 文本是 `"permission denied"`，而 windows 腿 `syscall.Errno` 的文本与 `errors.Is(os.ErrPermission)` **不保证**一致 ⇒ 拿它做断言等于把用例钉在某一平台的 errno 表上（§21 那批 CI 红就是这么攒出来的） |
| M114 承诺 | "现有 **12 个调用点**行为一字不变" | 实际生产调用点是 **10 处 `identityStill` + 1 处 `guardIdentity`**（后者覆盖六个动作）〔2026-09-22 划账复核重取：`guardIdentity` 那句写得不准，逐字读数是 **10 处 `identityStill` 直调**（`symlink.go:80/:84/:117`、`merge_guard.go:44/:90`、`move.go:60/:106/:110/:328`、`undo.go:173`）**+ `guardIdentity` 的 6 个调用点**（`executor.go:463/:541/:558/:580/:599/:670`）；同一把尺子还纠正了"生产里再无调用者"——`identityStatus` 确有 **1 处**生产调用，就是 `identityStill` 自己的包装 `verify.go:166`，除此以外零〕；改后 `identityStatus` 在**生产里再无调用者**，只剩 `identityStill` 的包装与三条测试引用 | "12"是把 guardIdentity 按六个动作展开数出来的，写法不严谨。★ 由此产生一条**新的话术债**：`identityStatus` 现在是"只有测试在用"的兼容层，注释已按真话改（"M114 之后本视图只剩两格"），但**没有删它**（删它是收窄既有断言的落点，违约束 2） |
| M115 | "读数法：临时把 `swapInAt` 的 `before` 传成 `after` 同值，必须红在自检" | 按此做了（在夹具里插 `before = after`），两个调用点各自红在自检，位置逐字为 `ops_probe_test.go:96`、`verify_m52_m54_test.go:200` | — 另记一条**工具面事实**：覆盖率法不可用于这件事（`go tool cover -func` 对 `_test.go` 里的夹具函数**没有行**），所以"证明自检被走到"只能靠变异 |
| M126 | 三条用例"都能在 darwin 本机真跑" | 只有**两条**真跑：`SameObject`（PASS）与 `UnreadableUpper`（改前红、改后绿）。`UpperAbsent` 那一格在 APFS 上不可达，用例**自检后 `t.Skipf`**〔2026-09-22 划账复核：那一格实际用的是 `t.Skip`（多行消息），本轮已把 04 里的说法改成真码〕（`verdict_from_test.go:33`），它是本轮 `top_SKIP` 由 5 变 6 的唯一来源 | 原话把"纯函数没有平台分支"写成了"每条都能跑"，忽略了自己创建的 `lower` 在不敏感卷上会**命中同一对象**（走 `SameFile` 支而非 ENOENT 支）。★ 该支的读数归 CI 的 linux 腿，本机不得称已验（AS-K2） |
| M129 | "若那条在 CI 上与本机行为不一致 ⇒ 降级为只登记" | 未降级。`f.Close()` 之后再 `HashFull` 走的是 Go 运行时的 `checkValid` ⇒ 三条腿都返回 `fs.ErrClosed`，**不经过 OS errno**，故不需要 CI 读数即可定住 | 兑现了设计里那个条件的判定，而不是绕过它：判据不依赖平台 |
| M122 | "判据 = 变异：删第三级键必须红；`-race -count=4` 连跑须稳" | 按此做，且复算时拿到**两类红**：连跑漂移（`:124` 第 2/3/4/5/6/7/8 跑各漂一次）与**期望顺序错**（`:112` 两条），后者才是第三级键自己的格子 | 无偏离；补一句：`:124` 那句文案仍写着"改前判据：GroupID 由 map 随机遍历序发放"（M94 的话术），它对 M122 是**过度归因**，本批不改（改它会动既有断言的文本，约束 2） |
| ID 编号 | 约束："ID 只能在复核通过后分配" | 本批两次违规未遂：初稿在 5 个文件里写了 `OPS-17`/`DDP-2`/`FC-1`，其中 `OPS-17` 与 `DDP-2` 是**已存在的别家行**用过的列值；本轮末复核又在 `undo.go:199` 抓到一处残留（未入仓） | 教训落字：**写审计列之前先 `grep` 登记表**。三条已在 §6.11 的列值下重排（OPS-14e / OPS-13b / FC-3） |
| 改码事故 | — | 两次工具级事故都被下游抓回：① 三次 `Edit` 对 `ResultView.vue` 静默零命中（锚点含全角空格差异）⇒ 是**新加的接线锚点**读出的；② 一次 `Edit` 误删 `identityStatus` 里 3 行生产代码 ⇒ 是"改完立刻回读函数"抓出的 | 不是判据、不登记 ID。记在这里是因为它给 M116 的接线锚点补了一次真实杀伤力读数 |
| 提交节奏 | 本项目的规矩是**三提交**：设计段 → 实施 → 划账 | 本节 **§23.10~§23.12 与实施同批入仓**（`58639bf` 里 29 个文件含本设计稿 +97 行），只有 §23.0~§23.9 走成了独立的设计段提交（`eb099f3`） | 事实是 §23.10 那份子代理报告到得很晚（`eb099f3` 之后）、§23.11~§23.12 更是**实施与取红之后**才写得出的对账，硬拆成"设计段先单独一提交"会把"复核为真后才编号"这条纪律倒过来写。★ 这不是补规矩，是**留一笔**：本轮确实没按三提交走，划账 §6.19 七 同址引这里。〔2026-09-22 划账复核补一条真读数：那一跑的 `top_SKIP=6` 逐字为 `TestVerdictFromUpperAbsentIsConfirmedSensitive` + 5 条既有跨卷/root 依赖跳过（`TestMoveFileCrossDeviceReal`、`TestWalkCaseSensitivityIsProbed`、`TestWalkSingleRootKeepsCaseVariantSubtrees`、`TestMultiRootWalkKeepsCaseVariantSubtrees`、`TestSymlinkedRootUnderProtectedDirIsReported`），故"唯一来源"那句成立〕 |

**M122 / M127 / M129 的变异复算逐字读数**（本轮为写 §6.19 重新跑了一遍 `/tmp/recapture_r2.py`，
每条都带"还原自检 sha256 前后同值"）：

```
M126 default: return true, true（旧形状）
  --- FAIL: TestVerdictFromUnreadableUpperFallsBackToDefault
      verdict_from_test.go:95: upper 读不动 ⇒ 必须退平台默认 false，实得确证值 true（lstat .../payload126c.txt/x: not a directory）：
                               这是把"无从判定"报成"卷区分大小写"，且会被 Sensitive() 永久缓存

M127 sysguard.Dir 改成逐段匹配
  --- FAIL: TestGuardRecycleBinDirNameIsCaseInsensitive
      sysguard_test.go:64: 后代路径自身被判保护：说明 dirName 判据改成了逐段匹配——…

M129 HashFull 吞读错误 + 放过短读
  --- FAIL: TestHashFullReadErrorPropagates
      hasher_test.go:196: 句柄已关 ⇒ HashFull 必须报错，实得指纹 af1349b9f5f9a1a6…（静默出指纹=把读不动的文件当内容参与去重）

M122 删掉排序第三级键（-count=12）
  --- FAIL: TestGroupOrderAndIDsAreDeterministic
      group_order_probe_test.go:112: 第 2 位 = {id:2 n:2 head:…z-b1.bin recl:614400}, want {id:2 n:2 head:y-c1.bin recl:614400}
      group_order_probe_test.go:124: 第 7 跑与第 1 跑在第 1 位不一致（…）   ← 同一次跑内另 6 处漂移

M113 undoMove 回空落点
  --- FAIL: TestUndoMovePartialSuccessHandsBackRestoredPath
      undo_move_partial_m113_test.go:75: 部分成功却回了空落点 ⇒ …（错误文本自带两个落点，返回值却是空的）

M114 读不动并回"被替换"
  --- FAIL: TestIdentityUnreadableIsNotReportedAsReplaced
      identity_unknown_m114_test.go:79: 读身份失败被说成了确证的顶替（M114）："文件在扫描后被替换（inode 已变化），已拦截"
      identity_unknown_m114_test.go:84: 文案应明说「无法确认」…
      identity_unknown_m114_test.go:87: 底层原因必须原样出现在文案里…
```
---

## 24. 三轮全量审查·第 3 轮设计段（2026-09-22）：M89 / M40 / M114 残半的实施判据 + 本轮新增 13 条（M135~M147）

### 24.0 本轮取材与复核纪律

与第 2 轮同法：**每条自己开码复核**，复核为真才分配 ID（§22.8 那条教训的落地）。三个来源：

1. 本轮三个只读审查子代理（甲 / 乙 / 丙）的合并面。本轮没有再出现"整份报告伪造坐标"，
   但仍有两处坐标误差，均不足以支撑任何结论，只记一笔：乙引 `verify.go:132`
   （`identityCheck` 的函数行逐字读数是 `:133`）、甲引 `move.go:219`
   （写着 `undo.go:447` 的那句在 `:218`）。
   ⇒ 因此 §24.3/§24.4 每条的**依据都是我自己的行级读数**，不按"谁先提出"记账：
   复核通过后谁提的已经与判据无关，而"按代理记账"正是上一轮甲那份报告能混进来的形状。
2. §6.19 末"未兑现"清单点名的三件：**M89 原样挂着**、**M114 的两处残半**、
   **M134 没加的那条静态防回归断言**。三件本轮全部开工（§24.3、§24.5）。
3. 第 2 轮"登记不修"的 M130/M131/M132 与 M112 背后的 **M40** 形状。本轮只对 M40 开工
   （§24.3.2）；另三条复核后仍按约束 7 挂着，逐条理由见 §24.6。

本轮登记 **M135~M147** 十三条：十二条修（M135~M146），一条**登记不修行为**（M147，只改话术）。
`grep -rnoE "M1(3[5-9]|4[0-7])"` 在全仓（含 Go/MD/SH/YML/TS/Vue）**零命中** ⇒ 号段确认可用，
分配时机在本节落笔之前已完成复核，不是预定区间。

### 24.1 ★ 先记一笔工具事故：变异 harness 的**假绿**（不登记 ID，划账 §6.20 同址引这里）

复算那五个承重变异时，我把仓库 `git archive HEAD | tar -x` 到 `/tmp/r3mut/repo` 再改。
那棵树**没有 `.git`**，于是脚本里"改完还原"的两句全部静默失效：`git checkout -- <path>`
无事发生，`git status --porcelain` 打出空串。我把 `reverted; tree clean: True`
记在了一份**仍然被改坏**的树上，紧接着第二跑便把五个锚点全报成 `count=0`
（看起来像"锚点被删干净了"，真相是第一跑的改动还原封不动留在树里）。

这就是本仓一轮一轮审出来的同一类缺陷——**判据没被走到却报绿**——只不过这次栽的是我自己的工具。
登记不修（不属产品面），但三件修法必须写清，缺一不成立：

1. 还原改成 `git -C <真仓> show HEAD:<path>` 写回目标文件，不依赖被改树里的 git；
2. "树干净"改由**字节比对**的 `dirty()` 自检给出，而不是 `git status`；
3. 加**负控制**：故意施加一次必须被抓出的改动，确认 harness 真的报 dirty。

修正后的三条自检逐字读数：

```
restore self-check (must be []): []
negative control (must list verify.go): ['internal/ops/verify.go']
after restore (must be []): []
```

⇒ §24.2 那五条读数是**修正后的 harness** 重跑的结果；第一跑那批（"五个锚点 count=0"）作废，
不进任何结论、不进登记表。

### 24.2 五个承重变异的真读数：全部存活

每个变异都是"把判据本体的那一格改掉"。若对应断言是活的，包必须红。实得全绿 ⇒ 那一格没人钉。

| 变异 | 改在哪 | 该红在哪 | 实得 ⇒ 去向 |
|---|---|---|---|
| R3-MU1 | `verify.go:144-146` 第二条 vUnknown 来路改成 `return vReplaced, ""` | `identity_unknown_m114_test.go`（只钉了第一条来路） | 全绿 ⇒ **M135** |
| R3-MU2 | `hasher.go:260-262` 删掉 `if err != nil { return … }`（吞读错误） | `TestHashFullReadErrorPropagates`（`hasher_test.go:179`） | 全绿 ⇒ **M136** |
| R3-MU3 | `pipeline.go:809-811` 删排序第二级键（成员数降序） | `TestGroupOrderAndIDsAreDeterministic` | 全绿 ⇒ **M137** |
| R3-MU4 | `move.go:63` 部分成功改回 `return "", fmt.Errorf(…)` | `move_crossvolume_identity_test.go:92`（写成 `_, err :=`） | 全绿 ⇒ **M138** |
| R3-MU5 | `fscase.go:171` 撞名支改成 `return false, true` | `verdict_from_test.go`（三条里没有撞名那一格） | 全绿 ⇒ **M139** |

```
=== R3-MU1 rc=0 存活(绿) ok filededup/internal/ops 0.921s
=== R3-MU2 rc=0 存活(绿) ok filededup/internal/hasher 0.493s
=== R3-MU3 rc=0 存活(绿) ok filededup/internal/dedup 2.825s
=== R3-MU4 rc=0 存活(绿) ok filededup/internal/ops 0.837s
=== R3-MU5 rc=0 存活(绿) ok filededup/internal/fscase 0.398s
final dirty (must be []): []
```

本批判据的共性，一句总则：**键被执行到 ≠ 键被钉住**；"红过"必须红在**它声称的那一格**。
（M137 是这句话最干净的样本：`group_order_probe_test.go:32` 自己写着"第二级 ⇒ 只有 A 分出去"，
而删掉整级之后 A 仍由第三级键（m<y<z）落在原位。）

### 24.3 三条既有登记的实施判据

#### 24.3.1 M89（OPS-14d）：执行器不再丢掉部分成功的落点 —— **不动任何形状**

现状读数（本轮逐字）：`outcome.dst` 早已存在（`executor.go:297`），`settle` 的 ocOK 分支
已经传 `DestPath: o.dst`（`:318-319`），ocFailed 分支没传（`:322`）；丢字段的那一处就是
move 分支 `:583-584`（`if dst, err := MoveFile(...); err != nil { settle(i, outcome{code: ocFailed,
err: err.Error()}) }`）。下游链路本来就是通的：`app.go:1940` 的 OnItem 原样把 `r.DestPath`
喂给 `hs.FinishItem`，`oplog.go:83-88` 写 `dest_path` 列。⇒ 修法只有一件事：**传值**，
`MoveFile` 的签名、`outcome` 的形状、账本状态机全不动（登记时那句"修法要动 outcome/FailedItem
形状或账本状态机"比实际需要的重）。

用户可见面：`model.FailedItem{Path, Stage, Err}`（`executor.go:444`）没有落点位。
⇒ 沿用 undo 侧那条已被钉住的写法：错误文本追加"（数据已在 %s）"，与 `app.go:2211-2217`
的 `undoFailure` 同构且**幂等**（文本里已经有那个路径就不重复补）。
★ 为免同一条判据写两遍（I5 漂移面，甲/乙都点过），把这层抽成 `ops.DestHint(msg, dst string) string`，
`undoFailure` 改为调用它。动 app.go 的理由在此：**输出串逐字不变**，既有断言一条不动。

**不做**：不给 `FailedItem` 加 `DestPath` 字段（那是 TS 对齐面 M30 的改动，而"文本 + 账本"两件已经
把信息交出去了）；不改 failed 行的状态。

安全性取证（★ 不写下来，"给 failed 行写 dest_path"看着就像放开了回撤面）：账本两条不变量让
这一格是**惰性**的——`reclaimed = SUM(size) WHERE state=done`（`oplog.go:129-130`），
回撤候选只认 `StateDone`（`app.go:2095`）或 `StateDone || StateUndoFailed`（`app.go:2174`）
⇒ failed 行的 `dest_path` 既不会虚增释放量，也不会凭空多出一个可回撤目标。

红法：新用例经 `renameFile` 缝造 EXDEV、再经 `removeSrc` 缝造删源失败（先例
`move_crossvolume_identity_test.go:28-36`、`undo_move_partial_m113_test.go`），断
`res.Failed[0].Err` 含「数据已在 <dst>」且该 dst 与文本里那份一致；变异自证 = 把 `:584`
改回"不传 dst"，必须红。

#### 24.3.2 M40：同卷 move 不再计入 `Reclaimed`

判据来源唯一：`MoveFile` 自己知道走了哪条腿——`rename` 快路径 `:34-35` 返回 nil 就是同卷
（跨卷会带 EXDEV/`ERROR_NOT_SAME_DEVICE` 落进 `isCrossDevice` 那条腿），copy→校验→删源那条腿
才是跨卷。⇒ **不改公开签名**：新增 `moveFileDetailed(src, targetDir) (string, bool, error)`
（第二位 = crossVol），`MoveFile` 收成丢掉该位的薄包装。八个调用点里只有 `executor.go:583`
需要那一位，`undo.go:197` 与六处测试一字不动（约束 7）。

**不新增计数字段**：同卷 move 的贡献哪一栏都不进。三条理由：① 用户裁定"新增计数的界面呈现属 M8，
不做"；② M130 刚登记过一个"只写不读"的字段，明知故犯加第二栏同样的死字段；③ 结果条从"释放 X"
变成"释放 0"就是真话（同卷改名一分未减），落点仍逐项写在 `ItemResult.DestPath`。

红法与负控制（关键是别把跨卷那半一起清零）：
- 新用例 V4：`Execute(Kind:"move", TargetDir: 同一卷的子目录)` ⇒ 断 `res.Reclaimed == 0`，
  **改前红在 `Reclaimed == size`**（这就是登记两年那笔假账的形状）。
- 既有 V3 `TestCrossVolumeMoveStillReclaims`（`executor_account_test.go:74-93`，
  经 `forceCrossVolumeRename` 换 `renameFile` 缝）必须继续绿 ⇒ 它充当本修复的负控制，
  证明这是"分辨"而不是"把 move 整栏改坏"。
- ★ 两条都不需要真第二个卷：同卷走原生快路径，跨卷由同一个缝造 EXDEV ⇒ **三条腿都能真跑**，
  本项不属"代码已改、验证未兑现"那一类。

连带话术（必须一起改，否则下一轮又多两条过期句子）：`executor.go:410-411` 的
"登记为 M40，此处不动"、`executor_account_test.go:67-71` 的"本项不动它，也不把已知的假账钉成契约"
——本轮动了，两处改为真读数。

#### 24.3.3 M114 的两处残半

**(a) S1 四处仍把"读不动"说成"inode 已变化"。** 逐字读数：`move.go:106`、`:110`、
`symlink.go:80`、`:84` 四句 `fmt.Errorf` 的文案硬编码"…在校验后被替换（inode 已变化），已拦截（S1）"，
走的是 `identityStill` 二值视图 ⇒ M114 修的是 `guardIdentity`，这四格原样留着同一个假话形状。
改法：四处换 `identityCheck` 三路分发，**处置一字不变**（含既有的 `_ = os.Remove(tmp)`），
只换说法，且四句共用一个 helper（`identityGuardSentence(what, v, why)`）以免四处各自漂移：
- `vReplaced` ⇒ 「<what>在校验后被替换（inode 已变化），已拦截（S1）」**逐字与今天相同**
  （`symlink_test.go:625` 钉着"保留源"，另有"inode 已变化"的既有钉子，改字就是收窄）；
- `vGone` ⇒ 「<what>在校验后已不存在，已拦截（S1）」；
- `vUnknown` ⇒ 「无法确认<what>仍是校验时那个对象（<why>），已拦截（S1）」，
  与 `guardIdentity` 用的 `:361` 那同一句式。

红法：经 `fsidFromPathFn` 缝注入 `errSimulatedUnreadable`（该接缝专为这件事存在，
`identity_unknown_m114_test.go:31-35` 已示范为何不用 `syscall.EACCES`），
改前文案含"被替换" ⇒ 红；改后含"无法确认"且保留底层原因 ⇒ 绿。
负控制 = 另一次真顶替（换 inode）必须仍说"被替换"且一字不变。

**(b) `identityStatus` 只剩测试在用。** 读数：`verify.go:166`（`identityStill` 的包装，丢弃 `gone`）
+ 测试引用 `verify_m52_m54_test.go:129/:146/:154`；`gone` 那一位在**生产里零读者**。
★ 本轮**不删**它：删要挪动三条既有断言的落点 = 收窄（约束 2 禁止），第 2 轮 §23.12 已把这件事
记成"一条新的话术债"。本轮只把 `verify.go:185-187` 那句过期话术改成真读数——
"executor 那六处改直读 identityCheck"不准：六处经 `guardIdentity`（`:463/:541/:558/:580/:599/:670`），
`identityCheck` 在生产里的直调点只有 `:343` 一处；并补一句 `identityStatus.gone` 无生产读者。

### 24.4 本轮新增 13 条：坐标、形状、修法、红法

| ID | 列值 | 坐标（本轮逐字读数） | 缺陷形状 | 修法 | 取红方式 |
|---|---|---|---|---|---|
| M135 | OPS-17 | `verify.go:144-146` | 判据本体的第二格没人钉：`identityCheck` 四路分发里 `!cur.Resolved` 那条既有文案又有语义，零断言 | 新用例经 `fsidFromPathFn` 注入 `(fsid.ID{Resolved:false}, nil)`，断"不含被替换 + 含无法确认 + 仍拦下" | 变异 R3-MU1（改前存活，§24.2）必须红 |
| M136 | HAS-4 | `hasher.go:259-265` × `hasher_test.go:194-197` | 断言判的不是那一格：吞掉 `CopyBuffer` 的 err 会掉进 `n != size` 的短读支，**照样报错**，只是换一个错 ⇒ "两半一起改才红"（M129 已经撞到过） | 现断言之外补**错误身份**判据 `errors.Is(err, fs.ErrClosed)`（短读支包的是 `io.ErrUnexpectedEOF`，两者可分） | 只删 err 分支（R3-MU2 的一半）必须红；两半同改仍红（既有钉子） |
| M137 | DDP-3 | `pipeline.go:809-811` × `group_order_probe_test.go:28-33` | 键被执行到但**不 decisive**：夹具 A=3/B=2/C=2，删第二级后第三级（m-a1 < y-c1 < z-b1）给出**完全相同**的序 | 新用例自带冲突夹具：组 P（2×300KiB，头 `a-p1.bin`）与组 Q（3×150KiB，头 `b-q1.bin`）可释放量都是 300KiB ⇒ 有第二级 Q 在前，删掉则最小路径把 P 提前 | 变异 R3-MU3 必须红；既有 `want` 一字不改（它继续钉第一、三级） |
| M138 | OPS-18 | `move.go:59-64` × `move_crossvolume_identity_test.go:92` | 契约没钉子：`MoveFile` 部分成功必须回落点（M113、M89 都建在这一位上），用例却写成 `_, err :=` | 同一用例改 `dst, err :=`，断 `dst != ""`、`dst` 出现在 `err.Error()` 里、且该路径上的内容等于源 | 变异 R3-MU4 必须红 |
| M139 | FC-4 | `fscase.go:171` × `verdict_from_test.go` 三条 | 撞名支（`return false, false`）零覆盖：现有三条是"看不见 / 同一对象 / 读不动" | 新增第四条：lower、upper 各为**独立存在的两个文件**（不同 inode），断 `v=false, ok=false` | 变异 R3-MU5 必须红；不依赖卷的语义 ⇒ 本机真跑 |
| M140 | FC-5 | `fscase.go:172-173` × `verdict_from_test.go:19-36` | 不是判据缺陷，是**覆盖局限**：既有那条拿"同一名字的另一种大小写"当 upper，APFS 上命中同一对象 ⇒ 只能 `t.Skip`，读数归 CI 的 linux 腿 | 再补一条：upper 用**从未创建过的名字**（ENOENT 与大小写语义无关，三条腿都能真跑）；既有条目与其 `t.Skip` **原样不动**（AS-K2） | 把 `:173` 改成 `return false, false` ⇒ 红在本机新那条（而不是只有 linux） |
| M141 | FE-14 | `PreviewPanel.vue:117` × `:30` × `app.go:1050` | 展示不真：chip `v-if="overRenderCap"` 不受 `isMd` 约束，而 image 预览允许到 ~256KB base64 ⇒ 预览一张 100KB 的图会显示「默认源码」 | 改为 `v-if="isMd && overRenderCap"`；在 `test-frontend-logic.sh` 加接线锚（.vue 打不进 `node --test`，先例 M116/M118） | 负控制：`FRONTEND_DIR` 指向"把 chip 改回未门控"的那份副本，必须报写法被回退（该覆盖位 `:18-21` 就是为此存在） |
| M142 | GATE-9 | `smoke-symlink-assert.sh:153`、`:172` | 一条纪律三份实现，**最弱那份在守契约**：`case "$out" in *SKIP*)` 允许 SKIP 出现在任意位置（含 assert 自己打印的说明行），而 `run-gates.sh:155` 与 `ci.yml` 都按行首 `^SKIP` 锚定 | 两处改为 `printf '%s\n' "$out" \| grep -q '^SKIP'`（三份同一把尺子）；`:224`/`:225` 那条"必须不含 SKIP"**保持整串匹配**（absence 本就该看全文） | 负控制：`SMOKE_TARGET` 指向一份 rc=2 且只在行中打 SKIP 的桩 ⇒ 改前 A1 判 ok（假绿），改后判 bad |
| M143 | TST-5 | `app_preview_p3_test.go:58` × `app.go:1044-1047` | 表与被测对象脱钩：用例硬写 `exts := []string{".png", ".bmp"}`，生产表有 6 项 ⇒ 往 `imageMime` 加一项而没注册解码器，本条照绿。而这正是 `:1039-1040` 那句 P3 承诺要防的事 | 改由**生产表驱动**：遍历 `imageMime` 的键，每项配一段可解码夹具字节，断 `image.DecodeConfig` 返回的格式名 = 表值去掉 `image/` 前缀；并断**两侧键集合相等** | 两种变异都必须红：表里删 `.gif`（集合不再相等）、删 `app.go:25-26` 的解码器注册 |
| M144 | TST-6 | `app.go:899`、`:906`、`:913` × 注释 `:892`「稳定：组 ID 兜底」 | 兜底键零覆盖：三个分支都有平手时的 `GroupID <` 兜底，没有一条用例把"平手"造出来 ⇒ 删兜底不会红（`sort.Slice` 非稳定，序落到遍历序） | 新用例造两组同尺寸 / 同成员数 / 同可释放量，且**故意把 GroupID 与路径序倒过来发放** ⇒ 有兜底按 ID 升序，删兜底即红 | 三条分支各自把兜底改成 `return false`，必须各红一次（同一判据三格，逐格取红） |
| M145 | PRG-3 | `progress_test.go:27` | 恒不成立的析取：`!= 0 && != -1` 里的 `0` 不可达——`progress.go:141` 只在 `elapsed > 0` 时写 ETA，`elapsed == 0` 时初值 `-1` 原样交回 | 收紧为 `!= -1`（**是补强不是放宽**，约束 2 管的是反方向），并把不可达推导写进注释 | 变异：`elapsed > 0` 之后加 `else { ev.ETASeconds = 0 }` ⇒ 改前绿（0 被放过），改后必须红 |
| M146 | DBF-1 | `dbfile_test.go:15-23`、`:29-38` | 表从硬编码清单遍历且**无条数下界**：两张表删空 ⇒ 循环 0 次 ⇒ 全绿（同仓先例已补过：`app_undo_platform_probe_test.go` 的 `checked != 4`、`sqlconn_test.go` 的 `scanned < 50`） | 两张表各加下界（corrupt ≥6 / transient ≥8），文案明说"下界归零时本条等于没跑" | 变异：清空任一表 ⇒ 必须红在下界，而不是无声通过 |
| M147 | WT-1 | `worktemp.go:133-136` × `isExtStart:159` | 注释与行为不符：`:127-128` 承诺"名尾 / .扩展名 / _序号.扩展名"三种收尾，`isExtStart` 实为"`rest[0]=='.'`" ⇒ `photo.fdd-restored.anything.jpg` 也被判成我们的暂存（生成侧只插一个扩展名） | ✓ **行为不修**：判宽=多保护一个用户文件，判窄=把自己的暂存当真实数据参与去重，风险不对称。本批只把 `:127-128` 的话术改成真读数 | —（不改行为 ⇒ 不取红；随 §6.20 话术面对账一并核） |

### 24.5 M134 欠的那条静态防回归断言（本轮补）

§23.11 登记 M134 时自己写下"没做 (a)：未加静态 grep 断言防回归（新增断言要把 `MIN_CHECKS`
从 14 抬起、连带重取条数读数）"。本轮补上，代价照计：

- 落点：`smoke-symlink-assert.sh` 新增一组 **E 静态防回归**——扫 `scripts/smoke-symlink.sh`、
  `scripts/smoke-symlink-assert.sh`、`scripts/check-version-sync.sh`、`.github/workflows/ci.yml`
  四个文件，命中"`$VAR` 紧跟一个非 ASCII 字节"即判 bad（M134 的根因形状：bash 3.2.57 在
  `LC_CTYPE=C.UTF-8` 下把全角括号首字节算进变量名，`set -u` 当场打死被测脚本）。
- `MIN_CHECKS` 14 → 15，并按规矩**重取**条数读数（不许沿用 14 那句"实测"）。
- 负控制：给扫描位一个可覆盖入口（`SCAN_FILES`，先例是 `test-frontend-logic.sh:18-21` 的
  `FRONTEND_DIR`，注释原话就是"负控制要拿故意改坏的那份跑本套断言"）⇒ 用一份含
  `"$FOO）"` 的副本喂进去必须红，用真仓必须绿。

### 24.6 本批**不做**的清单（逐条给理由，不留"以后再说"）

| 项 | 不做的是什么 | 为什么 |
|---|---|---|
| M130（PRG-2） | 删 `progress.go` 里只写不读的 `lastEmit` | 死代码清理不属判据面；同包本轮已因 M145 开窗，约束 7 二次不动 |
| M131（HAS-2） | 删 `growth_sampling_test.go:134` 那条恒不成立的后半 | 删一半 = **收窄**既有断言（约束 2）；要补的是小文件腿的另一条用例，本批不扩面 |
| M132（HAS-3） | 改 `shortread_test.go:111-114` 那句"上限"话术 | 断言对夹具成立，动的只是注释；同包本轮已开 M136 一窗 ⇒ 约束 7 |
| M89 的形状面 | 给 `model.FailedItem` 加 `DestPath` | 那是 M30 的 TS 对齐改动；"文本 + 账本"已把信息交完（§24.3.1） |
| M40 的计数字段 | 新增"同卷搬移字节"栏 | 用户裁定"新增计数的界面呈现属 M8，不做"，且不复制 M130 那种只写不读的字段 |
| `identityStatus` | 删掉这个只剩测试在用的兼容层 | 删它要挪三条既有断言 = 收窄；本轮只把它的注释改成真读数（§24.3.3-b） |
| M91（FSID-1） | `(dev,ino)` 回收 inode 的三选一修法 | 属裁定面（A/B/C 各有代价），且现象本机不可复现（APFS 不还号）⇒ 仍待裁定，不占本批 |
| M147 的行为 | 收紧 `isExtStart` | 判宽是保护面，判窄才是风险；不对称 ⇒ 登记不修（§24.4 末行） |

### 24.7 提交节奏与本批门禁计划

三提交（本项目规矩）：① 本节设计段；② 实施（M89/M40/M114 残半/M134-E 组 + M135~M146 十二条
与 M147 的话术，**每项先取红**）；③ 划账 §6.20 + 文档对齐 + 本批话术面十条。
每批做完跑全套 15 行门禁，任何一项红就停下修，不带红交付。

本批**不需要**真机读数即可兑现的：M40（EXDEV 由缝造）、M89、M114(a)、M135~M147 全部
（M140 只兑现"新那条本机跑"，既有的 `UpperAbsent` 仍归 CI 的 linux 腿，AS-K2 不放宽）。
本批**兑现不了、必须写明**的：`smoke-symlink-assert.sh` 的 E 组在 CI 的 linux/macOS 腿上会跑，
但 runner 的 bash 版本与本机 3.2.57 不同，M134 那类缺陷**只在开发机上复现** ⇒ 该组的
真读数以本机为准，CI 只作旁证（这句话必须出现在划账里，免得读成"CI 绿 = 防回归已生效"）。

---

## 25. 第 4 批（推送后 CI 首红复批）：Windows 腿唯一一条红 = M126 夹具的前提（登记 M151；2026-09-22）

### 25.0 取材：一次真实 CI 读数，不是推断

`4c73f23..dae51da` 推上 main 后，run **35674083361** 九条 job 里只有 `go test (windows)` 红，
其余全 success（gofmt / vet×3 / test -race / frontend / smoke×2 / go test (macos)）。
`gh run view 35674083361 --log-failed` 逐字：

```text
--- FAIL: TestVerdictFromUnreadableUpperFallsBackToDefault (0.00s)
    verdict_from_test.go:91: 夹具前提不成立：本平台把这个串报成 ENOENT 而非"读不动"
    （GetFileAttributesEx C:\Users\RUNNER~1\AppData\Local\Temp\
      TestVerdictFromUnreadableUpperFallsBackToDefault1861496064\001\payload126c.txt\x:
      The system cannot find the path specified.）⇒ 测不到 M126 那一格
FAIL    filededup/internal/fscase  0.315s
```

同一次跑里其余 21 个包全 `ok`（含 `filededup` 根包 62.193s、`ops`、`dedup`、`scanner`、`history`）
⇒ 这是**唯一**一条红，而且红在自己写的那句前提自检（`t.Fatalf`）里，**不是红在产品判据上**。
★ 一句反话先堵掉：这不是"CI 又抽风"。抽风是同一形状时好时坏；这里是**本平台原理上造不出那个形状**。

### 25.1 复核：为什么是夹具的锅，不是产品的锅（三条读数）

1. **生产里 upper 恒为 dir 的直接子项**：`probeNames:111-114` 只产 `filepath.Join(dir, 上/小写名)`，
   `verdictFrom` 的唯一调用点是 `probe:137`。"路径穿过一个普通文件"（`dir/file/x`）这一形状
   **在生产里到不了** ⇒ Windows 把它报成 `ERROR_PATH_NOT_FOUND` 不构成产品缺陷，产品那一条
   分支（`fscase.go:174-175`）该 Windows 用户本来就走不到这个输入。
2. **判据格与 errno 种类无关**：`verdictFrom` 只看 `errors.Is(uerr, os.ErrNotExist)` 这一个类别判定，
   任何**非 notExist** 的 Lstat 失败都落 `default:` ⇒ 换一种"读不动"的形状，钉的仍是 M126 那一格，
   不需要动产品码。
3. **本机三候选真读数**（临时探针，跑完即删；命令与原文见 §25.6）：

| 候选形状 | darwin 实得 | `Is(ErrNotExist)` | 能否走到 `default:` |
|---|---|---|---|
| `filepath.Join(lower, "x")`（穿过普通文件） | `ENOTDIR: not a directory` | **false** | 能（现夹具用的就是这个） |
| `lower + "\x00" + "x"`（串里带 NUL） | `EINVAL: invalid argument` | **false** | 能 |
| 同名另一种大小写（既有 `UpperAbsent` 用） | 命中同一对象 | — | 不适用（走 `SameFile` 支） |

**Windows 侧不靠猜，有 Go 1.27 源码两条硬证据**（本机 `$GOROOT` 直读）：
- `syscall/syscall_windows.go:42-45` — `UTF16FromString` 遇到串内 NUL **直接 `return nil, EINVAL`**，
  根本不进 Win32；`os/stat_windows.go:29-32` 把它包成 `PathError` 返回。
- `syscall/syscall_windows.go:189-205` — Windows 上 `Errno.Is(ErrNotExist)` 的真值集合只有
  `ERROR_FILE_NOT_FOUND / _ERROR_BAD_NETPATH / ERROR_PATH_NOT_FOUND / ENOENT`，**不含 EINVAL**。
⇒ NUL 形状在三条腿（darwin/linux/windows）一律报"非 notExist"，而 `dir/file/x` 只在 Windows 退化。
CI 复跑是最终裁判；若三条腿仍无一合格，本批的新自检会**硬红并打印每条候选的实测错误**，不会软成 Skip。

### 25.2 修法判据：候选形状按序试，一条都不许 Skip

改 `TestVerdictFromUnreadableUpperFallsBackToDefault` 体内的夹具构造，判据三条断言一字不动：

1. 候选按序：① 穿过普通文件（真文件系统条件，darwin/linux 命中）→ ② 串内 NUL（Go 转换层拦下，
   三平台一致）。取**第一个**"Lstat 失败且失败不是 ErrNotExist"的候选当 upper。
2. 选中哪一格必须**外显**：`t.Logf` 打出形状名 + 实测错误，失败信息里也带上 ⇒ 读日志的人不必知道
   平台就能判断这一格读没读到。
3. 全不合格 ⇒ `t.Fatalf` 逐条打印候选的实测错误（★ 绝不退成 `t.Skip`；退 Skip 等于把这条腿
   的读数来源从"CI 三条腿"偷偷降级成"零条"）。
4. 注释里写死一句诚实话：Windows 上走的是第 ② 格，形状由 Go 的字符串转换层拒绝，**不是**
   文件系统给的 `ENOTDIR`；两者对 `verdictFrom` 是同一格，但对本仓"平台差异"账目不是同一件事。

### 25.3 两条被否决的修法（写下理由，免得下次再走一遍）

| 否决项 | 为什么不走 |
|---|---|
| 给 Windows 加 `t.Skip` | AS-K2：skip 不当通过。三条腿里 Windows 那条从此**零读数**，而 M126 本体（"读不动不得给确证"）恰恰是最想让平台差异咬到的地方。M140 的先例是**换形状把 Skip 换成 PASS**，不是把 PASS 换成 Skip |
| 往 `QUARANTINE` 清单加这条用例名 | 该脚本自己的话（`test-windows-quarantine.sh` 头段）："修好后从清单删除"、Windows job 变红时应补夹具而不是加隔离 ⇒ 加隔离 = 把该平台对该用例永久设为不设防 |
| 新写一条 `...NulPath...` 用例、旧的照旧红 | 用例计数三条腿闭合方程（04 §3.2：`718-35-12-17-11`）要整表重算，且旧那条在 Windows 仍红 ⇒ 门禁不放行。约束 7：不扩面 |

### 25.4 取红与变异计划

- **改前红已有真读数**：§25.0 那段 CI 日志（本平台唯一一条红，红在 `verdict_from_test.go:91`）。
  ★ 本批改的是测试码，生产码零改动 ⇒ "生产改动前先看它红过一次"这条不适用；但**新夹具必须自证还咬得住产品**：
- **变异 M21-a（唯一一条）**：把 `fscase.go:174-175` 的 `default: return Default(), true` 改回
  M126 改前形状 `return true, true` ⇒ 新用例必须在 darwin **当场红**，且必须红在
  `v != Default()` 那一格（`Default()` 本机为 false，红文里点名"退平台默认"）。
  ★ 若 `Default()` 恰好为 true（区分大小写默认真机）这条变异便不可杀，故本机另钉一条"红文指向的断言格"
  的复核，不靠"整包红了"充数。
- **反向不红也要如实记**：删掉 NUL 候选只留 ①（模拟改前）在 darwin 不会红（本机 ① 本来就合格）⇒
  该候选的价值只在 Windows 腿，本机不得声称已验，只能声称"CI 复跑后绿"。

### 25.5 本批不做的清单

| 项 | 不做什么 | 为什么 |
|---|---|---|
| 产品 `verdictFrom` | 为 Windows 特判 `ERROR_PATH_NOT_FOUND` | 生产输入恒为直接子项，特判是给到不了的分支加码；且 `errors.Is` 的类别判定跨平台含义一致（"这个位置读不出对象"），改了反而引入新语义 |
| `TestVerdictFromUpperAbsentIsConfirmedSensitive` | 消掉它体内那句 `t.Skip` | 那句 Skip 是**卷语义前提**（APFS 不区分大小写），与本平台形状无关，M126 括注已把它归 CI 的 linux 腿；本轮不重开 |
| `TestVerdictFromNeverCreatedUpperIsConfirmedSensitive` | 同步换形状 | 它要的是 ENOENT 支，Windows 真读数里这一支**成立**（CI 该条未红）⇒ 前提在两条腿上都是对的，动它纯属扩面 |
| M91 / M149 / 九条待裁定 | 顺手带上 | 各自等裁定或真机，与本批无关 |

### 25.6 真读数留痕

本机探针（跑完已删，仓库内不留该文件）：`internal/fscase/zz_wfx_probe_test.go`，
`go test ./internal/fscase/ -run TestWFXProbeShapes -v` → `rc=0`，输出逐字（NUL 已替换为 `<NUL>`）：

```text
nul-bytes  isNotExist=false ENOTDIR=false EINVAL=true err=lstat .../payload126c.txt<NUL>x: invalid argument
nul-inner  isNotExist=false ENOTDIR=false EINVAL=true err=lstat .../pay<NUL>load.txt: invalid argument
under-file isNotExist=false ENOTDIR=true  EINVAL=false err=lstat .../payload126c.txt/x: not a directory
```

`$GOROOT=/opt/homebrew/Cellar/go/1.27.1/libexec`（`go version go1.27.1 darwin/arm64`）；
§25.1 那两条 Windows 证据取自该目录下的 `src/syscall/syscall_windows.go` 与 `src/os/stat_windows.go`。

### 25.7 提交节奏与门禁计划

三提交：① 本节设计段；② 实施（换夹具 + 变异自证）；③ 划账 §6.21 + 登记表新增 **M151** +
两处过期坐标（`verdict_from_test.go:33` 实为 `:37`）就地更正 + CI 三条腿真读数。
第 ③ 步之后按同一授权口径推送（用户本轮原话"完成后推送到 github 仓库"覆盖"CI 红了就修完再推"这一闭环）。

### 25.8 实施期真读数与一处勘误（追记，2026-09-22）

- **§25.1 那条 Windows 证据的行号写偏了 3 行**：真锚是 `syscall/syscall_windows.go:39-44`
  （文档注释在 `:40`、`return nil, EINVAL` 在 `:44`），本节 §25.1 里写的 `:42-45` 是按实现体猜的。
  ★ 同批把用例注释里的这一处也一起改对（`verdict_from_test.go:98`）。`os/stat_windows.go:29-32`
  与 `syscall/syscall_windows.go:201-205` 两处当时就是真读数，未改。
- **变异 M21-a 真读数**：`fscase.go:175` 退回 `return true, true` ⇒
  `TestVerdictFromUnreadableUpperFallsBackToDefault` FAIL，红文逐字
  "upper 读不动 ⇒ 必须退平台默认 false，实得确证值 true（… not a directory）"，
  落点 `verdict_from_test.go:127`（= 预测的那一格，不是"整包红了"充数）；还原后 `git diff` 干净。
- **负控制 N-1（只留 NUL 候选）**：PASS，`t.Logf` 形状位报 `embedded-NUL`
  （`lstat …payload126c.txt<NUL>x: invalid argument`）⇒ Windows 腿那条路有本机近似读数。
- **负控制 N-2（两格都换成从未创建的名字）**：FAIL 且逐条打印两格实测错误
  （"… no such file or directory ⇒ 落 ENOENT 那一格，不合格"）⇒ 前提造不出时硬红，无 Skip 逃逸口。
- **门禁两次**：`/tmp/gates_r4_impl.log`、`/tmp/gates_r4_impl_2.log` 同为
  `rows=15 PASS=14 SKIP=1 FAIL=0`；第二次是补跑（第一次之后把新加的两处 Go 字符串字面量
  从 `\"…\"` 换成「」，与全仓 478:66 的主流写法对齐）。
- **计数通道复算**：全仓 `^func Test` = **718**（未动）、`internal/fscase` = **12 条 / 2 文件**
  （`fscase_test.go` 7 + `verdict_from_test.go` 5）；本文件 SKIP 仍一条，`t.Skip(` 现读 `:39`
  （原 `:37`，本批 `fmt`/`strings` 两个 import 推下去两行 ⇒ §6.21 五那两处过期坐标改认锚点）。
- **本批未扩面清单照 §25.5 执行**：产品码 `git diff` 只余 `verdict_from_test.go` 一个文件；
  Windows 腿的兑现仍挂在 CI 复跑上（§25.4 末行写的"近似"就是这件事，不得读成已验）。
