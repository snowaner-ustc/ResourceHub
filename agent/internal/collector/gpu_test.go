package collector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseNvidiaSMICSV(t *testing.T) {
	raw := []byte("0, NVIDIA A100-SXM4-40GB, GPU-abc, 12.5, 1024, 40960, 35, 80.25\n1, NVIDIA A100-SXM4-40GB, GPU-def, 90, 20480, 40960, 62, 250\n")
	devs := parseNvidiaSMICSV(raw)
	if len(devs) != 2 {
		t.Fatalf("got %d devices", len(devs))
	}
	if devs[0].Index != 0 || devs[0].UtilPercent != 12.5 {
		t.Fatalf("dev0: %+v", devs[0])
	}
	if devs[1].MemoryUsedPercent < 49 || devs[1].MemoryUsedPercent > 51 {
		t.Fatalf("expected ~50%% mem, got %v", devs[1].MemoryUsedPercent)
	}
}

func TestCollectGPUsUnavailable(t *testing.T) {
	stats := collectGPUs(func(ctx context.Context) ([]byte, error) {
		return nil, errors.New("not found")
	}, time.Second)
	if stats.Available || stats.Status != "unavailable" {
		t.Fatalf("expected unavailable, got %+v", stats)
	}
}

func TestCollectGPUsOK(t *testing.T) {
	stats := collectGPUs(func(ctx context.Context) ([]byte, error) {
		return []byte("0, Tesla T4, GPU-x, 5, 100, 16000, 40, 30"), nil
	}, time.Second)
	if !stats.Available || len(stats.Devices) != 1 {
		t.Fatalf("expected 1 gpu, got %+v", stats)
	}
	if stats.Method != "nvidia-smi" {
		t.Fatalf("method %q", stats.Method)
	}
}
