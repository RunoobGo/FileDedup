# M202 设计段：错误事件的中文外壳（原文降级为 detail）

## 0. 任务源与裁定

登记表 **M202/APP-24**（§6.29 批 C 占号）：`app.go` 三类发射点把 `err.Error()` 原样上传
⇒ 界面直接出现英文 OS 错误（`open /Users/…/x: permission denied`）与内部绝对路径。
登记取向"收口=统一中文外壳（错误分类表），属文案契约面、波及前端既有断言 ⇒ 另批"。

**2026-09-24 用户裁定：统一中文外壳 + 原文降级为 detail**（不是"只包 OS 错误"，也不是
"只补文档"）。

## 1. 现状真读数（2026-09-24 现取）

- 三个发射点（同一形状 `map[string]string{"error": …}`）：
  1. `StartScan` 腿的 `scan:error`——载荷 `err.Error()`；
  2. `OnPanic` 回调的 `ops:error`（C7 守卫把 panic 转 error 后直传）；
  3. `recoverPanic` 里的 `scan:error`/`ops:error`——载荷
     `fmt.Sprintf("%s goroutine panic: %v", kind, r)`（**中英混排**：中文前缀没有，
     整条是 `ops goroutine panic: runtime error: …`）。
- 前端消费面：`stores/scan.ts` 的 `bind('ops:error')` 与 `bind('scan:error')` 都经
  `toast.errText(e)` 归一化（认 `string` / `Error` / `{error}` 三种形状），`notifyError`
  拼成 `title：detail` 单行投递。★ `scan:error` 那一格改前直接把**整个对象**丢给
  `notifyError('扫描失败', e)`，靠 `errText` 兜住——顺带收口。
- 既有断言面（现读）：Go 侧只钉**事件名**（`app_p3_test.go:34/79` 比的是
  `waitTerminal(...) == "scan:error"`），**没有一处钉载荷文本**；前端侧
  `tests/clipboard-copy.test.ts` 只碰复制路径。⇒ 载荷**加键**不会撞既有断言，
  把 `error` 换成中文外壳同样不撞。

## 2. 外壳的三分法（这一格是本设计的全部难度）

**不能一律套外壳**：本仓大量错误是应用自己写的中文句子（"…无法保证进入回收站，已拒绝以防
静默永久删除"、"文件在扫描后被替换（inode 已变化），已拦截"）。给它们再包一层
"操作未能完成"等于把已有信息降级，且用户会看到两句废话套一句真话。故：

| 类 | 判据 | 外壳 | detail |
|---|---|---|---|
| A 已知系统错误签名 | 错误串命中 OS  errno 字面（下表） | 对应中文句（不含路径） | 原文（含路径，供排查/复制） |
| B 应用自撰中文 | 串中含 CJK 字符 | **原文一字不改** | 留空（不重复） |
| C 未知/未分类 | 既非 A 也非 B | 「操作未能完成（未分类的系统错误）」 | 原文 |

★ C 类必须存在且必须显式：把认不出的英文塞进"已分类"的形状就是假装知道。

★ **实现期修正（2026-09-24，落笔时现读代码后改判）**：panic 载荷最终**不是**归入 C，
而是独立成第四档 `D`（壳＝「应用内部异常已被拦截，本次操作中断」，判据
`goroutine panic` / `panic: `），且**排在 B（含 CJK 即原文照出）之前**。原因现读即见：
`recoverGoroutine` 拼的串本身带中文kind 前缀的可能是有的（测试里 `scan goroutine panic:
模拟流水线内部 panic` 就是中英混排），落在 B 档会被"原文一字不改"直接送出 ⇒
"应用自撰中文"这条判据对 panic 不成立，顺序必须先行拦下。表格三档的说法与本节末段
（"panic 载荷…都算 C"）按约束(1)保留原位，**本段是对那两处的唯一修正**：实现里走 D 档。

A 类表（逐条取自 Go `syscall` 在 darwin/windows 的 `Errno.Error()` 字面，
只认**结尾的固定短语**，不认整句，避免把用户路径里的英文单词误判）：

- `permission denied` → 权限不足，无法访问该位置
- `no such file or directory` → 该文件或文件夹已不存在
- `is a directory` / `not a directory` → 路径类型不符（一个是目录一个是文件）
- `device or resource busy` → 正被其他程序占用
- `read-only file system` → 所在卷是只读的
- `no space left on device` → 目标卷空间已满
- `file name too long` → 路径过长（超过文件系统上限）
- `cross-device link` → 跨卷链接不被支持（硬链接仅同卷可用）
- `operation not supported` / `not supported` → 该卷或该操作不被支持
- `too many open files` → 打开的文件句柄过多
- `operation canceled` / `context canceled` / `context deadline exceeded` → 操作已取消或超时
- Windows 共享冲突串 `Access is denied` / `The process cannot access the file because it is being used` 一并归入前两类

panic 载荷（第 2/3 点）走 B 还是 C？都算 **C**：`runtime error: invalid memory address…`
无 CJK、也不在 A 表 ⇒ 外壳＝"操作未能完成（内部异常，已拦截）"，detail 保留 `goroutine panic`
原句（技术排查必须留着它）。

## 3. 落点

- 新文件 `app_error_shell.go`（根包，无 tag）：`errorShell(err error) (shell, detail string)`
  与 `errorEvent(err error) map[string]string`（产出 `{"error","detail"}`，detail 空时不写键，
  免得前端多一个空串分支）。
- 三处发射点改调 `errorEvent`；`recoverPanic` 那条保持 `kind` 信息进 detail
  （`ops goroutine panic: …` 原文一字不动地降级，不改写）。
- 前端 `scan.ts`：两处 handler 用新 helper `errWithDetail(e)` 组装
  `外壳` + 有 detail 时 `（系统原文：<detail>）`；`scan:error` 改传 `e` 经同一 helper
  （不再裸传对象）。单行 toast 的形态不变，只是**中文在前、原文在后且标明是原文**。
- `docs/09` §错误文案与 `docs/10` 对应用户可见口径各补一句：错误提示首段为中文，
  括号内系统原文可复制用于排查（含绝对路径，截图外发时留意）。

## 4. 判据（红探针在前，护栏在后）

- **P-1**（A 类）：注入 `open /tmp/definitely/not/here: permission denied` ⇒
  `error` 事件载荷的外壳必为中文句且**不含** `/tmp/definitely`；`detail` 必为原文逐字。
  改前红（载荷就是原文，含路径、含英文）。
- **P-2**（B 类，防过度包装）：注入应用自撰中文错误（回收站拒绝句）⇒ 外壳**必须等于原文**。
  改前也绿 ⇒ **它是护栏不是红探针**（AS-K2），但它是这一格唯一能拦住"一律套壳"的用例。
- **P-3**（C 类）：注入 `"boom: something weird"`（无 CJK、不在表）⇒ 外壳必为
  "未分类"那句、detail 必为原文。改前红（载荷 = 原文）。
- **P-4**（panic 腿）：`recoverPanic` 的载荷 detail 含 `goroutine panic` 原句、
  error 为中文。改前红（error 是 `ops goroutine panic: …` 中英混排串）。
- **P-5**（前端契约，node 用例进 `frontend/tests`）：`errWithDetail` 对
  `{error,detail}` / `{error}` / 裸字符串 / `Error` 四种输入的输出形状钉死
  （中文在前、原文在后带"系统原文"标记、无 detail 时不加括号）。
- 变异预测（实现后跑，预测集=实测集要对账；每条变异前 `cp` 备份、还原用 `cp`+`diff`）：
  - **Va** A 表清空 ⇒ 预测杀 P-1；
  - **Vb** 去掉 B 类分支（一律走 A/C）⇒ 预测杀 P-2；
  - **Vc** `errorEvent` 不写 detail ⇒ 预测杀 P-1、P-3、P-4；
  - **Vd** detail 与外壳合并成一句（前端不降级）⇒ 预测杀 P-5。

### 4.1 实测对账（2026-09-24 本机现跑）

| 变异 | 预测杀 | **实测杀** | 对账 |
|---|---|---|---|
| Va A 表清空 | P-1 | P-1 | 一致（红信息就是"真实 OS 错误落进未分类档"） |
| Vb 删 CJK 分支 | P-2 | P-2（两格断言同倒：改写 + 多出 detail） | 一致 |
| Vc `errorEvent` 丢 detail 键 | P-1、P-3、P-4 | **P-1、P-4** | ★ **预测过宽**：P-3 测的是 `errorShell` 的两个返回值本身，不经事件载荷 ⇒ 丢 detail 键动不到它。差集记这里，不改预测原文 |
| Vd 前端不降级（原文在前、无"系统原文"标记、同值也重复一遍） | P-5 | P-5 的 **A、B 两子格** | 一致（P-5 本就分三子格） |
| **Ve**（实现期追加，不在预测集内）把 panic 档挪到 CJK 档之后 | — | **P-4 两格** | ★ 这一发是**为 §2 那条实现期修正单独补打的**：挪序后 P-4 外壳变回原句、detail 变空串，同跑 `TestScanGoroutinePanicIsContained` 仍 PASS（只钉"进程没崩 + 事件名"）。⇒ 次序由 P-4 **顺带**拦住，不是专设判据；要专设需在 P-2 再注入一条"含 `panic` 字样的自撰中文句"，本批未写，记为已知覆盖形状 |

★ 实现期另加一条设计段没列的护栏：`TestM202NilErrorYieldsEmptyPayload`（`errorEvent(nil)`
不得产出幽灵事件）。它不是红探针，改前改后都绿，拦的是"未来有人把 `errorEvent` 用在
可能为 nil 的分支上"。

★ 附带取到的**真改前红**（不是变异）：把 `app.go` 的 `recoverGoroutine` 与 `scan:error`
两处 emit 退回改前原样 ⇒ `TestM202PanicLeg...` 红在两格（外壳 got `"scan goroutine panic:
模拟流水线内部 panic"`、detail 为空串）。P-1/P-2/P-3 依赖新符号 `errorShell`，改前树
编译不过 ⇒ 它们的"红"只能由变异提供，这一点与 `clipboard-copy.test.ts` 头注同一口径。

## 5. 边界（本批不做的）

- **不改失败清单（`FailedItem.Reason`）的逐条文案**：那是 `ops` 层自撰中文，
  本批只动"事件"这一条通道。★ 但事件里 B 类判据同样保住了它——若未来有人把
  `errorEvent` 用到逐条落账上，中文原文不会被再包一层。
- 不动 `app:error`（非致命后台异常）与 `app:quit-blocked`：载荷本已是应用自撰中文。
- 不引入 i18n 框架、不做英文界面：外壳是**中文常量句**，与产品当前单语口径一致。
- 不做错误码表（`code` 字段）：登记行要的是"用户读得懂"，不是"程序分得清"；
  加 code 会让契约面多一个无人消费的键（YAGNI）。
- 绝对路径仍在 detail 里出现（排查必需），本批不脱敏；只在手册里提示"截图外发留意"。
