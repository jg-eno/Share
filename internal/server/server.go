package server

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type Server struct {
	Root    string
	Port    int
	LogChan chan string

	// ApprovalChan delivers device approval requests to the TUI
	ApprovalChan chan ApprovalMsg

	// Auth & Devices state
	Devices         map[string]Device    // Map of public keys to Device profiles
	PendingRequests []Device             // Queue of pending device authorization requests
	Sessions        map[string]string    // Map of session tokens to device public keys
	ActiveNonces    map[string]time.Time // Map of nonces to their creation time
	Mu              sync.Mutex           // Mutex protecting state maps
}

func New(root string, port int) *Server {
	devices, err := LoadDevices()
	if err != nil {
		log.Printf("Warning: failed to load authorized devices: %v", err)
		devices = make(map[string]Device)
	}

	return &Server{
		Root:            root,
		Port:            port,
		Devices:         devices,
		PendingRequests: []Device{},
		Sessions:        make(map[string]string),
		ActiveNonces:    make(map[string]time.Time),
		ApprovalChan:    make(chan ApprovalMsg, 16),
	}
}

// Log logs a message either to standard log or to the TUI channel if present
func (s *Server) Log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("15:04:05")
	formattedMsg := fmt.Sprintf("[%s] %s", timestamp, msg)

	if s.LogChan != nil {
		select {
		case s.LogChan <- formattedMsg:
		default:
			// Non-blocking fallback to prevent lockups if log channel is full
		}
	} else {
		log.Println(msg)
	}
}

// NotifyApproval sends a device approval request to the TUI (non-blocking)
func (s *Server) NotifyApproval(dev Device) {
	if s.ApprovalChan != nil {
		select {
		case s.ApprovalChan <- ApprovalMsg{Device: dev}:
		default:
			// Non-blocking: drop if channel is full
		}
	}
}
