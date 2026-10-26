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
	Empty       string
	Budget      []string
	Ports       []ManagerPortUI
	CreateHref  string
	CreateLabel string
	SlotsLeft   int
}

type managerSpec struct {
	game       string
	accent     string
	noun       string
	empty      string
	deleteNoun string
	port       func(InstanceUI) int
	facts      func(InstanceUI) []string
	notices    func(InstanceUI) []ManagerNoticeUI
	links      []ActionUI
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

func tierFact(inst InstanceUI) string {
	return fmt.Sprintf("%s, %d GiB", inst.Tier, inst.MemoryGiB)
}

func managerLinks(spec managerSpec, inst InstanceUI) []ActionUI {
	if !inst.CanOperate {
		return nil
	}
	out := make([]ActionUI, 0, len(spec.links))
	for _, l := range spec.links {
		out = append(out, ActionUI{
			Label: l.Label,
			Href:  fmt.Sprintf("/%s/%d/%s", spec.game, inst.Number, l.Href),
		})
	}
	return out
}

func managerActions(spec managerSpec, inst InstanceUI) []ActionUI {
	var out []ActionUI
	switch {
	case !inst.CanOperate:
	case inst.State == "running":
		out = append(out, ActionUI{
			Label:  "Stop",
			Kind:   "go",
			Script: fmt.Sprintf("@post('/api/%s/instances/%d/stop')", spec.game, inst.Number),
		})
	case inst.CanStart:
		out = append(out, ActionUI{
			Label:  "Start",
			Kind:   "go",
			Script: fmt.Sprintf("@post('/api/%s/instances/%d/start')", spec.game, inst.Number),
		})
	default:
		out = append(out, ActionUI{Label: "Start", Kind: "blocked", Script: inst.StartBlockedReason})
	}

	if inst.IsAdmin && inst.State != "running" {
		prompt := fmt.Sprintf(
			"Delete %s #%02d (%s)? World data and configurations will be removed.",
			spec.deleteNoun, inst.Number, inst.Name,
		)
		out = append(out, ActionUI{
			Label: "Delete",
			Kind:  "danger",
			Script: fmt.Sprintf(
				"if (confirm('%s')) @delete('/api/%s/instances/%d')",
				EscapeJS(prompt), spec.game, inst.Number,
			),
		})
	}
	return out
}

func buildManager(spec managerSpec, instances []InstanceUI, budget BudgetUI) ManagerUI {
	m := ManagerUI{
		Game:        spec.game,
		Tint:        tintClass(spec.accent),
		Lede:        fmt.Sprintf("Up to %d %s, %d running at once.", budget.MaxInstances, spec.noun, budget.MaxRunning),
		Empty:       spec.empty,
		Budget:      budgetLine(budget, spec.noun),
		CreateHref:  "/" + spec.game + "/create",
		CreateLabel: "Create " + strings.TrimSuffix(spec.noun, "s"),
		SlotsLeft:   budget.MaxInstances - len(instances),
	}
	for i, inst := range instances {
		m.Ports = append(m.Ports, ManagerPortUI{
			Game:       spec.game,
			Tint:       m.Tint,
			Index:      i,
			Number:     inst.Number,
			Name:       inst.Name,
			Href:       fmt.Sprintf("/%s/%d", spec.game, inst.Number),
			Address:    ConnectAddress(inst.LBIP, spec.port(inst)),
			Lamp:       managerLamp(inst),
			StateLabel: managerStateLabel(inst),
			Presence:   managerPresence(inst),
			Facts:      spec.facts(inst),
			Notices:    spec.notices(inst),
			Links:      managerLinks(spec, inst),
			Actions:    managerActions(spec, inst),
		})
	}
	return m
}

func modUpdateNotice(game, store string, inst InstanceUI) []ManagerNoticeUI {
	if inst.ModUpdates == 0 {
		return nil
	}
	return []ManagerNoticeUI{{
		Label: fmt.Sprintf("%d mod updates", inst.ModUpdates),
		Href:  fmt.Sprintf("/%s/%d/mods", game, inst.Number),
		Title: "Newer versions of installed mods are published on " + store,
	}}
}

var worldLinks = []ActionUI{
	{Label: "Overview", Href: "overview"},
	{Label: "Mods", Href: "mods"},
	{Label: "Configs", Href: "configs"},
	{Label: "Console", Href: "console"},
}

var valheimSpec = managerSpec{
	game:       "valheim",
	accent:     "orange",
	noun:       "worlds",
	deleteNoun: "Valheim world",
	empty:      "No worlds yet. Creating one gives you a dedicated server with mod support, a seed of your choosing and storage that survives a restart.",
	port:       func(InstanceUI) int { return 2456 },
	links:      worldLinks,
	facts: func(inst InstanceUI) []string {
		facts := []string{modBadge(inst.Source, "BepInEx"), tierFact(inst)}
		if inst.Password != "" {
			facts = append(facts, "password set")
		}
		if inst.Seed != "" {
			facts = append(facts, "seed "+inst.Seed)
		}
		return facts
	},
	notices: func(inst InstanceUI) []ManagerNoticeUI {
		var out []ManagerNoticeUI
		if inst.ServerUpdate {
			out = append(out, ManagerNoticeUI{
				Label: "Server update waiting — installs itself once the world is empty",
				Title: "A newer Valheim build is published; the server installs it automatically once no one is playing",
			})
		}
		return append(out, modUpdateNotice("valheim", "Thunderstore", inst)...)
	},
}

var minecraftSpec = managerSpec{
	game:       "minecraft",
	accent:     "emerald",
	noun:       "worlds",
	deleteNoun: "Minecraft world",
	empty:      "No worlds yet. Creating one gives you a modded NeoForge or Fabric world, a modpack of your choosing, or plain vanilla Minecraft.",
	port:       MinecraftPort,
	links:      worldLinks,
	facts: func(inst InstanceUI) []string {
		facts := []string{minecraftLoaderLabel(inst.Loader)}
		if inst.MCVersion != "" {
			facts = append(facts, inst.MCVersion)
		}
		facts = append(facts, tierFact(inst))
		if inst.Pack != "" {
			facts = append(facts, inst.Pack)
		}
		if inst.Seed != "" {
			facts = append(facts, "seed "+inst.Seed)
		}
		return facts
	},
	notices: func(inst InstanceUI) []ManagerNoticeUI {
		return modUpdateNotice("minecraft", "Modrinth", inst)
	},
}

var gmodSpec = managerSpec{
	game:       "gmod",
	accent:     "sky",
	noun:       "servers",
	deleteNoun: "Garry's Mod server",
	empty:      "No servers yet. Creating one gives you a dedicated Garry's Mod server with a Workshop collection, a gamemode and a map.",
	port:       GModPort,
	links:      []ActionUI{{Label: "Overview", Href: "overview"}},
	facts: func(inst InstanceUI) []string {
		var facts []string
		if inst.Gamemode != "" {
			facts = append(facts, inst.Gamemode)
		}
		if inst.Map != "" {
			facts = append(facts, inst.Map)
		}
		facts = append(facts, tierFact(inst))
		if inst.Password != "" {
			facts = append(facts, "password set")
		}
		if inst.CollectionID != "" {
			facts = append(facts, "workshop "+inst.CollectionID)
		}
		return facts
	},
	notices: func(InstanceUI) []ManagerNoticeUI { return nil },
}

func minecraftLoaderLabel(loader string) string {
	switch loader {
	case "fabric":
		return "Fabric"
	case "neoforge":
		return "NeoForge"
	default:
		return "Vanilla"
	}
}

func BuildValheimManager(instances []InstanceUI, budget BudgetUI) ManagerUI {
	return buildManager(valheimSpec, instances, budget)
}

func BuildMinecraftManager(instances []InstanceUI, budget BudgetUI) ManagerUI {
	return buildManager(minecraftSpec, instances, budget)
}

func BuildGModManager(instances []InstanceUI, budget BudgetUI) ManagerUI {
	return buildManager(gmodSpec, instances, budget)
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
	return fmt.Sprintf("$%s ? 'copied!' : 'copy'", p.CopySignal())
}

func InstanceLamp(inst InstanceUI) string {
	return managerLamp(inst)
}

func InstanceStateLabel(inst InstanceUI) string {
	return managerStateLabel(inst)
}

func InstancePresence(inst InstanceUI) string {
	return managerPresence(inst)
}

func (p ManagerPortUI) SignalPrefix() string {
	return fmt.Sprintf("inst_%s_%d", p.Game, p.Number)
}

func (p ManagerPortUI) LampSignals() string {
	pref := p.SignalPrefix()
	return fmt.Sprintf(
		"{'lamp-lit': $%s_online && $%s_players > 0, 'lamp-dim': ($%s_online && !($%s_players > 0)) || $%s_state === 'Starting…', 'lamp-out': !$%s_online && $%s_state !== 'Starting…'}",
		pref, pref, pref, pref, pref, pref, pref,
	)
}

func (p ManagerPortUI) StateSignalExpr() string {
	return fmt.Sprintf("$%s_state || '%s'", p.SignalPrefix(), EscapeJS(p.StateLabel))
}

func (p ManagerPortUI) PresenceShowExpr() string {
	return fmt.Sprintf("$%s_online", p.SignalPrefix())
}

func (p ManagerPortUI) PresenceExpr() string {
	pref := p.SignalPrefix()
	return fmt.Sprintf(
		"($%s_players > 0 ? $%s_players + ' playing' : 'nobody on') + (($%s_uptime && $%s_uptime !== '—') ? ', up ' + $%s_uptime : '')",
		pref, pref, pref, pref, pref,
	)
}
