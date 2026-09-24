package ops

import "sync"

// trashCallMu / withTrashSerial：回收站调用的串行列（M197，2026-09-24 裁定「整体加互斥」）。
//
// 判据本体在 windows 侧是"操作前后该卷回收站条目数的增量"（`snapshotRecycleBinCounts`
// → `verifyRecycled`）。执行器的批量失败回退（C6）用 `runIndexed` **并发**逐个调 `Trash`，
// 两条腿同时在跑时 A 看到的增量可能全是 B 推进的——B 的成功替 A 作了保，H6 第二道防线
// （把 Shell 的静默永久删除变成响亮的错误）被旁路。
//
// 临界区必须罩住"取基准 → Shell 调用 → 复核增量"整段；只锁 Shell 那一行等于没锁
// （基准在锁外取，窗口照旧）。
//
// 放在无 build tag 的本文件里，是为了让"排队是否真发生"这一格在任何平台都取得到读数
// （见 `trash_serial_test.go`）；**目前只有 windows 的 `defaultTrash` 接这条列**——
// darwin 走 Finder 批量、linux 逐项目录记落点，两腿都不读计数，套上只会白白串行化。
var trashCallMu sync.Mutex

func withTrashSerial(call func([]string) (map[string]string, error), paths []string) (map[string]string, error) {
	trashCallMu.Lock()
	defer trashCallMu.Unlock()
	return call(paths)
}
