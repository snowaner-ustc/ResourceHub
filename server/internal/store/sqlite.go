package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS hosts (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	hostname TEXT NOT NULL,
	agent_version TEXT NOT NULL DEFAULT '',
	token TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'offline',
	last_seen_at TEXT,
	created_at TEXT NOT NULL,
	group_name TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS metric_snapshots (
	host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
	collected_at TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS alerts (
	id TEXT PRIMARY KEY,
	host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
	rule_type TEXT NOT NULL,
	severity TEXT NOT NULL,
	status TEXT NOT NULL,
	message TEXT NOT NULL,
	payload TEXT,
	fired_at TEXT NOT NULL,
	resolved_at TEXT
);
CREATE TABLE IF NOT EXISTS metric_samples (
	host_id TEXT NOT NULL,
	metric_name TEXT NOT NULL,
	ts TEXT NOT NULL,
	value REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
CREATE INDEX IF NOT EXISTS idx_alerts_host ON alerts(host_id);
CREATE INDEX IF NOT EXISTS idx_samples_host_metric_ts ON metric_samples(host_id, metric_name, ts);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if !s.hasColumn("hosts", "group_name") {
		if _, err := s.db.Exec(`ALTER TABLE hosts ADD COLUMN group_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hasColumn(table, col string) bool {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == col {
			return true
		}
	}
	return false
}

func (s *Store) RegisterHost(req models.RegisterRequest) (*models.RegisterResponse, error) {
	id := uuid.NewString()
	token := uuid.NewString()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO hosts (id, name, hostname, agent_version, token, status, created_at, group_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Hostname, req.AgentVersion, token, models.HostStatusOffline, now.Format(time.RFC3339Nano), req.Group,
	)
	if err != nil {
		return nil, err
	}
	return &models.RegisterResponse{HostID: id, Token: token}, nil
}

func (s *Store) HostByToken(token string) (*models.Host, error) {
	row := s.db.QueryRow(`SELECT id, name, hostname, agent_version, token, status, last_seen_at, created_at, group_name FROM hosts WHERE token = ?`, token)
	return scanHost(row)
}

func (s *Store) HostByID(id string) (*models.Host, error) {
	row := s.db.QueryRow(`SELECT id, name, hostname, agent_version, token, status, last_seen_at, created_at, group_name FROM hosts WHERE id = ?`, id)
	return scanHost(row)
}

func (s *Store) UpdateHostGroup(id, group string) error {
	res, err := s.db.Exec(`UPDATE hosts SET group_name = ? WHERE id = ?`, group, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("host not found")
	}
	return nil
}

func scanHost(row *sql.Row) (*models.Host, error) {
	var h models.Host
	var lastSeen, created sql.NullString
	if err := row.Scan(&h.ID, &h.Name, &h.Hostname, &h.AgentVersion, &h.Token, &h.Status, &lastSeen, &created, &h.Group); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("host not found")
		}
		return nil, err
	}
	if lastSeen.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastSeen.String)
		if err == nil {
			h.LastSeenAt = &t
		}
	}
	if created.Valid {
		t, _ := time.Parse(time.RFC3339Nano, created.String)
		h.CreatedAt = t
	}
	return &h, nil
}

func (s *Store) TouchHost(hostID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE hosts SET status = ?, last_seen_at = ? WHERE id = ?`, models.HostStatusOnline, now, hostID)
	return err
}

func (s *Store) SaveMetrics(hostID string, snap models.MetricSnapshot) error {
	snap.HostID = hostID
	if prev, err := s.GetSnapshot(hostID); err == nil && prev != nil {
		if snap.Processes == nil && prev.Processes != nil {
			snap.Processes = prev.Processes
		}
		if snap.GPUs == nil && prev.GPUs != nil {
			snap.GPUs = prev.GPUs
		}
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO metric_snapshots (host_id, collected_at, payload) VALUES (?, ?, ?)
		 ON CONFLICT(host_id) DO UPDATE SET collected_at = excluded.collected_at, payload = excluded.payload`,
		hostID, snap.CollectedAt.UTC().Format(time.RFC3339Nano), string(payload),
	)
	if err != nil {
		return err
	}
	if err := s.insertSamples(hostID, snap); err != nil {
		return err
	}
	_ = s.PruneSamples(7 * 24 * time.Hour)
	return s.TouchHost(hostID)
}

func (s *Store) insertSamples(hostID string, snap models.MetricSnapshot) error {
	ts := snap.CollectedAt.UTC().Format(time.RFC3339Nano)
	points := map[string]float64{
		"cpu.usage_percent":     snap.CPU.UsagePercent,
		"memory.used_percent":   snap.Memory.UsedPercent,
		"disk.max_used_percent": 0,
	}
	for _, d := range snap.Disks {
		if d.UsedPercent > points["disk.max_used_percent"] {
			points["disk.max_used_percent"] = d.UsedPercent
		}
	}
	if snap.Processes != nil {
		points["process.total"] = float64(snap.Processes.Summary.Total)
		points["process.zombie"] = float64(snap.Processes.Summary.Zombie)
	}
	if snap.GPUs != nil && snap.GPUs.Available {
		var maxUtil, maxMem float64
		for _, g := range snap.GPUs.Devices {
			if g.UtilPercent > maxUtil {
				maxUtil = g.UtilPercent
			}
			if g.MemoryUsedPercent > maxMem {
				maxMem = g.MemoryUsedPercent
			}
		}
		points["gpu.max_util_percent"] = maxUtil
		points["gpu.max_mem_percent"] = maxMem
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO metric_samples (host_id, metric_name, ts, value) VALUES (?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for name, v := range points {
		if _, err := stmt.Exec(hostID, name, ts, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) QuerySamples(hostID string, names []string, from, to time.Time) ([]models.MetricPoint, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-time.Hour)
	}
	if len(names) == 0 {
		names = []string{"cpu.usage_percent", "memory.used_percent", "disk.max_used_percent"}
	}
	placeholders := make([]string, len(names))
	args := []any{hostID, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)}
	for i, n := range names {
		placeholders[i] = "?"
		args = append(args, n)
	}
	q := `SELECT metric_name, ts, value FROM metric_samples
		WHERE host_id = ? AND ts >= ? AND ts <= ? AND metric_name IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY ts ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MetricPoint
	for rows.Next() {
		var p models.MetricPoint
		var ts string
		if err := rows.Scan(&p.Metric, &ts, &p.Value); err != nil {
			return nil, err
		}
		p.TS, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, p)
	}
	if out == nil {
		out = []models.MetricPoint{}
	}
	return downsample(out, 360), rows.Err()
}

func downsample(in []models.MetricPoint, max int) []models.MetricPoint {
	if max <= 0 || len(in) <= max {
		return in
	}
	byMetric := map[string][]models.MetricPoint{}
	for _, p := range in {
		byMetric[p.Metric] = append(byMetric[p.Metric], p)
	}
	var out []models.MetricPoint
	for _, series := range byMetric {
		if len(series) <= max {
			out = append(out, series...)
			continue
		}
		step := float64(len(series)-1) / float64(max-1)
		for i := 0; i < max; i++ {
			idx := int(float64(i) * step)
			if idx >= len(series) {
				idx = len(series) - 1
			}
			out = append(out, series[idx])
		}
	}
	return out
}

func (s *Store) PruneSamples(maxAge time.Duration) error {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339Nano)
	_, err := s.db.Exec(`DELETE FROM metric_samples WHERE ts < ?`, cutoff)
	return err
}

func (s *Store) SustainedAbove(hostID, metric string, threshold float64, window time.Duration) (bool, error) {
	from := time.Now().UTC().Add(-window)
	pts, err := s.QuerySamples(hostID, []string{metric}, from, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if len(pts) < 3 {
		return false, nil
	}
	for _, p := range pts {
		if p.Value < threshold {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) GetSnapshot(hostID string) (*models.MetricSnapshot, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM metric_snapshots WHERE host_id = ?`, hostID).Scan(&payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var snap models.MetricSnapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (s *Store) ListHosts(group string) ([]models.HostSummary, error) {
	q := `SELECT id, name, hostname, agent_version, status, last_seen_at, created_at, group_name FROM hosts`
	var args []any
	if group != "" {
		q += ` WHERE group_name = ?`
		args = append(args, group)
	}
	q += ` ORDER BY name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.HostSummary
	for rows.Next() {
		var h models.Host
		var lastSeen, created sql.NullString
		if err := rows.Scan(&h.ID, &h.Name, &h.Hostname, &h.AgentVersion, &h.Status, &lastSeen, &created, &h.Group); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t, _ := time.Parse(time.RFC3339Nano, lastSeen.String)
			h.LastSeenAt = &t
		}
		if created.Valid {
			t, _ := time.Parse(time.RFC3339Nano, created.String)
			h.CreatedAt = t
		}
		summary := models.HostSummary{Host: h}
		if snap, err := s.GetSnapshot(h.ID); err == nil && snap != nil {
			summary.CPUUsagePercent = snap.CPU.UsagePercent
			summary.MemoryUsedPercent = snap.Memory.UsedPercent
			for _, d := range snap.Disks {
				if d.UsedPercent > summary.MaxDiskUsedPercent {
					summary.MaxDiskUsedPercent = d.UsedPercent
					summary.MaxDiskMountpoint = d.Mountpoint
				}
			}
			if snap.Processes != nil {
				summary.ProcessTotal = snap.Processes.Summary.Total
				summary.ZombieCount = snap.Processes.Summary.Zombie
			}
			if snap.GPUs != nil && snap.GPUs.Available {
				summary.GPUCount = len(snap.GPUs.Devices)
				for _, g := range snap.GPUs.Devices {
					if g.UtilPercent > summary.MaxGPUUtilPercent {
						summary.MaxGPUUtilPercent = g.UtilPercent
					}
					if g.MemoryUsedPercent > summary.MaxGPUMemPercent {
						summary.MaxGPUMemPercent = g.MemoryUsedPercent
					}
				}
			}
		}
		out = append(out, summary)
	}
	return out, rows.Err()
}

func (s *Store) MarkOfflineHosts(threshold time.Duration) ([]models.Host, error) {
	cutoff := time.Now().UTC().Add(-threshold).Format(time.RFC3339Nano)
	rows, err := s.db.Query(`SELECT id, name, hostname, agent_version, token, status, last_seen_at, created_at, group_name FROM hosts WHERE status = ? AND (last_seen_at IS NULL OR last_seen_at < ?)`, models.HostStatusOnline, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hosts []models.Host
	for rows.Next() {
		h, err := scanHostRow(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_, err = s.db.Exec(`UPDATE hosts SET status = ? WHERE status = ? AND (last_seen_at IS NULL OR last_seen_at < ?)`, models.HostStatusOffline, models.HostStatusOnline, cutoff)
	return hosts, err
}

func scanHostRow(rows *sql.Rows) (*models.Host, error) {
	var h models.Host
	var lastSeen, created sql.NullString
	if err := rows.Scan(&h.ID, &h.Name, &h.Hostname, &h.AgentVersion, &h.Token, &h.Status, &lastSeen, &created, &h.Group); err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastSeen.String)
		h.LastSeenAt = &t
	}
	if created.Valid {
		t, _ := time.Parse(time.RFC3339Nano, created.String)
		h.CreatedAt = t
	}
	return &h, nil
}

func (s *Store) UpsertAlert(a models.Alert) error {
	_, err := s.db.Exec(
		`INSERT INTO alerts (id, host_id, rule_type, severity, status, message, payload, fired_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET status = excluded.status, message = excluded.message, payload = excluded.payload, resolved_at = excluded.resolved_at`,
		a.ID, a.HostID, a.RuleType, a.Severity, a.Status, a.Message, a.Payload,
		a.FiredAt.UTC().Format(time.RFC3339Nano), nullableTime(a.ResolvedAt),
	)
	return err
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (s *Store) ResolveAlerts(hostID, ruleType string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE alerts SET status = ?, resolved_at = ? WHERE host_id = ? AND rule_type = ? AND status = ?`,
		models.AlertStatusResolved, now, hostID, ruleType, models.AlertStatusFiring)
	return err
}

func (s *Store) GetFiringAlert(hostID, ruleType string) (*models.Alert, error) {
	row := s.db.QueryRow(`SELECT id, host_id, rule_type, severity, status, message, payload, fired_at, resolved_at FROM alerts WHERE host_id = ? AND rule_type = ? AND status = ? ORDER BY fired_at DESC LIMIT 1`, hostID, ruleType, models.AlertStatusFiring)
	var a models.Alert
	var payload sql.NullString
	var fired, resolved sql.NullString
	if err := row.Scan(&a.ID, &a.HostID, &a.RuleType, &a.Severity, &a.Status, &a.Message, &payload, &fired, &resolved); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if payload.Valid {
		a.Payload = payload.String
	}
	if fired.Valid {
		t, _ := time.Parse(time.RFC3339Nano, fired.String)
		a.FiredAt = t
	}
	if resolved.Valid {
		t, _ := time.Parse(time.RFC3339Nano, resolved.String)
		a.ResolvedAt = &t
	}
	return &a, nil
}

func (s *Store) ListAlerts(status string) ([]models.Alert, error) {
	query := `SELECT a.id, a.host_id, h.name, a.rule_type, a.severity, a.status, a.message, a.payload, a.fired_at, a.resolved_at
		FROM alerts a JOIN hosts h ON h.id = a.host_id`
	var args []any
	if status != "" {
		query += ` WHERE a.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY a.fired_at DESC LIMIT 200`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Alert
	for rows.Next() {
		a, err := scanAlertRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) NewAlertID() string {
	return uuid.NewString()
}

func scanAlertRows(rows *sql.Rows) (*models.Alert, error) {
	var a models.Alert
	var payload sql.NullString
	var fired, resolved sql.NullString
	if err := rows.Scan(&a.ID, &a.HostID, &a.HostName, &a.RuleType, &a.Severity, &a.Status, &a.Message, &payload, &fired, &resolved); err != nil {
		return nil, err
	}
	if payload.Valid {
		a.Payload = payload.String
	}
	if fired.Valid {
		t, _ := time.Parse(time.RFC3339Nano, fired.String)
		a.FiredAt = t
	}
	if resolved.Valid {
		t, _ := time.Parse(time.RFC3339Nano, resolved.String)
		a.ResolvedAt = &t
	}
	return &a, nil
}
