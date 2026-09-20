package ops

// M2（2026-09-21，第 7 篇 §五 2）：目标重名递增 `uniqueDst` 是"先查后用"——
// 查询（os.Stat）与使用（os.Rename / 复制的 O_TRUNC）之间有任何间隔，
// 第三方把文件放进那个名字上，我们就会**静默覆盖**它。
// 而 `dstExists` 把所有非 nil 的 Stat 错误（含"跟随链接失败"）都当作"不存在"，
// 于是连"查"这一步看到的都不是路径上真正的对象：
// 一个悬空符号链接在 Stat 眼里等于不存在，在 os.Rename 眼里却是一个可覆盖的位置。
//
// 修法是把"查"和"用"合成一个原子动作：O_CREATE|O_EXCL 抢占占位文件，
// 抢到即拥有，抢不到（EEXIST）才递增下一个名字。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestMoveFileDoesNotClobberDanglingSymlink 构造 dstExists 误判的最小现场：
// 目标目录里已经有一个**悬空符号链接**占了我们要用的名字。
//
// 选悬空链接而不是普通文件，是为了专打"Stat 报错 ≠ 位置是空的"这一半：
// 普通文件的场景两条实现都能躲开，钉不住这个缺陷。
func TestMoveFileDoesNotClobberDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	requireSymlinkSupport(t, target)

	src := filepath.Join(srcDir, "report.bin")
	if err := os.WriteFile(src, []byte("SRC-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 第三方在我们目标目录里放的一个悬空链接（目标在别处、当下不可达）。
	link := filepath.Join(target, "report.bin")
	if err := os.Symlink(filepath.Join(target, "not-there-yet.bin"), link); err != nil {
		t.Fatal(err)
	}

	dst, err := MoveFile(src, target)
	if err != nil {
		t.Fatalf("MoveFile 失败: %v", err)
	}
	// ① 那个链接必须还在原处，并且仍然悬空（我们既没覆盖它也没替它建目标）
	li, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("第三方的悬空符号链接被移除了（M2：Stat 误判为不存在后被覆盖）: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("链接位置变成了非链接对象（mode=%v）", li.Mode())
	}
	if _, err := os.Stat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("悬空链接被接上了目标（err=%v）：不是我们该处置的事", err)
	}
	// ② 我们落在递增后的名字上，且内容是真的源数据
	if dst == link {
		t.Fatalf("落位名字与第三方链接同名：%s", dst)
	}
	if base := filepath.Base(dst); base != "report_1.bin" {
		t.Fatalf("期望递增名 report_1.bin，实际 %s（_N 插在扩展名之前的约定见 worktemp 注释）", base)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "SRC-DATA" {
		t.Fatalf("落位内容不对: %q %v", b, err)
	}
	if _, err := os.Lstat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("源未移走（err=%v）", err)
	}
}

// TestClaimDstReservesTheReturnedName 钉住"抢占"二字的实际含义：
// 函数返回时该名字**已经有一个我们拥有的文件**，而不是"我们看到它是空的"。
// 后者与前者之差就是 M2 的全部窗口。
func TestClaimDstReservesTheReturnedName(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.bin", "a_1.bin"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("foreign"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := claimDst(dir, "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(c.path); got != "a_2.bin" {
		t.Fatalf("期望递增到 a_2.bin，实际 %s", got)
	}
	st, err := os.Lstat(c.path)
	if err != nil {
		t.Fatalf("返回的名字没被占住（M2 的「先查后用」就是这个样子）: %v", err)
	}
	if st.Size() != 0 {
		t.Fatalf("占位应为 0 字节，实际 %d", st.Size())
	}
	if !c.stillOurs() {
		t.Fatal("刚抢到的占位立即被判为非我方：身份口径不一致")
	}
	for _, n := range []string{"a.bin", "a_1.bin"} {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || string(b) != "foreign" {
			t.Fatalf("第三方文件 %s 被动了: %q %v", n, b, err)
		}
	}
}

// TestClaimDstConcurrentCallsGetDistinctNames 用并发把"查—用"两步拆开：
// 旧的 uniqueDst 是纯函数（只看一眼 Stat），八个调用者会挑到同一个名字，
// 于是七家的落位会互相覆盖；O_EXCL 抢占在同样的时序下必然各得其所。
//
// 这也是执行器把 MoveFile 串行化（executor.go 的注释）所依赖的真实理由——
// 串行只是绕开缺陷，抢占才是修掉它。
func TestClaimDstConcurrentCallsGetDistinctNames(t *testing.T) {
	dir := t.TempDir()
	const n = 8
	type res struct {
		path string
		err  error
	}
	done := make(chan res, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			<-start
			c, err := claimDst(dir, "same.bin")
			done <- res{c.path, err}
		}()
	}
	close(start)
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		if seen[r.path] {
			t.Fatalf("两个调用者抢到同一个目标名 %s：位置会被互相覆盖", r.path)
		}
		seen[r.path] = true
	}
}

// TestClaimDstReportsRealErrors 覆盖 dstExists 的另一半：非 EEXIST 的错误
// 不能"放行这个名字、赌下一步会报错"——下一步是破坏性动作。
func TestClaimDstReportsRealErrors(t *testing.T) {
	dir := t.TempDir()
	// 名字里带路径分隔符 → 父目录不存在 → ENOENT，且绝不是"位置是空的"
	c, err := claimDst(dir, "nested/a.bin")
	if err == nil {
		t.Fatal("无法建位时返回了 nil：调用方会把破坏性动作做到一个不存在的位置上")
	}
	if c.path != "" {
		t.Fatalf("出错时不该带回一个占位: %q", c.path)
	}
}
