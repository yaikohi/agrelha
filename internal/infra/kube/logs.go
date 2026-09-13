package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podName finds the active pod by the app label or instance role.
func (c *Client) podName(ctx context.Context) (string, error) {
	dep := c.activeDeploymentName(ctx)
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + dep,
	})
	if err == nil && len(pods.Items) > 0 {
		return pods.Items[0].Name, nil
	}
	role := "valheim-instance"
	if c.namespace != "valheim" {
		role = "mc-instance"
	}
	if rolePods, rErr := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "role=" + role,
	}); rErr == nil && len(rolePods.Items) > 0 {
		for _, p := range rolePods.Items {
			if p.Status.Phase == corev1.PodRunning {
				return p.Name, nil
			}
		}
		return rolePods.Items[0].Name, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("no pod for app=%s in %s", dep, c.namespace)
}

// StreamLogs follows the pod's logs, starting with the last `tail` lines.
// Caller must Close the reader (or cancel ctx) to stop.
func (c *Client) StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error) {
	name, err := c.podName(ctx)
	if err != nil {
		return nil, err
	}
	req := c.cs.CoreV1().Pods(c.namespace).GetLogs(name, &corev1.PodLogOptions{
		Follow:    true,
		TailLines: &tail,
	})
	return req.Stream(ctx)
}

// StreamDeploymentLogs follows logs for any deployment by app label.
// LogQuery selects which log stream to read for a deployment's pod.
type LogQuery struct {
	Tail     int64
	Follow   bool
	Previous bool
}

func (c *Client) StreamDeploymentLogs(ctx context.Context, depName string, tail int64) (io.ReadCloser, error) {
	return c.StreamDeploymentLogsQuery(ctx, depName, LogQuery{Tail: tail, Follow: true})
}

// StreamDeploymentLogsQuery reads a deployment pod's logs. With Previous set it
// reads the terminated container instead of the running one, which is the only
// place a crash reason survives.
func (c *Client) StreamDeploymentLogsQuery(ctx context.Context, depName string, q LogQuery) (io.ReadCloser, error) {
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + depName,
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pod found for app=%s in %s", depName, c.namespace)
	}
	pod := pods.Items[0]
	container := ""
	for _, cnt := range pod.Spec.Containers {
		if cnt.Name == "valheim" || cnt.Name == "minecraft" {
			container = cnt.Name
			break
		}
	}
	if container == "" && len(pod.Spec.Containers) > 0 {
		container = pod.Spec.Containers[0].Name
	}
	tail := q.Tail
	logOpts := &corev1.PodLogOptions{
		Follow:    q.Follow,
		Previous:  q.Previous,
		TailLines: &tail,
	}
	if container != "" {
		logOpts.Container = container
	}
	req := c.cs.CoreV1().Pods(c.namespace).GetLogs(pod.Name, logOpts)
	return req.Stream(ctx)
}

// PodStatus is a snapshot for the dashboard tiles.
type PodStatus struct {
	Phase     string
	Ready     bool
	StartedAt time.Time

	RestartCount  int32
	WaitingReason string

	LastExitCode   int32
	LastReason     string
	LastFinishedAt time.Time
	LastOOMKilled  bool
}

func (c *Client) PodStatus(ctx context.Context) (PodStatus, error) {
	return c.DeploymentPodStatus(ctx, c.activeDeploymentName(ctx))
}

// DeploymentPodStatus retrieves pod status for a specific deployment.
func (c *Client) DeploymentPodStatus(ctx context.Context, depName string) (PodStatus, error) {
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + depName,
	})
	if err != nil || len(pods.Items) == 0 {
		return PodStatus{Phase: "Down"}, err
	}
	p := pods.Items[0]
	ps := PodStatus{Phase: string(p.Status.Phase)}
	if p.Status.StartTime != nil {
		ps.StartedAt = p.Status.StartTime.Time
	}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady {
			ps.Ready = cond.Status == corev1.ConditionTrue
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Name == "istio-proxy" {
			continue
		}
		ps.RestartCount += cs.RestartCount
		if w := cs.State.Waiting; w != nil && ps.WaitingReason == "" {
			ps.WaitingReason = w.Reason
		}
		if t := cs.LastTerminationState.Terminated; t != nil && t.FinishedAt.Time.After(ps.LastFinishedAt) {
			ps.LastExitCode = t.ExitCode
			ps.LastReason = t.Reason
			ps.LastFinishedAt = t.FinishedAt.Time
			ps.LastOOMKilled = t.Reason == "OOMKilled"
		}
	}
	return ps, nil
}

// ServiceIP returns the address a LoadBalancer Service actually holds. It is
// empty while the allocation is pending, which is a real state and not an error.
func (c *Client) ServiceIP(ctx context.Context, name string) (string, error) {
	if ip, err, ok := c.cachedServiceIP(name); ok {
		return ip, err
	}

	ip, err := c.fetchServiceIP(ctx, name)
	c.storeServiceIP(name, ip, err)
	return ip, err
}

func (c *Client) cachedServiceIP(name string) (string, error, bool) {
	c.svcMu.Lock()
	defer c.svcMu.Unlock()
	e, ok := c.svcCache[name]
	if !ok || time.Since(e.at) > svcIPTTL {
		return "", nil, false
	}
	return e.ip, e.err, true
}

func (c *Client) storeServiceIP(name, ip string, err error) {
	c.svcMu.Lock()
	defer c.svcMu.Unlock()
	if c.svcCache == nil {
		c.svcCache = make(map[string]svcIPEntry)
	}
	c.svcCache[name] = svcIPEntry{ip: ip, err: err, at: time.Now()}
}

func (c *Client) fetchServiceIP(ctx context.Context, name string) (string, error) {
	svc, err := c.cs.CoreV1().Services(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP != "" {
			return ing.IP, nil
		}
		if ing.Hostname != "" {
			return ing.Hostname, nil
		}
	}
	return "", nil
}

// DeploymentEnv reads one environment variable from a deployment's first
// container. It reports whether the variable is set at all, which is different
// from it being set to the empty string.
func (c *Client) DeploymentEnv(ctx context.Context, depName, key string) (string, bool, error) {
	dep, err := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, depName, metav1.GetOptions{})
	if err != nil {
		return "", false, err
	}
	for _, container := range dep.Spec.Template.Spec.Containers {
		for _, e := range container.Env {
			if e.Name == key {
				return e.Value, true, nil
			}
		}
	}
	return "", false, nil
}
