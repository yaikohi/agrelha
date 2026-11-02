package components

import (
	"strings"
	"testing"
)

func TestSelectionIsNotSignalledByHueAlone(t *testing.T) {
	props := OptionCardProps{Name: "source", Value: "vanilla", BindSignal: "source", Title: "Vanilla"}
	got := props.selectionClasses()

	if !strings.Contains(got, "'border-accent': $source == 'vanilla'") {
		t.Fatalf("the selected card no longer takes the accent border: %s", got)
	}
	if !strings.Contains(got, "'border-edge': $source != 'vanilla'") {
		t.Fatalf("the unselected card no longer falls back to the edge border: %s", got)
	}

	for _, cue := range []string{"ring-1", "bg-accent/10"} {
		if !strings.Contains(got, cue) {
			t.Errorf(`selection lost its %q cue.

Border colour alone cannot carry this. In dark, edge -> accent is 8.81:1 and reads instantly; in light
it is 2.34:1 across a single pixel and is invisible, which is how an operator came to not know which
content option they had picked. It also fails WCAG 1.4.1, since hue was the only visual difference.
Keep a stroke-weight cue and a fill cue alongside the colour.`, cue)
		}
	}
}
