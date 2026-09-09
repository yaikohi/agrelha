package instances

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const defaultLBBaseIP = ""

// InstanceStat holds cached per-instance stats for players, status, and uptime.
type InstanceStat struct {
	Players      int
	PlayersKnown bool
	Uptime       string
}

type InstanceManager struct {
	repo             ports.InstanceRepository
	stateStore       ports.StateStore
	runtime          ports.Runtime
	totalBudgetGiB   int
	maxInstances     int
	maxRunning       int
	instancesRelPath string
	lbBaseIP         string
	renderer         ports.SpecRenderer
	namespace        string

	audit               ports.AuditRecorder
	event               ports.EventRecorder
	depResolver         func(ctx context.Context, slug, mcVersion, loader string) ([]string, error)
	modsReader          func(ctx context.Context, num int) ([]string, error)
	configsReader       func(ctx context.Context, num int) (map[string]string, error)
	globalConfigsReader func(ctx context.Context) (map[string]string, error)
	globalConfigsPath   string
	backupsDir          string
	preStopHook         func(ctx context.Context, inst domain.Instance)
	preDeleteHook       func(ctx context.Context, inst domain.Instance)
	telemetryProvider   func(ctx context.Context, inst domain.Instance) (players int, known bool)
	afterSyncHook       func(cm, dep, key string, want func(string) bool)
	commandExecutor     func(ctx context.Context, inst domain.Instance, cmd string) (string, error)
}

type Option func(*InstanceManager)

func WithCommandExecutor(fn func(ctx context.Context, inst domain.Instance, cmd string) (string, error)) Option {
	return func(m *InstanceManager) { m.commandExecutor = fn }
}

func WithAudit(recorder ports.AuditRecorder) Option {
	return func(m *InstanceManager) { m.audit = recorder }
}

func WithEvent(recorder ports.EventRecorder) Option {
	return func(m *InstanceManager) { m.event = recorder }
}

func WithDependencyResolver(fn func(ctx context.Context, slug, mcVersion, loader string) ([]string, error)) Option {
	return func(m *InstanceManager) { m.depResolver = fn }
}

func WithModsReader(fn func(ctx context.Context, num int) ([]string, error)) Option {
	return func(m *InstanceManager) { m.modsReader = fn }
}

func WithConfigsReader(fn func(ctx context.Context, num int) (map[string]string, error)) Option {
	return func(m *InstanceManager) { m.configsReader = fn }
}

func WithGlobalConfigsReader(fn func(ctx context.Context) (map[string]string, error)) Option {
	return func(m *InstanceManager) { m.globalConfigsReader = fn }
}

func WithGlobalConfigsPath(path string) Option {
	return func(m *InstanceManager) { m.globalConfigsPath = path }
}

func WithBackupsDir(dir string) Option {
	return func(m *InstanceManager) { m.backupsDir = dir }
}

func WithPreStopHook(fn func(ctx context.Context, inst domain.Instance)) Option {
	return func(m *InstanceManager) { m.preStopHook = fn }
}

func WithPreDeleteHook(fn func(ctx context.Context, inst domain.Instance)) Option {
	return func(m *InstanceManager) { m.preDeleteHook = fn }
}

func WithTelemetryProvider(fn func(ctx context.Context, inst domain.Instance) (players int, known bool)) Option {
	return func(m *InstanceManager) { m.telemetryProvider = fn }
}

func WithAfterSyncHook(fn func(cm, dep, key string, want func(string) bool)) Option {
	return func(m *InstanceManager) { m.afterSyncHook = fn }
}

func actorOrHyphen(actor []string) string {
	if len(actor) > 0 && actor[0] != "" {
		return actor[0]
	}
	return "-"
}

func NewInstanceManager(
	repo ports.InstanceRepository,
	stateStore ports.StateStore,
	rt ports.Runtime,
	totalBudgetGiB, maxInstances, maxRunning int,
	instancesRelPath string,
	lbBaseIP string,
	renderer ports.SpecRenderer,
	namespace string,
	opts ...Option,
) *InstanceManager {
	if totalBudgetGiB <= 0 {
		totalBudgetGiB = domain.DefaultTotalBudgetGiB
	}
	if maxInstances <= 0 {
		maxInstances = domain.DefaultMaxInstances
	}
	if maxRunning <= 0 {
		maxRunning = domain.DefaultMaxRunning
	}
	if instancesRelPath == "" {
		instancesRelPath = "manifests/minecraft-modded"
	}
	if lbBaseIP == "" {
		lbBaseIP = defaultLBBaseIP
	}
	m := &InstanceManager{
		repo:              repo,
		stateStore:        stateStore,
		runtime:           rt,
		totalBudgetGiB:    totalBudgetGiB,
		maxInstances:      maxInstances,
		maxRunning:        maxRunning,
		instancesRelPath:  instancesRelPath,
		lbBaseIP:          lbBaseIP,
		renderer:          renderer,
		namespace:         namespace,
		globalConfigsPath: "manifests/minecraft-modded/configs.yaml",
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// ApplyOptions configures additional options on an existing InstanceManager.
func (m *InstanceManager) ApplyOptions(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
}

func (m *InstanceManager) TotalBudgetGiB() int { return m.totalBudgetGiB }
func (m *InstanceManager) MaxInstances() int   { return m.maxInstances }
func (m *InstanceManager) MaxRunning() int     { return m.maxRunning }

// serverRef addresses one Instance in whatever runtime is configured.
func (m *InstanceManager) serverRef(inst domain.Instance) ports.ServerRef {
	return ports.ServerRef{Name: inst.DeploymentName(), Scope: m.namespace}
}

// stateFromStatus maps the runtime's Lifecycle/Available pair onto the
// Instance lifecycle. Available means players can connect; Lifecycle running
// without Available means it is still coming up.
func stateFromStatus(st ports.Status) domain.InstanceState {
	switch {
	case st.Available:
		return domain.StateRunning
	case st.Lifecycle == ports.LifecycleStopped:
		return domain.StateStopped
	default:
		return domain.StateProvisioning
	}
}

func (m *InstanceManager) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	records, err := m.repo.List()
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	instances := make([]domain.Instance, 0, len(records))
	for _, r := range records {
		inst := r
		if m.runtime != nil {
			if st, err := m.runtime.Status(ctx, m.serverRef(inst)); err == nil {
				if newState := stateFromStatus(st); inst.State != newState {
					inst.State = newState
					_ = m.repo.UpdateState(inst.Number, newState)
				}
			}
		}
		instances = append(instances, inst)
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Number < instances[j].Number
	})
	return instances, nil
}

func (m *InstanceManager) GetInstance(ctx context.Context, num int) (*domain.Instance, error) {
	rec, err := m.repo.Get(num)
	if err != nil {
		return nil, fmt.Errorf("get instance %d: %w", num, err)
	}
	if rec == nil {
		return nil, nil
	}
	inst := *rec
	if m.runtime != nil {
		if st, err := m.runtime.Status(ctx, m.serverRef(inst)); err == nil {
			if newState := stateFromStatus(st); inst.State != newState {
				inst.State = newState
				_ = m.repo.UpdateState(inst.Number, newState)
			}
		}
	}
	return &inst, nil
}

func (m *InstanceManager) CreateInstance(ctx context.Context, inst domain.Instance, modsTxt string, actor ...string) (*domain.Instance, error) {
	existing, err := m.repo.List()
	if err != nil {
		return nil, err
	}
	existingInstances := make([]domain.Instance, 0, len(existing))
	for _, e := range existing {
		existingInstances = append(existingInstances, e)
	}

	budget := domain.CalculateBudget(existingInstances, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
	if err := budget.CanCreate(); err != nil {
		return nil, err
	}

	usedNumbers := make(map[int]bool)
	for _, e := range existing {
		usedNumbers[e.Number] = true
	}

	if inst.Number <= 0 {
		for n := 1; n <= m.maxInstances; n++ {
			if !usedNumbers[n] {
				inst.Number = n
				break
			}
		}
	} else if usedNumbers[inst.Number] {
		return nil, fmt.Errorf("instance number %d is already in use", inst.Number)
	}

	if inst.Number <= 0 || inst.Number > m.maxInstances {
		return nil, fmt.Errorf("invalid instance number %d (must be 1..%d)", inst.Number, m.maxInstances)
	}

	if strings.TrimSpace(inst.Name) == "" {
		inst.Name = fmt.Sprintf("World %02d", inst.Number)
	}
	inst.EnsureDefaults(m.lbBaseIP)

	files, err := m.renderer.Render(inst, modsTxt)
	if err != nil {
		return nil, fmt.Errorf("render manifests: %w", err)
	}

	dirRel := fmt.Sprintf("%s/instance-%02d", m.instancesRelPath, inst.Number)
	commitMsg := fmt.Sprintf("mc: create instance %02d (%s)", inst.Number, inst.Name)

	if m.stateStore != nil {
		docs := make(map[string]ports.Document, len(files))
		for fname, content := range files {
			docs[fname] = ports.Document{Raw: content}
		}
		if err := m.stateStore.PutTree(ctx, dirRel, docs, commitMsg); err != nil {
			return nil, fmt.Errorf("state store write instance manifests: %w", err)
		}
	}

	if err := m.repo.Upsert(inst); err != nil {
		return nil, fmt.Errorf("save instance record: %w", err)
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-instance-create", fmt.Sprintf("World #%02d %q", inst.Number, inst.Name))
	}
	if m.event != nil {
		_ = m.event.RecordEvent("mc-instance-create", actorOrHyphen(actor))
	}

	return &inst, nil
}

func (m *InstanceManager) StartInstance(ctx context.Context, num int, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}
	if inst.State == domain.StateRunning {
		return nil
	}

	instances, err := m.ListInstances(ctx)
	if err != nil {
		return err
	}

	var others []domain.Instance
	for _, other := range instances {
		if other.Number != num {
			others = append(others, other)
		}
	}

	budget := domain.CalculateBudget(others, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
	if err := budget.CanStart(*inst); err != nil {
		return err
	}

	if m.runtime != nil {
		if err := m.runtime.Start(ctx, m.serverRef(*inst)); err != nil {
			return fmt.Errorf("start instance %d: %w", inst.Number, err)
		}
	}

	inst.State = domain.StateRunning
	if err := m.repo.UpdateState(num, domain.StateRunning); err != nil {
		return err
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-instance-start", fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent("mc-instance-start", actorOrHyphen(actor))
	}
	return nil
}

func (m *InstanceManager) StopInstance(ctx context.Context, num int, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if m.preStopHook != nil && inst.State == domain.StateRunning {
		m.preStopHook(ctx, *inst)
	}

	if m.runtime != nil {
		if err := m.runtime.Stop(ctx, m.serverRef(*inst)); err != nil {
			return fmt.Errorf("stop instance %d: %w", inst.Number, err)
		}
	}

	inst.State = domain.StateStopped
	if err := m.repo.UpdateState(num, domain.StateStopped); err != nil {
		return err
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-instance-stop", fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent("mc-instance-stop", actorOrHyphen(actor))
	}
	return nil
}

func (m *InstanceManager) DeleteInstance(ctx context.Context, num int, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if m.preDeleteHook != nil {
		m.preDeleteHook(ctx, *inst)
	}

	if m.runtime != nil {
		st, err := m.runtime.Status(ctx, m.serverRef(*inst))
		if err == nil && (st.Lifecycle == ports.LifecycleRunning || st.Available) {
			return fmt.Errorf("instance %d (%s) must be stopped before it can be deleted", num, inst.Name)
		}
	}

	dirRel := fmt.Sprintf("%s/instance-%02d", m.instancesRelPath, num)
	commitMsg := fmt.Sprintf("mc: delete instance %02d (%s)", num, inst.Name)

	if m.stateStore != nil {
		if err := m.stateStore.Delete(ctx, dirRel, commitMsg); err != nil {
			return fmt.Errorf("state store delete instance manifests: %w", err)
		}
	}

	if err := m.repo.Delete(num); err != nil {
		return err
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-instance-delete", fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent("mc-instance-delete", actorOrHyphen(actor))
	}
	return nil
}

func (m *InstanceManager) RestartInstance(ctx context.Context, num int, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if m.runtime != nil {
		if err := m.runtime.Restart(ctx, m.serverRef(*inst)); err != nil {
			return fmt.Errorf("restart instance %d: %w", num, err)
		}
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-instance-restart", fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent("mc-instance-restart", actorOrHyphen(actor))
	}
	return nil
}

func (m *InstanceManager) UpdateSettings(ctx context.Context, num int, name, motd, tier, mcVersion string, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if name != "" {
		inst.Name = name
	}
	inst.MOTD = motd
	inst.Tier = domain.NormalizeTier(tier)

	if mcVersion != "" && mcVersion != inst.MCVersion {
		if !inst.CanSetVersion() {
			return inst.PackOwnedFieldErr("Minecraft version")
		}
		inst.MCVersion = mcVersion
	}

	if err := m.repo.Upsert(*inst); err != nil {
		return fmt.Errorf("update instance %d settings: %w", num, err)
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-settings-save", fmt.Sprintf("Updated settings for #%02d", num))
	}
	return nil
}

func (m *InstanceManager) GetInstalledMods(ctx context.Context, num int) ([]string, error) {
	if m.modsReader != nil {
		return m.modsReader(ctx, num)
	}
	if m.stateStore == nil {
		return nil, nil
	}
	path := fmt.Sprintf("%s/instance-%02d/mods.yaml", m.instancesRelPath, num)
	doc, err := m.stateStore.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out []string
	if doc.Data != nil {
		for line := range strings.SplitSeq(doc.Data["mods.txt"], "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				out = append(out, strings.TrimSuffix(line, "?"))
			}
		}
	}
	return out, nil
}

func (m *InstanceManager) InstallMod(ctx context.Context, num int, slug string, actor ...string) (int, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return 0, fmt.Errorf("mod slug required")
	}
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return 0, err
	}
	if inst == nil {
		return 0, fmt.Errorf("instance %d not found", num)
	}

	wanted := []string{slug}
	if m.depResolver != nil {
		deps, err := m.depResolver(ctx, slug, inst.MCVersion, string(inst.Loader))
		if err != nil {
			slog.Warn("could not resolve all mod dependencies", "slug", slug, "instance", num, "err", err)
		} else {
			wanted = append(wanted, deps...)
		}
	}

	if m.stateStore == nil {
		return 0, ports.ErrNotImplemented
	}
	modsPath := fmt.Sprintf("%s/instance-%02d/mods.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("mc: install %s into instance #%02d", slug, num)
	addedCount := 0
	changed, err := m.stateStore.Patch(ctx, modsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		cur := doc.Data["mods.txt"]
		present := map[string]bool{}
		for l := range strings.SplitSeq(cur, "\n") {
			if t := strings.TrimSpace(strings.TrimSuffix(l, "?")); t != "" && !strings.HasPrefix(t, "#") {
				present[t] = true
			}
		}
		var body strings.Builder
		body.WriteString(strings.TrimRight(cur, "\n"))
		for _, w := range wanted {
			w = strings.TrimSpace(w)
			if w != "" && !present[w] {
				body.WriteString("\n" + w)
				present[w] = true
				addedCount++
			}
		}
		if addedCount == 0 {
			return false, nil
		}
		doc.Data["mods.txt"] = strings.TrimLeft(body.String(), "\n") + "\n"
		return true, nil
	})
	if err != nil {
		return 0, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-mod-install", fmt.Sprintf("Installed %s into #%02d", slug, num))
	}
	return addedCount, nil
}

func (m *InstanceManager) RemoveMod(ctx context.Context, num int, slug string, actor ...string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("mod slug required")
	}
	if m.stateStore == nil {
		return ports.ErrNotImplemented
	}
	modsPath := fmt.Sprintf("%s/instance-%02d/mods.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("mc: remove %s from instance #%02d", slug, num)
	changed, err := m.stateStore.Patch(ctx, modsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			return false, nil
		}
		cur := doc.Data["mods.txt"]
		lines := strings.Split(cur, "\n")
		var out []string
		found := false
		for _, l := range lines {
			if strings.TrimSpace(strings.TrimSuffix(l, "?")) != slug {
				out = append(out, l)
			} else {
				found = true
			}
		}
		if !found {
			return false, nil
		}
		doc.Data["mods.txt"] = strings.Join(out, "\n")
		return true, nil
	})
	if err != nil {
		return err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-mod-remove", fmt.Sprintf("Removed %s from #%02d", slug, num))
	}
	return nil
}

func (m *InstanceManager) ListConfigs(ctx context.Context, num int) ([]string, error) {
	if m.configsReader != nil {
		data, err := m.configsReader(ctx, num)
		if err != nil {
			return nil, err
		}
		files := make([]string, 0, len(data))
		for k := range data {
			files = append(files, k)
		}
		sort.Strings(files)
		return files, nil
	}
	if m.stateStore == nil {
		return nil, nil
	}
	path := fmt.Sprintf("%s/instance-%02d/configs.yaml", m.instancesRelPath, num)
	doc, err := m.stateStore.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(doc.Data))
	for k := range doc.Data {
		files = append(files, k)
	}
	sort.Strings(files)
	return files, nil
}

func (m *InstanceManager) GetConfig(ctx context.Context, num int, filename string) (string, error) {
	if m.configsReader != nil {
		data, err := m.configsReader(ctx, num)
		if err != nil {
			return "", err
		}
		return data[filename], nil
	}
	if m.stateStore == nil {
		return "", ports.ErrNotImplemented
	}
	path := fmt.Sprintf("%s/instance-%02d/configs.yaml", m.instancesRelPath, num)
	doc, err := m.stateStore.Get(ctx, path)
	if err != nil {
		return "", err
	}
	if doc.Data != nil {
		return doc.Data[filename], nil
	}
	return "", nil
}

func (m *InstanceManager) SaveConfig(ctx context.Context, num int, filename, content string, actor ...string) (bool, error) {
	if m.stateStore == nil {
		return false, ports.ErrNotImplemented
	}
	path := fmt.Sprintf("%s/instance-%02d/configs.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("agrelha: edit config %s for instance #%02d", filename, num)
	changed, err := m.stateStore.Patch(ctx, path, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		if doc.Data[filename] == content {
			return false, nil
		}
		doc.Data[filename] = content
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-config-edit", fmt.Sprintf("Saved %s on #%02d", filename, num))
	}
	return changed, nil
}

func (m *InstanceManager) DeleteConfig(ctx context.Context, num int, filename string, actor ...string) (bool, error) {
	if m.stateStore == nil {
		return false, ports.ErrNotImplemented
	}
	path := fmt.Sprintf("%s/instance-%02d/configs.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("agrelha: delete config %s for instance #%02d", filename, num)
	changed, err := m.stateStore.Patch(ctx, path, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			return false, nil
		}
		if _, ok := doc.Data[filename]; !ok {
			return false, nil
		}
		delete(doc.Data, filename)
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-config-delete", fmt.Sprintf("Deleted %s on #%02d", filename, num))
	}
	return changed, nil
}

func (m *InstanceManager) ListGlobalConfigs(ctx context.Context) ([]string, error) {
	if m.globalConfigsReader != nil {
		data, err := m.globalConfigsReader(ctx)
		if err != nil {
			return nil, err
		}
		files := make([]string, 0, len(data))
		for k := range data {
			files = append(files, k)
		}
		sort.Strings(files)
		return files, nil
	}
	if m.stateStore == nil {
		return nil, nil
	}
	doc, err := m.stateStore.Get(ctx, m.globalConfigsPath)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(doc.Data))
	for k := range doc.Data {
		files = append(files, k)
	}
	sort.Strings(files)
	return files, nil
}

func (m *InstanceManager) GetGlobalConfig(ctx context.Context, filename string) (string, error) {
	if m.globalConfigsReader != nil {
		data, err := m.globalConfigsReader(ctx)
		if err != nil {
			return "", err
		}
		return data[filename], nil
	}
	if m.stateStore == nil {
		return "", ports.ErrNotImplemented
	}
	doc, err := m.stateStore.Get(ctx, m.globalConfigsPath)
	if err != nil {
		return "", err
	}
	if doc.Data != nil {
		return doc.Data[filename], nil
	}
	return "", nil
}

func (m *InstanceManager) SaveGlobalConfig(ctx context.Context, filename, content string, actor ...string) (bool, error) {
	if m.stateStore == nil {
		return false, ports.ErrNotImplemented
	}
	msg := fmt.Sprintf("agrelha: edit minecraft config %s", filename)
	changed, err := m.stateStore.Patch(ctx, m.globalConfigsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		if doc.Data[filename] == content {
			return false, nil
		}
		doc.Data[filename] = content
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-config-edit", filename)
	}
	if changed && m.afterSyncHook != nil {
		m.afterSyncHook("minecraft-neoforge-configs", "", filename, func(v string) bool { return v == content })
	}
	return changed, nil
}

func (m *InstanceManager) DeleteGlobalConfig(ctx context.Context, filename string, actor ...string) (bool, error) {
	if m.stateStore == nil {
		return false, ports.ErrNotImplemented
	}
	msg := fmt.Sprintf("agrelha: delete minecraft config %s", filename)
	changed, err := m.stateStore.Patch(ctx, m.globalConfigsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			return false, nil
		}
		if _, ok := doc.Data[filename]; !ok {
			return false, nil
		}
		delete(doc.Data, filename)
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-config-delete", filename)
	}
	if changed && m.afterSyncHook != nil {
		m.afterSyncHook("minecraft-neoforge-configs", "", filename, func(v string) bool { return v == "" })
	}
	return changed, nil
}

func (m *InstanceManager) ListBackups(inst domain.Instance) []domain.BackupFile {
	if m.backupsDir == "" {
		return nil
	}
	pattern := filepath.Join(m.backupsDir, fmt.Sprintf("mc-%s-%02d-*.tar.gz", inst.Slug, inst.Number))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var out []domain.BackupFile
	for _, match := range matches {
		if fi, err := os.Stat(match); err == nil {
			out = append(out, domain.BackupFile{
				Name:      filepath.Base(match),
				SizeBytes: fi.Size(),
				CreatedAt: fi.ModTime().Format("2006-01-02 15:04"),
			})
		}
	}
	return out
}

func (m *InstanceManager) InstanceStats(ctx context.Context, insts []domain.Instance) map[int]InstanceStat {
	out := make(map[int]InstanceStat, len(insts))
	for _, inst := range insts {
		if inst.State != domain.StateRunning {
			continue
		}
		st := InstanceStat{}
		if m.runtime != nil {
			if ps, err := m.runtime.Status(ctx, m.serverRef(inst)); err == nil && !ps.StartedAt.IsZero() {
				st.Uptime = domain.FormatDuration(time.Since(ps.StartedAt))
			}
		}
		if m.telemetryProvider != nil {
			p, known := m.telemetryProvider(ctx, inst)
			st.Players = p
			st.PlayersKnown = known
		}
		out[inst.Number] = st
	}
	return out
}

type BudgetInfo = domain.Budget

func (m *InstanceManager) Budget(instances []domain.Instance) BudgetInfo {
	return domain.CalculateBudget(instances, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
}

// InstanceLogs streams logs for an instance using the configured runtime.
func (m *InstanceManager) InstanceLogs(ctx context.Context, num int, tail int64) (io.ReadCloser, error) {
	if m.runtime == nil {
		return nil, ports.ErrNotImplemented
	}
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return nil, err
	}
	if tail <= 0 {
		tail = 100
	}
	return m.runtime.Logs(ctx, m.serverRef(*inst), ports.LogOptions{Tail: tail})
}

// ExecuteCommand executes a console command on an instance via the configured command executor.
func (m *InstanceManager) ExecuteCommand(ctx context.Context, num int, cmd string) (string, error) {
	if m.commandExecutor == nil {
		return "", errors.New("command execution not configured")
	}
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return "", err
	}
	return m.commandExecutor(ctx, *inst, cmd)
}
