// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const miningGenerated = `## Settings file was created by plugin Mining v1.1.6
## Plugin GUID: org.bepinex.plugins.mining

[2 - Mining]

## Mining yield factor at skill level 100.
# Setting type: Single
# Default value: 2
# Acceptable value range: From 1 to 5
Mining Yield Factor = 2
`

func mergeDirs(t *testing.T) (generated, overrides string) {
	t.Helper()
	root := t.TempDir()
	generated = filepath.Join(root, "generated")
	overrides = filepath.Join(root, "overrides")
	if err := os.MkdirAll(generated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(overrides, 0o755); err != nil {
		t.Fatal(err)
	}
	return generated, overrides
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runMerge(t *testing.T, generated, overrides string) (code int, out, errOut string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code = run(t.Context(), &stdout, &stderr, []string{
		"bepinex", "merge",
		"--generated=" + generated,
		"--overrides=" + overrides,
		"--out=" + generated,
	})
	return code, stdout.String(), stderr.String()
}

func TestMergeCommandAppliesAnOverrideInPlace(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, generated, "org.bepinex.plugins.mining.cfg", miningGenerated)
	write(t, overrides, "org.bepinex.plugins.mining.cfg", "[2 - Mining]\nMining Yield Factor = 3\n")

	code, out, errOut := runMerge(t, generated, overrides)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}

	got := read(t, generated, "org.bepinex.plugins.mining.cfg")
	if !strings.Contains(got, "Mining Yield Factor = 3") {
		t.Error("the override was not applied")
	}
	if !strings.Contains(got, "## Settings file was created by plugin Mining v1.1.6") {
		t.Error("the file header must survive")
	}
	if !strings.Contains(got, "# Default value: 2") {
		t.Error("the metadata must survive, or the UI loses the defaults it reads from here")
	}
}

func TestMergeCommandLeavesAnUpToDateFileAlone(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, generated, "x.cfg", miningGenerated)
	write(t, overrides, "x.cfg", "[2 - Mining]\nMining Yield Factor = 2\n")

	before, err := os.Stat(filepath.Join(generated, "x.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, _ := runMerge(t, generated, overrides)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	after, err := os.Stat(filepath.Join(generated, "x.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("an unchanged file must not be rewritten")
	}
	if !strings.Contains(out, "already up to date") {
		t.Errorf("expected the no-op to be reported, got: %s", out)
	}
}

func TestMergeCommandWritesABareFileOnAFreshPVC(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, overrides, "new.cfg", "[General]\nThing = 5\n")

	code, out, errOut := runMerge(t, generated, overrides)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	got := read(t, generated, "new.cfg")
	if !strings.Contains(got, "[General]") || !strings.Contains(got, "Thing = 5") {
		t.Errorf("expected a bare config, got %q", got)
	}
}

func TestMergeCommandNeverTouchesAFileWithNoOverrides(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, generated, "untouched.cfg", miningGenerated)
	write(t, overrides, "other.cfg", "[General]\nThing = 5\n")

	if code, out, errOut := runMerge(t, generated, overrides); code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if got := read(t, generated, "untouched.cfg"); got != miningGenerated {
		t.Error("a config with no override set must be left exactly as it was")
	}
}

func TestMergeCommandReportsAnOverrideTheModDoesNotDeclare(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, generated, "x.cfg", miningGenerated)
	write(t, overrides, "x.cfg", "[Vanished]\nOld Setting = 7\n")

	code, out, _ := runMerge(t, generated, overrides)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "not declared by the mod") {
		t.Errorf("an unmatched override must be reported, got: %s", out)
	}
	if !strings.Contains(read(t, generated, "x.cfg"), "Old Setting = 7") {
		t.Error("it must still be written; a recorded decision outlives a mod being absent")
	}
}

func TestMergeCommandWithNoOverridesDirectoryStillBoots(t *testing.T) {
	generated, _ := mergeDirs(t)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, []string{
		"bepinex", "merge",
		"--generated=" + generated,
		"--overrides=" + filepath.Join(generated, "does-not-exist"),
	})
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to do") {
		t.Errorf("expected a clean no-op, got: %s", stdout.String())
	}
}

func TestMergeCommandIgnoresConfigMapProjectionEntries(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, generated, "real.cfg", miningGenerated)
	write(t, overrides, "real.cfg", "[2 - Mining]\nMining Yield Factor = 4\n")
	if err := os.MkdirAll(filepath.Join(overrides, "..2026_09_28_00_00_00.1234"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..2026_09_28_00_00_00.1234", filepath.Join(overrides, "..data")); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runMerge(t, generated, overrides)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(read(t, generated, "real.cfg"), "Mining Yield Factor = 4") {
		t.Error("the real override should still have been applied")
	}
}

func TestMergeCommandRejectsMissingFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), &stdout, &stderr, []string{"bepinex", "merge"}); code != 2 {
		t.Errorf("expected usage exit 2, got %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), &stdout, &stderr, []string{"bepinex"}); code != 2 {
		t.Errorf("expected usage exit 2, got %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), &stdout, &stderr, []string{"bepinex", "nonsense"}); code != 2 {
		t.Errorf("expected usage exit 2 for an unknown subcommand, got %d", code)
	}
}

func TestMergeCommandEmptyOverrideSetLeavesTheFileAlone(t *testing.T) {
	generated, overrides := mergeDirs(t)
	write(t, generated, "x.cfg", miningGenerated)
	write(t, overrides, "x.cfg", "# only a comment\n")

	if code, out, errOut := runMerge(t, generated, overrides); code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if got := read(t, generated, "x.cfg"); got != miningGenerated {
		t.Errorf("file was modified by an empty override set: %q", got)
	}
}
