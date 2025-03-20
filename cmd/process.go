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

		params := model.ProcessParams{
			OpmlFile:   viper.GetString("rss.opml_file"),
			OutputFile: outputFile,
			DaysBack:   viper.GetInt("rss.days_back"),
			DeepseekConfig: model.DeepseekConfig{
				APIKey:    viper.GetString("deepseek.api_key"),
				Model:     viper.GetString("deepseek.model"),
				MaxTokens: viper.GetInt("deepseek.max_tokens"),
				MaxCalls:  viper.GetInt("deepseek.max_calls"),
				APIUrl:    viper.GetString("deepseek.api_url"),
			},
			PromptTemplate: viper.GetString("analysis.prompt_template"),
			DatabaseConfig: model.DatabaseConfig{
				Enabled:  viper.GetBool("database.enabled"),
				FilePath: viper.GetString("database.file_path"),
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
		err = os.WriteFile(outputFile, []byte(result), 0644)
		if err != nil {
			return fmt.Errorf("写入输出文件失败: %w", err)
		}
		fmt.Printf("报告已保存到: %s\n", outputFile)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(processCmd)

	// 本地标志
	processCmd.Flags().StringVarP(&outputFile, "output", "f", "", "输出文件路径（可选，默认为stdout）")

}
