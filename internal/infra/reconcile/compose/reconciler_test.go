package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
	"gopkg.in/yaml.v3"
)

func TestComposeReconciler_Async(t *testing.T) {
	r := New()
	if r.Async() != false {
		t.Errorf("Async() = true, want false (compose convergence is synchronous)")
	}
}

func TestComposeReconciler_Converge(t *testing.T) {
	ctx := context.Background()
	var executedDir string
	var executedArgs []string

	mockExec := func(ctx context.Context, workDir string, args ...string) error {
		executedDir = workDir
		executedArgs = args
		return nil
	}

	tempDir := t.TempDir()
	r := New(WithWorkDir(tempDir), WithExecutor(mockExec))

	ref := ports.ServerRef{Name: "mc-survival"}
	if err := r.Converge(ctx, ref); err != nil {
		t.Fatalf("Converge failed: %v", err)
	}

	expectedDir := filepath.Join(tempDir, "mc-survival")
	if executedDir != expectedDir {
		t.Errorf("executedDir = %q, want %q", executedDir, expectedDir)
	}

	expectedArgs := []string{"up", "-d", "--remove-orphans"}
	if len(executedArgs) != len(expectedArgs) {
		t.Fatalf("executedArgs = %v, want %v", executedArgs, expectedArgs)
	}
	for i := range expectedArgs {
		if executedArgs[i] != expectedArgs[i] {
			t.Errorf("arg[%d] = %q, want %q", i, executedArgs[i], expectedArgs[i])
		}
	}
}

func TestComposeReconciler_ConvergeError(t *testing.T) {
	ctx := context.Background()
	mockExec := func(ctx context.Context, workDir string, args ...string) error {
		return errors.New("docker daemon not running")
	}

	r := New(WithExecutor(mockExec))
	err := r.Converge(ctx, ports.ServerRef{Name: "srv"})
	if err == nil || !strings.Contains(err.Error(), "docker daemon not running") {
		t.Errorf("expected daemon error, got: %v", err)
	}
}

func TestRenderCompose(t *testing.T) {
	spec := domain.RuntimeSpec{
		Image: "itzg/minecraft-server:java25",
		Ports: []domain.PortSpec{
			{Name: "game", Port: 25565, Protocol: "TCP"},
			{Name: "rcon", Port: 25575, Protocol: "TCP"},
		},
		Volumes: []domain.VolumeSpec{
			{Name: "data", MountPath: "/data", ReadOnly: false},
		},
		Env: map[string]string{
			"EULA": "TRUE",
			"TYPE": "FABRIC",
		},
	}

	out, err := RenderCompose("mc-test", spec)
	if err != nil {
		t.Fatalf("RenderCompose failed: %v", err)
	}

	var parsed struct {
		Services map[string]struct {
			ContainerName string            `yaml:"container_name"`
			Image         string            `yaml:"image"`
			Restart       string            `yaml:"restart"`
			Ports         []string          `yaml:"ports"`
			Volumes       []string          `yaml:"volumes"`
			Environment   map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}

	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal rendered compose yaml: %v", err)
	}

	svc, ok := parsed.Services["mc-test"]
	if !ok {
		t.Fatalf("service 'mc-test' not found in yaml")
	}

	if svc.ContainerName != "mc-test" {
		t.Errorf("ContainerName = %q, want 'mc-test'", svc.ContainerName)
	}
	if svc.Image != "itzg/minecraft-server:java25" {
		t.Errorf("Image = %q, want 'itzg/minecraft-server:java25'", svc.Image)
	}
	if svc.Environment["EULA"] != "TRUE" || svc.Environment["TYPE"] != "FABRIC" {
		t.Errorf("Environment mismatch: %+v", svc.Environment)
	}
	if len(svc.Ports) != 2 || svc.Ports[0] != "25565:25565/tcp" {
		t.Errorf("Ports mismatch: %+v", svc.Ports)
	}
	if len(svc.Volumes) != 1 || svc.Volumes[0] != "./data:/data" {
		t.Errorf("Volumes mismatch: %+v", svc.Volumes)
	}
}

func TestComposeReconciler_WriteAndConverge(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	var executedDir string
	mockExec := func(ctx context.Context, workDir string, args ...string) error {
		executedDir = workDir
		return nil
	}

	r := New(WithWorkDir(tempDir), WithExecutor(mockExec))
	ref := ports.ServerRef{Name: "valheim-01"}
	spec := domain.RuntimeSpec{
		Image: "lloesche/valheim-server:latest",
		Ports: []domain.PortSpec{
			{Name: "game", Port: 2456, Protocol: "UDP"},
		},
		Volumes: []domain.VolumeSpec{
			{Name: "config", MountPath: "/config"},
		},
	}

	if err := r.WriteAndConverge(ctx, ref, spec); err != nil {
		t.Fatalf("WriteAndConverge failed: %v", err)
	}

	// Verify file was written
	writtenFile := filepath.Join(tempDir, "valheim-01", "docker-compose.yml")
	content, err := os.ReadFile(writtenFile)
	if err != nil {
		t.Fatalf("read written compose file: %v", err)
	}
	if !strings.Contains(string(content), "lloesche/valheim-server:latest") {
		t.Errorf("compose file does not contain image: %s", string(content))
	}

	expectedDir := filepath.Join(tempDir, "valheim-01")
	if executedDir != expectedDir {
		t.Errorf("executedDir = %q, want %q", executedDir, expectedDir)
	}
}

func TestRenderComposeEmitsAHealthcheck(t *testing.T) {
	out, err := RenderCompose("valheim-boppo-02", domain.RuntimeSpec{
		Image:       "lloesche/valheim-server:latest",
		Ports:       []domain.PortSpec{{Name: "game", Port: 2456, Protocol: "UDP"}},
		Env:         map[string]string{"SERVER_NAME": "boppo"},
		HealthProbe: `ss -lun | grep -qE ':2456[[:space:]]'`,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	if !strings.Contains(body, "healthcheck:") {
		t.Fatalf("without a healthcheck, Available degrades to 'process is running':\n%s", body)
	}
	if !strings.Contains(body, "CMD-SHELL") || !strings.Contains(body, "2456") {
		t.Errorf("healthcheck must carry the game's declared probe:\n%s", body)
	}
}

func TestRenderComposeOmitsHealthcheckWhenNoProbe(t *testing.T) {
	out, err := RenderCompose("x", domain.RuntimeSpec{Image: "busybox"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "healthcheck:") {
		t.Error("a game with no declared probe must not get an empty healthcheck")
	}
}
