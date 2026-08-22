package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"atulm/code-annotator/internal/discovery"
)

func TestAgentOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{name: "IPv4", value: "http://127.0.0.1:8080/", want: "http://127.0.0.1:8080"},
		{name: "IPv6", value: "http://[::1]:8080", want: "http://[::1]:8080"},
		{name: "localhost", value: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "remote", value: "http://example.com", wantErr: "loopback"},
		{name: "HTTPS", value: "https://127.0.0.1:8080", wantErr: "loopback HTTP"},
		{name: "credentials", value: "http://user@127.0.0.1:8080", wantErr: "loopback HTTP"},
		{name: "path", value: "http://127.0.0.1:8080/view/README.md", wantErr: "loopback HTTP"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := agentOrigin(test.value)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("agentOrigin() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("agentOrigin() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("agentOrigin() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         func(string) []string
		wantMethod   string
		wantPath     string
		wantBody     map[string]string
		wantRevision string
		wantOutput   string
		wantErr      string
		nonReview    bool
		conflict     bool
	}{
		{
			name:       "queue",
			args:       func(origin string) []string { return []string{"queue", "--url", origin} },
			wantMethod: http.MethodGet,
			wantPath:   "/api/annotations?status=open%2Cneeds_changes",
			wantOutput: `"documents":[]`,
		},
		{
			name: "reply",
			args: func(origin string) []string {
				return []string{"reply", "--url", origin, "--document", "README.md", "--revision", "rev-1", "--id", "ann_test", "--author", "codex", "--message", "More context"}
			},
			wantMethod:   http.MethodPost,
			wantPath:     "/api/annotations/ann_test/replies",
			wantBody:     map[string]string{"document": "README.md", "author": "codex", "message": "More context"},
			wantRevision: "rev-1",
			wantOutput:   `"revision":"rev-2"`,
		},
		{
			name: "resolve",
			args: func(origin string) []string {
				return []string{"resolve", "--url", origin, "--document", "README.md", "--revision", "rev-1", "--id", "ann_test", "--author", "codex", "--status", "applied", "--role", "agent", "--summary", "Done", "--commit", "abc1234"}
			},
			wantMethod:   http.MethodPatch,
			wantPath:     "/api/annotations/ann_test",
			wantBody:     map[string]string{"document": "README.md", "author": "codex", "status": "applied", "actorRole": "agent", "summary": "Done", "commit": "abc1234"},
			wantRevision: "rev-1",
			wantOutput:   `"revision":"rev-2"`,
		},
		{
			name: "conflict",
			args: func(origin string) []string {
				return []string{"resolve", "--url", origin, "--document", "README.md", "--revision", "stale", "--id", "ann_test", "--author", "codex", "--status", "acknowledged", "--role", "agent"}
			},
			wantMethod:   http.MethodPatch,
			wantPath:     "/api/annotations/ann_test",
			wantBody:     map[string]string{"document": "README.md", "author": "codex", "status": "acknowledged", "actorRole": "agent"},
			wantRevision: "stale",
			conflict:     true,
			wantErr:      "reload the queue",
		},
		{
			name: "not review mode",
			args: func(origin string) []string {
				return []string{"reply", "--url", origin, "--document", "README.md", "--revision", "rev-1", "--id", "ann_test", "--author", "codex", "--message", "Question"}
			},
			nonReview: true,
			wantErr:   "not running in review mode",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/" {
					response.Header().Set("Content-Type", "text/html")
					if test.nonReview {
						_, _ = io.WriteString(response, "<html></html>")
					} else {
						_, _ = io.WriteString(response, `<meta name="code-annotator-review-token" content="test-token">`)
					}
					return
				}
				if request.Method != test.wantMethod || request.URL.RequestURI() != test.wantPath {
					t.Errorf("request = %s %s, want %s %s", request.Method, request.URL.RequestURI(), test.wantMethod, test.wantPath)
				}
				if test.wantBody != nil {
					if request.Header.Get("Origin") != server.URL || request.Header.Get("X-Code-Annotator-Token") != "test-token" || request.Header.Get("If-Match") != `"`+test.wantRevision+`"` {
						t.Errorf("mutation headers = %#v", request.Header)
					}
					var body map[string]string
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Errorf("Decode() error = %v", err)
						return
					}
					if !equalStringMap(body, test.wantBody) {
						t.Errorf("body = %#v, want %#v", body, test.wantBody)
					}
				}
				response.Header().Set("Content-Type", "application/json")
				if test.conflict {
					response.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(response, "revision conflict\n")
					return
				}
				if test.wantMethod == http.MethodGet {
					_, _ = io.WriteString(response, `{"schemaVersion":1,"documents":[]}`)
				} else {
					_, _ = io.WriteString(response, `{"annotation":{"status":"acknowledged"},"revision":"rev-2"}`)
				}
			}))
			defer server.Close()

			var output bytes.Buffer
			err := RunAgent(test.args(server.URL), &output, io.Discard)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("RunAgent() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunAgent() error = %v", err)
			}
			if !strings.Contains(output.String(), test.wantOutput) {
				t.Fatalf("output = %q, want containing %q", output.String(), test.wantOutput)
			}
		})
	}
}

func TestParseAgentConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "queue defaults", args: []string{"queue", "--url", "http://127.0.0.1:8080"}},
		{name: "missing command", wantErr: "subcommand"},
		{name: "unknown command", args: []string{"delete"}, wantErr: "unknown"},
		{name: "missing URL", args: []string{"queue"}, wantErr: "--url"},
		{name: "reply missing message", args: []string{"reply", "--url", "http://127.0.0.1:8080", "--document", "README.md", "--revision", "rev", "--id", "ann_test", "--author", "codex"}, wantErr: "--message"},
		{name: "resolve missing role", args: []string{"resolve", "--url", "http://127.0.0.1:8080", "--document", "README.md", "--revision", "rev", "--id", "ann_test", "--author", "codex", "--status", "applied"}, wantErr: "--role"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseAgentConfig(test.args, io.Discard)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseAgentConfig() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAgentConfig() error = %v", err)
			}
		})
	}
}

func TestRunAgentDiscover(t *testing.T) {
	t.Run("single match", func(t *testing.T) {
		t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())
		live := newHealthzServer(t)
		cleanup, err := discovery.Register("/content/live", live.URL+"/")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		defer cleanup()

		var output bytes.Buffer
		if err := RunAgent([]string{"discover"}, &output, io.Discard); err != nil {
			t.Fatalf("RunAgent() error = %v", err)
		}
		if !strings.Contains(output.String(), live.URL) {
			t.Fatalf("output = %q, want containing %q", output.String(), live.URL)
		}
	})

	t.Run("zero match", func(t *testing.T) {
		t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

		var output bytes.Buffer
		err := RunAgent([]string{"discover"}, &output, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "no running review server found") {
			t.Fatalf("RunAgent() error = %v, want containing %q", err, "no running review server found")
		}
	})

	t.Run("multiple matches disambiguated by root", func(t *testing.T) {
		t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

		rootA := resolvedTempDir(t)
		rootB := resolvedTempDir(t)
		serverA := newHealthzServer(t)
		serverB := newHealthzServer(t)
		// Register keys entries by PID, which is correct for real distinct
		// server processes but collides within a single test process, so the
		// second "server" is seeded as a fabricated entry with a different PID.
		cleanupA, err := discovery.Register(rootA, serverA.URL+"/")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		defer cleanupA()
		registerFakeEntry(t, 999999, rootB, serverB.URL+"/")

		var ambiguous bytes.Buffer
		err = RunAgent([]string{"discover"}, &ambiguous, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "multiple running review servers") {
			t.Fatalf("RunAgent() error = %v, want containing %q", err, "multiple running review servers")
		}

		var resolved bytes.Buffer
		if err := RunAgent([]string{"discover", "--root", rootA}, &resolved, io.Discard); err != nil {
			t.Fatalf("RunAgent() error = %v", err)
		}
		if !strings.Contains(resolved.String(), serverA.URL) {
			t.Fatalf("output = %q, want containing %q", resolved.String(), serverA.URL)
		}
	})

	t.Run("multiple matches auto-selected by working directory", func(t *testing.T) {
		t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

		insideRoot := resolvedTempDir(t)
		outsideRoot := resolvedTempDir(t)
		insideServer := newHealthzServer(t)
		outsideServer := newHealthzServer(t)
		cleanup, err := discovery.Register(insideRoot, insideServer.URL+"/")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		defer cleanup()
		registerFakeEntry(t, 888888, outsideRoot, outsideServer.URL+"/")

		nested := filepath.Join(insideRoot, "nested")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		t.Chdir(nested)

		var output bytes.Buffer
		if err := RunAgent([]string{"discover"}, &output, io.Discard); err != nil {
			t.Fatalf("RunAgent() error = %v", err)
		}
		if !strings.Contains(output.String(), insideServer.URL) {
			t.Fatalf("output = %q, want containing %q (auto-selected by cwd)", output.String(), insideServer.URL)
		}
	})

	t.Run("multiple matches outside every root stay ambiguous", func(t *testing.T) {
		t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

		rootA := resolvedTempDir(t)
		rootB := resolvedTempDir(t)
		serverA := newHealthzServer(t)
		serverB := newHealthzServer(t)
		cleanup, err := discovery.Register(rootA, serverA.URL+"/")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		defer cleanup()
		registerFakeEntry(t, 666666, rootB, serverB.URL+"/")

		t.Chdir(resolvedTempDir(t))

		var output bytes.Buffer
		err = RunAgent([]string{"discover"}, &output, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "multiple running review servers") {
			t.Fatalf("RunAgent() error = %v, want containing %q", err, "multiple running review servers")
		}
	})

	t.Run("stale entry is pruned", func(t *testing.T) {
		t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

		dead := newHealthzServer(t)
		if _, err := discovery.Register("/content/dead", dead.URL+"/"); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		dead.Close()

		var output bytes.Buffer
		err := RunAgent([]string{"discover"}, &output, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "no running review server found") {
			t.Fatalf("RunAgent() error = %v, want containing %q", err, "no running review server found")
		}

		entries, err := discovery.List()
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("List() = %v, want the stale entry pruned", entries)
		}
	})
}

// newHealthzServer fakes just enough of the review server for a discovery
// liveness probe: a 200 response on /healthz.
func newHealthzServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(response, "ok\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// resolvedTempDir returns a symlink-resolved temp directory, matching what
// content.Open (and therefore discovery.Register in production) stores.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	return resolved
}

// registerFakeEntry writes a registry entry directly under a fabricated PID,
// simulating a second server process without colliding with the real
// discovery.Register call already made by the current test process.
func registerFakeEntry(t *testing.T, pid int, root, url string) {
	t.Helper()
	directory, err := discovery.StateDir()
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	entry := discovery.Entry{SchemaVersion: 1, PID: pid, URL: url, Root: root, StartedAt: time.Now().UTC()}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	path := filepath.Join(directory, strconv.Itoa(pid)+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

// equalStringMap compares JSON request fields without depending on map order.
func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
