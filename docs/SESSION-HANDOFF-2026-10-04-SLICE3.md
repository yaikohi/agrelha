# Session Handoff — 2026-10-04 (Plan 0002 Slice 3: Garry's Mod Complete)

Follows `docs/plans/0002-game-registry-resources-and-garrys-mod.md`.

## 1. Current State

- Branch: `main`. **Nothing committed or pushed** — all changes remain in the working tree as requested.
- Test Suite: **100% green** (`rtk go test ./...` -> 937 passed in 72 packages).
- Architectural Ratchet: **Passing cleanly** (`rtk go test ./internal/arch/...` -> 2 passed).
- Guard Coverage Ratchet: **Passing cleanly** (`rtk go test ./internal/web/guard/...` -> 11 passed).

---

## 2. Done This Session: Slice 3 Complete (Steps 19–22)

### Step 19: Steam Web API Client (`internal/infra/content/steam`)
- Implemented `ports.WorkshopResolver` interface in [`internal/infra/content/steam/client.go`](file:///home/ykhi/projects/agrelha/internal/infra/content/steam/client.go).
- Fetches collection children via `ISteamRemoteStorage/GetCollectionDetails/v1/`.
- Fetches published file details and `time_updated` via `ISteamRemoteStorage/GetPublishedFileDetails/v1/`.
- Degrades cleanly when `SteamWebAPIKey` is blank/unset.
- Unit tested in [`internal/infra/content/steam/client_test.go`](file:///home/ykhi/projects/agrelha/internal/infra/content/steam/client_test.go) (7 tests).

### Step 20: Generic / GMod Instance Handler & Routing (`internal/web/handlers/gmod`)
- **Fixes the live defect on 0.31.0:** Navigating to `/gmod` now properly resolves to the Garry's Mod instances dashboard rather than `Cannot GET /gmod`.
- Created templates in `internal/web/pages/`:
  - [`gmod_dashboard.templ`](file:///home/ykhi/projects/agrelha/internal/web/pages/gmod_dashboard.templ): Instance grid, connect address (`ConnectAddress(inst.LBIP, 27015)`), budget bar, badges for gamemode, map, collection, tier, password, and "+ Create World" card.
  - [`gmod_wizard.templ`](file:///home/ykhi/projects/agrelha/internal/web/pages/gmod_wizard.templ): Stepper wizard for Name, Steam Workshop Collection ID, Gamemode, Map, Password, and Tiers (2 GiB Small, 4 GiB Medium, 8 GiB Large).
  - [`gmod_instance.templ`](file:///home/ykhi/projects/agrelha/internal/web/pages/gmod_instance.templ): Detail overview with status, connect address, badges, lifecycle controls (start/stop/restart/delete), and collection-drift restart banner.
- Implemented HTTP handler in [`internal/web/handlers/gmod/handler.go`](file:///home/ykhi/projects/agrelha/internal/web/handlers/gmod/handler.go):
  - Routes: `GET /gmod`, `GET /gmod/access`, `GET /gmod/create`, `POST /api/gmod/wizard/create`, `GET /gmod/:num<int>`, `GET /gmod/:num<int>/:tab`, `POST /api/gmod/instances/:num/start`, `POST /api/gmod/instances/:num/stop`, `POST /api/gmod/instances/:num/restart`, `DELETE /api/gmod/instances/:num`.
  - Creator permissions & world allowance enforcement via `app/requests`.
  - Unit tested in [`internal/web/handlers/gmod/handler_test.go`](file:///home/ykhi/projects/agrelha/internal/web/handlers/gmod/handler_test.go).
- Config & Security:
  - [`internal/platform/config/config.go`](file:///home/ykhi/projects/agrelha/internal/platform/config/config.go): Added `SteamWebAPIKey`, `GModNamespace`, `GModTotalBudgetGiB`, `GModMaxInstances`, `GModMaxRunning`, `GModLBBaseIP`, `GModInstancesPath`.
  - [`internal/web/guard/coverage_test.go`](file:///home/ykhi/projects/agrelha/internal/web/guard/coverage_test.go): Guard coverage ratchet passes (11/11 tests). Added `/gmod`, `/gmod/access`, `/gmod/create` to `anyAccount`, and `/api/gmod/wizard` to `anyAccountPrefixes`.
  - [`internal/web/handlers/grants/handler.go`](file:///home/ykhi/projects/agrelha/internal/web/handlers/grants/handler.go): Added `GModInstances` and updated `parseTarget` to dynamically resolve any game profile via `domain.ProfileFor(game)`.
- Wiring (`internal/wiring/`):
  - [`internal/wiring/wiring.go`](file:///home/ykhi/projects/agrelha/internal/wiring/wiring.go): Wired `d.Steam`, `d.GModRuntime`, `d.GModRef`, `d.GModGame`, `d.GModInstances`. Registered `GModProfile` into `d.Games`, handled `domain.GameGMod` in `requests.WithCreate`.
  - [`internal/wiring/server.go`](file:///home/ykhi/projects/agrelha/internal/wiring/server.go): Built and mounted GMod handler in `web.ServerConfig`, registered in `instanceManagers` (for `/overview` and `dashboardhttp`), and wired into `grantsH`.

### Step 21: Collection-Drift Banner
- Probes `Workshop.GetCollection(ctx, id)` on instance detail.
- If upstream `TimeUpdated` is newer than `inst.CreatedAt`, triggers `RestartPending = true` with reason and a direct "Restart Now" button.

### Step 22: Tiers & Backup Boundaries
- Tiers supported: Small (2 GiB / 3 GiB limit), Medium (4 GiB / 6 GiB limit), Large (8 GiB / 10 GiB limit).
- Persistent data mount is strictly `/home/gmod/server/garrysmod/data`. Workshop addons are excluded from PVC backups.

---

## 3. Verification

- Full suite: `rtk go test ./...` -> **937 passed in 72 packages**.
- Architecture ratchet: `rtk go test ./internal/arch/...` -> **2 passed**.
- Guard coverage: `rtk go test ./internal/web/guard/...` -> **11 passed**.
- No code comments in any generated/modified files.
- No git commits or pushes executed.
