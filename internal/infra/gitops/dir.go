package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// WriteDirectory writes all given files into dirRelPath in the git repository,
// staging additions/modifications/deletions, committing, and pushing to the remote branch.
func (c *Committer) WriteDirectory(ctx context.Context, dirRelPath string, files map[string][]byte, commitMsg string) (bool, error) {
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

	targetDir := filepath.Join(c.actualDir, dirRelPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	existingEntries, _ := os.ReadDir(targetDir)
	for _, entry := range existingEntries {
		if entry.IsDir() {
			continue
		}
		if _, keep := files[entry.Name()]; !keep {
			_ = os.Remove(filepath.Join(targetDir, entry.Name()))
		}
	}

	for fname, content := range files {
		filePath := filepath.Join(targetDir, fname)
		if cur, err := os.ReadFile(filePath); err == nil && bytes.Equal(cur, content) {
			continue
		}
		if err := os.WriteFile(filePath, content, 0o644); err != nil {
			c.cleanWorktreeLocked()
			return false, err
		}
	}

	wt, err := c.repo.Worktree()
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	status, err := wt.Status()
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	hasChanges := false
	normPrefix := strings.Trim(dirRelPath, "/") + "/"
	for file, fileStatus := range status {
		if strings.HasPrefix(file, normPrefix) || file == strings.Trim(dirRelPath, "/") {
			if fileStatus.Worktree != git.Unmodified || fileStatus.Staging != git.Unmodified {
				hasChanges = true
				if fileStatus.Worktree == git.Deleted {
					_, _ = wt.Remove(file)
				} else {
					_, _ = wt.Add(file)
				}
			}
		}
	}

	if !hasChanges {
		return false, nil
	}

	sig := &object.Signature{Name: c.AuthorName, Email: c.AuthorEmail, When: time.Now()}
	if _, err := wt.Commit(commitMsg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
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

// DeleteDirectory removes dirRelPath from the git repo, staging the deletions, committing, and pushing.
func (c *Committer) DeleteDirectory(ctx context.Context, dirRelPath string, commitMsg string) (bool, error) {
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

	targetDir := filepath.Join(c.actualDir, dirRelPath)
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return false, nil
	}

	if err := os.RemoveAll(targetDir); err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	wt, err := c.repo.Worktree()
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	status, err := wt.Status()
	if err != nil {
		c.cleanWorktreeLocked()
		return false, err
	}

	hasChanges := false
	normPrefix := strings.Trim(dirRelPath, "/") + "/"
	for file, fileStatus := range status {
		if strings.HasPrefix(file, normPrefix) || file == strings.Trim(dirRelPath, "/") {
			hasChanges = true
			if fileStatus.Worktree == git.Deleted {
				_, _ = wt.Remove(file)
			}
		}
	}

	if !hasChanges {
		return false, nil
	}

	sig := &object.Signature{Name: c.AuthorName, Email: c.AuthorEmail, When: time.Now()}
	if _, err := wt.Commit(commitMsg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
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
