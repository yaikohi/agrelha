package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"agrelha/internal/infra/gitops"
	"agrelha/internal/ports"
)

const testBranch = "main"

func newTestRemote(t *testing.T, seed map[string]string) string {
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

func TestNilAdapter(t *testing.T) {
	ctx := context.Background()
	check := func(name string, a *Adapter) {
		t.Run(name, func(t *testing.T) {
			if _, err := a.Get(ctx, "foo"); err != ports.ErrNotImplemented {
				t.Errorf("Get: want ErrNotImplemented, got %v", err)
			}
			if err := a.Put(ctx, "foo", ports.Document{}, "msg"); err != ports.ErrNotImplemented {
				t.Errorf("Put: want ErrNotImplemented, got %v", err)
			}
			if _, err := a.Patch(ctx, "foo", "msg", nil); err != ports.ErrNotImplemented {
				t.Errorf("Patch: want ErrNotImplemented, got %v", err)
			}
			if err := a.Delete(ctx, "foo", "msg"); err != ports.ErrNotImplemented {
				t.Errorf("Delete: want ErrNotImplemented, got %v", err)
			}
			if err := a.PutTree(ctx, "foo", nil, "msg"); err != ports.ErrNotImplemented {
				t.Errorf("PutTree: want ErrNotImplemented, got %v", err)
			}
		})
	}
	check("nil_receiver", nil)
	check("nil_committer", New(nil))
}

func TestGitStateStoreOperations(t *testing.T) {
	ctx := context.Background()
	cmContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: test-ns
  annotations:
    agrelha.dev/tier: medium
data:
  mods.txt: "mod1\n"
  whitelist.txt: "player1\n"
`
	remote := newTestRemote(t, map[string]string{
		"manifests/cm.yaml": cmContent,
	})
	committer := &gitops.Committer{
		RepoURL: remote, Branch: testBranch,
		AuthorName: "agrelha", AuthorEmail: "agrelha@example.test",
	}
	adapter := New(committer)

	// 1. Get
	doc, err := adapter.Get(ctx, "manifests/cm.yaml")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if doc.Data["mods.txt"] != "mod1\n" || doc.Data["whitelist.txt"] != "player1\n" {
		t.Fatalf("Get: unexpected Data: %+v", doc.Data)
	}
	if doc.Annotations["agrelha.dev/tier"] != "medium" {
		t.Fatalf("Get: unexpected Annotations: %+v", doc.Annotations)
	}

	// 2. Patch
	changed, err := adapter.Patch(ctx, "manifests/cm.yaml", "add mod2", func(d *ports.Document) (bool, error) {
		d.Data["mods.txt"] = d.Data["mods.txt"] + "mod2\n"
		return true, nil
	})
	if err != nil {
		t.Fatalf("Patch failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	// Verify patch via Get
	doc2, err := adapter.Get(ctx, "manifests/cm.yaml")
	if err != nil {
		t.Fatalf("Get after patch failed: %v", err)
	}
	if doc2.Data["mods.txt"] != "mod1\nmod2\n" {
		t.Fatalf("unexpected mods.txt after patch: %q", doc2.Data["mods.txt"])
	}

	// 3. Patch no-op
	changed, err = adapter.Patch(ctx, "manifests/cm.yaml", "no-op", func(d *ports.Document) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("Patch no-op failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false")
	}

	// 4. Put (replace data + annotations)
	newDoc := ports.Document{
		Data: map[string]string{
			"mods.txt": "mod3\n",
		},
		Annotations: map[string]string{
			"agrelha.dev/tier": "large",
		},
	}
	if err := adapter.Put(ctx, "manifests/cm.yaml", newDoc, "replace cm"); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	doc3, err := adapter.Get(ctx, "manifests/cm.yaml")
	if err != nil {
		t.Fatalf("Get after put failed: %v", err)
	}
	if doc3.Data["mods.txt"] != "mod3\n" || doc3.Annotations["agrelha.dev/tier"] != "large" {
		t.Fatalf("unexpected doc after put: %+v", doc3)
	}

	// 5. PutTree
	tree := map[string]ports.Document{
		"deployment.yaml": {Raw: []byte("kind: Deployment\n")},
		"service.yaml":    {Raw: []byte("kind: Service\n")},
	}
	if err := adapter.PutTree(ctx, "manifests/instance-01", tree, "put tree"); err != nil {
		t.Fatalf("PutTree failed: %v", err)
	}
	depDoc, err := adapter.Get(ctx, "manifests/instance-01/deployment.yaml")
	if err != nil {
		t.Fatalf("Get deployment.yaml failed: %v", err)
	}
	if string(depDoc.Raw) != "kind: Deployment\n" {
		t.Fatalf("unexpected raw deployment: %q", string(depDoc.Raw))
	}

	// 6. Delete
	if err := adapter.Delete(ctx, "manifests/instance-01", "delete tree"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = adapter.Get(ctx, "manifests/instance-01/deployment.yaml")
	if err == nil {
		t.Fatalf("expected error getting deleted file, got nil")
	}
}
