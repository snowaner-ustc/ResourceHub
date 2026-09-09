package collector

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
)

type procCPUSample struct {
	utime uint64
	stime uint64
}

type ProcessCollector struct {
	prevCPU   map[int]procCPUSample
	prevAt    time.Time
	topN      int
	timeout   time.Duration
	zombieMax int
	pageSize  uint64
	uidCache  map[string]string
}

func NewProcessCollector(topN int, timeout time.Duration, zombieMax int) *ProcessCollector {
	if topN <= 0 {
		topN = 10
	}
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	if zombieMax <= 0 {
		zombieMax = 50
	}
	return &ProcessCollector{
		prevCPU:   map[int]procCPUSample{},
		topN:      topN,
		timeout:   timeout,
		zombieMax: zombieMax,
		pageSize:  uint64(os.Getpagesize()),
		uidCache:  map[string]string{},
	}
}

func (pc *ProcessCollector) Collect(numCPU int) models.ProcessStats {
	start := time.Now()
	deadline := start.Add(pc.timeout)
	if numCPU <= 0 {
		numCPU = 1
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return models.ProcessStats{
			CollectedAt: start.UTC(),
			Summary: models.ProcessSummary{
				CollectDurationMs: time.Since(start).Milliseconds(),
				Partial:           true,
			},
			TopCPU:  []models.ProcessInfo{},
			TopRSS:  []models.ProcessInfo{},
			Zombies: []models.ProcessInfo{},
		}
	}

	type candidate struct {
		info     models.ProcessInfo
		cpuTicks uint64
	}
	var (
		sum       models.ProcessSummary
		zombies   []models.ProcessInfo
		cands     []candidate
		nextCPU   = map[int]procCPUSample{}
		commCache = map[int]string{}
		wallSec   = start.Sub(pc.prevAt).Seconds()
		haveDelta = pc.prevAt.After(time.Time{}) && wallSec > 0
	)

	for _, e := range entries {
		if time.Now().After(deadline) {
			sum.Partial = true
			break
		}
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		sum.ScannedPIDs++
		stat, ok := readProcStat(pid)
		if !ok {
			continue
		}
		sum.Total++
		commCache[pid] = stat.comm
		nextCPU[pid] = procCPUSample{utime: stat.utime, stime: stat.stime}
		rss := stat.rssPages * pc.pageSize
		userName := pc.lookupUID(stat.uid)

		switch stat.state {
		case 'R':
			sum.Running++
		case 'S', 'I':
			sum.Sleeping++
		case 'D':
			sum.Uninterruptible++
		case 'Z':
			sum.Zombie++
			if len(zombies) < pc.zombieMax {
				zombies = append(zombies, models.ProcessInfo{
					PID:      pid,
					PPID:     stat.ppid,
					Comm:     stat.comm,
					User:     userName,
					State:    "Z",
					RSSBytes: rss,
				})
			}
		case 'T', 't':
			sum.Stopped++
		default:
			sum.Unknown++
		}

		info := models.ProcessInfo{
			PID:      pid,
			PPID:     stat.ppid,
			Comm:     stat.comm,
			User:     userName,
			State:    string(stat.state),
			RSSBytes: rss,
		}
		var cpuTicks uint64
		if haveDelta {
			if prev, ok := pc.prevCPU[pid]; ok {
				cur := stat.utime + stat.stime
				old := prev.utime + prev.stime
				if cur >= old {
					cpuTicks = cur - old
					// Linux USER_HZ is typically 100 for /proc accounting fields.
					info.CPUPercent = (float64(cpuTicks) / 100.0 / wallSec / float64(numCPU)) * 100
					if info.CPUPercent < 0 {
						info.CPUPercent = 0
					}
				}
			}
		}
		cands = append(cands, candidate{info: info, cpuTicks: cpuTicks})
	}

	for i := range zombies {
		if c, ok := commCache[zombies[i].PPID]; ok {
			zombies[i].PPIDComm = c
		} else {
			zombies[i].PPIDComm = readComm(zombies[i].PPID)
		}
	}

	topCPU := make([]models.ProcessInfo, 0, pc.topN)
	topRSS := make([]models.ProcessInfo, 0, pc.topN)
	if haveDelta {
		sort.Slice(cands, func(i, j int) bool { return cands[i].cpuTicks > cands[j].cpuTicks })
		for i := 0; i < len(cands) && i < pc.topN; i++ {
			if cands[i].cpuTicks == 0 {
				break
			}
			topCPU = append(topCPU, cands[i].info)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].info.RSSBytes > cands[j].info.RSSBytes })
	for i := 0; i < len(cands) && i < pc.topN; i++ {
		topRSS = append(topRSS, cands[i].info)
	}

	pc.prevCPU = nextCPU
	pc.prevAt = start
	sum.CollectDurationMs = time.Since(start).Milliseconds()

	if topCPU == nil {
		topCPU = []models.ProcessInfo{}
	}
	if topRSS == nil {
		topRSS = []models.ProcessInfo{}
	}
	if zombies == nil {
		zombies = []models.ProcessInfo{}
	}

	return models.ProcessStats{
		CollectedAt: start.UTC(),
		Summary:     sum,
		TopCPU:      topCPU,
		TopRSS:      topRSS,
		Zombies:     zombies,
	}
}

type procStat struct {
	comm     string
	state    byte
	ppid     int
	utime    uint64
	stime    uint64
	rssPages uint64
	uid      string
}

func readProcStat(pid int) (procStat, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, false
	}
	line := string(data)
	lparen := strings.IndexByte(line, '(')
	rparen := strings.LastIndexByte(line, ')')
	if lparen < 0 || rparen < 0 || rparen+2 >= len(line) {
		return procStat{}, false
	}
	comm := line[lparen+1 : rparen]
	fields := strings.Fields(line[rparen+2:])
	// fields: state ppid ... utime stime ... rss(index 21 from state=0 → rss at 21)
	if len(fields) < 22 {
		return procStat{}, false
	}
	ppid, _ := strconv.Atoi(fields[1])
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	rss, _ := strconv.ParseUint(fields[21], 10, 64)
	uid := readUID(pid)
	state := byte('?')
	if fields[0] != "" {
		state = fields[0][0]
	}
	return procStat{
		comm:     comm,
		state:    state,
		ppid:     ppid,
		utime:    utime,
		stime:    stime,
		rssPages: rss,
		uid:      uid,
	}, true
}

func readComm(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

func readUID(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

func (pc *ProcessCollector) lookupUID(uid string) string {
	if uid == "" {
		return ""
	}
	if name, ok := pc.uidCache[uid]; ok {
		return name
	}
	u, err := user.LookupId(uid)
	if err != nil {
		pc.uidCache[uid] = uid
		return uid
	}
	pc.uidCache[uid] = u.Username
	return u.Username
}
