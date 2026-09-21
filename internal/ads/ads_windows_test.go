//go:build windows

package ads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V8（设计 §5.4）：**真机专用**，钉 E7 的结构偏移。
//
// 为什么非要有这一条：读错 cStreamName 的偏移时，拿到的是零长字符串——判据层看到
// "没有任何命名流"，守卫恒放行，而它在 Linux 主门禁里**全绿**（那里测的是手写的
// 名字列表，不是胶水真读到的字节）。所以偏移这件事只有真机能证。
//
// ★ 未兑现（M32）：本仓无 Windows runner，本条至今没有过真机读数。
func TestFirstStreamNameIsDefaultOnRealNTFS(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(p, []byte("probe"), 0o600); err != nil {
		t.Fatalf("造夹具失败：%v", err)
	}

	names, errno := enumerateStreams(p)
	t.Logf("第一个流：names=%q errno=%d", names, errno)

	if len(names) == 0 {
		t.Fatalf("枚举返回零个流：E5 说文件的第一个流恒是默认流，空列表说明胶水读错了偏移（errno=%d）", errno)
	}
	if !strings.HasSuffix(names[0], "::$DATA") {
		t.Fatalf("第一个流名 = %q，want 以 \"::$DATA\" 结尾（E7：读错偏移会得到空串/垃圾）", names[0])
	}
	if StreamIsNonDefault(names[0]) {
		t.Fatalf("默认流 %q 被判成了命名流——这会让整卷每个文件都被拒", names[0])
	}
}

// V8b：真机正例——写一条真实命名流，要求被判出来。
// 与 V8 分开是因为"卷不支持流"（exFAT 上的 %TEMP%）是**合法跳过**，
// 而 V8 的偏移钉在任何卷上都成立，不该被同一个 skip 带走。
func TestDetectsRealNamedStreamOnNTFS(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(p, []byte("probe"), 0o600); err != nil {
		t.Fatalf("造夹具失败：%v", err)
	}
	// 夹具本身就是 NTFS 语义：写 "probe.txt:note" 即给 probe.txt 挂一条命名流
	if err := os.WriteFile(p+":note", []byte("hidden"), 0o600); err != nil {
		t.Skipf("无法创建命名流（多半是本卷不支持，如 exFAT）：%v", err)
	}

	_, errno := enumerateStreams(p)
	if Classify(errno) == ErrFSNoStreams {
		t.Skipf("枚举报 87：该卷不支持备用流，本用例无判据（errno=%d）", errno)
	}

	names, _ := enumerateStreams(p)
	t.Logf("挂流后的 names=%q", names)
	if !HasNonDefault(names) {
		t.Fatalf("给 %s 挂了 :note 流却判不出命名流：names=%q", p, names)
	}
	if oc := Check(p); !oc.Reject {
		t.Fatalf("Check(%s) 放行了带命名流的文件（Reason=%q）", p, oc.Reason)
	}
}
