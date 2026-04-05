package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

// MetricsCollector 收集性能指标
type MetricsCollector struct {
	mu sync.RWMutex
	
	startTime time.Time
	
	// API调用统计
	apiCalls       int64
	apiFailures    int64
	apiDurations   []time.Duration
	apiTokenUsage  TokenUsage
	
	// RSS处理统计
	rsSources      int64
	downloaded     int64
	analyzed       int64
	cachedHit      int64
	cacheMiss      int64
	
	// 缓存统计
	cacheSize      int64
	cacheHitRate   float64
}

// TokenUsage API令牌使用情况
type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// NewMetricsCollector 创建新的性能监控器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime:    time.Now(),
		apiDurations: make([]time.Duration, 0, 1000),
	}
}

// RecordAPICall 记录API调用
func (m *MetricsCollector) RecordAPICall(duration time.Duration, tokens int64, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.apiCalls++
	if !success {
		m.apiFailures++
	}
	
	m.apiDurations = append(m.apiDurations, duration)
	if len(m.apiDurations) > 1000 {
		m.apiDurations = m.apiDurations[1:]
	}
}

// RecordRSSSourceProcessed 记录RSS源处理
func (m *MetricsCollector) RecordRSSSourceProcessed(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.rsSources += count
}

// RecordArticleProcessed 记录文章处理
func (m *MetricsCollector) RecordArticleProcessed(downloaded, analyzed, cacheHits int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.downloaded += downloaded
	m.analyzed += analyzed
	m.cachedHit += cacheHits
	m.cacheMiss += downloaded - cacheHits
	
	if m.downloaded > 0 {
		m.cacheHitRate = float64(m.cachedHit) / float64(m.downloaded) * 100
	}
}

// RecordCacheStats 记录缓存统计
func (m *MetricsCollector) RecordCacheStats(size int64, hitRate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.cacheSize = size
	m.cacheHitRate = hitRate
}

// GetReport 获取性能报告
func (m *MetricsCollector) GetReport() Report {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	upTime := time.Since(m.startTime)
	avgDuration := m.getAverageAPIDuration()
	
	return Report{
		RuntimeInfo: RuntimeInfo{
			StartTime:  m.startTime,
			Uptime:     upTime,
			ProcessSec: int64(upTime.Seconds()),
		},
		APIStats: APIStats{
			TotalCalls:     m.apiCalls,
			Successful:     m.apiCalls - m.apiFailures,
			Failed:         m.apiFailures,
			SuccessRate:    m.calculateSuccessRate(),
			AverageLatency: avgDuration.Milliseconds(),
		},
		RSSStats: RSSStats{
			TotalSources:   m.rsSources,
			Downloaded:     m.downloaded,
			Analyzed:       m.analyzed,
			CachedHits:     m.cachedHit,
			CachedMisses:   m.cacheMiss,
			CacheHitRate:   m.cacheHitRate,
		},
		CacheStats: CacheStats{
			Size:         m.cacheSize,
			HitRate:      m.cacheHitRate,
		},
	}
}

// getAverageAPIDuration 获取平均API响应时间
func (m *MetricsCollector) getAverageAPIDuration() time.Duration {
	if len(m.apiDurations) == 0 {
		return 0
	}
	
	var total time.Duration
	for _, d := range m.apiDurations {
		total += d
	}
	return total / time.Duration(len(m.apiDurations))
}

// calculateSuccessRate 计算成功率
func (m *MetricsCollector) calculateSuccessRate() float64 {
	if m.apiCalls == 0 {
		return 100.0
	}
	return float64(m.apiCalls-m.apiFailures) / float64(m.apiCalls) * 100
}

// Report 运行时报告
type Report struct {
	RuntimeInfo RuntimeInfo
	APIStats    APIStats
	RSSStats    RSSStats
	CacheStats  CacheStats
}

// RuntimeInfo 运行时信息
type RuntimeInfo struct {
	StartTime   time.Time
	Uptime      time.Duration
	ProcessSec  int64
}

// APIStats API统计信息
type APIStats struct {
	TotalCalls     int64
	Successful     int64
	Failed         int64
	SuccessRate    float64
	AverageLatency int64
}

// RSSStats RSS处理统计
struct RSS {
	TotalSources   int64
	Downloaded     int64
	Analyzed       int64
	CachedHits     int64
	CachedMisses   int64
	CacheHitRate   float64
}

// 完成 RSS 状态结构定义
type RSSStats struct {
	TotalSources   int64
	Downloaded     int64
	Analyzed       int64
	CachedHits     int64
	CachedMisses   int64
	CacheHitRate   float64
}

// CacheStats 缓存统计
type CacheStats struct {
	Size    int64
	HitRate float64
}

// WithMetrics 中间件函数包装器
type WithMetrics func(context.Context, func() error) error

// NewMetricsMiddleware 创建性能监控中间件
func NewMetricsMiddleware(collector *MetricsCollector) WithMetrics {
	return func(ctx context.Context, fn func() error) error {
		start := time.Now()
		
		err := fn()
		
		duration := time.Since(start)
		collector.RecordAPICall(duration, 0, err == nil)
		
		return err
	}
}

// LogMetrics 记录指标到日志
func LogMetrics(metrics *MetricsCollector) {
	report := metrics.GetReport()
	logger.Info("📊 性能上报", 
		"start_time", report.RuntimeInfo.StartTime,
		"uptime", report.RuntimeInfo.Uptime,
		"api_calls", report.APIStats.TotalCalls,
		"api_success_rate", fmt.Sprintf("%.2f%%", report.APIStats.SuccessRate),
		"api_avg_latency", fmt.Sprintf("%dms", report.APIStats.AverageLatency),
		"rss_sources", report.RSSStats.TotalSources,
		"articles_downloaded", report.RSSStats.Downloaded,
		"articles_analyzed", report.RSSStats.Analyzed,
		"cache_hits", report.RSSStats.CachedHits,
		"cache_misses", report.RSSStats.CachedMisses,
		"cache_hit_rate", fmt.Sprintf("%.2f%%", report.RSSStats.CacheHitRate),
	)
}