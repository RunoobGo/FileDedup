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

// osascriptArgsFor 构造命令行：静态脚本 + 原样路径参数（argv 机制，无转义）。
//
// M211（2026-09-24 第五轮审查批）：paths 前必须插 "--" 终止符。osascript 在 "--"
// 之前把后续 token 按**自身选项**解析（本机实测三格）：
//   - "-dash.txt" → `illegal option -- d` rc=2，整批失败；
//   - 恰为 "-i"（osascript 唯一无参选项）→ rc=0 且零输出，run handler 根本没跑
//     → 调用方见 err==nil 会把未移动的在盘文件整批记 done（虚账）。
//
// "--" 之后一切 token 原样进 argv，与路径形状无关。空批次不插（避免悬空终止符）。
func osascriptArgsFor(script string, paths []string) []string {
	args := make([]string, 0, len(paths)+3)
	args = append(args, "-e", script)
	if len(paths) > 0 {
		args = append(args, "--")
	}
	return append(args, paths...)
}

// osascriptArgs 生产入口：trashScript + paths。
func osascriptArgs(paths []string) []string {
	return osascriptArgsFor(trashScript, paths)
}

// batchOutputGap 判定"脚本应执行却零配对"防线（M211）：本批有输入、
// 却一行都没解析出来 ⇒ osascript 极可能根本没跑（rc=0 静默形状），
// 不能按成功记账。纯函数，判据可单测。
func batchOutputGap(parsed, batchLen int) bool {
	return batchLen > 0 && parsed == 0
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
		parsed := parseTrashOutput(string(out), batch)
		for k, v := range parsed {
			dst[k] = v
		}
		if batchOutputGap(len(parsed), len(batch)) {
			// C2 契约保持：此前批次已确认的落点不丢，随错误一起返回。
			return dst, fmt.Errorf("osascript（第 %d/%d 批 %d 个文件）返回成功但零条成对输出——脚本未执行（路径以 - 开头撞选项通道的形状已由 -- 终止符封死，这里防整族静默）",
				bi+1, len(batches), len(batch))
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
