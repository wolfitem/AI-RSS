package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter 提供了关键的限流功能
type RateLimiter struct {
	mu            sync.RWMutex
	requestsCount int64
	lastReset     time.Time
	window       time.Duration
	maxRequests  int64
}

// NewRateLimiter 创建新的速率限制器
func NewRateLimiter(maxRequests int64, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		window:      window,
		lastReset:   time.Now(),
	}
}

// Check 检查是否超过限额
func (rl *RateLimiter) Check() bool {
	if rl.maxRequests <= 0 {
		return true // 无限速据
	}

	now := time.Now()
	
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 重置窗口周期
	if now.Sub(rl.lastReset) >= rl.window {
		rl.requestsCount = 0
		rl.lastReset = now
	}

	if rl.requestsCount < rl.maxRequests {
		rl.requestsCount++
		return true
	}

	return false
}

// GetStatus 获取当前状态
func (rl *RateLimiter) GetStatus() Status {
	now := time.Now()
	
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	remaining := rl.maxRequests - rl.requestsCount
	if remaining < 0 {
		remaining = 0
	}

	percentUsed := float64(rl.requestsCount) / float64(rl.maxRequests) * 100
	
	return Status{
		Limit:        rl.maxRequests,
		Used:         rl.requestsCount,
		Remaining:    remaining,
		PercentUsed:   percentUsed,
		ResetIn:      rl.window - now.Sub(rl.lastReset),
	}
}

// Status 速率限制状态
type Status struct {
	Limit        int64
	Used         int64
	Remaining    int64
	PercentUsed  float64
	ResetIn      time.Duration
}

// GlobalRateLimiter 全局API调用限制器
var GlobalRateLimiter = NewRateLimiter(300, 24*time.Hour)

// LimitMiddleware 限流中间件
type LimitMiddleware func(context.Context, func() error) error

// NewLimitMiddleware 创建限流中间件
func NewLimitMiddleware(limiter *RateLimiter) LimitMiddleware {
	if limiter == nil {
		limiter = GlobalRateLimiter
	}

	return func(ctx context.Context, fn func() error) error {
		if limiter.Check() {
			return fn()
		}
		
		return &RateLimitError{Status: limiter.GetStatus()}
	}
}

// WithLimiter 使用限流器
func (rl *RateLimiter) WithLimiter(ctx context.Context, fn func() error) error {
	if rl.Check() {
		return fn()
	}
	
	return &RateLimitError{Status: rl.GetStatus()}
}

// RateLimitError 限流错误
type RateLimitError struct {
	Status Status
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: %d/%d used, reset in %v", 
		e.Status.Used, e.Status.Limit, e.Status.ResetIn.Round(time.Second))
}

// RetryWithBackoff 带退避重试的辅助函数
func RetryWithBackoff(ctx context.Context, maxRetries int, baseDelay time.Duration, fn func() error) error {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	
	if baseDelay <= 0 {
		baseDelay = time.Second
	}

	for i := 0; i < maxRetries; i++ {
		if err := fn(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			// 简单延迟重试
			delay := time.Duration(i+1) * baseDelay
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}
		return nil
	}

	return fmt.Errorf("max retries reached")
}