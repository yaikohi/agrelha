// Package bepinex serves the operator's view of a Valheim world's mod
// configuration: what Settings the mods expose, which of them the operator has
// pinned, and what a change would do.
//
// It exists apart from app/instances because the two answer different
// questions. instances owns an Instance's lifecycle and its Mod list; this owns
// the reconciliation of two sources that neither of them controls - the
// Generated configs a mod writes on the PVC, and the Override set agrelha keeps
// in git.
package bepinex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"agrelha/internal/domain"
)

// ErrStaleSnapshot is returned when a save was composed against a Generated
// config that has since been rewritten. BepInEx rewrites these files on its own
// schedule, so writing an Override against a stale view could pin the wrong
// Setting.
var ErrStaleSnapshot = errors.New("the server rewrote this config while you were editing it")

// ErrNotPublished is returned when nothing has been published for an Instance
// yet. It is not the same as the world having no configs, and the UI must say
// so differently.
var ErrNotPublished = errors.New("no configs have been published for this world yet")

// SnapshotReader reports what the publish sidecar last copied out of an
// Instance's BepInEx config directory.
type SnapshotReader func(slug string, num int) (*domain.ConfigSnapshot, error)

// FileReader returns one published Generated config and the digest of the
// bytes actually read.
type FileReader func(slug string, num int, name string) (body, sha256 string, err error)

// OverridesReader returns the Instance's stored Override sets, keyed by config
// file name. It reads the live ConfigMap rather than git: a StateStore.Get
// clones the whole repository, which is not something a page load can do.
type OverridesReader func(ctx context.Context, num int) (map[string]string, error)

// OverridesWriter persists one Override set, or removes it when the body is
// empty. Both are delegated to the instance manager so that every write to git
// goes through one path.
type OverridesWriter interface {
	SaveConfig(ctx context.Context, num int, filename, content string, actor ...string) (bool, error)
	DeleteConfig(ctx context.Context, num int, filename string, actor ...string) (bool, error)
}

type Option func(*Service)

func WithSnapshot(fn SnapshotReader) Option   { return func(s *Service) { s.snapshot = fn } }
func WithFile(fn FileReader) Option           { return func(s *Service) { s.file = fn } }
func WithOverrides(fn OverridesReader) Option { return func(s *Service) { s.overrides = fn } }
func WithWriter(w OverridesWriter) Option     { return func(s *Service) { s.writer = w } }

// Service answers questions about one world's mod configuration.
type Service struct {
	snapshot  SnapshotReader
	file      FileReader
	overrides OverridesReader
	writer    OverridesWriter

	mu    sync.Mutex
	cache map[string]domain.ConfigFile
}

func New(opts ...Option) *Service {
	s := &Service{cache: map[string]domain.ConfigFile{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// FileSummary is one Generated config as the file list shows it, without
// parsing anything the list does not display.
type FileSummary struct {
	Name          string
	PluginName    string
	PluginVersion string
	Settings      int
	Changed       int
	Overridden    int
	Size          int64
	Parsable      bool
}

// Files lists the Generated configs published for an Instance, with the
// Override count for each.
func (s *Service) Files(ctx context.Context, inst domain.Instance) ([]FileSummary, *domain.ConfigSnapshot, error) {
	if s == nil || s.snapshot == nil {
		return nil, nil, ErrNotPublished
	}
	snap, err := s.snapshot(inst.Slug, inst.Number)
	if err != nil {
		return nil, nil, err
	}
	if !snap.Known() {
		return nil, nil, ErrNotPublished
	}

	stored, err := s.storedOverrides(ctx, inst.Number)
	if err != nil {
		return nil, snap, err
	}

	out := make([]FileSummary, 0, len(snap.Files))
	for _, sf := range snap.Files {
		sum := FileSummary{Name: sf.Name, Size: sf.Size}
		if set, ok := stored[sf.Name]; ok {
			sum.Overridden = set.Len()
		}
		// Parsing a 207 KB file to count its settings is the expensive part, so
		// it is cached on the digest and only redone when the file moves.
		if f, err := s.parsed(inst, sf); err == nil {
			sum.PluginName = f.PluginName
			sum.PluginVersion = f.PluginVersion
			sum.Settings = f.Count()
			sum.Changed = f.ChangedCount()
			sum.Parsable = f.Parsable()
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, snap, nil
}

// FileView is one Generated config together with what the operator has pinned
// in it.
type FileView struct {
	Config    domain.ConfigFile
	Overrides domain.OverrideSet
	SHA256    string
	Size      int64
}

// File returns one Generated config, its Override set, and the digest a
// subsequent save must present.
func (s *Service) File(ctx context.Context, inst domain.Instance, name string) (FileView, error) {
	if s == nil || s.snapshot == nil || s.file == nil {
		return FileView{}, ErrNotPublished
	}
	snap, err := s.snapshot(inst.Slug, inst.Number)
	if err != nil {
		return FileView{}, err
	}
	if !snap.Known() {
		return FileView{}, ErrNotPublished
	}
	sf, ok := snap.Lookup(name)
	if !ok {
		return FileView{}, fmt.Errorf("%q is not in the published config set", name)
	}

	f, err := s.parsed(inst, sf)
	if err != nil {
		return FileView{}, err
	}
	stored, err := s.storedOverrides(ctx, inst.Number)
	if err != nil {
		return FileView{}, err
	}
	set, ok := stored[name]
	if !ok {
		set = domain.NewOverrideSet()
	}
	return FileView{Config: f, Overrides: set, SHA256: sf.SHA256, Size: sf.Size}, nil
}

// Change is one edit the operator made.
type Change struct {
	Section string
	Name    string
	Value   string
	// Forget removes the Override instead of setting one. It does NOT restore
	// the mod's default: the Generated config keeps whatever value was last
	// written to it, and only a fresh write changes that.
	Forget bool
}

// Apply records the operator's edits and commits the resulting Override set.
//
// sha256 is the digest the form was rendered from. If the Generated config has
// moved since, the save is refused rather than applied to a file whose Settings
// may no longer be where they were.
func (s *Service) Apply(ctx context.Context, inst domain.Instance, name, sha256 string, changes []Change, actor string) (bool, error) {
	if s == nil || s.writer == nil {
		return false, ErrNotPublished
	}
	view, err := s.File(ctx, inst, name)
	if err != nil {
		return false, err
	}
	if sha256 != "" && view.SHA256 != "" && sha256 != view.SHA256 {
		return false, ErrStaleSnapshot
	}

	set := view.Overrides
	for _, c := range changes {
		if c.Forget {
			set.Unset(c.Section, c.Name)
			continue
		}
		// Validate against what the mod said it accepts, when it said anything.
		// An Override for a Setting the file no longer declares is allowed
		// through: it may be a mod that is temporarily uninstalled.
		if setting, ok := view.Config.Lookup(c.Section, c.Name); ok {
			if err := setting.Validate(c.Value); err != nil {
				return false, fmt.Errorf("%s: %w", setting.Key(), err)
			}
		}
		set.Set(c.Section, c.Name, c.Value)
	}

	body := set.Encode()
	if body == "" {
		// An empty Override set is not stored as an empty value: the merge tool
		// would write an empty file over a good one, and the file list would
		// show a config nobody is managing.
		return s.writer.DeleteConfig(ctx, inst.Number, name, actor)
	}
	return s.writer.SaveConfig(ctx, inst.Number, name, body, actor)
}

// ResetToDefault pins a Setting to the value the mod ships with.
//
// This WRITES the default as an Override rather than removing one, because
// removing an Override restores nothing - the Generated config keeps its
// current value until something writes over it. A "reset" that merely forgot
// would visibly do nothing.
func (s *Service) ResetToDefault(ctx context.Context, inst domain.Instance, name, sha256, section, setting, actor string) (bool, error) {
	view, err := s.File(ctx, inst, name)
	if err != nil {
		return false, err
	}
	found, ok := view.Config.Lookup(section, setting)
	if !ok {
		return false, fmt.Errorf("%s is not declared by this mod, so it has no default to restore", SettingLabel(section, setting))
	}
	if !found.HasDefault {
		return false, fmt.Errorf("%s does not record a default value", found.Key())
	}
	return s.Apply(ctx, inst, name, sha256, []Change{{
		Section: section, Name: setting, Value: found.Default,
	}}, actor)
}

// SettingLabel renders a section/name pair for a message.
func SettingLabel(section, name string) string {
	return domain.SettingKey{Section: section, Name: name}.String()
}

// storedOverrides reads and parses every Override set held for an Instance.
func (s *Service) storedOverrides(ctx context.Context, num int) (map[string]domain.OverrideSet, error) {
	out := map[string]domain.OverrideSet{}
	if s.overrides == nil {
		return out, nil
	}
	data, err := s.overrides(ctx, num)
	if err != nil {
		return nil, err
	}
	for name, body := range data {
		out[name] = domain.ParseOverrideSet(body)
	}
	return out, nil
}

// parsed returns the parsed Generated config, caching on the file's digest.
// Re-parsing 1,398 settings on every keystroke of a search is not viable, and
// keying on the digest means a file BepInEx rewrote is never served stale.
func (s *Service) parsed(inst domain.Instance, sf domain.SnapshotFile) (domain.ConfigFile, error) {
	key := fmt.Sprintf("%s-%02d/%s@%s", inst.Slug, inst.Number, sf.Name, sf.SHA256)

	s.mu.Lock()
	if f, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return f, nil
	}
	s.mu.Unlock()

	body, digest, err := s.file(inst.Slug, inst.Number, sf.Name)
	if err != nil {
		return domain.ConfigFile{}, err
	}
	if sf.SHA256 != "" && digest != sf.SHA256 {
		// The index and the file disagree: a republish landed between the two
		// reads. Better to fail and be retried than to show one file's values
		// under another's digest.
		return domain.ConfigFile{}, ErrStaleSnapshot
	}
	f := domain.ParseConfigFile(sf.Name, body)

	s.mu.Lock()
	if len(s.cache) > 64 {
		s.cache = map[string]domain.ConfigFile{}
	}
	s.cache[key] = f
	s.mu.Unlock()
	return f, nil
}
