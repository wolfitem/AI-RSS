package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version 变量将在编译时通过 -ldflags 注入
var Version string

// versionCmd 表示 version 命令
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示程序版本信息",
	Long:  `显示当前程序的版本号信息。`,
	Run: func(cmd *cobra.Command, args []string) {
		// 如果 Version 为空，则显示开发版本
		if Version == "" {
			Version = "开发版本"
		}
		fmt.Printf("AI-RSS 版本: %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
