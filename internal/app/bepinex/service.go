package bepinex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"agrelha/internal/domain"
)

var ErrStaleSnapshot = errors.New("the server rewrote this config while you were editing it")

var ErrNotPublished = errors.New("no configs have been published for this world yet")

type SnapshotReader func(slug string, num int) (*domain.ConfigSnapshot, error)

type FileReader func(slug string, num int, name string) (body, sha256 string, err error)

type OverridesReader func(ctx context.Context, num int) (map[string]string, error)

type OverridesWriter interface {
	SaveConfig(ctx context.Context, num int, filename, content string, actor ...string) (bool, error)
	DeleteConfig(ctx context.Context, num int, filename string, actor ...string) (bool, error)
}

type Option func(*Service)

func WithSnapshot(fn SnapshotReader) Option   { return func(s *Service) { s.snapshot = fn } }
func WithFile(fn FileReader) Option           { return func(s *Service) { s.file = fn } }
func WithOverrides(fn OverridesReader) Option { return func(s *Service) { s.overrides = fn } }
func WithWriter(w OverridesWriter) Option     { return func(s *Service) { s.writer = w } }

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

type FileSummary struct {
	Name          string
	PluginName    string
	PluginVersion string
	PluginGUID    string
	Settings      int
	Changed       int
	Overridden    int
	Size          int64
	Parsable      bool
}

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

		if f, err := s.parsed(inst, sf); err == nil {
			sum.PluginName = f.PluginName
			sum.PluginVersion = f.PluginVersion
			sum.PluginGUID = f.PluginGUID
			sum.Settings = f.Count()
			sum.Changed = f.ChangedCount()
			sum.Parsable = f.Parsable()
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, snap, nil
}

type FileView struct {
	Config    domain.ConfigFile
	Overrides domain.OverrideSet
	SHA256    string
	Size      int64
}

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

type Change struct {
	Section string
	Name    string
	Value   string

	Forget bool
}

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

		if setting, ok := view.Config.Lookup(c.Section, c.Name); ok {
			if err := setting.Validate(c.Value); err != nil {
				return false, fmt.Errorf("%s: %w", setting.Key(), err)
			}
		}
		set.Set(c.Section, c.Name, c.Value)
	}

	body := set.Encode()
	if body == "" {
		return s.writer.DeleteConfig(ctx, inst.Number, name, actor)
	}
	return s.writer.SaveConfig(ctx, inst.Number, name, body, actor)
}

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

func SettingLabel(section, name string) string {
	return domain.SettingKey{Section: section, Name: name}.String()
}

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

const maxImportBytes = 1 << 19

var ErrImportTooLarge = errors.New("that config is too large to import")

var ErrNoMatchingConfig = errors.New("could not tell which mod that config belongs to")

func (s *Service) PlanImport(ctx context.Context, inst domain.Instance, name, pasted string) (domain.ImportPlan, FileView, error) {
	if len(pasted) > maxImportBytes {
		return domain.ImportPlan{}, FileView{}, ErrImportTooLarge
	}
	view, err := s.File(ctx, inst, name)
	if err != nil {
		return domain.ImportPlan{}, FileView{}, err
	}
	plan := domain.PlanImport(view.Config, pasted)
	if err := domain.MatchesConfig(view.Config, plan); err != nil {
		return plan, view, err
	}
	return plan, view, nil
}

func (s *Service) ApplyImport(ctx context.Context, inst domain.Instance, name, sha256, pasted, actor string) (int, bool, error) {
	plan, view, err := s.PlanImport(ctx, inst, name, pasted)
	if err != nil {
		return 0, false, err
	}
	if sha256 != "" && view.SHA256 != "" && sha256 != view.SHA256 {
		return 0, false, ErrStaleSnapshot
	}
	if !plan.Changes() {
		return 0, false, nil
	}

	var changes []Change
	for _, ov := range plan.Overrides().All() {
		changes = append(changes, Change{Section: ov.Section, Name: ov.Name, Value: ov.Value})
	}
	changed, err := s.Apply(ctx, inst, name, sha256, changes, actor)
	return len(changes), changed, err
}

func (s *Service) MatchFile(ctx context.Context, inst domain.Instance, pasted string) (string, error) {
	if len(pasted) > maxImportBytes {
		return "", ErrImportTooLarge
	}
	guid := domain.ParseConfigFile("", pasted).PluginGUID
	if guid == "" {
		return "", ErrNoMatchingConfig
	}
	files, _, err := s.Files(ctx, inst)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.EqualFold(f.PluginGUID, guid) {
			return f.Name, nil
		}
	}
	return "", fmt.Errorf("%w: no published config has the GUID %s", ErrNoMatchingConfig, guid)
}
