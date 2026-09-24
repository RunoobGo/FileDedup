# FileDedup

重复文件查找与清理桌面应用。基于 **Wails v2 + Vue 3 + Go** 构建，可在 macOS / Windows / Linux 上以原生窗口运行。

## 功能特性

- 多根目录扫描，按内容哈希（默认 BLAKE3-256）识别重复文件
- 分组展示重复项：按可释放空间 / 单文件大小 / 文件数排序，按扩展名过滤；扫描前可按扩展名 / 大小 / 路径预筛
- 可预览文件元信息与内容（Markdown 等），一键在文件管理器中定位
- 增量缓存：已扫描文件的哈希结果落盘（`cache.db`），二次扫描大幅提速；命中前须过四道检查
  （大小 + mtime → 物理身份 → 四点采样复核 → 采样非全零）。物理身份 unix 取 `fstat`
  的卷/inode/ctime，Windows 取句柄查询的卷序列号 + 文件索引；不给稳定索引的卷（FAT/exFAT、
  部分网络共享）上身份那道自动跳过，缓存只是加速手段而非等价证明
- 安全删除：默认移入系统回收站，永久删除需显式二次确认；每次清理写"写前账本"（`history.db`），
  逐文件落账，失败项可单项重试回撤
  - 可回撤范围：回收站（macOS / Linux）、移动、硬链接合并、软链接合并（跨卷）；**Windows 回收站与永久删除标注为
    不可回撤**（前者系统 API 不返回可靠落点，用「打开系统回收站」手动还原）
  - 账本写不进时**承诺可回撤的操作一律拒绝执行**，不允许"清完才发现无从恢复"
  - 硬链接合并校验 inode 防"校验后被替换"；跨卷移动为「复制 + 字节数校验 + fsync + 还原权限与
    mtime + 删源」，任一步失败保留源（不承诺复制后的内容哈希复核）
  - 跨卷去重走**软链接合并**：冗余盘上那份替换成指向保留源的符号链接，磁盘只留一份数据、原路径仍可
    访问。代价是链接会悬空——保留源被删或被挪、它所在磁盘被拔掉都会让链接打不开；悬空后**照样可回撤**
    （数据在合并前的备份里，与链接是否有效无关）
- 扫描历史与清理记录可回看；清理/回撤执行期由后端互斥门统一拒绝并发操作，前端按钮同时置灰

> 当前版本：**0.5.0**（应用内「关于」/ `GetVersion` 与仓库 6 个取值位 / 5 个文件的版本号由
> `scripts/check-version-sync.sh` 强制对齐，详见下方[发布](#发布)）。
> 完整功能与安全性说明以 [docs/09-用户手册.md](docs/09-用户手册.md) 为准；
> 工程现状与门禁看 [docs/04-开发与测试计划.md](docs/04-开发与测试计划.md)；
> 该看哪份文档的索引见 [docs/README.md](docs/README.md)。

## 技术栈

| 层 | 技术 |
|---|---|
| 桌面壳 | Wails v2（Go + WebView） |
| 前端 | Vue 3 + Pinia + Vite 5 |
| 后端 | Go（哈希、缓存、去重分析） |
| 存储 | SQLite（`cache.db` 哈希缓存 / `history.db` 扫描历史与清理账本，均 modernc 纯 Go 驱动） |

## 构建与运行

```bash
# 依赖：Go 1.27.1+（与 go.mod 的 go 指令一致）、Node 22（CI 口径，≥18 可本地跑）、Wails v2 CLI
wails dev      # 开发模式（热更新）
wails build    # 产出当前平台安装包（build/bin/）
```

CI 不使用 wails CLI（前端 npm 直构 + 后端 go 直编），仅发布构建 build.yml 装
`wails@v2.16.0`（与 go.mod 锁定的库版本一致，防 CLI 静默改写依赖）。

命令行工具（无界面，用于冒烟与基准）：

```bash
go run ./cmd/fdd-cli -paranoid -cache ./cache.db -o report.json <目录...>
go run ./cmd/benchgen -dataset C -scale 0.02 -out ./benchdata   # 合成数据集 + manifest
```

## 目录结构

```
.
├── main.go                 # 应用入口与窗口配置（go:embed frontend/dist）
├── app.go                  # Wails 应用生命周期与绑定（AppVersion 声明位）
├── internal/               # Go 后端 20 包（scanner / dedup / hasher / filter / cache / ops /
│                           #   history / model / fsid / fscase / sysguard / realbytes /
│                           #   cloudfile / ads / media / worktemp / progress / dbfile /
│                           #   pathnorm / sqlconn）
├── cmd/                    # fdd-cli（JSON 报告冒烟工具）、benchgen（合成数据集生成器）
├── frontend/               # Vue 3 前端
├── scripts/                # 回归与门禁脚本（见下）
├── build/                  # 图标与打包资源（Info.plist / manifest 模板）
└── docs/                   # 文档（索引见 docs/README.md）
    ├── 09-用户手册.md       # 活文档：功能与安全性口径真源
    ├── 10-用户使用手册.md   # 活文档：09 的用户向派生本（三段结构，无源码坐标；两文出入以 09 为准）
    ├── 04-开发与测试计划.md # 活文档：V2.0 起为「现状与质量门禁」，§6 是唯一权威待办清单
    ├── 05-真机实测清单.md   # 活文档：Windows / macOS / Linux **三腿**真机执行清单（M7 交付物；只写怎么测怎么判，读数回填 04）
    ├── 01/02/03-*.md        # 冻结设计/选型档案（正文不回填，偏差记页首日期化勘误表）
    ├── superpowers/         # 0.5.0 那批功能的设计 + 实施计划（历史产物，顶部有偏差说明）
    └── archive/             # 编年史：代码审查与修复 / UI 视觉与品牌 / 里程碑报告 /
                             #   04 计划原文快照 + 审核截图证据
```

## 测试与回归

改动后的本地回归配方（与 CI 同口径，见 `.github/workflows/ci.yml`）。
**唯一入口是脚本**：`bash scripts/run-gates.sh` 一次跑完下面全部命令，逐行给
PASS / SKIP / FAIL 并自带总判定与非 0 退出码（`smoke-symlink.sh` 需 root，本机恒
rc=2 ⇒ 记 **SKIP 不算通过**）。下面那份清单是它的手工对照，两者口径必须一起改：

```bash
gofmt -l .                                                   # 必须无输出
go vet ./...                                                 # 本机
GOOS=windows GOARCH=amd64 go vet ./...                       # 跨平台
GOOS=darwin GOARCH=arm64 go vet ./...                        # 跨平台
cd frontend && npm ci && npm run build                       # 先产 dist（go test 依赖 embed）
                                                             # 含 prebuild: vue-tsc 类型检查
bash scripts/test-frontend-logic.sh                          # 前端纯逻辑行为探针 + 组件接线断言（零依赖，Node ≥22.18）
go test -race -count=2 ./...                                 # 全包
go test -race -count=4 .                                     # 根包（App 层状态机多压两轮）
bash scripts/smoke-cli.sh                                    # 数据集 C×0.02 三跑比对
bash scripts/smoke-symlink-assert.sh                         # 自证跨卷冒烟的判据本身有效
bash scripts/check-version-sync.sh                           # 6 个取值位 / 5 个文件版本号对齐
```

| 脚本 | 作用 |
|---|---|
| `scripts/run-gates.sh` | **全套门禁的唯一入口**：一次跑完上面那 11 条命令（拆合后 **15 行读数**），逐行给 PASS / SKIP / FAIL 并**自己返回非 0**。`gofmt` 按列出文件数判、`race` 行要求 rc=0 **且** `DATA RACE`=0、`smoke-symlink` 的 rc=2 记 SKIP 不计通过（跳过不等于验过） |
| `scripts/smoke-cli.sh` | benchgen 生成固定数据集 → `fdd-cli` 冷扫 / 缓存首扫 / 缓存复扫，逐项比对分组集合、可释放字节、语料数，并与 manifest 对账 |
| `scripts/smoke-symlink.sh` | 挂真实独立卷（loop+ext4，退到 tmpfs）验证软链接合并赖以成立的 7 条文件系统语义；退出码 0=通过、2=**合法跳过**（无 root / 挂不上卷）、1=失败——跳过绝不伪装成通过 |
| `scripts/smoke-symlink-assert.sh` | 用 stub 打桩 `id`/`mount`/`losetup`，断言上一条脚本在"应跳过"时真的返回 2 并写明原因、在正常路径上确实释放了 loop 设备。冒烟脚本本身也是代码，没人测它的判据就等于判据可以静默失效 |
| `scripts/check-version-sync.sh` | 6 个取值位 / 5 个文件的版本号对齐（`package-lock.json` 贡献 2 处）；tag 触发时再比对 `v<版本>` |
| `scripts/test-frontend-logic.sh` | 前端纯逻辑行为探针（`frontend/tests/*.test.ts`）+ 组件接线静态断言。vue-tsc 只证明"能编译"，这里证明"界面上的句子是真的"——例如 hardlink 的确认框不得写"空间可释放"（磁盘占用其实不变）、组内冗余项数必须取自 `store.groupSelCount` 而不许组件自算。用 Node 自带的 TS 类型剥离与 test runner，**不引入测试框架依赖**；退出码 0=通过、2=本机跑不了（无 node / 版本过旧 / 目录不存在，CI 判失败）、1=断言失败 |
| `scripts/test-windows-quarantine.sh` | Windows 侧白名单式隔离（清单内逐条注明原因；**当前清单为空**，即该平台任何失败都阻断），清单外用例一律阻断，条目命中 0 个测试时脚本自身变红 |

CI 门禁：`ci.yml`（PR 与 main push）三条腿——ubuntu 跑上述全套、windows runner 跑隔离后的
`go test`（白名单清单现为空）、macOS runner 跑本机 `go vet` + `go test -race` + CLI 冒烟
（D-2，2026-09-21 新增；该腿已在 CI 真跑过并抓红过一次 —— 见 04 §3.2 附注与 §6.18 一）；
`build.yml`（`v*` tag 或手动触发）四平台打包 + 发布。
真实回收站 / GUI 端到端仍需人工，平台 checklist 见 docs/04 §3.5。

## 发布

1. 同步版本号（6 个取值位 / 5 个文件）：`app.go` 的 `AppVersion`、`wails.json` 的
   `info.productVersion`、`frontend/package.json`、`frontend/package-lock.json`（顶层
   `version` 与 `packages[""].version` 两处）、`docs/09` 首部「适用版本」。
   `build/darwin/*.plist` 用 `{{.Info.ProductVersion}}` 模板，无需手改。
2. `bash scripts/check-version-sync.sh` 绿。
3. 打 tag：`git tag vX.Y.Z && git push origin vX.Y.Z` —— tag 必须等于 `AppVersion`，
   否则 build.yml 第一步就断掉（历史上出现过 v1.0.0 发布物对应 0.5.0 产品的错位）。
4. build.yml 自动构建四平台产物、生成 `SHA256SUMS.txt`，一并挂到 GitHub Release。
   发布前人工过 docs/04 附录 A checklist。

## 开发前置：先构建前端

`main.go` 用 `//go:embed frontend/dist` 内嵌前端产物，而 `frontend/dist` 属构建产物
（被 .gitignore 排除）。因此全新克隆后须先 `cd frontend && npm ci && npm run build`，
否则 `go build` / `go vet` / `go test` 会因 embed 找不到目录而失败（CI 已按此顺序编排）。

`frontend/wailsjs/`（Wails 生成的绑定）同样被 .gitignore 排除，`wails dev` / `wails build`
每次重生成。★ 本仓库的前端**不 import 它**：调用面统一走手写的 `frontend/src/wails.ts`，
运行时按 `window.go.main.App` 直连后端（现读 grep：`wailsjs` 在 `frontend/src` 里只有
一处注释命中）。所以绑定过期既不会让 `npm run typecheck` 变红，也不影响运行时，
只影响"直接读 `App.d.ts` 当契约看"这类工具——要拿它当参考前，先跑一次 `wails dev` 重生成。

## 许可证

本项目采用 **MIT License**，详见仓库根目录 [LICENSE](LICENSE) 文件。
依赖链（Wails / xxhash / BLAKE3 / SQLite 驱动等）均为 MIT/BSD 等宽松许可，无 GPL 传染。
