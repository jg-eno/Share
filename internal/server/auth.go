package server

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Device represents a registered device profile
type Device struct {
	Name      string    `json:"name"`
	PublicKey string    `json:"publicKey"` // base64 SPKI DER public key
	Status    string    `json:"status"`    // "pending", "approved", "rejected"
	CreatedAt time.Time `json:"createdAt"`
}

func getDevicesFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".share_devices.json"
	}
	return filepath.Join(home, ".share_devices.json")
}

// LoadDevices loads the registered devices from user home directory
func LoadDevices() (map[string]Device, error) {
	path := getDevicesFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return make(map[string]Device), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read devices file: %v", err)
	}

	var devices []Device
	if err := json.Unmarshal(data, &devices); err != nil {
		return nil, fmt.Errorf("failed to parse devices file: %v", err)
	}

	deviceMap := make(map[string]Device)
	for _, dev := range devices {
		deviceMap[dev.PublicKey] = dev
	}
	return deviceMap, nil
}

// SaveDevices saves the registered devices to user home directory
func SaveDevices(deviceMap map[string]Device) error {
	path := getDevicesFilePath()
	devices := make([]Device, 0, len(deviceMap))
	for _, dev := range deviceMap {
		devices = append(devices, dev)
	}

	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize devices: %v", err)
	}

	// Create directory if not exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	return os.WriteFile(path, data, 0600)
}

// RevokeDevices removes the given public keys from the device map and persists the result.
func RevokeDevices(keys []string) (int, error) {
	devices, err := LoadDevices()
	if err != nil {
		return 0, fmt.Errorf("failed to load devices: %v", err)
	}

	revoked := 0
	for _, k := range keys {
		if _, ok := devices[k]; ok {
			delete(devices, k)
			revoked++
		}
	}

	if revoked == 0 {
		return 0, nil
	}

	if err := SaveDevices(devices); err != nil {
		return 0, fmt.Errorf("failed to save devices: %v", err)
	}
	return revoked, nil
}

// VerifySignature verifies an ECDSA P-256 signature against a nonce using base64 SPKI public key
func VerifySignature(pubKeyBase64, nonce, signatureBase64 string) (bool, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyBase64)
	if err != nil {
		return false, fmt.Errorf("failed to decode public key: %v", err)
	}

	genericPubKey, err := x509.ParsePKIXPublicKey(pubKeyBytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse PKIX public key: %v", err)
	}

	ecdsaPubKey, ok := genericPubKey.(*ecdsa.PublicKey)
	if !ok {
		return false, fmt.Errorf("public key is not an ECDSA key")
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %v", err)
	}

	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length, expected 64 bytes for raw P-256 signature")
	}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	hash := sha256.Sum256([]byte(nonce))
	return ecdsa.Verify(ecdsaPubKey, hash[:], r, s), nil
}
