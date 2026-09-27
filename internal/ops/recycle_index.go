package ops

// M279 的修法本体：从回收站实体目录里**读出进站证据**，把「原路径 → `$R…` 落点」
// 填进平台交回的 dst 表。
//
// 缺陷形状（登记原文）：`trash_windows.go` 的 `defaultTrashLocked` 从建表到交回，
// 一次都没往 `dst` 里写过东西 ⇒ `executor.go` 回退分支里那条 `known[p]` 命中路径
// 在 Windows 上**永不可达**，于是"源已消失 + 平台能验证却没报落点"稳定长成
// 一条要求用户「立即到回收站核实、考虑用数据恢复工具找回」的**假**事故警报
// （真机对账：20 条警报 20/20 都在回收站里，b5_rblist.txt）。
// 同一条根因还顺带造出 M278：成功项的「去向」也是空串。
//
// ★ 本文件刻意**无 build tag**：`$I` 怎么解、哪条证据算本轮进站、配不到时交回什么，
// 全都不含平台调用；按本仓库既定纪律（"平台判定下沉到无 tag 文件、平台真值由调用侧
// 注入"，见 recycle_policy.go 头部），这样 Linux/darwin 的 CI 腿也能断言它。
// 唯一的 Windows 真值——卷根——由调用侧经 `toWinRoot` 供给。
//
// ★ 安全边界（这条比修法本身重要）：**只有从回收站里真读出来的证据**才写进 dst。
// 一条都配不到时交回空表，执行器的 strict Failed 臂原样生效 ⇒ 本改动只会把
// "假警报"换成"有证据的确认"，绝不会把 2026-09-20 那道"静默永久删除"防线放软。

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// recycleBinDirName 是 Windows 回收站在每卷根下的实体目录名。
// sysguard 把它列为"整仓任何写路径都不得触碰"的目录（M279 只读不写）。
const recycleBinDirName = "$Recycle.Bin"

const (
	// $I 头部：version(8) + 文件大小(8) + 删除时间 FILETIME(8) + 路径长度 + 路径。
	// 本机（Windows 11 build 26100）实测是 version 2 布局，长度字段只有 4 字节：
	// 探针读 F:\$Recycle.Bin 的 6 个条目，`28 + 2*长度` 与文件实际长度**逐条相等**。
	recycleIdxV2PathOff = 28
	// 文档里的 version 1 布局同一字段是 8 字节、路径从 32 起（XP/2003 那一代）。
	// 当代读数用不上，但解析两种都认：判据若只在当代 Windows 上成立，换台老机器
	//就又回到"空 dst ⇒ 假警报"。
	recycleIdxV1PathOff = 28 + 4

	// winEpochDelta：FILETIME 起点 1601-01-01 到 Unix epoch 的 100ns 间隔。
	winEpochDelta = int64(116444736000000000)

	// recycleScanSlack 是"本轮窗口"的容差：mtime 预筛与头部删除时间都往前放这么多。
	// 真机读数里头部删除时间与条目 mtime 逐秒一致，容差挡的是时钟粒度与 FAT 的 2s 步进。
	recycleScanSlack = 2 * time.Minute

	// recycleIdxMaxRead：$I 只该有一两百字节（路径 + SID 尾巴）。给上限是因为
	// 我们读的是"名字长得像 $I"的任意文件，不能对未知内容放开读。
	recycleIdxMaxRead = 64 << 10
)

// winFiletime 把墙钟转成 FILETIME（100ns 间隔，起点 1601-01-01）。
func winFiletime(t time.Time) int64 { return t.UnixMicro()*10 + winEpochDelta }

// filetimeToTime 是 winFiletime 的反向。
func filetimeToTime(ft int64) time.Time {
	return time.UnixMicro((ft - winEpochDelta) / 10)
}

// parseRecycleIndex 解一份 `$I…` 元数据，交出**原路径**与**删除时间**（FILETIME）。
//
// ok=false 的每一种情形都必须按"没有证据"处理（fail-closed）：宁可让用户看到一条
// 需要核实的失败，也不能把猜出来的路径当成进站证据。
// 布局按"形状自洽"在 v2/v1 之间选，而不是照 version 字段硬切——长度字段读错位置会
// 得到 16 325 849 296 928 815 这种荒谬值（探针第一版正是拿 8 字节读长度，当场解不出）。
func parseRecycleIndex(raw []byte) (origPath string, delFiletime int64, ok bool) {
	if len(raw) < recycleIdxV2PathOff+2 {
		return "", 0, false
	}
	switch ver := int64(binary.LittleEndian.Uint64(raw[0:])); {
	case ver == 1, ver == 2:
	default:
		return "", 0, false // 未知布局：不猜
	}
	delFiletime = int64(binary.LittleEndian.Uint64(raw[16:]))

	if n := int(binary.LittleEndian.Uint32(raw[24:])); n >= 2 && n <= (len(raw)-recycleIdxV2PathOff)/2 {
		if s, good := decodeIndexPathUnits(raw[recycleIdxV2PathOff:], n); good {
			return s, delFiletime, true
		}
	}
	if len(raw) >= recycleIdxV1PathOff+2 {
		n64 := int64(binary.LittleEndian.Uint64(raw[24:]))
		if n64 >= 2 && n64 <= int64((len(raw)-recycleIdxV1PathOff)/2) {
			if s, good := decodeIndexPathUnits(raw[recycleIdxV1PathOff:], int(n64)); good {
				return s, delFiletime, true
			}
		}
	}
	return "", 0, false
}

// decodeIndexPathUnits 把 n 个 UTF-16 码元（含结尾 NUL）解成路径字符串。
func decodeIndexPathUnits(b []byte, n int) (string, bool) {
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	s := strings.TrimRight(string(utf16.Decode(units)), "\x00")
	if s == "" || strings.ContainsRune(s, 0) {
		return "", false
	}
	return s, true
}

// recyclePathKey 是配对用的路径规范化：Windows 路径大小写与 `/`\` 都不敏感，
// 而 `dst` 的键必须是**调用方交进来的那个原串**（执行器按它查表），故只用于比较。
func recyclePathKey(p string) string {
	s := strings.ReplaceAll(p, "/", `\`)
	s = strings.TrimRight(s, `\`)
	return strings.ToLower(s)
}

// recycleBinDirs 把卷根列表展开成该扫的实体目录：每卷 `$Recycle.Bin` 下的子目录
// （一个用户 SID 一个）。真机 ACL 读数：盘根给 `BUILTIN\Users` 的是
// `ReadAndExecute`（列得动 SID 目录名），每个 SID 目录只给 SYSTEM /
// Administrators / **该用户本人**——所以别的用户的目录必然读不开，跳过即可，
// 本用户删的东西一定在本人 SID 目录下。
func recycleBinDirs(roots []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		bin := filepath.Join(root, recycleBinDirName)
		if seen[bin] {
			continue
		}
		seen[bin] = true
		hits, err := os.ReadDir(bin)
		if err != nil {
			continue // 无回收站的卷（光驱 / 网络 shares / 策略禁用）
		}
		for _, e := range hits {
			if e.IsDir() {
				out = append(out, filepath.Join(bin, e.Name()))
			}
		}
	}
	return out
}

// collectRecycledDestinations 在给定卷根的回收站里，为 paths 找出**本轮**的进站证据。
//
// 三重筛子，缺一不算证据：
//  1. 条目名的路径长度自洽且解得动（parseRecycleIndex）；
//  2. 头部删除时间 ≥ notBefore − recycleScanSlack（同一路径的上一轮旧条目不得顶替本轮，
//     否则"这一轮被静默删除、上一轮确实进过站"会被记成成功）；
//  3. 解出的原路径落在候选集合里（用户自己在资源管理器删的东西与本次无关）。
//
// 同一路径命中多条时取**最新**那条。
func collectRecycledDestinations(roots []string, paths []string, notBefore time.Time) map[string]string {
	out := map[string]string{}
	want := make(map[string]string, len(paths))
	for _, p := range paths {
		if p != "" {
			want[recyclePathKey(p)] = p
		}
	}
	if len(want) == 0 {
		return out
	}
	floor := notBefore.Add(-recycleScanSlack)
	minFiletime := winFiletime(floor)
	best := map[string]int64{}

	for _, dir := range recycleBinDirs(roots) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 别的用户的 SID 目录：ACL 拒绝，不是失败
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "$I") || len(name) < 3 {
				continue
			}
			// 便宜的预筛：mtime 与头部删除时间逐秒一致（真机两条读数都验过），
			// 用它挡掉历史条目的**读文件**成本；权威判据仍是下面头部里的 FILETIME。
			if info, err := e.Info(); err == nil && info.ModTime().Before(floor) {
				continue
			}
			raw, err := readRecycleIndexFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			orig, ft, ok := parseRecycleIndex(raw)
			if !ok || ft < minFiletime {
				continue
			}
			key := recyclePathKey(orig)
			cand, hit := want[key]
			if !hit {
				continue
			}
			if prev, have := best[key]; have && prev >= ft {
				continue
			}
			best[key] = ft
			// 实体数据在同目录的 `$R…` 那一侧（`$I`/`$R` 只差前缀那一枚字母）。
			out[cand] = filepath.Join(dir, "$R"+name[2:])
		}
	}
	return out
}

// readRecycleIndexFile 带上限地读一份 $I 元数据。
func readRecycleIndexFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, recycleIdxMaxRead))
}

// fillRecycledDst 把证据**就地**合并进 dst（调用方的 defer 依赖"就地"这一点：
// 换引用等于什么都没写进去）。已有的键不覆盖——若平台自己报过落点，以它为准。
func fillRecycledDst(dst map[string]string, roots []string, paths []string, notBefore time.Time) {
	if dst == nil {
		return
	}
	for src, d := range collectRecycledDestinations(roots, paths, notBefore) {
		if _, has := dst[src]; !has {
			dst[src] = d
		}
	}
}
