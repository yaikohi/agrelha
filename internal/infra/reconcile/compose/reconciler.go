// Package compose implements ports.Reconciler on top of Docker Compose.
// It provides synchronous convergence: rendering compose projects and running `compose up`.
package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
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
	dataRoot string
	executor Executor
	remover  Remover
}

// Option configures a Reconciler.
type Option func(*Reconciler)

// WithWorkDir sets the base working directory for compose projects.
func WithWorkDir(dir string) Option {
	return func(r *Reconciler) {
		r.workDir = dir
	}
}

// WithDataRoot sets where a world's runtime data lives. It must sit outside
// workDir: workDir is materialised from git, so data under it would be deleted
// by a checkout or clean.
func WithDataRoot(dir string) Option {
	return func(r *Reconciler) {
		r.dataRoot = dir
	}
}

// WithExecutor overrides the command executor (useful for testing and dry-run).
func WithExecutor(e Executor) Option {
	return func(r *Reconciler) {
		r.executor = e
	}
}

// Remover deletes a world's directory tree.
type Remover func(ctx context.Context, path string) error

// WithRemover overrides how world data is deleted (useful for testing).
func WithRemover(rm Remover) Option {
	return func(r *Reconciler) {
		r.remover = rm
	}
}

// removeTree deletes path, falling back to a throwaway root container when the
// files are not ours. Docker creates a bind-mount source as root, so most game
// data ends up owned by root or by the image's own uid: agrelha can unlink the
// directory from its parent but cannot recurse into it. The daemon socket is
// the one root we already hold, so borrow it rather than running agrelha as
// root or leaving the saves behind.
func removeTree(ctx context.Context, path string) error {
	err := os.RemoveAll(path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	parent, base := filepath.Split(filepath.Clean(path))
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", filepath.Clean(parent)+":/target", "alpine:3.20",
		"rm", "-rf", "/target/"+base)
	if out, derr := cmd.CombinedOutput(); derr != nil {
		return fmt.Errorf("remove %s as root: %w (output: %s)", path, derr, strings.TrimSpace(string(out)))
	}
	return nil
}

func validatePaths(workDir, dataRoot string) error {
	if dataRoot == "" {
		return nil
	}
	if !filepath.IsAbs(dataRoot) {
		return fmt.Errorf("data root %q must be an absolute path: world saves would resolve relative to process cwd", dataRoot)
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolve compose work dir %q: %w", workDir, err)
	}
	cleanData := filepath.Clean(dataRoot)
	if absWork == cleanData {
		return fmt.Errorf("data root %q must not match compose work dir %q: materialization would overwrite game saves", dataRoot, workDir)
	}
	relData, err := filepath.Rel(absWork, cleanData)
	if err == nil && !strings.HasPrefix(relData, "..") {
		return fmt.Errorf("data root %q must not sit inside compose work dir %q: operations in work dir would delete world saves", dataRoot, workDir)
	}
	relWork, err := filepath.Rel(cleanData, absWork)
	if err == nil && !strings.HasPrefix(relWork, "..") {
		return fmt.Errorf("compose work dir %q must not sit inside data root %q: server mounts would collide with compose manifests", workDir, dataRoot)
	}
	return nil
}

// New creates a new Docker Compose reconciler.
func New(opts ...Option) (*Reconciler, error) {
	r := &Reconciler{
		workDir:  ".",
		executor: DefaultExecutor,
		remover:  removeTree,
	}
	for _, opt := range opts {
		opt(r)
	}
	if err := validatePaths(r.workDir, r.dataRoot); err != nil {
		return nil, err
	}
	return r, nil
}

func MustNew(opts ...Option) *Reconciler {
	r, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// DefaultExecutor is the default command executor used by Reconciler.
var DefaultExecutor Executor = func(ctx context.Context, workDir string, args ...string) error {
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

func EscapeComposeInterpolation(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}

func EscapeVars(s string) string {
	return EscapeComposeInterpolation(s)
}

const DefaultStartPeriod = "15m"

var _ ports.Reconciler = (*Reconciler)(nil)

// Async reports whether convergence is external/asynchronous.
// For Docker Compose, convergence is immediate and synchronous, so this returns false.
func (r *Reconciler) Async() bool {
	return false
}

func (r *Reconciler) Materialize(ctx context.Context, ref ports.ServerRef, files map[string][]byte) error {
	dir := r.resolveDir(ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

var _ ports.Materializer = (*Reconciler)(nil)

// Converge executes `docker compose up -d` in the instance or project directory.
func (r *Reconciler) Converge(ctx context.Context, ref ports.ServerRef) error {
	dir := r.resolveDir(ref)
	return r.executor(ctx, dir, append(r.projectArgs(ref), "up", "-d", "--remove-orphans")...)
}

// Destroy tears down one world: its containers, its network, its compose
// project directory and its saves. The Delete confirmation already promises
// "World data and configurations will be removed", and on a Docker host
// nothing else ever will - unlike the cluster, where dropping the manifests
// from the state store is enough for ArgoCD to prune the Deployment.
//
// It refuses a ref without an instance ID: resolveDir and instanceDataDir both
// fall back to their roots for an unidentified ref, so the removals below
// would take the whole compose work dir and every game's saves with them.
func (r *Reconciler) Destroy(ctx context.Context, ref ports.ServerRef) error {
	if ref.ID.Game == "" || ref.ID.Number <= 0 {
		return fmt.Errorf("destroy %q: refusing a ref with no instance ID, its paths resolve to the shared compose and data roots", ref.Name)
	}
	dir := r.resolveDir(ref)
	if composeFileIn(dir) != "" {
		if err := r.executor(ctx, dir, append(r.projectArgs(ref), "down", "--volumes", "--remove-orphans")...); err != nil {
			return fmt.Errorf("compose down %s: %w", ref.Name, err)
		}
	}
	if data := r.instanceDataDir(ref); data != "" {
		if err := r.remover(ctx, data); err != nil {
			return fmt.Errorf("remove world data %s: %w", data, err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove compose project %s: %w", dir, err)
	}
	return nil
}

var _ ports.Destroyer = (*Reconciler)(nil)

// composeFileIn reports which compose file dir holds, or "" for none.
func composeFileIn(dir string) string {
	for _, name := range []string{"docker-compose.yml", "compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name
		}
	}
	return ""
}

func (r *Reconciler) projectArgs(ref ports.ServerRef) []string {
	if ref.Name == "" {
		return nil
	}
	return []string{"-p", ref.Name}
}

func (r *Reconciler) resolveDir(ref ports.ServerRef) string {
	if ref.ID.Game != "" && ref.ID.Number > 0 {
		return filepath.Join(r.workDir, string(ref.ID.Game), domain.InstanceDirName(ref.ID.Number))
	}
	if ref.Name == "" {
		return r.workDir
	}
	// If a subdirectory for ref.Name exists and has a compose file, use it
	instanceDir := filepath.Join(r.workDir, ref.Name)
	if composeFileIn(instanceDir) != "" {
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
	ContainerName string             `yaml:"container_name,omitempty"`
	Image         string             `yaml:"image"`
	Restart       string             `yaml:"restart,omitempty"`
	Command       []string           `yaml:"command,omitempty"`
	Ports         []string           `yaml:"ports,omitempty"`
	Environment   map[string]string  `yaml:"environment,omitempty"`
	Volumes       []string           `yaml:"volumes,omitempty"`
	Healthcheck   *ComposeHealth     `yaml:"healthcheck,omitempty"`
	DependsOn     map[string]Depends `yaml:"depends_on,omitempty"`
	Labels        map[string]string  `yaml:"labels,omitempty"`
}

type Depends struct {
	Condition string `yaml:"condition"`
}

// ComposeHealth is a service healthcheck. The Docker runtime already reads
// container health into Status.Available, so without one "Available" degrades
// to "the process is running" - which is exactly the misreport the Kubernetes
// probes were changed to avoid.
type ComposeHealth struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
}

// ComposeFile defines the top-level docker-compose structure.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// RenderCompose converts a domain.RuntimeSpec into a valid docker-compose YAML document.
func RenderCompose(serviceName, dataRoot string, spec domain.RuntimeSpec) ([]byte, error) {
	return RenderComposeWith(serviceName, dataRoot, spec, nil)
}

// RenderComposeWith renders spec alongside extra services the game server must
// wait for, such as the step that stages config overrides before it boots.
func RenderComposeWith(serviceName, dataRoot string, spec domain.RuntimeSpec, extra map[string]ComposeService) ([]byte, error) {
	if serviceName == "" {
		serviceName = "game-server"
	}
	if strings.TrimSpace(dataRoot) == "" && len(spec.Volumes) > 0 {
		return nil, fmt.Errorf("compose render for %q: no data root, so volumes would resolve beside the compose file and put world data inside the materialised git tree", serviceName)
	}

	var portsList []string
	for _, p := range spec.Ports {
		proto := strings.ToLower(p.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		hostPort := p.HostPort
		if hostPort == 0 {
			hostPort = p.Port
		}
		portsList = append(portsList, fmt.Sprintf("%d:%d/%s", hostPort, p.Port, proto))
	}

	var volList []string
	for _, v := range spec.Volumes {
		hostPath := filepath.Join(dataRoot, v.Name)
		ro := ""
		if v.ReadOnly {
			ro = ":ro"
		}
		volList = append(volList, fmt.Sprintf("%s:%s%s", hostPath, v.MountPath, ro))
	}

	env := make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		env[k] = EscapeComposeInterpolation(v)
	}
	command := make([]string, 0, len(spec.Command))
	for _, c := range spec.Command {
		command = append(command, EscapeComposeInterpolation(c))
	}

	svc := ComposeService{
		ContainerName: serviceName,
		Image:         spec.Image,
		Restart:       "unless-stopped",
		Command:       command,
		Ports:         portsList,
		Environment:   env,
		Volumes:       volList,
		Healthcheck:   healthcheckFor(spec.HealthProbe),
	}

	if len(extra) > 0 {
		svc.DependsOn = make(map[string]Depends, len(extra))
		for name := range extra {
			svc.DependsOn[name] = Depends{Condition: "service_completed_successfully"}
		}
	}

	cf := ComposeFile{
		Services: map[string]ComposeService{
			serviceName: svc,
		},
	}
	maps.Copy(cf.Services, extra)

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

	composeBytes, err := RenderCompose(ref.Name, r.instanceDataDir(ref), spec)
	if err != nil {
		return err
	}

	target := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(target, composeBytes, 0644); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}

	return r.Converge(ctx, ref)
}

// InstanceDataDir is where one Instance's runtime data lives under dataRoot.
// Shared so the renderer that writes the compose file and the reconciler that
// runs it cannot disagree about where the volumes point.
func InstanceDataDir(dataRoot string, id domain.InstanceID, fallbackName string) string {
	if dataRoot == "" {
		return ""
	}
	if id.Game != "" && id.Number > 0 {
		return filepath.Join(dataRoot, string(id.Game), domain.InstanceDirName(id.Number))
	}
	return filepath.Join(dataRoot, fallbackName)
}

func (r *Reconciler) instanceDataDir(ref ports.ServerRef) string {
	return InstanceDataDir(r.dataRoot, ref.ID, ref.Name)
}

// healthcheckFor turns a game's declared probe command into a compose
// healthcheck. The same string drives the Kubernetes probe, so both runtimes
// agree on what "serving" means.
func healthcheckFor(probe string) *ComposeHealth {
	probe = strings.TrimSpace(probe)
	if probe == "" {
		return nil
	}
	return &ComposeHealth{
		Test:        []string{"CMD-SHELL", probe},
		Interval:    "30s",
		Timeout:     "5s",
		Retries:     3,
		StartPeriod: DefaultStartPeriod,
	}
}
