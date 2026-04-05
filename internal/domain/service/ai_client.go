package service

import (
	"context"
	"time"
)

// AIClient 定义AI客户端接口
type AIClient interface {
	// GenerateSummary 基于内容生成摘要
	GenerateSummary(ctx context.Context, content string) (string, error)
	// GetRateLimits 获取API限制信息
	GetRateLimits() RateLimit
}

// RateLimit 定义API限制信息
type RateLimit struct {
	MaxCalls     int
	CurrentCalls int
	Remaining    int
	ResetTime    time.Time
}

// DeepseekClient 深度求索客户端接口扩展
type DeepseekClient AIClient