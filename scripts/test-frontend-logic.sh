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
# ★ M318（GATE-27，2026-09-28 第六轮全量审查）：只有 `tests ≥ 1` 挡不住"全绿其实是全跳过"。
#   node --test 里全部用例被 skip 时 fail=0 ⇒ rc=0、tests 行照样计数 ⇒ 这一行此前会打 PASS。
#   AS-K2 的在册规矩是"SKIP 不作通过"（第 15 行 smoke-symlink 就照这条写的），第 11 行没有同等判据。
#   口径：pass 行必须解析得出且 ≥1，解析不出直接红（沿用上面那条自己立的 fail-closed 规矩）；
#   skipped 只打印不判红——本仓 node 腿确有合法的环境性 skip 先例，一刀切判红会把门禁
#   训练成"大家都忽略的那一行"。
pass_line="$(printf '%s\n' "$out" | grep -E '^[^ ]* *pass [0-9]+' | head -1)"
passed="$(printf '%s' "$pass_line" | grep -oE '[0-9]+' | tail -1)"
if [ -z "$passed" ]; then
	printf 'test-frontend-logic: 未能读到 pass 计数行（[%s]）——全跳过与"换了 reporter 文案"都长这样，不得读作通过\n' "$pass_line" >&2
	exit 1
fi
if [ "$passed" -lt 1 ]; then
	printf 'test-frontend-logic: pass=%s 而 tests=%s ⇒ 一条都没真跑，SKIP 不作通过（AS-K2/M318）\n' "$passed" "$count" >&2
	exit 1
fi
skipped_line="$(printf '%s\n' "$out" | grep -E '^[^ ]* *skipped [0-9]+' | head -1)"
skipped="$(printf '%s' "$skipped_line" | grep -oE '[0-9]+' | tail -1)"
printf 'test-frontend-logic: tests=%s pass=%s skipped=%s（skipped 只报数、不判红）\n' \
	"$count" "$passed" "${skipped:-未读到}"
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
# wiring_count <文件> <串> <期望出现次数> <说明>
# ★ R4-4（2026-09-23 第四轮全仓审查）补的第二把尺子：`wiring()` 只有"出现 / 不出现"两态，
#   钉不住"**只此一处**"这一类判据。M80 那条例子就是活证据：锚从 `'head: true'` 换长成
#   `'error', 12000, { head: true }` 之后，把 `head` 从摘要行**挪到**明细行仍然绿
#   （明细行也含那段尾巴）——负控制实测过，见 §30.13 实施读数。计数判据才是"M80 只给摘要破例"
#   这句话的直译。仍然按接线数计入自报合计，不留"看不见的断言"（M119 同一把尺子）。
wiring_count() { # $1=文件 $2=串 $3=期望次数 $4=说明
	wiring_total=$((wiring_total + 1))
	local file="$1" needle="$2" want="$3" why="$4" n
	if [ ! -f "$FE/$file" ]; then
		printf '  \033[31m✗ 找不到 %s，接线断言无法执行\033[0m\n' "$file" >&2
		wiring_fail=$((wiring_fail + 1))
		return
	fi
	n=$(grep -cF -- "$needle" "$FE/$file")
	if [ "$n" = "$want" ]; then
		printf '  ✓ %s\n' "$why"
	else
		printf '  \033[31m✗ %s（%s 在 %s 里出现 %s 次，应为 %s 次）\033[0m\n' \
			"$why" "$needle" "$file" "$n" "$want" >&2
		wiring_fail=$((wiring_fail + 1))
	fi
}
# wiring_window <文件> <锚串> <下方行数> <须见串> <说明>
# ★ M215（2026-09-24 第五轮审查批）补的第三把尺子：wiring() 全文件 grep 钉不住
#   "**这一处**挂了 .catch"——scan.ts 里别处本就有十几条 .catch，把 refreshStatus 的
#    promise 退回裸 .then，全文件 grep 仍假绿（R4-4 同族缺口的行窗口版）。
#   语义：每一处锚串命中行的"本行起下方 N 行"窗口内都必须见须见串（多处锚 = 多处都要过）。
wiring_window() { # $1=文件 $2=锚串 $3=窗口行数 $4=须见串 $5=说明
	wiring_total=$((wiring_total + 1))
	local file="$1" anchor="$2" span="$3" want="$4" why="$5"
	local lines n missing=0
	if [ ! -f "$FE/$file" ]; then
		printf '  \033[31m✗ 找不到 %s，接线断言无法执行\033[0m\n' "$file" >&2
		wiring_fail=$((wiring_fail + 1))
		return
	fi
	lines=$(grep -nF -- "$anchor" "$FE/$file" | cut -d: -f1)
	if [ -z "$lines" ]; then
		printf '  \033[31m✗ %s（锚 %s 已不在文件中）\033[0m\n' "$why" "$anchor" >&2
		wiring_fail=$((wiring_fail + 1))
		return
	fi
	for n in $lines; do
		if ! sed -n "$((n)),$((n + span))p" "$FE/$file" | grep -qF -- "$want"; then
			missing=$((missing + 1))
		fi
	done
	if [ "$missing" = "0" ]; then
		printf '  ✓ %s\n' "$why"
	else
		printf '  \033[31m✗ %s（%s 处锚点下方 %s 行窗口内未见 %s）\033[0m\n' \
			"$why" "$missing" "$span" "$want" >&2
		wiring_fail=$((wiring_fail + 1))
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
# ★ R4-4（2026-09-23 第四轮全仓审查）：下面 M80 那条原先只锚 `'head: true'` 六个字，而
#   `wiring()` 是全文件 `grep -qF`（无行锚、无上下文）⇒ 只要这个文件里**任何**一处出现该串，
#   锚就成立，摘要行上的 `head` 被删掉、或被挪到别的 `toast.push` 上，本探针都不会红——
#   钉不住的正是要钉的那件事。★ 换成长锚 `'error', 12000, { head: true }` 也**不够**：明细行
#   （`ResultView.vue:69`）本就是 `toast.push(w, 'error', 12000)`，把 `head` 挪过去之后那段
#   尾巴照样在——本批真取到这个假绿（负控制读数见 §30.13 实施读数）。⇒ 三把尺子一起上：
#   ① 四参形状必须在（want）、② 明细行不得带 options 对象（forbid）、
#   ③ `{ head: true }` 全文件**恰一处**（wiring_count，直译"M80 只给摘要破例"那个"只"字）。
#   ★ 已知残角（如实登记，不装作钉全了）：把 `head` 挪到**溢出行**（`:70`）三把尺子都读不出来，
#     要钉它得锚那一行的中文模板，违反"不锚文案"这条规矩。
wiring 'src/views/ResultView.vue' "'error', 12000, { head: true }" \
	'toast.push(w, '"'"'error'"'"', 12000, {' \
	'Warnings 摘要行标了 head、明细行没标（M80：不标就会被自己投出的明细挤掉）'
wiring_count 'src/views/ResultView.vue' '{ head: true }' 1 \
	'head 只给摘要行破例（M80 的"只"字；R4-4 补的计数尺子）'
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
# M282（2026-09-27 实施批）：动手**之前**那句预告与动手**之后**那条徽标，必须是同一家的两半。
# 改前模态里写的是内联句「文件将移入系统回收站，可随时还原。」，而记录页给同一批文件
# 盖「不可回撤」——真机两张截图（b5_modal2 / b5_detail）。被禁写法就是那句原话：
# 只要它在视图里回来，用户就又拿到一条自相矛盾的承诺，而 node 用例抓不到（它测的是 utils）。
wiring 'src/components/ConfirmDialog.vue' 'TRASH_MODAL_DESC' '可随时还原' \
	'回收站模态正文取自 utils/undoReason（M282：不得在视图里内联"可随时还原"）'
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
# R3-3 / R3-4 / R3-5（2026-09-23 第四轮全仓审查，设计段 §30.9）：三条 UI 小修。
# 同样只锚标识符（①③）与"新写法的必要一行"（②——那是代码形状不是文案，锚它不会因改字误报）。
wiring 'src/components/ConfirmDialog.vue' 'store.procCountError' '' \
	'确认框看得见"命中数算失败了"（R3-3：不读 procCountError 就永久停在"正在核算…"，没有出口）'
# ★ 这一条**只有必须侧**：被禁的旧写法 `store.undoItem(m.id, it.id)`（裸调用、不等返回值）
#   是新代码 `const accepted = await store.undoItem(m.id, it.id)` 的子串，锚不住——
#   grep 走的是 -F 定长匹配，没有行尾锚可用。退回"锚新写法的必要一行"：
#   `if (!accepted) undoingItem.value = null` 一删就红（变异 M-c 实测）。
wiring 'src/views/RecordsView.vue' 'if (!accepted) undoingItem.value = null' '' \
	'单项回撤被互斥挡下时立即收回"执行中…"（R3-4：问的是 store 的返回值，不是视图自查 busy）'
wiring 'src/views/SettingsView.vue' 'confirmClearCache' 'if (!confirm(' \
	'清空缓存走两段式就地确认（R3-5：原生 confirm() 是全仓唯一残留，绕开 useModal 收口的那套）'

# M349（2026-09-29 设计段 §3.4）：清空缓存必须消费后端回执、并且不许把"只有空间
# 没回收"那一档说成整次失败。两条都锚代码形状：前者锚回执字段（改回 notifySuccess
# 写死一句话就红），后者用禁止侧钉那句假话（warn 档的写法本身已被必须侧锚住）。
wiring 'src/views/SettingsView.vue' 'res.reclaimedBytes' '' \
	'清空回执把"回收了多少字节"报给用户（M349：改前只有一个 error，"条目 0 但占用没变"无从反证）'
wiring 'src/views/SettingsView.vue' "toast.push(toast.errText(e), 'warn'" '清空缓存失败' \
	'回收失败那一档走 warn 且不得写「清空缓存失败」（M347/M349：条目已删净，那半截是真话）'

# M353（2026-09-29 设计段 §7）：记录页导出/导入四个形状。四条都锚**标识符/写法**，
# 不锚中文文案（改措辞安全）；每条都预测了变异，见 04 §6.65 划账。
wiring 'src/views/RecordsView.vue' 'api.exportRecords()' '' \
	'导出入口问的是后端 ExportRecords（M353：视图不得自己拼落盘，两件产物的成对性只在后端）'
wiring 'src/views/RecordsView.vue' 'api.importRecords()' 'if (!confirm(' \
	'导入走后端 + 两步式就地确认（M353/R3-5：原生 confirm() 在无边框窗体里合成不出来，等于没告知）'
# ★ 这一条锚的是"两个都刷"这件事本身：只刷当前 tab 会让导入显示成"导入没生效"，
#   而合并同时动了两张清单（扫描历史 + 清理记录），这正是本批要消灭的那类误读。
wiring 'src/views/RecordsView.vue' 'store.refreshHistory(), store.refreshOps()' '' \
	'导入完成后两张清单一起重取（M353：只刷一个 = 界面在讲半截真话）'
# ★ 这里刻意用 wiring_window 而不是 wiring()：`res.cancelled` 在本文件出现**两处**
#   （导出与各有一份），全文件 grep 的锚删掉任一处都照样绿——变异 MU-l 实测逃过，
#   正是 R4-4/M215 记过的那条缺口（"锚存在性"≠"每一处都在"）。行窗口把两处各自钉住。
wiring_window 'src/views/RecordsView.vue' 'await api.exportRecords()' 3 'res.cancelled' \
	'导出：取消走 cancelled 字段、不走异常（M353：把"点了又反悔"显示成一次失败是假话）'
wiring_window 'src/views/RecordsView.vue' 'await api.importRecords()' 3 'res.cancelled' \
	'导入：取消同样走 cancelled 字段（M353：同上，且这一处与导出各自独立钉，缺一即红）'

# 拟处理清单（2026-09-23，设计段 specs/2026-09-23-pending-files-query-design.md §5）：
# 抽屉与 store 状态机有 node 用例钉，但 .vue 打不进 node --test（M116/M118 同族限制）——
# 视图接线只能走锚。三条都锚**标识符**，不锚中文文案（改措辞安全）。
wiring 'src/views/ResultView.vue' 'store.openPendingDrawer()' '' \
	'结果页入口问的是 store 的清单动作（判据归后端、清单腿不许视图自算，AS-H6 同一纪律）'
wiring 'src/stores/scan.ts' 'api.getPendingFiles(' '' \
	'清单数据来自后端 GetPendingFiles（与预览同一内核的两个投影，前端不重算拟处理集）'
wiring 'src/components/PendingDrawer.vue' 'store.pendingData' '' \
	'抽屉渲染消费 store.pendingData（常驻挂载+store 控显隐，FailedDrawer 同族形状）'
# C3 / R-前端-4：分界数学抽进 utils/pending.ts（node 可测），抽屉只许消费——
# 锚"有没有 import 并调用 firstExcludedIndex"，防止边界逻辑又被内联回 .vue（.vue 打不进 node）。
wiring 'src/components/PendingDrawer.vue' 'firstExcludedIndex' '' \
	'抽屉分界下标取自 utils/pending.ts 的纯函数（R-前端-4：整页排除段标题置顶的边界，node 可测）'

# 功能 3（2026-09-23「隐藏非拟处理项」）：藏行的判据是后端逐行 isPending 投影，
# 视图只许消费（请求上送链由 scan-hide-nonpending.test.ts 的 node 用例钉）。
# .vue 打不进 node --test（M116/M118 同族限制），这里钉"有没有去用"这一刀。
# ★ 只锚标识符，不锚中文文案（改措辞安全）。
wiring 'src/components/GroupCard.vue' 'f.isPending' 'files.filter(f => f.isKeep' \
	'GroupCard 藏行消费后端 isPending（功能 3：组件不得按 isKeep/路径重算拟处理集，AS-H6 同一纪律）'
wiring 'src/views/ResultView.vue' 'g.files.some(f => f.isPending)' '' \
	'整组无拟处理项才隐藏整卡，判据同样取自 isPending 投影（功能 3）'
wiring 'src/views/ResultView.vue' 'store.toggleHideNonPending()' '' \
	'开关走 store 动作（重取/上送链收在 store，视图不自拼请求，功能 3）'

# M409（2026-10-03 判据批，销 04 §6.80 的 B 态一格）：结果页两个筛选控件的 v-model 必须绑在 store 上。
#   09-17 那份外部补丁把这里从局部 ref 改回 store（不然选了排序不生效），但**把它改坏两条通道都不红**：
#   node 用例与 vue-tsc 都抓不住——`resultSort`／`resultExt` 在 scan.ts 一起导出，互换绑定在类型上完全合法；
#   改前现读 `grep -n "resultSort" scripts/test-frontend-logic.sh` 零命中。
# ★ 锚必须**带上元素标签**（`<select …`／`<input …`），只锚"绑定+处理器"那一对还不够：
#   变异 V-a 实测——把两控件的 (v-model, @change) **成对互换**后，
#   `v-model="store.resultSort" @change="changeSort"` 这串只是从 `<select>` 挪到了 `<input>` 上，
#   全文件 `grep -qF`（wiring 的判据形态，无行锚无上下文）照样 rc=0 ⇒ 配对锚在这一格假绿。
#   带上标签后同一格必红（互换=整对搬家，`<select` 前缀串消失）。
#   裸 `v-model="store.resultSort"` 更早就假绿了（R4-4 在 M80 那条锚上实测抓到过同一形状，负控制读数在 04 §30.13）。
# ★ .vue 打不进 node --test（M116／M118 同族限制），所以这两条是**接线锚**，不得称行为级红。
wiring 'src/views/ResultView.vue' '<select v-model="store.resultSort" @change="changeSort"' '' \
	'排序下拉绑 store.resultSort 且挂 changeSort（M409：绑回局部 ref、两控件成对互换都必红）'
wiring 'src/views/ResultView.vue' '<input v-model="store.resultExt" @change="changeExt"' '' \
	'扩展名过滤框绑 store.resultExt 且挂 changeExt（M409 同形：与排序同用一份 store，漂回即静默失效）'

# 功能 4（2026-09-23「失败清单逐行打开文件/所在目录」）：抽屉的按钮腿。
# 交接与抛错这两件事由 frontend/tests/failed-reveal-path.test.ts 钉（那是 wails.ts
# 的 api 包装，node 打得进）；这里钉的是"抽屉有没有真的去调这两个包装"。
# ★ 只锚标识符，不锚中文文案（改措辞安全）。
wiring 'src/components/FailedDrawer.vue' 'api.revealPath(' '' \
	'失败清单逐行走后端路径绑定定位（功能 4：不得复用按 ID 的 revealInFolder，失败项没进结果集）'
wiring 'src/components/FailedDrawer.vue' 'api.openPath(' '' \
	'失败清单逐行走后端路径绑定打开（功能 4）'
# 被禁写法正是"前端先替后端判这条路径能不能开"：判据全在 Go 侧 checkRevealPath，
# 视图把按钮藏起来只会让用户以为功能坏了（M83 那一族"点了没反应"的镜像——这次是"没点可点"）。
wiring 'src/components/FailedDrawer.vue' 'api.openPath(' 'v-if="f.Path"' \
	'按钮不因路径看着为空就消失（功能 4：AS-H6，前端不重算判据）'
# M215（2026-09-24 第五轮审查批）：refreshStatus 的 getStatus 挂在事件回调路径上
# （scan:stage / app:ready），GetStatus 无 error 返回 ⇒ 传输层 reject 时裸 .then
# 漏出 unhandled rejection。判据是"该 .then 就地挂了 .catch"——全文件 grep 挡不住
# （别处本就有一堆 .catch），必须走行窗口。
wiring_window 'src/stores/scan.ts' 'api.getStatus().then' 5 '.catch' \
	'refreshStatus 的 getStatus promise 就地挂了 catch（M215：事件回调不得漏 rejection）'
# C2（R-前端-2，2026-09-24 第五轮审查批）：全局快捷键（Space/Cmd+A/Delete）的模态门
# 此前只判 preview/confirmOpen，漏了 pendingOpen/failedOpen——抽屉开着时画面是快照、
# 与全局勾选脱钩，Cmd+A 会在模态背后改勾选、Delete 静默清空。判据是"门条件里同时
# 挂了这两把锁"。.vue 打不进 node --test（M116/M118 同族限制），走行窗口锚：
# 锚在 gate 独有的 `!store.preview && !store.confirmOpen` 片段（template 的
# `store.view === 'result'` 不是 gate，不能当锚），下方 1 行内必须见两把新锁。
wiring_window 'src/App.vue' '!store.preview && !store.confirmOpen' 1 '!store.pendingOpen' \
	'快捷键模态门补 pendingOpen（C2：拟处理抽屉开着时不得在背后改全局勾选）'
wiring_window 'src/App.vue' '!store.preview && !store.confirmOpen' 1 '!store.failedOpen' \
	'快捷键模态门补 failedOpen（C2：失败抽屉开着时同上）'

# ---- M365（第八轮批 2，设计段 §2.1(g)）：两个维护口的前端在途锁 ----
# 后端本批补了 scan/ops 双闸，前端这两把锁是同一条承诺的另一半：改前 clearCache 连
# in-flight ref 都没有（同仓 exportRecords/importRecords 各有 exporting/importing），
# 而两个「清空」的确认态都写在 await **之前** —— RPC 在途期间按钮恢复成初态且不带
# :disabled，"确认清空"再点两下就并发发出第二、第三次请求，正好撞在闸的窗口上。
# ★ .vue 打不进 node --test（M116/M118 同族限制）⇒ 判据走静态锚点，"双击只发一次"
#   那一格仍挂 docs/05 真机清单（设计段 §2.5 第 6 条）。
wiring 'src/views/SettingsView.vue' 'const clearingCache = ref(false)' '' \
	'清空缓存有在途标记（M365：与本页导出/导入同一形状，改前一件都没有）'
wiring_count 'src/views/SettingsView.vue' ':disabled="clearingCache"' 3 \
	'清空缓存那一行的三枚按钮全挂在在途标记上（M365：只灰确认钮的话，入口钮与"取消"仍可再点一次）'
wiring_window 'src/views/SettingsView.vue' 'await api.cacheClear()' 1 'confirmClearCache.value = false' \
	'确认态在 RPC 落定之后紧接着才关（M365：窗口从 8 行收到 1 行是 MU-12 实测逼出来的——8 行窗口把 catch 里那句一起算了，移回 await 之前照样假绿）'
wiring_count 'src/views/SettingsView.vue' 'confirmClearCache.value = false' 2 \
	'确认态只在成功腿与失败腿各关一次（M365：多一处就是有人把它挪回 await 之前）'
wiring 'src/views/RecordsView.vue' 'const clearingOps = ref(false)' '' \
	'清空清理记录有在途标记（M365：store.busy 不含"正在清记录"这一档）'
wiring_count 'src/views/RecordsView.vue' ':disabled="store.busy || clearingOps"' 2 \
	'清空清理记录的入口与确认两枚按钮都追加在途标记（M365：缺一枚就还剩一次并发机会）'
wiring_window 'src/views/RecordsView.vue' 'await store.clearOps()' 5 'confirmClearOps.value = false' \
	'清理记录确认态在 RPC 落定之后才关（M365：同上）'
# ---- P-47（第九轮批 A2，拟 M381）：扫描历史的「确认清空」四层防线 ----
# 报告 P1-2：清空清理记录那一条腿有四层（后端 claimMaintenance + store guard + 按钮
# :disabled + 复位时机），扫描历史的删除/清空两条腿**四层里只有一层**（后端都没有，
# 是本批 A2 后端补的）。★ .vue 与 pinia store 打不进 node --test（M116/M118 同族限制）
# ⇒ 下面全部是**静态接线锚**，不得读作行为级红；行为半边由 P-45/P-46 的后端判据兜住。
wiring 'src/views/RecordsView.vue' 'const clearingHistory = ref(false)' '' \
	'清空扫描历史有在途标记（M381：形状同本页 clearingOps）'
wiring_count 'src/views/RecordsView.vue' ':disabled="store.busy || clearingHistory"' 2 \
	'清空历史的入口与确认两枚按钮都追加在途标记（M381：改前这两枚根本没有 :disabled）'
wiring_window 'src/views/RecordsView.vue' 'await store.clearHistory()' 5 'confirmClear.value = false' \
	'历史确认态在 RPC 落定之后才关（M381：改前写在 await **之前**，双击会并发发出第二次清空）'
wiring_window 'src/views/RecordsView.vue' 'await store.clearHistory()' 3 '} finally {' \
	'复位落在 finally 里（M381：只挪到成功腿的话，后端回绝那一路会把按钮永久卡灰）'
wiring 'src/stores/scan.ts' "if (!guard('清空全部历史')) return" '' \
	'clearHistory 走 store 的同一道忙门（M381：改前两条历史腿都绕过 guard，与 clearOps 不同形）'
wiring 'src/stores/scan.ts' "if (!guard('删除历史记录')) return" '' \
	'deleteHistory 走 store 的同一道忙门（M381：同上）'

# ---- FE-44 / FE-45（2026-10-03 审查 P1-4 / P2）----
# FE-44：明细抽屉是**本页唯一**缺代际锁的异步回写（store 里 resultGen / histGen /
#   procReqSeq / pendingReqSeq 四处同类都有）。缺它：A→B 快速切换且 A 的回包后到时，
#   opDetail 被写成 A 的明细而 expandedOp 仍是 B ⇒ B 行渲染 A 的条目。
#   ★ 后端**不会**搬错文件（UndoOperationItem 有归属校验），但用户会拿到一条
#   与真实原因无关的报错、点「直达保留原目录」还会打开 A 条目的目录。
# ★ .vue 组件打不进 node --test（M116/M118 同族限制）⇒ 这里是**静态接线锚**，
#   不得读作行为级红；行为半边由后端 app_ops.go 的归属校验兜住（本组不测那一格，
#   它在 M380 那批里）。
wiring 'src/views/RecordsView.vue' 'let detailSeq = 0' '' \
	'FE-44：明细抽屉有代际锁（改前本页四处同类回写都有锁，唯独它没有）'
wiring 'src/views/RecordsView.vue' 'if (seq !== detailSeq) return' '' \
	'FE-44：回写前校验代际（缺这一行则锁只是摆设，后发的旧回包照样盖回界面）'
wiring_count 'src/views/RecordsView.vue' 'if (seq !== detailSeq) return' 3 \
	'FE-44：三处代际校验（toggleOpDetail 的成功腿 + 失败腿 + reloadDetail）都得出这一句；少一处就那条腿仍会作废当行'
wiring 'src/views/RecordsView.vue' 'const seq = ++detailSeq' '' \
	'FE-44：序号在发请求**之前**自增（先发后自增的话，两趟的 seq 会相同）'
# FE-45：扫描历史/清理记录两个 tab 只有视觉态（.on），屏幕阅读器与色觉障碍用户
#   都判不出当前选中的是哪个 ⇒ 加 :aria-pressed（ResultView 的 hideNonPending
#   开关已有同款先例）。
wiring_count 'src/views/RecordsView.vue' ':aria-pressed="tab ===' 2 \
	'FE-45：两个 tab 都带 aria-pressed（缺一枚则那一枚仍只有视觉态）'
# FE-46（DDP-9 的可见半边）：导入降级说明**常驻**，不靠 toast。
#   为什么不能只靠 toast：那句话解释的是"为什么这批记录没有回撤按钮"，
#   而用户是**翻记录页**时才看到这件事的 —— 那时 toast 早没了。
#   M296 那条取向在这里继续成立：把原因摆成可见文本，不挂在会消失的浮层里。
wiring 'src/views/RecordsView.vue' 'const importDowngradeNote = ref(' '' \
	'FE-46：导入降级说明有自己的常驻态（挂在 toast 上则超时即失，用户看不到原因）'
wiring 'src/views/RecordsView.vue' 'v-if="importDowngradeNote"' '' \
	'FE-46：横幅真的渲染出来了（少这一行则状态有了但界面不显示）'
wiring_count 'src/views/RecordsView.vue' 'res.opsUndoDowngradeNote' 1 \
	'FE-46：后端那句话被原样转达（出现两处即有人在前端另拼一句，两句口径会分叉）'
wiring 'src/views/RecordsView.vue' 'role="status"' 'role="alert"' \
	'FE-46：横幅带 role="status"（不是 alert —— 它不是出错，只是少一个能力，alert 会打断朗读）'
wiring 'src/views/RecordsView.vue' 'aria-label="关闭这条说明"' '' \
	'FE-46：关闭钮有可访问名（只有图标时读屏只会念成"按钮"）'
# ★ 底色/文字色这一格是**对比度判据**：改前 --text-3 在 --primary-weak 合成底上只有
#   3.01:1（不达 AA 4.5），--text 是 14.72:1。这一条不许退回 --text-2/--text-3。
wiring 'src/views/RecordsView.vue' 'background: var(--primary-weak)' '' \
	'FE-46：横幅底色走 --primary-weak（记录照常导入了、只是少一个能力，不是坏消息 ⇒ 不用 --warn-weak）'

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
