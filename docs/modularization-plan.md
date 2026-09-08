# Modularization plan — making agrelha open-source and adaptable

Status: **planned, not started.** Design agreed 2026-09-08.

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
| **0. Hygiene** | Rename `CODEBERG_*` → `GIT_*`; delete dead config (`FABRIC_DEPLOYMENT` still required for something removed); move hardcoded `192.168.20.x`, `ykhi.xyz`, `game-01` into config; ship `.env.example` | No personal values compiled in |
| **1. Runtime port** | Define `Runtime`; k8s adapter wraps today's client; **Docker skeleton compiles**; remove `k8s` import from `minecraft` | `minecraft` no longer imports `k8s` |
| **2. State + Reconciler** | Define both; git + ArgoCD adapters; local + compose skeletons; remove `gitops` imports from `minecraft`, `mods`, `admins` | No domain package imports infrastructure |
| **3. Domain extraction** | `internal/domain` with Instance, Game, Pack, Access, Budget; global budget replaces the Minecraft-only one | Domain imports nothing outward |
| **4. Game interface** | Define `Game`; Minecraft implements it; **Valheim extracted from the handlers** into a real package | A third game needs no handler edits |
| **5. Auth port** | OIDC adapter + local users; `ALLOWED_EMAIL` becomes a list | Installable with no external IdP |
| **6. HTTP split** | Break `server/` into per-feature packages under `http/` | No package over ~800 LOC |
| **7. Docker adapter** | Make the skeletons real: compose rendering, docker engine, local state | Audience 2 can install |
| **8. Packaging** | Helm chart + compose file; required config shrunk from 15 toward ~5 | Someone else installs it without reading Go |

Phases 1–6 serve audience 1 and are worth doing regardless of whether the
project is ever published. Phase 7 unlocks audience 2; phase 8 unlocks 3.

---

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
