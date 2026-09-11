package sysinfo

import (
	"time"
)

type Snapshot struct {
	CPU    CPU
	Memory Memory
	Disk   []DiskUsage
	Uptime time.Duration
}

type CPU struct {
	PercentUsed float64
}

type Memory struct {
	TotalBytes  uint64
	UsedBytes   uint64
	PercentUsed float64
}

type DiskUsage struct {
	MountPoint  string
	FSType      string
	TotalBytes  uint64
	UsedBytes   uint64
	PercentUsed float64
}

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Snapshot() Snapshot {
	return Snapshot{}
}

func (c *Collector) CPUPercent() (float64, error) {
	return 0, nil
}

func (c *Collector) MemoryUsage() (total, used uint64, err error) {
	return 0, 0, nil
}

func (c *Collector) DiskUsage() ([]DiskUsage, error) {
	return nil, nil
}

func (c *Collector) SystemUptime() (time.Duration, error) {
	return 0, nil
}
