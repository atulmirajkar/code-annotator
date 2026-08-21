package gitdiff

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseRevisionOptions(t *testing.T) {
	t.Parallel()
	sha1 := strings.Repeat("a", 40)
	sha256 := strings.Repeat("B", 64)
	tests := []struct {
		name    string
		input   []byte
		want    []RevisionOption
		wantErr bool
	}{
		{name: "empty", want: []RevisionOption{}},
		{name: "SHA-1 and SHA-256", input: []byte(sha1 + "\x00first subject\x00" + sha256 + "\x00second subject\x00"), want: []RevisionOption{
			{Commit: sha1, Subject: "first subject"},
			{Commit: strings.ToLower(sha256), Subject: "second subject"},
		}},
		{name: "empty subject", input: []byte(sha1 + "\x00\x00"), want: []RevisionOption{{Commit: sha1}}},
		{name: "missing terminator", input: []byte(sha1 + "\x00subject"), wantErr: true},
		{name: "odd fields", input: []byte(sha1 + "\x00"), wantErr: true},
		{name: "invalid commit", input: []byte("abc\x00subject\x00"), wantErr: true},
		{name: "invalid UTF-8", input: append([]byte(sha1+"\x00"), 0xff, 0), wantErr: true},
		{name: "duplicate commit", input: []byte(sha1 + "\x00one\x00" + sha1 + "\x00two\x00"), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRevisionOptions(test.input)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidRevisionList) {
					t.Fatalf("parseRevisionOptions() error = %v, want ErrInvalidRevisionList", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRevisionOptions() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseRevisionOptions() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRecentCommits(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tests := []struct {
		name     string
		subjects []string
	}{
		{name: "local history", subjects: []string{"second local commit", "third local commit"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepository(t)
			for index, subject := range test.subjects {
				path := fmt.Sprintf("commit-%d.txt", index)
				writeRepositoryFile(t, repository, path, subject)
				runRepositoryGit(t, repository, "add", path)
				runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", subject)
			}
			configuration, err := Open(context.Background(), repository, "HEAD")
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			options, err := configuration.RecentCommits(context.Background())
			if err != nil {
				t.Fatalf("RecentCommits() error = %v", err)
			}
			wantSubjects := []string{"third local commit", "second local commit", "initial"}
			if len(options) != len(wantSubjects) {
				t.Fatalf("RecentCommits() count = %d, want %d: %#v", len(options), len(wantSubjects), options)
			}
			for index, wantSubject := range wantSubjects {
				if options[index].Subject != wantSubject || !validObjectID(options[index].Commit) {
					t.Errorf("RecentCommits()[%d] = %#v, want subject %q and full object ID", index, options[index], wantSubject)
				}
			}
		})
	}
}

func TestParseHeadDistances(t *testing.T) {
	t.Parallel()
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)
	tests := []struct {
		name    string
		input   []byte
		want    map[string]int
		wantErr bool
	}{
		{name: "empty", want: map[string]int{}},
		{name: "ordered ancestry", input: []byte(first + "\x00" + second + "\x00"), want: map[string]int{first: 0, second: 1}},
		{name: "missing terminator", input: []byte(first + "\x00" + second), wantErr: true},
		{name: "invalid commit", input: []byte("nothex\x00"), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseHeadDistances(test.input)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidRevisionList) {
					t.Fatalf("parseHeadDistances() error = %v, want ErrInvalidRevisionList", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHeadDistances() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseHeadDistances() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestHeadDistances(t *testing.T) {
	t.Parallel()
	requireGit(t)
	repository := newRepository(t)
	initial := strings.ToLower(strings.TrimSpace(runRepositoryGit(t, repository, "rev-parse", "HEAD")))
	writeRepositoryFile(t, repository, "README.md", "second")
	runRepositoryGit(t, repository, "add", "README.md")
	runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "second")
	tip := strings.ToLower(strings.TrimSpace(runRepositoryGit(t, repository, "rev-parse", "HEAD")))

	configuration, err := Open(context.Background(), repository, "HEAD")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	distances, err := configuration.HeadDistances(context.Background())
	if err != nil {
		t.Fatalf("HeadDistances() error = %v", err)
	}
	if distances[tip] != 0 || distances[initial] != 1 {
		t.Fatalf("HeadDistances() = %#v, want tip 0 and initial 1", distances)
	}
}

func TestReresolveAdoptsMovingTip(t *testing.T) {
	t.Parallel()
	requireGit(t)
	repository := newRepository(t)
	configuration, err := Open(context.Background(), repository, "HEAD")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	frozen := configuration.BaseCommit

	writeRepositoryFile(t, repository, "README.md", "second")
	runRepositoryGit(t, repository, "add", "README.md")
	runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "second")

	resolved, err := configuration.Reresolve(context.Background())
	if err != nil {
		t.Fatalf("Reresolve() error = %v", err)
	}
	if resolved.BaseCommit == frozen {
		t.Fatal("Reresolve() kept the stale base after HEAD advanced")
	}
	if resolved.RequestedBase != configuration.RequestedBase || resolved.RepositoryRoot != configuration.RepositoryRoot || resolved.ContentPrefix != configuration.ContentPrefix {
		t.Fatalf("Reresolve() changed identity fields: %#v", resolved)
	}
	if configuration.BaseCommit != frozen {
		t.Fatal("Reresolve() mutated the receiver base")
	}
}

func TestReresolveRejectsUnconfigured(t *testing.T) {
	t.Parallel()
	if _, err := (Config{}).Reresolve(context.Background()); err == nil {
		t.Fatal("Reresolve() error = nil, want unconfigured error")
	}
}

func TestRecentCommitsRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		configuration Config
	}{
		{name: "empty configuration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.configuration.RecentCommits(context.Background()); err == nil {
				t.Fatal("RecentCommits() error = nil, want invalid configuration")
			}
		})
	}
}
