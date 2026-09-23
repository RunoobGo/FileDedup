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
# 本套断言在当前代码上**全绿**（2026-09-23 本机复跑：18 条断言、rc=0）；它守卫以下**五**类回归
# （回归一旦复现，本脚本必须红）：
#   A 跳过与全绿同形（必须 rc=2 + SKIP 标记）
#   B loop 泄漏（必须按 --find 返回的那个设备释放）
#   C 设备号 %d 字面量（失败信息里必须出现真数字）
#   D0 设备号闸门**放行**那一支（两卷不同号时必须越过闸门；R4-3，2026-09-23）
#   D 真失败分支（rc=1、含 FAIL 不含 SKIP、FAIL 即终止、设备号是真数字、同样释放 loop）
# ★ AS-K3（2026-09-20 全仓审计）：修正前本套**只驱动 skip 路径**——把被测脚本
# fail() 里的 `exit 1` 删掉，本套与 CI 两步仍全绿，即"能失败"这件事从未被验证。
# 现在补 D 组：桩出"挂载成功但两卷同号"，必须 rc=1、输出含 FAIL 不含 SKIP，
# 并在运行时断言设备号是真数字（C 从此不只是静态 grep）。

# 刻意不写 set -e：本脚本要**累计**多条断言后统一报告（rc 汇总自 fails 计数），
# 而"被测脚本以非 0 退出"正是若干断言的**预期值**，全局 errexit 会把它们
# 当成门禁自身的失败。setup 段的一次性动作改用 must() 逐个把关（AS-K3：
# 原先连桩文件写没写成都不检查，桩缺失时下面所有断言会以"红错原因"出现）。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# 被测脚本可覆盖（SMOKE_TARGET）：负控制要拿"故意改坏的那份"跑本套断言，
# 证明门禁真的会红。默认仍是仓库内的 smoke-symlink.sh。
TARGET="${SMOKE_TARGET:-$REPO_ROOT/scripts/smoke-symlink.sh}"
STUBS="$(mktemp -d /tmp/fdd-smoke-test-stubs.XXXXXX)"
trap 'rm -rf "$STUBS"' EXIT

# 被测脚本 losetup --find 的返回值由下面的桩给出；B 组据此**锚定**释放的设备，
# 不再接受"释放了某个 loop"这种宽松判据（AS-K3：放掉别的设备 = 泄漏照旧）。
LOOP_DEV=/dev/loop-9

# timeout：CI 的 ubuntu runner 一定有；本地 macOS 可能没有（属 GNU coreutils）。
# 没有时**明示**而不是静默不设防——挂死的桩命令会让门禁吃满 runner 时长。
RUN_TIMEOUT=''
if command -v timeout >/dev/null 2>&1; then
	RUN_TIMEOUT='timeout 90'
elif command -v gtimeout >/dev/null 2>&1; then
	RUN_TIMEOUT='gtimeout 90'
else
	printf 'NOTE: 本机无 timeout/gtimeout，桩运行不受时长保护（CI 的 ubuntu runner 上有）\n'
fi

fails=0
# M119（04 §6.11 GATE-3）：只判 fails==0 是不够的——一条断言都没走到时（桩挂错路径、
# case 全部 miss、某段被提前 return 跳过）fails 同样是 0，脚本却会打印"全部断言通过"
# 并 exit 0。本脚本的职责正是"自证冒烟判据本身有效"，它自己不能靠"什么都没验"骗过。
# ⇒ checks 由 ok/bad 各自累加，收尾要求它不低于 MIN_CHECKS。
# MIN_CHECKS=15 = 本机实跑 `grep -c "✓\|✗"` 的读数（A1/A2/B/C/D/E 六段各若干条；
# E 是 2026-09-22 第 3 轮 §24.5 补的 M134 静态防回归，14 是它进来之前的真读数）；
# 只设**下限**不设等号：以后加断言不必回来改这里，而"少跑到"一定会红。
# ★ 2026-09-23 R4-3 加了 D0 三条 ⇒ 本机实跑读数已是 **18**；下限**照旧不动**（15 是"少跑到"
#   的那道闸，不是"当前应有几条"的等号），这里只把"15 = 实跑读数"那句改成历史说明。
checks=0
MIN_CHECKS=15
ok() { checks=$((checks + 1)); printf '  ✓ %s\n' "$1"; }
bad() {
	checks=$((checks + 1))
	printf '  \033[31m✗ %s\033[0m\n' "$1" >&2
	fails=$((fails + 1))
}
# must = setup 段的硬前置：失败即门禁自身异常，立刻红，不带着残缺的桩往下跑。
must() {
	"$@" || {
		printf '  \033[31m✗ 门禁自身前置失败: %s\033[0m\n' "$*" >&2
		exit 1
	}
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
# 默认失败 = 模拟"loop 与 tmpfs 都挂不上"；FAKE_MOUNT_OK=1 时成功，
# 用于驱动 D 组：挂载这一步过了、但两个目录仍在同一设备（真失败分支）。
if [ "${FAKE_MOUNT_OK:-0}" = 1 ]; then exit 0; fi
exit 1
EOF
cat >"$STUBS/umount" <<'EOF'
#!/usr/bin/env bash
exit 0 # 桩挂载无需真卸载；cleanup 走到这里必须是 no-op
EOF
cat >"$STUBS/mkfs.ext4" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$STUBS/losetup" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$LOOP_LOG"
case "$1" in
--find) echo "${LOOP_DEV:-/dev/loop-9}" ;;
*) : ;;
esac
EOF
cat >"$STUBS/stat" <<'EOF'
#!/usr/bin/env bash
# 只接管 `stat -c %d <path>`（设备号），其余原样交给真 stat。
# FAKE_DEV_SAME=1 → 两个路径报同一个号，制造"挂载点其实同在一卷"的失败前提。
#
# ★ R4-3（2026-09-23 第四轮全仓审查）：卷 B 的识别原来靠"参数带尾斜杠"
#   （`[ "${3%/}" != "$3" ]`），而被测脚本两处调用（smoke-symlink.sh:114-115）传的是
#   `$WORK` 与 `$MNT="$WORK/mnt"`，**都不带尾斜杠** ⇒ 那一格从未触发过，桩永远回
#   42/42，"闸门放行"那一支从来没被驱动（D0 组改前红就是这么取到的）。
#   现在按**末段目录名**认卷 B：与被测脚本的 `MNT="$WORK/mnt"` 对齐。
#   ★ 这是一处**跨文件耦合**：被测脚本若改挂载点目录名，这里要同改——而改漏的表现是
#     D0 第一条转红（"闸门没有放行"），不会静默，所以不额外加静态钉。
if [ "$#" -eq 3 ] && [ "$1" = "-c" ] && [ "$2" = "%d" ]; then
	if [ "${FAKE_DEV_SAME:-0}" = 1 ]; then
		echo 42
	elif [ "${3##*/}" = "mnt" ]; then
		echo 7 # 挂载点（卷 B）：默认与宿主不同号
	else
		echo 42
	fi
	exit 0
fi
exec /usr/bin/stat "$@"
EOF
chmod +x "$STUBS"/*
must [ -x "$STUBS/mount" ]
must [ -x "$STUBS/stat" ]
must [ -f "$TARGET" ]

run_stubbed() {
	# $1 = 输出标签，$2 = FAKE_UID；其余开关（FAKE_MOUNT_OK / FAKE_DEV_SAME）
	# 由调用方以 `VAR=1 run_stubbed ...` 前缀传入，随环境继承给被测脚本。
	local tag="$1" uid="$2"
	PATH="$STUBS:$PATH" FAKE_UID="$uid" LOOP_LOG="$STUBS/losetup.log" LOOP_DEV="$LOOP_DEV" \
		$RUN_TIMEOUT bash "$TARGET" >"$STUBS/out.$tag" 2>&1
	echo $?
}

# ---- A1：非 root 必须是「跳过」而非「成功」----
say_title() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
say_title 'A1 非 root：退出码 2 且带 SKIP 标记'
rc="$(run_stubbed a1 1000)"
out="$(cat "$STUBS/out.a1")"
if [ "$rc" = "2" ]; then
	ok "退出码 = 2（区别于全绿的 0）"
else
	bad "非 root 时退出码 = ${rc}，应为 2：跳过若用 0，CI 里与全绿同形，环境退化时这条防线会静默消失"
fi
# M142（第 3 轮 §24.4，GATE 家族）：判据与 ci.yml:160 的 `grep -q '^SKIP'` **对齐到同一锚**。
# 改前这里是 `case "$out" in *SKIP*)` ——任意位置含 "SKIP" 五个字就算过：
# 被测脚本只要在任何一句话里提到"SKIP"（注释、提示、甚至"这不是 SKIP"），
# 跳过自证就成立了，而真正被 CI 认领的是"有一行以 SKIP 开头"。
# 守契约的那道闸比契约本身还松，是"一条纪律多份实现、最弱者守契约"的 I5 形状。
skip_anchored() { printf '%s\n' "$1" | grep -q '^SKIP'; }
if skip_anchored "$out"; then
	ok "输出含行首 SKIP 标记（与 ci.yml 的认领口径同锚）"
else
	bad "输出没有以 SKIP 开头的行，门禁无法区分「合法跳过」与「真失败」"
fi
if printf '%s' "$out" | grep -qE '跳过'; then
	ok "输出说明原因"
else
	bad "输出未说明跳过原因"
fi

# ---- A2：loop 与 tmpfs 都不可用时同样必须是 SKIP ----
say_title 'A2 挂不上任何独立卷：退出码 2 且带 SKIP 标记'
rc="$(run_stubbed a2 0)"
out="$(cat "$STUBS/out.a2")"
if [ "$rc" = "2" ]; then
	ok "退出码 = 2"
else
	bad "挂载全失败时退出码 = ${rc}，应为 2（当前是 0：静默通过）"
fi
if skip_anchored "$out"; then
	ok "输出含行首 SKIP 标记（同 A1 的锚，M142）"
else
	bad "输出没有以 SKIP 开头的行"
fi

# ---- B：losetup 成功后 mount 失败，必须释放**那个** loop ----
say_title 'B loop 未泄漏（losetup 建立、mount 失败）'
must : >"$STUBS/losetup.log"
run_stubbed b 0 >/dev/null
# ★ AS-K3：判据从"释放过某个 loop"收紧为"释放的就是 --find 返回的那个"。
# 旧写法 `grep -- '-d '` 连 `-d /dev/loop-3` 都算通过，被泄漏的 loop-9 照旧占着。
if grep -qF -- "-d $LOOP_DEV" "$STUBS/losetup.log"; then
	ok "已按 $LOOP_DEV 精确释放（loop 释放日志锚定到 --find 的返回值）"
elif grep -qF -- '-d ' "$STUBS/losetup.log"; then
	bad "释放的是别的设备而非 ${LOOP_DEV}（日志：$(tr '\n' ' ' <"$STUBS/losetup.log")）——${LOOP_DEV} 仍被占用"
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
else
	# AS-K3：这条原先没有 else 分支——不满足时既不 ok 也不 bad，
	# 断言静默消失而 fails 计数不变，等于**没写**。
	bad "设备号未经 printf 格式化（%d 会被 fail() 的 %s 原样打出，排查时看不到设备号）"
fi

# ---- D0：设备号闸门必须"认得出两个不同的卷"（R4-3，2026-09-23 第四轮全仓审查）----
# 上面 D 组桩的是"两卷同号 ⇒ 必须 FAIL"，但**反方向从来没人钉过**：没有任何一条
# 断言证明"两卷不同号时闸门会放行"。闸门被写成恒假（例如 `[ "$A_DEV" != "$B_DEV" ]`
# 退回 `=`）时，本套与 CI 仍全绿——因为所有 leg 喂给它的都是同号的 42/42。
# 这是"守卫静默空转"家族的又一形：**只测了判据为假的那一支**。
#
# ★ 本组钉的**只有**"闸门放行 + 放行之后脚本仍在跑"。它**不**是"七条判据全成立"：
#   桩只能接管 id/mount/umount/mkfs.ext4/losetup/stat -c %d，而 ① 的
#   `ln "$KEEP" "$MNT/hardlink-try"`（被测脚本 :127）是**真系统调用**——桩环境下 $MNT
#   只是 $WORK 下的普通子目录（同一真卷），硬链接必然成功 ⇒ :128 fail ⇒ rc=1。
#   真 rc=0 只在 Linux runner 真挂上第二卷时可得（本机 BSD stat 连 `stat -c %s` 都不认）。
say_title 'D0 设备号闸门放行：桩给两个不同号时必须越过闸门'
must : >"$STUBS/losetup.log"
rc="$(FAKE_MOUNT_OK=1 run_stubbed d0 0)"
out="$(cat "$STUBS/out.d0")"
# ① 闸门放行的唯一可见证据：那一行"卷 A dev=… ｜ 卷 B dev=…"打出来了，且两个号不同。
if printf '%s\n' "$out" | grep -qF '卷 A dev=42 ｜ 卷 B dev=7'; then
	ok "两卷设备号不同（42 / 7）时闸门放行，脚本继续往下跑"
else
	bad "两卷设备号不同（42 / 7）时闸门没有放行——设备号判据恒假无人察觉（R4-3）：$(printf '%s' "$out" | tail -3 | tr '\n' ' ')"
fi
# ② 放行之后必须**停在跨卷语义上**，而不是又落回设备号那一格。
case "$out" in
*'两个目录仍在同一设备'*) bad "越过闸门后却又报「同设备」⇒ 判据被走了两遍或桩没生效" ;;
*) ok "失败点不在设备号闸门（放行是真的放行了）" ;;
esac
if [ "$rc" != "0" ]; then
	ok "本机桩下必然非 0（rc=${rc}）：硬链接跨「假卷」会成功 ⇒ ① 抓到前提不成立，这是**预期**而非通过"
else
	bad "rc=0 反而可疑：桩环境是**单真卷**，① 的硬链接不可能失败；它若过了，说明被测脚本没真跑 ln"
fi

# ---- D：真失败分支必须 rc=1 且报 FAIL（AS-K3 的核心补桩）----
# 修正前本套只驱动 skip 路径：把被测脚本 fail() 的 `exit 1` 删掉，本套与
# CI 两步仍全绿——"它能失败"从来没被验证过。这里桩出"挂载成功、但两个目录
# 仍在同一设备"，让 fail() 必须被走到。
say_title 'D 真失败分支：rc=1、含 FAIL、不含 SKIP、设备号是数字'
must : >"$STUBS/losetup.log"
rc="$(FAKE_MOUNT_OK=1 FAKE_DEV_SAME=1 run_stubbed d 0)"
out="$(cat "$STUBS/out.d")"
if [ "$rc" = "1" ]; then
	ok "退出码 = 1（区别于合法的 2 与全绿的 0）"
else
	bad "真失败时退出码 = ${rc}，应为 1：fail() 若不 exit 1，CI 会把功能回归读成全绿"
fi
case "$out" in
*FAIL*) ok "输出含 FAIL 标记" ;;
*) bad "输出无 FAIL 标记（失败未自证；被测脚本的 fail() 可能没跑到或被吞）" ;;
esac
case "$out" in
*SKIP*) bad "同设备前提失败却被报成 SKIP——跳过与真失败再次同形（M8 的原始缺陷）" ;;
*) ok "输出不含 SKIP（真失败不会被门禁当作合法跳过放行）" ;;
esac
# 负控制实测：只断言 rc=1 抓不到"fail() 不再 exit"的变异（脚本会继续往下跑，
# 后面某步偶然非 0，rc 照样是 1）。必须同时断言**它没能继续**：
# 紧跟失败点之后的那条 echo（卷 A/B 设备号）不该出现在输出里。
case "$out" in
*'卷 A dev='*) bad "FAIL 之后脚本仍在往下跑（fail() 没终止它）：rc 可能是别的步骤凑出来的 1" ;;
*) ok "FAIL 即终止（失败点之后的语句一条都没执行）" ;;
esac
# C 的运行时版本：静态 grep 只能证明"代码里有 printf"，这里证明"打出来的是数字"。
case "$out" in
*'A=42 B=42'*) ok "失败信息带出真实设备号（A=42 B=42）" ;;
*'%d'*) bad "失败信息里残留 %d 字面量，设备号没被格式化出来" ;;
*) bad "失败信息未带设备号（A=… B=…），排查时无从判断卷是否真的同号" ;;
esac
# 挂载成功后仍要按 $LOOP_DEV 释放（D 走的是 loop 分支，泄漏面与 B 同源）
if grep -qF -- "-d $LOOP_DEV" "$STUBS/losetup.log"; then
	ok "D 组路径同样释放了 $LOOP_DEV"
else
	bad "D 组路径未释放 ${LOOP_DEV}（日志：$(tr '\n' ' ' <"$STUBS/losetup.log")）"
fi

# ---- E：静态防回归——`$VAR` 紧跟全角字符（M134 的成因，第 3 轮 §24.5）----
# M134 修的是"bash 3.2 在 LC_CTYPE=C.UTF-8 下把全角字符首字节算进变量名"，
# 症状是 set -u 报 unbound variable、门禁红而归因指向环境。修法当时是**逐处**
# 把 `$VAR` 改成 `${VAR}`——那是一次性修复，没有任何东西阻止下一处再写出来。
# 本组就是那道"阻止"：把规矩变成机器判据（M134 欠的静态断言，§24.5）。
#
# 只扫代码行：整行注释（含 YAML 的 # 注释）里的举例不构成运行时风险，
# 而本仓的注释正好就有 `$LOOP）` 这一形（M134 自己的说明段）。
say_title 'E 门禁脚本里没有 $VAR 紧邻全角字符的写法（M134 静态防回归）'
NA_BYTE="$(printf '\200-\377')"
if [ -n "${M134_SCAN_FILES:-}" ]; then
	scan_list="$M134_SCAN_FILES"
else
	scan_list="$(printf '%s\n' "$REPO_ROOT"/scripts/*.sh "$REPO_ROOT/.github/workflows/ci.yml")"
fi
scanned=0
m134_bad=''
for f in $scan_list; do
	if [ ! -f "$f" ]; then
		m134_bad="$m134_bad ${f}(读不到)"
		continue
	fi
	scanned=$((scanned + 1))
	hit=$(LC_ALL=C grep -v '^[[:space:]]*#' "$f" |
		LC_ALL=C grep -nE "\\\$[A-Za-z_][A-Za-z0-9_]*[${NA_BYTE}]" | head -3)
	if [ -n "$hit" ]; then
		m134_bad="$m134_bad ${f##*/}:$(printf '%s' "$hit" | tr '\n' ' ')"
	fi
done
# 下界：清单为空 / glob 没展开时，"没有命中"是假的通过（M119 同一个坑）。
if [ "$scanned" -lt 8 ]; then
	bad "只扫到 ${scanned} 个文件（7 个 scripts/*.sh + ci.yml = 8）⇒ 扫描面本身没成立，不读作通过"
elif [ -z "$m134_bad" ]; then
	ok "${scanned} 个门禁脚本的代码行里没有 \$VAR 紧邻全角字符"
else
	bad "M134 复发：\$VAR 直接贴着全角字符，bash 3.2 会把全角首字节读进变量名 ⇒${m134_bad}"
fi

printf '\n'
printf 'smoke-symlink-assert: 走到断言 %s 条（下限 %s 条），其中失败 %s 条\n' \
	"$checks" "$MIN_CHECKS" "$fails"
if [ "$fails" -gt 0 ]; then
	echo "smoke-symlink-assert: $fails 条断言失败" >&2
	exit 1
fi
# M119：条数不足 ⇒ 有整段没被执行。这时候 "fails=0" 不代表判据成立，只代表没验。
if [ "$checks" -lt "$MIN_CHECKS" ]; then
	echo "smoke-symlink-assert: 只走到 ${checks} 条断言（应不少于 ${MIN_CHECKS} 条）" \
		"⇒ 有分支整段没被执行，不读作通过" >&2
	exit 1
fi
echo "smoke-symlink-assert: 全部断言通过（A 跳过 / B 释放 / C 设备号 / D0 闸门放行 / D 真失败 / E 静态防回归）"
exit 0
