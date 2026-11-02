package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPage(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

var logPages = []string{
	"pages/instance.templ",
	"pages/valheim_instance.templ",
	"pages/mc_instance.templ",
	"pages/gmod_instance.templ",
	"pages/valheim_console.templ",
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
		if !strings.Contains(body, "components.LogPane(") && !strings.Contains(body, "@instanceConsole(") {
			t.Errorf("%s renders no log pane at all", name)
		}
	}
}

func TestTheScrollScriptBindsToTheSharedPaneNotAnID(t *testing.T) {
	layout := readPage(t, "pages/layout.templ")
	if !strings.Contains(layout, "[data-log-pane]") {
		t.Error("the scroll script no longer selects log panes by attribute; binding by id is what broke this before")
	}
	if strings.Contains(layout, `getElementById("logs")`) {
		t.Error("the scroll script still binds to a single hard-coded id, so only one pane on one page would follow")
	}
}

func TestThePaneOffersAWayBackToTheNewestLines(t *testing.T) {
	pane := readPage(t, "components/log_pane.templ")
	for _, want := range []string{"data-log-jump", "data-log-status", "Jump to newest"} {
		if !strings.Contains(pane, want) {
			t.Errorf(`the log pane is missing %q.

Following has to pause when the operator scrolls up to read history, or the view yanks away from
them on every incoming line - so there must be a visible way back to the newest output.`, want)
		}
	}
}

func TestTrimmingIsUnconditionalAndPreservesTheReadingPosition(t *testing.T) {
	layout := readPage(t, "pages/layout.templ")

	if !strings.Contains(layout, "MAX_LINES") || !strings.Contains(layout, "firstElementChild.remove()") {
		t.Fatal("the pane no longer caps its line count; a long-running stream would grow the DOM without bound")
	}

	trim := strings.Index(layout, "const trim = ")
	observer := strings.Index(layout, "MutationObserver")
	if trim < 0 || observer < 0 || trim > observer {
		t.Fatal("the trim step is no longer defined ahead of the observer that drives it")
	}

	body := layout[trim:observer]
	if strings.Contains(body, "if (!follow) {\n\t\t\t\t\t\t\t\treturn;") {
		t.Error(`trimming is gated on following again.

An earlier version skipped the cap whenever the operator had scrolled up, so a paused pane on a busy
stream grew without bound. The cap must apply in both states.`)
	}
	if !strings.Contains(body, "scrollHeight") || !strings.Contains(body, "pane.scrollTop = Math.max(") {
		t.Error(`trimming no longer compensates the scroll position.

Removing the oldest lines shifts content up under a reader who has scrolled back. Capping the DOM is
only safe because scrollTop is reduced by exactly the height that was removed; without that
compensation the view yanks on every trim and the old follow-gate was the lesser evil.`)
	}
}
