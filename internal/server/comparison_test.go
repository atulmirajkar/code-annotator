package server

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"atulm/md-viewer/internal/gitdiff"
)

// newComparisonRepository freezes a two-commit worktree with the base pinned at
// the tip and returns a controller plus the initial and tip commit IDs.
func newComparisonRepository(t *testing.T) (*comparisonController, string, string) {
	t.Helper()
	requireComparisonGit(t)
	repository := t.TempDir()
	runServerTestGit(t, repository, "init", "-b", "main")
	writeTestFile(t, repository+"/main.go", "package main\n")
	runServerTestGit(t, repository, "add", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	initial := revParse(t, repository, "HEAD")
	writeTestFile(t, repository+"/main.go", "package changed\n")
	runServerTestGit(t, repository, "add", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "second")
	tip := revParse(t, repository, "HEAD")

	configuration, err := gitdiff.Open(context.Background(), repository, "HEAD")
	if err != nil {
		t.Fatalf("gitdiff.Open() error = %v", err)
	}
	controller, err := newComparisonController(configuration, "", "")
	if err != nil {
		t.Fatalf("newComparisonController() error = %v", err)
	}
	return controller, initial, tip
}

func revParse(t *testing.T, repository, revision string) string {
	t.Helper()
	command := exec.Command("git", "-C", repository, "rev-parse", revision)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s error = %v: %s", revision, err, output)
	}
	return strings.ToLower(strings.TrimSpace(string(output)))
}

func requireComparisonGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
}

func TestComparisonStartsPinnedToConfiguredCommit(t *testing.T) {
	t.Parallel()
	controller, _, tip := newComparisonRepository(t)
	if active := controller.active(); active.BaseCommit != tip {
		t.Fatalf("active base = %s, want configured tip %s", active.BaseCommit, tip)
	}
}

func TestComparisonOptionsReportHeadDistances(t *testing.T) {
	t.Parallel()
	controller, initial, tip := newComparisonRepository(t)
	options, distances, err := controller.options(context.Background())
	if err != nil {
		t.Fatalf("options() error = %v", err)
	}
	if !containsCommit(options, initial) || !containsCommit(options, tip) {
		t.Fatalf("options missing initial or tip: %#v", options)
	}
	if distances[tip] != 0 {
		t.Errorf("tip distance = %d, want 0", distances[tip])
	}
	if distances[initial] != 1 {
		t.Errorf("initial distance = %d, want 1", distances[initial])
	}
}

func TestComparisonSelectPinsCommit(t *testing.T) {
	t.Parallel()
	controller, initial, _ := newComparisonRepository(t)
	base, err := controller.selectCommit(context.Background(), initial)
	if err != nil {
		t.Fatalf("selectCommit() error = %v", err)
	}
	if base.BaseCommit != initial || base.RequestedBase != abbreviatedCommit(initial) {
		t.Fatalf("selectCommit() base = %+v, want pinned %s", base, initial)
	}
	if controller.active().BaseCommit != initial {
		t.Fatalf("active base = %s, want %s", controller.active().BaseCommit, initial)
	}
}

func TestComparisonSelectRejectsUnknownCommit(t *testing.T) {
	t.Parallel()
	controller, _, tip := newComparisonRepository(t)
	if _, err := controller.selectCommit(context.Background(), strings.Repeat("f", 40)); !errors.Is(err, errUnknownCommit) {
		t.Fatalf("selectCommit() error = %v, want errUnknownCommit", err)
	}
	// A rejected selection leaves the previous base untouched.
	if controller.active().BaseCommit != tip {
		t.Fatalf("active base = %s, want unchanged tip %s", controller.active().BaseCommit, tip)
	}
}

func TestComparisonConcurrentSelectionsStaySafe(t *testing.T) {
	t.Parallel()
	controller, initial, tip := newComparisonRepository(t)

	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for iteration := 0; iteration < 12; iteration++ {
				commit := tip
				if worker%2 == 0 {
					commit = initial
				}
				_, _ = controller.selectCommit(context.Background(), commit)
				_ = controller.active()
			}
		}(worker)
	}
	group.Wait()

	if base := controller.active().BaseCommit; base != initial && base != tip {
		t.Fatalf("final base = %s, want initial or tip", base)
	}
}
