// app_reveal.go —— 预览与系统揭示。
//
// M336（2026-09-28 第七轮审查批）：由 app.go 按职责簇拆分而来。
// **方法名与签名一字未改**——Wails 绑定按方法名解析，与所在文件无关，
// 因此这是零行为改动的纯移动：前后端契约、调用方、既有测试都不受影响。
//
// 包级类型/常量/变量与非方法函数仍留在 app.go：它们被多簇共用，
// 拆开只会让「这个类型在哪」变成第二次查找。
package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// PreviewFile 预览：图片（512px / q85 JPEG 缩略图的 base64，体积随内容而变，函数里没有
// 字节上限常量）/ 文本（前 4KB）/ HEX（前 256B）。旧注释写的「图片 ≤256KB base64」在
// 高频细节图上是假的，真读数与登记见 04 §6.11 M148。
func (a *App) PreviewFile(id uint64) (PreviewData, error) {
	a.mu.Lock()
	e, ok := a.byID[id]
	a.mu.Unlock()
	if !ok {
		return PreviewData{}, fmt.Errorf("文件不存在或已过期（id=%d）", id)
	}
	f, err := os.Open(e.Path)
	if err != nil {
		return PreviewData{}, shellRPCError(err) // M214：RPC 返回腿不再直通英文 OS 错误
	}
	defer f.Close()

	if _, isImg := imageMime[strings.ToLower(e.Ext)]; isImg {
		// M4-T04：任意大小图片 → 缩略图（512px JPEG；超大文件防炸限制）。
		// Y5：源文件上限由 50MB 收紧到 20MB——预览峰值内存 ≈ 源体积 + 解码位图，
		// 20MB 已覆盖绝大多数照片/截图场景，显著降低单次预览的内存尖峰。
		const srcLimit = 20 << 20
		if e.Size > srcLimit {
			return PreviewData{Kind: "binary", Content: "图片超过 20MB，暂不支持预览"}, nil
		}
		b := make([]byte, e.Size)
		if _, err := io.ReadFull(f, b); err != nil {
			return PreviewData{}, shellRPCError(err)
		}
		thumb, err := thumbnail(b, 512)
		if err != nil {
			return PreviewData{Kind: "binary", Content: "图片解码失败: " + err.Error()}, nil
		}
		return PreviewData{Kind: "image", Content: base64.StdEncoding.EncodeToString(thumb), MimeType: "image/jpeg"}, nil
	}
	// 文本 / HEX
	const textLimit = 4 << 10
	buf := make([]byte, min64(e.Size, textLimit))
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return PreviewData{}, shellRPCError(err) // 文件短于记录 size：用已读部分（M214：此腿同壳）
	}
	b := buf[:n]
	if isLikelyText(b) {
		return PreviewData{Kind: "text", Content: string(b)}, nil
	}
	const hexLimit = 256
	m := n
	if m > hexLimit {
		m = hexLimit
	}
	return PreviewData{Kind: "hex", Content: hexDump(b[:m])}, nil
}

// RevealInFolder 平台"打开所在文件夹并选中"。
func (a *App) RevealInFolder(id uint64) error {
	a.mu.Lock()
	e, ok := a.byID[id]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("文件不存在或已过期（id=%d）", id)
	}
	cmd, err := revealCmd(e.Path)
	if err != nil {
		return err
	}
	// M58：定位命令"起得来但立刻非零退出"过去完全静默，现象是点一下没反应。
	// M288：同一出口的**反方向**——Windows 的 explorer 成功也返回 1，故退出码
	// 是否作数由 warnRevealExit 判，两侧在这一个出口上同时成立。
	return startCmd(a, cmd, func(werr error) {
		a.warnRevealExit(runtime.GOOS, cmd, "reveal",
			fmt.Sprintf("打开所在文件夹失败（%s）", e.Path), werr)
	})
}

// warnRevealExit 是定位/打开/回收站这类外部命令 onExit 的统一出口。
//
// M58 与 M288 是同一把 `startCmd` 的两个相反方向的错，这里一次满足两侧：
//   - 真失败看得见——空路径与"路径不存在/无法访问"在 checkRevealPath 就以
//     **返回值**回绝（根本走不到 exec），Start 失败同样走返回值；这条腿不受影响。
//   - 假成功不报错——退出码不作数的命令（Windows 的 explorer）只写 stderr 留痕，
//     不弹 error 级 toast；其余命令逐字沿用 warnBackground 的双通道。
//
// subject 是**已含路径与"失败"字样的整句话**，本函数只在其后拼"：原因"——
// 用户可见文案由调用方独家决定（P-19-2c 同一条规矩：出口不许替调用方说话）。
func (a *App) warnRevealExit(goos string, cmd *exec.Cmd, tag, subject string, werr error) {
	if werr == nil {
		return
	}
	if revealExitSilent(goos, cmd) {
		fmt.Fprintf(os.Stderr, "[%s] %s：%v（该命令成功也返回非零，退出码不作判据，未提示用户）\n", tag, subject, werr)
		return
	}
	a.warnBackground(tag, fmt.Sprintf("%s：%v", subject, werr))
}

// RevealPath 按路径"打开所在文件夹并选中"（功能 4：失败清单逐行定位）。
// 目录例外——直接打开自身：对目录做"到父目录里选中它"没有使用价值，用户想看的
// 是里面有什么（权限错在哪个子项上）。
func (a *App) RevealPath(path string) error {
	p, info, err := checkRevealPath(path)
	if err != nil {
		return err
	}
	cmd, err := revealCmd(p)
	if info.IsDir() {
		cmd, err = openCmd(p)
	}
	if err != nil {
		return err
	}
	return execRevealCmd(a, cmd, func(werr error) {
		a.warnRevealExit(runtime.GOOS, cmd, "reveal",
			fmt.Sprintf("打开所在文件夹失败（%s）", p), werr)
	})
}

// OpenPath 按路径打开文件/目录本身（失败清单里"路径还能看，只是处理失败"的那些行）。
func (a *App) OpenPath(path string) error {
	p, _, err := checkRevealPath(path)
	if err != nil {
		return err
	}
	cmd, err := openCmd(p)
	if err != nil {
		return err
	}
	return execRevealCmd(a, cmd, func(werr error) {
		a.warnRevealExit(runtime.GOOS, cmd, "open",
			fmt.Sprintf("打开失败（%s）", p), werr)
	})
}

// RevealKeepSource 直达「保留源」——软链接合并后磁盘上唯一那一份数据所在的位置。
//
// M290/M297 的实施腿（用户裁定：改文案 + 给直达出口，不改备份生命周期）：
// 合并成功路径无条件删合并前的 `.fdd-old`，而回撤把"备份必须在"当硬前提 ⇒ 应用内
// 回撤对走完流程的软链接合并**不可达**，数据只在保留源那一份上。既然不替用户搬回来，
// 界面至少要把"去哪儿拿回它"给出来：
//   - 保留源还在 → 与 RevealPath 同一条腿（打开所在文件夹并选中它）；
//   - 保留源已被删/挪走（悬空那一臂，正是最需要找回数据的场景）→ 打开**它所在的目录**；
//   - 连目录都不可达（盘拔了、网络卷断了）→ 明确报错，不弹空窗（同 checkRevealPath 的理由）。
//
// ★ 父目录必须在 Go 侧算：路径语义（`E:\a\b`、`/a/b`、尾分隔符、UNC）交给前端
// 就是 M116 / AS-H6 那一族"两份实现各自漂移"的成因。
func (a *App) RevealKeepSource(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return fmt.Errorf("路径为空")
	}
	if _, err := os.Lstat(p); err == nil {
		return a.RevealPath(p)
	}
	dir := filepath.Dir(p)
	if _, err := os.Lstat(dir); err != nil {
		return fmt.Errorf("保留源与其所在目录都不可达（%s）：%v", p, err)
	}
	return a.RevealPath(dir)
}
