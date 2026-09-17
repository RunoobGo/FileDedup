package ops

// P2 回归：XDG 回收站 trashinfo 与移动的先后顺序。
//
// 旧实现先 move 后写 trashinfo：写 info 失败（inode 耗尽、info 目录被替换为
// 只读等）时文件已经躺在 files/ 里且没有元数据 → 桌面回收站看不到原始路径，
// 用户无法还原，等价于静默丢数据。现改为先写 info，失败则回滚 info。

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTrashXDGWritesInfoAndFile(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "trash")
	src := filepath.Join(dir, "报告 a.txt") // 含空格与非 ASCII，检验 percent-encoding
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := trashXDG(trash, []string{src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("源文件未被移走: %v", err)
	}
	moved, err := os.ReadDir(filepath.Join(trash, "files"))
	if err != nil || len(moved) != 1 {
		t.Fatalf("files/ 内容异常: %v (%v)", moved, err)
	}
	infoB, err := os.ReadFile(filepath.Join(trash, "info", moved[0].Name()+".trashinfo"))
	if err != nil {
		t.Fatalf("trashinfo 缺失: %v", err)
	}
	info := string(infoB)
	if !strings.HasPrefix(info, "[Trash Info]\nPath=") {
		t.Fatalf("trashinfo 头不合规:\n%s", info)
	}
	if !strings.Contains(info, "%E6%8A%A5%E5%91%8A%20a.txt") {
		t.Errorf("Path 未按规范转义:\n%s", info)
	}
	if !strings.Contains(info, "DeletionDate=") {
		t.Errorf("缺少 DeletionDate:\n%s", info)
	}
	// info 文件不得泄露到 files/ 目录
	if _, err := os.Stat(filepath.Join(trash, "files", moved[0].Name()+".trashinfo")); err == nil {
		t.Error("trashinfo 错放到 files/")
	}
}

// TestTrashXDGRollbacksInfoOnMoveFail 移动本身失败（源已消失）时，
// 先写入的 trashinfo 必须被回收，否则回收站会显示无法还原的幽灵条目。
func TestTrashXDGRollbacksInfoOnMoveFail(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "trash")
	ghost := filepath.Join(dir, "already-gone.txt")
	if err := trashXDG(trash, []string{ghost}); err == nil {
		t.Fatal("源不存在时必须报错（不得静默吞掉）")
	}
	if _, err := os.Stat(filepath.Join(trash, "info", "already-gone.txt.trashinfo")); !os.IsNotExist(err) {
		t.Errorf("留下孤儿 trashinfo: %v", err)
	}
	moved, err := os.ReadDir(filepath.Join(trash, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Errorf("失败批次不应产生 files/ 条目: %v", moved)
	}
}

// TestTrashXDGInfoWriteFailureLeavesNoOrphan 判别 trashinfo 与移动的先后顺序：
// 预置 info/<name>.trashinfo 为**目录** → 写 info 必然失败（EISDIR，与是否 root 无关）。
// 旧顺序（先 move 后写 info）此时文件已经进了 files/ 却没有元数据 = 无法还原的孤儿；
// 新顺序下移动根本不会发生，源文件留在原位。
func TestTrashXDGInfoWriteFailureLeavesNoOrphan(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "trash")
	src := filepath.Join(dir, "foo.txt")
	if err := os.WriteFile(src, []byte("keepme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(trash, "info", "foo.txt.trashinfo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := trashXDG(trash, []string{src}); err == nil {
		t.Fatal("trashinfo 写入必然失败，应返回错误")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("失败时不得动源文件: %v", err)
	}
	moved, err := os.ReadDir(filepath.Join(trash, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Errorf("files/ 出现无元数据的孤儿（用户无法还原，等于静默丢数据）: %v", moved)
	}
}

// TestTrashXDGCrossDeviceNoPartialCopy 跨卷复制失败（此处以"目录不可读为文件"
// 触发）时，不得在 files/ 留下半成品残片，也不得留下孤儿 trashinfo。
// 仅在存在可写的独立文件系统（/dev/shm）时运行。
func TestTrashXDGCrossDeviceNoPartialCopy(t *testing.T) {
	const probe = "/dev/shm"
	if _, err := os.Stat(probe); err != nil {
		t.Skip("无 /dev/shm，跳过真实跨卷用例")
	}
	// 源必须真实存在且是目录：跨卷 rename → EXDEV，复制阶段读目录必然失败
	src := filepath.Join(probe, t.Name()+"-srcdir")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o700); err != nil {
		t.Skipf("无法在 %s 建目录: %v", probe, err)
	}
	trash := filepath.Join(t.TempDir(), "trash") // tmp 在根卷 → 与 shm 跨卷
	defer os.RemoveAll(src)

	if err := trashXDG(trash, []string{src}); err == nil {
		t.Skip("本次环境未产生预期的复制失败，跳过")
	}
	moved, err := os.ReadDir(filepath.Join(trash, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Errorf("files/ 残留半成品: %v", moved)
	}
	if _, err := os.Stat(filepath.Join(trash, "info", filepath.Base(src)+".trashinfo")); !os.IsNotExist(err) {
		t.Errorf("跨卷失败留下孤儿 trashinfo: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("复制失败不得删除源（数据优先）: %v", err)
	}
}

func TestTrashXDGEmptyIsNoop(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "trash")
	if err := trashXDG(trash, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trash, "files")); err != nil {
		t.Fatalf("目录应按现有实现预建: %v", err)
	}
	moved, err := os.ReadDir(filepath.Join(trash, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Errorf("空输入不应产生回收站条目: %v", moved)
	}
}

// TestTrashXDGConcurrentSameName 模拟 executor 的 runIndexed(opWorkers=4) 并发调用
// trash：多个不同目录下内容各异的同名文件 foo.txt 被并发移入同一回收站。
// 未修复时 uniqueXDG 的「先查后用」竞态会让多个 goroutine 选中同一 dst，
// 后到者覆盖已入站文件、源被删 → 回收站条目数 < 源数（数据丢失）。
// 修复后（trashXDGGuard 互斥）应保证：源全部消失、回收站条目数 == 源数、内容无丢失。
func TestTrashXDGConcurrentSameName(t *testing.T) {
	base, err := os.MkdirTemp("", "trashtest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	trashRoot := filepath.Join(base, "Trash")

	contents := []string{"aaa", "bbb", "ccc"}
	var paths []string
	for i, c := range contents {
		d := filepath.Join(base, "src", string(rune('a'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, "foo.txt")
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if e := trashXDG(trashRoot, []string{p}); e != nil {
				mu.Lock()
				errs = append(errs, e)
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("trash 返回错误: %v", errs)
	}

	for _, p := range paths {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			t.Fatalf("源文件应已移入回收站: %s", p)
		}
	}

	filesDir := filepath.Join(trashRoot, "files")
	entries, _ := os.ReadDir(filesDir)
	if len(entries) != len(paths) {
		t.Fatalf("回收站条目数 %d != 源数 %d（存在覆盖丢失）", len(entries), len(paths))
	}

	got := map[string]bool{}
	for _, e := range entries {
		b, rerr := os.ReadFile(filepath.Join(filesDir, e.Name()))
		if rerr != nil {
			t.Fatalf("读取回收站条目失败: %v", rerr)
		}
		got[string(b)] = true
	}
	for _, c := range contents {
		if !got[c] {
			t.Fatalf("回收站缺少内容 %q（被覆盖丢失）", c)
		}
	}
}
