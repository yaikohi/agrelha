package k8s

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClientLifecycleAndScale(t *testing.T) {
	one := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim", Namespace: "valheim"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "valheim",
							Env: []corev1.EnvVar{
								{Name: "SERVER_NAME", Value: "MyValheim"},
							},
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
	}

	cs := fake.NewSimpleClientset(dep)
	c := NewWithClientset(cs, "valheim", "valheim")

	if c.Deployment() != "valheim" {
		t.Errorf("expected deployment valheim, got %s", c.Deployment())
	}
	c.SetDeployment("valheim")
	if c.Clientset() == nil {
		t.Fatal("expected clientset not nil")
	}

	c.SetNodeSelector("dedicated=gameserver")
	if c.nodeSelectorKey != "dedicated" || c.nodeSelectorValue != "gameserver" {
		t.Errorf("unexpected node selector: %s=%s", c.nodeSelectorKey, c.nodeSelectorValue)
	}

	ctx := context.Background()

	// Replicas
	des, ready, err := c.DeploymentReplicas(ctx, "valheim")
	if err != nil || des != 1 || ready != 1 {
		t.Fatalf("DeploymentReplicas failed: des=%d ready=%d err=%v", des, ready, err)
	}

	des, ready, err = c.Replicas(ctx)
	if err != nil || des != 1 || ready != 1 {
		t.Fatalf("Replicas failed: des=%d ready=%d err=%v", des, ready, err)
	}

	// Scale
	if err := c.Scale(ctx, 2); err != nil {
		t.Fatalf("Scale failed: %v", err)
	}
	if err := c.ScaleDeployment(ctx, "valheim", 1); err != nil {
		t.Fatalf("ScaleDeployment failed: %v", err)
	}

	// Restart
	if err := c.Restart(ctx); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}
	if err := c.RestartDeployment(ctx, "valheim"); err != nil {
		t.Fatalf("RestartDeployment failed: %v", err)
	}

	// Jobs
	if err := c.CreateBackupJob(ctx, "bk-1", "bk.tar.gz", "data-pvc", "bk-pvc"); err != nil {
		t.Fatalf("CreateBackupJob failed: %v", err)
	}
	if err := c.CreateRestoreJob(ctx, "res-1", "bk.tar.gz", "data-pvc", "bk-pvc"); err != nil {
		t.Fatalf("CreateRestoreJob failed: %v", err)
	}

	// DeploymentEnv
	val, ok, err := c.DeploymentEnv(ctx, "valheim", "SERVER_NAME")
	if err != nil || !ok || val != "MyValheim" {
		t.Fatalf("DeploymentEnv failed: val=%s ok=%v err=%v", val, ok, err)
	}
	val, ok, err = c.DeploymentEnv(ctx, "valheim", "MISSING_KEY")
	if err != nil || ok || val != "" {
		t.Fatalf("DeploymentEnv missing key failed: val=%s ok=%v err=%v", val, ok, err)
	}
	_, _, err = c.DeploymentEnv(ctx, "nonexistent-dep", "SERVER_NAME")
	if err == nil {
		t.Fatal("expected error for nonexistent deployment env")
	}
}

func TestPodStatusAndPodName(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Minute))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valheim-01-abc",
			Namespace: "valheim",
			Labels:    map[string]string{"app": "valheim"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "valheim"},
				{Name: "istio-proxy"},
			},
		},
		Status: corev1.PodStatus{
			Phase:     corev1.PodRunning,
			StartTime: &now,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "init-mods",
					RestartCount: 1,
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode:   1,
							Reason:     "Error",
							FinishedAt: now,
						},
					},
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "valheim",
					RestartCount: 2,
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode:   137,
							Reason:     "OOMKilled",
							FinishedAt: later,
						},
					},
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
				{
					Name:         "istio-proxy",
					RestartCount: 5,
				},
			},
		},
	}

	cs := fake.NewSimpleClientset(pod)
	c := NewWithClientset(cs, "valheim", "valheim")
	ctx := context.Background()

	// podName
	name, err := c.podName(ctx)
	if err != nil || name != "valheim-01-abc" {
		t.Fatalf("podName failed: name=%s err=%v", name, err)
	}

	// PodStatus
	ps, err := c.PodStatus(ctx)
	if err != nil {
		t.Fatalf("PodStatus failed: %v", err)
	}
	if ps.Phase != "Running" || !ps.Ready {
		t.Errorf("ps status wrong: Phase=%s Ready=%v", ps.Phase, ps.Ready)
	}
	if ps.RestartCount != 2 {
		t.Errorf("ps RestartCount = %d, want 2 (ignoring istio-proxy)", ps.RestartCount)
	}
	if ps.InitRestartCount != 1 || ps.InitStep != "init-mods" {
		t.Errorf("ps Init wrong: count=%d step=%s", ps.InitRestartCount, ps.InitStep)
	}
	if !ps.LastOOMKilled || ps.LastExitCode != 137 {
		t.Errorf("ps LastTerm wrong: oom=%v exit=%d", ps.LastOOMKilled, ps.LastExitCode)
	}

	// Missing deployment pod status
	downPS, err := c.DeploymentPodStatus(ctx, "nonexistent")
	if downPS.Phase != "Down" {
		t.Errorf("expected Down phase for nonexistent pod, got %s", downPS.Phase)
	}
}

func TestPodNameFallbackRoles(t *testing.T) {
	// 1. Fallback role=valheim-instance
	vhPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valheim-role-pod",
			Namespace: "valheim",
			Labels:    map[string]string{"role": "valheim-instance"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	csVh := fake.NewSimpleClientset(vhPod)
	cVh := NewWithClientset(csVh, "valheim", "valheim-other")
	nameVh, err := cVh.podName(context.Background())
	if err != nil || nameVh != "valheim-role-pod" {
		t.Fatalf("valheim fallback role failed: name=%s err=%v", nameVh, err)
	}

	// 2. Fallback role=mc-instance
	mcPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mc-role-pod",
			Namespace: "minecraft-modded",
			Labels:    map[string]string{"role": "mc-instance"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	csMC := fake.NewSimpleClientset(mcPod)
	cMC := NewWithClientset(csMC, "minecraft-modded", "mc-other")
	nameMC, err := cMC.podName(context.Background())
	if err != nil || nameMC != "mc-role-pod" {
		t.Fatalf("mc fallback role failed: name=%s err=%v", nameMC, err)
	}

	// 3. No pod found at all
	csEmpty := fake.NewSimpleClientset()
	cEmpty := NewWithClientset(csEmpty, "valheim", "valheim")
	_, err = cEmpty.podName(context.Background())
	if err == nil {
		t.Fatal("expected error when no pods exist")
	}
}

func TestActiveLoaderAndSwitch(t *testing.T) {
	one := int32(1)
	zero := int32(0)

	neoDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft-neoforge", Namespace: "minecraft-modded"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
	fabDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft-fabric", Namespace: "minecraft-modded"},
		Spec:       appsv1.DeploymentSpec{Replicas: &zero},
	}

	cs := fake.NewSimpleClientset(neoDep, fabDep)
	c := NewWithClientset(cs, "minecraft-modded", "minecraft-neoforge")
	ctx := context.Background()

	// NeoForge active
	loader, err := c.ActiveLoader(ctx, "", "")
	if err != nil || loader != "neoforge" {
		t.Fatalf("expected neoforge active, got %s err=%v", loader, err)
	}

	// Switch to fabric
	if err := c.SwitchLoader(ctx, "fabric", "", ""); err != nil {
		t.Fatalf("SwitchLoader to fabric failed: %v", err)
	}

	// Switch to neoforge
	if err := c.SwitchLoader(ctx, "neoforge", "", ""); err != nil {
		t.Fatalf("SwitchLoader to neoforge failed: %v", err)
	}

	// Invalid target
	if err := c.SwitchLoader(ctx, "invalid-loader", "", ""); err == nil {
		t.Fatal("expected error for invalid loader")
	}
}

func TestConfigMapData(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim-config", Namespace: "valheim"},
		Data:       map[string]string{"foo": "bar", "hello": "world"},
	}
	cs := fake.NewSimpleClientset(cm)
	c := NewWithClientset(cs, "valheim", "valheim")
	ctx := context.Background()

	data, err := c.ConfigMapData(ctx, "valheim-config")
	if err != nil || data["foo"] != "bar" {
		t.Fatalf("ConfigMapData failed: data=%v err=%v", data, err)
	}

	_, err = c.ConfigMapData(ctx, "nonexistent-cm")
	if err == nil {
		t.Fatal("expected error for nonexistent configmap")
	}
}

func TestServiceIPHostname(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-host", Namespace: "valheim"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{Hostname: "valheim.example.com"},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(svc)
	c := NewWithClientset(cs, "valheim", "valheim")

	ip, err := c.ServiceIP(context.Background(), "svc-host")
	if err != nil || ip != "valheim.example.com" {
		t.Fatalf("expected valheim.example.com, got %s, err=%v", ip, err)
	}
}

func TestIsBlockedAndJobNodeSelector(t *testing.T) {
	c := &Client{}
	if c.jobNodeSelector() != nil {
		t.Error("expected nil node selector when key is empty")
	}

	csWaiting := corev1.ContainerStatus{
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
		},
	}
	if !isBlocked(csWaiting) {
		t.Error("expected isBlocked true for CrashLoopBackOff")
	}

	csInit := corev1.ContainerStatus{
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"},
		},
	}
	if isBlocked(csInit) {
		t.Error("expected isBlocked false for PodInitializing")
	}

	csRunning := corev1.ContainerStatus{
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		},
	}
	if isBlocked(csRunning) {
		t.Error("expected isBlocked false when waiting is nil")
	}
}

func TestKube_New_OutsideCluster(t *testing.T) {
	_, err := New("default", "valheim")
	if err == nil {
		t.Error("expected error when calling New outside cluster")
	}
}

func TestActiveLoader_Variations(t *testing.T) {
	ctx := context.Background()
	zero := int32(0)
	one := int32(1)

	// 1. Fabric replicas = 1
	fabDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft-fabric", Namespace: "mc"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
	neoDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft-neoforge", Namespace: "mc"},
		Spec:       appsv1.DeploymentSpec{Replicas: &zero},
	}
	cs := fake.NewSimpleClientset(fabDep, neoDep)
	c := NewWithClientset(cs, "mc", "minecraft-fabric")
	loader, err := c.ActiveLoader(ctx, "", "")
	if err != nil || loader != "fabric" {
		t.Errorf("ActiveLoader want fabric, got %s, %v", loader, err)
	}

	// 2. NeoForge replicas = 1
	fabDep.Spec.Replicas = &zero
	neoDep.Spec.Replicas = &one
	cs2 := fake.NewSimpleClientset(fabDep, neoDep)
	c2 := NewWithClientset(cs2, "mc", "minecraft-neoforge")
	loader, err = c2.ActiveLoader(ctx, "", "")
	if err != nil || loader != "neoforge" {
		t.Errorf("ActiveLoader want neoforge, got %s, %v", loader, err)
	}

	// 3. Fabric ready replicas = 1 (desired = 0)
	fabDep.Spec.Replicas = &zero
	neoDep.Spec.Replicas = &zero
	fabDep.Status.ReadyReplicas = 1
	cs3 := fake.NewSimpleClientset(fabDep, neoDep)
	c3 := NewWithClientset(cs3, "mc", "minecraft-fabric")
	loader, err = c3.ActiveLoader(ctx, "", "")
	if err != nil || loader != "fabric" {
		t.Errorf("ActiveLoader want fabric for readyReplicas, got %s, %v", loader, err)
	}

	// 4. Default when both 0
	fabDep.Status.ReadyReplicas = 0
	cs4 := fake.NewSimpleClientset(fabDep, neoDep)
	c4 := NewWithClientset(cs4, "mc", "minecraft-fabric")
	loader, err = c4.ActiveLoader(ctx, "", "")
	if err != nil || loader != "neoforge" {
		t.Errorf("ActiveLoader want default neoforge, got %s, %v", loader, err)
	}
}

func TestPodName_And_Logs(t *testing.T) {
	ctx := context.Background()

	// 1. Pod with label app=dep
	podApp := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valheim-pod-1",
			Namespace: "valheim",
			Labels:    map[string]string{"app": "valheim"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "valheim"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	cs := fake.NewSimpleClientset(podApp)
	c := NewWithClientset(cs, "valheim", "valheim")
	name, err := c.podName(ctx)
	if err != nil || name != "valheim-pod-1" {
		t.Fatalf("podName app match failed: %s, %v", name, err)
	}

	// StreamDeploymentLogs on existing pod
	_, _ = c.StreamDeploymentLogs(ctx, "valheim", 100)
	_, _ = c.StreamLogs(ctx, 100)

	// 2. Pod with role=mc-instance running fallback
	podRole := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mc-pod-role",
			Namespace: "minecraft",
			Labels:    map[string]string{"role": "mc-instance"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	cs2 := fake.NewSimpleClientset(podRole)
	c2 := NewWithClientset(cs2, "minecraft", "mc-missing-app")
	name2, err := c2.podName(ctx)
	if err != nil || name2 != "mc-pod-role" {
		t.Fatalf("podName role match failed: %s, %v", name2, err)
	}

	// 3. No pods found
	csEmpty := fake.NewSimpleClientset()
	cEmpty := NewWithClientset(csEmpty, "minecraft", "empty")
	_, err = cEmpty.podName(ctx)
	if err == nil {
		t.Error("expected error from podName when no pods exist")
	}
	_, err = cEmpty.StreamDeploymentLogsQuery(ctx, "empty", LogQuery{Tail: 50})
	if err == nil {
		t.Error("expected error from StreamDeploymentLogsQuery when no pods exist")
	}
}

func TestPodMetrics(t *testing.T) {
	ctx := context.Background()

	// 1. Pod not found error
	csEmpty := fake.NewSimpleClientset()
	cEmpty := NewWithClientset(csEmpty, "valheim", "valheim")
	_, _, err := cEmpty.PodMetrics(ctx)
	if err == nil {
		t.Error("expected error when no pods exist")
	}

	// Setup client with running pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valheim-pod",
			Namespace: "valheim",
			Labels:    map[string]string{"app": "valheim"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	c := NewWithClientset(fake.NewSimpleClientset(pod), "valheim", "valheim")

	origFetcher := podMetricsFetcher
	defer func() { podMetricsFetcher = origFetcher }()

	// 2. Fetcher returns error
	podMetricsFetcher = func(ctx context.Context, c *Client, podName string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	_, _, err = c.PodMetrics(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	// 3. Malformed JSON
	podMetricsFetcher = func(ctx context.Context, c *Client, podName string) ([]byte, error) {
		return []byte("not valid json"), nil
	}
	_, _, err = c.PodMetrics(ctx)
	if err == nil {
		t.Error("expected error for malformed json")
	}

	// 4. Success parsing quantities
	podMetricsFetcher = func(ctx context.Context, c *Client, podName string) ([]byte, error) {
		return []byte(`{
			"containers": [
				{
					"usage": {
						"cpu": "350m",
						"memory": "1048576000"
					}
				}
			]
		}`), nil
	}
	cpu, mem, err := c.PodMetrics(ctx)
	if err != nil {
		t.Fatalf("PodMetrics failed: %v", err)
	}
	if cpu != 350 {
		t.Errorf("expected 350m cpu, got %d", cpu)
	}
	if mem != 1000 {
		t.Errorf("expected 1000 MiB memory, got %d", mem)
	}
}

func TestClient_EdgeCases(t *testing.T) {
	ctx := context.Background()

	// 1. New fails outside cluster
	_, err := New("default", "valheim")
	if err == nil {
		t.Error("expected error from New when running outside cluster")
	}

	// 2. RestartDeployment with empty depName uses active deployment
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim", Namespace: "valheim"},
	}
	cs := fake.NewSimpleClientset(dep)
	c := NewWithClientset(cs, "valheim", "valheim")
	if err := c.RestartDeployment(ctx, ""); err != nil {
		t.Errorf("RestartDeployment('') failed: %v", err)
	}

	// 3. DeploymentReplicas with nil Spec.Replicas
	depNil := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "nil-rep", Namespace: "valheim"},
		Spec:       appsv1.DeploymentSpec{Replicas: nil},
	}
	cNil := NewWithClientset(fake.NewSimpleClientset(depNil), "valheim", "nil-rep")
	des, ready, err := cNil.DeploymentReplicas(ctx, "nil-rep")
	if err != nil || des != 0 || ready != 0 {
		t.Errorf("expected 0 replicas, got des=%d ready=%d err=%v", des, ready, err)
	}

	// 4. StreamLogs fails when podName fails
	csEmpty := fake.NewSimpleClientset()
	cEmpty := NewWithClientset(csEmpty, "valheim", "empty")
	if _, err := cEmpty.StreamLogs(ctx, 10); err == nil {
		t.Error("expected error from StreamLogs when no pods exist")
	}

	// 5. StreamDeploymentLogs wrapper
	podCustom := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-pod",
			Namespace: "valheim",
			Labels:    map[string]string{"app": "custom"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "other-container"},
			},
		},
	}
	cCustom := NewWithClientset(fake.NewSimpleClientset(podCustom), "valheim", "custom")
	// fake clientset GetLogs Stream will succeed or return reader
	_, _ = cCustom.StreamDeploymentLogs(ctx, "custom", 10)

	// 6. fetchServiceIP with hostname, empty ingress, and not found
	svcHost := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-host", Namespace: "valheim"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{Hostname: "lb.example.com"},
				},
			},
		},
	}
	svcEmpty := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-empty", Namespace: "valheim"},
	}
	cSvc := NewWithClientset(fake.NewSimpleClientset(svcHost, svcEmpty), "valheim", "valheim")

	ip, err := cSvc.fetchServiceIP(ctx, "svc-host")
	if err != nil || ip != "lb.example.com" {
		t.Errorf("fetchServiceIP hostname want 'lb.example.com', got %q, err=%v", ip, err)
	}
	ip, err = cSvc.fetchServiceIP(ctx, "svc-empty")
	if err != nil || ip != "" {
		t.Errorf("fetchServiceIP empty want '', got %q, err=%v", ip, err)
	}
	_, err = cSvc.fetchServiceIP(ctx, "svc-nonexistent")
	if err == nil {
		t.Error("fetchServiceIP expected error for nonexistent service")
	}

	// 7. podName with running pod vs pending pod
	podPending := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-valheim",
			Namespace: "valheim",
			Labels:    map[string]string{"role": "valheim-instance"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	cPending := NewWithClientset(fake.NewSimpleClientset(podPending), "valheim", "valheim-none")
	pName, err := cPending.podName(ctx)
	if err != nil || pName != "pending-valheim" {
		t.Errorf("podName pending fallback want 'pending-valheim', got %q, err=%v", pName, err)
	}
}


