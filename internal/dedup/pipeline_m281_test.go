package dedup

// M281 的判据（登记原文见 docs/04 的 M281 行 / §6.48 二·三）。
//
// 缺陷形状（真机读数 W9-7）：外部进程持有未提交事务时，一轮扫描交回两条
// `Stage="cache"` 的失败项——`Path` **为空**，`Err` 是原文
// 「缓存写回失败: database is locked (5) (SQLITE_BUSY)」。在失败抽屉里
// 它们长成"两行没有路径的失败"，与真正的文件操作失败混在同一个「失败项 n」计数里
// ⇒ 用户既不知道是哪个库、也不知道跟自己的文件有没有关系（证据 b5_imm_cli.json、
// b5_busytext.txt）。
//
// 取向两条，各自一条判据：
//   ① `Path` 带**可定位信息**（缓存库文件的路径，抽屉里的"定位"按钮因此有落点）；
//   ② 文案先讲清这是**降级**（本轮结果不受影响），再附系统原文（取证要按原文对表）。
//      SQLite 的英文内部串不得作为句子的主语直达用户。
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

// ② 真机那一档：`database is locked` 必须被译成中文归因，原文只能待在尾注里。
func TestM281BusyIsAttributedNotRaw(t *testing.T) {
	got := cacheWriteBackErrText("缓存写回", errors.New("database is locked (5) (SQLITE_BUSY)"))
	if !strings.Contains(got, "占用") {
		t.Fatalf("SQLITE_BUSY 仍是裸串直达用户：%q", got)
	}
	if !strings.Contains(got, "不影响本次") {
		t.Fatalf("没说清这是降级（本轮结果不受影响）：%q", got)
	}
	// 原文留档：本仓所有真机读数按原文/数字归档，译文会随版本改。
	if !strings.Contains(got, "SQLITE_BUSY") {
		t.Fatalf("系统原文被丢掉，取证无从对表：%q", got)
	}
	// 原文必须在**中文主语之后**（尾注），不能顶在句子开头。
	if strings.Index(got, "database is locked") < strings.Index(got, "：") {
		t.Fatalf("英文原文仍是句子主语：%q", got)
	}
	// 占用 ≠ 损坏：不得蹭停用/永久删除措辞（M279 那类假警报的成因）。
	for _, lie := range []string{"损坏", "已停用", "永久删除", "数据丢失"} {
		if strings.Contains(got, lie) {
			t.Fatalf("占用被报成了 %q：%q", lie, got)
		}
	}
}

// ② 两档的后果**不同**，共用一句就成了假话：
// 写回失败 ⇒ 这批哈希没进库，下次要重算；
// 续期失败 ⇒ 条目还在库里（哈希没丢），只是 LRU 以为它冷门、可能被提前淘汰。
func TestM281DegradationConsequenceFollowsTheOperation(t *testing.T) {
	store := cacheWriteBackErrText("缓存写回", errors.New("database is locked (5) (SQLITE_BUSY)"))
	if !strings.Contains(store, "重算") {
		t.Fatalf("写回档没说清「下次要重算」：%q", store)
	}
	if strings.Contains(store, "LRU 的「最近用过」") {
		t.Fatalf("写回档套用了续期档的后果（两件事不是一回事）：%q", store)
	}
	touch := cacheWriteBackErrText("缓存命中续期", errors.New("database is locked (5) (SQLITE_BUSY)"))
	if !strings.Contains(touch, "LRU") {
		t.Fatalf("续期档没说清后果是 LRU 记账未刷新：%q", touch)
	}
	if strings.Contains(touch, "哈希没进库") || strings.Contains(touch, "要重算") {
		t.Fatalf("续期档套用了写回档的后果（条目其实已经在库里）：%q", touch)
	}
}

// ② 归因表只收**有据**的档；认不出来的错误不得配自信原因（M280 同一纪律）。
func TestM281UnknownErrorKeepsRawAndRefusesToGuess(t *testing.T) {
	got := cacheWriteBackErrText("缓存写回", errors.New("某种没见过的盘错 (42)"))
	if !strings.Contains(got, "某种没见过的盘错") {
		t.Fatalf("原文被丢掉：%q", got)
	}
	if strings.Contains(got, "占用") || strings.Contains(got, "磁盘空间不足") {
		t.Fatalf("给认不出的错误配了猜来的原因：%q", got)
	}
	if !strings.Contains(got, "不影响本次") {
		t.Fatalf("兜底档也没说清降级：%q", got)
	}
}

// ② 淘汰那一档的既有事实不得被统一措辞冲掉：条目**已经写回**（M75(a) 的取证 #8）。
// 修这条最容易犯的错就是把三档又折成一档。
func TestM281KeepsEvictArmFacts(t *testing.T) {
	evict := fmt.Errorf("%w：哈希条目已写回，仅 LRU 淘汰未完成（探针）", cache.ErrEvictFailed)
	got := cacheWriteBackErrText("缓存写回", evict)
	if !strings.Contains(got, "已写回") {
		t.Fatalf("丢掉了「已写回」这个事实：%q", got)
	}
	if strings.HasPrefix(got, "缓存写回失败") {
		t.Fatalf("淘汰失败又被写成写回失败（Commit 早已成功）：%q", got)
	}
	if !strings.Contains(got, "不影响本次") {
		t.Fatalf("淘汰档没说清降级：%q", got)
	}
	// 这一档的**主语**是淘汰，不是写回。变异预判：把三档折回一档时，
	// 兜底档的尾注仍会带上哨兵原文里的"已写回"，只钉那三个字挡不住合并。
	if !strings.Contains(got, "淘汰") {
		t.Fatalf("淘汰档被写成别的档（主语丢了）：%q", got)
	}
	if strings.Contains(got, "没有进库") || strings.Contains(got, "要重算") {
		t.Fatalf("淘汰档套用了写回档的后果（哈希其实已经落库）：%q", got)
	}
	// 停用态仍然不在这里造句（轮末那条统一说明才是它的出口）——这条边界一寸未动。
	if _, ok := cacheOpErrText("缓存写回", cache.ErrCorruptDisabled); ok {
		t.Fatal("停用态不该再报一条写回失败")
	}
}

// ① 上报形状：`cache` 档的失败项必须带**可定位的库文件路径**。
//
// 手段用"库已关闭"——它与真机那档（外部持事务）走的是同一条出口，但可在任何平台复现。
// 断言落点很具体：`Path` 必须等于真实的 `.db` 路径且存在（空串是修前的形状；
// 把"缓存"这类子系统名塞进 Path 也不算过——抽屉里的「在文件夹中显示」要的是路径）。
func TestM281CacheFailureItemsCarryDatabasePath(t *testing.T) {
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
	dbPath := filepath.Join(t.TempDir(), "c.db")
	cch, err := cache.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := cch.Close(); err != nil {
		t.Fatal(err)
	}
	_, failed, err := New().WithCache(cch).Run(context.Background(),
		model.ScanConfig{Roots: []string{root}, UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, f := range failed {
		if f.Stage != "cache" {
			continue
		}
		n++
		if f.Path == "" {
			t.Fatalf("cache 失败项的 Path 又是空的（M281 的原形状）：%+v", f)
		}
		if got := filepath.Clean(f.Path); got != filepath.Clean(dbPath) {
			t.Fatalf("Path 指向 %q，期望缓存库 %q", got, dbPath)
		}
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatalf("Path 定位不了：%v ⇒「在文件夹中显示」会指向一个不存在的位置", err)
		}
		if !strings.Contains(f.Err, "不影响本次") {
			t.Fatalf("轮末上报仍是旧文案（没有降级语义）：%q", f.Err)
		}
	}
	if n == 0 {
		t.Fatal("一轮库已关闭的扫描没报出任何 cache 失败项 ⇒ 本条测试失效（判据没被走到）")
	}
}

// ① 的另一半：轮末那条"确证损坏 ⇒ 停用"的说明同样得带库路径（M74 的造句点）。
// 它报的是"这个库不再生效"，用户要找的正是那个文件。
func TestM281CorruptNoticeItemCarriesDatabasePath(t *testing.T) {
	p := New()
	item := p.cacheStageItem("测试说明")
	if item.Stage != "cache" {
		t.Fatalf("Stage 错：%+v", item)
	}
	if item.Path != "" {
		t.Fatalf("没挂缓存库时不得凭空造出路径：%+v", item)
	}
	// 挂上库之后必须带路径（defer 里的三个造句点共用这一条构造器）。
	cch, err := cache.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	p = New().WithCache(cch)
	got := p.cacheStageItem("测试说明")
	if got.Path != cch.DBPath() || got.Path == "" {
		t.Fatalf("cache 档条目没带库路径：%+v（库在 %q）", got, cch.DBPath())
	}
}
