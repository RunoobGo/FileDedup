package history

// 2026-09-20 缺陷：空列表返回 nil slice → JSON null → 前端白屏。
//
// 现象：全新的应用（历史库一条记录都没有）打开「记录 → 清理记录」，
// 页面**完全空白**——连「暂无清理记录」的提示卡片都不显示。
//
// 根因：Go 的 nil slice 序列化成 JSON 是 `null`，不是 `[]`。
//
//	var out []OpMeta     // 空表 → nil → null
//	（对比）out := make([]OpMeta, 0)   // 空表 → [] → []
//
// 前端 `stores/scan.ts` 把它直接赋给 `opList`，模板里 `!store.opList.length`
// 对 null 取 .length 抛 TypeError，**整个组件的渲染函数就此中断**——
// 所以页头、页签、"暂无记录"提示全都不渲染。用户看到的是一片空白，
// 而代码里那段提示其实一直都在。
//
// 为什么用 json.Marshal 断言而不是 `assert(x != nil)`：
// 缺陷的本质是**跨语言的序列化契约**被破坏，不是"变量恰好是 nil"。
// 直接断言 JSON 输出是 "[]"，钉死的正是前端真正依赖的那个东西。
// 将来若有人把结构改成自定义 MarshalJSON 或加 omitempty，这里会立刻报错。

import (
	"encoding/json"
	"testing"
)

// marshalIsEmptyArray 断言 v 序列化后恰好是 "[]"。
func marshalIsEmptyArray(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s 序列化失败: %v", name, err)
	}
	if got := string(b); got != "[]" {
		t.Fatalf("%s 的空列表应为 JSON \"[]\"（前端依赖它取 .length），实得 %s\n"+
			"这是 nil slice 被序列化成 null 的典型症状——前端拿到 null 后取 .length 会抛 TypeError，"+
			"整页渲染中断，用户看到空白页", name, got)
	}
}

// 空账本：ListOps 必须回 []。这是用户报的「清理记录页空白」的直接源头。
func TestListOpsEmptyReturnsJSONArray(t *testing.T) {
	s := testDB(t)
	ops, err := s.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 0 {
		t.Fatalf("空库不该有记录，实得 %d 条", len(ops))
	}
	marshalIsEmptyArray(t, "ListOps()", ops)
}

// 空历史库：ListScans 必须回 []。
// 同一个缺陷在扫描历史页是同样症状（那里也有空态提示，同样渲染不出来）。
func TestListScansEmptyReturnsJSONArray(t *testing.T) {
	s := testDB(t)
	scans, err := s.ListScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(scans) != 0 {
		t.Fatalf("空库不该有历史，实得 %d 条", len(scans))
	}
	marshalIsEmptyArray(t, "ListScans()", scans)
}

// 一笔操作若一条明细都没落账，GetOp 的 items 必须回 []。
//
// 这一处比 ListOps 更隐蔽：它只在"有记录、点开详情"时才出现，
// 所以用户报第一个缺陷时还遇不到。但它同样是整块渲染中断。
func TestGetOpEmptyItemsReturnsJSONArray(t *testing.T) {
	s := testDB(t)

	// 不落任何明细，只建一笔操作头。
	opID, err := s.BeginOp("trash", "", 0, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, items, err := s.GetOp(opID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("未落明细的操作不该有条目，实得 %d 条", len(items))
	}
	marshalIsEmptyArray(t, "GetOp() 的 items", items)
}

// 反向守卫：有记录时不能被上面的修复影响（列表仍按新→旧返回）。
// 只改"空"的形状，不改"非空"的顺序与内容。
func TestListOpsNonEmptyUnaffected(t *testing.T) {
	s := testDB(t)
	for i := 0; i < 3; i++ {
		if _, err := s.BeginOp("trash", "", 0, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	ops, err := s.ListOps()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 {
		t.Fatalf("应有 3 条记录，实得 %d", len(ops))
	}
	// 新→旧：id 递减
	for i := 1; i < len(ops); i++ {
		if ops[i-1].ID <= ops[i].ID {
			t.Fatalf("排序应为新→旧（id 递减），实得 %d 在 %d 之前", ops[i-1].ID, ops[i].ID)
		}
	}
}
