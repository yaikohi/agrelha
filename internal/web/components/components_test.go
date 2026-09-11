package components

import (
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
		if !strings.Contains(out, "bg-zinc-100") {
			t.Errorf("expected bg-zinc-100 for primary variant, got %q", out)
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

	t.Run("renders success and warning variants", func(t *testing.T) {
		var bufSuccess bytes.Buffer
		_ = Button(ButtonProps{Variant: VariantSuccess, Text: "Start"}).Render(ctx, &bufSuccess)
		if !strings.Contains(bufSuccess.String(), "bg-emerald-600") {
			t.Errorf("expected bg-emerald-600 for VariantSuccess, got %q", bufSuccess.String())
		}

		var bufWarn bytes.Buffer
		_ = Button(ButtonProps{Variant: VariantWarning, Text: "Stop"}).Render(ctx, &bufWarn)
		if !strings.Contains(bufWarn.String(), "bg-amber-950/30") {
			t.Errorf("expected bg-amber-950/30 for VariantWarning, got %q", bufWarn.String())
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
		if !strings.Contains(out, "focus:border-zinc-400") {
			t.Errorf("expected neutral focus border, got %q", out)
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
	if !strings.Contains(out, "border-b-2 border-zinc-100 text-zinc-100 font-semibold") {
		t.Errorf("expected active tab classes, got %q", out)
	}
	if !strings.Contains(out, "border-b-2 border-transparent text-zinc-400") {
		t.Errorf("expected inactive tab classes, got %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("expected badge '3', got %q", out)
	}
}

func TestStatusPill(t *testing.T) {
	ctx := context.Background()

	t.Run("renders running status pill with emerald dot", func(t *testing.T) {
		var buf bytes.Buffer
		comp := StatusPill(StatusPillProps{State: "running"})
		if err := comp.Render(ctx, &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "bg-emerald-950/80") || !strings.Contains(out, "bg-emerald-400 animate-pulse") {
			t.Errorf("expected emerald pulse dot, got %q", out)
		}
		if !strings.Contains(out, "Running") {
			t.Errorf("expected 'Running' label, got %q", out)
		}
	})

	t.Run("renders signal-driven pill", func(t *testing.T) {
		var buf bytes.Buffer
		comp := StatusPill(StatusPillProps{Signal: "online", State: "online"})
		if err := comp.Render(ctx, &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "data-class=") || !strings.Contains(out, "$online") {
			t.Errorf("expected signal binding on pill, got %q", out)
		}
	})
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

func TestModal(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	comp := Modal(ModalProps{
		OpenSignal: "showModal",
		Title:      "Confirm Action",
		Subtitle:   "This action is permanent",
	})
	if err := comp.Render(ctx, &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "data-show=\"$showModal\"") {
		t.Errorf("expected data-show for modal, got %q", out)
	}
	if !strings.Contains(out, "Confirm Action") {
		t.Errorf("expected modal title, got %q", out)
	}
	if !strings.Contains(out, "$showModal = false") {
		t.Errorf("expected close button handler, got %q", out)
	}
}

func TestBudgetBar(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	comp := BudgetBar(BudgetBarProps{
		UsedGiB:        6,
		TotalBudgetGiB: 16,
		RunningCount:   1,
		MaxRunning:     2,
		TotalInstances: 3,
		MaxInstances:   4,
		ActionHref:     "/admins",
		ActionText:     "Access (Admins)",
	})
	if err := comp.Render(ctx, &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "6 / 16 GiB") {
		t.Errorf("expected RAM display, got %q", out)
	}
	if !strings.Contains(out, "1 / 2") {
		t.Errorf("expected running ratio, got %q", out)
	}
	if !strings.Contains(out, "3 / 4") {
		t.Errorf("expected saved ratio, got %q", out)
	}
	if !strings.Contains(out, "Access (Admins)") || !strings.Contains(out, "href=\"/admins\"") {
		t.Errorf("expected access action button, got %q", out)
	}
}

func TestEmptyState(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	comp := EmptyState(EmptyStateProps{
		Icon:        "⚔️",
		Title:       "No Worlds Yet",
		Description: "Create a world to start playing.",
		ActionHref:  "/valheim/create",
		ActionText:  "+ Create World",
	})
	if err := comp.Render(ctx, &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "⚔️") {
		t.Errorf("expected icon, got %q", out)
	}
	if !strings.Contains(out, "No Worlds Yet") {
		t.Errorf("expected title, got %q", out)
	}
	if !strings.Contains(out, "Create a world to start playing.") {
		t.Errorf("expected description, got %q", out)
	}
	if !strings.Contains(out, "+ Create World") || !strings.Contains(out, "href=\"/valheim/create\"") {
		t.Errorf("expected action button, got %q", out)
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
