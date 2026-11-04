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
