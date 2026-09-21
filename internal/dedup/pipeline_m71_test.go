package dedup

// M71 / M75(a) / M74 在流水线侧的探针（设计段 §18.1-1、§18.1-3、§18.1-4；§18.3 P-18-1/P-18-5）。
//
// 分工：状态机与缓存本身的事实已由 internal/cache 与下面的用例分别钉住，这里只测
// "认领是否一次做完"和"缓存失败到用户眼前那句话"两件事。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/cache"
	"filededup/internal/model"
)

// P-18-1（M71）：认领 = 判 + 写，在同一个临界区里一次做完。
//
// 预测的修前红法：旧形状只 ValidateTransition 不写状态，于是"认领成功后
// p.status 仍为 Idle"，且第二次认领照样通过。改前树里 claimRunLocked 这个符号
// 根本不存在（探针编译不过），因此"修前红"由变异 M18-a（退回只判不写）提供，
// 读数抄在 §18.7。
func TestClaimRunLockedIsJudgeAndWrite(t *testing.T) {
	p := New()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status != model.StatusIdle {
		t.Fatalf("前提：初始应为 Idle，实测 %s", p.status)
	}
	if err := p.claimRunLocked(func() {}); err != nil {
		t.Fatalf("Idle 认领失败: %v", err)
	}
	if p.status != model.StatusScanning {
		t.Errorf("认领后状态必须已是 Scanning（判与写同临界区）：实测 %s", p.status)
	}
	// 第二次认领必须被拒，并且**不得**把状态改回去或留下半个 cancel。
	err := p.claimRunLocked(func() {})
	if err == nil {
		t.Fatal("第二次认领竟然成功：窗口还在，两个 Run 会同时跑")
	}
	if p.status != model.StatusScanning {
		t.Errorf("认领失败不得改动状态：实测 %s", p.status)
	}
	// 终态复位仍是受支持路径（P0-1 的既有语义，M71 不许把它改窄）。
	for _, term := range []model.TaskStatus{model.StatusDone, model.StatusCancelled, model.StatusFailed} {
		p.status = term
		if e := p.claimRunLocked(func() {}); e != nil {
			t.Errorf("终态 %s 后认领被拒: %v", term, e)
		}
		if p.status != model.StatusScanning {
			t.Errorf("终态 %s 认领后状态 = %s，want Scanning", term, p.status)
		}
	}
}

// P-18-1b（M71 的用户可见面）：认领一成功，Pause/Cancel 就必须认这次扫描。
// 这是那条窗口的真实代价——改前它回给用户两句假话（"当前不在扫描中"/
// "没有进行中的扫描任务"），而这一轮扫描其实正在跑。
func TestPauseAndCancelSeeJustClaimedRun(t *testing.T) {
	p := New()
	p.mu.Lock()
	err := p.claimRunLocked(func() {})
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if e := p.Pause(); e != nil {
		t.Errorf("刚认领就 Pause 被拒（改前的假话之一）：%v", e)
	}
	if e := p.Resume(); e != nil {
		t.Fatal(e)
	}
	if e := p.Cancel(); e != nil {
		t.Errorf("刚认领就 Cancel 被拒（改前的假话之二）：%v", e)
	}
}

// P-18-5（M75a/M74）：三档失败必须分诊成三句话，其中"已停用"那一档不在这里造句。
func TestCacheOpErrTextClassification(t *testing.T) {
	// ① 淘汰失败：哈希已写回，绝不能读成"写回失败"。
	evict := fmt.Errorf("%w：哈希条目已写回，仅 LRU 淘汰未完成（%v）", cache.ErrEvictFailed, errors.New("探针"))
	text, ok := cacheOpErrText("缓存写回", evict)
	if !ok {
		t.Fatal("淘汰失败不该被吞掉")
	}
	if !strings.Contains(text, "已写回") {
		t.Errorf("淘汰失败必须带上『已写回』这个事实：%q", text)
	}
	if strings.HasPrefix(text, "缓存写回失败") {
		t.Errorf("淘汰失败仍被写成写回失败（Commit 早已成功，这句是假话）：%q", text)
	}
	// ② 停用：轮末统一说明已经交代过，这里再报一条"写回失败"是重复且措辞不对。
	if _, ok := cacheOpErrText("缓存写回", cache.ErrCorruptDisabled); ok {
		t.Error("停用态不该再报一条写回失败")
	}
	if _, ok := cacheOpErrText("缓存命中续期", fmt.Errorf("包一层: %w", cache.ErrCorruptDisabled)); ok {
		t.Error("停用态在续期侧同样要吞掉（错误被包装后仍须认得）")
	}
	// ③ 真写回失败：措辞保持原样，不许被"已写回"污染。
	text, ok = cacheOpErrText("缓存写回", errors.New("disk I/O error"))
	if !ok || text != "缓存写回失败: disk I/O error" {
		t.Errorf("普通失败的文案走样：%q ok=%v", text, ok)
	}
	if _, ok := cacheOpErrText("缓存写回", errors.New("disk I/O error")); ok && strings.Contains(text, "已写回") {
		t.Error("普通失败里不得出现『已写回』")
	}
}

// corruptCacheNotice 的措辞边界与"至多一条"的结构前提（M74）。
func TestCorruptCacheNoticeWording(t *testing.T) {
	if got := corruptCacheNotice(7, false); got != "" {
		t.Errorf("未确证损坏不得造句：%q", got)
	}
	msg := corruptCacheNotice(7, true)
	if !strings.Contains(msg, "损坏") || !strings.Contains(msg, "已停用") {
		t.Errorf("措辞必须说清『确证损坏 + 已停用』：%q", msg)
	}
	if !strings.Contains(msg, "7") {
		t.Errorf("计数要说真数（本轮库错误次数）：%q", msg)
	}
	for _, lie := range []string{"已隔离", "已重建", "已自愈", "已恢复"} {
		if strings.Contains(msg, lie) {
			t.Errorf("措辞越界：运行期重建没做（转登记 M87），不得出现『%s』：%q", lie, msg)
		}
	}
}

// Run 级：缓存不可用时的失败清单形状（钉住"造句点确实走分诊"这一层）。
// 手段用"库已关闭"这种暂时性故障——它必须报一条、且只报一条写回失败，
// 并且**不得**冒出停用说明（停用判据比"出过错"高，这条边界不能松）。
func TestRunReportsCacheWriteBackFailureOnce(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 200<<10) // >128KiB：走预筛+全量两阶段，确保有 pending 写回
	for i := range big {
		big[i] = byte(i * 7)
	}
	for _, rel := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(root, rel), big, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cch, err := cache.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	// 健康一轮：不得有任何 cache 失败项（这条同时钉住"停用说明不会无故冒出来"）。
	if _, failed, e := New().WithCache(cch).Run(context.Background(),
		model.ScanConfig{Roots: []string{root}, UseCache: true}); e != nil || len(failed) != 0 {
		t.Fatalf("健康轮: err=%v failed=%+v", e, failed)
	}
	if err := cch.Close(); err != nil {
		t.Fatal(err)
	}
	groups, failed, err := New().WithCache(cch).Run(context.Background(),
		model.ScanConfig{Roots: []string{root}, UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	var cacheItems []model.FailedItem
	for _, f := range failed {
		if f.Stage == "cache" {
			cacheItems = append(cacheItems, f)
		}
	}
	if len(cacheItems) != 1 {
		t.Fatalf("cache 失败项应恰好一条，实测 %d：%+v", len(cacheItems), cacheItems)
	}
	if !strings.HasPrefix(cacheItems[0].Err, "缓存写回失败") {
		t.Errorf("暂时性故障应仍报写回失败：%q", cacheItems[0].Err)
	}
	if strings.Contains(cacheItems[0].Err, "已停用") || strings.Contains(cacheItems[0].Err, "损坏") {
		t.Errorf("不可用 ≠ 确证损坏，不得冒出停用措辞：%q", cacheItems[0].Err)
	}
	if cch.Corrupted() {
		t.Error("sql: database is closed 不是损坏，停用位不得置上")
	}
	// 事实核对：缓存坏了不影响结果，重复组照常报出来。
	if len(groups) != 1 {
		t.Errorf("缓存不可用时结果不得受影响：groups=%d want 1", len(groups))
	}
}
