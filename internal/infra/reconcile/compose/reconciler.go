// Package compose implements ports.Reconciler on top of Docker Compose.
// It provides synchronous convergence: rendering compose projects and running `compose up`.
package compose

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
	"gopkg.in/yaml.v3"
)

// Executor executes a docker compose command in workDir with the given arguments.
type Executor func(ctx context.Context, workDir string, args ...string) error

// Reconciler converges desired state to reality synchronously using Docker Compose.
type Reconciler struct {
	workDir  string
	executor Executor
}

// Option configures a Reconciler.
type Option func(*Reconciler)

// WithWorkDir sets the base working directory for compose projects.
func WithWorkDir(dir string) Option {
	return func(r *Reconciler) {
		r.workDir = dir
	}
}

// WithExecutor overrides the command executor (useful for testing and dry-run).
func WithExecutor(e Executor) Option {
	return func(r *Reconciler) {
		r.executor = e
	}
}

// New creates a new Docker Compose reconciler.
func New(opts ...Option) *Reconciler {
	r := &Reconciler{
		workDir:  ".",
		executor: defaultExecutor,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func defaultExecutor(ctx context.Context, workDir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var _ ports.Reconciler = (*Reconciler)(nil)

// Async reports whether convergence is external/asynchronous.
// For Docker Compose, convergence is immediate and synchronous, so this returns false.
func (r *Reconciler) Async() bool {
	return false
}

// Converge executes `docker compose up -d` in the instance or project directory.
func (r *Reconciler) Converge(ctx context.Context, ref ports.ServerRef) error {
	dir := r.resolveDir(ref)
	return r.executor(ctx, dir, "up", "-d", "--remove-orphans")
}

func (r *Reconciler) resolveDir(ref ports.ServerRef) string {
	if ref.Name == "" {
		return r.workDir
	}
	// If a subdirectory for ref.Name exists and has a compose file, use it
	instanceDir := filepath.Join(r.workDir, ref.Name)
	if _, err := os.Stat(filepath.Join(instanceDir, "docker-compose.yml")); err == nil {
		return instanceDir
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "compose.yaml")); err == nil {
		return instanceDir
	}
	// Otherwise use instanceDir (e.g. before initial write) or workDir
	if _, err := os.Stat(filepath.Join(r.workDir, "docker-compose.yml")); err == nil {
		return r.workDir
	}
	return instanceDir
}

// ComposeService defines a single container in a Compose file.
type ComposeService struct {
	ContainerName string            `yaml:"container_name,omitempty"`
	Image         string            `yaml:"image"`
	Restart       string            `yaml:"restart,omitempty"`
	Command       []string          `yaml:"command,omitempty"`
	Ports         []string          `yaml:"ports,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty"`
	Volumes       []string          `yaml:"volumes,omitempty"`
}

// ComposeFile defines the top-level docker-compose structure.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// RenderCompose converts a domain.RuntimeSpec into a valid docker-compose YAML document.
func RenderCompose(serviceName string, spec domain.RuntimeSpec) ([]byte, error) {
	if serviceName == "" {
		serviceName = "game-server"
	}

	var portsList []string
	for _, p := range spec.Ports {
		proto := strings.ToLower(p.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		portsList = append(portsList, fmt.Sprintf("%d:%d/%s", p.Port, p.Port, proto))
	}

	var volList []string
	for _, v := range spec.Volumes {
		hostPath := "./" + v.Name
		ro := ""
		if v.ReadOnly {
			ro = ":ro"
		}
		volList = append(volList, fmt.Sprintf("%s:%s%s", hostPath, v.MountPath, ro))
	}

	svc := ComposeService{
		ContainerName: serviceName,
		Image:         spec.Image,
		Restart:       "unless-stopped",
		Command:       spec.Command,
		Ports:         portsList,
		Environment:   spec.Env,
		Volumes:       volList,
	}

	cf := ComposeFile{
		Services: map[string]ComposeService{
			serviceName: svc,
		},
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cf); err != nil {
		return nil, fmt.Errorf("encode compose yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteAndConverge renders a compose file for the given spec, writes it to the server's
// directory, and executes Converge.
func (r *Reconciler) WriteAndConverge(ctx context.Context, ref ports.ServerRef, spec domain.RuntimeSpec) error {
	dir := filepath.Join(r.workDir, ref.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	composeBytes, err := RenderCompose(ref.Name, spec)
	if err != nil {
		return err
	}

	target := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(target, composeBytes, 0644); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}

	return r.Converge(ctx, ref)
}
