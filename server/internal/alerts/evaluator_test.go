package alerts

import (
	"path/filepath"
	"testing"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
	"github.com/snowaner-ustc/ResourceHub/server/internal/notify"
	"github.com/snowaner-ustc/ResourceHub/server/internal/store"
)

type recNotify struct{ evs []notify.Event }

func (r *recNotify) Notify(e notify.Event) { r.evs = append(r.evs, e) }
func (r *recNotify) Enabled() bool         { return true }

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

func TestNotifyOnNewUpgradeAndResolveOnly(t *testing.T) {
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
	rec := &recNotify{}
	ev := New(st, DefaultConfig())
	ev.SetNotifier(rec)
	rule := models.QuotaRuleType(policy.ID, "/home/a/data")
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{Path: "/home/a/data", UsedPercent: 90}}, true); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != 1 || rec.evs[0].Kind != notify.KindFiring {
		t.Fatalf("first fire: %+v", rec.evs)
	}
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{Path: "/home/a/data", UsedPercent: 90}}, true); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != 1 {
		t.Fatalf("duplicate fire should not notify: %+v", rec.evs)
	}
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{Path: "/home/a/data", UsedPercent: 120}}, true); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != 2 || rec.evs[1].Severity != models.AlertSeverityCritical {
		t.Fatalf("upgrade: %+v", rec.evs)
	}
	if err := ev.EvaluateQuota(host, policy, []models.QuotaUsage{{Path: "/home/a/data", UsedPercent: 10}}, true); err != nil {
		t.Fatal(err)
	}
	if len(rec.evs) != 3 || rec.evs[2].Kind != notify.KindResolved || rec.evs[2].RuleType != rule {
		t.Fatalf("resolve: %+v", rec.evs)
	}
}
