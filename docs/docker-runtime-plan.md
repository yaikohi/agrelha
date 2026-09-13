# Docker runtime, end to end

Status: **designed, not implemented.** Grilled 2026-09-13.

`RUNTIME=docker` boots agrelha and serves pages today, but no game server ever
reaches a container. This is the plan that makes Docker a real runtime rather
than a set of adapters with nothing joining them.

## Where it actually stands

Docker is **not** missing an abstraction. Three of the four ports already have
both implementations; one is missing, plus the wiring that selects it.

| Port | Question it answers | Kubernetes | Docker |
|---|---|---|---|
| `StateStore` | where does desired state live? | git | local files ✅ |
| `Reconciler` | how does it become real? | argocd | compose ✅ |
| `Runtime` | how do I start/stop/log it? | client-go | docker API ✅ |
| **`SpecRenderer`** | **what does desired state look like?** | manifests | **missing** |

What is genuinely broken or absent:

- **`wiring` passes the Kubernetes renderer unconditionally**, whatever `RUNTIME`
  says.
- **`Reconciler.Converge` has zero callers** on either runtime. ArgoCD covers
  Kubernetes; nothing covers Docker.
- **`domain.RuntimeSpec` has zero live consumers.** The Kubernetes renderer
  ignores it and renders from `Instance` + templates. Because nothing exercised
  it, its `Env` has drifted to a stale subset — no password, no seed, no
  `BEPINEX` — so rendering Docker from it today would ship servers without the
  vanilla/modded distinction that exists everywhere else.
- **`RenderCompose` emits no `healthcheck`**, while the Docker runtime already
  reads container health into `Available`. So `Available` means "process is
  running" — the same lie phase 4 removed from Kubernetes.
- **Every rendered service would bind `2456:2456`** and mount `./config`.

## Decisions

### Rendering

A `compose.Renderer` implementing `ports.SpecRenderer`, chosen on `RUNTIME` in
the composition root beside the `StateStore` / `Reconciler` / `Runtime`
switches that already branch correctly.

`domain.RuntimeSpec` becomes the single game-owned description of a server, and
**compose is its first real consumer**. Kubernetes keeps its templates for now:
it needs `nodeSelector`, tolerations, PVC claim names, an initContainer and
probes, and widening the spec to fit a runtime that already works is how
`ports.Game` acquired methods nobody used. Migrate Kubernetes only if it earns
it.

**`RuntimeSpec.Env` is built from `inst.Env()`.** One env source, so Vanilla,
seed, password and BepInEx reach Docker automatically. Two env maps guarantee
drift, and this repository has spent a great deal of time paying for exactly
that.

### Addressing

`LBIP` currently means "the LoadBalancer address we intend to have", which is
already one meaning too many. Split it:

- **`Host`** and **`Port`** on the Instance. `ConnectAddress(host, port)` stops
  taking a hardcoded per-game constant.
- Ports allocated `base + (N-1)*stride` by instance number, stride wide enough
  for each game's secondary port (Valheim 2456/2457, Minecraft 25565/25575).
- A **domain port-collision guard**, shaped like `checkLBIPFree`: the offset is
  a convention, not an enforcement, and a duplicate port fails as a container
  that will not start with the reason buried in daemon logs.
- **`PUBLIC_HOST`** config. A containerised agrelha cannot know its own
  reachable address — auto-detection confidently returns things like
  `172.17.0.1`, and `localhost` is right only for the person running it.

### Convergence

A **`StateStore` decorator** converges after each write. `app/` code is then
identical on both runtimes: git-backed writes get the ArgoCD reconciler
(effectively a no-op, since ArgoCD is watching), local writes get compose. The
runtime choice stays in the composition root, and `ports.Reconciler` finally
has callers.

Restart-after-mod-change stays an **explicit app-layer call**. A decorator
cannot know that a `Patch` touched the mod list, and having the reconciler sniff
filenames is the wrong shape. Two mechanisms, each with one job.

### Data

**Docker named volumes**, mirroring PVC-per-instance — including the
number-keyed naming, which is what saved `lareira-V2` when a stray instance
claimed slot 01.

This deliberately avoids a trap: `COMPOSE_DIR` is a path inside *agrelha's*
container, while `docker compose` runs against the **host** daemon. Any bind
mount agrelha writes resolves on the host, at a path that does not exist there.
Named volumes have no host path to get wrong.

Cost: backing up a world needs a helper container rather than `cp`. Acceptable —
Kubernetes already runs backups as a Job, so "backups are a mechanism, not a
folder" is established.

### Mods

A **one-shot service** with `depends_on: {condition:
service_completed_successfully}` — the compose analogue of an initContainer, so
both runtimes keep the same shape.

- **Valheim**: the existing 77-line mod-reconciler script ports across nearly
  verbatim; it is already written and tested in a container.
- **Minecraft**: writes `mods.txt` into the volume before itzg starts, which is
  what `MODRINTH_PROJECTS=@file` expects.

### Health

Emit `healthcheck:` from `RuntimeSpec.HealthProbe`, and **fix the probe values
while doing it** — `mc-health` for Minecraft, the `ss` port-bound check for
Valheim. `status.json` is declared today and is broken on Valheim 1.0: it
returns an empty body while UDP 2457 accumulates an unread backlog.

One field then feeds both the compose `healthcheck` and the Kubernetes probe,
which is the consolidation the whole plan is about.

### Lifecycle

Compose file as the declared artifact; the **Docker API for day-to-day
operations** — start, stop, logs, stats — which is already how the adapters are
split. `docker compose` must exist on the host: `deploy/README.md` mounts the
socket and currently says nothing about needing the CLI too.

## Out of scope

**Backups.** `RunTask` was deliberately left out of `ports.Runtime` because its
only implementation takes PVC claim names, and defining the volume model under
pressure from an unrelated feature is how `ports.Game` went wrong. Docker ships
without backups — but the Backups tab must **say so**, because a tab that
silently does nothing is worse than an absent one.

This makes Docker second-class in one visible way. If that is unacceptable, it
is a reason to widen the scope deliberately, not to discover it late.

## Definition of done

An operator with Docker and no cluster can **create a world, connect to it,
install a mod, and see it load** — for both games. Anything short of that and
the compose file is decoration.

## Phases

Each compiles and ships; a bad step is one revert.

| Phase | Work | Done when |
|---|---|---|
| **1** ✅ | `RuntimeSpec.Env` from `inst.Env()`; fix `HealthProbe` per game | One env source; probes are real commands |
| **2** | `compose.Renderer` implementing `ports.SpecRenderer`, both games, with healthcheck and named volumes | A rendered compose file starts a server by hand |
| **3** | Select the renderer on `RUNTIME` in the composition root | Creating an instance under Docker writes compose, not manifests |
| **4** | `Host`/`Port` on the Instance, offset allocation, collision guard, `PUBLIC_HOST`, `ConnectAddress` | Two instances coexist and the UI shows where to connect |
| **5** | `StateStore` decorator triggers `Converge`; explicit restart on mod change | Creating a world starts a container without manual intervention |
| **6** | Mod sidecar for both games | The definition of done is met |
| **7** | Hide/explain Backups on Docker; document the `docker compose` dependency | No tab lies about what it can do |

Phase 1 is the one with a wide blast radius — it touches `domain` and `ports`.
Worth doing deliberately and first, rather than discovering the shape at the end.

## Fixed ahead of the phases (2026-09-13)

Two of the four live defects had **zero consumers**, so they were safe to fix
immediately - nothing could regress:

- **`RuntimeSpec.Env` drift** (phase 1). Valheim built its own three-key map;
  it now returns `inst.Env()`. Minecraft was already correct. `HealthProbe` for
  Valheim changed from the broken `status.json` to the port-bound check, matching
  the Kubernetes probe.
- **No compose healthcheck.** `RenderCompose` now emits one from
  `HealthProbe`, and omits it when a game declares none.

A third defect surfaced while doing it: `Game.RuntimeSpec` called `inst.Env()`,
which **branches on `GameID`** - so an Instance built without one got Minecraft's
environment out of the Valheim game. Both games now assert their own `GameID`
first, as the manifests renderer already did. An existing test had been passing
an Instance with no `GameID` and never noticed.

The remaining two defects **cannot** be usefully fixed early:

- **`wiring` ignores `RUNTIME` when choosing a renderer** - there is no compose
  renderer to choose yet (phase 3).
- **`Converge` has no callers** - wiring the decorator now would run
  `docker compose up` against a directory with no compose file (phase 5).

## Consequence

This closes the correction currently in `plan.md`, where Phase 8 reads
*"⚠️ adapters only, NOT end-to-end"*.
