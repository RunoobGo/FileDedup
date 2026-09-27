package history

// M321（HIS-3，2026-09-28 第六轮全量审查）：JSON 列的编解码两头都在吞错误。
//
//   - 读侧 `decodeJSON` 是 `_ = json.Unmarshal(b, v)` ⇒ 坏行只报"解析成功 + 空集"。
//     症状：历史页点开某条记录说"这次扫描没有重复"，而同一条的文件数、可释放字节
//     都是非零——两格互相矛盾，用户无从判断该不该信这条记录。
//   - 写侧 `mustJSON` 失败时返回 `"[]"` ⇒ 一次序列化失败被降级成**空数组写进账本**，
//     SaveScan 照样记成功、UpdateKeepPaths 照样报已保存，真数据没了。
//     （旧实现在 filters 这一列留下的 "[]" 尤其难看：filters 是结构体，"[]" 从来就解不回去。）
//
// 判据（改前红可得）：读侧直接把某列写成非法 JSON（经同库句柄，绕过一切校验），
// 改前 LoadScan 返回 err=nil 且组数 0；写侧经 marshalJSON 接缝让序列化必失败，
// 改前 SaveScan 返回 nil 并**真的多出一行**（内容已空）。
// 变异：M321-a 把读侧的 error 再吞掉 ⇒ 读侧用例红；M321-b 让 encodeJSON 退回
// "失败就交回 []byte("[]")" ⇒ 两条写侧用例红。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// failMarshal 把包级接缝换成"必失败"，返回还原函数。
func failMarshal(t *testing.T, cause error) {
	t.Helper()
	orig := marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, cause }
	t.Cleanup(func() { marshalJSON = orig })
}

func TestM321LoadScanReportsUndecodableColumn(t *testing.T) {
	s := testDB(t)
	id, err := s.SaveScan(mkScanCfg(), mkGroups(2), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadScan(id); err != nil {
		t.Fatalf("前提：这条记录本来读得动: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE scan_history SET filters = ? WHERE id = ?`, "{not json", id); err != nil {
		t.Fatal(err)
	}

	_, groups, err := s.LoadScan(id)
	if err == nil {
		t.Fatal("filters 列已读不动，LoadScan 却报告成功（还交了空组集）——坏账本被说成\"这次扫描没有重复\"")
	}
	if !strings.Contains(err.Error(), "filters") || !strings.Contains(err.Error(), "无法解析") {
		t.Errorf("错误必须点名是哪一列读不动，实得: %v", err)
	}
	if !strings.Contains(err.Error(), "#") {
		t.Errorf("错误必须带历史记录号（用户据此才知道是哪一条）: %v", err)
	}
	if groups != nil {
		t.Errorf("读不动那一条不得交出组集（半条记录比报错更坏）: %d 组", len(groups))
	}
	// 列表页同一条记录也必须报错：把谎挡在"显示成空"之前。
	if _, err := s.ListScans(); err == nil {
		t.Error("ListScans 对坏行仍报告成功（同一列，同一个谎）")
	}
}

// 负控制：空列不是坏列。len(b)==0 走的是"这一列没有数据"，照旧放行。
func TestM321EmptyColumnsStillLoadAsNoData(t *testing.T) {
	s := testDB(t)
	id, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	// failed_json 是 NOT NULL 列，用 '[]'（"没有数据"的正常写法）；keep_paths 给空串，
	// 走 decodeJSON 的 len(b)==0 那一格——空列不是坏列。
	if _, err := s.db.Exec(`UPDATE scan_history SET failed_json = '[]', keep_paths = '' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	m, _, err := s.LoadScan(id)
	if err != nil {
		t.Fatalf("空列应读作\"没有数据\"而不是账本损坏: %v", err)
	}
	if len(m.Failed) != 0 || len(m.KeepPaths) != 0 {
		t.Errorf("空列解出了非零集合: failed=%d keep=%d", len(m.Failed), len(m.KeepPaths))
	}
}

func TestM321SaveScanRefusesToWriteUnencodablePayload(t *testing.T) {
	s := testDB(t)
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.ListScans()
	if err != nil {
		t.Fatal(err)
	}

	const cause = "json: unsupported type"
	failMarshal(t, errors.New(cause))
	if _, err := s.SaveScan(mkScanCfg(), mkGroups(3), nil); err == nil {
		t.Fatal("序列化失败却报告写入成功：真数据被降级成空数组落库（M321）")
	} else if !strings.Contains(err.Error(), "无法序列化") {
		t.Errorf("错误应说明是序列化失败，实得: %v", err)
	}
	// 一行都不许多：拒绝必须发生在开事务之前。
	after, err := s.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("被拒的写入仍留下了行（before=%d after=%d）：降级后的账本比不写更坏", len(before), len(after))
	}
}

func TestM321UpdateKeepPathsRefusesToWriteUnencodablePayload(t *testing.T) {
	s := testDB(t)
	id, err := s.SaveScan(mkScanCfg(), mkGroups(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateKeepPaths(id, []string{"/root/keep.bin"}); err != nil {
		t.Fatal(err)
	}

	failMarshal(t, errors.New("json: unsupported type"))
	if err := s.UpdateKeepPaths(id, []string{"/root/gone.bin"}); err == nil {
		t.Fatal("序列化失败却报告\"已保存\"：界面上显示生效、库里那行已被清空")
	}

	m, _, err := s.LoadScan(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.KeepPaths) != 1 || m.KeepPaths[0] != "/root/keep.bin" {
		t.Errorf("被拒的写入改动了既有保留集: %v", m.KeepPaths)
	}
}

// 接缝本身不是被测物，但它替换的是生产实现——留一条"没换时装的就是 json.Marshal"
// 的锚，免得将来有人把 marshalJSON 改成别的什么而全绿。
func TestM321MarshalJSONDefaultsToStdlib(t *testing.T) {
	b, err := marshalJSON([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `["a"]` {
		t.Fatalf("marshalJSON 默认实现不是 json.Marshal: %s", b)
	}
	var back []string
	if err := json.Unmarshal(b, &back); err != nil || len(back) != 1 || back[0] != "a" {
		t.Fatalf("默认实现往返不符: %v %v", back, err)
	}
}
