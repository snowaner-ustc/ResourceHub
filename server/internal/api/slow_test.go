package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/alerts"
	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

func TestDiskScanAPIRejectsRootAndIsAsync(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(st, alerts.New(st, alerts.DefaultConfig()), time.Minute)
	h := srv.Handler()

	reg := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(`{"hostname":"h1","name":"n1"}`))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, reg)
	if rw.Code != http.StatusCreated {
		t.Fatalf("register %d %s", rw.Code, rw.Body.String())
	}
	var rr models.RegisterResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &rr)

	post := func(root string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"root": root})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/hosts/"+rr.HostID+"/disk-scans", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := post("/"); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for /, got %d %s", w.Code, w.Body.String())
	}

	scanRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(scanRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	w := post(scanRoot)
	if w.Code != http.StatusAccepted && w.Code != http.StatusOK {
		t.Fatalf("enqueue %d %s", w.Code, w.Body.String())
	}
	var job models.Job
	_ = json.Unmarshal(w.Body.Bytes(), &job)
	if job.Status != models.JobStatusQueued && job.Status != models.JobStatusDone {
		t.Fatalf("job should be async queued, got %+v", job)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+rr.Token)
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("claim %d %s", rw.Code, rw.Body.String())
	}
}

func TestQuotaResultIngestThenIdempotentFinish(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h, token, hostID := setupQuotaHost(t, st)

	polBody, _ := json.Marshal(map[string]any{
		"name": "p", "path_pattern": "/home/*/*", "limit_bytes": 100, "enabled": true,
	})
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota-policies", bytes.NewReader(polBody))
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusCreated {
		t.Fatalf("policy %d %s", rw.Code, rw.Body.String())
	}
	var pol models.QuotaPolicy
	_ = json.Unmarshal(rw.Body.Bytes(), &pol)

	rw = httptest.NewRecorder()
	scanBody, _ := json.Marshal(map[string]any{"host_id": hostID, "force": true})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quota-policies/"+pol.ID+"/scan", bytes.NewReader(scanBody))
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("scan %d %s", rw.Code, rw.Body.String())
	}

	claim := httptest.NewRequest(http.MethodGet, "/api/v1/agent/jobs", nil)
	claim.Header.Set("Authorization", "Bearer "+token)
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, claim)
	if rw.Code != http.StatusOK {
		t.Fatalf("claim %d %s", rw.Code, rw.Body.String())
	}
	var job models.Job
	_ = json.Unmarshal(rw.Body.Bytes(), &job)

	report, _ := json.Marshal(models.QuotaScanReport{
		PolicyID: pol.ID,
		Targets: []models.QuotaTarget{
			{Path: "/home/a/keep", UsedBytes: 90, Owner: "a"},
			{Path: "/home/a/gone", UsedBytes: 90, Owner: "a"},
		},
	})
	postResult := func(rep json.RawMessage) *httptest.ResponseRecorder {
		body, _ := json.Marshal(models.JobResultRequest{Status: models.JobStatusDone, Report: rep})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/agent/jobs/"+job.ID+"/result", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := postResult(report); w.Code != http.StatusOK {
		t.Fatalf("result %d %s", w.Code, w.Body.String())
	}
	tampered, _ := json.Marshal(models.QuotaScanReport{
		PolicyID: pol.ID,
		Targets:  []models.QuotaTarget{{Path: "/home/a/keep", UsedBytes: 1, Owner: "a"}},
	})
	if w := postResult(tampered); w.Code != http.StatusOK {
		t.Fatalf("replay result %d %s", w.Code, w.Body.String())
	}

	usages, err := st.ListQuotaUsages(hostID, pol.ID)
	if err != nil || len(usages) != 2 {
		t.Fatalf("usages %+v %v", usages, err)
	}
	for _, u := range usages {
		if u.Path == "/home/a/keep" && u.UsedBytes != 90 {
			t.Fatalf("terminal replay must not re-ingest, got %+v", u)
		}
	}

	partial, _ := json.Marshal(models.QuotaScanReport{
		PolicyID: pol.ID,
		Partial:  true,
		Targets:  []models.QuotaTarget{{Path: "/home/a/keep", UsedBytes: 95, Owner: "a"}},
	})
	job2, err := st.EnqueueQuotaScan(hostID, &pol, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(hostID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(models.JobResultRequest{Status: models.JobStatusDone, Report: partial})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent/jobs/"+job2.ID+"/result", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("partial result %d %s", rw.Code, rw.Body.String())
	}
	usages, err = st.ListQuotaUsages(hostID, pol.ID)
	if err != nil || len(usages) != 2 {
		t.Fatalf("partial must keep gone path, got %+v %v", usages, err)
	}
	gone, err := st.GetFiringAlert(hostID, models.QuotaRuleType(pol.ID, "/home/a/gone"))
	if err != nil || gone == nil {
		t.Fatalf("gone alert should still fire: %+v %v", gone, err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/quota-usages?host_id="+hostID, nil)
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("usages api %d %s", rw.Code, rw.Body.String())
	}
	var listed []models.QuotaUsage
	_ = json.Unmarshal(rw.Body.Bytes(), &listed)
	if len(listed) != 2 {
		t.Fatalf("listed %+v", listed)
	}
	for _, u := range listed {
		if !u.PartialScan {
			t.Fatalf("expected partial_scan on %+v", u)
		}
	}
}

func TestQuotaIngestMissingPolicyFailsJob(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h, token, hostID := setupQuotaHost(t, st)

	polBody, _ := json.Marshal(map[string]any{
		"name": "p", "path_pattern": "/home/*/*", "limit_bytes": 100, "enabled": true,
	})
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota-policies", bytes.NewReader(polBody))
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusCreated {
		t.Fatalf("policy %d %s", rw.Code, rw.Body.String())
	}
	var pol models.QuotaPolicy
	_ = json.Unmarshal(rw.Body.Bytes(), &pol)

	rw = httptest.NewRecorder()
	scanBody, _ := json.Marshal(map[string]any{"host_id": hostID, "force": true})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quota-policies/"+pol.ID+"/scan", bytes.NewReader(scanBody))
	h.ServeHTTP(rw, req)

	claim := httptest.NewRequest(http.MethodGet, "/api/v1/agent/jobs", nil)
	claim.Header.Set("Authorization", "Bearer "+token)
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, claim)
	var job models.Job
	_ = json.Unmarshal(rw.Body.Bytes(), &job)

	if err := st.DeleteQuotaPolicy(pol.ID); err != nil {
		t.Fatal(err)
	}
	report, _ := json.Marshal(models.QuotaScanReport{
		PolicyID: pol.ID,
		Targets:  []models.QuotaTarget{{Path: "/home/a/keep", UsedBytes: 90}},
	})
	body, _ := json.Marshal(models.JobResultRequest{Status: models.JobStatusDone, Report: report})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent/jobs/"+job.ID+"/result", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rw.Code, rw.Body.String())
	}
	got, err := st.GetJob(job.ID)
	if err != nil || got.Status != models.JobStatusFailed {
		t.Fatalf("job should be failed, got %+v %v", got, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent/jobs/"+job.ID+"/requeue", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("requeue %d %s", rw.Code, rw.Body.String())
	}
	var rr map[string]string
	_ = json.Unmarshal(rw.Body.Bytes(), &rr)
	if rr["status"] != models.JobStatusFailed {
		t.Fatalf("requeue of failed job should report failed, got %v", rr)
	}
}

func TestRequeueReturnsQueued(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h, token, hostID := setupQuotaHost(t, st)
	job, err := st.EnqueueDiskScan(hostID, "/var", 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(hostID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/jobs/"+job.ID+"/requeue", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("requeue %d %s", rw.Code, rw.Body.String())
	}
	var rr map[string]string
	_ = json.Unmarshal(rw.Body.Bytes(), &rr)
	if rr["status"] != models.JobStatusQueued {
		t.Fatalf("got %v", rr)
	}
}

func setupQuotaHost(t *testing.T, st *store.Store) (http.Handler, string, string) {
	t.Helper()
	srv := New(st, alerts.New(st, alerts.DefaultConfig()), time.Minute)
	h := srv.Handler()
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", bytes.NewBufferString(`{"hostname":"h1","name":"n1"}`))
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusCreated {
		t.Fatalf("register %d %s", rw.Code, rw.Body.String())
	}
	var rr models.RegisterResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &rr)
	return h, rr.Token, rr.HostID
}
