package cmd

import (
	"log"

	"share/internal/network"
	"share/internal/server"

	"github.com/spf13/cobra"
)

var (
	rootDir string
	port    int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP file server",

	RunE: func(cmd *cobra.Command, args []string) error {

		ip, err := network.LocalIP()
		if err != nil {
			return err
		}

		log.Printf("Serving %s", rootDir)
		log.Printf("Address: http://%s:%d", ip, port)

		srv := server.New(rootDir, port)

		return srv.Start()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVarP(
		&rootDir,
		"dir",
		"d",
		".",
		"Directory to share",
	)

	serveCmd.Flags().IntVarP(
		&port,
		"port",
		"p",
		15016,
		"Server port",
	)
}