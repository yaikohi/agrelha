package compose

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func TestTwoGamesInstanceOneDoNotCollide(t *testing.T) {
	work := t.TempDir()
	var dirs, projects []string
	r := MustNew(WithWorkDir(work), WithExecutor(func(_ context.Context, dir string, args ...string) error {
		dirs = append(dirs, dir)
		for i, a := range args {
			if a == "-p" && i+1 < len(args) {
				projects = append(projects, args[i+1])
			}
		}
		return nil
	}))

	refs := []ports.ServerRef{
		{Name: "valheim-odin-01", ID: domain.NewInstanceID(domain.GameValheim, 1)},
		{Name: "gmod-lareira-01", ID: domain.NewInstanceID(domain.GameGMod, 1)},
	}
	for _, ref := range refs {
		if err := r.Converge(context.Background(), ref); err != nil {
			t.Fatalf("converge %s: %v", ref.Name, err)
		}
	}

	if dirs[0] == dirs[1] {
		t.Errorf("both worlds converged in %q; instance-01 of two games must not share a directory", dirs[0])
	}
	if len(projects) != 2 {
		t.Fatalf("expected an explicit -p per converge, got %v", projects)
	}
	if projects[0] == projects[1] {
		t.Errorf("both worlds used compose project %q; with --remove-orphans one would tear down the other's containers", projects[0])
	}
}

func TestInstanceDirMirrorsTheGitPath(t *testing.T) {
	work := t.TempDir()
	r := MustNew(WithWorkDir(work))

	got := r.resolveDir(ports.ServerRef{Name: "valheim-odin-03", ID: domain.NewInstanceID(domain.GameValheim, 3)})
	want := filepath.Join(work, "valheim", "instance-03")
	if got != want {
		t.Errorf("resolveDir = %q, want %q; the local layout must mirror the git path", got, want)
	}
}

func TestRefWithoutAnIDKeepsTheOldLayout(t *testing.T) {
	work := t.TempDir()
	r := MustNew(WithWorkDir(work))

	got := r.resolveDir(ports.ServerRef{Name: "legacy-deployment"})
	if got != filepath.Join(work, "legacy-deployment") {
		t.Errorf("a ref with no Instance id must fall back to the name-keyed directory, got %q", got)
	}
}

func TestVolumesNeverResolveBesideTheComposeFile(t *testing.T) {
	spec := domain.RuntimeSpec{
		Image:   "busybox",
		Volumes: []domain.VolumeSpec{{Name: "data", MountPath: "/data"}},
	}

	if _, err := RenderCompose("valheim-odin-01", "", spec); err == nil {
		t.Fatal("rendering volumes with no data root must fail: they would land inside the tree materialised from git, where a checkout or clean would delete the world")
	}

	out, err := RenderCompose("valheim-odin-01", "/srv/gamedata/valheim/instance-01", spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "./data:") {
		t.Errorf("volume still resolves beside the compose file:\n%s", out)
	}
	if !strings.Contains(string(out), "/srv/gamedata/valheim/instance-01/data:/data") {
		t.Errorf("volume does not point at the data root:\n%s", out)
	}
}

func TestRendererAndReconcilerAgreeOnTheDataDir(t *testing.T) {
	id := domain.NewInstanceID(domain.GameValheim, 2)
	shared := InstanceDataDir("/srv/gamedata", id, "valheim-boppo-02")

	r := MustNew(WithDataRoot("/srv/gamedata"))
	viaReconciler := r.instanceDataDir(ports.ServerRef{Name: "valheim-boppo-02", ID: id})

	if shared != viaReconciler {
		t.Errorf("data dir disagreement: helper %q vs reconciler %q; the compose file would point somewhere the reconciler does not create", shared, viaReconciler)
	}
}
