package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
)

func (s *Server) handleAgentJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	host, err := s.authenticateAgent(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	job, err := s.store.ClaimJob(host.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleAgentJobSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent/jobs/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	host, err := s.authenticateAgent(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	job, err := s.store.GetJob(parts[0])
	if err != nil || job.HostID != host.ID {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if parts[1] == "requeue" && r.Method == http.MethodPost {
		if err := s.store.RequeueJob(job.ID, host.ID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "requeued"})
		return
	}
	if parts[1] != "result" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req models.JobResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Status == models.JobStatusDone && job.Kind == models.JobKindQuotaScan {
		if err := s.ingestQuotaResult(host, job, req.Report); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.store.FinishJob(job.ID, host.ID, req.Status, req.Error, req.Report); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) ingestQuotaResult(host *models.Host, job *models.Job, report json.RawMessage) error {
	var params models.QuotaScanParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return err
	}
	policy, err := s.store.GetQuotaPolicy(params.PolicyID)
	if err != nil {
		return err
	}
	var rep models.QuotaScanReport
	if len(report) > 0 {
		if err := json.Unmarshal(report, &rep); err != nil {
			return err
		}
	}
	asOf := rep.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	var usages []models.QuotaUsage
	for _, t := range rep.Targets {
		if t.Error != "" {
			continue
		}
		var pct float64
		if policy.LimitBytes > 0 {
			pct = float64(t.UsedBytes) / float64(policy.LimitBytes) * 100
		}
		usages = append(usages, models.QuotaUsage{
			HostID:      host.ID,
			PolicyID:    policy.ID,
			Path:        t.Path,
			Owner:       t.Owner,
			UsedBytes:   t.UsedBytes,
			LimitBytes:  policy.LimitBytes,
			UsedPercent: pct,
			AsOf:        asOf,
		})
	}
	complete := !rep.Partial
	if complete {
		if err := s.store.ReplaceQuotaUsages(host.ID, policy.ID, usages); err != nil {
			return err
		}
	} else if err := s.store.UpsertQuotaUsages(host.ID, policy.ID, usages); err != nil {
		return err
	}
	return s.evaluator.EvaluateQuota(host, policy, usages, complete)
}

func (s *Server) handleDiskScans(w http.ResponseWriter, r *http.Request, hostID string) {
	switch r.Method {
	case http.MethodGet:
		jobs, err := s.store.ListJobs(hostID, models.JobKindDiskScan, 20)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	case http.MethodPost:
		var req models.DiskScanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		root, err := models.NormalizeScanRoot(req.Root)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job, err := s.store.EnqueueDiskScan(hostID, root, req.TopN, req.Force)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		status := http.StatusAccepted
		if job.Status == models.JobStatusDone {
			status = http.StatusOK
		}
		writeJSON(w, status, job)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleDiskScanOne(w http.ResponseWriter, r *http.Request, hostID, jobID string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	job, err := s.store.GetJob(jobID)
	if err != nil || job.HostID != hostID {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleQuotaPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListQuotaPolicies()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		req := models.QuotaPolicy{
			Enabled:             true,
			WarnPercent:         80,
			CriticalPercent:     100,
			ScanIntervalSeconds: 86400,
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		p, err := s.store.CreateQuotaPolicy(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleQuotaPolicySub(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/quota-policies/"), "/")
	parts := strings.Split(rest, "/")
	if rest == "" || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "scan" && r.Method == http.MethodPost {
		s.handleQuotaScan(w, r, id)
		return
	}
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := s.store.GetQuotaPolicy(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPatch:
		var req models.QuotaPolicy
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.ID = id
		p, err := s.store.UpdateQuotaPolicy(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if err := s.store.DeleteQuotaPolicy(id); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleQuotaScan(w http.ResponseWriter, r *http.Request, policyID string) {
	policy, err := s.store.GetQuotaPolicy(policyID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var req models.QuotaScanRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	var jobs []models.Job
	if req.HostID != "" {
		job, err := s.store.EnqueueQuotaScan(req.HostID, policy, true)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		jobs = append(jobs, *job)
	} else {
		hosts, err := s.store.ListHosts("")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, h := range hosts {
			if policy.HostGroup != "" && h.Group != policy.HostGroup {
				continue
			}
			job, err := s.store.EnqueueQuotaScan(h.ID, policy, true)
			if err != nil {
				continue
			}
			jobs = append(jobs, *job)
		}
	}
	if jobs == nil {
		jobs = []models.Job{}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobs": jobs})
}

func (s *Server) handleQuotaUsages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	list, err := s.store.ListQuotaUsages(r.URL.Query().Get("host_id"), r.URL.Query().Get("policy_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) RunSlowScheduler(stop <-chan struct{}) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	s.tickSlow()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.tickSlow()
		}
	}
}

func (s *Server) tickSlow() {
	_, _ = s.store.FailStaleJobs(10 * time.Minute)
	s.scheduleQuotaScans()
}

func (s *Server) scheduleQuotaScans() {
	policies, err := s.store.ListQuotaPolicies()
	if err != nil {
		return
	}
	hosts, err := s.store.ListHosts("")
	if err != nil {
		return
	}
	for i := range policies {
		p := &policies[i]
		if !p.Enabled {
			continue
		}
		for _, h := range hosts {
			if p.HostGroup != "" && h.Group != p.HostGroup {
				continue
			}
			if h.Status != models.HostStatusOnline {
				continue
			}
			_, _ = s.store.EnqueueQuotaScan(h.ID, p, false)
		}
	}
}
