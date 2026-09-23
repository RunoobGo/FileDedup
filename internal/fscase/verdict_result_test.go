package fscase

// M62+M85（2026-09-22 裁定「按卷型分岔」，设计稿 §28.2）的门禁：卷语义必须分得出
// **确证**与**退默认**两态。
//
// 改前的形状：`Sensitive(dir) bool` 把三种来源压成同一个 bool ——
//   ① 写探针实测成功（确证）
//   ② 探针不可用、但卷型本身就说"这卷不区分大小写"（FAT/exFAT/NTFS，也是确证）
//   ③ 探针不可用、卷型也拿不到（退平台默认，**没有**读数）
// ② 是 M85 要的：只读卷/无写权限的 FAT 目录今天落到 ③ 的"敏感"，于是同一棵树的
// 两种拼写各走一遍（重复组数与可释放空间虚高 ⇒ 据此下发的清理会多删文件，包注释 :8-9）。
// ③ 是 M62 要保住的：它不许被悄悄洗成确证，否则"退默认"这件事在整条链上彻底隐形。
//
// ★ 夹具形状：②③ 都要"探针必失败"。改前既有夹具是 `chmod 0500` 的只读目录
// （fscase_test.go:47-60），★ 本文件不用它做主形状 —— M151 的教训是同一形状在不同
// 平台报的不是同一格（Windows 对目录的只读位未必拒绝创建，那一腿就白测）。这里用
// **不存在的目录**：`O_CREATE` 恒 ENOENT（父目录都不在，与卷的写权限无关），三平台
// 同一格（probe 里"创建失败且非 EEXIST"那一条 return）。仍留一条前提自检，让落点由
// 现场说话而不是由注释声称。
//
// ★ 卷型经 `volumeTypeName` 这个包级变量注入（惯例同 scanner.probeCaseVerdict、
// ops.verifyFileFn）：darwin 走 media 的 statfs 真读数，另两平台今天恒 ""（§28.6
// 的"明确不做"）。真 FAT 卷本机造不出（无 root、不宜挂镜像），所以 ② 的判定腿由
// 注入给出；"读数本身取得到、且另两平台确实取不到"由 `media` 包的
// `TestFSTypeNameByPlatform` 钉住 —— 两条合起来才是 ② 的完整证据，缺前一条就是
// "注入测得过、生产拿不到"，缺后一条就是"表写得漂亮但没人验证卷型读得出来"。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// missingProbeDir 造一个"探针必失败"的目录名：父目录在、它自己不在，
// 于是 O_CREATE 报 ENOENT 而不是 EEXIST（后者会换号重试，落不到退默认那一格）。
func missingProbeDir(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fdd-m62-no-such-dir")
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("夹具前提不成立：%q 不是「不存在」（实得 err=%v）⇒ 落不到探针退默认那一格", p, err)
	}
	f, err := os.OpenFile(filepath.Join(p, "probe.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(p, "probe.txt"))
		t.Fatalf("夹具前提不成立：在 %q 下竟然能建文件 ⇒ 探针不会退默认，本条测不到 ②③", p)
	}
	if errors.Is(err, os.ErrExist) {
		t.Fatalf("夹具前提不成立：%q 下 O_CREATE 报 EEXIST（那是换号重试那一格），本条测不到退默认：%v", p, err)
	}
	t.Logf("本平台「探针不可用」的实测错误：%v", err)
	return p
}

// useVolumeType 临时把卷型读数换成给定名字，返回还原前的真读数。
// ★ 返回旧值而不是 Cleanup 里写死 ""：嵌套替换时能一层层退回去。
func useVolumeType(t *testing.T, name string) (restore func()) {
	t.Helper()
	// ★ 两端都要持 mu（M155）：读端在生产侧（fscase.go 的 volumeTypeReader），写端在这里。
	//   只锁一头等于把 race detector 哄住——被 VerdictCtx 放弃的探测 goroutine 会在
	//   本用例结束之后才走到那次读（CI run 35818865178 的红就是这个形状）。
	mu.Lock()
	prev := volumeTypeName
	volumeTypeName = func(string) string { return name }
	mu.Unlock()
	return func() {
		mu.Lock()
		volumeTypeName = prev
		mu.Unlock()
	}
}

// TestVerdictOnWritableDirIsProven 钉 ①：能写进去的目录必须报"确证"。
// 断言只钉 Proven，不钉 Sensitive —— 本机是 APFS（不敏感）而 CI 的 linux 腿是 ext4
// （敏感），拿本机语义当普适判据正是 M126 那条用例的返工原因。
func TestVerdictOnWritableDirIsProven(t *testing.T) {
	dir := t.TempDir()
	v := Verdict(dir)
	if !v.Proven {
		t.Fatalf("可写目录上探针本应成功，却报了「未确证」：%+v", v)
	}
	t.Logf("本机卷语义确证为 sensitive=%v", v.Sensitive)
}

// TestVerdictUnprovenWhenProbeFailsAndVolumeTypeUnknown 钉 ③：探针不可用 + 卷型也拿不到
// ⇒ 退平台默认，且 Proven=false。★ 这条是 M62 的"少扫不错删"方向，也是负控制：
// 若哪天有人把 Proven 恒置 true（变异 M25-d），红的是这一格。
func TestVerdictUnprovenWhenProbeFailsAndVolumeTypeUnknown(t *testing.T) {
	dir := missingProbeDir(t)
	restore := useVolumeType(t, "")
	defer restore()

	v := Verdict(dir)
	if v.Proven {
		t.Fatalf("探针失败且卷型读不到却报「确证」：这是把退默认洗成实测（M62 反对的无声改判）：%+v", v)
	}
	if v.Sensitive != Default() {
		t.Fatalf("拿不到任何证据时必须退平台默认 %v，实得 %v", Default(), v.Sensitive)
	}
}

// TestVerdictVolumeTypeConfirmsInsensitiveWhenProbeFails 钉 ②：探针不可用而卷型属于
// "天生不敏感"表 ⇒ 判不敏感，且这算**确证**（Proven=true）。
// ★ 这是 M85 要的增益；变异 M25-c 若把这张表写成"凡拿到卷型就算确证不敏感"，
// 红的是下面那条远端负控制，而不是这一条。
func TestVerdictVolumeTypeConfirmsInsensitiveWhenProbeFails(t *testing.T) {
	for _, name := range []string{"msdos", "fat", "exfat", "ntfs"} {
		t.Run(name, func(t *testing.T) {
			dir := missingProbeDir(t)
			restore := useVolumeType(t, name)
			defer restore()

			v := Verdict(dir)
			if v.Sensitive {
				t.Fatalf("卷型 %q 天生不区分大小写，探针又不可用 ⇒ 应判不敏感，实得 sensitive=true", name)
			}
			if !v.Proven {
				t.Fatalf("卷型本身就是证据，这一格不是「退默认」：%+v", v)
			}
		})
	}
}

// 远端文件系统一律**不进**"天生不敏感"表（§28.2 明写）：服务端语义无从由名字推断，
// 判成不敏感会把两棵真不同的树折成一棵（包注释 :5-8 那类"文件凭空消失"）。
// 变异 M25-c（"凡拿到卷型就算确证不敏感"）必须红在这一格。
func TestVerdictNetworkVolumeTypeIsNotConfirmed(t *testing.T) {
	for _, name := range []string{"smbfs", "cifs", "nfs", "afpfs", "webdav", "apfs", "ext4", "xfs", "btrfs"} {
		t.Run(name, func(t *testing.T) {
			dir := missingProbeDir(t)
			restore := useVolumeType(t, name)
			defer restore()

			v := Verdict(dir)
			if v.Proven {
				t.Fatalf("卷型 %q 不足以确证大小写语义，却报了 Proven=true：%+v", name, v)
			}
			if v.Sensitive != Default() {
				t.Fatalf("卷型 %q 不在天生不敏感表里 ⇒ 必须退默认 %v，实得 %v", name, Default(), v.Sensitive)
			}
		})
	}
}

// TestSensitiveIsBoolViewOfVerdict 钉"二态调用方语义不变"：四处老调用方
// （cmd/benchgen/main.go:316、ops/keep.go:177/:234/:243/:258、scanner 的注入缝）
// 继续拿 bool，比的就是 Verdict(dir).Sensitive —— 同一次判定，不留第二份实现。
// ★ 两处必须同值这一条不是琐碎的：Verdict 有按目录缓存，若 Sensitive 另走一条
// 不查表的路（或反过来 Verdict 绕过缓存），两边就会在不同时刻给出不同答案。
func TestSensitiveIsBoolViewOfVerdict(t *testing.T) {
	for _, dir := range []string{t.TempDir(), missingProbeDir(t)} {
		if got, want := Sensitive(dir), Verdict(dir).Sensitive; got != want {
			t.Fatalf("Sensitive(%q)=%v 与 Verdict(%q).Sensitive=%v 不同源", dir, got, dir, want)
		}
	}
}

// TestEmptyDirKeepsDefaultAndIsNotProven 钉空串入口：`dir==""` 那一支不查表也不探测，
// 直接给平台默认。★ 它同样必须是 Proven=false —— 否则"没问卷"这件事在计数链里隐身。
func TestEmptyDirKeepsDefaultAndIsNotProven(t *testing.T) {
	v := Verdict("")
	if v.Proven {
		t.Fatalf("dir=\"\" 什么都没问，不得报确证：%+v", v)
	}
	if v.Sensitive != Default() {
		t.Fatalf("dir=\"\" 必须退平台默认 %v，实得 %v", Default(), v.Sensitive)
	}
}
