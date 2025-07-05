package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wolfitem/ai-rss/internal/domain/model"
	"github.com/wolfitem/ai-rss/internal/domain/service"
	"github.com/wolfitem/ai-rss/internal/infrastructure/database"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

// RssProcessorService 定义RSS处理器的应用服务接口
type RssProcessorService interface {
	// ProcessRss 处理RSS源并生成报告
	ProcessRss(params model.ProcessParams) (string, error)
}

// rssProcessorService 实现RssProcessorService接口
type rssProcessorService struct {
	rssService service.RssService
	// 用于跟踪API调用次数的变量
	apiCallCount int
	// 数据库相关
	db          database.Database
	articleRepo database.ArticleRepository
}

// NewRssProcessorService 创建一个新的RSS处理器服务实例
func NewRssProcessorService() RssProcessorService {
	return &rssProcessorService{
		rssService:   service.NewRssService(),
		apiCallCount: 0,
	}
}

// ProcessRss 处理RSS源并生成报告
// 该函数是整个处理流程的入口点，包括解析OPML、获取文章、分析内容和生成报告
func (s *rssProcessorService) ProcessRss(params model.ProcessParams) (string, error) {
	logger.Info("开始处理RSS源", "opml_file", params.OpmlFile, "days_back", params.DaysBack)
	defer logger.TimeTrack("ProcessRss")()

	// 记录初始内存使用情况
	logger.LogMemStatsOnce()

	// 初始化数据库（如果启用）
	if params.DatabaseConfig.Enabled {
		if err := s.initDatabase(params.DatabaseConfig); err != nil {
			logger.Error("初始化数据库失败", "error", err)
			return "", fmt.Errorf("初始化数据库失败: %w", err)
		}
		// 确保在函数结束时关闭数据库连接
		defer func() {
			if s.db != nil {
				s.db.Close()
			}
		}()
	}

	// 1. 解析OPML文件获取RSS源列表
	logger.Debug("开始解析OPML文件", "file", params.OpmlFile)
	sources, err := s.rssService.ParseOpml(params.OpmlFile)
	if err != nil {
		logger.Error("解析OPML文件失败", "error", err)
		return "", fmt.Errorf("解析OPML文件失败: %w", err)
	}
	logger.Info("成功解析OPML文件", "sources_count", len(sources))

	// 2. 从RSS源获取文章
	logger.Debug("开始获取文章", "sources_count", len(sources))
	// 传递RSS配置
	articles, err := s.rssService.FetchArticles(sources, params.DaysBack, params.RssConfig)
	if err != nil {
		logger.Error("获取文章失败", "error", err)
		return "", fmt.Errorf("获取文章失败: %w", err)
	}
	logger.Info("成功获取文章", "articles_count", len(articles))

	// 3. 如果没有文章，返回空报告
	if len(articles) == 0 {
		logger.Info("没有找到文章", "days_back", params.DaysBack)
		return fmt.Sprintf("# 新闻摘要报告\n\n没有找到最近%d天内的文章。", params.DaysBack), nil
	}

	// 4. 分析文章内容
	logger.Info("开始分析文章内容", "articles_count", len(articles))
	analysisResults := s.analyzeArticles(articles, params)

	// 5. 保存分析结果到数据库（如果启用）
	if params.DatabaseConfig.Enabled && s.articleRepo != nil {
		logger.Info("开始保存分析结果到数据库", "results_count", len(analysisResults))
		for _, result := range analysisResults {
			if err := s.articleRepo.SaveArticle(result); err != nil {
				logger.Error("保存文章到数据库失败", "title", result.Title, "error", err)
				// 继续处理其他文章，不中断流程
			}
		}
	}

	// 6. 生成报告
	logger.Info("开始生成报告", "results_count", len(analysisResults))
	report := s.generateReport(analysisResults, params.DaysBack)

	logger.Info("RSS处理完成", "report_length", len(report))
	return report, nil
}

// processArticleTask 处理单个文章任务
func (s *rssProcessorService) processArticleTask(article model.Article, params model.ProcessParams) (model.AnalysisResult, bool) {
	// 检查文章内容是否为空
	if article.Content == "" {
		logger.Warn("文章内容为空，跳过处理", "title", article.Title)
		return model.AnalysisResult{}, true
	}

	// 如果启用了数据库，检查文章是否已存在
	if params.DatabaseConfig.Enabled && s.articleRepo != nil {
		exists, err := s.articleRepo.ArticleExists(article.Link)
		if err != nil {
			logger.Error("检查文章是否存在失败", "title", article.Title, "error", err)
			// 继续处理，不中断流程
		} else if exists {
			logger.Info("文章已存在于数据库中，跳过处理", "title", article.Title, "link", article.Link)
			// 从数据库获取已存在的文章
			existingArticle, err := s.articleRepo.GetArticleByLink(article.Link)
			if err == nil && existingArticle != nil {
				logger.Info("已从数据库获取文章", "title", existingArticle.Title)
				return *existingArticle, false
			}
			// 如果获取失败，继续正常处理
		}
	}

	result := model.AnalysisResult{
		Title:    article.Title,
		Summary:  s.processArticleContent(article, params.DeepseekConfig),
		Source:   article.Source.Title,
		PubDate:  article.PublishDate,
		Category: "未分类", // 默认分类
		Link:     article.Link,
	}

	// 尝试从内容中提取分类信息
	category := s.extractCategoryFromContent(article.Content)
	if category != "" {
		result.Category = category
	}

	logger.Debug("处理后的文章内容", "result.Summary", result.Summary, "content_length", len(result.Summary))
	return result, false
}

// analyzeArticles 使用Deepseek API分析文章内容
func (s *rssProcessorService) analyzeArticles(articles []model.Article, params model.ProcessParams) []model.AnalysisResult {
	logger.Debug("开始准备文章内容进行分析", "articles_count", len(articles))

	// 设置并发数量
	concurrency := 5
	if params.RssConfig.Concurrency > 0 {
		concurrency = params.RssConfig.Concurrency
	}
	logger.Info("使用并发处理文章分析", "concurrency", concurrency)

	// 创建工作通道和结果通道
	type articleTask struct {
		article model.Article
		index   int
	}
	type analysisResultWithIndex struct {
		result model.AnalysisResult
		index  int
		skip   bool
	}

	workChan := make(chan articleTask, len(articles))
	resultChan := make(chan analysisResultWithIndex, len(articles))

	// 启动工作协程
	for i := 0; i < concurrency; i++ {
		go func() {
			for task := range workChan {
				result, skip := s.processArticleTask(task.article, params)
				resultChan <- analysisResultWithIndex{result: result, index: task.index, skip: skip}
			}
		}()
	}

	// 发送任务到工作通道
	for i, article := range articles {
		workChan <- articleTask{article: article, index: i}
	}
	close(workChan)

	// 收集结果
	resultsMap := make(map[int]model.AnalysisResult)
	skippedCount := 0
	for i := 0; i < len(articles); i++ {
		result := <-resultChan
		if !result.skip {
			resultsMap[result.index] = result.result
		} else {
			skippedCount++
		}
	}

	// 按原始顺序整理结果
	var results []model.AnalysisResult
	for i := 0; i < len(articles); i++ {
		if result, ok := resultsMap[i]; ok {
			results = append(results, result)
		}
	}

	logger.Info("文章分析完成", "results_count", len(results), "skipped_count", skippedCount)
	return results
}

// processArticleContent 处理文章内容，如果内容过长则进行摘要
func (s *rssProcessorService) processArticleContent(article model.Article, config model.DeepseekConfig) string {
	content := article.Content

	// 如果内容超过100字符，调用API进行摘要
	if len(content) > 100 {
		logger.Debug("文章内容过长，进行摘要处理", "title", article.Title, "content_length", len(content))
		summary, err := s.summarizeContent(content, config)
		if err != nil {
			logger.Error("摘要处理失败，使用降级策略", "title", article.Title, "error", err)
			// 降级策略：截取前500字符作为摘要
			if len(content) > 500 {
				content = content[:500] + "..."
				logger.Info("使用截取策略作为降级方案", "title", article.Title, "truncated_length", len(content))
			}
		} else {
			logger.Debug("摘要处理成功", "summary", summary, "summary_length", len(summary))
			content = summary
		}
	}

	return content
}

// summarizeContent 使用Deepseek API对内容进行摘要
func (s *rssProcessorService) summarizeContent(content string, config model.DeepseekConfig) (string, error) {
	// 准备提示词
	prompt := fmt.Sprintf(`请将以下内容总结为100字以内的摘要：
%s`, content)

	// 调用API
	result, err := s.callDeepseekAPI(prompt, config)
	if err != nil {
		return "", err
	}

	return result, nil
}

// processAPIResponse 处理API响应
func (s *rssProcessorService) processAPIResponse(responseBody []byte, responsePreview string) (string, error) {
	// 解析响应JSON，使用更健壮的错误处理
	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		logger.Error("解析API响应失败", "error", err, "response_preview", responsePreview)
		return "", fmt.Errorf("解析API响应失败: %w", err)
	}

	// 提取响应内容，使用更健壮的类型断言
	var content string

	// 尝试从响应中提取内容
	if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
		if firstChoice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := firstChoice["message"].(map[string]interface{}); ok {
				if contentStr, ok := message["content"].(string); ok {
					content = contentStr
				} else {
					logger.Error("API响应中content字段类型错误", "response_preview", responsePreview)
					return "", fmt.Errorf("API响应中content字段类型错误")
				}
			} else {
				logger.Error("API响应中message字段类型错误", "response_preview", responsePreview)
				return "", fmt.Errorf("API响应中message字段类型错误")
			}
		} else {
			logger.Error("API响应中choice字段类型错误", "response_preview", responsePreview)
			return "", fmt.Errorf("API响应中choice字段类型错误")
		}
	} else {
		logger.Error("API响应中choices字段类型错误或为空", "response_preview", responsePreview)
		return "", fmt.Errorf("API响应中choices字段类型错误或为空")
	}

	// 记录API使用情况统计
	if usage, hasUsage := response["usage"].(map[string]interface{}); hasUsage {
		logger.Info("Deepseek API使用统计",
			"prompt_tokens", usage["prompt_tokens"],
			"completion_tokens", usage["completion_tokens"],
			"total_tokens", usage["total_tokens"])
	}

	return content, nil
}

// readAPIResponse 读取API响应
func (s *rssProcessorService) readAPIResponse(resp *http.Response, config model.DeepseekConfig) ([]byte, error) {
	readTimeout := s.getReadTimeout(config)
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	return s.readResponseWithTimeout(ctx, resp)
}

// getReadTimeout 获取读取超时时间
func (s *rssProcessorService) getReadTimeout(config model.DeepseekConfig) time.Duration {
	readTimeout := 120 * time.Second
	if config.ReadTimeout > 0 {
		readTimeout = time.Duration(config.ReadTimeout) * time.Second
	}
	return readTimeout
}

// readResponseWithTimeout 在超时控制下读取响应
func (s *rssProcessorService) readResponseWithTimeout(ctx context.Context, resp *http.Response) ([]byte, error) {
	responseBodyChan := make(chan []byte, 1)
	readErrChan := make(chan error, 1)

	go func() {
		body, err := s.readResponseBody(resp)
		if err != nil {
			readErrChan <- err
			return
		}
		responseBodyChan <- body
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("读取API响应超时: %w", ctx.Err())
	case err := <-readErrChan:
		return nil, err
	case body := <-responseBodyChan:
		return body, nil
	}
}

// readResponseBody 读取响应体内容
func (s *rssProcessorService) readResponseBody(resp *http.Response) ([]byte, error) {
	// 使用带缓冲的读取方式，避免大响应体导致的内存问题
	const maxSize = 10 * 1024 * 1024 // 10MB 最大响应大小限制

	// 使用 LimitReader 限制读取大小
	limitedReader := io.LimitReader(resp.Body, maxSize+1)

	// 读取响应体
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	// 检查是否超过大小限制
	if len(body) > maxSize {
		return nil, fmt.Errorf("响应体过大，超过%d字节限制", maxSize)
	}

	logger.Debug("成功读取API响应", "response_size_bytes", len(body))
	return body, nil
}

// handleAPIError 处理API错误
func (s *rssProcessorService) handleAPIError(statusCode int, responseBody []byte, requestDuration time.Duration) error {
	// 记录详细的错误信息
	logger.Error("API请求返回错误",
		"status_code", statusCode,
		"response", string(responseBody),
		"request_duration_ms", requestDuration.Milliseconds())

	// 根据状态码提供更具体的错误信息
	var errMsg string
	switch statusCode {
	case 429:
		errMsg = "API请求频率过高，请稍后重试"
	case 401, 403:
		errMsg = "API认证失败，请检查API密钥"
	case 500, 502, 503, 504:
		errMsg = "API服务器错误，请稍后重试"
	default:
		errMsg = fmt.Sprintf("API请求返回错误(状态码:%d): %s", statusCode, string(responseBody))
	}
	return errors.New(errMsg)
}

// prepareDeepseekRequest 准备Deepseek API请求
func (s *rssProcessorService) prepareDeepseekRequest(prompt string, config model.DeepseekConfig) (*http.Request, string, error) {
	// 准备请求URL和请求体
	apiURL := config.APIUrl
	if apiURL == "" {
		apiURL = "https://api.deepseek.com/v1/chat/completions"
		logger.Warn("未配置Deepseek API URL，使用默认值", "default_url", apiURL)
	}
	requestBody := map[string]interface{}{
		"model": config.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": config.MaxTokens,
	}

	// 将请求体转换为JSON
	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		logger.Error("构建API请求失败", "error", err)
		return nil, "", fmt.Errorf("构建API请求失败: %w", err)
	}

	// 创建HTTP请求
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(requestJSON))
	if err != nil {
		logger.Error("创建HTTP请求失败", "error", err)
		return nil, "", fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	return req, apiURL, nil
}

// sendDeepseekRequest 发送Deepseek API请求
func (s *rssProcessorService) sendDeepseekRequest(req *http.Request, _ string, config model.DeepseekConfig) (string, error) {
	client := s.createHTTPClient(config)
	return s.executeRequestWithRetry(req, client, config)
}

// createHTTPClient 创建配置好的HTTP客户端
func (s *rssProcessorService) createHTTPClient(config model.DeepseekConfig) *http.Client {
	// 使用配置中的超时时间，如果没有配置则使用默认值
	apiTimeout := 45 * time.Second
	if config.APITimeout > 0 {
		apiTimeout = time.Duration(config.APITimeout) * time.Second
	}

	return &http.Client{
		Timeout: apiTimeout,
		Transport: &http.Transport{
			ResponseHeaderTimeout: apiTimeout / 2,
			ExpectContinueTimeout: 10 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			DisableKeepAlives:     false,
			MaxIdleConnsPerHost:   10,
		},
	}
}

// executeRequestWithRetry 执行带重试的HTTP请求
func (s *rssProcessorService) executeRequestWithRetry(req *http.Request, client *http.Client, config model.DeepseekConfig) (string, error) {
	apiTimeout := s.getAPITimeout(config)
	logger.Debug("发送Deepseek API请求", "url", req.URL.String(), "timeout", apiTimeout.String())

	maxRetries := 3
	var lastErr error

	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		if retryCount > 0 {
			backoffTime := s.calculateBackoffTime(retryCount)
			if lastErr != nil && !s.isRetryableError(lastErr) {
				logger.Warn("错误不可重试，停止重试", "error", lastErr)
				break
			}
			logger.Info("API请求重试等待", "retry_count", retryCount, "backoff_time_ms", backoffTime.Milliseconds())
			time.Sleep(backoffTime)
		}

		result, err := s.executeSingleRequest(req, client, config, apiTimeout)
		if err == nil {
			return result, nil
		}

		lastErr = err
		logger.Warn("API请求失败，准备重试", "error", err, "attempt", retryCount+1)
	}

	return "", fmt.Errorf("API请求失败，已重试%d次: %w", maxRetries, lastErr)
}

// getAPITimeout 获取API超时时间
func (s *rssProcessorService) getAPITimeout(config model.DeepseekConfig) time.Duration {
	apiTimeout := 45 * time.Second
	if config.APITimeout > 0 {
		apiTimeout = time.Duration(config.APITimeout) * time.Second
	}
	return apiTimeout
}

// calculateBackoffTime 计算退避时间
func (s *rssProcessorService) calculateBackoffTime(retryCount int) time.Duration {
	// 安全的指数退避计算，避免整数溢出
	exponent := retryCount - 1
	if exponent > 10 { // 限制指数避免溢出
		exponent = 10
	}
	backoffTime := time.Duration(1<<exponent) * time.Second
	if backoffTime > 10*time.Second {
		backoffTime = 10 * time.Second // 最大退避10秒
	}
	return backoffTime
}

// executeSingleRequest 执行单次HTTP请求
func (s *rssProcessorService) executeSingleRequest(req *http.Request, client *http.Client, config model.DeepseekConfig, apiTimeout time.Duration) (string, error) {
	start := time.Now()

	// 为每次请求创建新的上下文，避免使用已取消的上下文
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	// 创建新的请求，使用新的上下文
	reqWithCtx := req.WithContext(ctx)
	resp, err := client.Do(reqWithCtx)
	requestDuration := time.Since(start)

	if err != nil {
		return "", s.handleRequestError(err, requestDuration)
	}

	return s.processResponse(resp, config, requestDuration)
}

// handleRequestError 处理请求错误
func (s *rssProcessorService) handleRequestError(err error, requestDuration time.Duration) error {
	if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "timeout") {
		logger.Warn("API请求超时", "error", err, "duration_ms", requestDuration.Milliseconds())
	} else {
		logger.Warn("发送API请求失败", "error", err, "duration_ms", requestDuration.Milliseconds())
	}
	return err
}

// processResponse 处理响应
func (s *rssProcessorService) processResponse(resp *http.Response, config model.DeepseekConfig, requestDuration time.Duration) (string, error) {
	// 读取响应
	responseBody, err := s.readAPIResponse(resp, config)
	// 立即关闭响应体，避免在循环中使用defer
	if resp != nil && resp.Body != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Warn("关闭响应体失败", "error", closeErr)
		}
	}

	if err != nil {
		return "", err
	}

	// 检查响应状态码并处理错误
	if resp.StatusCode != http.StatusOK {
		return "", s.handleAPIError(resp.StatusCode, responseBody, requestDuration)
	}
			logger.Info("API请求重试等待", "retry_count", retryCount, "backoff_time_ms", backoffTime.Milliseconds())

			// 检查是否是可重试的错误
			if lastErr != nil && !s.isRetryableError(lastErr) {
				logger.Warn("遇到不可重试的错误，停止重试", "error", lastErr)
				break
			}

			time.Sleep(backoffTime)
		}

		start := time.Now()

		// 为每次请求创建新的上下文，避免使用已取消的上下文
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)

		// 创建新的请求，使用新的上下文
		reqWithCtx := req.WithContext(ctx)
		resp, lastErr = client.Do(reqWithCtx)
		requestDuration = time.Since(start)

		// 请求完成后取消上下文
		cancel()

		// 处理错误情况
		if lastErr != nil {
			// 特别处理超时错误
			if strings.Contains(lastErr.Error(), "context deadline exceeded") || strings.Contains(lastErr.Error(), "timeout") {
				logger.Warn("API请求超时，准备重试", "error", lastErr, "attempt", retryCount+1, "duration_ms", requestDuration.Milliseconds())
			} else {
				logger.Warn("发送API请求失败，准备重试", "error", lastErr, "attempt", retryCount+1, "duration_ms", requestDuration.Milliseconds())
			}
			continue
		}

		// 读取响应
		responseBody, err := s.readAPIResponse(resp, config)
		// 立即关闭响应体，避免在循环中使用defer
		if resp != nil && resp.Body != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				logger.Warn("关闭响应体失败", "error", closeErr)
			}
		}

		if err != nil {
			return "", err
		}

		// 检查响应状态码并处理错误
		if resp.StatusCode != http.StatusOK {
			return "", s.handleAPIError(resp.StatusCode, responseBody, requestDuration)
		}

		// 记录原始响应内容（仅记录预览，避免日志过大）
		responsePreview := string(responseBody)
		if len(responsePreview) > 200 {
			responsePreview = responsePreview[:200] + "..."
		}
		logger.Debug("Deepseek API原始响应", "response_length", len(responseBody), "response_preview", responsePreview)

		// 处理响应
		content, err := s.processAPIResponse(responseBody, responsePreview)
		if err != nil {
			return "", err
		}

		logger.Info("成功获取Deepseek API响应", "content_length", len(content), "duration_ms", requestDuration.Milliseconds())
		logger.Debug("API响应内容", "content", content)

		return content, nil
	}

	// 所有重试都失败
	if lastErr != nil {
		logger.Error("发送API请求失败，已达到最大重试次数", "error", lastErr, "max_retries", maxRetries)
		return "", fmt.Errorf("发送API请求失败: %w", lastErr)
	}

	return "", nil
}

// isRetryableError 判断错误是否可重试
func (s *rssProcessorService) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// 网络相关错误通常可重试
	retryableErrors := []string{
		"context deadline exceeded",
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"network is unreachable",
		"no such host",
	}

	for _, retryableErr := range retryableErrors {
		if strings.Contains(errStr, retryableErr) {
			return true
		}
	}

	// 认证错误不可重试
	nonRetryableErrors := []string{
		"401",
		"403",
		"invalid api key",
		"unauthorized",
	}

	for _, nonRetryableErr := range nonRetryableErrors {
		if strings.Contains(errStr, nonRetryableErr) {
			return false
		}
	}

	// 默认认为可重试
	return true
}

// callDeepseekAPI 调用Deepseek API进行内容分析
func (s *rssProcessorService) callDeepseekAPI(prompt string, config model.DeepseekConfig) (string, error) {
	// 增加API调用计数
	s.apiCallCount++
	logger.Info("调用Deepseek API", "call_count", s.apiCallCount, "model", config.Model)

	// 检查API调用次数是否超过限制
	if config.MaxCalls > 0 && s.apiCallCount > config.MaxCalls {
		logger.Warn("已达到API调用次数上限", "max_calls", config.MaxCalls, "current_calls", s.apiCallCount)
		return "", nil
	}

	// 检查API密钥是否配置
	if config.APIKey == "" {
		logger.Error("未配置Deepseek API密钥")
		return "", fmt.Errorf("未配置Deepseek API密钥")
	}

	// 记录API请求参数（不包含完整的API密钥）
	apiKeyMasked := "****" + config.APIKey[len(config.APIKey)-4:]
	logger.Debug("Deepseek API请求参数",
		"model", config.Model,
		"max_tokens", config.MaxTokens,
		"api_key", apiKeyMasked,
		"prompt_length", len(prompt))
	logger.Debug("Deepseek API提示词", "prompt_preview", prompt)

	// 准备请求
	req, apiURL, err := s.prepareDeepseekRequest(prompt, config)
	if err != nil {
		return "", err
	}

	// 发送请求
	return s.sendDeepseekRequest(req, apiURL, config)
}

// generateReport 根据分析结果生成报告
func (s *rssProcessorService) generateReport(results []model.AnalysisResult, daysBack int) string {
	// 如果没有结果，返回空报告
	if len(results) == 0 {
		return fmt.Sprintf("# 新闻摘要报告\n\n没有找到最近%d天内的文章。", daysBack)
	}

	// 按分类对结果进行分组
	categoryMap := make(map[string][]model.AnalysisResult)
	for _, result := range results {
		category := result.Category
		if category == "" {
			category = "未分类"
		}
		categoryMap[category] = append(categoryMap[category], result)
	}

	// 获取所有分类并排序
	categories := make([]string, 0, len(categoryMap))
	for category := range categoryMap {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	// 生成报告标题
	now := time.Now().Format("2006-01-02")
	report := fmt.Sprintf("# 新闻摘要报告 (%s)\n\n", now)
	report += fmt.Sprintf("本报告包含最近%d天内的%d篇文章摘要，按分类整理。\n\n", daysBack, len(results))

	// 为每个分类生成表格内容
	for _, category := range categories {
		articles := categoryMap[category]
		report += fmt.Sprintf("## %s (%d篇)\n\n", category, len(articles))

		// 添加表格头部
		report += "| 标题 | 摘要 | 来源 | 发布日期 | 链接 |\n"
		report += "|------|------|------|----------|------|\n"

		// 添加每篇文章的表格行
		for _, article := range articles {
			// 处理摘要，确保不会破坏表格格式
			summary := strings.ReplaceAll(article.Summary, "\n", " ")
			summary = strings.ReplaceAll(summary, "|", "\\|")

			// 处理标题，确保不会破坏表格格式
			title := strings.ReplaceAll(article.Title, "\n", " ")
			title = strings.ReplaceAll(title, "|", "\\|")

			// 添加表格行
			report += fmt.Sprintf("| %s | %s | %s | %s | [链接](%s) |\n",
				title, summary, article.Source, article.PubDate, article.Link)
		}

		// 在表格后添加空行
		report += "\n"
	}

	return report
}

// extractCategoryFromContent 从内容中提取分类信息
func (s *rssProcessorService) extractCategoryFromContent(content string) string {
	// 使用正则表达式尝试提取分类信息
	if category := s.extractCategoryFromRegex(content); category != "" {
		return category
	}

	// 根据内容关键词判断分类
	return s.extractCategoryFromKeywords(strings.ToLower(content))
}

// extractCategoryFromRegex 使用正则表达式从内容中提取分类信息
func (s *rssProcessorService) extractCategoryFromRegex(content string) string {
	categoryRegex := regexp.MustCompile(`(?i)(?:分类|category)[：:](\s*)(\w+)`)
	matches := categoryRegex.FindStringSubmatch(content)

	if len(matches) >= 3 {
		return strings.TrimSpace(matches[2])
	}
	return ""
}

// extractCategoryFromKeywords 根据关键词判断分类
func (s *rssProcessorService) extractCategoryFromKeywords(lowerContent string) string {
	switch {
	case strings.Contains(lowerContent, "代码") ||
		strings.Contains(lowerContent, "编程"):
		return "技术"
	case strings.Contains(lowerContent, "人工智能") ||
		strings.Contains(lowerContent, "AI"):
		return "AI"
	case strings.Contains(lowerContent, "市场") ||
		strings.Contains(lowerContent, "经济"):
		return "商业"
	default:
		return "其他"
	}
}

// initDatabase 初始化数据库
func (s *rssProcessorService) initDatabase(config model.DatabaseConfig) error {
	logger.Info("初始化数据库", "enabled", config.Enabled, "file_path", config.FilePath)

	if !config.Enabled {
		logger.Info("数据库功能未启用，跳过初始化")
		return nil
	}

	// 创建数据库实例
	s.db = database.NewSQLiteDatabase(config.FilePath)

	// 初始化数据库
	if err := s.db.Init(); err != nil {
		logger.Error("初始化数据库失败", "error", err)
		return fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 创建文章存储库
	s.articleRepo = database.NewSQLiteArticleRepository(s.db)
	logger.Info("数据库和文章存储库初始化成功")
	return nil
}
