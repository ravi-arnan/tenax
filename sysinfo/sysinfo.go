package sysinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Snapshot struct {
	CPU    CPU
	Memory Memory
	Disk   []DiskUsage
	Uptime time.Duration
	Time   time.Time
}

type CPU struct {
	User   float64
	System float64
	Idle   float64
	Total  float64
}

type Memory struct {
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
	PercentUsed    float64
}

type DiskUsage struct {
	MountPoint  string
	FSType      string
	TotalBytes  uint64
	UsedBytes   uint64
	FreeBytes   uint64
	PercentUsed float64
}

type Collector struct {
	prevUser, prevSystem, prevIdle, prevTotal float64
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Snapshot() Snapshot {
	cpu, _ := c.CPUPercent()
	mem, _ := c.MemoryUsage()
	disk, _ := c.DiskUsage()
	uptime, _ := c.SystemUptime()

	return Snapshot{
		CPU:    cpu,
		Memory: mem,
		Disk:   disk,
		Uptime: uptime,
		Time:   time.Now(),
	}
}

func (c *Collector) CPUPercent() (CPU, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPU{}, fmt.Errorf("read /proc/stat: %w", err)
	}

	var user, nice, system, idle uint64
	_, err = fmt.Sscanf(string(data), "cpu %d %d %d %d", &user, &nice, &system, &idle)
	if err != nil {
		return CPU{}, fmt.Errorf("parse /proc/stat: %w", err)
	}

	total := user + nice + system + idle

	if c.prevTotal == 0 {
		c.prevUser = float64(user)
		c.prevSystem = float64(system)
		c.prevIdle = float64(idle)
		c.prevTotal = float64(total)
		return CPU{Total: float64(total)}, nil
	}

	diffUser := float64(user) - c.prevUser
	diffSystem := float64(system) - c.prevSystem
	diffIdle := float64(idle) - c.prevIdle
	diffTotal := float64(total) - c.prevTotal

	var userPct, systemPct, idlePct float64
	if diffTotal > 0 {
		userPct = diffUser / diffTotal * 100
		systemPct = diffSystem / diffTotal * 100
		idlePct = diffIdle / diffTotal * 100
	}

	c.prevUser = float64(user)
	c.prevSystem = float64(system)
	c.prevIdle = float64(idle)
	c.prevTotal = float64(total)

	return CPU{
		User:   userPct,
		System: systemPct,
		Idle:   idlePct,
		Total:  float64(total),
	}, nil
}

func (c *Collector) MemoryUsage() (Memory, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}, fmt.Errorf("read /proc/meminfo: %w", err)
	}

	var total, available uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				total, _ = strconv.ParseUint(fields[1], 10, 64)
			}
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				available, _ = strconv.ParseUint(fields[1], 10, 64)
			}
		}
		if total > 0 && available > 0 {
			break
		}
	}

	totalBytes := total * 1024
	availableBytes := available * 1024
	usedBytes := totalBytes - availableBytes
	percentUsed := float64(usedBytes) / float64(totalBytes) * 100

	return Memory{
		TotalBytes:     totalBytes,
		AvailableBytes: availableBytes,
		UsedBytes:      usedBytes,
		PercentUsed:    percentUsed,
	}, nil
}

func (c *Collector) DiskUsage() ([]DiskUsage, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("read /proc/mounts: %w", err)
	}

	seen := make(map[string]bool)
	var disks []DiskUsage

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mountPoint := fields[1]
		fsType := fields[2]

		if !strings.HasPrefix(mountPoint, "/") {
			continue
		}
		if strings.HasPrefix(fsType, "proc") || strings.HasPrefix(fsType, "sys") ||
			fsType == "devtmpfs" || fsType == "devpts" || fsType == "tmpfs" ||
			fsType == "cgroup" || fsType == "cgroup2" || fsType == "pstore" ||
			fsType == "securityfs" || fsType == "efivarfs" || fsType == "bpf" ||
			fsType == "debugfs" || fsType == "tracefs" || fsType == "mqueue" ||
			fsType == "configfs" || fsType == "fusectl" || fsType == "sunrpc" ||
			fsType == "autofs" || fsType == "overlay" {
			continue
		}
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		used := total - free
		percent := float64(used) / float64(total) * 100

		disks = append(disks, DiskUsage{
			MountPoint:  mountPoint,
			FSType:      fsType,
			TotalBytes:  total,
			UsedBytes:   used,
			FreeBytes:   free,
			PercentUsed: percent,
		})
	}

	return disks, nil
}

func parseUptime(path string) (time.Duration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected format")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(secs) * time.Second, nil
}

func (c *Collector) SystemUptime() (time.Duration, error) {
	return parseUptime("/proc/uptime")
}

func LoadAvg() (float64, float64, float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected /proc/loadavg format")
	}
	var one, five, fifteen float64
	_, err = fmt.Sscanf(fields[0]+" "+fields[1]+" "+fields[2], "%f %f %f", &one, &five, &fifteen)
	return one, five, fifteen, err
}

func Processes() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				count++
			}
		}
	}
	return count, nil
}

func Hostname() string {
	h, _ := os.Hostname()
	return h
}

func KernelVersion() string {
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err != nil {
		return "unknown"
	}
	raw := make([]byte, 0, len(uname.Release))
	for _, b := range uname.Release {
		if b == 0 {
			break
		}
		raw = append(raw, byte(b))
	}
	return string(raw)
}

func NetworkBytes() (rx uint64, tx uint64, err error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		iface := strings.TrimRight(fields[0], ":")
		if iface == "lo" {
			continue
		}
		r, _ := strconv.ParseUint(fields[1], 10, 64)
		t, _ := strconv.ParseUint(fields[9], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, nil
}

func RootDisk() (DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return DiskUsage{}, err
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	return DiskUsage{
		MountPoint:  "/",
		FSType:      "ext4",
		TotalBytes:  total,
		UsedBytes:   used,
		FreeBytes:   free,
		PercentUsed: float64(used) / float64(total) * 100,
	}, nil
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func DirSize(path string) (uint64, error) {
	var size uint64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += uint64(info.Size())
		}
		return nil
	})
	return size, err
}
