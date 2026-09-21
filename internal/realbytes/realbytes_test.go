package realbytes

// M6-P2 U1（04 §6.9.3 / 设计稿 §3.3）：实占回退规则的三向断言。
//
// 这一层无 build tag、纯算术，因此在 Linux/macOS 主门禁上就是可执行断言——
// 平台侧（unix 的 st_blocks、Windows 的 GetCompressedFileSizeW）只负责"报一个数
// 或报不出来"，判定与回退全在这里。平台读数本身在真机验证前不得当作已证。

import "testing"

func TestFromFallsBackWhenUnreported(t *testing.T) {
	// 平台读不到（句柄失效、FUSE/NFS 不支持）→ 逻辑大小 + known=false。
	// known 必须为 false：否则界面把"未知"显示成"实占=逻辑"，与说谎同罪。
	if a, k := From(4096, 0, false); a != 4096 || k {
		t.Errorf("From(4096,0,false) = (%d,%v), want (4096,false)", a, k)
	}
}

func TestFromFallsBackWhenPlatformReportsZero(t *testing.T) {
	// 部分卷（若干 FUSE/NFS 实现）把 st_blocks 恒报 0。一个非空文件的
	// "实占 0"在物理上不成立，必须判成"读不到"而非"真的不占空间"——
	// 否则报表上一整片重复组会显示为 0 字节可释放。
	if a, k := From(1<<20, 0, true); a != 1<<20 || k {
		t.Errorf("From(1MiB,0,true) = (%d,%v), want (1MiB,false)", a, k)
	}
	// 0 字节文件报 0 是真实的（扫描器虽内置跳过 0 字节，口径不能因此出错）。
	if a, k := From(0, 0, true); a != 0 || !k {
		t.Errorf("From(0,0,true) = (%d,%v), want (0,true)", a, k)
	}
}

func TestFromDoesNotCapAtLogicalSize(t *testing.T) {
	// 实占**允许大于**逻辑大小，两条真实来源（本机 APFS 前置实验 §3.0）：
	//   ① 块粒度：1 KiB 实写文件占满 4 KiB 一个块；
	//   ② 预分配：mkfile -n 8m 不写数据也占 8 MiB。
	// 任何 min(实占, 逻辑) 式封顶都会把这两类抹平，账面凭空少一块。
	if a, k := From(1024, 4096, true); a != 4096 || !k {
		t.Errorf("From(1024,4096,true) = (%d,%v), want (4096,true)", a, k)
	}
	if a, k := From(1<<20, 8<<20, true); a != 8<<20 || !k {
		t.Errorf("From(1MiB,8MiB,true) = (%d,%v), want (8MiB,true)", a, k)
	}
}

func TestFromPassesThroughSparseReported(t *testing.T) {
	// 稀疏文件是收益主项：65 MiB 逻辑只占 1 MiB，实占必须如实小于逻辑。
	if a, k := From(68157440, 1048576, true); a != 1048576 || !k {
		t.Errorf("From(65MiB,1MiB,true) = (%d,%v), want (1MiB,true)", a, k)
	}
}
