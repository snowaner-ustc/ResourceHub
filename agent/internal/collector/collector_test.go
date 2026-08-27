package collector

import (
	"testing"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
)

func TestCollectDisksUsesStatfs(t *testing.T) {
	disks, err := collectDisks()
	if err != nil {
		t.Fatalf("collectDisks: %v", err)
	}
	for _, d := range disks {
		if d.CollectMethod != "statfs" {
			t.Fatalf("expected statfs, got %q for %s", d.CollectMethod, d.Mountpoint)
		}
		if d.Mountpoint == "" {
			t.Fatal("empty mountpoint")
		}
	}
}

func TestCollectMemory(t *testing.T) {
	mem, err := collectMemory()
	if err != nil {
		t.Fatalf("collectMemory: %v", err)
	}
	if mem.TotalBytes == 0 {
		t.Fatal("expected non-zero total memory")
	}
}

func TestCollectorSnapshot(t *testing.T) {
	c := New()
	if _, err := c.Collect(); err != nil {
		t.Fatalf("first collect: %v", err)
	}
	snap, err := c.Collect()
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if snap.CollectDurationMs < 0 {
		t.Fatal("invalid duration")
	}
	_ = models.Snapshot(snap)
}
