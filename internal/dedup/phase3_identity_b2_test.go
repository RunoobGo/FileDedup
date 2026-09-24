package dedup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/fsid"
	"filededup/internal/model"
)

// B2（R-算法-2）：阶段 3 是**第二次** open（阶段 2 已 open 取采样与身份 ids[ci]）。
// 两次 open 之间文件可能被顶替（同步盘落新版、下载器原子改名进来）：full 哈希算的是
// **新内容**，而 ids[ci]/采样仍是**旧文件**的，写回缓存就存下"旧身份 + 新内容"。日后
// 旧文件回位且 size/mtime 未变，Lookup 四点采样全过 → 返回新内容的 full → 旧文件被并进
// 新内容的组（假重复组）。本测试用 phase3IdentityFn 接缝模拟"阶段 3 open 时身份已变"，
// 断言该条目：① 按"扫描期间被替换"记 failed；② 不进任何重复组；③ 全量哈希不写回缓存。
//
// 两个 200KiB（>SmallFileMax 128KiB）同内容文件 ⇒ 同采样桶、均走阶段 3。接缝只让
// victim 的阶段 3 身份与阶段 2 记录的不一致，survivor 保持真实身份。
func TestPhase3WritebackIdentityMismatch(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 200<<10)
	for i := range big {
		big[i] = byte(i * 7)
	}
	victim := filepath.Join(root, "victim.bin")
	survivor := filepath.Join(root, "survivor.bin")
	if err := os.WriteFile(victim, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(survivor, big, 0o644); err != nil {
		t.Fatal(err)
	}

	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()

	// 接缝：victim 的阶段 3 句柄身份被换成一个确信不同的身份（模拟两次 open 间被顶替）；
	// survivor 返回真实身份。生产路径下 phase3IdentityFn 恒为 fsid.FromFile。
	prev := phase3IdentityFn
	phase3IdentityFn = func(f *os.File) fsid.ID {
		real := fsid.FromFile(f)
		if strings.HasSuffix(f.Name(), "victim.bin") {
			return fsid.ID{Dev: real.Dev + 0x7fff, Ino: real.Ino + 0x7fff, Resolved: true}
		}
		return real
	}
	t.Cleanup(func() { phase3IdentityFn = prev })

	p := New().WithCache(cch)
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots: []string{root}, UseCache: true, Threads: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// ① victim 按"扫描期间被替换"记 failed（Stage=hash）。
	var sawVictim bool
	for _, fi := range failed {
		if strings.HasSuffix(fi.Path, "victim.bin") {
			sawVictim = true
			if fi.Stage != "hash" {
				t.Fatalf("victim failed.Stage = %q, want \"hash\"", fi.Stage)
			}
			if !strings.Contains(fi.Err, "替换") {
				t.Fatalf("victim failed.Err = %q, want 含「替换」", fi.Err)
			}
		}
	}
	if !sawVictim {
		t.Fatalf("victim 未按「扫描期间被替换」记 failed；failed=%+v", failed)
	}

	// ② victim 不进任何重复组（其唯一同伴 survivor 因之落单，故无组）。
	for _, g := range groups {
		for _, f := range g.Files {
			if strings.HasSuffix(f.Path, "victim.bin") {
				t.Fatalf("victim 被替换却仍进了重复组：%+v", g.Files)
			}
		}
	}
	if len(groups) != 0 {
		t.Fatalf("组数 = %d, want 0（victim 被剔除后 survivor 落单）", len(groups))
	}

	// ③ 全量哈希不写回缓存：两条 partial 行（阶段 2 采样）都在，但只有 survivor 拿到
	// full。victim 的 full 必须缺席——否则就是"旧身份 + 新内容"被缓存了下来。
	st, _ := cch.GetStats()
	if st.WithFull != 1 {
		t.Fatalf("缓存 WithFull = %d, want 1（victim 的全量哈希不得写回）；stats=%+v", st.WithFull, st)
	}
}

// 反向钉住：身份一致时（生产常态）阶段 3 复核**不得**误剔——两个同内容大文件照常
// 成组、双双写回 full。防止后人把复核写得过严（例如误用 fail-closed 判据）造成漏报。
func TestPhase3WritebackIdentityMatchStillGroups(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 200<<10)
	for i := range big {
		big[i] = byte(i * 11)
	}
	a := filepath.Join(root, "a.bin")
	b := filepath.Join(root, "b.bin")
	if err := os.WriteFile(a, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, big, 0o644); err != nil {
		t.Fatal(err)
	}

	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()

	p := New().WithCache(cch)
	groups, failed, err := p.Run(context.Background(), model.ScanConfig{
		Roots: []string{root}, UseCache: true, Threads: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("身份一致不应有 failed：%+v", failed)
	}
	if len(groups) != 1 || len(groups[0].Files) != 2 {
		t.Fatalf("组数/组内 = %d/%v, want 1 组 2 文件", len(groups), groups)
	}
	st, _ := cch.GetStats()
	if st.WithFull != 2 {
		t.Fatalf("缓存 WithFull = %d, want 2（两文件均应写回 full）；stats=%+v", st.WithFull, st)
	}
}
