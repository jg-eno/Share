package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "share",
	Short: "Secure file sharing for your personal devices",
	Long:  "Share is a simple HTTP-based file sharing tool.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}