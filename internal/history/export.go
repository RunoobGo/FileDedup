// export.go —— 记录导出的两件产物（M351，2026-09-29 设计段 §5）。
//
//   - `.db`：`VACUUM INTO` 的自包含一致影像，是**唯一能被导回**的形状（R-2 定的恢复通道）。
//   - `.json`：五张表的只读镜像，只供人核对与 diff，**不参与导入**（裁定"两个都出"里
//     第二个是给人看的；把它做成可导入的第二通道，等于让同一份账有两个入口，
//     两个入口的判重语义迟早分叉）。
//
// ★ 为什么这里不出现 `history.Open(srcPath)`：那条路上会执行 `PRAGMA journal_mode=WAL`
// 与两条 `UPDATE op_items`（把残留 planned/undoing 收口），确证损坏时还会把文件改名隔离——
// 全是**改动用户刚选中的那个文件**的动作。实测（设计段 §12 读数）：VACUUM INTO 的产物是
// `journal_mode=delete` 的单文件，用 `PRAGMA query_only=ON` 打开前后字节一字未变、写语句被拒
// ⇒ 读外来库只走只读连接（import.go）。
package history

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// rowPos 记一父行在镜像切片里的下标与其库内 id（子表按此归位）。
type rowPos struct {
	idx int
	id  int64
}

// MirrorFile / MirrorGroup / MirrorScan / MirrorItem / MirrorOp / Mirror
// 是记录导出 JSON 镜像的形状（M351）。
//
// 三处口径要如实说明，别读成"和 ScanMeta 同一套"：
//   - `Roots`/`Filters`/`Failed`/`KeepPaths` 用 `json.RawMessage` **原样搬运 TEXT 列**：
//     那些列本身就是 JSON 串，再解码成结构体然后重编，会让镜像与库里的字节在
//     字段序、空数组表示上分叉——而这个文件的存在理由就是 diff。
//     （列内容不是合法 JSON ⇒ 报错点名是哪一列，与 scanMeta/oplog 的 M321 口径同一条：
//     坏行不许被降级成"空集合"混进导出。）
//   - `Hash` 是 hex 串（BLOB 在 JSON 里别无去处）。
//   - `HistRef` 是**源库内**的 scan 行 id，不是导入后的本地 id；镜像是快照，不承担重映射。
type MirrorFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtimeNs"`
}

type MirrorGroup struct {
	Hash        string       `json:"hash"`
	Size        int64        `json:"size"`
	Reclaimable int64        `json:"reclaimable"`
	Ord         int          `json:"ord"`
	Files       []MirrorFile `json:"files"`
}

type MirrorScan struct {
	SavedAt     int64           `json:"savedAt"`
	Roots       json.RawMessage `json:"roots"`
	Filters     json.RawMessage `json:"filters"`
	Threads     int             `json:"threads"`
	Paranoid    bool            `json:"paranoid"`
	GroupsCount int             `json:"groupsCount"`
	FilesCount  int             `json:"filesCount"`
	OrigFiles   int             `json:"origFiles"`
	Reclaimable int64           `json:"reclaimable"`
	Failed      json.RawMessage `json:"failedJson"`
	KeepPaths   json.RawMessage `json:"keepPaths"`
	Groups      []MirrorGroup   `json:"groups"`
}

type MirrorItem struct {
	OrigPath string `json:"origPath"`
	DestPath string `json:"destPath"`
	LinkSrc  string `json:"linkSrc"`
	Hash     string `json:"hash"`
	Size     int64  `json:"size"`
	MtimeNs  int64  `json:"mtimeNs"`
	State    string `json:"state"`
	Err      string `json:"err"`
}

type MirrorOp struct {
	OpKind    string       `json:"opKind"`
	CreatedAt int64        `json:"createdAt"`
	TargetDir string       `json:"targetDir"`
	HistRef   int64        `json:"histRef"`
	Undoable  bool         `json:"undoable"`
	DoneCount int          `json:"doneCount"`
	Reclaimed int64        `json:"reclaimed"`
	Items     []MirrorItem `json:"items"`
}

type Mirror struct {
	SchemaVersion int          `json:"schemaVersion"`
	ExportedAt    int64        `json:"exportedAt"`
	Scans         []MirrorScan `json:"scans"`
	Ops           []MirrorOp   `json:"ops"`
}

// rawColumn 原样搬运一个 JSON 文本列。空列（NULL 或零长）交回 `null`——
// 建表语句给这四列的默认值是 `'[]'`/`'{}'`，空列只可能来自外来库，
// 报成"这一列没有内容"比猜一个默认值诚实。
func rawColumn(histID int64, col string, b []byte) (json.RawMessage, error) {
	if len(b) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("历史记录 id=%d 的 %s 列不是合法 JSON（导出已停止）", histID, col)
	}
	return json.RawMessage(b), nil
}

// ExportTo 一次调用交出**同一时刻的同一本账**两份产物：dest 处的自包含一致影像
// （VACUUM INTO）与五张表的只读镜像。
//
// 为什么是一个方法而不是 SnapshotTo + BuildMirror 两次调用：那两次各自取锁，
// 中间若有一次扫描收尾（SaveScan）或一笔清理落账，`.db` 与 `.json` 就会各讲一个故事，
// 而界面上它们是"同一次导出"的两个文件。把两步收进同一把写锁，这个分叉在结构上不可造。
//
// 影像与 cache.Snapshot 同一句式、同一条理由：拷主库在 WAL 下拿到的是旧影像
// （未 checkpoint 的内容都在 -wal 里）。dest 必须不存在（SQLite 对该语句的硬约束），
// 覆盖式落笔由调用方用 tmp+rename 完成。
//
// 顺序一律按 id 升序：镜像要能 diff，就得有一个**不因查询方式改变**的次序，
// 而 id 是唯一的候选（ListScans 那个 saved_at DESC 是展示序，不是账本序）。
// 空清单编成 `[]` 而不是 `null`（与 ListScans/ListOps 同一条在册口径：
// 前端拿到 null 会走"没有数据"那支，而这里连"有没有"都还没答）。
func (s *Store) ExportTo(dest string) (Mirror, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`VACUUM INTO ?`, dest); err != nil {
		return Mirror{}, fmt.Errorf("生成记录影像失败: %w", err)
	}
	m := Mirror{SchemaVersion: SchemaVersion, ExportedAt: time.Now().Unix(),
		Scans: []MirrorScan{}, Ops: []MirrorOp{}}

	scans, err := s.db.Query(`SELECT id, saved_at, roots, filters, threads, paranoid,
		groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths
		FROM scan_history ORDER BY id ASC`)
	if err != nil {
		return m, err
	}
	var positions []rowPos
	for scans.Next() {
		var (
			id                           int64
			sm                           MirrorScan
			roots, filters, failed, keep []byte
		)
		if err := scans.Scan(&id, &sm.SavedAt, &roots, &filters, &sm.Threads,
			&sm.Paranoid, &sm.GroupsCount, &sm.FilesCount, &sm.OrigFiles,
			&sm.Reclaimable, &failed, &keep); err != nil {
			scans.Close()
			return m, err
		}
		// 四列任一编不动就整包作废：导出一份"这次扫描没有 roots"的镜像，
		// 与 M321 修掉的"把序列化失败降级成空数组写进账本"是反方向的同一件事。
		if sm.Roots, err = rawColumn(id, "roots", roots); err != nil {
			scans.Close()
			return m, err
		}
		if sm.Filters, err = rawColumn(id, "filters", filters); err != nil {
			scans.Close()
			return m, err
		}
		if sm.Failed, err = rawColumn(id, "failed_json", failed); err != nil {
			scans.Close()
			return m, err
		}
		if sm.KeepPaths, err = rawColumn(id, "keep_paths", keep); err != nil {
			scans.Close()
			return m, err
		}
		sm.Groups = []MirrorGroup{}
		positions = append(positions, rowPos{idx: len(m.Scans), id: id})
		m.Scans = append(m.Scans, sm)
	}
	if err := scans.Err(); err != nil {
		scans.Close()
		return m, err
	}
	scans.Close()

	// 子表逐父行归位。为什么不用一次 JOIN：JOIN 回来仍要按 (hist_id, group_id)
	// 二级归位，代码量与这里相同，却丢掉"某一父行查不动就整包作废"这条清晰边界。
	for _, pos := range positions {
		gs, err := s.db.Query(`SELECT id, hash, size, reclaimable, ord FROM hist_groups
			WHERE hist_id = ? ORDER BY ord ASC, id ASC`, pos.id)
		if err != nil {
			return m, err
		}
		var groupIDs []int64
		var groups []MirrorGroup
		for gs.Next() {
			var (
				gid  int64
				g    MirrorGroup
				hash []byte
			)
			if err := gs.Scan(&gid, &hash, &g.Size, &g.Reclaimable, &g.Ord); err != nil {
				gs.Close()
				return m, err
			}
			g.Hash = hex.EncodeToString(hash)
			g.Files = []MirrorFile{}
			groupIDs = append(groupIDs, gid)
			groups = append(groups, g)
		}
		if err := gs.Err(); err != nil {
			gs.Close()
			return m, err
		}
		gs.Close()
		for gi, groupID := range groupIDs {
			fs, err := s.db.Query(`SELECT path, size, mtime_ns FROM hist_files
				WHERE group_id = ? ORDER BY id ASC`, groupID)
			if err != nil {
				return m, err
			}
			for fs.Next() {
				var f MirrorFile
				if err := fs.Scan(&f.Path, &f.Size, &f.MtimeNs); err != nil {
					fs.Close()
					return m, err
				}
				groups[gi].Files = append(groups[gi].Files, f)
			}
			if err := fs.Err(); err != nil {
				fs.Close()
				return m, err
			}
			fs.Close()
		}
		m.Scans[pos.idx].Groups = groups
	}

	ops, err := s.db.Query(`SELECT id, op_kind, created_at, target_dir, hist_id, undoable,
		done_count, reclaimed FROM op_records ORDER BY id ASC`)
	if err != nil {
		return m, err
	}
	var opPositions []rowPos
	for ops.Next() {
		var (
			id int64
			op MirrorOp
		)
		if err := ops.Scan(&id, &op.OpKind, &op.CreatedAt, &op.TargetDir, &op.HistRef,
			&op.Undoable, &op.DoneCount, &op.Reclaimed); err != nil {
			ops.Close()
			return m, err
		}
		op.Items = []MirrorItem{}
		opPositions = append(opPositions, rowPos{idx: len(m.Ops), id: id})
		m.Ops = append(m.Ops, op)
	}
	if err := ops.Err(); err != nil {
		ops.Close()
		return m, err
	}
	ops.Close()

	for _, pos := range opPositions {
		is, err := s.db.Query(`SELECT orig_path, dest_path, link_src, hash, size, mtime_ns, state, err
			FROM op_items WHERE op_id = ? ORDER BY id ASC`, pos.id)
		if err != nil {
			return m, err
		}
		for is.Next() {
			var (
				it   MirrorItem
				hash []byte
			)
			if err := is.Scan(&it.OrigPath, &it.DestPath, &it.LinkSrc, &hash,
				&it.Size, &it.MtimeNs, &it.State, &it.Err); err != nil {
				is.Close()
				return m, err
			}
			it.Hash = hex.EncodeToString(hash)
			m.Ops[pos.idx].Items = append(m.Ops[pos.idx].Items, it)
		}
		if err := is.Err(); err != nil {
			is.Close()
			return m, err
		}
		is.Close()
	}
	return m, nil
}
