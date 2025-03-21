package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gilliek/go-opml/opml"
	"github.com/mmcdole/gofeed"
	"github.com/wolfitem/ai-rss/internal/domain/model"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

// RssService 定义RSS处理的领域服务接口
type RssService interface {
	// ParseOpml 解析OPML文件并返回RSS源列表
	ParseOpml(opmlFilePath string) ([]model.RssSource, error)

	// FetchArticles 从RSS源获取文章
	FetchArticles(sources []model.RssSource, daysBack int, config model.RssConfig) ([]model.Article, error)
}

// rssService 实现RssService接口
type rssService struct{}

// NewRssService 创建一个新的RSS服务实例
func NewRssService() RssService {
	return &rssService{}
}

// ParseOpml 解析OPML文件并返回RSS源列表
func (s *rssService) ParseOpml(opmlFilePath string) ([]model.RssSource, error) {
	logger.Info("开始解析OPML文件", "file", opmlFilePath)
	defer logger.TimeTrack("ParseOpml")()

	// 解析OPML文件
	doc, err := opml.NewOPMLFromFile(opmlFilePath)
	if err != nil {
		logger.Error("解析OPML文件失败", "file", opmlFilePath, "error", err)
		return nil, fmt.Errorf("解析OPML文件失败: %w", err)
	}

	// 提取RSS源
	var sources []model.RssSource
	for _, outline := range doc.Outlines() {
		// 递归处理所有outline
		sources = append(sources, extractSources(outline)...)
	}

	logger.Info("OPML文件解析完成", "file", opmlFilePath, "sources_count", len(sources))
	return sources, nil
}

// extractSources 递归提取outline中的RSS源
func extractSources(outline opml.Outline) []model.RssSource {
	var sources []model.RssSource

	// 如果当前outline有xmlUrl属性，则它是一个RSS源
	if outline.XMLURL != "" {
		sources = append(sources, model.RssSource{
			Title:  outline.Title,
			XmlUrl: outline.XMLURL,
		})
	}

	// 递归处理子outline
	for _, child := range outline.Outlines {
		sources = append(sources, extractSources(child)...)
	}

	return sources
}

// 截断字符串，用于日志输出预览内容
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripHTMLTags 去除HTML标签，只保留纯文本
func stripHTMLTags(html string) string {
	// 如果内容为空，直接返回
	if html == "" {
		return ""
	}

	// 使用goquery解析HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		logger.Warn("解析HTML失败，返回原始内容", "error", err)
		return html
	}

	// 获取文本内容，去除HTML标签
	text := doc.Text()

	// 清理文本（去除多余的空白字符）
	text = strings.TrimSpace(text)
	// 将连续的空白字符替换为单个空格
	text = strings.Join(strings.Fields(text), " ")

	return text
}

// FetchArticles 从RSS源获取文章
func (s *rssService) FetchArticles(sources []model.RssSource, daysBack int, config model.RssConfig) ([]model.Article, error) {
	logger.Info("开始获取RSS文章", "sources_count", len(sources), "days_back", daysBack)
	defer logger.TimeTrack("FetchArticles")()

	// 设置配置，使用传入的配置或默认值
	timeout := 15
	concurrency := 3
	maxRetries := 3
	responseTimeout := 10
	overallTimeout := 60
	retryBackoffBase := 1

	// 使用传入的配置值（如果有）
	if config.Timeout > 0 {
		timeout = config.Timeout
	}
	if config.Concurrency > 0 {
		concurrency = config.Concurrency
	}
	if config.MaxRetries > 0 {
		maxRetries = config.MaxRetries
	}
	if config.ResponseTimeout > 0 {
		responseTimeout = config.ResponseTimeout
	}
	if config.OverallTimeout > 0 {
		overallTimeout = config.OverallTimeout
	}
	if config.RetryBackoffBase > 0 {
		retryBackoffBase = config.RetryBackoffBase
	}

	// 记录使用的配置值
	logger.Info("使用超时时间", "timeout_seconds", timeout)
	logger.Info("使用并发数量", "concurrency", concurrency)
	logger.Info("使用最大重试次数", "max_retries", maxRetries)
	logger.Info("使用响应超时时间", "response_timeout_seconds", responseTimeout)
	logger.Info("使用整体超时时间", "overall_timeout_seconds", overallTimeout)
	logger.Info("使用重试退避基数", "retry_backoff_base_seconds", retryBackoffBase)

	var articles []model.Article
	fp := gofeed.NewParser()

	// 设置超时时间
	fp.Client = &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// 计算截止日期
	cutoffDate := time.Now().AddDate(0, 0, -daysBack)
	logger.Debug("设置文章截止日期", "cutoff_date", cutoffDate.Format("2006-01-02"))

	// 使用通道和goroutine并行处理RSS源
	type sourceResult struct {
		articles []model.Article
		err      error
		source   model.RssSource
	}

	resultChan := make(chan sourceResult, len(sources))
	// 限制并发数量，避免过多的并发请求
	semaphore := make(chan struct{}, concurrency) // 使用配置的并发数量

	// 启动goroutine处理每个RSS源
	for _, source := range sources {
		go func(src model.RssSource) {
			// 获取信号量
			semaphore <- struct{}{}
			// 函数结束时释放信号量
			defer func() { <-semaphore }()

			logger.Info("开始获取RSS源", "title", src.Title, "url", src.XmlUrl)

			// 添加更长的上下文超时控制，避免慢速服务器导致的超时
			// 使用配置的超时时间
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			// 创建带有上下文的请求
			req, err := http.NewRequestWithContext(ctx, "GET", src.XmlUrl, nil)
			if err != nil {
				logger.Error("创建请求失败", "title", src.Title, "url", src.XmlUrl, "error", err)
				resultChan <- sourceResult{nil, err, src}
				return
			}

			// 添加更智能的重试机制，使用指数退避策略
			var feed *gofeed.Feed
			var fetchErr error
			// 使用配置的最大重试次数
			for retries := 0; retries < maxRetries; retries++ {
				logger.Debug("尝试解析RSS源", "title", src.Title, "url", src.XmlUrl, "attempt", retries+1)

				// 使用自定义的HTTP客户端发送请求
				resp, err := fp.Client.Do(req)
				if err != nil {
					fetchErr = err
					logger.Warn("HTTP请求失败，准备重试",
						"title", src.Title,
						"url", src.XmlUrl,
						"attempt", retries+1,
						"error", err)
					if retries < maxRetries-1 {
						// 使用指数退避策略，每次重试等待时间翻倍
						backoffTime := time.Duration(retryBackoffBase<<retries) * time.Second
						logger.Info("等待重试", "backoff_time_ms", backoffTime.Milliseconds())
						time.Sleep(backoffTime)
					}
					continue
				}

				// 确保响应体被关闭
				if resp != nil {
					// 使用defer和匿名函数确保响应体被正确关闭
					respBody := resp.Body
					defer func() {
						if err := respBody.Close(); err != nil {
							logger.Warn("关闭响应体失败", "error", err)
						}
					}()

					// 解析Feed，添加超时控制
					parseChan := make(chan struct{}, 1)
					go func() {
						feed, fetchErr = fp.Parse(respBody)
						parseChan <- struct{}{}
					}()

					// 等待解析完成或超时
					select {
					case <-parseChan:
						// 解析完成
						if fetchErr == nil {
							break // 解析成功，跳出重试循环
						}
					case <-time.After(time.Duration(responseTimeout) * time.Second):
						// 解析超时
						fetchErr = fmt.Errorf("解析Feed超时")
						logger.Warn("解析Feed超时", "title", src.Title, "url", src.XmlUrl, "timeout_seconds", responseTimeout)
					}

					if fetchErr == nil {
						break // 解析成功，跳出重试循环
					}

					logger.Warn("解析Feed失败，准备重试",
						"title", src.Title,
						"url", src.XmlUrl,
						"attempt", retries+1,
						"error", fetchErr)
				}

				if retries < maxRetries-1 && fetchErr != nil {
					// 使用指数退避策略，每次重试等待时间翻倍
					backoffTime := time.Duration(1<<retries) * time.Second
					logger.Info("等待重试", "backoff_time_ms", backoffTime.Milliseconds())
					time.Sleep(backoffTime)
				}
			}

			if fetchErr != nil {
				// 所有重试都失败，记录错误并返回
				logger.Error("无法解析RSS源，已达到最大重试次数",
					"title", src.Title,
					"url", src.XmlUrl,
					"error", fetchErr)
				resultChan <- sourceResult{nil, fetchErr, src}
				return
			}

			// 处理获取到的文章
			var sourceArticles []model.Article
			if feed != nil && len(feed.Items) > 0 {
				logger.Info("成功获取RSS源", "title", src.Title, "articles_count", len(feed.Items))

				// 处理每篇文章
				for _, item := range feed.Items {
					// 如果没有发布日期，尝试使用更新日期或当前日期
					publishDate := time.Now()
					if item.PublishedParsed != nil {
						publishDate = *item.PublishedParsed
					} else if item.UpdatedParsed != nil {
						publishDate = *item.UpdatedParsed
					}

					// 检查文章是否在指定日期范围内
					if publishDate.Before(cutoffDate) {
						continue
					}

					// 获取文章内容
					content := ""
					if item.Content != "" {
						content = item.Content
					} else if item.Description != "" {
						content = item.Description
					}

					// 去除HTML标签
					content = stripHTMLTags(content)

					// 创建文章对象
					article := model.Article{
						Title:       item.Title,
						Content:     content,
						Link:        item.Link,
						PublishDate: publishDate.Format("2006-01-02"),
						Source:      src,
					}

					sourceArticles = append(sourceArticles, article)
				}
			} else {
				logger.Warn("RSS源没有文章或解析失败", "title", src.Title, "url", src.XmlUrl)
			}

			// 发送结果
			resultChan <- sourceResult{sourceArticles, nil, src}
		}(source)
	}

	// 设置超时控制，使用配置的整体超时时间
	timeoutChan := time.After(time.Duration(overallTimeout) * time.Second)

	// 收集结果
	resultsCount := 0
	for resultsCount < len(sources) {
		select {
		case result := <-resultChan:
			resultsCount++

			if result.err != nil {
				logger.Error("处理RSS源失败", "title", result.source.Title, "error", result.err)
				continue
			}

			if result.articles != nil {
				articles = append(articles, result.articles...)
				logger.Info("添加文章到结果集", "source", result.source.Title, "articles_count", len(result.articles))
			}

		case <-timeoutChan:
			logger.Error("获取RSS源超时，已处理的源数量", "processed", resultsCount, "total", len(sources))
			return articles, fmt.Errorf("获取RSS源超时，已处理%d/%d个源", resultsCount, len(sources))
		}
	}

	logger.Info("所有RSS源处理完成", "total_articles", len(articles))
	return articles, nil
}
