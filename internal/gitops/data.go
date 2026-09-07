package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"gopkg.in/yaml.v3"
)

func (c *Committer) SetData(ctx context.Context, relPath, key, value, commitMsg string) (bool, error) {
	return c.mutate(ctx, relPath, commitMsg, func(data *yaml.Node) (bool, error) {
		return upsertData(data, key, value), nil
	})
}

func (c *Committer) DeleteData(ctx context.Context, relPath, key, commitMsg string) (bool, error) {
	return c.mutate(ctx, relPath, commitMsg, func(data *yaml.Node) (bool, error) {
		return deleteData(data, key), nil
	})
}

func (c *Committer) ReplaceData(ctx context.Context, relPath string, data map[string]string, commitMsg string) (bool, error) {
	return c.mutate(ctx, relPath, commitMsg, func(node *yaml.Node) (bool, error) {
		return replaceData(node, data), nil
	})
}

func (c *Committer) mutate(ctx context.Context, relPath, commitMsg string, fn func(data *yaml.Node) (bool, error)) (bool, error) {
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
	if len(doc.Content) == 0 {
		return false, fmt.Errorf("empty yaml %s", relPath)
	}
	data := mapValue(doc.Content[0], "data")
	if data == nil {
		return false, fmt.Errorf("no data map in %s", relPath)
	}

	changed, err := fn(data)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}

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

func upsertData(data *yaml.Node, key, value string) bool {
	if data.Kind != yaml.MappingNode {
		return false
	}
	if v := mapValue(data, key); v != nil {
		if v.Value == value {
			return false
		}
		v.Value = value
		v.Tag = "!!str"
		if strings.Contains(value, "\n") {
			v.Style = yaml.LiteralStyle
		} else {
			v.Style = 0
		}
		return true
	}
	kn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	vn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	if strings.Contains(value, "\n") {
		vn.Style = yaml.LiteralStyle
	}
	data.Content = append(data.Content, kn, vn)
	data.Style = 0
	return true
}

func deleteData(data *yaml.Node, key string) bool {
	if data.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(data.Content); i += 2 {
		if data.Content[i].Value == key {
			data.Content = append(data.Content[:i], data.Content[i+2:]...)
			return true
		}
	}
	return false
}

func replaceData(data *yaml.Node, next map[string]string) bool {
	if data.Kind != yaml.MappingNode {
		return false
	}

	keys := make([]string, 0, len(next))
	for k := range next {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	content := make([]*yaml.Node, 0, len(keys)*2)
	for _, k := range keys {
		v := next[k]
		kn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
		vn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		if strings.Contains(v, "\n") {
			vn.Style = yaml.LiteralStyle
		}
		content = append(content, kn, vn)
	}

	if sameMapping(data.Content, content) {
		return false
	}
	data.Content = content
	data.Style = 0
	return true
}

func sameMapping(a, b []*yaml.Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}
