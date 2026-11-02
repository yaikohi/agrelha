// Package gitops is agrelha's declarative plane: it clones yaya-ops, edits a
// ConfigMap data key in place (preserving YAML structure), commits as the agrelha
// identity, and pushes to main. ArgoCD (selfHeal) then reconciles the change.
package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"gopkg.in/yaml.v3"
)

type Committer struct {
	RepoURL, Branch         string
	Username, Token         string
	AuthorName, AuthorEmail string
	WorkDir                 string        // optional explicit path for working clone
	ReadTTL                 time.Duration // cache TTL for read operations (defaults to 5s if zero)

	mu        sync.Mutex
	repo      *git.Repository
	actualDir string
	autoClean bool
	lastSync  time.Time
}

func (c *Committer) auth() *githttp.BasicAuth {
	if c.Username == "" && c.Token == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: c.Username, Password: c.Token}
}

func (c *Committer) readTTL() time.Duration {
	if c.ReadTTL > 0 {
		return c.ReadTTL
	}
	return 5 * time.Second
}

func (c *Committer) ensureCloneLocked(ctx context.Context) (*git.Repository, bool, error) {
	if c.repo != nil && c.actualDir != "" {
		return c.repo, false, nil
	}

	dir := c.WorkDir
	autoClean := false
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "agrelha-gitops-")
		if err != nil {
			return nil, false, err
		}
		autoClean = true
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, false, err
		}
	}

	repo, err := git.PlainOpen(dir)
	if err == nil {
		c.repo = repo
		c.actualDir = dir
		c.autoClean = autoClean
		return repo, false, nil
	}

	repo, err = git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           c.RepoURL,
		Auth:          c.auth(),
		ReferenceName: plumbing.NewBranchReferenceName(c.Branch),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		if autoClean {
			_ = os.RemoveAll(dir)
		}
		return nil, false, fmt.Errorf("clone: %w", err)
	}

	c.repo = repo
	c.actualDir = dir
	c.autoClean = autoClean
	c.lastSync = time.Now()
	return repo, true, nil
}

func (c *Committer) cleanWorktreeLocked() {
	if c.repo == nil {
		return
	}
	if wt, err := c.repo.Worktree(); err == nil {
		_ = wt.Reset(&git.ResetOptions{Mode: git.HardReset})
		_ = wt.Clean(&git.CleanOptions{Dir: true})
	}
}

func (c *Committer) syncRemoteLocked(ctx context.Context) error {
	repo, cloned, err := c.ensureCloneLocked(ctx)
	if err != nil {
		return err
	}
	if cloned {
		return nil
	}

	err = repo.FetchContext(ctx, &git.FetchOptions{
		Auth:       c.auth(),
		RemoteName: "origin",
		Depth:      1,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", c.Branch, c.Branch)),
		},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("fetch: %w", err)
	}

	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", c.Branch), true)
	if err != nil {
		remoteRef, err = repo.Reference(plumbing.NewBranchReferenceName(c.Branch), true)
		if err != nil {
			return fmt.Errorf("resolve ref: %w", err)
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	if err := wt.Reset(&git.ResetOptions{
		Commit: remoteRef.Hash(),
		Mode:   git.HardReset,
	}); err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	_ = wt.Clean(&git.CleanOptions{Dir: true})
	c.lastSync = time.Now()
	return nil
}

// Close removes the working clone if it was automatically created in a temporary directory.
func (c *Committer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.autoClean && c.actualDir != "" {
		err := os.RemoveAll(c.actualDir)
		c.actualDir = ""
		c.repo = nil
		return err
	}
	return nil
}

// Patch updates a ConfigMap data key in place, committing and pushing if changed.
// It synchronizes against the remote branch on the persistent working clone.
func (c *Committer) Patch(ctx context.Context, relPath, dataKey, commitMsg string,
	transform func(current string) (string, error)) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.syncRemoteLocked(ctx); err != nil {
		return false, err
	}

	headRef, err := c.repo.Head()
	var headBefore plumbing.Hash
	if err == nil {
		headBefore = headRef.Hash()
	}

	full := filepath.Join(c.actualDir, relPath)
	raw, err := os.ReadFile(full)
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	node := findConfigMapValue(&doc, dataKey)
	if node == nil {
		c.cleanWorktreeLocked()
		return false, fmt.Errorf("data key %q not found in %s", dataKey, relPath)
	}
	updated, err := transform(node.Value)
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	if updated == node.Value {
		return false, nil // no change -> no commit
	}
	node.Value = updated

	out, err := yaml.Marshal(&doc)
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	if err := os.WriteFile(full, out, 0o644); err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	wt, err := c.repo.Worktree()
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	if _, err := wt.Add(relPath); err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	sig := &object.Signature{Name: c.AuthorName, Email: c.AuthorEmail, When: time.Now()}
	autoCommitMsg := "auto - " + commitMsg
	if _, err := wt.Commit(autoCommitMsg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}
	if err := c.repo.PushContext(ctx, &git.PushOptions{Auth: c.auth()}); err != nil {
		if !headBefore.IsZero() {
			_ = wt.Reset(&git.ResetOptions{Commit: headBefore, Mode: git.HardReset})
			_ = wt.Clean(&git.CleanOptions{Dir: true})
		}
		return false, fmt.Errorf("push: %w", err)
	}
	c.lastSync = time.Now()
	return true, nil
}

// findConfigMapValue walks a single-document ConfigMap YAML to the scalar node at
// data.<key>, returned by pointer so the caller can edit .Value in place.
func findConfigMapValue(doc *yaml.Node, key string) *yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0] // mapping
	data := mapValue(root, "data")
	if data == nil {
		return nil
	}
	return mapValue(data, key)
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
