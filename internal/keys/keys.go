// Package keys holds this device's Ed25519 key (ADR 0006). The private seed
// lives in the OS keychain through github.com/zalando/go-keyring; when the
// keychain fails or does not answer in time, it lives in a file readable
// only by the user instead, and stays there.
package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// Keychain entry for the seed.
const (
	keyringService = "crewbo"
	keyringUser    = "device-key"
)

// Timeout bounds every keychain call. go-keyring blocked for over 60 s when
// D-Bus could not start a secret service (Phase 0 S7, U-18).
const Timeout = 3 * time.Second

// FileName is the fallback seed file inside the data directory.
const FileName = "device-key"

// Where the seed is kept.
const (
	SourceKeychain = "keychain"
	SourceFile     = "file"
)

// Keyring is the OS secret store; OSKeyring is the real one.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
}

// OSKeyring is the system keychain: macOS Keychain, Windows Credential
// Manager, or a Secret Service such as gnome-keyring on Linux.
type OSKeyring struct{}

// Get reads a secret.
func (OSKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

// Set writes a secret.
func (OSKeyring) Set(service, user, secret string) error { return keyring.Set(service, user, secret) }

// Key is this device's signing key.
type Key struct {
	Private ed25519.PrivateKey
	// Source is SourceKeychain or SourceFile.
	Source string
	// Path is the seed file when Source is SourceFile.
	Path string
}

// Public returns the public key.
func (k Key) Public() ed25519.PublicKey { return k.Private.Public().(ed25519.PublicKey) }

// ID is the key's fingerprint: "ed25519:" and the first 16 bytes of the
// SHA-256 of the public key, in hex.
func (k Key) ID() string { return KeyID(k.Public()) }

// RegistrationMessage is what a device signs to prove it holds its key
// when registering it with a sync server. It includes the caller's session
// token, so a signature cannot be replayed by another member.
func RegistrationMessage(sessionToken string) []byte {
	return []byte("crewbo-register-device-v1\n" + sessionToken)
}

// KeyID returns the fingerprint of a public key; see Key.ID.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "ed25519:" + hex.EncodeToString(sum[:16])
}

// Load returns this device's key, creating it on first use. A seed file
// from an earlier fallback wins; otherwise the keychain is asked, each call
// bounded by timeout. If the keychain errors or times out, a new key is
// written to dataDir/device-key (0600) and used from then on.
func Load(kr Keyring, dataDir string, timeout time.Duration) (Key, error) {
	path := filepath.Join(dataDir, FileName)
	if seed, err := os.ReadFile(path); err == nil { //nolint:gosec // our own data directory
		return fromSeed(string(seed), SourceFile, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Key{}, fmt.Errorf("keys: read %s: %w", path, err)
	}
	stored, err := bounded(timeout, func() (string, error) { return kr.Get(keyringService, keyringUser) })
	switch {
	case err == nil:
		// A damaged entry is an error, never silently replaced: a new key
		// would orphan the one teammates trust.
		return fromSeed(stored, SourceKeychain, "")
	case !errors.Is(err, keyring.ErrNotFound):
		return createInFile(path, err)
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return Key{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(seed)
	if _, err := bounded(timeout, func() (string, error) { return "", kr.Set(keyringService, keyringUser, encoded) }); err != nil {
		return createInFile(path, err)
	}
	return Key{Private: ed25519.NewKeyFromSeed(seed), Source: SourceKeychain}, nil
}

// ErrKeychainTimeout means the keychain did not answer within the timeout.
var ErrKeychainTimeout = errors.New("keys: the keychain did not answer in time")

// bounded runs f with a deadline. go-keyring calls take no context, so a
// call that hangs cannot be stopped.
// ponytail: a hung call leaves its goroutine blocked until the keychain
// answers or the process exits; one leak per Load at most.
func bounded(timeout time.Duration, f func() (string, error)) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1) // buffered: a late answer never blocks
	go func() {
		s, err := f()
		ch <- result{s, err}
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-t.C:
		return "", ErrKeychainTimeout
	}
}

func createInFile(path string, why error) (Key, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return Key{}, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Key{}, err
	}
	// Write a complete temporary file, then publish it with a hard link,
	// which fails if the target exists: a process racing on first use
	// either wins or reads the winner's complete file, never a partial one.
	tmp, err := os.CreateTemp(dir, ".device-key-*")
	if err != nil {
		return Key{}, fmt.Errorf("keys: keychain unavailable (%w), and %s cannot be created: %w", why, path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return Key{}, err
	}
	if _, err := tmp.WriteString(base64.StdEncoding.EncodeToString(seed)); err != nil {
		_ = tmp.Close()
		return Key{}, err
	}
	if err := tmp.Close(); err != nil {
		return Key{}, err
	}
	if err := os.Link(tmp.Name(), path); errors.Is(err, os.ErrExist) {
		existing, err := os.ReadFile(path) //nolint:gosec // our own data directory
		if err != nil {
			return Key{}, err
		}
		return fromSeed(string(existing), SourceFile, path)
	} else if err != nil {
		return Key{}, fmt.Errorf("keys: keychain unavailable (%w), and %s cannot be created: %w", why, path, err)
	}
	return Key{Private: ed25519.NewKeyFromSeed(seed), Source: SourceFile, Path: path}, nil
}

func fromSeed(encoded, source, path string) (Key, error) {
	seed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(seed) != ed25519.SeedSize {
		where := "the keychain entry crewbo/device-key"
		if path != "" {
			where = path
		}
		return Key{}, fmt.Errorf("keys: %s does not hold a valid key", where)
	}
	return Key{Private: ed25519.NewKeyFromSeed(seed), Source: source, Path: path}, nil
}
