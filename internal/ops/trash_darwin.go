//go:build darwin

package ops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
//
// v0.5.0 功能 4：Finder move 返回已入站条目的引用列表，逐条转 POSIX 路径
// 输出（每行一个），供 parseTrashOutput 建立 src→dst 映射（回撤账本）。
const trashScript = `on run argv
set itemList to {}
repeat with p in argv
set end of itemList to POSIX file (contents of p)
end repeat
tell application "Finder"
set movedItems to move itemList to trash
end tell
-- Finder 单条目移动返回奇异引用（非列表），repeat 对其迭代 0 次
-- → 输出空串、账本去向丢失；先规范成列表再逐条转 POSIX 路径。
if class of movedItems is not list then set movedItems to {movedItems}
set out to ""
repeat with o in movedItems
set out to out & (POSIX path of (o as alias)) & linefeed
end repeat
return out
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

func defaultTrash(paths []string) (map[string]string, error) {
	dst := map[string]string{}
	if len(paths) == 0 {
		return dst, nil
	}
	batches := chunkPaths(paths, trashBatchSize)
	for bi, batch := range batches {
		ctx, cancel := context.WithTimeout(context.Background(), trashBatchTimeout)
		cmd := exec.CommandContext(ctx, "osascript", osascriptArgs(batch)...)
		cmd.WaitDelay = trashProcessDelay
		// Output()（仅 stdout）：stderr 若混入会破坏逐行路径协议
		out, err := cmd.Output()
		cancel()
		if err != nil {
			stderr := strings.TrimSpace(err.Error())
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				stderr = strings.TrimSpace(string(ee.Stderr))
			}
			if ctx.Err() == context.DeadlineExceeded {
				return dst, fmt.Errorf("osascript 超时（>%v，第 %d/%d 批 %d 个文件；此前批次可能已移入回收站）: %w",
					trashBatchTimeout, bi+1, len(batches), len(batch), err)
			}
			return dst, fmt.Errorf("osascript（第 %d/%d 批 %d 个文件；此前批次可能已移入回收站）: %v: %s",
				bi+1, len(batches), len(batch), err, stderr)
		}
		for k, v := range parseTrashOutput(string(out), batch) {
			dst[k] = v
		}
	}
	return dst, nil
}

// parseTrashOutput 把 Finder 输出的 moved POSIX 路径（每行一个）与输入路径
// 按基名多重集配对，返回 src→dst。数量或基名对不上（如入站重命名
// "a 2.txt"、含换行的文件名破坏行协议）时返回 nil——回撤依赖正确的
// dst，宁缺勿错配；调用方按「去向未知」处理。
func parseTrashOutput(stdout string, inputs []string) map[string]string {
	var lines []string
	for _, l := range strings.Split(stdout, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != len(inputs) {
		return nil
	}
	used := make([]bool, len(lines))
	out := make(map[string]string, len(inputs))
	for _, in := range inputs {
		base := filepath.Base(in)
		match := -1
		for j, l := range lines {
			if !used[j] && filepath.Base(l) == base {
				match = j
				break
			}
		}
		if match < 0 {
			return nil
		}
		used[match] = true
		out[in] = lines[match]
	}
	return out
}
