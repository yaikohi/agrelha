package pages

import (
	"fmt"
	"strings"
)

type PanelPortUI struct {
	Game       string
	Tint       string
	Index      int
	Row        ServerRowUI
	AccessNote string
}

type PanelEmptyUI struct {
	Game string
	Tint string
	Hint string
}

type PanelUI struct {
	Ports []PanelPortUI
	Idle  []PanelEmptyUI
}

func tintClass(accent string) string {
	switch accent {
	case "sky":
		return "text-tint-sky"
	case "emerald":
		return "text-tint-moss"
	case "orange":
		return "text-tint-flame"
	default:
		return "text-ink-muted"
	}
}

func BuildPanel(games []DashboardGameUI) PanelUI {
	var p PanelUI
	for _, g := range games {
		label := strings.TrimSuffix(strings.ToLower(g.Card.Title), " worlds")
		tint := tintClass(g.Card.Accent)
		if len(g.Card.Rows) == 0 {
			p.Idle = append(p.Idle, PanelEmptyUI{
				Game: label,
				Tint: tint,
				Hint: g.Card.EmptyHint,
			})
			continue
		}
		for _, r := range g.Card.Rows {
			p.Ports = append(p.Ports, PanelPortUI{
				Game:       label,
				Tint:       tint,
				Index:      len(p.Ports),
				Row:        r,
				AccessNote: g.Card.AccessNote,
			})
		}
	}
	return p
}

func (p PanelPortUI) LampClass() string {
	if p.Row.OnlineSignal != "" {
		return "lamp"
	}
	switch {
	case p.Row.Online && p.Row.PlayersKnown && p.Row.Players > 0:
		return "lamp lamp-lit"
	case p.Row.Online:
		return "lamp lamp-dim"
	default:
		return "lamp lamp-out"
	}
}

func (p PanelPortUI) LampSignals() string {
	if p.Row.OnlineSignal == "" {
		return ""
	}
	on := "$" + p.Row.OnlineSignal
	if p.Row.PlayersSignal == "" {
		return fmt.Sprintf("{'lamp-dim': %s, 'lamp-out': !%s}", on, on)
	}
	pl := "$" + p.Row.PlayersSignal
	return fmt.Sprintf(
		"{'lamp-lit': %s && %s > 0, 'lamp-dim': %s && !(%s > 0), 'lamp-out': !%s}",
		on, pl, on, pl, on,
	)
}

func (p PanelPortUI) Presence() string {
	if !p.Row.Online {
		return "Not running"
	}
	who := "nobody on"
	if p.Row.PlayersKnown && p.Row.Players > 0 {
		who = fmt.Sprintf("%d playing", p.Row.Players)
	}
	if p.Row.Uptime != "" && p.Row.Uptime != "—" {
		return who + ", up " + p.Row.Uptime
	}
	return who
}

func (p PanelPortUI) PresenceExpr() string {
	if p.Row.OnlineSignal == "" {
		return ""
	}
	on := "$" + p.Row.OnlineSignal
	who := "'nobody on'"
	if p.Row.PlayersSignal != "" {
		pl := "$" + p.Row.PlayersSignal
		who = fmt.Sprintf("(%s > 0 ? %s + ' playing' : 'nobody on')", pl, pl)
	}
	up := "''"
	if p.Row.UptimeSignal != "" {
		u := "$" + p.Row.UptimeSignal
		up = fmt.Sprintf("((%s && %s != '—') ? ', up ' + %s : '')", u, u, u)
	}
	return fmt.Sprintf("%s ? (%s + %s) : 'Not running'", on, who, up)
}

func (p PanelPortUI) CopySignal() string {
	return fmt.Sprintf("copied_%d", p.Index)
}

func (p PanelPortUI) CopyInit() string {
	return fmt.Sprintf("{%s:false}", p.CopySignal())
}

func (p PanelPortUI) CopyClick() string {
	s := p.CopySignal()
	return fmt.Sprintf(
		"agrelhaCopy('%s'); $%s = true; setTimeout(() => $%s = false, 1800)",
		EscapeJS(p.Row.Address), s, s,
	)
}

func (p PanelPortUI) CopyLabelExpr() string {
	return fmt.Sprintf("$%s ? 'copied' : 'click to copy'", p.CopySignal())
}

func (p PanelPortUI) ImportHint() string {
	if p.Row.DownloadURL == "" || len(p.Row.Launchers) == 0 {
		return ""
	}
	hint := "Import into " + strings.Join(p.Row.Launchers, " or ")
	if p.Row.ImportSteps != "" {
		hint += " (" + p.Row.ImportSteps + ")"
	}
	return hint
}

type AdminGameUI struct {
	Ports   []PanelPortUI
	Idle    *PanelEmptyUI
	Budget  []string
	Actions CardActionsUI
}

func cleanActionLabel(s string) string {
	s = strings.TrimSuffix(s, " →")
	s = strings.TrimPrefix(s, "+ ")
	s = strings.ReplaceAll(s, " Manager", " manager")
	s = strings.ReplaceAll(s, " World", " world")
	return s
}

func cleanAction(a ActionUI) ActionUI {
	a.Label = cleanActionLabel(a.Label)
	return a
}

func cleanActions(in []ActionUI) []ActionUI {
	if len(in) == 0 {
		return nil
	}
	out := make([]ActionUI, 0, len(in))
	for _, a := range in {
		out = append(out, cleanAction(a))
	}
	return out
}

func BuildAdminPanel(games []DashboardGameUI) []AdminGameUI {
	var out []AdminGameUI
	n := 0
	for _, g := range games {
		label := strings.TrimSuffix(strings.ToLower(g.Card.Title), " worlds")
		tint := tintClass(g.Card.Accent)

		section := AdminGameUI{
			Budget: g.Card.Extras,
			Actions: CardActionsUI{
				Lifecycle: cleanActions(g.Actions.Lifecycle),
				Special:   cleanActions(g.Actions.Special),
				Links:     cleanActions(g.Actions.Links),
				Manager:   cleanAction(g.Actions.Manager),
			},
		}

		if len(g.Card.Rows) == 0 {
			section.Idle = &PanelEmptyUI{Game: label, Tint: tint}
			out = append(out, section)
			continue
		}
		for _, r := range g.Card.Rows {
			section.Ports = append(section.Ports, PanelPortUI{
				Game:       label,
				Tint:       tint,
				Index:      n,
				Row:        r,
				AccessNote: g.Card.AccessNote,
			})
			n++
		}
		out = append(out, section)
	}
	return out
}

func hearthNavLabel(name string) string {
	return strings.ToLower(name)
}

func hearthNavClass(active bool) string {
	base := "focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors "
	if active {
		return base + "border-accent text-ink"
	}
	return base + "border-transparent text-ink-muted hover:text-accent"
}

func adminNavLinks() []ActionUI {
	return []ActionUI{
		{Label: "history", Href: "/history"},
		{Label: "access", Href: "/admins"},
		{Label: "overview", Href: "/overview"},
		{Label: "accounts", Href: "/accounts"},
		{Label: "requests", Href: "/requests"},
	}
}

func hearthActionStyle(primary bool) string {
	if primary {
		return "border border-accent text-accent hover:bg-accent hover:text-accent-ink"
	}
	return "border border-edge text-ink hover:border-accent hover:text-accent"
}
