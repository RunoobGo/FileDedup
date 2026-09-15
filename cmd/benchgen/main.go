// benchgen 合成基准数据集生成器（04 M1-T12，01 §12.2）。
//
// 数据集 A：小文件海（默认 10 万个 1KB~256KB，10% 重复）
// 数据集 C：混合真实感（A + 中等文件 1~16MB + 大文件 512MB~1GB）
// -scale 缩放系数（冒烟 0.001），-seed 固定随机种子，输出 manifest.json 记录期望重复组（E2E 断言依据）。
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	mrand "math/rand"
	"os"
	"path/filepath"
)

type manifest struct {
	Dataset    string   `json:"dataset"`
	Seed       int64    `json:"seed"`
	TotalFiles int      `json:"total_files"`
	Groups     []mgroup `json:"groups"`
}

type mgroup struct {
	Size  uint64   `json:"size"`
	Files []string `json:"files"` // 相对 out 的路径
}

func main() {
	dataset := flag.String("dataset", "A", "A=小文件海, B=大文件, C=混合")
	out := flag.String("out", "./benchdata", "输出目录")
	scale := flag.Float64("scale", 1.0, "规模系数（1.0=标准，冒烟 0.001）")
	seed := flag.Int64("seed", 42, "随机种子")
	flag.Parse()

	rng := mrand.New(mrand.NewSource(*seed))
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	m := manifest{Dataset: *dataset, Seed: *seed}

	switch *dataset {
	case "A":
		genA(*out, *scale, rng, &m)
	case "B":
		genLarge(*out, *scale, rng, &m) // 大文件专项：512MB~1GB，30% 重复（01 §12.2）
	case "C":
		genA(*out, *scale, rng, &m)     // 小文件部分
		genMid(*out, *scale, rng, &m)   // 中等文件
		genLarge(*out, *scale, rng, &m) // 大文件
	default:
		fail(fmt.Errorf("未知数据集 %s（支持 A/B/C）", *dataset))
	}

	mb, _ := json.MarshalIndent(&m, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "manifest.json"), mb, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("生成完成: %s 数据集=%s 文件=%d 期望重复组=%d\n", *out, *dataset, m.TotalFiles, len(m.Groups))
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
		if err := writeRandom(p, int64(size)); err != nil {
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
		if err := writeRandom(p, int64(size)); err != nil {
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
		if err := writeRandom(p, size); err != nil {
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

func writeRandom(p string, size int64) error {
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
		if _, err := rand.Read(buf[:n]); err != nil {
			return err
		}
		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
		total += n
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
