# M197 设计段：Windows 回收站调用的串行化（计数增量判据的并发污染）

## 0. 任务源与裁定

登记表 **M197/OPS-25**（§6.29 批 C 占号）：`executor.go:611` 的批量失败回退（C6）用
`runIndexed(…, opWorkers, …)` **并发**逐个调 `Trash`，而 `trash_windows.go` 的
`defaultTrash` 每次都独立取「操作前后该卷回收站条目数的增量」作事后判据 ⇒ 两条腿同时在跑时，
A 看到的增量可能全是 B 推进的，**B 的成功替 A 作了保**——H6 的第二道防线（把"Shell
静默永久删除"变成响亮的错误）被旁路。

登记行给两向（整体加锁串行 / 每腿独立命名空间）。**2026-09-24 用户裁定：整体加互斥**
（吞吐降为串行的代价可接受；判据语义一字不改）。

## 1. 现状真读数（2026-09-24 现取）

- `Trash` 是包级函数变量（`trash.go:14`），三平台各给一个 `defaultTrash`：
  darwin 走 osascript 批量、**不读计数**；linux 走 XDG 自研、**逐项目录记落点**；
  **只有 windows 依赖"回收站条目数增量"作判据** ⇒ 串行化只对 windows 生效。
- windows 的读窗：`before := snapshotRecycleBinCounts(paths)` → `SHFileOperation`
  → `verifyRecycled(paths, expected, before)`。`before` 与 `after` 之间夹着 Shell 调用，
  这就是可被别的腿推进计数的窗口。
- 预检段（`recyclableReason` 逐路径查卷类型/策略/配额，配额走 `SHQueryRBInfo`）也在同一
  函数内、同样读的是系统全局状态 ⇒ 一并纳入临界区。

## 2. 修法：中性串行列 + windows 侧接线

```go
// internal/ops/trash_serial.go（无 build tag）
var trashCallMu sync.Mutex
func withTrashSerial(call func([]string) (map[string]string, error), paths []string) (map[string]string, error) {
	trashCallMu.Lock()
	defer trashCallMu.Unlock()
	return call(paths)
}

// internal/ops/trash_windows.go
func defaultTrash(paths []string) (map[string]string, error) {
	return withTrashSerial(defaultTrashLocked, paths)
}
```

要点与不选的路：

1. **临界区必须罩住"取基准 → Shell 调用 → 复核增量"整段**。只锁 `SHFileOperation`
   那一行等于没锁：`before` 在锁外取，窗口照旧存在。故拆出 `defaultTrashLocked`
   整体入列，而不是在原函数中段插 `Lock`（插法每加一条 early-return 就多一次漏解锁风险）。
2. **为什么把 mutex 放在无 tag 文件里**：mutex 一旦写进 `trash_windows.go`，它的
   互斥语义在本机（darwin）**一行都跑不到**——只剩交叉编译能证明"能编译"。拆成中性
   的 `withTrashSerial` 后，"排队是否真发生"这一格在本机可测（§3），windows 的接线面
   由 `GOOS=windows go vet`（门禁第 5 行）+ 现读 grep 兜住。
3. 不在 darwin/linux 侧调用它：那两腿不读计数，套上只会白白串行化 Finder 批量与
   XDG 落点记录（行为变化无收益）。
4. 回退腿吞吐由并发降为串行，是本裁定的**自觉代价**；正常批量本就一次调用内完成，
   不受影响。锁内无长阻塞（配额查询毫秒级；Shell 调用无论谁持锁都要跑）。
5. 死锁面：`defaultTrashLocked` 不回环到 `Trash`（执行器在它返回之后才做回退判定），
   `sync.Mutex` 不可重入这一点在本格子成立。

## 3. 判据

- **P-1 串行列本体**（`trash_serial_test.go`，无 tag，本机真跑）：8 路并发经
  `withTrashSerial` 进临界区，观察量 `inFlight` 的最大值必须恒为 1，且 8 个不同参数
  全部按序送达（不吞、不串行成同一参数）。
- **P-2 解锁面**（`TestWithTrashReleasesLockOnPanic`）：被调方 panic 时列必须放行下一腿
  （锁泄漏的形态是"整条回收站路永久卡死"，比原判据缺陷更糟）。
- **变异**：摘掉 `withTrashSerial` 内的 `Lock/Unlock` ⇒ P-1 红（真探针，本机可跑）。
- ★ **接线面（"windows 的 defaultTrash 真走了这条列"）本机不可测**：判据本体依赖真实
  回收站的计数推进，darwin 造不出来；CI windows 腿能给的是**护栏读数**（并发多腿各自
  入站、逐路径落点齐——改前改后都绿，按 AS-K2 不冒领红探针）。
  真兑现途径登记为 docs/05 **W1-10**（单卷配额逼近上限时并发批量删除，构造
  "一条腿被静默永久删除 + 另一条腿正常入站"的串扰）。
  ⇒ **M197 本批按「代码已改、验证未兑现（判据格待 W1-10）」收口。**

## 4. 边界

- `verifyRecycled` 的判据形状（按卷增量、`expected` 数）一字不改。
- 执行器的并发形状（`opWorkers`）不改：收口在"被调方排队"，不在"调用方降并发"——
  后者会把不依赖计数的另两平台一起串行化。
- 若将来要恢复回退腿吞吐（每腿独立命名空间判据），那是登记行写的另一向，另批另裁。
- 回撤腿的 trash 分支（`undo.go`）走同一 `Trash` 入口 ⇒ 自动获得同一串行保护，不另改。
