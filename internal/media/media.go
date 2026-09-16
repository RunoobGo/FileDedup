// Package media 判定扫描目标所在存储介质的类别，用于自适应 IO 并发度（G6）。
//
// 背景：哈希阶段为 IO 密集型。并发度固定取「CPU 核数-1」对 SSD 是合理的，
// 但在机械盘（并发随机读引发寻道抖动）与网络卷（往返排队 + 带宽争抢）上，
// 更高并发反而使总吞吐下降。
//
// 设计约束：本包**只做降级**。仅在能够明确判定目标为机械盘 / 网络卷时
// 下调并发度；判定不出来（探测失败、未知文件系统、平台不支持）一律返回
// Unknown，使调用方维持自己的默认值——即改造前行为。因此引入本包
// 在任何环境下都不会让并发度变高，也不会因探测失败而改变既有行为。
package media

// Class 存储介质类别。
type Class int

const (
	// Unknown 无法判定（探测失败 / 平台不支持 / 非物理卷）。
	Unknown Class = iota
	// SSD 固态介质：可高并发。
	SSD
	// Rotational 旋转介质（机械盘 / 融合盘）：低并发以避免寻道抖动。
	Rotational
	// Network 网络文件系统：低并发以避免往返排队与带宽争抢。
	Network
)

func (c Class) String() string {
	switch c {
	case SSD:
		return "ssd"
	case Rotational:
		return "rotational"
	case Network:
		return "network"
	default:
		return "unknown"
	}
}

// lowConcurrency 低并发档位（机械盘 / 网络卷）。
// 取 2 而非 1：读 syscall 与哈希计算可重叠，2 个 worker 已足以掩盖
// 单次请求延迟；继续增加才会开始互相拖累（寻道抖动 / 排队）。
const lowConcurrency = 2

// AutoWorkers 按介质类别给出 IO 并发度。
// cpuDefault 为无法判定时的回退值（调用方通常传「核数-1」）。
// 对机械盘 / 网络卷下调到 lowConcurrency，但**不会上调** cpuDefault 更小的值。
func AutoWorkers(c Class, cpuDefault int) int {
	if cpuDefault < 1 {
		cpuDefault = 1
	}
	switch c {
	case Rotational, Network:
		if cpuDefault > lowConcurrency {
			return lowConcurrency
		}
		return cpuDefault
	default:
		return cpuDefault
	}
}

// Classify 归并多个卷的类别，取最受限者：
// 只要有一个根落在机械盘或网络卷上，整体就按低并发处理
// （同一次扫描的 worker 池是共享的，无法按根分别限流）。
func Classify(classes ...Class) Class {
	worst := Unknown
	for _, c := range classes {
		if severity(c) > severity(worst) {
			worst = c
		}
	}
	return worst
}

// severity 受限程度排序（越大越受限）。
func severity(c Class) int {
	switch c {
	case Network:
		return 3
	case Rotational:
		return 2
	case SSD:
		return 1
	default:
		return 0
	}
}

// RootsClass 探测各扫描根的介质类别并归并。
// probe 为 nil 或 roots 为空时返回 Unknown（等价于「不介入」）。
func RootsClass(roots []string, probe func(string) Class) Class {
	if probe == nil || len(roots) == 0 {
		return Unknown
	}
	classes := make([]Class, 0, len(roots))
	for _, r := range roots {
		classes = append(classes, probe(r))
	}
	return Classify(classes...)
}
