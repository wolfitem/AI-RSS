package logger

import (
	"runtime"
	"time"
)

// MemStatsMonitor 内存统计监控器
type MemStatsMonitor struct {
	interval time.Duration // 监控间隔
	stopped  chan struct{} // 停止信号
}

// NewMemStatsMonitor 创建一个新的内存统计监控器
func NewMemStatsMonitor(interval time.Duration) *MemStatsMonitor {
	return &MemStatsMonitor{
		interval: interval,
		stopped:  make(chan struct{}),
	}
}

// Start 开始监控内存使用情况
func (m *MemStatsMonitor) Start() {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.LogMemStats()
			case <-m.stopped:
				return
			}
		}
	}()
}

// Stop 停止监控
func (m *MemStatsMonitor) Stop() {
	close(m.stopped)
}

// LogMemStats 记录内存使用统计
func (m *MemStatsMonitor) LogMemStats() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	Info("内存使用统计",
		"alloc_mb", stats.Alloc/1024/1024,
		"sys_mb", stats.Sys/1024/1024,
		"heap_alloc_mb", stats.HeapAlloc/1024/1024,
		"heap_sys_mb", stats.HeapSys/1024/1024,
		"num_gc", stats.NumGC)
}

// LogMemStatsOnce 记录一次内存使用统计
func LogMemStatsOnce() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	Info("内存使用统计（单次）",
		"alloc_mb", stats.Alloc/1024/1024,
		"sys_mb", stats.Sys/1024/1024,
		"heap_alloc_mb", stats.HeapAlloc/1024/1024,
		"heap_sys_mb", stats.HeapSys/1024/1024,
		"num_gc", stats.NumGC)
}
