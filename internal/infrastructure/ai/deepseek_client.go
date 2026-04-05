package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/wolfitem/ai-rss/internal/domain/model"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

// DeepseekClient 实现service.AIClient接口
type DeepseekClient struct {
	config   model.DeepseekConfig
	client   *http.Client
	rateInfo RateLimit
}

// RateLimit 定义速率限制信息
type RateLimit struct {
	MaxCalls     int
	CurrentCalls int
	Remaining    int
	ResetTime    time.Time
}

// NewDeepseekClient 创建新的Deepseek客户端
func NewDeepseekClient(config model.DeepseekConfig) *DeepseekClient {
	// 设置安全的HTTP客户端配置
	https := &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		DisableKeepAlives:     false,
	}
	
	client := &http.Client{
		Timeout:   time.Duration(config.APITimeout) * time.Second,
		Transport: https,
	}

	return &DeepseekClient{
		config: config,
		client: client,
		rateInfo: RateLimit{
			MaxCalls:  config.MaxCalls,
			Remaining: config.MaxCalls,
			ResetTime: time.Now().Add(24 * time.Hour),
		},
	}
}

// GenerateSummary 生成内容摘要
func (c *DeepseekClient) GenerateSummary(ctx context.Context, content string) (string, error) {
	if c.config.MaxCalls > 0 && c.rateInfo.CurrentCalls >= c.config.MaxCalls {
		return "", fmt.Errorf("已达到API调用次数上限: %d/%d", c.rateInfo.CurrentCalls, c.config.MaxCalls)
	}

	prompt := fmt.Sprintf(`# 任务：文章摘要

请为以下内容生成100字以内的中文摘要，要求：
1. 保留关键信息
2. 语言简洁明了
3. 重点突出

内容：
%s

请返回纯文本摘要，不要添加任何前缀或后缀。`, content)

	result, err := c.callAPI(ctx, prompt)
	if err != nil {
		return "", err
	}

	c.rateInfo.CurrentCalls++
	c.rateInfo.Remaining = c.config.MaxCalls - c.rateInfo.CurrentCalls

	return result, nil
}

// callAPI 调用Deepseek API
func (c *DeepseekClient) callAPI(ctx context.Context, prompt string) (string, error) {
	endpoint := c.config.APIUrl
	if endpoint == "" {
		endpoint = "https://api.deepseek.com/v1/chat/completions"
	}

	// 验证URL
	_, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("无效的API端点: %w", err)
	}

	// 构建请求体
	requestBody := map[string]interface{}{
		"model": c.config.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":    c.config.MaxTokens,
		"stream":        false,
		"temperature":   0.3,
		"top_p":         0.7,
		"response_format": map[string]string{"type": "text"},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("创建请求体失败: %w", err)
	}

	// 延迟重试机制
	maxRetries := 3
	baseDelay := time.Second
	
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			// 指数退避重试
			backoff := time.Duration(exponentialBackoff(i, baseDelay))
			logger.Infof("请求失败第%d次，等待%dms后重试", i, backoff.Milliseconds())
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		result, err := c.doRequest(ctx, endpoint, jsonData)
		if err != nil {
			// 检查是否是可重试错误
			if isRetryableError(err) && i < maxRetries-1 {
				continue
			}
			return "", err
		}
		return result, nil
	}

	return "", fmt.Errorf("API调用失败，已重试%d次", maxRetries)
}

// doRequest 执行HTTP请求
func (c *DeepseekClient) doRequest(ctx context.Context, endpoint string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("User-Agent", "AI-RSS-Client/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API返回错误: %d %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("响应不包含有效内容")
	}

	logger.Infof("API调用成功 tokens: %d/%d", response.Usage.PromptTokens, response.Usage.TotalTokens)
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

// exponentialBackoff 指数退避计算
func exponentialBackoff(attempt int, baseDelay time.Duration) int64 {
	if attempt == 0 {
		return 0
	}
	
	// 指数退避 + 随机抖动
	base := float64(baseDelay.Milliseconds())
	exponential := base * float64(1<<attempt) // 2的n次方
	
	maxJitter := exponential * 0.1 // 10%的随机抖动
	jitter, _ := rand.Int(rand.Reader, big.NewInt(int64(maxJitter)))
	
	delay := int64(exponential) + jitter.Int64()
	
	// 限制最大退避时间为5分钟
	maxDelay := int64(5 * time.Minute.Milliseconds())
	if delay > maxDelay {
		delay = maxDelay
	}
	
	return delay
}

// isRetryableError 判断错误是否可重试func isRetryableError(err error) bool {
	retryableErrors := []string{
		"timeout",
		"connection",
		"reset",
		"unreachable",
		"temporary",
		"503",
		"429",
		"502",
		"504",
	}
	
	for _, keyword := range retryableErrors {
		if strings.Contains(err.Error(), keyword) {
			return true
		}
	}
	return false
}

// GetRateLimits 返回当前速率限制信息
func (c *DeepseekClient) GetRateLimits() RateLimit {
	return c.rateInfo
}

// Reset 重置速率限制
func (c *DeepseekClient) Reset() {
	c.rateInfo = RateLimit{
		MaxCalls:  c.config.MaxCalls,
		CurrentCalls: 0,
		Remaining: c.config.MaxCalls,
		ResetTime: time.Now().Add(24 * time.Hour),
	}
}