package history

import (
	"database/sql"
	"fmt"
	"time"
)

// OpItemPlan 清理操作执行前对单文件的计划记录（写前日志，spec §7）。
// Hash/Size/MtimeNs 快照用于回撤时校验与还原。
type OpItemPlan struct {
	OrigPath string
	Hash     [32]byte
	Size     uint64
	MtimeNs  int64
}

// OpMeta 一次清理操作摘要。计数列由 op_items 实时聚合，
// Reclaimable 取 FinalizeOp 时冻结的口径（回撤不回改历史账目）。
type OpMeta struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	CreatedAt   int64  `json:"createdAt"` // Unix 秒
	TargetDir   string `json:"targetDir"`
	HistID      int64  `json:"histId"`
	Undoable    bool   `json:"undoable"`
	Items       int    `json:"items"`
	Done        int    `json:"done"`
	Undone      int    `json:"undone"`
	Failed      int    `json:"failed"` // 执行失败（不含回撤失败）
	Reclaimable uint64 `json:"reclaimable"`
}

// OpItem 操作内的单文件记录。
type OpItem struct {
	ID       int64    `json:"id"`
	OrigPath string   `json:"origPath"`
	DestPath string   `json:"destPath"`
	LinkSrc  string   `json:"linkSrc"`
	State    string   `json:"state"`
	Err      string   `json:"err"`
	Hash     [32]byte `json:"-"`
	Size     uint64   `json:"size"`
	MtimeNs  int64    `json:"mtimeNs"`
}

// BeginOp 在任何文件系统动作之前落盘整笔操作的全部计划（state=planned）。
// 进程若在执行中死亡，残留 planned 由下次 Open 收口为 interrupted。
func (s *Store) BeginOp(kind, targetDir string, histID int64, undoable bool, items []OpItemPlan) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO op_records (op_kind, created_at, target_dir, hist_id, undoable)
		VALUES (?, ?, ?, ?, ?)`,
		kind, time.Now().Unix(), targetDir, histID, undoable)
	if err != nil {
		return 0, err
	}
	opID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO op_items (op_id, orig_path, hash, size, mtime_ns, state)
			VALUES (?, ?, ?, ?, ?, ?)`,
			opID, it.OrigPath, it.Hash[:], it.Size, it.MtimeNs, StatePlanned); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return opID, nil
}

// FinishItem 收口一条计划：写入实际去向与终态。同一原路径重复收口以最后一次为准。
func (s *Store) FinishItem(opID int64, origPath, destPath, linkSrc, state, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE op_items SET dest_path = ?, link_src = ?, state = ?, err = ?
		WHERE op_id = ? AND orig_path = ?`,
		destPath, linkSrc, state, errMsg, opID, origPath)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("操作 %d 中不存在计划条目: %s", opID, origPath)
	}
	return nil
}

// FinalizeOp 正常收尾：残留 planned（未派发，多为取消）置 cancelled，
// 冻结 done 计数，并把执行器算好的 reclaimed 原样落库。
//
// reclaimed 一律**由调用方传入**，库内不再用 SUM(size) 重算（B6 / R-操作-3）。
// 旧实现在此重算 `SUM(size) WHERE state=done`，是 kind-blind 的：它对
// hardlink / symlink / trash / 同卷 move 这些"源字节并未真正从磁盘移除"的操作
// 也按条目 size 累加，历史页于是显示一笔**假账**。执行器早已按 kind 精确算出
// res.Reclaimed（仅 delete + 跨卷 move；链接类归 LinkedBytes/SymlinkedBytes，
// 回收站归 TrashedBytes，见 internal/model.OpsResult 与 executor.go），
// FinalizeOp 的职责只是忠实地把这个数落库，与执行器同口径。
//
// 因此 trash / hardlink / symlink / 同卷 move 的历史记录「回收空间」显示 0 是
// **正确**的：那些字节没有从磁盘上消失。要补链接类统计请新增列，不要动这里。
// done_count 仍由库内 COUNT 得出——它是执行事实的汇总，无口径歧义。
func (s *Store) FinalizeOp(opID int64, reclaimed uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE op_items SET state = ? WHERE op_id = ? AND state = ?`,
		StateCancelled, opID, StatePlanned); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE op_records SET
		done_count = (SELECT COUNT(*) FROM op_items WHERE op_id = ? AND state = ?),
		reclaimed  = ?
		WHERE id = ?`,
		opID, StateDone, reclaimed, opID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("操作记录不存在（id=%d）", opID)
	}
	return tx.Commit()
}

// MarkItemUndo 记录单条回撤状态：执行前写 undoing（写前，I6），
// 收口写 undone / undo_failed。原 done 记录保留，审计链不断。
func (s *Store) MarkItemUndo(itemID int64, state, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE op_items SET state = ?, err = ? WHERE id = ?`,
		state, errMsg, itemID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("操作条目不存在（id=%d）", itemID)
	}
	return nil
}

// opMetaCols 聚合口径：
//   - Done = 执行成功过的条目（含其后被回撤/回撤失败的，回撤不冲销执行账目）；
//     正在回撤中的 undoing 也计入，否则回撤进行中刷新明细会让 Done 凭空少 1
//   - Undone = 已成功回撤；Failed = 执行失败（不含回撤失败，明细在条目状态）
//   - Reclaimed 只在 FinalizeOp 冻结一次，此处取列值不重算（见下方注释）
const opMetaCols = `r.id, r.op_kind, r.created_at, r.target_dir, r.hist_id, r.undoable,
	r.reclaimed, COUNT(i.id),
	COALESCE(SUM(i.state IN (?, ?, ?, ?)), 0),
	COALESCE(SUM(i.state = ?), 0),
	COALESCE(SUM(i.state = ?), 0)`

func scanOpMeta(row interface{ Scan(...any) error }) (OpMeta, error) {
	var (
		m     OpMeta
		und   int64
		recl  int64
		done  int
		undoN int
		failN int
	)
	err := row.Scan(&m.ID, &m.Kind, &m.CreatedAt, &m.TargetDir, &m.HistID, &und,
		&recl, &m.Items, &done, &undoN, &failN)
	if err != nil {
		return m, err
	}
	m.Undoable = und != 0
	m.Reclaimable = uint64(recl)
	m.Done = done
	m.Undone = undoN
	m.Failed = failN
	return m, nil
}

// opMetaAggArgs 与 opMetaCols 中的六个状态占位符一一对应。
var opMetaAggArgs = []any{StateDone, StateUndone, StateUndoFailed, StateUndoing,
	StateUndone, StateFailed}

// ListOps 操作记录列表（新→旧）。
func (s *Store) ListOps() ([]OpMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT `+opMetaCols+` FROM op_records r
		LEFT JOIN op_items i ON i.op_id = r.id
		GROUP BY r.id
		ORDER BY r.created_at DESC, r.id DESC`,
		opMetaAggArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// 2026-09-20 缺陷：原先写作 `var out []OpMeta`，空表时返回 nil slice，
	// 序列化成 JSON 是 null 而不是 []。前端拿到 null 后取 .length 抛 TypeError，
	// 整个记录页渲染中断——连"暂无清理记录"的提示都不显示，用户看到一片空白。
	// 空列表的正确形状是 []，不是 null（同 executor.go 的 OK: []string{}）。
	out := make([]OpMeta, 0, 16)
	for rows.Next() {
		m, err := scanOpMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetOp 单条操作及其全部条目（按登记序）。
func (s *Store) GetOp(opID int64) (*OpMeta, []OpItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	args := append(append([]any{}, opMetaAggArgs...), opID)
	m, err := scanOpMeta(s.db.QueryRow(`SELECT `+opMetaCols+` FROM op_records r
		LEFT JOIN op_items i ON i.op_id = r.id
		WHERE r.id = ?
		GROUP BY r.id`,
		args...))
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("操作记录不存在（id=%d）", opID)
	}
	if err != nil {
		return nil, nil, err
	}

	rows, err := s.db.Query(`SELECT id, orig_path, dest_path, link_src, state, err, hash, size, mtime_ns
		FROM op_items WHERE op_id = ? ORDER BY id`, opID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	// 同 ListOps：空明细必须是 []，不能是 nil（→ JSON null）。
	// 这处在"记录详情展开"时才会遇到：一笔操作若一条明细都没落账，
	// 展开后同样是整块渲染中断。
	items := make([]OpItem, 0, 8)
	for rows.Next() {
		var (
			it OpItem
			hb []byte
			sz int64
		)
		if err := rows.Scan(&it.ID, &it.OrigPath, &it.DestPath, &it.LinkSrc,
			&it.State, &it.Err, &hb, &sz, &it.MtimeNs); err != nil {
			return nil, nil, err
		}
		copy(it.Hash[:], hb)
		it.Size = uint64(sz)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return &m, items, nil
}

// ClearOps 清空全部操作记录（级联清条目）。
func (s *Store) ClearOps() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM op_records`)
	return err
}
