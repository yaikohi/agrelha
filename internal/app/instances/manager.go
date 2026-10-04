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
	profile           domain.GameProfile
	backupsPVC        string
	serverRefResolver func(inst domain.Instance) ports.ServerRef

	audit               ports.AuditRecorder
	event               ports.EventRecorder
	depResolver         func(ctx context.Context, slug, mcVersion, loader string) ([]string, error)
	versionResolver     func(ctx context.Context, fullName string) (string, error)
	modsReader          func(ctx context.Context, num int) ([]string, error)
	cfModsReader        func(ctx context.Context, num int) ([]string, error)
	cfResolver          func(ctx context.Context, slug, mcVersion, loader string) ([]string, error)
	configsReader       func(ctx context.Context, num int) (map[string]string, error)
	globalConfigsReader func(ctx context.Context) (map[string]string, error)
	globalConfigsPath   string
	backupsDir          string
	preStopHook         func(ctx context.Context, inst domain.Instance)
	preDeleteHook       func(ctx context.Context, inst domain.Instance)
	roleRegistrar       func(ctx context.Context, inst domain.Instance) error
	roleRetirer         func(ctx context.Context, inst domain.Instance) error
	telemetryProvider   func(ctx context.Context, inst domain.Instance) (players int, known bool)
	afterSyncHook       func(cm, dep, key string, want func(string) bool)
	commandExecutor     func(ctx context.Context, inst domain.Instance, cmd string) (string, error)
	jobRunner           ports.JobRunner
	sourceReconciler    func(ctx context.Context, inst *domain.Instance) (bool, error)
}

type Option func(*InstanceManager)

func WithGameID(id domain.GameID) Option {
	return func(m *InstanceManager) {
		m.gameID = id
		if p, ok := domain.ProfileFor(id); ok {
			m.profile = p
		} else {
			panic(fmt.Sprintf("unknown or unregistered game ID %q", id))
		}
	}
}

func WithGameProfile(p domain.GameProfile) Option {
	return func(m *InstanceManager) {
		m.gameID = p.ID
		m.profile = p
	}
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

// WithVersionResolver lets the manager pin the version it installed. Without it
// mods.txt records only a name, and an exported client profile has to guess the
// version from upstream - which drifts from what the server actually runs.
func WithVersionResolver(fn func(ctx context.Context, fullName string) (string, error)) Option {
	return func(m *InstanceManager) { m.versionResolver = fn }
}

func WithModsReader(fn func(ctx context.Context, num int) ([]string, error)) Option {
	return func(m *InstanceManager) { m.modsReader = fn }
}

// WithCurseForgeResolver supplies the resolver that turns a CurseForge slug into
// its own pinned entry plus a pinned entry per required dependency. Without it
// CurseForge installs are refused rather than written unresolved: see ADR 0004 -
// the server cannot resolve them either, so an unresolved entry is a boot-time
// crash loop.
func WithCurseForgeResolver(fn func(ctx context.Context, slug, mcVersion, loader string) ([]string, error)) Option {
	return func(m *InstanceManager) { m.cfResolver = fn }
}

// WithCurseForgeReader supplies the CurseForge half of a Minecraft Mod list.
// Valheim never sets it: Thunderstore is its only catalogue.
func WithCurseForgeReader(fn func(ctx context.Context, num int) ([]string, error)) Option {
	return func(m *InstanceManager) { m.cfModsReader = fn }
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

func WithInstanceRoleRegistrar(fn func(ctx context.Context, inst domain.Instance) error) Option {
	return func(m *InstanceManager) { m.roleRegistrar = fn }
}

func WithInstanceRoleRetirer(fn func(ctx context.Context, inst domain.Instance) error) Option {
	return func(m *InstanceManager) { m.roleRetirer = fn }
}

func WithTelemetryProvider(fn func(ctx context.Context, inst domain.Instance) (players int, known bool)) Option {
	return func(m *InstanceManager) { m.telemetryProvider = fn }
}

func WithAfterSyncHook(fn func(cm, dep, key string, want func(string) bool)) Option {
	return func(m *InstanceManager) { m.afterSyncHook = fn }
}

func WithSourceReconciler(fn func(ctx context.Context, inst *domain.Instance) (bool, error)) Option {
	return func(m *InstanceManager) { m.sourceReconciler = fn }
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
		profile:           domain.MinecraftProfile,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.profile.ID == "" {
		if p, ok := domain.ProfileFor(m.gameID); ok {
			m.profile = p
		} else {
			panic(fmt.Sprintf("unknown or unregistered game ID %q", m.gameID))
		}
	}
	if m.profile.ID == domain.GameValheim && m.globalConfigsPath == "manifests/minecraft-modded/configs.yaml" {
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

func (m *InstanceManager) TotalBudgetGiB() int         { return m.totalBudgetGiB }
func (m *InstanceManager) MaxInstances() int           { return m.maxInstances }
func (m *InstanceManager) MaxRunning() int             { return m.maxRunning }
func (m *InstanceManager) GameID() domain.GameID       { return m.gameID }
func (m *InstanceManager) Profile() domain.GameProfile { return m.profile }

func (m *InstanceManager) profileFor(inst domain.Instance) domain.GameProfile {
	if inst.GameID != "" {
		if p, ok := domain.ProfileFor(inst.GameID); ok {
			return p
		}
	}
	if m.profile.ID != "" {
		return m.profile
	}
	if m.gameID != "" {
		if p, ok := domain.ProfileFor(m.gameID); ok {
			return p
		}
	}
	return domain.MinecraftProfile
}

func (m *InstanceManager) gamePrefix() string {
	if m.gameID != "" {
		if p, ok := domain.ProfileFor(m.gameID); ok {
			return p.Prefix
		}
	}
	if m.profile.ID != "" {
		return m.profile.Prefix
	}
	return domain.MinecraftProfile.Prefix
}

func (m *InstanceManager) effectiveBackupsPVC() string {
	if m.backupsPVC != "" {
		return m.backupsPVC
	}
	id := m.gameID
	if id == "" {
		id = m.profile.ID
	}
	switch id {
	case domain.GameValheim:
		return "valheim-backups"
	case domain.GameMinecraft:
		return "minecraft-modded-backups"
	default:
		if p, ok := domain.ProfileFor(id); ok {
			return fmt.Sprintf("%s-backups", p.Prefix)
		}
		if m.profile.Prefix != "" {
			return fmt.Sprintf("%s-backups", m.profile.Prefix)
		}
		return "minecraft-modded-backups"
	}
}

// serverRef addresses one Instance in whatever runtime is configured.
func (m *InstanceManager) serverRef(inst domain.Instance) ports.ServerRef {
	if m.serverRefResolver != nil {
		return m.serverRefResolver(inst)
	}
	return ports.ServerRef{Name: inst.DeploymentName(m.profileFor(inst)), Scope: m.namespace}
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
		if m.sourceReconciler != nil {
			if changed, err := m.sourceReconciler(ctx, &inst); err == nil && changed {
				_ = m.repo.Upsert(inst)
			}
		}
		if m.runtime != nil {
			if st, err := m.runtime.Status(ctx, m.serverRef(inst)); err == nil {
				if newState := stateFromStatus(st); inst.State != newState {
					inst.State = newState
					_ = m.repo.UpdateState(inst.Number, newState)
				}
				if st.Address != "" && st.Address != inst.LBIP {
					inst.LBIP = st.Address
					_ = m.repo.Upsert(inst)
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
	if m.sourceReconciler != nil {
		if changed, err := m.sourceReconciler(ctx, &inst); err == nil && changed {
			_ = m.repo.Upsert(inst)
		}
	}
	if m.runtime != nil {
		if st, err := m.runtime.Status(ctx, m.serverRef(inst)); err == nil {
			if newState := stateFromStatus(st); inst.State != newState {
				inst.State = newState
				_ = m.repo.UpdateState(inst.Number, newState)
			}
			if st.Address != "" && st.Address != inst.LBIP {
				inst.LBIP = st.Address
				_ = m.repo.Upsert(inst)
			}
		}
	}
	return &inst, nil
}

// CreateInstance provisions a new Instance. mods arrives split by Provider
// because each Provider's entries reach the server in its own file.
func (m *InstanceManager) CreateInstance(ctx context.Context, inst domain.Instance, mods domain.ModList, actor ...string) (*domain.Instance, error) {
	existing, err := m.repo.List()
	if err != nil {
		return nil, err
	}
	existingInstances := make([]domain.Instance, 0, len(existing))
	for _, e := range existing {
		existingInstances = append(existingInstances, e)
	}

	profile := m.profileFor(inst)
	budget := domain.CalculateBudget(profile, existingInstances, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
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
		inst.GameID = profile.ID
	}

	if strings.TrimSpace(inst.Name) == "" {
		if inst.GameID == domain.GameValheim {
			inst.Name = fmt.Sprintf("Valheim %02d", inst.Number)
		} else {
			inst.Name = fmt.Sprintf("World %02d", inst.Number)
		}
	}
	inst.EnsureDefaults(profile, m.lbBaseIP)

	if err := m.checkLBIPFree(inst); err != nil {
		return nil, err
	}

	if err := m.checkInstanceDirFree(ctx, inst.Number); err != nil {
		return nil, err
	}

	if m.roleRegistrar != nil {
		if err := m.roleRegistrar(ctx, inst); err != nil {
			return nil, fmt.Errorf("reserve access role for instance %02d: %w", inst.Number, err)
		}
	}

	commitMsg := fmt.Sprintf("%s: create instance %02d (%s)", m.gamePrefix(), inst.Number, inst.Name)
	if err := m.writeManifests(ctx, inst, mods, commitMsg); err != nil {
		return nil, err
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

// writeManifests renders an Instance and commits the result to the declarative
// plane. Callers must pass the Instance's CURRENT mods: rendering with an empty
// ModList would rewrite mods.yaml and strip every mod from the world.
//
// Without a state store there is nowhere to write, so this is a no-op rather
// than a silent partial render.
func (m *InstanceManager) writeManifests(ctx context.Context, inst domain.Instance, mods domain.ModList, commitMsg string) error {
	files, err := m.renderer.Render(inst, mods)
	if err != nil {
		return fmt.Errorf("render manifests: %w", err)
	}
	if m.stateStore == nil {
		return nil
	}
	dirRel := fmt.Sprintf("%s/instance-%02d", m.instancesRelPath, inst.Number)
	docs := make(map[string]ports.Document, len(files))
	for fname, content := range files {
		docs[fname] = ports.Document{Raw: content}
	}
	if err := m.stateStore.PutTree(ctx, dirRel, docs, commitMsg); err != nil {
		return fmt.Errorf("state store write instance manifests: %w", err)
	}
	return nil
}

// SetResources resizes a world. Unlike UpdateSettings, which only touched the
// database, this re-renders the Instance's manifests so the change actually
// reaches the cluster - which means it restarts the world.
func (m *InstanceManager) SetResources(ctx context.Context, num int, r domain.Resources, actor ...string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	mods, err := m.GetModList(ctx, num)
	if err != nil {
		return fmt.Errorf("read current mods for instance %d: %w", num, err)
	}

	inst.Resources = r
	commitMsg := fmt.Sprintf("%s: resize instance %02d to %d/%d GiB",
		m.gamePrefix(), num, r.MemRequestGiB, r.MemLimitGiB)
	if err := m.writeManifests(ctx, *inst, mods, commitMsg); err != nil {
		return err
	}
	if err := m.repo.Upsert(*inst); err != nil {
		return fmt.Errorf("save instance %d resources: %w", num, err)
	}

	action := fmt.Sprintf("%s-instance-resize", m.gamePrefix())
	if m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), action,
			fmt.Sprintf("#%02d -> %d/%d GiB", num, r.MemRequestGiB, r.MemLimitGiB))
	}
	return nil
}

// ApplyTier re-sizes a world back onto a Tier's resources. Opt-in per world:
// changing a Tier definition never moves an existing world by itself.
func (m *InstanceManager) ApplyTier(ctx context.Context, num int, tier domain.Tier, actor ...string) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}
	if err := m.SetResources(ctx, num, tier.Resources, actor...); err != nil {
		return err
	}
	if tier.Key == "" || domain.ResourceTier(tier.Key) == inst.Tier {
		return nil
	}
	inst.Tier = domain.NormalizeTier(tier.Key)
	inst.Resources = tier.Resources
	return m.repo.Upsert(*inst)
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

	profile := m.profileFor(*inst)
	budget := domain.CalculateBudget(profile, others, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
	if err := budget.CanStart(profile, *inst); err != nil {
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

	if m.roleRetirer != nil {
		if err := m.roleRetirer(ctx, *inst); err != nil {
			slog.Error("instance role not retired, orphaned in the identity provider",
				"game", m.gamePrefix(), "number", num, "err", err)
			if m.audit != nil {
				_ = m.audit.RecordAudit(actorOrHyphen(actor),
					fmt.Sprintf("%s-instance-role-orphaned", m.gamePrefix()),
					fmt.Sprintf("Instance #%02d: %v", num, err))
			}
		}
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

	if mcVersion != "" {
		if inst.Minecraft == nil {
			inst.Minecraft = &domain.MinecraftConfig{}
		}
		if mcVersion != inst.Minecraft.MCVersion {
			if !inst.CanSetVersion() {
				return inst.PackOwnedFieldErr("Minecraft version")
			}
			inst.Minecraft.MCVersion = mcVersion
		}
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
		if inst.Valheim == nil {
			inst.Valheim = &domain.ValheimConfig{}
		}
		inst.Valheim.Password = password
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

// GetCurseForgeMods reads the CurseForge half of a Mod list. An Instance with no
// CurseForge entries has no such file, which is not an error.
func (m *InstanceManager) GetCurseForgeMods(ctx context.Context, num int) ([]string, error) {
	if m.cfModsReader != nil {
		return m.cfModsReader(ctx, num)
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
		for _, ref := range domain.SplitModLines(doc.Data["curseforge.txt"], domain.ProviderCurseForge) {
			out = append(out, ref.Entry())
		}
	}
	return out, nil
}

// GetModList reads everything an Instance runs, split by Provider - the shape
// the renderer needs.
func (m *InstanceManager) GetModList(ctx context.Context, num int) (domain.ModList, error) {
	primary, err := m.GetInstalledMods(ctx, num)
	if err != nil {
		return domain.ModList{}, err
	}
	out := domain.ModList{Primary: joinLines(primary)}
	if m.gameID != domain.GameMinecraft {
		return out, nil
	}
	cf, err := m.GetCurseForgeMods(ctx, num)
	if err != nil {
		return out, err
	}
	out.CurseForge = joinLines(cf)
	return out, nil
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
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
	if !inst.CanInstallMods() {
		return 0, inst.VanillaImmutableErr()
	}

	wanted := []string{slug}
	if m.depResolver != nil {
		mcVer := ""
		loader := ""
		if inst.Minecraft != nil {
			mcVer = inst.Minecraft.MCVersion
			loader = string(inst.Minecraft.Loader)
		}
		deps, err := m.depResolver(ctx, slug, mcVer, loader)
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

		var lines []string
		at := map[string]int{}
		for l := range strings.SplitSeq(cur, "\n") {
			if strings.TrimSpace(l) == "" {
				continue
			}
			lines = append(lines, l)
			if ref, ok := domain.ParseModRef(l, m.gameID); ok {
				if _, seen := at[ref.Key()]; !seen {
					at[ref.Key()] = len(lines) - 1
				}
			}
		}

		touched := 0
		for i, w := range wanted {
			ref, ok := domain.ParseModRef(w, m.gameID)
			if !ok {
				continue
			}
			idx, present := at[ref.Key()]
			// A slug the operator typed with a version pins that exact version,
			// even over an existing line: it is the way back to a build that
			// worked. Dependencies dragged in by the resolver never repin.
			if present {
				if i == 0 && ref.Version != "" && lines[idx] != ref.Entry() {
					lines[idx] = ref.Entry()
					touched++
				}
				continue
			}
			if ref.Version == "" && m.versionResolver != nil {
				v, err := m.versionResolver(ctx, ref.FullName())
				switch {
				case err != nil:
					// Installing a mod that cannot be resolved writes a line that
					// only fails at boot, as a crash loop. Refuse here, where the
					// operator is present and nothing is broken yet.
					return false, fmt.Errorf("cannot install %s: %w", ref.FullName(), err)
				case v == "":
					return false, fmt.Errorf("no mod named %s exists", ref.FullName())
				default:
					ref.Version = v
				}
			}
			lines = append(lines, ref.Entry())
			at[ref.Key()] = len(lines) - 1
			addedCount++
			touched++
		}
		if touched == 0 {
			return false, nil
		}
		doc.Data["mods.txt"] = strings.Join(lines, "\n") + "\n"
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

// ReplaceMods rewrites a world's whole mod list in one commit. Callers that
// change several pins at once (a mod update and everything it drags with it)
// use this rather than a sequence of installs, so the world never boots against
// a half-applied set. It reports whether anything actually changed.
func (m *InstanceManager) ReplaceMods(ctx context.Context, num int, entries []string, detail string, actor ...string) (bool, error) {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return false, err
	}
	if inst == nil {
		return false, fmt.Errorf("instance %d not found", num)
	}
	if !inst.CanInstallMods() {
		return false, inst.VanillaImmutableErr()
	}
	if m.stateStore == nil {
		return false, ports.ErrNotImplemented
	}

	var body strings.Builder
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		body.WriteString(e + "\n")
	}
	next := body.String()

	modsPath := fmt.Sprintf("%s/instance-%02d/mods.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("%s: update mods on instance #%02d", m.gamePrefix(), num)
	changed, err := m.stateStore.Patch(ctx, modsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		if doc.Data["mods.txt"] == next {
			return false, nil
		}
		doc.Data["mods.txt"] = next
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}

	if m.audit != nil {
		if detail == "" {
			detail = fmt.Sprintf("Updated mods on #%02d", num)
		}
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-mod-update", m.gamePrefix()), fmt.Sprintf("#%02d: %s", num, detail))
	}
	if m.afterSyncHook != nil {
		want := make(map[string]bool, len(entries))
		for _, e := range entries {
			want[strings.TrimSpace(e)] = true
		}
		p := m.profileFor(*inst)
		m.afterSyncHook(inst.ModsCMName(p), m.serverRef(*inst).Name, "mods.txt", func(txt string) bool {
			have := map[string]bool{}
			for line := range strings.SplitSeq(txt, "\n") {
				have[strings.TrimSpace(line)] = true
			}
			for e := range want {
				if !have[e] {
					return false
				}
			}
			return true
		})
	}
	return true, nil
}

// InstallCurseForgeMod adds a CurseForge mod, and every dependency it requires,
// to an Instance's CurseForge list. It is separate from InstallMod because the
// two Providers store their entries in different files in different syntaxes,
// and because CurseForge dependencies must be resolved here rather than by the
// server (ADR 0004).
func (m *InstanceManager) InstallCurseForgeMod(ctx context.Context, num int, slug string, actor ...string) (int, error) {
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
	if inst.GameID != domain.GameMinecraft {
		return 0, fmt.Errorf("CurseForge mods are a Minecraft concept")
	}
	if !inst.CanInstallMods() {
		return 0, inst.VanillaImmutableErr()
	}
	if m.cfResolver == nil {
		return 0, fmt.Errorf("CurseForge is not configured")
	}
	if m.stateStore == nil {
		return 0, ports.ErrNotImplemented
	}

	// Resolved before any write. A half-resolved list is worse than no change:
	// the server would download what it could and fail on the rest at boot.
	mcVer := ""
	loader := ""
	if inst.Minecraft != nil {
		mcVer = inst.Minecraft.MCVersion
		loader = string(inst.Minecraft.Loader)
	}
	wanted, err := m.cfResolver(ctx, slug, mcVer, loader)
	if err != nil {
		return 0, fmt.Errorf("cannot install %s: %w", slug, err)
	}
	if len(wanted) == 0 {
		return 0, fmt.Errorf("no CurseForge mod named %s exists for %s/%s", slug, mcVer, loader)
	}

	modsPath := fmt.Sprintf("%s/instance-%02d/mods.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("%s: install curseforge %s into instance #%02d", m.gamePrefix(), slug, num)
	added := 0
	changed, err := m.stateStore.Patch(ctx, modsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		refs := domain.SplitModLines(doc.Data["curseforge.txt"], domain.ProviderCurseForge)
		at := map[string]int{}
		for i, r := range refs {
			if _, seen := at[r.Key()]; !seen {
				at[r.Key()] = i
			}
		}
		for _, line := range wanted {
			ref, ok := domain.ParseMCModRef(line, domain.ProviderCurseForge)
			if !ok {
				continue
			}
			if i, present := at[ref.Key()]; present {
				// Re-pin rather than duplicate: the operator asked for this file.
				if refs[i].Entry() != ref.Entry() {
					refs[i] = ref
					added++
				}
				continue
			}
			refs = append(refs, ref)
			at[ref.Key()] = len(refs) - 1
			added++
		}
		if added == 0 {
			return false, nil
		}
		doc.Data["curseforge.txt"] = domain.JoinModLines(refs)
		return true, nil
	})
	if err != nil {
		return 0, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-mod-install", m.gamePrefix()),
			fmt.Sprintf("Installed curseforge %s into #%02d (%d entries)", slug, num, added))
	}
	return added, nil
}

// RemoveCurseForgeMod drops one entry from an Instance's CurseForge list.
// Dependencies it pulled in are left alone: agrelha cannot tell whether another
// mod also needs them, and removing one that is still required would turn a tidy
// list into a broken world.
func (m *InstanceManager) RemoveCurseForgeMod(ctx context.Context, num int, slug string, actor ...string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("mod slug required")
	}
	if m.stateStore == nil {
		return ports.ErrNotImplemented
	}
	target, ok := domain.ParseMCModRef(slug, domain.ProviderCurseForge)
	if !ok {
		return fmt.Errorf("mod slug required")
	}

	modsPath := fmt.Sprintf("%s/instance-%02d/mods.yaml", m.instancesRelPath, num)
	msg := fmt.Sprintf("%s: remove curseforge %s from instance #%02d", m.gamePrefix(), target.Slug, num)
	changed, err := m.stateStore.Patch(ctx, modsPath, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			return false, nil
		}
		refs := domain.SplitModLines(doc.Data["curseforge.txt"], domain.ProviderCurseForge)
		kept := refs[:0:0]
		found := false
		for _, r := range refs {
			if r.Key() == target.Key() {
				found = true
				continue
			}
			kept = append(kept, r)
		}
		if !found {
			return false, nil
		}
		doc.Data["curseforge.txt"] = domain.JoinModLines(kept)
		return true, nil
	})
	if err != nil {
		return err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), fmt.Sprintf("%s-mod-remove", m.gamePrefix()),
			fmt.Sprintf("Removed curseforge %s from #%02d", target.Slug, num))
	}
	return nil
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
		target, targetOK := domain.ParseModRef(slug, m.gameID)
		for _, l := range lines {
			ref, ok := domain.ParseModRef(l, m.gameID)
			if targetOK && ok && ref.Key() == target.Key() {
				found = true
				continue
			}
			out = append(out, l)
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

// ListConfigData returns every stored config body for an Instance, keyed by
// file name. ListConfigs answers "which files", which costs the same read; the
// BepInEx editor needs the bodies too and should not pay for that read once per
// file.
func (m *InstanceManager) ListConfigData(ctx context.Context, num int) (map[string]string, error) {
	if m.configsReader != nil {
		return m.configsReader(ctx, num)
	}
	if m.stateStore == nil {
		return nil, nil
	}
	path := fmt.Sprintf("%s/instance-%02d/configs.yaml", m.instancesRelPath, num)
	doc, err := m.stateStore.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	return doc.Data, nil
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
	return domain.CalculateBudget(m.profileFor(domain.Instance{}), instances, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
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
	if inst == nil {
		return nil, fmt.Errorf("instance %d not found", num)
	}
	if tail <= 0 {
		tail = 100
	}
	return m.runtime.Logs(ctx, m.serverRef(*inst), ports.LogOptions{Tail: tail, Follow: true})
}

// checkLBIPFree refuses an address another Instance already holds. Games
// allocate from one flat pool with per-game bases, so overlapping ranges are
// possible; without this the collision surfaces as a LoadBalancer stuck in
// <pending> with no IP and no error anywhere in agrelha.
func (m *InstanceManager) checkLBIPFree(inst domain.Instance) error {
	if inst.LBIP == "" || m.repo == nil {
		return nil
	}
	all, err := m.repo.List()
	if err != nil {
		return nil
	}
	for _, other := range all {
		if other.LBIP != inst.LBIP {
			continue
		}
		if other.GameID == inst.GameID && other.Number == inst.Number {
			continue
		}
		return fmt.Errorf("address %s is already used by %s instance #%02d (%s): change the game's LB base so the ranges do not overlap",
			inst.LBIP, other.GameID, other.Number, other.Name)
	}
	return nil
}

// checkInstanceDirFree refuses to render over an instance directory this agrelha
// does not know about.
//
// The database decides instance numbering, but the repository is shared state: a
// second agrelha pointed at the same repo starts from an empty database, sees
// number 1 as free, and renders over a live World. That is not hypothetical - a
// dev instance did exactly this to instance 01 and only the number-keyed PVC
// naming saved the world data.
func (m *InstanceManager) checkInstanceDirFree(ctx context.Context, num int) error {
	if m.stateStore == nil {
		return nil
	}
	path := fmt.Sprintf("%s/instance-%02d/slot.yaml", m.instancesRelPath, num)

	doc, err := m.stateStore.Get(ctx, path)
	if err != nil {
		return nil // absent, or a store that cannot read: nothing to protect
	}
	if len(doc.Data) == 0 && len(doc.Raw) == 0 {
		return nil
	}

	return fmt.Errorf("instance %02d already exists in %s as %q, but this agrelha's database does not know it: refusing to overwrite. Point at a different branch or repository, or reconcile the database first",
		num, m.instancesRelPath, existingWorldName(doc))
}

// existingWorldName digs a human name out of a slot document so the refusal can
// say which World it protected.
func existingWorldName(doc ports.Document) string {
	for _, key := range []string{"SERVER_NAME", "LEVEL", "WORLD_NAME"} {
		if v := strings.TrimSpace(doc.Data[key]); v != "" {
			return v
		}
	}
	return "an unknown world"
}

// SaveInstance persists an Instance record as-is. It exists for one-time
// backfills; ordinary changes go through the operations that enforce the rules.
func (m *InstanceManager) SaveInstance(inst domain.Instance) error {
	if m.repo == nil {
		return ports.ErrNotImplemented
	}
	return m.repo.Upsert(inst)
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

// CrashLogs reads the terminated container's logs. step names a setup container
// when the failure happened before the server started; empty reads the server. Kubernetes reaps these when
// the pod is replaced, so they must be captured at detection, not on demand.
func (m *InstanceManager) CrashLogs(ctx context.Context, num int, tail int64, step ...string) (io.ReadCloser, error) {
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
	container := ""
	if len(step) > 0 {
		container = step[0]
	}
	return m.runtime.Logs(ctx, m.serverRef(*inst), ports.LogOptions{Tail: tail, Previous: true, Container: container})
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
	if inst == nil {
		return "", fmt.Errorf("instance %d not found", num)
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

	p := m.profileFor(*inst)
	backupName := domain.FormatGameBackupFileName(p, inst.Slug, inst.Number, "")
	jobName := fmt.Sprintf("%s-bkp-%s-%d-%s", m.gamePrefix(), inst.Slug, inst.Number, time.Now().Format("150405"))

	if m.jobRunner != nil {
		dataPVC := inst.PVCName(p)
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
		p := m.profileFor(*inst)
		safetyArchive := domain.FormatGameBackupFileName(p, inst.Slug, inst.Number, "prerestore")
		safetyJob := fmt.Sprintf("%s-bkp-%s-%d-%s", m.gamePrefix(), inst.Slug, inst.Number, time.Now().Format("150405"))
		_ = m.jobRunner.CreateBackupJob(ctx, safetyJob, safetyArchive, inst.PVCName(p), m.effectiveBackupsPVC())

		restoreJobName := fmt.Sprintf("%s-rst-%s-%d-%s", m.gamePrefix(), inst.Slug, inst.Number, time.Now().Format("150405"))
		if err := m.jobRunner.CreateRestoreJob(ctx, restoreJobName, archiveName, inst.PVCName(p), m.effectiveBackupsPVC()); err != nil {
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
		GameID: srcInst.GameID,
		Name:   newName,
		Source: srcInst.Source,
		Tier:   newTier,
		MOTD:   fmt.Sprintf("%s (Restored)", newName),
		State:  domain.StateStopped,
	}
	if srcInst.Minecraft != nil {
		mcCopy := *srcInst.Minecraft
		if srcInst.Minecraft.Pack != nil {
			packCopy := *srcInst.Minecraft.Pack
			mcCopy.Pack = &packCopy
		}
		newInst.Minecraft = &mcCopy
	}
	if srcInst.Valheim != nil {
		vhCopy := *srcInst.Valheim
		newInst.Valheim = &vhCopy
	}

	// Duplicate carries both halves: a copy that silently dropped the CurseForge
	// mods would look right in the UI and boot without them.
	mods, _ := m.GetModList(ctx, srcInst.Number)

	created, err := m.CreateInstance(ctx, newInst, mods, actor...)
	if err != nil {
		return nil, fmt.Errorf("failed to create new instance: %w", err)
	}

	if m.jobRunner != nil {
		p := m.profileFor(*created)
		restoreJobName := fmt.Sprintf("%s-rst-%s-%d-%s", m.gamePrefix(), created.Slug, created.Number, time.Now().Format("150405"))
		if err := m.jobRunner.CreateRestoreJob(ctx, restoreJobName, archiveName, created.PVCName(p), m.effectiveBackupsPVC()); err != nil {
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
