package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	qrcode "github.com/skip2/go-qrcode"
)

type logMsg string
type serverStartedMsg struct{}

// ApprovalMsg is sent to the TUI when a new device requests authorization
type ApprovalMsg struct {
	Device Device
}

// TuiMode represents the current screen mode of the TUI
type TuiMode int

const (
	ModePicker TuiMode = iota
	ModeMonitor
)

// waitForActivity blocks until a log message is sent on the channel, then returns it.
func waitForActivity(ch chan string) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return logMsg(msg)
	}
}

// waitForApproval blocks until a device approval request arrives on the channel.
func waitForApproval(ch chan ApprovalMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// TuiModel represents the Bubble Tea TUI state
type TuiModel struct {
	server     *Server
	logChan    chan string
	logs       []string
	width      int
	height     int
	serverAddr string

	// Picker State
	activeMode   TuiMode
	currentDir   string
	localEntries []os.DirEntry
	cursor       int
	startIdx     int
	pickerErr    error
}

// NewTuiModel initializes the TUI model, defaulting to Picker mode if activeMode is picker
func NewTuiModel(s *Server, addr string, startInPicker bool) TuiModel {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err == nil {
		dir = absDir
	}

	mode := ModeMonitor
	if startInPicker {
		mode = ModePicker
	}

	m := TuiModel{
		server:     s,
		logChan:    s.LogChan,
		logs:       []string{"[System] Server TUI started successfully.", "[System] Monitoring active network logs..."},
		serverAddr: addr,
		activeMode: mode,
		currentDir: dir,
	}

	if startInPicker {
		m.readCurrentDir()
	}

	return m
}

func (m *TuiModel) readCurrentDir() {
	entries, err := os.ReadDir(m.currentDir)
	if err != nil {
		m.pickerErr = err
		m.localEntries = nil
		return
	}
	m.pickerErr = nil

	// Sort: directories first, then files
	m.localEntries = entries
	m.cursor = 0
	m.startIdx = 0
}

func (m TuiModel) startServerCmd() tea.Cmd {
	return func() tea.Msg {
		go func() {
			if err := m.server.Start(); err != nil {
				m.logChan <- fmt.Sprintf("[Error] Server failed to start: %v", err)
			}
		}()
		return serverStartedMsg{}
	}
}

func (m TuiModel) Init() tea.Cmd {
	if m.activeMode == ModeMonitor {
		cmds := []tea.Cmd{
			waitForActivity(m.logChan),
		}
		if m.server.ApprovalChan != nil {
			cmds = append(cmds, waitForApproval(m.server.ApprovalChan))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func (m TuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

		// Approval key handling in Monitor mode
		if m.activeMode == ModeMonitor {
			m.server.Mu.Lock()
			hasPending := len(m.server.PendingRequests) > 0
			m.server.Mu.Unlock()

			if hasPending {
				switch msg.String() {
				case "a":
					m.server.Mu.Lock()
					if len(m.server.PendingRequests) > 0 {
						dev := m.server.PendingRequests[0]
						m.server.PendingRequests = m.server.PendingRequests[1:]
						dev.Status = "approved"
						m.server.Devices[dev.PublicKey] = dev
						go func() {
							if err := SaveDevices(m.server.Devices); err != nil {
								m.logChan <- fmt.Sprintf("[Error] Failed to save device '%s': %v", dev.Name, err)
							}
						}()
						m.logChan <- fmt.Sprintf("[Auth] ✓ Device '%s' APPROVED", dev.Name)
					}
					m.server.Mu.Unlock()
					return m, nil

				case "r":
					m.server.Mu.Lock()
					if len(m.server.PendingRequests) > 0 {
						dev := m.server.PendingRequests[0]
						m.server.PendingRequests = m.server.PendingRequests[1:]
						m.logChan <- fmt.Sprintf("[Auth] ✗ Device '%s' REJECTED", dev.Name)
					}
					m.server.Mu.Unlock()
					return m, nil
				}
			}
		}

		// Picker Mode Input Handlers
		if m.activeMode == ModePicker {
			switch msg.String() {
			case "up":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down":
				if m.cursor < len(m.localEntries)-1 {
					m.cursor++
				}
			case "enter", "right", "l":
				if len(m.localEntries) > 0 {
					entry := m.localEntries[m.cursor]
					if entry.IsDir() {
						m.currentDir = filepath.Join(m.currentDir, entry.Name())
						m.readCurrentDir()
					}
				}
			case "backspace", "left", "h":
				parent := filepath.Dir(m.currentDir)
				if parent != m.currentDir {
					m.currentDir = parent
					m.readCurrentDir()
				}
			case "s":
				// Confirm selection and start server
				var selectedDir string
				if len(m.localEntries) > 0 {
					entry := m.localEntries[m.cursor]
					if entry.IsDir() {
						selectedDir = filepath.Join(m.currentDir, entry.Name())
					} else {
						selectedDir = m.currentDir
					}
				} else {
					selectedDir = m.currentDir
				}

				m.server.Root = selectedDir
				return m, m.startServerCmd()
			}

			// Adjust viewport scrolling bounds based on terminal height
			maxLines := 15
			if m.height > 12 {
				maxLines = m.height - 10
			}
			if maxLines < 1 {
				maxLines = 1
			}

			if m.cursor < m.startIdx {
				m.startIdx = m.cursor
			} else if m.cursor >= m.startIdx+maxLines {
				m.startIdx = m.cursor - maxLines + 1
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case serverStartedMsg:
		m.activeMode = ModeMonitor
		m.logs = append(m.logs, fmt.Sprintf("[System] Server listening on %s", m.serverAddr))
		cmds := []tea.Cmd{waitForActivity(m.logChan)}
		if m.server.ApprovalChan != nil {
			cmds = append(cmds, waitForApproval(m.server.ApprovalChan))
		}
		return m, tea.Batch(cmds...)

	case logMsg:
		m.logs = append(m.logs, string(msg))
		// Cap logs to avoid drawing off the terminal height
		maxLogs := 12
		if m.height > 12 {
			maxLogs = m.height - 8
		}
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
		cmds := []tea.Cmd{waitForActivity(m.logChan)}
		if m.server.ApprovalChan != nil {
			cmds = append(cmds, waitForApproval(m.server.ApprovalChan))
		}
		return m, tea.Batch(cmds...)

	case ApprovalMsg:
		// A new device is awaiting approval — log it and keep listening
		m.logs = append(m.logs, fmt.Sprintf("[Auth] ⚠  Device '%s' is requesting authorization", msg.Device.Name))
		maxLogs := 12
		if m.height > 12 {
			maxLogs = m.height - 8
		}
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}
		cmds := []tea.Cmd{waitForActivity(m.logChan)}
		if m.server.ApprovalChan != nil {
			cmds = append(cmds, waitForApproval(m.server.ApprovalChan))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m TuiModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing Share TUI..."
	}

	// 1. Neon Blue & Grey Theme Colors (softened neon blue)
	neonBlue := lipgloss.Color("#51b6f5ff")
	greyLight := lipgloss.Color("#9ca3af")
	greyDark := lipgloss.Color("#374151")
	emeraldGreen := lipgloss.Color("#10b981")
	textMuted := lipgloss.Color("#6b7280")
	neonRed := lipgloss.Color("#ef4444")
	neonAmber := lipgloss.Color("#f59e0b")

	// Styling Rules
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(neonBlue).
		Padding(0, 4).
		Align(lipgloss.Center)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(neonBlue)

	labelStyle := lipgloss.NewStyle().
		Foreground(greyLight).
		Bold(true)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff"))

	activeStatusStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(emeraldGreen)

	leftPanelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(neonBlue).
		Padding(1, 2)

	rightPanelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(greyDark).
		Padding(1, 2)

	footerStyle := lipgloss.NewStyle().
		Foreground(textMuted).
		Italic(true)

	// Header Text
	headerText := " SHARE FILE EXCHANGE "
	headerWidth := m.width
	if headerWidth > 80 {
		headerWidth = 80
	}
	header := headerStyle.Width(headerWidth).Render(headerText)

	// Render based on active TUI Mode
	if m.activeMode == ModePicker {
		// --- RENDER PICKER MODE ---
		pickerTitle := titleStyle.Render("SELECT DIRECTORY TO SHARE")
		pathDisplay := fmt.Sprintf("%s %s", labelStyle.Render("Current:"), valueStyle.Render(m.currentDir))

		// Render Directory Entries
		var entriesStr string
		if m.pickerErr != nil {
			entriesStr = lipgloss.NewStyle().Foreground(neonRed).Render(fmt.Sprintf("Error: %v", m.pickerErr))
		} else if len(m.localEntries) == 0 {
			entriesStr = lipgloss.NewStyle().Foreground(textMuted).Render("  (Empty directory)")
		} else {
			lines := []string{}

			// Show parent directory indicator if not root
			parent := filepath.Dir(m.currentDir)
			if parent != m.currentDir {
				lines = append(lines, "  📁 .. (Parent directory)")
			}

			maxLines := 15
			if m.height > 12 {
				maxLines = m.height - 10
			}
			if maxLines < 1 {
				maxLines = 1
			}

			// Slice entries based on viewport startIdx
			endIdx := m.startIdx + maxLines
			if endIdx > len(m.localEntries) {
				endIdx = len(m.localEntries)
			}

			visibleEntries := m.localEntries[m.startIdx:endIdx]

			for i, entry := range visibleEntries {
				actualIdx := m.startIdx + i
				prefix := "  "
				if actualIdx == m.cursor {
					prefix = "> "
				}

				name := entry.Name()
				var renderedLine string
				if entry.IsDir() {
					renderedLine = lipgloss.NewStyle().Foreground(neonBlue).Render(prefix + "📁 " + name + "/")
				} else {
					renderedLine = lipgloss.NewStyle().Foreground(greyLight).Render(prefix + "📄 " + name)
				}

				if actualIdx == m.cursor {
					renderedLine = lipgloss.NewStyle().
						Bold(true).
						Background(greyDark).
						Render(renderedLine)
				}

				lines = append(lines, renderedLine)
			}

			entriesStr = strings.Join(lines, "\n")
		}

		body := leftPanelStyle.
			Width(m.width - 4).
			Height(m.height - 8).
			Render(strings.Join([]string{pickerTitle, pathDisplay, "", entriesStr}, "\n"))

		footerText := "↑/↓: Navigate  |  Enter: Enter Folder  |  Backspace/h: Parent  |  [s]: Select folder & Start server  |  [q]: Quit"
		footer := footerStyle.Render(footerText)

		return lipgloss.JoinVertical(
			lipgloss.Left,
			header,
			"",
			body,
			"",
			footer,
		)
	}

	// --- RENDER MONITOR MODE ---

	// Check for pending device approvals
	m.server.Mu.Lock()
	pendingCount := len(m.server.PendingRequests)
	var pendingDeviceName string
	if pendingCount > 0 {
		pendingDeviceName = m.server.PendingRequests[0].Name
	}
	m.server.Mu.Unlock()

	// Build optional approval alert banner
	var approvalBanner string
	if pendingCount > 0 {
		alertText := fmt.Sprintf(
			"  ⚠  APPROVAL REQUIRED  —  Device \"%s\" wants to connect.  Press [a] to Approve  |  [r] to Reject  (%d pending)  ",
			pendingDeviceName,
			pendingCount,
		)
		approvalBanner = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(neonRed).
			Padding(0, 1).
			Width(m.width).
			Render(alertText)
		approvalBanner = "\n" + approvalBanner + "\n"
	}

	// 4. Render Left Panel (Server Status & QR Code)
	qrString := ""
	if m.serverAddr != "" {
		qr, err := qrcode.New(m.serverAddr, qrcode.Medium)
		if err == nil {
			qr.DisableBorder = false
			qrString = qr.ToSmallString(false)
		}
	}

	qrRendered := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Render(strings.TrimRight(qrString, "\n"))

	leftContent := strings.Join([]string{
		titleStyle.Render("SYSTEM MONITOR"),
		"",
		fmt.Sprintf("%s %s", labelStyle.Render("Status:"), activeStatusStyle.Render("ONLINE ●")),
		"",
		fmt.Sprintf("%s\n%s", labelStyle.Render("Server Address:"), valueStyle.Render(m.serverAddr)),
		"",
		fmt.Sprintf("%s\n%s", labelStyle.Render("Shared Folder:"), valueStyle.Render(m.server.Root)),
		"",
		fmt.Sprintf("%s %s", labelStyle.Render("Approved Devices:"), lipgloss.NewStyle().Foreground(neonAmber).Render(fmt.Sprintf("%d", len(m.server.Devices)))),
		"",
		labelStyle.Render("Scan to Connect:"),
		qrRendered,
	}, "\n")

	// 5. Render Right Panel (Live Server Logs)
	logLines := make([]string, len(m.logs))
	for i, logItem := range m.logs {
		if strings.Contains(logItem, "Error") || strings.Contains(logItem, "failed") {
			logLines[i] = lipgloss.NewStyle().Foreground(neonRed).Render(logItem)
		} else if strings.Contains(logItem, "APPROVED") {
			logLines[i] = lipgloss.NewStyle().Foreground(emeraldGreen).Bold(true).Render(logItem)
		} else if strings.Contains(logItem, "REJECTED") || strings.Contains(logItem, "requesting authorization") {
			logLines[i] = lipgloss.NewStyle().Foreground(neonAmber).Render(logItem)
		} else {
			logLines[i] = lipgloss.NewStyle().Foreground(greyLight).Render(logItem)
		}
	}

	rightContentTitle := titleStyle.Foreground(greyLight).Render("LIVE TRANSFER ACTIVITY LOGS")
	rightContent := rightContentTitle + "\n\n" + strings.Join(logLines, "\n")

	// 6. Join Layout Panels Responsive Design
	var body string
	leftPanelWidth := 38
	if m.width > 75 {
		rightPanelWidth := m.width - leftPanelWidth - 6
		if rightPanelWidth > 60 {
			rightPanelWidth = 60
		}

		leftPanel  := leftPanelStyle.Width(leftPanelWidth).Render(leftContent)
		rightPanel := rightPanelStyle.Width(rightPanelWidth).Render(rightContent)

		body = lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
	} else {
		bodyWidth := m.width - 4
		if bodyWidth > 60 {
			bodyWidth = 60
		}

		leftPanel  := leftPanelStyle.Width(bodyWidth).Render(leftContent)
		rightPanel := rightPanelStyle.Width(bodyWidth).Render(rightContent)

		body = lipgloss.JoinVertical(lipgloss.Left, leftPanel, rightPanel)
	}

	footerText := "Press [q] or [ctrl+c] to gracefully shutdown server"
	if pendingCount > 0 {
		footerText = "[a]: Approve device  |  [r]: Reject device  |  [q] or [ctrl+c]: Shutdown server"
	}
	footer := footerStyle.Render(footerText)

	parts := []string{header, ""}
	if approvalBanner != "" {
		parts = append(parts, approvalBanner)
	}
	parts = append(parts, body, "", footer)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
