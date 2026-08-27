package collector

import (
	"bufio"
	"strings"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
	"golang.org/x/sys/unix"
)

var defaultDenyFSTypes = map[string]struct{}{
	"tmpfs": {}, "devtmpfs": {}, "devfs": {}, "sysfs": {}, "proc": {},
	"cgroup": {}, "cgroup2": {}, "overlay": {}, "squashfs": {}, "ramfs": {},
	"rpc_pipefs": {}, "nfsd": {}, "autofs": {}, "fuse.portal": {}, "iso9660": {},
}

type mountEntry struct {
	Device     string
	Mountpoint string
	FSType     string
}

func collectDisks() ([]models.DiskStats, error) {
	mounts, err := readMounts()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]models.DiskStats, 0, len(mounts))
	for _, m := range mounts {
		if _, deny := defaultDenyFSTypes[m.FSType]; deny {
			continue
		}
		key := m.Device + "|" + m.Mountpoint
		if _, ok := seen[key]; ok {
			continue
		}
		start := time.Now()
		var st unix.Statfs_t
		if err := unix.Statfs(m.Mountpoint, &st); err != nil {
			continue
		}
		seen[m.Device] = struct{}{}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		free := st.Bfree * bsize
		avail := st.Bavail * bsize
		used := total - free
		var pct float64
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}
		out = append(out, models.DiskStats{
			Mountpoint:        m.Mountpoint,
			Device:            m.Device,
			FSType:            m.FSType,
			TotalBytes:        total,
			UsedBytes:         used,
			AvailBytes:        avail,
			UsedPercent:       pct,
			CollectMethod:     "statfs",
			CollectDurationMs: time.Since(start).Milliseconds(),
		})
	}
	return out, nil
}

func readMounts() ([]mountEntry, error) {
	f, err := openProcMounts()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []mountEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		out = append(out, mountEntry{
			Device:     fields[0],
			Mountpoint: fields[1],
			FSType:     fields[2],
		})
	}
	return out, sc.Err()
}
