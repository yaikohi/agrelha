package instances

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockJobRunner struct {
	createdBackups  []string
	createdRestores []string
	backupErr       error
	restoreErr      error
}

func (m *mockJobRunner) CreateBackupJob(ctx context.Context, jobName, backupName, dataPVC, backupsPVC string) error {
	m.createdBackups = append(m.createdBackups, backupName)
	return m.backupErr
}

func (m *mockJobRunner) CreateRestoreJob(ctx context.Context, jobName, archiveName, dataPVC, backupsPVC string) error {
	m.createdRestores = append(m.createdRestores, archiveName)
	return m.restoreErr
}

type mockEventRecorder struct {
	entries []string
}

func (m *mockEventRecorder) RecordEvent(eventType, message string) error {
	m.entries = append(m.entries, fmt.Sprintf("%s:%s", eventType, message))
	return nil
}

type mockRuntimePort struct {
	status ports.Status
	logs   string
	err    error
}

func (m *mockRuntimePort) Start(ctx context.Context, ref ports.ServerRef) error { return m.err }
func (m *mockRuntimePort) Stop(ctx context.Context, ref ports.ServerRef) error  { return m.err }
func (m *mockRuntimePort) Restart(ctx context.Context, ref ports.ServerRef) error {
	return m.err
}
func (m *mockRuntimePort) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return m.status, m.err
}
func (m *mockRuntimePort) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, m.err
}
func (m *mockRuntimePort) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	return io.NopCloser(strings.NewReader(m.logs)), nil
}
func (m *mockRuntimePort) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return m.err
}

type mockOpStateStore struct {
	docs map[string]ports.Document
}

func (s *mockOpStateStore) Get(_ context.Context, path string) (ports.Document, error) {
	if s.docs == nil {
		return ports.Document{}, nil
	}
	return s.docs[path], nil
}
func (s *mockOpStateStore) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	s.docs[path] = doc
	return nil
}
func (s *mockOpStateStore) Delete(_ context.Context, path, _ string) error {
	delete(s.docs, path)
	return nil
}
func (s *mockOpStateStore) PutTree(_ context.Context, _ string, tree map[string]ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	for k, v := range tree {
		s.docs[k] = v
	}
	return nil
}
func (s *mockOpStateStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
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

func TestManagerOptionsAndAccessors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), nil, nil, 32, 8, 4,
		"manifests/minecraft-modded", "192.168.20.224", nil, "minecraft-modded",
		WithGameID(domain.GameMinecraft),
	)

	if mgr.GameID() != domain.GameMinecraft {
		t.Errorf("expected GameMinecraft, got %v", mgr.GameID())
	}
	if mgr.TotalBudgetGiB() != 32 {
		t.Errorf("expected 32 GiB, got %d", mgr.TotalBudgetGiB())
	}
	if mgr.MaxInstances() != 8 {
		t.Errorf("expected max 8 instances, got %d", mgr.MaxInstances())
	}
	if mgr.MaxRunning() != 4 {
		t.Errorf("expected max 4 running, got %d", mgr.MaxRunning())
	}

	preStopCalled := false
	preDeleteCalled := false
	afterSyncCalled := false

	mgr.ApplyOptions(
		WithBackupsPVC("custom-backups-pvc"),
		WithServerRefResolver(func(inst domain.Instance) ports.ServerRef {
			return ports.ServerRef{Name: "custom-" + inst.Slug}
		}),
		WithJobRunner(&mockJobRunner{}),
		WithAudit(&mockAuditRecorder{}),
		WithEvent(&mockEventRecorder{}),
		WithDependencyResolver(func(ctx context.Context, slug, mcVersion, loader string) ([]string, error) {
			return []string{"dep1"}, nil
		}),
		WithVersionResolver(func(ctx context.Context, mod string) (string, error) { return "1.0", nil }),
		WithModsReader(func(ctx context.Context, num int) ([]string, error) { return []string{"mod1"}, nil }),
		WithConfigsReader(func(ctx context.Context, num int) (map[string]string, error) { return map[string]string{"k": "v"}, nil }),
		WithGlobalConfigsReader(func(ctx context.Context) (map[string]string, error) { return map[string]string{"g": "v"}, nil }),
		WithGlobalConfigsPath("manifests/minecraft-modded/configs.yaml"),
		WithBackupsDir(t.TempDir()),
		WithPreStopHook(func(ctx context.Context, inst domain.Instance) { preStopCalled = true }),
		WithPreDeleteHook(func(ctx context.Context, inst domain.Instance) { preDeleteCalled = true }),
		WithTelemetryProvider(func(ctx context.Context, inst domain.Instance) (int, bool) {
			return 3, true
		}),
		WithAfterSyncHook(func(cm, dep, key string, check func(string) bool) { afterSyncCalled = true }),
	)

	if mgr.effectiveBackupsPVC() != "custom-backups-pvc" {
		t.Errorf("expected effectiveBackupsPVC custom-backups-pvc, got %s", mgr.effectiveBackupsPVC())
	}

	// Test serverRef resolver
	ref := mgr.serverRef(domain.Instance{Slug: "world1", Number: 1})
	if ref.Name != "custom-world1" {
		t.Errorf("expected custom-world1, got %s", ref.Name)
	}

	// Trigger hooks
	if mgr.preStopHook != nil {
		mgr.preStopHook(context.Background(), domain.Instance{})
		if !preStopCalled {
			t.Errorf("preStopHook was not invoked")
		}
	}
	if mgr.preDeleteHook != nil {
		mgr.preDeleteHook(context.Background(), domain.Instance{})
		if !preDeleteCalled {
			t.Errorf("preDeleteHook was not invoked")
		}
	}
	if mgr.afterSyncHook != nil {
		mgr.afterSyncHook("cm", "dep", "key", nil)
		if !afterSyncCalled {
			t.Errorf("afterSyncHook was not invoked")
		}
	}
}

func TestManagerLifecycleAndRuntimeOperations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	mockRt := &mockRuntimePort{
		status: ports.Status{
			Lifecycle: ports.LifecycleRunning,
			Available: true,
			StartedAt: time.Now().Add(-120 * time.Second),
		},
		logs: "server ready\n",
	}

	commandsRun := []string{}
	cmdExec := func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
		commandsRun = append(commandsRun, cmd)
		return "ok", nil
	}

	ss := &mockOpStateStore{}

	mgr := NewInstanceManager(
		store.NewValheimInstanceRepo(st), ss, mockRt, 24, 4, 2,
		"manifests/valheim", "192.168.20.224", nil, "valheim",
		WithGameID(domain.GameValheim),
		WithCommandExecutor(cmdExec),
		WithTelemetryProvider(func(ctx context.Context, inst domain.Instance) (int, bool) {
			return 4, true
		}),
	)

	ctx := context.Background()

	// Seed an instance
	inst := domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Slug:   "iron-world",
		Name:   "Iron World",
		State:  domain.StateRunning,
		Tier:   domain.TierSmall,
	}
	if err := mgr.SaveInstance(inst); err != nil {
		t.Fatalf("save instance failed: %v", err)
	}

	// 1. RuntimeStatus
	status, err := mgr.RuntimeStatus(ctx, 1)
	if err != nil {
		t.Fatalf("runtime status error: %v", err)
	}
	if status.Lifecycle != ports.LifecycleRunning || !status.Available {
		t.Errorf("unexpected runtime status: %+v", status)
	}

	// 2. InstanceLogs and CrashLogs
	logsReader, err := mgr.InstanceLogs(ctx, 1, 50)
	if err != nil {
		t.Fatalf("instance logs error: %v", err)
	}
	buf, _ := io.ReadAll(logsReader)
	_ = logsReader.Close()
	if string(buf) != "server ready\n" {
		t.Errorf("expected server ready logs, got %q", string(buf))
	}

	crashLogsReader, err := mgr.CrashLogs(ctx, 1, 20)
	if err != nil {
		t.Fatalf("crash logs error: %v", err)
	}
	crashBuf, _ := io.ReadAll(crashLogsReader)
	_ = crashLogsReader.Close()
	if string(crashBuf) != "server ready\n" {
		t.Errorf("expected crash logs, got %q", string(crashBuf))
	}

	// 3. RestartInstance (running instance)
	if err := mgr.RestartInstance(ctx, 1); err != nil {
		t.Errorf("restart running instance failed: %v", err)
	}

	// 4. InstanceStats
	stats := mgr.InstanceStats(ctx, []domain.Instance{inst})
	st1, ok := stats[1]
	if !ok || st1.Players != 4 || !st1.PlayersKnown {
		t.Errorf("expected stats for inst 1 with 4 players, got %+v", st1)
	}

	// 5. ProvisioningStatus
	phase, ready, err := mgr.ProvisioningStatus(ctx, 1)
	if err != nil {
		t.Errorf("provisioning status error: %v", err)
	}
	if !ready || phase != "ready" {
		t.Errorf("expected ready phase, got phase=%s ready=%v", phase, ready)
	}

	// 6. ExecuteCommand
	out, err := mgr.ExecuteCommand(ctx, 1, "status")
	if err != nil || out != "ok" {
		t.Errorf("execute command failed: %v, out: %s", err, out)
	}
	if len(commandsRun) == 0 || commandsRun[0] != "status" {
		t.Errorf("command status not recorded: %v", commandsRun)
	}

	// 7. ReplaceMods
	changed, err := mgr.ReplaceMods(ctx, 1, []string{"ValheimPlus", "Jotunn"}, "test replace", "admin")
	if err != nil {
		t.Fatalf("replace mods failed: %v", err)
	}
	if !changed {
		t.Errorf("expected ReplaceMods to report changed=true")
	}

	// 8. Restart non-existent instance returns error
	if err := mgr.RestartInstance(ctx, 999); err == nil {
		t.Errorf("expected error restarting non-existent instance")
	}
}

func TestManagerBackupAndRestore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	backupsDir := t.TempDir()
	jobRunner := &mockJobRunner{}
	auditRec := &mockAuditRecorder{}
	eventRec := &mockEventRecorder{}
	ss := &mockOpStateStore{}
	renderer := manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded")

	commandsRun := []string{}
	cmdExec := func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
		commandsRun = append(commandsRun, cmd)
		return "flushed", nil
	}

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), ss, nil, 24, 4, 2,
		"manifests/minecraft-modded", "192.168.20.224", renderer, "minecraft-modded",
		WithGameID(domain.GameMinecraft),
		WithBackupsDir(backupsDir),
		WithJobRunner(jobRunner),
		WithAudit(auditRec),
		WithEvent(eventRec),
		WithCommandExecutor(cmdExec),
	)

	ctx := context.Background()

	// Seed instance
	inst := domain.Instance{
		GameID:    domain.GameMinecraft,
		Number:    1,
		Slug:      "vault-hunters",
		Name:      "Vault Hunters",
		State:     domain.StateRunning,
		Tier:      domain.TierLarge,
		MCVersion: "1.20.1",
		Loader:    domain.LoaderNeoForge,
	}
	if err := mgr.SaveInstance(inst); err != nil {
		t.Fatalf("save instance failed: %v", err)
	}

	// 1. CreateBackup on running Minecraft world
	bkpName, err := mgr.CreateBackup(ctx, 1, "admin1")
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}
	if !strings.HasPrefix(bkpName, "mc-vault-hunters-01-") || !strings.HasSuffix(bkpName, ".tar.gz") {
		t.Errorf("unexpected backup file name: %s", bkpName)
	}
	// Verify commands flushed
	expectedCmds := []string{"/save-off", "/save-all flush", "/save-on"}
	for i, exp := range expectedCmds {
		if i >= len(commandsRun) || commandsRun[i] != exp {
			t.Errorf("expected command %s at %d, got %v", exp, i, commandsRun)
		}
	}
	// Verify job created
	if len(jobRunner.createdBackups) != 1 || jobRunner.createdBackups[0] != bkpName {
		t.Errorf("expected job runner called with %s, got %v", bkpName, jobRunner.createdBackups)
	}

	// Create actual mock backup files in backupsDir to test listing and pruning
	f1 := filepath.Join(backupsDir, "mc-vault-hunters-01-20260901-120000.tar.gz")
	f2 := filepath.Join(backupsDir, "mc-vault-hunters-01-20260902-120000.tar.gz")
	f3 := filepath.Join(backupsDir, "mc-vault-hunters-01-20260903-120000.tar.gz")
	_ = os.WriteFile(f1, []byte("backup 1 contents"), 0o644)
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(f2, []byte("backup 2 contents"), 0o644)
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(f3, []byte("backup 3 contents"), 0o644)

	// 2. ListBackups
	list := mgr.ListBackups(inst)
	if len(list) != 3 {
		t.Errorf("expected 3 backups, got %d", len(list))
	}

	// 3. BackupSummary
	summary, ok := mgr.BackupSummary()
	if !ok || summary.Count != 3 || summary.TotalSize <= 0 {
		t.Errorf("expected summary with 3 backups, got %+v (ok=%v)", summary, ok)
	}

	// 4. pruneBackups (keep 2)
	if err := pruneBackups(backupsDir, "mc", "vault-hunters", 1, 2); err != nil {
		t.Fatalf("prune backups failed: %v", err)
	}
	if _, err := os.Stat(f1); !os.IsNotExist(err) {
		t.Errorf("expected oldest backup f1 to be pruned")
	}
	if _, err := os.Stat(f3); err != nil {
		t.Errorf("expected newest backup f3 to exist: %v", err)
	}

	// 5. RestoreInPlace on running instance must fail
	err = mgr.RestoreInPlace(ctx, 1, "mc-vault-hunters-01-20260903-120000.tar.gz", "admin")
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("expected error when restoring on running instance, got: %v", err)
	}

	// Stop instance, then RestoreInPlace succeeds
	inst.State = domain.StateStopped
	_ = mgr.SaveInstance(inst)
	err = mgr.RestoreInPlace(ctx, 1, "mc-vault-hunters-01-20260903-120000.tar.gz", "admin")
	if err != nil {
		t.Errorf("restore in place failed: %v", err)
	}
	if len(jobRunner.createdRestores) != 1 {
		t.Errorf("expected restore job created, got: %v", jobRunner.createdRestores)
	}

	// 6. RestoreNew provisions a restored clone
	restored, err := mgr.RestoreNew(ctx, 1, "Vault Hunters Restored", string(domain.TierSmall), "mc-vault-hunters-01-20260903-120000.tar.gz", "admin")
	if err != nil {
		t.Fatalf("restore new failed: %v", err)
	}
	if restored.Number != 2 || restored.Name != "Vault Hunters Restored" {
		t.Errorf("unexpected restored instance: %+v", restored)
	}

	// 7. DeleteBackup
	if err := mgr.DeleteBackup(ctx, 1, "mc-vault-hunters-01-20260903-120000.tar.gz", "admin"); err != nil {
		t.Errorf("delete backup failed: %v", err)
	}
	if _, err := os.Stat(f3); !os.IsNotExist(err) {
		t.Errorf("expected f3 to be deleted")
	}

	// DeleteBackup invalid names
	if err := mgr.DeleteBackup(ctx, 1, "../evil.tar.gz"); err == nil {
		t.Errorf("expected error for path traversal delete")
	}
}

func TestManagerGlobalConfigs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cfgPath := "manifests/minecraft-modded/configs.yaml"
	ss := &mockOpStateStore{
		docs: map[string]ports.Document{
			cfgPath: {
				Data: map[string]string{
					"server.properties": "motd=Test Server\npvp=true\n",
				},
			},
		},
	}

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), ss, nil, 24, 4, 2,
		"manifests/minecraft-modded", "192.168.20.224", nil, "minecraft-modded",
		WithGlobalConfigsPath(cfgPath),
		WithAudit(&mockAuditRecorder{}),
	)

	ctx := context.Background()

	// 1. ListGlobalConfigs
	list, err := mgr.ListGlobalConfigs(ctx)
	if err != nil {
		t.Fatalf("list global configs failed: %v", err)
	}
	if len(list) != 1 || list[0] != "server.properties" {
		t.Errorf("expected server.properties, got %v", list)
	}

	// 2. GetGlobalConfig
	content, err := mgr.GetGlobalConfig(ctx, "server.properties")
	if err != nil {
		t.Fatalf("get global config failed: %v", err)
	}
	if !strings.Contains(content, "motd=Test Server") {
		t.Errorf("unexpected content: %s", content)
	}

	// 3. SaveGlobalConfig
	changed, err := mgr.SaveGlobalConfig(ctx, "server.properties", "motd=Updated\npvp=false\n", "admin")
	if err != nil || !changed {
		t.Fatalf("save global config failed: %v, changed: %v", err, changed)
	}
	updated, _ := mgr.GetGlobalConfig(ctx, "server.properties")
	if !strings.Contains(updated, "motd=Updated") {
		t.Errorf("expected updated content, got %s", updated)
	}

	// 4. DeleteGlobalConfig
	delChanged, err := mgr.DeleteGlobalConfig(ctx, "server.properties", "admin")
	if err != nil || !delChanged {
		t.Fatalf("delete global config failed: %v, changed: %v", err, delChanged)
	}
	listAfter, _ := mgr.ListGlobalConfigs(ctx)
	if len(listAfter) != 0 {
		t.Errorf("expected empty list after deletion, got %v", listAfter)
	}
}
