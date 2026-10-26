package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPage(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("pages", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

var logPages = []string{
	"instance.templ",
	"valheim_instance.templ",
	"mc_instance.templ",
	"gmod_instance.templ",
	"valheim_console.templ",
}

func TestEveryLogViewerUsesTheSharedPane(t *testing.T) {
	for _, name := range logPages {
		body := readPage(t, name)
		if strings.Contains(body, `id="console-logs"`) || strings.Contains(body, `id="logs"`) {
			t.Errorf(`%s hand-rolls its log box instead of using @LogPane.

The auto-scroll script binds to [data-log-pane]. A hand-rolled box silently gets no follow
behaviour, which is exactly how three Console tabs ended up stuck at the oldest line while only the
legacy page scrolled: the old script bound to id="logs" and the tabs used id="console-logs".`, name)
		}
		if !strings.Contains(body, "@LogPane(") && !strings.Contains(body, "@instanceConsole(") {
			t.Errorf("%s renders no log pane at all", name)
		}
	}
}

func TestTheScrollScriptBindsToTheSharedPaneNotAnID(t *testing.T) {
	layout := readPage(t, "layout.templ")
	if !strings.Contains(layout, "[data-log-pane]") {
		t.Error("the scroll script no longer selects log panes by attribute; binding by id is what broke this before")
	}
	if strings.Contains(layout, `getElementById("logs")`) {
		t.Error("the scroll script still binds to a single hard-coded id, so only one pane on one page would follow")
	}
}

func TestThePaneOffersAWayBackToTheNewestLines(t *testing.T) {
	pane := readPage(t, "log_pane.templ")
	for _, want := range []string{"data-log-jump", "data-log-status", "Jump to newest"} {
		if !strings.Contains(pane, want) {
			t.Errorf(`the log pane is missing %q.

Following has to pause when the operator scrolls up to read history, or the view yanks away from
them on every incoming line - so there must be a visible way back to the newest output.`, want)
		}
	}
}

func TestTrimmingOnlyHappensWhileFollowing(t *testing.T) {
	layout := readPage(t, "layout.templ")
	idx := strings.Index(layout, "removeChild")
	if idx < 0 {
		t.Fatal("the pane no longer caps its line count; a long-running stream would grow the DOM without bound")
	}
	guard := strings.LastIndex(layout[:idx], "if (!follow) return;")
	observer := strings.LastIndex(layout[:idx], "MutationObserver")
	if guard < 0 || guard < observer {
		t.Error(`lines are trimmed even when the operator has scrolled up.

Removing the oldest lines while someone is reading history shifts the content under them.`)
	}
}
