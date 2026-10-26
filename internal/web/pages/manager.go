package pages

import (
	"fmt"
	"strings"
)

type ManagerNoticeUI struct {
	Label string
	Href  string
	Title string
}

type ManagerPortUI struct {
	Game       string
	Tint       string
	Index      int
	Number     int
	Name       string
	Href       string
	Address    string
	Lamp       string
	StateLabel string
	Presence   string
	Facts      []string
	Notices    []ManagerNoticeUI
	Links      []ActionUI
	Actions    []ActionUI
}

type ManagerUI struct {
	Game        string
	Tint        string
	Lede        string
	Budget      []string
	Ports       []ManagerPortUI
	CreateHref  string
	CreateLabel string
	SlotsLeft   int
}

func managerLamp(inst InstanceUI) string {
	switch {
	case inst.HostUnknown || inst.HostUnreachable:
		return "lamp lamp-unknown"
	case inst.State == "running" && inst.PlayersKnown && inst.Players > 0:
		return "lamp lamp-lit"
	case inst.State == "running", inst.State == "starting", inst.State == "provisioning":
		return "lamp lamp-dim"
	default:
		return "lamp lamp-out"
	}
}

func managerStateLabel(inst InstanceUI) string {
	switch {
	case inst.HostUnknown:
		return "Host not configured"
	case inst.HostUnreachable:
		return "Unknown"
	case inst.State == "running":
		return "Running"
	case inst.State == "starting", inst.State == "provisioning":
		return "Starting…"
	case inst.State == "":
		return "Stopped"
	default:
		return strings.ToUpper(inst.State[:1]) + inst.State[1:]
	}
}

func managerPresence(inst InstanceUI) string {
	if inst.State != "running" {
		return ""
	}
	who := "nobody on"
	if inst.PlayersKnown && inst.Players > 0 {
		who = fmt.Sprintf("%d playing", inst.Players)
	}
	if inst.Uptime != "" && inst.Uptime != "—" {
		return who + ", up " + inst.Uptime
	}
	return who
}

func budgetLine(b BudgetUI, noun string) []string {
	return []string{
		fmt.Sprintf("%d of %d %s saved", b.TotalInstances, b.MaxInstances, noun),
		fmt.Sprintf("%d of %d running", b.RunningCount, b.MaxRunning),
		fmt.Sprintf("RAM %d GiB of %d GiB", b.UsedGiB, b.TotalBudgetGiB),
	}
}

func valheimFacts(inst InstanceUI) []string {
	facts := []string{
		modBadge(inst.Source, "BepInEx"),
		fmt.Sprintf("%s, %d GiB", inst.Tier, inst.MemoryGiB),
	}
	if inst.Password != "" {
		facts = append(facts, "password set")
	}
	if inst.Seed != "" {
		facts = append(facts, "seed "+inst.Seed)
	}
	return facts
}

func valheimNotices(inst InstanceUI) []ManagerNoticeUI {
	var out []ManagerNoticeUI
	if inst.ServerUpdate {
		out = append(out, ManagerNoticeUI{
			Label: "Server update waiting — installs itself once the world is empty",
			Title: "A newer Valheim build is published; the server installs it automatically once no one is playing",
		})
	}
	if inst.ModUpdates > 0 {
		out = append(out, ManagerNoticeUI{
			Label: fmt.Sprintf("%d mod updates", inst.ModUpdates),
			Href:  fmt.Sprintf("/valheim/%d/mods", inst.Number),
			Title: "Newer versions of installed mods are published on Thunderstore",
		})
	}
	return out
}

func valheimLinks(inst InstanceUI) []ActionUI {
	if !inst.CanOperate {
		return nil
	}
	return []ActionUI{
		{Label: "Overview", Href: fmt.Sprintf("/valheim/%d/overview", inst.Number)},
		{Label: "Mods", Href: fmt.Sprintf("/valheim/%d/mods", inst.Number)},
		{Label: "Configs", Href: fmt.Sprintf("/valheim/%d/configs", inst.Number)},
		{Label: "Console", Href: fmt.Sprintf("/valheim/%d/console", inst.Number)},
	}
}

func valheimActions(inst InstanceUI) []ActionUI {
	var out []ActionUI
	switch {
	case !inst.CanOperate:
	case inst.State == "running":
		out = append(out, ActionUI{
			Label:  "Stop",
			Kind:   "go",
			Script: fmt.Sprintf("@post('/api/valheim/instances/%d/stop')", inst.Number),
		})
	case inst.CanStart:
		out = append(out, ActionUI{
			Label:  "Start",
			Kind:   "go",
			Script: fmt.Sprintf("@post('/api/valheim/instances/%d/start')", inst.Number),
		})
	default:
		out = append(out, ActionUI{Label: "Start", Kind: "blocked", Script: inst.StartBlockedReason})
	}

	if inst.IsAdmin && inst.State != "running" {
		out = append(out, ActionUI{
			Label: "Delete",
			Kind:  "danger",
			Script: fmt.Sprintf(
				"if (confirm('Delete Valheim world #%02d (%s)? World data and configurations will be removed.')) @delete('/api/valheim/instances/%d')",
				inst.Number, EscapeJS(inst.Name), inst.Number,
			),
		})
	}
	return out
}

func BuildValheimManager(instances []InstanceUI, budget BudgetUI) ManagerUI {
	m := ManagerUI{
		Game:        "valheim",
		Tint:        tintClass("orange"),
		Lede:        fmt.Sprintf("Up to %d worlds, %d running at once.", budget.MaxInstances, budget.MaxRunning),
		Budget:      budgetLine(budget, "worlds"),
		CreateHref:  "/valheim/create",
		CreateLabel: "Create world",
		SlotsLeft:   budget.MaxInstances - len(instances),
	}
	for i, inst := range instances {
		m.Ports = append(m.Ports, ManagerPortUI{
			Game:       "valheim",
			Tint:       m.Tint,
			Index:      i,
			Number:     inst.Number,
			Name:       inst.Name,
			Href:       fmt.Sprintf("/valheim/%d", inst.Number),
			Address:    ConnectAddress(inst.LBIP, 2456),
			Lamp:       managerLamp(inst),
			StateLabel: managerStateLabel(inst),
			Presence:   managerPresence(inst),
			Facts:      valheimFacts(inst),
			Notices:    valheimNotices(inst),
			Links:      valheimLinks(inst),
			Actions:    valheimActions(inst),
		})
	}
	return m
}

func (p ManagerPortUI) Slot() string {
	return fmt.Sprintf("#%02d", p.Number)
}

func (p ManagerPortUI) CopySignal() string {
	return fmt.Sprintf("mcopied_%d", p.Index)
}

func (p ManagerPortUI) CopyInit() string {
	return fmt.Sprintf("{%s:false}", p.CopySignal())
}

func (p ManagerPortUI) CopyClick() string {
	s := p.CopySignal()
	return fmt.Sprintf(
		"agrelhaCopy('%s'); $%s = true; setTimeout(() => $%s = false, 1800)",
		EscapeJS(p.Address), s, s,
	)
}

func (p ManagerPortUI) CopyLabelExpr() string {
	return fmt.Sprintf("$%s ? 'copied' : 'click to copy'", p.CopySignal())
}

func managerActionStyle(kind string) string {
	switch kind {
	case "danger":
		return "border border-edge text-ink-muted hover:border-danger hover:text-danger"
	case "blocked":
		return "border border-edge text-ink-faint cursor-not-allowed"
	default:
		return "border border-accent text-accent hover:bg-accent hover:text-accent-ink"
	}
}
