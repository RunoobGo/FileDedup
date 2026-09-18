# FileDedup

重复文件查找与清理桌面应用。基于 **Wails v2 + Vue 3 + Go** 构建，可在 macOS / Windows / Linux 上以原生窗口运行。

## 功能特性

- 多根目录扫描，按内容哈希（默认 BLAKE3-256）识别重复文件
- 分组展示重复项：按可释放空间 / 单文件大小 / 文件数排序，按扩展名过滤；扫描前可按扩展名 / 大小 / 路径预筛
- 可预览文件元信息与内容（Markdown 等），一键在文件管理器中定位
- 增量缓存：已扫描文件的哈希结果落盘，二次扫描大幅提速
- 安全删除：标记后批量清理，避免误删

## 技术栈

| 层 | 技术 |
|---|---|
| 桌面壳 | Wails v2（Go + WebView） |
| 前端 | Vue 3 + Pinia + Vite 5 |
| 后端 | Go（哈希、缓存、去重分析） |
| 存储 | SQLite（哈希缓存） |

## 构建与运行

```bash
# 依赖：Go 1.27.1+（见 go.mod 的 go 指令）、Node 18+、Wails v2 CLI
wails dev      # 开发模式（热更新）
wails build    # 产出各平台安装包
```

## 目录结构

```
.
├── main.go                 # 应用入口与窗口配置
├── app.go                  # Wails 应用生命周期与绑定
├── internal/               # Go 后端（scanner / dedup / hasher / cache / ops / filter / media / progress / model）
├── frontend/               # Vue 3 前端
├── build/                  # 图标与构建资源
└── docs/                   # 设计 / 开发 / 测试文档
```

## 开发前置：先构建前端

`main.go` 用 `//go:embed frontend/dist` 内嵌前端产物，而 `frontend/dist` 属构建产物
（被 .gitignore 排除）。因此全新克隆后须先 `cd frontend && npm ci && npm run build`，
否则 `go build` / `go vet` / `go test` 会因 embed 找不到目录而失败（CI 已按此顺序编排）。

## 许可证

本项目采用 **MIT License**，详见仓库根目录 [LICENSE](LICENSE) 文件。
依赖链（Wails / xxhash / BLAKE3 / SQLite 驱动等）均为 MIT/BSD 等宽松许可，无 GPL 传染。
