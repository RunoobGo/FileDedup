package ops

// M265 / M269 / M270 的**平台无关**判据腿（无 build tag ⇒ 跑进 Linux CI 主门禁）。
//
// 三条命题，各自对应一条真机读数（引用时必带"CI 腿绿不折算真机读数"这条纪律）：
//   ① M265 + M269：`SHFileOperationW` 这一腿在**任何** `LongPathsEnabled` 取值下都在
//      **260 个 UTF-16 码元**处静默丢文件（259 进站 / 260 丢弃，九档同一边界）。
//      所以量纲必须是码元，不是字节、不是 rune——第一批用字节量出"355 才失效"就是错觉。
//   ② M270：判据 2 在取不到卷读数的卷上**不得静默旁路**。旁路的那几卷要交给降级腿，
//      降级腿的判据是"逐条 `$I` 对账"，而"对不上账"与"根本没看见回收站目录"必须分开报。
//   ③ 判据 2 有基准的卷行为一字不变（本文件最后那条是**反向钉**：防我把修复做过头，
//      把有基准的卷也拖进降级腿，那等于把 M197 那套计数判据废掉）。
import (
	"strings"
	"testing"
	"unicode/utf16"
)

// m265Units 是本文件自带的**尺子**，刻意不复用生产的 `pathUnits`：
// 判据的全部风险就在"用了哪把尺子"，拿被测函数自己证明自己没错是循环论证。
// 这里的定义直接抄 `unicode/utf16` 的语义（BMP 字符 1 码元、非 BMP 2 码元），
// 下面每一档的期望值都是按这条定义**手算**出来的，不是跑出来回填的。
func m265Units(s string) int { return len(utf16.Encode([]rune(s))) }

// ① 边界必须钉在 259 通过 / 260 拒绝——真机九档（层数 2/10/20、单分量 9~97）全落这条线。
func TestM265LongPathRejectedAt260Units(t *testing.T) {
	for _, n := range []int{1, 100, 258, 259} {
		if why := shellPathDropReason(n); why != "" {
			t.Fatalf("%d 码元被误拒（真机 259 是**能进站**的）：%s", n, why)
		}
	}
	for _, n := range []int{260, 261, 302, 1066} {
		if why := shellPathDropReason(n); why == "" {
			t.Fatalf("%d 码元被放行 ⇒ 静默永久删除又回来了", n)
		}
	}
}

// ① 的量纲半边：同一串路径，字节数 / rune 数 / 码元数三把尺子只有码元这一把对得上真机。
//
// 真机读数：带中文段名那两组 **355 字节＝259 码元 → 进站**、**356 字节＝260 码元 → 丢弃**
// （211 个 ASCII + 48 个汉字：字节 = 211+48×3 = 355，码元 = 211+48 = 259）。
// 若判据用 `len(p)`（字节），汉字路径会在 260 **字节**处就被拒——那是**误拒**（真机能进站）；
// 若用 rune 数，130 个 emoji（260 码元、真机必须丢弃）只有 130 rune ⇒ **漏拒**。
func TestM265UnitIsUTF16CodeUnits(t *testing.T) {
	// (a) 复现真机那一对的形状：211 ASCII + 48 汉字 = 355 字节 / 259 码元 → 放行。
	cn := strings.Repeat("a", 211) + strings.Repeat("文", 48)
	if len(cn) != 355 {
		t.Fatalf("夹具字节数错：%d（应 355，与真机同形）", len(cn))
	}
	if got := m265Units(cn); got != 259 {
		t.Fatalf("夹具码元数错：%d（应 259）", got)
	}
	if why := longPathDropReasonFor(cn); why != "" {
		t.Fatalf("355 字节 / 259 码元被拒 ⇒ 判据内部用成了字节尺子（真机这一格是能进站的）：%s", why)
	}
	// (b) 再加一枚 ASCII = 356 字节 / 260 码元 → 必须拒。a/b 两点合起来才排除字节尺子：
	// 只看 (a) 时"字节尺子"与"码元尺子"都在 355 处放行，看不出差别。
	if why := longPathDropReasonFor(cn + "x"); why == "" {
		t.Fatal("356 字节 / 260 码元被放行（真机这一格已静默删除）")
	}
	// (c) rune 尺子的排除点：130 个 emoji = 520 字节 / 130 rune / **260 码元** → 必须拒。
	emoji := strings.Repeat("😀", 130)
	if len([]rune(emoji)) != 130 {
		t.Fatalf("夹具 rune 数错：%d（应 130）", len([]rune(emoji)))
	}
	if got := m265Units(emoji); got != 260 {
		t.Fatalf("emoji（代理对）的码元数算错：%d（应 260，每枚 2 码元）", got)
	}
	if why := longPathDropReasonFor(emoji); why == "" {
		t.Fatal("130 rune / 260 码元被放行 ⇒ 判据内部用成了 rune 尺子")
	}
	// (d) 同一把尺子在边界内侧也要成立：129 枚 emoji = 258 码元 → 放行。
	// 缺了这条，"`- 1` 之类的偏移把边界推歪一格"这种错只能靠 (c) 的拒侧发现。
	if why := longPathDropReasonFor(strings.Repeat("😀", 129)); why != "" {
		t.Fatalf("258 码元（129 rune / 516 字节）被拒：%s", why)
	}
	// (e) 纯汉字不能比 ASCII 更早失效：129 汉字 = 387 字节 / 129 码元 → 放行。
	if why := longPathDropReasonFor(strings.Repeat("文", 129)); why != "" {
		t.Fatalf("387 字节 / 129 码元的纯汉字路径被拒（字节尺子的另一种露馅形状）：%s", why)
	}
}

// ① 文案三件事：说清后果（静默永久删除）、给出路（改用移动或自行确认）、
// 并且**不许**把用户支去改 `LongPathsEnabled`——真机 `=1` 态逐格同读数。
func TestM265ReasonTextNamesConsequenceAndExit(t *testing.T) {
	why := shellPathDropReason(300)
	for _, want := range []string{"永久删除", "移动"} {
		if !strings.Contains(why, want) {
			t.Fatalf("拒绝原因缺 %q：%s", want, why)
		}
	}
	if strings.Contains(why, "LongPathsEnabled") {
		t.Fatalf("把用户支去开注册表，而真机证明开了也一样：%s", why)
	}
	if !strings.Contains(why, "259") || !strings.Contains(why, "260") {
		t.Fatalf("拒绝原因没给出边界数字，用户无从判断该缩短多少：%s", why)
	}
}

// ② 只有"预期入站 > 0 且快照里缺基准"的卷才算被旁路。规则取**两侧都要有**：
// `checkRecycled` 的两条 `continue` 分别是"事前没基准"与"事后查不到"，两者都会把
// 该卷整卷跳过 ⇒ 都属旁路。expected 里压根没有的卷不进来（无从判断该期待几条）。
func TestM270BypassedVolumesSelectsOnlyNoBaselineOnes(t *testing.T) {
	expected := RBState{`C:\`: 3, `D:\`: 2, `E:\`: 1}
	before := RBState{`C:\`: 10}
	after := RBState{`C:\`: 13, `D:\`: 0}
	got := bypassedVolumes(expected, before, after)
	if len(got) != 2 || got[0] != `D:\` || got[1] != `E:\` {
		t.Fatalf("旁路卷集合错：%v（应 [D:\\ E:\\]：C 两侧齐全，D 缺事前基准，E 两侧都缺）", got)
	}
	// 单卷、两侧全缺：仍必须进降级腿（这就是 W1-10 那次的形状 before=map[] after=map[]）。
	got2 := bypassedVolumes(RBState{`E:\`: 1}, RBState{}, RBState{})
	if len(got2) != 1 || got2[0] != `E:\` {
		t.Fatalf("两趟快照全缺时该卷必须进降级腿，实得 %v", got2)
	}
	// ★ 变异反证补的格：事前有基准、**事后查不到**的卷同样被 `checkRecycled` 第二条
	// `continue` 整卷跳过，因此也属旁路。少了这一格，"只看事前快照"的实现能蒙混过关。
	gotAfterMissing := bypassedVolumes(RBState{`I:\`: 1}, RBState{`I:\`: 5}, RBState{})
	if len(gotAfterMissing) != 1 || gotAfterMissing[0] != `I:\` {
		t.Fatalf("事后快照缺该卷时也必须进降级腿，实得 %v", gotAfterMissing)
	}
	// ★ 反向钉：expected 为空的卷不得进降级腿（它无从对账，硬塞只会造假警报）。
	if got3 := bypassedVolumes(RBState{`F:\`: 0}, RBState{}, RBState{}); len(got3) != 0 {
		t.Fatalf("预期 0 的卷被拖进降级腿：%v", got3)
	}
	// ★ 反向钉二：顺序必须稳定（同一输入两次调用给出同一句错误，否则日志无法比对）。
	big := RBState{`Z:\`: 1, `Y:\`: 1, `X:\`: 1}
	first := bypassedVolumes(big, RBState{}, RBState{})
	second := bypassedVolumes(big, RBState{}, RBState{})
	if strings.Join(first, "|") != `X:\|Y:\|Z:\` || strings.Join(first, "|") != strings.Join(second, "|") {
		t.Fatalf("旁路卷列表未排序或不可复现：%v vs %v", first, second)
	}
}

// ② 降级腿的三态：账对得上 → 通过；对不上 → 响亮失败；**根本没看见回收站** → 报"复核不可用"。
// 后两者都是错误，但归因不同：一个是"疑似数据丢失"，一个是"这台机器给不出证据"。
// 混成一句就是把 M265 的失明换个地方继续犯。
func TestM270EvidenceDegradationThreeStates(t *testing.T) {
	exp := RBState{`G:\`: 3}

	if err := checkRecycledByEvidence(exp, []string{`G:\`}, RBState{`G:\`: 3},
		map[string]bool{`G:\`: true}); err != nil {
		t.Fatalf("逐条对上了账却仍报错：%v", err)
	}
	// 证据**超出**期待也算通过：同一批里混有用户自己删过的历史条目不能反过来判我们失败。
	if err := checkRecycledByEvidence(exp, []string{`G:\`}, RBState{`G:\`: 5},
		map[string]bool{`G:\`: true}); err != nil {
		t.Fatalf("证据多于期待被误判：%v", err)
	}

	missing := checkRecycledByEvidence(exp, []string{`G:\`}, RBState{`G:\`: 1},
		map[string]bool{`G:\`: true})
	if missing == nil {
		t.Fatal("3 条预期只有 1 条有进站痕迹 ⇒ 必须报错（M270 的原始形状是 err=<nil>）")
	}
	if !strings.Contains(missing.Error(), "永久删除") {
		t.Fatalf("对不上账时没说清后果：%v", missing)
	}
	if !strings.Contains(missing.Error(), "缺 2 条") {
		t.Fatalf("对不上账时没报缺几条，用户无法核对：%v", missing)
	}

	// ★ 变异反证补的格：**只差一条**没对上账也必须报。缺这格时，把判据写成
	// `got < want-1`（少算一条）也能通过 3→1 那种"差得很多"的例子。
	offByOne := checkRecycledByEvidence(exp, []string{`G:\`}, RBState{`G:\`: 2},
		map[string]bool{`G:\`: true})
	if offByOne == nil {
		t.Fatal("3 条预期只有 2 条有痕迹 ⇒ 必须报错（缺的就是那一条静默删除）")
	}
	if !strings.Contains(offByOne.Error(), "缺 1 条") {
		t.Fatalf("只差一条时报错没写出「缺 1 条」：%v", offByOne)
	}

	unavailable := checkRecycledByEvidence(exp, []string{`G:\`}, RBState{}, map[string]bool{})
	if unavailable == nil {
		t.Fatal("连回收站目录都没读到却判定通过 ⇒ 又是一次静默旁路")
	}
	if !strings.Contains(unavailable.Error(), "复核不可用") {
		t.Fatalf("取证通道本身失效时必须说「复核不可用」，不许写成数据丢失：%v", unavailable)
	}
	// ★ 关键区分：这一条**不许**出现"永久删除"——我们没资格这么说，只是看不见证据。
	if strings.Contains(unavailable.Error(), "永久删除") {
		t.Fatalf("把「取不到证据」报成「数据已丢」就是 M279 那类假警报：%v", unavailable)
	}

	// ★ 反向钉：预期 0 的卷（本轮没有文件落在它上面）不得由降级腿报错——
	// 扫描多卷是常态，其余卷读不到目录与本轮无关。
	if err := checkRecycledByEvidence(RBState{`G:\`: 3}, []string{`G:\`, `H:\`},
		RBState{`G:\`: 3}, map[string]bool{`G:\`: true}); err != nil {
		t.Fatalf("把预期 0 的 H:\\ 卷也算进降级腿：%v", err)
	}
}

// ③ 反向钉：判据 2 有基准的卷**不经过**降级腿——`checkRecycled` 的行为一字未动。
// 没有这条，"我加了降级腿"与"我把原判据换掉了"在测试里长得一模一样。
func TestM270KeepsBaselineVolumeJudgement(t *testing.T) {
	if err := checkRecycled(nil, RBState{`H:\`: 2}, RBState{`H:\`: 5}, RBState{`H:\`: 6}); err == nil {
		t.Fatal("增量不足本该报错（回归）")
	}
	if err := checkRecycled(nil, RBState{`H:\`: 2}, RBState{`H:\`: 5}, RBState{`H:\`: 7}); err != nil {
		t.Fatalf("增量满足却报错（回归）：%v", err)
	}
	// 无基准的卷在 checkRecycled 里仍然**不报**——它的报错来自降级腿，两处不得重复计数。
	if err := checkRecycled(nil, RBState{`H:\`: 2}, RBState{}, RBState{}); err != nil {
		t.Fatalf("无基准卷不该由判据 2 报：%v", err)
	}
}
