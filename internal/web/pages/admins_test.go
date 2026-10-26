package pages_test

import (
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
)

func TestAdminTabsDerivedFromProfiles(t *testing.T) {
	profiles := domain.Profiles()
	tabs := pages.AccessTabs("minecraft")

	if len(tabs) != len(profiles) {
		t.Fatalf("AccessTabs returned %d tabs, want %d from domain.Profiles(); newly registered game profiles will not appear in the access navigation bar during game additions", len(tabs), len(profiles))
	}

	for i, p := range profiles {
		if tabs[i].Href != "/access?game="+string(p.ID) {
			t.Fatalf("AccessTabs[%d].Href = %q, want %q; navigation links will lead operators to the wrong access dashboard", i, tabs[i].Href, "/access?game="+string(p.ID))
		}
		if p.ID == domain.GameMinecraft && !tabs[i].Active {
			t.Fatalf("AccessTabs[%d] for active game %q was not marked active; active tab highlight missing on access dashboard", i, p.ID)
		}
		if p.ID != domain.GameMinecraft && tabs[i].Active {
			t.Fatalf("AccessTabs[%d] for inactive game %q was marked active; multiple tabs appear highlighted simultaneously", i, p.ID)
		}
	}
}
