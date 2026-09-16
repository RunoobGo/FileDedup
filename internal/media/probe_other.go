//go:build !darwin

package media

// Probe 非 darwin 平台暂不做介质探测，恒返回 Unknown。
// 语义上等价于「不介入」：调用方维持「CPU 核数-1」的默认并发度，
// 即引入本包前的行为，因此这些平台不会有任何回归。
//
// 若要在其他平台启用，只需在本文件所在平台实现 Probe
// （例如 Linux 可由 statfs 的 f_type 魔数识别 nfs/smbfs，
// 再由 /sys/block/<dev>/queue/rotational 判定旋转介质），
// 上层 RootsClass / AutoWorkers 无需改动。
func Probe(_ string) Class { return Unknown }
