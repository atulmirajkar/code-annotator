// Package app owns command-line configuration and the server process lifecycle.
package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/commands"
	"atulm/code-annotator/internal/content"
	"atulm/code-annotator/internal/discovery"
	"atulm/code-annotator/internal/gitdiff"
	"atulm/code-annotator/internal/launch"
	mdrender "atulm/code-annotator/internal/render"
	"atulm/code-annotator/internal/server"
)

const shutdownTimeout = 5 * time.Second

type config struct {
	rootPath       string
	port           int
	noOpen         bool
	review         bool
	annotationsDir string
	includeCode    bool
	codeExtensions string
	excludeDirs    string
	diffBase       string
}

// Run parses args, starts the local viewer, and blocks until ctx is canceled or
// the HTTP server fails. stdout receives the usable viewer URL.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return run(ctx, args, stdout, stderr, launch.OpenURL)
}

type urlLauncher func(string) error

func run(ctx context.Context, args []string, stdout, stderr io.Writer, openURL urlLauncher) error {
	if len(args) > 0 && args[0] == "annotations" {
		return commands.RunAnnotations(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "agent" {
		return commands.RunAgent(args[1:], stdout, stderr)
	}
	configuration, err := parseConfig(args, stderr)
	if err != nil {
		return err
	}

	root, err := content.Open(configuration.rootPath)
	if err != nil {
		return fmt.Errorf("open Markdown directory: %w", err)
	}
	comparison, err := openGitComparison(ctx, configuration, root)
	if err != nil {
		return err
	}
	annotations, err := openAnnotationStore(configuration, root)
	if err != nil {
		return err
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(configuration.port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	defer listener.Close()

	viewerURL := "http://" + listener.Addr().String() + "/"
	var serverOptions []server.Option
	indexOptions, err := catalogOptions(configuration)
	if err != nil {
		return err
	}
	serverOptions = append(serverOptions, server.WithIndexOptions(indexOptions))
	if comparison != nil {
		comparisonToken, err := newControlToken()
		if err != nil {
			return err
		}
		serverOptions = append(serverOptions, server.WithGitComparison(*comparison, viewerURL, comparisonToken))
	}
	if annotations != nil {
		token, err := newControlToken()
		if err != nil {
			return err
		}
		serverOptions = append(serverOptions, server.WithReviewSession(annotations, viewerURL, token))
	}
	viewer, err := server.New(root, mdrender.New(), serverOptions...)
	if err != nil {
		return fmt.Errorf("create Markdown viewer: %w", err)
	}
	httpServer := viewer.HTTPServer(listener.Addr().String())
	fmt.Fprintf(stdout, "Serving %s at %s\n", root.Path(), viewerURL)
	if annotations != nil {
		fmt.Fprintf(stdout, "Review mode enabled; annotations: %s\n", annotations.Root())
		if cleanup, err := discovery.Register(root.Path(), viewerURL); err != nil {
			fmt.Fprintf(stderr, "Could not register server for agent discovery: %v\n", err)
		} else {
			defer cleanup()
		}
	}
	if comparison != nil {
		fmt.Fprintf(stdout, "Git comparison: %s (%s)\n", comparison.RequestedBase, comparison.BaseCommit)
	}
	fmt.Fprintln(stdout, "Press Ctrl-C to stop")

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- httpServer.Serve(listener)
	}()
	if !configuration.noOpen {
		if err := openURL(viewerURL); err != nil {
			fmt.Fprintf(stderr, "Could not open the default browser: %v\n", err)
			fmt.Fprintf(stderr, "Open %s manually.\n", viewerURL)
		} else {
			fmt.Fprintln(stdout, "Opened in the default browser")
		}
	}

	select {
	case serveErr := <-serveResult:
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve Markdown viewer: %w", serveErr)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down Markdown viewer: %w", err)
		}
		serveErr := <-serveResult
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve Markdown viewer: %w", serveErr)
		}
		return nil
	}
}

// newControlToken returns 256 bits of URL-safe random browser authority. The
// token is embedded in pages that expose mutations but never written to
// terminal output. Review and Git comparison each receive an independent token.
func newControlToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate control token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	flags := flag.NewFlagSet("code-annotator", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", 0, "loopback port; 0 selects an available port")
	noOpen := flags.Bool("no-open", false, "do not open the default browser")
	review := flags.Bool("review", false, "enable writable annotation review mode")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory (requires --review)")
	includeCode := flags.Bool("include-code", false, "include supported source files")
	codeExtensions := flags.String("code-extensions", "", "comma-separated source extensions (implies --include-code)")
	excludeDirs := flags.String("exclude-dirs", "", "comma-separated directory base names to exclude")
	diffBase := flags.String("diff-base", "", "locally available Git revision for comparison")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: code-annotator [options] <directory>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Options:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return config{}, errors.New("exactly one Markdown directory is required")
	}
	if *port < 0 || *port > 65535 {
		return config{}, fmt.Errorf("port must be between 0 and 65535: %d", *port)
	}
	if *annotationsDir != "" && !*review {
		return config{}, errors.New("--annotations-dir requires --review")
	}
	configuration := config{
		rootPath: flags.Arg(0), port: *port, noOpen: *noOpen, review: *review,
		annotationsDir: *annotationsDir, includeCode: *includeCode,
		codeExtensions: *codeExtensions, excludeDirs: *excludeDirs,
		diffBase: *diffBase,
	}
	if _, err := catalogOptions(configuration); err != nil {
		return config{}, err
	}

	return configuration, nil
}

// openGitComparison resolves the optional display name once so a moving branch
// cannot change comparison identity while the viewer process is running.
func openGitComparison(ctx context.Context, configuration config, root *content.Root) (*gitdiff.Config, error) {
	if configuration.diffBase == "" {
		return nil, nil
	}
	comparison, err := gitdiff.Open(ctx, root.Path(), configuration.diffBase)
	if err != nil {
		return nil, fmt.Errorf("configure Git comparison: %w", err)
	}
	return &comparison, nil
}

// catalogOptions applies CLI implication and default-exclusion semantics before
// delegating validation and normalization to the content package.
func catalogOptions(configuration config) (content.IndexOptions, error) {
	extensions := splitCSV(configuration.codeExtensions)
	includeCode := configuration.includeCode || len(extensions) > 0
	if includeCode && len(extensions) == 0 {
		extensions = content.DefaultCodeExtensions()
	}
	excluded := splitCSV(configuration.excludeDirs)
	if includeCode {
		excluded = append(content.DefaultExcludedDirectories(), excluded...)
	}
	options, err := content.NewIndexOptions(extensions, excluded)
	if err != nil {
		return content.IndexOptions{}, fmt.Errorf("configure content catalog: %w", err)
	}
	return options, nil
}

// splitCSV preserves invalid empty entries for the catalog validator while
// treating an entirely omitted flag as no values.
func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

// openAnnotationStore establishes the writable boundary for review mode. The
// default remains inside the content root, while an explicit path is resolved
// relative to the process working directory by the store.
func openAnnotationStore(configuration config, root *content.Root) (*annotationstore.Store, error) {
	if !configuration.review {
		return nil, nil
	}
	directory := configuration.annotationsDir
	if directory == "" {
		directory = filepath.Join(root.Path(), ".code-annotator", "annotations")
	}
	annotations, err := annotationstore.Open(directory)
	if err != nil {
		return nil, fmt.Errorf("open annotation directory: %w", err)
	}
	return annotations, nil
}
