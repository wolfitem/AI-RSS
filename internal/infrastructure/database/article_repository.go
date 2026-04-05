package database

import (
	"fmt"

	"github.com/wolfitem/ai-rss/internal/domain/model"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

// ArticleRepository 定义文章存储库接口
type ArticleRepository interface {
	// SaveArticle 保存文章分析结果
	SaveArticle(article model.AnalysisResult) error
	// SaveArticleToCache 保存文章缓存
	SaveArticleToCache(article *model.AnalysisResult) error
	// ArticleExists 检查文章是否已存在
	ArticleExists(link string) (bool, error)
	// GetArticleByLink 根据链接获取文章
	GetArticleByLink(link string) (*model.AnalysisResult, error)
	// GetArticleByHash 根据内容哈希获取文章
	GetArticleByHash(hash string) (*model.AnalysisResult, error)
	// GetArticleCount 获取文章总数
	GetArticleCount() (int64, error)
	// DeleteExpiredArticles 删除过期文章
	DeleteExpiredArticles(before string) (int64, error)
}

// SQLiteArticleRepository 实现ArticleRepository接口的SQLite存储库
type SQLiteArticleRepository struct {
	db Database
}

// NewSQLiteArticleRepository 创建一个新的SQLite文章存储库
func NewSQLiteArticleRepository(db Database) ArticleRepository {
	return &SQLiteArticleRepository{
		db: db,
	}
}

// SaveArticle 保存文章分析结果到数据库
func (r *SQLiteArticleRepository) SaveArticle(article model.AnalysisResult) error {
	logger.Info("保存文章到数据库", "title", article.Title, "link", article.Link)

	// 检查文章是否已存在
	exists, err := r.ArticleExists(article.Link)
	if err != nil {
		logger.Error("检查文章是否存在失败", "error", err)
		return fmt.Errorf("检查文章是否存在失败: %w", err)
	}

	// 如果文章已存在，则不再保存
	if exists {
		logger.Info("文章已存在，跳过保存", "link", article.Link)
		return nil
	}

	// 计算摘要长度
	article.SummaryLength = len([]rune(article.Summary))

	// 插入文章记录
	query := `
	INSERT INTO articles (title, summary, summary_length, source, pub_date, category, link)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.Exec(query, article.Title, article.Summary, article.SummaryLength, article.Source, article.PubDate, article.Category, article.Link)
	if err != nil {
		logger.Error("保存文章失败", "error", err)
		return fmt.Errorf("保存文章失败: %w", err)
	}

	logger.Info("文章保存成功", "title", article.Title)
	return nil
}

// ArticleExists 检查文章是否已存在于数据库中
func (r *SQLiteArticleRepository) ArticleExists(link string) (bool, error) {
	logger.Debug("检查文章是否存在", "link", link)

	query := "SELECT COUNT(*) FROM articles WHERE link = ?"
	var count int
	err := r.db.QueryRow(query, link).Scan(&count)
	if err != nil {
		logger.Error("查询文章失败", "error", err)
		return false, fmt.Errorf("查询文章失败: %w", err)
	}

	exists := count > 0
	logger.Debug("文章存在检查结果", "link", link, "exists", exists)
	return exists, nil
}

// GetArticleByLink 根据链接获取文章
func (r *SQLiteArticleRepository) GetArticleByLink(link string) (*model.AnalysisResult, error) {
	logger.Debug("根据链接获取文章", "link", link)

	query := "SELECT title, summary, summary_length, source, pub_date, category, link FROM articles WHERE link = ?"
	row := r.db.QueryRow(query, link)

	var article model.AnalysisResult
	err := row.Scan(&article.Title, &article.Summary, &article.SummaryLength, &article.Source, &article.PubDate, &article.Category, &article.Link)
	if err != nil {
		logger.Error("获取文章失败", "error", err)
		return nil, fmt.Errorf("获取文章失败: %w", err)
	}

	return &article, nil
}

// GetArticleByHash 根据内容哈希获取缓存的文章
func (r *SQLiteArticleRepository) GetArticleByHash(hash string) (*model.AnalysisResult, error) {
	query := "SELECT title, summary, summary_length, source, pub_date, category, link FROM articles WHERE link = ? AND category = 'cache'"
	row := r.db.QueryRow(query, hash)

	var article model.AnalysisResult
	err := row.Scan(&article.Title, &article.Summary, &article.SummaryLength, &article.Source, &article.PubDate, &article.Category, &article.Link)
	if err != nil {
		logger.Debug("未找到缓存文章", "hash", hash)
		return nil, nil // 未找到缓存，返回nil而不是错误
	}

	logger.Debug("找到缓存文章", "hash", hash)
	return &article, nil
}

// SaveArticleToCache 保存文章到缓存
func (r *SQLiteArticleRepository) SaveArticleToCache(article *model.AnalysisResult) error {
	logger.Debug("保存缓存文章", "hash", article.Link, "title", article.Title)

	article.SummaryLength = len([]rune(article.Summary))

	query := `
	INSERT OR REPLACE INTO articles (title, summary, summary_length, source, pub_date, category, link) 
	VALUES (?, ?, ?, ?, ?, 'cache', ?)
	`
	
	_, err := r.db.Exec(query, article.Title, article.Summary, article.SummaryLength, article.Source, article.PubDate, article.Link)
	if err != nil {
		logger.Error("保存缓存文章失败", "error", err)
		return fmt.Errorf("保存缓存文章失败: %w", err)
	}

	return nil
}

// GetArticleCount 获取文章总数
func (r *SQLiteArticleRepository) GetArticleCount() (int64, error) {
	query := "SELECT COUNT(*) FROM articles"
	var count int64
	err := r.db.QueryRow(query).Scan(&count)
	if err != nil {
		logger.Error("获取文章数量失败", "error", err)
		return 0, fmt.Errorf("获取文章数量失败: %w", err)
	}
	return count, nil
}

// DeleteExpiredArticles 删除过期文章
func (r *SQLiteArticleRepository) DeleteExpiredArticles(before string) (int64, error) {
	logger.Info("删除过期文章", "before_date", before)

	query := "DELETE FROM articles WHERE pub_date <= ? AND category = 'cache'"
	result, err := r.db.Exec(query, before)
	if err != nil {
		logger.Error("删除过期文章失败", "error", err)
		return 0, fmt.Errorf("删除过期文章失败: %w", err)
	}

	affected, _ := result.RowsAffected()
	logger.Info("删除过期文章完成", "count", affected)
	return affected, nil
}
