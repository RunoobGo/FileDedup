# 设计文档：跨卷软链接合并（SymlinkMerge）

> **历史设计存档（2026-09-20 实施完成）**：本文是**设计阶段**的规格快照，
> 已于同日按 §7 的十个步骤实施完毕，**不再随代码演进更新**。
> 因此本文与当前实现存在下列**已记录的偏差**，阅读时以右侧来源为准：
>
> | # | 本文的写法 | 实际实现 | 偏差原因 |
> |---|---|---|---|
> | 1 | §3.7「判定依据：`model.FileEntry.VolumeID`（已有字段）」 | 前端**拿不到** `VolumeID`（`FileView` 从不暴露它，`grep -rn VolumeID frontend/src` 为空）。改为后端新增 `FileView.Volume` / `VolumeResolved` 两个字段下发（`app.go fileVolume()`），前端只做相等比较 | 原写法默认前端能访问核心数据结构，实际上 Wails 桥接只暴露契约视图；且 Windows 盘符 ≠ 卷（挂载点/UNC 会让路径前缀判错），判定必须由后端做 |
> | 2 | 未提及 | 新增 `ops.AggregateWarnings` + 失败阶段标记 `symlink-privilege` | §4 的 R2 只说了"给出可操作指引"，未预见**逐条重复长文案**会让结果页不可读（200 个文件即 200 条相同指引）。归一化为一条汇总 |
> | 3 | §3.7「ResultsView 记录明细需显示链接 → 目标路径」 | 实现为 `destSummary` 按 `it.isSymlink` 分流（`软链接 → 目标` / `链接到 目标`），并新增后端 `ops.SymlinkStatus` + `OpRecordItem.{isSymlink,dangling}` 逐条标注 | 前端无法 stat 文件系统；且 `kind` 是整笔操作的属性，无法表达"这一条执行失败了、原位其实没东西" |
> | 4 | §3.6 仅描述 `undoSymlink` 的正常路径 | 实现额外包含**崩溃残留自检**（原位已非链接但内容等于记录哈希 → 视为已还原）与"原位被换成第三方文件"的拦截 | 与 `undoHardlink` 的三重防线对齐时发现：回撤中途被杀会留下"已删链接、只差改名备份"的残局，若不识别会让该条目**永久无法回撤** |
> | 5 | §7 步骤 4「执行器接入」未提会计 | 新增 `model.OpsResult.SymlinkedBytes` 独立字段 | 若并入 `Reclaimed`，UI 就无法把"软链接确实释放了一整份空间"与"链接有悬空风险"分开表述 |
> | 6 | §7 步骤 7「前端」未提无权限环境的降级 | 新增：权限失败时前端结果条以「权限 N」标签呈现（`warnLabel` 按内容区分"权限类指引"与"临时文件残留"） | 两类内容性质不同（一个必须提权重试、一个可自行清理），共用「提示」标签会让用户按错的提示行动 |
>
> **未按本文实现且被明确否决**：无。§0 的"明确不做"三条全部遵守。
>
> **实施后仍未验证**：Windows 真机的四种权限/卷组合（见 `docs/04` §6.5 新增开放项）。
> 本文 §5 的测试计划全部在 Linux 上以注入点覆盖，**平台层 `createSymlink` 的真实
> Win32 调用路径未经真机执行**。
>
> **现行行为口径**：以 `docs/09-用户手册.md`（§6.3.2）与 `docs/04-开发与测试计划.md`（§6.5）为准。

- 日期：2026-09-20
- 版本目标：0.5.0 内增量（版本号不变）
- 状态：**已实施**（2026-09-20）；设计阶段与需求方澄清：跨卷去重为真实痛点；环境为管理员且可提权
- 前置决策：**仅跨卷启用**；同卷一律硬链接（见 §1.3）

---

## 0. 需求与已确认语义

| # | 需求 | 已确认语义 |
|---|------|-----------|
| 1 | 跨卷重复文件去重 | 硬链接**不能跨卷**（`os.Link` 跨卷必失败）。当重复组跨越不同卷时，提供"软链接合并"作为唯一可行的"保路径、省空间"手段 |
| 2 | 保持路径可用 | 被合并项的原路径**必须继续可访问**（需求方真实场景：某软件在 D 盘放了数据文件，F 盘有副本；删 D 盘那份会导致软件找不到文件）。软链接指向保留项，原路径照常可读 |
| 3 | 同卷不退化为软链接 | 同卷场景一律走既有硬链接；**软链接不提供为通用选项**，避免平白引入悬空风险 |
| 4 | 可回撤 | 复用既有 `BeginOp`/`OnItem`/回撤体系。回撤 = 删除软链接 + 还原备份的原文件 |

**明确不做**（本轮范围外）：

- 目录软链接 / Junction（文件去重不需要）
- 指向网络路径（`\\server\share`）：`SymlinkEvaluation` 默认禁用远程→远程与远程→本地，且悬空风险高
- 自动推断是否该用软链接（由用户在跨卷场景下显式选择）

---

## 1. 现状关键事实（设计依据）

### 1.1 硬链接的跨卷天花板

`internal/ops/move.go:HardlinkMerge` 用 `os.Link(keep, tmp)` 建链接。
`CreateHardLinkW` 的同卷限制是**文件系统语义**，无法绕过：

- 跨卷 → `CreateHardLinkW` 失败（Windows）；unix `link(2)` 返回 `EXDEV`
- 现有错误文案已含"可能跨卷"（`move.go`：`硬链接失败（可能跨卷或权限）`）

### 1.2 扫描器**不跟随**符号链接（保持）

`internal/scanner/scanner.go:211`：

```go
if typ&fs.ModeSymlink != 0 {
    continue // 符号链接：不跟随
}
```

这是**不可关闭的内置行为**，理由有二（`docs/09-用户手册.md` §内置行为）：

- 避免**环路**（A→B→A 导致遍历无限递归）
- 避免**重复计数**（同一物理文件被链接多次访问）

**本设计不改这一条。** 新增的软链接在下次扫描中仍被跳过——这正是期望行为
（它指向的文件已被保留项覆盖，不该再成为一个"重复项"）。

### 1.3 与硬链接的适用边界（核心决策）

| 场景 | 采用 | 理由 |
|---|---|---|
| **同卷** | 硬链接 | 零权限门槛、引用计数保护（删任一名数据仍在）、无悬空风险 |
| **跨卷** | 软链接 | 硬链接无解；软链接是唯一"保路径 + 省空间"手段 |

> **不得**把软链接作为同卷的可选项。同卷用软链接是净损失：引入悬空风险、
> 增加管理员权限门槛，换不到任何额外能力。

### 1.4 身份校验体系（复用）

既有三道防线（`internal/ops/verify.go`、`undo.go`）：

- `identityStill(path, id)` — `fsid.FromPathNoFollow`（不跟随链接）比对
- `verifyHardlinked(keep, dup)` — 终局复核两路径是否同一文件
- `ResolveKey` → `{VolumeID, FileIndex}` — 阶段 1.5 FileKey 去重

**软链接需要新增一个"解析目标"的校验**（`identityStill` 对软链接会取到**链接自身**
的身份，而非目标），见 §3.3。

### 1.5 回撤体系（复用）

`internal/ops/undo.go` 按 `it.Kind` 分派（`:66-70`）：

```go
case "trash":   ...
case "move":    ...
case "hardlink": ...
```

新增 `case "symlink"`。`BeginOp`/`OnItem`/`Finalize` 逐项落账机制可直接复用
（`app.go` 侧无需改动）。

### 1.6 执行器分派点

`internal/ops/executor.go`：

- `:318` 操作类型白名单：`case "trash", "delete", "move", "hardlink":`
- `:424` `case "hardlink":` 分支
- `:284` `op.Kind == "hardlink"` 特判（结果聚合）

新增 `"symlink"` 需同步这三处。

---

## 2. 总体架构

```
┌─ frontend ─────────────────────────────────────────────┐
│ ResultView：跨卷组显示"软链接合并"按钮（同卷不显示）    │
│   顶部警示条：悬空风险 + 需管理员权限                    │
│ RecordsView：软链接操作可回撤，明细含"链接→目标"         │
└───────────────┬────────────────────────────────────────┘
                │ wails.ts 手写桥（OpsResult 已有 Warnings 通道）
┌───────────────▼────────────────────────────────────────┐
│ app.go  ExecuteOperation（复用 BeginOp/OnItem/Finalize）│
└───────────────┬────────────────────────────────────────┘
                │
┌───────────────▼────────────────────────────────────────┐
│ internal/ops/executor.go                                │
│   op.Kind == "symlink" → SymlinkMerge(keep, dup, ...)   │
└───────────────┬────────────────────────────────────────┘
                │
┌───────────────▼────────────────────────────────────────┐
│ internal/ops/symlink.go（新增）                         │
│   SymlinkMerge  —— 骨架照搬 HardlinkMerge 的 5 步安全模式│
│   verifySymlinked —— 终局复核（解析链接并比对目标身份）  │
└───────────────┬────────────────────────────────────────┘
                │
┌───────────────▼────────────────────────────────────────┐
│ 平台层（新增）                                          │
│   symlink_windows.go  CreateSymbolicLinkW + 提权失败映射 │
│   symlink_unix.go     os.Symlink                        │
└─────────────────────────────────────────────────────────┘
```

---

## 3. 详细设计

### 3.1 平台层：创建软链接

**`internal/ops/symlink_unix.go`**（新增）

```go
//go:build !windows

package ops

import "os"

// createSymlink 在 linkPath 处创建指向 target 的符号链接。
// unix 上无特权要求；跨卷、跨文件系统均可。
func createSymlink(target, linkPath string) error {
    return os.Symlink(target, linkPath)
}
```

**`internal/ops/symlink_windows.go`**（新增）

```go
//go:build windows

package ops

import (
    "fmt"
    "syscall"
)

var (
    modkernel32            = syscall.NewLazyDLL("kernel32.dll")
    procCreateSymbolicLinkW = modkernel32.NewProc("CreateSymbolicLinkW")
)

const (
    // SYMBOLIC_LINK_FLAG_FILE：目标是文件（而非目录）
    symlinkFlagFile = 0x0
    // SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE：
    // 开发者模式下允许非提权创建；未开启开发者模式且未提权时该标志会
    // 返回 ERROR_INVALID_PARAMETER(87)，需去掉后重试（见下）
    symlinkFlagAllowUnprivilegedCreate = 0x2
    errPrivilegeNotHeld   = syscall.Errno(1314) // ERROR_PRIVILEGE_NOT_HELD
    errInvalidParameter   = syscall.Errno(87)   // ERROR_INVALID_PARAMETER
)

// createSymlink 创建指向 target 的文件符号链接。
//
// 关键：先带 ALLOW_UNPRIVILEGED_CREATE 尝试（开发者模式可用），
// 失败于 ERROR_INVALID_PARAMETER 时说明该 Windows 版本/策略不认这个标志，
// 去掉后重试（走提权路径）。最终仍失败于 1314 则给出可操作提示。
func createSymlink(target, linkPath string) error {
    t, err := syscall.UTF16PtrFromString(target)
    if err != nil { return err }
    l, err := syscall.UTF16PtrFromString(linkPath)
    if err != nil { return err }

    flags := uintptr(symlinkFlagFile | symlinkFlagAllowUnprivilegedCreate)
    r, _, e := procCreateSymbolicLinkW.Call(
        uintptr(unsafe.Pointer(l)), uintptr(unsafe.Pointer(t)), flags)
    if r != 0 {
        return nil
    }
    if errno, ok := e.(syscall.Errno); ok && errno == errInvalidParameter {
        // 该环境不认 ALLOW_UNPRIVILEGED_CREATE，退回普通方式（需提权）
        r, _, e = procCreateSymbolicLinkW.Call(
            uintptr(unsafe.Pointer(l)), uintptr(unsafe.Pointer(t)), uintptr(symlinkFlagFile))
        if r != 0 { return nil }
    }
    if errno, ok := e.(syscall.Errno); ok && errno == errPrivilegeNotHeld {
        return fmt.Errorf(
            "创建软链接需要管理员权限（缺少 SeCreateSymbolicLinkPrivilege）。"+
                "请以管理员身份重新启动本程序，或在系统设置中开启「开发者模式」。原始错误: %w", e)
    }
    return fmt.Errorf("创建软链接失败: %w", e)
}

// symlinkTarget 读取链接指向的原始目标（不解析为绝对路径）
func symlinkTarget(linkPath string) (string, error) {
    return os.Readlink(linkPath)
}
```

> **`unsafe` 导入**：Windows 文件需要 `import "unsafe"`。unix 文件不需要。
> 两者用 build tag 隔离，互不影响。

**错误映射的意义**：`1314` 是最可能的失败（未提权且未开开发者模式）。
把它翻译成**可操作的指引**，而不是抛一个 `A required privilege is not held`。

### 3.2 核心：SymlinkMerge

**`internal/ops/symlink.go`**（新增）——骨架严格照搬 `HardlinkMerge` 的安全模式：

```go
package ops

// SymlinkMerge 把 dup 替换为指向 keep 的符号链接。
//
// 五步安全模式（与 HardlinkMerge 同构，理由见该函数头注释）：
//  1. 建临时软链接（不直接动 dup）
//  2. 复核 keep 与 dup 在校验后未被替换
//  3. 把 dup 改名为备份（保留原数据，绝不先删）
//  4. 把临时软链接改名为 dup 路径（原子）
//  5. 终局复核链接确实指向 keep；成功后才删除备份
//
// 任一步失败都能把 dup 还原为原始文件，杜绝数据丢失。
func SymlinkMerge(keep, dup string, keepID, dupID fsid.ID) error {
    if keep == dup {
        return fmt.Errorf("同一路径")
    }

    // 步骤 1：建临时软链接
    tmp := dup + FddTempSuffix
    _ = os.Remove(tmp)
    if err := createSymlink(keep, tmp); err != nil {
        return err   // 含权限不足的可操作提示（见 3.1）
    }

    // 步骤 2：复核身份（校验与执行之间可能被替换）
    if !identityStill(tmp, keepID) {
        _ = os.Remove(tmp)
        return fmt.Errorf("保留源在校验后被替换（inode 已变化），已拦截（S1）")
    }
    if !identityStill(dup, dupID) {
        _ = os.Remove(tmp)
        return fmt.Errorf("目标文件在校验后被替换（inode 已变化），已拦截（S1）")
    }

    // 步骤 3：备份原 dup（绝不先删）
    backup := dup + FddOldSuffix
    _ = os.Remove(backup)
    if err := hardlinkRename(dup, backup); err != nil {
        _ = os.Remove(tmp)
        return err
    }

    // 步骤 4：临时链接顶替 dup 位置（原子）
    if err := hardlinkRename(tmp, dup); err != nil {
        _ = hardlinkRename(backup, dup) // 还原
        _ = os.Remove(tmp)
        return err
    }

    // 步骤 5：终局复核——链接必须真的指向 keep 所代表的那份数据
    if err := verifySymlinked(keep, dup); err != nil {
        if rerr := hardlinkRename(dup, backup+".undo"); rerr == nil {
            if berr := hardlinkRename(backup, dup); berr != nil {
                return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失）",
                    err, backup+".undo")
            }
            _ = os.Remove(backup + ".undo")
            return err
        }
        return fmt.Errorf("%w；且还原 dup 失败，原文件保留在 %s（数据未丢失）", err, backup)
    }

    // 成功：删除备份
    if err := workTempRemove(backup); err != nil {
        _ = os.Remove(tmp)
        return &ResidueError{Path: backup, Err: err}
    }
    return nil
}
```

> `hardlinkRename`（= `os.Rename`）与 `workTempRemove` 是既有的可注入点，
> 直接复用，测试可模拟失败。

### 3.3 终局复核：verifySymlinked

**这是与硬链接最不同的一处。** 硬链接复核用 `verifyHardlinked`
（比较两路径的身份是否相同）。软链接**不能这么做**——`fsid.FromPathNoFollow(dup)`
会取到**链接自身**的身份，而链接是独立的文件系统对象，永远不等于 keep。

正确做法是**解析链接目标，比对目标身份**：

```go
// verifySymlinked 确认 dup 是一个指向 keep 所代表数据的符号链接。
//
// 与 verifyHardlinked 的区别（关键）：
//   - 硬链接：dup 与 keep 是同一 inode → 直接比身份
//   - 软链接：dup 是独立对象，自身身份无意义 → 必须解析目标再比
//
// 校验两层：
//   ① dup 确实是符号链接（Lstat 的 ModeSymlink）
//   ② 解析后的目标身份 == keep 的身份
func verifySymlinked(keep, dup string) error {
    li, err := os.Lstat(dup)
    if err != nil {
        return fmt.Errorf("复核失败：无法读取 %s: %w", dup, err)
    }
    if li.Mode()&os.ModeSymlink == 0 {
        return fmt.Errorf("复核失败：%s 不是符号链接（实际模式 %v）", dup, li.Mode())
    }

    // 解析目标并取身份（这里**要跟随**，因为要看的是目标本身）
    tid, err := fsid.FromPath(dup)   // 跟随链接
    if err != nil {
        return fmt.Errorf("复核失败：%s 的目标不可达（悬空链接？）: %w", dup, err)
    }
    kid, err := fsid.FromPath(keep)
    if err != nil {
        return fmt.Errorf("复核失败：保留源 %s 不可达: %w", keep, err)
    }
    if !tid.Resolved || !kid.Resolved {
        // 平台不支持取身份（如 Windows 的 FromPathNoFollow 场景）：
        // 退化为"目标可达 + 大小一致"检查，但明确标记为弱校验
        st1, e1 := os.Stat(dup)
        st2, e2 := os.Stat(keep)
        if e1 != nil || e2 != nil {
            return fmt.Errorf("复核失败：目标或保留源不可达")
        }
        if st1.Size() != st2.Size() {
            return fmt.Errorf("复核失败：链接目标大小与保留源不一致")
        }
        return nil
    }
    if !tid.SameIdentity(kid) {
        return fmt.Errorf("复核失败：链接目标与保留源不是同一文件（inode 不符）")
    }
    return nil
}
```

> **依赖**：需要 `fsid.FromPath`（**跟随**链接取目标身份）。
> 现有 `fsid_unix.go` 有 `FromPathNoFollow`（`Lstat`），需补 `FromPath`（`Stat`）。
> Windows 侧同理：`FromPathNoFollow` 用 `FILE_FLAG_OPEN_REPARSE_POINT`，
> `FromPath` 则**不加**该标志（跟随）。

### 3.4 身份校验补充（`identityStill` 的软链接语义）

`identityStill` 现有实现取"路径自身"身份，对符号链接会拿到链接自身。
在 `SymlinkMerge` 的**步骤 2**里对 `tmp`（一个软链接）调用 `identityStill(tmp, keepID)`
会**必然失败**（链接身份 ≠ keep 身份）。

**修正**：步骤 2 对 `tmp` 的校验应改为"解析链接后比对"：

```go
// 步骤 2（修正版）
if err := verifySymlinked(keep, tmp); err != nil {
    _ = os.Remove(tmp)
    return fmt.Errorf("保留源在校验后被替换，已拦截（S1）: %w", err)
}
```

`dup` 的校验仍用 `identityStill`（此时 dup 还是普通文件，语义正确）。

> 这是设计里**最容易写错的一处**：直接把硬链接的 `identityStill(tmp, keepID)`
> 照搬过来，会导致每次合并都报"保留源在校验后被替换"，功能完全不可用。

### 3.5 执行器接入

**`internal/ops/executor.go`**

```go
// :318 白名单
switch op.Kind {
case "trash", "delete", "move", "hardlink", "symlink":   // ← 加 "symlink"
default: ...

// :424 附近新增分支（结构照抄 hardlink 分支）
case "symlink":
    // 与 hardlink 相同的前置：逐项复核身份
    // 差异：soft 链接允许跨卷，故不做同卷前置检查
    for each i in toProcess {
        if err := SymlinkMerge(src.Path, e.Path, srcID, procIDs[i]); err != nil {
            var residue *ResidueError
            if errors.As(err, &residue) {
                settle(i, outcome{code: ocOK, linkSrc: src.Path, warn: residue.Error()})
            } else {
                settle(i, outcome{code: ocFailed, err: err.Error()})
            }
        } else {
            settle(i, outcome{code: ocOK, linkSrc: src.Path})
        }
    }
```

**跨卷前置检查**：软链接**允许**跨卷，但应显式确认"确实跨卷"——
若同卷，引导用户改用硬链接（更安全）。这不阻断，只提示：

```go
// 同卷却选了软链接：非错误，但提示更优选择
if !isCrossDeviceBetween(keep, dup) {
    // 记入 Warnings，不判失败
    warn = "这两个文件在同一卷上，使用硬链接更安全（无悬空风险）"
}
```

### 3.6 回撤

**`internal/ops/undo.go`**（`:66-70` 的 switch 加分支）

```go
case "symlink":
    return undoSymlink(it)

// undoSymlink 回撤软链接合并：
//   1. 确认 it.DestPath 现在确实是指向 it.LinkSrc 的软链接（防误删第三方）
//   2. 删除该链接
//   3. 把备份（.fdd-old）还原回原路径
//
// 与 undoHardlink 的关键差异：
//   - 链接是独立对象，删它不影响数据（数据在 keep 处）
//   - 因此"删链接失败"比"还原备份失败"轻得多
//   - 但仍要防"路径已被换成普通文件"→ 用 verifySymlinked 拦
func undoSymlink(it model.OpItem) (string, error) {
    // ① 目标位置必须仍是指向 keep 的软链接
    if err := verifySymlinked(it.LinkSrc, it.DestPath); err != nil {
        return "", fmt.Errorf("目标已非本次创建的软链接，为避免覆盖第三方文件已拦截: %w", err)
    }
    // ② 备份必须还在，且内容与记录一致
    backup := it.DestPath + FddOldSuffix
    if _, err := os.Lstat(backup); err != nil {
        return "", fmt.Errorf("找不到原始备份 %s，无法回撤（链接保留未动）", backup)
    }
    // ③ 删链接 → 还原备份
    if err := os.Remove(it.DestPath); err != nil {
        return "", err
    }
    if err := hardlinkRename(backup, it.DestPath); err != nil {
        return "", fmt.Errorf("链接已删除但还原备份失败，原文件在 %s（数据未丢失）", backup)
    }
    return it.DestPath, nil
}
```

### 3.7 前端

**作用范围**：仅当重复组**跨卷**时，在组卡片显示"软链接合并"按钮。

判定依据：`model.FileEntry.VolumeID`（已有字段）。组内 `VolumeID` 不唯一 → 跨卷。

**必须在按钮附近显示风险提示**（不可省）：

> ⚠️ 软链接是"指向另一位置的替身"。**若保留项被删除、移动，或所在磁盘被移除，此链接会失效**（文件打不开）。
>
> 需要管理员权限。同卷文件请改用"硬链接合并"（更安全）。

**结果文案**：

```
成功 N（已合并为软链接 X，占用不变；需管理员权限）
```

**RecordsView**：软链接操作的明细需显示"链接路径 → 目标路径"，让用户能看出它是链接。

### 3.8 悬空链接检测（新增，非阻塞）

用户可能在应用外删除保留项，导致链接悬空。为让用户能发现，新增只读检测：

- 位置：扫描结束后（或结果页加载时）对**当前结果集中不可见的**已知软链接做检查
- 实现：读取历史记录中 `Kind == "symlink"` 的项，`os.Stat(destPath)` 失败即悬空
- 展示：RecordsView 对应行标红 + "链接已失效"

> 本轮**只做展示**，不做自动修复（修复需要用户决定用哪份数据，超出范围）。

---

## 4. 风险与缓解

| # | 风险 | 严重度 | 缓解 |
|---|------|:-:|---|
| R1 | **悬空链接**：源被删/盘被拔 → 链接失效，用户以为文件还在 | 🔴 高 | ① 仅跨卷启用；② UI 显式风险提示；③ 悬空检测标红；④ 同卷强制走硬链接 |
| R2 | **权限不足**（1314）：未提权且未开开发者模式 | 🟡 中 | 错误映射为可操作指引；建议应用 manifest 请求提权 |
| R3 | 指向可移动/网络存储 | 🟡 中 | UI 提示避免指向移动盘；不提供网络路径（§0 明确不做） |
| R4 | 复核写错（照搬硬链接逻辑） | 🔴 高 | §3.4 已单独标注；专项测试覆盖 |
| R5 | 回撤时误删第三方文件 | 🔴 高 | `verifySymlinked` 前置校验（§3.6） |
| R6 | 扫描器若改为跟随链接 → 环路 | 🟡 中 | **明确不改**（§1.2），保持跳过 |

---

## 5. 测试计划

### 5.1 单元测试

**`internal/ops/symlink_test.go`**（新增）

| 用例 | 断言 |
|---|---|
| `TestSymlinkMergeCreatesWorkingLink` | 合并后 dup 是链接，读内容 == keep |
| `TestSymlinkMergeIsAtomicOnRenameFailure` | 注入 rename 失败 → dup 原样、无残留 |
| `TestSymlinkMergeRollsBackWhenVerifyFails` | 注入复核失败 → dup 还原为普通文件 |
| `TestSymlinkMergeResidueReported` | 注入删除失败 → 返回 `ResidueError`，链接仍有效 |
| `TestSymlinkMergeRejectsSamePath` | keep == dup → 报错 |
| **`TestSymlinkMergeVerifiesResolvedTarget`** | **核心**：确保复核是"解析目标"而非"比链接自身"（防 §3.4 写错） |
| `TestVerifySymlinkedRejectsRegularFile` | dup 是普通文件 → 复核失败 |
| `TestVerifySymlinkedRejectsWrongTarget` | 链接指向别的文件 → 复核失败 |
| `TestVerifySymlinkedDetectsDangling` | 目标不存在 → 复核失败 |

**`internal/ops/undo_symlink_test.go`**（新增）

| 用例 | 断言 |
|---|---|
| `TestUndoSymlinkRestoresOriginal` | 回撤后 dup 恢复为普通文件，内容完整 |
| `TestUndoSymlinkBlocksSwappedTarget` | dup 被换成第三方文件 → 拦截不删 |
| `TestUndoSymlinkDanglingLinkStillUndoable` | 链接已悬空 → 仍可回撤（数据在备份里） |

### 5.2 Windows 专项（真机验证，无法在 CI 覆盖）

CI 的 `windows` job 跑 `scripts/test-windows-quarantine.sh`，需同步登记这些用例。

| 场景 | 预期 |
|---|---|
| 提权运行 + 跨卷合并 | ✅ 成功，链接可读 |
| 未提权 + 未开开发者模式 | ❌ 报"需要管理员权限"（1314 映射） |
| 未提权 + **已开开发者模式** | ✅ 成功（走 `ALLOW_UNPRIVILEGED_CREATE`） |
| 同卷合并 | 成功但 **Warnings 提示"同卷建议硬链接"** |
| 合并后拔掉源盘 | 链接悬空，检测标红 |

### 5.3 端到端

`scripts/smoke-cli.sh` 的固定语料在**单卷**上，无法覆盖跨卷。新增独立脚本
`scripts/smoke-symlink.sh`：**两个临时目录模拟两卷**（unix 上可用 `mount --bind`
或两个目录 + 同卷软链接仍可用；跨卷语义在 unix 上以 `EXDEV` 判定）。

---

## 6. 交付清单

| 文件 | 类型 |
|---|---|
| `internal/ops/symlink.go` | 新增（SymlinkMerge / verifySymlinked） |
| `internal/ops/symlink_unix.go` | 新增（os.Symlink） |
| `internal/ops/symlink_windows.go` | 新增（CreateSymbolicLinkW + 错误映射） |
| `internal/ops/symlink_test.go` | 新增 |
| `internal/ops/undo_symlink_test.go` | 新增 |
| `internal/fsid/fsid_unix.go` | 改（补 `FromPath`） |
| `internal/fsid/fsid_windows.go` | 改（补 `FromPath`，不加 REPARSE 标志） |
| `internal/ops/executor.go` | 改（白名单 + symlink 分支） |
| `internal/ops/undo.go` | 改（undoSymlink） |
| `internal/model/model.go` | 改（Kind 注释补 symlink；如需记录 TargetPath） |
| `frontend/src/views/ResultView.vue` | 改（跨卷才显示 + 风险提示） |
| `frontend/src/views/RecordsView.vue` | 改（链接→目标显示 + 悬空标红） |
| `frontend/src/wails.ts` | 改（类型） |
| `scripts/test-windows-quarantine.sh` | 改（登记新用例） |
| `scripts/smoke-symlink.sh` | 新增 |
| `docs/09-用户手册.md` | 改（新增功能说明 + 风险） |
| `docs/04-开发与测试计划.md` | 改（交付状态） |

---

## 7. 实施顺序

1. **平台层**（`symlink_unix.go` / `symlink_windows.go`）+ 单测 → 先确认能建出链接
2. **fsid.FromPath** 补齐（两平台）
3. **verifySymlinked** + 单测（**先于** SymlinkMerge，它是复核的基础）
4. **SymlinkMerge** + 单测（含全部失败路径注入）
5. **执行器接入** + 单测
6. **回撤** + 单测
7. **前端**（跨卷判定 + 风险提示）
8. **悬空检测**
9. 文档同步
10. Windows 真机验证

> 顺序理由：3 先于 4，因为复核写错会导致 SymlinkMerge 全部失败，
> 先独立验证复核逻辑可大幅降低调试成本。
