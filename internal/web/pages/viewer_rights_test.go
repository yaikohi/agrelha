package pages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var dashboards = []string{
	"gmod_dashboard.templ",
	"mc_dashboard.templ",
}

func readTempl(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestNoDashboardOffersLifecycleControlsToAViewerWhoCannotOperate(t *testing.T) {
	for _, name := range dashboards {
		body := readTempl(t, name)
		for _, verb := range []string{"/start')", "/stop')", "/restart')"} {
			for idx := strings.Index(body, verb); idx >= 0; idx = strings.Index(body[idx+1:], verb) + idx + 1 {
				window := body[max(0, idx-600):idx]
				if !strings.Contains(window, "inst.CanOperate") {
					t.Errorf(`%s posts %s inside a block that never consults inst.CanOperate.

Signed-in Accounts without a Role for a world can now reach these dashboards, so an ungated button
is one the server will refuse. A button that always 403s is worse than no button.`, name, verb)
				}
				if strings.Index(body[idx+1:], verb) < 0 {
					break
				}
			}
		}
	}
}

func TestDeleteStaysAdminOnlyOnEveryDashboard(t *testing.T) {
	for _, name := range dashboards {
		body := readTempl(t, name)
		idx := strings.Index(body, "@delete(")
		if idx < 0 {
			continue
		}
		window := body[max(0, idx-400):idx]
		if !strings.Contains(window, "inst.IsAdmin") {
			t.Errorf(`%s renders a Delete button that is not gated on inst.IsAdmin.

Deleting a world is admin-only in the guard regardless of who holds a Role for it, so showing the
button to an operator promises something the server will always refuse.`, name)
		}
	}
}

func TestTheViewerRightsFieldExists(t *testing.T) {
	var ui InstanceUI
	ui.CanOperate = true
	ui.IsAdmin = true
	if !ui.CanOperate || !ui.IsAdmin {
		t.Error("InstanceUI must carry both the viewer's admin flag and their per-world right")
	}
}
