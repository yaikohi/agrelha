package domain

import (
	"strings"
	"testing"
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
