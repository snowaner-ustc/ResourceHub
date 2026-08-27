package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/alerts"
	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

type Server struct {
	store      *store.Store
	evaluator  *alerts.Evaluator
	offlineTTL time.Duration
}

func New(st *store.Store, ev *alerts.Evaluator, offlineTTL time.Duration) *Server {
	return &Server{store: st, evaluator: ev, offlineTTL: offlineTTL}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/agent/register", s.handleRegister)
	mux.HandleFunc("/api/v1/agent/metrics", s.handleMetrics)
	mux.HandleFunc("/api/v1/hosts", s.handleHosts)
	mux.HandleFunc("/api/v1/alerts", s.handleAlerts)
	mux.Handle("/api/v1/hosts/", s.hostDetailHandler())
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		req.Name = req.Hostname
	}
	if req.Hostname == "" {
		writeError(w, http.StatusBadRequest, "hostname required")
		return
	}
	resp, err := s.store.RegisterHost(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	host, err := s.authenticateAgent(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req models.MetricsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.CollectedAt.IsZero() {
		req.CollectedAt = time.Now().UTC()
	}
	snap := models.MetricSnapshot{
		HostID:                host.ID,
		CollectedAt:           req.CollectedAt.UTC(),
		CPU:                   req.CPU,
		Memory:                req.Memory,
		Disks:                 req.Disks,
		CollectDurationMs:     req.CollectDurationMs,
		DiskCollectDurationMs: req.DiskCollectDurationMs,
	}
	if err := s.store.SaveMetrics(host.ID, snap); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.evaluator.ResolveOffline(host.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.evaluator.EvaluateSnapshot(host, &snap); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	hosts, err := s.store.ListHosts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hosts == nil {
		hosts = []models.HostSummary{}
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (s *Server) hostDetailHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/hosts/")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		host, err := s.store.HostByID(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		host.Token = ""
		snap, err := s.store.GetSnapshot(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"host":     host,
			"snapshot": snap,
		})
	})
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	status := r.URL.Query().Get("status")
	list, err := s.store.ListAlerts(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []models.Alert{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) RunOfflineChecker(stop <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.checkOffline()
		}
	}
}

func (s *Server) checkOffline() {
	hosts, err := s.store.MarkOfflineHosts(s.offlineTTL)
	if err != nil {
		return
	}
	for i := range hosts {
		_ = s.evaluator.EvaluateOffline(&hosts[i])
	}
}

func (s *Server) authenticateAgent(r *http.Request) (*models.Host, error) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		return nil, errUnauthorized("missing bearer token")
	}
	return s.store.HostByToken(token)
}

type apiError struct{ msg string }

func (e apiError) Error() string { return e.msg }

func errUnauthorized(msg string) error { return apiError{msg: msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
