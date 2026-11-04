package models

import "time"

type HostStatus string

const (
	HostStatusOnline  HostStatus = "online"
	HostStatusOffline HostStatus = "offline"
)

type Host struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Hostname     string     `json:"hostname"`
	AgentVersion string     `json:"agent_version"`
	Token        string     `json:"-"`
	Status       HostStatus `json:"status"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

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

type ProcessSummary struct {
	Total             int   `json:"total"`
	Running           int   `json:"running"`
	Sleeping          int   `json:"sleeping"`
	Uninterruptible   int   `json:"uninterruptible"`
	Zombie            int   `json:"zombie"`
	Stopped           int   `json:"stopped"`
	Unknown           int   `json:"unknown"`
	CollectDurationMs int64 `json:"collect_duration_ms"`
	Partial           bool  `json:"partial"`
	ScannedPIDs       int   `json:"scanned_pids"`
}

type ProcessInfo struct {
	PID        int     `json:"pid"`
	PPID       int     `json:"ppid,omitempty"`
	PPIDComm   string  `json:"ppid_comm,omitempty"`
	Comm       string  `json:"comm"`
	User       string  `json:"user,omitempty"`
	State      string  `json:"state,omitempty"`
	CPUPercent float64 `json:"cpu_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
}

type ProcessStats struct {
	CollectedAt time.Time      `json:"collected_at"`
	Summary     ProcessSummary `json:"summary"`
	TopCPU      []ProcessInfo  `json:"top_cpu"`
	TopRSS      []ProcessInfo  `json:"top_rss"`
	Zombies     []ProcessInfo  `json:"zombies"`
}

type MetricSnapshot struct {
	HostID                string        `json:"host_id"`
	CollectedAt           time.Time     `json:"collected_at"`
	CPU                   CPUStats      `json:"cpu"`
	Memory                MemoryStats   `json:"memory"`
	Disks                 []DiskStats   `json:"disks"`
	Processes             *ProcessStats `json:"processes,omitempty"`
	CollectDurationMs     int64         `json:"collect_duration_ms"`
	DiskCollectDurationMs int64         `json:"disk_collect_duration_ms"`
}

type HostSummary struct {
	Host
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsedPercent  float64 `json:"memory_used_percent"`
	MaxDiskUsedPercent float64 `json:"max_disk_used_percent"`
	MaxDiskMountpoint  string  `json:"max_disk_mountpoint,omitempty"`
	ProcessTotal       int     `json:"process_total"`
	ZombieCount        int     `json:"zombie_count"`
}

type AlertSeverity string

const (
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

type AlertStatus string

const (
	AlertStatusFiring  AlertStatus = "firing"
	AlertStatusResolved AlertStatus = "resolved"
)

type Alert struct {
	ID         string        `json:"id"`
	HostID     string        `json:"host_id"`
	HostName   string        `json:"host_name"`
	RuleType   string        `json:"rule_type"`
	Severity   AlertSeverity `json:"severity"`
	Status     AlertStatus   `json:"status"`
	Message    string        `json:"message"`
	Payload    string        `json:"payload,omitempty"`
	FiredAt    time.Time     `json:"fired_at"`
	ResolvedAt *time.Time    `json:"resolved_at,omitempty"`
}

type RegisterRequest struct {
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
}

type RegisterResponse struct {
	HostID string `json:"host_id"`
	Token  string `json:"token"`
}

type MetricsRequest struct {
	CollectedAt           time.Time     `json:"collected_at"`
	CPU                   CPUStats      `json:"cpu"`
	Memory                MemoryStats   `json:"memory"`
	Disks                 []DiskStats   `json:"disks"`
	Processes             *ProcessStats `json:"processes,omitempty"`
	CollectDurationMs     int64         `json:"collect_duration_ms"`
	DiskCollectDurationMs int64         `json:"disk_collect_duration_ms"`
}
