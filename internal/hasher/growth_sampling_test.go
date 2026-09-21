package hasher

// 采样路径的变长守卫（2026-09-21 全仓审查，CACHE-1/PARA-1 的结构性成因）。
//
// AS-H2 的三条入口守卫里 `rejectGrowthBeyond` 只挂在**小文件一趟**与两条全量路径
// （HashFull / HashFullSegmented）上。大文件走 4 点采样时它不在场，于是"声明长度
// 之后还有可读字节"这一形状在采样阶段完全无人探测。
//
// 为什么这要紧：`Lookup` 命中缓存且四点采样相符时，pipeline 会**跳过阶段 3**
// （pipeline.go 的 `if pre[ci].fullValid { continue }`），而那正是唯一会调用
// HashFull（自带守卫）的地方。于是"扫描期间被追加"的大文件可以拿着上一轮的
// full 哈希直接进组——它的四点采样按**声明 size** 排布，追加的尾巴落在四点之外，
// 采样覆盖率为 0。 paranoid 那一层同样按 size 截断比对（见 dedup 侧用例），
// 所以声明里"最严档"的兜底也不在场。

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashHeadTailRejectsGrowthOnSamplingPath(t *testing.T) {
	dir := t.TempDir()
	// 真实 400 KiB，声明 300 KiB ⇒ 走采样分支（声明 > SmallFileMax）。
	actual := bytes.Repeat([]byte("A"), 400*1024)
	p := filepath.Join(dir, "grew-sampling.bin")
	if err := os.WriteFile(p, actual, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	declared := int64(300 * 1024)
	_, err = HashHeadTail(f, declared, make([]byte, HeadTailChunk))
	if err == nil {
		t.Fatalf("声明 %d 字节、实际 %d 字节可读，采样路径仍给出结果："+
			"追加的 %d 字节落在四个采样点之外（采样偏移由声明 size 决定），"+
			"这张采样指纹与「真的只有 %d 字节」的文件逐位相同 ⇒ 假重复组（AS-H2 的第四入口）",
			declared, len(actual), len(actual)-int(declared), declared)
	}
	if !strings.Contains(err.Error(), "文件比声明长") {
		t.Fatalf("报错文案应指明是变长守卫（与 HashFull 同一判据同一措辞），得到：%v", err)
	}
}

// 负数/畸形 st_size：损坏目录项、畸形 FUSE/SMB 实现会把长度报成负值。
// scanner 侧 `uint64(info.Size())` 再 `int64(e.Size)` 转回来仍是负数，而
// `size <= SmallFileMax` 对负数成立 ⇒ `buf[:size]` 切片越界 panic。
func TestHashHeadTailRejectsNegativeDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.bin")
	if err := os.WriteFile(p, bytes.Repeat([]byte("B"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	panicked := func() (pan any) {
		defer func() { pan = recover() }()
		_, err = HashHeadTail(f, -1, make([]byte, SmallFileMax))
		return nil
	}()
	if panicked != nil {
		t.Fatalf("负数声明长度导致 panic（%v）：一个坏卷/畸形挂载就能把整轮扫描打成 "+
			"StatusFailed（worker panic 会让 Run 整轮作废），而不是记一条 FailedItem", panicked)
	}
	if err == nil {
		t.Fatal("负数声明长度应 fail-closed 报错，不得给出哈希结果")
	}
	if !strings.Contains(err.Error(), "声明长度") {
		t.Fatalf("报错应指明是入参不可信，得到：%v", err)
	}
}

func TestHashFullRejectsNegativeDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.bin")
	if err := os.WriteFile(p, bytes.Repeat([]byte("B"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, StreamChunk)
	if _, err := HashFull(f, -1, buf); err == nil {
		t.Fatal("负数声明长度应报错")
	} else if !strings.Contains(err.Error(), "声明长度") {
		t.Fatalf("报错应指明是入参不可信（而不是恰好被计数分支挡下），得到：%v", err)
	}
}

// 负控制：健康的大文件（声明 == 实际）在加了守卫之后照常出结果，
// 且四点采样指纹不变——本项不是把采样路径整体判死。
func TestHashHeadTailSamplingPathUnchangedWhenSizeMatches(t *testing.T) {
	dir := t.TempDir()
	content := bytes.Repeat([]byte("C"), 300*1024)
	p := filepath.Join(dir, "stable.bin")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, err := HashHeadTail(f, int64(len(content)), make([]byte, HeadTailChunk))
	if err != nil {
		t.Fatalf("声明与实际一致的大文件不应报错：%v", err)
	}
	if r.Short || r.Small || r.ActualSize != int64(len(content)) {
		t.Fatalf("采样结果字段异常：%+v", r)
	}
	// 与"再打开一次"逐位相同：守卫不得改变指纹口径。
	f2, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	r2, err := HashHeadTail(f2, int64(len(content)), make([]byte, HeadTailChunk))
	if err != nil {
		t.Fatal(err)
	}
	if r.Partial != r2.Partial || r.Full != r2.Full {
		t.Fatal("同一文件两次采样指纹不一致")
	}
}
