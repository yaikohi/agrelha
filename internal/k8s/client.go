// Package k8s is agrelha's imperative plane: it acts on the valheim Deployment
// via an in-cluster ServiceAccount whose RBAC is scoped to the valheim namespace
// (pods+logs read, deployment patch/scale — no exec). See yaya-ops
// manifests/agrelha-rbac.yaml.
package k8s

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	cs            kubernetes.Interface
	namespace     string
	deployment    string
	altDeployment string
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

// Clientset returns the underlying kubernetes clientset.
func (c *Client) Clientset() kubernetes.Interface {
	return c.cs
}

// SetAltDeployment configures an alternate deployment (e.g. fabric vs neoforge) in the same namespace.
func (c *Client) SetAltDeployment(alt string) {
	c.altDeployment = alt
}

func (c *Client) activeDeploymentName(ctx context.Context) string {
	if c.altDeployment == "" {
		return c.deployment
	}
	if d, err := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, c.altDeployment, metav1.GetOptions{}); err == nil {
		if (d.Spec.Replicas != nil && *d.Spec.Replicas > 0) || d.Status.ReadyReplicas > 0 {
			return c.altDeployment
		}
	}
	return c.deployment
}

// Restart triggers a rolling restart of the active deployment.
func (c *Client) Restart(ctx context.Context) error {
	return c.RestartDeployment(ctx, c.activeDeploymentName(ctx))
}

// RestartDeployment triggers a rolling restart of a specific deployment by name.
func (c *Client) RestartDeployment(ctx context.Context, depName string) error {
	if depName == "" {
		depName = c.activeDeploymentName(ctx)
	}
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"agrelha.ykhi.xyz/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339),
	)
	_, err := c.cs.AppsV1().Deployments(c.namespace).Patch(
		ctx, depName, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	return err
}

// Scale sets the replica count of the active deployment.
func (c *Client) Scale(ctx context.Context, replicas int32) error {
	return c.ScaleDeployment(ctx, c.activeDeploymentName(ctx), replicas)
}

// Replicas reports desired/ready replica counts for the active deployment.
func (c *Client) Replicas(ctx context.Context) (desired, ready int32, err error) {
	return c.DeploymentReplicas(ctx, c.activeDeploymentName(ctx))
}

// DeploymentReplicas reports desired/ready replica counts for a specific deployment.
func (c *Client) DeploymentReplicas(ctx context.Context, depName string) (desired, ready int32, err error) {
	d, err := c.cs.AppsV1().Deployments(c.namespace).Get(ctx, depName, metav1.GetOptions{})
	if err != nil {
		return 0, 0, err
	}
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return desired, d.Status.ReadyReplicas, nil
}

// CreateBackupJob creates a one-off Kubernetes Job that mounts the instance PVC and archives /data to the backups PVC.
func (c *Client) CreateBackupJob(ctx context.Context, jobName, archiveName, dataClaimName, backupsClaimName string) error {
	ttl := int32(300)
	backoff := int32(1)
	readOnly := true

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: c.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "mc-backup",
				"app.kubernetes.io/component": "backup-job",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name": "mc-backup",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					NodeSelector: map[string]string{
						"ykhi.xyz/gameserver": "true",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "dedicated",
							Operator: corev1.TolerationOpEqual,
							Value:    "gameserver",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "backup",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf("tar -czf /backups/%s -C /data .", archiveName),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
									ReadOnly:  readOnly,
								},
								{
									Name:      "backups",
									MountPath: "/backups",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: dataClaimName,
								},
							},
						},
						{
							Name: "backups",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: backupsClaimName,
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := c.cs.BatchV1().Jobs(c.namespace).Create(ctx, job, metav1.CreateOptions{})
	return err
}
