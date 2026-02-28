package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "vault",
	Short: "Vault - Unraid Backup & Restore",
}

func Execute() error {
	return rootCmd.Execute()
}
