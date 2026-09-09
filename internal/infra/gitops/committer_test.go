package gitops

import (
	"context"
	"strings"
	"testing"
)

const initialConfigMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: test-ns
data:
  server.properties: "motd=Initial\nmax-players=10\n"
  whitelist.txt: "player1\n"
`

const initialConfigMapWithMetaYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: test-ns
  annotations:
    agrelha.dev/source: modpack
data:
  foo: bar
`

func TestPatch(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml": initialConfigMapYAML,
	})
	committer := newCommitter(remote)

	// 1. Successful change
	changed, err := committer.Patch(ctx, "manifests/cm.yaml", "whitelist.txt", "add player2", func(current string) (string, error) {
		return current + "player2\n", nil
	})
	if err != nil {
		t.Fatalf("Patch failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 2 { // 1 seed + 1 commit
		t.Fatalf("expected 2 commits, got %d", n)
	}
	content := readRemote(t, remote, "manifests/cm.yaml")
	if !strings.Contains(content, "player1") || !strings.Contains(content, "player2") {
		t.Fatalf("remote YAML does not contain player2: %s", content)
	}

	// 2. No-op path (no change -> no commit)
	changed, err = committer.Patch(ctx, "manifests/cm.yaml", "whitelist.txt", "no-op", func(current string) (string, error) {
		return current, nil
	})
	if err != nil {
		t.Fatalf("Patch no-op failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for no-op, got true")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected commit count to remain 2, got %d", n)
	}

	// 3. Missing key error
	_, err = committer.Patch(ctx, "manifests/cm.yaml", "nonexistent.txt", "missing", func(current string) (string, error) {
		return "something", nil
	})
	if err == nil {
		t.Fatalf("expected error for nonexistent key, got nil")
	}
}

func TestSetData(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml": initialConfigMapYAML,
	})
	committer := newCommitter(remote)

	// 1. Update existing key
	changed, err := committer.SetData(ctx, "manifests/cm.yaml", "whitelist.txt", "player1\nplayer2\n", "update whitelist")
	if err != nil {
		t.Fatalf("SetData failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected 2 commits, got %d", n)
	}

	// 2. Add new key
	changed, err = committer.SetData(ctx, "manifests/cm.yaml", "ops.txt", "admin1\n", "add ops")
	if err != nil {
		t.Fatalf("SetData new key failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 3 {
		t.Fatalf("expected 3 commits, got %d", n)
	}
	content := readRemote(t, remote, "manifests/cm.yaml")
	if !strings.Contains(content, "ops.txt") || !strings.Contains(content, "admin1") {
		t.Fatalf("remote YAML does not contain ops.txt: %s", content)
	}

	// 3. No-op path
	changed, err = committer.SetData(ctx, "manifests/cm.yaml", "ops.txt", "admin1\n", "re-add ops")
	if err != nil {
		t.Fatalf("SetData no-op failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false, got true")
	}
	if n := remoteCommitCount(t, remote); n != 3 {
		t.Fatalf("expected commit count to remain 3, got %d", n)
	}
}

func TestDeleteData(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml": initialConfigMapYAML,
	})
	committer := newCommitter(remote)

	// 1. Delete existing key
	changed, err := committer.DeleteData(ctx, "manifests/cm.yaml", "whitelist.txt", "remove whitelist")
	if err != nil {
		t.Fatalf("DeleteData failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected 2 commits, got %d", n)
	}
	content := readRemote(t, remote, "manifests/cm.yaml")
	if strings.Contains(content, "whitelist.txt") {
		t.Fatalf("remote YAML still contains whitelist.txt: %s", content)
	}

	// 2. Delete non-existent key (no-op)
	changed, err = committer.DeleteData(ctx, "manifests/cm.yaml", "whitelist.txt", "remove again")
	if err != nil {
		t.Fatalf("DeleteData non-existent failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for non-existent key, got true")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected commit count to remain 2, got %d", n)
	}
}

func TestReplaceData(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml": initialConfigMapYAML,
	})
	committer := newCommitter(remote)

	// 1. Replace with new data map
	newData := map[string]string{
		"alpha": "1",
		"beta":  "2",
	}
	changed, err := committer.ReplaceData(ctx, "manifests/cm.yaml", newData, "replace data")
	if err != nil {
		t.Fatalf("ReplaceData failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected 2 commits, got %d", n)
	}
	content := readRemote(t, remote, "manifests/cm.yaml")
	if !strings.Contains(content, "alpha") || !strings.Contains(content, "beta") || strings.Contains(content, "whitelist.txt") {
		t.Fatalf("remote YAML not expected after replace: %s", content)
	}

	// 2. No-op path
	changed, err = committer.ReplaceData(ctx, "manifests/cm.yaml", newData, "replace identical")
	if err != nil {
		t.Fatalf("ReplaceData identical failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for identical data, got true")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected commit count to remain 2, got %d", n)
	}
}

func TestReplaceConfigMap(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml":      initialConfigMapYAML,
		"manifests/cm_meta.yaml": initialConfigMapWithMetaYAML,
	})
	committer := newCommitter(remote)

	// 1. ReplaceConfigMap on CM without initial annotations creates them
	newData := map[string]string{"foo": "bar"}
	newAnn := map[string]string{"agrelha.dev/tier": "large"}
	changed, err := committer.ReplaceConfigMap(ctx, "manifests/cm.yaml", newData, newAnn, "add annotations and replace data")
	if err != nil {
		t.Fatalf("ReplaceConfigMap failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	content := readRemote(t, remote, "manifests/cm.yaml")
	if !strings.Contains(content, "agrelha.dev/tier: large") || !strings.Contains(content, "foo: bar") {
		t.Fatalf("remote YAML missing annotations or data: %s", content)
	}

	// 2. No-op on identical ReplaceConfigMap
	changed, err = committer.ReplaceConfigMap(ctx, "manifests/cm.yaml", newData, newAnn, "identical replace")
	if err != nil {
		t.Fatalf("ReplaceConfigMap identical failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for identical replace, got true")
	}

	// 3. Update existing annotations on cm_meta.yaml
	updatedAnn := map[string]string{
		"agrelha.dev/source": "modlist",
	}
	changed, err = committer.ReplaceConfigMap(ctx, "manifests/cm_meta.yaml", map[string]string{"foo": "bar"}, updatedAnn, "update annotations")
	if err != nil {
		t.Fatalf("ReplaceConfigMap update annotations failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	contentMeta := readRemote(t, remote, "manifests/cm_meta.yaml")
	if !strings.Contains(contentMeta, "agrelha.dev/source: modlist") {
		t.Fatalf("remote YAML missing updated annotation: %s", contentMeta)
	}
}

func TestWriteDirectoryAndDeleteDirectory(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/seed.yaml": "seed\n",
	})
	committer := newCommitter(remote)

	dirRel := "manifests/instance-01"
	files := map[string][]byte{
		"deployment.yaml": []byte("kind: Deployment\n"),
		"service.yaml":    []byte("kind: Service\n"),
	}

	// 1. WriteDirectory creates directory with files
	changed, err := committer.WriteDirectory(ctx, dirRel, files, "create instance-01")
	if err != nil {
		t.Fatalf("WriteDirectory failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected 2 commits, got %d", n)
	}
	if got := readRemote(t, remote, dirRel+"/deployment.yaml"); got != "kind: Deployment\n" {
		t.Fatalf("unexpected deployment.yaml content: %q", got)
	}
	if got := readRemote(t, remote, dirRel+"/service.yaml"); got != "kind: Service\n" {
		t.Fatalf("unexpected service.yaml content: %q", got)
	}

	// 2. WriteDirectory with identical files -> no-op
	changed, err = committer.WriteDirectory(ctx, dirRel, files, "no-op write")
	if err != nil {
		t.Fatalf("WriteDirectory identical failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for identical files, got true")
	}
	if n := remoteCommitCount(t, remote); n != 2 {
		t.Fatalf("expected commit count to remain 2, got %d", n)
	}

	// 3. WriteDirectory removes omitted file and updates existing file
	updatedFiles := map[string][]byte{
		"deployment.yaml": []byte("kind: Deployment\n# updated\n"),
		// service.yaml omitted -> should be deleted
		"pvc.yaml": []byte("kind: PVC\n"),
	}
	changed, err = committer.WriteDirectory(ctx, dirRel, updatedFiles, "update instance-01")
	if err != nil {
		t.Fatalf("WriteDirectory update failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 3 {
		t.Fatalf("expected 3 commits, got %d", n)
	}
	if got := readRemote(t, remote, dirRel+"/service.yaml"); got != "" {
		t.Fatalf("expected service.yaml to be removed, but got %q", got)
	}
	if got := readRemote(t, remote, dirRel+"/pvc.yaml"); got != "kind: PVC\n" {
		t.Fatalf("unexpected pvc.yaml: %q", got)
	}

	// 4. DeleteDirectory removes the directory
	changed, err = committer.DeleteDirectory(ctx, dirRel, "delete instance-01")
	if err != nil {
		t.Fatalf("DeleteDirectory failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true, got false")
	}
	if n := remoteCommitCount(t, remote); n != 4 {
		t.Fatalf("expected 4 commits, got %d", n)
	}
	if got := readRemote(t, remote, dirRel+"/deployment.yaml"); got != "" {
		t.Fatalf("expected deployment.yaml to be deleted, got %q", got)
	}

	// 5. DeleteDirectory non-existent directory -> no-op
	changed, err = committer.DeleteDirectory(ctx, dirRel, "delete again")
	if err != nil {
		t.Fatalf("DeleteDirectory again failed: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for deleting nonexistent dir, got true")
	}
	if n := remoteCommitCount(t, remote); n != 4 {
		t.Fatalf("expected commit count to remain 4, got %d", n)
	}
}
