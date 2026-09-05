package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podName finds the active pod by the app label.
func (c *Client) podName(ctx context.Context) (string, error) {
	dep := c.activeDeploymentName(ctx)
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + dep,
	})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pod for app=%s in %s", dep, c.namespace)
	}
	return pods.Items[0].Name, nil
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

// PodStatus is a snapshot for the dashboard tiles.
type PodStatus struct {
	Phase     string
	Ready     bool
	StartedAt time.Time
}

func (c *Client) PodStatus(ctx context.Context) (PodStatus, error) {
	dep := c.activeDeploymentName(ctx)
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + dep,
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
	return ps, nil
}
