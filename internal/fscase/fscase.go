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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Fold 归一化路径用于「同一目录/前缀」比较：按所在卷语义折叠大小写，
// 并统一为 "/" 分隔（Windows 原生 "\" 与用户手写的 "/" 混用会漏匹配）。
func Fold(p string, sensitive bool) string {
	if !strings.Contains(p, "\\") {
		if sensitive {
			return p
		}
		return strings.ToLower(p)
	}
	q := strings.ReplaceAll(p, "\\", "/")
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
)

// probe 实际探测。任何一步不确定都退回默认值，不猜测。
func probe(dir string) bool {
	base := fmt.Sprintf(".fdd-case-probe-%d", probeNo.Add(1))
	lower := filepath.Join(dir, strings.ToLower(base))
	upper := filepath.Join(dir, strings.ToUpper(base))
	// 先确认另一种大小写形式当前不存在，否则"存在"就不能归因于本次写入
	if _, err := os.Lstat(upper); err == nil {
		return Default()
	}
	f, err := os.OpenFile(lower, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Default() // 只读卷 / 无写权限 / 目录不存在
	}
	_ = f.Close()
	defer func() {
		if err := os.Remove(lower); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "[fscase] 探测文件残留 %s: %v\n", lower, err)
		}
	}()
	if _, err := os.Lstat(upper); err == nil {
		return false // 换一种大小写仍命中同一文件 → 卷不敏感
	}
	return true
}
