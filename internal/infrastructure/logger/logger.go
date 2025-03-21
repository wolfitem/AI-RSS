package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 全局日志实例
var log *zap.Logger

// 日志配置
type Config struct {
	// 日志级别: debug, info, warn, error, dpanic, panic, fatal
	Level string `mapstructure:"level"`
	// 是否输出到控制台
	Console bool `mapstructure:"console"`
	// 日志文件路径
	FilePath string `mapstructure:"file_path"`
	// 单个日志文件最大大小，单位MB
	MaxSize int `mapstructure:"max_size"`
	// 最多保留的旧日志文件数量
	MaxBackups int `mapstructure:"max_backups"`
	// 保留日志文件的最大天数
	MaxAge int `mapstructure:"max_age"`
	// 是否压缩旧日志文件
	Compress bool `mapstructure:"compress"`
}

// Init 初始化日志系统
func Init(config Config) error {
	// 设置默认值
	if config.Level == "" {
		config.Level = "info"
	}
	if config.FilePath == "" {
		config.FilePath = "logs/ai-rss.log"
	}
	if config.MaxSize == 0 {
		config.MaxSize = 100
	}
	if config.MaxBackups == 0 {
		config.MaxBackups = 3
	}
	if config.MaxAge == 0 {
		config.MaxAge = 28
	}

	// 确保日志目录存在
	logDir := filepath.Dir(config.FilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	// 解析日志级别
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		return fmt.Errorf("无效的日志级别 '%s': %w", config.Level, err)
	}

	// 配置编码器
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 配置日志输出
	var cores []zapcore.Core

	// 文件输出
	fileWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   config.FilePath,
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAge,
		Compress:   config.Compress,
	})
	fileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		fileWriter,
		level,
	)
	cores = append(cores, fileCore)

	// 控制台输出（可选）
	if config.Console {
		consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
		consoleCore := zapcore.NewCore(
			consoleEncoder,
			zapcore.AddSync(os.Stdout),
			level,
		)
		cores = append(cores, consoleCore)
	}

	// 创建日志实例
	core := zapcore.NewTee(cores...)
	log = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	// 记录初始化成功日志
	Info("日志系统初始化成功", "level", config.Level, "file", config.FilePath)

	return nil
}

// Sync 同步日志缓冲区到输出
func Sync() error {
	if log != nil {
		return log.Sync()
	}
	return nil
}

// Debug 记录调试级别日志
func Debug(msg string, keysAndValues ...interface{}) {
	if log != nil {
		sugar := log.Sugar()
		sugar.Debugw(msg, keysAndValues...)
	}
}

// Info 记录信息级别日志
func Info(msg string, keysAndValues ...interface{}) {
	if log != nil {
		sugar := log.Sugar()
		sugar.Infow(msg, keysAndValues...)
	}
}

// Warn 记录警告级别日志
func Warn(msg string, keysAndValues ...interface{}) {
	if log != nil {
		sugar := log.Sugar()
		sugar.Warnw(msg, keysAndValues...)
	}
}

// Error 记录错误级别日志
func Error(msg string, keysAndValues ...interface{}) {
	if log != nil {
		sugar := log.Sugar()
		sugar.Errorw(msg, keysAndValues...)
	}
}

// Fatal 记录致命错误日志并退出程序
func Fatal(msg string, keysAndValues ...interface{}) {
	if log != nil {
		sugar := log.Sugar()
		sugar.Fatalw(msg, keysAndValues...)
	}
}

// WithContext 创建带有上下文信息的日志记录器
func WithContext(ctx string) *ContextLogger {
	return &ContextLogger{context: ctx}
}

// ContextLogger 带有上下文信息的日志记录器
type ContextLogger struct {
	context string
}

// Debug 记录带上下文的调试级别日志
func (c *ContextLogger) Debug(msg string, keysAndValues ...interface{}) {
	if log != nil {
		kvs := append([]interface{}{"context", c.context}, keysAndValues...)
		Debug(msg, kvs...)
	}
}

// Info 记录带上下文的信息级别日志
func (c *ContextLogger) Info(msg string, keysAndValues ...interface{}) {
	if log != nil {
		kvs := append([]interface{}{"context", c.context}, keysAndValues...)
		Info(msg, kvs...)
	}
}

// Warn 记录带上下文的警告级别日志
func (c *ContextLogger) Warn(msg string, keysAndValues ...interface{}) {
	if log != nil {
		kvs := append([]interface{}{"context", c.context}, keysAndValues...)
		Warn(msg, kvs...)
	}
}

// Error 记录带上下文的错误级别日志
func (c *ContextLogger) Error(msg string, keysAndValues ...interface{}) {
	if log != nil {
		kvs := append([]interface{}{"context", c.context}, keysAndValues...)
		Error(msg, kvs...)
	}
}

// Fatal 记录带上下文的致命错误日志并退出程序
func (c *ContextLogger) Fatal(msg string, keysAndValues ...interface{}) {
	if log != nil {
		kvs := append([]interface{}{"context", c.context}, keysAndValues...)
		Fatal(msg, kvs...)
	}
}

// TimeTrack 记录函数执行时间
func TimeTrack(name string) func() {
	start := time.Now()
	return func() {
		Info("函数执行时间统计", "function", name, "duration", time.Since(start))
	}
}
