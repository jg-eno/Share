package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"share/internal/server"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// ── Styles ───────────────────────────────────────────────────────────────────

var (
	revokeHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#51b6f5")).
				Padding(0, 4).
				Align(lipgloss.Center)

	revokeBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")).
			Padding(1, 2)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ef4444"))

	checkedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444"))

	uncheckedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ca3af"))

	nameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f9fafb"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6b7280"))

	revokeDangerStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ef4444"))

	revokeFooterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6b7280")).
				Italic(true)
)

// ── Model ─────────────────────────────────────────────────────────────────────

type revokeItem struct {
	device  server.Device
	checked bool
}

type revokeModel struct {
	items    []revokeItem
	cursor   int
	width    int
	height   int
	done     bool
	aborted  bool
	confirmed bool
}

func newRevokeModel(devices map[string]server.Device) revokeModel {
	// Sort by name for consistent display
	keys := make([]string, 0, len(devices))
	for k := range devices {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return devices[keys[i]].Name < devices[keys[j]].Name
	})

	items := make([]revokeItem, len(keys))
	for i, k := range keys {
		items[i] = revokeItem{device: devices[k]}
	}
	return revokeModel{items: items}
}

func (m revokeModel) Init() tea.Cmd { return nil }

func (m revokeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.aborted = true
			m.done = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}

		case " ":
			m.items[m.cursor].checked = !m.items[m.cursor].checked

		case "a":
			// Toggle all
			allChecked := true
			for _, item := range m.items {
				if !item.checked {
					allChecked = false
					break
				}
			}
			for i := range m.items {
				m.items[i].checked = !allChecked
			}

		case "enter":
			// Only proceed if at least one is selected
			selected := m.selectedItems()
			if len(selected) == 0 {
				return m, nil
			}
			m.confirmed = true
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m revokeModel) selectedItems() []revokeItem {
	var out []revokeItem
	for _, item := range m.items {
		if item.checked {
			out = append(out, item)
		}
	}
	return out
}

func (m revokeModel) View() string {
	if m.width == 0 {
		return ""
	}

	header := revokeHeaderStyle.Width(min(m.width, 72)).Render(" SHARE — REVOKE DEVICE ACCESS ")

	// Build device list
	var rows []string
	for i, item := range m.items {
		checkbox := uncheckedStyle.Render("○")
		if item.checked {
			checkbox = checkedStyle.Render("●")
		}

		line := fmt.Sprintf("  %s  %s", checkbox, nameStyle.Render(item.device.Name))
		meta := dimStyle.Render(fmt.Sprintf("  registered %s", item.device.CreatedAt.Format("2006-01-02")))

		if i == m.cursor {
			line = selectedStyle.Render(">") + line[1:] // replace leading space with arrow
			line = lipgloss.NewStyle().Background(lipgloss.Color("#1f2937")).Render(line)
		}

		rows = append(rows, line)
		rows = append(rows, meta)
		rows = append(rows, "")
	}

	// Selection summary
	selected := m.selectedItems()
	var summary string
	if len(selected) == 0 {
		summary = dimStyle.Render("No devices selected.")
	} else {
		names := make([]string, len(selected))
		for i, s := range selected {
			names[i] = s.device.Name
		}
		summary = revokeDangerStyle.Render(
			fmt.Sprintf("⚠  Will revoke: %s", strings.Join(names, ", ")),
		)
	}

	body := revokeBoxStyle.Width(min(m.width-4, 70)).Render(
		strings.Join(rows, "\n") + "\n" + summary,
	)

	footer := revokeFooterStyle.Render(
		"↑/↓: Navigate  Space: Toggle  [a]: Select all  Enter: Confirm  Esc/q: Cancel",
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}

// ── Confirmation prompt ───────────────────────────────────────────────────────

type confirmModel struct {
	names   []string
	answer  bool
	done    bool
	width   int
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch strings.ToLower(msg.String()) {
		case "y":
			m.answer = true
			m.done = true
			return m, tea.Quit
		case "n", "q", "ctrl+c", "esc":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	box := revokeBoxStyle.Width(min(m.width-4, 70)).Render(
		revokeDangerStyle.Render("⚠  This will permanently remove access for:") + "\n\n" +
			"  " + strings.Join(m.names, "\n  ") + "\n\n" +
			nameStyle.Render("These devices will need to re-register and be re-approved.\n") +
			"\n" +
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f9fafb")).Render("Confirm? [y/N]"),
	)
	return "\n" + box
}

// ── Cobra command ─────────────────────────────────────────────────────────────

var revokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke access for one or more approved devices",
	Long: `Interactively select devices to revoke.

Revoked devices are removed from ~/.share_devices.json.
They will need to re-register and be re-approved on their next visit.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		devices, err := server.LoadDevices()
		if err != nil {
			return fmt.Errorf("failed to load devices: %w", err)
		}

		// Filter to only approved devices (nothing to revoke for pending/rejected)
		approved := make(map[string]server.Device)
		for k, d := range devices {
			if d.Status == "approved" {
				approved[k] = d
			}
		}

		if len(approved) == 0 {
			fmt.Println("No approved devices found.")
			return nil
		}

		// Step 1: multi-select picker
		picker := newRevokeModel(approved)
		p := tea.NewProgram(picker)
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}

		final := result.(revokeModel)
		if final.aborted || !final.confirmed {
			fmt.Println("Cancelled.")
			return nil
		}

		selected := final.selectedItems()
		if len(selected) == 0 {
			fmt.Println("No devices selected.")
			return nil
		}

		// Step 2: confirmation prompt
		names := make([]string, len(selected))
		keys := make([]string, len(selected))
		for i, s := range selected {
			names[i] = s.device.Name
			keys[i] = s.device.PublicKey
		}

		confirm := confirmModel{names: names}
		cp := tea.NewProgram(confirm)
		cr, err := cp.Run()
		if err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}
		if !cr.(confirmModel).answer {
			fmt.Println("Cancelled.")
			return nil
		}

		// Step 3: revoke
		revoked, err := server.RevokeDevices(keys)
		if err != nil {
			return fmt.Errorf("failed to revoke devices: %w", err)
		}

		fmt.Printf("\n✓ Revoked %d device(s):\n", revoked)
		for _, name := range names {
			fmt.Printf("  • %s\n", name)
		}
		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(revokeCmd)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure time package is used (via Device.CreatedAt formatting)
var _ = time.Now
