package k8s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const miniKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: test
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: test
    context:
      cluster: test
      user: test
current-context: test
users:
  - name: test
    user:
      token: not-a-real-token
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(miniKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKubeconfigLetsAgrelhaRunOutsideTheCluster(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))

	cfg, err := restConfig()
	if err != nil {
		t.Fatalf("a valid KUBECONFIG must be usable outside the cluster: %v", err)
	}
	if cfg.Host != "https://127.0.0.1:6443" {
		t.Errorf("server = %q, want the one from the kubeconfig", cfg.Host)
	}
}

func TestNoCredentialsAtAllIsAnError(t *testing.T) {
	t.Setenv("KUBECONFIG", "")

	if _, err := restConfig(); err == nil {
		t.Fatal("with no KUBECONFIG and no in-cluster credentials, this must fail rather than build an unusable client")
	}
}

func TestAnUnreadableKubeconfigNamesThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("KUBECONFIG", missing)

	_, err := restConfig()
	if err == nil {
		t.Fatal("a KUBECONFIG that cannot be read must not fall back to in-cluster credentials")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error should name the path it tried, got %v", err)
	}
}
