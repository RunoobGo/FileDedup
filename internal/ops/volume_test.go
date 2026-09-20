package ops

// M5（2026-09-20 全仓审计）：unix 上 volumeRoot 恒返回 "/"，跨挂载点恒判同卷。
// 本文件钉住三件事：① sameVolume 必须真的能分出跨挂载点（注入 + 真实设备各一例）；
// ② 判定不了时仍是"保守同卷"（只影响提示文案，宁可多提示不可少提示）；
// ③ 调用点数量不扩大——它是提示判据，不得参与执行决策。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func containsName(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestSameVolumeNotUsedForExecutionDecisions 钉住 sameVolume 的影响面：
// 它是"判不了就保守返回 true"的**提示用**判据，一旦被拿去做执行判据（例如
// "同卷才允许删源"），保守分支就会把跨卷当同卷、直接演变成数据丢失。
//
// 本测试不是"证明它安全"，而是**强制扩容者停下来看一眼契约段**：新增一个
// 调用点必然让这里变红，届时要么改判据（换成 fsid/卷序列号口径的真判据并另立
// 函数），要么明写理由后再把期望值改成新的调用点清单。
// （2026-09-20 全仓审计 M5：登记该契约。）
func TestSameVolumeNotUsedForExecutionDecisions(t *testing.T) {
	want := []string{"executor.go"} // 唯一合法调用点：同卷软链接的非阻断提示

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析本包失败: %v", err)
	}
	var got []string
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			name := filepath.Base(path)
			if name == "volume.go" {
				continue // 定义与回落辅助都算 volume.go 自己
			}
			ast.Inspect(file, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || id.Name != "sameVolume" {
					return true
				}
				if !containsName(got, name) {
					got = append(got, name)
				}
				return true
			})
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sameVolume 调用点变了：%v，期望 %v。新增调用点前请先读 volume.go 的契约段", got, want)
	}
}

func TestSameVolumeDetectsCrossMount(t *testing.T) {
	orig := volumeIDOf
	defer func() { volumeIDOf = orig }()
	volumeIDOf = func(p string) (string, bool) {
		if strings.HasPrefix(p, "/mnt/b") {
			return "dev-B", true
		}
		return "dev-A", true
	}

	if sameVolume("/mnt/a/f.bin", "/mnt/b/g.bin") {
		t.Fatal("跨挂载点被判为同卷：volumeRoot 在 unix 恒为 \"/\"，M5 未修")
	}
	if !sameVolume("/mnt/a/f.bin", "/mnt/a/sub/g.bin") {
		t.Fatal("同挂载点被判为跨卷")
	}
}

// TestSameVolumeRealDevices 不走注入，直接证平台实现在真实设备上有效：
// 取本机 st_dev 互异的两条路径（macOS："/" 与 "/System/Volumes/Data"；
// Linux："/" 与 /proc 等伪文件系统挂载）。找不到第二块设备时跳过而不是伪造。
func TestSameVolumeRealDevices(t *testing.T) {
	candidates := []string{
		"/", "/tmp", "/usr", "/home", "/proc", "/sys", "/dev/shm", "/run",
		"/System/Volumes/Data", "/Volumes", "C:\\", "D:\\",
	}
	var reps []string
	seen := map[string]bool{}
	for _, p := range candidates {
		id, ok := volumeIDOf(p)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		reps = append(reps, p)
	}
	if len(reps) < 2 {
		t.Skipf("本机只有 %d 个可辨识卷，无法验证跨卷判定", len(reps))
	}
	first, second := reps[0], reps[1]
	if sameVolume(first, second) {
		t.Fatalf("真实跨卷路径 %s 与 %s 被判为同卷", first, second)
	}
	if !sameVolume(first, first) {
		t.Fatalf("同一路径 %s 被判为跨卷", first)
	}
}

func TestSameVolumeConservativeWhenUndetectable(t *testing.T) {
	orig := volumeIDOf
	defer func() { volumeIDOf = orig }()
	volumeIDOf = func(string) (string, bool) { return "", false }

	// 平台卷标识拿不到 → 回落到根卷名比较；两条都在 unix 根下 → 同卷。
	if !sameVolume("/a/b.bin", "/c/d.bin") {
		t.Fatal("卷标识缺失时应回落到根名比较并给出同卷")
	}
	// 相对路径同样不得 panic、且给出保守答案（此处的同卷是"回落到同一根名"的结果，
	// 不是"跨卷被误判"——相对路径本就不携带卷信息）。
	if !sameVolume("rel/a.bin", "rel/sub/b.bin") {
		t.Fatal("相对路径应给出保守同卷")
	}
}
