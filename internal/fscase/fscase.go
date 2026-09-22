// Package fscase 探测目录所在文件卷的大小写语义（2026-09-18 审查 I2）。
//
// 遍历器靠「路径折叠」判断两个路径是否指向同一棵目录。此前折叠按编译目标
// 硬编码（darwin/windows 一律折叠、linux 一律不折叠），两类真实场景都会判错：
//   - macOS 上用户自建的大小写敏感 APFS 卷：A 与 a 是两棵目录，折叠后其中一棵
//     整棵静默不被扫描（表现为"文件凭空消失"，且没有任何失败记录）。
//   - Linux 上启用 casefold 的 ext4/btrfs、或 CIFS/NFS 挂载的不敏感远端：
//     a 与 A 是同一棵，不折叠则同一目录被重复收集，重复组数与可释放空间虚高，
//     据此下发的清理会多删文件。
//
// 因此改为实测：在目标目录写入一个混合大小写的临时文件，再用另一种大小写
// Lstat 同一位置——能看到即卷不敏感。
//
// 探测不可用（只读卷、无写权限、目录尚不存在）时分两步收尾（M85，2026-09-22
// 裁定「按卷型分岔」）：先读卷型，名字本身就代表不敏感的卷（FAT/exFAT/NTFS）
// 按卷型确证；卷型也拿不到才退回平台默认，行为与改动前一致。
// ★ 上面那句"退回平台默认，行为与改动前一致"到 2026-09-22 之前一直是全部真相，
// 现在它只描述第三档 ⇒ 结论必须连同"这格有没有证据"一起给出，见 Result 与 Verdict。
package fscase

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"filededup/internal/media"
	"filededup/internal/pathnorm"
	"filededup/internal/worktemp"
)

// Fold 归一化路径用于「同一目录/前缀」比较：按所在卷语义折叠大小写，
// 并把**本平台**的分隔符统一为 "/"。
//
// 分隔符那一半走 pathnorm（M64 收归：本仓曾有四份反斜杠转斜杠的实现），
// 注入的是平台真值 string(filepath.Separator)，与 visitKey 同一口径。
//
// ★ M63（FC-2）已于 2026-09-22 裁定改判据并实施（设计稿 §27.3）：改前这里给字面
// "\"，理由是"输入里两种分隔符都可能已有，键空间必须只有一个形状"，代价是 unix 上
// "a\b" 这个合法文件名被当成结构，两个不同目录折成同键 ⇒ 一棵整棵静默不被扫描。
// 现在按平台取：Windows 仍折 "\"（用户手输 "/" 与遍历给的 "\" 混用那一格照旧覆盖），
// unix 上 "\" 还原为普通文件名字符。pathnorm 包注释本来就写明这条口径
// （"非 Windows 上 \\ 是合法文件名字符，不能当成结构"），此前只有 Fold 没跟上。
//
// 大小写折叠腿一字未动（按卷实测，本包自己的判据），仍与宿主平台无关。
func Fold(p string, sensitive bool) string {
	return fold(p, sensitive, string(filepath.Separator))
}

// fold 是 Fold 的平台注入版：sep 传平台分隔符真值，使 Windows 腿与 unix 腿
// 在任一主机上都能被断言（H6 手法，同 app.go 的 undoReasonCodeFor）。
func fold(p string, sensitive bool, sep string) string {
	q := pathnorm.Slash(p, sep)
	if sensitive {
		return q
	}
	return strings.ToLower(q)
}

// Default 平台默认语义（true = 区分大小写），也是探测失败时的兜底。
func Default() bool { return defaultSensitive }

// Result 是卷大小写语义的**三态**出口（M62+M85，2026-09-22 裁定「按卷型分岔」）。
//
// 光有 Sensitive 那一个 bool 会撒两个方向的谎：
//   - 只读卷上的 FAT/exFAT/NTFS：卷型本身就是"不区分大小写"的确证证据，但改前它和
//     "什么都没问到"给出同一个 bool（= Default() = 敏感），于是同一棵树的两种拼写各
//     走一遍，重复组数与可释放空间虚高 ⇒ 据此下发的清理会多删文件（M85 要修的正是这格）。
//   - 反过来，把"退默认"标成确证会让上层在完全没有读数的情况下放宽判据（M62 反对的
//     "无声放宽"）——M105 拿 Proven 作放宽的**前置条件**，所以这一位是承重的。
//
// 于是 Proven 的语义写成一句可核对的话：**"这一格要么有实测，要么有卷型，二者皆无即 false"**。
// 注意 Proven=false 时 Sensitive 一定等于 Default()，但反过来不成立：不敏感卷的 Default()
// 与"确证不敏感"可能同值（linux 上两态都返回 false），所以不许用 `Sensitive == Default()`
// 反推未确证。
type Result struct {
	Sensitive bool // 这一卷是否区分大小写
	Proven    bool // 该结论是否有证据（写探针实测 / 卷型直读）支撑；false = 退平台默认
}

// volumeTypeName 卷型名读取的注入缝（H6 惯例，同 scanner.probeCaseVerdict、
// ops.verifyFileFn）。真实现只在 darwin 有读数，另两平台恒 ""（media.probe_other.go）
// ⇒ 那两条腿永远走"退默认"，与 M62 之前一字不差。测试换它扮演 FAT/远端卷。
var volumeTypeName = media.FSTypeName

// insensitiveVolumeTypes 天生不区分大小写的卷型名（statfs 的 f_fstypename，小写口径
// 同 media/probe_darwin.go:39-47 那份网络表）。
//
// ★ 只列"名字本身就代表不敏感语义"的本地文件系统（FAT 家族与 NTFS）。远端一律不进：
// cifs/smbfs/nfs/afpfs 的服务端可以随便是什么卷，客户端名字不能替它决定（拿它判不敏感
// 会把两棵真不同的树折成一棵，包注释 :5-8 那类"文件凭空消失"）。ext4/xfs/btrfs/apfs
// 更明确：**能**区分大小写，判不敏感就是误折。
//
// ★ 表里刻意没有 linux 的 vfat/msdosfs：本批不做 linux 卷型腿（§28.6），加了就是
// 往没有读数的方向里填猜测。做那条腿时一并补，并给这一格取一个真实夹具。
var insensitiveVolumeTypes = map[string]bool{
	"msdos": true,
	"fat":   true,
	"exfat": true,
	"ntfs":  true,
}

// defaulted 是"没问到任何证据"的结论：退平台默认，且如实记为未确证。
func defaulted() Result { return Result{Sensitive: Default(), Proven: false} }

// fromVolumeType 探针不可用时先看卷型（M85）：天生不敏感 ⇒ 按卷型确证，否则退默认（M62）。
func fromVolumeType(dir string) Result {
	if insensitiveVolumeTypes[volumeTypeName(dir)] {
		return Result{Sensitive: false, Proven: true}
	}
	return defaulted()
}

// Verdict 报告 dir 所在卷的大小写语义，**并说清这格有没有证据**。
// 探测要写盘，故按目录缓存结论（缓存的是三态整体，不是 bool）；扫描根数量级为个位数，
// 缓存无需淘汰。dir 为空时连问都不问 ⇒ 必须是未确证，别让"没问卷"在计数链里隐身。
func Verdict(dir string) Result {
	if dir == "" {
		return defaulted()
	}
	key := filepath.Clean(dir)
	mu.Lock()
	if v, ok := cache[key]; ok {
		mu.Unlock()
		return v
	}
	mu.Unlock()
	v := probe(key)
	mu.Lock()
	cache[key] = v
	mu.Unlock()
	return v
}

// Sensitive 报告 dir 所在卷是否区分大小写。
//
// ★ M62+M85 之后它是 Verdict 的一层薄投影，**不是**另一份判定：二态调用方共四处
// （cmd/benchgen/main.go:316、ops/keep.go:177/:234/:243、:258 交出函数值本身）语义一字未变，
// 而需要"这格是不是实测"的上层改问 Verdict。留这个入口而不是把调用方全改成 Result，
// 是为了不把 5 个包卷进本轮改动面（约束 7）；它们要的确实只是 bool。
func Sensitive(dir string) bool { return Verdict(dir).Sensitive }

var (
	mu      sync.Mutex
	cache   = make(map[string]Result)
	probeNo atomic.Uint64 // 进程内递增，避免与上次运行的残留探测文件撞名
	hook    func(dir string)
)

// SetProbeHook 安装（传 nil 清除）一个「即将开始卷语义探测」的观察点。**仅供测试**。
//
// 为什么需要它（AS-R3，2026-09-20 全仓审计）：探测不是纯计算——它要往目标目录
// **写一个文件**再反向 Lstat。调用方若在持锁期间触发探测，一个死挂载（NFS/网络盘
// 拔走后 stat 会挂住）就会把该锁连同全部应用绑定一起卡死。
// "有没有在锁内做 I/O" 这种性质从返回值上完全观测不到，只能有个能插在 I/O 之前的钩子。
func SetProbeHook(h func(dir string)) {
	mu.Lock()
	hook = h
	mu.Unlock()
}

// fireProbeHook 在探测动手前回调（不持 mu：钩子里可能做任意观测）。
func fireProbeHook(dir string) {
	mu.Lock()
	h := hook
	mu.Unlock()
	if h != nil {
		h(dir)
	}
}

// probeAttempts 单次探测最多换几个名字。撞名不再让探测退化为平台默认（M13），
// 但也不能无限重试：目录里被人放了一串同名文件时如实退回默认，行为与改动前一致。
const probeAttempts = 8

// probeNames 第 n 号探测用到的文件名（小写=实际写入名，大写=反向核对名）。
//
// 序号必须是**纯数字**：worktemp 的忽略规则按「前缀 + 全数字」判定，
// 掺进 pid 或随机量就没法在不放宽成 Contains 的前提下认出来（放宽成 Contains
// 会连带忽略 `x.fdd-case-probe-notes.txt` 这类用户文件——M1 的漏扫形态）。
// 跨进程撞名靠"换号重试"解决，不靠名字唯一。
func probeNames(dir string, n uint64) (lower, upper string) {
	base := worktemp.MarkCaseProbe + strconv.FormatUint(n, 10)
	return filepath.Join(dir, strings.ToLower(base)), filepath.Join(dir, strings.ToUpper(base))
}

// probe 实际探测。任何一步不确定都先试卷型（fromVolumeType），拿不到卷型才退回默认值，
// 不猜测——这条承诺的完整表述见包注释与 Verdict。
//
// M13（2026-09-20 全仓审计）：修正前这里有一行"`Lstat(upper)` 已存在 → return
// Default()"，而 probeNo 每个进程都从 1 重启，上次被 kill 留下的残留因此与首轮
// 探测恒撞名 → 该目录**永久**拿不到实测结论，静默退回平台默认。
// 现在换号重试；并且用 os.SameFile 判定"是否同一个对象"来归因，
// 不再依赖"另一种写法事先必须不存在"这一脆弱前提。
//
// ★ M62+M85：三处"探针给不出结论"的出口（创建失败 / 重试用尽 / verdictFrom 读不动）
// 从"直接 return Default()"改成"先问卷型"。为什么收在 probe 里而不是让调用方补一步：
// Verdict 按目录缓存整份结论，退默认一旦被标成确证就永久洗不白（M126 记的正是
// "一次瞬时故障钉死整趟扫描"那一类）。
func probe(dir string) Result {
	fireProbeHook(dir)
	for i := 0; i < probeAttempts; i++ {
		lower, upper := probeNames(dir, probeNo.Add(1))
		f, err := os.OpenFile(lower, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			// 只读卷 / 无写权限 / 目录不存在 → 问卷型再退；
			// EEXIST（别的进程或残留占了这个名字）→ 换号再试。
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return fromVolumeType(dir)
		}
		_ = f.Close()
		v, proven, ok := verdictFrom(lower, upper)
		// 本次创建的文件一定由本次清掉：无论判出结论还是归因失败要换号，都不能留残片。
		if rmErr := os.Remove(lower); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "[fscase] 探测文件残留 %s: %v\n", lower, rmErr)
		}
		if ok {
			if proven {
				return Result{Sensitive: v, Proven: true}
			}
			return fromVolumeType(dir) // 读不动 = 无从判定，与上面三条同一顺序处理
		}
	}
	return fromVolumeType(dir)
}

// verdictFrom 用刚创建的 lower 与另一种大小写的 upper 作判据。
// ok=false 表示"upper 位置上另有其文件"，这次的结果不可归因，应换号重来。
//
// proven 是 M62+M85 新加的一位，它把 M126 用文字写下的判据变成机器可核对的形式：
// "确证"只在两种现场成立——看见同一个对象（不敏感）、或换一种写法确实看不见（敏感）。
//
// ★ M126（04 §6.11 FC-3，设计段 §23.10）：改成先分清 upper 的失败**是哪一种**。
// 原先 `case uerr != nil: return true, true` 把"看不见"和"读不动"当成同一件事，
// 于是 EIO / ESTALE / EPERM / ENOTDIR（死挂载、权限、路径形状）都会被报成
// "卷区分大小写"这一**确证结论**，再被 Verdict 的按目录缓存永久固化：
// 一次瞬时 I/O 故障就把整趟扫描的折叠语义钉死。包注释与 probe 的承诺都是
// "不确定就退回平台默认、不猜测"，这里是那两条承诺唯一的破口。
// 判错方向不是中性的：见包注释——折叠判错会让同一目录重复收集，
// "重复组数与可释放空间虚高，据此下发的清理会多删文件"。
//
// ★ 只删自己创建的 lower：upper 可能是上次运行的残留，也可能是用户放的同名文件，
// 我们无从分辨，因此一个字节都不碰（与 ops 侧 claimSlot 同一口径）。
func verdictFrom(lower, upper string) (v bool, proven bool, ok bool) {
	li, lerr := os.Lstat(lower)
	ui, uerr := os.Lstat(upper)
	switch {
	case uerr == nil:
		if lerr == nil && os.SameFile(li, ui) {
			return false, true, true // 命中同一个对象 → 卷不区分（确证）
		}
		return false, false, false // upper 是另一个文件：撞名，换号
	case errors.Is(uerr, os.ErrNotExist):
		return true, true, true // 换一种大小写确实看不见 → 卷区分大小写（确证）
	default:
		// 读不动 = 无从判定：值照旧给 Default()，但 proven=false 让 probe 去问卷型。
		// ok 仍为 true 以保持改前的控制流——这一格不是撞名，不该再白耗一轮换号。
		return Default(), false, true
	}
}
