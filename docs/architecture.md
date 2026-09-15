# Architecture

How agrelha is laid out, why it is laid out that way, and which rules a change
has to obey. This describes the system **as it is now**. The decision that
produced the shape is recorded separately in
[ADR 0002](adr/0002-hexagonal-layering-and-server-dissolution.md); an ADR is a
record of what was decided and when, so it is never rewritten as the code moves.

Where this document and `internal/arch/arch_test.go` disagree, **the test is
right and this document is wrong**. Fix the document.

## Glossary

Structural vocabulary only. The *domain* language — Instance, World, Mod list,
Incident, Vanilla — lives in [`CONTEXT.md`](../CONTEXT.md).

**Layer** — one of the eight top-level groupings under `internal/`. A package's
layer is decided purely by its import path prefix, so moving a file between
directories changes what it is allowed to import.

**Port** — an interface in `internal/ports` naming something the core needs from
the outside world. Ports are declared by the consumer, not the implementer: the
core says what it wants, and the outside world conforms.

**Adapter** — a concrete implementation of a port. Driven adapters live in
`internal/infra`; driving adapters live in `internal/web`.

**Driving (inbound)** — something that calls *into* the core. HTTP handlers are
the only driving adapters agrelha has.

**Driven (outbound)** — something the core calls *out* to. Kubernetes, git,
SQLite, Thunderstore, RCON, Docker.

**Plane** — one of the two ways agrelha changes a server. The declarative plane
writes desired state and waits; the imperative plane acts on the cluster now.
See [The two planes](#the-two-planes).

**Ratchet** — `internal/arch/arch_test.go`, the test that fails the build when a
layer imports something it may not. It only ever tightens.

## The hexagon as agrelha implements it

The core is `domain` + `ports` + `app`. It contains every rule about what a
game server *is* and what may be done to one, and it knows nothing about HTTP,
Kubernetes, git or SQLite. Everything technical is an adapter on the outside,
reachable only through an interface the core declared.

```
                         driving (inbound)
                    ┌───────────────────────────┐
                    │          web              │
                    │  handlers · pages · sse   │
                    └─────────────┬─────────────┘
                                  │ calls
                                  ▼
        ┌─────────────────────────────────────────────────┐
        │                      app                        │
        │   use cases: instances, health, modupdates,      │
        │   backups, access, admins, ingest, games         │
        │                                                 │
        │      ┌───────────────────────────────────┐      │
        │      │              ports                │      │
        │      │  Runtime · StateStore · Reconciler │      │
        │      │  Game · Console · Auth · …         │      │
        │      │                                   │      │
        │      │     ┌───────────────────────┐     │      │
        │      │     │        domain         │     │      │
        │      │     │  Instance · ModRef ·  │     │      │
        │      │     │  Incident · Tier · …  │     │      │
        │      │     │   (imports nothing)   │     │      │
        │      │     └───────────────────────┘     │      │
        │      └───────────────────────────────────┘      │
        └─────────────────────────┬───────────────────────┘
                                  │ implemented by
                                  ▼
                    ┌───────────────────────────┐
                    │          infra            │
                    │  kube · gitops · store ·  │
                    │  rcon · content · runtime │
                    └───────────────────────────┘
                         driven (outbound)

   wiring  — the composition root: the only package that may see all of them
   platform — config, logging, build stamps: bootstrap, no domain knowledge
   arch    — the ratchet test; depends on nothing, guards everything
```

### Why `web` and `infra` are separate top-level packages

Both are adapters, and a naive reading of the hexagon puts them on the same ring
— so why not one `adapters` package?

The tempting wrong answer is "it would be an import cycle". It would not:
`adapters/http → app → ports ← adapters/kube` is acyclic and compiles fine.

The real reason is that the ratchet classifies packages **by import-path
prefix**. Under one `infra` prefix, the driving adapters need `infra → app` to
be allowed — and once it is allowed, nothing stops `infra/kube` from importing
`app` too, because the rule cannot tell a driving adapter from a driven one
when they share a prefix. The distinction is real but invisible to any check
you could write. Splitting the prefix is what makes both halves of the rule
enforceable at once:

- `web` may import `app`; `infra` may not.
- `app` may import neither.

### Which way the arrows point

The commonest confusion is to read the hexagon as a runtime call sequence and
conclude that `infra` sits between `web` and `app`. It does not. `infra` is not
"the technical half of the program"; it is "the things the core calls **out**
to". `web` is not a consumer of `infra` — the two are peers on opposite sides
of the core, and `web` never imports `infra` at all.

Runtime call flow and compile-time dependency are different graphs, and the
second one is the point of the architecture:

```
  runtime (who calls whom):
      web handler ──► app service ──► [ports.Runtime] ──► infra/kube ──► k8s API

  compile time (who imports whom):
      web ──► app ──► ports ◄── infra
                        ▲
                     domain
```

At runtime, control flows left to right, straight through. At compile time,
`app` does not know `infra` exists: it depends on the interface, and `infra`
depends on that same interface from the other side. That inversion — both sides
pointing inward at a contract the core owns — is the whole trick. It is why
`app` can be tested with a fake `Runtime` and no cluster, and why swapping
Kubernetes for Docker is a wiring change rather than a rewrite.

One consequence to keep straight: a handler that needs data must call `app`,
never `infra/store`. Today no package under `internal/web` imports `internal/infra`,
and the ratchet keeps it that way.

The rule is about direction, not about technicality. `web` is allowed to contain
technical code — `web/sse` speaks a wire protocol, and the image proxy in
`web/handlers/content` makes outbound HTTP calls of its own. What it may not do
is reach for an adapter that implements a port, because that is the core's
business and would route around the contract.

### Why `app` is core and not infrastructure

`app` holds use cases — `InstanceManager.CreateInstance`, `health.Watcher.Check`,
`modupdates.Checker.Apply`. These are business processes: they enforce the RAM
budget, refuse to add mods to a Vanilla world, decide that a crash is new. None
of it is technical plumbing, and all of it must be testable without a cluster.
That is why `app` may not import `infra`, and why every external thing it needs
arrives as a port or a narrow function option.

## The import matrix

Taken from `internal/arch/arch_test.go`. A layer may import only the layers
marked `✓`, plus the Go standard library and third-party packages appropriate
to its level.

| from ↓ / to → | domain | ports | platform | app | infra | web | root |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **domain**   |   |   |   |   |   |   |   |
| **ports**    | ✓ |   |   |   |   |   |   |
| **platform** |   |   |   |   |   |   |   |
| **meta**     |   |   |   |   |   |   |   |
| **app**      | ✓ | ✓ | ✓ | ✓ |   |   |   |
| **infra**    | ✓ | ✓ | ✓ |   | ✓ |   |   |
| **web**      | ✓ | ✓ | ✓ | ✓ |   | ✓ |   |
| **root**     | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

`root` is `internal/wiring` and `cmd/`. `meta` is `internal/arch`. `domain`,
`platform` and `meta` import nothing from the module at all — the ratchet in
particular must not depend on the thing it judges.

One extra rule the table cannot express: **`internal/platform/config` may be
imported only by `root`.** Everything else takes narrow functional options or
ports instead, so no adapter or service can reach for a global config value.
This is enforced, not aspirational — `configImporters` in the ratchet allows
exactly one layer.

The exception ledger is empty. When it was written it held twenty entries; the
cleanup recorded in [`architecture-cleanup-plan.md`](architecture-cleanup-plan.md)
took it to zero.

### What the ratchet fails on

Run it with `go test ./internal/arch/...`. It reports four distinct problems:

1. **Forbidden dependency** — a package imports a layer it may not.
2. **Unclassified package** — a new package matches no known prefix. Add it to
   `layers` or move it; the build will not go green until you decide which layer
   it is in.
3. **Stale exception** — an entry in the ledger whose violation no longer
   exists. Exceptions are temporary by construction; when the code is fixed the
   entry must go, or the next person will think the rule is softer than it is.
4. **Unmatched prefix** — a layer prefix that matches no package, meaning the
   layer list itself has gone stale.

There is also a fifth check, added with this document: **every package under
`internal/` must appear in the package table below**, and every row must name a
package that exists. See `TestPackageTable`.

## The two planes

Layering explains the shape. It does not explain why `StateStore` and `Runtime`
both exist, or why `InstanceManager` sometimes commits to git and sometimes
talks to the cluster. That is the two-plane split, and it is the thing most
likely to be got wrong.

```
  operator action
        │
        ├── changes WHAT SHOULD BE TRUE ──────► declarative plane
        │   create instance, install mod,        ports.StateStore
        │   edit config, change tier             └─ infra/state/git ─► git commit
        │                                                              to yaya-ops
        │                                            ▼
        │                                     ArgoCD converges
        │                                     (ports.Reconciler)
        │                                            ▼
        │                                     cluster matches git
        │
        └── changes WHAT IS HAPPENING NOW ───► imperative plane
            start, stop, restart, read logs,     ports.Runtime, ports.Console
            exec a command, read status          └─ infra/runtime/k8s ─► client-go
```

**Declarative** changes are written as desired state and then waited for. They
are durable: ArgoCD runs with `selfHeal: true`, so anything not written to git
is reverted the moment it notices. A mod installed by patching a ConfigMap
directly would survive until the next sync and then silently vanish.

**Imperative** changes are actions with no persistent desired state. Starting a
server is a scale to 1; nothing in git records "running", because the operator
may stop it again in a minute.

The practical rule: **if the answer should survive a full cluster rebuild, it
belongs in the declarative plane.** Mod lists, configs, instance definitions and
tiers do. Lifecycle, logs and console commands do not.

Both planes are pluggable, and the composition root picks per environment
(`wiring.buildDeclarativePlane`):

| configuration | StateStore | Reconciler |
|---|---|---|
| `GIT_REPO_URL` + `GIT_TOKEN` set | `state/git` | `reconcile/argocd` |
| `RUNTIME=docker`, or `LOCAL_STATE_DIR` set | `state/local` | `reconcile/compose` |
| neither | `state/unconfigured` (reads nothing, refuses writes) | `reconcile/argocd` |

The Runtime is chosen separately, by `RUNTIME` alone: `runtime/docker` when it
is `docker`, `runtime/k8s` otherwise. The two planes are configured
independently, so a Docker runtime with a git state store is a legal — if
unusual — combination.

Three consequences worth knowing, each with its own ADR:

- **One Deployment per Instance** ([ADR 0001](adr/0001-one-deployment-per-instance.md))
  — the declarative plane renders a full manifest set per world, so a world is a
  directory in git rather than a row in a table.
- **A mod that cannot be fetched is fatal** ([ADR 0003](adr/0003-mod-install-failure-is-fatal.md))
  — because the declarative plane cannot ask a question at boot, the setup step
  refuses rather than starting with a wrong mod set.
- **Reads are expensive on the git plane** — every `StateStore.Get` currently
  clones the repository. See [`git-state-read-cost.md`](git-state-read-cost.md).

## Testing, by layer

The layering exists so that most of the system can be tested without any of the
outside world. What a test may fake follows directly from which layer it is in.

**`domain`** — no fakes, no context, no I/O. If a domain test needs a stub, the
type under test has a dependency it should not have.

**`ports`** — not tested. Interfaces have no behaviour; a test here would assert
that Go compiles.

**`app`** — hand-written fakes for the ports it consumes, in the test file that
uses them. This is where most behavioural coverage should live: it is the layer
with the rules, and it runs in milliseconds with no cluster, no network and no
database. `app` tests must never reach for a real adapter; if one is hard to
fake, the port is shaped wrong.

**`infra`** — tested against the real protocol, faked at the boundary *below*
the adapter: an `httptest.Server` for a REST client, a temporary directory and a
real `git init` for the git store, a `fake.NewSimpleClientset` for client-go, a
temp file for SQLite. An infra test proves the translation is right, so faking
the translation defeats it.

**`web`** — driven through `app.Test(req)` against a Fiber app wired with fake
`app` services. Assert on status, on the rendered HTML, and on the SSE frames —
not on internal handler state.

**`wiring`** — end-to-end smoke: build the real router over fakes and check the
routes exist and answer. It is the only place where a mistake in composition
(a port left nil, a handler never registered) can be caught.

## Packages

Every package under `internal/`. `TestPackageTable` in `internal/arch` fails if
this list and `go list ./internal/...` disagree, in either direction.

### Core

| package | purpose |
|---|---|
| `domain` | Business types and invariants: Instance, Tier, ModRef, Incident, Bundle. Imports nothing. |
| `ports` | The interfaces the core needs from outside: Runtime, StateStore, Reconciler, Game, Console, Auth, PackageCatalog, InstanceRepository, and the recorder interfaces. |
| `app/access` | Minecraft mods, whitelist and operator persistence (`ModManager`, `AccessManager`), synchronised to the live console. |
| `app/admins` | Valheim admin Steam64 IDs, held in declarative state. |
| `app/backups` | Scheduled world snapshots and retention pruning. |
| `app/content` | Mod cart compatibility analysis before an instance is created. |
| `app/games/minecraft` | The Minecraft `ports.Game`: runtime spec, content resolution, client bundle export, telemetry. |
| `app/games/valheim` | The Valheim `ports.Game`, including BepInEx handling and `.r2z` profile export. |
| `app/health` | Turns what the runtime knows about a dead server into a recorded Incident. |
| `app/ingest` | Tails server logs and turns connection lines into the player roster and join events. |
| `app/instances` | `InstanceManager`: the central use case. Create, start, stop, delete, mods, configs, backups, budget enforcement. |
| `app/modpack` | Builds and parses modpack archives: `.mrpack`, Prism zips, raw mod lists. |
| `app/mods` | **Unused.** Edited the global `valheim-mods` ConfigMap, which no longer exists; its last consumer was removed with the global mods page. Constructed in `wiring` but never read. |
| `app/modupdates` | Asks the catalogue whether installed mods have newer versions, applies the ones chosen, and holds the single step back. |

### Driven adapters

| package | purpose |
|---|---|
| `infra/auth/local` | Password authentication with Argon2 hashing and session cookies. |
| `infra/auth/oidc` | OIDC authentication against Zitadel, plus a dev-mode bypass. |
| `infra/backups` | Backup archive naming, stat and pruning on the local or NFS filesystem. |
| `infra/content/mcversions` | Minecraft version list and version comparison. |
| `infra/content/modpackindex` | Modpack Index REST client. |
| `infra/content/modrinth` | Modrinth v2 REST client: projects, versions, dependencies. |
| `infra/content/thunderstore` | Thunderstore client: warm package index for search, per-package version and README lookups. |
| `infra/gitops` | Clones the ops repository, edits a document in place preserving YAML structure, commits and pushes. |
| `infra/kube` | client-go wrapper: deployments, pods, logs, exec, services, PVCs, jobs, metrics. |
| `infra/manifests` | Renders the Minecraft manifest set for an Instance from templates. |
| `infra/manifests/valheim` | Renders the Valheim manifest set, including the mod-reconciler init container. |
| `infra/rcon` | Minecraft RCON client and connection pool. |
| `infra/reconcile/argocd` | `ports.Reconciler` for ArgoCD: converge and report sync status. |
| `infra/reconcile/compose` | `ports.Reconciler` for Docker Compose. |
| `infra/runtime/docker` | `ports.Runtime` over the Docker engine API. |
| `infra/runtime/k8s` | `ports.Runtime` over Kubernetes, via `infra/kube`. |
| `infra/state/git` | `ports.StateStore` over `gitops.Committer`. |
| `infra/state/local` | `ports.StateStore` over local files and SQLite. |
| `infra/state/unconfigured` | `ports.StateStore` that reads nothing and refuses writes, for installations with no declarative plane. |
| `infra/store` | Embedded SQLite (modernc, pure Go): player roster and sessions, audit trail, event timeline, incidents, mod index cache, mod restore points, instance rows. |

### Driving adapters

| package | purpose |
|---|---|
| `web` | Route registration and the Fiber app: which handlers are public and which sit behind auth. |
| `web/components` | Typed presentation primitives — Button, StatusPill, TabBar, Modal — and the colour language that goes with them. |
| `web/handlers/access` | Admin grant and revoke, and the history page. |
| `web/handlers/backups` | Backup create, restore in place, restore as new, download. |
| `web/handlers/console` | Live console, log streaming and direct server commands. |
| `web/handlers/content` | Mod detail pages, global configs, and the SSRF-guarded image proxy. |
| `web/handlers/dashboard` | The public hub page and the main SSE signal stream. |
| `web/handlers/minecraft` | Minecraft instance pages: detail tabs, mod search and install, configs, lifecycle. |
| `web/handlers/valheim` | Valheim instance pages: detail tabs, mod search, mod updates, configs, lifecycle, profile export. |
| `web/handlers/wizard` | The Minecraft provisioning flow: pack and mod search, cart validation, creation. |
| `web/mdrender` | Renders untrusted upstream Markdown (mod READMEs) to sanitised HTML. |
| `web/metrics` | Prometheus handler and the counters the app publishes. |
| `web/pages` | templ page templates and the view types they render. |
| `web/shared` | Cross-handler helpers: actor identity, flash messages, request logging, human-readable sizes and durations. |
| `web/sse` | Emits the Datastar SSE wire protocol onto a `bufio.Writer`. |

### Supporting

| package | purpose |
|---|---|
| `arch` | The ratchet: layer rules, the exception ledger, and the package-table check. Depends on nothing. |
| `platform/build` | Version, commit and build date stamped in at link time. |
| `platform/config` | Environment configuration. Importable only by the composition root. |
| `platform/logging` | slog setup: level and format. |
| `wiring` | The composition root. The only package that decides which adapter satisfies which port, and the only one that may see every layer. |

## Adding something new

**A new use case** goes in `app`. If it needs something external, add a port
first and a fake second; the adapter comes last.

**A new external system** gets a port in `ports` named for what the *core*
wants, not for the vendor, and an adapter in `infra` named for the vendor.

**A new page** is a handler in `web/handlers` plus a template in `web/pages`.
If the handler grows a loop, a retry, or a second port, that logic belongs in
`app` and the handler should call it.

**A new package** must be classified: add its prefix to `layers` in the ratchet
if it introduces one, and add a row to the package table above. Both tests will
tell you if you forget.
