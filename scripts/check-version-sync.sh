#!/usr/bin/env bash
# 版本号一致性门禁（04 附录 A「版本号与 GetVersion 一致」的机器化版本）。
#
# 校验以下声明位必须完全相等。共 **6 个取值位 / 5 个文件**：
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

# tag 比对：优先命令行参数，其次 GITHUB_REF_NAME / GITHUB_REF
REF="${1:-${GITHUB_REF_NAME:-${GITHUB_REF:-}}}"
if [ -n "$REF" ]; then
	TAG=${REF##refs/tags/}
	case "$TAG" in
		v[0-9]*) ;;
		*) TAG="" ;;   # 普通分支 push / workflow_dispatch：不比对 tag
	esac
fi
if [ -n "${TAG:-}" ]; then
	if [ "${TAG#v}" != "$APP_VER" ]; then
		fail "tag $TAG 与 AppVersion $APP_VER 不一致（发布物名字与 GetVersion 会各说各话）"
	else
		printf '\033[32m✓ tag %s 与版本号一致\033[0m\n' "$TAG"
	fi
else
	echo "  （非 tag 触发，跳过 tag 比对）"
fi

if [ "$BAD" -ne 0 ]; then
	echo
	echo "修复：改版本号只需一次同步 6 个取值位 / 5 个文件——"
	echo "      app.go / wails.json / frontend/package.json / frontend/package-lock.json(2 处) / docs/09，"
	echo "     然后重新运行本脚本。build/darwin/*.plist 用 {{.Info.ProductVersion}} 模板，无需手改。"
	exit 1
fi
echo
printf '\033[32m✓ 版本号全部对齐：%s\033[0m\n' "$APP_VER"
