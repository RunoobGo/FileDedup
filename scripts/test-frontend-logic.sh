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
# FE-4/FE-7/FE-8/FE-10（§20 第九批，2026-09-22）：三条"判据收在一份、组件只许引用"的锚。
# 与 FE-3 同理由——探针钉得住判据本身，钉不住"组件有没有去用它"；模板退回内联拼串时
# node 用例与 vue-tsc 都照样绿。★ 只锚标识符/写法，不锚中文文案（改措辞是安全的）。
wiring 'src/views/ResultView.vue' 'opFailedLabel(' '失败 {{ formatCount(store.opsResult.Failed.length) }}' \
	'失败按钮措辞取自 opFailedLabel（M81：组件不得自拼"失败 N"，那是本次数与全量数不同源的成因）'
wiring 'src/views/ResultView.vue' 'head: true' '' \
	'Warnings 摘要行标了 head（M80：不标就会被自己投出的明细挤掉）'
wiring 'src/components/FailedDrawer.vue' 'copyText(' 'navigator.clipboard?.writeText(' \
	'复制全部走 utils/clipboard 的 copyText（M83：可选链短路时 .catch 从未挂上，无剪贴板环境完全静默）'
# M116 / M118（第 2 轮 §23.5）：展示面两处「判据收在一份、组件只许引用」。
# M116：同一句「秒级时间戳 → 本地时间串」在三个视图各写一遍内联串，而 utils/format 里
#	早就有 formatMtime；两份实现可以各自漂移（hour12 / locale / 是否带秒），
#	改一处忘两处时界面上会出现两种时间写法。锚的是「视图引用 formatUnixSec」。
# M118：ScanView 的进度条自己写了 `Math.min(100, …)`——有上限、无下限、无 NaN 防，
#	而同一 UI 里 ResultView 用的是三重钳的 percentOf。判据本体（越界钳住 + 非法值回 0）
#	已由 opdisplay.test.ts 的两条 node 用例钉住，这里补的仍是「有没有去用」这一刀：
#	.vue 无法被 node --test 导入，行为探针打不到视图，只能走接线锚。
wiring 'src/utils/format.ts' 'export function formatUnixSec' '' \
	'秒级时间格式化收在 utils/format（M116 落点）'
wiring 'src/views/ScanView.vue' 'formatUnixSec(' '.toLocaleString(' \
	'扫描页历史时间引用 formatUnixSec（M116：不得内联拼日期串）'
wiring 'src/views/RecordsView.vue' 'formatUnixSec(' '.toLocaleString(' \
	'记录页时间引用 formatUnixSec（M116）'
wiring 'src/views/ResultView.vue' 'formatUnixSec(' '.toLocaleString(' \
	'结果页历史时间引用 formatUnixSec（M116）'
wiring 'src/views/ScanView.vue' 'percentOf(' 'Math.min(100, ' \
	'扫描页进度条取自 percentOf（M118：不得自拼只夹上限的百分比）'
# M141（第 3 轮 §24.4 FE-14）：「默认源码」chip 原先只判 overRenderCap，而 overRenderCap
# 量的 store.preview.content.length 对**图片**也成立——图片腿不经文本截断（512px / q85
# 缩略图 base64 后本机实测 264,680–265,132 B，见 M148），阈值 64 KiB 打得到它。于是预览一
# 张大图会挂出「默认源码」，而图片既没有渲染态也没有源码态。chip 的语义只对 Markdown 成立，
# 锚的就是「chip 问过 isMd 没有」；判据本体是 :30 的 isMd，这里钉的仍是模板接线（.vue 打不进
# node --test，先例 M116/M118）。★ 锚写法、不锚中文文案。
wiring 'src/components/PreviewPanel.vue' 'v-if="isMd && overRenderCap"' 'v-if="overRenderCap"' \
	'「默认源码」chip 受 isMd 约束（M141：图片预览不得挂出该 chip）'
# M79（2026-09-22 裁定"判据归后端、文案归前端"，设计段 §28.1）：「这条记录为什么不可回撤」
# 那两句中文原先有**两份**——后端 app.go 的 error 正文（进 toast）与本文件的徽标 title，
# 两份措辞已经各自漂移。现在后端只下发原因码，中文全仓只写在 `src/utils/undoReason.ts`。
# 锚的是"消费方有没有去问那一家"：视图问 title 体、store 的 catch 问 toast 体；
# 被禁写法正是搬家前的形状（视图里内联整句 / store 把后端串直接当正文投出去）。
# ★ 禁串取的是搬家前文案的一段稳定前缀，改文案时要同步改这里——但改文案本身
#   已经是"只有 undoReason.ts 能动"的动作，漂移空间比改前小一个量级。
wiring 'src/views/RecordsView.vue' 'undoBlockedTitle(' '不支持应用内回撤：系统 API' \
	'记录页的不可回撤说明取自 utils/undoReason（M79：不得在视图里内联整句）'
wiring 'src/stores/scan.ts' 'undoBlockedText(' "notifyError('回撤失败', e)" \
	'回撤失败的 toast 正文经 utils/undoReason 翻译（M79：后端串不得原样上屏）'
# R3-1（2026-09-23 第四轮全仓审查，设计段 §30.7）：三处"唯一判据/唯一文案方"的漏网入口。
# ★ 三条都锚**标识符**、禁的是**被取代的旧写法** ⇒ 日后改措辞不会误报。
wiring 'src/components/GroupCard.vue' 'selectionWording(' '标记为删除' \
	'勾选项措辞取自 utils/opdisplay 的 selectionWording（R3-1：组件不得内联"删除"承诺——五种操作只有 delete 是删除）'
wiring 'src/views/ScanView.vue' 'store.histBusy' ':disabled="store.opsRunning"' \
	'扫描页历史横幅的恢复按钮问 store.histBusy（R3-1：FE-3 判据的漏网点，openHistory 在途可再点）'
# ★ 必须侧锚的是**标识符**（逐条徽标的判据本身），不是新写的中文句：本脚本开头就立了
#   "只锚标识符、锚文案会误报"这条规矩，这里若锚新文案，将来改一句话就多一处假红。
#   新文案本身的真伪归 node 层（frontend/tests/selection-wording.test.ts 读源文件断言）。
wiring 'src/views/RecordsView.vue' 'm.undoable' '并支持对回收站' \
	'记录页空态不再替所有记录打包票，可撤性指回逐条徽标（R3-1：Windows 回收站不可应用内回撤）'

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
