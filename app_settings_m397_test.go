package main

// M397（第九轮 P2-2 / 设计段 §六 E8）：设置文件损坏留证这一步**自己**不能销毁上一次的证据。
//
// 现读取证（改前形状，app_settings.go:39）：`os.Rename(path, path+".corrupt")` 的目标已存在时
// 直接替换 ⇒ 第二次配置损坏会把第一次那份物证抹掉。而那份文件是唯一能解释"设置为什么坏"的
// 证据，M10b 立这条留证分支防的正是"证据消失"——加固动作自己销毁证据不成立。
//
// ★ 判据落在**读数**上（盘上到底有几份、每份是什么字节），不是"代码里有没有 corruptBackupPath"
//   这类形状断言；这样改前是真的跑起来红，而不是红在编译错误上（§2.2 第 6 条 M364 那条纪律）。
// ★ 时钟由 corruptBackupPath 的参数注入：同一秒内连续两次损坏必然撞秒级时间戳，
//   这一格只有把时钟钉进那一秒才测得到（否则"撞名再 +1"那半边是摆设）。

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeBrokenSettings 往设置位写一份"解析必失败"的内容（截断的 JSON）。
func writeBrokenSettings(t *testing.T, path, marker string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建配置目录：%v", err)
	}
	// 故意留一个没闭合的数组 ⇒ json.Unmarshal 当场报错，才会走留证分支。
	// ★ 早先这里写的是合法 JSON 加一个未知字段，Go 会**无声忽略**它、解析成功 ⇒ 整组用例空转，
	//   所以夹具必须真的坏，且坏法要能在字节里看见 marker。
	content := `{"threads": 9, "theme": [` + marker
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写坏设置：%v", err)
	}
}

// TestM397SecondCorruptionKeepsFirstEvidence 是主判据格（改前必红）。
//
// 改前：第二次的 Rename 覆盖 settings.json.corrupt ⇒ 第一份物证的字节再也读不到。
// 改后：第一次仍在固定名上，第二次落到一个带时间戳的名字上，两份都读得出来。
func TestM397SecondCorruptionKeepsFirstEvidence(t *testing.T) {
	a, rec := newHistApp(t)
	path := mustSettingsPath(t, a)

	writeBrokenSettings(t, path, "FIRST")
	if got := a.GetSettings(); !isDefaultSettings(got) {
		t.Fatalf("首趟损坏应回纯默认值，实得 %+v", got)
	}
	first, err := os.ReadFile(path + ".corrupt")
	if err != nil {
		t.Fatalf("首趟未留证（应改名 settings.json.corrupt）：%v", err)
	}
	if !strings.Contains(string(first), "FIRST") {
		t.Fatalf("首趟留证内容不对：%q", first)
	}

	writeBrokenSettings(t, path, "SECOND")
	if got := a.GetSettings(); !isDefaultSettings(got) {
		t.Fatalf("第二趟损坏应回纯默认值，实得 %+v", got)
	}

	// ★ 这一断言就是改前红的那一格：固定名上必须还是**第一份**的字节。
	kept, err := os.ReadFile(path + ".corrupt")
	if err != nil {
		t.Fatalf("第二趟之后固定名读不到了：%v", err)
	}
	if !strings.Contains(string(kept), "FIRST") {
		t.Fatalf("上一次的损坏证据被覆盖（P2-2 的实测形状）：固定名上现在是 %q", kept)
	}

	// 第二份也必须留下，且提示那句要点名**真实落点**（否则用户按固定名去找会扑空）。
	extra := secondCorruptFile(t, path)
	body, err := os.ReadFile(extra)
	if err != nil {
		t.Fatalf("第二份证据落点读不到：%v", err)
	}
	if !strings.Contains(string(body), "SECOND") {
		t.Fatalf("第二份证据内容不对：%q", body)
	}
	msg := ledgerErrText(t, rec)
	if !strings.Contains(msg, filepath.Base(extra)) {
		t.Fatalf("提示未点名第二份的真实落点 %s，实得 %q", filepath.Base(extra), msg)
	}
}

// secondCorruptFile 返回除固定名之外的那一份留证文件（本用例里应当恰好只有一份）。
func secondCorruptFile(t *testing.T, path string) string {
	t.Helper()
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("检索带时间戳的留证件：%v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("期望恰好一份带时间戳的留证件，实得 %d 份：%v", len(matches), matches)
	}
	return matches[0]
}

// TestM397FirstCorruptionStillUsesFixedName 是负控制：盘上没有旧证据时**不许**长出时间戳后缀，
// 固定名 `settings.json.corrupt` 是 M10b 起用户与手册都认的那个名字（09 §4.1 的表里也是这一行）。
func TestM397FirstCorruptionStillUsesFixedName(t *testing.T) {
	a, _ := newHistApp(t)
	path := mustSettingsPath(t, a)

	writeBrokenSettings(t, path, "ONLY")
	a.GetSettings()

	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("首趟必须落固定名：stat %s.corrupt 报 %v", filepath.Base(path), err)
	}
	if matches, _ := filepath.Glob(path + ".corrupt-*"); len(matches) != 0 {
		t.Fatalf("首趟不该产生带时间戳的落点，实得 %v", matches)
	}
}

// TestM397BackupPathWalksForwardOnSameSecondCollision 钉"撞名再 +1"那半边（纯函数 + 注入时钟）。
func TestM397BackupPathWalksForwardOnSameSecondCollision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	const sec = int64(1700000000)
	now := time.Unix(sec, 0)

	// 盘上已有固定名与固定名-sec 两份 ⇒ 第三份只能往前让一格。
	for _, name := range []string{".corrupt", ".corrupt-1700000000"} {
		if err := os.WriteFile(path+name, []byte("old"), 0o644); err != nil {
			t.Fatalf("铺夹具 %s：%v", name, err)
		}
	}
	got := corruptBackupPath(path, now)
	want := path + ".corrupt-1700000001"
	if got != want {
		t.Fatalf("同一秒内第三次损坏的落点应往前让一格：期望 %s，实得 %s", filepath.Base(want), filepath.Base(got))
	}
}

// TestM397BackupPathSaturatesToNanos 钉兜底格：60 格全被占时仍不得返回一个**已存在**的名字。
func TestM397BackupPathSaturatesToNanos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	const sec = int64(1700000000)

	if err := os.WriteFile(path+".corrupt", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 60; i++ {
		name := path + ".corrupt-" + strconv.FormatInt(sec+i, 10)
		if err := os.WriteFile(name, []byte("old"), 0o644); err != nil {
			t.Fatalf("铺第 %d 格夹具：%v", i, err)
		}
	}
	got := corruptBackupPath(path, time.Unix(sec, 0))
	if _, err := os.Stat(got); err == nil {
		t.Fatalf("兜底落点压到了一个已存在的文件上（等于覆盖证据）：%s", filepath.Base(got))
	}
	if got == path+".corrupt" {
		t.Fatal("兜底落点不得回到固定名")
	}
}
