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
	FetchArticles(sources []model.RssSource, daysBack int) ([]model.Article, error)
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
func (s *rssService) FetchArticles(sources []model.RssSource, daysBack int) ([]model.Article, error) {
	logger.Info("开始获取RSS文章", "sources_count", len(sources), "days_back", daysBack)
	defer logger.TimeTrack("FetchArticles")()

	var articles []model.Article
	fp := gofeed.NewParser()

	// 设置更短的超时时间，避免长时间等待
	fp.Client = &http.Client{
		Timeout: 15 * time.Second, // 从30秒减少到15秒
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
	semaphore := make(chan struct{}, 3) // 最多3个并发请求

	// 启动goroutine处理每个RSS源
	for _, source := range sources {
		go func(src model.RssSource) {
			// 获取信号量
			semaphore <- struct{}{}
			// 函数结束时释放信号量
			defer func() { <-semaphore }()

			logger.Info("开始获取RSS源", "title", src.Title, "url", src.XmlUrl)

			// 添加上下文超时控制
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			// 创建带有上下文的请求
			req, err := http.NewRequestWithContext(ctx, "GET", src.XmlUrl, nil)
			if err != nil {
				logger.Error("创建请求失败", "title", src.Title, "url", src.XmlUrl, "error", err)
				resultChan <- sourceResult{nil, err, src}
				return
			}

			// 添加重试机制
			var feed *gofeed.Feed
			var fetchErr error
			for retries := 0; retries < 3; retries++ {
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
					if retries < 2 {
						time.Sleep(2 * time.Second)
					}
					continue
				}

				// 确保响应体被关闭
				if resp != nil {
					defer resp.Body.Close()

					// 解析Feed
					feed, fetchErr = fp.Parse(resp.Body)
					if fetchErr == nil {
						break // 解析成功，跳出重试循环
					}

					logger.Warn("解析Feed失败，准备重试",
						"title", src.Title,
						"url", src.XmlUrl,
						"attempt", retries+1,
						"error", fetchErr)
				}

				if retries < 2 {
					time.Sleep(2 * time.Second)
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

	// 设置超时控制
	timeout := time.After(60 * time.Second)

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

		case <-timeout:
			logger.Error("获取RSS源超时，已处理的源数量", "processed", resultsCount, "total", len(sources))
			return articles, fmt.Errorf("获取RSS源超时，已处理%d/%d个源", resultsCount, len(sources))
		}
	}

	logger.Info("所有RSS源处理完成", "total_articles", len(articles))
	return articles, nil
}
