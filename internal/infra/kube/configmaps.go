package k8s

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapData returns a ConfigMap's data map (read-only, for displaying the
// current mod/admin lists). Requires configmaps get in the valheim ns.
func (c *Client) ConfigMapData(ctx context.Context, name string) (map[string]string, error) {
	cm, err := c.cs.CoreV1().ConfigMaps(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm.Data, nil
}
