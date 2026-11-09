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

// PanelGameUI is one game and the worlds it runs. A game with no worlds still
// appears — absence is information on this page.
type PanelGameUI struct {
	Slug   string
	Label  string
	Tint   string
	Ember  string
	Worlds []PanelPortUI
	Hint   string
}

// PanelTallyUI counts what is up, across every game. Lit means running: the
// lamp's lit-versus-dim distinction is about players, not about the world.
type PanelTallyUI struct {
	Lit     int
	Playing int
	Worlds  int
}

type PanelUI struct {
	Games []PanelGameUI
	Tally PanelTallyUI
}

// Count is the rail's first line: how many worlds this game has and how many of
// them are up.
func (g PanelGameUI) Count() string {
	if len(g.Worlds) == 0 {
		return "No worlds yet"
	}
	lit := 0
	for _, w := range g.Worlds {
		if w.Row.Online {
			lit++
		}
	}
	worlds := fmt.Sprintf("%s worlds", countWord(len(g.Worlds)))
	if len(g.Worlds) == 1 {
		worlds = "One world"
	}
	switch lit {
	case 0:
		return worlds + ", none lit"
	case len(g.Worlds):
		if lit == 1 {
			return worlds + ", lit"
		}
		return worlds + ", all lit"
	default:
		return fmt.Sprintf("%s, %s lit", worlds, countWord(lit))
	}
}

// Flavour is the rail's second line — what a world of this game runs. It speaks
// only when every world agrees: one line cannot say two things, and each card
// carries its own badge.
func (g PanelGameUI) Flavour() string {
	if len(g.Worlds) == 0 {
		return ""
	}
	badge := g.Worlds[0].Row.VersionBadge
	for _, w := range g.Worlds[1:] {
		if w.Row.VersionBadge != badge {
			return ""
		}
	}
	return badge
}

// BorderTint paints the card's top edge in the game's colour. The classes are
// spelled out because Tailwind reads source text: a class built at runtime never
// reaches the stylesheet.
func (p PanelPortUI) BorderTint() string {
	switch p.Tint {
	case "text-tint-sky":
		return "border-t-tint-sky"
	case "text-tint-moss":
		return "border-t-tint-moss"
	case "text-tint-flame":
		return "border-t-tint-flame"
	}
	return "border-t-edge"
}

// Headline says the state of the house in words, split so the page can hush the
// second half: "Two worlds are lit." / "One is sleeping."
func (t PanelTallyUI) Headline() (lit, asleep string) {
	switch {
	case t.Worlds == 0:
		return "No worlds yet.", ""
	case t.Lit == 0 && t.Worlds == 1:
		return "The world is sleeping.", ""
	case t.Lit == 0:
		return "Nothing is running right now.", ""
	}

	if t.Lit == 1 {
		lit = "One world is lit."
	} else {
		lit = fmt.Sprintf("%s worlds are lit.", capitalise(countWord(t.Lit)))
	}
	switch off := t.Worlds - t.Lit; {
	case off == 0:
		return lit, ""
	case off == 1:
		return lit, "One is sleeping."
	default:
		return lit, fmt.Sprintf("%s are sleeping.", capitalise(countWord(off)))
	}
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// countWord spells the small numbers, which is how the sentence above reads out
// loud. Past nine a digit is clearer than a word.
func countWord(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	if n < 0 || n >= len(words) {
		return fmt.Sprintf("%d", n)
	}
	return words[n]
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

func emberName(accent string) string {
	switch accent {
	case "sky":
		return "sky"
	case "emerald":
		return "moss"
	case "orange":
		return "flame"
	default:
		return ""
	}
}

func BuildPanel(games []DashboardGameUI) PanelUI {
	var p PanelUI
	index := 0
	for _, g := range games {
		label := strings.TrimSuffix(strings.ToLower(g.Card.Title), " worlds")
		game := PanelGameUI{
			Slug:  panelSlug(label),
			Label: label,
			Tint:  tintClass(g.Card.Accent),
			Ember: emberName(g.Card.Accent),
			Hint:  g.Card.EmptyHint,
		}
		for _, r := range g.Card.Rows {
			// Index numbers every world on the page, not every world in its
			// game: it names the copy-button signals, which share one scope.
			game.Worlds = append(game.Worlds, PanelPortUI{
				Game:       label,
				Tint:       game.Tint,
				Index:      index,
				Row:        r,
				AccessNote: g.Card.AccessNote,
			})
			index++
			p.Tally.Worlds++
			if r.Online {
				p.Tally.Lit++
			}
			if r.PlayersKnown {
				p.Tally.Playing += r.Players
			}
		}
		p.Games = append(p.Games, game)
	}
	return p
}

// panelSlug turns a game label into an anchor. Garry's Mod has an apostrophe and
// a space, and both have to survive into a usable fragment.
func panelSlug(label string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func (p PanelPortUI) LampClass() string {
	switch {
	case p.Row.Online && p.Row.PlayersKnown && p.Row.Players > 0:
		return "lamp lamp-lit"
	case p.Row.Online:
		return "lamp lamp-dim"
	default:
		return "lamp lamp-out"
	}
}

func (p PanelPortUI) SignalsInit() string {
	if p.Row.OnlineSignal == "" {
		return ""
	}
	parts := []string{
		fmt.Sprintf("%s:%t", p.Row.OnlineSignal, p.Row.Online),
	}
	if p.Row.PlayersSignal != "" {
		parts = append(parts, fmt.Sprintf("%s:%d", p.Row.PlayersSignal, p.Row.Players))
	}
	if p.Row.UptimeSignal != "" {
		parts = append(parts, fmt.Sprintf("%s:'%s'", p.Row.UptimeSignal, EscapeJS(p.Row.Uptime)))
	}
	return "{" + strings.Join(parts, ",") + "}"
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

// Presence says who is on. A count nobody could read is not a count of zero:
// reporting "nobody on" for a world the panel cannot ask is how two people
// playing became an empty server on the page.
func (p PanelPortUI) Presence() string {
	if !p.Row.Online {
		return "Not running"
	}
	who := "nobody on"
	switch {
	case !p.Row.PlayersKnown:
		who = "players unknown"
	case p.Row.Players > 0:
		who = fmt.Sprintf("%d playing", p.Row.Players)
	}
	if p.Row.Uptime != "" && p.Row.Uptime != "—" {
		return who + ", up " + p.Row.Uptime
	}
	return who
}

// PresenceExpr binds the row to live signals. With no readable count there is
// no signal worth binding - the expression would print "nobody on" again - so
// the row keeps the static text that says so.
func (p PanelPortUI) PresenceExpr() string {
	if p.Row.OnlineSignal == "" || !p.Row.PlayersKnown {
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
	return fmt.Sprintf("$%s ? 'copied!' : 'copy'", p.CopySignal())
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
	Title   string
	Icon    string
	Accent  string
	Tint    string
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

		title := g.Card.Title
		if title == "" {
			title = label
		}

		section := AdminGameUI{
			Title:  title,
			Icon:   g.Card.Icon,
			Accent: g.Card.Accent,
			Tint:   tint,
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
