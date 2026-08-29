// Package k8s is agrelha's imperative plane: it acts on the valheim Deployment
// via an in-cluster ServiceAccount whose RBAC is scoped to the valheim namespace
// (pods+logs read, deployment patch/scale — no exec). See yaya-ops
// manifests/agrelha-rbac.yaml.
package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	cs         kubernetes.Interface
	namespace  string
	deployment string
}

func New(namespace, deployment string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("clientset: %w", err)
	}
	return &Client{cs: cs, namespace: namespace, deployment: deployment}, nil
}

func NewWithClientset(cs kubernetes.Interface, namespace, deployment string) *Client {
	return &Client{cs: cs, namespace: namespace, deployment: deployment}
}

// Restart triggers a rolling restart by stamping the pod template annotation,
// exactly like `kubectl rollout restart`.
func (c *Client) Restart(ctx context.Context) error {
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"agrelha.ykhi.xyz/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339),
	)
	_, err := c.cs.AppsV1().Deployments(c.namespace).Patch(
		ctx, c.deployment, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	return err
}

// Scale sets the replica count (Stop = 0, Start = 1).
func (c *Client) Scale(ctx context.Context, replicas int32) error {
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err := c.cs.AppsV1().Deployments(c.namespace).Patch(
		ctx, c.deployment, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	return err
}

// Replicas reports desired/ready replica counts for the status tiles.
func (c *Client) Replicas(ctx context.Context) (desired, ready int32, err error) {
	d, err := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, c.deployment, metav1.GetOptions{})
	if err != nil {
		return 0, 0, err
	}
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return desired, d.Status.ReadyReplicas, nil
}

// TODO(step③): StreamLogs — follow the valheim pod's logs (pods/log) and pipe
// lines to the SSE handler; the log-ingester goroutine also parses join events.
