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
// Lstat 同一位置——能看到即卷不敏感。探测不可用（只读卷、无写权限、目录尚不
// 存在）时退回平台默认，行为与改动前一致。
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

	"filededup/internal/pathnorm"
	"filededup/internal/worktemp"
)

// Fold 归一化路径用于「同一目录/前缀」比较：按所在卷语义折叠大小写，
// 并统一为 "/" 分隔（Windows 原生 "\" 与用户手写的 "/" 混用会漏匹配）。
//
// 分隔符那一半走 pathnorm（M64 收归：本仓曾有四份反斜杠转斜杠的实现）。这里给字面
// "\" 而不是平台真值，是因为本函数的输入里两种分隔符都可能已有（用户手输 "/"、
// 遍历给 "\"），键空间必须只有一个形状。
// ★ 由此产生的已知偏差登记在 M63（FC-2）：unix 上 "a\b" 是合法文件名，这里仍按
//
//	分隔符处理。本轮不改判据，只保证"要改只需改一处"。
//
// 大小写折叠是本包自己的判据（按卷实测），不属于路径归一，保持就地。
func Fold(p string, sensitive bool) string {
	q := pathnorm.Slash(p, "\\")
	if sensitive {
		return q
	}
	return strings.ToLower(q)
}

// Default 平台默认语义（true = 区分大小写），也是探测失败时的兜底。
func Default() bool { return defaultSensitive }

// Sensitive 报告 dir 所在卷是否区分大小写。
// 探测要写盘，故按目录缓存结论；扫描根数量级为个位数，缓存无需淘汰。
func Sensitive(dir string) bool {
	if dir == "" {
		return Default()
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

var (
	mu      sync.Mutex
	cache   = make(map[string]bool)
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

// probe 实际探测。任何一步不确定都退回默认值，不猜测。
//
// M13（2026-09-20 全仓审计）：修正前这里有一行"`Lstat(upper)` 已存在 → return
// Default()"，而 probeNo 每个进程都从 1 重启，上次被 kill 留下的残留因此与首轮
// 探测恒撞名 → 该目录**永久**拿不到实测结论，静默退回平台默认。
// 现在换号重试；并且用 os.SameFile 判定"是否同一个对象"来归因，
// 不再依赖"另一种写法事先必须不存在"这一脆弱前提。
func probe(dir string) bool {
	fireProbeHook(dir)
	for i := 0; i < probeAttempts; i++ {
		lower, upper := probeNames(dir, probeNo.Add(1))
		f, err := os.OpenFile(lower, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			// 只读卷 / 无写权限 / 目录不存在 → 退回默认；
			// EEXIST（别的进程或残留占了这个名字）→ 换号再试。
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return Default()
		}
		_ = f.Close()
		v, ok := verdictFrom(lower, upper)
		// 本次创建的文件一定由本次清掉：无论判出结论还是归因失败要换号，都不能留残片。
		if rmErr := os.Remove(lower); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "[fscase] 探测文件残留 %s: %v\n", lower, rmErr)
		}
		if ok {
			return v
		}
	}
	return Default()
}

// verdictFrom 用刚创建的 lower 与另一种大小写的 upper 作判据。
// ok=false 表示"upper 位置上另有其文件"，这次的结果不可归因，应换号重来。
//
// ★ 只删自己创建的 lower：upper 可能是上次运行的残留，也可能是用户放的同名文件，
// 我们无从分辨，因此一个字节都不碰（与 ops 侧 claimSlot 同一口径）。
func verdictFrom(lower, upper string) (v bool, ok bool) {
	li, lerr := os.Lstat(lower)
	ui, uerr := os.Lstat(upper)
	switch {
	case uerr != nil:
		return true, true // 换一种大小写看不见 → 卷区分大小写
	case lerr == nil && os.SameFile(li, ui):
		return false, true // 命中同一个对象 → 卷不区分
	default:
		return false, false // upper 是另一个文件：撞名，换号
	}
}
