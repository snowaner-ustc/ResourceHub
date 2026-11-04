package models

import (
	"encoding/json"
	"time"
)

const (
	JobKindDiskScan  = "disk_scan"
	JobKindQuotaScan = "quota_scan"
	JobStatusDone    = "done"
	JobStatusFailed  = "failed"
)

type Job struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Params json.RawMessage `json:"params"`
}

type DiskScanParams struct {
	Root      string   `json:"root"`
	TopN      int      `json:"top_n"`
	TimeoutMs int      `json:"timeout_ms"`
	MaxFiles  int      `json:"max_files"`
	LargeN    int      `json:"large_n"`
	Skip      []string `json:"skip,omitempty"`
}

type QuotaScanParams struct {
	PolicyID    string   `json:"policy_id"`
	PathPattern string   `json:"path_pattern"`
	Exclude     []string `json:"exclude,omitempty"`
	TimeoutMs   int      `json:"timeout_ms"`
	MaxFiles    int      `json:"max_files"`
	MaxTargets  int      `json:"max_targets"`
}

type DiskEntry struct {
	Path      string `json:"path"`
	Bytes     uint64 `json:"bytes"`
	Owner     string `json:"owner,omitempty"`
	EntryType string `json:"entry_type"`
}

type OwnerBytes struct {
	Owner string `json:"owner"`
	Bytes uint64 `json:"bytes"`
}

type DiskScanReport struct {
	Root          string       `json:"root"`
	AsOf          time.Time    `json:"as_of"`
	DurationMs    int64        `json:"duration_ms"`
	Partial       bool         `json:"partial"`
	PartialReason string       `json:"partial_reason,omitempty"`
	FilesVisited  int          `json:"files_visited"`
	ScanErrors    int          `json:"scan_errors"`
	RootBytes     uint64       `json:"root_bytes"`
	Entries       []DiskEntry  `json:"entries"`
	OthersBytes   uint64       `json:"others_bytes"`
	ByOwner       []OwnerBytes `json:"by_owner"`
	LargeFiles    []DiskEntry  `json:"large_files"`
}

type QuotaTarget struct {
	Path      string `json:"path"`
	UsedBytes uint64 `json:"used_bytes"`
	Owner     string `json:"owner,omitempty"`
	Error     string `json:"error,omitempty"`
}

type QuotaScanReport struct {
	PolicyID      string        `json:"policy_id"`
	PathPattern   string        `json:"path_pattern"`
	AsOf          time.Time     `json:"as_of"`
	DurationMs    int64         `json:"duration_ms"`
	Partial       bool          `json:"partial"`
	PartialReason string        `json:"partial_reason,omitempty"`
	Targets       []QuotaTarget `json:"targets"`
}

type JobResult struct {
	Status string          `json:"status"`
	Error  string          `json:"error,omitempty"`
	Report json.RawMessage `json:"report,omitempty"`
}
