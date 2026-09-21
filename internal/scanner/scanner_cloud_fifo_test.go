//go:build !windows

package scanner

// V3（设计稿 §4.2 第一条理由 / §4.4 变异 M-P1-c）：占位判定排在
// !info.Mode().IsRegular() **之前**这条顺序不变量。
//
// 为什么这条顺序在 Windows 上是本项的主要价值：占位文件是 reparse point，
// Go 的 mode() 会给它 ModeIrregular，于是被 !IsRegular() 顺带无声丢弃
// （设计稿 §4.0 E8）。判定放后面就永远数不到它，"让这一类可见"直接归零。
//
// Windows 上造不出可复现的占位夹具（需要 OneDrive 客户端与账号），这里改用
// FIFO 作**同等地位**的替身：它同样过不了 IsRegular，因此"谁先看到它"这件事
// 与占位在 Windows 上的遭遇完全同构。顺序本身写在无 tag 的 scanner.go 里，
// 三平台共用同一份代码，所以这条腿在 unix 上成立即证明代码次序成立；
// 平台差异只在"占位是否真的走 ModeIrregular"，那一条按 §4.5 记为未兑现。

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"filededup/internal/model"
)

func TestWalkCloudJudgmentBeatsNonRegularSkip(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.bin"), 0o644); err != nil {
		t.Skipf("当前环境无法创建 FIFO：%v", err)
	}
	mkDirFiles(t, root, "regular.txt")

	setCloudCheck(t, func(fi os.FileInfo) bool {
		return fi != nil && fi.Name() == "pipe.bin"
	})
	res := Walk(context.Background(), []string{root}, &model.Filters{}, 2)

	if res.SkippedCloudFiles != 1 {
		t.Errorf("SkippedCloudFiles = %d, want 1：非普通文件的占位被 !IsRegular() 抢先丢掉了",
			res.SkippedCloudFiles)
	}
	if hasPathWith(res, "/pipe.bin") {
		t.Error("FIFO 进了语料")
	}
	if !hasPathWith(res, "/regular.txt") {
		t.Error("普通文件被误伤")
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v, want 空", res.Failed)
	}
}
