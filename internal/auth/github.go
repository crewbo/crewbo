// Package auth is GitHub sign-in for `crewbo login` (ADR 0009): the OAuth
// device flow on the member's machine, and the user lookup the sync server
// makes once to learn who a GitHub token belongs to.
//
// Contracts, verified 2026-10-12:
// https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow
// https://docs.github.com/en/rest/users/users#get-the-authenticated-user
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientID is the Crewbo OAuth app on the crewbo GitHub org (device flow
// enabled, no client secret). Client IDs are public. Self-hosters with
// their own app override it (ADR 0009).
const ClientID = "Ov23liMSnQmA2q3VqyiK"

// Default endpoints.
const (
	GitHubURL = "https://github.com"
	APIURL    = "https://api.github.com"
)

// apiVersion is the REST API version the user lookup was checked against.
const apiVersion = "2026-03-10"

// Errors from Poll that callers report differently.
var (
	ErrExpired = errors.New("auth: the code expired before it was approved; run crewbo login again")
	ErrDenied  = errors.New("auth: sign-in was cancelled on GitHub")
)

// slowDownStep is what GitHub adds to the interval on slow_down.
const slowDownStep = 5 * time.Second

// DeviceFlow runs the GitHub OAuth device flow.
type DeviceFlow struct {
	HTTP     *http.Client
	BaseURL  string // GitHubURL in production
	ClientID string
	// Wait pauses between polls; tests replace it to avoid real sleeps.
	Wait func(ctx context.Context, d time.Duration) error
}

// NewDeviceFlow returns a DeviceFlow against github.com.
func NewDeviceFlow(clientID string) *DeviceFlow {
	return &DeviceFlow{HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: GitHubURL, ClientID: clientID, Wait: sleep}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Code is what the member needs to approve the sign-in.
type Code struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"` // seconds
	Interval        int    `json:"interval"`   // seconds between polls
}

// Start asks GitHub for a device and user code. No scope is requested: the
// public profile is enough to identify the member (ADR 0009).
func (f *DeviceFlow) Start(ctx context.Context) (Code, error) {
	var c Code
	var e oauthError
	if err := f.post(ctx, "/login/device/code", url.Values{"client_id": {f.ClientID}}, &c, &e); err != nil {
		return Code{}, err
	}
	if e.Code != "" {
		return Code{}, e
	}
	if c.DeviceCode == "" || c.UserCode == "" || c.VerificationURI == "" {
		return Code{}, errors.New("auth: GitHub returned an incomplete device code")
	}
	return c, nil
}

// Poll waits for the member to approve c and returns the GitHub token. It
// never polls faster than GitHub's interval, and gives up when the code
// expires. A poll that fails on the network (timeout, dropped connection)
// is tried again at the next interval: one lost request must not end a
// sign-in the member may be approving right now.
func (f *DeviceFlow) Poll(ctx context.Context, c Code) (string, error) {
	interval := time.Duration(c.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second // the documented example; never poll in a tight loop
	}
	if c.ExpiresIn > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.ExpiresIn)*time.Second)
		defer cancel()
	}
	form := url.Values{
		"client_id":   {f.ClientID},
		"device_code": {c.DeviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	var netErr error // the last poll lost on the network
	expired := func() error {
		if netErr != nil {
			return fmt.Errorf("%w (the last request to GitHub failed: %w)", ErrExpired, netErr)
		}
		return ErrExpired
	}
	for {
		if err := f.Wait(ctx, interval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return "", expired()
			}
			return "", err
		}
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		var e oauthError
		if err := f.post(ctx, "/login/oauth/access_token", form, &tok, &e); err != nil {
			if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", expired()
			}
			var transport *url.Error // http.Client.Do failed: no answer arrived
			if errors.As(err, &transport) && ctx.Err() == nil {
				netErr = err
				continue
			}
			return "", err
		}
		switch e.Code {
		case "":
			if tok.AccessToken == "" {
				return "", errors.New("auth: GitHub returned no token")
			}
			return tok.AccessToken, nil
		case "authorization_pending":
		case "slow_down":
			// GitHub sends the new minimum interval; at least add 5 s.
			if e.Interval > 0 {
				interval = time.Duration(e.Interval) * time.Second
			} else {
				interval += slowDownStep
			}
		case "expired_token":
			return "", ErrExpired
		case "access_denied":
			return "", ErrDenied
		default: // device_flow_disabled, incorrect_client_credentials, ...
			return "", e
		}
	}
}

// oauthError is GitHub's error body for the OAuth endpoints. It is read
// whatever the HTTP status, since the docs do not say which status carries
// it.
type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Interval    int    `json:"interval"`
}

func (e oauthError) Error() string {
	if e.Description != "" {
		return "auth: GitHub: " + e.Code + ": " + e.Description
	}
	return "auth: GitHub: " + e.Code
}

// post sends a form and decodes the JSON answer into ok, and its error
// fields into e.
func (f *DeviceFlow) post(ctx context.Context, path string, form url.Values, ok any, e *oauthError) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.BaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // default is form-encoded
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("auth: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("auth: %s: %w", path, err)
	}
	if json.Unmarshal(body, e) == nil && e.Code != "" {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: %s: %s", path, resp.Status)
	}
	if err := json.Unmarshal(body, ok); err != nil {
		return fmt.Errorf("auth: %s: answer is not JSON: %w", path, err)
	}
	return nil
}

// User is a GitHub account.
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// ErrBadToken means GitHub did not accept the token.
var ErrBadToken = errors.New("auth: GitHub did not accept the token")

// LookupUser returns the account a GitHub token belongs to. apiURL is
// APIURL in production.
func LookupUser(ctx context.Context, c *http.Client, apiURL, token string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/user", nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	resp, err := c.Do(req)
	if err != nil {
		return User{}, fmt.Errorf("auth: GitHub user lookup: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return User{}, ErrBadToken
	}
	if resp.StatusCode != http.StatusOK {
		return User{}, fmt.Errorf("auth: GitHub user lookup: %s", resp.Status)
	}
	var u User
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil {
		return User{}, fmt.Errorf("auth: GitHub user lookup: %w", err)
	}
	if u.ID == 0 || u.Login == "" {
		return User{}, errors.New("auth: GitHub user lookup returned no id or login")
	}
	return u, nil
}
