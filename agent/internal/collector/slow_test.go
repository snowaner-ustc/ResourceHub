package collector

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
)

func TestValidateSlowPath(t *testing.T) {
	if err := ValidateSlowPath("/"); err == nil {
		t.Fatal("expected reject /")
	}
	if err := ValidateSlowPath("/*"); err == nil {
		t.Fatal("expected reject /*")
	}
	if err := ValidateSlowPath("relative"); err == nil {
		t.Fatal("expected reject relative")
	}
	if err := ValidateSlowPath("/proc/foo"); err == nil {
		t.Fatal("expected reject /proc")
	}
	if err := ValidateSlowPath("/home/*/*"); err != nil {
		t.Fatalf("home glob should be allowed: %v", err)
	}
	if err := ValidateSlowPath("/var"); err != nil {
		t.Fatalf("/var should be allowed: %v", err)
	}
}

func TestScanLayerTopNAndSkip(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "alice", "big.bin"), 1000)
	mustWrite(t, filepath.Join(root, "bob", "mid.bin"), 400)
	mustWrite(t, filepath.Join(root, "carol", "tiny.bin"), 50)
	mustWrite(t, filepath.Join(root, ".git", "objects", "pack"), 8000)

	rep, err := ScanLayer(models.DiskScanParams{
		Root:      root,
		TopN:      2,
		TimeoutMs: 5000,
		MaxFiles:  1000,
		LargeN:    5,
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.Partial {
		t.Fatalf("unexpected partial: %+v", rep)
	}
	if len(rep.Entries) != 2 {
		t.Fatalf("expected top 2, got %+v", rep.Entries)
	}
	if filepath.Base(rep.Entries[0].Path) != "alice" {
		t.Fatalf("expected alice first, got %+v", rep.Entries)
	}
	if rep.OthersBytes != 50 {
		t.Fatalf("others want 50 got %d (entries=%+v)", rep.OthersBytes, rep.Entries)
	}
	for _, e := range rep.Entries {
		if filepath.Base(e.Path) == ".git" {
			t.Fatal(".git should be skipped")
		}
	}
	if len(rep.LargeFiles) == 0 || rep.LargeFiles[0].Bytes != 1000 {
		t.Fatalf("expected largest file 1000, got %+v", rep.LargeFiles)
	}
	if rep.RootBytes != 1450 {
		t.Fatalf("root bytes want 1450 (alice+bob+carol, no .git) got %d", rep.RootBytes)
	}
}

func TestScanLayerMaxFilesPartial(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		mustWrite(t, filepath.Join(root, "d", "f"+string(rune('a'+i))), 10)
	}
	rep, err := ScanLayer(models.DiskScanParams{
		Root:      root,
		TopN:      10,
		TimeoutMs: 5000,
		MaxFiles:  3,
		LargeN:    3,
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !rep.Partial || rep.PartialReason != "max_files" {
		t.Fatalf("expected max_files partial, got %+v", rep)
	}
}

func TestScanQuotaGlob(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "u1", "projA", "a.bin"), 200)
	mustWrite(t, filepath.Join(root, "u1", "projB", "b.bin"), 50)
	mustWrite(t, filepath.Join(root, "u2", "projA", "c.bin"), 80)

	rep, err := ScanQuota(models.QuotaScanParams{
		PolicyID:    "p1",
		PathPattern: filepath.Join(root, "*", "*"),
		TimeoutMs:   5000,
		MaxFiles:    1000,
		MaxTargets:  50,
	})
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if len(rep.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %+v", rep.Targets)
	}
	byPath := map[string]uint64{}
	for _, tget := range rep.Targets {
		byPath[tget.Path] = tget.UsedBytes
	}
	if byPath[filepath.Join(root, "u1", "projA")] != 200 {
		t.Fatalf("projA size: %+v", rep.Targets)
	}
}

func TestRunSlowJobRejectsRoot(t *testing.T) {
	res := RunSlowJob(models.Job{
		Kind:   models.JobKindDiskScan,
		Params: []byte(`{"root":"/"}`),
	})
	if res.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %+v", res)
	}
}

func TestScanLayerDoesNotBlockFastPath(t *testing.T) {
	// Fast path still uses statfs and should finish quickly even if a slow
	// scan ran earlier in the process.
	c := New()
	start := time.Now()
	if _, err := c.Collect(); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("fast path too slow: %s", time.Since(start))
	}
}

func mustWrite(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), n), 0o644); err != nil {
		t.Fatal(err)
	}
}
