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
	// ArticleExists 检查文章是否已存在
	ArticleExists(link string) (bool, error)
	// GetArticleByLink 根据链接获取文章
	GetArticleByLink(link string) (*model.AnalysisResult, error)
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
