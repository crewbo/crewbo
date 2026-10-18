package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/crewbo/crewbo/internal/daemon"
)

// apiCallTimeout bounds one request; claim requests wait up to 5 s for
// the sync server.
const apiCallTimeout = 30 * time.Second

// runAPI speaks the daemon's local API on stdin and stdout, one JSON
// request per line answered by one JSON response per line, for editor
// extensions (M5-05). Node cannot open an AF_UNIX socket on Windows, and
// this keeps the socket and token paths in one place. It exits when stdin
// closes or the daemon goes away; the caller starts it again.
//
// Approving a task is refused here: approval stays in `crewbo task
// approve`, in a real terminal, after the member reads the full text
// (ADR 0006, #114), so an agent cannot approve by piping one line in.
func runAPI(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	c, err := daemon.Dial(dctx, p)
	cancel()
	if err != nil {
		return write(stderr, ExitError, "crewbo: the daemon is not running; start it with `crewbo daemon`\n")
	}
	defer func() { _ = c.Close() }()

	in := bufio.NewScanner(stdin)
	in.Buffer(make([]byte, 64*1024), 1<<20)
	out := json.NewEncoder(stdout)
	for in.Scan() {
		var req daemon.Request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			return write(stderr, ExitError, "crewbo: not a JSON request: %v\n", err)
		}
		id := req.ID
		var resp daemon.Response
		if req.Method == daemon.MethodTaskApprove {
			resp.Error = fmt.Sprintf("approve in a terminal with `crewbo task approve %s`", req.Task)
		} else {
			cctx, cancel := context.WithTimeout(ctx, apiCallTimeout)
			resp, err = c.Call(cctx, req)
			cancel()
			if err != nil && resp.Error == "" {
				return write(stderr, ExitError, "crewbo: the daemon stopped answering: %v\n", err)
			}
		}
		resp.ID = id // Call numbers its own requests
		if err := out.Encode(resp); err != nil {
			return ExitError
		}
	}
	if err := in.Err(); err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return ExitOK
}
