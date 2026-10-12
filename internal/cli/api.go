package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// callServer sends in as JSON to base+path with the session token, if any,
// and decodes a 200 answer into out (nil: ignore the body). A non-2xx
// answer becomes an error carrying the server's message.
func callServer(ctx context.Context, c *http.Client, base, token, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the sync server at %s: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &serverError{Status: resp.StatusCode, Msg: strings.TrimSpace(string(msg))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out); err != nil {
		return fmt.Errorf("the sync server sent an unexpected answer: %w", err)
	}
	return nil
}

// serverError is a refusal from the sync server.
type serverError struct {
	Status int
	Msg    string
}

func (e *serverError) Error() string {
	return fmt.Sprintf("the sync server said: %s (%d %s)", e.Msg, e.Status, http.StatusText(e.Status))
}
