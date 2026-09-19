#!/usr/bin/env bash
# Windows 测试门禁（2026-09-18 审查 B3-4）。
#
# 原先 Windows job 整体 `continue-on-error: true`，等于该平台的测试完全不设防：
# 任何真实回归都会被"仅告警"吞掉。这里改成白名单式隔离——清单内的用例暂不执行，
# **其余用例一律阻断**。每加一条隔离都必须写清症状与判定，修好后从清单删除。
#
# ---------------------------------------------------------------------------
# 2026-09-19：短读（short read）根因修复后，本清单**已清空**。
#
# 原清单里的 5 个用例全部来自同一根因：
#   TestPipelineE2E / TestProgressAccountingConsistent
#     —— 子目录里的副本文件在 prefilter 阶段报 "unexpected EOF"（记录 size 大于
#        实际可读长度）
#   TestCacheMtimeContentChanged / TestSecondScanCompletes
#     —— 首扫 0 组，与上面同一现象家族（同为短读导致候选被剔除）
#   TestPipelinePauseResume
#     —— 暂停/恢复后组数不足，同为候选被剔除导致的组数偏少
#
# 根因：internal/hasher 的 HashHeadTail 把 io.ErrUnexpectedEOF 当成真实错误返回
# （只放过了 io.EOF），pipeline 阶段 2 据此把该文件 skip=true 整个剔除出预筛
# 分组 → 整组重复静默消失 + 该文件永远不入 pending 队列（缓存也不写）。
# 这正是用户报的「有扫描动作但扫不到重复文件，也未保存缓存」。
#
# 修复：短读不再视为错误，改用实际可读长度就地纠正条目再参与分桶（见
# hasher.Result.Short / ActualSize，以及 pipeline 阶段 2 的纠正分支）。
#
# 本次新增了针对该缺陷的回归用例（internal/hasher/shortread_test.go、
# internal/dedup/shortread_e2e_test.go），它们必须连同原 5 个用例一起在
# Windows 上跑起来——这正是当初被跳过的那一层保护。
# ---------------------------------------------------------------------------
set -euo pipefail

# 当前隔离清单：空。若需新增，请逐条写清症状与判定依据，并在修复后删除。
SKIP=''

exec go test -count=1 -skip "$SKIP" ./...
