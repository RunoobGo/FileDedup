package history

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/model"
)

// ScanMeta 历史记录摘要（列表展示与恢复入口共用）。
// JSON 标签与前端 HistoryMeta 约定一致（camelCase）。
type ScanMeta struct {
	ID          int64              `json:"id"`
	SavedAt     int64              `json:"savedAt"` // Unix 秒
	Roots       []string           `json:"roots"`
	Filters     model.Filters      `json:"filters"`
	Threads     int                `json:"threads"`
	Paranoid    bool               `json:"paranoid"`
	Groups      int                `json:"groups"`
	Files       int                `json:"files"`
	OrigFiles   int                `json:"origFiles"` // 保存时的文件数（裁剪后不变，供对比）
	Reclaimable uint64             `json:"reclaimable"`
	Failed      []model.FailedItem `json:"failed"`
	KeepPaths   []string           `json:"keepPaths"`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}

func decodeJSON(b []byte, v any) {
	if len(b) == 0 {
		return
	}
	_ = json.Unmarshal(b, v)
}

// SaveScan 保存一次扫描结果（同一事务写入组/文件行），并淘汰超出
// MaxScanHistory 的最旧记录（CASCADE 清子表）。返回历史行 ID。
// 文件行按「组序 + 组内原序」插入，rowid 全局唯一且单调，
// LoadScan 直接以 rowid 作为 FileEntry.ID。
func (s *Store) SaveScan(cfg model.ScanConfig, groups []*model.DuplicateGroup, failed []model.FailedItem) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var files, reclaim uint64
	for _, g := range groups {
		files += uint64(len(g.Files))
		reclaim += g.Reclaimable
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO scan_history
		(saved_at, roots, filters, threads, paranoid,
		 groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]')`,
		time.Now().Unix(), mustJSON(cfg.Roots), mustJSON(cfg.Filters),
		cfg.Threads, cfg.Paranoid, len(groups), files, files, reclaim,
		mustJSON(failed))
	if err != nil {
		return 0, err
	}
	histID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for ord, g := range groups {
		if len(g.Files) == 0 {
			continue
		}
		gres, err := tx.Exec(`INSERT INTO hist_groups (hist_id, hash, size, reclaimable, ord)
			VALUES (?, ?, ?, ?, ?)`,
			histID, g.Hash[:], g.Files[0].Size, g.Reclaimable, ord)
		if err != nil {
			return 0, err
		}
		gid, err := gres.LastInsertId()
		if err != nil {
			return 0, err
		}
		for _, f := range g.Files {
			if _, err := tx.Exec(`INSERT INTO hist_files (hist_id, group_id, path, size, mtime_ns)
				VALUES (?, ?, ?, ?, ?)`,
				histID, gid, f.Path, f.Size, f.ModTime); err != nil {
				return 0, err
			}
		}
	}
	// 淘汰最旧：按 id 降序保留前 MaxScanHistory 条，其余删除（外键级联清子行）
	if _, err := tx.Exec(`DELETE FROM scan_history WHERE id IN (
		SELECT id FROM scan_history ORDER BY id DESC LIMIT -1 OFFSET ?)`,
		MaxScanHistory); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return histID, nil
}

const metaCols = `id, saved_at, roots, filters, threads, paranoid,
	groups_count, files_count, orig_files, reclaimable, failed_json, keep_paths`

func scanMeta(scan interface{ Scan(...any) error }) (ScanMeta, error) {
	var (
		m                                    ScanMeta
		rootsJS, filtersJS, failedJS, keepJS []byte
		reclaim                              int64
	)
	err := scan.Scan(&m.ID, &m.SavedAt, &rootsJS, &filtersJS, &m.Threads, &m.Paranoid,
		&m.Groups, &m.Files, &m.OrigFiles, &reclaim, &failedJS, &keepJS)
	if err != nil {
		return m, err
	}
	m.Reclaimable = uint64(reclaim)
	decodeJSON(rootsJS, &m.Roots)
	decodeJSON(filtersJS, &m.Filters)
	decodeJSON(failedJS, &m.Failed)
	decodeJSON(keepJS, &m.KeepPaths)
	return m, nil
}

// ListScans 历史列表（新→旧）。
func (s *Store) ListScans() ([]ScanMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT ` + metaCols + ` FROM scan_history
		ORDER BY saved_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScanMeta
	for rows.Next() {
		m, err := scanMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LoadScan 读取一条历史及其完整结果集。组按保存序（ord）、
// 文件按插入序（rowid）返回，与保存时的顺序一致。
func (s *Store) LoadScan(id int64) (ScanMeta, []*model.DuplicateGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := scanMeta(s.db.QueryRow(`SELECT `+metaCols+` FROM scan_history WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return m, nil, fmt.Errorf("历史记录不存在（id=%d）", id)
	}
	if err != nil {
		return m, nil, err
	}

	grows, err := s.db.Query(`SELECT id, hash, reclaimable FROM hist_groups
		WHERE hist_id = ? ORDER BY ord`, id)
	if err != nil {
		return m, nil, err
	}
	type groupRow struct {
		id          int64
		hash        []byte
		reclaimable uint64
	}
	var grs []groupRow
	for grows.Next() {
		var gr groupRow
		if err := grows.Scan(&gr.id, &gr.hash, &gr.reclaimable); err != nil {
			grows.Close()
			return m, nil, err
		}
		grs = append(grs, gr)
	}
	if err := grows.Err(); err != nil {
		grows.Close()
		return m, nil, err
	}
	grows.Close()

	groups := make([]*model.DuplicateGroup, 0, len(grs))
	for _, gr := range grs {
		g := &model.DuplicateGroup{GroupID: uint64(gr.id), Reclaimable: gr.reclaimable}
		copy(g.Hash[:], gr.hash)
		frows, err := s.db.Query(`SELECT id, path, size, mtime_ns FROM hist_files
			WHERE group_id = ? ORDER BY id`, gr.id)
		if err != nil {
			return m, nil, err
		}
		for frows.Next() {
			var (
				fid   int64
				path  string
				size  uint64
				mtime int64
			)
			if err := frows.Scan(&fid, &path, &size, &mtime); err != nil {
				frows.Close()
				return m, nil, err
			}
			g.Files = append(g.Files, &model.FileEntry{
				ID: uint64(fid), Path: path, Size: size, ModTime: mtime,
				Ext: strings.ToLower(filepath.Ext(path)), // 结果页扩展名筛选依赖
			})
		}
		if err := frows.Err(); err != nil {
			frows.Close()
			return m, nil, err
		}
		frows.Close()
		groups = append(groups, g)
	}
	return m, groups, nil
}

// UpdateKeepPaths 覆盖保存手动/策略决策的保留路径集合（功能 3：恢复后
// 保留建议不丢；FileID 是运行内编号，跨会话只有路径稳定）。
func (s *Store) UpdateKeepPaths(histID int64, paths []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE scan_history SET keep_paths = ? WHERE id = ?`,
		mustJSON(paths), histID)
	return err
}

// PruneScanFiles 按已消失路径裁剪历史（清理操作完成后调用）：删文件行、
// 删不足 2 员的组、重算组与汇总计数，并从 keep_paths 移除已删路径。
func (s *Store) PruneScanFiles(histID int64, gone map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for path := range gone {
		if _, err := tx.Exec(`DELETE FROM hist_files WHERE hist_id = ? AND path = ?`,
			histID, path); err != nil {
			return err
		}
	}
	// 不足 2 员的组失去意义（与 app 层结果集清理同语义），一并移除
	if _, err := tx.Exec(`DELETE FROM hist_groups WHERE hist_id = ? AND id NOT IN (
		SELECT group_id FROM hist_files WHERE hist_id = ?
		GROUP BY group_id HAVING COUNT(*) >= 2)`,
		histID, histID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE hist_groups SET reclaimable = (
		(SELECT COUNT(*) FROM hist_files f WHERE f.group_id = hist_groups.id) - 1
	) * size WHERE hist_id = ?`, histID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE scan_history SET
		groups_count = (SELECT COUNT(*) FROM hist_groups WHERE hist_id = scan_history.id),
		files_count  = (SELECT COUNT(*) FROM hist_files WHERE hist_id = scan_history.id),
		reclaimable  = (SELECT COALESCE(SUM(reclaimable), 0) FROM hist_groups WHERE hist_id = scan_history.id)
		WHERE id = ?`, histID); err != nil {
		return err
	}

	// keep_paths 与已删路径求交
	var keepJS []byte
	err = tx.QueryRow(`SELECT keep_paths FROM scan_history WHERE id = ?`, histID).Scan(&keepJS)
	if err == sql.ErrNoRows {
		return fmt.Errorf("历史记录不存在（id=%d）", histID)
	}
	if err != nil {
		return err
	}
	var keep []string
	decodeJSON(keepJS, &keep)
	if len(keep) > 0 {
		out := keep[:0]
		for _, p := range keep {
			if !gone[p] {
				out = append(out, p)
			}
		}
		if _, err := tx.Exec(`UPDATE scan_history SET keep_paths = ? WHERE id = ?`,
			mustJSON(out), histID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteScan 删除一条历史（级联清组/文件行）。
func (s *Store) DeleteScan(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM scan_history WHERE id = ?`, id)
	return err
}

// ClearScans 清空全部扫描历史（op 记录不受影响，另见 ClearOps）。
func (s *Store) ClearScans() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM scan_history`)
	return err
}
