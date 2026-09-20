#!/usr/bin/env bash
# smoke-symlink.sh 自身判据的回归（2026-09-20，审查 M8）。
#
# 被验收脚本要解决的问题：它的价值恰恰在于「跑不了就说清楚跑不了」。
# 修正前三处缺陷让它做不到：
#   A. 无 root / 挂不上卷时 `exit 0` —— 跳过与全绿在门禁里**完全同形**，
#      哪天 runner 环境变了（无 loop、无 tmpfs、sudo 策略收紧）这条防线
#      会静默消失，而 CI 仍是绿的。
#   B. losetup 成功、mount 失败时 LOOP 被清空 → 占用的 loop 设备**不释放**，
#      反复运行会耗尽 /dev/loop*（同一 runner 上表现为后续作业莫名失败）。
#   C. `fail "...（%d）..."`：fail() 用 printf '%s' 渲染，%d 是**字面量**，
#      失败信息里看不到设备号——恰是排查最需要的数字。
#
# 这些断言在**当前代码**上会红（A/B/C 均未修），修好后转绿。

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$REPO_ROOT/scripts/smoke-symlink.sh"
STUBS="$(mktemp -d /tmp/fdd-smoke-test-stubs.XXXXXX)"
trap 'rm -rf "$STUBS"' EXIT

fails=0
ok() { printf '  ✓ %s\n' "$1"; }
bad() {
	printf '  \033[31m✗ %s\033[0m\n' "$1" >&2
	fails=$((fails + 1))
}

[ -f "$TARGET" ] || {
	bad "找不到 $TARGET"
	exit 1
}

# ---- 桩：把脚本驱动到各分支，无需真 root、不碰真设备 ----
cat >"$STUBS/id" <<'EOF'
#!/usr/bin/env bash
case "$*" in
*-u*) printf '%s\n' "${FAKE_UID:-0}" ;;
*) printf 'stub-id: %s\n' "$*" ;;
esac
EOF
cat >"$STUBS/mount" <<'EOF'
#!/usr/bin/env bash
# 一律失败：模拟"loop 与 tmpfs 都挂不上"的环境
exit 1
EOF
cat >"$STUBS/mkfs.ext4" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$STUBS/losetup" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$LOOP_LOG"
case "$1" in
--find) echo /dev/loop-9 ;;
*) : ;;
esac
EOF
chmod +x "$STUBS"/*

run_stubbed() {
	# $1 = FAKE_UID
	PATH="$STUBS:$PATH" FAKE_UID="$1" LOOP_LOG="$STUBS/losetup.log" \
		bash "$TARGET" >"$STUBS/out.$1" 2>&1
	echo $?
}

# ---- A1：非 root 必须是「跳过」而非「成功」----
say_title() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
say_title 'A1 非 root：退出码 2 且带 SKIP 标记'
rc="$(run_stubbed 1000)"
out="$(cat "$STUBS/out.1000")"
if [ "$rc" = "2" ]; then
	ok "退出码 = 2（区别于全绿的 0）"
else
	bad "非 root 时退出码 = ${rc}，应为 2：跳过若用 0，CI 里与全绿同形，环境退化时这条防线会静默消失"
fi
case "$out" in
*SKIP*) ok "输出含 SKIP 标记（供门禁精确放行）" ;;
*) bad "输出无 SKIP 标记，门禁无法区分「合法跳过」与「真失败」" ;;
esac
if printf '%s' "$out" | grep -qE '跳过'; then
	ok "输出说明原因"
else
	bad "输出未说明跳过原因"
fi

# ---- A2：loop 与 tmpfs 都不可用时同样必须是 SKIP ----
say_title 'A2 挂不上任何独立卷：退出码 2 且带 SKIP 标记'
rc="$(run_stubbed 0)"
out="$(cat "$STUBS/out.0")"
if [ "$rc" = "2" ]; then
	ok "退出码 = 2"
else
	bad "挂载全失败时退出码 = ${rc}，应为 2（当前是 0：静默通过）"
fi
case "$out" in
*SKIP*) ok "输出含 SKIP 标记" ;;
*) bad "输出无 SKIP 标记" ;;
esac

# ---- B：losetup 成功后 mount 失败，必须释放 loop ----
say_title 'B loop 未泄漏（losetup 建立、mount 失败）'
: >"$STUBS/losetup.log"
run_stubbed 0 >/dev/null
if grep -q -- '-d ' "$STUBS/losetup.log"; then
	ok "已调用 losetup -d 释放"
else
	bad "未释放 loop 设备（日志：$(tr '\n' ' ' <"$STUBS/losetup.log")）——反复运行会耗尽 /dev/loop*"
fi

# ---- C：失败信息里的设备号必须是真数字 ----
say_title 'C 设备号断言用 printf（不用 %d 字面量）'
if grep -qE 'fail "两个目录仍在同一设备' "$TARGET"; then
	bad "fail() 以 %s 渲染，传入 \"（%d）\" 会原样输出字面量，失败信息里看不到设备号"
else
	ok "不再把 %d 传给 fail()"
fi
if grep -qE 'printf.*两个目录仍在同一设备' "$TARGET" ||
	grep -qE 'fail "两个目录仍在同一设备.*\$\(printf' "$TARGET"; then
	ok "设备号经 printf 格式化"
fi

printf '\n'
if [ "$fails" -eq 0 ]; then
	echo "smoke-symlink-assert: 全部断言通过"
	exit 0
fi
echo "smoke-symlink-assert: $fails 条断言失败" >&2
exit 1
