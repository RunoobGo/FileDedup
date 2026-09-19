//go:build darwin

package ops

import (
	"context"
	"errors"
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
//
// v0.5.0 功能 4（UI 验收两处修正）：
//   - Finder `move <列表> to trash` 在列表仅一项时返回奇异引用（非列表），
//     repeat 迭代 0 次；批量输出行数也与输入无对应保证。
//   - 入站重名时 Finder 会改写基名（"b 19.04.56.bin"），按基名回推配对
//     会整体放弃 → 账本 DestPath 为空、回撤失效。
//     现逐文件 move 并输出 "src␟dst"（0x1F 分隔）成对行，配对在脚本内
//     一一对应完成，与改名无关。注意 POSIX file 解析必须在 tell 块外，
//     tell 内会被当作 Finder 对象引用（-1728）。
const trashScript = `on run argv
set out to ""
repeat with p in argv
set srcItem to POSIX file (contents of p)
tell application "Finder"
set moved to move srcItem to trash
end tell
set out to out & (contents of p) & (character id 31) & (POSIX path of (moved as alias)) & linefeed
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

// parseTrashOutput 解析 Finder 输出的 "src␟dst" 成对行（0x1F 分隔，
// 每行一条），返回 src→dst 映射。src 必须命中本批输入才采信；
// 无法解析的行（如含换行的文件名破坏行协议）只丢自身一条——
// 该输入按「去向未知」处理（账本 DestPath 为空 → 回撤给出明确错误）。
func parseTrashOutput(stdout string, inputs []string) map[string]string {
	want := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		want[in] = true
	}
	out := make(map[string]string, len(inputs))
	for _, l := range strings.Split(stdout, "\n") {
		l = strings.TrimSuffix(l, "\r")
		i := strings.IndexByte(l, 0x1f)
		if i <= 0 || i == len(l)-1 {
			continue
		}
		src, dst := l[:i], l[i+1:]
		if want[src] {
			out[src] = dst
		}
	}
	return out
}
