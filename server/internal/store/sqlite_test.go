package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/snowaner-ustc/ResourceHub/server/internal/models"
)

func TestRegisterAndMetrics(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	resp, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1", AgentVersion: "0.1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	host, err := st.HostByToken(resp.Token)
	if err != nil {
		t.Fatalf("host by token: %v", err)
	}
	snap := models.MetricSnapshot{
		CollectedAt: time.Now().UTC(),
		CPU:         models.CPUStats{UsagePercent: 12.5, Cores: 4},
		Memory:      models.MemoryStats{UsedPercent: 40},
		Disks: []models.DiskStats{
			{Mountpoint: "/", UsedPercent: 55, CollectMethod: "statfs"},
		},
	}
	if err := st.SaveMetrics(host.ID, snap); err != nil {
		t.Fatalf("save metrics: %v", err)
	}
	list, err := st.ListHosts("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].CPUUsagePercent != 12.5 {
		t.Fatalf("unexpected list: %+v", list)
	}
	pts, err := st.QuerySamples(host.ID, []string{"cpu.usage_percent"}, time.Now().Add(-time.Hour), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("query samples: %v", err)
	}
	if len(pts) != 1 || pts[0].Value != 12.5 {
		t.Fatalf("unexpected samples: %+v", pts)
	}
}

func TestHostGroup(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if _, err := st.RegisterHost(models.RegisterRequest{Name: "n1", Hostname: "h1", Group: "gpu"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	list, err := st.ListHosts("gpu")
	if err != nil || len(list) != 1 || list[0].Group != "gpu" {
		t.Fatalf("group filter: %+v %v", list, err)
	}
	empty, err := st.ListHosts("other")
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty, got %+v %v", empty, err)
	}
}
