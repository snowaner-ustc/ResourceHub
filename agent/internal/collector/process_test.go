package collector

import (
	"testing"
	"time"
)

func TestProcessCollectSummary(t *testing.T) {
	pc := NewProcessCollector(5, time.Second, 20)
	first := pc.Collect(1)
	if first.Summary.Total == 0 {
		t.Fatal("expected some processes")
	}
	time.Sleep(200 * time.Millisecond)
	second := pc.Collect(1)
	if second.Summary.Total == 0 {
		t.Fatal("expected processes on second scan")
	}
	if second.Summary.Zombie < 0 {
		t.Fatal("invalid zombie count")
	}
	if second.TopRSS == nil {
		t.Fatal("top_rss should be non-nil")
	}
	for _, z := range second.Zombies {
		if z.State != "Z" {
			t.Fatalf("zombie state want Z got %q", z.State)
		}
	}
}

func TestCollectAttachesProcesses(t *testing.T) {
	c := New()
	c.CollectProcesses()
	snap, err := c.Collect()
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snap.Processes == nil {
		t.Fatal("expected processes attached")
	}
	if snap.Processes.Summary.Total == 0 {
		t.Fatal("expected process total > 0")
	}
}
