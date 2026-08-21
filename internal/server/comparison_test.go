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

// newComparisonRepository freezes a two-commit worktree at HEAD and returns a
// controller plus the initial and tip commit IDs used by selection assertions.
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
	controller, err := newComparisonController(configuration, nil, "", "")
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

func TestComparisonRefreshAdoptsMovingTip(t *testing.T) {
	t.Parallel()
	controller, _, tip := newComparisonRepository(t)

	// The startup snapshot froze HEAD; advancing HEAD and refreshing must adopt
	// the new tip because the moving configured revision is active.
	repository := controller.configured.RepositoryRoot
	writeTestFile(t, repository+"/main.go", "package refreshed\n")
	runServerTestGit(t, repository, "add", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "third")
	newTip := revParse(t, repository, "HEAD")

	before := controller.snapshot()
	refreshed, err := controller.refresh(context.Background(), before.revision)
	if err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	if refreshed.explicit {
		t.Fatal("refresh() explicit = true, want moving base")
	}
	if refreshed.config.BaseCommit != newTip {
		t.Fatalf("refresh() base = %s, want %s", refreshed.config.BaseCommit, newTip)
	}
	if refreshed.revision == before.revision {
		t.Fatal("refresh() reused the previous state revision")
	}
	if !containsCommit(refreshed.options, tip) {
		t.Fatalf("refresh() options = %#v, want to include prior tip %s", refreshed.options, tip)
	}
}

func TestComparisonSelectPinsCommitAndRefreshRetainsIt(t *testing.T) {
	t.Parallel()
	controller, initial, _ := newComparisonRepository(t)

	seeded, err := controller.refresh(context.Background(), controller.snapshot().revision)
	if err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	selected, err := controller.selectCommit(context.Background(), seeded.revision, initial)
	if err != nil {
		t.Fatalf("selectCommit() error = %v", err)
	}
	if !selected.explicit {
		t.Fatal("selectCommit() explicit = false, want pinned base")
	}
	if selected.config.BaseCommit != initial {
		t.Fatalf("selectCommit() base = %s, want %s", selected.config.BaseCommit, initial)
	}
	if selected.config.RequestedBase != abbreviatedCommit(initial) {
		t.Fatalf("selectCommit() requested base = %s, want %s", selected.config.RequestedBase, abbreviatedCommit(initial))
	}

	// A refresh must retain the pinned commit while still updating options.
	refreshed, err := controller.refresh(context.Background(), selected.revision)
	if err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	if !refreshed.explicit || refreshed.config.BaseCommit != initial {
		t.Fatalf("refresh() after pin = %+v, want retained pinned commit %s", refreshed, initial)
	}
}

func TestComparisonSelectConfiguredCommitReturnsToMoving(t *testing.T) {
	t.Parallel()
	controller, initial, tip := newComparisonRepository(t)

	seeded, err := controller.refresh(context.Background(), controller.snapshot().revision)
	if err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	pinned, err := controller.selectCommit(context.Background(), seeded.revision, initial)
	if err != nil {
		t.Fatalf("selectCommit(initial) error = %v", err)
	}
	// Selecting the configured revision's current commit reverts to moving mode.
	moving, err := controller.selectCommit(context.Background(), pinned.revision, tip)
	if err != nil {
		t.Fatalf("selectCommit(tip) error = %v", err)
	}
	if moving.explicit {
		t.Fatal("selectCommit(tip) explicit = true, want moving base")
	}
	if moving.config.RequestedBase != controller.configured.RequestedBase {
		t.Fatalf("selectCommit(tip) requested base = %s, want %s", moving.config.RequestedBase, controller.configured.RequestedBase)
	}
}

func TestComparisonSelectRejectsStaleAndUnknown(t *testing.T) {
	t.Parallel()
	controller, initial, _ := newComparisonRepository(t)
	seeded, err := controller.refresh(context.Background(), controller.snapshot().revision)
	if err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	if _, err := controller.refresh(context.Background(), "stale-revision"); !errors.Is(err, errStaleComparison) {
		t.Fatalf("refresh() stale error = %v, want errStaleComparison", err)
	}
	if _, err := controller.selectCommit(context.Background(), "stale-revision", initial); !errors.Is(err, errStaleComparison) {
		t.Fatalf("selectCommit() stale error = %v, want errStaleComparison", err)
	}
	if _, err := controller.selectCommit(context.Background(), seeded.revision, strings.Repeat("f", 40)); !errors.Is(err, errUnknownCommit) {
		t.Fatalf("selectCommit() unknown error = %v, want errUnknownCommit", err)
	}
}

func TestComparisonConcurrentMutationsStaySafe(t *testing.T) {
	t.Parallel()
	controller, initial, tip := newComparisonRepository(t)
	if _, err := controller.refresh(context.Background(), controller.snapshot().revision); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for iteration := 0; iteration < 12; iteration++ {
				current := controller.snapshot()
				switch worker % 3 {
				case 0:
					_, _ = controller.refresh(context.Background(), current.revision)
				case 1:
					_, _ = controller.selectCommit(context.Background(), current.revision, initial)
				default:
					_, _ = controller.selectCommit(context.Background(), current.revision, tip)
				}
			}
		}(worker)
	}
	group.Wait()

	final := controller.snapshot()
	if final.revision == "" || final.config.BaseCommit == "" {
		t.Fatalf("final snapshot is inconsistent: %+v", final)
	}
}
