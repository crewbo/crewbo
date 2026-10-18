package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crewbo/crewbo/internal/auth"
	"github.com/crewbo/crewbo/internal/server"
)

const deviceUsage = `Usage:
  crewbo device list
  crewbo device revoke KEY-ID

list shows the device keys registered to your GitHub account on the sync
server you are signed in to. Each computer you sign in on has its own key,
which signs the tasks you send as a room lead.

revoke retires the key of a lost or replaced computer for good: teammates'
daemons reject tasks signed with it, and the server refuses connections
that present it. It also signs you out everywhere, this computer included;
run crewbo login again on each computer you still use. KEY-ID may be any
unique start of a key id. Revoke from another computer: this computer's
own key cannot be revoked from here. See docs/runbooks/device-key-rotation.md.
`

// runDevice implements `crewbo device` (M5-10, threat-model.md: a stolen
// device key).
func runDevice(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "list" || len(args) != 1) && (args[0] != "revoke" || len(args) != 2) {
		return write(stderr, ExitUsage, "%s", deviceUsage)
	}
	p, err := paths()
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	sess, err := auth.LoadSession(auth.SessionPath(p.Config))
	if err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	c := &http.Client{Timeout: 30 * time.Second}
	var devices []server.Device
	if err := callServer(ctx, c, sess.Server, sess.SessionToken, http.MethodGet, "/v1/devices", nil, &devices); err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	if args[0] == "list" {
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "KEY\tNAME\tOS\tADDED\tSTATE")
		for _, d := range devices {
			state := "active"
			switch {
			case d.RevokedUnixMs != 0:
				state = "revoked " + time.UnixMilli(d.RevokedUnixMs).Local().Format("2006-01-02 15:04")
			case d.KeyID == sess.DeviceKeyID:
				state = "active, this computer"
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", d.KeyID, cell(d.Name), cell(d.OS), time.UnixMilli(d.CreatedUnixMs).Local().Format("2006-01-02"), state)
		}
		if err := tw.Flush(); err != nil {
			return ExitError
		}
		return ExitOK
	}

	want := args[len(args)-1] // revoke has exactly one argument (checked above)
	var match []server.Device
	for _, d := range devices {
		if strings.HasPrefix(d.KeyID, want) {
			match = append(match, d)
		}
	}
	switch {
	case want == "" || len(match) == 0:
		return write(stderr, ExitError, "crewbo: no device key %s is registered to you; see crewbo device list\n", want)
	case len(match) > 1:
		return write(stderr, ExitError, "crewbo: %s matches %d keys; give more of the key id\n", want, len(match))
	case match[0].KeyID == sess.DeviceKeyID:
		return write(stderr, ExitError, "crewbo: %s is this computer's own key; revoke it from another computer you are signed in on\n", match[0].KeyID)
	case match[0].RevokedUnixMs != 0:
		return write(stdout, ExitOK, "Key %s (%s) is already revoked.\n", match[0].KeyID, cell(match[0].Name))
	}
	d := match[0]
	if err := callServer(ctx, c, sess.Server, sess.SessionToken, http.MethodPost, "/v1/devices/"+d.KeyID+"/revoke", nil, nil); err != nil {
		return write(stderr, ExitError, "crewbo: %v\n", err)
	}
	return write(stdout, ExitOK, "Revoked key %s (%s, %s). Teammates' daemons now reject tasks signed with it.\n"+
		"You are signed out everywhere, this computer included: run crewbo login here and on each computer you still use.\n",
		d.KeyID, cell(d.Name), cell(d.OS))
}
