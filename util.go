package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// dotEnv is a minimal .env parser: KEY=VALUE lines, # comments.
func dotEnv(path string) map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			v = strings.Trim(v, `"'`)
			m[k] = v
		}
	}
	return m
}

// deriveKey hashes a passphrase into a 32-byte AES key.
func deriveKey(pass string) []byte {
	h := sha256.Sum256([]byte("dt:" + pass))
	return h[:]
}

// encryptAESGCM encrypts plaintext with key, output: nonce||ciphertext.
func encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptAESGCM decrypts nonce||ciphertext produced by encryptAESGCM.
func decryptAESGCM(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// serveAuth authenticates a token: token == secret, or sha256(token) == secret.
func serveAuth(token, serverSecret string) bool {
	if serverSecret == "" || token == "" {
		return false
	}
	if token == serverSecret {
		return true
	}
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:]) == serverSecret
}

// tokenObfuscate produces hex(nonce||AESGCM(sha256(pass), token)) so the
// shared config never carries the plain master secret.
func tokenObfuscate(pass, token string) (string, error) {
	enc, err := encryptAESGCM(deriveKey(pass), []byte(token))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(enc), nil
}

// tokenDeobfuscate reverses tokenObfuscate.
func tokenDeobfuscate(pass, s string) (string, error) {
	data, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	pt, err := decryptAESGCM(deriveKey(pass), data)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// safeHost normalizes a subdomain label: lowercase [a-z0-9-], 1..62 chars.
func safeHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 62 {
		out = out[:62]
	}
	return out
}

// configDir returns ~/.dreamtunnel.
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".dreamtunnel")
}

// joinHome joins paths under the user's home directory.
func joinHome(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

// writeFileAtomic writes data to path via temp file + rename.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// platformTag returns OS/arch for logs.
func platformTag() string { return runtime.GOOS + "/" + runtime.GOARCH }
