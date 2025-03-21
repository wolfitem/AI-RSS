package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

// Database 定义数据库接口
type Database interface {
	// Init 初始化数据库
	Init() error
	// Close 关闭数据库连接
	Close() error
	// Exec 执行SQL语句
	Exec(query string, args ...interface{}) (sql.Result, error)
	// Query 查询数据
	Query(query string, args ...interface{}) (*sql.Rows, error)
	// QueryRow 查询单行数据
	QueryRow(query string, args ...interface{}) *sql.Row
}

// SQLiteDatabase 实现Database接口的SQLite数据库
type SQLiteDatabase struct {
	db         *sql.DB
	dbFilePath string
}

// NewSQLiteDatabase 创建一个新的SQLite数据库实例
func NewSQLiteDatabase(dbFilePath string) Database {
	return &SQLiteDatabase{
		dbFilePath: dbFilePath,
	}
}

// Init 初始化SQLite数据库
func (s *SQLiteDatabase) Init() error {
	logger.Info("初始化SQLite数据库", "db_path", s.dbFilePath)

	// 确保数据库文件所在目录存在
	dbDir := filepath.Dir(s.dbFilePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("创建数据库目录失败", "error", err)
		return fmt.Errorf("创建数据库目录失败: %w", err)
	}

	// 打开数据库连接
	db, err := sql.Open("sqlite3", s.dbFilePath)
	if err != nil {
		logger.Error("打开数据库连接失败", "error", err)
		return fmt.Errorf("打开数据库连接失败: %w", err)
	}
	s.db = db

	// 测试数据库连接
	if err := db.Ping(); err != nil {
		logger.Error("数据库连接测试失败", "error", err)
		return fmt.Errorf("数据库连接测试失败: %w", err)
	}

	// 创建文章表
	if err := s.createTables(); err != nil {
		logger.Error("创建数据库表失败", "error", err)
		return fmt.Errorf("创建数据库表失败: %w", err)
	}

	logger.Info("SQLite数据库初始化成功")
	return nil
}

// createTables 创建必要的数据库表
func (s *SQLiteDatabase) createTables() error {
	// 创建文章表
	articleTableSQL := `
	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		summary TEXT NOT NULL,
		summary_length INTEGER DEFAULT 0,
		source TEXT NOT NULL,
		pub_date TEXT NOT NULL,
		category TEXT NOT NULL,
		link TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_articles_link ON articles(link);
	`

	_, err := s.db.Exec(articleTableSQL)
	if err != nil {
		logger.Error("创建文章表失败", "error", err)
		return fmt.Errorf("创建文章表失败: %w", err)
	}

	logger.Info("数据库表创建成功")
	return nil
}

// Close 关闭数据库连接
func (s *SQLiteDatabase) Close() error {
	if s.db != nil {
		logger.Info("关闭数据库连接")
		return s.db.Close()
	}
	return nil
}

// Exec 执行SQL语句
func (s *SQLiteDatabase) Exec(query string, args ...interface{}) (sql.Result, error) {
	return s.db.Exec(query, args...)
}

// Query 查询数据
func (s *SQLiteDatabase) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return s.db.Query(query, args...)
}

// QueryRow 查询单行数据
func (s *SQLiteDatabase) QueryRow(query string, args ...interface{}) *sql.Row {
	return s.db.QueryRow(query, args...)
}
