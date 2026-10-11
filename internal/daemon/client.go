package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crewbo/crewbo/internal/platform"
)

// ErrNotRunning means no daemon answered on the socket.
var ErrNotRunning = errors.New("daemon: not running")

// Client is an authenticated connection to the daemon. It is not safe for
// concurrent use.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	nextID int64
}

// Dial connects and authenticates with the token in paths.Runtime. ctx
// bounds the whole handshake; hooks pass a short deadline so a missing
// daemon fails fast.
func Dial(ctx context.Context, paths platform.Paths) (*Client, error) {
	token, err := os.ReadFile(filepath.Join(paths.Runtime, TokenName))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	conn, err := platform.Dial(ctx, paths.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	hello, _ := json.Marshal(Hello{Token: strings.TrimSpace(string(token))})
	if _, err := conn.Write(append(hello, '\n')); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	return &Client{conn: conn, reader: bufio.NewReader(conn)}, nil
}

// Call sends one request and waits for its response, honouring ctx's
// deadline.
func (c *Client) Call(ctx context.Context, req Request) (Response, error) {
	c.nextID++
	req.ID = c.nextID
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Time{}
	}
	_ = c.conn.SetDeadline(dl)
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		return Response{}, err
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != "" {
		return resp, errors.New("daemon: " + resp.Error)
	}
	return resp, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }
