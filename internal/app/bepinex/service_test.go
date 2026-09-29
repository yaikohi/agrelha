package bepinex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"agrelha/internal/domain"
)

const miningCfg = `## Settings file was created by plugin Mining v1.1.6
## Plugin GUID: org.bepinex.plugins.mining

[2 - Mining]

## Mining yield factor at skill level 100.
# Setting type: Single
# Default value: 2
# Acceptable value range: From 1 to 5
Mining Yield Factor = 2
`

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type fakeWriter struct {
	saved   map[string]string
	deleted []string
	err     error
}

func (f *fakeWriter) SaveConfig(_ context.Context, _ int, name, content string, _ ...string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.saved == nil {
		f.saved = map[string]string{}
	}
	f.saved[name] = content
	return true, nil
}

func (f *fakeWriter) DeleteConfig(_ context.Context, _ int, name string, _ ...string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.deleted = append(f.deleted, name)
	return true, nil
}

type harness struct {
	svc    *Service
	writer *fakeWriter
	inst   domain.Instance
	body   string
	stored map[string]string
	reads  int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		writer: &fakeWriter{},
		inst:   domain.Instance{GameID: domain.GameValheim, Number: 2, Slug: "boppo"},
		body:   miningCfg,
		stored: map[string]string{},
	}
	h.svc = New(
		WithSnapshot(func(string, int) (*domain.ConfigSnapshot, error) {
			return &domain.ConfigSnapshot{
				Instance: "boppo-02",
				Files: []domain.SnapshotFile{{
					Name: "mining.cfg", Size: int64(len(h.body)), SHA256: digestOf(h.body),
				}},
			}, nil
		}),
		WithFile(func(_ string, _ int, _ string) (string, string, error) {
			h.reads++
			return h.body, digestOf(h.body), nil
		}),
		WithOverrides(func(context.Context, int) (map[string]string, error) {
			return h.stored, nil
		}),
		WithWriter(h.writer),
	)
	return h
}

func TestFilesReportsSettingsAndOverrideCounts(t *testing.T) {
	h := newHarness(t)
	h.stored["mining.cfg"] = "[2 - Mining]\nMining Yield Factor = 3\n"

	files, snap, err := h.svc.Files(context.Background(), h.inst)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Known() || len(files) != 1 {
		t.Fatalf("want one published file, got %d", len(files))
	}
	f := files[0]
	if f.PluginName != "Mining" || f.PluginVersion != "1.1.6" {
		t.Errorf("plugin = %q %q", f.PluginName, f.PluginVersion)
	}
	if f.Settings != 1 {
		t.Errorf("settings = %d, want 1", f.Settings)
	}
	if f.Overridden != 1 {
		t.Errorf("overridden = %d, want 1", f.Overridden)
	}
	if !f.Parsable {
		t.Error("a real mod config should be parsable")
	}
}

// Nothing published is a different answer from "this world has no configs".
func TestNoSnapshotIsNotAnEmptyConfigSet(t *testing.T) {
	svc := New(WithSnapshot(func(string, int) (*domain.ConfigSnapshot, error) { return nil, nil }))
	_, _, err := svc.Files(context.Background(), domain.Instance{Slug: "boppo", Number: 2})
	if !errors.Is(err, ErrNotPublished) {
		t.Errorf("want ErrNotPublished, got %v", err)
	}
	if _, err := svc.File(context.Background(), domain.Instance{Slug: "boppo", Number: 2}, "x.cfg"); !errors.Is(err, ErrNotPublished) {
		t.Errorf("want ErrNotPublished, got %v", err)
	}
}

func TestApplyWritesTheOverrideSet(t *testing.T) {
	h := newHarness(t)
	changed, err := h.svc.Apply(context.Background(), h.inst, "mining.cfg", digestOf(h.body),
		[]Change{{Section: "2 - Mining", Name: "Mining Yield Factor", Value: "4"}}, "ykhi")
	if err != nil || !changed {
		t.Fatalf("apply failed: %v", err)
	}
	got := h.writer.saved["mining.cfg"]
	if !strings.Contains(got, "Mining Yield Factor = 4") {
		t.Errorf("stored override set = %q", got)
	}
	// Only the override, never the whole generated file: three of instance-02's
	// configs exceed 100 KB against a 1 MiB ConfigMap ceiling.
	if strings.Contains(got, "# Setting type:") {
		t.Error("the generated file's metadata must never be stored in git")
	}
}

// An Override that happens to equal the current default is still an Override:
// it records a decision, and a mod update can move the default underneath it.
func TestAnOverrideEqualToTheDefaultIsStillRecorded(t *testing.T) {
	h := newHarness(t)
	changed, err := h.svc.Apply(context.Background(), h.inst, "mining.cfg", digestOf(h.body),
		[]Change{{Section: "2 - Mining", Name: "Mining Yield Factor", Value: "2"}}, "ykhi")
	if err != nil || !changed {
		t.Fatalf("apply failed: %v", err)
	}
	if !strings.Contains(h.writer.saved["mining.cfg"], "Mining Yield Factor = 2") {
		t.Error("an override matching the default must still be stored")
	}
}

// Deleting the key, not writing an empty one: an empty value would make the
// merge tool write an empty file over a good config.
func TestForgettingTheLastOverrideDeletesTheKey(t *testing.T) {
	h := newHarness(t)
	h.stored["mining.cfg"] = "[2 - Mining]\nMining Yield Factor = 3\n"

	changed, err := h.svc.Apply(context.Background(), h.inst, "mining.cfg", digestOf(h.body),
		[]Change{{Section: "2 - Mining", Name: "Mining Yield Factor", Forget: true}}, "ykhi")
	if err != nil || !changed {
		t.Fatalf("apply failed: %v", err)
	}
	if len(h.writer.deleted) != 1 || h.writer.deleted[0] != "mining.cfg" {
		t.Errorf("want the key deleted, got saved=%v deleted=%v", h.writer.saved, h.writer.deleted)
	}
	if _, ok := h.writer.saved["mining.cfg"]; ok {
		t.Error("an emptied override set must not be written as an empty value")
	}
}

func TestStaleDigestIsRefused(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Apply(context.Background(), h.inst, "mining.cfg", "0000deadbeef",
		[]Change{{Section: "2 - Mining", Name: "Mining Yield Factor", Value: "4"}}, "ykhi")
	if !errors.Is(err, ErrStaleSnapshot) {
		t.Errorf("want ErrStaleSnapshot, got %v", err)
	}
	if len(h.writer.saved) != 0 {
		t.Error("nothing should have been written")
	}
}

func TestApplyRejectsAValueTheModWontTake(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Apply(context.Background(), h.inst, "mining.cfg", digestOf(h.body),
		[]Change{{Section: "2 - Mining", Name: "Mining Yield Factor", Value: "99"}}, "ykhi")
	if err == nil {
		t.Fatal("99 is outside the mod's stated range 1..5 and must be refused")
	}
	if !strings.Contains(err.Error(), "range") {
		t.Errorf("the message should name the constraint, got: %v", err)
	}
	if len(h.writer.saved) != 0 {
		t.Error("a rejected value must not be committed")
	}
}

// A mod that drops a Setting, or is uninstalled for an afternoon, must not stop
// the operator keeping their decision.
func TestOverrideForASettingTheModNoLongerDeclaresIsAllowed(t *testing.T) {
	h := newHarness(t)
	changed, err := h.svc.Apply(context.Background(), h.inst, "mining.cfg", digestOf(h.body),
		[]Change{{Section: "Gone", Name: "Old Setting", Value: "anything"}}, "ykhi")
	if err != nil || !changed {
		t.Fatalf("apply failed: %v", err)
	}
	if !strings.Contains(h.writer.saved["mining.cfg"], "Old Setting = anything") {
		t.Error("the override should still have been recorded")
	}
}

// "Reset to default" must WRITE the default, not forget the override. Removing
// an override restores nothing - the file on the PVC keeps its current value -
// so a reset that only forgot would visibly do nothing.
func TestResetToDefaultWritesTheDefaultRatherThanForgetting(t *testing.T) {
	h := newHarness(t)
	h.stored["mining.cfg"] = "[2 - Mining]\nMining Yield Factor = 5\n"

	changed, err := h.svc.ResetToDefault(context.Background(), h.inst, "mining.cfg", digestOf(h.body),
		"2 - Mining", "Mining Yield Factor", "ykhi")
	if err != nil || !changed {
		t.Fatalf("reset failed: %v", err)
	}
	got := h.writer.saved["mining.cfg"]
	if !strings.Contains(got, "Mining Yield Factor = 2") {
		t.Errorf("reset must pin the default value, got %q", got)
	}
	if len(h.writer.deleted) != 0 {
		t.Error("reset must not delete the override set")
	}
}

func TestResetRefusesWhenThereIsNoDefaultToRestore(t *testing.T) {
	h := newHarness(t)
	h.body = "[S]\n\nKey = 1\n"

	if _, err := h.svc.ResetToDefault(context.Background(), h.inst, "mining.cfg", "", "S", "Key", "ykhi"); err == nil {
		t.Error("a setting with no recorded default cannot be reset")
	}
	if _, err := h.svc.ResetToDefault(context.Background(), h.inst, "mining.cfg", "", "S", "Nope", "ykhi"); err == nil {
		t.Error("a setting the mod does not declare cannot be reset")
	}
}

// Parsing 1,398 settings on every keystroke of a search is not viable, but a
// file BepInEx rewrote must never be served from the cache.
func TestParsedConfigIsCachedUntilTheFileMoves(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.svc.File(ctx, h.inst, "mining.cfg"); err != nil {
		t.Fatal(err)
	}
	first := h.reads
	if _, err := h.svc.File(ctx, h.inst, "mining.cfg"); err != nil {
		t.Fatal(err)
	}
	if h.reads != first {
		t.Errorf("second read should have come from the cache, reads went %d -> %d", first, h.reads)
	}

	h.body = strings.Replace(miningCfg, "Mining Yield Factor = 2", "Mining Yield Factor = 3", 1)
	view, err := h.svc.File(ctx, h.inst, "mining.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if h.reads == first {
		t.Error("a rewritten file must be re-read, not served from the cache")
	}
	s, _ := view.Config.Lookup("2 - Mining", "Mining Yield Factor")
	if s.Value != "3" {
		t.Errorf("stale value served: %q", s.Value)
	}
}

func TestFileNotInTheSnapshotIsNamed(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.File(context.Background(), h.inst, "nothere.cfg")
	if err == nil || !strings.Contains(err.Error(), "nothere.cfg") {
		t.Errorf("the error should name the file, got %v", err)
	}
}

func TestNilServiceDoesNotPanic(t *testing.T) {
	var s *Service
	if _, _, err := s.Files(context.Background(), domain.Instance{}); !errors.Is(err, ErrNotPublished) {
		t.Errorf("want ErrNotPublished, got %v", err)
	}
	if _, err := s.File(context.Background(), domain.Instance{}, "x"); !errors.Is(err, ErrNotPublished) {
		t.Errorf("want ErrNotPublished, got %v", err)
	}
	if _, err := s.Apply(context.Background(), domain.Instance{}, "x", "", nil, "a"); !errors.Is(err, ErrNotPublished) {
		t.Errorf("want ErrNotPublished, got %v", err)
	}
}
