package gitops

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const testBranch = "main"

// newRemote creates a bare repository on disk seeded with the given files and
// returns its path. Everything runs offline: the Committer clones and pushes
// over the filesystem transport, so these tests exercise the real go-git code
// paths without a network or a forge.
func newRemote(t *testing.T, seed map[string]string) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")

	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}

	work := filepath.Join(root, "seed")
	repo, err := git.PlainInit(work, false)
	if err != nil {
		t.Fatalf("init seed worktree: %v", err)
	}
	// Point HEAD at the branch before the first commit; you cannot check out a
	// branch in a repository that has no commits yet.
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD, plumbing.NewBranchReferenceName(testBranch),
	)); err != nil {
		t.Fatalf("set HEAD to %s: %v", testBranch, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	for name, content := range seed {
		full := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	sig := &object.Signature{Name: "seed", Email: "seed@example.test"}
	if _, err := wt.Commit("seed", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remote}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	return remote
}

func newCommitter(remote string) *Committer {
	return &Committer{
		RepoURL: remote, Branch: testBranch,
		AuthorName: "agrelha", AuthorEmail: "agrelha@example.test",
	}
}

// readRemote clones the remote fresh and returns one file's contents, so
// assertions describe what actually landed in the repository.
func readRemote(t *testing.T, remote, relPath string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainClone(dir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName(testBranch),
		SingleBranch:  true,
	}); err != nil {
		t.Fatalf("clone remote: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, relPath))
	if err != nil {
		return ""
	}
	return string(b)
}

// remoteCommitCount tells us whether a call actually committed.
func remoteCommitCount(t *testing.T, remote string) int {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainClone(dir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName(testBranch),
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("clone remote: %v", err)
	}
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = iter.ForEach(func(*object.Commit) error { n++; return nil })
	return n
}

func TestHarnessRoundTrips(t *testing.T) {
	remote := newRemote(t, map[string]string{"manifests/x.yaml": "hello\n"})
	if got := readRemote(t, remote, "manifests/x.yaml"); got != "hello\n" {
		t.Fatalf("seed not readable back, got %q", got)
	}
	if n := remoteCommitCount(t, remote); n != 1 {
		t.Fatalf("commit count = %d, want 1", n)
	}
}
