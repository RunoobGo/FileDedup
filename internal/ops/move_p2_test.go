package ops

// P2 回归：跨卷复制路径的元数据还原 + isCrossDevice 的判定收紧。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestCopyVerifyPreservesModeAndMtime(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "exec.bin")
	if err := os.WriteFile(src, []byte("payload-1234"), 0o750); err != nil {
		t.Fatal(err)
	}
	mt := time.Unix(1_600_000_000, 123_456_789)
	if err := os.Chtimes(src, mt, mt); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.bin")
	if err := copyVerify(src, dst, st); err != nil {
		t.Fatal(err)
	}
	dt, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got, want := dt.Mode().Perm(), os.FileMode(0o750); got != want {
			t.Errorf("权限位未还原: got %v want %v（旧实现 os.Create 会掉成 0644/0666&^umask）", got, want)
		}
		if !dt.ModTime().Equal(mt) {
			t.Errorf("mtime 未还原: got %v want %v", dt.ModTime(), mt)
		}
	} else if d := dt.ModTime().Sub(mt); d > time.Microsecond || d < -time.Microsecond {
		// Windows 平台语义差异，不是 copyVerify 的缺陷：
		//   - 无 POSIX 权限位（os.Chmod 只映射只读属性），权限断言无从成立；
		//   - 时间戳落在 FILETIME 上，粒度 100ns，纳秒尾数必然被截。
		// 仍校验时间戳还原到位（误差远小于 1µs 即为生效），不比逐纳秒。
		t.Errorf("mtime 未还原（偏差超出 FILETIME 粒度）: got %v want %v Δ%v", dt.ModTime(), mt, d)
	}
	// 源保持不动：删源由调用方负责
	if _, err := os.Stat(src); err != nil {
		t.Errorf("copyVerify 不应触碰源文件: %v", err)
	}
}

func TestCopyVerifySizeMismatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyVerify(src, filepath.Join(dir, "b"), &shortInfo{st}); err == nil {
		t.Fatal("size 声明与实际不一致时必须报错（复制不完整）")
	}
}

// shortInfo 谎报更小的 size，模拟复制过程中源被截断。
type shortInfo struct{ os.FileInfo }

func (s *shortInfo) Size() int64 { return 3 }

func TestIsCrossDeviceOnlyRealExdev(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"EXDEV", &os.LinkError{Op: "rename", Err: syscall.EXDEV}, true},
		{"EXDEV wrapped", fmt.Errorf("x: %w", &os.LinkError{Err: syscall.EXDEV}), true},
		{"EPERM 不得当作跨卷兜底", &os.LinkError{Err: syscall.EPERM}, false},
		{"EACCES 不得当作跨卷兜底", &os.LinkError{Err: syscall.EACCES}, false},
		{"ENOTEMPTY", &os.LinkError{Err: syscall.ENOTEMPTY}, false},
		{"非 LinkError", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := isCrossDevice(c.err); got != c.want {
			t.Errorf("%s: isCrossDevice=%v want %v", c.name, got, c.want)
		}
	}
}

// TestMoveFileCrossDeviceReal 在存在独立文件系统（/dev/shm）时验证真实的
// EXDEV 慢路径：内容一致 + 元数据还原 + 源已删除。环境不满足时跳过。
func TestMoveFileCrossDeviceReal(t *testing.T) {
	const probe = "/dev/shm"
	if _, err := os.Stat(probe); err != nil {
		t.Skip("无 /dev/shm，跳过真实跨卷用例")
	}
	srcDir := t.TempDir()
	dstDir := filepath.Join(probe, fmt.Sprintf("fdd-p2-%d", os.Getpid()))
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		t.Skipf("无法写入 %s: %v", dstDir, err)
	}
	defer os.RemoveAll(dstDir)

	src := filepath.Join(srcDir, "bin")
	if err := os.WriteFile(src, []byte("cross-device-payload"), 0o700); err != nil {
		t.Fatal(err)
	}
	mt := time.Unix(1_500_000_000, 987_654_321)
	if err := os.Chtimes(src, mt, mt); err != nil {
		t.Fatal(err)
	}

	dst, err := MoveFile(src, dstDir)
	if err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("跨卷移动后源应被删除: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cross-device-payload" {
		t.Errorf("内容不一致: %q", data)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("跨卷后权限位丢失: %v", st.Mode().Perm())
	}
	if !st.ModTime().Equal(mt) {
		t.Errorf("跨卷后 mtime 未还原: %v want %v", st.ModTime(), mt)
	}
}

func TestMoveFileEmptyTargetDir(t *testing.T) {
	if _, err := MoveFile("/etc/hostname", ""); err == nil {
		t.Fatal("空目标目录必须报错")
	}
}
