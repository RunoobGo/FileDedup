//go:build darwin

package cloudfile

// V7（设计稿 §4.4）真机腿：钉住 flags_darwin.go 那条"只读一个数"的胶水读的是
// **正确字段**。判据本体（From）由参数化用例覆盖，平台注入值却是本机真值——
// 如果哪天 os.FileInfo.Sys() 回吐的不再是 *syscall.Stat_t，或者取错了字段名，
// 参数化用例全绿而真实扫描一个占位都认不出来。这类"接线正确性"只有真机能证。
//
// 对照用的是**独立的第二条读取路径**（syscall.Stat 直取 st_flags），而不是
// 再调一次本包函数——自己对自己永远一致。
//
// 全程只 stat，绝不 open：本项存在的理由就是"读一次就替用户下载一份"，
// 探针若开了口子就是自证其罪。

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// directDataless 绕过本包，直接从 syscall 取 SF_DATALESS 位。
// 常量在此重写一遍（不复用 sfDataless）：复用会让"取错字段"和"取错位"互相抵消。
func directDataless(path string) (bool, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return false, err
	}
	return st.Flags&0x40000000 != 0, nil
}

func TestDarwinRealFilesAgreeWithDirectStat(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("取不到家目录：%v", err)
	}
	roots := []string{
		filepath.Join(home, "Library/Mobile Documents/com~apple~CloudDocs"),
		filepath.Join(home, "Library/CloudStorage"),
	}
	var scanned, dataless int
	for _, root := range roots {
		if _, err := os.Lstat(root); err != nil {
			continue // 没有云盘客户端/未登录：这条腿在本机无从取样
		}
		base := root
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 权限/竞态目录：跳过该条，不影响其余取样
			}
			if d.IsDir() {
				// 限定深度：CloudStorage 下可能挂着网络卷与别人的库，
				// 无界遍历既慢又越界。
				rel, err := filepath.Rel(base, p)
				if err == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) >= 4 {
					return filepath.SkipDir
				}
				return nil
			}
			if scanned >= 4000 {
				return filepath.SkipAll
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil // 对照用的 syscall.Stat 会跟随符号链接，语义不对等
			}
			scanned++
			got := Of(info)
			want, err := directDataless(p)
			if err != nil {
				return nil
			}
			if got != want {
				t.Errorf("%s：Of(info)=%v 与直读 st_flags=%v 不一致（胶水读错字段？）", p, got, want)
				return nil
			}
			if got {
				dataless++
				// 记下路径与逻辑大小：划账时要说清"正例是不是非空样本"，
				// 一个 0 字节的 .localized 证明不了"省下一次真实下载"。
				t.Logf("dataless 样本：%s（逻辑 %d 字节）", p, info.Size())
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败：%v", root, err)
		}
	}
	if scanned == 0 {
		t.Skipf("本机无可取样的本地文件（两个云盘根都不存在），真机腿不成立")
	}
	t.Logf("真机取样 %d 个文件，其中 dataless 占位 %d 个", scanned, dataless)
	if dataless == 0 {
		// 没有正样本时这条腿只证明了"读的是同一个字段、且不会误报"，
		// 不证明"能认出占位"。诚实地记下来，不当成已验证。
		t.Log("本机没有非空 dataless 样本：正例路径未经真机读数（见 §4.5 未兑现清单）")
	}
}
