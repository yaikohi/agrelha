// Package gitops is agrelha's declarative plane: it clones yaya-ops, edits a
// ConfigMap data key in place (preserving YAML structure), commits as the agrelha
// identity, and pushes to main. ArgoCD (selfHeal) then reconciles the change.
package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"gopkg.in/yaml.v3"
)

type Committer struct {
	RepoURL, Branch         string
	Username, Token         string
	AuthorName, AuthorEmail string
}

// Patch clones the repo, applies `transform` to configmap `dataKey` inside the
// YAML file at relPath, and commits+pushes if the value changed. Returns
// (changed, error). A no-op transform makes no commit.
func (c *Committer) Patch(ctx context.Context, relPath, dataKey, commitMsg string,
	transform func(current string) (string, error)) (bool, error) {

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

	full := filepath.Join(dir, relPath)
	raw, err := os.ReadFile(full)
	if err != nil {
		return false, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return false, err
	}
	node := findConfigMapValue(&doc, dataKey)
	if node == nil {
		return false, fmt.Errorf("data key %q not found in %s", dataKey, relPath)
	}
	updated, err := transform(node.Value)
	if err != nil {
		return false, err
	}
	if updated == node.Value {
		return false, nil // no change -> no commit
	}
	node.Value = updated

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(full, out, 0o644); err != nil {
		return false, err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return false, err
	}
	if _, err := wt.Add(relPath); err != nil {
		return false, err
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
