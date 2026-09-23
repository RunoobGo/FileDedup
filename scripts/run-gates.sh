#!/usr/bin/env bash
# 全套回归门禁 harness（04 §3.3 那 11 条命令 + 两条补的，共 **15 行读数**）。
#
# 为什么要有这个文件（M106）：§3.3 是一份**给人抄的命令清单**，每批划账时靠人
# 一条一条手敲再把 rc 抄进文档。此前这套命令只活在 `/tmp/run_gates.sh` 里，
# 于是出过两类事故：
#   ① /tmp 会被清 ⇒ 下一批从头手敲，敲漏哪一行没人知道；
#   ② 那份 /tmp 脚本**自身恒 exit 0**（末行是 `tail`，退出码被它盖掉），
#      而 `gofmt -l` 那一行更是拿不到失败信号（`gofmt -l` 列出文件时 rc 仍为 0）
#      ⇒ "跑过全套门禁"这件事没有任何机器判据，全靠人读 15 行 rc。
# 本文件把两件事都补上：脚本进仓，**并且自己给出总判定与非 0 退出码**。
#
# 口径纪律（沿用 04 §6.8.0 约束 4 / AS-K2）：
#   - **SKIP 绝不读作 PASS**。`smoke-symlink.sh` 需要 root 挂独立文件系统，本机不 root 时走
#     "合法跳过"那条路 ⇒ 单独记为 SKIP 行，不计入通过数，也不影响总判定（★ 认领是**双**条件：
#     `skipok=1` 且 `rc=2` 且日志有以 `SKIP` **开头**的行，见下方 `note` 分支；M142 同一把尺子）。
#   - 每行的 rc 一律取**该命令自己**的退出码（ PIPESTATUS 显式取，不让管道尾巴盖掉）。
#   - 计数行（top_PASS / src_test / data_race …）只是**留证**，不参与判定；
#     判定只看 rc 与 gofmt 的文件数、以及 DATA RACE 必须为 0。
#
# 用法：
#   bash scripts/run-gates.sh              # 全 15 行
#   bash scripts/run-gates.sh --fast       # 跳掉最贵的三行（6 的 -v 全量 / 7 / 8 两轮 race）
# 输出全部走 stdout（划账时整段重定向进带时间戳的文件再引用，别读旧报告）。

set -u
cd "$(dirname "$0")/.." || exit 3

FAST=0
[[ "${1:-}" == "--fast" ]] && FAST=1

LOGDIR="$(mktemp -d)"
trap 'rm -rf "$LOGDIR"' EXIT

# 判定累加器：三态 PASS / SKIP / FAIL，最后逐行打印。
ROWS=()
VERDICTS=()
note() { ROWS+=("$1"); VERDICTS+=("$2"); }
FAILS=0
fail() { FAILS=$((FAILS + 1)); }

# run <行号名> <命令...>：把命令的 stdout+stderr 收进该行的日志，回显 rc 与摘要。
row() {
  local name="$1"; shift
  echo "### ${name}"
  "$@" >"$LOGDIR/$name.log" 2>&1
  local rc=$?
  echo "rc=$rc"
  tail -n 20 "$LOGDIR/$name.log"
  return $rc
}

# 0) 嵌入产物前置检查（R4-5，2026-09-23 第四轮全仓审查）
# main.go:21 是 `//go:embed frontend/dist`，而 dist 属构建产物（.gitignore 排除）。本 harness 的
# 第 2~8 行全是 go 命令、跑在第 10 行 frontend-build **之前** ⇒ fresh clone 上那七行会一起红在
# `pattern frontend/dist: no matching files found`（本机实测：把 dist 移开即复现，见设计稿 §30.13），
# 而有 stale dist 时那七行是在**旧嵌入产物**上跑的。ci.yml 头部把"必须先构建前端"写成了顺序约束
# （:10-12），harness 不能是另一套口径。
# ★ 为什么不干脆把 frontend-build 挪到第 2 行：npm 一失败，2~8 行根本没跑过，15 行读数表会留
#   一片空行与级联红，比"一行都不齐且原因写在脸上"更难读。
# ★ 不新增判定行：`rows=15` 这个口径被 docs/04 §3.2/§3.3 多处引用，加行等于制造新的文档错位。
# ★ 判据只到"存在"为止：**旧不等于坏**，据"比 src 旧"判红是另一种谎 ⇒ 那一格只打 NOTE 不判红。
if [[ ! -f frontend/dist/index.html ]]; then
  echo "### 0 dist 前置检查"
  echo "FAIL：frontend/dist/index.html 不存在 ⇒ 第 2~8 行的 go 命令会一起红在 go:embed，那不是产品的问题"
  echo "  先产一次嵌入产物：cd frontend && npm ci && npm run build（或单独跑第 10 行 frontend-build）"
  exit 1
fi
if [[ -n "$(find frontend/src frontend/package.json -newer frontend/dist/index.html -print -quit 2>/dev/null)" ]]; then
  echo "### 0 dist 前置检查"
  echo "NOTE：frontend/dist 比 frontend/src 里的源文件旧 ⇒ 第 2~8 行是在旧嵌入产物上跑的（不判红）"
fi

# 1) gofmt：rc 恒 0，判据必须是**列出的文件数**。
# M124：但"文件数为 0"单独不成立——gofmt 自己崩了（rc≠0 且 stdout 空）也会数出 0，
# 那一格改前读成 PASS。⇒ 判据两条：rc 必须为 0，且列出文件数必须为 0。
echo "### 1 gofmt"
GF=$(gofmt -l . 2>&1); GRC=$?
echo "rc=$GRC"
if [[ $GRC -ne 0 ]]; then
  [[ -n "$GF" ]] && echo "$GF"
  echo "GOFMT 自身退出码非 0（rc=${GRC}）⇒ 本行判 FAIL，不得退化成「没文件要报」就是绿"
  fail; note "1 gofmt" FAIL
elif [[ -n "$GF" ]]; then
  echo "$GF"
  echo "gofmt_files=$(printf '%s\n' "$GF" | wc -l | tr -d ' ')"
  echo "GOFMT 有文件未格式化 ⇒ 本行判 FAIL"
  fail; note "1 gofmt" FAIL
else
  echo "gofmt_files=0"
  note "1 gofmt" PASS
fi

# 2~5) 编译与三平台 vet
# 三行的 GOOS/GOARCH 与 CI 逐字对齐：`ci.yml:98` 是 `GOOS=windows GOARCH=amd64`、
# `:101` 是 `GOOS=darwin GOARCH=arm64`，linux 行取 CI 原生 job（ubuntu-latest = amd64）
# 的平台。★ 只钉 GOOS 不钉 GOARCH 是不够的：本机 GOARCH=arm64 会被继承，
# 于是 windows 行跑成 windows/arm64、与 §3.3 那份配方（钉了 GOARCH）不同口径。
# 列序是 "序号 行名 GOOS GOARCH"：取错列会把行名当成 GOOS 交给 go，
# 三行会以「build constraints exclude all Go files」恒红（本文件首跑即栽在此），
# 那是 harness 的错，不是产品的错 ⇒ 改判据前先看红的是不是那一格。
row "2 build" go build ./... && note "2 build" PASS || { fail; note "2 build" FAIL; }
for pair in "3 vet-linux linux amd64" "4 vet-darwin darwin arm64" "5 vet-windows windows amd64"; do
  set -- $pair
  label="$1 $2"; goos="$3"; goarch="$4"
  echo "### ${label}"
  GOOS=$goos GOARCH=$goarch go vet ./... >"$LOGDIR/$1.log" 2>&1
  rc=$?
  echo "rc=$rc"
  tail -n 20 "$LOGDIR/$1.log"
  if [[ $rc -eq 0 ]]; then note "$label" PASS; else fail; note "$label" FAIL; fi
done

# 6) 全量测试（带 -v，用于取 PASS/SKIP/FAIL 与源码级用例数）
if [[ $FAST -eq 1 ]]; then
  echo "### 6 test -v --fast 已跳过（PASS/SKIP 计数本次不取）"
  note "6 test -v" SKIP
  GO6=0
else
  echo "### 6 test -v"
  go test -count=1 -v ./... >"$LOGDIR/6-test.log" 2>&1
  GO6=$?
  echo "rc=$GO6"
  grep -c "^ok  " "$LOGDIR/6-test.log" | sed 's/^/ok_pkgs=/'
  grep -c "no test files" "$LOGDIR/6-test.log" | sed 's/^/no_test_pkgs=/'
  echo "top_PASS=$(grep -c '^--- PASS' "$LOGDIR/6-test.log")"
  echo "top_SKIP=$(grep -c '^--- SKIP' "$LOGDIR/6-test.log")"
  echo "top_FAIL=$(grep -c '^--- FAIL' "$LOGDIR/6-test.log")"
  echo "sub_PASS=$(grep -c '^    --- PASS' "$LOGDIR/6-test.log")"
  echo "sub_SKIP=$(grep -c '^    --- SKIP' "$LOGDIR/6-test.log")"
  echo "sub_FAIL=$(grep -c '^    --- FAIL' "$LOGDIR/6-test.log")"
  echo "run=$(grep -c '^=== RUN' "$LOGDIR/6-test.log")"
  echo "src_test=$(grep -rh '^func Test' --include='*_test.go' --exclude-dir=node_modules --exclude-dir=.workbuddy . | wc -l | tr -d ' ')"
  [[ $GO6 -eq 0 ]] && note "6 test -v" PASS || { fail; note "6 test -v" FAIL; }
fi

# 7~8) 竞态面：rc=0 还不够，**DATA RACE 必须为 0**（race detector 报在 ok 行之后时
# 包行可能仍显 ok，只信 rc 会漏）。
race_row() {
  local label="$1"; shift
  if [[ $FAST -eq 1 ]]; then
    echo "### ${label} --fast 已跳过"
    note "$label" SKIP
    return 0
  fi
  echo "### ${label}"
  "$@" >"$LOGDIR/$label.log" 2>&1
  local rc=$?
  echo "rc=$rc"
  grep -c "^ok  " "$LOGDIR/$label.log" | sed 's/^/ok_pkgs=/'
  local dr
  dr=$(grep -c 'DATA RACE' "$LOGDIR/$label.log")
  echo "data_race=$dr"
  if [[ $rc -eq 0 && $dr -eq 0 ]]; then note "$label" PASS
  else fail; note "$label" FAIL; fi
}
race_row "7 race-x2-all" go test -race -count=2 ./...
race_row "8 race-x4-root" go test -race -count=4 .

# 9~15) 前端与脚本面
#
# ★ M123（04 §6.11 GATE-5，设计段 §23.9）：`skipok=1` 那行改前只认"裸 rc==2 ⇒ SKIP"。
# 而被包脚本自己的契约（smoke-symlink.sh:19-22、skip():37-40）是**两件事同真**：
# "SKIP（跳过，非通过）:" 前缀 + 退出码 2。它整个脚本是 `set -euo pipefail`，
# 中途任何命令以 2 退出（grep 读不出、mount 参数错都会给 2）都会被这里降级成
# SKIP、总判定照样 exit 0——正是 AS-K2/M8 要防的"真失败伪装成合法跳过"那一格。
# ⇒ 判据补成双条件：码对**且**日志里有那行自证，缺一即 FAIL。
sh_row() {
  local label="$1" skipok="${2:-0}"; shift 2
  echo "### ${label}"
  ( "$@" ) >"$LOGDIR/$label.log" 2>&1
  local rc=$?
  echo "rc=$rc"
  tail -n 6 "$LOGDIR/$label.log"
  if [[ $rc -eq 0 ]]; then note "$label" PASS
  elif [[ $skipok -eq 1 && $rc -eq 2 ]] && grep -q '^SKIP' "$LOGDIR/$label.log"; then
    note "$label" SKIP   # 环境性跳过（双条件齐），不算通过、只算未取读数
  else
    if [[ $skipok -eq 1 && $rc -eq 2 ]]; then
      echo "rc=2 但日志里没有以 SKIP 开头的那行自证 ⇒ 按真失败判，不降级为 SKIP"
    fi
    fail; note "$label" FAIL
  fi
}
sh_row "9 frontend-typecheck" 0 bash -c 'cd frontend && npm run typecheck'
sh_row "10 frontend-build" 0 bash -c 'cd frontend && npm run build'
sh_row "11 frontend-logic" 0 bash scripts/test-frontend-logic.sh
sh_row "12 version-sync" 0 bash scripts/check-version-sync.sh
sh_row "13 smoke-cli" 0 bash scripts/smoke-cli.sh
sh_row "14 smoke-symlink-assert" 0 bash scripts/smoke-symlink-assert.sh
sh_row "15 smoke-symlink" 1 bash scripts/smoke-symlink.sh

echo
echo "=== 总判定 ==="
i=0
SKIPN=0
while [[ $i -lt ${#ROWS[@]} ]]; do
  printf '%-26s %s\n' "${ROWS[$i]}" "${VERDICTS[$i]}"
  [[ "${VERDICTS[$i]}" == SKIP ]] && SKIPN=$((SKIPN + 1))
  i=$((i + 1))
done
echo "rows=${#ROWS[@]} PASS=$(( ${#ROWS[@]} - FAILS - SKIPN )) SKIP=${SKIPN} FAIL=${FAILS}"
if [[ $FAILS -gt 0 ]]; then
  echo "⇒ 全套门禁**未过**（有 ${FAILS} 行 FAIL），不得划账、不得交付"
  exit 1
fi
echo "⇒ 全绿（SKIP 行按 AS-K2 不算通过，只算未取读数）"
exit 0
