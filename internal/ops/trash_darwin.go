//go:build darwin

package ops

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// trashScript 静态 AppleScript：路径经 osascript 的 run handler argv 传入，
// 不再拼进脚本文本（C8）。
//
// 修正前用 fmt.Sprintf("POSIX file %q", p) 内插路径——Go 的 %q 转义（\xNN、
// \n、\"）不是 AppleScript 字符串转义，路径含双引号/反斜杠/换行时脚本非法，
// 移回收站直接失败。改为 on run argv + POSIX file (contents of p) 后，
// 任意路径（含引号/反斜杠/换行/unicode）都原样到达脚本，无转义面。
const trashScript = `on run argv
set itemList to {}
repeat with p in argv
set end of itemList to POSIX file (contents of p)
end repeat
tell application "Finder" to move itemList to trash
end run`

// osascriptArgs 构造命令行：静态脚本 + 原样路径参数（argv 机制，无转义）。
func osascriptArgs(paths []string) []string {
	args := make([]string, 0, len(paths)+2)
	args = append(args, "-e", trashScript)
	return append(args, paths...)
}

// osascript 批量与超时参数（D6）：
//   - trashBatchSize：单次 Finder 调用过万条会长时间独占进程且失败面变大，
//     按 100 条分批；后续批失败时前面批次已入回收站（不丢数据，
//     executor 的 C6 逐文件回退重试对已入站文件按 ENOENT→Skipped 处理）。
//   - trashBatchTimeout：Finder 无响应/授权弹窗时 osascript 可无限挂起，
//     每批 2 分钟上限；WaitDelay 防子进程持有 stdio 管道导致超时后仍不返回。
const (
	trashBatchSize    = 100
	trashBatchTimeout = 2 * time.Minute
	trashProcessDelay = 5 * time.Second
)

// chunkPaths 纯函数：按 size 切分路径批次（size<1 视为 1）。
func chunkPaths(paths []string, size int) [][]string {
	if size < 1 {
		size = 1
	}
	var out [][]string
	for start := 0; start < len(paths); start += size {
		end := start + size
		if end > len(paths) {
			end = len(paths)
		}
		out = append(out, paths[start:end])
	}
	return out
}

func defaultTrash(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	batches := chunkPaths(paths, trashBatchSize)
	for bi, batch := range batches {
		ctx, cancel := context.WithTimeout(context.Background(), trashBatchTimeout)
		cmd := exec.CommandContext(ctx, "osascript", osascriptArgs(batch)...)
		cmd.WaitDelay = trashProcessDelay
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("osascript 超时（>%v，第 %d/%d 批 %d 个文件；此前批次可能已移入回收站）: %w",
					trashBatchTimeout, bi+1, len(batches), len(batch), err)
			}
			return fmt.Errorf("osascript（第 %d/%d 批 %d 个文件；此前批次可能已移入回收站）: %v: %s",
				bi+1, len(batches), len(batch), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
