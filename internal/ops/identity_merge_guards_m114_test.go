package ops

// M114 残半 a（第 3 轮 §24.3.3，04 §6.11 OPS-13b）：合并路径上的四道身份守卫
// 用的是 identityStill 的**两布尔视图**，把 M114 在 executor 那六处已经分开的
// 三种归因又压回成一句话——读不动身份、原先能解析现在解析不出，统统报成
// "在校验后被替换（inode 已变化）"，等于宣称看到过一个并不存在的新对象。
//
// 修法：四道守卫改走 identityGuardSentence（判据本体仍是 identityCheck 的四格）。
// ★ 本文件同时钉住两件事，缺一件就是空口白话：
//   - vReplaced 那一格的文本与改前**逐字节相同**（用等值断言，不是 Contains）；
//   - 另外两格不再出现"被替换"，并各自带上自己的说法。
// 处置方向一个字没改：三格全部拦下、tmp 全部清掉、dup 全部原样留着。
//
// 注入一律走 verify.go:103 的 fsidFromPathFn 包级接缝（它只被 identityCheck 调用，
// 不会顺带改变 claimSlot / verifySymlinked / stillOurs 的行为）。因此三条腿
// （darwin/linux/windows）都能真跑，不依赖测试卷是否提供稳定索引。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/fsid"
)

// 两个伪造身份：与被复核路径的真实身份无关（真实读取已被接缝覆盖）。
var (
	m114KeepID = fsid.ID{Dev: 0xAA01, Ino: 0xBB02, CtimeNs: 1, Resolved: true}
	m114DupID  = fsid.ID{Dev: 0xAA01, Ino: 0xCC03, CtimeNs: 1, Resolved: true}
	// 与上面两个都不同的第三个号，用来造"确实换成另一个对象"。
	m114OtherID = fsid.ID{Dev: 0xAA01, Ino: 0xDD04, CtimeNs: 1, Resolved: true}
)

// m114Site 描述一处守卫：目标路径 + 走到它之前必须放行的上游守卫。
type m114Site struct {
	name   string
	merge  func(keep, dup string, keepID, dupID fsid.ID) error
	target func(keep, dup, tmp string) string
	// upstream 是"要让更早那道守卫放行，得把哪个路径指认成哪个身份"。
	upstream func(keep, dup, tmp string) (string, fsid.ID)
	// subject 是该守卫文案里对目标的称呼（决定 vReplaced 的等值文本）。
	subject string
}

func TestMergeIdentityGuardsSplitThreeVerdicts(t *testing.T) {
	sites := []m114Site{
		{
			name: "HardlinkMerge/保留源", merge: HardlinkMerge,
			target:   func(_, dup, tmp string) string { return tmp },
			subject:  "保留源",
			upstream: func(keep, dup, tmp string) (string, fsid.ID) { return "", fsid.ID{} },
		},
		{
			name: "HardlinkMerge/目标文件", merge: HardlinkMerge,
			target:   func(_, dup, _ string) string { return dup },
			subject:  "目标文件",
			upstream: func(_, _, tmp string) (string, fsid.ID) { return tmp, m114KeepID },
		},
		{
			name: "SymlinkMerge/保留源", merge: SymlinkMerge,
			target:   func(keep, _, _ string) string { return keep },
			subject:  "保留源",
			upstream: func(keep, dup, tmp string) (string, fsid.ID) { return "", fsid.ID{} },
		},
		{
			name: "SymlinkMerge/目标文件", merge: SymlinkMerge,
			target:  func(_, dup, _ string) string { return dup },
			subject: "目标文件",
			// ★ 两个 Merge 的第一道守卫读的**不是同一个路径**：HardlinkMerge 读
			// tmp（临时硬链接，与 keep 同一 inode），SymlinkMerge 读 keep 本身
			// （tmp 是链接，读它会拿到链接自身的身份，见 symlink.go:76-79）。
			// 上游放行因此各自指定，写错就是探针打在空处。
			upstream: func(keep, _, _ string) (string, fsid.ID) { return keep, m114KeepID },
		},
	}

	// 三格：vReplaced（等值钉旧文本）、vUnknown（读不动）、vGone（已消失）。
	for _, site := range sites {
		for _, verdict := range []struct {
			kind   string
			want   string // vReplaced 用等值，其余用 Contains
			forbid string
		}{
			{"被替换", site.subject + "在校验后被替换（inode 已变化），已拦截（S1）", ""},
			{"读不动", "无法确认", "被替换"},
			{"已消失", "已消失", "被替换"},
		} {
			t.Run(site.name+"/"+verdict.kind, func(t *testing.T) {
				dir := t.TempDir()
				keep := filepath.Join(dir, "keep.bin")
				dup := filepath.Join(dir, "dup.bin")
				payload := []byte("M114-a payload that must survive every guard")
				for _, p := range []string{keep, dup} {
					if err := os.WriteFile(p, payload, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				tmp := dup + FddTempSuffix

				target := site.target(keep, dup, tmp)
				var hits int
				prev := fsidFromPathFn
				fsidFromPathFn = func(p string) (fsid.ID, error) {
					if up, id := site.upstream(keep, dup, tmp); up != "" && p == up {
						return id, nil // 放行上游守卫
					}
					if p != target {
						return prev(p)
					}
					hits++
					switch verdict.kind {
					case "被替换":
						return m114OtherID, nil
					case "读不动":
						return fsid.ID{}, errSimulatedUnreadable
					default:
						return fsid.ID{}, os.ErrNotExist
					}
				}
				t.Cleanup(func() { fsidFromPathFn = prev })

				err := site.merge(keep, dup, m114KeepID, m114DupID)
				if hits == 0 {
					t.Fatal("前提自检：注入从未落到被测守卫上 ⇒ 探针打在空处，本条没有读数")
				}
				if err == nil {
					t.Fatalf("三格都必须拦下（fail-closed 的方向不因说法而变），实得 err=nil")
				}
				msg := err.Error()
				if verdict.forbid != "" && strings.Contains(msg, verdict.forbid) {
					t.Errorf("%q 被说成了「被替换」：那是只有真看到另一个 inode 时才允许说的话（M114 残半 a）\n实得: %s",
						verdict.kind, msg)
				}
				if verdict.kind == "被替换" {
					if msg != verdict.want {
						t.Errorf("vReplaced 的文本必须与改前逐字节相同（既有文案不许顺带改动）：\n want %q\n  got %q",
							verdict.want, msg)
					}
				} else if !strings.Contains(msg, verdict.want) {
					t.Errorf("文案应含 %q，实得: %s", verdict.want, msg)
				}

				// 处置侧未变：dup 原样在盘上，tmp 不留存。
				if got, rerr := os.ReadFile(dup); rerr != nil || string(got) != string(payload) {
					t.Errorf("守卫拦下后 dup 必须原样保留（err=%v 内容=%q）", rerr, got)
				}
				if _, lerr := os.Lstat(tmp); !os.IsNotExist(lerr) {
					t.Errorf("守卫拦下后临时对象 %s 必须清掉，实得 Lstat=%v", tmp, lerr)
				}
			})
		}
	}
}
