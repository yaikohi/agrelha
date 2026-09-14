package instances

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type fullMockRepo struct {
	instances   map[int]domain.Instance
	listErr     error
	getErr      error
	upsertErr   error
	upStateErr  error
	deleteErr   error
}

func newFullMockRepo() *fullMockRepo {
	return &fullMockRepo{instances: make(map[int]domain.Instance)}
}

func (r *fullMockRepo) Upsert(inst domain.Instance) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.instances[inst.Number] = inst
	return nil
}

func (r *fullMockRepo) Get(num int) (*domain.Instance, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	inst, ok := r.instances[num]
	if !ok {
		return nil, nil
	}
	return &inst, nil
}

func (r *fullMockRepo) List() ([]domain.Instance, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.Instance
	for _, inst := range r.instances {
		out = append(out, inst)
	}
	return out, nil
}

func (r *fullMockRepo) UpdateState(num int, state domain.InstanceState) error {
	if r.upStateErr != nil {
		return r.upStateErr
	}
	inst, ok := r.instances[num]
	if !ok {
		return fmt.Errorf("not found")
	}
	inst.State = state
	r.instances[num] = inst
	return nil
}

func (r *fullMockRepo) Delete(num int) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.instances, num)
	return nil
}

type fullMockRuntime struct {
	status     ports.Status
	statusFunc func(ref ports.ServerRef) (ports.Status, error)
	metrics    ports.Metrics
	logs       string
	statusErr  error
	startErr   error
	stopErr    error
	restartErr error
	logsErr    error
}

func (m *fullMockRuntime) Start(ctx context.Context, ref ports.ServerRef) error { return m.startErr }
func (m *fullMockRuntime) Stop(ctx context.Context, ref ports.ServerRef) error  { return m.stopErr }
func (m *fullMockRuntime) Restart(ctx context.Context, ref ports.ServerRef) error {
	return m.restartErr
}
func (m *fullMockRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ref)
	}
	return m.status, m.statusErr
}
func (m *fullMockRuntime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return m.metrics, nil
}
func (m *fullMockRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	if m.logsErr != nil {
		return nil, m.logsErr
	}
	return io.NopCloser(strings.NewReader(m.logs)), nil
}
func (m *fullMockRuntime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

type fullMockStateStore struct {
	docs     map[string]ports.Document
	getErr   error
	putErr   error
	patchErr error
	delErr   error
	treeErr  error
}

func newFullMockStateStore() *fullMockStateStore {
	return &fullMockStateStore{docs: make(map[string]ports.Document)}
}

func (s *fullMockStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	if s.getErr != nil {
		return ports.Document{}, s.getErr
	}
	return s.docs[path], nil
}

func (s *fullMockStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.docs[path] = doc
	return nil
}

func (s *fullMockStateStore) Delete(ctx context.Context, path string, msg string) error {
	if s.delErr != nil {
		return s.delErr
	}
	delete(s.docs, path)
	return nil
}

func (s *fullMockStateStore) PutTree(ctx context.Context, dir string, tree map[string]ports.Document, msg string) error {
	if s.treeErr != nil {
		return s.treeErr
	}
	for k, v := range tree {
		s.docs[k] = v
	}
	return nil
}

func (s *fullMockStateStore) Patch(ctx context.Context, path string, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if s.patchErr != nil {
		return false, s.patchErr
	}
	doc := s.docs[path]
	changed, err := fn(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		s.docs[path] = doc
	}
	return changed, nil
}

type fullMockRenderer struct {
	files map[string][]byte
	err   error
}

func (r *fullMockRenderer) Render(inst domain.Instance, modsTxt string) (map[string][]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.files != nil {
		return r.files, nil
	}
	return map[string][]byte{"server.yaml": []byte("dummy")}, nil
}

type fullMockAuditAndEvent struct {
	audits []string
	events []string
}

func (a *fullMockAuditAndEvent) RecordAudit(actor, action, detail string) error {
	a.audits = append(a.audits, fmt.Sprintf("%s:%s:%s", actor, action, detail))
	return nil
}

func (a *fullMockAuditAndEvent) RecordEvent(kind, detail string) error {
	a.events = append(a.events, fmt.Sprintf("%s:%s", kind, detail))
	return nil
}

type fullMockJobRunner struct {
	backupErr  error
	restoreErr error
}

func (j *fullMockJobRunner) CreateBackupJob(ctx context.Context, jobName, archiveName, dataPVC, backupsPVC string) error {
	return j.backupErr
}

func (j *fullMockJobRunner) CreateRestoreJob(ctx context.Context, jobName, archiveName, dataPVC, backupsPVC string) error {
	return j.restoreErr
}

func TestManagerOptionsAndGetters(t *testing.T) {
	mgr := &InstanceManager{}
	aud := &fullMockAuditAndEvent{}
	opts := []Option{
		WithGameID(domain.GameValheim),
		WithBackupsPVC("custom-pvc"),
		WithServerRefResolver(func(inst domain.Instance) ports.ServerRef {
			return ports.ServerRef{Name: "custom-ref"}
		}),
		WithJobRunner(&fullMockJobRunner{}),
		WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
			return "out", nil
		}),
		WithAudit(aud),
		WithEvent(aud),
		WithDependencyResolver(func(ctx context.Context, slug, mcVersion, loader string) ([]string, error) {
			return []string{"dep"}, nil
		}),
		WithVersionResolver(func(ctx context.Context, fullName string) (string, error) {
			return "1.0", nil
		}),
		WithModsReader(func(ctx context.Context, num int) ([]string, error) {
			return []string{"mod"}, nil
		}),
		WithConfigsReader(func(ctx context.Context, num int) (map[string]string, error) {
			return map[string]string{"cfg": "data"}, nil
		}),
		WithGlobalConfigsReader(func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"global": "data"}, nil
		}),
		WithGlobalConfigsPath("custom/path.yaml"),
		WithBackupsDir("/backups"),
		WithPreStopHook(func(ctx context.Context, inst domain.Instance) {}),
		WithPreDeleteHook(func(ctx context.Context, inst domain.Instance) {}),
		WithTelemetryProvider(func(ctx context.Context, inst domain.Instance) (int, bool) {
			return 5, true
		}),
		WithAfterSyncHook(func(cm, dep, key string, want func(string) bool) {}),
	}
	mgr.ApplyOptions(opts...)

	if mgr.GameID() != domain.GameValheim {
		t.Errorf("expected GameValheim, got %s", mgr.GameID())
	}
	if mgr.effectiveBackupsPVC() != "custom-pvc" {
		t.Errorf("expected custom-pvc, got %s", mgr.effectiveBackupsPVC())
	}
	mgr.backupsPVC = ""
	if mgr.effectiveBackupsPVC() != "valheim-backups" {
		t.Errorf("expected valheim-backups, got %s", mgr.effectiveBackupsPVC())
	}
	mgr.gameID = domain.GameMinecraft
	if mgr.effectiveBackupsPVC() != "minecraft-modded-backups" {
		t.Errorf("expected minecraft-modded-backups, got %s", mgr.effectiveBackupsPVC())
	}

	if mgr.gamePrefix() != "mc" {
		t.Errorf("expected mc prefix, got %s", mgr.gamePrefix())
	}
	mgr.gameID = domain.GameValheim
	if mgr.gamePrefix() != "valheim" {
		t.Errorf("expected valheim prefix, got %s", mgr.gamePrefix())
	}

	if ref := mgr.serverRef(domain.Instance{}); ref.Name != "custom-ref" {
		t.Errorf("expected custom-ref from resolver, got %s", ref.Name)
	}
	mgr.serverRefResolver = nil
	mgr.namespace = "test-ns"
	if ref := mgr.serverRef(domain.Instance{GameID: domain.GameMinecraft, Number: 1}); ref.Scope != "test-ns" {
		t.Errorf("expected scope test-ns, got %s", ref.Scope)
	}

	// Budget getters
	mgr.totalBudgetGiB = 32
	mgr.maxInstances = 4
	mgr.maxRunning = 2
	if mgr.TotalBudgetGiB() != 32 || mgr.MaxInstances() != 4 || mgr.MaxRunning() != 2 {
		t.Errorf("unexpected budget getters: %d, %d, %d", mgr.TotalBudgetGiB(), mgr.MaxInstances(), mgr.MaxRunning())
	}

	// actorOrHyphen
	if actorOrHyphen(nil) != "-" || actorOrHyphen([]string{}) != "-" || actorOrHyphen([]string{""}) != "-" || actorOrHyphen([]string{"alice"}) != "alice" {
		t.Errorf("unexpected actorOrHyphen results")
	}

	// stateFromStatus
	if st := stateFromStatus(ports.Status{Available: true}); st != domain.StateRunning {
		t.Errorf("want StateRunning, got %s", st)
	}
	if st := stateFromStatus(ports.Status{Lifecycle: ports.LifecycleStopped}); st != domain.StateStopped {
		t.Errorf("want StateStopped, got %s", st)
	}
	if st := stateFromStatus(ports.Status{Lifecycle: ports.LifecycleRunning}); st != domain.StateProvisioning {
		t.Errorf("want StateProvisioning, got %s", st)
	}
}

func TestManagerListAndGetInstancesEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	mgr := &InstanceManager{repo: repo}

	// 1. List error
	repo.listErr = fmt.Errorf("list fail")
	if _, err := mgr.ListInstances(ctx); err == nil {
		t.Errorf("expected error on ListInstances")
	}
	if _, err := mgr.ListInstancesByGame(ctx, domain.GameValheim); err == nil {
		t.Errorf("expected error on ListInstancesByGame")
	}

	// 2. State and LBIP update in ListInstances
	repo.listErr = nil
	repo.Upsert(domain.Instance{
		Number: 1,
		GameID: domain.GameValheim,
		State:  domain.StateStopped,
		LBIP:   "1.1.1.1",
	})
	repo.Upsert(domain.Instance{
		Number: 2,
		GameID: domain.GameMinecraft,
		State:  domain.StateStopped,
		LBIP:   "1.1.1.2",
	})

	rt := &fullMockRuntime{
		status: ports.Status{Available: true, Address: "2.2.2.2"},
	}
	mgr.runtime = rt

	insts, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("unexpected ListInstances error: %v", err)
	}
	if len(insts) != 2 || insts[0].State != domain.StateRunning || insts[0].LBIP != "2.2.2.2" {
		t.Errorf("expected updated state and LBIP, got %+v", insts[0])
	}

	// 3. ListInstancesByGame filter
	valheimInsts, err := mgr.ListInstancesByGame(ctx, domain.GameValheim)
	if err != nil || len(valheimInsts) != 1 {
		t.Errorf("expected 1 valheim instance, got %d, err: %v", len(valheimInsts), err)
	}

	// 4. GetInstance error and nil
	repo.getErr = fmt.Errorf("get fail")
	if _, err := mgr.GetInstance(ctx, 1); err == nil {
		t.Errorf("expected error on GetInstance")
	}
	repo.getErr = nil
	if inst, err := mgr.GetInstance(ctx, 999); err != nil || inst != nil {
		t.Errorf("expected nil instance for 999")
	}
	if inst, err := mgr.GetInstance(ctx, 1); err != nil || inst == nil {
		t.Fatalf("unexpected error on GetInstance 1: %v", err)
	}
}

func TestManagerCreateInstanceEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	store := newFullMockStateStore()
	renderer := &fullMockRenderer{}
	mgr := &InstanceManager{
		repo:             repo,
		stateStore:       store,
		renderer:         renderer,
		totalBudgetGiB:   32,
		maxInstances:     4,
		maxRunning:       2,
		instancesRelPath: "manifests",
		lbBaseIP:         "192.168.20.220",
	}

	// 1. repo.List error
	repo.listErr = fmt.Errorf("list fail")
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Name: "W1"}, ""); err == nil {
		t.Errorf("expected error when repo.List fails")
	}
	repo.listErr = nil

	// 2. Budget exceeded
	mgr.maxInstances = 1
	repo.Upsert(domain.Instance{Number: 1, State: domain.StateStopped})
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Name: "W2"}, ""); err == nil {
		t.Errorf("expected error when budget max instances exceeded")
	}
	mgr.maxInstances = 4

	// 3. Number already in use & invalid number
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Number: 1, Name: "W2"}, ""); err == nil {
		t.Errorf("expected error for duplicate instance number")
	}
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Number: 99, Name: "W99"}, ""); err == nil {
		t.Errorf("expected error for instance number out of range")
	}

	// 4. GameID fallback and Valheim default name
	delete(repo.instances, 1)
	instV, err := mgr.CreateInstance(ctx, domain.Instance{GameID: domain.GameValheim}, "")
	if err != nil {
		t.Fatalf("create valheim instance failed: %v", err)
	}
	if instV.Name != "Valheim 01" {
		t.Errorf("want Valheim 01, got %s", instV.Name)
	}

	// 5. checkLBIPFree collision
	if err := mgr.checkLBIPFree(domain.Instance{Number: 2, GameID: domain.GameMinecraft, LBIP: instV.LBIP}); err == nil {
		t.Errorf("expected collision error for duplicate LBIP")
	}
	repo.listErr = fmt.Errorf("list fail")
	if err := mgr.checkLBIPFree(domain.Instance{Number: 2, LBIP: "1.2.3.4"}); err != nil {
		t.Errorf("expected nil when repo.List errors in checkLBIPFree")
	}
	repo.listErr = nil
	if err := mgr.checkLBIPFree(domain.Instance{LBIP: ""}); err != nil {
		t.Errorf("expected nil for empty LBIP")
	}

	// 6. checkInstanceDirFree
	store.docs["manifests/instance-02/slot.yaml"] = ports.Document{Data: map[string]string{"SERVER_NAME": "Protected"}}
	if err := mgr.checkInstanceDirFree(ctx, 2); err == nil {
		t.Errorf("expected refusing to overwrite error")
	}
	// Test existingWorldName variations
	if n := existingWorldName(ports.Document{Data: map[string]string{"LEVEL": "MyLevel"}}); n != "MyLevel" {
		t.Errorf("want MyLevel, got %s", n)
	}
	if n := existingWorldName(ports.Document{Data: map[string]string{"WORLD_NAME": "MyValheim"}}); n != "MyValheim" {
		t.Errorf("want MyValheim, got %s", n)
	}
	if n := existingWorldName(ports.Document{}); n != "an unknown world" {
		t.Errorf("want an unknown world, got %s", n)
	}
	delete(store.docs, "manifests/instance-02/slot.yaml")

	// 7. renderer error
	renderer.err = fmt.Errorf("render fail")
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Number: 2, Name: "W2"}, ""); err == nil {
		t.Errorf("expected error when renderer fails")
	}
	renderer.err = nil

	// 8. stateStore.PutTree error
	store.treeErr = fmt.Errorf("put tree fail")
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Number: 2, Name: "W2"}, ""); err == nil {
		t.Errorf("expected error when stateStore.PutTree fails")
	}
	store.treeErr = nil

	// 9. repo.Upsert error
	repo.upsertErr = fmt.Errorf("upsert fail")
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Number: 2, Name: "W2"}, ""); err == nil {
		t.Errorf("expected error when repo.Upsert fails")
	}
	repo.upsertErr = nil

	// 10. Audit and Event recording
	events := &fullMockAuditAndEvent{}
	mgr.audit = events
	mgr.event = events
	inst2, err := mgr.CreateInstance(ctx, domain.Instance{Number: 2, Name: "W2"}, "", "operator")
	if err != nil || inst2 == nil {
		t.Fatalf("unexpected error creating instance 2: %v", err)
	}
	if len(events.events) == 0 {
		t.Errorf("expected events recorded")
	}
}

func TestManagerLifecycleEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	store := newFullMockStateStore()
	rt := &fullMockRuntime{}
	rt.statusFunc = func(ref ports.ServerRef) (ports.Status, error) {
		if strings.HasSuffix(ref.Name, "-02") {
			return ports.Status{Available: true, Lifecycle: ports.LifecycleRunning}, nil
		}
		inst, _ := repo.Get(1)
		if inst != nil && inst.State == domain.StateRunning {
			return ports.Status{Available: true, Lifecycle: ports.LifecycleRunning}, nil
		}
		return ports.Status{Lifecycle: ports.LifecycleStopped}, nil
	}
	mgr := &InstanceManager{
		repo:             repo,
		stateStore:       store,
		runtime:          rt,
		totalBudgetGiB:   32,
		maxInstances:     4,
		maxRunning:       1,
		instancesRelPath: "manifests",
	}

	repo.Upsert(domain.Instance{Number: 1, Name: "Inst1", State: domain.StateStopped, Tier: domain.TierMedium})
	repo.Upsert(domain.Instance{Number: 2, Name: "Inst2", State: domain.StateRunning, Tier: domain.TierMedium})

	// StartInstance
	if err := mgr.StartInstance(ctx, 999); err == nil {
		t.Errorf("expected error starting non-existent instance")
	}
	if err := mgr.StartInstance(ctx, 2); err != nil {
		t.Errorf("expected nil starting already running instance: %v", err)
	}
	// budget exceeded: maxRunning is 1, inst 2 is running
	if err := mgr.StartInstance(ctx, 1); err == nil {
		t.Errorf("expected error starting when maxRunning exceeded")
	}
	mgr.maxRunning = 2
	rt.startErr = fmt.Errorf("runtime start fail")
	if err := mgr.StartInstance(ctx, 1); err == nil {
		t.Errorf("expected error when runtime.Start fails")
	}
	rt.startErr = nil
	repo.upStateErr = fmt.Errorf("update state fail")
	if err := mgr.StartInstance(ctx, 1); err == nil {
		t.Errorf("expected error when UpdateState fails")
	}
	repo.upStateErr = nil

	events := &fullMockAuditAndEvent{}
	mgr.audit = events
	mgr.event = events
	if err := mgr.StartInstance(ctx, 1, "operator"); err != nil {
		t.Fatalf("unexpected error starting instance 1: %v", err)
	}

	// StopInstance
	if err := mgr.StopInstance(ctx, 999); err == nil {
		t.Errorf("expected error stopping non-existent instance")
	}
	var preStopped bool
	mgr.preStopHook = func(ctx context.Context, inst domain.Instance) { preStopped = true }
	rt.stopErr = fmt.Errorf("runtime stop fail")
	if err := mgr.StopInstance(ctx, 1); err == nil {
		t.Errorf("expected error when runtime.Stop fails")
	}
	if !preStopped {
		t.Errorf("expected preStopHook called")
	}
	rt.stopErr = nil
	repo.upStateErr = fmt.Errorf("update state fail")
	if err := mgr.StopInstance(ctx, 1); err == nil {
		t.Errorf("expected error when UpdateState fails")
	}
	repo.upStateErr = nil
	if err := mgr.StopInstance(ctx, 1, "operator"); err != nil {
		t.Fatalf("unexpected error stopping instance 1: %v", err)
	}

	// DeleteInstance
	if err := mgr.DeleteInstance(ctx, 999); err == nil {
		t.Errorf("expected error deleting non-existent instance")
	}
	var preDeleted bool
	mgr.preDeleteHook = func(ctx context.Context, inst domain.Instance) { preDeleted = true }
	rt.status = ports.Status{Lifecycle: ports.LifecycleRunning}
	if err := mgr.DeleteInstance(ctx, 2); err == nil {
		t.Errorf("expected error deleting running instance")
	}
	rt.status = ports.Status{Lifecycle: ports.LifecycleStopped}
	store.delErr = fmt.Errorf("store del fail")
	if err := mgr.DeleteInstance(ctx, 1); err == nil {
		t.Errorf("expected error when stateStore.Delete fails")
	}
	store.delErr = nil
	repo.deleteErr = fmt.Errorf("repo del fail")
	if err := mgr.DeleteInstance(ctx, 1); err == nil {
		t.Errorf("expected error when repo.Delete fails")
	}
	repo.deleteErr = nil
	if err := mgr.DeleteInstance(ctx, 1, "operator"); err != nil {
		t.Fatalf("unexpected error deleting instance 1: %v", err)
	}
	if !preDeleted {
		t.Errorf("expected preDeleteHook called")
	}

	// RestartInstance
	if err := mgr.RestartInstance(ctx, 999); err == nil {
		t.Errorf("expected error restarting non-existent instance")
	}
	rt.restartErr = fmt.Errorf("runtime restart fail")
	if err := mgr.RestartInstance(ctx, 2); err == nil {
		t.Errorf("expected error when runtime.Restart fails")
	}
	rt.restartErr = nil
	if err := mgr.RestartInstance(ctx, 2, "operator"); err != nil {
		t.Fatalf("unexpected error restarting instance 2: %v", err)
	}
}

func TestManagerUpdateSettingsEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	events := &fullMockAuditAndEvent{}
	mgr := &InstanceManager{repo: repo, audit: events}

	repo.Upsert(domain.Instance{
		Number:    1,
		Name:      "MC1",
		MCVersion: "1.21.1",
		Source:    domain.SourceModpack, // CanSetVersion() == false
		Pack:      &domain.Pack{Name: "ATM"},
	})
	repo.Upsert(domain.Instance{
		Number:    2,
		GameID:    domain.GameValheim,
		Name:      "VH2",
		MCVersion: "1.21.1",
		Source:    domain.SourceModlist,
	})

	// UpdateSettings
	if err := mgr.UpdateSettings(ctx, 999, "Name", "motd", "medium", "1.21.1"); err == nil {
		t.Errorf("expected error for non-existent instance")
	}
	if err := mgr.UpdateSettings(ctx, 1, "NewName", "motd", "medium", "1.21.4"); err == nil {
		t.Errorf("expected error changing version on modpack-defined world")
	}
	repo.upsertErr = fmt.Errorf("upsert fail")
	if err := mgr.UpdateSettings(ctx, 2, "NewName", "motd", "medium", "1.21.1"); err == nil {
		t.Errorf("expected error when repo.Upsert fails")
	}
	repo.upsertErr = nil
	if err := mgr.UpdateSettings(ctx, 2, "NewName", "motd", "medium", "1.21.1", "operator"); err != nil {
		t.Fatalf("unexpected error on UpdateSettings: %v", err)
	}

	// UpdateValheimSettings
	if err := mgr.UpdateValheimSettings(ctx, 999, "Name", "motd", "medium", "pw"); err == nil {
		t.Errorf("expected error for non-existent instance")
	}
	repo.upsertErr = fmt.Errorf("upsert fail")
	if err := mgr.UpdateValheimSettings(ctx, 2, "ValheimWorld", "motd", "medium", "secret"); err == nil {
		t.Errorf("expected error when repo.Upsert fails")
	}
	repo.upsertErr = nil
	if err := mgr.UpdateValheimSettings(ctx, 2, "ValheimWorld", "motd", "medium", "secret", "operator"); err != nil {
		t.Fatalf("unexpected error on UpdateValheimSettings: %v", err)
	}
}

func TestManagerModsEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	store := newFullMockStateStore()
	events := &fullMockAuditAndEvent{}
	mgr := &InstanceManager{
		repo:             repo,
		stateStore:       store,
		instancesRelPath: "manifests",
		audit:            events,
	}

	repo.Upsert(domain.Instance{Number: 1, Source: domain.SourceModlist, MCVersion: "1.21.1", Loader: domain.LoaderNeoForge})
	repo.Upsert(domain.Instance{Number: 2, Source: domain.SourceVanilla})

	// 1. GetInstalledMods
	mgr.modsReader = func(ctx context.Context, num int) ([]string, error) {
		return []string{"custom-reader-mod"}, nil
	}
	if mods, err := mgr.GetInstalledMods(ctx, 1); err != nil || len(mods) != 1 || mods[0] != "custom-reader-mod" {
		t.Errorf("unexpected mods from modsReader: %v, err: %v", mods, err)
	}
	mgr.modsReader = nil
	mgr.stateStore = nil
	if mods, err := mgr.GetInstalledMods(ctx, 1); err != nil || len(mods) != 0 {
		t.Errorf("expected empty mods when stateStore is nil")
	}
	mgr.stateStore = store
	store.getErr = fmt.Errorf("get fail")
	if _, err := mgr.GetInstalledMods(ctx, 1); err == nil {
		t.Errorf("expected error when stateStore.Get fails")
	}
	store.getErr = nil
	store.docs["manifests/instance-01/mods.yaml"] = ports.Document{
		Data: map[string]string{"mods.txt": "jei\n# comment\nappleskin?\n"},
	}
	if mods, err := mgr.GetInstalledMods(ctx, 1); err != nil || len(mods) != 2 || mods[0] != "jei" || mods[1] != "appleskin" {
		t.Errorf("unexpected parsed mods: %v", mods)
	}

	// 2. InstallMod
	if _, err := mgr.InstallMod(ctx, 1, ""); err == nil {
		t.Errorf("expected error for empty slug")
	}
	if _, err := mgr.InstallMod(ctx, 999, "mod"); err == nil {
		t.Errorf("expected error for non-existent instance")
	}
	if _, err := mgr.InstallMod(ctx, 2, "mod"); err == nil {
		t.Errorf("expected error on vanilla instance")
	}
	mgr.stateStore = nil
	if _, err := mgr.InstallMod(ctx, 1, "mod"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented when stateStore is nil")
	}
	mgr.stateStore = store

	// depResolver error (should log and continue)
	mgr.depResolver = func(ctx context.Context, slug, mcVersion, loader string) ([]string, error) {
		return nil, fmt.Errorf("dep fail")
	}
	// versionResolver error and empty
	mgr.versionResolver = func(ctx context.Context, fullName string) (string, error) {
		return "", fmt.Errorf("version fail")
	}
	if _, err := mgr.InstallMod(ctx, 1, "newmod"); err == nil {
		t.Errorf("expected error when versionResolver fails")
	}
	mgr.versionResolver = func(ctx context.Context, fullName string) (string, error) {
		return "", nil
	}
	if _, err := mgr.InstallMod(ctx, 1, "newmod"); err == nil {
		t.Errorf("expected error when versionResolver returns empty")
	}

	// Successful install with version, repin, unparseable mod, and untouched
	mgr.versionResolver = func(ctx context.Context, fullName string) (string, error) {
		return "1.2.3", nil
	}
	mgr.depResolver = func(ctx context.Context, slug, mcVersion, loader string) ([]string, error) {
		return []string{"# badmod", "depmod/1.0.0"}, nil
	}
	added, err := mgr.InstallMod(ctx, 1, "jei/19.2.0", "operator")
	if err != nil {
		t.Fatalf("InstallMod failed: %v", err)
	}
	if added == 0 {
		t.Errorf("expected added > 0")
	}
	// install again when all present -> touched == 0
	if _, err := mgr.InstallMod(ctx, 1, "jei/19.2.0"); err != nil {
		t.Fatalf("second InstallMod failed: %v", err)
	}

	// store patchErr
	store.patchErr = fmt.Errorf("patch fail")
	if _, err := mgr.InstallMod(ctx, 1, "another"); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil

	// 3. ReplaceMods
	if _, err := mgr.ReplaceMods(ctx, 999, nil, ""); err == nil {
		t.Errorf("expected error for non-existent instance")
	}
	if _, err := mgr.ReplaceMods(ctx, 2, nil, ""); err == nil {
		t.Errorf("expected error on vanilla instance")
	}
	mgr.stateStore = nil
	if _, err := mgr.ReplaceMods(ctx, 1, nil, ""); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented when stateStore is nil")
	}
	mgr.stateStore = store

	var hookCalled bool
	mgr.afterSyncHook = func(cm, dep, key string, want func(string) bool) {
		hookCalled = true
		want("jei\n")
		want("different\n")
	}
	changed, err := mgr.ReplaceMods(ctx, 1, []string{"jei", "", "appleskin"}, "", "operator")
	if err != nil || !changed {
		t.Errorf("expected changed ReplaceMods: %v, err: %v", changed, err)
	}
	if !hookCalled {
		t.Errorf("expected afterSyncHook called")
	}
	// no change
	changed2, err := mgr.ReplaceMods(ctx, 1, []string{"jei", "appleskin"}, "already there")
	if err != nil || changed2 {
		t.Errorf("expected no change on second ReplaceMods")
	}
	store.patchErr = fmt.Errorf("patch fail")
	if _, err := mgr.ReplaceMods(ctx, 1, []string{"new"}, ""); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil

	// 4. RemoveMod
	if err := mgr.RemoveMod(ctx, 1, ""); err == nil {
		t.Errorf("expected error for empty slug")
	}
	mgr.stateStore = nil
	if err := mgr.RemoveMod(ctx, 1, "jei"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented when stateStore is nil")
	}
	mgr.stateStore = store
	// nil data doc
	store.docs["manifests/instance-01/mods.yaml"] = ports.Document{Data: nil}
	if err := mgr.RemoveMod(ctx, 1, "jei"); err != nil {
		t.Errorf("expected nil when doc.Data is nil")
	}
	// not found in lines
	store.docs["manifests/instance-01/mods.yaml"] = ports.Document{Data: map[string]string{"mods.txt": "appleskin\n"}}
	if err := mgr.RemoveMod(ctx, 1, "jei"); err != nil {
		t.Errorf("expected nil when mod not found")
	}
	// successful remove
	store.docs["manifests/instance-01/mods.yaml"] = ports.Document{Data: map[string]string{"mods.txt": "jei\nappleskin\n"}}
	if err := mgr.RemoveMod(ctx, 1, "jei", "operator"); err != nil {
		t.Fatalf("unexpected error on RemoveMod: %v", err)
	}
	store.patchErr = fmt.Errorf("patch fail")
	if err := mgr.RemoveMod(ctx, 1, "appleskin"); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil
}

func TestManagerConfigsEdges(t *testing.T) {
	ctx := context.Background()
	store := newFullMockStateStore()
	events := &fullMockAuditAndEvent{}
	mgr := &InstanceManager{
		stateStore:          store,
		instancesRelPath:    "manifests",
		globalConfigsPath:   "manifests/global.yaml",
		audit:               events,
	}

	// 1. ListConfigs & GetConfig
	mgr.configsReader = func(ctx context.Context, num int) (map[string]string, error) {
		return map[string]string{"cfg1": "v1"}, nil
	}
	if cfgs, err := mgr.ListConfigs(ctx, 1); err != nil || len(cfgs) != 1 {
		t.Errorf("unexpected ListConfigs from reader: %v", cfgs)
	}
	if val, err := mgr.GetConfig(ctx, 1, "cfg1"); err != nil || val != "v1" {
		t.Errorf("unexpected GetConfig from reader: %s", val)
	}
	mgr.configsReader = func(ctx context.Context, num int) (map[string]string, error) {
		return nil, fmt.Errorf("reader fail")
	}
	if _, err := mgr.ListConfigs(ctx, 1); err == nil {
		t.Errorf("expected error when configsReader fails")
	}
	if _, err := mgr.GetConfig(ctx, 1, "cfg1"); err == nil {
		t.Errorf("expected error when configsReader fails")
	}
	mgr.configsReader = nil
	mgr.stateStore = nil
	if cfgs, err := mgr.ListConfigs(ctx, 1); err != nil || len(cfgs) != 0 {
		t.Errorf("expected empty when stateStore is nil")
	}
	if _, err := mgr.GetConfig(ctx, 1, "cfg1"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.stateStore = store
	store.getErr = fmt.Errorf("get fail")
	if _, err := mgr.ListConfigs(ctx, 1); err == nil {
		t.Errorf("expected error when get fails")
	}
	if _, err := mgr.GetConfig(ctx, 1, "cfg1"); err == nil {
		t.Errorf("expected error when get fails")
	}
	store.getErr = nil
	store.docs["manifests/instance-01/configs.yaml"] = ports.Document{Data: map[string]string{"test.cfg": "content"}}
	if cfgs, err := mgr.ListConfigs(ctx, 1); err != nil || len(cfgs) != 1 {
		t.Errorf("unexpected ListConfigs: %v", cfgs)
	}
	if val, err := mgr.GetConfig(ctx, 1, "test.cfg"); err != nil || val != "content" {
		t.Errorf("unexpected GetConfig: %s", val)
	}
	store.docs["manifests/instance-01/configs.yaml"] = ports.Document{Data: nil}
	if val, err := mgr.GetConfig(ctx, 1, "test.cfg"); err != nil || val != "" {
		t.Errorf("expected empty string when Data is nil")
	}

	// 2. SaveConfig & DeleteConfig
	mgr.stateStore = nil
	if _, err := mgr.SaveConfig(ctx, 1, "a", "b"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	if _, err := mgr.DeleteConfig(ctx, 1, "a"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.stateStore = store
	changed, err := mgr.SaveConfig(ctx, 1, "cfg", "data", "operator")
	if err != nil || !changed {
		t.Errorf("expected SaveConfig changed: %v, err: %v", changed, err)
	}
	changedNoop, err := mgr.SaveConfig(ctx, 1, "cfg", "data")
	if err != nil || changedNoop {
		t.Errorf("expected no-op on identical content")
	}
	store.patchErr = fmt.Errorf("patch fail")
	if _, err := mgr.SaveConfig(ctx, 1, "cfg", "other"); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil

	// DeleteConfig
	delChanged, err := mgr.DeleteConfig(ctx, 1, "cfg", "operator")
	if err != nil || !delChanged {
		t.Errorf("expected DeleteConfig changed: %v, err: %v", delChanged, err)
	}
	delNoop, err := mgr.DeleteConfig(ctx, 1, "nonexistent")
	if err != nil || delNoop {
		t.Errorf("expected no-op deleting nonexistent config")
	}
	store.docs["manifests/instance-01/configs.yaml"] = ports.Document{Data: nil}
	if _, err := mgr.DeleteConfig(ctx, 1, "cfg"); err != nil {
		t.Errorf("expected nil deleting from nil Data")
	}
	store.patchErr = fmt.Errorf("patch fail")
	if _, err := mgr.DeleteConfig(ctx, 1, "cfg"); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil

	// 3. ListGlobalConfigs & GetGlobalConfig
	mgr.globalConfigsReader = func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"g1": "v1"}, nil
	}
	if gcfgs, err := mgr.ListGlobalConfigs(ctx); err != nil || len(gcfgs) != 1 {
		t.Errorf("unexpected ListGlobalConfigs from reader: %v", gcfgs)
	}
	if val, err := mgr.GetGlobalConfig(ctx, "g1"); err != nil || val != "v1" {
		t.Errorf("unexpected GetGlobalConfig from reader: %s", val)
	}
	mgr.globalConfigsReader = func(ctx context.Context) (map[string]string, error) {
		return nil, fmt.Errorf("global reader fail")
	}
	if _, err := mgr.ListGlobalConfigs(ctx); err == nil {
		t.Errorf("expected error when globalConfigsReader fails")
	}
	if _, err := mgr.GetGlobalConfig(ctx, "g1"); err == nil {
		t.Errorf("expected error when globalConfigsReader fails")
	}
	mgr.globalConfigsReader = nil
	mgr.stateStore = nil
	if gcfgs, err := mgr.ListGlobalConfigs(ctx); err != nil || len(gcfgs) != 0 {
		t.Errorf("expected empty when stateStore is nil")
	}
	if _, err := mgr.GetGlobalConfig(ctx, "g1"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.stateStore = store
	store.getErr = fmt.Errorf("get fail")
	if _, err := mgr.ListGlobalConfigs(ctx); err == nil {
		t.Errorf("expected error when get fails")
	}
	if _, err := mgr.GetGlobalConfig(ctx, "g1"); err == nil {
		t.Errorf("expected error when get fails")
	}
	store.getErr = nil
	store.docs["manifests/global.yaml"] = ports.Document{Data: map[string]string{"g.cfg": "val"}}
	if gcfgs, err := mgr.ListGlobalConfigs(ctx); err != nil || len(gcfgs) != 1 {
		t.Errorf("unexpected ListGlobalConfigs: %v", gcfgs)
	}
	if val, err := mgr.GetGlobalConfig(ctx, "g.cfg"); err != nil || val != "val" {
		t.Errorf("unexpected GetGlobalConfig: %s", val)
	}
	store.docs["manifests/global.yaml"] = ports.Document{Data: nil}
	if val, err := mgr.GetGlobalConfig(ctx, "g.cfg"); err != nil || val != "" {
		t.Errorf("expected empty string when Data is nil")
	}

	// 4. SaveGlobalConfig & DeleteGlobalConfig
	mgr.afterSyncHook = func(cm, dep, key string, want func(string) bool) {
		want("val")
		want("")
	}
	mgr.stateStore = nil
	if _, err := mgr.SaveGlobalConfig(ctx, "g", "v"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	if _, err := mgr.DeleteGlobalConfig(ctx, "g"); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.stateStore = store
	gChanged, err := mgr.SaveGlobalConfig(ctx, "g.cfg", "val", "operator")
	if err != nil || !gChanged {
		t.Errorf("expected SaveGlobalConfig changed: %v, err: %v", gChanged, err)
	}
	gNoop, err := mgr.SaveGlobalConfig(ctx, "g.cfg", "val")
	if err != nil || gNoop {
		t.Errorf("expected no-op on identical global content")
	}
	store.patchErr = fmt.Errorf("patch fail")
	if _, err := mgr.SaveGlobalConfig(ctx, "g.cfg", "other"); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil

	// DeleteGlobalConfig
	gDelChanged, err := mgr.DeleteGlobalConfig(ctx, "g.cfg", "operator")
	if err != nil || !gDelChanged {
		t.Errorf("expected DeleteGlobalConfig changed: %v, err: %v", gDelChanged, err)
	}
	gDelNoop, err := mgr.DeleteGlobalConfig(ctx, "nonexistent")
	if err != nil || gDelNoop {
		t.Errorf("expected no-op deleting nonexistent global config")
	}
	store.docs["manifests/global.yaml"] = ports.Document{Data: nil}
	if _, err := mgr.DeleteGlobalConfig(ctx, "g.cfg"); err != nil {
		t.Errorf("expected nil deleting from nil Data")
	}
	store.patchErr = fmt.Errorf("patch fail")
	if _, err := mgr.DeleteGlobalConfig(ctx, "g.cfg"); err == nil {
		t.Errorf("expected error when patch fails")
	}
	store.patchErr = nil
}

func TestManagerDiagnosticsAndStatusEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	rt := &fullMockRuntime{}
	mgr := &InstanceManager{
		repo:    repo,
		runtime: rt,
	}

	repo.Upsert(domain.Instance{
		Number: 1,
		State:  domain.StateRunning,
		Name:   "MC1",
	})
	repo.Upsert(domain.Instance{
		Number: 2,
		State:  domain.StateStopped,
		Name:   "MC2",
	})

	// 1. ListBackups
	if b := mgr.ListBackups(domain.Instance{}); b != nil {
		t.Errorf("expected nil backups when backupsDir is empty")
	}
	tmpDir := t.TempDir()
	mgr.backupsDir = tmpDir
	_ = os.WriteFile(filepath.Join(tmpDir, "mc-mc1-01-daily.tar.gz"), []byte("data"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "valheim-val-01-daily.tar.gz"), []byte("data"), 0644)
	if b := mgr.ListBackups(domain.Instance{Number: 1, Slug: "mc1", GameID: domain.GameMinecraft}); len(b) != 1 {
		t.Errorf("expected 1 mc backup, got %d", len(b))
	}
	if b := mgr.ListBackups(domain.Instance{Number: 1, Slug: "val", GameID: domain.GameValheim}); len(b) != 1 {
		t.Errorf("expected 1 valheim backup, got %d", len(b))
	}

	// 2. InstanceStats
	rt.status = ports.Status{StartedAt: time.Now().Add(-10 * time.Minute)}
	mgr.telemetryProvider = func(ctx context.Context, inst domain.Instance) (int, bool) {
		return 3, true
	}
	stats := mgr.InstanceStats(ctx, []domain.Instance{
		{Number: 1, State: domain.StateRunning},
		{Number: 2, State: domain.StateStopped},
	})
	if len(stats) != 1 || stats[1].Players != 3 || !stats[1].PlayersKnown {
		t.Errorf("unexpected stats: %+v", stats)
	}

	// 3. ProvisioningStatus
	if _, _, err := mgr.ProvisioningStatus(ctx, 999); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	mgr.runtime = nil
	if phase, ready, err := mgr.ProvisioningStatus(ctx, 1); err != nil || phase != "ready" || !ready {
		t.Errorf("expected ready when runtime is nil: %s, %v, %v", phase, ready, err)
	}
	mgr.runtime = rt
	rt.statusErr = fmt.Errorf("status fail")
	if phase, ready, _ := mgr.ProvisioningStatus(ctx, 1); phase != "syncing" || ready {
		t.Errorf("expected syncing when status errors: %s", phase)
	}
	rt.statusErr = nil
	rt.status = ports.Status{Available: true}
	if phase, ready, _ := mgr.ProvisioningStatus(ctx, 1); phase != "ready" || !ready {
		t.Errorf("expected ready: %s", phase)
	}
	rt.status = ports.Status{Available: false, Lifecycle: ports.LifecycleRunning}
	if phase, ready, _ := mgr.ProvisioningStatus(ctx, 1); phase != "booting" || ready {
		t.Errorf("expected booting: %s", phase)
	}
	rt.status = ports.Status{Available: false, Lifecycle: ports.LifecycleStopped}
	if phase, ready, _ := mgr.ProvisioningStatus(ctx, 1); phase != "syncing" || ready {
		t.Errorf("expected syncing: %s", phase)
	}

	// 4. Budget
	b := mgr.Budget([]domain.Instance{{Number: 1, State: domain.StateRunning, Tier: domain.TierMedium}})
	if b.UsedGiB != 8 {
		t.Errorf("expected 8 GiB used, got %d", b.UsedGiB)
	}

	// 5. InstanceLogs
	mgr.runtime = nil
	if _, err := mgr.InstanceLogs(ctx, 1, 0); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.runtime = rt
	if _, err := mgr.InstanceLogs(ctx, 999, 0); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	rt.logs = "some logs"
	rc, err := mgr.InstanceLogs(ctx, 1, -1)
	if err != nil || rc == nil {
		t.Fatalf("unexpected logs error: %v", err)
	}
	_ = rc.Close()

	// 6. SaveInstance
	mgr.repo = nil
	if err := mgr.SaveInstance(domain.Instance{}); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.repo = repo
	if err := mgr.SaveInstance(domain.Instance{Number: 3, Name: "MC3"}); err != nil {
		t.Errorf("unexpected SaveInstance error: %v", err)
	}

	// 7. RuntimeStatus
	mgr.runtime = nil
	if _, err := mgr.RuntimeStatus(ctx, 1); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.runtime = rt
	if _, err := mgr.RuntimeStatus(ctx, 999); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	if st, err := mgr.RuntimeStatus(ctx, 1); err != nil {
		t.Errorf("unexpected RuntimeStatus error: %v", err)
	} else if st.Lifecycle != ports.LifecycleStopped {
		t.Errorf("unexpected lifecycle: %s", st.Lifecycle)
	}

	// 8. CrashLogs
	mgr.runtime = nil
	if _, err := mgr.CrashLogs(ctx, 1, 0); !errors.Is(err, ports.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented")
	}
	mgr.runtime = rt
	if _, err := mgr.CrashLogs(ctx, 999, 0); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	rcCrash, err := mgr.CrashLogs(ctx, 1, 0, "init-container")
	if err != nil || rcCrash == nil {
		t.Fatalf("unexpected CrashLogs error: %v", err)
	}
	_ = rcCrash.Close()

	// 9. ExecuteCommand
	if _, err := mgr.ExecuteCommand(ctx, 1, "cmd"); err == nil {
		t.Errorf("expected error when commandExecutor is nil")
	}
	mgr.commandExecutor = func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
		return "executed: " + cmd, nil
	}
	if _, err := mgr.ExecuteCommand(ctx, 999, "cmd"); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	if out, err := mgr.ExecuteCommand(ctx, 1, "say hi"); err != nil || out != "executed: say hi" {
		t.Errorf("unexpected command output: %s, err: %v", out, err)
	}
}

func TestManagerBackupAndRestoreEdges(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	runner := &fullMockJobRunner{}
	events := &fullMockAuditAndEvent{}
	tmpDir := t.TempDir()

	mgr := &InstanceManager{
		repo:       repo,
		jobRunner:  runner,
		backupsDir: tmpDir,
		audit:      events,
		event:      events,
	}

	repo.Upsert(domain.Instance{
		Number: 1,
		GameID: domain.GameValheim,
		Slug:   "val-world",
		State:  domain.StateRunning,
	})
	repo.Upsert(domain.Instance{
		Number: 2,
		GameID: domain.GameMinecraft,
		Slug:   "mc-world",
		State:  domain.StateRunning,
	})
	repo.Upsert(domain.Instance{
		Number: 3,
		GameID: domain.GameMinecraft,
		Slug:   "mc-stopped",
		State:  domain.StateStopped,
	})

	var executedCmds []string
	mgr.commandExecutor = func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
		executedCmds = append(executedCmds, cmd)
		return "ok", nil
	}

	// 1. CreateBackup
	if _, err := mgr.CreateBackup(ctx, 999); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	// Valheim backup command
	if bkp, err := mgr.CreateBackup(ctx, 1, "operator"); err != nil || bkp == "" {
		t.Fatalf("unexpected error creating valheim backup: %v", err)
	}
	// Minecraft backup commands (/save-off, /save-all flush, /save-on)
	if bkp, err := mgr.CreateBackup(ctx, 2, "operator"); err != nil || bkp == "" {
		t.Fatalf("unexpected error creating mc backup: %v", err)
	}
	if len(executedCmds) != 4 { // "save" + 3 mc commands
		t.Errorf("expected 4 commands, got %v", executedCmds)
	}
	runner.backupErr = fmt.Errorf("job fail")
	if _, err := mgr.CreateBackup(ctx, 3); err == nil {
		t.Errorf("expected error when CreateBackupJob fails")
	}
	runner.backupErr = nil

	// 2. pruneBackups edge cases
	if err := pruneBackups("", "mc", "slug", 1, 5); err != nil {
		t.Errorf("expected nil for empty backupsDir")
	}
	if err := pruneBackups(tmpDir, "mc", "slug", 1, 0); err != nil {
		t.Errorf("expected nil for 0 keepCount")
	}
	// Create multiple files to test pruning
	for i := 1; i <= 3; i++ {
		f := filepath.Join(tmpDir, fmt.Sprintf("mc-prune-01-%02d.tar.gz", i))
		_ = os.WriteFile(f, []byte("data"), 0644)
		_ = os.Chtimes(f, time.Now().Add(time.Duration(i)*time.Second), time.Now().Add(time.Duration(i)*time.Second))
	}
	if err := pruneBackups(tmpDir, "mc", "prune", 1, 1); err != nil {
		t.Fatalf("pruneBackups failed: %v", err)
	}

	// 3. RestoreInPlace
	if err := mgr.RestoreInPlace(ctx, 999, "backup.tar.gz"); err == nil {
		t.Errorf("expected error on non-existent instance")
	}
	if err := mgr.RestoreInPlace(ctx, 2, "backup.tar.gz"); err == nil {
		t.Errorf("expected error restoring into running instance")
	}
	if err := mgr.RestoreInPlace(ctx, 3, "invalid.zip"); err == nil {
		t.Errorf("expected error on invalid archive name")
	}
	runner.restoreErr = fmt.Errorf("restore fail")
	if err := mgr.RestoreInPlace(ctx, 3, "mc-mc-stopped-03-daily.tar.gz"); err == nil {
		t.Errorf("expected error when CreateRestoreJob fails")
	}
	runner.restoreErr = nil
	if err := mgr.RestoreInPlace(ctx, 3, "mc-mc-stopped-03-daily.tar.gz", "operator"); err != nil {
		t.Fatalf("unexpected error on RestoreInPlace: %v", err)
	}

	// 4. RestoreNew
	if _, err := mgr.RestoreNew(ctx, 999, "New", "medium", "backup.tar.gz"); err == nil {
		t.Errorf("expected error on non-existent source instance")
	}
	if _, err := mgr.RestoreNew(ctx, 3, "New", "medium", "invalid.zip"); err == nil {
		t.Errorf("expected error on invalid archive name")
	}
	mgr.renderer = &fullMockRenderer{}
	mgr.maxInstances = 5
	runner.restoreErr = fmt.Errorf("restore fail")
	if _, err := mgr.RestoreNew(ctx, 3, "", "", "mc-mc-stopped-03-daily.tar.gz"); err == nil {
		t.Errorf("expected error when restore job fails")
	}
	runner.restoreErr = nil
	restored, err := mgr.RestoreNew(ctx, 3, "Restored World", "large", "mc-mc-stopped-03-daily.tar.gz", "operator")
	if err != nil || restored == nil {
		t.Fatalf("unexpected error on RestoreNew: %v", err)
	}

	// 5. DeleteBackup
	mgr.backupsDir = ""
	if err := mgr.DeleteBackup(ctx, 1, "test.tar.gz"); err == nil {
		t.Errorf("expected error when backupsDir is empty")
	}
	mgr.backupsDir = tmpDir
	if err := mgr.DeleteBackup(ctx, 1, ""); err == nil {
		t.Errorf("expected error on empty filename")
	}
	if err := mgr.DeleteBackup(ctx, 1, "../unsafe.tar.gz"); err == nil {
		t.Errorf("expected error on unsafe filename")
	}
	delFile := "mc-delete-01-test.tar.gz"
	_ = os.WriteFile(filepath.Join(tmpDir, delFile), []byte("data"), 0644)
	if err := mgr.DeleteBackup(ctx, 1, delFile, "operator"); err != nil {
		t.Fatalf("unexpected error deleting backup: %v", err)
	}

	// 6. BackupSummary
	mgr.backupsDir = ""
	if _, ok := mgr.BackupSummary(); ok {
		t.Errorf("expected false when backupsDir is empty")
	}
	mgr.backupsDir = filepath.Join(tmpDir, "nonexistent")
	if _, ok := mgr.BackupSummary(); ok {
		t.Errorf("expected false for nonexistent directory")
	}
	mgr.backupsDir = tmpDir
	summaryFile := filepath.Join(tmpDir, "mc-sum-01-test.tar.gz")
	_ = os.WriteFile(summaryFile, []byte("some-data"), 0644)
	_ = os.Mkdir(filepath.Join(tmpDir, "subfolder"), 0755)
	summary, ok := mgr.BackupSummary()
	if !ok || summary.Count == 0 {
		t.Errorf("expected summary with count > 0: %+v", summary)
	}
}

func TestManagerExactEdgeCases(t *testing.T) {
	ctx := context.Background()
	repo := newFullMockRepo()
	store := newFullMockStateStore()
	rt := &fullMockRuntime{}
	renderer := &fullMockRenderer{}

	// 1. NewInstanceManager defaults instancesRelPath
	defMgr := NewInstanceManager(repo, store, rt, 0, 0, 0, "", "", renderer, "")
	if defMgr.instancesRelPath != "manifests/minecraft-modded" {
		t.Errorf("expected default instancesRelPath, got %s", defMgr.instancesRelPath)
	}

	mgr := &InstanceManager{
		repo:             repo,
		stateStore:       store,
		runtime:          rt,
		renderer:         renderer,
		totalBudgetGiB:   32,
		maxInstances:     10,
		maxRunning:       5,
		instancesRelPath: "manifests",
		commandExecutor:  func(ctx context.Context, inst domain.Instance, cmd string) (string, error) { return "ok", nil },
	}

	// 2. GetInstance updates LBIP from runtime Address
	repo.Upsert(domain.Instance{Number: 42, LBIP: "1.1.1.1"})
	rt.status = ports.Status{Address: "2.2.2.2"}
	inst42, err := mgr.GetInstance(ctx, 42)
	if err != nil || inst42.LBIP != "2.2.2.2" {
		t.Errorf("expected LBIP updated to 2.2.2.2, got %+v, %v", inst42, err)
	}

	// 3. CreateInstance default Minecraft name
	createdMC, err := mgr.CreateInstance(ctx, domain.Instance{Number: 5, GameID: domain.GameMinecraft, Name: ""}, "")
	if err != nil || createdMC.Name != "World 05" {
		t.Errorf("expected World 05, got %+v, %v", createdMC, err)
	}

	// 4. CreateInstance checkLBIPFree collision
	mgr.maxInstances = 100
	repo.Upsert(domain.Instance{Number: 50, GameID: domain.GameValheim, LBIP: "192.168.20.99"})
	if _, err := mgr.CreateInstance(ctx, domain.Instance{Number: 51, GameID: domain.GameMinecraft, LBIP: "192.168.20.99"}, ""); err == nil {
		t.Errorf("expected error on LBIP collision in CreateInstance")
	}

	// 5. UpdateSettings version update success
	repo.Upsert(domain.Instance{Number: 60, Name: "MC60", MCVersion: "1.21.1"})
	if err := mgr.UpdateSettings(ctx, 60, "MC60", "motd", "medium", "1.21.4"); err != nil {
		t.Errorf("unexpected error on UpdateSettings version change: %v", err)
	}
	if inst, _ := repo.Get(60); inst.MCVersion != "1.21.4" {
		t.Errorf("expected MCVersion 1.21.4, got %s", inst.MCVersion)
	}

	// 6. ReplaceMods afterSyncHook predicate returning true
	var predResult bool
	mgr.afterSyncHook = func(cm, deploy, file string, pred func(string) bool) {
		predResult = pred("mod1\nmod2\n")
	}
	repo.Upsert(domain.Instance{Number: 70, Name: "MC70"})
	if _, err := mgr.ReplaceMods(ctx, 70, []string{"mod1", "mod2"}, "update detail"); err != nil {
		t.Fatalf("unexpected ReplaceMods error: %v", err)
	}
	if !predResult {
		t.Errorf("expected predicate to evaluate to true")
	}

	// 7. pruneBackups bad pattern error
	if err := pruneBackups(t.TempDir(), "mc", "[-bad-glob", 1, 1); err == nil {
		t.Errorf("expected error from pruneBackups with bad glob pattern")
	}

	// 8. RestoreNew CreateInstance failure
	repo.Upsert(domain.Instance{Number: 80, Name: "MC80"})
	renderer.err = fmt.Errorf("render failure")
	if _, err := mgr.RestoreNew(ctx, 80, "MC81", "medium", "archive.tar.gz"); err == nil {
		t.Errorf("expected error when CreateInstance fails in RestoreNew")
	}
	renderer.err = nil

	// 9. DeleteBackup os.Remove error (file does not exist)
	tmpDir := t.TempDir()
	mgr.backupsDir = tmpDir
	if err := mgr.DeleteBackup(ctx, 1, "mc-world-01-daily.tar.gz"); err == nil {
		t.Errorf("expected error when deleting non-existent backup file")
	}

	// 10. BackupSummary e.Info() failure skipped
	noExecDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(noExecDir, "file.tar.gz"), []byte("data"), 0644)
	_ = os.Chmod(noExecDir, 0600)
	defer os.Chmod(noExecDir, 0700)
	mgr.backupsDir = noExecDir
	summary2, ok := mgr.BackupSummary()
	if !ok || summary2.Count != 0 {
		t.Errorf("expected summary with 0 count when e.Info() fails, got %d", summary2.Count)
	}

	// 11. repo.Get error paths across all methods
	repo.getErr = fmt.Errorf("repo get fail")
	if err := mgr.StartInstance(ctx, 1); err == nil {
		t.Errorf("expected error on StartInstance when repo.Get fails")
	}
	if err := mgr.StopInstance(ctx, 1); err == nil {
		t.Errorf("expected error on StopInstance when repo.Get fails")
	}
	if err := mgr.DeleteInstance(ctx, 1); err == nil {
		t.Errorf("expected error on DeleteInstance when repo.Get fails")
	}
	if err := mgr.RestartInstance(ctx, 1); err == nil {
		t.Errorf("expected error on RestartInstance when repo.Get fails")
	}
	if err := mgr.UpdateSettings(ctx, 1, "n", "m", "t", "v"); err == nil {
		t.Errorf("expected error on UpdateSettings when repo.Get fails")
	}
	if err := mgr.UpdateValheimSettings(ctx, 1, "n", "m", "t", "p"); err == nil {
		t.Errorf("expected error on UpdateValheimSettings when repo.Get fails")
	}
	if _, err := mgr.InstallMod(ctx, 1, "slug"); err == nil {
		t.Errorf("expected error on InstallMod when repo.Get fails")
	}
	if _, err := mgr.ReplaceMods(ctx, 1, []string{"mod1"}, "detail"); err == nil {
		t.Errorf("expected error on ReplaceMods when repo.Get fails")
	}
	if backups := mgr.ListBackups(domain.Instance{Number: 1, Slug: "[-bad-glob"}); backups != nil {
		t.Errorf("expected nil backups when glob pattern fails")
	}
	if _, err := mgr.InstanceLogs(ctx, 1, 10); err == nil {
		t.Errorf("expected error on InstanceLogs when repo.Get fails")
	}
	if _, err := mgr.RuntimeStatus(ctx, 1); err == nil {
		t.Errorf("expected error on RuntimeStatus when repo.Get fails")
	}
	if _, err := mgr.CrashLogs(ctx, 1, 10); err == nil {
		t.Errorf("expected error on CrashLogs when repo.Get fails")
	}
	if _, err := mgr.ExecuteCommand(ctx, 1, "cmd"); err == nil {
		t.Errorf("expected error on ExecuteCommand when repo.Get fails")
	}
	repo.getErr = nil

	// 12. StartInstance repo.List error
	repo.listErr = fmt.Errorf("repo list fail")
	repo.Upsert(domain.Instance{Number: 90, State: domain.StateStopped})
	rt.status = ports.Status{Lifecycle: ports.LifecycleStopped}
	if err := mgr.StartInstance(ctx, 90); err == nil {
		t.Errorf("expected error on StartInstance when repo.List fails")
	}
	repo.listErr = nil
}
