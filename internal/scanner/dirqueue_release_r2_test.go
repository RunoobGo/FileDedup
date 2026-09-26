package scanner

// R-扫描-2（2026-09-24 审查五轮 F 批，J-4=(b) 随批修）：dirQueue.pop 用
// `q.items = q.items[1:]` 出队，只前移切片头，从不释放底层数组里已弹出那一格对
// 字符串的引用。百万目录级扫描时，所有访问过的目录路径都被底层数组一直握着无法
// GC，内存峰值随"访问过的目录数"单调上涨（而非随"当前待处理目录数"）。
// 修法（plan 的"置零"选项，一行级）：出队前把第 0 格置空串再 reslice，
// 让那一格的字符串可被回收。环形队列是更彻底的方案但侵入大，本轮不做。

import "testing"

func TestDirQueuePopReleasesBackingSlot(t *testing.T) {
	q := newDirQueue()
	q.push("/a")
	q.push("/b")
	q.push("/c")
	backing := q.items // 与 q.items 共享同一底层数组
	if len(backing) != 3 {
		t.Fatalf("前置：底层数组应有 3 项，实得 %d", len(backing))
	}

	d, ok := q.pop()
	if !ok || d != "/a" {
		t.Fatalf("pop 应返回队首 /a：got %q ok=%v", d, ok)
	}
	if backing[0] != "" {
		t.Fatalf("R-扫描-2：出队未释放底层数组槽位，backing[0]=%q（应置空串以便 GC）", backing[0])
	}
	// 置零只应动已弹出的那一格，未弹出项不得受损。
	if backing[1] != "/b" || backing[2] != "/c" {
		t.Fatalf("置零误伤未弹出项：backing=%q", backing)
	}
}

// 行为不回归：FIFO 顺序、弹尽返回 false。
func TestDirQueuePopFIFOUnchanged(t *testing.T) {
	q := newDirQueue()
	for _, s := range []string{"x", "y", "z"} {
		q.push(s)
	}
	q.close()
	var got []string
	for {
		d, ok := q.pop()
		if !ok {
			break
		}
		got = append(got, d)
	}
	if len(got) != 3 || got[0] != "x" || got[1] != "y" || got[2] != "z" {
		t.Fatalf("FIFO 顺序被破坏：%q", got)
	}
}
