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
# 可覆盖（FRONTEND_DIR）：负控制要拿"故意改坏的那份"跑本套断言，证明门禁真的会红。
# 先例见 smoke-symlink-assert.sh 的 SMOKE_TARGET。
FE="${FRONTEND_DIR:-$REPO_ROOT/frontend}"
# 归一成绝对路径：下面要 `cd "$FE"` 跑 node，而接线断言在这之后仍按 $FE 找文件，
# 相对路径会在新 cwd 下错位（负控制实测：报"找不到文件"而不是报"写法被回退"）。
FE_ABS="$(cd "$FE" 2>/dev/null && pwd)" || { printf 'test-frontend-logic: 找不到前端目录 %s\n' "$FE" >&2; exit 2; }
FE="$FE_ABS"

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
# ---- 静态接线断言（M15 / M18）----
# 探针钉得住"判据本身对不对"，钉不住"组件有没有去用这个判据"：模板退回自己拼
# 字符串时，node 用例与 vue-tsc 都照样绿。这里补一刀最小必要的文本锚点。
# ★ 只锚标识符引用、不锚中文文案——改措辞是安全的，锚文案会误报。
wiring_fail=0
wiring_total=0
wiring() { # $1=文件 $2=必须出现 $3=禁止出现（可空） $4=说明
	wiring_total=$((wiring_total + 1))
	local file="$1" want="$2" forbid="$3" why="$4"
	if [ ! -f "$FE/$file" ]; then
		printf '  \033[31m✗ 找不到 %s，接线断言无法执行\033[0m\n' "$file" >&2
		wiring_fail=$((wiring_fail + 1))
		return
	fi
	if ! grep -qF -- "$want" "$FE/$file"; then
		printf '  \033[31m✗ %s（未引用 %s）\033[0m\n' "$why" "$want" >&2
		wiring_fail=$((wiring_fail + 1))
	elif [ -n "$forbid" ] && grep -qF -- "$forbid" "$FE/$file"; then
		printf '  \033[31m✗ %s（出现被禁写法：%s）\033[0m\n' "$why" "$forbid" >&2
		wiring_fail=$((wiring_fail + 1))
	else
		printf '  ✓ %s\n' "$why"
	fi
}
wiring 'src/components/GroupCard.vue' 'store.groupSelCount(group)' 'group.files.length - 1' \
	'组内冗余项数取自 store.groupSelCount（M18：组件不得自算）'
wiring 'src/components/ConfirmDialog.vue' 'reclaimLine(' 'humanBytes' \
	'确认框字节口径取自 utils/opdisplay（M15：组件不得自行格式化并配文案）'
# FE-1（2026-09-21 全量审查）：「打开回收站」原先无条件挂在结果条上，
# hardlink/delete 这类"一个文件都没进回收站"的操作也照挂。锚的是"按钮问过 TrashedBytes"
# 这件事本身——判据在 Go 侧（executor_account_test 钉死 trash 记它、delete/move 恒 0），
# 前端只许引用它，不许自己另记"我刚才请求了哪种操作"。
wiring 'src/views/ResultView.vue' 'v-if="store.opsResult.TrashedBytes"' '' \
	'「打开回收站」按回收站实测字节显示（FE-1：不得无条件挂在结果条上）'
# FE-2（穷尽断言在位）：真正的守卫是 vue-tsc —— 变异取证见 04 §6.11（给 OpKind
# 加一类 archive 却不补 switch 分支，两处各报一次 TS2322）。这里再锚一刀防的是
# 「把断言删掉、连 default 一起删」让类型检查重新变绿：两把锁互相兜。
wiring 'src/utils/opdisplay.ts' 'const _exhaustive: never = kind' '' \
	'opdisplay 的 switch 带 never 穷尽断言（FE-2）'
wiring 'src/components/ConfirmDialog.vue' 'const _exhaustive: never = props.kind' '' \
	'确认框的 switch 带 never 穷尽断言（FE-2）'
# FE-3：历史行三个按钮的禁用态收在 store.histBusy 一份。禁掉的正是那句
# 「组件自己拼 store.scanning || store.opsRunning」——它既是 store.busy 的第二实现，
# 又各自漏项（重扫漏 histLoading、删除什么都没挡），是「能点但点了出事」的温床。
wiring 'src/views/RecordsView.vue' 'store.histBusy' 'store.scanning || store.opsRunning' \
	'历史行禁用态取自 store.histBusy（FE-3：组件不得自拼互斥判据）'

if [ "$wiring_fail" -ne 0 ]; then
	printf 'test-frontend-logic: %s 条接线断言失败\n' "$wiring_fail" >&2
	exit 1
fi
# 计数不许说谎（G3，设计稿 §14.0）：原先打的是 "$count 个用例…（含 2 条接线断言）"，
# 而 $count 只数了 node 的 tests 行——接线断言是**另算**的，读起来像"20 里有 2"。
# 现在三个数各自真，接线数以实测计数为准（将来加条目不用再改文案）。
if [ "$wiring_total" -lt 1 ]; then
	printf 'test-frontend-logic: 一条接线断言都没执行（wiring_total=0）——判据面被删空了\n' >&2
	exit 1
fi
printf 'test-frontend-logic: node 用例 %s 项 + 接线断言 %s 项 = 合计 %s 项全部通过\n' \
	"$count" "$wiring_total" "$((count + wiring_total))"
