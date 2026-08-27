package collector

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
)

type Collector struct {
	lastCPU cpuSample
}

type cpuSample struct {
	total uint64
	idle  uint64
	ok    bool
}

func New() *Collector {
	return &Collector{}
}

func (c *Collector) Collect() (models.Snapshot, error) {
	start := time.Now()
	cpu, err := c.collectCPU()
	if err != nil {
		return models.Snapshot{}, err
	}
	mem, err := collectMemory()
	if err != nil {
		return models.Snapshot{}, err
	}
	diskStart := time.Now()
	disks, err := collectDisks()
	diskDur := time.Since(diskStart).Milliseconds()
	if err != nil {
		return models.Snapshot{}, err
	}
	return models.Snapshot{
		CollectedAt:           time.Now().UTC(),
		CPU:                   cpu,
		Memory:                mem,
		Disks:                 disks,
		CollectDurationMs:     time.Since(start).Milliseconds(),
		DiskCollectDurationMs: diskDur,
	}, nil
}

func (c *Collector) collectCPU() (models.CPUStats, error) {
	sample, cores, err := readCPUStat()
	if err != nil {
		return models.CPUStats{}, err
	}
	load1, load5, load15 := readLoadAvg()
	out := models.CPUStats{Cores: cores, Load1: load1, Load5: load5, Load15: load15}
	if c.lastCPU.ok {
		totalDelta := float64(sample.total - c.lastCPU.total)
		idleDelta := float64(sample.idle - c.lastCPU.idle)
		if totalDelta > 0 {
			out.UsagePercent = (1 - idleDelta/totalDelta) * 100
			if out.UsagePercent < 0 {
				out.UsagePercent = 0
			}
			if out.UsagePercent > 100 {
				out.UsagePercent = 100
			}
		}
	}
	c.lastCPU = sample
	c.lastCPU.ok = true
	return out, nil
}

func readCPUStat() (cpuSample, int, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	cores := 0
	var sample cpuSample
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)[1:]
			var vals []uint64
			for _, field := range fields {
				v, err := strconv.ParseUint(field, 10, 64)
				if err != nil {
					return cpuSample{}, 0, err
				}
				vals = append(vals, v)
			}
			if len(vals) < 5 {
				return cpuSample{}, 0, fmt.Errorf("unexpected /proc/stat format")
			}
			var total uint64
			for _, v := range vals {
				total += v
			}
			idle := vals[3]
			if len(vals) > 4 {
				idle += vals[4]
			}
			sample = cpuSample{total: total, idle: idle}
			continue
		}
		if len(line) > 3 && strings.HasPrefix(line, "cpu") && line[3] >= '0' && line[3] <= '9' {
			cores++
		}
	}
	if sample.total == 0 {
		return cpuSample{}, 0, fmt.Errorf("cpu line not found in /proc/stat")
	}
	return sample, cores, sc.Err()
}

func readLoadAvg() (float64, float64, float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	l1, _ := strconv.ParseFloat(fields[0], 64)
	l5, _ := strconv.ParseFloat(fields[1], 64)
	l15, _ := strconv.ParseFloat(fields[2], 64)
	return l1, l5, l15
}

func collectMemory() (models.MemoryStats, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return models.MemoryStats{}, err
	}
	defer f.Close()
	values := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = v * 1024
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"]
	}
	used := total - available
	var usedPct float64
	if total > 0 {
		usedPct = float64(used) / float64(total) * 100
	}
	swapTotal := values["SwapTotal"]
	swapFree := values["SwapFree"]
	return models.MemoryStats{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsedPercent:    usedPct,
		SwapTotalBytes: swapTotal,
		SwapUsedBytes:  swapTotal - swapFree,
	}, sc.Err()
}
