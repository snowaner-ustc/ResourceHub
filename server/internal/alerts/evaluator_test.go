package alerts

import (
	"path/filepath"
	"testing"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

func TestEvaluateQuotaSoftAlerts(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := st.HostByID(resp.HostID)
	if err != nil {
		t.Fatal(err)
	}
	policy := &models.QuotaPolicy{
		ID:              "pol1",
		Name:            "t",
		WarnPercent:     80,
		CriticalPercent: 100,
		LimitBytes:      100,
	}
	ev := New(st, DefaultConfig())
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{
		Path: "/home/a/data", UsedBytes: 90, LimitBytes: 100, UsedPercent: 90,
	}}, true); err != nil {
		t.Fatal(err)
	}
	a, err := st.GetFiringAlert(host.ID, models.QuotaRuleType(policy.ID, "/home/a/data"))
	if err != nil || a == nil || a.Severity != models.AlertSeverityWarning {
		t.Fatalf("warn: %+v %v", a, err)
	}
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{
		Path: "/home/a/data", UsedBytes: 120, LimitBytes: 100, UsedPercent: 120,
	}}, true); err != nil {
		t.Fatal(err)
	}
	a, err = st.GetFiringAlert(host.ID, models.QuotaRuleType(policy.ID, "/home/a/data"))
	if err != nil || a == nil || a.Severity != models.AlertSeverityCritical {
		t.Fatalf("crit: %+v %v", a, err)
	}
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{
		Path: "/home/a/data", UsedBytes: 10, LimitBytes: 100, UsedPercent: 10,
	}}, true); err != nil {
		t.Fatal(err)
	}
	a, err = st.GetFiringAlert(host.ID, models.QuotaRuleType(policy.ID, "/home/a/data"))
	if err != nil || a != nil {
		t.Fatalf("expected resolve, got %+v %v", a, err)
	}
}

func TestPartialQuotaScanDoesNotResolveMissingPaths(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := st.HostByID(resp.HostID)
	if err != nil {
		t.Fatal(err)
	}
	policy := &models.QuotaPolicy{ID: "pol1", WarnPercent: 80, CriticalPercent: 100, LimitBytes: 100}
	ev := New(st, DefaultConfig())
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{
		{Path: "/home/a/keep", UsedPercent: 90},
		{Path: "/home/a/gone", UsedPercent: 90},
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{
		{Path: "/home/a/keep", UsedPercent: 95},
	}, false); err != nil {
		t.Fatal(err)
	}
	keep, err := st.GetFiringAlert(host.ID, models.QuotaRuleType(policy.ID, "/home/a/keep"))
	if err != nil || keep == nil {
		t.Fatalf("keep: %+v %v", keep, err)
	}
	gone, err := st.GetFiringAlert(host.ID, models.QuotaRuleType(policy.ID, "/home/a/gone"))
	if err != nil || gone == nil {
		t.Fatalf("partial scan should leave gone firing, got %+v %v", gone, err)
	}
}
