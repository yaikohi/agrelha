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
	repo              ports.InstanceRepository
	stateStore        ports.StateStore
	runtime           ports.Runtime
	totalBudgetGiB    int
	maxInstances      int
	maxRunning        int
	instancesRelPath  string
	lbBaseIP          string
	renderer          ports.SpecRenderer
	namespace         string
	gameID            domain.GameID
	backupsPVC        string
	serverRefResolver func(inst domain.Instance) ports.ServerRef

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
	jobRunner           ports.JobRunner
}

type Option func(*InstanceManager)

func WithGameID(id domain.GameID) Option {
	return func(m *InstanceManager) { m.gameID = id }
}

func WithBackupsPVC(pvc string) Option {
	return func(m *InstanceManager) { m.backupsPVC = pvc }
}

func WithServerRefResolver(fn func(inst domain.Instance) ports.ServerRef) Option {
	return func(m *InstanceManager) { m.serverRefResolver = fn }
}

func WithJobRunner(runner ports.JobRunner) Option {
	return func(m *InstanceManager) { m.jobRunner = runner }
}

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
		gameID:            domain.GameMinecraft,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.gameID == domain.GameValheim && m.globalConfigsPath == "manifests/minecraft-modded/configs.yaml" {
		m.globalConfigsPath = "manifests/valheim-mod-configs.yaml"
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

func (m *InstanceManager) TotalBudgetGiB() int   { return m.totalBudgetGiB }
func (m *InstanceManager) MaxInstances() int     { return m.maxInstances }
func (m *InstanceManager) MaxRunning() int       { return m.maxRunning }
func (m *InstanceManager) GameID() domain.GameID { return m.gameID }

func (m *InstanceManager) gamePrefix() string {
	if m.gameID == domain.GameValheim {
		return "valheim"
	}
	return "mc"
}

func (m *InstanceManager) effectiveBackupsPVC() string {
	if m.backupsPVC != "" {
		return m.backupsPVC
	}
	if m.gameID == domain.GameValheim {
		return "valheim-backups"
	}
	return "minecraft-modded-backups"
}

// serverRef addresses one Instance in whatever runtime is configured.
func (m *InstanceManager) serverRef(inst domain.Instance) ports.ServerRef {
	if m.serverRefResolver != nil {
		return m.serverRefResolver(inst)
	}
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

func (m *InstanceManager) ListInstancesByGame(ctx context.Context, gameID domain.GameID) ([]domain.Instance, error) {
	all, err := m.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	var filtered []domain.Instance
	for _, inst := range all {
		if inst.GameID == gameID {
			filtered = append(filtered, inst)
		}
	}
	return filtered, nil
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

	if inst.GameID == "" {
		if m.gameID != "" {
			inst.GameID = m.gameID
		} else {
			inst.GameID = domain.GameMinecraft
		}
	}

	if strings.TrimSpace(inst.Name) == "" {
		if inst.GameID == domain.GameValheim {
			inst.Name = fmt.Sprintf("Valheim %02d", inst.Number)
		} else {
			inst.Name = fmt.Sprintf("World %02d", inst.Number)
		}
	}
	inst.EnsureDefaults(m.lbBaseIP)

	files, err := m.renderer.Render(inst, modsTxt)
	if err != nil {
		return nil, fmt.Errorf("render manifests: %w", err)
	}

	dirRel := fmt.Sprintf("%s/instance-%02d", m.instancesRelPath, inst.Number)
	commitMsg := fmt.Sprintf("%s: create instance %02d (%s)", m.gamePrefix(), inst.Number, inst.Name)

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

	action := fmt.Sprintf("%s-instance-create", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), action, fmt.Sprintf("World #%02d %q", inst.Number, inst.Name))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(action, actorOrHyphen(actor))
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

	startAction := fmt.Sprintf("%s-instance-start", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), startAction, fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(startAction, actorOrHyphen(actor))
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

	stopAction := fmt.Sprintf("%s-instance-stop", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), stopAction, fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(stopAction, actorOrHyphen(actor))
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
	commitMsg := fmt.Sprintf("%s: delete instance %02d (%s)", m.gamePrefix(), num, inst.Name)

	if m.stateStore != nil {
		if err := m.stateStore.Delete(ctx, dirRel, commitMsg); err != nil {
			return fmt.Errorf("state store delete instance manifests: %w", err)
		}
	}

	if err := m.repo.Delete(num); err != nil {
		return err
	}

	deleteAction := fmt.Sprintf("%s-instance-delete", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), deleteAction, fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(deleteAction, actorOrHyphen(actor))
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

	restartAction := fmt.Sprintf("%s-instance-restart", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), restartAction, fmt.Sprintf("Instance #%02d", num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(restartAction, actorOrHyphen(actor))
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

	saveAction := fmt.Sprintf("%s-settings-save", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), saveAction, fmt.Sprintf("Updated settings for #%02d", num))
	}
	return nil
}

func (m *InstanceManager) UpdateValheimSettings(ctx context.Context, num int, name, motd, tier, password string, actor ...string) error {
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
	if password != "" {
		inst.Password = password
	}

	if err := m.repo.Upsert(*inst); err != nil {
		return fmt.Errorf("update instance %d settings: %w", num, err)
	}

	saveAction := fmt.Sprintf("%s-settings-save", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), saveAction, fmt.Sprintf("Updated settings for #%02d", num))
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
	msg := fmt.Sprintf("%s: install %s into instance #%02d", m.gamePrefix(), slug, num)
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
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-mod-install", m.gamePrefix()), fmt.Sprintf("Installed %s into #%02d", slug, num))
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
	msg := fmt.Sprintf("%s: remove %s from instance #%02d", m.gamePrefix(), slug, num)
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
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-mod-remove", m.gamePrefix()), fmt.Sprintf("Removed %s from #%02d", slug, num))
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
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-config-edit", m.gamePrefix()), fmt.Sprintf("Saved %s on #%02d", filename, num))
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
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-config-delete", m.gamePrefix()), fmt.Sprintf("Deleted %s on #%02d", filename, num))
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
	prefix := "mc"
	if inst.GameID == domain.GameValheim {
		prefix = "valheim"
	}
	pattern := filepath.Join(m.backupsDir, fmt.Sprintf("%s-%s-%02d-*.tar.gz", prefix, inst.Slug, inst.Number))
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

// ProvisioningStatus reports the current readiness phase ("syncing", "booting", "ready") of an instance during provisioning.
func (m *InstanceManager) ProvisioningStatus(ctx context.Context, num int) (phase string, ready bool, err error) {
	inst, err := m.GetInstance(ctx, num)
	if err != nil || inst == nil {
		return "unknown", false, fmt.Errorf("instance not found")
	}
	if m.runtime == nil {
		return "ready", true, nil
	}
	st, err := m.runtime.Status(ctx, m.serverRef(*inst))
	if err != nil {
		return "syncing", false, nil
	}
	if st.Available {
		return "ready", true, nil
	}
	if st.Lifecycle == ports.LifecycleRunning {
		return "booting", false, nil
	}
	return "syncing", false, nil
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
	return m.runtime.Logs(ctx, m.serverRef(*inst), ports.LogOptions{Tail: tail, Follow: true})
}

// RuntimeStatus reports what the runtime knows about an instance, including how
// it last died.
func (m *InstanceManager) RuntimeStatus(ctx context.Context, num int) (ports.Status, error) {
	if m.runtime == nil {
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, ports.ErrNotImplemented
	}
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, err
	}
	if inst == nil {
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, fmt.Errorf("instance %d not found", num)
	}
	return m.runtime.Status(ctx, m.serverRef(*inst))
}

// CrashLogs reads the terminated container's logs. Kubernetes reaps these when
// the pod is replaced, so they must be captured at detection, not on demand.
func (m *InstanceManager) CrashLogs(ctx context.Context, num int, tail int64) (io.ReadCloser, error) {
	if m.runtime == nil {
		return nil, ports.ErrNotImplemented
	}
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("instance %d not found", num)
	}
	if tail <= 0 {
		tail = 100
	}
	return m.runtime.Logs(ctx, m.serverRef(*inst), ports.LogOptions{Tail: tail, Previous: true})
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

// CreateBackup launches an asynchronous backup job for a specific instance.
func (m *InstanceManager) CreateBackup(ctx context.Context, num int, actor ...string) (string, error) {
	inst, err := m.GetInstance(ctx, num)
	if err != nil || inst == nil {
		return "", fmt.Errorf("instance not found")
	}

	// Flush world save via command executor if running
	if inst.State == domain.StateRunning && m.commandExecutor != nil {
		if inst.GameID == domain.GameValheim {
			_, _ = m.commandExecutor(ctx, *inst, "save")
		} else {
			_, _ = m.commandExecutor(ctx, *inst, "/save-off")
			_, _ = m.commandExecutor(ctx, *inst, "/save-all flush")
			defer func() {
				_, _ = m.commandExecutor(ctx, *inst, "/save-on")
			}()
		}
	}

	backupName := domain.FormatGameBackupFileName(inst.GameID, inst.Slug, inst.Number, "")
	jobName := fmt.Sprintf("%s-bkp-%s-%d-%s", m.gamePrefix(), inst.Slug, inst.Number, time.Now().Format("150405"))

	if m.jobRunner != nil {
		dataPVC := inst.PVCName()
		backupsPVC := m.effectiveBackupsPVC()
		if err := m.jobRunner.CreateBackupJob(ctx, jobName, backupName, dataPVC, backupsPVC); err != nil {
			return "", fmt.Errorf("failed to launch backup Job: %w", err)
		}
	}

	if m.backupsDir != "" {
		_ = pruneBackups(m.backupsDir, m.gamePrefix(), inst.Slug, inst.Number, 5)
	}

	backupAction := fmt.Sprintf("%s-backup", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), backupAction, fmt.Sprintf("Backup %s for instance #%02d", backupName, num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(backupAction, actorOrHyphen(actor))
	}

	return backupName, nil
}

func pruneBackups(backupsDir, gamePrefix, slug string, num, keepCount int) error {
	if backupsDir == "" || keepCount <= 0 {
		return nil
	}
	pattern := filepath.Join(backupsDir, fmt.Sprintf("%s-%s-%02d-*.tar.gz", gamePrefix, slug, num))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	if len(matches) <= keepCount {
		return nil
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(matches))
	for _, match := range matches {
		if fi, err := os.Stat(match); err == nil {
			files = append(files, fileInfo{path: match, modTime: fi.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	for i := keepCount; i < len(files); i++ {
		_ = os.Remove(files[i].path)
	}
	return nil
}

// RestoreInPlace uncompresses an archive back into an existing stopped instance PVC.
func (m *InstanceManager) RestoreInPlace(ctx context.Context, num int, archive string, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil || inst == nil {
		return fmt.Errorf("instance not found")
	}

	if inst.State == domain.StateRunning {
		return fmt.Errorf("Cannot restore while world is running. Please stop the server first.")
	}

	archiveName := filepath.Base(strings.TrimSpace(archive))
	if archiveName == "" || archiveName == "." || !strings.HasSuffix(archiveName, ".tar.gz") {
		return fmt.Errorf("valid backup archive name required")
	}

	if m.jobRunner != nil {
		safetyArchive := domain.FormatGameBackupFileName(inst.GameID, inst.Slug, inst.Number, "prerestore")
		safetyJob := fmt.Sprintf("%s-bkp-%s-%d-%s", m.gamePrefix(), inst.Slug, inst.Number, time.Now().Format("150405"))
		_ = m.jobRunner.CreateBackupJob(ctx, safetyJob, safetyArchive, inst.PVCName(), m.effectiveBackupsPVC())

		restoreJobName := fmt.Sprintf("%s-rst-%s-%d-%s", m.gamePrefix(), inst.Slug, inst.Number, time.Now().Format("150405"))
		if err := m.jobRunner.CreateRestoreJob(ctx, restoreJobName, archiveName, inst.PVCName(), m.effectiveBackupsPVC()); err != nil {
			return fmt.Errorf("failed to launch restore Job: %w", err)
		}
	}

	restoreAction := fmt.Sprintf("%s-backup-restore-inplace", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), restoreAction, fmt.Sprintf("Restored %s into #%02d", archiveName, num))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(restoreAction, actorOrHyphen(actor))
	}

	return nil
}

// RestoreNew provisions a new instance initialized from an existing backup archive.
func (m *InstanceManager) RestoreNew(ctx context.Context, num int, newName, tier, archive string, actor ...string) (*domain.Instance, error) {
	srcInst, err := m.GetInstance(ctx, num)
	if err != nil || srcInst == nil {
		return nil, fmt.Errorf("source instance not found")
	}

	archiveName := filepath.Base(strings.TrimSpace(archive))
	if archiveName == "" || archiveName == "." || !strings.HasSuffix(archiveName, ".tar.gz") {
		return nil, fmt.Errorf("valid backup archive name required")
	}

	newName = strings.TrimSpace(newName)
	if newName == "" {
		newName = fmt.Sprintf("%s Restored", srcInst.Name)
	}

	newTier := srcInst.Tier
	if tier != "" {
		newTier = domain.NormalizeTier(tier)
	}

	newInst := domain.Instance{
		GameID:     srcInst.GameID,
		Name:       newName,
		Seed:       srcInst.Seed,
		Password:   srcInst.Password,
		Loader:     srcInst.Loader,
		Source:     srcInst.Source,
		Pack:       srcInst.Pack,
		MCVersion:  srcInst.MCVersion,
		Tier:       newTier,
		MOTD:       fmt.Sprintf("%s (Restored)", newName),
		Difficulty: srcInst.Difficulty,
		Gamemode:   srcInst.Gamemode,
		WorldType:  srcInst.WorldType,
		State:      domain.StateStopped,
	}

	mods, _ := m.GetInstalledMods(ctx, srcInst.Number)
	modsTxt := strings.Join(mods, "\n")

	created, err := m.CreateInstance(ctx, newInst, modsTxt, actor...)
	if err != nil {
		return nil, fmt.Errorf("failed to create new instance: %w", err)
	}

	if m.jobRunner != nil {
		restoreJobName := fmt.Sprintf("%s-rst-%s-%d-%s", m.gamePrefix(), created.Slug, created.Number, time.Now().Format("150405"))
		if err := m.jobRunner.CreateRestoreJob(ctx, restoreJobName, archiveName, created.PVCName(), m.effectiveBackupsPVC()); err != nil {
			return created, fmt.Errorf("instance created, but restore Job failed: %w", err)
		}
	}

	newAction := fmt.Sprintf("%s-backup-restore-new", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), newAction, fmt.Sprintf("Restored %s into new instance #%02d %q", archiveName, created.Number, created.Name))
	}
	if m.event != nil {
		_ = m.event.RecordEvent(newAction, actorOrHyphen(actor))
	}

	return created, nil
}

// DeleteBackup safely deletes a specific backup archive file from backupsDir.
func (m *InstanceManager) DeleteBackup(ctx context.Context, num int, fileName string, actor ...string) error {
	if m.backupsDir == "" {
		return fmt.Errorf("backups directory unconfigured")
	}

	cleanName := filepath.Base(strings.TrimSpace(fileName))
	if cleanName == "" || cleanName == "." {
		return fmt.Errorf("file name required")
	}
	if !domain.IsSafeBackupFileName(cleanName) {
		return fmt.Errorf("invalid or unauthorized backup file name %q", cleanName)
	}

	targetPath := filepath.Join(m.backupsDir, cleanName)
	if err := os.Remove(targetPath); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}

	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "mc-backup-delete", fmt.Sprintf("Deleted backup %s for #%02d", cleanName, num))
	}

	return nil
}

// BackupSummary aggregates metadata for storage and backup health reporting.
func (m *InstanceManager) BackupSummary() (domain.BackupSummary, bool) {
	if m.backupsDir == "" {
		return domain.BackupSummary{}, false
	}
	entries, err := os.ReadDir(m.backupsDir)
	if err != nil {
		return domain.BackupSummary{}, false
	}
	var info domain.BackupSummary
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		info.Count++
		info.TotalSize += fi.Size()
		if fi.ModTime().After(info.LatestAt) {
			info.LatestAt = fi.ModTime()
			info.LatestName = e.Name()
			info.LatestSize = fi.Size()
		}
	}
	return info, true
}
