//go:build linux

package collector

import "os"

func openProcMounts() (*os.File, error) {
	return os.Open("/proc/mounts")
}
