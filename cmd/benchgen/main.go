// benchgen 合成基准数据集生成器（04 M1-T12，01 §12.2）。
//
// 数据集 A：小文件海（默认 10 万个 1KB~256KB，10% 重复）
// 数据集 C：混合真实感（A + 中等文件 1~16MB + 大文件 512MB~1GB）
// -scale 缩放系数（冒烟 0.001），-seed 固定随机种子（内容同样由 seed 派生，见
// writeRandom），输出 manifest.json 记录期望重复组（E2E 断言依据）。
//
// 除数据集外还固定追加 shapes/ 边界形态（AS-K6）：硬链接对、符号链接、
// .fdd-old 残留、隐藏同名内容、大小写同名对。理由见 genShapes 的注释。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	mrand "math/rand"
	"os"
	"path/filepath"

	"filededup/internal/fscase"
	"filededup/internal/worktemp"
)

type manifest struct {
	Dataset string `json:"dataset"`
	Seed    int64  `json:"seed"`
	// TotalFiles 是「扫描器应采集到的文件数」，不是「benchgen 写出的文件数」。
	// 符号链接、工作临时名、隐藏文件、0 字节都在写出的同时**主动不计数**，
	// 因此冒烟的 files_total 对账（scripts/smoke-cli.sh）一旦变红，说明的是
	// 扫描规则或这份口径漂移了——两者都是必须发现的回归。
	TotalFiles int      `json:"total_files"`
	Groups     []mgroup `json:"groups"`
	// Shapes 记录边界形态的**实际**建立情况。
	//
	// 为什么要进 manifest：造不出来的形态必须与"造出来了"可区分，否则门禁会把
	// 「本轮未覆盖」读成「覆盖且通过」（AS-K3 处理冒烟跳过时踩过的同一类坑）。
	Shapes shapes `json:"shapes"`
}

// shapes 各边界形态是否真的建出来了（false = 本环境/本卷语义下不存在）。
type shapes struct {
	// Symlink false = os.Symlink 被拒（Windows 需开发者模式或特权）。
	Symlink bool `json:"symlink"`
	// CasePair false = 卷不区分大小写；那里"同名大小写对"根本不是两个文件。
	CasePair bool `json:"case_pair"`
}

type mgroup struct {
	Size  uint64   `json:"size"`
	Files []string `json:"files"` // 相对 out 的路径
}

// 边界形态的文件名与所在子目录。单一来源：main 与测试都用它们，且
// residualName 直接由 worktemp 注册表拼出——语料里的"临时名"必须与
// 应用真正忽略的那个名字同源，否则测的是不存在的形态。
const (
	shapeSubdir   = "shapes"
	shapeSize     = 4096
	hardAName     = "hard_a.bin"
	hardBName     = "hard_b.bin"
	symTargetName = "sym_target.bin"
	symLinkName   = "sym_link.bin"
	keepName      = "keep.bin"
	residualName  = keepName + worktemp.SuffixOld
	hiddenName    = ".hidden_of_group.bin"
	caseLowerName = "case_pair.bin"
	caseUpperName = "CASE_PAIR.BIN"
)

func main() {
	dataset := flag.String("dataset", "A", "A=小文件海, B=大文件, C=混合")
	out := flag.String("out", "./benchdata", "输出目录")
	scale := flag.Float64("scale", 1.0, "规模系数（1.0=标准，冒烟 0.001）")
	seed := flag.Int64("seed", 42, "随机种子")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	m := generate(*out, *dataset, *scale, *seed)

	mb, _ := json.MarshalIndent(&m, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "manifest.json"), mb, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("生成完成: %s 数据集=%s 文件=%d 期望重复组=%d 形态[symlink=%v case_pair=%v]\n",
		*out, *dataset, m.TotalFiles, len(m.Groups), m.Shapes.Symlink, m.Shapes.CasePair)
}

// generate 按数据集+规模+种子铺设语料，返回与磁盘状态同源的 manifest。
// 抽成函数（而非留在 main 里）是为了让 cmd/benchgen 的回归测试跑**同一条**路径。
func generate(out, dataset string, scale float64, seed int64) manifest {
	rng := mrand.New(mrand.NewSource(seed))
	m := manifest{Dataset: dataset, Seed: seed}

	switch dataset {
	case "A":
		genA(out, scale, rng, &m)
	case "B":
		genLarge(out, scale, rng, &m) // 大文件专项：512MB~1GB，30% 重复（01 §12.2）
	case "C":
		genA(out, scale, rng, &m)     // 小文件部分
		genMid(out, scale, rng, &m)   // 中等文件
		genLarge(out, scale, rng, &m) // 大文件
	default:
		fail(fmt.Errorf("未知数据集 %s（支持 A/B/C）", dataset))
	}
	genShapes(out, rng, &m) // 与 scale 无关，恒为个位数文件
	return m
}

// genA 小文件海：n 个随机小文件，其中 10% 复制出副本形成重复组。
func genA(out string, scale float64, rng *mrand.Rand, m *manifest) {
	n := int(100000 * scale)
	if n < 10 {
		n = 10
	}
	files := make([]string, 0, n)
	for i := 0; i < n; i++ {
		size := 1024 + rng.Intn(255*1024) // 1KB ~ 256KB
		p := filepath.Join(out, "small", fmt.Sprintf("f%06d.bin", i))
		if err := writeRandom(p, int64(size), rng); err != nil {
			fail(err)
		}
		files = append(files, p)
		m.TotalFiles++
	}
	// 10% 文件复制 1~2 份
	// 源按序独占分配：保证组之间内容互斥，manifest 记录与物理分组一致
	// （否则两个组若随机命中同一源，物理上会合并为一组，manifest 与实际不符）
	dupCount := n / 10
	for i := 0; i < dupCount; i++ {
		src := files[i]
		g := mgroup{}
		if err := readManifestSize(src, &g.Size); err != nil {
			fail(err)
		}
		g.Files = append(g.Files, rel(out, src))
		copies := 1 + rng.Intn(2)
		for c := 0; c < copies; c++ {
			dst := filepath.Join(out, "small", fmt.Sprintf("d%06d_%d.bin", i, c))
			if err := copyFile(dst, src); err != nil {
				fail(err)
			}
			g.Files = append(g.Files, rel(out, dst))
			m.TotalFiles++
		}
		m.Groups = append(m.Groups, g)
	}
}

// genMid 中等文件 1~16MB，重复率 ~8%。
func genMid(out string, scale float64, rng *mrand.Rand, m *manifest) {
	n := int(2000 * scale)
	if n < 4 {
		n = 4
	}
	for i := 0; i < n; i++ {
		size := 1<<20 + rng.Intn(15<<20) // 1MB ~ 16MB
		p := filepath.Join(out, "mid", fmt.Sprintf("m%05d.bin", i))
		if err := writeRandom(p, int64(size), rng); err != nil {
			fail(err)
		}
		m.TotalFiles++
		if i%12 == 0 { // ~8% 重复
			dst := filepath.Join(out, "mid", fmt.Sprintf("mc%05d.bin", i))
			if err := copyFile(dst, p); err != nil {
				fail(err)
			}
			m.TotalFiles++
			m.Groups = append(m.Groups, mgroup{Size: uint64(size), Files: []string{rel(out, p), rel(out, dst)}})
		}
	}
}

// genLarge 大文件 512MB~1GB，30% 重复（冒烟 scale 下已缩至 KB 级，仅验证路径）。
func genLarge(out string, scale float64, rng *mrand.Rand, m *manifest) {
	n := int(100 * scale)
	if n < 2 {
		n = 2
	}
	for i := 0; i < n; i++ {
		size := int64(512<<20 + rng.Intn(512<<20)) // 512MB ~ 1GB
		size = int64(float64(size) * scale)        // 冒烟缩放
		if size < 200<<10 {
			size = 200<<10 + int64(i)
		}
		p := filepath.Join(out, "large", fmt.Sprintf("L%04d.bin", i))
		if err := writeRandom(p, size, rng); err != nil {
			fail(err)
		}
		m.TotalFiles++
		if i%3 == 0 { // 30% 重复
			dst := filepath.Join(out, "large", fmt.Sprintf("Lc%04d.bin", i))
			if err := copyFile(dst, p); err != nil {
				fail(err)
			}
			m.TotalFiles++
			m.Groups = append(m.Groups, mgroup{Size: uint64(size), Files: []string{rel(out, p), rel(out, dst)}})
		}
	}
}

// writeRandom 用 -seed 派生的 rng 填满 size 字节。
//
// ★ 2026-09-20（AS-K6）：修正前这里用 crypto/rand，内容**不受 seed 控制**，
// 「-seed 固定随机种子」只对尺寸/副本数成立。后果不只是名字骗人：冒烟断言的
// 是分组路径集合摘要，内容每次不同意味着这条基线跨运行不可复现，历史结论
// 无法复算，也谈不上"同一 seed 生成同一语料"的确定性检查。
//
// 用的是 math/rand：这里的随机是**合成语料填充字节**，不承担任何安全职责，
// 可复现性才是它的需求。
func writeRandom(p string, size int64, rng *mrand.Rand) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	total := int64(0)
	for total < size {
		n := int64(len(buf))
		if size-total < n {
			n = size - total
		}
		if _, err := rng.Read(buf[:n]); err != nil {
			return err
		}
		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
		total += n
	}
	return nil
}

// genShapes 追加「扫描器/流水线对它各有专门规则」的边界形态。
//
// 为什么必须混进同一份语料、而不是只留在单测里：单测验证的是规则函数本身，
// 冒烟跑的是真扫描器 + 真流水线 + 缓存三跑。只有语料里真的有这些形态，
// 「manifest 的组数/语料口径 == 实际结论」这条对账才可能在规则失效时变红。
// 修正前整条 CI 里这些形态一个都没出现过。
//
// 每个形态的"应计数"口径都写在旁边，与 manifest.TotalFiles 的注释同源；
// 计数断言由 cmd/benchgen 的测试独立复算一遍（不信任这里的自增）。
func genShapes(out string, rng *mrand.Rand, m *manifest) {
	dir := filepath.Join(out, shapeSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}

	// ① 硬链接对：两条路径都采集（计 2），但流水线阶段 1.5 按文件身份去重 → 不成组。
	// 建不出来即致命：静默降级等于悄悄撤掉这条防线。
	hardA := filepath.Join(dir, hardAName)
	if err := writeRandom(hardA, shapeSize, rng); err != nil {
		fail(err)
	}
	hardB := filepath.Join(dir, hardBName)
	if err := clearForRecreate(hardB); err != nil {
		fail(err)
	}
	if err := os.Link(hardA, hardB); err != nil {
		fail(fmt.Errorf("建立硬链接失败（阶段 1.5 去重形态无法覆盖，不做静默跳过）: %w", err))
	}
	m.TotalFiles += 2

	// ② 符号链接：扫描器不跟随，链接本身**不计数**；目标按普通文件计 1。
	// 唯一允许降级的形态——Windows 建符号链接需要开发者模式或特权（见
	// scripts/test-windows-quarantine.sh 的同段说明），硬失败只会把环境问题
	// 报成代码回归。降级结果写进 manifest.Shapes，门禁据此区分"没覆盖"。
	symTarget := filepath.Join(dir, symTargetName)
	if err := writeRandom(symTarget, shapeSize, rng); err != nil {
		fail(err)
	}
	m.TotalFiles++
	link := filepath.Join(dir, symLinkName)
	if err := clearForRecreate(link); err != nil {
		fail(err)
	}
	if err := os.Symlink(symTarget, link); err != nil {
		fmt.Fprintf(os.Stderr, "benchgen: 本环境无法建立符号链接（%v）——该形态本轮未覆盖，"+
			"结果已记入 manifest.shapes.symlink=false\n", err)
	} else {
		m.Shapes.Symlink = true
	}

	// ③ .fdd-old 残留：与保留文件逐字节相同，**不计入 TotalFiles**（扫描器按
	// worktemp.IsTempName 忽略，且 M21 起计入 skipped_work_temp_files——本语料
	// 因此是该计数的一个可对账常数 1）。这正是缺陷 6 的现场：忽略规则一旦失效，
	// 重扫必然多出这个重复组，冒烟的组数对账立刻变红。
	keep := filepath.Join(dir, keepName)
	if err := writeRandom(keep, shapeSize, rng); err != nil {
		fail(err)
	}
	m.TotalFiles++
	if err := copyFile(filepath.Join(dir, residualName), keep); err != nil {
		fail(err)
	}

	// ④ 隐藏文件：内容刻意复用已有组的一个成员，且**不计数**（隐藏规则）。
	// IncludeHidden 规则失效时它并入既有组 → 冒烟的分组路径集合摘要随即变化。
	if len(m.Groups) > 0 {
		src := filepath.Join(out, m.Groups[0].Files[0])
		if err := copyFile(filepath.Join(dir, hiddenName), src); err != nil {
			fail(err)
		}
	}

	// ⑤ 同名大小写对：只在**大小写敏感**的卷上存在（不敏感卷上它俩是同一个
	// 文件，造出来的是不存在的假形态）。存在时计 2，并成 1 组 2 文件——
	// 这是遍历器路径折叠（fscase/I2）在冒烟里唯一的实物见证。
	if fscase.Sensitive(dir) {
		lower := filepath.Join(dir, caseLowerName)
		if err := writeRandom(lower, shapeSize, rng); err != nil {
			fail(err)
		}
		upper := filepath.Join(dir, caseUpperName)
		if err := copyFile(upper, lower); err != nil {
			fail(err)
		}
		m.TotalFiles += 2
		m.Shapes.CasePair = true
		m.Groups = append(m.Groups, mgroup{
			Size:  uint64(shapeSize),
			Files: []string{rel(out, lower), rel(out, upper)},
		})
	}
}

// clearForRecreate 删掉形态位置上的一次产物，让"重复生成到同一个 -out"仍然可用。
//
// ★ 2026-09-20（AS-R6，自查发现，AS-K6 自引入）：数据集部分一直是覆盖写
// （os.Create / os.WriteFile 都截断重建），而 Link 与 Symlink 遇到已存在的
// 目标会直接 "file exists" 失败——README 里那条 `-out ./benchdata` 是条复跑
// 命令，第二次就炸了。删的只是本工具在 shapes/ 下固定使用的这几个名字。
func clearForRecreate(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func copyFile(dst, src string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func readManifestSize(p string, size *uint64) error {
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	*size = uint64(st.Size())
	return nil
}

func rel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "benchgen: %v\n", err)
	os.Exit(1)
}
