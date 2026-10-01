#!/usr/bin/env bash
# CI 腿的 go test 包装（M331 / 04 §6.11 GATE-30，设计段
# docs/superpowers/specs/2026-10-01-d1-d3-m331-ci-design.md §2.5）。
#
# 要解决的问题：CI 三腿的 `go test` 都不带 `-v`，于是包级 `ok` 分不清
# 「这条 ok 的用例体内没有 t.Skip」与「有 Skip」——台账把 `ok` 抄成 PASS 就是
# 拿一个读不出来的量当读数。改前红（run 36820677331，三 job 全 success）：
# 三份逐 job 日志里 `^--- SKIP` = 0/0/0，而本仓含 t.Skip 的 `*_test.go` 有 59 个文件
# ⇒ skip 一条都读不出来，不是"没有 skip"。
#
# 为什么包一层脚本而不把管道直接铺在 YAML 里：`go test -v | 任何下游命令` 会让步骤
# 退出码变成管道末端那条命令的（`tee`/`sed` 成功就算过），要保住 go 自己的码就得
# `PIPESTATUS[0]`；这个形状在 YAML 里铺四遍，写歪一格就把"测试红"读成绿——
# 正是本文件要防的那类同形。这里 `|| rc=$?` 直接取 `go test` 的码，中间不过管道。
#
# 显示口径：`-v` 的全量输出在 22 包 × count=2 的量级上是数万行，会把"哪一步真红"
# 淹掉，所以 stdout 只删 `=== RUN` 与 `--- PASS`（顶层与子测试四种形状）；
# 失败详情行（`    file_test.go:NN: …`）、`--- FAIL`、包级 `ok`/`FAIL` 都不带 PASS
# 形状 ⇒ 可排查性不减。全量原文留在 $LOG（默认 /tmp/ci-go-test.log），需要时读它。
#
# SKIP 只打清册、不判红：本仓确有合法的环境性 skip 先例
# （scripts/test-windows-quarantine.sh 头段记的 Windows 符号链接那一族），一刀切判红
# 会把门禁训练成"大家都忽略的那一行"（同 M318 在第 11 行立的取向）。
# M331 要修的是"读不出来"，不是"不许跳过"。
# 〔2026-10-01 M404〕收尾那句仍然成立，但多了一条**同族不同轴**的红：清册实际打出的条数
# 与计数不等 ⇒ 红。它判的不是"有没有 skip"，是"这份读数自己完不完"（grep 的二进制判定
# 会让名单静默缺条，见下面读 $LOG 那一组 -a 的说明）。
#
# 计数口径与本机 harness 的行 6 对齐（scripts/run-gates.sh:136-156）：顶层 `^--- SKIP`
# 与子测试 `^    --- SKIP` 分开数、不混算。
set -uo pipefail

# 默认日志名带 PID：同一条腿里本脚本会被调两次（`./...` 与根包 `-count=4`），
# 固定名会让后一次把前一次的全量原文冲掉。路径写在 M331_READOUT 行里，要找就 grep 那行。
LOG="${CI_GO_TEST_LOG:-/tmp/ci-go-test-$$.log}"

if [ "$#" -eq 0 ]; then
	printf '用法: %s <go test 参数…>（-v 由本脚本补，勿重复传）\n' "$0" >&2
	printf '反例: 空参数会让 go test 收不到包列表 ⇒ 本脚本直接红，不放行。\n' >&2
	exit 2
fi
case " $* " in
*" -v "* | *" -v="*)
	printf 'FAIL: %s 自己会加 -v，调用方重复传会掩盖这条口径\n' "$0" >&2
	exit 2 ;;
esac

rc=0
go test -v "$@" >"$LOG" 2>&1 || rc=$?

# stdout：裁掉 RUN/PASS 四类噪声行，其余原样（含包级 ok/FAIL 与失败详情行）。
# sed 不做二进制判定，故这一行不加 -a（M404 §2：实测含串内 NUL 的那行照样穿得过去）。
sed -E '/^=== RUN/d; /^--- PASS/d; /^    --- PASS/d; /^    === RUN/d' "$LOG"

# 读 $LOG 一律带 -a（M404）：go test 日志里可能带串内 NUL 或非法编码字节（本仓就有这类
# 夹具）。grep 一旦把文件判成二进制就只打一行 "Binary file ... matches" 而不再给匹配行
# —— `-c` 计数照旧准，名单却静默缺条（windows 腿实测：skip_top=28 而清册只 10 条）。
# 计数与名单必须同一把尺：只给名单加 -a 会留下"数按二进制、列按文本"这种新的不同形。
skip_top=$(grep -a -c '^--- SKIP' "$LOG" || true)
skip_sub=$(grep -a -c '^    --- SKIP' "$LOG" || true)
fail_top=$(grep -a -c '^--- FAIL' "$LOG" || true)
fail_sub=$(grep -a -c '^    --- FAIL' "$LOG" || true)

# 名单先攒进变量：READOUT 行要报"实际打出去几条"，名字仍按原顺序在 READOUT 之后打印。
top_roster=$(grep -a '^--- SKIP' "$LOG" | sed -E 's/ \([0-9.]+s\)$//')
sub_roster=$(grep -a '^    --- SKIP' "$LOG" | sed -E 's/ \([0-9.]+s\)$//')
roster_top=$(printf '%s' "$top_roster" | grep -a -c . || true)
roster_sub=$(printf '%s' "$sub_roster" | grep -a -c . || true)

go_rc_note=""
[ "$rc" -eq 0 ] || go_rc_note=" go_rc=${rc}"
printf 'M331_READOUT rc=%s pkgs_ok=%s skip_top=%s skip_sub=%s roster_top=%s roster_sub=%s fail_top=%s fail_sub=%s raw_log=%s%s\n' \
	"$rc" "$(grep -a -cE '^ok[[:space:]]' "$LOG" || true)" \
	"$skip_top" "$skip_sub" "$roster_top" "$roster_sub" "$fail_top" "$fail_sub" "$LOG" "$go_rc_note"

# 有 skip 就把名单逐条打出来（去掉耗时尾巴，只留用例名）。
[ -n "$top_roster" ] && printf '%s\n' "$top_roster"
[ -n "$sub_roster" ] && printf '%s\n' "$sub_roster"

# 自检：清册条数必须等于计数。这条腿的读者是 CI，没有任何人在核对名单少了几行 ⇒
# "一条都没打"与"打了但缺 17 条"在日志里同样只是"少几行"，只有脚本自己数才不同形。
if [ "$roster_top" -ne "$skip_top" ] || [ "$roster_sub" -ne "$skip_sub" ]; then
	printf 'FAIL: SKIP 清册与计数不同源（roster_top=%s vs skip_top=%s，roster_sub=%s vs skip_sub=%s）⇒ 名单被截断，本轮 SKIP 读数不可信\n' \
		"$roster_top" "$skip_top" "$roster_sub" "$skip_sub" >&2
	[ "$rc" -eq 0 ] && rc=1
fi

exit "$rc"
