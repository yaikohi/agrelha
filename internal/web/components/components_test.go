package components

import (
	"agrelha/internal/domain"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestButton(t *testing.T) {
	ctx := context.Background()

	t.Run("renders button with primary variant and text", func(t *testing.T) {
		var buf bytes.Buffer
		comp := Button(ButtonProps{
			Variant: VariantPrimary,
			Size:    SizeMd,
			Text:    "Click Me",
			OnClick: "@post('/api/action')",
		})
		if err := comp.Render(ctx, &buf); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "<button") || !strings.Contains(out, "Click Me") {
			t.Errorf("expected <button> with text 'Click Me', got %q", out)
		}
		if !strings.Contains(out, "data-on:click=\"@post(&#39;/api/action&#39;)\"") && !strings.Contains(out, "data-on:click=\"@post('/api/action')\"") {
			t.Errorf("expected data-on:click, got %q", out)
		}
		if !strings.Contains(out, "border-accent") {
			t.Errorf("expected the accent outline for the primary variant, got %q", out)
		}
	})

	t.Run("renders anchor tag when Href provided", func(t *testing.T) {
		var buf bytes.Buffer
		comp := Button(ButtonProps{
			Variant: VariantSecondary,
			Href:    "/valheim/1",
			Text:    "Open Details",
		})
		if err := comp.Render(ctx, &buf); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "<a") || !strings.Contains(out, "href=\"/valheim/1\"") {
			t.Errorf("expected <a> tag with href, got %q", out)
		}
	})

	t.Run("only the danger variant reads as destructive", func(t *testing.T) {
		for _, v := range []Variant{VariantPrimary, VariantSuccess, VariantWarning} {
			var buf bytes.Buffer
			_ = Button(ButtonProps{Variant: v, Text: "Go"}).Render(ctx, &buf)
			if strings.Contains(buf.String(), "danger") {
				t.Errorf(`variant %q renders in the danger colour.

docs/design-system.md reserves danger for destructive actions. Stopping a world is
not destructive, and colouring it like Delete teaches people to ignore the warning.`, v)
			}
		}

		var buf bytes.Buffer
		_ = Button(ButtonProps{Variant: VariantDanger, Text: "Delete"}).Render(ctx, &buf)
		if !strings.Contains(buf.String(), "hover:text-danger") {
			t.Errorf("expected the danger variant to read as destructive, got %q", buf.String())
		}
	})
}

func TestInputAndTextarea(t *testing.T) {
	ctx := context.Background()

	t.Run("renders input with label, bind and focus styles", func(t *testing.T) {
		var buf bytes.Buffer
		comp := Input(InputProps{
			Label:       "World Name",
			Name:        "name",
			Value:       "boppo",
			Placeholder: "e.g. My Realm",
			Required:    true,
			Bind:        "worldName",
			HelperText:  "Choose a unique name",
		})
		if err := comp.Render(ctx, &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "World Name") {
			t.Errorf("expected label, got %q", out)
		}
		if !strings.Contains(out, "required") {
			t.Errorf("expected required attr, got %q", out)
		}
		if !strings.Contains(out, "data-bind=\"worldName\"") {
			t.Errorf("expected data-bind, got %q", out)
		}
		if !strings.Contains(out, "focus-accent") {
			t.Errorf("expected the shared focus ring, got %q", out)
		}
	})

	t.Run("renders textarea with rows and content", func(t *testing.T) {
		var buf bytes.Buffer
		comp := Textarea(TextareaProps{
			Label: "Config Content",
			Rows:  12,
			Value: "key = value",
			Mono:  true,
		})
		if err := comp.Render(ctx, &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "rows=\"12\"") {
			t.Errorf("expected rows=12, got %q", out)
		}
		if !strings.Contains(out, "font-mono") {
			t.Errorf("expected font-mono, got %q", out)
		}
		if !strings.Contains(out, "key = value") {
			t.Errorf("expected text content, got %q", out)
		}
	})
}

func TestTabBar(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	comp := TabBar(TabBarProps{
		Tabs: []TabItem{
			{ID: "overview", Label: "Overview", Href: "/overview", Active: true},
			{ID: "mods", Label: "Mods", Href: "/mods", Active: false, Badge: "3"},
		},
	})
	if err := comp.Render(ctx, &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "border-accent text-ink") {
		t.Errorf("expected the active tab to carry the accent rule, got %q", out)
	}
	if !strings.Contains(out, "border-transparent text-ink-muted") {
		t.Errorf("expected inactive tabs to stay quiet, got %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("expected badge '3', got %q", out)
	}
}

func TestOptionCard(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	comp := OptionCard(OptionCardProps{
		Name:        "tier",
		Value:       "medium",
		BindSignal:  "tier",
		Title:       "Medium Tier",
		Description: "Recommended for 8-10 players",
		Badge:       "6 GiB",
	})
	if err := comp.Render(ctx, &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "<label") || !strings.Contains(out, "Medium Tier") {
		t.Errorf("expected option card with title, got %q", out)
	}
	if !strings.Contains(out, "$tier == &#39;medium&#39;") && !strings.Contains(out, "$tier == 'medium'") {
		t.Errorf("expected data-class tier binding, got %q", out)
	}
}

func TestStepIndicator(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	comp := StepIndicator(StepIndicatorProps{
		Signal: "step",
		Steps: []StepItem{
			{Number: 1, Label: "Identity"},
			{Number: 2, Label: "Content"},
			{Number: 3, Label: "Review"},
		},
	})
	if err := comp.Render(ctx, &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Identity") || !strings.Contains(out, "Content") || !strings.Contains(out, "Review") {
		t.Errorf("expected all step labels, got %q", out)
	}
	if !strings.Contains(out, "$step == 1") || !strings.Contains(out, "$step == 2") {
		t.Errorf("expected signal data-class bindings, got %q", out)
	}
}

func TestNavLeadsToEveryRegisteredGame(t *testing.T) {
	var buf bytes.Buffer
	if err := Nav(NavProps{SignedIn: true, IsAdmin: true}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, p := range domain.Profiles() {
		href := `href="/` + string(p.ID) + `"`
		if !strings.Contains(out, href) {
			t.Errorf(`the nav has no entry for %s.

A registered game that the nav never links to is one nobody can reach.`, p.ID)
		}
	}
	for _, l := range AdminNavLinks() {
		if !strings.Contains(out, `href="`+l.Href+`"`) {
			t.Errorf("admin nav is missing %s", l.Href)
		}
	}
}

func TestNavHidesAdminToolsFromNonAdmins(t *testing.T) {
	var buf bytes.Buffer
	_ = Nav(NavProps{SignedIn: true}).Render(context.Background(), &buf)
	if strings.Contains(buf.String(), `href="/accounts"`) {
		t.Error("a signed-in non-admin was shown the admin tools")
	}
}

func TestToast(t *testing.T) {
	var buf bytes.Buffer
	if err := Toast().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "data-show=\"$toast != ''\"") {
		t.Fatalf("Toast component missing data-show binding for $toast; at 2am operators receive zero notifications when servers fail or boot, got: %s", out)
	}
	if !strings.Contains(out, "$toastkind == 'loading'") {
		t.Fatalf("Toast component missing $toastkind == 'loading' indicator; at 2am an operator triggering a lifecycle action has no visual feedback during the network delay, got: %s", out)
	}
	if !strings.Contains(out, "data-effect=") || !strings.Contains(out, "setTimeout") {
		t.Fatalf("Toast component missing auto-dismiss data-effect; at 2am stale success toasts linger on-screen indefinitely obscuring controls, got: %s", out)
	}
}

