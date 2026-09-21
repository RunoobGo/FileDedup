package ops

// §20 第九批 P-20-5（M88，04 §6.11 OPS-14c；设计段 §20.0-5 / §20.1-5）。
//
// 形状与 M86（undo.go:178）完全同型：跨卷复制已到家、删源失败 ⇒ 盘上是两份，
// 而 move.go:67 改前只说"两份并存"，一个落点都不给。
//
// 比 undo 侧更要紧的一格（本批取证新证）：`executor.go:568-572` 拿到
// `MoveFile` 的 `(dst, err)` 后，`err != nil` 就只记 `ocFailed` **并把 dst 丢掉**
// ⇒ 这份孤儿副本在整条链路上只有错误文本一个留痕处（账本无条目、历史页与回撤都看不见它）。
// 所以"文本里必须有两个路径"不是措辞偏好，是唯一的信息通道。
// （dst 被丢弃这件事本身不属 M88 判据，已另登记 M89，见设计段 §20.6-2。）
//
// 改前必红：只引用改前就有的符号（MoveFile / removeSrc 接缝 / forceCrossVolumeRename），
// 失败原因须落在"文本里没有那两个路径"这一格上。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errSimulatedSrcRemove 是删源接缝的哨兵：既要让 MoveFile 报错，又要能验 `%w` 没被换掉。
var errSimulatedSrcRemove = errors.New("模拟：源文件被第三方句柄占用，删不掉")

func TestMoveFilePartialDeleteSourceNamesBothSides(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(srcDir, "victim.bin")
	payload := []byte("M88-CROSS-VOLUME-COPIED-PAYLOAD")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	forceCrossVolumeRename(t)
	origRemove := removeSrc
	removeSrc = func(string) error { return errSimulatedSrcRemove }
	t.Cleanup(func() { removeSrc = origRemove })

	dst, err := MoveFile(src, dstDir)

	// ---- 前提自检（独立于判据，先确认"两份并存"这个状态真的造成了）----
	if err == nil {
		t.Fatal("前提自检：删源失败时 MoveFile 必须报错")
	}
	if dst == "" {
		t.Fatalf("前提自检：部分成功必须把副本路径回出来（改前就是这么回的），实测空串——探针打在没成立的状态上: %v", err)
	}
	if b, rerr := os.ReadFile(dst); rerr != nil || string(b) != string(payload) {
		t.Fatalf("前提自检：副本应已完整落在 %s（rerr=%v got=%q）", dst, rerr, b)
	}
	if _, lerr := os.Lstat(src); lerr != nil {
		t.Fatalf("前提自检：源应仍在盘上（删失败才有两份并存）: %v", lerr)
	}
	if !errors.Is(err, errSimulatedSrcRemove) {
		t.Errorf("底层原因必须仍可 errors.Is 到（包装链不许被换成字符串）：实测 %v", err)
	}

	// ---- 判据格：两个落点都得在文本里 ----
	msg := err.Error()
	if !strings.Contains(msg, dst) {
		t.Errorf("错误文本没给出副本落点 %s，用户不知道新那份在哪: %q", dst, msg)
	}
	if !strings.Contains(msg, src) {
		t.Errorf("错误文本没给出仍留在盘上的源路径 %s，用户不知道该删哪一份: %q", src, msg)
	}
}
