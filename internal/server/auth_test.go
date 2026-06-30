package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ----------------------------------------------------------------
// Helpers: generate a real P-256 key pair and produce a valid sig
// ----------------------------------------------------------------

func generateTestKeyPair(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key pair: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("failed to marshal public key: %v", err)
	}
	return priv, base64.StdEncoding.EncodeToString(pubDER)
}

// rawSign returns the raw 64-byte (r‖s) ECDSA signature matching the format
// WebCrypto produces and the Go verifier expects.
func rawSign(t *testing.T, priv *ecdsa.PrivateKey, nonce string) string {
	t.Helper()
	hash := sha256.Sum256([]byte(nonce))
	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatalf("failed to sign nonce: %v", err)
	}
	// Pad each component to exactly 32 bytes
	rb := r.Bytes()
	sb := s.Bytes()
	sig := make([]byte, 64)
	copy(sig[32-len(rb):32], rb)
	copy(sig[64-len(sb):64], sb)
	return base64.StdEncoding.EncodeToString(sig)
}

func newGetRequest(path string) (*http.Request, error) {
	return http.NewRequest(http.MethodGet, path, nil)
}

// ----------------------------------------------------------------
// Tests: VerifySignature
// ----------------------------------------------------------------

func TestVerifySignature_Valid(t *testing.T) {
	priv, pubB64 := generateTestKeyPair(t)
	nonce  := "test-nonce-abc123"
	sigB64 := rawSign(t, priv, nonce)

	ok, err := VerifySignature(pubB64, nonce, sigB64)
	if err != nil {
		t.Fatalf("VerifySignature returned error: %v", err)
	}
	if !ok {
		t.Error("expected VerifySignature to return true for a valid signature")
	}
}

func TestVerifySignature_WrongNonce(t *testing.T) {
	priv, pubB64 := generateTestKeyPair(t)
	sigB64 := rawSign(t, priv, "correct-nonce")

	ok, err := VerifySignature(pubB64, "different-nonce", sigB64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected VerifySignature to return false for wrong nonce")
	}
}

func TestVerifySignature_WrongKey(t *testing.T) {
	priv, _         := generateTestKeyPair(t)
	_, otherPubB64  := generateTestKeyPair(t)
	nonce           := "nonce-xyz"
	sigB64          := rawSign(t, priv, nonce)

	ok, err := VerifySignature(otherPubB64, nonce, sigB64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected VerifySignature to return false for mismatched key")
	}
}

func TestVerifySignature_InvalidPublicKey(t *testing.T) {
	_, err := VerifySignature("not-valid-base64!!!", "nonce", "sig")
	if err == nil {
		t.Error("expected error for invalid public key base64")
	}
}

func TestVerifySignature_InvalidSignatureLength(t *testing.T) {
	_, pubB64 := generateTestKeyPair(t)
	badSig    := base64.StdEncoding.EncodeToString([]byte("tooshort"))
	_, err    := VerifySignature(pubB64, "nonce", badSig)
	if err == nil {
		t.Error("expected error for signature with wrong byte length")
	}
}

func TestVerifySignature_TamperedSignature(t *testing.T) {
	priv, pubB64 := generateTestKeyPair(t)
	nonce        := "nonce-tampered"
	sigB64       := rawSign(t, priv, nonce)

	sigBytes, _ := base64.StdEncoding.DecodeString(sigB64)
	sigBytes[0] ^= 0xFF
	tamperedB64  := base64.StdEncoding.EncodeToString(sigBytes)

	ok, _ := VerifySignature(pubB64, nonce, tamperedB64)
	if ok {
		t.Error("expected VerifySignature to return false for tampered signature")
	}
}

// ----------------------------------------------------------------
// Tests: LoadDevices / SaveDevices
// ----------------------------------------------------------------

func TestSaveAndLoadDevices(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	devices := map[string]Device{
		"key1": {
			Name:      "My Phone",
			PublicKey: "key1",
			Status:    "approved",
			CreatedAt: time.Now().UTC().Round(time.Second),
		},
		"key2": {
			Name:      "Work Laptop",
			PublicKey: "key2",
			Status:    "pending",
			CreatedAt: time.Now().UTC().Round(time.Second),
		},
	}

	if err := SaveDevices(devices); err != nil {
		t.Fatalf("SaveDevices returned error: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, ".share_devices.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("expected devices file at %s, not found", expectedPath)
	}

	loaded, err := LoadDevices()
	if err != nil {
		t.Fatalf("LoadDevices returned error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(loaded))
	}
	for key, orig := range devices {
		got, ok := loaded[key]
		if !ok {
			t.Errorf("device with key %q not found after reload", key)
			continue
		}
		if got.Name != orig.Name {
			t.Errorf("device %q: Name=%q, want %q", key, got.Name, orig.Name)
		}
		if got.Status != orig.Status {
			t.Errorf("device %q: Status=%q, want %q", key, got.Status, orig.Status)
		}
	}
}

func TestLoadDevices_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	devices, err := LoadDevices()
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(devices))
	}
}

func TestLoadDevices_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	os.WriteFile(filepath.Join(tmpDir, ".share_devices.json"), []byte("not json"), 0600)

	_, err := LoadDevices()
	if err == nil {
		t.Error("expected error for corrupt devices file, got nil")
	}
}

func TestSaveDevices_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	devices := map[string]Device{
		"k": {Name: "Dev", PublicKey: "k", Status: "approved"},
	}
	if err := SaveDevices(devices); err != nil {
		t.Fatalf("SaveDevices error: %v", err)
	}

	info, err := os.Stat(filepath.Join(tmpDir, ".share_devices.json"))
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}
}

// ----------------------------------------------------------------
// Tests: JSON round-trip integrity
// ----------------------------------------------------------------

func TestDevice_JSONRoundTrip(t *testing.T) {
	original := Device{
		Name:      "iPad Pro",
		PublicKey: "base64encodedkey==",
		Status:    "approved",
		CreatedAt: time.Now().UTC().Round(time.Second),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded Device
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.PublicKey != original.PublicKey {
		t.Error("PublicKey mismatch")
	}
	if decoded.Status != original.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, original.Status)
	}
}

// ----------------------------------------------------------------
// Tests: Session validation helper
// ----------------------------------------------------------------

func TestIsSessionValid(t *testing.T) {
	s := New(".", 8080)

	// No cookie → invalid
	req1, _ := newGetRequest("/api/files")
	if s.isSessionValid(req1) {
		t.Error("expected isSessionValid=false for request without cookie")
	}

	// Inject a valid session
	token := "valid-token-xyz"
	s.Mu.Lock()
	s.Sessions[token] = "some-device-key"
	s.Mu.Unlock()

	req2, _ := newGetRequest("/api/files")
	req2.AddCookie(&http.Cookie{Name: "share_session", Value: token})
	if !s.isSessionValid(req2) {
		t.Error("expected isSessionValid=true for valid session cookie")
	}

	// Wrong token
	req3, _ := newGetRequest("/api/files")
	req3.AddCookie(&http.Cookie{Name: "share_session", Value: "wrong-token"})
	if s.isSessionValid(req3) {
		t.Error("expected isSessionValid=false for invalid token")
	}
}

// ----------------------------------------------------------------
// Tests: ECDSA big-int parsing (low-level math verification)
// ----------------------------------------------------------------

func TestVerifySignature_BigIntParsing(t *testing.T) {
	priv, pubB64 := generateTestKeyPair(t)
	nonce        := "bigint-test-nonce"
	sigB64       := rawSign(t, priv, nonce)
	sigBytes, _  := base64.StdEncoding.DecodeString(sigB64)

	if len(sigBytes) != 64 {
		t.Fatalf("rawSign produced %d bytes, want 64", len(sigBytes))
	}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	if r.Sign() == 0 || s.Sign() == 0 {
		t.Error("expected non-zero r and s in signature")
	}

	ok, err := VerifySignature(pubB64, nonce, sigB64)
	if err != nil || !ok {
		t.Errorf("VerifySignature failed: ok=%v err=%v", ok, err)
	}
}
