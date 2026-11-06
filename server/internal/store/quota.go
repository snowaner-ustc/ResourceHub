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
)

var ErrPolicyNotFound = errors.New("policy not found")

func (s *Store) CreateQuotaPolicy(p models.QuotaPolicy) (*models.QuotaPolicy, error) {
	if err := normalizePolicy(&p); err != nil {
		return nil, err
	}
	p.ID = uuid.NewString()
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	ex, _ := json.Marshal(p.Exclude)
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO quota_policies (id, name, enabled, host_group, path_pattern, match_mode, limit_bytes, warn_percent, critical_percent, scan_interval_seconds, exclude_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, enabled, p.HostGroup, p.PathPattern, p.MatchMode, int64(p.LimitBytes), p.WarnPercent, p.CriticalPercent, p.ScanIntervalSeconds, string(ex),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) UpdateQuotaPolicy(p models.QuotaPolicy) (*models.QuotaPolicy, error) {
	existing, err := s.GetQuotaPolicy(p.ID)
	if err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = existing.Name
	}
	if p.PathPattern == "" {
		p.PathPattern = existing.PathPattern
	}
	if p.LimitBytes == 0 {
		p.LimitBytes = existing.LimitBytes
	}
	if p.MatchMode == "" {
		p.MatchMode = existing.MatchMode
	}
	if p.WarnPercent == 0 {
		p.WarnPercent = existing.WarnPercent
	}
	if p.CriticalPercent == 0 {
		p.CriticalPercent = existing.CriticalPercent
	}
	if p.ScanIntervalSeconds == 0 {
		p.ScanIntervalSeconds = existing.ScanIntervalSeconds
	}
	if p.Exclude == nil {
		p.Exclude = existing.Exclude
	}
	p.CreatedAt = existing.CreatedAt
	if err := normalizePolicy(&p); err != nil {
		return nil, err
	}
	p.UpdatedAt = time.Now().UTC()
	ex, _ := json.Marshal(p.Exclude)
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	_, err = s.db.Exec(
		`UPDATE quota_policies SET name=?, enabled=?, host_group=?, path_pattern=?, match_mode=?, limit_bytes=?, warn_percent=?, critical_percent=?, scan_interval_seconds=?, exclude_json=?, updated_at=? WHERE id=?`,
		p.Name, enabled, p.HostGroup, p.PathPattern, p.MatchMode, int64(p.LimitBytes), p.WarnPercent, p.CriticalPercent, p.ScanIntervalSeconds, string(ex),
		p.UpdatedAt.Format(time.RFC3339Nano), p.ID,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) DeleteQuotaPolicy(id string) error {
	res, err := s.db.Exec(`DELETE FROM quota_policies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (s *Store) GetQuotaPolicy(id string) (*models.QuotaPolicy, error) {
	row := s.db.QueryRow(`SELECT `+policyColumns+` FROM quota_policies WHERE id = ?`, id)
	p, err := scanPolicy(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPolicyNotFound
		}
		return nil, err
	}
	return p, nil
}

func (s *Store) ListQuotaPolicies() ([]models.QuotaPolicy, error) {
	rows, err := s.db.Query(`SELECT ` + policyColumns + ` FROM quota_policies ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.QuotaPolicy
	for rows.Next() {
		p, err := scanPolicyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if out == nil {
		out = []models.QuotaPolicy{}
	}
	return out, rows.Err()
}

// UpsertQuotaUsages writes measured paths without deleting others.
// Use this for partial scans so unscanned paths keep their last usage.
func (s *Store) UpsertQuotaUsages(hostID, policyID string, rows []models.QuotaUsage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO quota_usages (host_id, policy_id, path, owner, used_bytes, limit_bytes, used_percent, as_of)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id, policy_id, path) DO UPDATE SET
			owner = excluded.owner,
			used_bytes = excluded.used_bytes,
			limit_bytes = excluded.limit_bytes,
			used_percent = excluded.used_percent,
			as_of = excluded.as_of`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, u := range rows {
		if _, err := stmt.Exec(hostID, policyID, u.Path, u.Owner, int64(u.UsedBytes), int64(u.LimitBytes), u.UsedPercent, u.AsOf.UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ReplaceQuotaUsages drops previous rows for the host+policy, then inserts `rows`.
// Use this only for complete scans.
func (s *Store) ReplaceQuotaUsages(hostID, policyID string, rows []models.QuotaUsage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM quota_usages WHERE host_id = ? AND policy_id = ?`, hostID, policyID); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO quota_usages (host_id, policy_id, path, owner, used_bytes, limit_bytes, used_percent, as_of) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, u := range rows {
		if _, err := stmt.Exec(hostID, policyID, u.Path, u.Owner, int64(u.UsedBytes), int64(u.LimitBytes), u.UsedPercent, u.AsOf.UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListQuotaUsages(hostID, policyID string) ([]models.QuotaUsage, error) {
	q := `SELECT u.host_id, h.name, u.policy_id, p.name, u.path, u.owner, u.used_bytes, u.limit_bytes, u.used_percent, u.as_of
		FROM quota_usages u
		JOIN hosts h ON h.id = u.host_id
		JOIN quota_policies p ON p.id = u.policy_id
		WHERE 1=1`
	var args []any
	if hostID != "" {
		q += ` AND u.host_id = ?`
		args = append(args, hostID)
	}
	if policyID != "" {
		q += ` AND u.policy_id = ?`
		args = append(args, policyID)
	}
	q += ` ORDER BY u.used_percent DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.QuotaUsage
	for rows.Next() {
		var u models.QuotaUsage
		var asOf string
		var used, limit int64
		if err := rows.Scan(&u.HostID, &u.HostName, &u.PolicyID, &u.PolicyName, &u.Path, &u.Owner, &used, &limit, &u.UsedPercent, &asOf); err != nil {
			return nil, err
		}
		u.UsedBytes = uint64(used)
		u.LimitBytes = uint64(limit)
		u.AsOf, _ = time.Parse(time.RFC3339Nano, asOf)
		out = append(out, u)
	}
	if out == nil {
		out = []models.QuotaUsage{}
	}
	return out, rows.Err()
}

func (s *Store) ListFiringAlertsByHost(hostID string) ([]models.Alert, error) {
	rows, err := s.db.Query(
		`SELECT id, host_id, rule_type, severity, status, message, payload, fired_at, resolved_at FROM alerts WHERE host_id = ? AND status = ?`,
		hostID, models.AlertStatusFiring,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Alert
	for rows.Next() {
		var a models.Alert
		var payload, fired, resolved sql.NullString
		if err := rows.Scan(&a.ID, &a.HostID, &a.RuleType, &a.Severity, &a.Status, &a.Message, &payload, &fired, &resolved); err != nil {
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
		out = append(out, a)
	}
	return out, rows.Err()
}

func normalizePolicy(p *models.QuotaPolicy) error {
	p.Name = strings.TrimSpace(p.Name)
	p.PathPattern = strings.TrimSpace(p.PathPattern)
	p.HostGroup = strings.TrimSpace(p.HostGroup)
	if p.Name == "" {
		return fmt.Errorf("name required")
	}
	if err := models.ValidateSlowPath(p.PathPattern); err != nil {
		return err
	}
	if p.LimitBytes == 0 {
		return fmt.Errorf("limit_bytes required")
	}
	if p.MatchMode == "" {
		p.MatchMode = "glob"
	}
	if p.WarnPercent <= 0 {
		p.WarnPercent = 80
	}
	if p.CriticalPercent <= 0 {
		p.CriticalPercent = 100
	}
	if p.ScanIntervalSeconds <= 0 {
		p.ScanIntervalSeconds = 86400
	}
	if p.ScanIntervalSeconds < 60 {
		p.ScanIntervalSeconds = 60
	}
	if p.Exclude == nil {
		p.Exclude = []string{}
	}
	return nil
}

const policyColumns = `id, name, enabled, host_group, path_pattern, match_mode, limit_bytes, warn_percent, critical_percent, scan_interval_seconds, exclude_json, created_at, updated_at`

func scanPolicy(row *sql.Row) (*models.QuotaPolicy, error) {
	var p models.QuotaPolicy
	var enabled int
	var limit int64
	var ex, created, updated string
	if err := row.Scan(&p.ID, &p.Name, &enabled, &p.HostGroup, &p.PathPattern, &p.MatchMode, &limit, &p.WarnPercent, &p.CriticalPercent, &p.ScanIntervalSeconds, &ex, &created, &updated); err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	p.LimitBytes = uint64(limit)
	_ = json.Unmarshal([]byte(ex), &p.Exclude)
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &p, nil
}

func scanPolicyRows(rows *sql.Rows) (*models.QuotaPolicy, error) {
	var p models.QuotaPolicy
	var enabled int
	var limit int64
	var ex, created, updated string
	if err := rows.Scan(&p.ID, &p.Name, &enabled, &p.HostGroup, &p.PathPattern, &p.MatchMode, &limit, &p.WarnPercent, &p.CriticalPercent, &p.ScanIntervalSeconds, &ex, &created, &updated); err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	p.LimitBytes = uint64(limit)
	_ = json.Unmarshal([]byte(ex), &p.Exclude)
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &p, nil
}
