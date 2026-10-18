package bepinex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const generatedCfg = `[General]
## How loud
Volume = 50

[Advanced]
Debug = false
`

func TestOverridesAreAppliedToTheGeneratedFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoldout.cfg")
	if err := os.WriteFile(path, []byte(generatedCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	results := MergeDir(dir, dir, map[string]string{
		"zoldout.cfg": "[General]\nVolume = 90\n",
	})
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("merge: %v", results[0].Err)
	}
	if !results[0].Changed {
		t.Fatal("an override with a new value must rewrite the file")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Volume = 90") {
		t.Errorf("override not applied:\n%s", body)
	}
	if !strings.Contains(string(body), "## How loud") {
		t.Error("the mod's own comments must survive a merge")
	}
	if !strings.Contains(string(body), "Debug = false") {
		t.Error("untouched sections must survive a merge")
	}
}

func TestAnAlreadyCorrectFileIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoldout.cfg")
	_ = os.WriteFile(path, []byte(generatedCfg), 0o644)
	before, _ := os.Stat(path)

	results := MergeDir(dir, dir, map[string]string{"zoldout.cfg": "[General]\nVolume = 50\n"})
	if results[0].Changed {
		t.Error("a no-op merge must not rewrite the file, or every converge would look like a change")
	}
	after, _ := os.Stat(path)
	if before.ModTime() != after.ModTime() {
		t.Error("file was rewritten despite no change")
	}
}

func TestEmptyOverrideSetIsSkipped(t *testing.T) {
	dir := t.TempDir()
	results := MergeDir(dir, dir, map[string]string{"zoldout.cfg": "# only a comment\n"})
	if !results[0].Skipped {
		t.Error("an empty override set must be skipped, not written as an empty config")
	}
	if _, err := os.Stat(filepath.Join(dir, "zoldout.cfg")); !os.IsNotExist(err) {
		t.Error("nothing should have been created")
	}
}

func TestMergeCreatesTheFileWhenTheModHasNotGeneratedItYet(t *testing.T) {
	dir := t.TempDir()
	results := MergeDir(dir, dir, map[string]string{"new.cfg": "[General]\nVolume = 10\n"})
	if results[0].Err != nil {
		t.Fatalf("a world that has not run yet must still accept overrides: %v", results[0].Err)
	}
	if !results[0].Changed {
		t.Fatal("expected the file to be written")
	}
	body, err := os.ReadFile(filepath.Join(dir, "new.cfg"))
	if err != nil || !strings.Contains(string(body), "Volume = 10") {
		t.Errorf("override not written: %v %s", err, body)
	}
}
