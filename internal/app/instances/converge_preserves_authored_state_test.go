package instances

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type modListSpyRenderer struct {
	seen domain.ModList
}

func (r *modListSpyRenderer) Render(inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	r.seen = mods
	return map[string][]byte{
		"docker-compose.yml": []byte("services: {}\n"),
		"configs.yaml":       []byte("apiVersion: v1\nkind: ConfigMap\ndata: {}\n"),
		"mods.yaml":          []byte("apiVersion: v1\nkind: ConfigMap\ndata:\n  mods.txt: |\n" + indentLines(mods.Primary)),
	}, nil
}

func indentLines(s string) string {
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		sb.WriteString("    " + line + "\n")
	}
	return sb.String()
}

func authoredStateManager(t *testing.T, game domain.GameID, profile domain.GameProfile) (*InstanceManager, *mockStateStore, *modListSpyRenderer) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "authored.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	states := newMockStateStore()
	renderer := &modListSpyRenderer{}
	mgr := NewInstanceManager(
		store.NewGameInstanceRepo(st, profile), states, nil, 64, 4, 4,
		"manifests/"+string(game), "192.168.20.224",
		renderer, string(game),
		WithGameID(game),
		WithGameProfile(profile),
		WithReconciler(&recordingReconciler{}),
	)
	return mgr, states, renderer
}

const authoredConfigs = "apiVersion: v1\nkind: ConfigMap\ndata:\n  org.bepinex.plugins.valheim_plus.cfg: |\n    [AutoStack]\n    enabled = true\n"

func seedAuthoredWorld(t *testing.T, mgr *InstanceManager, states *mockStateStore, extra map[string]string) domain.Instance {
	t.Helper()
	inst := domain.Instance{
		Number: 1, Name: "Lareira", Slug: "lareira", Tier: domain.TierSmall,
		Host: "fogo", GameID: mgr.gameID,
	}
	if err := mgr.repo.Upsert(inst); err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	dir := mgr.instanceDirFor(inst)
	ctx := context.Background()
	_ = states.Put(ctx, dir+"/configs.yaml", ports.Document{
		Data: map[string]string{"org.bepinex.plugins.valheim_plus.cfg": "[AutoStack]\nenabled = true\n"},
		Raw:  []byte(authoredConfigs),
	}, "seed")
	data := map[string]string{"mods.txt": "Advize/PlantEverything/1.21.4\n"}
	for k, v := range extra {
		data[k] = v
	}
	_ = states.Put(ctx, dir+"/mods.yaml", ports.Document{Data: data}, "seed")
	return inst
}

func TestConvergingAModUpdateKeepsAuthoredConfigOverrides(t *testing.T) {
	mgr, states, _ := authoredStateManager(t, domain.GameValheim, domain.ValheimProfile)
	inst := seedAuthoredWorld(t, mgr, states, nil)
	ctx := context.Background()

	if _, err := mgr.ReplaceMods(ctx, 1, []string{"Advize/PlantEverything/1.21.5"}, "", "tester"); err != nil {
		t.Fatalf("ReplaceMods: %v", err)
	}

	got, err := states.Get(ctx, mgr.instanceDirFor(inst)+"/configs.yaml")
	if err != nil {
		t.Fatalf("read configs.yaml: %v", err)
	}
	if !strings.Contains(string(got.Raw), "valheim_plus.cfg") {
		t.Errorf("converging a mod update overwrote the authored config Overrides with the renderer's empty ConfigMap.\n"+
			"the renderer derives its output from the Instance and the Mod list; it is given no configs, so it emits \"data: {}\",\n"+
			"and PutTree replaces the stored file with it. Overrides are authored through the config editor, not derived,\n"+
			"so convergence must carry the stored file forward.\ngot:\n%s", string(got.Raw))
	}
}

func TestConvergingAModUpdateKeepsTheCurseForgeHalfOfTheModList(t *testing.T) {
	mgr, states, renderer := authoredStateManager(t, domain.GameMinecraft, domain.MinecraftProfile)
	seedAuthoredWorld(t, mgr, states, map[string]string{"curseforge.txt": "jei:12345\n"})

	if _, err := mgr.ReplaceMods(context.Background(), 1, []string{"fabric/sodium"}, "", "tester"); err != nil {
		t.Fatalf("ReplaceMods: %v", err)
	}

	if strings.TrimSpace(renderer.seen.CurseForge) == "" {
		t.Errorf("ReplaceMods converged with only the Primary half of the Mod list, so the renderer re-emitted mods.yaml\n"+
			"without curseforge.txt and every CurseForge mod was dropped from Desired state on the next mod update.\n"+
			"got ModList{Primary: %q, CurseForge: %q}", renderer.seen.Primary, renderer.seen.CurseForge)
	}
}
