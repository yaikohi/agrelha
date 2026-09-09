package minecraft

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agrelha/internal/gitops"
	"agrelha/internal/k8s"
	"agrelha/internal/store"
)

type InstanceManager struct {
	store            *store.Store
	committer        *gitops.Committer
	k8sClient        *k8s.Client
	totalBudgetGiB   int
	maxInstances     int
	maxRunning       int
	instancesRelPath string
	lbBaseIP         string
	nodeSelector     string
	namespace        string
}

func NewInstanceManager(
	st *store.Store,
	c *gitops.Committer,
	k *k8s.Client,
	totalBudgetGiB, maxInstances, maxRunning int,
	instancesRelPath string,
	lbBaseIP string,
	nodeSelector string,
	namespace string,
) *InstanceManager {
	if totalBudgetGiB <= 0 {
		totalBudgetGiB = TotalBudgetGiB
	}
	if maxInstances <= 0 {
		maxInstances = MaxInstances
	}
	if maxRunning <= 0 {
		maxRunning = MaxRunning
	}
	if instancesRelPath == "" {
		instancesRelPath = "manifests/minecraft-modded"
	}
	if lbBaseIP == "" {
		lbBaseIP = DefaultLBBaseIP
	}
	return &InstanceManager{
		store:            st,
		committer:        c,
		k8sClient:        k,
		totalBudgetGiB:   totalBudgetGiB,
		maxInstances:     maxInstances,
		maxRunning:       maxRunning,
		instancesRelPath: instancesRelPath,
		lbBaseIP:         lbBaseIP,
		nodeSelector:     nodeSelector,
		namespace:        namespace,
	}
}

func (m *InstanceManager) TotalBudgetGiB() int { return m.totalBudgetGiB }
func (m *InstanceManager) MaxInstances() int   { return m.maxInstances }
func (m *InstanceManager) MaxRunning() int     { return m.maxRunning }

func (m *InstanceManager) ListInstances(ctx context.Context) ([]Instance, error) {
	records, err := m.store.ListInstances()
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	instances := make([]Instance, 0, len(records))
	for _, r := range records {
		inst := InstanceFromRecord(r)
		if m.k8sClient != nil {
			desired, ready, err := m.k8sClient.DeploymentReplicas(ctx, inst.DeploymentName())
			if err == nil {
				var newState InstanceState
				if ready > 0 {
					newState = StateRunning
				} else if desired == 0 {
					newState = StateStopped
				} else {
					newState = StateProvisioning
				}
				if inst.State != newState {
					inst.State = newState
					_ = m.store.UpdateInstanceState(inst.Number, string(newState))
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

func (m *InstanceManager) GetInstance(ctx context.Context, num int) (*Instance, error) {
	rec, err := m.store.GetInstance(num)
	if err != nil {
		return nil, fmt.Errorf("get instance %d: %w", num, err)
	}
	if rec == nil {
		return nil, nil
	}
	inst := InstanceFromRecord(*rec)
	if m.k8sClient != nil {
		desired, ready, err := m.k8sClient.DeploymentReplicas(ctx, inst.DeploymentName())
		if err == nil {
			var newState InstanceState
			if ready > 0 {
				newState = StateRunning
			} else if desired == 0 {
				newState = StateStopped
			} else {
				newState = StateProvisioning
			}
			if inst.State != newState {
				inst.State = newState
				_ = m.store.UpdateInstanceState(inst.Number, string(newState))
			}
		}
	}
	return &inst, nil
}

func (m *InstanceManager) CreateInstance(ctx context.Context, inst Instance, modsTxt string) (*Instance, error) {
	existing, err := m.store.ListInstances()
	if err != nil {
		return nil, err
	}
	if len(existing) >= m.maxInstances {
		return nil, fmt.Errorf("cannot create instance: maximum limit of %d instances reached", m.maxInstances)
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

	files, err := RenderInstanceManifests(inst, modsTxt, m.nodeSelector, m.namespace)
	if err != nil {
		return nil, fmt.Errorf("render manifests: %w", err)
	}

	dirRel := fmt.Sprintf("%s/instance-%02d", m.instancesRelPath, inst.Number)
	commitMsg := fmt.Sprintf("mc: create instance %02d (%s)", inst.Number, inst.Name)

	if m.committer != nil {
		if _, err := m.committer.WriteDirectory(ctx, dirRel, files, commitMsg); err != nil {
			return nil, fmt.Errorf("gitops write instance manifests: %w", err)
		}
	}

	if err := m.store.UpsertInstance(inst.ToRecord()); err != nil {
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
	if inst.State == StateRunning {
		return nil
	}

	instances, err := m.ListInstances(ctx)
	if err != nil {
		return err
	}

	usedGiB := 0
	runningCount := 0
	for _, other := range instances {
		if other.Number != num && other.State == StateRunning {
			runningCount++
			usedGiB += other.MemoryGiB()
		}
	}

	if runningCount >= m.maxRunning {
		return fmt.Errorf("cannot start instance: maximum of %d running instances reached (please stop another world first)", m.maxRunning)
	}

	if usedGiB+inst.MemoryGiB() > m.totalBudgetGiB {
		return fmt.Errorf("cannot start instance: RAM budget exceeded (%d GiB in use, requires %d GiB, total budget is %d GiB)",
			usedGiB, inst.MemoryGiB(), m.totalBudgetGiB)
	}

	if m.k8sClient != nil {
		if err := m.k8sClient.ScaleDeployment(ctx, inst.DeploymentName(), 1); err != nil {
			return fmt.Errorf("scale up deployment %s: %w", inst.DeploymentName(), err)
		}
	}

	inst.State = StateRunning
	return m.store.UpdateInstanceState(num, string(StateRunning))
}

func (m *InstanceManager) StopInstance(ctx context.Context, num int) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if m.k8sClient != nil {
		if err := m.k8sClient.ScaleDeployment(ctx, inst.DeploymentName(), 0); err != nil {
			return fmt.Errorf("scale down deployment %s: %w", inst.DeploymentName(), err)
		}
	}

	inst.State = StateStopped
	return m.store.UpdateInstanceState(num, string(StateStopped))
}

func (m *InstanceManager) DeleteInstance(ctx context.Context, num int) error {
	inst, err := m.GetInstance(ctx, num)
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("instance %d not found", num)
	}

	if m.k8sClient != nil {
		desired, ready, err := m.k8sClient.DeploymentReplicas(ctx, inst.DeploymentName())
		if err == nil && (desired > 0 || ready > 0) {
			return fmt.Errorf("instance %d (%s) must be stopped before it can be deleted", num, inst.Name)
		}
	}

	dirRel := fmt.Sprintf("%s/instance-%02d", m.instancesRelPath, num)
	commitMsg := fmt.Sprintf("mc: delete instance %02d (%s)", num, inst.Name)

	if m.committer != nil {
		if _, err := m.committer.DeleteDirectory(ctx, dirRel, commitMsg); err != nil {
			return fmt.Errorf("gitops delete instance manifests: %w", err)
		}
	}

	return m.store.DeleteInstance(num)
}

type BudgetInfo struct {
	UsedGiB        int
	TotalBudgetGiB int
	RunningCount   int
	MaxRunning     int
	TotalInstances int
	MaxInstances   int
}

func (m *InstanceManager) Budget(instances []Instance) BudgetInfo {
	usedGiB := 0
	runningCount := 0
	for _, inst := range instances {
		if inst.State == StateRunning {
			usedGiB += inst.MemoryGiB()
			runningCount++
		}
	}
	return BudgetInfo{
		UsedGiB:        usedGiB,
		TotalBudgetGiB: m.totalBudgetGiB,
		RunningCount:   runningCount,
		MaxRunning:     m.maxRunning,
		TotalInstances: len(instances),
		MaxInstances:   m.maxInstances,
	}
}
