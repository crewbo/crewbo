package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/claims"
	"github.com/crewbo/crewbo/web"
)

// The dashboard (M5-06) is a read-only page the daemon serves on
// 127.0.0.1 (threat-model.md: a page in the member's browser must not be
// able to read it). It starts on the first `crewbo dashboard` and stops
// with the daemon. Every API call needs the token from the address the
// command prints; Host is checked against DNS rebinding and Origin
// against other pages.

// dashboardState is what /api/state returns, in the shapes the local API
// uses, so the page and the VS Code extension read the same data.
type dashboardState struct {
	Status Status          `json:"status"`
	Tasks  []Task          `json:"tasks"`
	Repos  []dashboardRepo `json:"repos"`
}

type dashboardRepo struct {
	Root     string         `json:"root"`
	Room     string         `json:"room,omitempty"`
	Claims   []claims.Claim `json:"claims"`
	Handoffs []Handoff      `json:"handoffs"`
}

// dashboard returns the dashboard's address, starting it if needed.
func (d *Daemon) dashboard(ctx context.Context, req Request) Response {
	d.webMu.Lock()
	defer d.webMu.Unlock()
	if d.webURL != "" {
		return Response{ID: req.ID, URL: d.webURL}
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return Response{ID: req.ID, Error: fmt.Sprintf("dashboard: %v", err)}
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	token, host := hex.EncodeToString(b), ln.Addr().String()
	srv := &http.Server{
		Handler:           d.dashboardHandler(host, token),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	d.web.Add(1)
	go func() {
		defer d.web.Done()
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			d.cfg.Log.Warn("dashboard stopped", "err", err)
		}
	}()
	context.AfterFunc(ctx, func() { _ = srv.Close() })
	d.webURL = "http://" + host + "/#token=" + token
	d.cfg.Log.Info("dashboard started", "address", "http://"+host)
	return Response{ID: req.ID, URL: d.webURL}
}

// dashboardHandler serves the page to anyone on this host and the state
// only with the token.
func (d *Daemon) dashboardHandler(host, token string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		st, err := d.dashboardState(r.Context())
		if err != nil {
			http.Error(w, "the daemon could not read its state", http.StatusInternalServerError)
			d.cfg.Log.Warn("dashboard state", "err", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Another name for this address (DNS rebinding) is refused.
		if r.Host != host {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
				http.Error(w, "missing or wrong token", http.StatusUnauthorized)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// dashboardState gathers the status, tasks, and each repository's claims
// and (in a room) handoff steps.
func (d *Daemon) dashboardState(ctx context.Context) (dashboardState, error) {
	var st dashboardState
	repos := map[string]*dashboardRepo{}
	var order []string
	err := d.onOwner(ctx, func() {
		st.Status = *d.handle(Request{Method: MethodStatus}).Status
		now := d.cfg.Now()
		for key, set := range d.claims {
			if active := set.Active(now); len(active) > 0 {
				repos[key] = &dashboardRepo{Root: key, Claims: active, Handoffs: []Handoff{}}
				order = append(order, key)
			}
		}
	})
	if err != nil {
		return st, err
	}
	for _, b := range d.sync.bindings() {
		key := RepoKey(b.Repo)
		r, ok := repos[key]
		if !ok {
			r = &dashboardRepo{Claims: []claims.Claim{}}
			repos[key] = r
			order = append(order, key)
		}
		r.Root, r.Room = b.Repo, b.Name
		if r.Handoffs, err = d.listHandoffs(ctx, b); err != nil {
			return st, err
		}
		if r.Handoffs == nil {
			r.Handoffs = []Handoff{}
		}
	}
	tl := d.listTasks(ctx, Request{})
	if tl.Error != "" {
		return st, errors.New(tl.Error)
	}
	st.Tasks = tl.Tasks
	st.Repos = []dashboardRepo{}
	slices.Sort(order) // map order is random; keep the page still
	for _, key := range order {
		st.Repos = append(st.Repos, *repos[key])
	}
	return st, nil
}
