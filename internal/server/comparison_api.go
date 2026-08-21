package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const (
	comparisonTokenHeader            = "X-MD-Viewer-Comparison-Token"
	maxComparisonMutationBytes int64 = 4 << 10
)

// comparisonOptionView is one selectable base offered to the browser. Subject
// and Name are untrusted display text that the browser must escape.
type comparisonOptionView struct {
	Commit      string `json:"commit"`
	CommitShort string `json:"commitShort"`
	Subject     string `json:"subject,omitempty"`
	Configured  bool   `json:"configured"`
	Name        string `json:"name,omitempty"`
}

// comparisonStateView is the complete comparison state returned to the browser
// on load, after a mutation, and alongside a conflict so a tab can reconcile.
type comparisonStateView struct {
	Revision      string                 `json:"revision"`
	ActiveCommit  string                 `json:"activeCommit"`
	ActiveShort   string                 `json:"activeShort"`
	RequestedBase string                 `json:"requestedBase"`
	Explicit      bool                   `json:"explicit"`
	Options       []comparisonOptionView `json:"options"`
}

// comparisonMutation is the accepted request body for a refresh or selection.
type comparisonMutation struct {
	Action string `json:"action"`
	Commit string `json:"commit"`
}

// viewComparison renders an immutable snapshot as the browser-facing state. The
// configured moving revision is folded into the option that shares its commit
// so the same object never appears twice in the selector.
func viewComparison(snapshot comparisonSnapshot) comparisonStateView {
	options := make([]comparisonOptionView, 0, len(snapshot.options)+1)
	matchedConfigured := false
	for _, option := range snapshot.options {
		view := comparisonOptionView{
			Commit:      option.Commit,
			CommitShort: abbreviatedCommit(option.Commit),
			Subject:     option.Subject,
		}
		if option.Commit == snapshot.configuredCommit {
			view.Configured = true
			view.Name = snapshot.configuredName
			matchedConfigured = true
		}
		options = append(options, view)
	}
	if !matchedConfigured && snapshot.configuredCommit != "" {
		configured := comparisonOptionView{
			Commit:      snapshot.configuredCommit,
			CommitShort: abbreviatedCommit(snapshot.configuredCommit),
			Configured:  true,
			Name:        snapshot.configuredName,
		}
		options = append([]comparisonOptionView{configured}, options...)
	}
	return comparisonStateView{
		Revision:      snapshot.revision,
		ActiveCommit:  snapshot.config.BaseCommit,
		ActiveShort:   abbreviatedCommit(snapshot.config.BaseCommit),
		RequestedBase: snapshot.config.RequestedBase,
		Explicit:      snapshot.explicit,
		Options:       options,
	}
}

// protectComparisonMutation enforces the loopback origin, control token, and
// JSON content type before a refresh or selection handler runs. It mirrors the
// annotation mutation boundary but uses the distinct comparison control token.
func (s *Server) protectComparisonMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if s.comparison == nil || s.comparison.token == "" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Origin") != s.comparison.origin {
			http.Error(response, "forbidden comparison origin", http.StatusForbidden)
			return
		}
		providedToken := request.Header.Get(comparisonTokenHeader)
		if subtle.ConstantTimeCompare([]byte(providedToken), []byte(s.comparison.token)) != 1 {
			http.Error(response, "invalid comparison token", http.StatusForbidden)
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(response, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maxComparisonMutationBytes)
		next.ServeHTTP(response, request)
	})
}

// handleComparisonState returns the active comparison identity, opaque state
// revision, and bounded selector options. The revision doubles as an ETag so a
// browser can supply it as If-Match on the next mutation.
func (s *Server) handleComparisonState(response http.ResponseWriter, _ *http.Request) {
	snapshot := s.comparison.snapshot()
	response.Header().Set("ETag", strconv.Quote(snapshot.revision))
	writeComparisonState(response, http.StatusOK, viewComparison(snapshot))
}

// handleComparisonMutate applies a refresh or selection under optimistic
// concurrency. A stale If-Match returns 409 with the current state; a Git
// failure retains the previous snapshot and returns a non-sensitive error.
func (s *Server) handleComparisonMutate(response http.ResponseWriter, request *http.Request) {
	ifMatch := parseComparisonIfMatch(request.Header.Get("If-Match"))
	if ifMatch == "" {
		writeComparisonError(response, http.StatusPreconditionRequired, "If-Match state revision is required")
		return
	}
	var mutation comparisonMutation
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mutation); err != nil {
		writeComparisonError(response, http.StatusBadRequest, "request body must be a comparison mutation")
		return
	}

	var (
		snapshot comparisonSnapshot
		err      error
	)
	switch mutation.Action {
	case "refresh":
		snapshot, err = s.comparison.refresh(request.Context(), ifMatch)
	case "select":
		snapshot, err = s.comparison.selectCommit(request.Context(), ifMatch, strings.ToLower(strings.TrimSpace(mutation.Commit)))
	default:
		writeComparisonError(response, http.StatusBadRequest, "action must be refresh or select")
		return
	}

	switch {
	case err == nil:
		response.Header().Set("ETag", strconv.Quote(snapshot.revision))
		writeComparisonState(response, http.StatusOK, viewComparison(snapshot))
	case errors.Is(err, errStaleComparison):
		current := s.comparison.snapshot()
		response.Header().Set("ETag", strconv.Quote(current.revision))
		writeComparisonState(response, http.StatusConflict, viewComparison(current))
	case errors.Is(err, errUnknownCommit):
		writeComparisonError(response, http.StatusBadRequest, "commit is not a selectable comparison option")
	default:
		// Retain the previous snapshot and avoid exposing Git command output.
		writeComparisonError(response, http.StatusBadGateway, "the Git comparison could not be updated")
	}
}

// parseComparisonIfMatch extracts the bare opaque revision from a strong ETag
// If-Match header, tolerating a weak prefix and quotes but rejecting wildcards.
func parseComparisonIfMatch(header string) string {
	value := strings.TrimSpace(header)
	if value == "" || value == "*" {
		return ""
	}
	value = strings.TrimPrefix(value, "W/")
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func writeComparisonState(response http.ResponseWriter, status int, view comparisonStateView) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(view)
}

func writeComparisonError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}

// comparisonControlEnabled reports whether authenticated mutation routes should
// be registered and the control token exposed to the page.
func (s *Server) comparisonControlEnabled() bool {
	return s.comparison != nil && s.comparison.token != ""
}
