package fsid

// 2026-09-18 审查 I7：缓存身份改由**句柄**解析（Windows 只有这条路拿得到卷号+文件索引）。
// 本文件跑在所有平台，只断言与平台无关的部分；强断言见 fsid_windows_test.go。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mkTempFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("FSID-IDENTITY-PAYLOAD"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFromFileNil(t *testing.T) {
	if id := FromFile(nil); id.Resolved {
		t.Fatalf("nil 句柄不得解析出身份: %+v", id)
	}
}

// 同一句柄两次查询必须一致（缓存比对全靠这个确定性）。
func TestFromFileDeterministic(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Open(mkTempFile(t, dir, "a.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if a, b := FromFile(f), FromFile(f); a != b {
		t.Fatalf("同一句柄两次查询不一致: %+v vs %+v", a, b)
	}
}

// 非 Windows：句柄路径与 FileInfo 路径必须给出同一身份（两条 API 不得各说各话）。
func TestFromFileMatchesFileInfoOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 FileInfo 有意不解析（拿不到卷号/索引），见专用测试")
	}
	dir := t.TempDir()
	p := mkTempFile(t, dir, "a.bin")
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	id := FromFile(f)
	if !id.Resolved || id.Ino == 0 {
		t.Fatalf("unix 上应从句柄解析出身份: %+v", id)
	}
	if got := FromFileInfo(st); got != id {
		t.Fatalf("FromFile %+v 与 FromFileInfo %+v 不符", id, got)
	}
	// 路径被换成另一个文件时身份必须改变（缓存防伪命中的那一重证据）
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("DIFFERENT-FILE-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	f2, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if FromFile(f2) == id {
		t.Fatal("换文件后身份未变")
	}
}
