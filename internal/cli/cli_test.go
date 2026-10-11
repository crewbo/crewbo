package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	info := BuildInfo{Version: "v0.1.0", Commit: "abc1234"}
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no args prints usage", nil, ExitOK, "Usage:", ""},
		{"help", []string{"help"}, ExitOK, "Usage:", ""},
		{"short help flag", []string{"-h"}, ExitOK, "Usage:", ""},
		{"long help flag", []string{"--help"}, ExitOK, "Usage:", ""},
		{"version", []string{"version"}, ExitOK, "crewbo v0.1.0 (commit abc1234)\n", ""},
		{"unknown command", []string{"frobnicate"}, ExitUsage, "", `unknown command "frobnicate"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tt.args, nil, &stdout, &stderr, info)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStdout == "" && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if tt.wantStderr == "" && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestRunWriteFailure(t *testing.T) {
	code := Run(context.Background(), []string{"version"}, nil, failingWriter{}, failingWriter{}, BuildInfo{})
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
}
