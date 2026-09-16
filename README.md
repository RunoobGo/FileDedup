# FileDedup

重复文件查找与清理桌面应用。基于 **Wails v2 + Vue 3 + Go** 构建，可在 macOS / Windows / Linux 上以原生窗口运行。

## 功能特性

- 多根目录扫描，按内容哈希（默认 SHA-256）识别重复文件
- 分组展示重复项，支持按扩展名 / 大小 / 修改时间筛选与排序
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
# 依赖：Go 1.22+、Node 18+、Wails v2 CLI
wails dev      # 开发模式（热更新）
wails build    # 产出各平台安装包
```

## 目录结构

```
.
├── main.go                 # 应用入口与窗口配置
├── app.go                  # Wails 应用生命周期与绑定
├── internal/               # Go 后端（cache / dedup / scan）
├── frontend/               # Vue 3 前端
├── build/                  # 图标与构建资源
└── docs/                   # 设计 / 开发 / 测试文档
```

## 许可证

本项目源码按相应许可证发布，详见仓库内 LICENSE 文件（如有）。
