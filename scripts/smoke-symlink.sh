#!/usr/bin/env bash
# 软链接合并端到端冒烟（2026-09-20，跨卷软链接功能）。
#
# 为什么需要这个脚本（而不是只靠单测）：
#   单测里的"跨卷"是用两个 t.TempDir() 模拟的——在 Linux 上它们其实是
#   **同一个卷**，所以真正被验证的只有"软链接能建、能复核、能回撤"，
#   而**跨卷这件事本身从未被端到端跑过**。本脚本用 loop 设备挂一个真实的
#   独立文件系统，让 os.Link 真的失败、os.Symlink 真的跨越卷边界。
#
# 覆盖的判据（每条都必须成立，任一失败即 FAIL）：
#   ① 硬链接跨卷**失败**（这是本功能存在的理由，必须实证）
#   ② 软链接跨卷**成功**，且原路径可读、内容与保留源一致
#   ③ 软链接自身是一个独立小文件（占用远小于数据本体）
#   ④ 删除链接**不影响**保留源数据
#   ⑤ 保留源被移动后，链接变为悬空（Lstat 是链接、Stat 失败）
#   ⑥ 回撤（概念等价操作）：删链接 + 还原备份 → 原路径恢复为独立文件
#   ⑦ 悬空链接仍可回撤（备份是数据本体，与链接有效性无关）
#
# 退出码契约（调用方必须按此区分，勿只看"非零即红、零即绿"）：
#   0 = 7 条判据全部成立；2 = 环境无法构造独立卷的**合法跳过**（stdout/stderr
#   带 "SKIP:" 前缀）；1 = 真失败。跳过**不**算通过，见 skip() 的说明。
#
# 独立卷的构造方式（按可用性降级，两条路径都提供真正的跨设备语义）：
#   优先 loop + ext4（更接近真实磁盘：有真实 inode、真实空间限制）；
#   loop 不可用时退到 tmpfs——它是**独立的内存文件系统**，设备号不同，
#   `ln` 同样返回 EXDEV，因此"硬链接跨卷必失败"这一核心判据依旧成立。
#   两者都不行才跳过（以 2 跳过，绝不静默通过）。
#
# 不依赖 fdd-cli / 应用二进制：直接编排 shell 层面的文件系统语义，
# 因此它验证的是"这个功能赖以成立的内核行为"，而非应用代码——
# 应用代码的正确性由 go test 负责，两者互补。

set -euo pipefail

say() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\n\033[31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }
# skip 以退出码 2 + "SKIP:" 前缀标记**自证**，绝不与全绿（0）同形。
#
# ★ 2026-09-20（审查 M8）：修正前跳过走 `exit 0`。门禁只看退出码，于是
# "环境挂不上独立卷"与"7 条判据全部成立"在 CI 里长得一模一样——哪天
# runner 收紧（无 loop 设备、无 tmpfs、sudo 策略变化），这条唯一的真实跨卷
# 防线会**静默消失**而流水线仍是绿的。
# 现在跳过必须由调用方**显式认领**（见 .github/workflows/ci.yml：
# 只接受 "SKIP:" 前缀 + 退出码 2，其余非零一律红）。
skip() {
	printf 'SKIP（跳过，非通过）: %s\n' "$*" >&2
	exit 2
}

if [ "$(id -u)" -ne 0 ]; then
	skip "需要 root 才能挂载独立文件系统（当前 uid=$(id -u)）。本脚本不降级为「同卷模拟」——那样就失去了它存在的意义；请在容器/普通 root shell 中运行，或接受本项未验证。"
fi

WORK="$(mktemp -d /tmp/fdd-symlink-smoke.XXXXXX)"
MNT="$WORK/mnt"
IMG="$WORK/vol.img"
LOOP=""
MOUNTED_BY_US=""
cleanup() {
	if [ -n "$MOUNTED_BY_US" ]; then
		umount "$MNT" 2>/dev/null || true
	fi
	if [ -n "$LOOP" ]; then
		losetup -d "$LOOP" 2>/dev/null || true
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT

mkdir -p "$MNT"

# ---- 构造独立卷：优先 loop+ext4，退到 tmpfs ----
if command -v mkfs.ext4 >/dev/null 2>&1 && command -v losetup >/dev/null 2>&1; then
	say "尝试 loop + ext4 独立卷"
	dd if=/dev/zero of="$IMG" bs=1M count=32 status=none
	if mkfs.ext4 -q -F "$IMG" 2>/dev/null && LOOP="$(losetup --find --show "$IMG" 2>/dev/null)" \
		&& mount "$LOOP" "$MNT" 2>/dev/null; then
		MOUNTED_BY_US=loop
		echo "    ✓ 已挂载 ext4（$LOOP）"
	else
		# ★ 2026-09-20（审查 M8）：修正前这里直接 LOOP=""。losetup 已建立的
		# 设备就此泄漏（cleanup 见 LOOP 为空便不 losetup -d）——反复运行会
		# 耗尽 /dev/loop*，症状是**后续作业**挂不上设备然后跳过，很难归因到这里。
		# 顺序不能反：必须先按旧值释放，再清空变量。
		if [ -n "$LOOP" ]; then
			losetup -d "$LOOP" 2>/dev/null || true
		fi
		LOOP=""
		echo "    loop/ext4 不可用（容器常见：无 /dev/loop*），退到 tmpfs"
	fi
fi

if [ -z "$MOUNTED_BY_US" ]; then
	say "使用 tmpfs 作为独立卷"
	if ! mount -t tmpfs -o size=32M tmpfs "$MNT" 2>/dev/null; then
		skip "无法挂载任何独立文件系统（loop 与 tmpfs 均不可用）。核心判据「硬链接跨卷必失败」无法在纯同卷环境验证。"
	fi
	MOUNTED_BY_US=tmpfs
	echo "    ✓ 已挂载 tmpfs"
fi

# 卷 A（宿主）与卷 B（挂载点）确实是两个不同的文件系统
A_DEV="$(stat -c %d "$WORK")"
B_DEV="$(stat -c %d "$MNT")"
# 设备号必须真打出来：fail() 以 %s 渲染，直接传 "%d" 会原样输出字面量，
# 排查时恰恰看不到想知道的两个设备号。
[ "$A_DEV" != "$B_DEV" ] || fail "$(printf '两个目录仍在同一设备（A=%s B=%s）上，测试前提不成立' "$A_DEV" "$B_DEV")"
echo "    卷 A dev=$A_DEV ｜ 卷 B dev=$B_DEV"

KEEP="$WORK/keep.bin"
DUP="$MNT/dup.bin"
head -c 262144 /dev/urandom > "$KEEP"
cp "$KEEP" "$DUP"
KEEP_SUM="$(sha256sum "$KEEP" | cut -d' ' -f1)"

# ---- ① 硬链接跨卷必须失败 ----
say "① 硬链接跨卷（预期失败）"
if ln "$KEEP" "$MNT/hardlink-try" 2>"$WORK/ln.err"; then
	fail "硬链接竟然跨卷成功了——本功能的前提不成立（内核/文件系统行为与预期不符）"
fi
echo "    ✓ 如预期失败：$(tr -d '\n' < "$WORK/ln.err" | sed 's/^ln: //')"

# ---- ② 软链接跨卷必须成功，且内容跟随 ----
say "② 软链接跨卷（预期成功）"
ln -s "$KEEP" "$MNT/link.bin"
[ -L "$MNT/link.bin" ] || fail "目标位置不是符号链接"
[ "$(sha256sum "$MNT/link.bin" | cut -d' ' -f1)" = "$KEEP_SUM" ] \
	|| fail "通过链接读到的内容与保留源不一致"
[ "$(sha256sum "$KEEP" | cut -d' ' -f1)" = "$KEEP_SUM" ] || fail "保留源内容被改变"
echo "    ✓ 链接建立，内容与保留源一致（$KEEP_SUM）"

# ---- ③ 链接是独立小文件，占用远小于数据本体 ----
say "③ 链接自身占用（预期远小于数据）"
LINK_SZ="$(stat -c %s "$MNT/link.bin")"
DATA_SZ="$(stat -c %s "$KEEP")"
[ "$LINK_SZ" -lt "$DATA_SZ" ] || fail "链接大小（$LINK_SZ）不小于数据（$DATA_SZ）"
echo "    ✓ 链接 $LINK_SZ B ｜ 数据 $DATA_SZ B（省下约 $((DATA_SZ - LINK_SZ)) B）"

# ---- ④ 删除链接不影响保留源 ----
say "④ 删除链接后保留源应完好"
rm "$MNT/link.bin"
[ -f "$KEEP" ] || fail "删除链接竟影响了保留源"
[ "$(sha256sum "$KEEP" | cut -d' ' -f1)" = "$KEEP_SUM" ] || fail "删除链接后保留源内容变化"
echo "    ✓ 保留源完好（这正是链接与'删除一份副本'的根本区别）"

# ---- ⑤ 保留源被移动 → 链接悬空 ----
say "⑤ 保留源移动后链接应悬空"
ln -s "$KEEP" "$MNT/dangling.bin"
mv "$KEEP" "$WORK/keep-moved.bin"
if [ -L "$MNT/dangling.bin" ] && [ ! -e "$MNT/dangling.bin" ]; then
	echo "    ✓ 链接仍存在（Lstat 可见）但目标不可达（Stat 失败）= 悬空"
else
	fail "链接未按预期悬空：-L=$([ -L "$MNT/dangling.bin" ] && echo y || echo n) -e=$([ -e "$MNT/dangling.bin" ] && echo y || echo n)"
fi

# ---- ⑥ 回撤语义：删链接 + 还原备份 → 独立文件 ----
say "⑥ 回撤语义（删链接 + 还原备份）"
# 模拟 SymlinkMerge 的终态：dup 位置是链接，备份是 .fdd-old
RESTORE="$MNT/restore.bin"
cp "$WORK/keep-moved.bin" "$RESTORE.fdd-old"   # 合并前备份（内容 = 原始数据）
rm -f "$MNT/dangling.bin"
ln -s "$WORK/keep-moved.bin" "$RESTORE"        # 合并结果：dup 位置是链接
[ -L "$RESTORE" ] || fail "前置状态错误：$RESTORE 不是链接"

# 执行与 undoSymlink 等价的三步
rm "$RESTORE"
mv "$RESTORE.fdd-old" "$RESTORE"
[ -f "$RESTORE" ] && [ ! -L "$RESTORE" ] || fail "回撤后应为独立普通文件"
[ "$(sha256sum "$RESTORE" | cut -d' ' -f1)" = "$KEEP_SUM" ] \
	|| fail "回撤后内容与原始数据不符"
echo "    ✓ 还原为独立文件且内容正确（$KEEP_SUM）"

# ---- ⑦ 悬空链接仍可回撤（备份是数据本体，与链接有效性无关）----
say "⑦ 悬空状态下回撤（应成功）"
D="$MNT/dangle-undo.bin"
cp "$WORK/keep-moved.bin" "$D.fdd-old"
ln -s "$WORK/does-not-exist.bin" "$D"      # 一开始就指向虚空
if [ ! -e "$D" ]; then
	rm "$D" && mv "$D.fdd-old" "$D"
	[ "$(sha256sum "$D" | cut -d' ' -f1)" = "$KEEP_SUM" ] \
		|| fail "悬空链接回撤后内容不符"
	echo "    ✓ 悬空链接照常回撤成功（数据在备份里，与链接是否有效无关）"
else
	fail "前置状态错误：链接未悬空"
fi

printf '\n\033[32mOK: 跨卷软链接的 7 条核心判据全部成立\033[0m\n'
echo "    注：本脚本验证的是文件系统语义与应用的算法前提；"
echo "        应用代码路径（SymlinkMerge/verifySymlinked/undoSymlink）由 go test 覆盖。"
