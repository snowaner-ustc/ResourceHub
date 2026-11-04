package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
)

var defaultSkipDirs = []string{".git", "lost+found", "node_modules", ".cache", "overlay2", "__pycache__"}

func (s *Store) EnqueueDiskScan(hostID, root string, topN int, force bool) (*models.Job, error) {
	if _, err := s.HostByID(hostID); err != nil {
		return nil, err
	}
	if topN <= 0 {
		topN = 20
	}
	if topN > 50 {
		topN = 50
	}
	cacheKey := "disk_scan:" + root
	if active, err := s.findActiveJob(hostID, cacheKey); err != nil {
		return nil, err
	} else if active != nil {
		return active, nil
	}
	if !force {
		if cached, err := s.findCachedJob(hostID, cacheKey, models.DiskScanCacheTTL); err != nil {
			return nil, err
		} else if cached != nil {
			cached.Cached = true
			return cached, nil
		}
	}
	params, err := json.Marshal(models.DiskScanParams{
		Root:      root,
		TopN:      topN,
		TimeoutMs: 30000,
		MaxFiles:  200000,
		LargeN:    20,
		Skip:      defaultSkipDirs,
	})
	if err != nil {
		return nil, err
	}
	return s.insertJob(hostID, models.JobKindDiskScan, cacheKey, params)
}

func (s *Store) EnqueueQuotaScan(hostID string, policy *models.QuotaPolicy, force bool) (*models.Job, error) {
	if policy == nil {
		return nil, fmt.Errorf("policy required")
	}
	if _, err := s.HostByID(hostID); err != nil {
		return nil, err
	}
	cacheKey := "quota_scan:" + policy.ID
	if active, err := s.findActiveJob(hostID, cacheKey); err != nil {
		return nil, err
	} else if active != nil {
		return active, nil
	}
	interval := time.Duration(policy.ScanIntervalSeconds) * time.Second
	if interval < time.Minute {
		interval = time.Minute
	}
	if !force {
		if cached, err := s.findCachedJob(hostID, cacheKey, interval); err != nil {
			return nil, err
		} else if cached != nil {
			cached.Cached = true
			return cached, nil
		}
	}
	timeoutMs := 90000
	if policy.ScanIntervalSeconds >= 3600 {
		timeoutMs = 120000
	}
	params, err := json.Marshal(models.QuotaScanParams{
		PolicyID:    policy.ID,
		PathPattern: policy.PathPattern,
		Exclude:     policy.Exclude,
		TimeoutMs:   timeoutMs,
		MaxFiles:    200000,
		MaxTargets:  500,
	})
	if err != nil {
		return nil, err
	}
	return s.insertJob(hostID, models.JobKindQuotaScan, cacheKey, params)
}

func (s *Store) insertJob(hostID, kind, cacheKey string, params []byte) (*models.Job, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO jobs (id, host_id, kind, status, cache_key, params, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, hostID, kind, models.JobStatusQueued, cacheKey, string(params), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return s.GetJob(id)
}

func (s *Store) findActiveJob(hostID, cacheKey string) (*models.Job, error) {
	row := s.db.QueryRow(
		`SELECT `+jobColumns+` FROM jobs WHERE host_id = ? AND cache_key = ? AND status IN (?, ?) ORDER BY created_at DESC LIMIT 1`,
		hostID, cacheKey, models.JobStatusQueued, models.JobStatusRunning,
	)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func (s *Store) findCachedJob(hostID, cacheKey string, ttl time.Duration) (*models.Job, error) {
	cutoff := time.Now().UTC().Add(-ttl).Format(time.RFC3339Nano)
	row := s.db.QueryRow(
		`SELECT `+jobColumns+` FROM jobs WHERE host_id = ? AND cache_key = ? AND status = ? AND finished_at IS NOT NULL AND finished_at >= ? ORDER BY finished_at DESC LIMIT 1`,
		hostID, cacheKey, models.JobStatusDone, cutoff,
	)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func (s *Store) ClaimJob(hostID string) (*models.Job, error) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	var running int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE host_id = ? AND status = ?`, hostID, models.JobStatusRunning).Scan(&running); err != nil {
		return nil, err
	}
	if running > 0 {
		return nil, nil
	}
	var id string
	err := s.db.QueryRow(`SELECT id FROM jobs WHERE host_id = ? AND status = ? ORDER BY created_at LIMIT 1`, hostID, models.JobStatusQueued).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE jobs SET status = ?, started_at = ? WHERE id = ? AND status = ?`, models.JobStatusRunning, now, id, models.JobStatusQueued)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil
	}
	return s.GetJob(id)
}

func (s *Store) FinishJob(id, hostID, status, errMsg string, report []byte) error {
	if status != models.JobStatusDone && status != models.JobStatusFailed {
		return fmt.Errorf("invalid job status")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var reportVal any
	if len(report) > 0 {
		reportVal = string(report)
	}
	res, err := s.db.Exec(
		`UPDATE jobs SET status = ?, error = ?, report = ?, finished_at = ? WHERE id = ? AND host_id = ? AND status = ?`,
		status, errMsg, reportVal, now, id, hostID, models.JobStatusRunning,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job not running")
	}
	return nil
}

func (s *Store) GetJob(id string) (*models.Job, error) {
	row := s.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("job not found")
		}
		return nil, err
	}
	return job, nil
}

func (s *Store) ListJobs(hostID, kind string, limit int) ([]models.Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := `SELECT ` + jobColumns + ` FROM jobs WHERE host_id = ?`
	args := []any{hostID}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	if out == nil {
		out = []models.Job{}
	}
	return out, rows.Err()
}

func (s *Store) FailStaleJobs(age time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-age).Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE jobs SET status = ?, error = ?, finished_at = ? WHERE status = ? AND started_at IS NOT NULL AND started_at < ?`,
		models.JobStatusFailed, "stale: agent did not report result", now, models.JobStatusRunning, cutoff,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const jobColumns = `id, host_id, kind, status, cache_key, params, report, error, created_at, started_at, finished_at`

func scanJob(row *sql.Row) (*models.Job, error) {
	var j models.Job
	var params, report, errMsg, created, started, finished sql.NullString
	if err := row.Scan(&j.ID, &j.HostID, &j.Kind, &j.Status, &j.CacheKey, &params, &report, &errMsg, &created, &started, &finished); err != nil {
		return nil, err
	}
	fillJob(&j, params, report, errMsg, created, started, finished)
	return &j, nil
}

func scanJobRows(rows *sql.Rows) (*models.Job, error) {
	var j models.Job
	var params, report, errMsg, created, started, finished sql.NullString
	if err := rows.Scan(&j.ID, &j.HostID, &j.Kind, &j.Status, &j.CacheKey, &params, &report, &errMsg, &created, &started, &finished); err != nil {
		return nil, err
	}
	fillJob(&j, params, report, errMsg, created, started, finished)
	return &j, nil
}

func fillJob(j *models.Job, params, report, errMsg, created, started, finished sql.NullString) {
	if params.Valid {
		j.Params = json.RawMessage(params.String)
	}
	if report.Valid && report.String != "" {
		j.Report = json.RawMessage(report.String)
	}
	if errMsg.Valid {
		j.Error = errMsg.String
	}
	if created.Valid {
		t, _ := time.Parse(time.RFC3339Nano, created.String)
		j.CreatedAt = t
	}
	if started.Valid {
		t, _ := time.Parse(time.RFC3339Nano, started.String)
		j.StartedAt = &t
	}
	if finished.Valid {
		t, _ := time.Parse(time.RFC3339Nano, finished.String)
		j.FinishedAt = &t
	}
}
