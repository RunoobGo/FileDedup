#!/usr/bin/env bash
# 全套回归门禁 harness（04 §3.3 那 11 条命令 + 两条补的，共 **15 行判定读数**）。
# 〔2026-09-27 M283 批追加第 16 行「文档锚点命中率」，**只报数不判定** ⇒ 判定行数仍是 15，
#  `rows=` 的口径没有变（见该行注释与本文件末尾那两行说明）。〕
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
#     ★ M405（2026-10-01）给行 6 开了一条例外，且**不是**把"计数"升成判据：
#       `roster_top ≡ top_SKIP` 钉的是"我外显的名单全不全"（取数通道自身的完整性），
#       与"有多少条 skip"无关——有 skip 照旧不判红（合法环境性 skip 先例在册，M331 的取向）。
#
# 用法：
#   bash scripts/run-gates.sh              # 全 15 行
#   bash scripts/run-gates.sh --fast       # 跳掉最贵的三行（6 的 -v 全量 / 7 / 8 两轮 race）
#   GATE_LOGDIR=/tmp/gates bash scripts/run-gates.sh   # 指定日志目录（M405）：
#     ★ 指定时**全绿也不删**（默认路径的 mktemp 会在绿的时候把行 6 的 SKIP 名单一起回收）。
#     行 6 的清册单独落 `$LOGDIR/6-skip-{top,sub}.roster`，同时逐条打到 stdout。
# 输出全部走 stdout（划账时整段重定向进带时间戳的文件再引用，别读旧报告）。

set -u
cd "$(dirname "$0")/.." || exit 3

FAST=0
[[ "${1:-}" == "--fast" ]] && FAST=1

# ★ M405（2026-10-01，设计段 §1.2 之一）：LOGDIR 给一个可指定入口。
# 原来这里恒是 `mktemp -d`，而 gates_cleanup() 在**全绿时 rm -rf** ⇒ 行 6 那份 `6-test.log`
# 连同 SKIP 名单一起被回收，"哪几条 skip 了"永久拿不到（§6.72 七·5 实录：同一时间窗
# 行 6 报 7、包装脚本报 6，两边都对得上各自的账，却没有任何一份名单可用来做差集归因）。
# 入口惯例照仓内既有的负控制入口（M134_SCAN_FILES / ANCH_SRCLIST / FRONTEND_DIR）：
# **默认行为一字不变**；调用方给路径时只保证目录存在，**不删调用方的目录**（见 gates_cleanup）。
LOGDIR="${GATE_LOGDIR:-$(mktemp -d)}"
LOGDIR_OWNED=1
if [ -n "${GATE_LOGDIR:-}" ]; then
	mkdir -p "$LOGDIR" || exit 3
	LOGDIR_OWNED=0
fi
# 判定累加器：三态 PASS / SKIP / FAIL，最后逐行打印。
ROWS=()
VERDICTS=()
note() { ROWS+=("$1"); VERDICTS+=("$2"); }
FAILS=0
fail() { FAILS=$((FAILS + 1)); }

# ★M325（GATE-29，2026-09-28 第六轮全量审查）：这一行原先是
# `trap 'rm -rf "$LOGDIR"' EXIT`——**无论成败都把现场删了**。
# 而本仓的交付纪律是"逐行读数落进 04 划账"，第 16 行的明细（DRIFT/MISSING/OOR）
# 就在那份日志里（第 16 行那段的自述写着"明细整份留在本行日志里"）：只要有一行红，
# 想抄明细就只能重跑一遍全量。现在改成"有 FAIL 就保留并打出路径，全绿照旧清"。
# 判定、行序、`--fast` 语义一字未动——本条只改"红的时候东西还在不在"。
gates_cleanup() {
	if [[ $FAILS -gt 0 ]]; then
		printf '日志保留在 %s（有 %s 行 FAIL：逐行 .log 还在，划账直接抄明细，不必重跑）\n' \
			"$LOGDIR" "$FAILS"
		return
	fi
	# ★ M405：LOGDIR_OWNED=0（调用方给了 GATE_LOGDIR）⇒ **全绿也保留**（这正是这一格存在的理由：
	#   要归因的恰恰是"什么都没坏的那一跑里名单长什么样"）。自建的临时目录照旧清。
	if [[ $LOGDIR_OWNED -eq 0 ]]; then
		printf '日志保留在 %s（GATE_LOGDIR 由调用方指定，本脚本不删调用方的目录）\n' "$LOGDIR"
		return
	fi
	rm -rf "$LOGDIR"
}
trap gates_cleanup EXIT

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
#   〔2026-09-27 M283 批：第 16 行确实加了，但按上面这条的理由做成 **REPORT 档不计入 rows=**
#   ⇒ 那几处"15 行读数"一字未动仍然为真。〕
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
# 三行的 GOOS/GOARCH 与 CI 逐字对齐：CI 的 windows 腿是 `GOOS=windows GOARCH=amd64`、
# darwin 腿是 `GOOS=darwin GOARCH=arm64`，linux 行取 CI 原生 job（ubuntu-latest = amd64）
# 的平台。★ 行号不写死（2026-10-01 第九轮修订批 E4：原写 `ci.yml:98`/`:101`，b3e3a4a 加
# 了一个步骤就把它们挪成了 :100/:103，注释却留在旧值上）。要重数跑：
#   grep -n 'GOOS=\(windows\|darwin\) GOARCH=[a-z0-9]* go vet' .github/workflows/ci.yml
# ★ 只钉 GOOS 不钉 GOARCH 是不够的：本机 GOARCH=arm64 会被继承，
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
  # ★ M405：本行读 6-test.log 的**每一处**都带 `-a`（M404 的教训：BSD/GNU grep 一旦把日志判成
  #   二进制，`-c` 照旧准但匹配行不再打 ⇒ "计数准、名单缺"；本行新增的恰恰是取名单那几处）。
  #   只给名单加 -a 会留下"数按二进制、列按文本"这种新的不同形 ⇒ 计数与名单同一把尺。
  grep -a -c "^ok  " "$LOGDIR/6-test.log" | sed 's/^/ok_pkgs=/'
  grep -a -c "no test files" "$LOGDIR/6-test.log" | sed 's/^/no_test_pkgs=/'
  TOP_PASS=$(grep -a -c '^--- PASS' "$LOGDIR/6-test.log")
  TOP_SKIP=$(grep -a -c '^--- SKIP' "$LOGDIR/6-test.log")
  TOP_FAIL=$(grep -a -c '^--- FAIL' "$LOGDIR/6-test.log")
  SUB_PASS=$(grep -a -c '^    --- PASS' "$LOGDIR/6-test.log")
  SUB_SKIP=$(grep -a -c '^    --- SKIP' "$LOGDIR/6-test.log")
  SUB_FAIL=$(grep -a -c '^    --- FAIL' "$LOGDIR/6-test.log")
  echo "top_PASS=${TOP_PASS}"
  echo "top_SKIP=${TOP_SKIP}"
  echo "top_FAIL=${TOP_FAIL}"
  echo "sub_PASS=${SUB_PASS}"
  echo "sub_SKIP=${SUB_SKIP}"
  echo "sub_FAIL=${SUB_FAIL}"
  echo "run=$(grep -a -c '^=== RUN' "$LOGDIR/6-test.log")"
  echo "src_test=$(grep -rh '^func Test' --include='*_test.go' --exclude-dir=node_modules --exclude-dir=.workbuddy . | wc -l | tr -d ' ')"

  # ★ M405（04 §6.11 GATE 族的本机版本，设计段 §1.2 之二）：外显 SKIP **清册**，
  #   并钉"清册 ≡ 计数"。原形状是八个计数一条名字都不打，而日志随 mktemp 回收
  #   ⇒ §6.72 七·5 那次"行 6 报 7、包装脚本报 6"永远无法归因（没有名单可做差集）。
  #   ★ 判红的不是"有 skip"，是"名单不全"：合法环境性 skip 先例在册（requireSymlinkSupport 一族），
  #     一刀切会把门禁训练成"大家都忽略的那一行"（M331 的取向，一字未改）。
  #   先落文件再打印：这样"取名单"有独立产物，删掉下面任一条 echo 也骗不出"0 ≡ 0"的绿
  #   （见下面判据链的第一臂，它读的就是这两个文件在不在——MU-2 顶的正是这一格）。
  #   sed 只去掉耗时尾巴，行首形状与 scripts/ci-go-test.sh 的清册**逐字同形** ⇒ 两份可直接 comm。
  grep -a '^--- SKIP' "$LOGDIR/6-test.log" | sed -E 's/ \([0-9.]+s\)$//' >"$LOGDIR/6-skip-top.roster"
  grep -a '^    --- SKIP' "$LOGDIR/6-test.log" | sed -E 's/ \([0-9.]+s\)$//' >"$LOGDIR/6-skip-sub.roster"
  ROSTER_TOP=$(grep -a -c . "$LOGDIR/6-skip-top.roster" 2>/dev/null)
  ROSTER_SUB=$(grep -a -c . "$LOGDIR/6-skip-sub.roster" 2>/dev/null)
  ROSTER_TOP=${ROSTER_TOP:-0}
  ROSTER_SUB=${ROSTER_SUB:-0}
  echo "roster_top=${ROSTER_TOP}"
  echo "roster_sub=${ROSTER_SUB}"
  echo "-- 6-skip-roster-begin"
  cat "$LOGDIR/6-skip-top.roster" "$LOGDIR/6-skip-sub.roster" 2>/dev/null
  echo "-- 6-skip-roster-end"
  if [[ ! -f "$LOGDIR/6-skip-top.roster" || ! -f "$LOGDIR/6-skip-sub.roster" ]]; then
    echo "FAIL: 6-skip-*.roster 不在场 ⇒ SKIP 名单通道本身缺失，本轮 SKIP 读数不可信（M405）"
    fail
    note "6 test -v" FAIL
  elif [[ $ROSTER_TOP -ne $TOP_SKIP || $ROSTER_SUB -ne $SUB_SKIP ]]; then
    echo "FAIL: SKIP 清册与计数不同源（roster_top=${ROSTER_TOP} vs top_SKIP=${TOP_SKIP}，roster_sub=${ROSTER_SUB} vs sub_SKIP=${SUB_SKIP}）⇒ 名单被截断（M405）"
    fail
    note "6 test -v" FAIL
  elif [[ $GO6 -ne 0 ]]; then
    fail
    note "6 test -v" FAIL
  else
    note "6 test -v" PASS
  fi
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

# 16) 文档锚点命中率抽查（M299 取向：「红了只报数不自动改」）
#
# ★ 为什么它**不参与总判定**，而 04 §3.2/§3.3 引用的 `rows=15` 口径仍然成立：
#   登记表约束 (1) 禁止改写已登记行 ⇒ 命中率红了也没有合法的修法可执行，硬判红只会
#   把这一行训练成"大家都忽略的那一行"。所以第 16 行进 REPORT 档：打印、留日志、
#   从 rows= 里扣掉（rows 仍是**参与判定**的行数），总判定不受它影响。
# ★ 但"扫描面本身没成立"是另一件事，那一格必须红：awk 缺失、docs 改名、清单为空
#   都会让 `anchors=` 一行都打不出来——读空当通过是 AS-K2 反复点名的形状（判据同 B1）。
# ★ 明细（DRIFT/MISSING/OOR）整份留在本行日志里，划账时按 `anchors=` 那行抄总数。
echo "### 16 anchor-hit-rate（只报数）"
# 负控制入口（同 smoke-symlink-assert.sh 的 M134_SCAN_FILES 惯例，M324-a 用）：
# 调用方**预设** ANCH_SRCLIST 就按那份清单扫、不再自己 find——"扫描面塌成空"这一格
# 只有能把清单换成空的才造得出来。（★ 不做成"预设路径但仍重写它"：那样入口只是换了个
# 临时文件名，负控制永远打不出来，M324 的判据也就永远没人验证过。）
if [[ -n "${ANCH_SRCLIST:-}" ]]; then
  : # 沿用调用方给的清单（可以是不存在的文件，那就是空扫描面）
else
  ANCH_SRCLIST="$LOGDIR/16.srclist"
  find . \( -name node_modules -o -name .git -o -name dist -o -name build -o -name .workbuddy \) -prune -o \
    -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.vue' -o -name '*.js' \
               -o -name '*.mjs' -o -name '*.sh' -o -name '*.json' -o -name '*.yml' -o -name '*.md' \) -print \
    | sed 's|^\./||' | sort > "$ANCH_SRCLIST"
fi
awk -v SRCLIST="$ANCH_SRCLIST" -f scripts/anchor-hits.awk docs/*.md > "$LOGDIR/16.anchor-hit-rate.log" 2>&1
ANCH_RC=$?
tail -n 4 "$LOGDIR/16.anchor-hit-rate.log"
if [[ $ANCH_RC -ne 0 ]] || ! grep -q '^anchors=' "$LOGDIR/16.anchor-hit-rate.log"; then
  echo "rc=$ANCH_RC 且日志里没有 anchors= 那行 ⇒ 扫描面本身没成立，按 FAIL 记（不是命中率红，是这条通道坏了）"
  fail; note "16 anchor-hit-rate" FAIL
else
  # ★M324（GATE-28，2026-09-28 第六轮全量审查）：上面那段自述的"清单为空必须红"从未成立——
  #   `anchor-hits.awk` 的 END 块是**无条件**打印 `anchors=… src_files=…` 的，扫描面塌成空时
  #   打出来的是 `anchors=0 src_files=0`，`grep -q '^anchors='` 照样命中 ⇒ 记的是 REPORT。
  #   触发场景是真的：上面 find 的 -prune 列表里某个目录名被改、或仓根换布局，
  #   整块扫描面可以静默塌空，这一行仍报"看过"。补的是"扫描面本身"这一格，
  #   **不是**命中率：两个数任意为 0 就 FAIL，其余照旧只报数不参与总判定。
  ANCH_N="$(sed -n 's/^anchors=\([0-9][0-9]*\).*/\1/p' "$LOGDIR/16.anchor-hit-rate.log" | head -1)"
  SRC_N="$(sed -n 's/^anchors=[0-9][0-9]* src_files=\([0-9][0-9]*\).*/\1/p' "$LOGDIR/16.anchor-hit-rate.log" | head -1)"
  if [[ -z "$ANCH_N" || -z "$SRC_N" || "$ANCH_N" -eq 0 || "$SRC_N" -eq 0 ]]; then
    echo "anchors=[${ANCH_N}] src_files=[${SRC_N}] ⇒ 扫描面塌空（锚或源清单一个都没有），这条通道没在扫东西，按 FAIL 记（M324）"
    fail; note "16 anchor-hit-rate" FAIL
  else
    note "16 anchor-hit-rate" REPORT   # 只报数：不进 PASS 数、不进 FAIL 数
  fi
fi

echo
echo "=== 总判定 ==="
i=0
SKIPN=0
REPORTN=0
while [[ $i -lt ${#ROWS[@]} ]]; do
  printf '%-26s %s\n' "${ROWS[$i]}" "${VERDICTS[$i]}"
  [[ "${VERDICTS[$i]}" == SKIP ]] && SKIPN=$((SKIPN + 1))
  [[ "${VERDICTS[$i]}" == REPORT ]] && REPORTN=$((REPORTN + 1))
  i=$((i + 1))
done
# ★ `rows=` 的口径 = **参与判定的行数**（REPORT 档不计入），所以 M283 批加了第 16 行之后
#   rows 仍是 15——docs/04 §3.2/§3.3 那几处"15 行读数"不必跟着改，改了反而是另一处错位。
echo "rows=$(( ${#ROWS[@]} - REPORTN )) PASS=$(( ${#ROWS[@]} - FAILS - SKIPN - REPORTN )) SKIP=${SKIPN} FAIL=${FAILS} REPORT=${REPORTN}"
if [[ $FAILS -gt 0 ]]; then
  echo "⇒ 全套门禁**未过**（有 ${FAILS} 行 FAIL），不得划账、不得交付"
  exit 1
fi
echo "⇒ 全绿（SKIP 行按 AS-K2 不算通过，只算未取读数）"
exit 0
