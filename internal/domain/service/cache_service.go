package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/wolfitem/ai-rss/internal/domain/model"
	"github.com/wolfitem/ai-rss/internal/infrastructure/database"
)

// CacheService 定义缓存服务接口
type CacheService interface {
	// GetCachedResult 获取缓存的分析结果
	GetCachedResult(content, configHash string) (*model.AnalysisResult, error)
	
	// SetCachedResult 设置缓存的分析结果
	SetCachedResult(content, configHash string, result *model.AnalysisResult, ttl time.Duration) error
	
	// GetContentHash 基于内容生成唯一哈希
	GetContentHash(content string) string
	
	// CleanExpiredItems 清理过期缓存
	CleanExpiredItems() error
	
	// GetCacheStats 获取缓存统计
	GetCacheStats() CacheStats
}

// CacheStats 缓存统计信息
type CacheStats struct {
	TotalItems    int64
	ExpiredItems  int64
	HitRate       float64
	CacheSize     int64
	LastCleanTime time.Time
}

// sqliteCacheService 基于SQLite的缓存实现
type sqliteCacheService struct {
	repo database.ArticleRepository
	
	// 缓存统计
	stats CacheStats
}

// NewCacheService 创建新的缓存服务
func NewCacheService(db database.Database) CacheService {
	return &sqliteCacheService{
		repo: database.NewSQLiteArticleRepository(db),
		stats: CacheStats{
			LastCleanTime: time.Now(),
		},
	}
}

// GetCachedResult 获取缓存的分析结果
func (c *sqliteCacheService) GetCachedResult(content, configHash string) (*model.AnalysisResult, error) {
	contentHash := c.GetContentHash(content + configHash)
	
	// 从数据库检查是否存在缓存
	article, err := c.repo.GetArticleByHash(contentHash)
	if err != nil {
		return nil, fmt.Errorf("获取缓存数据失败: %w", err)
	}
	
	if article != nil {
		// 检查缓存是否过期
		if article.PubDate != "" {
			pubDate, err := time.Parse("2006-01-02", article.PubDate)
			if err == nil && time.Since(pubDate) > 7*24*time.Hour {
				// 超过7天视为过期
				return nil, nil
			}
		}
		return article, nil
	}
	
	return nil, nil
}

// SetCachedResult 设置缓存的分析结果
func (c *sqliteCacheService) SetCachedResult(content, configHash string, result *model.AnalysisResult, ttl time.Duration) error {
	contentHash := c.GetContentHash(content + configHash)
	
	// 创建缓存项
	cacheResult := &model.AnalysisResult{
		Title:   result.Title,
		Summary: result.Summary,
		Source:  result.Source,
		PubDate: time.Now().Format("2006-01-02"),
		Category: result.Category,
		Link:     contentHash, // 用哈希值作为链接标识
	}
	
	// 保存到数据库
	return c.repo.SaveArticleToCache(cacheResult)
}

// GetContentHash 基于内容生成唯一哈希
func (c *sqliteCacheService) GetContentHash(content string) string {
	hasher := sha256.New()
	
	// 标准化内容：去除空白字符和HTML标签
	normalized := strings.TrimSpace(content)
	normalized = strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return ' '
		}
		return r
	}, normalized)
	
	// 取前1000字符进行哈希
	if len(normalized) > 1000 {
		normalized = normalized[:1000]
	}
	
	hasher.Write([]byte(normalized))
	return hex.EncodeToString(hasher.Sum(nil))
}

// CleanExpiredItems 清理过期缓存
func (c *sqliteCacheService) CleanExpiredItems() error {
	sevenDaysAgo := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	
	count, err := c.repo.DeleteExpiredArticles(sevenDaysAgo)
	if err != nil {
		return fmt.Errorf("清理过期缓存失败: %w", err)
	}
	
	c.stats.ExpiredItems = count
	c.stats.LastCleanTime = time.Now()
	
	return nil
}

// GetCacheStats 获取缓存统计
func (c *sqliteCacheService) GetCacheStats() CacheStats {
	count, err := c.repo.GetArticleCount()
	if err == nil {
		c.stats.TotalItems = count
	}
	return c.stats
}

// HasContentChanged 检测内容是否发生变化
func HasContentChanged(original string, newContent string) bool {
	original = strings.TrimSpace(original)
	newContent = strings.TrimSpace(newContent)
	
	if len(original) == 0 && len(newContent) == 0 {
		return false
	}
	
	if len(original) == 0 || len(newContent) == 0 {
		return true
	}
	
	// 如果长度差异超过20%，认为内容发生变化
	lenDiff := float64(len(original)-len(newContent)) / float64(len(original))
	if lenDiff > 0.2 || lenDiff < -0.2 {
		return true
	}
	
	// 检查是否包含新增的关键信息段落
	originalWords := strings.Fields(strings.ToLower(original))
	newWords := strings.Fields(strings.ToLower(newContent))
	
	// 简化的相似度检查（简化版Jaccard）
	common := 0
	originalSet := make(map[string]bool)
	for _, word := range originalWords {
		originalSet[word] = true
	}
	
	for _, word := range newWords {
		if originalSet[word] {
			common++
			delete(originalSet, word)
		}
	}
	
	// 计算相似度比例
	union := len(originalWords) + len(newWords) - common
	if union > 0 {
		similarity := float64(common) / float64(union)
		return similarity < 0.8 // 相似度低于80%认为有变化
	}
	
	return false
}