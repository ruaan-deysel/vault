package cli

import (
	"log"

	"github.com/ruaandeysel/vault/internal/api"
	"github.com/ruaandeysel/vault/internal/db"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the Vault daemon (API server + scheduler)",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath, _ := cmd.Flags().GetString("db")
		addr, _ := cmd.Flags().GetString("addr")

		database, err := db.Open(dbPath)
		if err != nil {
			return err
		}
		defer database.Close()

		log.Println("Starting Vault daemon...")
		srv := api.NewServer(database, addr)
		return srv.Start()
	},
}

func init() {
	daemonCmd.Flags().String("db", "/boot/config/plugins/vault/vault.db", "Database path")
	daemonCmd.Flags().String("addr", ":28085", "API listen address")
	rootCmd.AddCommand(daemonCmd)
}
