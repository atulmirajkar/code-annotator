package app

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		want      config
		wantErr   string
		errTarget error
	}{
		{name: "defaults", args: []string{"./docs"}, want: config{rootPath: "./docs"}},
		{name: "options", args: []string{"--port", "8080", "--no-open", "./notes"}, want: config{rootPath: "./notes", port: 8080, noOpen: true}},
		{name: "missing directory", wantErr: "exactly one"},
		{name: "extra directory", args: []string{"one", "two"}, wantErr: "exactly one"},
		{name: "negative port", args: []string{"--port", "-1", "./docs"}, wantErr: "between 0 and 65535"},
		{name: "large port", args: []string{"--port", "65536", "./docs"}, wantErr: "between 0 and 65535"},
		{name: "help", args: []string{"--help"}, errTarget: flag.ErrHelp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got, err := parseConfig(tt.args, &stderr)
			switch {
			case tt.errTarget != nil:
				if !errors.Is(err, tt.errTarget) {
					t.Fatalf("parseConfig() error = %v, want %v", err, tt.errTarget)
				}
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseConfig() error = %v, want containing %q", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("parseConfig() error = %v", err)
				}
				if got != tt.want {
					t.Fatalf("parseConfig() = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestRunServesOnLoopbackAndShutsDown(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "README.md"), []byte("# Running"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := newSyncBuffer()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, []string{"--no-open", rootPath}, stdout, io.Discard)
	}()

	viewerURL := waitForViewerURL(t, stdout)
	if !strings.HasPrefix(viewerURL, "http://127.0.0.1:") {
		t.Fatalf("viewer URL = %q, want loopback URL", viewerURL)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(viewerURL + "healthz")
	if err != nil {
		t.Fatalf("GET healthz error = %v", err)
	}
	response.Body.Close()
	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Fatalf("health status = %d, want %d", got, want)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not shut down after cancellation")
	}
}

func TestRunRejectsInvalidRoot(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), []string{"--no-open", filepath.Join(t.TempDir(), "missing")}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "open Markdown directory") {
		t.Fatalf("Run() error = %v, want invalid root error", err)
	}
}

type syncBuffer struct {
	mu sync.RWMutex
	b  bytes.Buffer
}

func newSyncBuffer() *syncBuffer {
	return &syncBuffer{}
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *syncBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.b.String()
}

func waitForViewerURL(t *testing.T, output *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, field := range strings.Fields(output.String()) {
			if strings.HasPrefix(field, "http://") {
				return strings.TrimSpace(field)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for viewer URL in %q", output.String())
	return ""
}

func Example_parseConfig() {
	configuration, _ := parseConfig([]string{"--port", "8080", "--no-open", "./docs"}, io.Discard)
	fmt.Println(configuration.port, configuration.noOpen, configuration.rootPath)
	// Output: 8080 true ./docs
}
