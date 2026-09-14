package domain

import (
	"strings"
	"testing"
	"time"
)

func TestBudget(t *testing.T) {
	inst1 := Instance{Number: 1, Name: "Large", Tier: TierLarge, State: StateRunning}   // 12 GiB
	inst2 := Instance{Number: 2, Name: "Medium", Tier: TierMedium, State: StateRunning} // 8 GiB
	inst3 := Instance{Number: 3, Name: "Small", Tier: TierSmall, State: StateStopped}   // 4 GiB

	// Budget: 24 GiB total, max 2 running, max 4 instances
	b := CalculateBudget([]Instance{inst1, inst2, inst3}, 24, 2, 4)
	if b.UsedGiB != 20 {
		t.Fatalf("UsedGiB = %d, want 20", b.UsedGiB)
	}
	if b.RunningCount != 2 {
		t.Fatalf("RunningCount = %d, want 2", b.RunningCount)
	}

	// 1. CanStart fails due to maxRunning
	if err := b.CanStart(inst3); err == nil || !strings.Contains(err.Error(), "maximum of 2 running instances reached") {
		t.Fatalf("expected maxRunning error, got %v", err)
	}

	// 2. CanStart fails due to RAM budget
	b2 := CalculateBudget([]Instance{inst1}, 14, 2, 4) // 12 used of 14
	if err := b2.CanStart(inst3); err == nil || !strings.Contains(err.Error(), "RAM budget exceeded") {
		t.Fatalf("expected RAM budget error, got %v", err)
	}

	// 3. CanStart succeeds
	b3 := CalculateBudget([]Instance{inst1}, 24, 2, 4) // 12 used of 24, 1 running
	if err := b3.CanStart(inst3); err != nil {
		t.Fatalf("expected CanStart to succeed, got %v", err)
	}

	// 4. CanCreate
	bFull := CalculateBudget([]Instance{inst1, inst2, inst3, inst3}, 24, 2, 4)
	if err := bFull.CanCreate(); err == nil || !strings.Contains(err.Error(), "maximum limit of 4 instances reached") {
		t.Fatalf("expected CanCreate error when full, got %v", err)
	}
}

func TestResourceTier(t *testing.T) {
	if TierSmall.MemoryGiB() != 4 || TierSmall.MemoryLimitGiB() != 6 || TierSmall.HeapInitMemoryGiB() != 3 {
		t.Errorf("TierSmall unexpected memory values")
	}
	if TierMedium.MemoryGiB() != 8 || TierMedium.MemoryLimitGiB() != 10 || TierMedium.HeapInitMemoryGiB() != 6 {
		t.Errorf("TierMedium unexpected memory values")
	}
	if TierLarge.MemoryGiB() != 12 || TierLarge.MemoryLimitGiB() != 16 || TierLarge.HeapInitMemoryGiB() != 10 {
		t.Errorf("TierLarge unexpected memory values")
	}
	if NormalizeTier("  SMALL  ") != TierSmall {
		t.Errorf("NormalizeTier SMALL failed")
	}
	if NormalizeTier("large") != TierLarge {
		t.Errorf("NormalizeTier large failed")
	}
	if NormalizeTier("unknown") != TierMedium {
		t.Errorf("NormalizeTier unknown fallback failed")
	}
}

func TestPackInvariantsAndSlugify(t *testing.T) {
	if got := Slugify(" My Awesome Server! 2026 "); got != "my-awesome-server-2026" {
		t.Errorf("Slugify failed, got %q", got)
	}
	if got := Slugify(""); got != "default" {
		t.Errorf("Slugify empty failed, got %q", got)
	}

	pack := &Pack{Provider: ProviderCurseForge, Ref: "ref-1", Name: "ATM9"}
	inst := Instance{Source: SourceModpack, Pack: pack}
	if !inst.PackDefined() {
		t.Errorf("expected PackDefined=true")
	}
	if inst.CanSetLoader() || inst.CanSetVersion() {
		t.Errorf("pack defined instance should not allow loader/version changes")
	}
	if err := inst.PackOwnedFieldErr("Loader"); err == nil || !strings.Contains(err.Error(), "ATM9") {
		t.Errorf("unexpected PackOwnedFieldErr: %v", err)
	}

	free := Instance{Source: SourceModlist}
	if !free.CanSetLoader() || !free.CanSetVersion() {
		t.Errorf("modlist instance should allow loader/version changes")
	}
}

func TestInstanceDefaultsAndEnv(t *testing.T) {
	inst := Instance{Name: "World Alpha", Number: 1}
	inst.EnsureDefaults("192.168.20.224")

	if inst.GameID != GameMinecraft {
		t.Errorf("GameID = %s, want minecraft", inst.GameID)
	}
	if inst.Slug != "world-alpha" {
		t.Errorf("Slug = %s, want world-alpha", inst.Slug)
	}
	if inst.Tier != TierMedium {
		t.Errorf("Tier = %s, want medium", inst.Tier)
	}
	if inst.State != StateStopped {
		t.Errorf("State = %s, want stopped", inst.State)
	}
	if inst.LBIP != "192.168.20.225" {
		t.Errorf("LBIP = %s, want 192.168.20.225", inst.LBIP)
	}

	env := inst.Env()
	if env["LEVEL"] != "world-alpha" {
		t.Errorf("LEVEL = %q, want world-alpha", env["LEVEL"])
	}
	if env["TYPE"] != "NEOFORGE" {
		t.Errorf("TYPE = %q, want NEOFORGE default", env["TYPE"])
	}
}

func TestInstanceAnnotationsAndNaming(t *testing.T) {
	inst := Instance{
		Name:      "Testing Server",
		Slug:      "testing-server",
		Number:    3,
		Loader:    LoaderFabric,
		Source:    SourceModpack,
		Pack:      &Pack{Provider: ProviderModrinth, Ref: "modrinth-ref", Name: "Fabulously Optimized"},
		MCVersion: "1.21.1",
		Tier:      TierLarge,
		Seed:      "424242",
	}

	if inst.DeploymentName() != "mc-testing-server-03" {
		t.Errorf("DeploymentName = %q", inst.DeploymentName())
	}
	if inst.ServiceName() != "mc-testing-server-03" {
		t.Errorf("ServiceName = %q", inst.ServiceName())
	}
	if inst.PVCName() != "mc-instance-03-data" {
		t.Errorf("PVCName = %q", inst.PVCName())
	}
	if inst.ConfigCMName() != "mc-testing-server-03-slot" {
		t.Errorf("ConfigCMName = %q", inst.ConfigCMName())
	}
	if inst.ModsCMName() != "mc-testing-server-03-mods" {
		t.Errorf("ModsCMName = %q", inst.ModsCMName())
	}
	if inst.ConfigsCMName() != "mc-testing-server-03-configs" {
		t.Errorf("ConfigsCMName = %q", inst.ConfigsCMName())
	}

	ann := inst.Annotations()
	if ann["agrelha.dev/instance-number"] != "3" {
		t.Errorf("ann instance-number = %q", ann["agrelha.dev/instance-number"])
	}
	if ann["agrelha.dev/pack-provider"] != "modrinth" {
		t.Errorf("ann pack-provider = %q", ann["agrelha.dev/pack-provider"])
	}
	if ann["agrelha.dev/pack-ref"] != "modrinth-ref" {
		t.Errorf("ann pack-ref = %q", ann["agrelha.dev/pack-ref"])
	}
	if ann["agrelha.dev/pack-name"] != "Fabulously Optimized" {
		t.Errorf("ann pack-name = %q", ann["agrelha.dev/pack-name"])
	}

	env := inst.Env()
	if env["TYPE"] != "MODRINTH" || env["MODRINTH_MODPACK"] != "modrinth-ref" {
		t.Errorf("modrinth env unexpected: %v", env)
	}

	vanillaInst := Instance{
		Name:      "Vanilla",
		Slug:      "vanilla",
		Number:    4,
		Source:    SourceVanilla,
		MCVersion: "1.21.4",
	}
	vanillaEnv := vanillaInst.Env()
	if vanillaEnv["TYPE"] != "VANILLA" || vanillaEnv["VERSION"] != "1.21.4" {
		t.Errorf("vanilla env unexpected: %v", vanillaEnv)
	}
}

func TestAccessParsingAndNormalizers(t *testing.T) {
	raw := `
# This is a comment
user1
user2 # inline comment without space handled
  user3  

`
	users := ParseUsers(raw)
	if len(users) != 3 || users[0] != "user1" || users[1] != "user2 # inline comment without space handled" || users[2] != "user3" {
		t.Errorf("ParseUsers result unexpected: %v", users)
	}

	fields := Fields(" steam_id_1   steam_id_2  \t steam_id_3\n")
	if len(fields) != 3 || fields[0] != "steam_id_1" {
		t.Errorf("Fields unexpected: %v", fields)
	}

	if NormalizeLoader("FABRIC") != LoaderFabric {
		t.Errorf("NormalizeLoader FABRIC failed")
	}
	if NormalizeLoader("something_else") != LoaderNeoForge {
		t.Errorf("NormalizeLoader fallback failed")
	}

	if NormalizeSource("MODPACK") != SourceModpack {
		t.Errorf("NormalizeSource MODPACK failed")
	}
	if NormalizeSource("VANILLA") != SourceVanilla {
		t.Errorf("NormalizeSource VANILLA failed")
	}
	if NormalizeSource("other") != SourceModlist {
		t.Errorf("NormalizeSource fallback failed")
	}

	// Budget.AddUsage
	b := Budget{UsedGiB: 8, RunningCount: 1}
	b.AddUsage(1, 4)
	if b.UsedGiB != 12 || b.RunningCount != 2 {
		t.Errorf("AddUsage unexpected: %v", b)
	}

	// AssignLBIP
	if AssignLBIP("", 1) != "" {
		t.Errorf("AssignLBIP empty base failed")
	}
	if AssignLBIP("192.168.1.100", 0) != "192.168.1.100" {
		t.Errorf("AssignLBIP number <= 0 failed")
	}
	if AssignLBIP("192.168.1.100", 5) != "192.168.1.105" {
		t.Errorf("AssignLBIP failed, got %s", AssignLBIP("192.168.1.100", 5))
	}
}

func TestValheimInstance(t *testing.T) {
	vInst := Instance{
		GameID:   GameValheim,
		Name:     "Viking World",
		Number:   1,
		Password: "secretpassword",
		Seed:     "valheimseed123",
		Tier:     TierSmall,
	}
	vInst.EnsureDefaults("192.168.20.210")

	if vInst.GameID != GameValheim {
		t.Fatalf("GameID = %s, want valheim", vInst.GameID)
	}
	if vInst.Slug != "viking-world" {
		t.Fatalf("Slug = %s, want viking-world", vInst.Slug)
	}
	if vInst.MaxPlayers != 10 {
		t.Fatalf("MaxPlayers = %d, want 10", vInst.MaxPlayers)
	}
	if vInst.LBIP != "192.168.20.211" {
		t.Fatalf("LBIP = %s, want 192.168.20.211", vInst.LBIP)
	}

	// Memory calculations for Valheim
	if vInst.MemoryGiB() != 4 || vInst.MemoryLimitGiB() != 5 || vInst.HeapInitMemoryGiB() != 0 {
		t.Fatalf("Valheim TierSmall unexpected memory: %d, %d, %d", vInst.MemoryGiB(), vInst.MemoryLimitGiB(), vInst.HeapInitMemoryGiB())
	}

	vInstMed := Instance{GameID: GameValheim, Tier: TierMedium}
	if vInstMed.MemoryGiB() != 6 || vInstMed.MemoryLimitGiB() != 7 {
		t.Fatalf("Valheim TierMedium unexpected memory: %d, %d", vInstMed.MemoryGiB(), vInstMed.MemoryLimitGiB())
	}

	vInstLarge := Instance{GameID: GameValheim, Tier: TierLarge}
	if vInstLarge.MemoryGiB() != 8 || vInstLarge.MemoryLimitGiB() != 10 {
		t.Fatalf("Valheim TierLarge unexpected memory: %d, %d", vInstLarge.MemoryGiB(), vInstLarge.MemoryLimitGiB())
	}

	// Naming
	if vInst.DeploymentName() != "valheim-viking-world-01" {
		t.Errorf("DeploymentName = %q, want valheim-viking-world-01", vInst.DeploymentName())
	}
	if vInst.ServiceName() != "valheim-viking-world-01" {
		t.Errorf("ServiceName = %q, want valheim-viking-world-01", vInst.ServiceName())
	}
	if vInst.PVCName() != "valheim-instance-01-data" {
		t.Errorf("PVCName = %q, want valheim-instance-01-data", vInst.PVCName())
	}
	if vInst.ConfigCMName() != "valheim-viking-world-01-slot" {
		t.Errorf("ConfigCMName = %q, want valheim-viking-world-01-slot", vInst.ConfigCMName())
	}
	if vInst.ModsCMName() != "valheim-viking-world-01-mods" {
		t.Errorf("ModsCMName = %q, want valheim-viking-world-01-mods", vInst.ModsCMName())
	}
	if vInst.ConfigsCMName() != "valheim-viking-world-01-configs" {
		t.Errorf("ConfigsCMName = %q, want valheim-viking-world-01-configs", vInst.ConfigsCMName())
	}

	// Env
	env := vInst.Env()
	if env["SERVER_NAME"] != "Viking World" || env["WORLD_NAME"] != "viking-world" || env["SERVER_PASS"] != "secretpassword" {
		t.Errorf("Valheim Env unexpected: %v", env)
	}
	if env["BEPINEX"] != "true" || env["WORLD_SEED"] != "valheimseed123" {
		t.Errorf("Valheim Env missing bepinex/seed: %v", env)
	}

	// Annotations
	ann := vInst.Annotations()
	if ann["agrelha.dev/game"] != "valheim" || ann["agrelha.dev/instance-number"] != "1" || ann["agrelha.dev/seed"] != "valheimseed123" {
		t.Errorf("Valheim Annotations unexpected: %v", ann)
	}

	// Backup file naming
	bkpName := FormatGameBackupFileName(GameValheim, "viking-world", 1, "manual")
	if !strings.HasPrefix(bkpName, "valheim-viking-world-01-manual-") || !strings.HasSuffix(bkpName, ".tar.gz") {
		t.Errorf("Valheim backup name unexpected: %s", bkpName)
	}
	if !IsSafeBackupFileName(bkpName) {
		t.Errorf("IsSafeBackupFileName rejected valid Valheim backup: %s", bkpName)
	}
}

func TestServiceNameMatchesDeploymentName(t *testing.T) {
	for _, inst := range []Instance{
		{GameID: GameMinecraft, Slug: "bob", Number: 3},
		{GameID: GameValheim, Slug: "boppo", Number: 2},
	} {
		if inst.ServiceName() != inst.DeploymentName() {
			t.Errorf("%s: ServiceName %q != DeploymentName %q — the runtime looks a Service up by the deployment name in ServerRef, so divergence makes every address read as unallocated",
				inst.GameID, inst.ServiceName(), inst.DeploymentName())
		}
	}
}

func TestParseModRefHandlesBothGames(t *testing.T) {
	cases := []struct {
		entry string
		game  GameID
		key   string
		full  string
		ver   string
	}{
		{"Neobotics-SlayerSkills", GameValheim, "neobotics-slayerskills", "Neobotics-SlayerSkills", ""},
		{"Neobotics/SlayerSkills/1.2.0", GameValheim, "neobotics-slayerskills", "Neobotics-SlayerSkills", "1.2.0"},
		{"Smoothbrain-Mining?", GameValheim, "smoothbrain-mining", "Smoothbrain-Mining", ""},
		{"cloth-config", GameMinecraft, "cloth-config", "cloth-config", ""},
		{"xaeros-minimap", GameMinecraft, "xaeros-minimap", "xaeros-minimap", ""},
		{"sodium", GameMinecraft, "sodium", "sodium", ""},
		{"sodium:0.6.0+mc1.21.1", GameMinecraft, "sodium", "sodium", "0.6.0+mc1.21.1"},
		{"fabric-api/0.100.0+1.21.1", GameMinecraft, "fabric-api", "fabric-api", "0.100.0+1.21.1"},
	}
	for _, c := range cases {
		ref, ok := ParseModRef(c.entry, c.game)
		if !ok {
			t.Errorf("%q: not parsed", c.entry)
			continue
		}
		if ref.Key() != c.key {
			t.Errorf("%q key = %q, want %q", c.entry, ref.Key(), c.key)
		}
		if ref.FullName() != c.full {
			t.Errorf("%q full = %q, want %q", c.entry, ref.FullName(), c.full)
		}
		if ref.Version != c.ver {
			t.Errorf("%q version = %q, want %q", c.entry, ref.Version, c.ver)
		}
	}

	for _, skip := range []string{"", "   ", "# a comment"} {
		if _, ok := ParseModRef(skip, GameValheim); ok {
			t.Errorf("%q must not parse as a mod", skip)
		}
	}
}

func TestModRefEntryPinsWhenVersionKnown(t *testing.T) {
	if got := (ModRef{Namespace: "Smoothbrain", Name: "Mining", Version: "1.1.6"}).Entry(); got != "Smoothbrain/Mining/1.1.6" {
		t.Errorf("got %q", got)
	}
	if got := (ModRef{Namespace: "Smoothbrain", Name: "Mining"}).Entry(); got != "Smoothbrain-Mining" {
		t.Errorf("got %q", got)
	}
	if got := (ModRef{Name: "sodium"}).Entry(); got != "sodium" {
		t.Errorf("got %q", got)
	}
	if got := (ModRef{Name: "sodium", Version: "0.6.0+mc1.21.1"}).Entry(); got != "sodium:0.6.0+mc1.21.1" {
		t.Errorf("got %q, want sodium:0.6.0+mc1.21.1", got)
	}
}

func TestVersionNewerSemverAndMetadata(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.6.1+mc1.21.1", "0.6.0+mc1.21.1", true},
		{"0.6.0+mc1.21.1", "0.5.11+mc1.21", true},
		{"0.6.0+mc1.21.1", "0.6.0+mc1.21.1", false},
		{"v1.2.3", "1.2.0", true},
		{"1.2.0", "v1.2.3", false},
		{"1.0.0", "(unpinned)", true},
		{"1.0.0", "", true},
		{"", "1.0.0", false},
		{"1.0.0", "1.0.0-beta.1", true},
		{"1.0.0-beta.2", "1.0.0-beta.1", true},
		{"1.0.0-beta.1", "1.0.0", false},
	}
	for _, c := range cases {
		if got := VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("VersionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVanillaInstanceRefusesMods(t *testing.T) {
	vanilla := Instance{GameID: GameValheim, Name: "lareira-V2", Source: SourceVanilla}
	if vanilla.CanInstallMods() {
		t.Error("a vanilla world must refuse mods: gaining them revokes achievements its players earned")
	}
	if vanilla.VanillaImmutableErr() == nil {
		t.Error("the refusal must explain itself")
	}
	if got := vanilla.Env()["BEPINEX"]; got != "false" {
		t.Errorf("vanilla must run without BepInEx, got BEPINEX=%q", got)
	}

	modded := Instance{GameID: GameValheim, Name: "boppo", Source: SourceModlist}
	if !modded.CanInstallMods() {
		t.Error("a modded world accepts mods")
	}
	if got := modded.Env()["BEPINEX"]; got != "true" {
		t.Errorf("modded must run BepInEx, got BEPINEX=%q", got)
	}
}

func TestParseModRefAcceptsThunderstoresVersionedID(t *testing.T) {
	cases := []struct{ entry, ns, name, ver string }{
		{"blacks7ar-BowPlugin-1.8.7", "blacks7ar", "BowPlugin", "1.8.7"},
		{"ArgusMagnus-ServersideQoL_AutoStore-2.0.8", "ArgusMagnus", "ServersideQoL_AutoStore", "2.0.8"},
		{"Neobotics-SlayerSkills", "Neobotics", "SlayerSkills", ""},
		{"Neobotics/SlayerSkills/1.2.0", "Neobotics", "SlayerSkills", "1.2.0"},
	}
	for _, c := range cases {
		ref, ok := ParseModRef(c.entry, GameValheim)
		if !ok || ref.Namespace != c.ns || ref.Name != c.name || ref.Version != c.ver {
			t.Errorf("ParseModRef(%q) = %+v, want {%s %s %s}", c.entry, ref, c.ns, c.name, c.ver)
		}
	}
	// All three spellings are the same mod.
	a, _ := ParseModRef("blacks7ar-BowPlugin-1.8.7", GameValheim)
	b, _ := ParseModRef("blacks7ar-BowPlugin", GameValheim)
	c, _ := ParseModRef("blacks7ar/BowPlugin/1.8.7", GameValheim)
	if a.Key() != b.Key() || b.Key() != c.Key() {
		t.Errorf("same mod must share an identity: %q %q %q", a.Key(), b.Key(), c.Key())
	}
	// A Minecraft slug that merely looks versioned must not be split.
	if ref, _ := ParseModRef("cloth-config", GameMinecraft); ref.Name != "cloth-config" {
		t.Errorf("minecraft slug split: %+v", ref)
	}
}

func TestIncidentSummaryDistinguishesSetupFromCrash(t *testing.T) {
	setup := Incident{Step: "mod-reconciler", RestartCount: 5}
	if got := setup.Summary(); !strings.Contains(got, "Mod install") || !strings.Contains(got, "never started") {
		t.Errorf("a failed mod install must not read as a game crash: %q", got)
	}

	crash := Incident{RestartCount: 5, Reason: "CrashLoopBackOff"}
	if got := crash.Summary(); strings.Contains(got, "never started") {
		t.Errorf("a real crash must not claim the server never started: %q", got)
	}
	if setup.Summary() == crash.Summary() {
		t.Error("the two failures need different fixes and must read differently")
	}
}

func TestDomainEdgeCasesAndHelpers(t *testing.T) {
	// 1. Budget defaults and CanCreate
	bZero := CalculateBudget(nil, 0, 0, 0)
	if bZero.TotalBudgetGiB != DefaultTotalBudgetGiB || bZero.MaxRunning != DefaultMaxRunning || bZero.MaxInstances != DefaultMaxInstances {
		t.Errorf("expected default budget values, got %+v", bZero)
	}
	bNotFull := CalculateBudget(nil, 24, 2, 4)
	if err := bNotFull.CanCreate(); err != nil {
		t.Errorf("CanCreate should succeed when not full, got: %v", err)
	}

	// 2. FormatDuration
	if d := FormatDuration(48 * time.Hour + 3 * time.Hour); d != "2d 3h" {
		t.Errorf("expected 2d 3h, got %s", d)
	}
	if d := FormatDuration(2 * time.Hour + 15 * time.Minute); d != "2h 15m" {
		t.Errorf("expected 2h 15m, got %s", d)
	}
	if d := FormatDuration(45 * time.Minute); d != "45m" {
		t.Errorf("expected 45m, got %s", d)
	}

	// 3. Health Incident summaries
	incidents := []struct {
		inc  Incident
		want string
	}{
		{Incident{OOMKilled: true}, "Out of memory"},
		{Incident{ExitCode: 137}, "Exited with code 137"},
		{Incident{Reason: "Evicted"}, "Evicted"},
		{Incident{}, "Stopped unexpectedly"},
		{Incident{Step: "sync-configs", RestartCount: 0}, "Config sync failed — the server never started"},
		{Incident{Step: "custom-step", RestartCount: 2}, "custom-step failed — the server never started (2 attempts)"},
	}
	for _, tc := range incidents {
		if !strings.Contains(tc.inc.Summary(), tc.want) {
			t.Errorf("Summary() = %q, want containing %q", tc.inc.Summary(), tc.want)
		}
	}

	// 4. Instance memory and backup name helpers
	mcInst := Instance{GameID: GameMinecraft, Tier: TierMedium}
	if mcInst.MemoryLimitGiB() != TierMedium.MemoryLimitGiB() {
		t.Errorf("MemoryLimitGiB for mcInst = %d, want %d", mcInst.MemoryLimitGiB(), TierMedium.MemoryLimitGiB())
	}
	if mcInst.HeapInitMemoryGiB() != TierMedium.HeapInitMemoryGiB() {
		t.Errorf("HeapInitMemoryGiB for mcInst = %d, want %d", mcInst.HeapInitMemoryGiB(), TierMedium.HeapInitMemoryGiB())
	}

	// AssignLBIP invalid last octet
	if ip := AssignLBIP("192.168.1.invalid", 1); ip != "192.168.1.invalid" {
		t.Errorf("expected invalid IP to be returned as-is, got %s", ip)
	}

	// EnsureDefaults with empty LBIP
	noLB := Instance{Name: "Solo"}
	noLB.EnsureDefaults("")
	if noLB.MOTD != "Solo" {
		t.Errorf("expected MOTD Solo, got %s", noLB.MOTD)
	}

	// Level type and Curseforge pack env
	envInst := Instance{
		GameID:    GameMinecraft,
		Name:      "CF-Pack",
		Source:    SourceModpack,
		WorldType: "flat",
		Pack:      &Pack{Provider: ProviderCurseForge, Ref: "https://curseforge.com/modpack"},
		Loader:    LoaderFabric,
	}
	env := envInst.Env()
	if env["LEVEL_TYPE"] != "flat" || env["TYPE"] != "AUTO_CURSEFORGE" || env["CF_PAGE_URL"] != "https://curseforge.com/modpack" {
		t.Errorf("unexpected env: %+v", env)
	}

	// Fabric env in default loader branch
	fabricInst := Instance{
		GameID:    GameMinecraft,
		Name:      "FabricNormal",
		Source:    SourceModlist,
		Loader:    LoaderFabric,
		MCVersion: "1.21.1",
	}
	fEnv := fabricInst.Env()
	if fEnv["TYPE"] != "FABRIC" {
		t.Errorf("expected TYPE FABRIC, got %s", fEnv["TYPE"])
	}

	// Backup file naming
	if name := FormatBackupFileName("myserver", 1, ""); !strings.HasPrefix(name, "mc-myserver-01-") {
		t.Errorf("unexpected FormatBackupFileName: %s", name)
	}
	if name := FormatBackupFileName("myserver", 1, "manual"); !strings.Contains(name, "manual") {
		t.Errorf("unexpected tagged backup name: %s", name)
	}

	// Slugify > 40 chars
	longSlug := Slugify("This is a ridiculously long instance name that will exceed forty characters")
	if len(longSlug) > 40 {
		t.Errorf("Slugify should be at most 40 chars, got %d (%s)", len(longSlug), longSlug)
	}

	// ResourceTier unknown default
	unknownTier := ResourceTier("unknown")
	if unknownTier.MemoryGiB() != 8 {
		t.Errorf("unknown tier MemoryGiB = %d, want 8", unknownTier.MemoryGiB())
	}

	// ModSearchResult.FullName
	sr := ModSearchResult{Owner: "author", Name: "coolmod"}
	if sr.FullName() != "author/coolmod" {
		t.Errorf("FullName = %s, want author/coolmod", sr.FullName())
	}

	// ModRef.CatalogKey
	refBare := ModRef{Name: "jei"}
	if refBare.CatalogKey() != "jei" {
		t.Errorf("CatalogKey bare = %s, want jei", refBare.CatalogKey())
	}
	refNS := ModRef{Namespace: "author", Name: "coolmod"}
	if refNS.CatalogKey() != "author/coolmod" {
		t.Errorf("CatalogKey ns = %s, want author/coolmod", refNS.CatalogKey())
	}

	// VersionNewer build metadata comparison
	if !VersionNewer("1.0.0+build.2", "1.0.0+build.1") {
		t.Errorf("expected +build.2 newer than +build.1")
	}
	if VersionNewer("1.0.0+build.1", "1.0.0+build.2") {
		t.Errorf("did not expect +build.1 newer than +build.2")
	}
	if !VersionNewer("1.0.0+build.2.alpha", "1.0.0+build.1.beta") {
		t.Errorf("expected build.2 newer than build.1")
	}
	if !VersionNewer("1.0.0+build.1.beta", "1.0.0+build.1.alpha") {
		t.Errorf("expected beta newer than alpha")
	}
	if !VersionNewer("1.0.0+beta", "1.0.0+alpha") {
		t.Errorf("expected string comparison fallback beta > alpha")
	}
	if !VersionNewer("1.0.0-beta", "1.0.0-alpha") {
		t.Errorf("expected prerelease beta newer than alpha")
	}
	if !VersionNewer("1.0.0b", "1.0.0a") {
		t.Errorf("expected 1.0.0b newer than 1.0.0a")
	}

	// ModRestorePoint.Matches
	rp := ModRestorePoint{
		Applied: []string{"mod1", "mod2"},
	}
	if rp.Matches([]string{"mod1"}) {
		t.Errorf("Matches should be false for different length")
	}
	if !rp.Matches([]string{"mod2", "mod1"}) {
		t.Errorf("Matches should be true regardless of order")
	}
	if rp.Matches([]string{"mod1", "mod3"}) {
		t.Errorf("Matches should be false when elements differ")
	}

	// ParseModRef bare valheim entry without hyphen
	if entry, ok := ParseModRef("baremod", GameValheim); !ok || entry.Name != "baremod" {
		t.Errorf("expected ParseModRef baremod, got %+v", entry)
	}

	// ParseModRef isSemver false branches
	ParseModRef("ns-name-1.2", GameValheim)
	ParseModRef("ns-name-1..2", GameValheim)
	ParseModRef("ns-name-1.2.x", GameValheim)

	// isVersionNewer buildA > buildB tiebreak and line 274 return false
	if !VersionNewer("1.0.0+1.a", "1.0.0+1") {
		t.Errorf("expected build tiebreak 1.a > 1")
	}
	if VersionNewer("1.0.0", "v1.0.0") {
		t.Errorf("1.0.0 should not be newer than v1.0.0")
	}
}


