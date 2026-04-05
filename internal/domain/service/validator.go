package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wolfitem/ai-rss/internal/domain/model"
)

// Validator 提供输入验证功能
type Validator struct{}

// NewValidator 创建新的验证器实例
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateFilePath 验证文件路径安全性
func (v *Validator) ValidateFilePath(filePath string) error {
	// 检查文件路径是否为空
	if strings.TrimSpace(filePath) == "" {
		return errors.New("文件路径不能为空")
	}

	// 使用filepath.Clean清理路径
	cleanPath := filepath.Clean(filePath)

	// 检查路径是否包含目录遍历尝试
	if strings.Contains(cleanPath, "..") || strings.Contains(cleanPath, "~") {
		return fmt.Errorf("路径包含非法字符: %s", cleanPath)
	}

	// 验证是否为绝对路径
	if !filepath.IsAbs(cleanPath) { 
		// 相对于工作目录解析
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("获取工作目录失败: %w", err)
		}
		absPath := filepath.Join(wd, cleanPath)
		cleanPath = filepath.Clean(absPath)
	}

	// 确保路径在允许的根目录下
	allowedRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}
	
	relPath, err := filepath.Rel(allowedRoot, cleanPath)
	if err != nil {
		return fmt.Errorf("验证路径相对位置失败: %w", err)
	}
	
	// 确保相对路径不以".."开头，防止访问父目录
	if strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("路径尝试访问工作目录外部: %s", cleanPath)
	}

	// 检查文件扩展名
	if !strings.HasSuffix(strings.ToLower(cleanPath), ".opml") {
		return fmt.Errorf("只允许.OPML文件格式: %s", cleanPath)
	}

	// 验证文件是否存在且具有可读权限
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("文件访问失败: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("路径指向目录而非文件: %s", cleanPath)
	}

	// 尝试读取文件内容以验证可读性
	file, err := os.Open(cleanPath)
	if err != nil {
		return fmt.Errorf("文件无法打开读取: %w", err)
	}
	file.Close()

	// 验证文件大小合理性（最大10MB限制）
	if info.Size() > 10*1024*1024 {
		return fmt.Errorf("文件过大(>10MB): %s", cleanPath)
	}

	return nil
}

// ValidateURL 验证RSS源URL合法性
func (v *Validator) ValidateURL(url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("URL不能为空")
	}

	// 基本格式验证
	urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9\-\.]+\.[a-zA-Z]{2,}(?:[/\w\.-]*)*/?$`)
	if !urlRegex.MatchString(url) {
		return fmt.Errorf("无效的URL格式: %s", url)
	}

	// 限制协议类型
	lowerURL := strings.ToLower(url)
	if !strings.HasPrefix(lowerURL, "http://") && !strings.HasPrefix(lowerURL, "https://") {
		return fmt.Errorf("只允许HTTP/HTTPS协议: %s", url)
	}

	// 黑名单检查 - 禁止访问内部网络
	blacklistDomains := []string{
		"localhost", "127.0.0.1", "0.0.0.0", "::1",
		"192.168.", "10.0.", "172.16.", "169.254.",
	}

	for _, banned := range blacklistDomains {
		if strings.Contains(lowerURL, banned) {
			return fmt.Errorf("禁止访问内部网络地址: %s", banned)
		}
	}

	// 域名验证
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	
	// 提取域名部分
	var domain string
	if strings.HasPrefix(lowerURL, "http://") {
		domain = strings.TrimPrefix(lowerURL, "http://")
	} else if strings.HasPrefix(lowerURL, "https://") {
		domain = strings.TrimPrefix(lowerURL, "https://")
	}
	
	// 移除端口号和路径
	if slashIndex := strings.Index(domain, "/"); slashIndex != -1 {
		domain = domain[:slashIndex]
	}
	if colonIndex := strings.Index(domain, ":"); colonIndex != -1 {
		domain = domain[:colonIndex]
	}

	// 验证域名格式
	parts := strings.Split(domain, ".")
	for _, part := range parts {
		if !domainRegex.MatchString(part) {
			return fmt.Errorf("无效域名: %s", part)
		}
	}

	return nil
}

// GetConfigValue 安全获取环境变量或默认配置值
func (v *Validator) GetConfigValue(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetAPIKey 安全获取Deepseek API密钥
func (v *Validator) GetAPIKey(configModel *model.DeepseekConfig) (string, error) {
	// 优先从环境变量获取
	if apiKey := os.Getenv("DEEPSEEK_API_KEY"); apiKey != "" {
		return apiKey, nil
	}

	// 检查配置文件中的API密钥
	if configModel == nil || configModel.APIKey == "" {
		return "", errors.New("未找到Deepseek API密钥配置，请设置环境变量: export DEEPSEEK_API_KEY=your-key-here")
	}

	// 检查是否为占位符
	if strings.Contains(configModel.APIKey, "****") {
		return "", errors.New("检测到占位符API密钥，请使用环境变量设置真实密钥")
	}

	return configModel.APIKey, nil
}