package service

import (
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

	// 设置超时时间，避免长时间等待
	fp.Client = &http.Client{
		Timeout: 30 * time.Second,
	}

	// 计算截止日期
	cutoffDate := time.Now().AddDate(0, 0, -daysBack)
	logger.Debug("设置文章截止日期", "cutoff_date", cutoffDate.Format("2006-01-02"))

	// 处理每个RSS源
	for _, source := range sources {
		logger.Info("开始获取RSS源", "title", source.Title, "url", source.XmlUrl)

		// 添加重试机制
		var feed *gofeed.Feed
		var err error
		for retries := 0; retries < 3; retries++ {
			logger.Debug("尝试解析RSS源", "title", source.Title, "url", source.XmlUrl, "attempt", retries+1)
			feed, err = fp.ParseURL(source.XmlUrl)
			if err == nil {
				break
			}

			logger.Warn("无法解析RSS源，准备重试",
				"title", source.Title,
				"url", source.XmlUrl,
				"attempt", retries+1,
				"error", err)

			if retries < 2 {
				// 等待一段时间后重试
				time.Sleep(2 * time.Second)
			}
		}

		if err != nil {
			// 所有重试都失败，记录错误并继续处理其他源
			logger.Error("无法解析RSS源，已达到最大重试次数",
				"title", source.Title,
				"url", source.XmlUrl,
				"error", err)
			continue
		}

		// 记录获取到的文章数量和Feed结构信息
		logger.Info("成功获取RSS源", "title", source.Title, "articles_count", len(feed.Items))

		// 输出Feed数据结构信息
		logger.Debug("Feed数据结构",
			"feed_title", feed.Title,
			"feed_description", feed.Description,
			"feed_link", feed.Link,
			"feed_feedType", feed.FeedType,
			"feed_feedVersion", feed.FeedVersion)

		// 检查并输出第一篇文章的结构（如果存在）
		if len(feed.Items) > 0 {
			firstItem := feed.Items[0]
			logger.Debug("Feed.Item数据结构示例",
				"item_title", firstItem.Title,
				"has_content", firstItem.Content != "",
				"content_length", len(firstItem.Content),
				"has_description", firstItem.Description != "",
				"description_length", len(firstItem.Description),
				"has_link", firstItem.Link != "")

			// 特别检查是否包含文章内容
			if firstItem.Content != "" {
				logger.Info("文章包含Content字段", "title", firstItem.Title, "content_preview", truncateString(firstItem.Content, 100))
			} else if firstItem.Description != "" {
				logger.Info("文章包含Description字段", "title", firstItem.Title, "description_preview", truncateString(firstItem.Description, 100))
			} else {
				logger.Warn("文章不包含内容字段", "title", firstItem.Title)
			}
		}

		// 处理每篇文章
		validArticles := 0
		for _, item := range feed.Items {
			// 如果没有发布日期，尝试使用更新日期或当前日期
			publishDate := time.Now()
			if item.PublishedParsed != nil {
				publishDate = *item.PublishedParsed
			} else if item.UpdatedParsed != nil {
				publishDate = *item.UpdatedParsed
			}

			// 检查文章是否在指定日期范围内
			if publishDate.After(cutoffDate) {
				validArticles++
				logger.Debug("处理文章", "title", item.Title, "date", publishDate.Format("2006-01-02"))

				// 提取文章内容，优先使用Content，其次是Description
				content := ""
				if item.Content != "" {
					content = item.Content
				} else if item.Description != "" {
					content = item.Description
				}

				// 如果内容为空，记录警告
				if content == "" {
					logger.Warn("文章没有内容", "title", item.Title, "source", source.Title)
					// 使用标题作为内容的备选方案
					content = "内容不可用: " + item.Title
				}

				// 过滤HTML标签，只保留纯文本
				plainContent := stripHTMLTags(content)

				// 创建文章对象
				article := model.Article{
					Title:       item.Title,
					Content:     plainContent,
					Link:        item.Link,
					PublishDate: publishDate.Format("2006-01-02"),
					Source:      source,
				}

				articles = append(articles, article)
			}
		}
		logger.Info("RSS源处理完成", "title", source.Title, "valid_articles", validArticles)
	}

	logger.Info("RSS文章获取完成", "total_articles", len(articles))
	return articles, nil
}
