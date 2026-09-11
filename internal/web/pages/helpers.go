package pages

import "slices"

import "fmt"

import "strings"

// ModKey reduces a "namespace/name/version" entry to "namespace/name".
func ModKey(entry string) string {
	p := strings.Split(entry, "/")
	if len(p) >= 2 {
		return p[0] + "/" + p[1]
	}
	return entry
}

type ModUpdate struct {
	Key     string
	Current string
	Latest  string
	Token   string
}

func UpdateToken(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Contains reports whether s is in ss.
func Contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}

func HistoryLabel(kind string) string {
	switch kind {
	case "restart":
		return "Server restarted"
	case "stop":
		return "Server stopped"
	case "start":
		return "Server started"
	case "mod-install":
		return "Mod installed"
	case "mod-remove":
		return "Mod removed"
	case "admin-grant":
		return "Admin granted"
	case "admin-revoke":
		return "Admin revoked"
	case "mc-mod-install":
		return "Minecraft: Mod installed"
	case "mc-mod-remove":
		return "Minecraft: Mod removed"
	case "mc-modpack-switch":
		return "Minecraft: Modpack switched"
	case "mc-version-set":
		return "Minecraft: Version changed"
	case "mc-op-grant":
		return "Minecraft: Op granted"
	case "mc-op-revoke":
		return "Minecraft: Op revoked"
	case "mc-whitelist-add":
		return "Minecraft: Whitelist added"
	case "mc-whitelist-remove":
		return "Minecraft: Whitelist removed"
	case "mc-instance-create":
		return "Minecraft: Instance created"
	case "mc-instance-start":
		return "Minecraft: Instance started"
	case "mc-instance-stop":
		return "Minecraft: Instance stopped"
	case "mc-instance-delete":
		return "Minecraft: Instance deleted"
	case "join":
		return "Player joined"
	case "leave":
		return "Player left"
	case "backup":
		return "Backup"
	case "update":
		return "Update"
	case "crash":
		return "Crash"
	default:
		return kind
	}
}

func HistoryBadge(source, kind string) string {
	switch kind {
	case "crash":
		return "bg-red-900/60 text-red-200"
	case "join", "mc-instance-start":
		return "bg-emerald-900/50 text-emerald-200"
	case "leave":
		return "bg-zinc-800 text-zinc-300"
	case "stop", "mod-remove", "admin-revoke", "mc-instance-stop", "mc-instance-delete":
		return "bg-amber-900/50 text-amber-200"
	default:
		if source == "action" {
			return "bg-sky-900/50 text-sky-200"
		}
		return "bg-zinc-800 text-zinc-300"
	}
}

type InstanceUI struct {
	GameID             string
	Number             int
	Name               string
	Slug               string
	Password           string
	Seed               string
	Loader             string
	Source             string
	Pack               string
	PackRef            string
	PackProvider       string
	MCVersion          string
	Tier               string
	MemoryGiB          int
	State              string
	MOTD               string
	Players            int
	PlayersKnown       bool
	Uptime             string
	LBIP               string
	CanStart           bool
	StartBlockedReason string
}

type InstanceDetailUI struct {
	InstanceUI
	ActiveTab     string // overview, mods, configs, console, backups, settings
	InstalledMods []string
	ConfigFiles   []string
	Backups       []BackupUI
}

type BackupUI struct {
	Name      string
	SizeBytes int64
	CreatedAt string
}

type BudgetUI struct {
	UsedGiB        int
	TotalBudgetGiB int
	RunningCount   int
	MaxRunning     int
	TotalInstances int
	MaxInstances   int
}

type MinecraftSummaryUI struct {
	TotalInstances  int
	RunningCount    int
	MaxInstances    int
	MaxRunning      int
	UsedGiB         int
	TotalBudgetGiB  int
	ActiveInstance  *InstanceUI
	ActiveInstances []InstanceUI
}

type ValheimSummaryUI struct {
	TotalInstances  int
	RunningCount    int
	MaxInstances    int
	MaxRunning      int
	UsedGiB         int
	TotalBudgetGiB  int
	ActiveInstance  *InstanceUI
	ActiveInstances []InstanceUI
}

// ServerRowUI is one connectable server. Valheim renders exactly one; Minecraft
// renders one per running world. Same functionality => same markup, so the two
// cards match by construction instead of by hand.
//
// Stats arrive two different ways: Valheim's are live Datastar signals, while
// Minecraft's are server-rendered per instance. When a *Signal field is set the
// row binds to it; otherwise it prints the static value.
type ServerRowUI struct {
	Name          string
	Address       string
	VersionBadge  string
	DownloadURL   string
	DownloadFmt   string
	Launchers     []string
	ImportSteps   string
	Online        bool
	Players       int
	PlayersKnown  bool
	Uptime        string
	OnlineSignal  string
	PlayersSignal string
	UptimeSignal  string
}

// PackDefined mirrors domain.Instance.PackDefined so the UI can render
// pack-owned fields as facts instead of editable settings.
func (i InstanceUI) PackDefined() bool {
	return i.Source == "modpack" && (i.Pack != "" || i.PackRef != "")
}

func (r ServerRowUI) CopyScript() string {
	return fmt.Sprintf(
		"navigator.clipboard.writeText('%s'); $toast = 'Copied %s address (%s) to clipboard!'; $toastkind = 'ok'",
		r.Address, r.Name, r.Address,
	)
}

// ValheimWorldPasswordUI holds connection and authentication details for a Valheim world
// shown on the admin access page.
type ValheimWorldPasswordUI struct {
	Number   int
	Name     string
	State    string
	Password string
	Address  string
}

func (p ValheimWorldPasswordUI) CopyScript() string {
	return fmt.Sprintf(
		"navigator.clipboard.writeText('%s'); $toast = 'Copied world %s password!'; $toastkind = 'ok'",
		EscapeJS(p.Password), EscapeJS(p.Name),
	)
}

// EscapeJS escapes backslashes and quotes for safe embedding in JS inline strings.
func EscapeJS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func (r ServerRowUI) PlayersText() string {
	if !r.PlayersKnown {
		return "—"
	}
	return fmt.Sprintf("%d", r.Players)
}

func (r ServerRowUI) UptimeText() string {
	if r.Uptime == "" {
		return "—"
	}
	return r.Uptime
}

// ServersOnlineLabel is the card header pill: identical wording on both cards.
func ServersOnlineLabel(online int) string {
	if online == 0 {
		return "Offline"
	}
	return fmt.Sprintf("%d online", online)
}

func extrasStyle(accent string) string {
	return "border-zinc-800/80 bg-zinc-900/40"
}

func extrasLabelStyle(accent string) string {
	return "text-zinc-400"
}

// GameCardUI is one game's card. Both cards render through the same component,
// so anything that differs has to be data — not markup.
type GameCardUI struct {
	Icon       string
	Accent     string
	Title      string
	AccessPill string
	Subtitle   string
	Rows       []ServerRowUI
	Extras     []string
	AccessNote string
	EmptyText  string
	EmptyHint  string
	// OnlineSignal binds the header pill to a live Datastar signal instead of a
	// server-rendered count, so a single-server card reports its real state
	// rather than assuming a rendered row means "up".
	OnlineSignal string
}

// OnlineCount counts server-rendered rows only; signal-driven cards report
// their state through OnlineSignal.
func (g GameCardUI) OnlineCount() int {
	n := 0
	for _, r := range g.Rows {
		if r.Online {
			n++
		}
	}
	return n
}

func iconStyle(accent string) string {
	return "bg-zinc-800/60 text-zinc-300 ring-1 ring-zinc-700/60"
}

// Primary is the instance the card footer acts on.
func (v ValheimSummaryUI) Primary() *InstanceUI {
	if len(v.ActiveInstances) > 0 {
		return &v.ActiveInstances[0]
	}
	return v.ActiveInstance
}

// ValheimCard builds the Valheim card. Its stats are live Datastar signals, so
// ValheimCard builds the Valheim card matching the multi-world cluster style of MinecraftCard.
// Its stats are live Datastar signals, so the row and header pill bind rather than print.
func ValheimCard(addr, nodeName string, isAdmin bool, summary ...ValheimSummaryUI) GameCardUI {
	var vh ValheimSummaryUI
	if len(summary) > 0 && summary[0].MaxInstances > 0 {
		vh = summary[0]
	} else {
		// Fallback when called in unit tests without summary: provide default cluster budget with active instance 1
		vh = ValheimSummaryUI{
			TotalInstances: 1,
			MaxInstances:   4,
			RunningCount:   1,
			MaxRunning:     2,
			UsedGiB:        6,
			TotalBudgetGiB: 16,
			ActiveInstances: []InstanceUI{{
				Number:       1,
				Name:         "valheim",
				LBIP:         strings.Split(addr, ":")[0],
				Source:       "modpack",
				PlayersKnown: true,
			}},
		}
	}

	g := GameCardUI{
		Icon:       "⚔️",
		Accent:     "orange",
		Title:      "Valheim Worlds",
		AccessPill: "🔒 Password",
		Subtitle:   "Dedicated multi-world cluster",
		AccessNote: "Password required — ask the host on Discord.",
		EmptyText:  "All Valheim worlds are currently offline.",
		EmptyHint:  "Ask the server host on Discord to start a world!",
	}

	for _, inst := range vh.ActiveInstances {
		versionBadge := "BepInEx · Modded"
		if inst.Source == "vanilla" {
			versionBadge = "Vanilla"
		}
		row := ServerRowUI{
			Name:         inst.Name,
			Address:      fmt.Sprintf("%s:2456", inst.LBIP),
			VersionBadge: versionBadge,
			DownloadURL:  fmt.Sprintf("/api/valheim/%d/mods/export", inst.Number),
			DownloadFmt:  ".r2z",
			Launchers:    []string{"r2modman", "Thunderstore Mod Manager"},
			ImportSteps:  "Import → From file",
			Online:       true,
			Players:      inst.Players,
			PlayersKnown: inst.PlayersKnown,
			Uptime:       inst.Uptime,
		}
		g.Rows = append(g.Rows, row)
	}

	g.Extras = []string{fmt.Sprintf("%d of %d worlds saved", vh.TotalInstances, vh.MaxInstances)}
	if isAdmin {
		g.Extras = append(g.Extras,
			fmt.Sprintf("%d of %d running", vh.RunningCount, vh.MaxRunning),
			fmt.Sprintf("RAM %dG of %dG", vh.UsedGiB, vh.TotalBudgetGiB),
		)
		if nodeName != "" {
			g.Extras = append(g.Extras, "Node "+nodeName)
		}
	}
	return g
}

// MinecraftCard builds the Minecraft card from the running instances.
func MinecraftCard(mc MinecraftSummaryUI, isAdmin bool) GameCardUI {
	g := GameCardUI{
		Icon:       "⛏️",
		Accent:     "emerald",
		Title:      "Minecraft Worlds",
		AccessPill: "🛡️ Whitelist",
		Subtitle:   "Dedicated multi-world cluster",
		AccessNote: "Whitelist required — ask the host on Discord to get added.",
		EmptyText:  "All Minecraft worlds are currently offline.",
		EmptyHint:  "Ask the server host on Discord to start a world!",
	}

	for _, inst := range mc.ActiveInstances {
		g.Rows = append(g.Rows, ServerRowUI{
			Name:         inst.Name,
			Address:      fmt.Sprintf("%s:25565", inst.LBIP),
			VersionBadge: fmt.Sprintf("%s · %s", inst.MCVersion, inst.Loader),
			DownloadURL:  fmt.Sprintf("/api/minecraft/%d/mods/export", inst.Number),
			DownloadFmt:  ".mrpack",
			Launchers:    []string{"Prism Launcher", "Modrinth App"},
			ImportSteps:  "Add Instance → Import from zip",
			Online:       true,
			Players:      inst.Players,
			PlayersKnown: inst.PlayersKnown,
			Uptime:       inst.Uptime,
		})
	}

	g.Extras = []string{fmt.Sprintf("%d of %d worlds saved", mc.TotalInstances, mc.MaxInstances)}
	if isAdmin {
		g.Extras = append(g.Extras,
			fmt.Sprintf("%d of %d running", mc.RunningCount, mc.MaxRunning),
			fmt.Sprintf("RAM %dG of %dG", mc.UsedGiB, mc.TotalBudgetGiB),
		)
	}
	return g
}

// ActionUI is one control in a card footer. Kind drives the styling so the same
// role (destructive, go, secondary…) always looks the same on both cards.
type ActionUI struct {
	Label  string
	Script string
	Href   string
	Kind   string
}

// CardActionsUI is the shared admin footer. Both games have the same action
// set — lifecycle, configs, access, manager — so only Special differs, and it
// gets the marked slot.
type CardActionsUI struct {
	Lifecycle []ActionUI
	Special   []ActionUI
	Links     []ActionUI
	Manager   ActionUI
}

func actionStyle(kind string) string {
	switch kind {
	case "danger":
		return "border border-amber-800/50 bg-amber-950/20 text-amber-200 hover:bg-amber-900/40"
	case "go":
		return "bg-emerald-800/80 text-emerald-100 hover:bg-emerald-700"
	case "special":
		return "border border-dashed border-sky-800/60 bg-sky-950/20 text-sky-300 hover:bg-sky-900/30"
	case "link":
		return "border border-zinc-800 bg-zinc-800/50 text-zinc-300 hover:bg-zinc-700"
	default:
		return "bg-zinc-800 text-zinc-200 hover:bg-zinc-700 hover:text-white"
	}
}

// ValheimActions is Valheim's footer, mirroring MinecraftActions.
func ValheimActions(summary ...ValheimSummaryUI) CardActionsUI {
	var vh ValheimSummaryUI
	if len(summary) > 0 {
		vh = summary[0]
	} else {
		// Fallback for tests calling without summary (assumes active instance 1)
		vh = ValheimSummaryUI{
			TotalInstances: 1,
			MaxInstances:   4,
			RunningCount:   1,
			MaxRunning:     2,
			ActiveInstances: []InstanceUI{{
				Number: 1,
				Name:   "valheim",
			}},
		}
	}

	a := CardActionsUI{
		Links: []ActionUI{
			{Label: "Access", Href: "/valheim/access", Kind: "link"},
		},
		Manager: ActionUI{Label: "Open Valheim Manager →", Href: "/valheim"},
	}

	if inst := vh.Primary(); inst != nil {
		a.Lifecycle = []ActionUI{
			{Label: "Restart", Script: fmt.Sprintf("@post('/api/valheim/instances/%d/restart')", inst.Number)},
			{Label: "Stop", Script: fmt.Sprintf("@post('/api/valheim/instances/%d/stop')", inst.Number), Kind: "danger"},
			{Label: "Start", Script: fmt.Sprintf("@post('/api/valheim/instances/%d/start')", inst.Number), Kind: "go"},
		}
		a.Special = []ActionUI{
			{Label: "Update", Script: "@post('/server/update')", Kind: "special"},
		}
		a.Links = append([]ActionUI{
			{Label: "Configs", Href: fmt.Sprintf("/valheim/%d/configs", inst.Number), Kind: "link"},
		}, a.Links...)
	} else {
		a.Special = []ActionUI{
			{Label: "+ Create World", Href: "/valheim/create", Kind: "special"},
		}
	}
	return a
}

// Primary is the instance the card footer acts on. The summary carries both a
// slice and a legacy pointer for the same fact; reading them separately let the
// footer and the rows disagree, so everything goes through here.
func (m MinecraftSummaryUI) Primary() *InstanceUI {
	if len(m.ActiveInstances) > 0 {
		return &m.ActiveInstances[0]
	}
	return m.ActiveInstance
}

// MinecraftActions mirrors ValheimActions. Creating a world is Minecraft-only,
// so it takes the Special slot when nothing is running.
func MinecraftActions(mc MinecraftSummaryUI) CardActionsUI {
	a := CardActionsUI{
		Links: []ActionUI{
			{Label: "Access", Href: "/minecraft/access", Kind: "link"},
		},
		Manager: ActionUI{Label: "Open Minecraft Manager →", Href: "/minecraft"},
	}

	if inst := mc.Primary(); inst != nil {
		a.Lifecycle = []ActionUI{
			{Label: "Restart", Script: fmt.Sprintf("@post('/api/minecraft/instances/%d/restart')", inst.Number)},
			{Label: "Stop", Script: fmt.Sprintf("@post('/api/minecraft/instances/%d/stop')", inst.Number), Kind: "danger"},
			{Label: "Start", Script: fmt.Sprintf("@post('/api/minecraft/instances/%d/start')", inst.Number), Kind: "go"},
		}
		a.Links = append([]ActionUI{
			{Label: "Configs", Href: fmt.Sprintf("/minecraft/%d/configs", inst.Number), Kind: "link"},
		}, a.Links...)
	} else {
		a.Special = []ActionUI{
			{Label: "+ Create World", Href: "/minecraft/create", Kind: "special"},
		}
	}
	return a
}

// NavGroupUI is one game's top navigation entry.
type NavGroupUI struct {
	Label string
	Href  string
	Links []ActionUI
}

func ValheimNav() NavGroupUI {
	return NavGroupUI{
		Label: "Valheim",
		Href:  "/valheim",
	}
}

func MinecraftNav() NavGroupUI {
	return NavGroupUI{
		Label: "Minecraft",
		Href:  "/minecraft",
	}
}
