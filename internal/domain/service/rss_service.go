package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
			XMLUrl: outline.XMLURL,
		})
	}

	// 递归处理子outline
	for _, child := range outline.Outlines {
		sources = append(sources, extractSources(child)...)
	}

	return sources
}

// stripHTMLTags 去除HTML标签，只保留纯文本
func stripHTMLTags(html string) string {
	// 移除HTML标签
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	return doc.Text()
}

// sourceResult 表示单个RSS源的处理结果
type sourceResult struct {
	articles []model.Article
	err      error
	source   model.RssSource
}

// FetchArticles 从RSS源获取文章
func (s *rssService) FetchArticles(sources []model.RssSource, daysBack int, config model.RssConfig) ([]model.Article, error) {
	logger.Info("开始获取RSS文章", "sources_count", len(sources), "days_back", daysBack)
	defer logger.TimeTrack("FetchArticles")()

	// 设置配置，使用传入的配置或默认值
	config = s.setDefaultConfig(config)

	// 计算截止日期
	cutoffDate := time.Now().AddDate(0, 0, -daysBack)
	logger.Debug("设置文章截止日期", "cutoff_date", cutoffDate.Format("2006-01-02"))

	// 使用通道和goroutine并行处理RSS源
	resultChan := make(chan sourceResult, len(sources))
	// 限制并发数量，避免过多的并发请求
	semaphore := make(chan struct{}, config.Concurrency)

	// 启动goroutine处理每个RSS源
	for _, source := range sources {
		go s.processSingleSource(source, cutoffDate, config, semaphore, resultChan)
	}

	// 收集结果，带超时控制
	articles := s.collectResults(sources, resultChan)
	return articles, nil
}

// setDefaultConfig 设置默认配置值
func (s *rssService) setDefaultConfig(config model.RssConfig) model.RssConfig {
	if config.Timeout <= 0 {
		config.Timeout = 15 // 默认15秒
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 3 // 默认3个并发
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3 // 默认3次重试
	}
	if config.ResponseTimeout <= 0 {
		config.ResponseTimeout = 10 // 默认10秒
	}
	if config.OverallTimeout <= 0 {
		config.OverallTimeout = 60 // 默认60秒
	}
	if config.RetryBackoffBase <= 0 {
		config.RetryBackoffBase = 1 // 默认1秒
	}

	logger.Info("使用超时时间", "timeout_seconds", config.Timeout)
	logger.Info("使用并发数量", "concurrency", config.Concurrency)
	logger.Info("使用最大重试次数", "max_retries", config.MaxRetries)
	logger.Info("使用响应超时时间", "response_timeout_seconds", config.ResponseTimeout)
	logger.Info("使用整体超时时间", "overall_timeout_seconds", config.OverallTimeout)
	logger.Info("使用重试退避基数", "retry_backoff_base_seconds", config.RetryBackoffBase)

	return config
}

// processSingleSource 处理单个RSS源
func (s *rssService) processSingleSource(src model.RssSource, cutoffDate time.Time, config model.RssConfig, semaphore chan struct{}, resultChan chan<- sourceResult) {
	// 获取信号量
	semaphore <- struct{}{}
	// 函数结束时释放信号量
	defer func() { <-semaphore }()

	logger.Info("开始获取RSS源", "title", src.Title, "url", src.XMLUrl)

	// 添加更长的上下文超时控制，避免慢速服务器导致的超时
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.Timeout)*time.Second)
	defer cancel()

	// 创建带有上下文的请求
	req, err := http.NewRequestWithContext(ctx, "GET", src.XMLUrl, nil)
	if err != nil {
		logger.Error("创建请求失败", "title", src.Title, "url", src.XMLUrl, "error", err)
		resultChan <- sourceResult{nil, err, src}
		return
	}

	// 添加更智能的重试机制，使用指数退避策略
	var feed *gofeed.Feed
	var fetchErr error
	fp := gofeed.NewParser()
	fp.Client = &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	for retries := 0; retries < config.MaxRetries; retries++ {
		feed, fetchErr = s.fetchFeed(fp, req, src, retries, config)
		if fetchErr == nil {
			break
		}
	}

	if fetchErr != nil {
		logger.Error("无法解析RSS源，已达到最大重试次数",
			"title", src.Title,
			"url", src.XMLUrl,
			"error", fetchErr)
		resultChan <- sourceResult{nil, fetchErr, src}
		return
	}

	// 处理获取到的文章
	articles := s.processArticles(feed, src, cutoffDate)
	resultChan <- sourceResult{articles, nil, src}
}

// processArticles 处理获取到的文章
func (s *rssService) processArticles(feed *gofeed.Feed, src model.RssSource, cutoffDate time.Time) []model.Article {
	var articles []model.Article

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

			articles = append(articles, article)
		}
	} else {
		logger.Warn("RSS源没有文章或解析失败", "title", src.Title, "url", src.XMLUrl)
	}

	return articles
}

// collectResults 收集处理结果
func (s *rssService) collectResults(sources []model.RssSource, resultChan chan sourceResult) []model.Article {
	var articles []model.Article
	resultsCount := 0

	for resultsCount < len(sources) {
		result := <-resultChan
		resultsCount++

		if result.err != nil {
			logger.Error("处理RSS源失败", "title", result.source.Title, "error", result.err)
			continue
		}

		if result.articles != nil {
			articles = append(articles, result.articles...)
			logger.Info("添加文章到结果集", "source", result.source.Title, "articles_count", len(result.articles))
		}
	}

	logger.Info("所有RSS源处理完成", "total_articles", len(articles))
	return articles
}

// fetchFeed 获取并解析Feed
func (s *rssService) fetchFeed(fp *gofeed.Parser, req *http.Request, src model.RssSource, retries int, config model.RssConfig) (*gofeed.Feed, error) {
	logger.Debug("尝试解析RSS源", "title", src.Title, "url", src.XMLUrl, "attempt", retries+1)

	// 使用自定义的HTTP客户端发送请求
	resp, err := fp.Client.Do(req)
	if err != nil {
		logger.Warn("HTTP请求失败，准备重试",
			"title", src.Title,
			"url", src.XMLUrl,
			"attempt", retries+1,
			"error", err)
		if retries < config.MaxRetries-1 {
			backoffTime := time.Duration(config.RetryBackoffBase<<retries) * time.Second
			logger.Info("等待重试", "backoff_time_ms", backoffTime.Milliseconds())
			time.Sleep(backoffTime)
		}
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("HTTP响应为空")
	}
	defer resp.Body.Close()

	// 读取响应体内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	// 解析Feed，添加超时控制
	parseChan := make(chan struct{}, 1)
	var feed *gofeed.Feed
	var fetchErr error

	go func() {
		feed, fetchErr = fp.Parse(bytes.NewReader(body))
		parseChan <- struct{}{}
	}()

	// 等待解析完成或超时
	select {
	case <-parseChan:
		// 解析完成
		if fetchErr == nil {
			return feed, nil
		}
	case <-time.After(time.Duration(config.ResponseTimeout) * time.Second):
		// 解析超时
		fetchErr = fmt.Errorf("解析Feed超时")
		logger.Warn("解析Feed超时", "title", src.Title, "url", src.XMLUrl, "timeout_seconds", config.ResponseTimeout)
	}

	return nil, fetchErr
}
