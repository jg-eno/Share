package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// FileEntry represents a file or directory entry in the sharing folder
type FileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// FilesResponse represents the list of files in the current folder
type FilesResponse struct {
	CurrentPath string      `json:"currentPath"`
	Entries     []FileEntry `json:"entries"`
}

// resolvePath resolves the relative path within s.Root securely, preventing directory traversal.
func (s *Server) resolvePath(relPath string) (string, error) {
	absRoot, err := filepath.Abs(s.Root)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path of root: %v", err)
	}

	// Join absRoot with relPath. Join cleans the path automatically.
	targetPath := filepath.Join(absRoot, relPath)

	// Verify the target path is inside the root directory
	rel, err := filepath.Rel(absRoot, targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %v", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("access denied: path traversal attempt")
	}

	return targetPath, nil
}

func generateRandomHex(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func (s *Server) isSessionValid(r *http.Request) bool {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	cookie, err := r.Cookie("share_session")
	if err != nil {
		return false
	}

	_, ok := s.Sessions[cookie.Value]
	return ok
}

// Handler returns the HTTP handler for the server, setting up all routes
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 1. Serve embedded static WebUI files under /web/
	mux.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.FS(webAssets))))

	// 2. Serve main index.html at root "/"
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(webAssets, "index.html")
		if err != nil {
			http.Error(w, "WebUI template not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// --- AUTHENTICATION ENDPOINTS ---

	// POST /api/auth/register - Register a new device for approval
	mux.HandleFunc("/api/auth/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Name      string `json:"name"`
			PublicKey string `json:"publicKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.PublicKey == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		s.Mu.Lock()
		defer s.Mu.Unlock()

		// Check if already authorized
		if _, ok := s.Devices[req.PublicKey]; ok {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "approved"})
			return
		}

		// Check if already in pending requests
		alreadyPending := false
		for _, dev := range s.PendingRequests {
			if dev.PublicKey == req.PublicKey {
				alreadyPending = true
				break
			}
		}

		if !alreadyPending {
			newReq := Device{
				Name:      req.Name,
				PublicKey: req.PublicKey,
				Status:    "pending",
				CreatedAt: time.Now(),
			}
			s.PendingRequests = append(s.PendingRequests, newReq)
			s.Log("Auth: New device request '%s' (Approval Required)", req.Name)
			// Notify TUI about the approval request (non-blocking, outside lock)
			go s.NotifyApproval(newReq)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	})

	// GET /api/auth/status - Check status of a device public key
	mux.HandleFunc("/api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		pubKey := r.URL.Query().Get("pubkey")
		if pubKey == "" {
			http.Error(w, "Missing pubkey", http.StatusBadRequest)
			return
		}

		s.Mu.Lock()
		defer s.Mu.Unlock()

		status := "unknown"
		if dev, ok := s.Devices[pubKey]; ok {
			status = dev.Status
		} else {
			for _, dev := range s.PendingRequests {
				if dev.PublicKey == pubKey {
					status = "pending"
					break
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": status})
	})

	// GET /api/auth/challenge - Get a random nonce challenge
	mux.HandleFunc("/api/auth/challenge", func(w http.ResponseWriter, r *http.Request) {
		nonce := generateRandomHex(16)
		if nonce == "" {
			http.Error(w, "failed to generate challenge", http.StatusInternalServerError)
			return
		}

		s.Mu.Lock()
		// Clean up expired nonces
		now := time.Now()
		for n, t := range s.ActiveNonces {
			if now.Sub(t) > 5*time.Minute {
				delete(s.ActiveNonces, n)
			}
		}
		s.ActiveNonces[nonce] = now
		s.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": nonce})
	})

	// POST /api/auth/verify - Verify ecdsa signature and create session cookie
	mux.HandleFunc("/api/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PublicKey string `json:"publicKey"`
			Nonce     string `json:"nonce"`
			Signature string `json:"signature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		s.Mu.Lock()
		// Verify and delete nonce to prevent replay
		createdTime, nonceExists := s.ActiveNonces[req.Nonce]
		if !nonceExists || time.Since(createdTime) > 5*time.Minute {
			s.Mu.Unlock()
			http.Error(w, "Challenge expired or invalid", http.StatusForbidden)
			return
		}
		delete(s.ActiveNonces, req.Nonce)

		// Check if device is approved
		dev, ok := s.Devices[req.PublicKey]
		if !ok || dev.Status != "approved" {
			s.Mu.Unlock()
			http.Error(w, "Device not approved", http.StatusForbidden)
			return
		}
		s.Mu.Unlock()

		// Verify cryptographic signature
		valid, err := VerifySignature(req.PublicKey, req.Nonce, req.Signature)
		if err != nil || !valid {
			http.Error(w, "Invalid signature", http.StatusForbidden)
			return
		}

		// Create Session
		sessionToken := generateRandomHex(32)
		s.Mu.Lock()
		s.Sessions[sessionToken] = req.PublicKey
		s.Mu.Unlock()

		// Set Session Cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "share_session",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 30, // 30 days
		})

		s.Log("Auth: Device '%s' authenticated successfully", dev.Name)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// POST /api/auth/verify-simple - Issue a session for approved devices that
	// cannot perform ECDSA signing (e.g. plain-HTTP contexts where crypto.subtle
	// is unavailable). The TUI approval is still required as the trust gate.
	mux.HandleFunc("/api/auth/verify-simple", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PublicKey string `json:"publicKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PublicKey == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		s.Mu.Lock()
		dev, ok := s.Devices[req.PublicKey]
		if !ok || dev.Status != "approved" {
			s.Mu.Unlock()
			http.Error(w, "Device not approved", http.StatusForbidden)
			return
		}
		sessionToken := generateRandomHex(32)
		s.Sessions[sessionToken] = req.PublicKey
		s.Mu.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:     "share_session",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 30,
		})

		s.Log("Auth: Device '%s' authenticated (simple mode)", dev.Name)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// --- PROTECTED FILE SHARING ENDPOINTS ---

	// 3. API endpoint for file lists
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) {
		if !s.isSessionValid(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		relPath := r.URL.Query().Get("path")
		targetPath, err := s.resolvePath(relPath)
		if err != nil {
			s.Log("API Error: forbidden path resolve for '%s' (Client: %s)", relPath, r.RemoteAddr)
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		s.Log("Browsing directory: /%s (Client: %s)", relPath, r.RemoteAddr)

		entries, err := os.ReadDir(targetPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read directory: %v", err), http.StatusInternalServerError)
			return
		}

		fileEntries := []FileEntry{}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			fileEntries = append(fileEntries, FileEntry{
				Name:    entry.Name(),
				IsDir:   entry.IsDir(),
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FilesResponse{
			CurrentPath: relPath,
			Entries:     fileEntries,
		})
	})

	// 4. API endpoint to upload a file
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if !s.isSessionValid(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit upload size to 10GB for local network file transfers
		r.Body = http.MaxBytesReader(w, r.Body, 10<<30)

		relPath := r.URL.Query().Get("path")
		targetDir, err := s.resolvePath(relPath)
		if err != nil {
			s.Log("API Error: forbidden upload path '%s' (Client: %s)", relPath, r.RemoteAddr)
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		// Verify target is a directory
		dirInfo, err := os.Stat(targetDir)
		if err != nil || !dirInfo.IsDir() {
			http.Error(w, "target is not a valid directory", http.StatusBadRequest)
			return
		}

		// Parse multipart form
		err = r.ParseMultipartForm(32 << 20) // 32MB buffer
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to parse multipart form: %v", err), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to retrieve file: %v", err), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Protect against malicious filenames containing path characters
		safeFilename := filepath.Base(header.Filename)
		destPath := filepath.Join(targetDir, safeFilename)

		// Final check to verify destPath is indeed within targetDir
		if _, err := s.resolvePath(filepath.Join(relPath, safeFilename)); err != nil {
			s.Log("API Error: invalid upload filename '%s' (Client: %s)", safeFilename, r.RemoteAddr)
			http.Error(w, "invalid destination filename", http.StatusForbidden)
			return
		}

		s.Log("Uploading file: %s into /%s (Client: %s)", safeFilename, relPath, r.RemoteAddr)

		out, err := os.Create(destPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create destination file: %v", err), http.StatusInternalServerError)
			return
		}
		defer out.Close()

		bytesCopied, err := io.Copy(out, file)
		if err != nil {
			s.Log("Upload failed: %s (Error: %v)", safeFilename, err)
			http.Error(w, fmt.Sprintf("failed to write file: %v", err), http.StatusInternalServerError)
			return
		}

		s.Log("Uploaded: %s (%d KB)", safeFilename, bytesCopied/1024)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// 5. API endpoint to get sharing stats/configuration
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if !s.isSessionValid(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		absPath, err := filepath.Abs(s.Root)
		if err != nil {
			absPath = s.Root
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"sharingDir": absPath,
		})
	})

	// 6. API endpoint to generate connection QR codes dynamically
	mux.HandleFunc("/api/qr", func(w http.ResponseWriter, r *http.Request) {
		data := r.URL.Query().Get("data")
		if data == "" {
			http.Error(w, "missing data parameter", http.StatusBadRequest)
			return
		}

		png, err := qrcode.Encode(data, qrcode.Medium, 256)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to generate qr code: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})

	// 7. Secure File download handler serving files under /files/*
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		if !s.isSessionValid(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Strip /files/ prefix
		relPath := strings.TrimPrefix(r.URL.Path, "/files/")

		resolvedFilePath, err := s.resolvePath(relPath)
		if err != nil {
			s.Log("API Error: forbidden download path '%s' (Client: %s)", relPath, r.RemoteAddr)
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		info, err := os.Stat(resolvedFilePath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		// Disable directory listing for the file server endpoint
		if info.IsDir() {
			http.Error(w, "Directory browsing not allowed via file server", http.StatusForbidden)
			return
		}

		s.Log("Downloading: /%s (Client: %s)", relPath, r.RemoteAddr)
		http.ServeFile(w, r, resolvedFilePath)
	})

	return mux
}

// Start sets up routing and starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.Port)
	s.Log("Starting secure file sharing server on %s", addr)
	return http.ListenAndServe(addr, s.Handler())
}
