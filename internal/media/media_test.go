package media

import "testing"

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
