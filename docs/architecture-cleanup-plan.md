# Architecture cleanup

Companion to `docs/modularization-plan.md`, which ran phases 0–9. Those phases
built the domain, the ports and the adapters. This one finishes the job they
left half-done: the seams exist, but most of the code still reaches around them.

## Audit (2026-09-09)

Taken from `go list` and the interface inventory, not from reading.

### Clean

- `internal/domain` imports nothing.
- `internal/ports` imports only `domain`.
- `internal/mods` and `internal/admins` depend on ports alone.
- Adapters implement ports and do not import one another, except
  `runtime/k8s → kube`, which is an adapter using its own client.

### Violations

| # | Violation | Evidence |
|---|---|---|
| 1 | **`ports.Game` is dead.** No production consumer. `internal/games/minecraft` is imported by nothing but its own test; `internal/games/valheim` is imported for one free function, `WithBepInEx`. `ResolveContent` returns `domain.ContentSet{}, nil` in both implementations. | `go list` importers; `games/*/game.go` |
| 2 | **`internal/app` is not an application layer** — it imports `adapters/kube`, `adapters/store`, `config`, `minecraft`. | `app/backups.go`, `app/ingest` |
| 3 | **Inverted interfaces, un-inverted data.** `modpack.ModrinthProvider` is declared consumer-side but its methods return `*modrinth.Project`, `[]modrinth.Version`. `minecraft/compat.go` takes `*modrinth.Client` outright. | `modpack/mrpack.go:104-111`, `minecraft/compat.go:19` |
| 4 | **`ports.ContentProvider` is too thin to replace that** — `ID()` + `Search()`, where `modpack` needs `GetProject`, `GetProjectVersions`, `GetProjects`. | `ports/game.go:10` |
| 5 | **No inversion at the HTTP boundary.** All 7 `internal/http/*` packages import `adapters/kube` and `adapters/store`; their `Config` structs take `*store.Store`, `*k8s.Client`, `*minecraft.InstanceManager`. | `http/dashboard/handler.go` |
| 6 | **`internal/http/*` imports `cmd/web/pages`** — `internal` depending on `cmd`. | 6 of 7 http packages |
| 7 | **Views typed on infrastructure** — `store.Player`, `store.HistoryEntry`, `thunderstore.SearchResult` in templ signatures. 3 types, 4 of 21 files. | `pages/*.templ` |
| 8 | **`server.New` is a god constructor** — 27 fields, constructs 14 concrete adapters, calls `os.Exit(1)`. Composition root fused into the HTTP server. | `server/server.go:102` |
| 9 | **Adapters depend on global config** — `auth/oidc` and `app` take `*config.Config` rather than narrow options. | `auth/oidc/oidc.go` |

Ten packages also belong to no layer at all: `minecraft`, `modpack`, `mods`,
`admins`, `metrics`, `sse`, `mdrender`, `logging`, `build`, `config`.

## Decisions

- **Rebuild `ports.Game` from a real slice**, do not adopt it as designed. It
  was written top-down with no consumer, and one of its seven methods is a
  proven no-op. Route Valheim's mods+access path through a Game abstraction
  first, let the handler's needs reshape the interface, then do Minecraft, and
  keep only what two real consumers demanded.
- **Layout: `domain / app / infra / web`**, with `internal/ports` kept central.
  One directory listing every seam navigates better than seams scattered across
  their consumers, and the package is already clean.
- **Move the tree first**, as one pure-move commit. Everything after is written
  in its final home rather than moved twice.
- **Enforce with an architecture test in the repo**, not a linter and not
  review. A hand check missed `internal/minecraft → internal/store` earlier
  precisely because it only looked for three package names.
- **Templates take domain types.** No view-model layer for a three-type leak.
- **Done means the architecture test passes with zero whitelisted exceptions.**

## Target directory structure

```
cmd/
  agrelha/                  composition root: config → adapters → services → routes
internal/
  domain/                   entities, value objects, invariants. Imports NOTHING.
    instance.go             Instance, Tier, Lifecycle, Availability, Loader, Source
    content.go              ContentItem, ContentSet, Pack, Provider
    access.go               Admission, Operator, Player
    history.go              HistoryEntry
    game.go                 GameID, Display, RuntimeSpec, AdmissionModel, Bundle
    budget.go               capacity rules
  ports/                    every seam, one directory. Imports domain only.
    state.go                StateStore
    reconciler.go           Reconciler
    runtime.go              Runtime
    repository.go           InstanceRepository
    content.go              ContentProvider  (widened in phase F)
    game.go                 Game             (rebuilt in phase G)
    auth.go                 Auth, UserStore
  app/                      application services. Imports domain + ports ONLY.
    instances/              InstanceManager, scheduler, budget admission
    mods/                   Valheim mod list
    admins/                 Valheim operators
    access/                 Minecraft whitelist + ops
    content/                pack resolution, mrpack/r2z assembly
    backups/                backup scheduling
    ingest/                 player-presence ingestion
    games/                  Valheim + Minecraft implementations of ports.Game
  infra/                    adapters. May import domain + ports. Never app, never web.
    store/                  SQLite: instances, history, mods, presence, users
    kube/                   client-go: pods, logs, metrics, configmaps, scale
    gitops/                 go-git committer
    state/                  git | local | unconfigured   (StateStore)
    reconcile/              argocd | compose             (Reconciler)
    runtime/                k8s | docker                 (Runtime)
    content/                modrinth | thunderstore | modpackindex | mcversions
    auth/                   oidc | local                 (Auth)
    rcon/                   Minecraft RCON client + pool
    backups/                NFS backup inspection
  web/                      delivery. May import domain + ports + app. Never infra.
    routes.go               route table
    handlers/               access | backups | console | content | dashboard | instances | wizard
    shared/                 actor, flash, format, render, sse helpers
    pages/                  templ components  (moved out of cmd/)
    sse/                    Datastar wire protocol
    mdrender/               markdown → sanitised HTML
    metrics/                Prometheus endpoint
  platform/                 cross-cutting, no business meaning
    config/                 environment loading
    logging/                slog setup
    build/                  version + SourceURL
```

### Allowed dependency edges

The architecture test encodes exactly this table. Everything not listed is a
failure.

| From | May import |
|---|---|
| `domain` | (nothing internal) |
| `ports` | `domain` |
| `app` | `domain`, `ports`, `platform` |
| `infra` | `domain`, `ports`, `platform` |
| `web` | `domain`, `ports`, `app`, `platform` |
| `platform` | (nothing internal) |
| `cmd/agrelha` | everything |

Two consequences worth stating out loud: `web` may not import `infra`, which is
what forces phases E and H; and nothing under `internal/` may import `cmd/`,
which is what forces `pages` to move.

### Where today's packages land

| Today | Target |
|---|---|
| `cmd/api` | `cmd/agrelha` |
| `cmd/web/pages` | `internal/web/pages` |
| `internal/http/*` | `internal/web/handlers/*` |
| `internal/http/shared` | `internal/web/shared` |
| `internal/sse`, `mdrender`, `metrics` | `internal/web/{sse,mdrender,metrics}` |
| `internal/adapters/*` | `internal/infra/*` |
| `internal/mods`, `internal/admins` | `internal/app/{mods,admins}` |
| `internal/app/ingest` | `internal/app/ingest` (unchanged) |
| `internal/app/backups.go` | `internal/app/backups/` |
| `internal/games/*` | `internal/app/games/*` |
| `internal/config`, `logging`, `build` | `internal/platform/*` |
| `internal/server` | dissolved: wiring → `cmd/agrelha`, routes → `internal/web`, handlers → `internal/web/handlers` |
| `internal/minecraft` | split three ways (phase C) |
| `internal/modpack` | split two ways (phase C) |

`internal/minecraft` is the awkward one — it is four layers in one package:

| File | Target |
|---|---|
| `instance.go` | `domain` (Instance invariants, `CanSetLoader`, `PackOwnedFieldErr`) |
| `instance_manager.go` | `app/instances` |
| `mods.go`, `access.go` | `app/access` |
| `content.go`, `compat.go` | `app/content` |
| `manifests.go`, `templates/` | `infra/manifests` (renders Kubernetes YAML) |
| `backup.go` | `app/backups` |
| `rcon.go` | `infra/rcon` |

## Phases

Each compiles, passes tests and deploys. A bad step is one revert.

| Phase | Work | Done when |
|---|---|---|
| **A. Layout move** ✅ | Relocate whole packages, rewrite imports, `cmd/api`→`cmd/agrelha`, `pages` out of `cmd/`. No behaviour change. | Build + full suite green; diff is renames only |
| **B. Architecture test** ✅ | Encode the edge table as a Go test over the parsed import graph. | It runs, and its failure list matches the audit above |
| **C. Split `minecraft` + `modpack`** ✅ | Per the file table. `Instance` rules to domain, managers to app, RCON and manifests to infra. | `app` and `domain` no longer hold infrastructure |
| **D. Composition root** ✅ | Wiring leaves `server.New` for `cmd/agrelha`. No `os.Exit` outside `main`. `internal/web` becomes routing only. | `server.New` no longer constructs adapters |
| **E. Invert the HTTP boundary** | Handler `Config`s take ports, not `*store.Store` / `*k8s.Client` / concrete managers. Tests land WITH each inversion. | `web` no longer imports `infra`; handlers testable with fakes |
| **F. Content provider port** | Widen `ContentProvider` to what `app/content` needs; its DTOs become domain types. | `modrinth` is imported only by `infra` and `cmd` |
| **G. Game abstraction, rebuilt** | Valheim mods+access slice through a Game port, reshape, then Minecraft. Delete what neither demanded. | Handlers dispatch through `ports.Game`; no dead methods |
| **H. View types** | `store.Player`, `store.HistoryEntry` → domain; `thunderstore.SearchResult` → `domain.ContentItem`. | No infra types in templ signatures |
| **I. Narrow adapter config** | `auth/oidc` and `app/*` take options structs, not `*config.Config`. | Only `cmd/agrelha` imports `platform/config` |

### Scope honesty

This is roughly the size of phases 1–8 combined, on a codebase that works and
ships daily.

- **A–C** buy navigation and the layer shape.
- **D–E** buy testability, and are what make the 33.5% coverage number movable —
  handler tests are unwritable until the boundary is inverted.
- **F–G** are the expensive half and the only part that buys a third game.

Stopping after E is a coherent place to stand. Choose it deliberately rather
than discovering it midway.

## Progress

### Phase A — done (2026-09-09)

Pure move. Every package relocated into the target tree, all import paths
rewritten, `gofmt` clean, `go build` / `go vet` / `go test ./...` green, and the
full `task build` pipeline (Tailwind → templ → ldflags-stamped binary) verified
end to end.

Two adjustments to the plan, made while executing:

- **`internal/server` stays put for now.** Moving it would mean renaming its
  package and rewriting every handler reference, which is a refactor, not a
  move. It is dissolved in phase D as planned.
- **`internal/app/backups.go` was not sub-packaged.** It is already in the app
  layer; splitting it out collides with `infra/backups` and needs an alias, so
  it waits for phase C.

Also updated for the new paths: `Taskfile.yml` (including the ldflags target,
now `agrelha/internal/platform/build.Version`), `.gitignore`, `.air.toml`
(needed nothing — it is path-agnostic), `third_party/README.md`, and the README
layout section, which was stale anyway (it still listed `internal/auth`,
`internal/k8s`, `internal/valheim` and `internal/thunderstore`, none of which
have existed for several phases).

**Confirmed gone:** no package under `internal/` imports `cmd/` any more —
violation #6 is closed by the move alone.

**Still open, now plainly visible in the graph:** all seven
`web/handlers/* → infra/{kube,store}` edges (#5), `app → infra` (#2),
`minecraft`/`modpack` → `infra/content/modrinth` (#3), `web/pages → infra` (#7),
and `infra/auth/oidc → platform/config` (#9).

### Phase B — done (2026-09-09)

`internal/arch/arch_test.go`. Pure stdlib (`go/parser` + `filepath.WalkDir`), no
new dependency and no subprocess, so it runs inside the existing `go test ./...`.

It works as a **ratchet** rather than a red test: the edge table is the permanent
spec, and `exceptions` lists the violations that exist today, each tagged with
the phase that removes it. The test fails on three things:

1. a **new** forbidden edge (a regression),
2. a **stale exception** — the violation is gone, so delete the entry,
3. an **unclassified package**, or a **stale layer prefix** nothing lives under
   any more.

All three were verified by deliberately introducing each condition and confirming
the test caught it. Emptying the exception list yields exactly the 20 entries,
collapsed from 68 raw edges — matching the audit with nothing missing and nothing
invented.

Two design notes:

- Exceptions are keyed `package -> layer`, not `package -> package`. That keeps
  the list at ~20 readable entries instead of 68, at the cost of not noticing a
  package adding a *second* import from a layer it already breaches. Acceptable:
  every package on the list is scheduled for repair.
- `internal/minecraft` and `internal/modpack` are classified **app**, and
  `internal/server` is classified **web** — the layer they are becoming, not a
  "legacy" bucket. That is what makes violations #3 and #8 show up rather than
  being excused, and their entries disappear when phases C and D dissolve them.

**Done means `exceptions` is empty and the `layers` table has no entry for
`internal/minecraft`, `internal/modpack` or `internal/server`.**

### Phase C — done (2026-09-09)

`internal/minecraft` and `internal/modpack` no longer exist. Ledger: **20 → 19**,
and the app layer is clean apart from the two content packages phase F owns.

Where the 1,258 lines went:

| Was | Now | Note |
|---|---|---|
| `instance.go`, `content.go` | *deleted* | Both were pure re-export shims — every symbol already existed in `domain`. No new domain code was written; the work was rewriting ~120 call sites across 22 files |
| `instance_manager.go` | `app/instances` | |
| `mods.go`, `access.go` | `app/access` | |
| `compat.go` | `app/content` | Still imports modrinth — the violation moved, it did not vanish. Phase F |
| `manifests.go` + `templates/` | `infra/manifests` | |
| `rcon.go` | `infra/rcon` | `RconClient`→`Client`, `RconPool`→`Pool` (no `rcon.RconClient` stutter) |
| `backup.go` | `infra/backups` | Filesystem operations, so infra — not `app/backups` as the plan said |
| `internal/modpack` | `app/modpack` | |
| `internal/app/backups.go` | `app/backups/` | |

**Three dependencies were inverted rather than moved**, because moving alone
would have left `app` importing `infra`:

- `ports.SpecRenderer` — `InstanceManager` rendered Kubernetes YAML directly. It
  now takes a renderer, and `nodeSelector` moved off the manager onto the
  adapter where it belongs. A Docker adapter renders compose files through the
  same seam.
- `ports.Console` — `AccessManager` held a `*RconClient`; it now takes an
  interface with `Execute`.
- `ingest.presenceStore` — a four-method consumer-side interface, matching the
  `logStreamer` interface already in that file. Local rather than in `ports`:
  one consumer, and it keeps the file internally consistent.

**Deviations from the plan, with reasons:**

- `backup.go` went to `infra/backups`, not `app/backups`. It is `os.Remove` and
  `filepath.Glob` — infrastructure. Merging it into the existing package also
  avoided a name collision.
- `internal/modpack` was **not** split two ways. All three files are content
  assembly, and the thing that actually needs separating is the modrinth
  coupling, which is phase F's job. Splitting it here would have been motion
  without progress.
- `app/backups` kept its `infra` exception, re-tagged from phase C to E. It
  creates Kubernetes Jobs, and `RunTask` was deliberately left out of
  `ports.Runtime` because its only implementation takes PVC claim names. It
  needs the volume model, not a move.

**A mistake worth recording:** the bulk symbol rewrite qualified identifiers
inside comments and struct field names too, turning `Pack:` into `domain.Pack:`
and prose into "A domain.Pack owns its domain.Instance's domain.Loader". The
compiler caught the field names; the comments it could not, and they needed a
separate pass. Bulk-renaming by regex needs a comment-and-field guard.

### Phase C regression — typed nil in an interface (2026-09-09)

Inverting `AccessManager`'s RCON dependency introduced a runtime panic that the
whole test suite passed straight through. Found by running the app, not by CI.

`AccessManager.rcon` went from `*RconClient` to `ports.Console`. Every guard in
that file is `if a.rcon != nil`. When RCON is unconfigured, `server.New` passed
`s.mcRcon` — a **nil `*rcon.Client`** — into the interface parameter, producing an
interface value that holds a type and a nil pointer. Such a value is **not**
`nil`, so every guard passed and `Execute` dereferenced a nil receiver:

```
panic: runtime error: invalid memory address or nil pointer dereference
agrelha/internal/infra/rcon.(*Client).Execute(0x0, ...)
agrelha/internal/app/access.(*AccessManager).OnlinePlayers(...)
agrelha/internal/web/handlers/dashboard.(*Handler).TileSignals(...)
```

It fired on the dashboard SSE tile refresh, so the panic hit on first page load
with no RCON password set — the default configuration for a new install.

Fixed in two places:

- **The wiring**: `server.New` now builds an explicitly nil `ports.Console` when
  there is no client, instead of stuffing a typed nil into the interface.
- **The adapter**: `Client.Execute` and `Client.Close` guard `c == nil`, matching
  the `r == nil || r.c == nil` pattern the k8s runtime adapter already used.

Two regression tests, both verified to fail before the fix — the typed-nil one
reproduces the exact panic.

**The lesson for the phases still to come:** every remaining phase replaces a
concrete pointer with an interface, and every one of them can hit this. When
inverting a dependency whose concrete value is allowed to be nil, the nil check
must move to the construction site, or the adapter must be nil-receiver safe.
Prefer both.

### Phase D — done (2026-09-09)

`server.New` constructs no adapters and there is no `os.Exit` anywhere under
`internal/` any more. Ledger unchanged at **19** — phase D is about testability,
not about edges.

**`internal/wiring` is the composition root**, not `cmd/agrelha`. The plan said
`cmd`, and that was wrong: two existing tests
(`TestLocalAuthFlow`, `TestServer_DockerAdapterBootstrap`) exercise the wiring
*decisions* — which authenticator, which state store, which reconciler — and Go
forbids importing `package main`. Putting the root in `cmd` would have deleted
two real tests to satisfy a diagram. `cmd/agrelha/main.go` is now nine lines of
`config.Load` → `wiring.Build` → `server.New` → `Listen`.

- `wiring.Build(ctx, cfg) (server.Deps, error)` — **returns an error** where the
  old code called `os.Exit(1)` on a failed store open, which is what made the
  path untestable.
- Split into `buildThunderstore`, `buildDeclarativePlane`, `buildRuntime`,
  `buildAuth`, so each decision reads as one function instead of a 120-line
  straight line.
- `server.New(cfg, Deps)` assigns fields and builds handlers. The unused
  `WithAuth` / `WithStateStore` / `WithReconciler` options were deleted — `Deps`
  makes them redundant, and nothing outside the package used them.
- `internal/server/modindex.go` deleted: it was a two-function forwarder to
  `contenthttp.RowsToResults`, which `wiring` now calls directly.
- The moved tests improved on the way: `TestServer_DockerAdapterBootstrap` used
  to reach into unexported `s.stateStore` / `s.reconciler`. It now asserts on the
  `Deps` that `wiring.Build` returns, which is what it was actually testing.

**A new layer, `root`**, covers `cmd/` and `internal/wiring`: it may import
everything, and it is the only layer allowed to import `platform/config`. That
last rule is what will drive phase I.

`internal/server` keeps its `infra` and `config` exceptions, re-tagged D→E:
`Deps` still declares concrete types (`*store.Store`, `*k8s.Client`). Phase E
turns those into ports.

**Verified by running it**, not only by the suite: `/healthz` 200, `/` 200,
`/sse` 200 with no panic on the tile-refresh path that failed in phase C,
`/minecraft` 302 to `/auth/login`, clean shutdown.

**Naming trap found:** the directory `internal/infra/kube` contains
`package k8s`. Every import of it must be written `"agrelha/internal/infra/kube"`
and referenced as `k8s.`. Worth reconciling, but renaming it is churn for its own
sake — noted rather than done.
