#!/usr/bin/env bash
# 前端纯逻辑探针（M15 起，2026-09-21 全仓审计 §六 15）。
#
# 为什么需要这个脚本：仓库里前端只有 `vue-tsc --noEmit` + `vite build` 两道门禁，
# 它们能证明"类型对、能打包"，证明不了"界面上的句子是真的"。M15 那类缺陷
# （确认框对 hardlink 说"可释放"、结果条同一件事说"占用不变"）在 vue-tsc 下永远全绿。
#
# 为什么不装 vitest：加测试运行器属依赖决策（§6.8.5 同类），未擅自做。
# Node 22.18+/23+ 自带 TS 类型剥离与 test runner，本机实测可直接跑 `.test.ts`，
# 于是用零依赖方式拿到"行为级红"。代价：这些用例不受 vue-tsc 类型检查。
#
# ★ 跳过与全绿必须不同形（教训见 smoke-symlink-assert.sh 的 A 组）：
#   没有 node / node 太旧 → exit 2 并打 SKIP，绝不 exit 0。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FE="$REPO_ROOT/frontend"

skip() {
	printf 'SKIP: %s\n' "$1" >&2
	printf 'SKIP（前端逻辑探针未运行，不得读作全绿）\n' >&2
	exit 2
}

[ -d "$FE/tests" ] || skip "找不到 $FE/tests"
command -v node >/dev/null 2>&1 || skip "本机无 node，前端逻辑探针无法运行"

node -e '
const [maj, min] = process.versions.node.split(".").map(Number)
// 类型剥离默认开启：22.18 起（22.6 需 --experimental-strip-types），23+ 全程可用。
const ok = maj >= 23 || (maj === 22 && min >= 18)
process.exit(ok ? 0 : 1)
' || skip "node $(node -p 'process.versions.node') 不支持默认 TS 类型剥离（需 ≥22.18）"

cd "$FE" || exit 2
# 传 glob 而非裸目录：Node 会把目录参数当模块去解析（ERR_UNSUPPORTED_DIR_IMPORT）。
out="$(node --import ./tests/register.mjs --test 'tests/*.test.ts' 2>&1)"
rc=$?
printf '%s\n' "$out"

if [ "$rc" -ne 0 ]; then
	printf 'test-frontend-logic: 失败（node --test 退出码 %s）\n' "$rc" >&2
	exit 1
fi

# 防"绿因为啥也没跑"：必须能读到测试计数且 ≥1。
# 用 fail-closed 写法——将来 node 换了 reporter 文案，这里会红而不是静默通过。
tests_line="$(printf '%s\n' "$out" | grep -E '^[^ ]* *tests [0-9]+' | head -1)"
count="$(printf '%s' "$tests_line" | grep -oE '[0-9]+' | tail -1)"
if [ -z "$count" ] || [ "$count" -lt 1 ]; then
	printf 'test-frontend-logic: 未能确认有测试被执行（计数行：[%s]）\n' "$tests_line" >&2
	exit 1
fi
printf 'test-frontend-logic: %s 个用例全部通过\n' "$count"
