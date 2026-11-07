package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestModSearchControlsPostEveryFilterOnEveryTrigger(t *testing.T) {
	html := render(t, ModSearchControls("/api/valheim/1/mods/search", "Mining…"))

	for _, want := range []string{
		`data-on:input__debounce.300ms`,
		`data-bind="modQuery"`,
		`data-bind="modSort"`,
		`data-bind="modDeprecated"`,
		`value="downloads"`,
		`value="updated"`,
		`Show deprecated`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("controls missing %q", want)
		}
	}

	// Text, sort and deprecated travel together: a sort change must not drop the
	// query, and typing must not drop the sort.
	script := strings.ReplaceAll(ModSearchScript("/api/valheim/1/mods/search", 0), "'", "&#39;")
	if got := strings.Count(html, script); got != 4 {
		t.Errorf("expected the search box, the sort, the checkbox and the button to post %q; got %d of them", script, got)
	}
}

func TestValheimModsTabDeclaresEverySignalItsControlsRead(t *testing.T) {
	html := render(t, valheimModsTabOperate(InstanceDetailUI{InstanceUI: InstanceUI{Number: 1, CanOperate: true}}))

	for _, signal := range []string{"modQuery", "modSort", "modDeprecated", "modSearchLoading"} {
		if !strings.Contains(html, signal+":") {
			t.Errorf("signal %q is read by the controls but never declared", signal)
		}
	}
}
