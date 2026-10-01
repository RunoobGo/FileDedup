# M404 细化设计段：`ci-go-test.sh` 的 SKIP 清册在"二进制判定"下会静默缺条

日期：2026-10-01　依据：用户「继续」（§6.71 收口之后）　拟号：**M404**
本轮范围：**一项**，只补 M331 自己的缺陷，不碰 D1／D3 已落形状，不新增脚本、不新增用例。

---

## 1. 现状取证（三条，全部现读，命令可照抄复取）

### 1.1 CI 现读（run `36826343884`，windows 腿 job `110252769986`）

该腿 `scripts/test-windows-quarantine.sh` 的 `QUARANTINE` 清单为空 ⇒ 走的是
`exec bash "$(dirname "$0")/ci-go-test.sh" -count=1 ./...` 那一支（无 `-skip`）。读数行：

```
M331_READOUT rc=0 pkgs_ok=23 skip_top=28 skip_sub=1 fail_top=0 fail_sub=0 raw_log=/tmp/ci-go-test-1237.log
```

而 READOUT **之后**那份"清册"（脚本自己 `grep '^--- SKIP' "$LOG"` 打出来的一段）只列出
**10 条**，紧跟着的是一行：

```
Binary file /tmp/ci-go-test-1237.log matches
```

⇒ `skip_top=28` 与名单 **28** 条不一致：**计数是准的，名单是缺的**。
完整名单不重跑也能复原——同一条腿里 `go test` 自己的 stdout（经显示口径 sed 后仍在日志中）
逐条带着 `--- SKIP`，`grep -oE -- '--- SKIP: [^ ]+' | sort -u` 得 **29 个唯一名**
（28 顶层 + 1 子测试），与 READOUT 两个计数逐值相符。

### 1.2 本机复刻（这是本批的"改前红"，见 §3 P-67）

放一枚临时探针文件（两条 `t.Skip` + 一条 `t.Log` 里带串内 NUL，跑完即删、不进提交），
用**未改动的**仓库脚本跑：

```
CI_GO_TEST_LOG=/tmp/r672_pre.log bash scripts/ci-go-test.sh -count=1 -run '^TestM404' ./internal/ops/
```

实测三件事**同时**成立：

| 量 | 实测 |
|---|---|
| `skip_top` | `2`（准） |
| 脚本自己打出的清册 | `0` 条，只有一行 `Binary file /tmp/r672_pre.log matches` |
| 退出码 | `rc=0`（绿） |

⇒ 本机 darwin（BSD grep 2.6.0-FreeBSD）与 CI windows（GNU grep）**同一签名、不同截断点**：
BSD 一旦判二进制就只给那一行通知（连前面的匹配行也不给），GNU 是"先给已匹配的若干行、
命中二进制数据处停止 + 通知"。两族都会**静默**少印名单，且都不影响 `-c` 计数。

### 1.3 一条取证限制（本轮新撞，值得钉住）

`gh run view --log` 下载的 job 日志里 **NUL 已被清掉**：三份 job 日志
（check / macos / windows）`LC_ALL=C tr -d '\000' < f | cmp -s - f` 全部逐字节相同 ⇒ 零 NUL；
而同一条命令的本机 stdout 里 NUL 原样在（`tr -cd '\000' | wc -c` = 1）。
⇒ **不能拿 CI 下载日志去定位"是哪条用例把 NUL 打进日志的"**；要找触发用例只能在
本机复刻（§3）或读 `go test` 自己的输出段。本批**不给成因下结论**：`-a` 对 NUL 与
非法编码两条路都强制按文本处理，修法是同一条，成因归属不影响它。

---

## 2. 修法

改 `scripts/ci-go-test.sh` 一个文件，两处：

1. **读 `$LOG` 的那一组 grep 一律加 `-a`**（计数 4 处 + 包级 `ok` 1 处 + 名单打印 2 处）。
   要点是**计数与名单用同一把尺**：只给名单加 `-a` 会留下"数的时候按二进制、列的时候
   按文本"这种新的不同形。
   - 为什么用 `-a` 不用 `LC_ALL=C`：`-a` 在 BSD／GNU 两族语义一致（强制按文本），
     而 `LC_ALL=C` 同时改掉字符类与区间比较的语义（本仓为它踩过 M134 那一族），
     一次只引入一个概念；`E 组` 那处 `LC_ALL=C grep` 是"按字节找非 ASCII"，需求不同。
2. **加一条自检等式**：清册实际打出的条数必须等于计数（顶层 `roster_top == skip_top`、
   子测试 `roster_sub == skip_sub`），READOUT 行随之多报 `roster_top=` / `roster_sub=`
   两格；不等就向 stderr 打一条 `FAIL:` 并把退出码置红（`go test` 自己已经红时保留它的码）。
   - 为什么自检必须**脚本自己数**：这条腿的读者是 CI，没有任何人在核对名单条数；
     "一条都没打"与"打了但缺 17 条"在日志里都表现为"少几行" ⇒ 同形。M331 立的就是
     这类规矩，本轮把它用回它自己身上。
   - 等式在正常日志下恒成立（同一文件、同一模式，`-a` 后 grep 不再改口），
     ⇒ 判红不存在"合法不等"的来源；若哪天 go 换了输出形状，那正是该红的东西。

不改的东西（各给一句理由）：

- `sed` 显示口径不动：`sed` 不做二进制判定，实测那两条 `--- SKIP` 与 NUL 行都原样穿过去了。
- `run-gates.sh` 行数、04 §7「门禁脚本 N 份」不动：没有新脚本，活文档计数格一格不动。
- `smoke-symlink-assert.sh` E 组扫描面不动（仍是 8 个 `scripts/*.sh` + `ci.yml` = 9）。

---

## 3. 探针与变异

| 号 | 动作 | 期望 |
|---|---|---|
| P-67 | 未改动脚本 + 含 NUL 日志（§1.2 命令） | **红在"缺"**：`skip_top=2`、清册 0 条、`rc=0` ⇒ 静默（改前形状） |
| P-68 | 加 `-a` + 自检后，同一命令 | `roster_top=2 skip_top=2`、两条名字都列出、`rc=0` |
| MU11-a | 只摘掉 `-a`（自检留着），同一命令 | **必红**：`roster_top=0 ≠ skip_top=2` ⇒ 证明自检承重，不是永远绿 |
| MU11-b | 只摘掉自检（`-a` 留着），临时把名单 grep 改回不带 `-a` 的等价形状 | 复现 P-67 的静默 ⇒ 证明"改后绿"来自 `-a` 而不是别的改动 |

变异还原一律 `cp` 备份 + `cmp` 复验（本仓规矩，不用 `git checkout --`）。
探针文件 `internal/ops/zz_m404_tmp_probe_test.go` **不进提交**，跑完 `rm` 并用
`git status --short` 确认工作树只剩 `build/m7scratch/`。

---

## 4. 兑现边界（写在前头，免得划账吹）

- 本机 darwin 能兑现的：P-67／P-68／MU11-a／MU11-b 四格（BSD grep 与 GNU 同一签名）。
- 本机**兑现不了**的：GNU grep 在 windows 腿上的确切截断点与那 28 条的名字是否**逐条**对上。
  下一次 push 的 windows 腿 READOUT 会自报 `roster_top=`——若 `roster_top=28` 且与
  `skip_top=28` 相等，则该格由脚本自己判定为闭合。
- ★ 那一跑是本批自己触发的 run ⇒ 按 §6.21 七 的**回填边界**，其读数**只报给用户，
  不回写 `docs/04`**。本设计段引用的是**上一批**（§6.71）那跑的现读，不是本批自己的。
- 本轮不占别的号：M401（`build.yml` 无防回归锚）、M402（`build.yml` 仍不带 `-v`）
  与 M177／M334（E 组扫描面加 `build.yml`）**都不在本批范围**，用户未点名，不动。
