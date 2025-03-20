package service

import (
	"bytes"
	"context"
	"encoding/json"
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
	articles, err := s.rssService.FetchArticles(sources, params.DaysBack)
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
	analysisResults, err := s.analyzeArticles(articles, params)
	if err != nil {
		logger.Error("分析文章失败", "error", err)
		return "", fmt.Errorf("分析文章失败: %w", err)
	}

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

// analyzeArticles 使用Deepseek API分析文章内容
// 该函数处理文章内容，调用API进行分析，并返回分析结果
func (s *rssProcessorService) analyzeArticles(articles []model.Article, params model.ProcessParams) ([]model.AnalysisResult, error) {
	// 1. 准备文章内容
	logger.Debug("开始准备文章内容进行分析", "articles_count", len(articles))
	var results []model.AnalysisResult
	for _, article := range articles {
		// 检查文章内容是否为空
		if article.Content == "" {
			logger.Warn("文章内容为空，跳过处理", "title", article.Title)
			continue
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
					// 添加到结果中
					results = append(results, *existingArticle)
					logger.Info("已从数据库获取文章", "title", existingArticle.Title)
					continue
				}
				// 如果获取失败，继续正常处理
			}
		}

		result := model.AnalysisResult{
			Title:    article.Title,
			Summary:  s.processArticleContent(article, params.DeepseekConfig), // 内容已在之前的处理中被摘要
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

		results = append(results, result)

		logger.Debug("处理后的文章内容", "result.Summary", result.Summary, "content_length", len(result.Summary))
	}

	logger.Info("文章分析完成", "results_count", len(results))
	return results, nil
}

// processArticleContent 处理文章内容，如果内容过长则进行摘要
func (s *rssProcessorService) processArticleContent(article model.Article, config model.DeepseekConfig) string {
	content := article.Content

	// 如果内容超过100字符，调用API进行摘要
	if len(content) > 100 {
		logger.Debug("文章内容过长，进行摘要处理", "title", article.Title, "content_length", len(content))
		summary, err := s.summarizeContent(content, config)
		if err != nil {
			logger.Error("摘要处理失败，使用原始内容", "title", article.Title, "error", err)
			// 使用原始内容继续处理
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

// callDeepseekAPI 调用Deepseek API进行内容分析
func (s *rssProcessorService) callDeepseekAPI(prompt string, config model.DeepseekConfig) (string, error) {
	// 增加API调用计数
	s.apiCallCount++
	logger.Info("调用Deepseek API", "call_count", s.apiCallCount, "model", config.Model)

	// 检查API调用次数是否超过限制
	if config.MaxCalls > 0 && s.apiCallCount > config.MaxCalls {
		logger.Warn("已达到API调用次数上限", "max_calls", config.MaxCalls, "current_calls", s.apiCallCount)
		return "", nil // 直接返回空
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

	// 准备请求URL和请求体
	apiURL := config.APIUrl
	// 如果配置中的API URL为空，则使用默认值
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
		return "", fmt.Errorf("构建API请求失败: %w", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(requestJSON))
	if err != nil {
		logger.Error("创建HTTP请求失败", "error", err)
		return "", fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	// 发送请求，使用更短的超时时间
	client := &http.Client{Timeout: 60 * time.Second} // 将超时从60秒减少到30秒
	logger.Debug("发送Deepseek API请求", "url", apiURL, "timeout", "60s")
	start := time.Now()

	// 添加上下文超时控制
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	requestDuration := time.Since(start)
	logger.Debug("Deepseek API请求耗时", "duration_ms", requestDuration.Milliseconds())

	if err != nil {
		logger.Error("发送API请求失败", "error", err, "duration_ms", requestDuration.Milliseconds())
		return "", fmt.Errorf("发送API请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应，添加超时控制
	responseBodyChan := make(chan []byte, 1)
	readErrChan := make(chan error, 1)

	go func() {
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			readErrChan <- err
			return
		}
		responseBodyChan <- responseBody
	}()

	// 等待读取完成或超时
	var responseBody []byte
	select {
	case responseBody = <-responseBodyChan:
		// 读取成功
	case err = <-readErrChan:
		logger.Error("读取API响应失败", "error", err)
		return "", fmt.Errorf("读取API响应失败: %w", err)
	case <-time.After(10 * time.Second):
		logger.Error("读取API响应超时")
		return "", fmt.Errorf("读取API响应超时")
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		logger.Error("API请求返回错误", "status_code", resp.StatusCode, "response", string(responseBody))
		return "", fmt.Errorf("API请求返回错误: %s", string(responseBody))
	}

	// 记录原始响应内容
	logger.Debug("Deepseek API原始响应", "response_length", len(responseBody), "response_preview", string(responseBody))

	// 解析响应JSON
	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		logger.Error("解析API响应失败", "error", err, "response", string(responseBody))
		return "", fmt.Errorf("解析API响应失败: %w", err)
	}

	// 提取响应内容
	choices, ok := response["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		logger.Error("API响应格式错误", "response", string(responseBody))
		return "", fmt.Errorf("API响应格式错误")
	}

	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		logger.Error("API响应格式错误", "response", string(responseBody))
		return "", fmt.Errorf("API响应格式错误")
	}

	message, ok := firstChoice["message"].(map[string]interface{})
	if !ok {
		logger.Error("API响应格式错误", "response", string(responseBody))
		return "", fmt.Errorf("API响应格式错误")
	}

	content, ok := message["content"].(string)
	if !ok {
		logger.Error("API响应格式错误", "response", string(responseBody))
		return "", fmt.Errorf("API响应格式错误")
	}

	// 记录API使用情况统计
	if usage, hasUsage := response["usage"].(map[string]interface{}); hasUsage {
		logger.Info("Deepseek API使用统计",
			"prompt_tokens", usage["prompt_tokens"],
			"completion_tokens", usage["completion_tokens"],
			"total_tokens", usage["total_tokens"])
	}

	logger.Info("成功获取Deepseek API响应", "content_length", len(content), "duration_ms", requestDuration.Milliseconds())
	logger.Debug("API响应内容", "content", content)

	return content, nil
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
	categoryRegex := regexp.MustCompile(`(?i)(?:分类|category)[：:](\s*)(\w+)`)
	matches := categoryRegex.FindStringSubmatch(content)

	if len(matches) >= 3 {
		return strings.TrimSpace(matches[2])
	}

	// 根据内容关键词判断分类
	lowerContent := strings.ToLower(content)

	// 技术相关
	if strings.Contains(lowerContent, "技术") ||
		strings.Contains(lowerContent, "编程") ||
		strings.Contains(lowerContent, "开发") ||
		strings.Contains(lowerContent, "代码") {
		return "技术"
	}

	// 科技相关
	if strings.Contains(lowerContent, "科技") ||
		strings.Contains(lowerContent, "ai") ||
		strings.Contains(lowerContent, "人工智能") {
		return "科技"
	}

	// 商业相关
	if strings.Contains(lowerContent, "商业") ||
		strings.Contains(lowerContent, "经济") ||
		strings.Contains(lowerContent, "金融") ||
		strings.Contains(lowerContent, "市场") {
		return "商业"
	}

	return ""
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
