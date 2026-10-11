package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	crewbov1 "github.com/crewbo/crewbo/internal/protocol/crewbo/v1"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testServer(t *testing.T, s *Server) string {
	t.Helper()
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/ws"
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, url, nil)
	closeBody(resp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func sendRaw(t *testing.T, c *websocket.Conn, typ websocket.MessageType, b []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, typ, b); err != nil {
		t.Fatal(err)
	}
}

func sendEnv(t *testing.T, c *websocket.Conn, env *crewbov1.Envelope) {
	t.Helper()
	b, err := protojson.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	sendRaw(t, c, websocket.MessageText, b)
}

// recv returns the next Envelope, or nil and the error once closed.
func recv(t *testing.T, c *websocket.Conn) (*crewbov1.Envelope, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	env := &crewbov1.Envelope{}
	if err := protojson.Unmarshal(b, env); err != nil {
		t.Fatalf("server sent an invalid envelope %s: %v", b, err)
	}
	return env, nil
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func hello(v int32) *crewbov1.Envelope {
	return &crewbov1.Envelope{Id: "h1", Body: &crewbov1.Envelope_Hello{Hello: &crewbov1.Hello{ProtocolVersion: v}}}
}

func TestHealthz(t *testing.T) {
	ts := httptest.NewServer(New(quiet()).Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(b) != "ok\n" {
		t.Errorf("healthz = %d %q", resp.StatusCode, b)
	}
}

func TestHelloWelcomeThenUnsupported(t *testing.T) {
	c := dial(t, testServer(t, New(quiet())))
	// Unknown fields from a newer daemon are ignored.
	sendRaw(t, c, websocket.MessageText, []byte(`{"id":"h1","hello":{"protocolVersion":1,"futureField":"x"},"futureBody":{}}`))
	env, err := recv(t, c)
	if err != nil || env.GetWelcome().GetProtocolVersion() != ProtocolVersion {
		t.Fatalf("want Welcome, got %v %v", env, err)
	}
	sendEnv(t, c, &crewbov1.Envelope{Id: "r1", Body: &crewbov1.Envelope_ClaimRequest{ClaimRequest: &crewbov1.ClaimRequest{}}})
	env, err = recv(t, c)
	if e := env.GetError(); err != nil || e.GetCode() != crewbov1.ErrorCode_ERROR_CODE_UNSUPPORTED || e.GetRequestId() != "r1" {
		t.Fatalf("want UNSUPPORTED for r1, got %v %v", env, err)
	}
}

func TestRejectsBadFirstFrames(t *testing.T) {
	for name, tc := range map[string]struct {
		send   func(*websocket.Conn)
		code   crewbov1.ErrorCode // expected Error, or 0 for none
		status websocket.StatusCode
	}{
		"not hello": {
			send: func(c *websocket.Conn) {
				sendEnv(t, c, &crewbov1.Envelope{Id: "x", Body: &crewbov1.Envelope_Ack{Ack: &crewbov1.Ack{}}})
			},
			code:   crewbov1.ErrorCode_ERROR_CODE_INVALID,
			status: websocket.StatusPolicyViolation,
		},
		"old version": {
			send:   func(c *websocket.Conn) { sendEnv(t, c, hello(0)) },
			code:   crewbov1.ErrorCode_ERROR_CODE_UPGRADE_REQUIRED,
			status: websocket.StatusPolicyViolation,
		},
		"binary frame": {
			send:   func(c *websocket.Conn) { sendRaw(t, c, websocket.MessageBinary, []byte{1}) },
			status: websocket.StatusUnsupportedData,
		},
		"malformed json": {
			send:   func(c *websocket.Conn) { sendRaw(t, c, websocket.MessageText, []byte(`{"hello":`)) },
			status: websocket.StatusPolicyViolation,
		},
		"oversized": {
			send: func(c *websocket.Conn) {
				sendRaw(t, c, websocket.MessageText, []byte(`{"id":"`+strings.Repeat("a", maxEnvelope)+`"}`))
			},
			status: websocket.StatusMessageTooBig,
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := dial(t, testServer(t, New(quiet())))
			tc.send(c)
			env, err := recv(t, c)
			if tc.code != 0 {
				if env.GetError().GetCode() != tc.code || env.GetError().GetRequestId() == "" {
					t.Fatalf("want Error %v with the request id, got %v %v", tc.code, env, err)
				}
				_, err = recv(t, c)
			}
			if got := websocket.CloseStatus(err); got != tc.status {
				t.Errorf("close status = %v (%v), want %v", got, err, tc.status)
			}
		})
	}
}

// The library's default read limit is 32 KiB; the protocol allows 64 KiB.
func TestAcceptsLargeEnvelope(t *testing.T) {
	c := dial(t, testServer(t, New(quiet())))
	big := hello(1)
	big.Id = strings.Repeat("a", 60<<10)
	sendEnv(t, c, big)
	if env, err := recv(t, c); err != nil || env.GetWelcome() == nil {
		t.Fatalf("60 KiB Hello: %v %v", env, err)
	}
}

func TestHelloTimeout(t *testing.T) {
	s := New(quiet())
	s.HelloWait = 50 * time.Millisecond
	c := dial(t, testServer(t, s))
	if _, err := recv(t, c); err == nil {
		t.Fatal("connection without Hello stayed open")
	}
}

func TestKeepalive(t *testing.T) {
	s := New(quiet())
	// Wide margins: Windows sleeps in ~15.6 ms steps and CI runs with -race.
	s.PingEvery, s.PongWait = 20*time.Millisecond, 300*time.Millisecond

	// A daemon that reads answers pings and stays connected.
	alive := dial(t, testServer(t, s))
	sendEnv(t, alive, hello(1))
	if env, err := recv(t, alive); err != nil || env.GetWelcome() == nil {
		t.Fatalf("welcome: %v %v", env, err)
	}
	for i := range 15 { // 600 ms, past PongWait; each Read answers pings
		time.Sleep(40 * time.Millisecond)
		id := "r" + strconv.Itoa(i)
		sendEnv(t, alive, &crewbov1.Envelope{Id: id, Body: &crewbov1.Envelope_ClaimRenew{ClaimRenew: &crewbov1.ClaimRenew{}}})
		if env, err := recv(t, alive); err != nil || env.GetError().GetRequestId() != id {
			t.Fatalf("connection dropped while reading: %v %v", env, err)
		}
	}

	// A daemon that stops reading never pongs and is closed.
	dead := dial(t, testServer(t, s))
	sendEnv(t, dead, hello(1))
	time.Sleep(900 * time.Millisecond) // no Read, so no pong within PongWait
	if env, err := recv(t, dead); err != nil || env.GetWelcome() == nil {
		t.Fatalf("welcome: %v %v", env, err)
	}
	if _, err := recv(t, dead); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("silent daemon: %v, want close %v", err, websocket.StatusPolicyViolation)
	}
}

func TestRunLoopbackRule(t *testing.T) {
	// A deadline so a broken rule fails the test instead of serving forever.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := New(quiet()).Run(ctx, Config{Addr: "0.0.0.0:0"}, nil)
	if !errors.Is(err, ErrInsecure) {
		t.Errorf("plain HTTP on all interfaces: %v, want ErrInsecure", err)
	}
	if err := New(quiet()).Run(ctx, Config{Addr: "127.0.0.1:0", CertFile: "c.pem"}, nil); err == nil {
		t.Error("cert without key accepted")
	}
	// --insecure-lan allows it.
	runAndStop(t, Config{Addr: "0.0.0.0:0", InsecureLAN: true}, "http", nil)
}

func TestRunTLSAndShutdownWithOpenConnection(t *testing.T) {
	cert, key, pool := selfSigned(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	runAndStop(t, Config{Addr: "127.0.0.1:0", CertFile: cert, KeyFile: key}, "https", client)
}

// runAndStop starts Run, checks /healthz and a WebSocket handshake, then
// cancels and expects Run to return promptly with the connection open.
func runAndStop(t *testing.T, cfg Config, scheme string, client *http.Client) {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- New(quiet()).Run(ctx, cfg, func(a net.Addr) { addrs <- a }) }()
	var addr net.Addr
	select {
	case addr = <-addrs:
	case err := <-done:
		t.Fatalf("Run: %v", err)
	}
	port := addr.(*net.TCPAddr).Port
	base := scheme + "://127.0.0.1:" + strconv.Itoa(port)
	resp, err := client.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
	dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
	defer dcancel()
	c, wsResp, err := websocket.Dial(dctx, "ws"+strings.TrimPrefix(base, "http")+"/v1/ws", &websocket.DialOptions{HTTPClient: client})
	closeBody(wsResp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	sendEnv(t, c, hello(1))
	if env, err := recv(t, c); err != nil || env.GetWelcome() == nil {
		t.Fatalf("welcome: %v %v", env, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run after cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return with a WebSocket open")
	}
}

func selfSigned(t *testing.T) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "crewbo test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(c)
	return certFile, keyFile, pool
}
