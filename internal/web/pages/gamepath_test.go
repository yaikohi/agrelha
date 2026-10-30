package pages

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func TestGamePathRefusesToBuildAHalfFormedURL(t *testing.T) {
	var zero InstanceDetailUI
	if got := zero.GamePath(); got != "" {
		t.Fatalf("GamePath() = %q for a zero value, want empty", got)
	}

	url := "/api/" + zero.GamePath() + "/1/mods/updates/apply"
	if !strings.Contains(url, "//") {
		t.Fatal("this test exists because an empty GamePath produces /api//1/..., which routes nowhere and returns 200 in 0ms")
	}
}

func TestEveryGameResolvesAGamePath(t *testing.T) {
	for _, id := range []domain.GameID{domain.GameValheim, domain.GameMinecraft, domain.GameGMod} {
		d := InstanceDetailUI{InstanceUI: InstanceUI{GameID: string(id)}}
		got := d.GamePath()
		if got == "" {
			t.Errorf("GamePath() empty for %q; every handler rendering this panel must set GameID, or the mod-update buttons POST to /api//<instanceNumber>/... and silently do nothing", id)
		}
		if strings.Contains("/api/"+got+"/1/mods/updates/apply", "//") {
			t.Errorf("malformed URL for %q", id)
		}
	}
}
