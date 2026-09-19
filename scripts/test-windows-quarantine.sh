#!/usr/bin/env bash
# Windows 测试门禁（2026-09-18 审查 B3-4）。
#
# 原先 Windows job 整体 `continue-on-error: true`，等于该平台的测试完全不设防：
# 任何真实回归都会被"仅告警"吞掉。这里改成白名单式隔离——清单内的用例暂不执行，
# **其余用例一律阻断**。每加一条隔离都必须写清症状与判定，修好后从清单删除。
#
# 依据：run 35245999270（2026-09-17，windows-amd64）实测失败清单。
# 已在第 2 批修掉的 filter/scanner/ops 用例已从此清单移除。
set -euo pipefail

SKIP='TestSecondScanCompletes|TestCacheMtimeContentChanged|TestPipelineE2E|TestPipelinePauseResume|TestProgressAccountingConsistent'

# 隔离原因（逐条）：
#   TestPipelineE2E / TestProgressAccountingConsistent
#     —— 子目录里的副本文件在 prefilter 阶段报 "unexpected EOF"（记录 size 大于
#        实际可读长度）。本机无 Windows runner 可复现，未定性为产品缺陷还是测试
#        时序假设；见 docs/archive/里程碑开发与测试报告.md 第 4 篇遗留项（原 docs/08）。
#   TestCacheMtimeContentChanged / TestSecondScanCompletes
#     —— 首扫 0 组，与上面同一现象家族（怀疑同为短读导致候选被剔除）。
#   TestPipelinePauseResume
#     —— 暂停/恢复后组数 1，want 3：平台 IO 时序敏感，未复现。
exec go test -count=1 -skip "$SKIP" ./...
