package models

import "time"

type CPUStats struct {
	UsagePercent float64 `json:"usage_percent"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
	Cores        int     `json:"cores"`
}

type MemoryStats struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	SwapTotalBytes uint64  `json:"swap_total_bytes"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes"`
}

type DiskStats struct {
	Mountpoint        string  `json:"mountpoint"`
	Device            string  `json:"device"`
	FSType            string  `json:"fstype"`
	TotalBytes        uint64  `json:"total_bytes"`
	UsedBytes         uint64  `json:"used_bytes"`
	AvailBytes        uint64  `json:"avail_bytes"`
	UsedPercent       float64 `json:"used_percent"`
	CollectMethod     string  `json:"collect_method"`
	CollectDurationMs int64   `json:"collect_duration_ms"`
}

type Snapshot struct {
	CollectedAt           time.Time   `json:"collected_at"`
	CPU                   CPUStats    `json:"cpu"`
	Memory                MemoryStats `json:"memory"`
	Disks                 []DiskStats `json:"disks"`
	CollectDurationMs     int64       `json:"collect_duration_ms"`
	DiskCollectDurationMs int64       `json:"disk_collect_duration_ms"`
}

type RegisterResponse struct {
	HostID string `json:"host_id"`
	Token  string `json:"token"`
}
