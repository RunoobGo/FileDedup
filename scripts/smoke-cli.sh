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
#
# 除"三跑互比"外的四道硬断言（2026-09-23 第四轮审查 R1-2/R4-1，设计段 §30.2）：
#   failed == 0、reclaimable 与 dup_files 等于 manifest 逐组推得的期望值、
#   **逐组** (可释放字节, 组内路径集合) 整表等于 manifest 重建的期望表、
#   manifest.shapes 的 symlink/case_pair 在场（case_pair 仅在 linux 判红，其余打印）。
# 为什么要加：互比只能发现"不一致"，组数与语料总数只能发现"整组没了"，
# 三者一起漏（某组成员被一致性剔除而组仍 ≥2 成员）在旧断言下是绿的。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DATASET="${SMOKE_DATASET:-C}"
SCALE="${SMOKE_SCALE:-0.02}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/fdd-smoke.XXXXXX")"
# D1（R-门禁-3）：失败时**保留**现场供检。旧版无条件 rm -rf，把 fdd-cli 的
# 整段 stderr（.err）连同三跑的 .json 一起销毁——门禁报红时只看到"退出码非 0"，
# 拿不到任何可定位的现场。改成按退出码分流：rc=0 才清理，rc≠0 保留并打印路径。
cleanup() {
	local rc=$?
	chmod -R u+rwX "$WORK" 2>/dev/null
	if [ "$rc" -ne 0 ]; then
		printf '\n\033[1;31m==> 冒烟失败（rc=%s），现场已保留供检：%s\033[0m\n' "$rc" "$WORK" >&2
		ls -la "$WORK" >&2 2>/dev/null || true
	else
		rm -rf "$WORK"
	fi
}
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
	# D1（R-门禁-3）：不让 set -e 在 fdd-cli 非零退出时直接掀桌——先接住退出码，
	# 失败则把**整段** stderr 打出来（旧版只 sed 第 1 行，且那行在失败时根本轮不到执行）。
	# D3（R-门禁-5）：rc=3 = "扫描完成但有失败项"，报告 JSON 仍有效——放行到下面的比对块，
	# 由 python 侧 `failed != 0` 断言判红（保留三跑互比与逐组对账的诊断价值）；
	# rc=1/2（崩溃/用法错）才就地停下。绿路径 failed=0 → rc=0，本分支都不触发。
	local rc=0
	"$WORK/fdd-cli" -paranoid -o "$WORK/$tag.json" "$@" "$WORK/bench" 2>"$WORK/$tag.err" || rc=$?
	if [ "$rc" -eq 3 ]; then
		printf '    %s: fdd-cli rc=3（有失败项），报告有效，交由比对块裁决\n' "$tag"
	elif [ "$rc" -ne 0 ]; then
		printf '\n\033[1;31m==> %s：fdd-cli 退出码 %s，完整 stderr：\033[0m\n' "$tag" "$rc" >&2
		cat "$WORK/$tag.err" >&2 2>/dev/null || true
		exit "$rc"
	fi
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
    # 第三个返回值是**逐组**读数（R1-2 的 manifest 对账要用；上面那个 digest 只能互比，
    # 发现不了"三跑一起漏"，也定位不到是哪一组漏了哪个成员）。
    return cmp, int(s["cache_hits"]), groups

vals, hits, grps = {}, {}, {}
for t in tags:
    vals[t], hits[t], grps[t] = sig(t)
for t in tags:
    print(f"    {t:7s} {vals[t]} cache_hits={hits[t]}")

base = vals[tags[0]]
bad = [t for t in tags if vals[t] != base]
if bad:
    print(f"\nFAIL: {','.join(bad)} 与 {tags[0]} 不一致", file=sys.stderr)
    sys.exit(1)
# R1-2（第四轮审查，设计段 §30.2）：failed 原先**只进三跑互比键**（上面那个 cmp dict），
# 三跑一起失败照样逐字一致 ⇒ 这条断言从来没有存在过。门禁语料是本地合成、无权限障碍的
# 干净树，任何一条失败项都说明扫描器/流水线在吃错误。
if base["failed"] != 0:
    print(f"\nFAIL: failed={base['failed']}，门禁语料不允许有失败项"
          "（一致性剔除、预筛、权限任一处的失败都会把整组成员悄悄带走）", file=sys.stderr)
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

# ---- R1-2：逐组对账（旧的两项"组数 / 语料总数"粒度粗于数据面） ----
# 组内成员被一致性剔除一个而组仍 ≥2 成员时：组数不变、files_total 不变、三跑照样一致
# ⇒ 上面四条断言全绿。这里按 manifest 逐组重建期望的 (可释放字节, 组内路径集合)，
# 与报告交出的那一串**整表**比对，并顺手把两个聚合量也钉住（红的时候一眼看得出是哪一维）。
def norm(p):
    # 只做分隔符归一：manifest 的 rel 由 Go 的 filepath.Rel 产出（Windows 上是反斜杠），
    # 报告里的 path 由扫描器产出。比对的是"组成员集合"，不是拼写口径。
    return str(p).replace("\\", "/")

root = norm(work / "bench")
exp = []
exp_reclaimable = 0
exp_dup = 0
for g in mani["groups"]:
    files = sorted(f"{root}/{norm(f)}" for f in g["files"])
    n = len(files)
    if n < 2:
        print(f"\nFAIL: manifest 有一组只有 {n} 个成员（{files}）——生成器产出了不成组的组，"
              "逐组期望值无法定义", file=sys.stderr)
        sys.exit(1)
    exp.append((int(g["size"]) * (n - 1), "\n".join(files)))
    exp_reclaimable += int(g["size"]) * (n - 1)
    # ★ duplicate_files 的定义是"**位于重复组内的文件数**"（含每组那个保留项），
    #   不是冗余项数——现场取证：cmd/fdd-cli/main.go:156 对 g.Files 逐个自增，
    #   而 reclaimable 走的是 g.Reclaimable（已扣除保留项）。本脚本第一次跑新断言时
    #   按"冗余项"推得 313、实报 518，差值恰为组数 205 ⇒ 钉的是 Σn 这一格。
    #   将来谁把这个字段改成冗余项数，这里必须红一次并要求同步。
    exp_dup += n
exp.sort(key=lambda x: x[1])
act = grps[tags[0]]

if base["reclaimable"] != exp_reclaimable:
    print(f"\nFAIL: reclaimable={base['reclaimable']}，manifest 逐组期望 {exp_reclaimable}"
          "（组成员被一致性剔除在此暴露）", file=sys.stderr)
    sys.exit(1)
if base["dup_files"] != exp_dup:
    print(f"\nFAIL: dup_files={base['dup_files']}，manifest 逐组期望 {exp_dup}"
          "（口径：位于重复组内的文件数，每组 n 个成员计 n 个，含保留项）", file=sys.stderr)
    sys.exit(1)
if act != exp:
    only_exp = [e for e in exp if e not in act][:3]
    only_act = [a for a in act if a not in exp][:3]
    print(f"\nFAIL: 逐组对账不一致（期望 {len(exp)} 组 / 实报 {len(act)} 组）", file=sys.stderr)
    for e in only_exp:
        print(f"      仅 manifest 有此组: reclaimable={e[0]} files={e[1].splitlines()}",
              file=sys.stderr)
    for a in only_act:
        print(f"      仅报告有此组: reclaimable={a[0]} files={a[1].splitlines()}",
              file=sys.stderr)
    sys.exit(1)

# ---- R4-1：消费 manifest.shapes（benchgen 早就在写，本脚本此前全文不读） ----
# benchgen 的注释承诺"造不出来的形态必须与造出来了可区分，否则门禁把'本轮未覆盖'
# 读成'覆盖且通过'"（AS-K3 同族）。这句话原先只落在 manifest 里，没人接。
shapes = mani.get("shapes", {})
if not isinstance(shapes, dict):
    print(f"\nFAIL: manifest.shapes 是 {type(shapes).__name__}，应为对象", file=sys.stderr)
    sys.exit(1)
if not shapes.get("symlink", False):
    # 符号链接在 Linux/macOS 上无特权要求，造不出来只能是 runner 文件系统退化；
    # 冒烟的"扫描器不跟随符号链接"这条防线会随之一同消失，不许只留 warning。
    print("\nFAIL: manifest.shapes.symlink=false——语料形态退化，冒烟覆盖面缩水"
          "（软链接不跟随这条防线本轮没有任何实物见证）", file=sys.stderr)
    sys.exit(1)
if not shapes.get("case_pair", False):
    # darwin 默认卷不区分大小写（APFS 区分大小写案是显式创建的），那一格造不出来是**事实**，
    # 而 linux（CI 腿）上 /tmp 恒区分大小写，造不出来就是退化 ⇒ 分平台判，且必须打印。
    if sys.platform == "linux":
        print(f"\nFAIL: shapes.case_pair=false（平台 {sys.platform}）——"
              "遍历器路径折叠（fscase/I2）在冒烟里唯一的实物见证消失了", file=sys.stderr)
        sys.exit(1)
    print(f"    WARN: shapes.case_pair=false（平台 {sys.platform}）："
          "本卷不区分大小写，该形态本轮未覆盖（不进通过判定）")

print(f"\nOK: 三跑一致、缓存命中生效，并与 manifest **逐组**对账通过"
      f"（{base['groups']} 组 / 可释放 {base['reclaimable']} B = 期望值 / "
      f"组内文件 {base['dup_files']} 个 = 期望值 / 语料 {base['files_total']} 文件 / "
      f"失败 {base['failed']} / 复扫命中 {hits['cache2']} / shapes={shapes}）")
PY
