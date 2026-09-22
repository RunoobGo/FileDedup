package scanner

// M62+M85 计数链第一跳（设计稿 §28.2 ①）：**"本轮有 N 个根的卷语义未经实测"必须可见**。
//
// 为什么要有这个数（M62 的立项理由）：退平台默认是一个**猜**出来的结论。猜错的方向
// 已经由包注释写过两次（少扫一棵树 / 多算一份可释放空间），但改前它跟"实测出来的
// 结论"在返回值上完全同形 ⇒ 上层既不能警告用户，也不能自己收窄判据（M105 要的正是
// "只有确证不敏感才放宽"）。计数不是给用户看的装饰，是那条前置条件的载体。
//
// ★ 口径钉死在三格，缺一格就是假账：
//   - 只数**问卷过的**根（dedupeRoots 里那个 sens 圈），同一根只计一次；
//   - 无消费方时不探测 ⇒ 那里必须是 0（判重需要 ≥2 根；排除模式那条消费方是 M105 接上的，
//     见下面的口径收窄说明），而这个 0 的含义是"本轮没有需要卷语义的根"，
//     **不是**"该卷已实测"（下面第 4 格把这条边界钉住，免得将来有人拿它当后者用）；
//   - 被宽根覆盖而丢弃的子根照样计：它的折叠键参与过排序与判重，猜错的读数已经生效。
//
// 四格都在纯逻辑里（无 build tag、不碰真卷），三条平台都跑；卷型/探针本身的可信度
// 归 internal/fscase（三态取数与 M25-c/M25-d 变异在那边）。

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"filededup/internal/fscase"
	"filededup/internal/model"
)

// stubVerdict 换装 probeCaseVerdict，按根的**末段名**回答；返回调用次数计数器。
// 未列出的名字一律按"确证敏感"回答，免得漏配时静默走到另一条腿。
func stubVerdict(t *testing.T, byBase map[string]fscase.Result) *atomic.Int32 {
	t.Helper()
	restoreProbeSeam(t)
	var calls atomic.Int32
	probeCaseVerdict = func(_ context.Context, dir string) (fscase.Result, error) {
		calls.Add(1)
		if r, ok := byBase[filepath.Base(dir)]; ok {
			return r, nil
		}
		return fscase.Result{Sensitive: true, Proven: true}, nil
	}
	return &calls
}

func restoreProbeSeam(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { probeCaseVerdict = fscase.VerdictCtx })
}

func provenSensitive() fscase.Result   { return fscase.Result{Sensitive: true, Proven: true} }
func provenInsensitive() fscase.Result { return fscase.Result{Sensitive: false, Proven: true} }
func defaultedResult() fscase.Result {
	return fscase.Result{Sensitive: fscase.Default(), Proven: false}
}

// siblingRoots 造三棵互不包含、各带一个文件的真目录，返回绝对路径。
func siblingRoots(t *testing.T) []string {
	t.Helper()
	base := t.TempDir()
	out := make([]string, 0, 3)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		dir := filepath.Join(base, name)
		mkDirFiles(t, dir, name+".txt")
		out = append(out, dir)
	}
	return out
}

func TestDedupeRootsCountsUnprovenVerdicts(t *testing.T) {
	roots := siblingRoots(t)

	// ① 三根里两根退默认 ⇒ 2。谁未确证与"是否被保留"无关，三根都是兄弟、各自入 kept。
	calls := stubVerdict(t, map[string]fscase.Result{
		"alpha": provenSensitive(),
		"beta":  defaultedResult(),
		"gamma": defaultedResult(),
	})
	kept, _, unproven, _, _ := dedupeRoots(context.Background(), roots, false)
	if len(kept) != 3 {
		t.Fatalf("三棵互不包含的根应全保留，实得 %d：%v", len(kept), kept)
	}
	if unproven != 2 {
		t.Fatalf("两根退默认 ⇒ CaseProbeUnproven 应为 2，实得 %d", unproven)
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("三根应问卷 3 次，实得 %d（多问=白写探测文件，少问=有根没读到）", n)
	}

	// ② 全确证 ⇒ 0。这一格防的是"计数把问卷次数当未确证次数"那一类错位。
	stubVerdict(t, map[string]fscase.Result{
		"alpha": provenSensitive(), "beta": provenSensitive(), "gamma": provenInsensitive(),
	})
	if _, _, unproven, _, _ := dedupeRoots(context.Background(), roots, false); unproven != 0 {
		t.Fatalf("全部确证 ⇒ 应为 0，实得 %d", unproven)
	}

	// ③ 含被丢弃的子根：宽根 + 其子根，子根被覆盖丢弃，但它的折叠键参与过排序判重
	// ⇒ 照样计一次。少这一格，"只数 kept"的写法在本用例里看不出来。
	// ★ 两根给**同一个** Sensitive 值：M36 之后每个根按自己那卷折叠，一卷敏感一卷不敏感时
	// "/Tmp/X/sub" 与 "/Tmp/X" 折出来不同形、本来就不该并（那是 M36 的正确行为，不是覆盖失败），
	// 夹具要是拿那种形状断 kept==1 就红在前提上。这里要钉的是"丢弃不等于不计数"。
	base := t.TempDir()
	nested := []string{base, filepath.Join(base, "sub")}
	stubVerdict(t, map[string]fscase.Result{
		filepath.Base(base): defaultedResult(),
		"sub":               defaultedResult(),
	})
	kept, _, unproven, _, _ = dedupeRoots(context.Background(), nested, false)
	if len(kept) != 1 {
		t.Fatalf("子根应被宽根覆盖而丢弃，实得 kept=%v", kept)
	}
	if unproven != 2 {
		t.Fatalf("两根都退默认 ⇒ 必须各计一次，含随后被丢弃的那根，实得 %d", unproven)
	}
}

// TestWalkReportsUnprovenVerdictCount 钉第一跳的**出口**：计数必须挂到 Walk 的 Result 上。
// 只有 dedupeRoots 的返回值而没有赋值点，就是"引擎有数、账本零值"那一类假账（M21 同族）。
func TestWalkReportsUnprovenVerdictCount(t *testing.T) {
	roots := siblingRoots(t)
	stubVerdict(t, map[string]fscase.Result{
		"alpha": provenInsensitive(),
		"beta":  defaultedResult(),
		"gamma": provenSensitive(),
	})
	res := Walk(context.Background(), roots, &model.Filters{}, 2)
	if res.CaseProbeUnproven != 1 {
		t.Fatalf("三根里一根未确证 ⇒ Result.CaseProbeUnproven 应为 1，实得 %d", res.CaseProbeUnproven)
	}
}

// TestSingleRootWithoutExcludesDoesNotAskAndCountsZero 钉住 C1 那条边界在计数上的含义。
// 单根且**没有排除模式**时一趟探测都不发起（这是 C1 的原意：不给最常用的那条路往用户
// 目录写探测文件），于是本数只能是 0 —— 而 0 说的是"本轮没有消费方需要卷语义"，
// **不是**"这卷已实测"。
//
// ★ 2026-09-22 的适用范围收窄（M105，设计稿 §28.3 补记二）：本用例原来叫
// TestSingleRootDoesNotAskAndCountsZero、钉的是"单根一律不探测"。M105 正是本文件注释
// 预先点名的那个诱因（"将来若有人让单根也问卷……这一格会先红，逼他回来改口径"），
// 红过之后按预定程序把口径改成"**无排除模式**才不探测"：有排除模式时不探测就没有卷
// 语义读数，单根用户的排除永远不放宽，fail-open 那一格在最常见形态上原样留着。
// 反向那一格（有排除模式 ⇒ 恰好探测一次）在 scanner_m105_test.go，两格合起来才是新口径。
func TestSingleRootWithoutExcludesDoesNotAskAndCountsZero(t *testing.T) {
	roots := siblingRoots(t)
	calls := stubVerdict(t, map[string]fscase.Result{"alpha": defaultedResult()})
	_, _, unproven, _, _ := dedupeRoots(context.Background(), roots[:1], false)
	if n := calls.Load(); n != 0 {
		t.Fatalf("单根且无排除模式不得发起卷探测（C1）：实得 %d 次", n)
	}
	if unproven != 0 {
		t.Fatalf("未问卷 ⇒ 计数应为 0（含义见本用例注释），实得 %d", unproven)
	}
}
