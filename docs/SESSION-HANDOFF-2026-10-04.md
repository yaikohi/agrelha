# Session Handoff — 2026-10-04 (Plan 0002 Slice 1: Game Registry, Value Objects, Clean Ports, Dynamic Dashboard)

Follows `docs/plans/0002-game-registry-resources-and-garrys-mod.md`.

## 1. Current State

- Branch: `main`. **Nothing committed or pushed** — all changes remain in the working tree as requested.
- Test Suite: **100% green** (`rtk go test ./...` -> 876 passed in 66 packages).
- Architectural Ratchet: **Passing cleanly** (`rtk go test ./internal/arch/...` -> 2 passed).
- Manifest Invariant: **Byte-identical** for both Minecraft and Valheim manifests (`internal/infra/manifests/...` tests pass).

---

## 2. Done This Session: Slice 1 Complete (Steps 1–8)

### Step 1 & 2: Domain Primitives & Per-Game Value Objects ✅
- Added `domain.InstanceID` (composite identity `GameID` + `Number`).
- Added `domain.Capabilities` and `domain.GameProfile` declaring per-game capabilities and table/resource naming.
- Built-in profiles: `domain.MinecraftProfile` and `domain.ValheimProfile`. Profile registry functions `domain.ProfileFor(id)` and `domain.Profiles()`.
- Extracted game-specific configurations off `domain.Instance` into dedicated value objects:
  - `domain.MinecraftConfig` (`Loader`, `Pack`, `MCVersion`, `Difficulty`, `Gamemode`, `WorldType`)
  - `domain.ValheimConfig` (`Password`, `Seed`)
- Deleted game-specific fields from `domain.Instance`.

### Step 3: Thread GameProfile & Delete `if GameID == GameValheim` ✅
- Threaded `GameProfile` through all 17 branching methods on `domain.Instance`:
  `EffectiveResources`, `DeploymentName`, `ServiceName`, `PVCDataName`, `ConfigMapName`, `SecretName`, `MemoryGiB`, `EffectiveBackupsPVC`, `PVCClaimName`, `ConfigMapVolName`, `EnvFromConfigMap`, `SecretsVolName`, `EnvFromSecret`, `DataVolumeMountPath`, `LogTailLines`, `GamePort`, `GameProtocol`.
- Eliminated every `if GameID == GameValheim` check from `domain.Instance`.
- Updated all call sites in `domain`, `app/instances`, `infra/manifests`, `infra/store`, `web/handlers`, and `wiring`.

### Step 4: Registry in `app/games` & Architecture Ratchet ✅
- Created `internal/app/games/registry.go` with `Entry`, `Registry`, `NewRegistry()`, `Register()`, `Get()`, `Engine()`, `Profiles()`, `Entries()`.
- Added `app/games` to the architecture ledger in `docs/architecture.md`. Ratchet test `internal/arch/table_test.go` passes cleanly.
- Wired `Games: gamesReg` in `internal/wiring/wiring.go` with both default profiles and engines.

### Step 5: Generic & Registry-Driven Repository ✅
- Refactored `internal/infra/store/instance_repo.go`:
  - `mapCoreFromRecord` and `mapCoreToRecord` map common metadata once.
  - Pluggable `GameRecordMapper` map handles game-specific columns.
  - Added `NewGameInstanceRepo(s, profile)` factory.
  - Unknown/unregistered `GameID` returns a hard error, never a Minecraft fallback.

### Step 6: Resolved Access Collisions ✅
- `ParseInstanceRole`: dynamically verifies `domain.ProfileFor(gameID)`.
- `requests.WithCreate`: switches explicitly on game IDs; returns explicit error for unknown games.
- `guard.Mounts()`: dynamically mounts per-game instance prefixes by iterating over `domain.Profiles()`.

### Step 7: Deleted Dead `ports.Game` Methods & Unimplemented Ports ✅
- Removed dead interfaces from `internal/ports/game.go`: `ContentProvider` and `ModResolver`.
- Pruned 6 dead methods off `ports.Game`: `ID()`, `Display()`, `Providers()`, `ResolveContent()`, `AdmissionModel()`, `OperatorIDKind()`.
- Kept the 3 live methods: `RuntimeSpec(inst)`, `Telemetry(ctx)`, `ExportClientBundle(ctx, inst)`.
- Cleaned up `minecraft/game.go` and `valheim/game.go` along with mock test implementations.

### Step 8: Dynamic Dashboard, Nav & SSE Signals ✅
- `internal/web/pages/helpers.go`: Added `GameSummaryUI` (with type aliases for `MinecraftSummaryUI` and `ValheimSummaryUI`), `DashboardGameUI`, `GenericGameCard`, `GameActions`, and `GameNav`.
- `internal/web/pages/nav.templ`: Iterates over `domain.Profiles()` for admin links and mod update badges.
- `internal/web/pages/dashboard.templ`: Dynamic grid rendering `for _, g := range games { @gameCard(g.Card) { if isAdmin { @cardActions(g.Actions) } } }`.
- `internal/web/handlers/dashboard/handler.go`:
  - `DashboardPage`: Builds `[]pages.DashboardGameUI` driven by `h.registeredProfiles()`.
  - `TileSignals`: Loops over registered profiles to collect mod updates and engine telemetry.
- `internal/wiring/server.go`: Passes `d.Games` to `dashboardhttp.Config`.

---

## 3. Verification Checklist

1. **Full test suite green:** `rtk go test ./...` -> 876 passed in 66 packages.
2. **Architecture ratchet:** `rtk go test ./internal/arch/...` -> 2 passed.
3. **Manifests byte-identical:** `rtk go test ./internal/infra/manifests/...` -> 16 passed.
4. **Unregistered GameID:** Produces hard error, no silent fallback.
5. **No git commits/pushes:** All changes remain in the local working copy for user review.

---

## 4. Next Step: Slice 2 (Per-Instance Resources)

Per `docs/plans/0002-game-registry-resources-and-garrys-mod.md`:
- **Step 9:** `domain.Resources` (memory + CPU, request + limit); per-game tier catalogues in SQLite.
- **Step 10:** Backfill migration: ensure every existing Instance gets concrete resources from today's hardcoded tier values.
- **Step 11:** Render resources from `Instance.Resources`, not the tier; heap-init becomes a Minecraft-only tier field.
- **Step 12:** Budget sums concrete resources; per-game ceiling enforced in `app/capacity`.
- **Step 13:** `FreeCreations` becomes a global setting.
- **Step 14:** `/overview` page for all worlds, all games.
- **Step 15:** Opt-in per-world "apply tier".
