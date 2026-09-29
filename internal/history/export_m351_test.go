package history

// M351 导出记录的判据（2026-09-29 设计段 §5.3）。
//
// 导出这个功能的全部意义是"以后还能导回来"，所以判据不能停在"文件生成了、大小不为 0"：
//   - `.db` 必须**被 history.Open 吃得动**，且五张表逐条等值（半本账也能长成一个大文件）；
//   - `.json` 必须能被 `json.Unmarshal`，条数与 `.db` 一致（两份产物各讲一个故事的导出没意义）；
//   - 两件必须来自**同一时刻的同一本账**（一个方法两产物，见 export.go 的取锁理由）。

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/model"
)

func newStoreAt(t *testing.T, dir, name string) *Store {
	t.Helper()
	s, err := Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedScan(t *testing.T, s *Store, root string, groups int) int64 {
	t.Helper()
	gs := mkGroups(groups) // 每组 3 个文件
	cfg := model.ScanConfig{Roots: []string{root}, Threads: 4}
	id, err := s.SaveScan(cfg, gs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// 判据 §5.3-1：导出的 .db 打得开、读得出，且与源库逐条等值。
func TestExportDBImageOpensAndMatchesSource(t *testing.T) {
	dir := t.TempDir()
	src := newStoreAt(t, dir, "src.db")
	seedScan(t, src, "/root/a", 2)
	seedScan(t, src, "/root/b", 1)
	opID, err := src.BeginOp("trash", "/root/a", 1, true,
		[]OpItemPlan{{OrigPath: "/root/a/a0.bin", Size: 1000, MtimeNs: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := src.FinishItem(opID, "/root/a/a0.bin", "", "", StateDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := src.FinalizeOp(opID, 1000); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "image.db")
	mirror, err := src.ExportTo(dest)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("影像没落盘: %v", err)
	}

	// ★ 用真实开库路径验，不是只 SELECT 一下：能导回的意思是 history.Open 吃它。
	im, err := Open(dest)
	if err != nil {
		t.Fatalf("M351：导出的 .db 导不回来（Open 失败）: %v", err)
	}
	defer im.Close()

	scans, err := im.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	wantScans, err := src.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != len(wantScans) || len(scans) != 2 {
		t.Fatalf("M351：影像里的扫描条数 %d，源库 %d，want 2", len(scans), len(wantScans))
	}
	for i := range scans {
		if scans[i].Groups != wantScans[i].Groups || scans[i].Files != wantScans[i].Files ||
			strings.Join(scans[i].Roots, "|") != strings.Join(wantScans[i].Roots, "|") {
			t.Errorf("M351：影像第 %d 条与源库不等值：img=%+v src=%+v", i, scans[i], wantScans[i])
		}
	}
	ops, err := im.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Done != 1 || ops[0].Kind != "trash" {
		t.Errorf("M351：影像里的清理记录不等值：%+v", ops)
	}
	// 明细也得在（只有父行的账回撤不了）
	meta, items, err := im.GetOp(ops[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || len(items) != 1 || items[0].State != StateDone {
		t.Errorf("M351：影像里的清理明细丢了：%+v %+v", meta, items)
	}

	// 镜像的条数必须与影像一致（同一把锁下的同一本账）
	if len(mirror.Scans) != 2 || len(mirror.Ops) != 1 {
		t.Errorf("M351：镜像条数与影像不符：scans=%d ops=%d", len(mirror.Scans), len(mirror.Ops))
	}
	if mirror.SchemaVersion != SchemaVersion {
		t.Errorf("M351：镜像没带 schemaVersion（=%d，本包为 %d）", mirror.SchemaVersion, SchemaVersion)
	}
	// 子表归位：每组 3 个文件
	for _, sc := range mirror.Scans {
		for _, g := range sc.Groups {
			if len(g.Files) != 3 {
				t.Errorf("M351：镜像里某组的文件数=%d want 3（子行没归到父行上）", len(g.Files))
			}
		}
	}
}

// 判据 §5.3-1 的 JSON 腿：镜像编得动、解得开，且解出来条数一致。
func TestExportMirrorIsParseableJSON(t *testing.T) {
	dir := t.TempDir()
	src := newStoreAt(t, dir, "src.db")
	seedScan(t, src, "/root/a", 1)

	dest := filepath.Join(dir, "image.db")
	mirror, err := src.ExportTo(dest)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(mirror, "", "  ")
	if err != nil {
		t.Fatalf("M351：镜像序列化失败，界面拿不到可核对的文件: %v", err)
	}
	var back Mirror
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("M351：镜像不是合法 JSON: %v", err)
	}
	if len(back.Scans) != len(mirror.Scans) || len(back.Ops) != len(mirror.Ops) {
		t.Errorf("M351：往返后条数变了：scans %d→%d ops %d→%d",
			len(mirror.Scans), len(back.Scans), len(mirror.Ops), len(back.Ops))
	}
	// roots 列是**搬运**的，不是"解码再重编"：字段序、空数组表示都不会分叉。
	// ★ 比的是 compact 后的内容而不是原始字节——app 层落盘用 MarshalIndent，
	//   encoding/json 会把嵌套的 RawMessage 一起重排空白（实测 `[` 换行缩进），
	//   这是格式化行为、不是这一列被改写；把"逐字节等于库内列"当断言会红在缩进上，
	//   而红在缩进上的判据拦不住真正要防的那件事（字段丢失/重排）。
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, back.Scans[0].Roots); err != nil {
		t.Fatalf("M351：roots 列不是合法 JSON: %v", err)
	}
	if compacted.String() != `["/root/a"]` {
		t.Errorf("M351：roots 列内容变了（=%s），搬运失真", compacted.String())
	}
	// 空清单必须是 [] 不是 null（前端/人读都会把 null 当"没数据"）
	if len(back.Ops) != 0 {
		t.Fatalf("夹具前提走样：%v", back.Ops)
	}
}

// 空账本也要能导：一次没清过的用户按「导出记录」得到的应该是成功+两份空文件，
// 而不是"没有可导出的内容"那种把成功说成失败的措辞。
func TestExportEmptyLedgerSucceeds(t *testing.T) {
	src := newStoreAt(t, t.TempDir(), "src.db")
	dest := filepath.Join(t.TempDir(), "image.db")
	mirror, err := src.ExportTo(dest)
	if err != nil {
		t.Fatalf("M351：空账本导不出来: %v", err)
	}
	if mirror.Scans == nil || mirror.Ops == nil {
		t.Errorf("M351：空清单编成了 null（应为 []）：scans=%v ops=%v", mirror.Scans, mirror.Ops)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("M351：空账本的影像没落盘: %v", err)
	}
}

// VACUUM INTO 的硬约束：目标已存在 ⇒ 直接失败。
// 这条不是多余的：app 层靠 tmp+rename 实现覆盖式导出，而"影像落位会覆盖别人文件"
// 在本仓是 P0 形状（§6.63），所以这里必须钉住"库层不覆盖"这一格。
func TestExportRefusesExistingDest(t *testing.T) {
	dir := t.TempDir()
	src := newStoreAt(t, dir, "src.db")
	seedScan(t, src, "/root/a", 1)
	dest := filepath.Join(dir, "image.db")
	if err := os.WriteFile(dest, []byte("占用这个名字的第三方文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.ExportTo(dest); err == nil {
		t.Fatal("M351：目标已存在却报告导出成功")
	}
	after, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("M351：导出失败时改动了目标文件（覆盖了第三方内容）")
	}
}

// 四列任一坏了都必须**当场点名是哪一列**并停止导出（M321 的口径反向用到导出上：
// 序列化/反序列化失败不许降级成"这一列没有内容"）。
//
// 逐列循环而不是只测 roots：变异实测（设计段 §12.6 MU-r）——把 `rawColumn` 里的
// `json.Valid` 拦截整段删掉，四项导出用例照样全绿，因为夹具没有一列是坏的。
// 一条 roots 的用例兜不住"另外三处漏写拦截"，所以这里按列名表跑一圈。
func TestExportRejectsCorruptColumn(t *testing.T) {
	cols := []string{"roots", "filters", "failed_json", "keep_paths"}
	for _, col := range cols {
		t.Run(col, func(t *testing.T) {
			dir := t.TempDir()
			src := newStoreAt(t, dir, "src.db")
			id := seedScan(t, src, "/root/a", 1)
			if _, err := src.db.Exec(`UPDATE scan_history SET `+col+` = '这不是 JSON' WHERE id = ?`, id); err != nil {
				t.Fatal(err)
			}
			_, err := src.ExportTo(filepath.Join(dir, "image.db"))
			if err == nil {
				t.Fatalf("M351：%s 列是坏 JSON 却报告导出成功（镜像里会带进一段非法 RawMessage）", col)
			}
			if !strings.Contains(err.Error(), col) {
				t.Errorf("M351：报错没点名坏掉的列 %s：%v", col, err)
			}
			if !strings.Contains(err.Error(), "不是合法 JSON") {
				t.Errorf("M351：报错没说是 JSON 坏掉：%v", err)
			}
		})
	}
}
