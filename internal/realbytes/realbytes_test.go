package realbytes

// M6-P2 U1（04 §6.9.3 / 设计稿 §3.3）：实占回退规则的三向断言。
//
// 这一层无 build tag、纯算术，因此在 Linux/macOS 主门禁上就是可执行断言——
// 平台侧（unix 的 st_blocks、Windows 的 GetCompressedFileSizeW）只负责"报一个数
// 或报不出来"，判定与回退全在这里。平台读数本身在真机验证前不得当作已证。
//
// M28（2026-09-21）：`From` 增第四参 `trustsZero`（卷级证据）。本节三条既有用例
// **只加了一个 `false` 实参，断言一字未动**——它们钉的仍是 M6-P2 的规则 1/2/3；
// "有证据"的那一侧由 TestFromTrustsZeroOnlyWhenVolumeProven 单独钉。

import "testing"

func TestFromFallsBackWhenUnreported(t *testing.T) {
	// 平台读不到（句柄失效、FUSE/NFS 不支持）→ 逻辑大小 + known=false。
	// known 必须为 false：否则界面把"未知"显示成"实占=逻辑"，与说谎同罪。
	if a, k := From(4096, 0, false, false); a != 4096 || k {
		t.Errorf("From(4096,0,false,false) = (%d,%v), want (4096,false)", a, k)
	}
}

func TestFromFallsBackWhenPlatformReportsZero(t *testing.T) {
	// 部分卷（若干 FUSE/NFS 实现）把 st_blocks 恒报 0。一个非空文件的
	// "实占 0"在物理上不成立，必须判成"读不到"而非"真的不占空间"——
	// 否则报表上一整片重复组会显示为 0 字节可释放。
	if a, k := From(1<<20, 0, true, false); a != 1<<20 || k {
		t.Errorf("From(1MiB,0,true,false) = (%d,%v), want (1MiB,false)", a, k)
	}
	// 0 字节文件报 0 是真实的（扫描器虽内置跳过 0 字节，口径不能因此出错）。
	if a, k := From(0, 0, true, false); a != 0 || !k {
		t.Errorf("From(0,0,true,false) = (%d,%v), want (0,true)", a, k)
	}
}

func TestFromDoesNotCapAtLogicalSize(t *testing.T) {
	// 实占**允许大于**逻辑大小，两条真实来源（本机 APFS 前置实验 §3.0）：
	//   ① 块粒度：1 KiB 实写文件占满 4 KiB 一个块；
	//   ② 预分配：mkfile -n 8m 不写数据也占 8 MiB。
	// 任何 min(实占, 逻辑) 式封顶都会把这两类抹平，账面凭空少一块。
	if a, k := From(1024, 4096, true, false); a != 4096 || !k {
		t.Errorf("From(1024,4096,true,false) = (%d,%v), want (4096,true)", a, k)
	}
	if a, k := From(1<<20, 8<<20, true, false); a != 8<<20 || !k {
		t.Errorf("From(1MiB,8MiB,true,false) = (%d,%v), want (8MiB,true)", a, k)
	}
}

func TestFromPassesThroughSparseReported(t *testing.T) {
	// 稀疏文件是收益主项：65 MiB 逻辑只占 1 MiB，实占必须如实小于逻辑。
	if a, k := From(68157440, 1048576, true, false); a != 1048576 || !k {
		t.Errorf("From(65MiB,1MiB,true,false) = (%d,%v), want (1MiB,true)", a, k)
	}
}

// TestFromTrustsZeroOnlyWhenVolumeProven M28（设计稿 §11.3 V1）：第四参只作用于
// "报 0"这一分支，四向各自对应判据表的一行。第 1/2 向是本项的分界线：
// 同一个 `(size,0,true)` 在有/无卷级证据下取不同的值，方向由证据决定。
func TestFromTrustsZeroOnlyWhenVolumeProven(t *testing.T) {
	if a, k := From(1<<20, 0, true, false); a != 1<<20 || k {
		t.Errorf("无证据：From(1MiB,0,true,false) = (%d,%v), want (1MiB,false)", a, k)
	}
	if a, k := From(1<<20, 0, true, true); a != 0 || !k {
		t.Errorf("有证据：From(1MiB,0,true,true) = (%d,%v), want (0,true)——全洞文件的真读数", a, k)
	}
	if a, k := From(1<<20, 4096, true, true); a != 4096 || !k {
		t.Errorf("非零读数不受证据影响：From(1MiB,4096,true,true) = (%d,%v), want (4096,true)", a, k)
	}
	// 读不到与证据无关：证据说的是"这个 0 可信"，不是"读得到"。
	if a, k := From(1<<20, 0, false, true); a != 1<<20 || k {
		t.Errorf("读不到不因卷证据翻案：From(1MiB,0,false,true) = (%d,%v), want (1MiB,false)", a, k)
	}
}

// TestTrackingOnlyRecordsNonZeroEvidence M28（设计稿 §11.3 V2）：证据的三个条件
// ——读到了、非零、**同卷**。第三条是本项最重要的一条不变式：一个卷的读数
// 不能给另一个卷作证（连 statfs f_type 相同都不够，§11.0 E3）。
func TestTrackingOnlyRecordsNonZeroEvidence(t *testing.T) {
	var tr Tracking        // 零值可用（首次 Observe 时初始化）
	tr.Observe(7, 0, true) // 0 不是证据：它正是待解释的那个值，拿它作证是循环论证
	if tr.Trust(7) {
		t.Error("Observe(vid,0,true) 之后 Trust = true：待解释的 0 被当成了证据")
	}
	tr.Observe(7, 4096, false) // 读不到不是证据（合约的一环：ok 与 reported 是两件事）
	if tr.Trust(7) {
		t.Error("Observe(vid,4096,false) 之后 Trust = true：ok=false 的读数没有信息量")
	}
	tr.Observe(7, 4096, true)
	if !tr.Trust(7) {
		t.Error("Observe(vid,4096,true) 之后 Trust = false：非零证据没有被记下")
	}
	if tr.Trust(8) {
		t.Error("Trust(8) = true：证据跨卷生效了（本项最重要的一条不变式）")
	}
}

// TestTrackingMergeKeepsKeysSeparate M28（设计稿 §11.3 V3）：各 worker 的证据合并后
// 仍按卷分账——合并是"并集"，不是"平均"或"任一成立即全体成立"。
func TestTrackingMergeKeepsKeysSeparate(t *testing.T) {
	var a, b Tracking
	a.Observe(1, 4096, true)
	b.Observe(2, 4096, true)
	b.Observe(3, 0, true) // 无证据的卷：合并后也不许凭空成立
	a.Merge(&b)
	if !a.Trust(1) || !a.Trust(2) {
		t.Errorf("Merge 后 Trust(1)/Trust(2) = %v/%v, want true/true", a.Trust(1), a.Trust(2))
	}
	if a.Trust(3) {
		t.Error("Trust(3) = true：合并把只报过 0 的卷也算成了有证据")
	}
	if a.Trust(4) {
		t.Error("Trust(4) = true：合并没有见过的卷")
	}
}
