package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
	"golang.org/x/sys/unix"
)

var defaultSkipDirNames = []string{".git", "lost+found", "node_modules", ".cache", "overlay2", "__pycache__"}

const (
	defaultTopN     = 20
	defaultLargeN   = 20
	defaultMaxFiles = 200_000
	defaultTimeout  = 30 * time.Second
	maxTimeout      = 2 * time.Minute
	maxTargets      = 500
)

// RunSlowJob executes a disk_scan or quota_scan off the heartbeat path.
func RunSlowJob(job models.Job) models.JobResult {
	switch job.Kind {
	case models.JobKindDiskScan:
		var p models.DiskScanParams
		if err := json.Unmarshal(job.Params, &p); err != nil {
			return failResult("invalid disk_scan params: " + err.Error())
		}
		rep, err := ScanLayer(p)
		if err != nil {
			return failResult(err.Error())
		}
		return reportResult(rep)
	case models.JobKindQuotaScan:
		var p models.QuotaScanParams
		if err := json.Unmarshal(job.Params, &p); err != nil {
			return failResult("invalid quota_scan params: " + err.Error())
		}
		rep, err := ScanQuota(p)
		if err != nil {
			return failResult(err.Error())
		}
		return reportResult(rep)
	default:
		return failResult("unknown job kind: " + job.Kind)
	}
}

func failResult(msg string) models.JobResult {
	return models.JobResult{Status: models.JobStatusFailed, Error: msg}
}

func reportResult(v any) models.JobResult {
	raw, err := json.Marshal(v)
	if err != nil {
		return failResult(err.Error())
	}
	return models.JobResult{Status: models.JobStatusDone, Report: raw}
}

func ValidateSlowPath(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return fmt.Errorf("path required")
	}
	if !filepath.IsAbs(pattern) {
		return fmt.Errorf("path must be absolute")
	}
	prefix := staticPrefix(pattern)
	if prefix == "/" {
		return fmt.Errorf("refusing unbounded scan from /; pick a subdirectory")
	}
	for _, f := range []string{"/proc", "/sys", "/dev"} {
		if prefix == f || strings.HasPrefix(prefix, f+"/") {
			return fmt.Errorf("refusing to scan %s", f)
		}
	}
	return nil
}

func staticPrefix(pattern string) string {
	i := strings.IndexAny(pattern, "*?[")
	var p string
	if i < 0 {
		p = filepath.Clean(pattern)
	} else {
		p = filepath.Clean(pattern[:i])
	}
	if p == "." || p == "" {
		return "/"
	}
	return p
}

func ScanLayer(p models.DiskScanParams) (*models.DiskScanReport, error) {
	if err := ValidateSlowPath(p.Root); err != nil {
		return nil, err
	}
	root := filepath.Clean(p.Root)
	st, err := lstat(root)
	if err != nil {
		return nil, fmt.Errorf("stat root: %w", err)
	}
	if fileType(st.Mode) != "dir" {
		return nil, fmt.Errorf("root is not a directory: %s", root)
	}

	sc := newScanner(p.TimeoutMs, p.MaxFiles, p.LargeN, p.Skip)
	sc.rootDev = st.Dev
	start := time.Now()
	entries, rootBytes := sc.scanChildren(root)
	topN := p.TopN
	if topN <= 0 {
		topN = defaultTopN
	}
	if topN > 50 {
		topN = 50
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Bytes == entries[j].Bytes {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Bytes > entries[j].Bytes
	})
	var others uint64
	if len(entries) > topN {
		for _, e := range entries[topN:] {
			others += e.Bytes
		}
		entries = entries[:topN]
	}
	if entries == nil {
		entries = []models.DiskEntry{}
	}
	rep := &models.DiskScanReport{
		Root:          root,
		AsOf:          start.UTC(),
		DurationMs:    time.Since(start).Milliseconds(),
		Partial:       sc.partial,
		PartialReason: sc.reason,
		FilesVisited:  sc.files,
		ScanErrors:    sc.errors,
		RootBytes:     rootBytes,
		Entries:       entries,
		OthersBytes:   others,
		ByOwner:       sc.ownerList(),
		LargeFiles:    sc.largeFiles(),
	}
	return rep, nil
}

func ScanQuota(p models.QuotaScanParams) (*models.QuotaScanReport, error) {
	if err := ValidateSlowPath(p.PathPattern); err != nil {
		return nil, err
	}
	limit := p.MaxTargets
	if limit <= 0 || limit > maxTargets {
		limit = maxTargets
	}
	targets, globPartial, err := expandGlobDirs(p.PathPattern, limit)
	if err != nil {
		return nil, err
	}
	sc := newScanner(p.TimeoutMs, p.MaxFiles, 0, p.Exclude)
	start := time.Now()
	out := make([]models.QuotaTarget, 0, len(targets))
	for _, dir := range targets {
		if sc.timedOut() {
			sc.partial = true
			sc.reason = "timeout"
			break
		}
		st, err := lstat(dir)
		if err != nil {
			out = append(out, models.QuotaTarget{Path: dir, Error: err.Error()})
			sc.errors++
			continue
		}
		if fileType(st.Mode) != "dir" {
			continue
		}
		sc.rootDev = st.Dev
		used := sc.walkDir(dir)
		out = append(out, models.QuotaTarget{
			Path:      dir,
			UsedBytes: used,
			Owner:     lookupUID(sc.uids, st.Uid),
		})
	}
	if globPartial {
		sc.partial = true
		if sc.reason == "" {
			sc.reason = "max_targets"
		}
	}
	if out == nil {
		out = []models.QuotaTarget{}
	}
	return &models.QuotaScanReport{
		PolicyID:      p.PolicyID,
		PathPattern:   p.PathPattern,
		AsOf:          start.UTC(),
		DurationMs:    time.Since(start).Milliseconds(),
		Partial:       sc.partial,
		PartialReason: sc.reason,
		Targets:       out,
	}, nil
}

func expandGlobDirs(pattern string, max int) ([]string, bool, error) {
	pattern = filepath.Clean(strings.TrimSpace(pattern))
	var matches []string
	if strings.ContainsAny(pattern, "*?[") {
		found, err := filepath.Glob(pattern)
		if err != nil {
			return nil, false, err
		}
		matches = found
	} else {
		matches = []string{pattern}
	}
	partial := false
	if max > 0 && len(matches) > max {
		matches = matches[:max]
		partial = true
	}
	var dirs []string
	for _, m := range matches {
		st, err := lstat(m)
		if err != nil {
			continue
		}
		if fileType(st.Mode) != "dir" {
			continue
		}
		dirs = append(dirs, m)
	}
	return dirs, partial, nil
}

type scanner struct {
	deadline time.Time
	maxFiles int
	largeN   int
	skip     map[string]struct{}
	rootDev  uint64
	files    int
	errors   int
	partial  bool
	reason   string
	owners   map[string]uint64
	large    []models.DiskEntry
	uids     map[string]string
}

func newScanner(timeoutMs, maxFiles, largeN int, skip []string) *scanner {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}
	if largeN < 0 {
		largeN = 0
	}
	if largeN > 50 {
		largeN = 50
	}
	if len(skip) == 0 {
		skip = defaultSkipDirNames
	}
	skipSet := map[string]struct{}{}
	for _, n := range skip {
		n = strings.TrimSpace(n)
		if n != "" {
			skipSet[n] = struct{}{}
		}
	}
	return &scanner{
		deadline: time.Now().Add(timeout),
		maxFiles: maxFiles,
		largeN:   largeN,
		skip:     skipSet,
		owners:   map[string]uint64{},
		uids:     map[string]string{},
	}
}

func (s *scanner) timedOut() bool {
	return time.Now().After(s.deadline)
}

func (s *scanner) scanChildren(root string) ([]models.DiskEntry, uint64) {
	ents, err := os.ReadDir(root)
	if err != nil {
		s.errors++
		return nil, 0
	}
	var out []models.DiskEntry
	var total uint64
	for _, ent := range ents {
		if s.timedOut() {
			s.partial = true
			s.reason = "timeout"
			break
		}
		if s.files >= s.maxFiles {
			s.partial = true
			s.reason = "max_files"
			break
		}
		name := ent.Name()
		full := filepath.Join(root, name)
		st, err := lstat(full)
		if err != nil {
			s.errors++
			continue
		}
		if st.Dev != s.rootDev {
			continue
		}
		kind := fileType(st.Mode)
		if kind == "other" {
			continue
		}
		owner := lookupUID(s.uids, st.Uid)
		var size uint64
		if kind == "dir" {
			if _, skip := s.skip[name]; skip {
				continue
			}
			size = s.walkDir(full)
		} else {
			size = uint64(st.Size)
			s.noteFile(full, size, owner)
		}
		total += size
		out = append(out, models.DiskEntry{
			Path:      full,
			Bytes:     size,
			Owner:     owner,
			EntryType: kind,
		})
	}
	return out, total
}

func (s *scanner) walkDir(path string) uint64 {
	if s.timedOut() {
		s.partial = true
		s.reason = "timeout"
		return 0
	}
	if s.files >= s.maxFiles {
		s.partial = true
		s.reason = "max_files"
		return 0
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		s.errors++
		return 0
	}
	var total uint64
	for _, ent := range ents {
		if s.timedOut() {
			s.partial = true
			s.reason = "timeout"
			return total
		}
		if s.files >= s.maxFiles {
			s.partial = true
			s.reason = "max_files"
			return total
		}
		name := ent.Name()
		full := filepath.Join(path, name)
		st, err := lstat(full)
		if err != nil {
			s.errors++
			continue
		}
		if st.Dev != s.rootDev {
			continue
		}
		kind := fileType(st.Mode)
		switch kind {
		case "dir":
			if _, skip := s.skip[name]; skip {
				continue
			}
			total += s.walkDir(full)
		case "file":
			size := uint64(st.Size)
			s.noteFile(full, size, lookupUID(s.uids, st.Uid))
			total += size
		}
	}
	return total
}

func (s *scanner) noteFile(path string, size uint64, owner string) {
	s.files++
	s.owners[owner] += size
	if s.largeN == 0 {
		return
	}
	e := models.DiskEntry{Path: path, Bytes: size, Owner: owner, EntryType: "file"}
	if len(s.large) < s.largeN {
		s.large = append(s.large, e)
		return
	}
	minIdx := 0
	for i := 1; i < len(s.large); i++ {
		if s.large[i].Bytes < s.large[minIdx].Bytes {
			minIdx = i
		}
	}
	if size <= s.large[minIdx].Bytes {
		return
	}
	s.large[minIdx] = e
}

func (s *scanner) largeFiles() []models.DiskEntry {
	if s.large == nil {
		return []models.DiskEntry{}
	}
	sort.Slice(s.large, func(i, j int) bool { return s.large[i].Bytes > s.large[j].Bytes })
	return s.large
}

func (s *scanner) ownerList() []models.OwnerBytes {
	out := make([]models.OwnerBytes, 0, len(s.owners))
	for owner, n := range s.owners {
		out = append(out, models.OwnerBytes{Owner: owner, Bytes: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

func lstat(path string) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return unix.Stat_t{}, err
	}
	return st, nil
}

func fileType(mode uint32) string {
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return "dir"
	case unix.S_IFREG:
		return "file"
	default:
		return "other"
	}
}

func lookupUID(cache map[string]string, uid uint32) string {
	key := strconv.FormatUint(uint64(uid), 10)
	if name, ok := cache[key]; ok {
		return name
	}
	u, err := user.LookupId(key)
	if err != nil {
		cache[key] = key
		return key
	}
	cache[key] = u.Username
	return u.Username
}
