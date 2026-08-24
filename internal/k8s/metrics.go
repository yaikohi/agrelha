package k8s

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/api/resource"
)

// PodMetrics reads live CPU/memory usage for the valheim pod from the
// metrics.k8s.io API (metrics-server). Returns summed-container CPU millicores
// and memory MiB. Done via a raw REST call to avoid the k8s.io/metrics dep.
func (c *Client) PodMetrics(ctx context.Context) (cpuMilli, memMiB int64, err error) {
	name, err := c.podName(ctx)
	if err != nil {
		return 0, 0, err
	}
	raw, err := c.cs.CoreV1().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", c.namespace, "pods", name).
		DoRaw(ctx)
	if err != nil {
		return 0, 0, err
	}
	var pm struct {
		Containers []struct {
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(raw, &pm); err != nil {
		return 0, 0, err
	}
	for _, cnt := range pm.Containers {
		if q, e := resource.ParseQuantity(cnt.Usage.CPU); e == nil {
			cpuMilli += q.MilliValue()
		}
		if q, e := resource.ParseQuantity(cnt.Usage.Memory); e == nil {
			memMiB += q.Value() / (1024 * 1024)
		}
	}
	return cpuMilli, memMiB, nil
}
