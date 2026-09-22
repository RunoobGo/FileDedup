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

// FSTypeName 非 darwin 平台不读卷型，恒返回空串（与上面的 Probe 同一口径：先只做一条腿）。
//
// 调用方（fscase 的 M85 卷型分岔）把空串读作"这一格没有读数"，从而退回它原本的行为，
// 因此这两条腿上"探针不可用 ⇒ 退平台默认"与 M62 之前**一字不差**。
// 要在 linux 启用：由 statfs 的 f_type 魔数给出对应名字（vfat / msdos / exfat / ntfs…）；
// windows 同理走 GetVolumeInformationW 的 lpFileSystemName。★ 本批**不做**（设计稿 §28.6）。
func FSTypeName(_ string) string { return "" }
