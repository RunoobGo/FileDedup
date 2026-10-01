package dbfile

// M379（2026-10-01 裁-1）：HardenSidecars 本体的判据。
//
// 这一包里现在住着两份"伴随文件有哪些"的知识：Quarantine 的改名清单与这里的收档清单。
// 它们共用 sidecarSuffixes，所以本文件同时钉住**列表没被谁偷偷抄成第二份**这件事
// （I5；与 M382 把 PickDefaultKeepIndex 上收进 model 是同一条理由）。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubChmod 记录被调用到的路径，并按注入的表返回错误。
type stubChmod struct {
	calls []string
	errs  map[string]error
}

func (s *stubChmod) fn(p string) error {
	s.calls = append(s.calls, p)
	if e, ok := s.errs[p]; ok {
		return e
	}
	return nil
}

// TestHardenSidecarsCoversBothSuffixes 钉清单本体：一次调用必须问到 `-wal` 与 `-shm` 两个名字，
// 且名字就是 `base + 后缀`（没有多余的 Clean/Join 把 WAL 约定改歪）。
func TestHardenSidecarsCoversBothSuffixes(t *testing.T) {
	st := &stubChmod{}
	base := filepath.Join(t.TempDir(), "cache.db")
	if fails := HardenSidecars(base, st.fn); len(fails) != 0 {
		t.Fatalf("全成功时不该有失败项：%+v", fails)
	}
	want := []string{base + "-wal", base + "-shm"}
	if strings.Join(st.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("收档问到的文件不对：\n got %v\nwant %v", st.calls, want)
	}
}

// TestHardenSidecarsSilentOnNotExist 是 P-56 的判据本体（负控制）：
// ErrNotExist 是常态（-shm 可以不建、-wal 在 checkpoint 后会被删），不得报成失败。
func TestHardenSidecarsSilentOnNotExist(t *testing.T) {
	st := &stubChmod{errs: map[string]error{
		"/x.db-wal": os.ErrNotExist,
		// 驱动实际给的是包装过的错误（原文里带"no such file"），errors.Is 必须仍然认得出。
		"/x.db-shm": fmt.Errorf("chmod: %w", os.ErrNotExist),
	}}
	fails := HardenSidecars("/x.db", st.fn)
	if len(fails) != 0 {
		t.Fatalf("ErrNotExist（含包装后的等价错误）不得算失败：%+v", fails)
	}
	// 反面对照：只把原文塞进消息、不带 %w 的错误，errors.Is 认不出来 ⇒ 必须报出去。
	// 这一格钉的是"滤除按错误身份、不按字符串猜测"（IsCorruption 那套保守判据的同一条纪律）。
	stub := &stubChmod{errs: map[string]error{"/x.db-wal": errors.New("no such file or directory")}}
	if got := HardenSidecars("/x.db", stub.fn); len(got) != 1 {
		t.Fatalf("非错误身份的\"不存在\"消息应当报出去（不得靠字符串猜），实得 %+v", got)
	}
}

// TestHardenSidecarsReportsRealFailure 钉另一半：真失败必须带路径与原文报出去，
// 调用方才有"出声但不上升为写腿失败"可执行（M376 的口径）。
func TestHardenSidecarsReportsRealFailure(t *testing.T) {
	denied := errors.New("permission denied")
	st := &stubChmod{errs: map[string]error{"/x.db-wal": denied}}
	fails := HardenSidecars("/x.db", st.fn)
	if len(fails) != 1 {
		t.Fatalf("只有一条真失败，实得 %+v", fails)
	}
	if fails[0].Path != "/x.db-wal" || !errors.Is(fails[0].Err, denied) {
		t.Fatalf("失败项必须带原路径与系统原文：%+v", fails[0])
	}
}

// TestSidecarSuffixListIsShared 是"只有一份列表"的锚：Quarantine 走的必须也是同一份
// sidecarSuffixes。做法是不看源码而是打一次真隔离——主库改名后，两个侧文件若在场也必须
// 一起被挪走；若哪天有人另抄一份清单漏了 -shm，这一格就会红。
func TestSidecarSuffixListIsShared(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "cache.db")
	for _, p := range []string{base, base + "-wal", base + "-shm"} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	q, err := Quarantine(base)
	if err != nil {
		t.Fatalf("隔离失败：%v", err)
	}
	for _, suffix := range sidecarSuffixes {
		if _, err := os.Stat(base + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("侧文件 %s 留在了原路径（隔离清单与收档清单分叉了）", suffix)
		}
		if _, err := os.Stat(q + suffix); err != nil {
			t.Fatalf("侧文件 %s 没跟着隔离：%v", suffix, err)
		}
	}
}
