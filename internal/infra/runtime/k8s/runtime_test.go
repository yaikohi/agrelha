package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	k8sclient "agrelha/internal/infra/kube"
	"agrelha/internal/ports"
)

func TestNilReceiverAndNilClient(t *testing.T) {
	ctx := context.Background()
	ref := ports.ServerRef{Name: "server-01", Scope: "test-ns"}

	check := func(name string, r *Runtime) {
		t.Run(name, func(t *testing.T) {
			if err := r.Start(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("expected ErrNotImplemented for Start, got %v", err)
			}
			if err := r.Stop(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("expected ErrNotImplemented for Stop, got %v", err)
			}
			if err := r.Restart(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("expected ErrNotImplemented for Restart, got %v", err)
			}
			st, err := r.Status(ctx, ref)
			if err != ports.ErrNotImplemented || st.Lifecycle != ports.LifecycleUnknown {
				t.Fatalf("expected ErrNotImplemented & LifecycleUnknown for Status, got st=%+v, err=%v", st, err)
			}
			if _, err := r.Metrics(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("expected ErrNotImplemented for Metrics, got %v", err)
			}
			if _, err := r.Logs(ctx, ref, ports.LogOptions{}); err != ports.ErrNotImplemented {
				t.Fatalf("expected ErrNotImplemented for Logs, got %v", err)
			}
			if err := r.WatchAvailability(ctx, ref, time.Millisecond); err != ports.ErrNotImplemented {
				t.Fatalf("expected ErrNotImplemented for WatchAvailability, got %v", err)
			}
		})
	}

	check("nil_receiver", nil)
	check("nil_client", New(nil))
}

func TestStartStopRestart(t *testing.T) {
	ctx := context.Background()
	ns := "minecraft-modded"
	depName := "mc-world-01"
	zero := int32(0)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &zero,
		},
	}

	cs := fake.NewSimpleClientset(dep)
	client := k8sclient.NewWithClientset(cs, ns, depName)
	rt := New(client)
	ref := ports.ServerRef{Name: depName, Scope: ns}

	// 1. Start scales deployment to 1
	if err := rt.Start(ctx, ref); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	d, err := cs.AppsV1().Deployments(ns).Get(ctx, depName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 1 {
		t.Fatalf("expected replicas = 1 after Start, got %v", d.Spec.Replicas)
	}

	// 2. Stop scales deployment to 0
	if err := rt.Stop(ctx, ref); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	d, err = cs.AppsV1().Deployments(ns).Get(ctx, depName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 0 {
		t.Fatalf("expected replicas = 0 after Stop, got %v", d.Spec.Replicas)
	}

	// 3. Restart updates restartedAt annotation on deployment template
	if err := rt.Restart(ctx, ref); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}
	d, err = cs.AppsV1().Deployments(ns).Get(ctx, depName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}
	ann := d.Spec.Template.Annotations["agrelha.dev/restartedAt"]
	if ann == "" {
		t.Fatalf("expected agrelha.dev/restartedAt annotation to be set after Restart, got %+v", d.Spec.Template.Annotations)
	}
}

func TestStatusDerivation(t *testing.T) {
	ctx := context.Background()
	ns := "minecraft-modded"
	depName := "mc-world-01"
	ref := ports.ServerRef{Name: depName, Scope: ns}

	t.Run("desired_zero_even_with_running_phase_pod_is_stopped", func(t *testing.T) {
		// Case: Replicas = 0, but a Pod object exists with Phase="Running" (e.g. terminating).
		// Status must report LifecycleStopped because desired replicas is 0!
		zero := int32(0)
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: depName, Namespace: ns},
			Spec:       appsv1.DeploymentSpec{Replicas: &zero},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mc-world-01-pod",
				Namespace: ns,
				Labels:    map[string]string{"app": depName},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionFalse},
				},
			},
		}

		cs := fake.NewSimpleClientset(dep, pod)
		client := k8sclient.NewWithClientset(cs, ns, depName)
		rt := New(client)

		st, err := rt.Status(ctx, ref)
		if err != nil {
			t.Fatalf("Status error: %v", err)
		}
		if st.Lifecycle != ports.LifecycleStopped {
			t.Fatalf("expected LifecycleStopped when desired=0, got %s", st.Lifecycle)
		}
		if st.Available {
			t.Fatalf("expected Available=false, got true")
		}
	})

	t.Run("desired_one_not_ready_is_running_lifecycle_unavailable", func(t *testing.T) {
		one := int32(1)
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: depName, Namespace: ns},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mc-world-01-pod",
				Namespace: ns,
				Labels:    map[string]string{"app": depName},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionFalse},
				},
			},
		}

		cs := fake.NewSimpleClientset(dep, pod)
		client := k8sclient.NewWithClientset(cs, ns, depName)
		rt := New(client)

		st, err := rt.Status(ctx, ref)
		if err != nil {
			t.Fatalf("Status error: %v", err)
		}
		if st.Lifecycle != ports.LifecycleRunning {
			t.Fatalf("expected LifecycleRunning when desired=1, got %s", st.Lifecycle)
		}
		if st.Available {
			t.Fatalf("expected Available=false while pod is pending/not ready, got true")
		}
	})

	t.Run("desired_one_pod_ready_is_running_and_available", func(t *testing.T) {
		one := int32(1)
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: depName, Namespace: ns},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		}
		startTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mc-world-01-pod",
				Namespace: ns,
				Labels:    map[string]string{"app": depName},
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				StartTime: &startTime,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}

		cs := fake.NewSimpleClientset(dep, pod)
		client := k8sclient.NewWithClientset(cs, ns, depName)
		rt := New(client)

		st, err := rt.Status(ctx, ref)
		if err != nil {
			t.Fatalf("Status error: %v", err)
		}
		if st.Lifecycle != ports.LifecycleRunning {
			t.Fatalf("expected LifecycleRunning, got %s", st.Lifecycle)
		}
		if !st.Available {
			t.Fatalf("expected Available=true when pod has Ready condition True, got false")
		}
		if st.StartedAt.IsZero() {
			t.Fatalf("expected StartedAt to be populated from Pod StartTime")
		}
	})

	t.Run("nonexistent_deployment_returns_unknown_and_error", func(t *testing.T) {
		cs := fake.NewSimpleClientset()
		client := k8sclient.NewWithClientset(cs, ns, depName)
		rt := New(client)

		st, err := rt.Status(ctx, ref)
		if err == nil {
			t.Fatalf("expected error for nonexistent deployment, got nil")
		}
		if st.Lifecycle != ports.LifecycleUnknown {
			t.Fatalf("expected LifecycleUnknown for nonexistent deployment, got %s", st.Lifecycle)
		}
	})
}

func TestLogsAndMetrics(t *testing.T) {
	ctx := context.Background()
	ns := "minecraft-modded"
	depName := "mc-world-01"
	ref := ports.ServerRef{Name: depName, Scope: ns}

	cs := fake.NewSimpleClientset()
	client := k8sclient.NewWithClientset(cs, ns, depName)
	rt := New(client)

	// Metrics with fake clientset returns error (metrics client is not configured)
	_, err := rt.Metrics(ctx, ref)
	if err == nil {
		t.Fatalf("expected error when metrics client is not configured, got nil")
	}

	// Logs when no pod exists
	_, err = rt.Logs(ctx, ref, ports.LogOptions{Tail: 100})
	if err == nil || !strings.Contains(err.Error(), "no pod found") {
		t.Fatalf("expected 'no pod found' error, got %v", err)
	}

	// Logs with default tail (Tail <= 0)
	_, err = rt.Logs(ctx, ref, ports.LogOptions{Tail: 0})
	if err == nil || !strings.Contains(err.Error(), "no pod found") {
		t.Fatalf("expected 'no pod found' error with tail 0, got %v", err)
	}
}

func TestStatusWithServiceIP(t *testing.T) {
	ctx := context.Background()
	ns := "minecraft-modded"
	depName := "mc-world-01"
	ref := ports.ServerRef{Name: depName, Scope: ns}
	one := int32(1)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: depName, Namespace: ns},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: depName, Namespace: ns},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "192.168.20.225"},
				},
			},
		},
	}

	cs := fake.NewSimpleClientset(dep, svc)
	client := k8sclient.NewWithClientset(cs, ns, depName)
	rt := New(client)

	st, err := rt.Status(ctx, ref)
	if err != nil {
		t.Fatalf("Status error: %v", err)
	}
	if st.Address != "192.168.20.225" {
		t.Errorf("Address = %q, want '192.168.20.225'", st.Address)
	}
}

func TestK8sRuntime_WatchAvailability(t *testing.T) {
	ctx := context.Background()
	ns := "minecraft-modded"
	depName := "mc-world-01"
	ref := ports.ServerRef{Name: depName, Scope: ns}

	t.Run("timeout when unavailable", func(t *testing.T) {
		cs := fake.NewSimpleClientset()
		client := k8sclient.NewWithClientset(cs, ns, depName)
		rt := New(client)

		err := rt.WatchAvailability(ctx, ref, 10*time.Millisecond)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})

	t.Run("success when ready", func(t *testing.T) {
		one := int32(1)
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: depName, Namespace: ns},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		}
		startTime := metav1.NewTime(time.Now())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mc-world-01-pod",
				Namespace: ns,
				Labels:    map[string]string{"app": depName},
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				StartTime: &startTime,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}

		cs := fake.NewSimpleClientset(dep, pod)
		client := k8sclient.NewWithClientset(cs, ns, depName)
		rt := New(client)

		err := rt.WatchAvailability(ctx, ref, 2500*time.Millisecond)
		if err != nil {
			t.Fatalf("expected nil error on available deployment, got %v", err)
		}
	})
}

