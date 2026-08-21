package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxAgentResponseBytes int64 = 8 << 20

var reviewTokenPattern = regexp.MustCompile(`<meta name="code-annotator-review-token" content="([A-Za-z0-9_-]+)">`)

type agentConfig struct {
	command  string
	origin   string
	status   string
	document string
	revision string
	id       string
	author   string
	role     string
	message  string
	summary  string
	commit   string
}

// RunAgent executes live-server annotation operations without opening sidecar
// storage. Browser and agent mutations therefore share one revision authority.
func RunAgent(args []string, stdout, stderr io.Writer) error {
	configuration, err := parseAgentConfig(args, stderr)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if configuration.command == "queue" {
		query := url.Values{"status": []string{configuration.status}}.Encode()
		return sendAgentRequest(client, configuration, http.MethodGet, "/api/annotations?"+query, nil, "", stdout)
	}

	token, err := fetchReviewToken(client, configuration.origin)
	if err != nil {
		return err
	}
	body := map[string]string{"document": configuration.document, "author": configuration.author}
	method := http.MethodPatch
	path := "/api/annotations/" + url.PathEscape(configuration.id)
	if configuration.command == "reply" {
		method = http.MethodPost
		path += "/replies"
		body["message"] = configuration.message
	} else {
		body["status"] = configuration.status
		body["actorRole"] = configuration.role
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
	if configuration.command != "queue" && configuration.command != "reply" && configuration.command != "resolve" {
		return agentConfig{}, fmt.Errorf("unknown agent subcommand %q", configuration.command)
	}
	flags := flag.NewFlagSet("code-annotator agent "+configuration.command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	viewerURL := flags.String("url", "", "running loopback code-annotator URL")
	status := flags.String("status", "", "status filter or target lifecycle status")
	document := flags.String("document", "", "reviewable document path")
	revision := flags.String("revision", "", "current document sidecar revision")
	identifier := flags.String("id", "", "annotation identifier")
	author := flags.String("author", "", "agent name")
	role := flags.String("role", "", "actor role")
	message := flags.String("message", "", "discussion or rejection message")
	summary := flags.String("summary", "", "applied-work summary")
	commit := flags.String("commit", "", "optional applied-work commit")
	if err := flags.Parse(args[1:]); err != nil {
		return agentConfig{}, err
	}
	if flags.NArg() != 0 {
		return agentConfig{}, fmt.Errorf("agent %s does not accept positional arguments", configuration.command)
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
	case "reply", "resolve":
		required := []struct{ name, value string }{
			{name: "--document", value: *document},
			{name: "--revision", value: *revision},
			{name: "--id", value: *identifier},
			{name: "--author", value: *author},
		}
		if configuration.command == "reply" {
			required = append(required, struct{ name, value string }{name: "--message", value: *message})
		} else {
			required = append(required,
				struct{ name, value string }{name: "--status", value: *status},
				struct{ name, value string }{name: "--role", value: *role},
			)
		}
		for _, field := range required {
			if strings.TrimSpace(field.value) == "" {
				return agentConfig{}, fmt.Errorf("%s is required", field.name)
			}
		}
		configuration.document = *document
		configuration.revision = *revision
		configuration.id = *identifier
		configuration.author = *author
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
