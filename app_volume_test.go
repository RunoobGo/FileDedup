package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"filededup/internal/model"
)

// TestFileVolumePrefersResolvedIdentity 卷标识优先取扫描阶段解析出的真实卷身份。
//
// 为什么这个优先级不能反：路径卷根（filepath.VolumeName）只是**字符串前缀**。
// Windows 上一个盘符未必是一个卷（挂载点会把别的卷挂到 C:\Mount\X），
// UNC 路径（\\server\share）也拿不到稳定的卷名。若用路径前缀判跨卷，
// 用户点到的「软链接合并」按钮可能出现在同卷组上（白白失败于权限），
// 或者该出现的跨卷组上不出现。真实卷身份由内核给出，是唯一可靠依据。
func TestFileVolumePrefersResolvedIdentity(t *testing.T) {
	e := &model.FileEntry{
		ID:   1,
		Path: `D:\data\a.bin`,
		Key:  model.FileKey{VolumeID: 0x1A2B3C4D, FileIndex: 99, Resolved: true},
	}
	vol, ok := fileVolume(e)
	if !ok {
		t.Fatal("已解析的身份必须报告为可信（前端据此显示跨卷入口）")
	}
	// 不硬编码十进制字面量：格式串一旦调整（如加上卷序列号的十六进制形式），
	// 写死数字会让测试变成"改了格式就红"的噪音源，而它真正要断言的是
	// "用的是 VolumeID 而不是路径"。
	if want := fmt.Sprintf("vid:%d", 0x1A2B3C4D); vol != want {
		t.Fatalf("应使用 VolumeID 构造标识，期望 %q，实得 %q", want, vol)
	}

	// 同一卷上的另一个文件必须得到**同一个**标识，否则跨卷判定恒为真
	e2 := &model.FileEntry{
		ID:   2,
		Path: `D:\other\b.bin`,
		Key:  model.FileKey{VolumeID: 0x1A2B3C4D, FileIndex: 1234, Resolved: true},
	}
	if vol2, _ := fileVolume(e2); vol2 != vol {
		t.Fatalf("同卷文件应得到相同标识：%q ≠ %q", vol2, vol)
	}

	// 不同卷必须得到不同标识（这是跨卷判定的全部意义）
	e3 := &model.FileEntry{
		ID:   3,
		Path: `F:\backup\c.bin`,
		Key:  model.FileKey{VolumeID: 0x5E6F7A8B, FileIndex: 7, Resolved: true},
	}
	if vol3, _ := fileVolume(e3); vol3 == vol {
		t.Fatalf("跨卷文件不应得到相同标识：%q", vol3)
	}
}

// TestFileVolumeFallbackIsUntrusted 未解析身份时退化为路径卷根，
// 且**必须**报告为不可信——否则前端会拿一个可能错的判定去显示按钮。
func TestFileVolumeFallbackIsUntrusted(t *testing.T) {
	e := &model.FileEntry{ID: 1, Path: filepath.Join("/mnt", "data", "a.bin")}
	vol, ok := fileVolume(e)
	if ok {
		t.Fatal("路径推断必须标记为不可信")
	}
	want := "root:" + string(filepath.Separator)
	if vol != want {
		t.Fatalf("回退标识应为 %q，实得 %q", want, vol)
	}

	// 同一根下的文件得到同一标识（保守按同卷处理 → 不显示软链接按钮）
	e2 := &model.FileEntry{ID: 2, Path: filepath.Join("/mnt", "other", "b.bin")}
	if vol2, _ := fileVolume(e2); vol2 != vol {
		t.Fatalf("同根文件应得到相同回退标识：%q ≠ %q", vol2, vol)
	}
}

// TestToGroupViewCarriesVolume 视图必须把卷信息带给前端。
//
// 这是「跨卷软链接入口」的**唯一**数据来源：前端拿不到 VolumeID，
// 只看得到 Path。缺了这两个字段，前端只能瞎猜（或永远不显示按钮）。
func TestToGroupViewCarriesVolume(t *testing.T) {
	g := &model.DuplicateGroup{
		GroupID: 1,
		Files: []*model.FileEntry{
			{ID: 10, Path: `D:\data\a.bin`, Size: 100,
				Key: model.FileKey{VolumeID: 111, Resolved: true}},
			{ID: 11, Path: `F:\backup\a.bin`, Size: 100,
				Key: model.FileKey{VolumeID: 222, Resolved: true}},
		},
	}
	v := toGroupView(g, nil, nil)
	if len(v.Files) != 2 {
		t.Fatalf("视图文件数错误: %d", len(v.Files))
	}
	for _, f := range v.Files {
		if f.Volume == "" || !f.VolumeResolved {
			t.Fatalf("视图必须携带可信的卷标识: %+v", f)
		}
	}
	if v.Files[0].Volume == v.Files[1].Volume {
		t.Fatal("跨卷组的两个成员必须得到不同卷标识（前端据此显示软链接入口）")
	}
}

// TestToGroupViewSameVolumeGroupDetectable 同卷组：两个成员标识相同。
// 前端据此**不**显示软链接按钮（同卷用硬链接，零权限、无悬空风险）。
func TestToGroupViewSameVolumeGroupDetectable(t *testing.T) {
	g := &model.DuplicateGroup{
		GroupID: 2,
		Files: []*model.FileEntry{
			{ID: 20, Path: `C:\a\x.bin`, Size: 50,
				Key: model.FileKey{VolumeID: 7, Resolved: true}},
			{ID: 21, Path: `C:\b\x.bin`, Size: 50,
				Key: model.FileKey{VolumeID: 7, Resolved: true}},
		},
	}
	v := toGroupView(g, nil, nil)
	if v.Files[0].Volume != v.Files[1].Volume {
		t.Fatalf("同卷组不应被识别为跨卷: %q vs %q",
			v.Files[0].Volume, v.Files[1].Volume)
	}
}

// TestGroupVolumeUnresolvedOnUnixLikePaths 回归：unix 下（本测试实际运行环境）
// 路径卷根为空，回退到 "/"，且可信位为 false——确保不会因为
// "Volume 字段是空串"而被前端误判成"跨卷"（空串相等，其实是同卷）。
func TestGroupVolumeUnresolvedOnUnixLikePaths(t *testing.T) {
	g := &model.DuplicateGroup{
		GroupID: 3,
		Files: []*model.FileEntry{
			{ID: 30, Path: "/tmp/a/x.bin", Size: 10},
			{ID: 31, Path: "/tmp/b/x.bin", Size: 10},
		},
	}
	v := toGroupView(g, nil, nil)
	if v.Files[0].Volume == "" {
		t.Fatal("Volume 不应为真正的空串（会与 JSON 缺省值混淆）")
	}
	if v.Files[0].VolumeResolved {
		t.Skipf("本平台解析出了真实卷身份（%s），回退路径未被覆盖",
			runtime.GOOS)
	}
	if v.Files[0].Volume != v.Files[1].Volume {
		t.Fatal("无身份信息时应保守视为同卷")
	}
}
