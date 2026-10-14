package cli

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/keys"
	"github.com/crewbo/crewbo/internal/server"
)

const loginUsage = `Usage:
  crewbo login --server URL [--insecure-lan]

Signs in with your GitHub account and saves a Crewbo session for the sync
server at URL. The server must use https, except on this computer
(http://127.0.0.1:...) or with --insecure-lan on a trusted network.

When URL is a new address of the server you are signed in to (a restarted
tunnel, for example), your sign-in and rooms move there without signing
in again. After signing in to a server that already has your rooms, they
move to it too.
`

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _ = write(stderr, ExitUsage, "%s", loginUsage) }
	serverURL := fs.String("server", "", "sync server URL, for example https://crewbo.example.com")
	insecure := fs.Bool("insecure-lan", false, "allow http:// to a server on the local network")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 || *serverURL == "" {
		return write(stderr, ExitUsage, "%s", loginUsage)
	}
	base, err := checkServerURL(*serverURL, *insecure)
	if err != nil {
		return write(stderr, ExitUsage, "crewbo: %v\n", err)
	}
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	var info server.Info
	if err := callServer(ctx, hc, base, "", http.MethodGet, "/v1/info", nil, &info); err != nil || info.ServerID == "" {
		return write(stderr, ExitError, "crewbo: %s does not answer like a Crewbo server: %v\n", base, err)
	}
	// The same server at a new address: move, sending it no credential
	// until its id has matched.
	if prev, err := auth.LoadSession(auth.SessionPath(p.Config)); err == nil && sameServerNewAddress(prev, info, base, time.Now()) {
		old := prev.Server
		prev.Server = base
		if err := auth.SaveSession(auth.SessionPath(p.Config), prev); err != nil {
			return write(stderr, ExitError, "crewbo: save session: %v\n", err)
		}
		n, err := moveBindings(p.Config, base, func(b auth.Binding) bool { return b.Server == old })
		if err != nil {
			return write(stderr, ExitError, "crewbo: move rooms: %v\n", err)
		}
		return write(stdout, ExitOK, "%s is the same Crewbo server as %s. Moved your sign-in and %d room(s) there; no new sign-in needed.\n", base, old, n)
	}
	s, err := login(ctx, auth.NewDeviceFlow(auth.ClientID), hc, base, stdout)
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	key, err := keys.Load(keys.OSKeyring{}, p.Data, keys.Timeout)
	if err != nil {
		return write(stderr, ExitError, "crewbo: device key: %v\n", err)
	}
	if s.DeviceKeyID, err = registerDevice(ctx, &http.Client{Timeout: 30 * time.Second}, base, s.SessionToken, key); err != nil {
		return write(stderr, ExitError, "crewbo: register this device: %v\n", err)
	}
	s.ServerID = info.ServerID
	if err := auth.SaveSession(auth.SessionPath(p.Config), s); err != nil {
		return write(stderr, ExitError, "crewbo: save session: %v\n", err)
	}
	if n, err := moveKnownRooms(ctx, hc, base, s.SessionToken, p.Config); err == nil && n > 0 {
		_ = write(stdout, ExitOK, "Moved %d room(s) to %s.\n", n, base)
	}
	_ = write(stdout, ExitOK, "Signed in to %s as %s. The session is valid until %s.\n",
		s.Server, s.GitHubLogin, time.UnixMilli(s.ExpiresUnixMs).Format("2006-01-02 15:04"))
	if key.Source == keys.SourceFile {
		_ = write(stdout, ExitOK, "No system keychain answered, so this device's key is kept in %s, readable only by you.\n", key.Path)
	}
	return write(stdout, ExitOK, "This device's key %s is registered.\n", s.DeviceKeyID)
}

// checkServerURL accepts https, and http only on loopback or with
// insecureLAN, so a GitHub token never crosses a network in clear text by
// accident. It returns the URL without a trailing slash.
func checkServerURL(raw string, insecureLAN bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("--server must be a URL like https://crewbo.example.com, got %q", raw)
	}
	if u.Scheme == "http" && !insecureLAN {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", fmt.Errorf("refusing to send your sign-in over plain http to %s; use https, or --insecure-lan on a trusted network", host)
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// login runs the device flow, then exchanges the GitHub token for a Crewbo
// session at the server. The GitHub token is not saved.
func login(ctx context.Context, flow *auth.DeviceFlow, c *http.Client, base string, out io.Writer) (auth.Session, error) {
	code, err := flow.Start(ctx)
	if err != nil {
		return auth.Session{}, err
	}
	_, _ = fmt.Fprintf(out, "Open %s and enter the code %s\nWaiting for you to approve the sign-in (Ctrl+C to stop)...\n", code.VerificationURI, code.UserCode)
	githubToken, err := flow.Poll(ctx, code)
	if err != nil {
		return auth.Session{}, err
	}
	var r server.SessionResponse
	if err := callServer(ctx, c, base, "", http.MethodPost, "/v1/auth/session", server.SessionRequest{GitHubToken: githubToken}, &r); err != nil {
		return auth.Session{}, fmt.Errorf("the sync server refused the sign-in: %w", err)
	}
	if r.SessionToken == "" {
		return auth.Session{}, errors.New("the sync server sent an unexpected answer; is --server a Crewbo server?")
	}
	return auth.Session{Server: base, SessionToken: r.SessionToken, GitHubLogin: r.GitHubLogin, ExpiresUnixMs: r.ExpiresUnixMs}, nil
}

// registerDevice registers key with the server for the signed-in member,
// signing keys.RegistrationMessage to prove it holds the private key.
func registerDevice(ctx context.Context, c *http.Client, base, sessionToken string, key keys.Key) (string, error) {
	name, _ := os.Hostname()
	if name == "" || len(name) > 64 {
		name = "device"
	}
	req := server.DeviceRequest{
		PublicKey: key.Public(),
		Signature: ed25519.Sign(key.Private, keys.RegistrationMessage(sessionToken)),
		Name:      name,
		OS:        runtime.GOOS,
	}
	var r server.DeviceResponse
	if err := callServer(ctx, c, base, sessionToken, http.MethodPost, "/v1/devices", req, &r); err != nil {
		return "", fmt.Errorf("the sync server refused the key: %w", err)
	}
	if r.KeyID != key.ID() {
		return "", errors.New("the sync server sent an unexpected answer to the key registration")
	}
	return r.KeyID, nil
}

// sameServerNewAddress reports whether base is a new address of the server
// prev is signed in to: same server id, a different address, and a session
// still valid.
func sameServerNewAddress(prev auth.Session, info server.Info, base string, now time.Time) bool {
	return prev.ServerID != "" && prev.ServerID == info.ServerID && prev.Server != base && now.UnixMilli() < prev.ExpiresUnixMs
}

// moveBindings points the room bindings that match at base and returns how
// many moved.
func moveBindings(configDir, base string, match func(auth.Binding) bool) (int, error) {
	path := auth.BindingsPath(configDir)
	bs, err := auth.LoadBindings(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range bs {
		if match(bs[i]) {
			bs[i].Server = base
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, auth.SaveBindings(path, bs)
}

// moveKnownRooms points at base every room binding of another address
// whose room the server at base lists for this member.
func moveKnownRooms(ctx context.Context, hc *http.Client, base, token, configDir string) (int, error) {
	var rooms []server.RoomInfo
	if err := callServer(ctx, hc, base, token, http.MethodGet, "/v1/rooms", nil, &rooms); err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, r := range rooms {
		known[r.ID] = true
	}
	return moveBindings(configDir, base, func(b auth.Binding) bool { return b.Server != base && known[b.RoomID] })
}
