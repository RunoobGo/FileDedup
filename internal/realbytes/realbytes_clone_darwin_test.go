//go:build darwin

package realbytes

// M27（设计稿 §13）：CoW 克隆/块级去重卷上，共享 extent 被每一份各计一次——
// 实占口径在这类卷上系统性高估，且方向是**上限**（不是登记行原写的"下限"）：
// 克隆对各自报满额 S，删掉一份真正释放 T=0，T ≤ S（§13.1 的算术 + 真读数）。
//
// 为什么夹具必须"真造克隆"而不是同卷模拟：这条高估钉的是**环境事实**。
// 两个独立文件各占一份是正确行为，不是缺陷——模拟出来的只是行为、证不了环境。
// 因此前提用卷可用空间增量自证（写 8 MiB 看得见分配；克隆后增量≈0，克隆占零）。
// 自证不过就 skip——fail-closed，不降级成"同卷模拟"（与 smoke-symlink.sh 同款纪律）。
//
// 为什么走 /bin/cp -c：stdlib 无 clonefile 绑定，x/sys 有 unix.Clonefile 但仓库
// 政策是零第三方依赖（§13.0 E7）。darwin-only：/bin/cp -c 与 APFS 都是 darwin
// 面；Linux 的 reflink btrfs 与 Windows 的 ReFS 本机不可造（§13.5-1 挂账，未兑现）。

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// cloneFixtureSize 8 MiB：足够大（分配/克隆的空间增量远超文件系统噪声），
// 又足够小（本机实测写盘一瞬）。内容必须不可压缩——全零语料在个别卷型上
// 会被透明压缩绕过 CoW 路径。
const cloneFixtureSize = 8 << 20

// freeBytes 独立读卷可用空间（量具）：不经过被测函数——前提段本身就是被测对象，
// "用被测物验证被测物"在这条用例上等于自证循环。返回 Bavail×Bsize（普通用户可用口径）。
func freeBytes(t *testing.T, dir string) uint64 {
	t.Helper()
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		t.Fatalf("statfs %s: %v", dir, err)
	}
	return uint64(st.Bavail) * uint64(st.Bsize)
}

func TestClonePairDoubleCountsActualBlocks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "clone.bin")

	// 前提段 1（量具自检）：写满 8 MiB 后可用空间若没按字节减少，说明这个环境
	// 不这样记账（配额、覆盖层、网络卷……），后面的 Δ 全部无意义。
	// Δ 用 int64：与测试机上的并发写盘比大小，负值要能表达出来（转 uint64 会翻转成天文数字）。
	f0 := freeBytes(t, dir)
	buf := make([]byte, cloneFixtureSize)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	f1 := freeBytes(t, dir)
	wrote := int64(f0) - int64(f1)
	if wrote < cloneFixtureSize/2 {
		t.Skipf("量具自检不过：写 %d B 后可用空间只减 %d B（< 半份）——本环境不按字节记账，Δ 不可用",
			cloneFixtureSize, wrote)
	}
	t.Logf("量具自检通过：写 %d B 后可用空间 Δ=%d B", cloneFixtureSize, wrote)

	// 前提段 2（真造出克隆）：cp -c 失败（该卷无克隆支持）或克隆后增量≈整份
	// （退化成真实复制）都判"本环境造不出克隆" ⇒ skip，不是红。
	if out, err := exec.Command("/bin/cp", "-c", src, dst).CombinedOutput(); err != nil {
		t.Skipf("/bin/cp -c 失败（%v；输出 %q）：本环境不支持克隆", err, string(out))
	}
	f2 := freeBytes(t, dir)
	cloned := int64(f1) - int64(f2)
	if cloned >= cloneFixtureSize/4 {
		t.Skipf("克隆后可用空间减少 %d B（≥ 整份的四分之一）：做成的是真实复制，不是克隆", cloned)
	}
	t.Logf("克隆成立：cp -c 后可用空间 Δ=%d B（克隆本身占零）", cloned)

	if os.SameFile(statOf(t, src), statOf(t, dst)) {
		t.Skipf("两份是同一 inode（硬链接形）：前提不是克隆，夹具不成立——硬链接的防线在 FileID 去重，不在本项")
	}

	// 钉子 1（§13.3 V2）：产品口径下两份**各自报满额**——共享 extent 被计了两次。
	// 走 From 这条生产判定路径（trustsZero=false：本用例没有遍历期卷级证据）。
	for _, p := range []string{src, dst} {
		reported, ok := Reported(p, statOf(t, p))
		actual, known := From(cloneFixtureSize, reported, ok, false)
		if !known || actual < cloneFixtureSize {
			t.Errorf("%s 报实占 = (%d, known=%v), want 至少一整份 (%d, true)：共享 extent 没被各计满，双计前提不成立",
				filepath.Base(p), actual, known, cloneFixtureSize)
		}
	}

	// 钉子 2（§13.3 V3）：删掉克隆真正释放 ≪ 整份——"可释放一整份"是虚报，
	// 产品按实占账报出的数值在这类卷上是**上限**（T ≤ S）。
	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}
	f3 := freeBytes(t, dir)
	freed := int64(f2) - int64(f3)
	t.Logf("删掉克隆后真正释放 %d B（产品实占口径会报可释放 %d B，本机偏差即虚报量）",
		freed, cloneFixtureSize)
	if freed >= cloneFixtureSize/4 {
		t.Errorf("删掉一份克隆释放了 %d B（≥ 整份的四分之一）：两份没有共享 extent，T ≤ S 的夹具前提不成立", freed)
	}
}
