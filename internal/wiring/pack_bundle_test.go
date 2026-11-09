package wiring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/content/curseforge"
	"agrelha/internal/ports"
)

type fakePackReader struct {
	pack ports.InstalledPack
	err  error
}

func (f fakePackReader) InstalledPack(context.Context, domain.InstanceID) (ports.InstalledPack, error) {
	return f.pack, f.err
}

func craftoria() ports.InstalledPack {
	return ports.InstalledPack{
		Provider: domain.ProviderCurseForge,
		ModID:    1535822,
		FileID:   8732682,
		Name:     "craftoria-2",
		Version:  "0.11.0",
		FileName: "Craftoria 2 - 0.11.0",
	}
}

func packWorld() domain.Instance {
	return domain.Instance{
		Number: 2, Name: "Valeria", Slug: "mc-valeria-02",
		GameID: domain.GameMinecraft,
		Source: domain.SourceModpack,
		Minecraft: &domain.MinecraftConfig{
			MCVersion: "26.1.2",
			Loader:    domain.LoaderNeoForge,
			Pack: &domain.Pack{
				Provider: domain.ProviderCurseForge,
				Name:     "Craftoria 2",
				Ref:      "https://www.curseforge.com/minecraft/modpacks/craftoria-2",
			},
		},
	}
}

// cfServing answers the one call packBundle makes, with whatever download URL
// the test wants CurseForge to admit to.
func cfServing(t *testing.T, downloadURL string) *curseforge.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mods/1535822/files/8732682" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"id":8732682,"fileName":"Craftoria 2-0.11.0.zip","downloadUrl":%q,"fileLength":21045464}}`, downloadURL)
	}))
	t.Cleanup(srv.Close)
	return curseforge.New(srv.URL, "test-key")
}

func TestPackBundleHandsOverTheReleaseTheServerInstalled(t *testing.T) {
	const cdn = "https://edge.forgecdn.net/files/8732/682/Craftoria%202-0.11.0.zip"
	d := Deps{CF: cfServing(t, cdn)}

	got, err := packBundle(context.Background(), d, fakePackReader{pack: craftoria()}, packWorld())
	if err != nil {
		t.Fatalf("packBundle: %v", err)
	}
	if got.URL != cdn {
		t.Errorf("URL = %q, want the pack file %q: the mods directory holds only the server half of the pack, so nothing else a player imports will let them join", got.URL, cdn)
	}
	if got.Data != nil {
		t.Error("the panel copied the publisher's pack into the response instead of sending the player to it")
	}
	if got.Filename != "craftoria-2-0.11.0.zip" {
		t.Errorf("filename = %q, want craftoria-2-0.11.0.zip", got.Filename)
	}
}

func TestPackBundleRefusesWhenThePackCannotBeIdentifiedOrFetched(t *testing.T) {
	cases := []struct {
		name  string
		deps  Deps
		packs ports.PackReader
	}{
		{"no installer record", Deps{CF: cfServing(t, "https://example.invalid/p.zip")}, fakePackReader{err: os.ErrNotExist}},
		{"no reader at all", Deps{CF: cfServing(t, "https://example.invalid/p.zip")}, nil},
		{"no CurseForge key", Deps{}, fakePackReader{pack: craftoria()}},
		{"author forbids distribution", Deps{CF: cfServing(t, "")}, fakePackReader{pack: craftoria()}},
		{"a provider the panel cannot resolve", Deps{CF: cfServing(t, "https://example.invalid/p.zip")},
			fakePackReader{pack: ports.InstalledPack{Provider: domain.ProviderModrinth, ModID: 1, FileID: 2}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := packBundle(context.Background(), tc.deps, tc.packs, packWorld())
			if !errors.Is(err, domain.ErrNothingToExport) {
				t.Errorf("err = %v, want ErrNothingToExport so the handler can point at the pack page instead of serving an empty pack", err)
			}
		})
	}
}
