package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
)

const (
	comparisonTokenHeader            = "X-MD-Viewer-Comparison-Token"
	maxComparisonMutationBytes int64 = 4 << 10
)

// comparisonOptionView is one selectable base offered to the browser. Subject
// is untrusted display text that the browser must escape. HeadDistance is the
// commit's first-parent distance from HEAD (0 for HEAD, 1 for HEAD~1) and is
// omitted for commits off the first-parent line, so labels stay orientation-only.
type comparisonOptionView struct {
	Commit       string `json:"commit"`
	CommitShort  string `json:"commitShort"`
	Subject      string `json:"subject,omitempty"`
	HeadDistance *int   `json:"headDistance,omitempty"`
}

// comparisonStateView is the browser-facing comparison state: the active base
// identity and the bounded selectable commits.
type comparisonStateView struct {
	ActiveCommit  string                 `json:"activeCommit"`
	ActiveShort   string                 `json:"activeShort"`
	RequestedBase string                 `json:"requestedBase"`
	Options       []comparisonOptionView `json:"options"`
}

// comparisonSelection is the accepted selection request body.
type comparisonSelection struct {
	Commit string `json:"commit"`
}

// comparisonState builds the view from the active base and a fresh bounded
// option listing. Options are best-effort so a transient Git failure still
// reports the active base rather than failing the whole request.
func (s *Server) comparisonState(ctx context.Context) comparisonStateView {
	active := s.comparison.active()
	view := comparisonStateView{
		ActiveCommit:  active.BaseCommit,
		ActiveShort:   abbreviatedCommit(active.BaseCommit),
		RequestedBase: active.RequestedBase,
	}
	options, distances, err := s.comparison.options(ctx)
	if err != nil {
		return view
	}
	view.Options = make([]comparisonOptionView, 0, len(options))
	for _, option := range options {
		optionView := comparisonOptionView{
			Commit:      option.Commit,
			CommitShort: abbreviatedCommit(option.Commit),
			Subject:     option.Subject,
		}
		if distance, ok := distances[option.Commit]; ok {
			optionView.HeadDistance = &distance
		}
		view.Options = append(view.Options, optionView)
	}
	return view
}

// protectComparisonMutation enforces the loopback origin, control token, and
// JSON content type before a selection handler runs. It mirrors the annotation
// mutation boundary but uses the distinct comparison control token.
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

// handleComparisonState returns the active base identity and bounded selector
// options, listed fresh so newly created commits appear after a page load.
func (s *Server) handleComparisonState(response http.ResponseWriter, request *http.Request) {
	writeComparisonState(response, http.StatusOK, s.comparisonState(request.Context()))
}

// handleComparisonSelect pins the server-wide base to a commit from the current
// bounded option set. An unlisted commit returns 400; a Git failure retains the
// previous base behind a non-sensitive error.
func (s *Server) handleComparisonSelect(response http.ResponseWriter, request *http.Request) {
	var selection comparisonSelection
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		writeComparisonError(response, http.StatusBadRequest, "request body must be a comparison selection")
		return
	}
	commit := strings.ToLower(strings.TrimSpace(selection.Commit))
	base, err := s.comparison.selectCommit(request.Context(), commit)
	switch {
	case err == nil:
		writeComparisonState(response, http.StatusOK, comparisonStateView{
			ActiveCommit:  base.BaseCommit,
			ActiveShort:   abbreviatedCommit(base.BaseCommit),
			RequestedBase: base.RequestedBase,
		})
	case errors.Is(err, errUnknownCommit):
		writeComparisonError(response, http.StatusBadRequest, "commit is not a selectable comparison option")
	default:
		writeComparisonError(response, http.StatusBadGateway, "the Git comparison could not be updated")
	}
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

// comparisonControlEnabled reports whether selection routes should be registered
// and the control token exposed to the page.
func (s *Server) comparisonControlEnabled() bool {
	return s.comparison != nil && s.comparison.token != ""
}
