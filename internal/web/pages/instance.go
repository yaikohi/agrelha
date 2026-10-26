package pages

import (
	"fmt"

	"agrelha/internal/domain"
)

type InstanceTabUI struct {
	Label  string
	Href   string
	Active bool
}

type InstanceHeadUI struct {
	Game       string
	GameLabel  string
	Tint       string
	Slot       string
	Name       string
	Lamp       string
	StateLabel string
	Presence   string
	Actions    []ActionUI
	Blocked    string
	Tabs       []InstanceTabUI
}

type InstanceFactUI struct {
	Label string
	Value string
	Note  string
}

type InstanceNoticeUI struct {
	Label  string
	Body   string
	Note   string
	Action *ActionUI
}

type InstanceOverviewUI struct {
	Address  string
	Password string
	Facts    []InstanceFactUI
	Actions  []ActionUI
	JoinHint string
	Notices  []InstanceNoticeUI
}

type instanceSpec struct {
	game     string
	label    string
	accent   string
	port     func(InstanceUI) int
	facts    func(InstanceDetailUI) []InstanceFactUI
	actions  func(InstanceDetailUI) []ActionUI
	joinHint func(InstanceDetailUI) string
	notices  func(InstanceDetailUI) []InstanceNoticeUI
	pending  func(InstanceDetailUI) string
}

func instanceTabs(game string, num int, active string, vanilla bool) []InstanceTabUI {
	slugs, ok := implementedTabs[game]
	if !ok {
		slugs = []string{"overview"}
	}
	consoleLabel := tabLabelBySlug["console"]
	if p, ok := domain.ProfileFor(domain.GameID(game)); ok && !p.Capabilities.Console {
		consoleLabel = "Logs"
	}

	tabs := make([]InstanceTabUI, 0, len(slugs))
	for _, slug := range slugs {
		if slug == "mods" && vanilla {
			continue
		}
		label := tabLabelBySlug[slug]
		if slug == "console" {
			label = consoleLabel
		}
		tabs = append(tabs, InstanceTabUI{
			Label:  label,
			Href:   fmt.Sprintf("/%s/%d/%s", game, num, slug),
			Active: active == slug,
		})
	}
	return tabs
}

func instanceActions(spec instanceSpec, d InstanceDetailUI) []ActionUI {
	if d.HostUnreachable || d.HostUnknown {
		return nil
	}
	post := func(verb string) string {
		return fmt.Sprintf("@post('/api/%s/instances/%d/%s')", spec.game, d.Number, verb)
	}
	if d.State == "running" {
		return []ActionUI{
			{Label: "Stop", Kind: "go", Script: post("stop")},
			{Label: "Restart", Kind: "quiet", Script: post("restart")},
		}
	}
	return []ActionUI{{Label: "Start", Kind: "go", Script: post("start")}}
}

func buildInstanceHead(spec instanceSpec, d InstanceDetailUI) InstanceHeadUI {
	h := InstanceHeadUI{
		Game:       spec.game,
		GameLabel:  spec.label,
		Tint:       tintClass(spec.accent),
		Slot:       fmt.Sprintf("#%02d", d.Number),
		Name:       d.Name,
		Lamp:       managerLamp(d.InstanceUI),
		StateLabel: managerStateLabel(d.InstanceUI),
		Presence:   managerPresence(d.InstanceUI),
		Actions:    instanceActions(spec, d),
		Tabs:       instanceTabs(spec.game, d.Number, d.ActiveTab, d.Vanilla),
	}
	if d.HostUnreachable || d.HostUnknown {
		h.Blocked = "Lifecycle controls are unavailable while the Host is out of reach."
	}
	return h
}

func buildInstanceOverview(spec instanceSpec, d InstanceDetailUI) InstanceOverviewUI {
	o := InstanceOverviewUI{
		Address:  ConnectAddress(d.LBIP, spec.port(d.InstanceUI)),
		Password: d.Password,
		Facts:    spec.facts(d),
		Actions:  spec.actions(d),
		JoinHint: spec.joinHint(d),
		Notices:  spec.notices(d),
	}
	if d.RestartPending {
		body := d.RestartReason
		if body == "" {
			body = spec.pending(d)
		}
		notice := InstanceNoticeUI{Label: "Restart pending", Body: body}
		if d.State == "running" && !d.HostUnreachable && !d.HostUnknown {
			notice.Action = &ActionUI{
				Label:  "Restart now",
				Kind:   "go",
				Script: fmt.Sprintf("@post('/api/%s/instances/%d/restart')", spec.game, d.Number),
			}
		}
		o.Notices = append([]InstanceNoticeUI{notice}, o.Notices...)
	}
	return o
}

func (o InstanceOverviewUI) CopyInit() string {
	return "{ocopied:false}"
}

func (o InstanceOverviewUI) CopyClick() string {
	return fmt.Sprintf(
		"agrelhaCopy('%s'); $ocopied = true; setTimeout(() => $ocopied = false, 1800)",
		EscapeJS(o.Address),
	)
}

func (o InstanceOverviewUI) CopyLabelExpr() string {
	return "$ocopied ? 'copied' : 'click to copy'"
}

func noNotices(InstanceDetailUI) []InstanceNoticeUI { return nil }

func defaultPending(InstanceDetailUI) string {
	return "A committed change is waiting for the world to empty before it is applied."
}

var valheimInstanceSpec = instanceSpec{
	game:   "valheim",
	label:  "Valheim",
	accent: "orange",
	port:   func(InstanceUI) int { return 2456 },
	facts: func(d InstanceDetailUI) []InstanceFactUI {
		facts := []InstanceFactUI{
			{Label: "Modding", Value: modBadge(d.Source, "BepInEx"), Note: "Thunderstore packages"},
			{Label: "Resources", Value: d.Tier, Note: fmt.Sprintf("%d GiB memory limit", d.MemoryGiB)},
			{Label: "Mods", Value: fmt.Sprintf("%d installed", len(d.InstalledMods))},
		}
		if d.Seed != "" {
			facts = append(facts, InstanceFactUI{Label: "Seed", Value: d.Seed})
		}
		return facts
	},
	actions: func(d InstanceDetailUI) []ActionUI {
		return []ActionUI{{
			Label: "Download the .r2z profile",
			Kind:  "download",
			Href:  fmt.Sprintf("/api/valheim/%d/mods/export", d.Number),
		}}
	},
	pending: defaultPending,
	joinHint: func(InstanceDetailUI) string {
		return "Import the profile into r2modman or Thunderstore Mod Manager (Import → From file) to match the server mod for mod, then connect to the address above."
	},
	notices: func(d InstanceDetailUI) []InstanceNoticeUI {
		if !d.ServerUpdate {
			return nil
		}
		n := InstanceNoticeUI{
			Label: "Server update available",
			Body:  "A newer Valheim build is published. The server installs it on its own within 15 minutes of the world being empty, then restarts — world saves are not touched. Nothing to do unless you want it sooner.",
		}
		if d.ServerBuild != "" {
			n.Note = "running build " + d.ServerBuild
		}
		return []InstanceNoticeUI{n}
	},
}

var minecraftInstanceSpec = instanceSpec{
	game:   "minecraft",
	label:  "Minecraft",
	accent: "emerald",
	port:   MinecraftPort,
	facts: func(d InstanceDetailUI) []InstanceFactUI {
		facts := []InstanceFactUI{
			{Label: "Loader", Value: minecraftLoaderLabel(d.Loader), Note: d.MCVersion},
			{Label: "Resources", Value: d.Tier, Note: fmt.Sprintf("%d GiB memory limit", d.MemoryGiB)},
			{Label: "Mods", Value: fmt.Sprintf("%d installed", len(d.InstalledMods))},
		}
		if d.Pack != "" {
			facts = append(facts, InstanceFactUI{Label: "Modpack", Value: d.Pack})
		}
		if d.Seed != "" {
			facts = append(facts, InstanceFactUI{Label: "Seed", Value: d.Seed})
		}
		return facts
	},
	actions: func(d InstanceDetailUI) []ActionUI {
		if d.Vanilla {
			return nil
		}
		return []ActionUI{{
			Label: "Download the .mrpack",
			Kind:  "download",
			Href:  fmt.Sprintf("/api/minecraft/%d/mods/export", d.Number),
		}}
	},
	pending: defaultPending,
	joinHint: func(d InstanceDetailUI) string {
		if d.Vanilla {
			return "A vanilla world needs no client setup — connect to the address above."
		}
		return "Import the pack into Prism Launcher or the Modrinth App (Add Instance → Import from zip) to match the server mod for mod, then connect to the address above."
	},
	notices: noNotices,
}

var gmodInstanceSpec = instanceSpec{
	game:   "gmod",
	label:  "Garry's Mod",
	accent: "sky",
	port:   GModPort,
	facts: func(d InstanceDetailUI) []InstanceFactUI {
		facts := []InstanceFactUI{
			{Label: "Resources", Value: d.Tier, Note: fmt.Sprintf("%d GiB memory limit", d.MemoryGiB)},
		}
		if d.Gamemode != "" {
			facts = append(facts, InstanceFactUI{Label: "Gamemode", Value: d.Gamemode})
		}
		if d.Map != "" {
			facts = append(facts, InstanceFactUI{Label: "Map", Value: d.Map})
		}
		if d.CollectionID != "" {
			note := ""
			if d.CollectionItems > 0 {
				note = fmt.Sprintf("%d items", d.CollectionItems)
			}
			facts = append(facts, InstanceFactUI{Label: "Workshop collection", Value: d.CollectionID, Note: note})
		}
		return facts
	},
	actions: func(d InstanceDetailUI) []ActionUI {
		if d.LBIP == "" {
			return nil
		}
		addr := fmt.Sprintf("%s:%d", d.LBIP, GModPort(d.InstanceUI))
		cmd := "connect " + addr
		steam := "steam://connect/" + addr
		if d.Password != "" {
			cmd += "; password " + d.Password
			steam += "/" + d.Password
		}
		return []ActionUI{
			{Label: "Join from Steam", Kind: "link", Href: steam},
			{Label: "Copy the console command", Kind: "copy", Script: fmt.Sprintf("agrelhaCopy('%s')", EscapeJS(cmd))},
		}
	},
	joinHint: func(InstanceDetailUI) string {
		return "Join from Steam, or open the Garry's Mod console and paste the connect command."
	},
	notices: noNotices,
	pending: func(InstanceDetailUI) string {
		return "The Workshop collection changed upstream. Restarting the world installs the new addons."
	},
}

func logStreamExpr(game string, num int) string {
	return fmt.Sprintf("@get('/api/%s/%d/logs/stream')", game, num)
}
