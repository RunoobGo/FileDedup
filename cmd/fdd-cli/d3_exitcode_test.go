package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// D3 / R-门禁-5（J-6=(a)，2026-09-24 第五轮审查）：扫描完成但**有失败项** → exit 3，
// 与全成(0) / 运行崩溃(1) / 用法错误(2) 区分开。门禁语料是干净树、failed 恒应为 0；
// 一旦非 0 就说明扫描器/流水线在吃错误，CLI 必须让脚本与 CI 检测得到，不能藏在 exit 0 背后。
//
// 必失败输入用「把普通文件当扫描根」——ReadDir 得 ENOTDIR，记为 FailedItem{Stage:"scan"}。
// 不依赖权限（root 下 chmod 000 照样能读，权限法在 CI 不可靠），跨 darwin/linux 稳定。
func TestCLIExitThreeOnFailedItems(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fdd-cli")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("构建 fdd-cli 失败: %v", err)
	}

	aFile := filepath.Join(dir, "afile.txt")
	if err := os.WriteFile(aFile, []byte("aaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := filepath.Join(dir, "rep.json")

	cmd := exec.Command(bin, "-o", rep, aFile)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("期望非零退出（exit 3），实得 err=%v，输出：%s", err, out)
	}
	if ee.ExitCode() != 3 {
		t.Fatalf("有失败项时必须 exit 3（区别 0=全成/1=崩溃/2=用法），实得 %d，输出：%s", ee.ExitCode(), out)
	}
	// 报告 JSON 必须在退出前已完整落盘（exit 3 不能跳过写/关输出文件），且 failed 非空。
	b, rerr := os.ReadFile(rep)
	if rerr != nil {
		t.Fatalf("exit 3 前报告未落盘: %v", rerr)
	}
	var r report
	if jerr := json.Unmarshal(b, &r); jerr != nil {
		t.Fatalf("报告 JSON 解析失败: %v\n%s", jerr, b)
	}
	if len(r.Failed) == 0 {
		t.Fatalf("failed 应非空（必失败语料却零失败项，退出码 3 的判据失真）：%s", b)
	}
	if r.Stats.FilesFailed == 0 {
		t.Fatalf("stats.files_failed 应 > 0：%s", b)
	}
}

// 源码级接线锚（AS-K6 / M134/M142 同族；main() 无可注入接缝）：钉三件事——
// ① FilesFailed>0 的判据在场；② os.Exit(3) 在场；③ exit 3 在报告编码之后
// （否则 JSON 还没落盘就退出，失败项无从对账，也违背上面集成测试的"报告已落盘"前提）。
func TestExitThreeIsWiredAfterReport(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读 main.go: %v", err)
	}
	s := string(src)
	const guard = "r.Stats.FilesFailed > 0"
	if !strings.Contains(s, guard) {
		t.Fatalf("main.go 缺少 %q 判据（exit 3 未接线，failed>0 仍会静默 exit 0）", guard)
	}
	const exit3 = "os.Exit(3)"
	if !strings.Contains(s, exit3) {
		t.Fatalf("main.go 缺少 os.Exit(3)（有失败项时退出码仍是 0，门禁/CI 检测不到）")
	}
	encIdx := strings.Index(s, "enc.Encode(&r)")
	exit3Idx := strings.Index(s, exit3)
	if encIdx < 0 {
		t.Fatalf("找不到 enc.Encode(&r) 锚（main.go 结构变了，请同步本锚）")
	}
	if exit3Idx < encIdx {
		t.Fatalf("os.Exit(3) 出现在 enc.Encode 之前——报告还没写就退出，failed 项无从对账")
	}
}
