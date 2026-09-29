// app_records_io.go —— 记录导出 / 导入（M351/M352/M353，2026-09-29 设计段 §5~§7）。
//
// 这一对方法是裁定 R-2「手动导出导入，导入时以增量的形式扩展本地记录」的落点，
// 也是"防止误删除"这句话在本仓唯一有恢复力的实现：批 1 的 `cache-backup.db` 只保
// "这次手滑"（哈希缓存本就可重算），真正不可再生的是账本——回撤依据、历史、
// 以及"哪个文件原本在哪"。所以：
//
//	导出 = 影像（可导回）+ JSON 镜像（给人核对），两件同时交出、缺一不算导出成功；
//	导入 = 增量合并，只新增本地没有的行，不删不改任何已有记录（判据见 internal/history/import.go）。
//
// ★ 与「清空缓存」的分界（裁定 R-1）：清缓存只动 hash_cache，账本由这里的导出/导入管；
// 记录页那两个「清空」出口是用户唯一的删记录入口，本批一字未动。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filededup/internal/history"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// RecordsExportResult 是导出的回执。
//
// `Cancelled` 单独立一个字段而不是靠"全零值"表达：对话框取消在 Wails 那条腿上是
// `dir == ""` 且 `err == nil`，如果把它压成一个零值结构体，前端就只能靠"字段是不是空"
// 去猜用户到底有没有导出过——而"我点了导出，屏幕上什么都没变"正是本批要消灭的那类误读。
type RecordsExportResult struct {
	Cancelled bool   `json:"cancelled"`
	Dir       string `json:"dir"`
	DbPath    string `json:"dbPath"`
	JsonPath  string `json:"jsonPath"`
	Scans     int    `json:"scans"`
	Ops       int    `json:"ops"`
	Bytes     int64  `json:"bytes"`
}

// RecordsImportResult 是导入的回执。
//
// 五个计数一个都不能少：少了「跳过」就说不清"这份文件我是不是已经导过了"，
// 少了「孤儿」就说不清导入进来的清理记录还能不能联动（`op_records.hist_id`
// 指不到本地扫描行，回撤时的身份复核依据就缺了一角）。
//
// `Cancelled` 与导出同理单独成字段：Wails 的对话框取消是 `path == ""` 且 `err == nil`，
// 把它压成错误会让"我点了导入又反悔"在界面上显示成一次失败——那是假话。
type RecordsImportResult struct {
	Cancelled    bool   `json:"cancelled"`
	Source       string `json:"source"`
	ScansAdded   int    `json:"scansAdded"`
	ScansSkipped int    `json:"scansSkipped"`
	OpsAdded     int    `json:"opsAdded"`
	OpsSkipped   int    `json:"opsSkipped"`
	OpsOrphaned  int    `json:"opsOrphaned"`
}

// 对话框接缝（惯例同 cache.evictFn / dbfile.renameFile / cacheClearSnapshot）。
//
// 为什么要接缝：真机对话框无法在 `go test` 里出现，而本批的承重判据全在接缝之后
// （在途拒绝、两件成对落盘、失败不留半件、导入的增量化与重映射）。
// ★ 与批 1 那条自纠同一条纪律：**判据不许建立在接缝的返回值上**——接缝只用来把
// "用户选了哪个目录"这件事交进来，落盘行为一律走真实文件系统断言。
var (
	pickExportDir = func(ctx context.Context) (string, error) {
		return wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{
			Title: "选择导出记录的目录",
		})
	}
	pickImportFile = func(ctx context.Context) (string, error) {
		return wruntime.OpenFileDialog(ctx, wruntime.OpenDialogOptions{
			Title:   "选择要导入的记录影像",
			Filters: []wruntime.FileFilter{{DisplayName: "FileDedup 记录影像 (*.db)", Pattern: "*.db"}},
		})
	}
)

// ExportRecords 把整本账本导出到用户选定的目录（M351）。
//
// 一次导出交回两个文件：`filededup-records-<时间戳>.db`（影像，唯一能导回的形状）
// 与同名 `.json`（只读镜像，给人核对/diff）。
//
// ★ 为什么不是 `SaveFileDialog`：要一次写**两个**文件，选目录比连点两次"另存为"
// 更贴合，而且 `OpenDirectoryDialog` 是本仓已经证明可用那条腿（`SelectDirectory`）。
func (a *App) ExportRecords() (RecordsExportResult, error) {
	var res RecordsExportResult
	a.mu.Lock()
	busy := a.opsRunning
	ctx := a.ctx
	a.mu.Unlock()
	if busy {
		return res, fmt.Errorf("清理/回撤操作执行中，请等待结束后再导出记录")
	}
	hs := a.histSnapshot()
	if hs == nil {
		return res, fmt.Errorf("历史库不可用")
	}
	dir, err := pickExportDir(ctx)
	if err != nil {
		return res, shellRPCError(err)
	}
	if dir == "" { // 用户取消：不是错误，但必须让前端分得清"没导出"和"导了个零条的账"
		res.Cancelled = true
		return res, nil
	}
	return a.exportRecordsTo(hs, dir, time.Now())
}

// exportRecordsTo 是导出去掉对话框之后的全部实质工作，也是判据打的那一层。
//
// 落盘顺序刻意是"两件都先落 .tmp，两件都改名到位"：任何一步失败都交不回一个
// `.db` 孤件——用户拿到的要么是能完整导回的成对文件，什么都没有。
// （半本账被固化成一个看起来完整的文件，比没有文件更容易害人。）
func (a *App) exportRecordsTo(hs *history.Store, dir string, now time.Time) (RecordsExportResult, error) {
	var res RecordsExportResult
	stem := "filededup-records-" + now.Format("20060102-150405")
	dbPath := filepath.Join(dir, stem+".db")
	jsonPath := filepath.Join(dir, stem+".json")

	// 同秒二次导出会撞名。VACUUM INTO 对"目标已存在"直接报错，改名（os.Rename）却会
	// **静默覆盖**——所以撞名必须在动手前挡下来，而不是让第二次导出把第一次的影像顶掉。
	if exists(dbPath) || exists(jsonPath) {
		return res, fmt.Errorf("目标目录里已有同名的导出文件（%s），请换目录或稍后再试", stem)
	}
	tmpDB, tmpJSON := dbPath+".tmp", jsonPath+".tmp"
	// 残留的 .tmp 也挡一下：它落位时会覆盖别人的 .tmp（第三方文件名撞上这一格是可能的，
	// 而覆盖第三方文件在本仓是 P0 级形状，见 §6.63 那条落位改名）。
	if exists(tmpDB) || exists(tmpJSON) {
		return res, fmt.Errorf("目标目录里已有同名的临时文件（%s.tmp），请清理后重试", stem)
	}
	cleanup := func() {
		_ = os.Remove(tmpDB)
		_ = os.Remove(tmpJSON)
	}

	mirror, err := hs.ExportTo(tmpDB)
	if err != nil {
		cleanup()
		return res, shellRPCError(err)
	}
	// ★ 影像的权限得自己收：`VACUUM INTO` 建的文件跟 SQLite 默认走（实测 0644），
	// 而它里面装的是**同一条隐私内容**（五张表逐行，含用户机器上的全部路径）。
	// 只把 .json 做成 0600、放任 .db 是 0644，等于隐私承诺只兑了一半。
	// 设不上就停止：静默留一份 0644 的账本影像在很可能共享的导出目录里，
	// 比"这次导出没成"更难看（M61 的在册取向：不确定的写入要在落账前拒绝）。
	// ★ 但这一步**只在 unix 腿兑现那句"仅所有者可读写"**：Windows 的模式位只表达"只读属性"，
	//   `0600` 与 `0644` 在那条腿上读回同一个 `0666`（CI 现读，04 §6.66 / M354），真正的访问权
	//   由所在目录的 NTFS ACL 继承 ⇒ 手册按分平台措辞写，不拿这句冒称跨平台保证。
	if err := os.Chmod(tmpDB, 0o600); err != nil {
		cleanup()
		return res, shellRPCError(fmt.Errorf("记录影像权限设置失败（导出已停止）: %w", err))
	}
	body, err := json.MarshalIndent(mirror, "", "  ")
	if err != nil {
		cleanup()
		return res, fmt.Errorf("记录镜像序列化失败（导出已停止，未留下任何文件）: %w", err)
	}
	// 0o600 而不是 0o644：账本里是用户机器上的**全部文件路径**（含回收站落位路径），
	// 导出目录很可能是别的盘、甚至别人可读的共享目录。
	if err := os.WriteFile(tmpJSON, append(body, '\n'), 0o600); err != nil {
		cleanup()
		return res, shellRPCError(err)
	}
	// 两件都在 tmp 里成形了，才配占住正式名字。
	if err := os.Rename(tmpDB, dbPath); err != nil {
		cleanup()
		return res, shellRPCError(fmt.Errorf("记录影像落位失败: %w", err))
	}
	if err := os.Rename(tmpJSON, jsonPath); err != nil {
		// 影像已经落位而镜像落不了 ⇒ 把刚落位的那件收回（它三秒前还不存在，删掉不丢用户数据），
		// 免得留下一件"看起来是完整导出"的孤 .db。
		_ = os.Remove(dbPath)
		_ = os.Remove(tmpJSON)
		return res, shellRPCError(fmt.Errorf("记录镜像落位失败（影像已收回，未留下半份导出）: %w", err))
	}
	var total int64
	for _, p := range []string{dbPath, jsonPath} {
		if st, err := os.Stat(p); err == nil {
			total += st.Size()
		}
	}
	res.DbPath, res.JsonPath, res.Dir = dbPath, jsonPath, dir
	res.Scans, res.Ops, res.Bytes = len(mirror.Scans), len(mirror.Ops), total
	return res, nil
}

// exists 只回答"这个名字有没有被占"，不区分是文件还是目录。
// 落位判据要的是"撞名就停手"，多问一层类型没有意义。
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ImportRecords 把用户选中的一份记录影像**增量**并入本地账本（M352）。
//
// 只读对话框选出来的文件绝不会被本方法改动：源库全程走 `PRAGMA query_only=ON`
// 的只读连接（实测打开前后字节一字未变，见设计段 §12），而 `history.Open` 那条路
// 会 journal_mode/收口 UPDATE/隔离改名地改它，所以没用它做校验。
func (a *App) ImportRecords() (RecordsImportResult, error) {
	var res RecordsImportResult
	a.mu.Lock()
	busy := a.opsRunning
	ctx := a.ctx
	a.mu.Unlock()
	if busy {
		return res, fmt.Errorf("清理/回撤操作执行中，请等待结束后再导入记录")
	}
	hs := a.histSnapshot()
	if hs == nil {
		return res, fmt.Errorf("历史库不可用")
	}
	path, err := pickImportFile(ctx)
	if err != nil {
		return res, shellRPCError(err)
	}
	if path == "" { // 用户取消：不是失败，回执里必须能分清
		res.Cancelled = true
		return res, nil
	}
	sum, err := hs.ImportFrom(path)
	if err != nil {
		return res, shellRPCError(err)
	}
	res.Source, res.ScansAdded, res.ScansSkipped = path, sum.ScansAdded, sum.ScansSkipped
	res.OpsAdded, res.OpsSkipped, res.OpsOrphaned = sum.OpsAdded, sum.OpsSkipped, sum.OpsOrphaned
	return res, nil
}
