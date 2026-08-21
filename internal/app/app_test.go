package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"atulm/md-viewer/internal/annotation"
	annotationstore "atulm/md-viewer/internal/annotation/store"
	"atulm/md-viewer/internal/commands"
	"atulm/md-viewer/internal/content"
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
		{name: "review defaults", args: []string{"--review", "./docs"}, want: config{rootPath: "./docs", review: true}},
		{name: "custom annotations", args: []string{"--review", "--annotations-dir", "./reviews", "./docs"}, want: config{rootPath: "./docs", review: true, annotationsDir: "./reviews"}},
		{name: "annotations without review", args: []string{"--annotations-dir", "./reviews", "./docs"}, wantErr: "requires --review"},
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

func TestOpenAnnotationStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		review  bool
		custom  bool
		invalid bool
		wantNil bool
		wantErr string
	}{
		{name: "disabled", wantNil: true},
		{name: "default under content root", review: true},
		{name: "custom directory", review: true, custom: true},
		{name: "invalid custom directory", review: true, custom: true, invalid: true, wantErr: "open annotation directory"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rootPath := t.TempDir()
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			configuration := config{review: test.review}
			wantRoot := filepath.Join(root.Path(), ".md-viewer", "annotations")
			if test.custom {
				wantRoot = filepath.Join(t.TempDir(), "reviews")
				configuration.annotationsDir = wantRoot
			}
			if test.invalid {
				if err := os.WriteFile(wantRoot, []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}

			got, err := openAnnotationStore(configuration, root)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("openAnnotationStore() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("openAnnotationStore() error = %v", err)
			}
			if test.wantNil {
				if got != nil {
					t.Fatalf("openAnnotationStore() = %#v, want nil", got)
				}
				return
			}
			resolvedWant, err := filepath.EvalSymlinks(wantRoot)
			if err != nil {
				t.Fatalf("EvalSymlinks() error = %v", err)
			}
			if got == nil || got.Root() != resolvedWant {
				t.Fatalf("openAnnotationStore() root = %v, want %q", got, resolvedWant)
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
	var launched atomic.Bool
	go func() {
		result <- run(ctx, []string{"--no-open", rootPath}, stdout, io.Discard, func(string) error {
			launched.Store(true)
			return nil
		})
	}()

	viewerURL := waitForViewerURL(t, stdout)
	if !strings.HasPrefix(viewerURL, "http://127.0.0.1:") {
		t.Fatalf("viewer URL = %q, want loopback URL", viewerURL)
	}
	if launched.Load() {
		t.Fatal("browser launcher was called with --no-open")
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

func TestRunLaunchesReadyServerAndToleratesFailure(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "README.md"), []byte("# Running"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := newSyncBuffer()
	stderr := newSyncBuffer()
	launchedURL := make(chan string, 1)
	result := make(chan error, 1)
	launchError := errors.New("no graphical browser")
	go func() {
		result <- run(ctx, []string{rootPath}, stdout, stderr, func(viewerURL string) error {
			client := &http.Client{Timeout: 2 * time.Second}
			response, err := client.Get(viewerURL + "healthz")
			if err != nil {
				return fmt.Errorf("server was not ready before launch: %w", err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("health status before launch = %d", response.StatusCode)
			}
			launchedURL <- viewerURL
			return launchError
		})
	}()

	var viewerURL string
	select {
	case viewerURL = <-launchedURL:
	case <-time.After(3 * time.Second):
		t.Fatalf("browser launcher was not called; stderr: %s", stderr.String())
	}
	if !strings.HasPrefix(viewerURL, "http://127.0.0.1:") {
		t.Fatalf("launched URL = %q, want loopback URL", viewerURL)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		warning := stderr.String()
		if strings.Contains(warning, launchError.Error()) && strings.Contains(warning, viewerURL) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if warning := stderr.String(); !strings.Contains(warning, launchError.Error()) || !strings.Contains(warning, viewerURL) {
		t.Fatalf("launch warning = %q, want error and manual URL", warning)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not shut down after browser launch failure")
	}
}

func TestRunRejectsInvalidRoot(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), []string{"--no-open", filepath.Join(t.TempDir(), "missing")}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "open Markdown directory") {
		t.Fatalf("Run() error = %v, want invalid root error", err)
	}
}

func TestLiveAgentClientAgainstReviewServer(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "README.md"), []byte("# Review"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	annotationsDir := filepath.Join(t.TempDir(), "annotations")
	seedLiveAgentAnnotation(t, annotationsDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := newSyncBuffer()
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, []string{"--review", "--no-open", "--annotations-dir", annotationsDir, rootPath}, stdout, io.Discard, func(string) error { return nil })
	}()
	viewerURL := waitForViewerURL(t, stdout)

	type queueResponse struct {
		Documents []struct {
			Document    string `json:"document"`
			Revision    string `json:"revision"`
			Annotations []struct {
				ID     string            `json:"id"`
				Status annotation.Status `json:"status"`
			} `json:"annotations"`
		} `json:"documents"`
	}
	type mutationResponse struct {
		Revision   string `json:"revision"`
		Annotation struct {
			ID     string            `json:"id"`
			Status annotation.Status `json:"status"`
		} `json:"annotation"`
	}

	var initialRevision string
	steps := []struct {
		name    string
		args    func() []string
		wantErr string
		check   func(*testing.T, []byte)
	}{
		{
			name: "discover queue and revision",
			args: func() []string { return []string{"queue", "--url", viewerURL} },
			check: func(t *testing.T, output []byte) {
				var queue queueResponse
				if err := json.Unmarshal(output, &queue); err != nil {
					t.Fatalf("json.Unmarshal(queue) error = %v; output: %s", err, output)
				}
				if len(queue.Documents) != 1 || queue.Documents[0].Document != "README.md" || len(queue.Documents[0].Annotations) != 1 || queue.Documents[0].Annotations[0].ID != "ann_live_agent" {
					t.Fatalf("queue = %#v", queue)
				}
				initialRevision = queue.Documents[0].Revision
				if initialRevision == "" {
					t.Fatal("queue revision is empty")
				}
			},
		},
		{
			name: "acknowledge through authenticated API",
			args: func() []string {
				return []string{"resolve", "--url", viewerURL, "--document", "README.md", "--revision", initialRevision, "--id", "ann_live_agent", "--status", "acknowledged", "--role", "agent", "--author", "codex"}
			},
			check: func(t *testing.T, output []byte) {
				var mutation mutationResponse
				if err := json.Unmarshal(output, &mutation); err != nil {
					t.Fatalf("json.Unmarshal(mutation) error = %v; output: %s", err, output)
				}
				if mutation.Annotation.ID != "ann_live_agent" || mutation.Annotation.Status != annotation.StatusAcknowledged || mutation.Revision == initialRevision {
					t.Fatalf("mutation = %#v", mutation)
				}
			},
		},
		{
			name: "reject stale revision",
			args: func() []string {
				return []string{"reply", "--url", viewerURL, "--document", "README.md", "--revision", initialRevision, "--id", "ann_live_agent", "--author", "codex", "--message", "Stale reply"}
			},
			wantErr: "reload the queue",
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			var output bytes.Buffer
			err := commands.RunAgent(step.args(), &output, io.Discard)
			if step.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), step.wantErr) {
					t.Fatalf("RunAgent() error = %v, want containing %q", err, step.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunAgent() error = %v", err)
			}
			step.check(t, output.Bytes())
		})
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("review server did not stop")
	}
}

// seedLiveAgentAnnotation creates one document-level action for the end-to-end
// client test without depending on browser annotation creation.
func seedLiveAgentAnnotation(t *testing.T, directory string) {
	t.Helper()
	store, err := annotationstore.Open(directory)
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      "README.md",
		Annotations: []annotation.Annotation{{
			ID:        "ann_live_agent",
			Intent:    annotation.IntentChangeRequest,
			Status:    annotation.StatusOpen,
			Comment:   "Update this document.",
			Author:    "reviewer",
			CreatedAt: now,
			UpdatedAt: now,
			Thread:    []annotation.ThreadEntry{},
		}},
	}
	if _, err := store.Save(sidecar, ""); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
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
