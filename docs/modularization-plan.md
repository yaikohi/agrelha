# Modularization plan — making agrelha open-source and adaptable

Status: **phases 0–8 done, phase 9 next.** Design agreed 2026-09-08;
last worked 2026-09-09. See docs/SESSION-HANDOFF-2026-09-09.md for pick-up state.

Vocabulary in this document follows [CONTEXT.md](../CONTEXT.md). Background on
why one Deployment per Instance: [ADR-0001](adr/0001-one-deployment-per-instance.md).

---

## 1. Goal and audience

agrelha is currently one person's control plane for one homelab. The goal is a
panel someone else can install without adopting that homelab.

**Audience, in shipping order:**

1. **GitOps homelabbers on Kubernetes** — the setup agrelha already assumes. The
   two-plane design (git owns desired state, the cluster owns actions) is the
   differentiator, so this audience needs modularisation, not a rewrite.
2. **Self-hosters on Docker/Compose** — a much larger audience, reached through a
   second set of adapters.
3. **Friend-group admins who want no infrastructure** — *not a third adapter*.
   They are audience 2 plus a good install story. **Two adapters, not three.**

**The discipline that makes this work:** every port is designed against
Kubernetes **and** a Docker adapter from the start. The Docker adapter may
return `ErrNotImplemented` for a while, but it must exist and must compile.

> A port validated by a single implementation is not a port — it is that
> implementation with extra steps.

This matters here specifically, because today's de-facto "runtime interface" is
21 methods including `ConfigMapData`, `ConfigMapMeta`, `Deployment`, `Replicas`,
`Scale` and `CreateBackupJob`. ConfigMaps, Deployments, replicas and Jobs are
Kubernetes concepts with no Docker equivalent. Lifting that surface as-is is the
single most likely way this effort fails.

---

## 2. Where the code is today

Measured 2026-09-08:

| Fact | Value |
|---|---|
| `internal/server` | **5,453 LOC** — roughly half the codebase |
| `internal/minecraft` | 1,812 LOC |
| `internal/valheim` | **49 LOC** — Valheim's real logic lives in `server` |
| Domain packages importing infrastructure | `minecraft`, `mods`, `admins` → `gitops`, `k8s` |
| Domain packages already clean | `modpack`, `valheim`, `store` |
| Config knobs | 43 total, **15 with no default** |
| Install artifacts shipped | **none** (no chart, no compose file) |

Two structural problems follow from that table:

- **Dependencies point outward.** The domain imports infrastructure, which is
  backwards for an onion. Nothing can be swapped until that is inverted.
- **There is no game abstraction.** 1,812 vs 49 LOC is not a difference in
  complexity between the games; it is Valheim's logic living in the handlers.
  Adding a third game today means editing the monolith.

---

## 3. Target architecture

```
        ┌──────────────────────────────────────────────┐
        │  adapters   k8s · docker · git · local · oidc │   outer
        ├──────────────────────────────────────────────┤
        │  ports      Runtime · StateStore · Reconciler │
        │             ContentProvider · Auth ·          │
        │             Repository · BackupStore          │
        ├──────────────────────────────────────────────┤
        │  domain     Instance · Game · Pack · Access   │   inner
        │             (no imports outward, ever)        │
        └──────────────────────────────────────────────┘
```

**Rule:** `domain` imports nothing from `ports` or `adapters`. `ports` are
interfaces defined in terms of domain types only. Adapters depend inward.

### Proposed layout

```
internal/
  domain/          instance, game, pack, access, budget — pure types + rules
  ports/           interfaces only, no implementations
  adapters/
    runtime/k8s/       wraps client-go; owns the manifest templates
    runtime/docker/    compose rendering + docker engine
    state/git/         go-git committer
    state/local/       files + SQLite
    reconcile/argocd/  commit, then wait for convergence
    reconcile/compose/ render, then `up`
    auth/oidc/         existing OIDC flow
    auth/local/        argon2id users
    content/modrinth · thunderstore · modpackindex · curseforge
  games/
    valheim/        implements domain.Game
    minecraft/      implements domain.Game
  http/             Fiber handlers, split per feature (see §7)
```

### The ports

Deliberately narrow — expressed in what a *game server* needs, not what
Kubernetes offers:

```go
// Runtime makes servers run. No ConfigMaps, no Deployments, no replicas.
type Runtime interface {
    Start(ctx, InstanceRef) error
    Stop(ctx, InstanceRef) error
    Restart(ctx, InstanceRef) error
    Status(ctx, InstanceRef) (Status, error)      // lifecycle + availability
    Logs(ctx, InstanceRef, opts) (io.ReadCloser, error)
    Metrics(ctx, InstanceRef) (Metrics, error)
    RunTask(ctx, TaskSpec) (TaskHandle, error)    // backup/restore one-shots
}

// StateStore persists desired state. Says nothing about how it becomes real.
type StateStore interface {
    Put(ctx, path string, doc Document, msg string) error
    Get(ctx, path string) (Document, error)
    Delete(ctx, path string, msg string) error
    PutTree(ctx, path string, docs map[string]Document, msg string) error
}

// Reconciler makes reality match desired state. This is the axis on which the
// two runtimes genuinely differ, and today it is implicit.
type Reconciler interface {
    Converge(ctx, InstanceRef) error
    // Async reports whether convergence is external (ArgoCD) or immediate.
    Async() bool
}
```

`Status` carries **Lifecycle** and **Availability** separately, per CONTEXT.md —
never one merged "state" string.

### Why StateStore and Reconciler are separate

Under GitOps, *writing the state is the action*: agrelha commits, ArgoCD
converges, agrelha never applies anything. Under Docker, writing a compose file
does nothing — something must also apply it.

Collapsing these into one port hides that convergence is **asynchronous and
external** under GitOps, which is exactly the property that has bitten this
project repeatedly (ArgoCD `selfHeal` silently reverting live `kubectl apply`).
`applyMinecraftAfterSync` is already an ad-hoc Reconciler; this names it.

**Dropping GitOps for Docker does not drop desired state.** The mod list, the
configs and the whitelist still have to live somewhere, and something must make
the container match them:

| | Kubernetes | Docker |
|---|---|---|
| StateStore | git commit | local files + SQLite |
| Reconciler | commit → ArgoCD converges (async) | render → `compose up` (sync) |

Docker users therefore still get reproducibility, history and rollback, without
needing git or ArgoCD.

---

## 4. The Game interface

A **Go interface, with games in-tree.** The variation between games is
behavioural, not configurational:

| | Valheim | Minecraft |
|---|---|---|
| Image | `lloesche/valheim-server` | `itzg/minecraft-server` |
| Content source | Thunderstore | Modrinth / CurseForge / modpackindex |
| Client export | `.r2z` | `.mrpack` |
| Admission | shared password | whitelist |
| Operator id | Steam64 | username |
| Content model | flat mod list | loader + source + pack + version |

Export formats and dependency resolution cannot be expressed as YAML, so a
declarative game definition would need an escape hatch into code anyway.

```go
type Game interface {
    ID() string                       // "valheim", "minecraft"
    Display() Display                 // name, icon, accent

    // Content
    Providers() []ContentProvider
    ResolveContent(ctx, Instance) (ContentSet, error)
    ExportClientBundle(ctx, Instance) (Bundle, error)   // .r2z / .mrpack

    // Runtime shape — consumed by whichever adapter is active
    RuntimeSpec(Instance) RuntimeSpec  // image, ports, env, volumes, health

    // Access
    AdmissionModel() AdmissionModel    // password | allowlist
    OperatorIDKind() IDKind            // steam64 | username
}
```

**Cardinality is not a game property.** Everything is an Instance; how many may
exist or run is **operator configuration** (2–4 per game is the sensible
default range). Nothing about Valheim forbids a second server — ports and RAM
do, and those are the operator's.

**The budget is global**, with optional per-game caps on top. Today
`MC_MAX_RUNNING` and the 24 GiB budget are Minecraft-only, so Valheim consumes
RAM the budget cannot see. Starting any Instance checks the global budget first.

---

## 5. Templates and cluster conventions

The k8s adapter **owns** the six manifest templates (Deployment, PVC, Service,
ConfigMaps). Users override **values and output path** — storage class, node
selector, ingress class, resource tiers — never the YAML itself.

Rationale: upgrades keep working because agrelha owns the templates, and in
practice clusters differ only in those values. User-supplied templates would
break silently whenever the render data struct changes.

> **Trap, learned the hard way:** Go templates resolve fields at *runtime*. A
> rename that `go build` and `go vet` both accept will break manifest rendering
> at instance-creation time. Template rendering must stay covered by tests.

---

## 6. Authentication

An `Auth` port with two adapters:

- **OIDC** — the existing flow, already generic (only 2 Zitadel references).
- **Local users** — argon2id hashes in SQLite, reusing the existing HMAC-signed
  session cookies and the `actor` already recorded on every audit row.

`ALLOWED_EMAIL` becomes a **list**; the panel supports several admins.

**PocketBase was considered and rejected.** It is an all-or-nothing framework:
using it means adopting its HTTP server, its SQLite schema and its admin UI —
rewriting 5,453 LOC of Fiber handlers and migrating the store. That is more work
than writing local auth, and it would put a framework at the centre of the
onion, which is the opposite of the goal.

---

## 7. Splitting `internal/server`

5,453 LOC in one package is the practical pain. Split **by feature**, moving to
`internal/http/`:

```
http/
  dashboard/   the public + admin landing page
  instances/   lifecycle, settings, provisioning
  content/     mods, packs, search
  access/      admission + operators
  backups/
  console/     logs + rcon
  shared/      render, flash, sse, middleware
```

Handlers depend on **ports and domain**, never on adapters directly.

---

## 8. Execution — strangler, always shippable

agrelha runs in production daily. Every phase compiles, passes tests and
deploys, so a bad step is one revert rather than a lost branch.

| Phase | Work | Done when |
|---|---|---|
| **0. Hygiene** ✅ | Rename `CODEBERG_*` → `GIT_*`; delete dead config (`FABRIC_DEPLOYMENT` still required for something removed); move hardcoded `192.168.20.x`, `ykhi.xyz`, `game-01` into config; ship `.env.example` | No personal values compiled in |
| **1. Runtime port** ✅ | Define `Runtime`; k8s adapter wraps today's client; **Docker skeleton compiles**; remove `k8s` import from `minecraft` | `minecraft` no longer imports `k8s` |
| **2. Test coverage (scoped)** ✅ | Cover only code that survives later phases; everything else is tested as it moves. See §8.1 | `store`, `gitops` behaviour, k8s adapter, invariants & config covered |
| **3. State + Reconciler** ✅ | Define both; git + ArgoCD adapters; local + compose skeletons; remove `gitops` imports from `minecraft`, `mods`, `admins` | No domain package imports infrastructure |
| **4. Domain extraction** ✅ | `internal/domain` with Instance, Game, Pack, Access, Budget; global budget replaces the Minecraft-only one | Domain imports nothing outward |
| **5. Game interface** ✅ | Define `Game` port; Minecraft implements it; **Valheim extracted from the handlers** into a real package | A third game needs no handler edits |
| **6. Auth port** ✅ | OIDC adapter + local users; `ALLOWED_EMAIL` becomes a list | Installable with no external IdP |
| **7. HTTP split** ✅ | Break `server/` into per-feature packages under `http/` | Split done; `instances` 1,739→994 via new `http/wizard`; remaining overages justified in §7 notes |
| **8. Docker adapter** ✅ | Make the skeletons real: compose rendering, docker engine, local state | Audience 2 can install |
| **9. Packaging** ✅ | Helm chart + compose file; required config shrunk from 15 toward ~5 | Chart lints and renders; `RUNTIME` is the only required key, and only off-Kubernetes |

**Phase 0 and 1 are done (2026-09-09).** Notes from doing them:

- `RunTask` (backup/restore) was deliberately left OUT of `ports.Runtime`. Its
  only implementation is `CreateBackupJob(jobName, archiveName, dataClaimName,
  backupsClaimName)` — PVC claim names, i.e. a Kubernetes volume model. Defining
  an interface method whose shape we cannot yet honour is worse than deferring
  it; it needs the volume model from a later phase.
- `ConfigMapData` is called 22 times from `server/`, but it is **not** a Runtime
  concern — it reads declarative state that ArgoCD has applied. It belongs to
  the StateStore port in phase 3.
- The port's Lifecycle/Available split turned out to express exactly the three
  states `InstanceManager` already derived ad-hoc from `(desired, ready)`
  replica counts, which is good evidence the split is real and not invented.
- `LBIP` on the domain `Instance` is still a leak: Docker binds a host port
  rather than allocating a LoadBalancer address. Allocation should move into the
  runtime adapter.

Phases 1–7 serve audience 1 and are worth doing regardless of whether the
project is ever published. Phase 8 unlocks audience 2; phase 9 unlocks 3.

### 8.1 Phase 2 in detail — test coverage

Measured 2026-09-09, before this phase: **23.8% overall.**

| Package | LOC | Coverage | Why it matters |
|---|---:|---:|---|
| `store` | 518 | **0%** | Instance records, audit, roster. A bug here loses data silently. |
| `gitops` | 510 | **0%** | **Writes to the ops repo.** A bug here corrupts the source of truth for the whole cluster. |
| `k8s` | 509 | **0%** | Starts, stops and restarts real servers. |
| `adapters/runtime/k8s` | new | **0%** | The translation layer everything now goes through. |
| `minecraft` | 1,812 | 43.6% | Holds the domain invariants (pack ownership, budget, tiers). |
| `server` | 5,453 | 28.4% | Largest package; splits in phase 7. |
| `config` | 135 | 0% | Cheap to cover; silently wrong config is hard to debug. |

**The goal is not a percentage.** It is that the paths which can lose data,
corrupt git, or stop a server are covered. Chasing a global number would push
effort into `logging` and `mdrender`, which are neither risky nor interesting.

**First: will this code survive the later phases?** Tests are only wasted if the
*behaviour* they assert disappears. Evidence that behaviour-shaped tests survive
here: all 46 existing `server` tests go through `App.Test` at the HTTP level and
none call internal functions, so the phase-7 file split cannot break them.

| Package | Survives? | Decision |
|---|---|---|
| `store` | Yes — becomes the Repository adapter, same SQL | **Write now** |
| `adapters/runtime/k8s` | Yes — just written, stable | **Write now** |
| `minecraft` invariants | Yes — rules survive the move to `domain` | **Write now** |
| `config` | Yes | **Write now** |
| `gitops` | Behaviour yes, API no (phase 3 reshapes it) | **Write now, behaviour-shaped only** |
| `internal/k8s` | **No** — phase 3 hollows it out (`ConfigMapData` leaves) | **Skip.** Test the adapter instead |
| `server` breadth | Routes yes, organisation no | **Defer** — add per feature as it moves in phase 7 |

**Write `gitops` tests through outcomes, not methods.** Assert "after this call
the repo contains X, and an unchanged write produces no commit" rather than
exercising `Patch`/`SetData`/`ReplaceData` one by one. Phase 3 renames those into
`Put`/`Get`/`Delete`/`PutTree`; outcome-shaped tests survive that almost verbatim,
method-shaped tests get rewritten.

**Order, by blast radius:**

1. **`gitops`** — highest risk, zero cover. Testable without a network: `go-git`
   can `PlainInit` a temp repo and commit to it, so `Patch`, `SetData`,
   `ReplaceData`, `ReplaceConfigMap`, `WriteDirectory` and `DeleteDirectory` can
   all be exercised against a real repository in `t.TempDir()`. Cover the
   no-op-when-unchanged path especially — it decides whether a commit happens.
2. **`store`** — SQLite in `t.TempDir()`, the pattern the existing tests already
   use. Cover instance CRUD, the `COALESCE(NULLIF(...))` upsert semantics (an
   earlier bug: a later write with empty fields wiping known metadata), audit
   append, and that `migrate()` is idempotent on an existing database.
3. **`adapters/runtime/k8s`** — `fake.NewSimpleClientset`, already used
   elsewhere. Assert Start/Stop become replica counts, and that `Status` derives
   Lifecycle from desired replicas and Available from readiness — never from the
   pod `Phase`, whose capital-R "Running" is a different concept. Test the
   adapter, **not** `internal/k8s` beneath it: that package is being hollowed out
   in phase 3 and tests written against it now would be thrown away.
4. **`minecraft`** — the invariants, not the getters: pack-owned fields refuse
   changes, budget rejects over-commit, tiers map to memory, `Env()` emits the
   right disjoint key set per Source.
5. **`config`** — that `GIT_USERNAME` wins over `CODEBERG_USERNAME` but the old
   name still works, and that empty `GAME_NODE_SELECTOR`/`MC_LB_BASE_IP` mean
   "unconstrained" rather than a literal empty value in a manifest.
6. **`server`** — *deferred, not skipped.* The 46 existing `App.Test` cases
   already survive the phase-7 split. Rather than a broad push now, add
   handler-level coverage for each feature at the moment it moves, where it acts
   as the characterization test proving the move preserved behaviour.

**Guardrails**

- Add a `task test:cover` that prints per-package coverage, so regressions are
  visible without a CI gate that blocks work.
- Prefer a **ratchet** over a fixed threshold: coverage may not fall below what
  is already achieved. A hard global % invites tests written to satisfy the
  number.
- **Template rendering must stay covered.** Go templates resolve fields at
  runtime, so `go build` and `go vet` both pass while manifest rendering is
  broken — this has already happened once, during the `SlotCMName` rename.

**Progress (2026-09-09):** Phase 2 complete.
- `internal/gitops` (79.7%): offline git harness (`harness_test.go`) and outcome-shaped unit tests (`committer_test.go`) covering `Patch`, `SetData`, `DeleteData`, `ReplaceData`, `ReplaceConfigMap`, `WriteDirectory`, `DeleteDirectory`, and verified the no-op path makes no commit.
- `internal/store` (82.1%): SQLite test suite in `store_test.go` covering `Open`/`Close`, `migrate()` idempotency, `UpsertSeen` `COALESCE(NULLIF(...))` metadata protection, presence counting, instance CRUD, history event filtering, and mod index/readme caching.
- `internal/adapters/runtime/k8s` (94.1%): fake clientset test suite in `runtime_test.go` verifying start/stop/restart replica scaling and status derivation (lifecycle from desired replicas, availability from Pod readiness condition, never from Pod phase).
- `internal/minecraft` invariants: `invariants_test.go` verifying resource tiers memory mapping, disjoint `Env()` key sets per source, RAM budget overcommit rejection, and max instances limit.
- `internal/config` (100%): `config_test.go` verifying defaults, `GIT_*` fallback/precedence over `CODEBERG_*`, and unconstrained empty values.
- Guardrail: added `task test:cover` in `Taskfile.yml`. Total statements: 28.2% (127 passing tests across 27 packages).

**Progress (2026-09-09):** Phase 7 complete.
- Split monolithic `internal/server` (5,453 LOC) into feature packages under `internal/http/`:
  - `internal/http/shared`: rendering, flash cookies, formatting, actor context, structured logging, SSE toast notifications.
  - `internal/http/access`: admission & operator management for Valheim and Minecraft, audit history.
  - `internal/http/backups`: backup status, manual snapshot creation, restore, download, deletion.
  - `internal/http/console`: Valheim log streaming & server restart/stop, Minecraft live log streaming & interactive RCON command execution.
  - `internal/http/dashboard`: public & admin landing page, live Datastar tile signals, updates SSE stream.
  - `internal/http/content`: Valheim mods, mod configs, update checking & bulk application, `.r2z` bundle export, image proxy.
  - `internal/http/instances`: Minecraft dashboard, instance lifecycle, detail page, settings, modpack wizard & cart provisioning, instance configs, daily backup scheduler.
- Strangler pattern preserved: `internal/server` shims delegate directly to `internal/http/*` handlers, keeping all legacy integration tests and route bindings intact.
- The ~800 LOC rule is **mostly met** (updated 2026-09-10):
  - `internal/http/instances` **1,739 → 994**, with the provisioning flow split
    into `internal/http/wizard` (706). They shared no helpers and the wizard uses
    only 7 of the 11 `Config` fields, so its package declares that narrower set.
  - `internal/http/content` (841) is 5% over. Splitting `updates.go` out needs
    the pending-update state untangled first: it both *consumes*
    `cfg.PendingActive`/`cfg.SetPending` and *exports* `SetPending`/
    `PendingActive`/`ClearPending`. That is application state living in the HTTP
    layer, so it belongs in `internal/app`, not in another file move.
  - `internal/server` (1,254) is **a composition root**, not a feature package:
    `server.go` (354) wires dependencies and `routes.go` (199) declares routes.
    Splitting it to satisfy a line count would make wiring harder to follow. The
    rest is transitional shims that disappear as callers move off them.
  - Out of this phase's scope but worth naming: `cmd/web/pages` (6,427) is now
    the largest package in the repo, and `internal/minecraft` (1,327) still mixes
    domain aliases with the instance manager.
- Full test suite passes: 191 tests passing across 43 packages with race detector enabled.

**Progress (2026-09-09):** Phase 8 complete.
- Implemented `internal/adapters/state/local`:
  - Satisfies `ports.StateStore` on top of local filesystem documents and SQLite.
  - Guarantees no-op invariant: identical writes make no disk modifications or history entries.
  - Features SQLite-backed revision tracking (`state_history` table) enabling audit and point-in-time rollback.
  - Prevents path escaping and traversal attacks.
- Implemented `internal/adapters/reconcile/compose`:
  - Satisfies `ports.Reconciler` with immediate, synchronous convergence (`Async() == false`).
  - Implements `RenderCompose(serviceName, domain.RuntimeSpec)` converting domain runtime specs into valid `docker-compose.yml`.
  - Executes `docker compose up -d --remove-orphans` via configurable command executor.
- Implemented `internal/adapters/runtime/docker`:
  - Satisfies `ports.Runtime` on top of Docker Engine HTTP REST API.
  - Uses Go standard library `net/http` and Unix domain socket `net.Dialer` with zero external CGO/SDK dependencies.
  - Implements container start, stop, restart, inspect, stats, and demultiplexed log streaming.
  - Accurately derives `ports.Status` (Lifecycle and health-based Availability) and calculates CPU millicores and memory MiB.
- Server integration:
  - Added `RUNTIME=docker`, `DOCKER_SOCKET`, `LOCAL_STATE_DIR`, `COMPOSE_DIR` configuration knobs.
  - Wired into `server.New()`: dynamically initializes Docker runtime, local state store, and compose reconciler when running without Git/Kubernetes.
- Full test suite passes: 176 test functions across 43 packages, race detector clean.

**Explicit non-goals:** chasing 80%; testing `logging`, `mdrender` or `metrics`
for their own sake; snapshot-testing rendered HTML, which locks in markup and
makes the UI painful to change.

**Why here and not later:** phases 3–7 move code between packages. Tests written
against behaviour make those moves safe; without them, the refactors are
unverifiable and this document's "always shippable" claim is not true.

**Why not everything here:** tests written against code that phase 3 deletes or
reshapes are waste. The split above is the compromise — cover what is stable and
dangerous now, and let the rest be written as characterization tests immediately
before each move, which is the same discipline the strangler approach already
implies.


---

### 7.1 The missing application layer

**Gap in this plan, found 2026-09-10.** The target layout in §3 has `domain`,
`ports`, `adapters`, `games` and `http` — but **no application layer**. A JVM
codebase would call it `application`; use cases that orchestrate the domain and
the ports while belonging to neither.

Without a home, that code has been landing in the HTTP layer. Evidence:

- `internal/http/instances/scheduler.go` — a daily backup job with **zero fiber
  references** — lived in an HTTP package. Now `internal/app`.
- `internal/http/content/updates.go` — pending-update state both injected into
  and exported from a handler.

**Resolved 2026-09-10.** `internal/app` now exists, infrastructure has moved
under `adapters/`, and a real dependency violation was fixed:

- **Infrastructure relocated:** `k8s`→`adapters/kube`, `gitops`→`adapters/gitops`,
  `store`→`adapters/store`, `backups`→`adapters/backups`, and
  `thunderstore`/`modrinth`/`modpackindex`/`mcversions`→`adapters/content/*`.
  `ingest` (a log-tailing use case) moved to `app/ingest`.
- **Dead packages deleted:** `internal/valheim` (49 LOC) and `internal/auth`
  (25 LOC) had zero production importers, superseded by `games/valheim` and
  `adapters/auth/*`.
- **A genuine onion violation, fixed:** `internal/minecraft` imported
  `internal/store` directly — an application service depending concretely on
  SQLite. The earlier invariant check missed it because it only looked for
  `k8s`/`gitops`/`server`. Now `ports.InstanceRepository` speaks `domain.Instance`,
  and `adapters/store.InstanceRepo` owns `InstanceRecord` and the conversion, so
  no caller sees the storage shape.

Remaining, smaller: `internal/minecraft` and `internal/modpack` still import
`adapters/content/modrinth` directly rather than through `ports.ContentProvider`.
`mdrender`, `metrics` and `sse` stay top-level deliberately — they are
cross-cutting utilities, not adapters onto an external system.

## 9. Decisions still open

- **The name.** "Agrelha" is currently the name of one Valheim server, which
  reads oddly for a multi-game panel. Renaming is cheapest before publication.
- **Licence** — not chosen.
- **Whether Valheim multi-instance is actually wanted**, once cardinality stops
  being a game property. It becomes possible; it need not be enabled.

## 10. Explicitly out of scope

- Config presets for common setups (agreed as a follow-on).
- External/RPC plugins — games stay in-tree.
- Any third runtime beyond Kubernetes and Docker.
- Multi-tenancy. agrelha stays a single-operator panel with several admins.
