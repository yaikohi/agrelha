package pages

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
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
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

type SlotUI struct {
	Slot         string
	Loader       string
	Source       string
	Pack         string
	PackProvider string
	MCVersion    string
	Active       bool
	LastUsed     string
}

type InstanceUI struct {
	Number             int
	Name               string
	Slug               string
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
	LBIP               string
	CanStart           bool
	StartBlockedReason string
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

// SlotEngine describes a slot the way the domain does: the loader is what runs,
// and the source (with its provider) is how the content got there. CurseForge is
// a distributor, never an engine.
func SlotEngine(loader, source, provider string) string {
	name := "NeoForge"
	if loader == "fabric" {
		name = "Fabric"
	}
	switch source {
	case "modpack":
		if provider != "" {
			return fmt.Sprintf("%s · %s pack", name, provider)
		}
		return name + " · modpack"
	case "vanilla":
		return "Vanilla"
	default:
		return name + " · mod list"
	}
}

func slotRowStyle(active bool) string {
	if active {
		return "border-emerald-800/50 bg-emerald-950/20"
	}
	return "border-zinc-800 bg-zinc-950"
}
