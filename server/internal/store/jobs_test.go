package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
)

func TestDiskScanCacheAndClaim(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.EnqueueDiskScan(resp.HostID, "/var", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != models.JobStatusQueued {
		t.Fatalf("queued: %+v", job)
	}
	again, err := st.EnqueueDiskScan(resp.HostID, "/var", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != job.ID {
		t.Fatalf("expected reuse of active job, %s vs %s", again.ID, job.ID)
	}
	claimed, err := st.ClaimJob(resp.HostID)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %+v", err, claimed)
	}
	second, err := st.ClaimJob(resp.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if second != nil {
		t.Fatalf("concurrency=1 violated: %+v", second)
	}
	rep, _ := json.Marshal(map[string]any{"root": "/var", "entries": []any{}})
	if err := st.FinishJob(claimed.ID, resp.HostID, models.JobStatusDone, "", rep); err != nil {
		t.Fatal(err)
	}
	cached, err := st.EnqueueDiskScan(resp.HostID, "/var", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Cached || cached.ID != claimed.ID {
		t.Fatalf("expected cache hit, got %+v", cached)
	}
	forced, err := st.EnqueueDiskScan(resp.HostID, "/var", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if forced.ID == claimed.ID || forced.Status != models.JobStatusQueued {
		t.Fatalf("force should create new job: %+v", forced)
	}
}

func TestRejectUnknownHost(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.EnqueueDiskScan("missing", "/var", 10, true); err == nil {
		t.Fatal("expected error")
	}
}

func TestQuotaPolicyAndUsages(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateQuotaPolicy(models.QuotaPolicy{Name: "bad", PathPattern: "/", LimitBytes: 1}); err == nil {
		t.Fatal("should reject /")
	}
	p, err := st.CreateQuotaPolicy(models.QuotaPolicy{
		Name:        "home-sub",
		Enabled:     true,
		PathPattern: "/home/*/*",
		LimitBytes:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceQuotaUsages(host.HostID, p.ID, []models.QuotaUsage{{
		Path: "/home/a/x", Owner: "a", UsedBytes: 90, LimitBytes: 100, UsedPercent: 90, AsOf: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListQuotaUsages(host.HostID, p.ID)
	if err != nil || len(list) != 1 || list[0].Path != "/home/a/x" {
		t.Fatalf("usages: %+v %v", list, err)
	}
	if err := st.UpsertQuotaUsages(host.HostID, p.ID, []models.QuotaUsage{{
		Path: "/home/a/y", Owner: "a", UsedBytes: 10, LimitBytes: 100, UsedPercent: 10, AsOf: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	list, err = st.ListQuotaUsages(host.HostID, p.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("upsert should keep existing path, got %+v %v", list, err)
	}
}

func TestDiskScanCacheSkipsPartial(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.EnqueueDiskScan(resp.HostID, "/var", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimJob(resp.HostID)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	rep, _ := json.Marshal(map[string]any{"root": "/var", "partial": true, "entries": []any{}})
	if err := st.FinishJob(claimed.ID, resp.HostID, models.JobStatusDone, "", rep); err != nil {
		t.Fatal(err)
	}
	next, err := st.EnqueueDiskScan(resp.HostID, "/var", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if next.Cached || next.ID == job.ID {
		t.Fatalf("partial report must not be cached, got %+v", next)
	}
}

func TestFinishJobIdempotentAndRequeue(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.EnqueueDiskScan(resp.HostID, "/var/tmp", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(resp.HostID); err != nil {
		t.Fatal(err)
	}
	rep, _ := json.Marshal(map[string]any{"root": "/var/tmp", "partial": false})
	if err := st.FinishJob(job.ID, resp.HostID, models.JobStatusDone, "", rep); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishJob(job.ID, resp.HostID, models.JobStatusDone, "", rep); err != nil {
		t.Fatalf("idempotent finish: %v", err)
	}
	job2, err := st.EnqueueDiskScan(resp.HostID, "/var/cache", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(resp.HostID); err != nil {
		t.Fatal(err)
	}
	if err := st.RequeueJob(job2.ID, resp.HostID); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetJob(job2.ID)
	if err != nil || got.Status != models.JobStatusQueued {
		t.Fatalf("requeue: %+v %v", got, err)
	}
	if err := st.RequeueJob(job.ID, resp.HostID); err != nil {
		t.Fatalf("requeue of finished job should no-op: %v", err)
	}
}

func TestQuotaScanNotAutoEnqueuedUntilFirstDone(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, err := st.CreateQuotaPolicy(models.QuotaPolicy{
		Name: "home-sub", Enabled: true, PathPattern: "/home/*/*", LimitBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.EnqueueQuotaScan(host.HostID, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("new policy must not auto-enqueue, got %+v", job)
	}
	forced, err := st.EnqueueQuotaScan(host.HostID, p, true)
	if err != nil || forced == nil {
		t.Fatalf("manual first scan: %v %+v", err, forced)
	}
	if _, err := st.ClaimJob(host.HostID); err != nil {
		t.Fatal(err)
	}
	rep, _ := json.Marshal(map[string]any{"partial": false, "targets": []any{}})
	if err := st.FinishJob(forced.ID, host.HostID, models.JobStatusDone, "", rep); err != nil {
		t.Fatal(err)
	}
	cached, err := st.EnqueueQuotaScan(host.HostID, p, false)
	if err != nil || cached == nil || !cached.Cached {
		t.Fatalf("after first done, scheduler should reuse cache: %+v %v", cached, err)
	}
}

func TestFailStaleJobs(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.EnqueueDiskScan(resp.HostID, "/var/log", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(resp.HostID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`UPDATE jobs SET started_at = ? WHERE id = ?`, old, job.ID); err != nil {
		t.Fatal(err)
	}
	n, err := st.FailStaleJobs(10 * time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("stale n=%d err=%v", n, err)
	}
	got, err := st.GetJob(job.ID)
	if err != nil || got.Status != models.JobStatusFailed {
		t.Fatalf("got %+v %v", got, err)
	}
}
