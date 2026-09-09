package instances

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const defaultLBBaseIP = ""

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
	return &InstanceManager{
		repo:             repo,
		stateStore:       stateStore,
		runtime:          rt,
		totalBudgetGiB:   totalBudgetGiB,
		maxInstances:     maxInstances,
		maxRunning:       maxRunning,
		instancesRelPath: instancesRelPath,
		lbBaseIP:         lbBaseIP,
		renderer:         renderer,
		namespace:        namespace,
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

func (m *InstanceManager) CreateInstance(ctx context.Context, inst domain.Instance, modsTxt string) (*domain.Instance, error) {
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

	return &inst, nil
}

func (m *InstanceManager) StartInstance(ctx context.Context, num int) error {
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
	return m.repo.UpdateState(num, domain.StateRunning)
}

func (m *InstanceManager) StopInstance(ctx context.Context, num int) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if m.runtime != nil {
		if err := m.runtime.Stop(ctx, m.serverRef(*inst)); err != nil {
			return fmt.Errorf("stop instance %d: %w", inst.Number, err)
		}
	}

	inst.State = domain.StateStopped
	return m.repo.UpdateState(num, domain.StateStopped)
}

func (m *InstanceManager) DeleteInstance(ctx context.Context, num int) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
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

	return m.repo.Delete(num)
}

type BudgetInfo = domain.Budget

func (m *InstanceManager) Budget(instances []domain.Instance) BudgetInfo {
	return domain.CalculateBudget(instances, m.totalBudgetGiB, m.maxRunning, m.maxInstances)
}
