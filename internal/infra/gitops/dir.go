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
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// WriteDirectory writes all given files into dirRelPath in the git repository,
// staging additions/modifications/deletions, committing, and pushing to the remote branch.
func (c *Committer) WriteDirectory(ctx context.Context, dirRelPath string, files map[string][]byte, commitMsg string) (bool, error) {
	dir, err := os.MkdirTemp("", "agrelha-git-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)

	auth := &githttp.BasicAuth{Username: c.Username, Password: c.Token}
	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           c.RepoURL,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(c.Branch),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return false, fmt.Errorf("clone: %w", err)
	}

	targetDir := filepath.Join(dir, dirRelPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
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
			return false, err
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return false, err
	}

	status, err := wt.Status()
	if err != nil {
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
		return false, err
	}
	if err := repo.PushContext(ctx, &git.PushOptions{Auth: auth}); err != nil {
		return false, fmt.Errorf("push: %w", err)
	}
	return true, nil
}

// DeleteDirectory removes dirRelPath from the git repo, staging the deletions, committing, and pushing.
func (c *Committer) DeleteDirectory(ctx context.Context, dirRelPath string, commitMsg string) (bool, error) {
	dir, err := os.MkdirTemp("", "agrelha-git-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)

	auth := &githttp.BasicAuth{Username: c.Username, Password: c.Token}
	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           c.RepoURL,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(c.Branch),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return false, fmt.Errorf("clone: %w", err)
	}

	targetDir := filepath.Join(dir, dirRelPath)
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return false, nil
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return false, err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return false, err
	}

	status, err := wt.Status()
	if err != nil {
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
		return false, err
	}
	if err := repo.PushContext(ctx, &git.PushOptions{Auth: auth}); err != nil {
		return false, fmt.Errorf("push: %w", err)
	}
	return true, nil
}
