package k8s

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ActiveLoader checks the deployments in namespace (e.g. "minecraft-neoforge" and "minecraft-fabric")
// and returns "neoforge" or "fabric" based on which deployment has desired/ready replicas > 0.
func (c *Client) ActiveLoader(ctx context.Context, neoforgeDep, fabricDep string) (string, error) {
	if neoforgeDep == "" {
		neoforgeDep = "minecraft-neoforge"
	}
	if fabricDep == "" {
		fabricDep = "minecraft-fabric"
	}

	fabD, errFab := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, fabricDep, metav1.GetOptions{})
	if errFab == nil && fabD.Spec.Replicas != nil && *fabD.Spec.Replicas > 0 {
		return "fabric", nil
	}

	neoD, errNeo := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, neoforgeDep, metav1.GetOptions{})
	if errNeo == nil && neoD.Spec.Replicas != nil && *neoD.Spec.Replicas > 0 {
		return "neoforge", nil
	}

	if errFab == nil && fabD.Status.ReadyReplicas > 0 {
		return "fabric", nil
	}

	return "neoforge", nil
}

// SwitchLoader scales the current active loader to 0 and scales the target loader to 1.
func (c *Client) SwitchLoader(ctx context.Context, target, neoforgeDep, fabricDep string) error {
	target = strings.ToLower(strings.TrimSpace(target))
	if target != "fabric" && target != "neoforge" {
		return fmt.Errorf("invalid target loader: %q (must be 'fabric' or 'neoforge')", target)
	}
	if neoforgeDep == "" {
		neoforgeDep = "minecraft-neoforge"
	}
	if fabricDep == "" {
		fabricDep = "minecraft-fabric"
	}

	if target == "fabric" {
		// Stop neoforge first to free RAM/CPU on node game-01
		_ = c.ScaleDeployment(ctx, neoforgeDep, 0)
		return c.ScaleDeployment(ctx, fabricDep, 1)
	}

	_ = c.ScaleDeployment(ctx, fabricDep, 0)
	return c.ScaleDeployment(ctx, neoforgeDep, 1)
}

// ScaleDeployment scales a specific deployment by name.
func (c *Client) ScaleDeployment(ctx context.Context, depName string, replicas int32) error {
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err := c.cs.AppsV1().Deployments(c.namespace).Patch(
		ctx, depName, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	return err
}

// Deployment reports the configured deployment name.
func (c *Client) Deployment() string {
	return c.deployment
}

// SetDeployment dynamically switches the deployment target for this client.
func (c *Client) SetDeployment(dep string) {
	c.deployment = dep
}
