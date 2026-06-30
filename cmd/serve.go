package cmd

import (
	"fmt"

	"share/internal/network"
	"share/internal/server"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	rootDir string
	port    int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP file server",
	Args:  cobra.NoArgs,

	RunE: func(cmd *cobra.Command, args []string) error {

		ip, err := network.LocalIP()
		if err != nil {
			return err
		}

		serverAddr := fmt.Sprintf("http://%s:%d", ip, port)

		isInteractive := !cmd.Flags().Changed("dir")

		logChan := make(chan string, 100)
		srv := server.New(rootDir, port)
		srv.LogChan = logChan

		// If dir is explicitly provided, start the server in background immediately.
		// Otherwise, the TUI picker will handle starting the server.
		if !isInteractive {
			go func() {
				if err := srv.Start(); err != nil {
					logChan <- fmt.Sprintf("Error starting server: %v", err)
				}
			}()
		}

		// Run TUI in the foreground (interactive picker or monitor)
		model := server.NewTuiModel(srv, serverAddr, isInteractive)
		p := tea.NewProgram(model)
		if _, err := p.Run(); err != nil {
			return err
		}

		return nil
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
