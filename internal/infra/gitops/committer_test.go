package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestReadFileAndDocument(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml": initialConfigMapWithMetaYAML,
		"plain.txt":         "hello world\n",
	})
	committer := newCommitter(remote)

	// 1. ReadFile plain text
	raw, err := committer.ReadFile(ctx, "plain.txt")
	if err != nil || string(raw) != "hello world\n" {
		t.Fatalf("ReadFile failed: raw=%q, err=%v", string(raw), err)
	}

	// 2. ReadFile nonexistent
	_, err = committer.ReadFile(ctx, "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error reading nonexistent file")
	}

	// 3. ReadDocument valid yaml with data & annotations
	data, ann, docRaw, err := committer.ReadDocument(ctx, "manifests/cm.yaml")
	if err != nil {
		t.Fatalf("ReadDocument failed: %v", err)
	}
	if len(docRaw) == 0 {
		t.Fatal("expected non-empty raw bytes")
	}
	if data["foo"] != "bar" {
		t.Errorf("expected data[foo]=bar, got %v", data)
	}
	if ann["agrelha.dev/source"] != "modpack" {
		t.Errorf("expected annotation modpack, got %v", ann)
	}

	// 4. ReadDocument on non-yaml plain text file
	dataPlain, annPlain, _, err := committer.ReadDocument(ctx, "plain.txt")
	if err != nil || dataPlain != nil || annPlain != nil {
		t.Errorf("expected nil data and annotations for plain text, got data=%v ann=%v err=%v", dataPlain, annPlain, err)
	}

	// 5. ReadDocument nonexistent
	_, _, _, err = committer.ReadDocument(ctx, "nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error reading nonexistent document")
	}
}

func TestPatchDocument(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml":      initialConfigMapYAML,
		"manifests/cm-meta.yaml": initialConfigMapWithMetaYAML,
	})
	committer := newCommitter(remote)

	// 1. Unchanged patch
	changed, err := committer.PatchDocument(ctx, "manifests/cm.yaml", "no-op", func(data, ann map[string]string) (bool, error) {
		return false, nil
	})
	if err != nil || changed {
		t.Fatalf("expected changed=false, got changed=%v err=%v", changed, err)
	}

	// 2. Mutate data and add new annotations
	changed, err = committer.PatchDocument(ctx, "manifests/cm.yaml", "update data and add ann", func(data, ann map[string]string) (bool, error) {
		data["whitelist.txt"] = "player1\nplayer2\n"
		ann["agrelha.dev/new"] = "true"
		return true, nil
	})
	if err != nil || !changed {
		t.Fatalf("expected changed=true, got changed=%v err=%v", changed, err)
	}
	cmContent := readRemote(t, remote, "manifests/cm.yaml")
	if !strings.Contains(cmContent, "player2") || !strings.Contains(cmContent, "agrelha.dev/new") {
		t.Errorf("patched content missing expected fields: %s", cmContent)
	}

	// 3. Mutate existing annotations on cm-meta.yaml
	changed, err = committer.PatchDocument(ctx, "manifests/cm-meta.yaml", "update ann", func(data, ann map[string]string) (bool, error) {
		ann["agrelha.dev/source"] = "manual"
		return true, nil
	})
	if err != nil || !changed {
		t.Fatalf("expected changed=true for ann update, got %v err=%v", changed, err)
	}
	metaContent := readRemote(t, remote, "manifests/cm-meta.yaml")
	if !strings.Contains(metaContent, "agrelha.dev/source: manual") {
		t.Errorf("expected updated annotation, got: %s", metaContent)
	}

	// 4. Mutator returns error
	_, err = committer.PatchDocument(ctx, "manifests/cm.yaml", "fail", func(data, ann map[string]string) (bool, error) {
		return false, context.Canceled
	})
	if err == nil {
		t.Fatal("expected error when mutate function returns error")
	}

	// 5. File has no data map
	remoteNoData := newRemote(t, map[string]string{
		"manifests/nodata.yaml": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: foo\n",
	})
	cNoData := newCommitter(remoteNoData)
	_, err = cNoData.PatchDocument(ctx, "manifests/nodata.yaml", "edit", func(data, ann map[string]string) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("expected error when YAML has no data map")
	}
}

func TestWriteDirectory_And_DeleteDirectory(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"config/initial.txt": "hello",
	})
	committer := newCommitter(remote)

	// 1. Write directory with 2 files
	files := map[string][]byte{
		"server.properties": []byte("motd=Hello"),
		"whitelist.json":    []byte("[]"),
	}
	changed, err := committer.WriteDirectory(ctx, "minecraft-01", files, "create mc01")
	if err != nil || !changed {
		t.Fatalf("WriteDirectory failed: changed=%v, err=%v", changed, err)
	}

	// 2. Write identical files -> no-op (changed=false)
	changed, err = committer.WriteDirectory(ctx, "minecraft-01", files, "no-op")
	if err != nil || changed {
		t.Fatalf("WriteDirectory identical files want changed=false, got changed=%v, err=%v", changed, err)
	}

	// 3. Modify one file and remove another
	updatedFiles := map[string][]byte{
		"server.properties": []byte("motd=Updated"),
		"ops.json":          []byte("[]"),
	}
	changed, err = committer.WriteDirectory(ctx, "minecraft-01", updatedFiles, "update mc01")
	if err != nil || !changed {
		t.Fatalf("WriteDirectory update failed: changed=%v, err=%v", changed, err)
	}
	sp := readRemote(t, remote, "minecraft-01/server.properties")
	if sp != "motd=Updated" {
		t.Errorf("server.properties = %q, want 'motd=Updated'", sp)
	}

	// 4. DeleteDirectory on existing directory
	changed, err = committer.DeleteDirectory(ctx, "minecraft-01", "delete mc01")
	if err != nil || !changed {
		t.Fatalf("DeleteDirectory failed: changed=%v, err=%v", changed, err)
	}

	// 5. DeleteDirectory on non-existent directory -> changed=false, nil
	changed, err = committer.DeleteDirectory(ctx, "minecraft-01", "delete again")
	if err != nil || changed {
		t.Fatalf("DeleteDirectory non-existent want changed=false, got changed=%v, err=%v", changed, err)
	}
}

func TestReplaceConfigMap_And_DeleteData_EdgeCases(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml": initialConfigMapYAML,
	})
	committer := newCommitter(remote)

	// ReplaceConfigMap
	newData := map[string]string{"foo": "bar"}
	newAnnotations := map[string]string{"agrelha.dev/source": "replaced"}
	changed, err := committer.ReplaceConfigMap(ctx, "manifests/cm.yaml", newData, newAnnotations, "replace full cm")
	if err != nil || !changed {
		t.Fatalf("ReplaceConfigMap failed: changed=%v, err=%v", changed, err)
	}
	content := readRemote(t, remote, "manifests/cm.yaml")
	if !strings.Contains(content, "foo: bar") || !strings.Contains(content, "agrelha.dev/source: replaced") {
		t.Fatalf("ReplaceConfigMap did not update remote: %s", content)
	}

	// DeleteData for non-existent key returns false, nil
	changed, err = committer.DeleteData(ctx, "manifests/cm.yaml", "nonexistent-key", "delete key")
	if err != nil || changed {
		t.Fatalf("DeleteData nonexistent want false, got changed=%v, err=%v", changed, err)
	}

	// ReadFile success and failure
	readBytes, err := committer.ReadFile(ctx, "manifests/cm.yaml")
	if err != nil || len(readBytes) == 0 {
		t.Fatalf("ReadFile failed: %v", err)
	}
	_, err = committer.ReadFile(ctx, "manifests/does-not-exist.yaml")
	if err == nil {
		t.Fatalf("ReadFile non-existent want error, got nil")
	}

	// ReadDocument success and failure
	data, ann, raw, err := committer.ReadDocument(ctx, "manifests/cm.yaml")
	if err != nil || data["foo"] != "bar" || len(raw) == 0 || ann["agrelha.dev/source"] != "replaced" {
		t.Fatalf("ReadDocument failed: data=%+v, ann=%+v, err=%v", data, ann, err)
	}
	_, _, _, err = committer.ReadDocument(ctx, "manifests/does-not-exist.yaml")
	if err == nil {
		t.Fatalf("ReadDocument non-existent want error, got nil")
	}
}

func TestCommitter_CloneFailures(t *testing.T) {
	ctx := context.Background()
	badCommitter := &Committer{
		RepoURL: "/nonexistent/repo.git",
		Branch:  "main",
	}

	if _, err := badCommitter.Patch(ctx, "any", "key", "msg", func(s string) (string, error) { return s, nil }); err == nil {
		t.Error("Patch expected clone error")
	}
	if _, err := badCommitter.SetData(ctx, "any", "k", "v", "msg"); err == nil {
		t.Error("SetData expected clone error")
	}
	if _, err := badCommitter.WriteDirectory(ctx, "dir", map[string][]byte{"f": []byte("x")}, "msg"); err == nil {
		t.Error("WriteDirectory expected clone error")
	}
	if _, err := badCommitter.DeleteDirectory(ctx, "dir", "msg"); err == nil {
		t.Error("DeleteDirectory expected clone error")
	}
	if _, err := badCommitter.ReadFile(ctx, "f"); err == nil {
		t.Error("ReadFile expected clone error")
	}
	if _, _, _, err := badCommitter.ReadDocument(ctx, "f"); err == nil {
		t.Error("ReadDocument expected clone error")
	}
}

func TestGitops_RemainingErrorBranches(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"manifests/cm.yaml":     initialConfigMapYAML,
		"manifests/bad.yaml":    ": bad: yaml:",
		"manifests/empty.yaml":  "",
		"manifests/nodata.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n",
		"manifests/nometa.yaml": "apiVersion: v1\nkind: ConfigMap\ndata:\n  foo: bar\n",
		"dir/sub/file.txt":      "subcontent",
		"dir/file1.txt":         "content1",
	})
	committer := newCommitter(remote)

	// 1. Patch error branches
	if _, err := committer.Patch(ctx, "manifests/nonexistent.yaml", "key", "msg", func(s string) (string, error) { return s, nil }); err == nil {
		t.Error("Patch nonexistent file expected error")
	}
	if _, err := committer.Patch(ctx, "manifests/bad.yaml", "key", "msg", func(s string) (string, error) { return s, nil }); err == nil {
		t.Error("Patch bad yaml expected error")
	}
	if _, err := committer.Patch(ctx, "manifests/empty.yaml", "key", "msg", func(s string) (string, error) { return s, nil }); err == nil {
		t.Error("Patch empty yaml expected error")
	}
	if _, err := committer.Patch(ctx, "manifests/nodata.yaml", "key", "msg", func(s string) (string, error) { return s, nil }); err == nil {
		t.Error("Patch nodata expected error")
	}
	if _, err := committer.Patch(ctx, "manifests/cm.yaml", "whitelist.txt", "msg", func(string) (string, error) { return "", errors.New("transform err") }); err == nil {
		t.Error("Patch transform expected error")
	}

	// 2. SetData error branches & single-line value style
	if _, err := committer.SetData(ctx, "manifests/nonexistent.yaml", "key", "val", "msg"); err == nil {
		t.Error("SetData nonexistent expected error")
	}
	if _, err := committer.SetData(ctx, "manifests/bad.yaml", "key", "val", "msg"); err == nil {
		t.Error("SetData bad yaml expected error")
	}
	if _, err := committer.SetData(ctx, "manifests/empty.yaml", "key", "val", "msg"); err == nil {
		t.Error("SetData empty yaml expected error")
	}
	if _, err := committer.SetData(ctx, "manifests/nodata.yaml", "key", "val", "msg"); err == nil {
		t.Error("SetData nodata expected error")
	}
	// Single-line value update (v.Style = 0)
	changed, err := committer.SetData(ctx, "manifests/cm.yaml", "whitelist.txt", "singleline", "single line update")
	if err != nil || !changed {
		t.Errorf("SetData singleline failed: changed=%v, err=%v", changed, err)
	}

	// 3. DeleteData error branches
	if _, err := committer.DeleteData(ctx, "manifests/nonexistent.yaml", "key", "msg"); err == nil {
		t.Error("DeleteData nonexistent expected error")
	}
	if _, err := committer.DeleteData(ctx, "manifests/nodata.yaml", "key", "msg"); err == nil {
		t.Error("DeleteData nodata expected error")
	}

	// 4. ReplaceData error branches
	if _, err := committer.ReplaceData(ctx, "manifests/nonexistent.yaml", map[string]string{"k": "v"}, "msg"); err == nil {
		t.Error("ReplaceData nonexistent expected error")
	}
	if _, err := committer.ReplaceData(ctx, "manifests/nodata.yaml", map[string]string{"k": "v"}, "msg"); err == nil {
		t.Error("ReplaceData nodata expected error")
	}

	// 5. ReplaceConfigMap error branches
	if _, err := committer.ReplaceConfigMap(ctx, "manifests/nodata.yaml", map[string]string{"k": "v"}, nil, "msg"); err == nil {
		t.Error("ReplaceConfigMap nodata expected error")
	}
	if _, err := committer.ReplaceConfigMap(ctx, "manifests/nometa.yaml", map[string]string{"k": "v"}, nil, "msg"); err == nil {
		t.Error("ReplaceConfigMap nometa expected error")
	}

	// 6. ReadDocument fallback on bad/empty yaml
	d1, a1, r1, err1 := committer.ReadDocument(ctx, "manifests/bad.yaml")
	if err1 != nil || d1 != nil || a1 != nil || len(r1) == 0 {
		t.Errorf("ReadDocument on bad yaml: d=%v, a=%v, r=%v, err=%v", d1, a1, r1, err1)
	}
	d2, a2, r2, err2 := committer.ReadDocument(ctx, "manifests/empty.yaml")
	if err2 != nil || d2 != nil || a2 != nil || len(r2) != 0 {
		t.Errorf("ReadDocument on empty yaml: d=%v, a=%v, r=%v, err=%v", d2, a2, r2, err2)
	}

	// 7. Non-mapping node helpers: upsertData, deleteData, replaceData
	scalarNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "scalar"}
	if upsertData(scalarNode, "k", "v") {
		t.Error("expected upsertData on scalarNode to return false")
	}
	if deleteData(scalarNode, "k") {
		t.Error("expected deleteData on scalarNode to return false")
	}
	if replaceData(scalarNode, map[string]string{"k": "v"}) {
		t.Error("expected replaceData on scalarNode to return false")
	}

	// 8. DeleteDirectory on non-existent directory -> false, nil
	changed, err = committer.DeleteDirectory(ctx, "nonexistent-dir", "msg")
	if err != nil || changed {
		t.Errorf("DeleteDirectory nonexistent: changed=%v, err=%v", changed, err)
	}

	// 9. WriteDirectory preserving subdirectories
	changed, err = committer.WriteDirectory(ctx, "dir", map[string][]byte{"file1.txt": []byte("content1")}, "msg")
	if err != nil {
		t.Errorf("WriteDirectory with subdirs failed: %v", err)
	}
}

func TestCommitter_DirectoryOperations(t *testing.T) {
	ctx := context.Background()
	remote := newRemote(t, map[string]string{
		"testdir/file1.txt":    "one",
		"testdir/file2.txt":    "two",
		"testdir/sub/file.txt": "sub",
		"emptydir/.gitkeep":    "",
	})
	committer := newCommitter(remote)

	// 1. WriteDirectory no changes -> false, nil
	changed, err := committer.WriteDirectory(ctx, "testdir", map[string][]byte{
		"file1.txt": []byte("one"),
		"file2.txt": []byte("two"),
	}, "no-op")
	if err != nil {
		t.Fatalf("WriteDirectory no-op failed: %v", err)
	}
	if changed {
		t.Errorf("expected changed=false for identical files")
	}

	// 2. WriteDirectory with removed file and modified file
	changed, err = committer.WriteDirectory(ctx, "testdir", map[string][]byte{
		"file1.txt": []byte("one-modified"),
	}, "update dir")
	if err != nil {
		t.Fatalf("WriteDirectory update failed: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true")
	}

	// 3. DeleteDirectory on existing dir with files
	changed, err = committer.DeleteDirectory(ctx, "testdir", "delete testdir")
	if err != nil {
		t.Fatalf("DeleteDirectory failed: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true for delete testdir")
	}

	// 4. DeleteDirectory on empty directory (contains no tracked files)
	// First write an empty dir in a worktree, or remove .gitkeep
	changed, err = committer.DeleteDirectory(ctx, "emptydir", "delete emptydir")
	if err != nil {
		t.Fatalf("DeleteDirectory emptydir failed: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true for deleting emptydir with .gitkeep")
	}
}

func TestCommitter_TempDirFailures(t *testing.T) {
	ctx := context.Background()
	committer := &Committer{
		RepoURL: "/any",
		Branch:  "main",
	}

	t.Setenv("TMPDIR", "/dev/null/impossible/path")

	if _, err := committer.Patch(ctx, "file.yaml", "key", "msg", func(s string) (string, error) { return s, nil }); err == nil {
		t.Error("expected Patch to fail on invalid TMPDIR")
	}
	if _, err := committer.SetData(ctx, "file.yaml", "key", "val", "msg"); err == nil {
		t.Error("expected SetData to fail on invalid TMPDIR")
	}
	if _, err := committer.WriteDirectory(ctx, "dir", map[string][]byte{"f": []byte("x")}, "msg"); err == nil {
		t.Error("expected WriteDirectory to fail on invalid TMPDIR")
	}
	if _, err := committer.DeleteDirectory(ctx, "dir", "msg"); err == nil {
		t.Error("expected DeleteDirectory to fail on invalid TMPDIR")
	}
	if _, err := committer.ReadFile(ctx, "file.yaml"); err == nil {
		t.Error("expected ReadFile to fail on invalid TMPDIR")
	}
}

func TestCommitter_PushFailures(t *testing.T) {
	ctx := context.Background()

	setupFailingRemote := func(t *testing.T) (*Committer, string) {
		t.Helper()
		remote := newRemote(t, map[string]string{
			"manifests/cm.yaml": initialConfigMapYAML,
			"dir/file.txt":      "hello",
		})
		// Install a pre-receive hook in remote.git that always rejects pushes
		hookDir := filepath.Join(remote, "hooks")
		_ = os.MkdirAll(hookDir, 0o755)
		hookFile := filepath.Join(hookDir, "pre-receive")
		_ = os.WriteFile(hookFile, []byte("#!/bin/sh\nexit 1\n"), 0o755)
		return newCommitter(remote), remote
	}

	t.Run("Patch push failure", func(t *testing.T) {
		committer, _ := setupFailingRemote(t)
		_, err := committer.Patch(ctx, "manifests/cm.yaml", "whitelist.txt", "msg", func(s string) (string, error) {
			return s + "newplayer\n", nil
		})
		if err == nil {
			t.Error("expected push failure on Patch")
		}
	})

	t.Run("SetData push failure", func(t *testing.T) {
		committer, _ := setupFailingRemote(t)
		_, err := committer.SetData(ctx, "manifests/cm.yaml", "whitelist.txt", "changed", "msg")
		if err == nil {
			t.Error("expected push failure on SetData")
		}
	})

	t.Run("WriteDirectory push failure", func(t *testing.T) {
		committer, _ := setupFailingRemote(t)
		_, err := committer.WriteDirectory(ctx, "dir", map[string][]byte{"file.txt": []byte("updated")}, "msg")
		if err == nil {
			t.Error("expected push failure on WriteDirectory")
		}
	})

	t.Run("DeleteDirectory push failure", func(t *testing.T) {
		committer, _ := setupFailingRemote(t)
		_, err := committer.DeleteDirectory(ctx, "dir", "msg")
		if err == nil {
			t.Error("expected push failure on DeleteDirectory")
		}
	})
}

