package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wolfitem/ai-rss/internal/application/service"
	"github.com/wolfitem/ai-rss/internal/domain/model"
	"github.com/wolfitem/ai-rss/internal/infrastructure/logger"
)

var (
	outputFile string
)

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:   "process",
	Short: "处理OPML文件并生成新闻报告",
	Long: `处理指定的OPML文件，提取RSS源并获取最近几天的文章，
使用Deepseek API进行内容分析和总结，最终生成Markdown格式的新闻报告。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 创建应用服务
		appService := service.NewRssProcessorService()

		// 获取RSS配置，设置默认值
		timeout := viper.GetInt("rss.timeout")
		if timeout <= 0 {
			timeout = 30 // 默认30秒
		}

		concurrency := viper.GetInt("rss.concurrency")
		if concurrency <= 0 {
			concurrency = 5 // 默认5个并发
		}

		// 获取完整的RSS配置
		maxRetries := viper.GetInt("rss.max_retries")
		if maxRetries <= 0 {
			maxRetries = 3 // 默认3次重试
		}

		responseTimeout := viper.GetInt("rss.response_timeout")
		if responseTimeout <= 0 {
			responseTimeout = 120 // 默认120秒
		}

		overallTimeout := viper.GetInt("rss.overall_timeout")
		if overallTimeout <= 0 {
			overallTimeout = 160 // 默认160秒
		}

		retryBackoffBase := viper.GetInt("rss.retry_backoff_base")
		if retryBackoffBase <= 0 {
			retryBackoffBase = 1 // 默认1秒
		}

		params := model.ProcessParams{
			OpmlFile:   viper.GetString("rss.opml_file"),
			OutputFile: outputFile,
			DaysBack:   viper.GetInt("rss.days_back"),
			DeepseekConfig: model.DeepseekConfig{
				APIKey:      viper.GetString("deepseek.api_key"),
				Model:       viper.GetString("deepseek.model"),
				MaxTokens:   viper.GetInt("deepseek.max_tokens"),
				MaxCalls:    viper.GetInt("deepseek.max_calls"),
				APIUrl:      viper.GetString("deepseek.api_url"),
				ReadTimeout: viper.GetInt("deepseek.read_timeout"),
				APITimeout:  viper.GetInt("deepseek.api_timeout"),
			},
			PromptTemplate: viper.GetString("analysis.prompt_template"),
			DatabaseConfig: model.DatabaseConfig{
				Enabled:  viper.GetBool("database.enabled"),
				FilePath: viper.GetString("database.file_path"),
			},
			RssConfig: model.RssConfig{
				Timeout:          timeout,
				Concurrency:      concurrency,
				MaxRetries:       maxRetries,
				ResponseTimeout:  responseTimeout,
				OverallTimeout:   overallTimeout,
				RetryBackoffBase: retryBackoffBase,
			},
		}

		// 处理OPML文件
		result, err := appService.ProcessRss(params)
		if err != nil {
			logger.Error("处理RSS失败", "error", err)
			return fmt.Errorf("处理RSS失败: %w", err)
		}
		logger.Info("RSS处理成功", "output_file", outputFile)

		// 输出结果
		if outputFile == "" {
			// 生成默认输出文件名，格式为 report-YYYY-MM-DD.md
			currentDate := time.Now().Format("2006-01-02")
			outputFile = fmt.Sprintf("report-%s.md", currentDate)
		}

		// 确保输出目录存在
		outputDir := filepath.Dir(outputFile)
		if outputDir != "." {
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("创建输出目录失败: %w", err)
			}
		}

		// 输出到文件
		err = os.WriteFile(outputFile, []byte(result), 0600)
		if err != nil {
			return fmt.Errorf("写入输出文件失败: %w", err)
		}
		fmt.Printf("报告已保存到: %s\n", outputFile)

		return nil
	},
}

// initProcessCmd 初始化处理命令
func initProcessCmd() {
	rootCmd.AddCommand(processCmd)

	// 本地标志
	processCmd.Flags().StringVarP(&outputFile, "output", "f", "", "输出文件路径（可选，默认为stdout）")
}
