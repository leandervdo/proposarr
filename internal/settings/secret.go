package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SecretKeyEnv names the variable that supplies the key for secrets at rest.
const SecretKeyEnv = "PROPOSARR_SECRET_KEY"

const keyFileName = "secret.key"

// ErrNoKey means neither PROPOSARR_SECRET_KEY nor a key file is available.
var ErrNoKey = errors.New("no secret key: set " + SecretKeyEnv + " or start `proposarr serve` once to create one")

// LoadKey returns the 32-byte key for secrets at rest: PROPOSARR_SECRET_KEY
// (base64 or hex of 32 bytes; any other string is hashed with SHA-256), else
// <dataDir>/secret.key. When create is set and neither exists, a random key is
// written to that file with mode 0600.
func LoadKey(dataDir string, getenv func(string) string, create bool) ([]byte, error) {
	if v := strings.TrimSpace(getenv(SecretKeyEnv)); v != "" {
		return parseKey(v), nil
	}
	path := filepath.Join(dataDir, keyFileName)
	if key, err := readKeyFile(path); err == nil {
		return key, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if !create {
		return nil, ErrNoKey
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return readKeyFile(path) // another process created it first
	}
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	_, werr := f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return nil, fmt.Errorf("secret key: %w", werr)
	}
	return key, nil
}

func readKeyFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("secret key: %s is not a base64-encoded 32-byte key", path)
	}
	return key, nil
}

func parseKey(s string) []byte {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b
		}
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b
	}
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

// Cipher encrypts secrets with AES-256-GCM. The setting key is bound as
// additional data, so a ciphertext cannot be moved to another setting.
type Cipher struct{ aead cipher.AEAD }

const cipherPrefix = "v1:"

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(name, plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), []byte(name))
	return cipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(name, value string) (string, error) {
	raw, ok := strings.CutPrefix(value, cipherPrefix)
	if !ok {
		return "", errors.New("unknown secret format")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) < c.aead.NonceSize() {
		return "", errors.New("malformed secret")
	}
	plain, err := c.aead.Open(nil, b[:c.aead.NonceSize()], b[c.aead.NonceSize():], []byte(name))
	if err != nil {
		return "", errors.New("secret does not decrypt with the current key")
	}
	return string(plain), nil
}
