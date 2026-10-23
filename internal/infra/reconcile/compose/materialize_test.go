package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func TestMaterializeWritesWhereConvergeWillLook(t *testing.T) {
	work := t.TempDir()
	r := MustNew(WithWorkDir(work), WithExecutor(func(context.Context, string, ...string) error { return nil }))
	ref := ports.ServerRef{Name: "valheim-odin-01", ID: domain.NewInstanceID(domain.GameValheim, 1)}

	files := map[string][]byte{
		"docker-compose.yml": []byte("services: {}\n"),
		"mods.txt":           []byte("a-b-1.0.0\n"),
	}
	if err := r.Materialize(context.Background(), ref, files); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	dir := r.resolveDir(ref)
	if dir != filepath.Join(work, "valheim", "instance-01") {
		t.Fatalf("unexpected dir %q", dir)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s not written where converge runs: %v", name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s content mismatch", name)
		}
	}
}

func TestMaterializeCreatesTheDirectory(t *testing.T) {
	r := MustNew(WithWorkDir(filepath.Join(t.TempDir(), "does", "not", "exist")))
	ref := ports.ServerRef{Name: "gmod-lareira-01", ID: domain.NewInstanceID(domain.GameGMod, 1)}

	if err := r.Materialize(context.Background(), ref, map[string][]byte{"docker-compose.yml": []byte("x")}); err != nil {
		t.Fatalf("a first converge must create its own directory: %v", err)
	}
}
