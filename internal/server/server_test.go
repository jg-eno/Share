package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injectSession creates a valid session in the server and returns a cookie
// that can be attached to test requests for protected endpoints.
func injectSession(s *Server) *http.Cookie {
	token := "test-session-token-abc123"
	s.Mu.Lock()
	s.Sessions[token] = "test-device-pubkey"
	s.Mu.Unlock()
	return &http.Cookie{Name: "share_session", Value: token}
}

func TestResolvePath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "share-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s := New(tempDir, 8080)

	// Root path resolves to absRoot
	path, err := s.resolvePath("")
	if err != nil {
		t.Errorf("expected no error for empty path, got: %v", err)
	}
	absRoot, _ := filepath.Abs(tempDir)
	if path != absRoot {
		t.Errorf("expected %s, got %s", absRoot, path)
	}

	// Safe subdirectory
	subDir := filepath.Join(tempDir, "docs")
	os.Mkdir(subDir, 0755)
	path, err = s.resolvePath("docs")
	if err != nil {
		t.Errorf("expected no error for safe subdir, got: %v", err)
	}
	expectedSub, _ := filepath.Abs(subDir)
	if path != expectedSub {
		t.Errorf("expected %s, got %s", expectedSub, path)
	}

	// Path traversal: parent directory
	_, err = s.resolvePath("../")
	if err == nil {
		t.Error("expected error for path traversal attempt (../), got nil")
	}

	// Path traversal: nested
	_, err = s.resolvePath("docs/../../")
	if err == nil {
		t.Error("expected error for path traversal attempt (docs/../../), got nil")
	}
}

func TestAPI_Files(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "share-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(tempDir, "folder"), 0755)

	s := New(tempDir, 8080)
	handler := s.Handler()
	cookie := injectSession(s)

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	var resp FilesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.Entries))
	}

	var foundText, foundFolder bool
	for _, entry := range resp.Entries {
		if entry.Name == "test.txt" {
			foundText = true
			if entry.IsDir {
				t.Error("expected test.txt to not be a directory")
			}
			if entry.Size != 5 {
				t.Errorf("expected size 5, got %d", entry.Size)
			}
		}
		if entry.Name == "folder" {
			foundFolder = true
			if !entry.IsDir {
				t.Error("expected folder to be a directory")
			}
		}
	}

	if !foundText || !foundFolder {
		t.Error("did not find expected files in response")
	}
}

func TestAPI_Files_Unauthorized(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "share-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s := New(tempDir, 8080)
	handler := s.Handler()

	// No cookie — should get 401
	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without session, got %d", rr.Code)
	}
}

func TestAPI_Upload(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "share-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s := New(tempDir, 8080)
	handler := s.Handler()
	cookie := injectSession(s)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "uploaded.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	part.Write([]byte("uploaded content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload?path=", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	uploadedPath := filepath.Join(tempDir, "uploaded.txt")
	content, err := os.ReadFile(uploadedPath)
	if err != nil {
		t.Fatalf("uploaded file does not exist: %v", err)
	}
	if string(content) != "uploaded content" {
		t.Errorf("expected 'uploaded content', got '%s'", string(content))
	}
}

func TestFiles_DownloadAndSecurity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "share-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	os.WriteFile(filepath.Join(tempDir, "secret.txt"), []byte("top secret"), 0644)
	sub := filepath.Join(tempDir, "public")
	os.Mkdir(sub, 0755)
	os.WriteFile(filepath.Join(sub, "hello.txt"), []byte("hello public"), 0644)

	s := New(tempDir, 8080)
	handler := s.Handler()
	cookie := injectSession(s)

	// 1. Download file successfully
	req := httptest.NewRequest(http.MethodGet, "/files/secret.txt", nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "top secret" {
		t.Errorf("expected 'top secret', got '%s'", rr.Body.String())
	}

	// 2. Reject directory browsing via /files/
	req2 := httptest.NewRequest(http.MethodGet, "/files/public", nil)
	req2.AddCookie(cookie)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusForbidden {
		t.Errorf("expected status 403 for directory, got %d", rr2.Code)
	}

	// 3. Reject download without a session cookie
	req3 := httptest.NewRequest(http.MethodGet, "/files/secret.txt", nil)
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without session, got %d", rr3.Code)
	}

	// 4. Reject path traversal
	req4 := httptest.NewRequest(http.MethodGet, "/files/../secret.txt", nil)
	req4.AddCookie(cookie)
	rr4 := httptest.NewRecorder()
	handler.ServeHTTP(rr4, req4)

	if rr4.Code != http.StatusForbidden && rr4.Code != http.StatusMovedPermanently && rr4.Code != http.StatusNotFound {
		t.Errorf("expected 403/301/404 for path traversal, got %d", rr4.Code)
	}
}

func TestStaticWeb(t *testing.T) {
	s := New(".", 8080)
	handler := s.Handler()

	// Root serves HTML
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 for /, got %d", rr.Code)
	}
	contentType := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("expected Content-Type text/html, got %s", contentType)
	}

	// Static CSS file
	req2 := httptest.NewRequest(http.MethodGet, "/web/style.css", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected status 200 for /web/style.css, got %d", rr2.Code)
	}
}

func TestAuth_Register(t *testing.T) {
	s := New(".", 8080)
	handler := s.Handler()

	body := `{"name":"Test Device","publicKey":"dGVzdC1wdWJsaWMta2V5"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "pending" {
		t.Errorf("expected status=pending, got %q", resp["status"])
	}

	// Verify it appears in PendingRequests
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if len(s.PendingRequests) != 1 {
		t.Errorf("expected 1 pending request, got %d", len(s.PendingRequests))
	}
	if s.PendingRequests[0].Name != "Test Device" {
		t.Errorf("expected device name 'Test Device', got %q", s.PendingRequests[0].Name)
	}
}

func TestAuth_Challenge(t *testing.T) {
	s := New(".", 8080)
	handler := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/challenge", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse challenge response: %v", err)
	}
	if resp["challenge"] == "" {
		t.Error("expected non-empty challenge nonce")
	}
	if len(resp["challenge"]) != 32 { // 16 random bytes → 32 hex chars
		t.Errorf("expected 32-char hex nonce, got length %d", len(resp["challenge"]))
	}
}

func TestAuth_Status(t *testing.T) {
	s := New(".", 8080)
	handler := s.Handler()

	// Status for unknown key
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status?pubkey=unknownkey", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "unknown" {
		t.Errorf("expected status=unknown, got %q", resp["status"])
	}

	// Status after registering
	s.Mu.Lock()
	s.PendingRequests = append(s.PendingRequests, Device{Name: "Dev", PublicKey: "mykey", Status: "pending"})
	s.Mu.Unlock()

	req2 := httptest.NewRequest(http.MethodGet, "/api/auth/status?pubkey=mykey", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	json.Unmarshal(rr2.Body.Bytes(), &resp)
	if resp["status"] != "pending" {
		t.Errorf("expected status=pending, got %q", resp["status"])
	}
}
