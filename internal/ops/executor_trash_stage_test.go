package ops

// M279 的第三半（登记行 ③）：同一抽屉里的失败清单，stage 归因必须一致。
//
// 真机读数（本轮重放，build/m7scratch/b9/mutation_t5_m279.txt）：同一批 12 项混批里
//   · 3 条假警报带 stage="trash"（executor.go 的 strict 臂显式给了）
//   · 3 条真失败带 stage 缺省 ⇒ UI 渲染成 "ops"
// 于是"同一批同类操作"在失败清单里被分成两种来源标签，用户按来源筛就漏一半。
// 修法只有一行，但**没有钉子就会再漂**：给 :669 补 stage 与给 :654 保持一致，
// 靠的是"两条臂同表"这一条断言，而不是靠人记得。

import (
	"errors"
	"os"
	"strings"
	"testing"

	"filededup/internal/model"
)

// trash 一律失败（批量与逐个都一样）：把两条失败臂同场摆出来。
func TestTrashFailureArmsReportSameStage(t *testing.T) {
	fx := newFixture(t)
	trash := func([]string) (map[string]string, error) {
		return nil, errors.New("SHFileOperation 错误码 32（模拟：同批有文件被别的进程占用）")
	}
	res := Execute(Options{
		Groups:               []*model.DuplicateGroup{fx.group},
		TrashFn:              trash,
		TrashVerifiesRecycle: true,
	}, model.OpRequest{Kind: "trash", FileIDs: []uint64{fx.dup1.ID, fx.dup2.ID}})

	if len(res.Failed) != 2 {
		t.Fatalf("失败项 = %d，期望 2（两条臂各一条）：%v", len(res.Failed), res.Failed)
	}
	var stages []string
	for _, f := range res.Failed {
		stages = append(stages, f.Path+" stage="+f.Stage)
	}
	for _, f := range res.Failed {
		if f.Stage != "trash" {
			t.Fatalf("回收站失败项的 stage = %q，期望 \"trash\"（M279 ③：同一批同类操作被分成两种来源标签）：\n%s",
				f.Stage, strings.Join(stages, "\n"))
		}
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatalf("夹具文件应仍在原处（这一臂钉的是'派发失败'）：%v", err)
		}
	}
}
