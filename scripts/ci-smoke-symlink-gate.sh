#!/usr/bin/env bash
# 跨卷软链接冒烟的**判红臂**（D3 收归，2026-10-01；设计段
# docs/superpowers/specs/2026-10-01-unfulfilled-closure-design.md §4）。
#
# 为什么要有这个文件：D3 那段 shell 原来只住在 `.github/workflows/ci.yml` 的**内联 `run:` 块**里，
# 于是它没有可调用入口 ⇒ 谁都不执行它，除了 GitHub。而 ubuntu 腿的
# `sudo bash scripts/smoke-symlink.sh` 真挂得上 loop＋ext4、恒 rc=0，`2)` 那一臂
# 在 CI 上**一次都没跑过**（本机 harness 行 15 走的是 `sh_row`，也不是那一臂）。
# ⇒ 「D3 改对了」这一格至今只有反事实，没有读数。
#
# 本文件是那段内联逻辑的**行为等价迁移**：判据、文案、退出码、`::error::`、
# `$GITHUB_STEP_SUMMARY` 落笔、`PIPESTATUS` 取码，全部逐字照搬（划账里给 diff 结果）。
# 只做三处**环境耦合入口**，不改判定：
#   SMOKE_GATE_CMD     = 被测命令（默认 `sudo bash scripts/smoke-symlink.sh`）
#   SMOKE_GATE_LOG     = 输出留档路径（默认 /tmp/smoke-symlink.log，与改前同一处）
#   SMOKE_GATE_SUMMARY = 步骤摘要落笔处；**未指定时沿用 $GITHUB_STEP_SUMMARY**（与改前的内联块
#                        同一处，M407），两者都没有才落一次性临时文件
# ⇒ `scripts/smoke-symlink-assert.sh` 的 H 组能用桩把四条臂各打一次，**每次门禁都执行 `2)` 那一臂**。
#
# ★ 摘要为什么也是判据对象：D3 的价值有一半在"红的时候人看得见原因"。
#   只断言退出码 = 把那一半丢了（设计段 §4.2）。
# ★ 本机没有 GNU `timeout`（属 coreutils）时**明示**并不静默降级：那一格 CI 上仍然在用，
#   本脚本这里只是让桩驱动不必依赖它；rc=124 那一臂与 timeout 命令无关（码由被测命令给出）。
set -euo pipefail

cd "$(dirname "$0")/.." || exit 3

CMD="${SMOKE_GATE_CMD:-sudo bash scripts/smoke-symlink.sh}"
LOG="${SMOKE_GATE_LOG:-/tmp/smoke-symlink.log}"
SUMMARY_OWNED=0
if [ -n "${SMOKE_GATE_SUMMARY:-}" ]; then
	SUMMARY="$SMOKE_GATE_SUMMARY"
elif [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	# ★ 与改前的内联块同一处：CI 上判红文案必须由 runner 发布出去。
	SUMMARY="$GITHUB_STEP_SUMMARY"
else
	# 模板必须自带 XXXXXX：BSD mktemp 的 `-t 前缀` 会自动补，GNU 直接把参数当模板、
	# 没有 X 就报 "too few X's"（M407：本机全绿、CI ubuntu 腿第一步就红在这一格）。
	SUMMARY="$(mktemp "${TMPDIR:-/tmp}/fdd-smoke-gate-summary.XXXXXX")"
	SUMMARY_OWNED=1
fi

# step 级 timeout（AS-K5）：mount/losetup 坏的时候是**阻塞**而不是报错，
# 只靠作业级 timeout-minutes 会把整个 job 连日志一起废掉。
TIMEOUT_PREFIX=''
if command -v timeout >/dev/null 2>&1; then
	TIMEOUT_PREFIX='timeout --signal=TERM --kill-after=30s 420s'
else
	printf 'NOTE: 本机无 timeout/gtimeout，420s 那一层不受时长保护（CI 的 ubuntu 腿上有；rc=124 那一臂与此无关）\n'
fi

set +e
# M133：输出一份留档，供 rc=2 那一格核对自证标记（PIPESTATUS 取 timeout 的码，
# 不是 tee 的——取错就变成"只要 tee 成功就算通过"）。
$TIMEOUT_PREFIX $CMD 2>&1 | tee "$LOG"
rc=${PIPESTATUS[0]}
set -e

case "$rc" in
	0) echo "跨卷软链接 7 条判据全部成立" ;;
	124|137|143) msg="跨卷软链接冒烟超时（420s 未完成，rc=${rc}）——多半是 mount/losetup 阻塞"
		echo "::error::$msg"
		{
			echo '## ⏱ 跨卷软链接冒烟超时'
			echo
			echo "$msg"
			echo "超时按失败处理：阻塞与判据不成立都需要人来看。"
		} >>"$SUMMARY"
		exit 1 ;;
	2) if grep -q '^SKIP' "$LOG"; then
		# D3（推翻 M8/M133 的"环境受限不该怪代码"定性，2026-10-01 用户解封）：
		# 脚本自己认领为跳过 ⇒ 判据没跑，唯一一条真跨卷防线这一轮不存在。
		msg="跨卷软链接冒烟被跳过（runner 无法挂载独立文件系统）——该防线本轮未被执行；按 D3 裁定判红，不再与全绿同形"
		why="环境受限：loop/tmpfs/sudo 任一条不可用。判据 ①「硬链接跨卷必失败」只能在真实独立卷上实证，同卷无法降级验证。"
	else
		# M133 那一半原样保留：rc=2 却没有行首 SKIP 自证 ⇒ 是真失败，不是跳过。
		msg="rc=2 但输出里没有以 SKIP 开头的那行自证 ⇒ 按真失败判，不降级为跳过（M133）"
		why="被测脚本整体是 set -euo pipefail，中途任何命令以 2 退出都会长这样——必须人来分清是哪一种。"
	fi
	echo "::error::$msg"
	{
		echo '## ❌ 跨卷软链接冒烟未通过（rc=2 一律判红，D3）'
		echo
		echo "$msg"
		echo
		echo "$why"
	} >>"$SUMMARY"
	exit 1 ;;
	*) echo "::error::跨卷软链接冒烟失败（rc=${rc}）"; exit "$rc" ;;
esac

if [[ $SUMMARY_OWNED -eq 1 ]]; then
	rm -f "$SUMMARY"
fi
exit 0
