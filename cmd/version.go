package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wolfitem/ai-rss/internal/version"
)

// versionCmd 表示 version 命令
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示程序版本信息",
	Long:  `显示当前程序的版本号信息。`,
	Run: func(cmd *cobra.Command, args []string) {
		// 如果 Version 为空，则显示开发版本
		if version.Version == "" {
			version.Version = "开发版本"
		}
		fmt.Printf("AI-RSS 版本: %s\n", version.Version)
	},
}

// initVersionCmd 初始化版本命令
func initVersionCmd() {
	rootCmd.AddCommand(versionCmd)
}
