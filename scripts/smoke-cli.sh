#!/usr/bin/env bash
# CLI 冒烟（回归配方第 6 步，见 docs/archive/里程碑开发与测试报告.md 第 4 篇 §7.5）：固定合成数据集上「冷扫 / 缓存首扫 /
# 缓存复扫」三跑结果必须逐项一致。
#
# 为什么必须三跑而不是单跑：
#   - 冷扫 vs 缓存首扫：验证「写缓存」不改变本轮结论；
#   - 缓存首扫 vs 复扫：验证「命中缓存」不改变分组资格（P0-1 可复扫 + P0-2 增量
#     不漏报 + H1 采样身份 + B3-6 语料口径）。历史上这三条各自翻过车，
#     单跑一遍看不出来。
#
# 比较键（任一项不一致即失败）：
#   groups / reclaimable_bytes / duplicate_files / files_total / 分组路径集合摘要
#   - files_total 自 B3-6 起走 Pipeline.ScannedFiles()（语料口径），三跑必然相等；
#     若有人改回从进度事件反推，这里会立刻红（进度 FilesTotal 是阶段口径）。
#   - 分组路径集合摘要 = 每组内路径排序后拼接、组间再排序，取 sha256。
#     组顺序随并发调度变化，不能直接比 JSON 文本。
#
# 不比较：mtime（同一文件必然一致，但对断言无增益）、elapsed/速度（天生抖动）。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DATASET="${SMOKE_DATASET:-C}"
SCALE="${SMOKE_SCALE:-0.02}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/fdd-smoke.XXXXXX")"
cleanup() { chmod -R u+rwX "$WORK" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

say "构建 benchgen / fdd-cli"
go build -o "$WORK/benchgen" ./cmd/benchgen
go build -o "$WORK/fdd-cli" ./cmd/fdd-cli

say "生成数据集 ${DATASET}×${SCALE}"
"$WORK/benchgen" -dataset "$DATASET" -scale "$SCALE" -out "$WORK/bench" | tee "$WORK/gen.log"

say "扫描三跑（冷扫 / 缓存首扫 / 缓存复扫，均 -paranoid）"
run() { # $1=标签 $2..=额外参数
	local tag="$1"; shift
	"$WORK/fdd-cli" -paranoid -o "$WORK/$tag.json" "$@" "$WORK/bench" 2>"$WORK/$tag.err"
	sed -n '1p' "$WORK/$tag.err" | sed "s/^/    $tag: /"
}
run cold
run cache1 -cache "$WORK/cache.db"
run cache2 -cache "$WORK/cache.db"

say "逐项比对"
python3 - "$WORK" <<'PY'
import hashlib, json, sys, pathlib

work = pathlib.Path(sys.argv[1])
tags = ["cold", "cache1", "cache2"]

def sig(tag):
    r = json.loads((work / f"{tag}.json").read_text())
    s = r["stats"]
    # APP-10（2026-09-21 全量审查）：列表字段必须是 JSON 数组。原先这里写的是
    # `len(r["failed"] or [])`——一个替后端把契约违约藏下来的兜底：空失败表序列化成
    # null 时冒烟照样绿，而外部按 `for x in r["failed"]` 消费的机器读者直接 TypeError。
    # 去掉兜底、改成显式形状断言，null 即红。
    for key in ("groups", "failed"):
        if not isinstance(r[key], list):
            print(f"\nFAIL: {key} 序列化为 {type(r[key]).__name__}，应为数组"
                  "（空列表必须是 []，不能是 null）", file=sys.stderr)
            sys.exit(1)
    groups = []
    for g in r["groups"]:
        paths = sorted(f["path"] for f in g["files"])
        groups.append((int(g["reclaimable"]), "\n".join(paths)))
    groups.sort(key=lambda x: x[1])
    blob = "\x1e".join(f"{c}\x1f{p}" for c, p in groups).encode()
    cmp = {
        "groups": s["groups"],
        "reclaimable": s["reclaimable_bytes"],
        "dup_files": s["duplicate_files"],
        "files_total": s["files_total"],
        "failed": len(r["failed"]),
        "digest": hashlib.sha256(blob).hexdigest()[:16],
    }
    # cache_hits **不进比较键**：冷扫/首扫/复扫三者的命中数本就应不同
    # （0 / 0 / >0），把它并进一致性比对会把脚本钉死成永远红。
    # 它单独走下面的正向断言。
    if "cache_hits" not in s:
        print("\nFAIL: stats 缺 cache_hits 字段（AS-K1 断言依赖它；改字段名请同步本脚本）",
              file=sys.stderr)
        sys.exit(1)
    return cmp, int(s["cache_hits"])

vals, hits = {}, {}
for t in tags:
    vals[t], hits[t] = sig(t)
for t in tags:
    print(f"    {t:7s} {vals[t]} cache_hits={hits[t]}")

base = vals[tags[0]]
bad = [t for t in tags if vals[t] != base]
if bad:
    print(f"\nFAIL: {','.join(bad)} 与 {tags[0]} 不一致", file=sys.stderr)
    sys.exit(1)
if base["groups"] == 0:
    print("\nFAIL: 数据集未产生任何重复组，冒烟失去意义", file=sys.stderr)
    sys.exit(1)

# AS-K1：上面那些只能发现"三跑不一致"，发现不了"缓存压根没生效"——
# UseCache 断线时三跑等价于三次冷扫，逐项照样一致、门禁照样全绿。
# 所以要一个**正向**信号：复扫必须报出命中。
if hits["cold"] != 0:
    print(f"\nFAIL: 冷扫（未挂 -cache）命中数 = {hits['cold']}，应为 0"
          "（cacheOn=false 时一次 Lookup 都不该发生）", file=sys.stderr)
    sys.exit(1)
if hits["cache1"] != 0:
    print(f"\nFAIL: 首扫（空库）命中数 = {hits['cache1']}，应为 0"
          "（非 0 说明命中计数被接到了别的东西上，本批断言全部失真）", file=sys.stderr)
    sys.exit(1)
if hits["cache2"] <= 0:
    print(f"\nFAIL: 复扫命中数 = {hits['cache2']}，必须 > 0"
          "——零命中即缓存断线（cfg.UseCache / WithCache(nil) / cacheOn 条件任一处）。"
          "\n      负控制验证：把 cache2 那行的 -cache 参数去掉重跑本脚本，必须红在这里。",
          file=sys.stderr)
    sys.exit(1)

# 与 benchgen 的 manifest 对账：漏报（缓存/预筛收敛过头）在此暴露，
# 三跑互比只能发现"不一致"，发现不了"三跑一起漏"。
mani = json.loads((work / "bench" / "manifest.json").read_text())
want_groups = len(mani["groups"])
if base["groups"] != want_groups:
    print(f"\nFAIL: 报出 {base['groups']} 组，manifest 期望 {want_groups} 组（漏报或误报）",
          file=sys.stderr)
    sys.exit(1)
want_total = mani["total_files"] + 1  # +1：manifest.json 本身也位于扫描根内
if base["files_total"] != want_total:
    print(f"\nFAIL: files_total={base['files_total']}，manifest 语料={want_total}"
          "（语料口径回归，见 B3-6）", file=sys.stderr)
    sys.exit(1)
print(f"\nOK: 三跑一致、缓存命中生效且与 manifest 对账通过"
      f"（{base['groups']} 组 / 可释放 {base['reclaimable']} B / 语料 {base['files_total']} 文件 / "
      f"复扫命中 {hits['cache2']}）")
PY
