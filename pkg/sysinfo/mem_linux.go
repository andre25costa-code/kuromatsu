//go:build linux || android

package sysinfo

// platformMemory reads MemTotal/MemAvailable from /proc/meminfo.
func platformMemory() (total, avail uint64, err error) {
	return memInfoAt("/proc/meminfo")
}

// platformMemLimit is the cgroup (v2, then v1) memory limit, 0 if none.
func platformMemLimit() uint64 {
	if _, limit, ok := CgroupMemory(); ok {
		return limit
	}
	return 0
}
