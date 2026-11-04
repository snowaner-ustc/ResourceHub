package collector

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
)

const nvidiaQuery = "index,name,uuid,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw"

type gpuRunner func(ctx context.Context) ([]byte, error)

func defaultNvidiaSMI(ctx context.Context) ([]byte, error) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu="+nvidiaQuery,
		"--format=csv,noheader,nounits",
	)
	return cmd.Output()
}

func collectGPUs(run gpuRunner, timeout time.Duration) models.GPUStats {
	start := time.Now()
	if run == nil {
		run = defaultNvidiaSMI
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := run(ctx)
	stats := models.GPUStats{
		Status:            "unavailable",
		Available:         false,
		Devices:           []models.GPUDevice{},
		CollectDurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		return stats
	}
	devs := parseNvidiaSMICSV(out)
	if len(devs) == 0 {
		return stats
	}
	stats.Available = true
	stats.Status = "ok"
	stats.Method = "nvidia-smi"
	stats.Devices = devs
	return stats
}

func parseNvidiaSMICSV(raw []byte) []models.GPUDevice {
	lines := strings.Split(string(bytes.TrimSpace(raw)), "\n")
	out := make([]models.GPUDevice, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := splitCSV(line)
		if len(fields) < 6 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		usedMiB := parseNAFloat(fields[4])
		totalMiB := parseNAFloat(fields[5])
		var memPct float64
		if totalMiB > 0 {
			memPct = usedMiB / totalMiB * 100
		}
		dev := models.GPUDevice{
			Index:             idx,
			Name:              strings.TrimSpace(fields[1]),
			UtilPercent:       parseNAFloat(fields[3]),
			MemoryUsedBytes:   uint64(usedMiB * 1024 * 1024),
			MemoryTotalBytes:  uint64(totalMiB * 1024 * 1024),
			MemoryUsedPercent: memPct,
		}
		if len(fields) > 2 {
			dev.UUID = strings.TrimSpace(fields[2])
		}
		if len(fields) > 6 {
			dev.TemperatureC = parseNAFloat(fields[6])
		}
		if len(fields) > 7 {
			dev.PowerWatts = parseNAFloat(fields[7])
		}
		out = append(out, dev)
	}
	return out
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseNAFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "[N/A]") || strings.EqualFold(s, "N/A") {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
