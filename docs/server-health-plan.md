# Detecting and explaining crashed servers

Status: **phases 1-5 implemented** (2026-09-11). Detection, capture and the
incident panel are live; the dashboard-tile hint is dropped and `ListIncidents`
is still unread.

A server whose game process has died — or which never started serving — reports
**Online** in agrelha, because the pod is running. This is the gate that makes
"online" mean *players can connect*, and that records what happened when it
stops being true.

## Evidence

| Fact | Where |
|---|---|
| `Available` is nothing but `pod.Ready` | `infra/runtime/k8s/runtime.go` — `st.Available = ps.Ready` |
| Valheim readiness is `pgrep -f valheim_server` | process existence only |
| Minecraft readiness is `tcpSocket: 25565` | bound early, says nothing about serving |
| `ports.Status` = `{Lifecycle, Available, StartedAt}` | cannot carry a reason |
| `kube.PodStatus` = `{Phase, Ready, StartedAt}` | **discards** `RestartCount`, `LastTerminationState` (exit code, `OOMKilled`), and the waiting reason (`CrashLoopBackOff`) |
| `"crash"` is a UI label in `pages/helpers.go` | the history timeline renders crash badges. **Nothing produces a crash event** |

**Observed, not theorised:** during the Valheim 1.0 world generation the pod sat
at `1/1 Ready` for roughly three minutes while bound to **no game ports at
all**. `pgrep` was satisfied the whole time.

### The domain already knows the right Minecraft probe

`domain.RuntimeSpec.HealthProbe` is set to `"mc-health"` for Minecraft
(`app/games/minecraft/game.go:141`) — itzg ships that binary and it performs a
real Server List Ping. **The deployment template ignores it and emits
`tcpSocket` instead.** Wiring the existing field through is most of the
Minecraft fix.

### The Valheim probe in the domain is wrong

`app/games/valheim/game.go:143` declares `HealthProbe: "status.json"`. Measured
on the running instance today:

- `curl localhost:8080/status.json` returns an **empty body**, though
  `STATUS_HTTP: "true"` is set and one `valheim-status` process is running.
- UDP 2457 has a **growing unread backlog** (`Recv-Q` 102720 → 103680 across
  samples) while 2456 and 2457 are both bound and the server is genuinely
  healthy.

Nothing is answering A2S queries on Valheim 1.0, which is why lloesche's status
updater times out. **So neither `status.json` nor a direct A2S query is a usable
Valheim health signal today**, and the plan must not assume one.

## Decisions

1. **Deep readiness probes for "is it serving", k8s telemetry for "what
   happened".** Two different questions; neither answers the other. A probe
   cannot explain a crash, because by the time anyone looks the container has
   been replaced.
2. **Health is a separate domain concept**, not a Lifecycle value and not an
   Availability value. `CONTEXT.md` defines Lifecycle as *what the operator
   asked for* — a crash is emphatically not that — and Availability is a boolean
   rendered Online/Offline. Folding "crashed" into either is what produced this
   bug class in the first place.
3. **Capture standard context at detection time**: timestamp, exit code, restart
   count, `OOMKilled`, waiting reason, and the last 50–100 log lines of the
   **previous** container.
4. **Both games together.** The expensive half — ports, domain concept, capture,
   storage — is game-agnostic. Only the probe command differs.

## Design

### Domain — a third term

Add to `CONTEXT.md`, alongside Lifecycle and Availability:

> **Health**: what is known to have gone wrong with an Instance, independent of
> what the operator asked for (Lifecycle) and of whether players can connect
> right now (Availability). An Instance can be Lifecycle `running`, Availability
> offline, and carry a last **Incident** explaining why — three independent
> facts, each worth showing.
>
> **Incident**: one recorded failure — when it happened, how it ended (exit
> code, OOM, crash loop), and the log tail captured at the moment of detection.

The existing rule stands and gains a clause: Lifecycle, Availability and Health
are never compared with one another and never share a field.

### Ports

- Widen `ports.Status` with the failure signal: restart count, last termination
  (exit code, reason, finished-at), `OOMKilled`, waiting reason.
- `ports.Runtime` gains a way to read the **previous** container's logs
  (`LogOptions` already exists; it needs a `Previous bool`).

Both are additive; the docker adapter can return zero values until it implements
them.

### Infra

- `kube.PodStatus` carries the fields it currently throws away, read from
  `ContainerStatuses[].RestartCount`, `.LastTerminationState.Terminated`, and
  `.State.Waiting.Reason`.
- Probes move into the deployment templates per game:
  - **Minecraft**: `exec: ["mc-health"]` — honour the `HealthProbe` the domain
    already declares.
  - **Valheim**: interim `ss -lun | grep -q ':2456'` — strictly stronger than
    `pgrep` and true to what actually broke. Revisit once the A2S/status problem
    above is understood; do **not** wire `status.json` in until it returns a
    body.

### Detection and capture

A poller in the application layer, alongside the existing scheduler. On each
tick, for every Instance with Lifecycle `running`:

- restart count increased since last tick, or waiting reason is
  `CrashLoopBackOff`, or last termination exists and is newer than the last
  recorded Incident → **record an Incident**.
- Capture the previous container's log tail **at that moment**. This is the only
  step with a hard deadline: Kubernetes reaps the evidence.
- Write the Incident to SQLite and emit an `event` of kind `crash` — which makes
  the badge in `pages/helpers.go` light up for the first time.

## Phases

| Phase | Work | Done when |
|---|---|---|
| **1** ✅ | Widen `kube.PodStatus` + `ports.Status`; add `Previous` to log options | Crash telemetry reaches the application layer |
| **2** ✅ | `Health`/`Incident` in `CONTEXT.md` and `internal/domain`; incidents table | The domain can express "running, offline, and here is why" |
| **3** ✅ | Detection poller + log-tail capture + `crash` events | The existing history badge has a producer |
| **4** ✅ | Deep probes per game in the templates | `pod.Ready` means "serving", and Availability stops lying |
| **5** ✅ | Surface it: incident detail on the instance page | The operator sees what happened without `kubectl` |

Phase 4 is the one that fixes the reported symptom and is nearly free for
Minecraft; phases 1–3 are what make the answer to *"what happened?"* exist at
all. They are independent — 4 can ship first.

## Open decisions

1. **Does a crash-looping Instance get auto-stopped** to return its RAM to the
   budget? Recommendation: no automatic action, but surface it prominently —
   silently stopping something the operator asked to run makes Lifecycle lie in
   the other direction.
2. **Incident retention** — count per instance, or age-based prune.
3. **Does an Incident block a restart** until acknowledged? Recommendation: no.
4. **Poll interval**, and whether to watch the API instead of polling.

## Out of scope

- Fixing Valheim's A2S/`status.json` regression on 1.0 — a prerequisite for a
  proper Valheim probe, but its own investigation.
- Docker runtime parity. The port widening is additive; the docker adapter can
  return zero values until someone needs it.
- Predicting crashes. This plan explains them after the fact.

## Phase 4 — done (2026-09-11)

Probes replaced in both templates and in the four live instance manifests.

| Game | Was | Now |
|---|---|---|
| Minecraft | `tcpSocket: 25565` | `exec: ["mc-health"]` |
| Valheim | `pgrep -f valheim_server` | `ss -lun \| grep -qE ':2456[[:space:]]'` |

Both commands were verified in the running containers, in both directions,
before shipping:

- `mc-health` on a healthy server → `0`; `mc-monitor status --port 25599`
  (a dead port, which is what mc-health wraps) → `1`. Binary confirmed present
  at `/usr/local/bin/mc-health`.
- Valheim port probe on a healthy server → `0`; same probe against an unbound
  port → `1`. The old `pgrep` returns `0` on the same pod, which is the whole
  problem.

Two regression tests added, each naming the failure it prevents rather than just
asserting a string.

**Valheim deliberately does not use `status.json`**, even though
`domain.RuntimeSpec.HealthProbe` declares it: measured today it returns an empty
body while UDP 2457 accumulates an unread backlog. That regression is still
open — see Out of scope. The port-bound check is the honest interim.

**`domain.RuntimeSpec.HealthProbe` is still not read by either template.** The
values are now correct for Minecraft and wrong for Valheim, and both templates
hardcode their probe. Wiring the field through is a small follow-up that belongs
with phases 1-3, when the domain starts carrying health meaningfully.

## Phases 1-3 — done (2026-09-11)

**Phase 1 — telemetry reaches the application layer.**
`kube.PodStatus` now carries `RestartCount`, `WaitingReason`, and the previous
termination (`LastExitCode`, `LastReason`, `LastFinishedAt`, `LastOOMKilled`),
summed across containers and ignoring sidecars. `ports.Status` gained a
`Failure` struct with a `Crashed()` predicate, mapped through the k8s runtime
adapter. `ports.LogOptions` gained `Previous`.

`kube.StreamDeploymentLogs` hardcoded `Follow: true`, so a new
`StreamDeploymentLogsQuery(ctx, dep, LogQuery{...})` takes the options and the
old function delegates to it.

**A regression this nearly shipped:** honouring `opts.Follow` in the runtime
adapter silently broke the live console. `InstanceLogs` passed
`ports.LogOptions{Tail: tail}` with no `Follow`, relying on the adapter forcing
it — so both console handlers would have turned from a live stream into a finite
read, with everything still compiling and every test still green. Found by
grepping the call sites before trusting the build. `Follow: true` is now
explicit at that call site.

**Phase 2 — the domain can say what went wrong.**
`domain.Incident` with a `Summary()` that names the cause (OOM, crash loop, exit
code). An `incidents` table indexed on `(game_id, number, at DESC)`, with
`RecordIncident`, `LastIncident` and `ListIncidents` scoped per game.

**Phase 3 — detection and capture.**
`internal/app/health` holds a `Watcher` with two consumer-side interfaces —
`Source` (one per game manager) and `Recorder` — so the package imports only
`domain` and `ports`. `InstanceManager` gained `RuntimeStatus` and `CrashLogs`
(previous container) to satisfy `Source`.

Each tick: skip anything not `Crashed()`; compare against the last recorded
incident; record only when the restart count rose or the termination is newer
than what is on file. That dedupe matters — without it a crash-looping instance
writes one incident per tick forever, and there is a test that fails if it
regresses. Capture reads the terminated container's logs, capped at 64 KiB, and
emits a `crash` event, which gives the history badge in `pages/helpers.go` its
first producer.

Started from `wiring.Build`, skipping any manager that is nil.

Verified against the live cluster: `mc-bob-03` reports
`restartCount=10, waiting=CrashLoopBackOff, lastExit=1` — precisely what was
being thrown away before phase 1.

**Still open:** the four decisions above, plus surfacing incidents in the UI
(phase 5). Nothing yet reads `ListIncidents`.

## Phase 5 — done (2026-09-11)

`pages.IncidentPanel` renders the last recorded failure at the top of an
instance's Overview tab, for **both games**: a one-line cause from
`Incident.Summary()`, the time, badges for out-of-memory / restart count / exit
code, and the captured container log behind a `<details>`.

- `pages.IncidentView` adapts `domain.Incident` and returns **nil** when there
  is nothing to show, so the template branches on presence and a healthy
  instance renders no panel at all.
- Both detail handlers gained a `LastIncident` reader, wired in
  `incidentReader(d, game)` — which returns nil without a store, so the panel
  degrades to absent rather than erroring.
- When the log tail is empty the panel says *"No log was captured — the
  container was replaced before agrelha could read it"* instead of showing a
  blank box. That case is real: capture races pod replacement.

Five tests, including one asserting a nil incident renders literally nothing.

**Not done: the dashboard tile hint.** The hub shows only *running* worlds, and
a crashed instance is by definition not running, so the tile has nowhere to put
a cause. Surfacing it there needs the hub to list stopped worlds too — an open
product decision, not a missing implementation.

**`ListIncidents` is still unread.** Only the latest incident is surfaced; the
history of failures is stored and not yet shown anywhere.
