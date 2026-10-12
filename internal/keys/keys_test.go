package keys

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// fakeKeyring is an in-memory keychain whose calls can fail or hang.
type fakeKeyring struct {
	mu      sync.Mutex
	secrets map[string]string
	getErr  error
	setErr  error
	hang    chan struct{} // Get and Set block until closed when non-nil
}

func (f *fakeKeyring) wait() {
	if f.hang != nil {
		<-f.hang
	}
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	s, ok := f.secrets[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return s, nil
}

func (f *fakeKeyring) Set(service, user, secret string) error {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	f.secrets[service+"/"+user] = secret
	return nil
}

const short = 50 * time.Millisecond

func TestKeychainFirstUseAndReuse(t *testing.T) {
	kr, dir := &fakeKeyring{}, t.TempDir()
	k1, err := Load(kr, dir, short)
	if err != nil || k1.Source != SourceKeychain {
		t.Fatalf("first Load = %v %v", k1.Source, err)
	}
	k2, err := Load(kr, dir, short)
	if err != nil || !k1.Private.Equal(k2.Private) || k2.Source != SourceKeychain {
		t.Fatalf("second Load returned a different key or source: %v %v", k2.Source, err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("seed file written although the keychain worked")
	}
	if !strings.HasPrefix(k1.ID(), "ed25519:") || len(k1.ID()) != len("ed25519:")+32 || k1.ID() != KeyID(k2.Public()) {
		t.Errorf("ID = %q", k1.ID())
	}
	msg := []byte("task body")
	if !ed25519.Verify(k2.Public(), msg, ed25519.Sign(k1.Private, msg)) {
		t.Error("signature from the reloaded key does not verify")
	}
}

func TestFallbackToFile(t *testing.T) {
	cases := map[string]*fakeKeyring{
		"keychain errors":      {getErr: errors.New("org.freedesktop.secrets not provided")},
		"keychain read hangs":  {hang: make(chan struct{})},
		"keychain write fails": {setErr: errors.New("locked")},
	}
	for name, kr := range cases {
		t.Run(name, func(t *testing.T) {
			if kr.hang != nil {
				t.Cleanup(func() { close(kr.hang) })
			}
			dir := t.TempDir()
			start := time.Now()
			k, err := Load(kr, dir, short)
			if err != nil || k.Source != SourceFile || k.Path != filepath.Join(dir, FileName) {
				t.Fatalf("Load = %+v %v", k.Source, err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("Load took %v with a %v timeout", d, short)
			}
			if runtime.GOOS != "windows" {
				if fi, err := os.Stat(k.Path); err != nil || fi.Mode().Perm() != 0o600 {
					t.Errorf("seed file mode %v %v", fi.Mode().Perm(), err)
				}
			}
			// The file wins from now on, even once the keychain works.
			again, err := Load(&fakeKeyring{}, dir, short)
			if err != nil || !again.Private.Equal(k.Private) || again.Source != SourceFile {
				t.Errorf("reload after fallback = %v %v", again.Source, err)
			}
		})
	}
}

// Set hangs past the timeout: the file is used, and stays the key even if
// the late Set lands in the keychain afterwards.
func TestKeychainWriteHangs(t *testing.T) {
	kr, dir := &fakeKeyring{}, t.TempDir()
	block := make(chan struct{})
	slow := &slowSet{fakeKeyring: kr, block: block}
	k, err := Load(slow, dir, short)
	if err != nil || k.Source != SourceFile {
		t.Fatalf("Load = %v %v", k.Source, err)
	}
	close(block) // the late Set now completes
	time.Sleep(20 * time.Millisecond)
	again, err := Load(kr, dir, short)
	if err != nil || !again.Private.Equal(k.Private) {
		t.Errorf("key changed after a late keychain write: %v", err)
	}
}

type slowSet struct {
	*fakeKeyring
	block chan struct{}
}

func (s *slowSet) Set(service, user, secret string) error {
	<-s.block
	return s.fakeKeyring.Set(service, user, secret)
}

func TestDamagedSeedsAreErrorsNotReplaced(t *testing.T) {
	kr := &fakeKeyring{secrets: map[string]string{keyringService + "/" + keyringUser: "not base64!"}}
	dir := t.TempDir()
	if _, err := Load(kr, dir, short); err == nil || !strings.Contains(err.Error(), "crewbo/device-key") {
		t.Errorf("damaged keychain entry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a new key was written over a damaged keychain entry")
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("c2hvcnQ="), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(&fakeKeyring{}, dir, short); err == nil || !strings.Contains(err.Error(), FileName) {
		t.Errorf("damaged seed file: %v", err)
	}
}

// Two processes falling back at the same time end up with one key.
func TestConcurrentFallbackAgrees(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	keys := make([]Key, 8)
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, err := Load(&fakeKeyring{getErr: errors.New("no secret service")}, dir, short)
			if err != nil {
				t.Error(err)
			}
			keys[i] = k
		}()
	}
	wg.Wait()
	for _, k := range keys[1:] {
		if k.Private == nil || !k.Private.Equal(keys[0].Private) {
			t.Fatal("concurrent fallbacks produced different keys")
		}
	}
}
