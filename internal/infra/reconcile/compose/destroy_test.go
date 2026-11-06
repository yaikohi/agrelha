package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func seedInstance(t *testing.T, work, data string, ref ports.ServerRef) (string, string) {
	t.Helper()
	composeDir := filepath.Join(work, string(ref.ID.Game), domain.InstanceDirName(ref.ID.Number))
	dataDir := filepath.Join(data, string(ref.ID.Game), domain.InstanceDirName(ref.ID.Number))
	for _, d := range []string{composeDir, dataDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(composeDir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "world.db"), []byte("save"), 0o644); err != nil {
		t.Fatal(err)
	}
	return composeDir, dataDir
}

func TestDestroyRemovesProjectDataAndDir(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	ref := ports.ServerRef{Name: "valheim-gaha-03", ID: domain.NewInstanceID(domain.GameValheim, 3)}
	composeDir, dataDir := seedInstance(t, work, data, ref)

	var got []string
	r := MustNew(WithWorkDir(work), WithDataRoot(data), WithExecutor(func(_ context.Context, dir string, args ...string) error {
		got = append(got, dir+" :: "+strings.Join(args, " "))
		return nil
	}))

	if err := r.Destroy(context.Background(), ref); err != nil {
		t.Fatalf("destroy: %v", err)
	}

	want := composeDir + " :: -p valheim-gaha-03 down --volumes --remove-orphans"
	if len(got) != 1 || got[0] != want {
		t.Errorf("executor calls = %v, want exactly [%q]", got, want)
	}
	for _, d := range []string{composeDir, dataDir} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s still exists after destroy", d)
		}
	}
	// The roots themselves outlive any one world.
	for _, root := range []string{work, data} {
		if _, err := os.Stat(root); err != nil {
			t.Errorf("destroy removed the shared root %s: %v", root, err)
		}
	}
}

// Without an instance ID both resolveDir and instanceDataDir fall back to their
// roots, so an unguarded Destroy would delete every world of every game.
func TestDestroyRefusesRefWithoutInstanceID(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	ref := ports.ServerRef{Name: "valheim-gaha-03", ID: domain.NewInstanceID(domain.GameValheim, 3)}
	_, dataDir := seedInstance(t, work, data, ref)

	r := MustNew(WithWorkDir(work), WithDataRoot(data), WithExecutor(func(context.Context, string, ...string) error {
		t.Error("executor ran for a ref with no instance ID")
		return nil
	}))

	if err := r.Destroy(context.Background(), ports.ServerRef{Name: "valheim-gaha-03"}); err == nil {
		t.Fatal("destroy accepted a ref with no instance ID")
	}
	for _, d := range []string{work, data, dataDir} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("refused destroy still removed %s: %v", d, err)
		}
	}
}

// A world whose compose file is already gone must still have its data reaped,
// not be abandoned because `compose down` would have failed.
func TestDestroyWithoutComposeFileStillRemovesData(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	ref := ports.ServerRef{Name: "mc-test-01", ID: domain.NewInstanceID(domain.GameMinecraft, 1)}
	composeDir, dataDir := seedInstance(t, work, data, ref)
	if err := os.Remove(filepath.Join(composeDir, "docker-compose.yml")); err != nil {
		t.Fatal(err)
	}

	r := MustNew(WithWorkDir(work), WithDataRoot(data), WithExecutor(func(context.Context, string, ...string) error {
		t.Error("executor ran with no compose file present")
		return nil
	}))

	if err := r.Destroy(context.Background(), ref); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("%s survived a destroy with no compose file", dataDir)
	}
}

// A failed teardown must not be swallowed: the caller deletes the records next.
func TestDestroyPropagatesComposeFailure(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	ref := ports.ServerRef{Name: "valheim-boppo-02", ID: domain.NewInstanceID(domain.GameValheim, 2)}
	_, dataDir := seedInstance(t, work, data, ref)

	r := MustNew(WithWorkDir(work), WithDataRoot(data), WithExecutor(func(context.Context, string, ...string) error {
		return os.ErrPermission
	}))

	if err := r.Destroy(context.Background(), ref); err == nil {
		t.Fatal("destroy reported success after compose down failed")
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Errorf("world data removed even though teardown failed: %v", err)
	}
}

// Game data is routinely owned by root or by the image's uid, so deletion goes
// through the Remover rather than a bare os.RemoveAll.
func TestDestroyDeletesWorldDataThroughTheRemover(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	ref := ports.ServerRef{Name: "mc-valeria-02", ID: domain.NewInstanceID(domain.GameMinecraft, 2)}
	_, dataDir := seedInstance(t, work, data, ref)

	var asked []string
	r := MustNew(WithWorkDir(work), WithDataRoot(data),
		WithExecutor(func(context.Context, string, ...string) error { return nil }),
		WithRemover(func(_ context.Context, path string) error {
			asked = append(asked, path)
			return nil
		}),
	)

	if err := r.Destroy(context.Background(), ref); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(asked) != 1 || asked[0] != dataDir {
		t.Errorf("remover asked for %v, want [%q]", asked, dataDir)
	}
}

// If the saves could not be deleted, say so: reporting success would drop the
// records while gigabytes stayed on disk with nothing pointing at them.
func TestDestroyPropagatesRemoverFailure(t *testing.T) {
	work, data := t.TempDir(), t.TempDir()
	ref := ports.ServerRef{Name: "mc-valeria-02", ID: domain.NewInstanceID(domain.GameMinecraft, 2)}
	seedInstance(t, work, data, ref)

	r := MustNew(WithWorkDir(work), WithDataRoot(data),
		WithExecutor(func(context.Context, string, ...string) error { return nil }),
		WithRemover(func(context.Context, string) error { return os.ErrPermission }),
	)

	if err := r.Destroy(context.Background(), ref); err == nil {
		t.Fatal("destroy reported success after world data could not be removed")
	}
}
