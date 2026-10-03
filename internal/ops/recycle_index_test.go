package ops

// M279：Windows「部分成功的回收站批次」必然误报数据丢失——判据的钉子。
//
// 登记的原话（docs/04 M279 行）把因裁定得很死：`defaultTrashLocked` 从 :245 建
// `dst := map[string]string{}` 到 :287 交回，**一次都没往里写过东西**，而
// `executor.go:643` 的回退分支靠 `known[p]` 命中才把"源已消失"判成"已进站"⇒
// 那条命中路径在 Windows 上**永不可达**，于是 2026-09-20 为堵静默永久删除而刻意
// 升级的 strict Failed 在真机上稳定长成 20 条**假**事故警报
// （`$I` 逐条对账 20/20 都在回收站里，见 b5_rblist.txt）。
//
// ★ 本文件刻意写成**无 tag**：修法的判据（`$I` 字节怎么解、哪条证据算本轮进站、
//   配对不到时必须交回什么）全都不含平台调用，按本仓库既定的
//   "平台判定下沉到无 tag 文件、平台真值由调用侧注入"（见 recycle_policy.go 头部），
//   于是 Linux/darwin 的 CI 腿也能断言它——而 M279 恰恰是"只有 Windows 真机看得见"
//   那一类，判据能跨平台断言就不该只在真机上撞。
//
// 真机字节读数（本轮一次性探针 `probe_rb_acl.ps1` / `probe_i_hex.ps1`，
// 输出留存 build/m7scratch/b9/probe_rb_acl_hex.txt，跑完即删）：
// 〔2026-10-03 追记〕那个留存目录**从未入库**，本机也已按用户裁定清理 ⇒ 指针不可复取；
// 下面那两行 HEX 是抄进注释的字面读数，判据以**本注释自身**为准，不必去找原件。
//
//	$I3MUGNX.exe len=122  HEX 02 00…(v=2) | 41 e2 11 0c…(size) | 40 74 78 04 61 13 db 01(ft) | 2f 00 00 00(len=47) | 46 00 3a 00 5c 00 …
//	$IIKWGXL.4   len=62   28 + 17*2 = 62  ← 整除文件长度，逐条验证过 6 个条目
//
// ⇒ 本机（Windows 11 build 26100）写的是 **version 2 布局**：
//
//	[0:8)   int64  version（读数 2）
//	[8:16)  int64  文件字节数
//	[16:24) int64  删除时间 FILETIME（★ 与同条目 mtime 逐秒一致，两条读数都验过）
//	[24:28) int32  原路径 UTF-16 码元数（**含**结尾 NUL）
//	[28…)   UTF-16LE 原路径
//
// 注意偏移 24 处**不是** 8 字节长度（那是文档里常见的 version 1 布局）：把
// int64 当长度读会得到 16 325 849 296 928 815 这种荒谬值——探针第一次就是这么错的。
// 因此解析按"形状自洽"选布局，并**两种布局都钉住**（下面的用例各占一格）。

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// winEpoch 是 FILETIME 的起点（1601-01-01）到 Unix epoch 的 100ns 间隔。
const winEpoch = int64(116444736000000000)

// writeFakeIndexBytes 按 version 2 布局摆一份 $I 内容（真机同形状）。
func writeFakeIndexBytes(t *testing.T, ver int, origPath string, delAt time.Time) []byte {
	t.Helper()
	units := append(utf16.Encode([]rune(origPath)), 0) // 含结尾 NUL，与真机读数一致
	raw := make([]byte, 28+2*len(units))
	binary.LittleEndian.PutUint64(raw[0:], uint64(ver))
	binary.LittleEndian.PutUint64(raw[8:], uint64(len(origPath)*7))
	binary.LittleEndian.PutUint64(raw[16:], uint64(delAt.UnixMicro()*10+winEpoch))
	binary.LittleEndian.PutUint32(raw[24:], uint32(len(units)))
	for i, u := range units {
		binary.LittleEndian.PutUint16(raw[28+2*i:], u)
	}
	return raw
}

// putIndexFile 在 binDir 下摆一对 `$I<id><ext>` / `$R<id><ext>`，返回两侧路径。
func putIndexFile(t *testing.T, binDir, id, origPath string, delAt time.Time) (string, string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 真机条目名 = `$I` + 6 位随机 + **原文件扩展名**（`$I3MUGNX.exe`）。
	iPath := filepath.Join(binDir, "$I"+id+filepath.Ext(origPath))
	rPath := filepath.Join(binDir, "$R"+id+filepath.Ext(origPath))
	if err := os.WriteFile(iPath, writeFakeIndexBytes(t, 2, origPath, delAt), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rPath, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return iPath, rPath
}

func TestParseRecycleIndexReadsRealMachineV2Layout(t *testing.T) {
	delAt := time.Date(2026, 9, 27, 10, 30, 0, 0, time.Local)
	raw := writeFakeIndexBytes(t, 2, `F:\dup\g100_2.dat`, delAt)

	orig, ft, ok := parseRecycleIndex(raw)
	if !ok {
		t.Fatalf("真机 v2 形状的条目解不出来 ⇒ 修后 Windows 仍会交回空 dst，假警报原样回来")
	}
	if orig != `F:\dup\g100_2.dat` {
		t.Fatalf("原路径 = %q，期望 F:\\dup\\g100_2.dat", orig)
	}
	if got := filetimeToTime(ft); !got.Equal(delAt) {
		t.Fatalf("删除时间 = %v，期望 %v（判据 2 靠它划本轮窗口）", got, delAt)
	}

	// 探针现场那条**完整 68 字节**的真机条目（F:\$Recycle.Bin\S-1-5-21-…-500\$IINJV9X.exe，
	// 原路径 F:\setup_v2.1.3.exe，删除时间 FILETIME 0x01DC7361BC7B4750）逐字节摆在这里：
	// 自己造的夹具可能把错误假设摆得很自洽，真机字节不会。
	// ★ 断言只钉路径与 FILETIME 整数——墙钟串随本地时区变，写进断言就成了一台机器一个数。
	probe := []byte{
		0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xfd, 0x8c, 0x88, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x50, 0x47, 0x7b, 0xbc, 0x61, 0x73, 0xdc, 0x01,
		0x14, 0x00, 0x00, 0x00,
		0x46, 0x00, 0x3a, 0x00, 0x5c, 0x00, 0x73, 0x00,
		0x65, 0x00, 0x74, 0x00, 0x75, 0x00, 0x70, 0x00,
		0x5f, 0x00, 0x76, 0x00, 0x32, 0x00, 0x2e, 0x00,
		0x31, 0x00, 0x2e, 0x00, 0x33, 0x00, 0x2e, 0x00,
		0x65, 0x00, 0x78, 0x00, 0x65, 0x00, 0x00, 0x00,
	}
	p, pft, pok := parseRecycleIndex(probe)
	if !pok || p != `F:\setup_v2.1.3.exe` {
		t.Fatalf("探针真机字节解成 %q(ok=%v)，期望原样交回路径", p, pok)
	}
	if winFiletime(filetimeToTime(pft)) != pft {
		t.Fatalf("FILETIME 往返不自洽：%d → %v → %d", pft, filetimeToTime(pft), winFiletime(filetimeToTime(pft)))
	}
	if got := filetimeToTime(pft); got.IsZero() {
		t.Fatalf("真机条目的删除时间解成零值：%v", got)
	}
}

// 偏移 24 处按 8 字节读长度是**文档里的 version 1 布局**（XP/2003 那一代）。
// 本机读数是 v2，但解析必须两种都认：判据若只在当代 Windows 上成立，
// 换一台老机器就又回到"空 dst ⇒ 假警报"。
func TestParseRecycleIndexAlsoAcceptsDocumentedV1Layout(t *testing.T) {
	delAt := time.Date(2026, 9, 27, 11, 0, 0, 0, time.Local)
	units := append(utf16.Encode([]rune(`E:\a.bin`)), 0)
	raw := make([]byte, 32+2*len(units))
	binary.LittleEndian.PutUint64(raw[0:], 1)
	binary.LittleEndian.PutUint64(raw[8:], 4096)
	binary.LittleEndian.PutUint64(raw[16:], uint64(delAt.UnixMicro()*10+winEpoch))
	binary.LittleEndian.PutUint64(raw[24:], uint64(len(units))) // v1：8 字节长度
	for i, u := range units {
		binary.LittleEndian.PutUint16(raw[32+2*i:], u)
	}
	orig, ft, ok := parseRecycleIndex(raw)
	if !ok || orig != `E:\a.bin` {
		t.Fatalf("v1 布局解不出（orig=%q ok=%v）⇒ 解析把当代读数当成了唯一形状", orig, ok)
	}
	if got := filetimeToTime(ft); !got.Equal(delAt) {
		t.Fatalf("v1 删除时间 = %v，期望 %v", got, delAt)
	}
}

// 解不出来时必须**明确失败**（ok=false），而不是猜一条路径出来：
// 猜出来的原路径若恰好等于某个候选，就是把假成功塞进真失败。
func TestParseRecycleIndexRejectsMalformedWithoutPanic(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"空文件", []byte{}},
		{"不足 v2 头", []byte("012345678901234567")},
		{"长度字段为 0", func() []byte {
			b := writeFakeIndexBytes(t, 2, `F:\a.bin`, time.Now())
			binary.LittleEndian.PutUint32(b[24:], 0)
			return b
		}()},
		{"长度超出文件实际长度", func() []byte {
			b := writeFakeIndexBytes(t, 2, `F:\a.bin`, time.Now())
			binary.LittleEndian.PutUint32(b[24:], 9999)
			return b
		}()},
		{"version=3（未知布局，按 fail-closed 拒绝）", func() []byte {
			b := writeFakeIndexBytes(t, 2, `F:\a.bin`, time.Now())
			binary.LittleEndian.PutUint64(b[0:], 3)
			return b
		}()},
		{"全零（把 int64 当长度读会算出荒谬值，探针第一版正栽在这）", make([]byte, 64)},
		{"随机噪声", []byte("\x02\x00\x00\x00\x00\x00\x00\x00\xff\xfe\xff\xfe\xff\xfe\xff\xfe\xff\xfe\xff\xfe\xff\xfe\xff\xfe\x7f\x7f\x00\x00\x00\x00")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic = %v ⇒ 解析必须对任意字节安全（读的是别的进程写的文件）", r)
				}
			}()
			if orig, _, ok := parseRecycleIndex(c.raw); ok {
				t.Fatalf("畸形条目被解成 %q ⇒ 会把猜出来的路径当证据", orig)
			}
		})
	}
}

func TestRecyclePathKeyIgnoresCaseSlashAndTrailingSep(t *testing.T) {
	// 只列 Windows 语义下**同一个文件**的写法：盘符大小写、正/反斜杠、结尾分隔符。
	// 不列 `f:\\dup\\a.bin` 那种双分隔符——它是另一串字面量，归一化不该顺手"猜"掉。
	want := recyclePathKey(`F:\dup\a.bin`)
	for _, p := range []string{`F:\dup\a.bin`, `f:/dup/A.BIN`, `F:\dup\a.bin\`, `f:\DUP\a.Bin`} {
		if got := recyclePathKey(p); got != want {
			t.Fatalf("%q 的键 = %q，与 %q 不等价（Windows 路径大小写/斜杠不敏感）", p, got, `F:\dup\a.bin`)
		}
	}
	if recyclePathKey(`F:\dup\a.bin`) == recyclePathKey(`F:\dup\a.binx`) {
		t.Fatal("不同文件不得共用一个键")
	}
}

// ★ 判据本体：给一批候选路径与卷根，从回收站实体目录里配对出"本轮确实进站"的证据。
func TestCollectRecycledDestinationsPairsOnlyThisRoundsEntries(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, recycleBinDirName, "S-1-5-21-100")
	now := time.Now()

	// 夹具里的路径一律用 filepath.Join 逐段拼：把 `dup\a.bin` 整串塞进 Join 只在
	// Windows 上等价于两段，在 Linux 上是一个含反斜杠的文件名——同一份断言两台机器两个数。
	aPath := filepath.Join(root, "dup", "a.bin")
	_, dstA := putIndexFile(t, binDir, "AAA111", aPath, now)
	// (2) 同一路径的**上一轮**旧条目（更早的删除时间）⇒ 不得算作本轮证据
	putIndexFile(t, binDir, "OLD111", aPath, now.Add(-24*time.Hour))
	// (3) 本轮进站但不在候选名单里（用户自己在资源管理器删的）⇒ 忽略
	putIndexFile(t, binDir, "OTHER1", filepath.Join(root, "elsewhere", "x.bin"), now)
	// (4) 本轮、候选、但同路径有更新的一条 ⇒ 取最新
	_, dstA2 := putIndexFile(t, binDir, "NEW222", aPath, now.Add(time.Second))

	cand := aPath
	got := collectRecycledDestinations([]string{root + string(filepath.Separator)}, []string{cand}, now)
	if len(got) != 1 {
		t.Fatalf("配对结果 = %v，期望恰好一条（旧条目与他人的条目都不算证据）", got)
	}
	if got[cand] != dstA2 {
		t.Fatalf("落点 = %q，期望最新那条 %q", got[cand], dstA2)
	}
	if got[cand] == dstA {
		t.Fatal("同路径多条时取了最早那条 ⇒ 划账会把上一轮的旧条目当成本轮证据")
	}
	if !strings.HasPrefix(filepath.Base(got[cand]), "$R") {
		t.Fatalf("落点应是 `$R…` 实体文件，got %q", got[cand])
	}
	if _, err := os.Stat(got[cand]); err != nil {
		t.Fatalf("交回的落点读不到：%v ⇒ 记录页「去向」会指向一个不存在的路径", err)
	}
}

// ★ fail-closed：一点证据都没有时必须交回**空表**，而不是"源不在了就算成功"。
// 这条性质决定了改动的安全边界——判据只会把"假警报"换成"有证据的确认"，
// 绝不会把"真·静默永久删除"顺手放过（executor.go 的 strict Failed 臂原样保留）。
func TestCollectRecycledDestinationsIsFailClosed(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "dup", "gone.bin") // 回收站里没有任何条目
	if got := collectRecycledDestinations([]string{root + string(filepath.Separator)}, []string{missing}, time.Now()); len(got) != 0 {
		t.Fatalf("无证据却配对了 %v ⇒ 静默永久删除会被报成成功", got)
	}

	// 卷根解析不出来（相对路径 / UNC）⇒ 不入扫描面，同样交回空表而不是崩。
	if got := collectRecycledDestinations(nil, []string{`relative\a.bin`}, time.Now()); len(got) != 0 {
		t.Fatalf("无卷根却配对了 %v", got)
	}
}

// 别的用户的 SID 目录读不开（真机 ACL：每个 SID 目录只给 SYSTEM / Administrators /
// 该用户本人），必须**跳过而不是整次失败**——否则修法在最常见的情形下不成立。
func TestCollectRecycledDestinationsToleratesUnreadableDirs(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	okPath := filepath.Join(root, `dup`, "ok.bin")
	_, dst := putIndexFile(t, filepath.Join(root, recycleBinDirName, "S-1-5-21-mine"), "OK1111", okPath, now)
	// 一个名字像 SID 目录、实际是**文件**的位置：ReadDir 必失败。
	blocker := filepath.Join(root, recycleBinDirName, "S-1-5-21-blocked")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 一个连 $Recycle.Bin 都没有的卷根（例如未挂载 / 光驱）。
	emptyRoot := t.TempDir()

	got := collectRecycledDestinations(
		[]string{root + string(filepath.Separator), emptyRoot + string(filepath.Separator)},
		[]string{okPath}, now)
	if len(got) != 1 || got[okPath] != dst {
		t.Fatalf("读不开的目录把整次扫描带走了：%v", got)
	}
}

// 扫描面 = 每个受影响卷根的 `$Recycle.Bin` 下的子目录（真机读数：盘根下并列多个 SID 目录，
// 只有本用户那个可读；根目录本身给 BUILTIN\Users 的是 ReadAndExecute）。
func TestRecycleBinDirsExpandsEachRootIntoSidSubdirs(t *testing.T) {
	root := t.TempDir()
	for _, sid := range []string{"S-1-5-18", "S-1-5-21-a", "S-1-5-21-b"} {
		if err := os.MkdirAll(filepath.Join(root, recycleBinDirName, sid), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, recycleBinDirName, "desktop.ini"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 第二个卷根刻意用"没有 $Recycle.Bin 的空目录"而不是 `F:\`：
	// 真机 F 盘**有**回收站（7 个 SID 目录），拿它当夹具会让本条在 Windows 上
	// 读出 3+N 个目录——夹具必须只含自己摆的东西，否则同一份断言两台机器两个数。
	empty := t.TempDir()
	sep := string(filepath.Separator)
	got := recycleBinDirs([]string{root + sep, empty + sep, root + sep})
	want := map[string]bool{
		filepath.Join(root, recycleBinDirName, "S-1-5-18"):   true,
		filepath.Join(root, recycleBinDirName, "S-1-5-21-a"): true,
		filepath.Join(root, recycleBinDirName, "S-1-5-21-b"): true,
	}
	if len(got) != len(want) {
		t.Fatalf("扫描面 = %v，期望恰好每个 SID 子目录一次（卷根去重后共 2 个卷）", got)
	}
	for _, d := range got {
		if !want[d] {
			t.Fatalf("扫描面里出现了 %q（文件条目不得算目录）", d)
		}
	}
}

// dst 是**就地合并**的：defaultTrashLocked 的每条出口都交回同一个 map，
// 换掉引用会让 defer 的合并落进一个已经返回给调用方的旧表之外。
func TestFillRecycledDstMergesIntoGivenMap(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	cand := filepath.Join(root, "dup", "m.bin")
	_, dst := putIndexFile(t, filepath.Join(root, recycleBinDirName, "S-1-5-21-mine"), "MERGE111", cand, now)

	table := map[string]string{"先已有.key": "先已有值"}
	before := table
	// 〔2026-09-27 M270〕证据改由调用方**一次枚举**后交进来（`scanRecycleBin`），
	// 本函数的职责收窄成"就地合并"。原签名 `fillRecycledDst(table, roots, cand, now)`
	// 自己扫一趟，会和复核那趟给出互相矛盾的读数。本条断言的"就地、不覆盖已有键"不变。
	found, _ := scanRecycleBin([]string{root + string(filepath.Separator)}, []string{cand}, now)
	fillRecycledDst(table, found)
	if len(table) != 2 || table[cand] != dst {
		t.Fatalf("合并结果 = %v，期望保留原有键并补上进站落点", table)
	}
	if len(before) != 2 {
		t.Fatal("before 与 table 不是同一个表（fillRecycledDst 换了引用而不是就地写）")
	}
	// ★ 已有键优先：平台自己报过落点时，扫描不得把它改成回收站里的形状。
	taken := map[string]string{cand: "平台自报的落点"}
	fillRecycledDst(taken, found)
	if taken[cand] != "平台自报的落点" {
		t.Fatalf("已有键被扫描结果覆盖了：%v", taken[cand])
	}
	// ★ nil dst 不得 panic（defer 里 dst 也许还没建）。
	fillRecycledDst(nil, found)
}

// ★ 接线锚（无 tag 也能读源码：文件就在仓库里，Linux CI 同样扫得到）：
// `defaultTrashLocked` 的 dst 必须由**一条覆盖全部出口的归拢**填，
// 而不是散在几个 return 分支里——M279 的形状正是"建了表、所有出口都原样交回空表"，
// 而逐分支补写迟早漏一条（新增 early-return 时症状是"偶发假警报回来"）。
func TestDefaultTrashLockedFillsDstOnEveryExit(t *testing.T) {
	raw, err := os.ReadFile("trash_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "func defaultTrashLocked(")
	if i < 0 {
		t.Fatal("找不到 defaultTrashLocked ⇒ 锚的扫描面对不上，先重读结构")
	}
	body := src[i:]
	if j := strings.Index(body, "\n// recyclableReason"); j > 0 {
		body = body[:j] // 截到下一个函数的注释头为止，别把整份文件当函数体
	}
	if n := strings.Count(body, "fillRecycledDst("); n != 1 {
		t.Fatalf("defaultTrashLocked 里 fillRecycledDst 出现 %d 次，期望恰好 1 次（defer 一处归拢）", n)
	}
	if !strings.Contains(body, "defer func()") {
		t.Fatal("fillRecycledDst 没有挂在 defer 上 ⇒ 新增 early-return 会静默绕开它（M279 的原形状）")
	}
	// 建表那一行不得再是"建完就不管"的孤儿。
	if strings.Contains(body, "dst := map[string]string{}") && !strings.Contains(body, "fillRecycledDst(") {
		t.Fatal("dst 又变回从未写入的空表（M279 的登记原话）")
	}
	// 修法现场必须留下 ID：这条 defer 删掉就是"假警报回来"，而症状不会指向它。
	if !strings.Contains(body, "M279") {
		t.Fatal("defaultTrashLocked 里没有了 M279 的锚 ⇒ 下一位读者无从知道这条 defer 为什么不能删")
	}
}
