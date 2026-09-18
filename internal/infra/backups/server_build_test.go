package backups

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBuild(t *testing.T, dir, name, body string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, serverBuildFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadServerBuildReportsAnUpdate(t *testing.T) {
	dir := t.TempDir()
	writeBuild(t, dir, "lareira-v2-01",
		`{"instance":"lareira-v2-01","installed":"25253791","latest":"25364309","checked_at":"2026-09-17T13:33:18Z"}`)

	b, err := ReadServerBuild(dir, "lareira-v2", 1)
	if err != nil || b == nil {
		t.Fatalf("ReadServerBuild: %v, %v", b, err)
	}
	if !b.UpdateAvailable() {
		t.Error("installed behind latest must report an available update")
	}
	if b.CheckedAt.IsZero() {
		t.Error("want the check timestamp parsed, so the UI can say how stale it is")
	}
}

func TestReadServerBuildUpToDate(t *testing.T) {
	dir := t.TempDir()
	writeBuild(t, dir, "boppo-02",
		`{"instance":"boppo-02","installed":"25364309","latest":"25364309","checked_at":"2026-09-17T13:33:18Z"}`)

	b, err := ReadServerBuild(dir, "boppo", 2)
	if err != nil || b == nil {
		t.Fatalf("ReadServerBuild: %v, %v", b, err)
	}
	if b.UpdateAvailable() {
		t.Error("matching builds must not report an update")
	}
	if !b.Known() {
		t.Error("want Known for a complete report")
	}
}

// The sidecar has not run, or the world has no build report. That is "unknown",
// never "up to date" - claiming the latter would hide a pending update.
func TestReadServerBuildMissingIsNotAnError(t *testing.T) {
	b, err := ReadServerBuild(t.TempDir(), "nothing-here", 3)
	if err != nil {
		t.Fatalf("a missing report must not be an error: %v", err)
	}
	if b != nil {
		t.Fatalf("want nil for a missing report, got %+v", b)
	}
}

// A Steam query that failed leaves latest empty. Reporting that as up to date
// would be a silent wrong answer, so Known and UpdateAvailable must both refuse.
func TestReadServerBuildHalfAnswerClaimsNothing(t *testing.T) {
	dir := t.TempDir()
	writeBuild(t, dir, "half-01", `{"instance":"half-01","installed":"25364309","latest":"","checked_at":""}`)

	b, err := ReadServerBuild(dir, "half", 1)
	if err != nil || b == nil {
		t.Fatalf("ReadServerBuild: %v, %v", b, err)
	}
	if b.Known() {
		t.Error("an incomplete report is not a known answer")
	}
	if b.UpdateAvailable() {
		t.Error("an incomplete report must not claim an update either way")
	}
}

func TestReadServerBuildRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	writeBuild(t, dir, "bad-01", `not json at all`)
	if _, err := ReadServerBuild(dir, "bad", 1); err == nil {
		t.Error("want an error for an unparseable report rather than a silent nil")
	}
}
