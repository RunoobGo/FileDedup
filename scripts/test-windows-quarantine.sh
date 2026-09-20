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
# ---------------------------------------------------------------------------
# 2026-09-20：跨卷软链接合并（SymlinkMerge）实施后，本清单**仍为空**。
#
# 但软链接在 Windows 上多了一层环境依赖，此处必须记明，否则将来 CI 变红会被
# 误当成代码回归：
#
#  1. 新增的软链接用例大多通过 requireSymlinkSupport() 自探环境：探不通就
#     t.Skip，**不会红**。这是刻意的——GitHub windows runner 的默认账户
#     通常未开开发者模式，创建符号链接会失败于 ERROR_PRIVILEGE_NOT_HELD。
#     若把它们改成硬断言，Windows job 会因**环境**而非代码失败。
#  2. 涉及"真实身份可解析"的用例（TestExecuteSymlinkUsesRealIdentityWhenAvailable）
#     在身份未解析时自 Skip；Windows 上 FileKey 按需解析，可能不解析。
#  3. platform 层的 createSymlink 两段式重试、错误码翻译（1314/87/50/5）
#     在 Linux 上**只经过 go vet 的交叉编译检查，未经真实调用**。
#     真正的平台行为验证需要真机（见 docs/04 §6.5 新增开放项 E）。
#
# 因此：Windows job 变红时，先看失败用例是否属于"环境不支持链接"这一类
# （日志里会有 t.Skipf 的原文）；属于则应对其补 requireSymlinkSupport，
# 而不是加进本清单掩盖。
# ---------------------------------------------------------------------------
set -euo pipefail

# 当前隔离清单：空。若需新增，**每行一条用例全名**（不带子测试路径），
# 并在同行注释里写清症状与判定依据，修复后删除。
#
# ★ AS-K2（2026-09-20 全仓审计）：清单条目的匹配语义从"子串"改为"整名锚定"，
# 并且每条都必须命中至少一个现存用例，否则本脚本直接红。两个理由：
#
#   ① go test 的 -skip 是**未锚定**的正则子串匹配。旧写法把条目写成裸名字，
#      TestCache 会连带跳过 TestCacheSecondScan、TestCacheMtimeContentChanged
#      以及**将来新增的任何同族用例**——隔离范围悄悄扩大，没人会察觉。
#   ② 命中 0 个用例的条目**永不过期**：用例改名/删除后清单还留着它，
#      于是"当前有多少测试被隔离"这件事永久失真，白名单变成暗坑。
QUARANTINE=()

# 新增条目时的形状（示例，勿解注释）：
# QUARANTINE=(
#   'TestPipelinePauseResume' # 暂停恢复后组数偏少，见 2026-09-19 说明
# )

if [ "${#QUARANTINE[@]}" -eq 0 ]; then
	exec go test -count=1 ./...
fi

# 1) 条目语法自检：只允许合法用例名（字母数字下划线，可含 . 与 / 之外的
#    Go 用例名字符）。掺进正则元字符会让"锚定"这件事失效或误伤一片。
for entry in "${QUARANTINE[@]}"; do
	if [[ ! "$entry" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
		printf 'FAIL: 隔离条目 %q 不是合法用例全名（只允许字母数字下划线；正则元字符一律不许，本脚本自己负责锚定）\n' "$entry" >&2
		exit 1
	fi
done

# 2) 编译并列出全部现存用例（不执行），逐条断言"至少命中 1 个"。
#    `go test -list` 只列举不跑；输出里 "ok  <pkg>" 之类的行不以 Test 开头，
#    用 ^Test 过滤即得用例名集合。
ALL_TESTS="$( { go test -list '.*' ./... 2>/dev/null || true; } | sed -n 's/^\(Test[A-Za-z0-9_]*\)$/\1/p')"
if [ -z "$ALL_TESTS" ]; then
	echo "FAIL: go test -list 没列出任何用例（构建失败？先跑 go vet ./...）" >&2
	exit 1
fi

patterns=''
for entry in "${QUARANTINE[@]}"; do
	n=$(printf '%s\n' "$ALL_TESTS" | grep -c "^${entry}\$" || true)
	if [ "$n" -eq 0 ]; then
		printf 'FAIL: 隔离条目 %q 命中 0 个现存用例——它已失效（用例改名/删除/从未存在），' \
			"$entry" >&2
		printf '请从清单删除；留着只会让"被隔离了多少测试"永久失真（AS-K2）。\n' >&2
		exit 1
	fi
	printf '    隔离 %-42s 命中 %s 个用例\n' "$entry" "$n"
	if [ -z "$patterns" ]; then
		patterns="^${entry}\$"
	else
		patterns="${patterns}|^${entry}\$"
	fi
done

# 3) 锚定后的正则交给 go test：`^(A|B)$` 只会整名匹配，
#    前缀相同的兄弟用例不受影响（AS-K2 的理由 ①）。
echo "==> go test -count=1 -skip '${patterns}' ./..."
exec go test -count=1 -skip "$patterns" ./...
