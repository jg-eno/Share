package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"share/internal/server"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var devicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List all registered devices",
	Long:  `Display all devices that have been registered with this Share server, along with their approval status and registration date.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		devices, err := server.LoadDevices()
		if err != nil {
			return fmt.Errorf("failed to load devices: %w", err)
		}

		if len(devices) == 0 {
			fmt.Println("No devices registered yet.")
			fmt.Println("Run `share serve` and connect a device to get started.")
			return nil
		}

		// Sort by name
		keys := make([]string, 0, len(devices))
		for k := range devices {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return devices[keys[i]].Name < devices[keys[j]].Name
		})

		// ── Styles ────────────────────────────────────────────────────────
		headerStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#51b6f5")).
			Padding(0, 2)

		colHeaderStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9ca3af"))

		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f9fafb")).
			Bold(true)

		approvedStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10b981")).
			Bold(true)

		pendingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f59e0b")).
			Bold(true)

		rejectedStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444")).
			Bold(true)

		dimStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280"))

		modeStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280"))

		dividerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1f2937"))

		// ── Count by status ───────────────────────────────────────────────
		counts := map[string]int{"approved": 0, "pending": 0, "rejected": 0}
		for _, d := range devices {
			counts[d.Status]++
		}

		fmt.Println()
		fmt.Println(headerStyle.Render(" SHARE — Registered Devices "))
		fmt.Println()

		// Column widths
		const (
			colName   = 24
			colStatus = 12
			colMode   = 10
			colDate   = 12
		)

		// Header row
		fmt.Printf("  %s  %s  %s  %s\n",
			colHeaderStyle.Render(padRight("NAME", colName)),
			colHeaderStyle.Render(padRight("STATUS", colStatus)),
			colHeaderStyle.Render(padRight("MODE", colMode)),
			colHeaderStyle.Render(padRight("REGISTERED", colDate)),
		)
		fmt.Println("  " + dividerStyle.Render(strings.Repeat("─", colName+colStatus+colMode+colDate+6)))

		// Device rows
		for _, k := range keys {
			d := devices[k]

			// Name (truncate if too long)
			name := d.Name
			if len(name) > colName {
				name = name[:colName-1] + "…"
			}

			// Status badge
			var statusStr string
			switch d.Status {
			case "approved":
				statusStr = approvedStyle.Render(padRight("✓ approved", colStatus))
			case "pending":
				statusStr = pendingStyle.Render(padRight("⏳ pending", colStatus))
			default:
				statusStr = rejectedStyle.Render(padRight("✗ "+d.Status, colStatus))
			}

			// Auth mode — simple device IDs start with "simple-"
			mode := "ECDSA"
			if strings.HasPrefix(d.PublicKey, "simple-") {
				mode = "simple"
			}
			modeStr := modeStyle.Render(padRight(mode, colMode))

			// Registration date
			date := d.CreatedAt.Format("2006-01-02")
			if d.CreatedAt.IsZero() {
				date = "—"
			}
			dateStr := dimStyle.Render(padRight(date, colDate))

			fmt.Printf("  %s  %s  %s  %s\n",
				nameStyle.Render(padRight(name, colName)),
				statusStr,
				modeStr,
				dateStr,
			)
		}

		// ── Summary footer ─────────────────────────────────────────────────
		fmt.Println()
		parts := []string{}
		if counts["approved"] > 0 {
			parts = append(parts, approvedStyle.Render(fmt.Sprintf("%d approved", counts["approved"])))
		}
		if counts["pending"] > 0 {
			parts = append(parts, pendingStyle.Render(fmt.Sprintf("%d pending", counts["pending"])))
		}
		if counts["rejected"] > 0 {
			parts = append(parts, rejectedStyle.Render(fmt.Sprintf("%d rejected", counts["rejected"])))
		}
		fmt.Printf("  %d device(s) total  —  %s\n\n", len(devices), strings.Join(parts, "  "))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(devicesCmd)
}

// padRight pads or truncates s to exactly width runes.
func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

// Ensure time is referenced (used via Device.CreatedAt)
var _ = time.Now
