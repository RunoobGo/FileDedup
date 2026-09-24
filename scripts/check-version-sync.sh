#!/usr/bin/env bash
# 文档取值位一致性门禁（04 附录 A「版本号与 GetVersion 一致」的机器化版本 + M210 的计数层锚）。
#
# 本脚本两组判据，同一族事：**文档里写的值必须等于代码里此刻的值**。
#   A 组 版本号（下方 6 个取值位）   —— 一直就有
#   B 组 活文档计数（§3.2 逐包/合计/平台三格、§4.1 前端、§4.3、docs/README 勘误表、§7 包数与
#          门禁脚本份数名单）        —— 2026-09-24 M210 加，设计段
#          `docs/superpowers/specs/2026-09-24-doc-counter-anchor-design.md`
#        为什么长在版本号脚本里而不是新开脚本/新加门禁行，见该设计段 §1 与下方 B 组注释。
#
# A 组校验以下声明位必须完全相等。共 **6 个取值位 / 5 个文件**：
#   取值位 1   app.go                 const AppVersion      （GetVersion 绑定返回值）
#   取值位 2   wails.json             info.productVersion   （打包壳；Win/Linux 资源与
#                                     build/*/Info.plist 的 {{.Info.ProductVersion}} 同源）
#   取值位 3   frontend/package.json  version
#   取值位 4   frontend/package-lock.json version
#   取值位 5   frontend/package-lock.json packages[""].version   （同一文件贡献 2 个取值位）
#   取值位 6   docs/09-用户手册.md    「适用版本：」
# 注：对外叙述习惯说"5 处"（按文件数），但门禁实际读 6 个值——包锁文件里
#     `version` 与 `packages[""].version` 是两处独立声明，任一漂移都会被本脚本抓住。
#
# 打 tag 发布时额外校验：tag 必须是 v<版本号>（build.yml 由 v* 触发并建 Release，
# 修复前 tag 与产品版本各说各话，Release 资产名与"关于"里读到的版本会不一致）。
#
# 用法：
#   bash scripts/check-version-sync.sh                # 只查声明位互相对齐
#   bash scripts/check-version-sync.sh v0.5.1         # 再比对指定 tag
#   GITHUB_REF_NAME=refs/tags/v0.5.1 ...              # CI 里无需插值，脚本自解析
#                                                     # （不把事件字段拼进 shell 命令）
#
# JSON 一律显式按 UTF-8 读：Python 的 open() 默认用平台 locale 编码，Windows 上是 cp1252，
# 而 wails.json 的 productDescription 是中文——build.yml 的 windows 矩阵 job 曾在第一步
# 就被 json.load 的 UnicodeDecodeError 打断，三个 Linux/macOS job 全绿、Release 却整体没建
# （2026-09-19 首次 tag 触发发布时暴露；此门禁此前从未在 Windows 上跑过）。

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail() { printf '\033[31m✗ %s\033[0m\n' "$*" >&2; BAD=1; }
BAD=0

APP_VER=$(sed -nE 's/^const AppVersion = "([^"]+)".*/\1/p' app.go)
WAILS_VER=$(python3 -c 'import json;print(json.load(open("wails.json",encoding="utf-8"))["info"]["productVersion"])')
PKG_VER=$(python3 -c 'import json;print(json.load(open("frontend/package.json",encoding="utf-8"))["version"])')
LOCK_VER=$(python3 -c 'import json;d=json.load(open("frontend/package-lock.json",encoding="utf-8"));print(d["version"])')
LOCK_ROOT_VER=$(python3 -c 'import json;d=json.load(open("frontend/package-lock.json",encoding="utf-8"));print(d["packages"][""]["version"])')
DOC_VER=$(sed -nE 's/^> 适用版本：([^ ｜]+).*/\1/p' docs/09-用户手册.md | head -1)

printf '  app.go            AppVersion        = %s\n' "${APP_VER:-<缺失>}"
printf '  wails.json        productVersion    = %s\n' "${WAILS_VER:-<缺失>}"
printf '  package.json      version           = %s\n' "${PKG_VER:-<缺失>}"
printf '  package-lock.json version           = %s\n' "${LOCK_VER:-<缺失>}"
printf '  package-lock.json packages[""]      = %s\n' "${LOCK_ROOT_VER:-<缺失>}"
printf '  docs/09           适用版本          = %s\n' "${DOC_VER:-<缺失>}"

for pair in "wails.json:$WAILS_VER" "package.json:$PKG_VER" "package-lock.json:$LOCK_VER" \
            "package-lock.json packages[\"\"]:$LOCK_ROOT_VER" "docs/09 适用版本:$DOC_VER"; do
	name="${pair%%:*}"; val="${pair#*:}"
	[ -n "$val" ] || fail "$name 未取到版本号（声明位被改动或格式变化，请同步更新本脚本）"
	[ -n "$val" ] && [ "$val" != "$APP_VER" ] && fail "$name=$val 与 app.go AppVersion=$APP_VER 不一致"
done

# tag 比对：★ AS-K4（2026-09-20 全仓审计）——"是不是 tag 触发"必须看**触发类型**，
# 不能看 tag 的拼写。修正前这里用 `case $TAG in v[0-9]*)` 来识别 tag，而
# build.yml 的触发条件是 `v*`：`vBeta`、`vRelease` 这类标签**会触发整条发布流水线**，
# 却在门禁里被认成"普通分支 push"而跳过比对，于是 0.5.0 的产物顶着 vBeta 的名字
# 发出去，Release 资产名与"关于"里读到的版本各说各话。
#
# 现在三处信号任一成立即按 tag 触发处理：GITHUB_REF_TYPE=tag、
# ref 形如 refs/tags/…、或命令行显式传了参数。是 tag 却对不上 v<AppVersion> 即红。
REF="${1:-${GITHUB_REF_NAME:-${GITHUB_REF:-}}}"
REF_TYPE="${GITHUB_REF_TYPE:-}"

TAG=''
TAG_WHY=''
case "$REF" in
	refs/tags/*)
		TAG="${REF#refs/tags/}"
		TAG_WHY='ref 形如 refs/tags/'
		;;
esac
if [ "$REF_TYPE" = 'tag' ] && [ -z "$TAG" ] && [ -n "$REF" ]; then
	TAG="${REF##*/}" # GITHUB_REF_NAME 在 tag 触发时就是裸 tag 名
	TAG_WHY='GITHUB_REF_TYPE=tag'
fi
if [ "$#" -ge 1 ] && [ -n "$1" ]; then
	TAG="${1#refs/tags/}"
	TAG_WHY='命令行显式指定 tag'
fi

if [ -n "$TAG" ]; then
	if [ "$TAG" != "v$APP_VER" ]; then
		fail "tag $TAG 与 AppVersion $APP_VER 不一致（按 tag 触发处理，依据：${TAG_WHY}）。发布物名字与 GetVersion/「关于」会各说各话——要么改 tag，要么先同步版本号再打 tag"
	else
		printf '\033[32m✓ tag %s 与版本号一致（依据：%s）\033[0m\n' "$TAG" "$TAG_WHY"
	fi
else
	echo "  （非 tag 触发，跳过 tag 比对。若本次确是 tag 触发，请确认 GITHUB_REF_TYPE / GITHUB_REF 已传入环境）"
fi

# ============================================================================
# B 组：活文档计数取值位（M210，2026-09-24）
# ============================================================================
# 为什么长在"版本号脚本"里（设计段 §1，三选一）：这一组和 A 组是**同一件事**——文档里写的值
# 必须等于代码里此刻的值（`docs/09` 的「适用版本：」本来就是一个文档取值位）。放这里的理由：
#   ① run-gates.sh 第 12 行与 ci.yml:72 **都已经在调本脚本** ⇒ 本地门禁与 CI 各覆盖一次，
#      既不必新增判定行（`run-gates.sh:61` 明写拒绝第 16 行：`rows=15` 被 §3.2/§3.3 多处引用），
#      也不必改任何 workflow 文件；
#   ② fail-closed 的写法 A 组有现成先例（"未取到版本号 ⇒ 判红"）；
#   ③ 代价：脚本名比职责窄一圈——所以这里把 A/B 两组写明白，别让下一个人以为它只管版本号。
#
# 判据纪律（设计段 §3）：
#   - 每个锚点**恰好命中一次**才算数：0 次＝锚点被改写、>1 次＝比对对象有歧义 ⇒ 都判红。
#     这一条是 B 组的全部风险所在：锚点读空却"跳过"，等于防线静默消失（AS-K2 同一族）。
#   - 真值通道读空、读 0、取数命令没成功 ⇒ 一律判红。"加载失败"和"确实没有"必须可分辨。
#   - `go list` 带 `-e` 只为躲一件**与本判据无关**的事：CI 的这一步跑在前端构建之前
#     （`ci.yml:72` vs `:90`），那一刻仓库里还没有 `frontend/dist`，根包 `main.go:21` 的
#     `//go:embed frontend/dist` 会让整条加载红在 `pattern frontend/dist: no matching files found`
#     （2026-09-24 用最小模块实测：无 dist 时 `go list ./...` 与带 `-f` 的取文件通道均 rc=1
#     且不输出任何路径；补一个空 `frontend/dist/index.html` 即 rc=0）。
#     `-e` 关掉的是**报错文本**，关不掉判据：读空照样红。且 B1 走纯 `find`+`grep`、不依赖 go，
#     两路互为对照。
#   - 历史层不进判据（设计段 §4）：§3.2"说明"列与表下追记里的旧读数、§6.x 划账、archive/、
#     superpowers/ 一概不比对——那些是当时的证词，本就该不等于现值。
DOC04='docs/04-开发与测试计划.md'
DOCR='docs/README.md'
ANCH=''
N_ANCH=0
N_CMP=0
B_TST='^func Test'

# anch <ERE（可含捕获）> <文件> <锚点名>：校验"恰好命中一次"，命中行存进 $ANCH。
anch() {
	local re="$1" file="$2" label="$3" n
	N_ANCH=$((N_ANCH + 1))
	n=$(grep -cE "$re" "$file") || n=0
	if [ "$n" -ne 1 ]; then
		fail "B 组锚点「${label}」在 ${file} 命中 ${n} 次（要求恰好 1 次）⇒ 锚点被改写或比对对象有歧义，本组不产假绿，请同步更新本脚本"
		ANCH=''
		return 1
	fi
	ANCH=$(grep -E "$re" "$file")
	return 0
}

# cmpv <格名> <文档值> <真值>：任一侧为空即红（空＝取数没成功，不许读作"相等"）。
cmpv() {
	local label="$1" doc="$2" real="$3"
	N_CMP=$((N_CMP + 1))
	if [ -z "$doc" ] || [ -z "$real" ]; then
		fail "${label}：文档=[${doc:-<未取到>}] 真值=[${real:-<未取到>}] ⇒ 有一侧取数没成功，不判通过"
	elif [ "$doc" != "$real" ]; then
		fail "${label}：文档=${doc} ≠ 真值=${real} ⇒ 活文档漂移，改文档（口径见 04 §3.2 / 设计段 M210）"
	else
		printf '  \033[32m✓\033[0m %-46s %s\n' "$label" "$real"
	fi
}

# real_tests <目录>：该目录（不含子目录）里 *_test.go 的 `^func Test` 条数。
real_tests() {
	local tc
	tc=$(find "$1" -maxdepth 1 -name '*_test.go' -exec grep -h "$B_TST" {} + 2>/dev/null | wc -l | tr -d '[:space:]') || tc=''
	printf '%s' "$tc"
}

# real_files <目录>：同上取文件个数。
real_files() {
	local fc
	fc=$(find "$1" -maxdepth 1 -name '*_test.go' 2>/dev/null | wc -l | tr -d '[:space:]') || fc=''
	printf '%s' "$fc"
}

say_b() { printf '\033[36m— %s\033[0m\n' "$1"; }

# ---- B1 §3.2 逐包：23 行 ×（Test 数 / 测试文件数）+ 包集合双向对齐 ----
say_b 'B1 docs/04 §3.2 逐包格（Test 数 + 测试文件数，逐包相加 ≡ 合计）'
DOC_ROWS=$(awk '
/^\| 包 \| Test 数 \| 说明 \|$/ {t=1; next}
t && /^\|---/ {next}
t && /^\| 合计 \|/ {exit}
t { print }
' "$DOC04")
B1_ROWS=0
B1_SUM=0
B1_DOCDIRS=''
while IFS= read -r line; do
	[ -n "$line" ] || continue
	pkg=$(printf '%s' "$line" | sed -nE 's/^\| `([^`]*)`[[:space:]]*\|[[:space:]]*\*\*([0-9]+)（([0-9]+)[[:space:]]*个测试文件.*/\1/p')
	d_tst=$(printf '%s' "$line" | sed -nE 's/^\| `([^`]*)`[[:space:]]*\|[[:space:]]*\*\*([0-9]+)（([0-9]+)[[:space:]]*个测试文件.*/\2/p')
	d_fil=$(printf '%s' "$line" | sed -nE 's/^\| `([^`]*)`[[:space:]]*\|[[:space:]]*\*\*([0-9]+)（([0-9]+)[[:space:]]*个测试文件.*/\3/p')
	if [ -z "$pkg" ] || [ -z "$d_tst" ] || [ -z "$d_fil" ]; then
		fail "B1 有一行解析不出「\`包\` | **N（M 个测试文件**」的形状：$(printf '%s' "$line" | cut -c1-40) ⇒ 表形状变了，本脚本要跟改"
		continue
	fi
	B1_ROWS=$((B1_ROWS + 1))
	: $((B1_SUM += ${d_tst}))
	B1_DOCDIRS="${B1_DOCDIRS}${pkg}
"
	if [ "$pkg" = '根包（App 层）' ]; then
		dir='.'
	else
		dir="$pkg"
	fi
	cmpv "  ${pkg} Test 数" "$d_tst" "$(real_tests "$dir")"
	cmpv "  ${pkg} 测试文件数" "$d_fil" "$(real_files "$dir")"
done <<EOF
${DOC_ROWS}
EOF
if [ "$B1_ROWS" -lt 1 ]; then
	fail "B1 的 §3.2 表范围一条行都没解析出来（表头「| 包 | Test 数 | 说明 |」到「| 合计 |」之间为空）⇒ 扫描面本身没成立，不读作通过"
else
	printf '  （B1 扫到 %s 行，逐包 Test 数相加 = %s）\n' "$B1_ROWS" "$B1_SUM"
fi
# 包集合双向对齐：表里列的包 ≡ 目录里有 *_test.go 的包（漏一行/多一行都红）
REAL_DIRS=$(find . -name '*_test.go' -not -path './.git/*' -not -path '*/node_modules/*' -not -path './.workbuddy/*' \
	| sed -e 's|/[^/]*$||' -e 's|^\./||' | sort -u)
DOC_DIRS_SORT=$(printf '%s' "$B1_DOCDIRS" | while IFS= read -r p; do
	[ -n "$p" ] || continue
	if [ "$p" = '根包（App 层）' ]; then printf '.\n'; else printf './%s\n' "$p"; fi
done | sed 's|^\./||; s|^\.$|.|' | sort -u)
cmpv 'B1 §3.2 包集合 ≡ 含 *_test.go 的包集合' "$(printf '%s' "$DOC_DIRS_SORT" | tr '\n' ' ')" "$(printf '%s' "$REAL_DIRS" | tr '\n' ' ')"

# ---- B2 §3.2 合计格 ----
say_b 'B2 docs/04 §3.2 合计格 + 全仓源码级通道（与 run-gates 的 src_test 同一条命令）'
if anch '^\| 合计 \| \*\*[0-9]+ Test / [0-9]+ Benchmark\*\*' "$DOC04" '§3.2 合计格'; then
	d_all=$(printf '%s' "$ANCH" | sed -nE 's/^\| 合计 \| \*\*([0-9]+) Test \/ ([0-9]+) Benchmark\*\*.*/\1/p')
	d_bench=$(printf '%s' "$ANCH" | sed -nE 's/^\| 合计 \| \*\*([0-9]+) Test \/ ([0-9]+) Benchmark\*\*.*/\2/p')
else
	d_all=''
	d_bench=''
fi
r_all=$(grep -rh "$B_TST" --include='*_test.go' --exclude-dir=node_modules --exclude-dir=.workbuddy . | wc -l | tr -d '[:space:]') || r_all=''
r_bench=$(grep -rh '^func Benchmark' --include='*_test.go' --exclude-dir=node_modules --exclude-dir=.workbuddy . | wc -l | tr -d '[:space:]') || r_bench=''
cmpv '  §3.2 合计 Test 数' "$d_all" "$r_all"
cmpv '  §3.2 合计 Benchmark 数' "$d_bench" "$r_bench"
cmpv '  B1 逐包相加 ≡ §3.2 合计格' "$B1_SUM" "${r_all:-}"

# ---- B3 §3.2 平台三格（GOOS 交叉清单通道，即表内"来源"列自己写的命令） ----
say_b 'B3 docs/04 §3.2 平台三格（linux / darwin / windows）'
plat_tests() {
	local goos="$1" paths out
	paths=$(GOOS=$goos go list -e -f '{{range .TestGoFiles}}{{$.Dir}}/{{.}}
{{end}}' ./...) || paths=''
	if [ -z "$paths" ]; then
		printf ''
		return 1
	fi
	out=$(printf '%s\n' "$paths" | xargs grep -h "$B_TST" 2>/dev/null | wc -l | tr -d '[:space:]') || out=''
	printf '%s' "$out"
	return 0
}
for plat in 'linux' 'darwin' 'windows'; do
	if anch "^\| ${plat} \| \*\*[0-9]+\*\*" "$DOC04" "§3.2 平台 ${plat} 格"; then
		d_p=$(printf '%s' "$ANCH" | sed -nE "s/^\| ${plat} \| \*\*([0-9]+)\*\*.*/\1/p")
	else
		d_p=''
	fi
	r_p=$(plat_tests "$plat") || r_p=''
	cmpv "  ${plat} 腿 Test 数（go list -e 通道）" "$d_p" "${r_p:-<go list 取空>}"
done

# ---- B4 §4.1 前端格（node 用例 + 接线断言 = 合计） ----
say_b 'B4 docs/04 §4.1 前端格（node 用例 / 接线断言 / 合计自洽式）'
if anch 'node 用例 [0-9]+ 项 \+ 接线断言 [0-9]+ 项 = 合计 [0-9]+ 项\*\*（脚本末行自报' "$DOC04" '§4.1 前端格'; then
	d_node=$(printf '%s' "$ANCH" | sed -nE 's/.*node 用例 ([0-9]+) 项 \+ 接线断言 ([0-9]+) 项 = 合计 ([0-9]+) 项\*\*.*/\1/p')
	d_wir=$(printf '%s' "$ANCH" | sed -nE 's/.*node 用例 ([0-9]+) 项 \+ 接线断言 ([0-9]+) 项 = 合计 ([0-9]+) 项\*\*.*/\2/p')
	d_sum=$(printf '%s' "$ANCH" | sed -nE 's/.*node 用例 ([0-9]+) 项 \+ 接线断言 ([0-9]+) 项 = 合计 ([0-9]+) 项\*\*.*/\3/p')
else
	d_node=''
	d_wir=''
	d_sum=''
fi
r_node=$(grep -h '^test(' frontend/tests/*.test.ts 2>/dev/null | wc -l | tr -d '[:space:]') || r_node=''
r_wir1=$(grep -c '^wiring ' scripts/test-frontend-logic.sh) || r_wir1=''
r_wir2=$(grep -c '^wiring_count ' scripts/test-frontend-logic.sh) || r_wir2=''
r_wir3=$(grep -c '^wiring_window ' scripts/test-frontend-logic.sh) || r_wir3=''
if [ -n "$r_wir1" ] && [ -n "$r_wir2" ] && [ -n "$r_wir3" ]; then
	r_wir=$((r_wir1 + r_wir2 + r_wir3))
else
	r_wir=''
fi
cmpv '  前端 node 用例数' "$d_node" "$r_node"
cmpv '  前端接线断言数（wiring + wiring_count + wiring_window）' "$d_wir" "$r_wir"
cmpv '  前端合计自洽式（node + 接线 = 合计）' "$d_sum" "$([ -n "$d_node" ] && [ -n "$d_wir" ] && printf '%s' "$((d_node + d_wir))")"

# ---- B5 §4.3 的接线断言条数（与 B4 同源：防"两格分叉"） ----
say_b 'B5 docs/04 §4.3 接线断言条数（同一真值二次出现，防两格分叉）'
if anch '状态机 \+ \*\*[0-9]+ 条\*\*静态接线断言' "$DOC04" '§4.3 接线断言条数'; then
	d_wir5=$(printf '%s' "$ANCH" | sed -nE 's/.*状态机 \+ \*\*([0-9]+) 条\*\*静态接线断言.*/\1/p')
else
	d_wir5=''
fi
cmpv '  §4.3 静态接线断言条数' "$d_wir5" "$r_wir"

# ---- B6 docs/README 勘误表条数（口径就用该文件自己钉的那条 awk） ----
say_b 'B6 docs/README 勘误表条数（01/02/03 表体数据行）'
if anch '\*\*表体 [0-9]+ / [0-9]+ / [0-9]+ 条\*\*' "$DOCR" '勘误表条数格'; then
	d_e1=$(printf '%s' "$ANCH" | sed -nE 's/.*\*\*表体 ([0-9]+) \/ ([0-9]+) \/ ([0-9]+) 条\*\*.*/\1/p')
	d_e2=$(printf '%s' "$ANCH" | sed -nE 's/.*\*\*表体 ([0-9]+) \/ ([0-9]+) \/ ([0-9]+) 条\*\*.*/\2/p')
	d_e3=$(printf '%s' "$ANCH" | sed -nE 's/.*\*\*表体 ([0-9]+) \/ ([0-9]+) \/ ([0-9]+) 条\*\*.*/\3/p')
else
	d_e1=''
	d_e2=''
	d_e3=''
fi
i=0
r_err=''
for f in docs/01-*.md docs/02-*.md docs/03-*.md; do
	if [ ! -f "$f" ]; then
		fail "B6 的勘误表文件读不到：${f} ⇒ 扫描面本身没成立，不读作通过"
		continue
	fi
	i=$((i + 1))
	n=$(awk '/^> *[|][[:space:]]*-{3}/{t=1;next} t&&/^> *[|]/{c++;next} t{exit} END{print c+0}' "$f")
	r_err="${r_err}${n} "
done
[ "$i" -eq 3 ] || fail "B6 只数到 ${i} 份 docs/01–03（要求 3 份）⇒ glob 没展开或文件改名，不读作通过"
cmpv '  勘误表条数 01/02/03' "$(printf '%s / %s / %s' "${d_e1:-}" "${d_e2:-}" "${d_e3:-}")" "$(printf '%s' "$r_err" | sed 's/ $//; s/ / \/ /g')"

# ---- B7 §7 包数 / B8 §7 门禁脚本份数与名单 ----
say_b 'B7/B8 docs/04 §7 交付物清单（internal 包数、门禁脚本份数与名单双向对齐）'
if anch '（\*\*[0-9]+ 包\*\*' "$DOC04" '§7 代码行包数'; then
	d_pkg=$(printf '%s' "$ANCH" | sed -nE 's/.*（\*\*([0-9]+) 包\*\*.*/\1/p')
else
	d_pkg=''
fi
r_pkg=$(go list ./internal/... 2>/dev/null | wc -l | tr -d '[:space:]') || r_pkg=''
cmpv '  §7 internal 包数' "$d_pkg" "${r_pkg:-<go list 取空>}"
if anch '^\| 门禁脚本 \| [0-9]+ 份：' "$DOC04" '§7 门禁脚本行'; then
	d_scripts=$(printf '%s' "$ANCH" | sed -nE 's/^\| 门禁脚本 \| ([0-9]+) 份：.*/\1/p')
	r_listed=$(printf '%s' "$ANCH" | grep -oE '`[^`]*\.sh`' | tr -d '`' | xargs -n1 basename 2>/dev/null | sort -u | tr '\n' ' ')
	n_listed=$(printf '%s' "$r_listed" | wc -w | tr -d '[:space:]') || n_listed=0
else
	d_scripts=''
	r_listed=''
	n_listed=0
fi
r_actual=$(ls scripts/*.sh 2>/dev/null | xargs -n1 basename 2>/dev/null | sort -u | tr '\n' ' ') || r_actual=''
cmpv '  §7 门禁脚本份数（行内声明 ≡ ls scripts/*.sh）' "$d_scripts" "$(printf '%s' "$r_actual" | wc -w | tr -d '[:space:]')"
cmpv '  §7 门禁脚本名单（行内列出 ≡ 目录实际）' "${r_listed:-空}" "${r_actual:-空}"
if [ "$n_listed" != "${d_scripts:-0}" ]; then
	fail "§7 门禁脚本行：声明 ${d_scripts:-<未取到>} 份，但行内实际列出 ${n_listed} 个文件名 ⇒ 「份数」与「名单」自相矛盾"
else
	printf '  \033[32m✓\033[0m %-46s %s\n' '  §7 份数 ≡ 行内名单条数' "$n_listed"
fi

echo
printf 'B 组小计：锚点 %s 个，比对 %s 格（外加 B1 的 %s 行逐包）\n' "$N_ANCH" "$N_CMP" "$B1_ROWS"

if [ "$BAD" -ne 0 ]; then
	echo
	echo "修复：A 组改版本号只需一次同步 6 个取值位 / 5 个文件——"
	echo "      app.go / wails.json / frontend/package.json / frontend/package-lock.json(2 处) / docs/09，"
	echo "     然后重新运行本脚本。build/darwin/*.plist 用 {{.Info.ProductVersion}} 模板，无需手改。"
	echo "     B 组红了改文档：M210 的取向是「文档跟着代码走」，每条比对的真值通道就印在本脚本里，"
	echo "     现跑现取；只有确认锚点本身过时（表形状变了、包改名了）才动本脚本，并在 04 划账里说明。"
	exit 1
fi
echo
printf '\033[32m✓ A 组版本号全部对齐：%s\033[0m\n' "$APP_VER"
printf '\033[32m✓ B 组活文档计数全部相符：%s 个锚点 / %s 格比对（含 §3.2 逐包 %s 行）\033[0m\n' "$N_ANCH" "$N_CMP" "$B1_ROWS"
