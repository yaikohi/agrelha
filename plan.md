# agrelha — plan & status

Multi-game server control panel (Go + Fiber + templ + Tailwind + Datastar) supporting
**Valheim** and **Minecraft Modded** (NeoForge & Fabric multi-instance). Deployable to
Kubernetes via GitOps (`yaya-ops`) and self-hostable via Docker / Docker Compose.
Image hosted in self-hosted Zot registry (`registry.ykhi.xyz/agrelha`) and reach at
`https://agrelha.ykhi.xyz` (WireGuard-only, Zitadel OIDC or local auth).

**Current version:** `0.23.6` (Cluster runs `0.23.4`; local HEAD contains Phase 0–8 modularization complete).

### Modularization Plan Status (2026-09-09)
Following [docs/modularization-plan.md](docs/modularization-plan.md):
- **Phase 0 (Hygiene)** ✅: Config sanitized, personal values removed, fallback to old keys preserved.
- **Phase 1 (Runtime Port)** ✅: `ports.Runtime` defined; k8s and docker runtime adapters created.
- **Phase 2 (Test Coverage)** ✅: Critical path behavior tests across gitops, store, k8s runtime, minecraft invariants, and config.
- **Phase 3 (State + Reconciler)** ✅: `ports.StateStore` and `ports.Reconciler` defined; outward infrastructure dependencies inverted.
- **Phase 4 (Domain Extraction)** ✅: `internal/domain` pure package (Instance, Game, Pack, Budget, Access, Tier).
- **Phase 5 (Game Interface)** ✅: `ports.Game` defined; `internal/games/valheim` and `internal/games/minecraft` engines implemented.
- **Phase 6 (Auth Port)** ✅: `ports.Auth` defined; OIDC and Argon2id local user authenticator adapters.
- **Phase 7 (HTTP Split)** ✅: Monolith `internal/server` (5,453 LOC) split into 7 feature packages under `internal/http/` (shared, access, backups, console, dashboard, content, instances).
- **Phase 8 (Docker Adapter)** ⚠️ **adapters only, NOT end-to-end**: Implemented `infra/state/local` (files + SQLite history/rollback), `infra/reconcile/compose` (synchronous convergence + YAML render), `infra/runtime/docker` (443 LOC of real Docker Engine API over the unix socket, incl. log demuxing). All three are real code and the app boots and serves under `RUNTIME=docker`. **But nothing connects them** — audited 2026-09-09:
  - `compose.RenderCompose` and `compose.WriteAndConverge` have **zero callers**; nothing ever writes a `docker-compose.yml`, and `Converge` runs `docker compose up -d` in a directory that must already contain one.
  - `wiring.Build` passes the Kubernetes `manifests.New(...)` renderer **unconditionally**, so creating an instance under Docker writes k8s YAML that compose cannot read. `ports.SpecRenderer` has exactly one implementation.
  - `Reconciler.Converge` is never invoked anywhere, on either the ArgoCD or the compose side.
  - Valheim's control routes call `s.k8s.Restart`/`Scale` directly in `routes.go`, bypassing `ports.Runtime`; under Docker `K8s` is nil, so Valheim control is dead.
  - **Root cause:** `compose.RenderCompose` consumes a `domain.RuntimeSpec`, and the only producers are the dead `ports.Game` implementations. Docker support is blocked on the Game abstraction — i.e. on **architecture-cleanup phase G**, not on phase E. The claim "Audience 2 can install" is not true today.
- **Phase 9 (Packaging)** ✅: `deploy/docker-compose.yml` (only `RUNTIME: docker` required) and `deploy/helm/agrelha` (lints clean; renders with zero values and with a full git+OIDC+ingress+NFS-backups values file). RBAC is per-namespace, never cluster-wide. Config surface audited: **every key has a default**, so the required set is `RUNTIME` alone, and only when off Kubernetes — documented in `docs/configuration.md`, install paths in `deploy/README.md`.
  - A git-less Kubernetes install used to leave `stateStore` nil, so opening the mods page would have nil-panicked. Added `adapters/state/unconfigured`: reads return empty documents, writes return `ErrUnconfigured`. The declarative plane is now gated on `GIT_REPO_URL && GIT_TOKEN` (a token with no repo URL was previously accepted).
### Architecture Cleanup Status (2026-09-09)
Following [docs/architecture-cleanup-plan.md](docs/architecture-cleanup-plan.md).
An audit after phase 9 found nine layering violations; that document holds the
evidence, the decisions and the full phase table. Progress:

- **Phase A (Layout move)** ✅: whole tree relocated to `domain / ports / app / infra / web / platform`, `cmd/api`→`cmd/agrelha`, `pages` out of `cmd/`. Pure move, no behaviour change. Closed violation #6 (`internal` → `cmd`).
- **Phase B (Architecture test)** ✅: `internal/arch/arch_test.go`, pure stdlib, runs in `go test ./...`. A **ratchet**: the edge table is the spec, `exceptions` lists today's violations tagged by the phase that removes each. Fails on a new forbidden edge, a stale exception, an unclassified package, or a stale layer prefix. All four failure modes verified by deliberately triggering them.
- **Phase C (Split `minecraft` + `modpack`)** ✅: both packages gone. `instance.go`/`content.go` turned out to be pure re-export shims, so the work was rewriting ~120 call sites, not writing domain code. Three dependencies **inverted**: `ports.SpecRenderer`, `ports.Console`, and a local `ingest.presenceStore`.
- **Phase D (Composition root)** ✅: `internal/wiring` (**not** `cmd`, because Go forbids importing `package main` and two real tests exercise the wiring decisions). `wiring.Build` returns an error where the old code called `os.Exit(1)`. `server.New(cfg, Deps)` now constructs no adapters and does no I/O. No `os.Exit` anywhere under `internal/`.
- **Phases E–I** remain. **Ledger: 10 known violations** (was 19). Done means `exceptions` is empty and the `layers` table no longer mentions `internal/server`.

- **Quality & Verification** (2026-09-09, after candidate 4): `go build`,
  `go vet`, `go test ./...` and `gofmt` all clean (229 tests passing across 51 packages).
  Coverage expanding with pure Go unit tests on deep managers and thin HTTP handlers.

### Architecture Deepening Roadmap (2026-09-09)
Refactoring shallow modules into deep modules with narrow interfaces hiding significant complexity, maximizing testability ("the interface is the test surface") and locality:

- **Candidate 1 (Complete): Deepen the Game Engine Seam (`ports.Game` & `internal/app/games`)** ✅
  - **Target Seam**: `ports.Game`, `internal/app/games/valheim`, `internal/app/games/minecraft`
  - **Delivered**: `ports.Game` interface deepened to encapsulate client bundle export (`ExportClientBundle`), telemetry retrieval, admission policies, and runtime specifications. Concrete engines in `internal/app/games/minecraft` and `internal/app/games/valheim` encapsulate game-specific mechanics. Handlers delegate directly without leaking format or client details.
  - **Impact**: High locality and leverage; game engine specifics decoupled from web presentation.

- **Candidate 2 (Complete): Invert Web Handlers into Deep Application Modules (`internal/app/instances`, `internal/app/access`, `internal/app/admins`)** ✅
  - **Target Seam**: `internal/app/instances.InstanceManager`, `internal/app/access.AccessManager`, `internal/app/admins.Manager`
  - **Delivered**: Handlers in `internal/web/handlers/access` and `internal/web/handlers/instances` inverted into thin delivery adapters, completely eliminating concrete dependencies on `infra/` (`*k8s.Client`, `*gitops.Committer`, `*store.Store`, `*rcon.Pool`) and `platform/config/` (`*config.Config`). Deepened application managers encapsulate composite business workflows: git state mutations via `ports.StateStore`, audit logging via `ports.AuditRecorder`, event recording via `ports.EventRecorder`, configs/mods access via functional readers, and automated sync hooks.
  - **Impact**: Removed 4 ratchet exceptions in `arch_test.go` (`handlers/access` -> `infra`, `handlers/access` -> `config`, `handlers/instances` -> `infra`, `handlers/instances` -> `config`). Ratchet ledger reduced from 19 to 15 known violations. All 227 tests passing.

- **Candidate 3 (Complete): Deepen Content and Pack Resolution (`ports.ModResolver`, `internal/app/content`, `internal/app/modpack`)** ✅
  - **Target Seam**: `ports.ModResolver`, `internal/domain.Mod*`, `internal/app/content`, `internal/app/modpack`, `internal/infra/content/modrinth`
  - **Delivered**: Extracted pure domain mod types (`domain.ModProject`, `domain.ModVersion`, `domain.ModVersionFile`, `domain.VersionDependency`). Defined narrow `ports.ModResolver` port for batch version resolution and compatibility lookups. Refactored `internal/app/content` and `internal/app/modpack` to consume domain models, completely eliminating direct imports of `internal/infra/content/modrinth`.
  - **Impact**: Removed 2 Phase F exceptions in `arch_test.go` (`app/content` -> `infra`, `app/modpack` -> `infra`). Ratchet ledger reduced from 15 to 13 known violations.

- **Candidate 4 (Complete): Unify Workload Lifecycle into `ports.Runtime` (`ports.Runtime`, `internal/app/backups`, `internal/web/handlers/console`, `internal/server`)** ✅
  - **Target Seam**: `ports.Runtime`, `internal/app/backups.BackupScheduler`, `internal/web/handlers/console`, `internal/server`
  - **Delivered**: Deepened `ports.Runtime` with `WatchAvailability`. Implemented across `k8s` and `docker` runtime adapters. Deepened `internal/app/instances.InstanceManager` with `InstanceLogs` and `ExecuteCommand` using seam options. Inverted `internal/web/handlers/console` to use pure `ports.Runtime`, `ports.AuditRecorder`, and `ports.EventRecorder`, removing concrete `k8s.Client`, `rcon.Pool`, and `store.Store`. Refactored `internal/app/backups.BackupScheduler` to consume pure `JobRunner`, `InstanceLister`, and functional options, extracting `domain.FormatBackupFileName` into `domain` and eliminating all `infra/` and `platform/config` dependencies. Unified server and control routes to execute lifecycle commands through `ports.Runtime` rather than raw k8s client scaling/restarts.
  - **Impact**: Removed 3 ratchet exceptions in `arch_test.go` (`app/backups` -> `infra`, `app/backups` -> `config`, `web/handlers/console` -> `infra`). Ratchet ledger reduced from 13 to **10 known violations**. All 229 tests passing.

### Layering Model & Package Roles (Agreed 2026-09-09)
Aligned with Hexagonal (Ports & Adapters) and Onion Architecture:
- **`internal/domain`** (Core): Entities, value objects, and domain invariants. Imports **nothing**.
- **`internal/ports`** (Core Seams): Inverted interfaces for driven adapters. Imports **`domain` only**.
- **`internal/app`** (Core Use Cases): Application services orchestrating business workflows (budget enforcement, instance management, backup scheduling, mod compatibility). Imports **`domain` + `ports` only**. Never `infra`, never `web`.
- **`internal/infra`** (Driven / Outbound Adapters): Technical plumbing and external integrations (SQLite, Kubernetes, Docker, Modrinth, RCON). Imports **`domain` + `ports`**. Never `app`, never `web`.
- **`internal/web`** (Driving / Inbound Adapters): Presentation delivery (Fiber routing, thin handlers, templ components, Datastar SSE). Imports **`domain` + `ports` + `app`**. Never `infra`.
- **`internal/platform`** (Bootstrap): Cross-cutting environment config for bootstrap.
- **`internal/wiring`** (Composition Root): Pure dependency injection graph assembly.
- **`internal/server`** (Transitional): Legacy monolith package holding routing and daunting handlers; slated for complete dissolution.

- **Candidate 5: Package READMEs & Documentation** [In Progress]
  - Create a structured `README.md` inside each of the 9 `internal/*` subdirectories explaining layer roles, permitted import directions, and package contents.

- **Candidate 6: Decompose Daunting Handlers & Dissolve `internal/server`** [Next]
  - **Target Seams**: `internal/server` -> `internal/app/*` (orchestration) + `internal/web/handlers/*` (thin delivery) + `internal/web/routes.go` (routing).
  - Move wizard/provisioning orchestration out of `handlers_mc_wizard.go` into `app/instances` or `app/wizard`.
  - Invert remaining `server` handlers (`handlers_mc_backups`, `handlers_dashboard`, `handlers_mods`) to consume application services and ports.
  - Migrate route registrations into `internal/web/routes.go`, move server assembly into `internal/wiring`, and completely delete `internal/server`, eliminating the final ratchet exceptions.

---

> Self-metrics + Grafana dashboard (`0.8.3`). agrelha exposes an unauthenticated
> Prometheus `/metrics` (registered outside the auth group, next to `/healthz`) via
> `prometheus/client_golang` + `gofiber/adaptor`; `internal/metrics` holds the
> collectors. Custom series: `agrelha_http_requests_total{method,route,status}` +
> `_duration_seconds` histogram, `agrelha_datastar_requests_total`,
> `agrelha_sse_active_connections` (gauge), `agrelha_sse_opened_total`,
> `agrelha_sse_closed_total{reason}`, `agrelha_sse_frames_total{kind}`,
> `agrelha_control_actions_total{action,result}` — plus stock `go_*`/`process_*`.
> Recorded in `requestLogger` (route pattern via `c.Route().Path`; **`c.Method()`
> is copied with `utils.CopyString` — Fiber returns an unsafe buffer-aliased string
> that Prometheus would retain and later corrupt**), `guard`, and the SSE handler.
> Pipeline (verified against live InfluxDB): telegraf scrapes
> `agrelha.agrelha.svc/metrics` → bucket `metrics`, measurement `prometheus`, field
> = metric name, labels → tags, `url` tag isolates agrelha. Dashboard =
> `yaya-ops manifests/observability-dashboard-agrelha.yaml` (uid `agrelha`, Flux).
> After deploy, bump telegraf `config-revision` (done: `4-scrape-agrelha`) so its
> subPath-mounted config reloads.

> Logging (`0.8.2`, stdlib `log/slog`). Chose slog over zap/zerolog: structured,
> leveled, zero deps (fits pure-Go/CGO-off), and its global default lets low-level
> `internal/sse` emit frames without dependency injection. `internal/logging.Setup`
> reads `LOG_LEVEL` (debug|info|warn|error, default info) + `LOG_FORMAT` (text|json,
> default text) from the ConfigMap — flip `LOG_LEVEL=debug` (no rebuild) to trace
> SSE/Datastar. `requestLogger` middleware logs one line per request with the fields
> that pinpoint client-vs-server issues: `method path status dur_ms ip actor` plus
> `ds` (the `Datastar-Request` header), `accept`, `resp_ct`, `location` — a missing
> line for a button click = the client never sent it. The SSE handler logs
> `sse open`/`sse close` (correlated by `rid`, with frame counts + close reason) and
> surfaces the previously-swallowed `StreamLogs` error; `internal/sse` logs each
> frame's event name + size at DEBUG so the `datastar-patch-*` wire is visible. All
> prior `log.Printf` calls converted to slog.

> Dashboard control buttons (`0.8.2`). The buttons never worked: they used a
> Datastar backend action (`data-on-click="@post('/server/…')"`) that fired **no
> request at all** (nothing in the Network panel; the `audit`/`events` tables were
> empty across all history despite `mod_index` holding 10k+ rows, proving no
> `/server/*` POST ever reached a handler). The SSE tiles/logs work because
> `data-effect="@get('/sse')"` runs at load — but the click-driven `@post` action
> did not. Rather than keep chasing the client action, the four control buttons are
> now plain `<form method="post" action="/server/…">` doing POST-redirect-GET +
> flash, exactly like the Mods/Admins/Configs buttons. `guard` sets a flash and
> `303`s to `/`; the dashboard renders `@flashBanner`. Datastar now only drives the
> live SSE (tiles/logs), which is the part that actually worked. `0.8.1`'s
> `application/json` toast approach is reverted. (RBAC confirmed: the agrelha SA can
> `patch deployments` in `valheim`; Stop/Start patch `.spec.replicas`, not the
> `scale` subresource. Bundle stays pinned to `v1.0.2` so `internal/sse`'s
> `datastar-patch-*` frames match.)

> Modpack export (`0.8.0`). `GET /mods/export` streams a `valheim-YYYY-MM-DD.r2z`
> (a zip) built from the live `valheim-mods` ConfigMap + the `valheim-mod-configs`
> `.cfg` keys. Layout is r2modman/Thunderstore-Mod-Manager's own: `export.r2x`
> (`profileName` + `mods[]` of `name: namespace-name`, `version:{major,minor,patch}`,
> `enabled: true`) plus each config under `config/<file>.cfg`. Friends import via
> r2modman → Import profile → From file. No comments, no external calls.
> Code: `internal/modpack/modpack.go`, `internal/server/handlers_modpack.go`. The
> route is registered before `/mods/:namespace/:name` so the static path wins.

> README styling: this Tailwind v4 build has no Typography plugin, so the `prose`
> classes were dead. README now uses a `.md` scope with hand-written markdown CSS
> in `internal/web/assets/css/input.css` (rebuild `output.css` via `task tailwind:build`
> / the CLI when it changes).
>
> README images: the grilled "CDN-only images" rule stripped every body image
> (mods host them on GitHub/imgur, not gcdn — only the package icon is on gcdn),
> so READMEs showed bare alt text. `internal/mdrender` now allows any `https://`
> image src (still blocks `http`/`data:`/`javascript:`).
>
> Image proxy (0.6.3): the browser never contacts external image hosts.
> `mdrender` rewrites every sanitized `https://` `<img src>` to `/img?u=<escaped>`
> (post-sanitize HTML pass via `x/net/html`); `GET /img` (auth-gated) fetches the
> image server-side and re-serves it. Guards: https-only (+ on redirects), host
> must resolve to a public IP (blocks SSRF to loopback/private/link-local), 15s
> timeout, 12 MB cap, `Content-Type` must be `image/*`, 7-day cache. The package
> icons on the list/detail pages are gcdn direct (not proxied) — only README-body
> images go through the proxy.

---

## Where we are

Two-plane hybrid (chosen because the `valheim` ArgoCD app has `selfHeal: true`, so
live cluster edits get reverted):

- **Declarative plane (git):** mods (`valheim-mods.yaml`), admin list
  (`valheim-admins.yaml`) → agrelha commits to `yaya-ops@main` as `agrelha
  <agrelha@ykhi.xyz>` → ArgoCD syncs → pod rolls.
- **Imperative plane (k8s API):** restart / stop / start / logs / metrics → `client-go`
  against a ServiceAccount scoped to the `valheim` namespace (no exec).

### Done

- **Infra:** Zot registry (`registry.ykhi.xyz`, `docker2s2` compat), agrelha ns + SA +
  scoped RBAC (pods, pods/log, deployments+scale, configmaps read, metrics.k8s.io),
  ExternalSecrets (codeberg token, OIDC client, registry pull creds) seeded via tal
  `02-platform-config/apps-secrets.tf`.
- **Auth:** Zitadel OIDC (`https://zitadel.o0o.zip`), single identity `ykhi@proton.me`,
  UserInfo fallback, `/login` landing page, **RP-initiated logout** (ends the Zitadel
  session too).
- **Dashboard:** live SSE tiles (Players / CPU / Memory / Uptime) + live log tail;
  restart/stop/start buttons; Grafana deep-link.
- **Mods:** Thunderstore search (background-streamed index) + dependency-resolved
  install + remove, each a git commit; direct install-by-identifier fallback.
- **Admins:** grant/revoke in-game admin (Steam64 → `ADMINLIST_IDS`) via git commit;
  player roster from the log ingester.
- **Auto-apply:** after a mod/admin commit, `applyAfterSync` waits for ArgoCD to
  reconcile the ConfigMap, then rolls the pod so the change takes effect.
- **Persistence:** SQLite (`players`, `audit`, `events`, `mod_index`, `mod_readme`,
  `meta`) at `/data`.
- **Mod browsing + metadata cache (0.6.0):** the streamed index now keeps
  icon/description/version/downloads/deprecated per package, is persisted to
  `mod_index` and preloaded on startup (instant browse, no startup re-pull, bg 6h
  refresh). Search results + installed list show icon + description and link to a
  detail page `GET /mods/{namespace}/{name}` (header, deprecated warning, direct
  dependency list, README, Install button, Thunderstore link). READMEs are lazily
  fetched and cached forever by immutable version in `mod_readme`, rendered via
  `internal/mdrender` (goldmark, raw HTML off → bluemonday allowlist; images
  restricted to `gcdn.thunderstore.io`, links forced `nofollow noopener _blank`).

---

## Remaining work

### P1 — correctness / it-bugs-me — DONE (0.7.0)

- [x] **Session persistence.** Sessions are now stateless HMAC-SHA256-signed cookies
      (now `internal/adapters/auth/oidc`): payload `{email, idToken, exp}` signed with a key derived
      `sha256("agrelha-session-v1:"+OIDCClientSecret)` (stable across restarts, no new
      secret to seed, no server-side store). Survives deploys; no re-login. Verified
      tamper/garbage/wrong-key all rejected.
- [x] **OIDC `state` + `nonce` CSRF.** `Login` generates random `state`+`nonce`, stores
      them in a short-lived signed `agrelha_oidc` cookie, passes `nonce` via
      `oidc.Nonce`. `Callback` constant-time-compares `state` and the ID token's `nonce`
      claim, then clears the cookie.
- [x] **Session eviction.** Moot — stateless cookies, nothing to evict (`exp` in payload).
- [x] **Inline error/success feedback.** Flash cookie (`internal/server/flash.go`):
      install/remove/grant/revoke set an ok/err message + redirect; the Mods/Admins pages
      render a banner (`flashBanner`). e.g. "Installed X (+3 dependencies) — committed;
      the server will restart to apply." / "Couldn't resolve …". No more raw Fiber error
      pages for these actions.

### P2 — features from the original design not yet built

- [x] **Mod browsing + metadata cache.** Done in 0.6.0 (see Done above); spec kept
      below for reference.
- [x] **Audit / event timeline UI (0.7.1).** `GET /history` — merged timeline of `audit`
      (who did what) + `events` (joins/leaves/backups/crashes), newest first, 200 rows.
      `store.ListHistory` UNIONs both and excludes `restart/stop/start` events (they'd
      duplicate the audit rows). Labels/badges via `pages.HistoryLabel`/`HistoryBadge`.
      Nav gained a History link.
- [x] **Mod config (.cfg) editing (0.7.3).** `/configs` lists the `.cfg` keys in the
      `valheim-mod-configs` ConfigMap; edit/new/delete each commit via new
      `gitops.Committer.SetData`/`DeleteData` (upsert-or-create a data key, literal block
      scalar for multiline, into an empty `data: {}` too) → `applyAfterSync` restarts so
      the mod-reconciler re-copies the file. Textarea editor, CRLF→LF normalized, filename
      validated `^[A-Za-z0-9][A-Za-z0-9._-]*\.cfg$`, flash feedback, nav link. New config
      env `MOD_CONFIGS_PATH`.
- [x] **Player presence (0.7.2).** `players.online`/`online_since` columns (idempotent
      ALTER migration). Ingester `SetOnline` on connect/disconnect; `ClearPresence` on
      startup (live state unknown across restarts — rebuilds from the 200-line tail, so a
      player connected >200 log-lines ago shows offline until they reconnect). Dashboard
      **Players tile now reads `store.CountOnline`** (replaces the broken A2S `status.json`
      that always `TimeoutError`'d). Admin roster sorts online-first with a green dot.
- [x] **"Update now" action (0.7.4).** `POST /server/update` = restart recorded as an
      `update` action/event (the lloesche image installs any Valheim update on boot).
      Dashboard "Update" button (sky, with a tooltip that it restarts).
- [x] **Last-backup / backup-size tiles (0.7.4).** agrelha NFS-mounts the NAS
      `valheim-backups` export **read-only** at `/backups` (direct `nfs:` volume, not a
      PVC — avoids cross-ns + RWO-accessMode). `internal/backups.Stat` → newest mtime,
      count, total + latest size. Dashboard shows "Last backup: 3h ago · N backups · X
      total · latest Y". Cached 60s + 3s-timeout-bounded (`s.backupInfo`) so a hung NAS
      can't block the SSE tick. (Live world-save size not shown — it's node-local RWO on
      game-01, unreadable without exec; the latest backup size is the proxy.)
      **Resilience caveat:** a raw `nfs:` volume mount blocks pod start, so if the NAS is
      down agrelha won't start. If that matters, switch to a decoupled collector
      (separate deployment writing a summary agrelha reads) or drop `BACKUPS_DIR`.

### P3 — polish / infra

- [ ] **Cut the periodic 162 MB pull (optional).** Index persistence (startup re-pull)
      is handled by the mod-browsing spec below; this item is only the *further*
      optimization of replacing the 162 MB v1 list with the gzip
      `/api/experimental/package-index/` JSONL endpoint to cut the 6-hourly bandwidth.
      Needs validating that endpoint carries description+icon and isn't Cloudflare-walled.
- [ ] **"Update available" detection.** Compare installed mod versions (mods.txt) against
      the Thunderstore index `latest` and flag upgrades on the mods page.
- [ ] **CI.** No CI — images are built manually (`task build:image TAG=x`). Add
      Forgejo Actions/Woodpecker on Codeberg to build+push on tag and (optionally) bump
      the yaya-ops image tag.
- [ ] **Lean image build.** `task setup` installs `air` (dev-only) in the Docker build,
      dragging in a go1.26 toolchain download. Split a `setup:ci` that skips it.
- [ ] **Tests.** None yet. Unit-test the pure logic: `mods.Parse`/Install/Remove,
      `admins` grant/revoke transforms, `thunderstore.entry`/`ResolveTree` parsing,
      `gitops` YAML round-trip.
- [x] **Players tile fixed (0.7.2).** No longer depends on `status.json` A2S (which still
      `TimeoutError`s) — the tile now shows `store.CountOnline` from the log ingester.
      (`VALHEIM_STATUS_URL`/`valheim.FetchStatus` are now unused; drop later if desired.)
- [ ] **InfluxDB sparklines (optional).** CPU/Mem are instantaneous from metrics-server;
      historical mini-charts would need an InfluxDB read token.

---

## P4 — zero-downtime HA (rqlite)

Goal: run **2+ replicas with `RollingUpdate` so deploys have zero downtime** (wanted as
a technical feature, not out of necessity — it's a single-user tool, so this is a
deliberate "do it properly" project, not a fix for a real availability problem).

### Why it doesn't work today

- State is **embedded SQLite on a RWO, node-local `local-path` PVC** (`agrelha-data`).
  Two replicas can't share it: a replica on another node can't mount the RWO/local-path
  PV; two on the same node would be **two SQLite writers → `SQLITE_BUSY`/corruption**.
  That's exactly why the Deployment is `strategy: Recreate` (kill-then-start = a few
  seconds of downtime).
- The **log ingester is a singleton** by nature. Even with perfect HA storage, running it
  in two pods = two log tails = **duplicate join/leave events**. Same for the Thunderstore
  warm loop and `applyAfterSync`. HA storage does NOT solve this — it needs a single
  owner.
- Sessions are already **stateless signed cookies** (P1), so auth already survives
  multiple replicas — that part's done.

### Decision: rqlite (SQLite + Raft), not the alternatives

Evaluated the "distributed SQLite" field against our constraints (pure-Go /
`CGO_ENABLED=0`, Talos = no easy FUSE, tiny relational schema):

- **rqlite** ✅ — self-contained Raft in one binary, normal container (no FUSE/CGO),
  **pure-Go `gorqlite` client**, and it *is* SQLite so the existing SQL mostly ports
  as-is. Keeps the "still SQLite" ethos. **Chosen.**
- **Litestream** ❌ — backup/DR streaming only, single-writer, no HA.
- **LiteFS** ⚠️ — most SQLite-native (keep embedded SQLite) but needs **FUSE** →
  fragile on Talos, and semi-abandoned.
- **dqlite** ❌ — needs CGO + libdqlite; breaks the pure-Go build.
- **libSQL/Turso `sqld`** ⚠️ — embedded replicas need CGO; remote pure-Go mode works but
  self-hosted HA is immature.
- **Marmot** ❌ — eventually-consistent multi-master; wrong semantics.
- **SurrealDB** ❌ — overkill: HA needs a TiKV/FoundationDB cluster *underneath* it, a
  full SurrealQL rewrite, and a multi-model graph DB for ~6 tiny relational tables. Only
  worth it as a platform decision / if we specifically wanted its live-queries.

### Architecture: split web / worker (preferred over in-process leader election)

Rather than one Deployment doing leader-election, split responsibilities:

- **`agrelha-web` Deployment** — 2+ replicas, `RollingUpdate` with `maxUnavailable: 0`,
  fully stateless (talks to rqlite, sessions are cookies). This is what gives
  zero-downtime deploys and survives a node/pod loss.
- **`agrelha-worker` Deployment** — 1 replica; owns the **singleton** work: log ingester,
  Thunderstore warm loop, `applyAfterSync`. Its ~seconds restart is harmless (presence is
  rebuilt from the log tail anyway; index reloads from rqlite). No leader-election code —
  the "singleton" is just a 1-replica Deployment.
- Both talk to the same **`rqlite` StatefulSet** (3 nodes, Raft, one PVC each).

(Alternative if we ever want a single Deployment: client-go `coordination.k8s.io` Lease
leader-election gating the singleton goroutines, + RBAC for leases. More code; rejected
for now in favor of the split.)

### Work breakdown

1. **rqlite StatefulSet** manifests (3 replicas, headless Service, per-pod PVC, join via
   the headless DNS) in yaya-ops; ArgoCD app. Right-size for the small VMs.
2. **Port `internal/adapters/store`** from modernc `database/sql` → `gorqlite` (pure-Go). SQL is
   ~all compatible (it's SQLite). Rework the spots that assume a local file / interactive
   transactions:
   - `SaveModIndex` uses an interactive `BEGIN`+prepared-stmt loop → rqlite wants a single
     **batched write request** (queued statements), not an interactive txn. Rewrite as one
     batch (DELETE + parameterised INSERTs + meta upsert).
   - Drop the WAL/`foreign_keys` pragmas (N/A to rqlite).
   - Timestamps: keep the `asTime` tolerance (rqlite returns strings) — already robust.
   - Idempotent `ALTER TABLE` migration still works (run once at startup against rqlite).
3. **Split the binary's roles**: a `ROLE=web|worker` (or two entrypoints). `web` skips
   `ingest.Run`/`ts.WarmLoop`/`applyAfterSync`; `worker` runs them and serves nothing (or
   just `/healthz`). Config flag + wire in `server.New`.
4. **Manifests**: `agrelha-web` (2 replicas, RollingUpdate) + `agrelha-worker` (1 replica)
   Deployments; drop the `agrelha-data` PVC and the SQLite volume; keep the backups NFS
   mount on whichever role surfaces the tile (web). HTTPRoute → web Service.
5. **Config**: `RQLITE_URL` (e.g. `http://rqlite.agrelha.svc:4001`); remove `DB_PATH`.
6. Verify: kill a web pod under load → no blip; rolling deploy → zero 5xx; only one
   ingester writing (no duplicate events in History).

**Caveat to keep in view:** this is a 3-node Raft cluster + a role split to erase a
~3-second deploy blip on a single-user tool. Justified only as a deliberate technical
exercise (which is the stated intent).

---

## Spec: mod browsing & metadata cache

Grilled + agreed 2026-08-24. Read-only browsing feature; does **not** change how
install works (still ResolveTree → git commit). Key facts that shaped it:

- Short **description + icon are already in the 162 MB list we stream** (per version) —
  enriching the index is free, no extra fetch.
- The **README (rich markdown) is a separate per-mod fetch**
  (`…/api/experimental/package/{ns}/{name}/{version}/readme/` → `{markdown}`).
- **Thunderstore versions are immutable** → a README cached by version is never stale
  (no TTL, no invalidation).

### Data model (SQLite) — replaces the unused `mod_cache` placeholder

```sql
mod_index(
  full_name TEXT PRIMARY KEY,   -- "namespace/name"
  namespace, name, owner, version, description, icon, package_url,
  downloads INTEGER, is_deprecated INTEGER, updated_at TIMESTAMP
);
mod_readme(
  full_name TEXT, version TEXT, markdown TEXT, fetched_at TIMESTAMP,
  PRIMARY KEY (full_name, version)   -- immutable, no expiry
);
```

### Index lifecycle (persisted, background refresh)

- Extend the streamed-parse (`thunderstore.warm`) to also keep `description`, `icon`,
  `version`, `downloads`, `is_deprecated` per package.
- **Startup:** load `mod_index` from SQLite into memory → browse instantly, survives
  restarts (no startup re-pull). Search stays in-memory substring, ordered
  **most-downloaded first**.
- **Background:** if the persisted index is >6 h old, stream Thunderstore and upsert
  `mod_index`. Never blocks startup/browsing. (The 162 MB pull still happens every 6 h —
  cutting it is the separate P3 gzip-package-index item.)

### Browse UI

- Search results **and the installed list** show icon + short description (looked up in
  the index), each linking to a detail page.
- Detail page `GET /mods/{namespace}/{name}`: header (icon, name, version, downloads,
  last-updated, **deprecated warning**), dependency list, README (sanitized markdown),
  **Install +deps** button, **View on Thunderstore ↗** link.

### README fetch + render

- **Lazy:** on detail view, cache hit → render from SQLite (zero network); miss → fetch
  the readme endpoint, store in `mod_readme`, render.
- **Render pipeline (untrusted mod-author markdown = stored-XSS surface):**
  `goldmark` (raw HTML **disabled**) → AST pass dropping `<img>` whose src host ≠
  `gcdn.thunderstore.io` → `bluemonday` sanitize; links forced
  `target=_blank rel="noopener nofollow"`. Basic markdown only (headings, emphasis,
  lists, links, code, blockquotes, tables, CDN-only images). Render markdown→HTML on
  each view (goldmark is fast); inject via `templ.Raw` on the sanitized output only.
- **New deps:** `github.com/yuin/goldmark`, `github.com/microcosm-cc/bluemonday`.

### Not doing

- No arbitrary-host images (privacy + CSP); rich visuals live behind the Thunderstore
  link.
- No mutable-metadata (downloads/rating) live fetch on detail view — use index values,
  so a detail view is at most one network call (the README) and a cache hit is zero.

---

## Deploy & ops runbook

```sh
# build + push a new image
cd ~/projects/agrelha
git add -A && git commit -m "…"
task build:image TAG=<next>        # -> registry.ykhi.xyz/agrelha:<next>

# point the deployment at it
cd ~/projects/yaya-ops
#   edit manifests/agrelha-app.yaml image tag -> <next>
git add manifests/agrelha-app.yaml && git commit && git push
kubectl -n agrelha rollout status deploy/agrelha
```

- **Dev loop:** `cp .env.example .env` (leave `OIDC_ISSUER` empty to bypass auth),
  `go mod tidy` once, `task watch`. k8s/metrics/mods degrade gracefully off-cluster.
- **Secrets** live in tal `02-platform-config` (`vault_kv_secret_v2 "agrelha"`):
  `agrelha_codeberg_username/token`, `agrelha_oidc_client_id/secret`; registry pull
  creds reuse `var.zot_*`. `tofu apply` after changing them.
- **Zitadel app** requires: redirect URI `https://agrelha.ykhi.xyz/auth/callback`,
  post-logout URI `https://agrelha.ykhi.xyz/login`, and **"User Info inside ID Token"**
  enabled. `ALLOWED_EMAIL=ykhi@proton.me` in `agrelha-config`.
- **DNS:** `agrelha.ykhi.xyz` → `192.168.20.220` (Traefik LB), grey-cloud/DNS-only,
  WireGuard-only.

---

## Gotchas we already hit (don't re-debug these)

- **Every `*.ykhi.xyz` host** must A-record to **`192.168.20.220`** (Traefik LB),
  DNS-only. Pointing at a public IP → Traefik serves its default self-signed cert.
- **Zot** rejects Docker schema-2 manifests with 415 unless `"compat":["docker2s2"]`
  is set (it is). `docker build` + `docker push` then works.
- **Datastar v1.0.2:** `data-on-load` does NOT fire (no `load` event on a div). Use
  `data-effect="@get('/sse')"`. SSE frames are hand-written (`internal/sse`) because
  the datastar-go SDK is net/http and Fiber is fasthttp.
- **Thunderstore** `v1/package/` is ~162 MB — never fetch it in a request; stream it in
  the background (done).
- **htpasswd** for Zot is derived in tofu (`loafoe/htpasswd`) from `zot_password` — one
  source of truth, no drift.
- **ExternalSecret ↔ OpenBao drift:** a new ExternalSecret property must be seeded in
  tal `apps-secrets.tf` + `tofu apply` or the pod sits in `CreateContainerConfigError`.

---

## Layout

```
cmd/api/main.go                 entrypoint
cmd/web/pages/*.templ           templ templates for Valheim, Minecraft, Admin, Wizard, Login
internal/web/assets/                 Tailwind CSS + vendored Datastar JS
internal/domain/                pure domain models (Instance, Game, Pack, Budget, Access, Tier)
internal/ports/                 technology-agnostic interfaces (Runtime, StateStore, Reconciler, Auth, Game)
internal/adapters/              technology implementations:
  runtime/k8s/                  Kubernetes client adapter
  runtime/docker/               Docker Engine REST API & socket client adapter
  state/git/                    GitOps (go-git) declarative state committer adapter
  state/local/                  Local filesystem & SQLite history/rollback state adapter
  reconcile/argocd/             ArgoCD asynchronous convergence adapter
  reconcile/compose/            Docker Compose synchronous convergence adapter & YAML renderer
  auth/oidc/                    Zitadel OIDC authenticator adapter
  auth/local/                   Argon2id local user authenticator adapter
internal/games/                 in-tree game engines:
  valheim/                      Valheim engine & .r2z exporter
  minecraft/                    Minecraft engine & .mrpack exporter
internal/http/                  feature HTTP handlers:
  shared/                       render, flash, format, actor, logging, sse
  dashboard/                    landing page & live Datastar tile signals
  access/                       whitelisting, ops, admission, audit history
  backups/                      backup status, snapshots, restore, download
  console/                      log streaming, RCON interactive terminal
  content/                      mods, configs, updates, export, img proxy
  instances/                    Minecraft lifecycle, detail, configs, wizard cart, backup scheduler
internal/server/                Fiber server routing, middleware, and delegating shims
  store/                        SQLite DB + InstanceRepo (players, audit, mod_index, users, state_history)
  kube/                         raw Kubernetes client (wrapped by runtime/k8s)
  gitops/                       raw go-git committer (wrapped by state/git)
  backups/                      NAS backup directory stat
  content/                      thunderstore, modrinth, modpackindex, mcversions
internal/app/                   application services (backup scheduler, log ingest)
internal/config/                runtime configuration loader
```
