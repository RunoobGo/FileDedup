package media

import (
	"runtime"
	"testing"
)

// G6：并发度只降级、不升级。
func TestAutoWorkers(t *testing.T) {
	cases := []struct {
		class      Class
		cpuDefault int
		want       int
	}{
		// SSD / Unknown 维持默认
		{SSD, 9, 9},
		{Unknown, 9, 9},
		{SSD, 1, 1},
		{Unknown, 0, 1}, // 非法默认值收敛到 1
		// 机械盘 / 网络卷下调到 lowConcurrency
		{Rotational, 9, lowConcurrency},
		{Network, 9, lowConcurrency},
		{Network, 16, lowConcurrency},
		// 默认值本来就低于低并发档时不得上调
		{Rotational, 1, 1},
		{Network, 1, 1},
		{Rotational, 2, 2},
		{Rotational, 0, 1},
	}
	for _, c := range cases {
		if got := AutoWorkers(c.class, c.cpuDefault); got != c.want {
			t.Fatalf("AutoWorkers(%s, %d) = %d, want %d", c.class, c.cpuDefault, got, c.want)
		}
	}
}

// G6：多根归并取最受限者。
func TestClassify(t *testing.T) {
	cases := []struct {
		in   []Class
		want Class
	}{
		{nil, Unknown},
		{[]Class{SSD}, SSD},
		{[]Class{Unknown}, Unknown},
		{[]Class{SSD, SSD}, SSD},
		{[]Class{SSD, Unknown}, SSD},
		{[]Class{SSD, Rotational}, Rotational},
		{[]Class{Rotational, Network}, Network},
		{[]Class{SSD, Rotational, Unknown}, Rotational},
		{[]Class{Network, Rotational, SSD}, Network},
	}
	for _, c := range cases {
		if got := Classify(c.in...); got != c.want {
			t.Fatalf("Classify(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

// G6：RootsClass 的归并、nil 探测与空根处理。
func TestRootsClass(t *testing.T) {
	probe := func(p string) Class {
		switch p {
		case "/ssd":
			return SSD
		case "/hdd":
			return Rotational
		case "/net":
			return Network
		default:
			return Unknown
		}
	}
	cases := []struct {
		roots []string
		probe func(string) Class
		want  Class
	}{
		{[]string{"/ssd"}, probe, SSD},
		{[]string{"/ssd", "/hdd"}, probe, Rotational},
		{[]string{"/ssd", "/net"}, probe, Network},
		{[]string{"/hdd", "/net"}, probe, Network},
		{[]string{"/ssd", "/other"}, probe, SSD}, // Unknown 不参与降级
		{[]string{"/other"}, probe, Unknown},
		{nil, probe, Unknown},
		{[]string{"/ssd"}, nil, Unknown}, // 无探测能力 = 不介入
	}
	for _, c := range cases {
		if got := RootsClass(c.roots, c.probe); got != c.want {
			t.Fatalf("RootsClass(%v) = %s, want %s", c.roots, got, c.want)
		}
	}
}

// G6：探测能力缺失时必须等价于"不介入"，即并发度等于传入默认值。
func TestUnknownKeepsDefault(t *testing.T) {
	for _, def := range []int{1, 2, 4, 8, 16} {
		cls := RootsClass([]string{"/whatever"}, nil)
		if got := AutoWorkers(cls, def); got != def {
			t.Fatalf("默认 %d 被改成 %d", def, got)
		}
	}
}

func TestClassString(t *testing.T) {
	cases := map[Class]string{
		Unknown: "unknown", SSD: "ssd", Rotational: "rotational", Network: "network",
	}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Fatalf("Class(%d).String() = %q, want %q", c, got, want)
		}
	}
}

// M85（设计稿 §28.2）钉 FSTypeName 的两条腿：darwin 真取到名字（不钉具体值，
// 那等于拿本机卷型当普适判据），另两平台恒空串。
//
// ★ 后半句是承重的而不是凑数的：fscase 的"探针不可用 ⇒ 先问卷型"依赖这个约定 ——
// 空串读作"没有读数"，退默认，与 M62 之前一字不差。哪天有人补上 linux 的 f_type 魔数
// 或 windows 的 GetVolumeInformation 却不改这张表（§28.6 的"明确不做"），
// 红的是**这条**而不是 fscase 里那条注入式用例。
func TestFSTypeNameByPlatform(t *testing.T) {
	dir := t.TempDir()
	name := FSTypeName(dir)
	t.Logf("goos=%s FSTypeName(%q)=%q", runtime.GOOS, dir, name)
	switch runtime.GOOS {
	case "darwin":
		if name == "" {
			t.Fatalf("darwin 的卷型读不出来 ⇒ fromVolumeType 永远拿不到证据，" +
				"M85 那一档形同未做（不是'安全退回'，是白做）")
		}
	default:
		if name != "" {
			t.Fatalf("%s 的 FSTypeName 返回了 %q，但 §28.6 明写本批只做 darwin 一条腿 ⇒ "+
				"fscase 的天生不敏感表里没有为这个名字准备的条目，补一条腿就得同步补表并给真夹具",
				runtime.GOOS, name)
		}
	}
}
