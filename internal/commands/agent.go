package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"atulm/code-annotator/internal/annotation"
	"atulm/code-annotator/internal/discovery"
)

const discoveryHealthTimeout = 500 * time.Millisecond

const maxAgentResponseBytes int64 = 8 << 20

var reviewTokenPattern = regexp.MustCompile(`<meta name="code-annotator-review-token" content="([A-Za-z0-9_-]+)">`)

type agentConfig struct {
	command  string
	origin   string
	status   string
	document string
	revision string
	id       string
	role     string
	message  string
	summary  string
	commit   string
	root     string
	etag     string
}

// RunAgent executes live-server annotation operations without opening sidecar
// storage. Browser and agent mutations therefore share one revision authority.
func RunAgent(args []string, stdout, stderr io.Writer) error {
	configuration, err := parseAgentConfig(args, stderr)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if configuration.command == "discover" {
		return runAgentDiscover(client, configuration, stdout)
	}
	if configuration.command == "queue" {
		return runAgentQueue(client, configuration, stdout)
	}

	token, err := fetchReviewToken(client, configuration.origin)
	if err != nil {
		return err
	}
	body := map[string]string{"document": configuration.document, "role": configuration.role}
	method := http.MethodPatch
	path := "/api/annotations/" + url.PathEscape(configuration.id)
	if configuration.command == "reply" {
		method = http.MethodPost
		path += "/replies"
		body["message"] = configuration.message
	} else {
		body["status"] = configuration.status
		for name, value := range map[string]string{"message": configuration.message, "summary": configuration.summary, "commit": configuration.commit} {
			if value != "" {
				body[name] = value
			}
		}
	}
	return sendAgentRequest(client, configuration, method, path, body, token, stdout)
}

// parseAgentConfig validates the API client command without making a request.
func parseAgentConfig(args []string, stderr io.Writer) (agentConfig, error) {
	if len(args) == 0 {
		return agentConfig{}, errors.New("agent subcommand is required")
	}
	configuration := agentConfig{command: args[0]}
	if configuration.command != "queue" && configuration.command != "reply" && configuration.command != "resolve" && configuration.command != "discover" {
		return agentConfig{}, fmt.Errorf("unknown agent subcommand %q", configuration.command)
	}
	flags := flag.NewFlagSet("code-annotator agent "+configuration.command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	viewerURL := flags.String("url", "", "running loopback code-annotator URL")
	status := flags.String("status", "", "status filter or target lifecycle status")
	document := flags.String("document", "", "reviewable document path")
	revision := flags.String("revision", "", "current document sidecar revision")
	identifier := flags.String("id", "", "annotation identifier")
	role := flags.String("role", "", "role (agent or reviewer)")
	message := flags.String("message", "", "discussion or rejection message")
	summary := flags.String("summary", "", "applied-work summary")
	commit := flags.String("commit", "", "optional applied-work commit")
	root := flags.String("root", "", "content root to disambiguate multiple discovered servers")
	etag := flags.String("etag", "", "queue etag from a previous poll; a match returns 304 with no body")
	if err := flags.Parse(args[1:]); err != nil {
		return agentConfig{}, err
	}
	if flags.NArg() != 0 {
		return agentConfig{}, fmt.Errorf("agent %s does not accept positional arguments", configuration.command)
	}
	if configuration.command == "discover" {
		configuration.root = *root
		return configuration, nil
	}
	origin, err := agentOrigin(*viewerURL)
	if err != nil {
		return agentConfig{}, err
	}
	configuration.origin = origin

	switch configuration.command {
	case "queue":
		configuration.status = *status
		if configuration.status == "" {
			configuration.status = "open,needs_changes"
		}
		configuration.etag = *etag
	case "reply", "resolve":
		required := []struct{ name, value string }{
			{name: "--document", value: *document},
			{name: "--revision", value: *revision},
			{name: "--id", value: *identifier},
			{name: "--role", value: *role},
		}
		if configuration.command == "reply" {
			required = append(required, struct{ name, value string }{name: "--message", value: *message})
		} else {
			required = append(required,
				struct{ name, value string }{name: "--status", value: *status},
			)
		}
		for _, field := range required {
			if strings.TrimSpace(field.value) == "" {
				return agentConfig{}, fmt.Errorf("%s is required", field.name)
			}
		}
		if !annotation.Role(*role).Valid() {
			return agentConfig{}, fmt.Errorf("invalid annotation role %q", *role)
		}
		configuration.document = *document
		configuration.revision = *revision
		configuration.id = *identifier
		configuration.status = *status
		configuration.role = *role
		configuration.message = *message
		configuration.summary = *summary
		configuration.commit = *commit
	}
	return configuration, nil
}

// agentOrigin accepts only the loopback HTTP origin used by code-annotator and
// discards a harmless root path slash.
func agentOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("--url must be a loopback HTTP viewer URL")
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("--url host must be loopback")
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), nil
}

// fetchReviewToken obtains the per-process mutation authority from the served
// review page without logging or returning it to command output.
func fetchReviewToken(client *http.Client, origin string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, origin+"/", nil)
	if err != nil {
		return "", fmt.Errorf("create review-page request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("read review page: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read review page: HTTP %d", response.StatusCode)
	}
	body, err := readAgentResponse(response.Body)
	if err != nil {
		return "", fmt.Errorf("read review page: %w", err)
	}
	match := reviewTokenPattern.FindSubmatch(body)
	if len(match) != 2 {
		return "", errors.New("viewer is not running in review mode")
	}
	return string(match[1]), nil
}

// sendAgentRequest adds the live-session mutation contract and copies only the
// server's JSON response to stdout.
func sendAgentRequest(client *http.Client, configuration agentConfig, method, path string, body map[string]string, token string, output io.Writer) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode annotation request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, configuration.origin+path, requestBody)
	if err != nil {
		return fmt.Errorf("create annotation request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		revision, _ := json.Marshal(configuration.revision)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", configuration.origin)
		request.Header.Set("If-Match", string(revision))
		request.Header.Set("X-Code-Annotator-Token", token)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("call annotation API: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := readAgentResponse(response.Body)
	if err != nil {
		return fmt.Errorf("read annotation API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if response.StatusCode == http.StatusConflict {
			return fmt.Errorf("annotations changed concurrently; reload the queue: %s", message)
		}
		return fmt.Errorf("annotation API returned HTTP %d: %s", response.StatusCode, message)
	}
	if !json.Valid(responseBody) {
		return errors.New("annotation API returned invalid JSON")
	}
	if _, err := output.Write(responseBody); err != nil {
		return fmt.Errorf("write annotation API response: %w", err)
	}
	if len(responseBody) == 0 || responseBody[len(responseBody)-1] != '\n' {
		_, err = io.WriteString(output, "\n")
	}
	return err
}

// queuePollEnvelope wraps a queue response with the ETag needed for the next
// poll, only ever emitted when the caller opted in with --etag: the default
// "agent queue" output stays byte-identical to the raw server response.
type queuePollEnvelope struct {
	ETag     string          `json:"etag"`
	Modified bool            `json:"modified"`
	Queue    json.RawMessage `json:"queue,omitempty"`
}

// runAgentQueue polls the annotation queue, optionally as a conditional GET.
// A caller that never passes --etag sees exactly today's raw response; a
// caller that does gets a small envelope carrying the current ETag either
// way, so a polling loop never needs a second request to learn it.
func runAgentQueue(client *http.Client, configuration agentConfig, output io.Writer) error {
	query := url.Values{"status": []string{configuration.status}}.Encode()
	request, err := http.NewRequest(http.MethodGet, configuration.origin+"/api/annotations?"+query, nil)
	if err != nil {
		return fmt.Errorf("create annotation request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if configuration.etag != "" {
		request.Header.Set("If-None-Match", strconv.Quote(configuration.etag))
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("call annotation API: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := readAgentResponse(response.Body)
	if err != nil {
		return fmt.Errorf("read annotation API response: %w", err)
	}

	if response.StatusCode == http.StatusNotModified {
		return writeQueueEnvelope(output, unquoteETag(response.Header.Get("ETag")), false, nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("annotation API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if !json.Valid(responseBody) {
		return errors.New("annotation API returned invalid JSON")
	}
	if configuration.etag == "" {
		if _, err := output.Write(responseBody); err != nil {
			return fmt.Errorf("write annotation API response: %w", err)
		}
		if len(responseBody) == 0 || responseBody[len(responseBody)-1] != '\n' {
			_, err = io.WriteString(output, "\n")
		}
		return err
	}
	return writeQueueEnvelope(output, unquoteETag(response.Header.Get("ETag")), true, responseBody)
}

// writeQueueEnvelope emits the --etag output contract as one JSON object.
func writeQueueEnvelope(output io.Writer, etag string, modified bool, queueBody []byte) error {
	envelope := queuePollEnvelope{ETag: etag, Modified: modified}
	if modified {
		envelope.Queue = json.RawMessage(queueBody)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode queue poll envelope: %w", err)
	}
	if _, err := output.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write queue poll envelope: %w", err)
	}
	return nil
}

// unquoteETag strips the quoting an HTTP ETag header carries on the wire, so
// the value round-trips cleanly back into --etag on the next call.
func unquoteETag(header string) string {
	value, err := strconv.Unquote(header)
	if err != nil {
		return header
	}
	return value
}

// discoveredServer is the JSON shape returned by "agent discover" for a
// single unambiguous match.
type discoveredServer struct {
	URL  string `json:"url"`
	Root string `json:"root"`
	PID  int    `json:"pid"`
}

// runAgentDiscover finds a running review-mode server without requiring the
// caller to already know its URL. It verifies each registry entry against
// the server's own /healthz route rather than scanning ports, and prunes
// entries that fail that check, since those were left behind by a server
// that exited without cleaning up after itself.
func runAgentDiscover(client *http.Client, configuration agentConfig, output io.Writer) error {
	entries, err := discovery.List()
	if err != nil {
		return fmt.Errorf("list discovery entries: %w", err)
	}
	var wantRoot string
	if configuration.root != "" {
		absolute, err := filepath.Abs(configuration.root)
		if err != nil {
			return fmt.Errorf("resolve --root: %w", err)
		}
		// Registered roots are symlink-resolved (see content.Open), so the
		// filter must resolve the same way or it silently matches nothing.
		wantRoot, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("resolve --root: %w", err)
		}
	}
	var live []discovery.Entry
	for _, entry := range entries {
		if !isServerLive(client, entry.URL) {
			_ = discovery.Remove(entry.PID)
			continue
		}
		if wantRoot != "" && entry.Root != wantRoot {
			continue
		}
		live = append(live, entry)
	}

	// Without an explicit --root, prefer the one candidate whose content
	// root contains the caller's own working directory, since an agent is
	// almost always invoked from inside the project it should be discussing.
	if wantRoot == "" && len(live) > 1 {
		if narrowed := narrowByWorkingDirectory(live); len(narrowed) == 1 {
			live = narrowed
		}
	}

	switch len(live) {
	case 0:
		return errors.New("no running review server found; start one with --review or supply --url directly")
	case 1:
		encoded, err := json.Marshal(discoveredServer{URL: live[0].URL, Root: live[0].Root, PID: live[0].PID})
		if err != nil {
			return fmt.Errorf("encode discovered server: %w", err)
		}
		if _, err := output.Write(append(encoded, '\n')); err != nil {
			return fmt.Errorf("write discovered server: %w", err)
		}
		return nil
	default:
		var candidates strings.Builder
		for _, entry := range live {
			fmt.Fprintf(&candidates, "\n  %s (root: %s)", entry.URL, entry.Root)
		}
		return fmt.Errorf("multiple running review servers found; pass --root to disambiguate or supply --url directly:%s", candidates.String())
	}
}

// isServerLive issues a bounded request to a single, already-known loopback
// address recorded by the server itself. This is not a port scan: discovery
// only ever probes addresses a server registered, never guesses at one.
func isServerLive(client *http.Client, serverURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), discoveryHealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(serverURL, "/")+"/healthz", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode == http.StatusOK
}

// narrowByWorkingDirectory prefers whichever candidates contain the caller's
// own working directory. If that yields no match or more than one, the
// original candidate list is returned unchanged so the ordinary zero/many
// selection logic still applies; this narrowing only ever helps disambiguate,
// it never introduces a false positive by itself.
func narrowByWorkingDirectory(candidates []discovery.Entry) []discovery.Entry {
	cwd, err := os.Getwd()
	if err != nil {
		return candidates
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return candidates
	}
	var narrowed []discovery.Entry
	for _, entry := range candidates {
		if entry.Root == resolvedCwd || strings.HasPrefix(resolvedCwd, entry.Root+string(filepath.Separator)) {
			narrowed = append(narrowed, entry)
		}
	}
	if len(narrowed) == 0 {
		return candidates
	}
	return narrowed
}

// readAgentResponse bounds local HTTP responses before retaining them in
// memory, including unexpected error pages.
func readAgentResponse(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxAgentResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxAgentResponseBytes {
		return nil, errors.New("response is too large")
	}
	return body, nil
}
