package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ai-rss",
	Short: "RSS新闻聚合与分析工具",
	Long: `AI-RSS是一个基于Go语言的控制台程序，用于获取指定OPML文件中订阅的RSS源，
拉取每个RSS地址的文章内容，并使用Deepseek API对文章内容进行提炼总结，
最终输出每日新闻报告。`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	// 设置信号处理
	setupSignalHandler()

	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// 程序退出前同步日志
	defer logger.Sync()
}

func init() {
	cobra.OnInitialize(initConfig)

	// 全局标志
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径 (默认为 ./config.yaml)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// 使用指定的配置文件
		viper.SetConfigFile(cfgFile)
	} else {
		// 在当前目录中查找配置文件
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	// 读取配置文件
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("使用配置文件:", viper.ConfigFileUsed())

		// 初始化日志系统
		initLogger()
	} else {
		fmt.Printf("无法读取配置文件: %v\n", err)
	}

	// 读取环境变量
	viper.AutomaticEnv()
}

// initLogger 初始化日志系统
func initLogger() {
	// 从配置文件中读取日志配置
	logConfig := logger.Config{
		Level:      viper.GetString("logger.level"),
		Console:    viper.GetBool("logger.console"),
		FilePath:   viper.GetString("logger.file_path"),
		MaxSize:    viper.GetInt("logger.max_size"),
		MaxBackups: viper.GetInt("logger.max_backups"),
		MaxAge:     viper.GetInt("logger.max_age"),
		Compress:   viper.GetBool("logger.compress"),
	}

	// 初始化日志系统
	if err := logger.Init(logConfig); err != nil {
		fmt.Printf("初始化日志系统失败: %v\n", err)
	}
}

// setupSignalHandler 设置信号处理函数
func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	// 监听 SIGINT (Ctrl+C) 和 SIGTERM 信号
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\n接收到中断信号，正在优雅退出...")
		// 执行清理工作
		logger.Info("程序接收到中断信号，正在清理资源")
		// 同步日志
		logger.Sync()
		// 退出程序
		os.Exit(0)
	}()
}
