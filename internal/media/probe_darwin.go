//go:build darwin

package media

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// probeInput 一次探测所需的 statfs 输出。
type probeInput struct {
	fsType string // f_fstypename，已小写，如 "apfs" / "smbfs"
	mount  string // f_mntonname，卷的挂载点，如 "/System/Volumes/Data"
	device string // f_mntfromname，如 "/dev/disk3s5"
}

// Probe 探测路径所在卷的介质类别。darwin 上按开销从低到高分三步：
//  1. 网络文件系统 → Network（读 statfs 字段，约 0.7µs，免费）；
//  2. 与启动卷同物理盘的卷宗 → SSD（免费）。macOS 内置盘恒为固态，
//     且 /（disk3s1s1）与 /System/Volumes/Data（disk3s5）虽为不同卷宗
//     却同属 disk3，按物理盘判定才能覆盖用户的真实扫描目标；
//  3. 其余卷（外置盘、其他卷宗）→ 对**挂载点**调 diskutil 问 SolidState，
//     单次约 90ms，结果按挂载点缓存（同一卷多个根只问一次）。
//
// 必须用挂载点而非原始路径：diskutil 对 /var/folders/... 这类合成路径
// 会返回 "Could not find disk"（且退出码仍为 0），只有卷挂载点能稳定解析。
//
// 任意一步失败都返回 Unknown，调用方据此维持默认并发度。
func Probe(path string) Class {
	return statfsInfo(path).classify(isInternalDevice, probeRotational)
}

// networkFSTypes 网络（含非本地随机访问）文件系统类型名，取自 statfs 的
// f_fstypename（小写）。cddafs 为光盘/映像，同样按低并发处理。
var networkFSTypes = map[string]bool{
	"smbfs":  true,
	"nfs":    true,
	"afpfs":  true,
	"webdav": true,
	"ftp":    true,
	"cifs":   true,
	"cddafs": true,
}

// classify 判定逻辑本体（纯函数，便于注入 isInternal 与 rot 做测试）。
// 字段缺失一律返回 Unknown，绝不猜测。
func (in probeInput) classify(isInternal func(device string) bool, rot func(mount string) Class) Class {
	if in.fsType == "" {
		return Unknown
	}
	if networkFSTypes[in.fsType] {
		return Network
	}
	if in.mount == "" || in.device == "" {
		return Unknown
	}
	if isInternal != nil && isInternal(in.device) {
		return SSD // 内置盘：免子进程快路径
	}
	if rot == nil {
		return Unknown
	}
	return rot(in.mount)
}

// statfsInfo 一次 statfs 取齐类型名 / 挂载点 / 设备名。
// 失败时返回零值（fsType 为空），classify 会据此返回 Unknown。
func statfsInfo(path string) probeInput {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return probeInput{}
	}
	return probeInput{
		fsType: strings.ToLower(cstr(st.Fstypename[:])),
		mount:  cstr(st.Mntonname[:]),
		device: cstr(st.Mntfromname[:]),
	}
}

// cstr 定长 C 字符串数组 → Go 字符串（NUL 截断）。
// 泛型签名以兼容 darwin 上 int8 与部分版本 uint8 两种元素类型。
func cstr[T int8 | uint8](b []T) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}

var (
	bootDiskOnce sync.Once
	bootDiskVal  string
)

// bootDiskKey 启动卷所在物理盘标识（如 "disk3"），进程内只计算一次。
func bootDiskKey() string {
	bootDiskOnce.Do(func() {
		bootDiskVal = diskKey(statfsInfo("/").device)
	})
	return bootDiskVal
}

// isInternalDevice 该设备是否与启动卷同属一个物理盘。
// 启动盘标识为空（statfs 失败）时一律返回 false → 走 diskutil 或 Unknown，
// 不会把外置盘误判成内置固态。
func isInternalDevice(device string) bool {
	k := bootDiskKey()
	return k != "" && diskKey(device) == k
}

// diskKey 从设备名提取物理盘标识：
//
//	/dev/disk3s5    → disk3   （APFS 数据卷宗）
//	/dev/disk3s1s1  → disk3   （APFS 系统卷快照）
//	/dev/disk3      → disk3   （整盘，无分区后缀）
//	/dev/disk10s2   → disk10
//
// 注意不能简单找首个 's'：设备名本身就是 "disk…" 开头，会把 "disk3" 切坏。
func diskKey(device string) string {
	s := strings.TrimPrefix(device, "/dev/")
	const prefix = "disk"
	if !strings.HasPrefix(s, prefix) {
		return device
	}
	i := len(prefix)
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == len(prefix) {
		return device // 异常输入（无盘号）：原样返回，不参与内置盘判定
	}
	return s[:i]
}

// diskutilTimeout diskutil 子进程超时：探测失败必须快速退出，
// 不能因为一条系统命令卡住而拖住整个扫描。
const diskutilTimeout = 2 * time.Second

var (
	diskutilMu    sync.Mutex
	diskutilCache = map[string]string{} // 挂载点 -> plist 原文（"" = 探测失败，同样缓存以免反复重试）
)

// probeRotational 问 diskutil 该挂载点是否旋转介质。
func probeRotational(mount string) Class {
	plist, ok := diskutilInfo(mount)
	if !ok {
		return Unknown
	}
	c, ok := parseSolidState(plist)
	if !ok {
		return Unknown
	}
	return c
}

// parseSolidState 从 diskutil plist 文本解析 SolidState 键。
// 键缺失（磁盘映像、合成卷等）时返回 ok=false → Unknown，
// 绝不默认成 Rotational（否则会把磁盘映像等误降级）。
func parseSolidState(plist string) (Class, bool) {
	const key = "<key>SolidState</key>"
	i := strings.Index(plist, key)
	if i < 0 {
		return Unknown, false
	}
	rest := strings.TrimSpace(plist[i+len(key):])
	switch {
	case strings.HasPrefix(rest, "<true/>"):
		return SSD, true
	case strings.HasPrefix(rest, "<false/>"):
		return Rotational, true
	}
	return Unknown, false
}

// isDiskutilError 识别 diskutil 的错误 plist。
// 关键点：diskutil 失败时**退出码仍为 0**，把错误写进 plist
// （<key>Error</key><true/>），因此必须解析内容判断，不能只看进程退出码。
func isDiskutilError(plist string) bool {
	const key = "<key>Error</key>"
	i := strings.Index(plist, key)
	if i < 0 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(plist[i+len(key):]), "<true/>")
}

func diskutilInfo(mount string) (string, bool) {
	diskutilMu.Lock()
	out, cached := diskutilCache[mount]
	diskutilMu.Unlock()
	if cached {
		return out, out != ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), diskutilTimeout)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "diskutil", "info", "-plist", mount).Output()

	s := ""
	if err == nil {
		text := string(raw)
		if !isDiskutilError(text) {
			s = text
		}
	}

	diskutilMu.Lock()
	diskutilCache[mount] = s
	diskutilMu.Unlock()
	return s, s != ""
}
