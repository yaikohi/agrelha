# Tracing agrelha

The app half of **Phase 5** of `yaya-ops/docs/observability-expansion-runbook.md`, which covers Tempo
and the Grafana wiring.

**Status: implemented in the working tree, not deployed.** The code below exists and is green
(`go build`, `go vet`, 74 packages / 0 failures). Nothing is committed, Tempo does not exist yet, and
no span has reached a collector. Tracing is **off** until `OTEL_EXPORTER_OTLP_ENDPOINT` is set, so
merging this changes no behaviour on its own.

## Where agrelha actually runs

This decides most of what follows, so it goes first.

agrelha runs on **fogo**, a NixOS host at `192.168.20.31`, as a **native systemd service** built with
`buildGoModule` (`fogo/modules/agrelha.nix` — `ExecStart = "${agrelhaPkg}/bin/agrelha"`,
`User = agrelha`). It is **not** in the Kubernetes cluster and **not** itself in a container;
`deploy/docker-compose.yml` in this repo is for other people running agrelha, not for fogo.

`RUNTIME = "docker"` is set, and `internal/wiring/wiring.go` branches hard on it:

| What | Where | Consequence |
|---|---|---|
| `buildRuntime` returns the Docker runtime immediately | `wiring.go:1067` | the k8s branch is never reached, so **`internal/infra/kube/` is dead code here** |
| `buildDeclarativePlane` returns a **nil** `*gitops.Committer` | `wiring.go:995` | **`internal/infra/gitops/` is dead code here**; state is `localstate` + SQLite |
| the Compose reconciler is used, `Async() == false` | `reconcile/compose/reconciler.go:131` | convergence is synchronous, so a trace covers it end to end |

`KUBECONFIG` is still provisioned in the nix module. Given the above it is **vestigial**, left from
when agrelha drove the cluster.

### The git-plane story, corrected

An earlier draft justified tracing as a way to diagnose "the git plane has been silent since
2026-10-21". **That was never a fault.** agrelha stopped committing because `RUNTIME=docker` does not
construct a committer — the silence is `wiring.go:995`. agrelha used to hold a git token and commit to
`yaya-ops` so Argo CD would reconcile the cluster; the move to fogo and Compose removed the need, and
the token with it. The four commits by `agrelha <agrelha@localhost>` on 2026-11-02 predate the
changeover.

So tracing is not about a mystery. It is about the work agrelha actually does on fogo.

## Why

To answer *"where did that go"* for agrelha's outbound work, which on fogo is three things:

1. **Content APIs** — eleven HTTP clients (CurseForge, Modrinth, Thunderstore, Steam, …). Where mod
   installs and update checks spend their time, and mostly invisible before this.
2. **The Docker socket** — every start, stop and container action on the Valheim, Minecraft and
   Garry's Mod servers. This replaced the Kubernetes API as the control path.
3. **RCON** — the admin and whitelist path for Minecraft and Garry's Mod, where errors are discarded
   by the caller.

### What this deliberately is not

- **No `gitops.*` or `kube.*` spans.** Both packages are dead under `RUNTIME=docker`.
- **Not metric→trace.** Grafana jumps metric→trace with *exemplars*, a Prometheus construct. fogo's
  Telegraf ships metrics to InfluxDB (`fogo/modules/observability.nix`), which has nowhere to put one.
  Clicking a latency spike to get a trace **will not work**, by design.
- **No domain spans everywhere.** Mod install, world create and wizard steps get spans when a
  specific question needs one.
- **One outbound call is knowingly untraced:** `modpack.ResolveNeoForgeVersion`
  (`internal/app/modpack/mrpack.go`) is a free function in `app` with no injection seam and eight call
  sites. Adding a client parameter for one best-effort metadata lookup was not worth it.

## The one architectural constraint

This repo's layer table (`docs/architecture.md`) *permits* `app` and `infra` to import `platform`,
but at the time this work started **neither did, in a single file** — the convention is that platform
belongs to the composition root and inner layers receive what they need, exactly as the architecture
doc says of `config`: *"Everything else takes narrow functional options or ports instead, so no
adapter or service can reach for a global config value."*

A first attempt had 16 inner-layer files importing `platform/tracing` and `platform/httpclient`
directly. That passed the ratchet and still inverted the dependency direction, so it was redone:
**every observability dependency is injected from `internal/wiring`**, through each package's own
functional-option pattern, with a no-op default.

The invariant, and how to check it:

```bash
grep -rl 'agrelha/internal/platform' internal/app internal/infra   # must print nothing
```

`internal/web` keeps two tracked files importing `platform/build` — pre-existing, version constants in
templates, unrelated.

## Dependencies

Verified to compile together against this module (`go 1.25.1`, Fiber `v2.52.9`):

| Module | Version |
|---|---|
| `github.com/gofiber/contrib/otelfiber/v2` | `v2.2.3` |
| `go.opentelemetry.io/otel` | `v1.47.0` |
| `go.opentelemetry.io/otel/sdk` | `v1.47.0` |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | `v1.47.0` |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | `v0.72.0` |

**OTLP over HTTP, not gRPC.** agrelha is off-cluster and reaches Tempo through a Traefik route with
basic auth; HTTP carries that in a header, gRPC through an auth middleware is more trouble than it
buys.

> `otelfiber v2.2.3` declares `otel v1.19.0` and `fiber v2.52.6` in its own `go.mod`. Minimal-version
> selection resolves upward and the OTel Go trace API is stable across v1, so it compiles — checked by
> building, not assumed. **Re-check on an `otel` major bump**; otelfiber is not actively tracking OTel
> releases. Note `otelfiber.WithServerName` does **not** exist in v2.2.3 (the real options are
> `WithNext`, `WithoutMetrics`, `WithTracerProvider`, `WithSpanNameFormatter`, `WithPort`,
> `WithPropagators`, `WithCollectClientIP`, `WithCustomAttributes`, `WithCustomMetricAttributes`,
> `WithMeterProvider`).

## The two new platform packages

Both import only stdlib and OTel — nothing from this module — so they satisfy the platform rule.

**`internal/platform/tracing`**

| Symbol | Purpose |
|---|---|
| `Enabled() bool` | true when `OTEL_EXPORTER_OTLP_ENDPOINT` is set |
| `Init(ctx, version, commit) (shutdown, error)` | builds the provider; a no-op shutdown when disabled |
| `Tracer(name) trace.Tracer` | `otel.Tracer("agrelha/" + name)` |
| `Job(ctx, name, fn)` | runs `fn` inside a root span called `job.<name>` |
| `Starter(component)` | returns `func(ctx, name) (ctx, func(error))` — start/finish pair that records the error |
| `ServiceName` | `"agrelha"` |

**`internal/platform/httpclient`**

| Symbol | Purpose |
|---|---|
| `Wrap(base) http.RoundTripper` | `otelhttp` over `base` (nil → `http.DefaultTransport`) |
| `Transport() http.RoundTripper` | `Wrap(nil)` |
| `New(timeout) *http.Client` | instrumented client with that timeout |

Both are referenced **only** from `internal/wiring` and `cmd/`.

## What was built

### 1. Tracer provider — `cmd/agrelha/main.go`

`tracing.Init(ctx, build.Version, build.Commit)` runs after `logging.Setup`, with its shutdown
deferred using the existing `shutdownTimeout`. Exporter `otlptracehttp` configured entirely from the
standard `OTEL_*` environment variables, `AlwaysSample()` (100%), resource carrying `service.name`,
`service.version`, `vcs.revision` and `host.name` from `os.Hostname()`.

**It degrades quietly.** A failing `Init` logs `slog.Warn("tracing disabled", …)` and agrelha starts
anyway; an unreachable Tempo is not an outage of the thing being observed. The startup line now also
reports `"tracing", tracing.Enabled()`.

### 2. Structured logging — `internal/web/shared/logging.go`

JSON logging was **already built** (`internal/platform/logging/logging.go:14` does
`slog.NewJSONHandler` + `SetDefault`, selected by `LOG_FORMAT`) and **already enabled** on fogo
(`agrelha.nix:105`). An earlier draft specified this work; it existed.

What changed: the request logger now adds **`trace_id`**, conditionally, in the same shape as the
file's existing optional fields:

```go
if tid := traceID(c); tid != "" {
    lg = lg.With("trace_id", tid)
}
```

with `traceID` reading `trace.SpanContextFromContext(c.UserContext())`. Conditional on purpose —
otherwise every line carries `trace_id:""` while tracing is off. `rid` is kept: it stays useful when
reading raw logs, but it is `reqCounter.Add(1)`, a per-process counter that resets on restart, so
`trace_id` is the field Grafana joins on.

### 3. Inbound HTTP — injected, not registered in `web`

`internal/web/routes.go` does **not** know about OTel. `ServerConfig` gained:

```go
TraceMiddleware fiber.Handler
```

registered first in `RegisterRoutes` when non-nil. `internal/wiring/server.go` builds it:

```go
func traceMiddleware() fiber.Handler {
	if !tracing.Enabled() {
		return nil
	}
	return otelfiber.Middleware(
		otelfiber.WithoutMetrics(true),
		otelfiber.WithNext(func(c *fiber.Ctx) bool { return shared.Untraced(c.Path()) }),
	)
}
```

`WithoutMetrics(true)` matters: otelfiber emits OTel **metrics** by default, which would start a
second metrics pipeline beside `/metrics` → Telegraf → InfluxDB.

**The SSE route is excluded.** `handlers/dashboard/handler.go:311` opens a long-lived Datastar stream;
the live journal on fogo shows one closing at `"dur_ms":460016` — **7.7 minutes**. Wrapped naively
that is a single 7.7-minute span. The exclusion is measured, not precautionary.

The skip list is factored in `shared/logging.go`, deliberately as **two** predicates, because the sets
differ by one path:

```go
func Unobserved(path string) bool  // /healthz, /metrics, /assets  — the logger skips these
func Untraced(path string) bool    // Unobserved + /sse            — tracing also skips SSE
```

A single shared list would have silently stopped logging `/sse` requests, which fogo logs today.

### 4. Outbound HTTP — one constructor, injected everywhere

Rather than a guard test policing twelve copies of `&http.Client{…}`, every client now takes one.
Each adapter gained an exported `DefaultTimeout` so wiring references the policy instead of
duplicating the number:

| Package | `DefaultTimeout` | Seam |
|---|---|---|
| `content/modrinth` | 15s | `WithHTTPClient` (new) |
| `content/curseforge` | 20s | `WithHTTPClient` (new) |
| `content/thunderstore` | 15s | `WithHTTPClient` (new) |
| `content/modpackindex` | 20s | `WithHTTPClient` (new) |
| `content/mcversions` | 15s | `WithHTTPClient` (new) |
| `content/steam` | 15s | `WithHTTPClient` (existed) |
| `valheimstatus` | 3s | `WithHTTPClient` (existed) |
| `roles/zitadel` | 20s | `Config.HTTPClient` (existed) |
| `runtime/docker` | 30s | `WithHTTPClient` (existed) + `SocketTransport` (new) |

Wiring supplies `httpclient.New(pkg.DefaultTimeout)`; the default inside each package is a plain,
uninstrumented client, so tests and other embedders are unaffected.

**The Docker socket is the highest-value item.** `SocketTransport(path)` is now exported, so wiring
wraps it:

```go
dockerruntime.NewSocketClient(cfg.DockerSocket,
    dockerruntime.WithHTTPClient(&http.Client{
        Transport: httpclient.Wrap(dockerruntime.SocketTransport(cfg.DockerSocket)),
        Timeout:   dockerruntime.DefaultTimeout,
    }))
```

It is a plain `*http.Transport` with a unix `DialContext`, so `otelhttp` wraps it like any other and
real method/path/status land on the span; only the URL host is synthetic. Exporting the constructor
also let `client_test.go` keep asserting the dial invariant without reaching into the struct.

Two more: `thunderstore.warm` now reuses `c.http` (its context already carries `indexTimeout`, so the
one-off client was redundant), and the image fetcher takes `Config.ImageTransport http.RoundTripper`
— transport only, because `imgHTTP`'s `CheckRedirect` is a security control (https-only, public hosts)
that must not be replaceable by an injected client.

### 5. Outbound non-HTTP — RCON

`internal/infra/rcon` does not import `platform`. It declares its own seam:

```go
type Tracer func(ctx context.Context, name string) (context.Context, func(error))
func WithTracer(t Tracer) Option
```

defaulting to `noTrace`, threaded through `NewClient` and `Pool` (so pooled clients inherit it).
Wiring passes `tracing.Starter("rcon")`. Span name is `rcon.execute <verb>`.

**Only the verb is recorded**, via `commandVerb(cmd)` — `/op`, `/whitelist` — never the full command,
which carries usernames.

The reason this is worth a port change: `internal/app/access/access.go` **discards these errors** at
six call sites.

```go
_, _ = a.rcon.Execute(ctx, "/op "+username)   // access.go:114, and :156 :194 :195 :237 :238
```

The finish func receives the error even when the caller drops it, so the span is the only place that
failure surfaces.

What the signature change cost, in full — larger than first estimated:
`Console.Execute(ctx, cmd)` on the port (`internal/ports/runtime.go:155`), `rcon.Client.Execute`,
three test fakes, **and three `AccessManager` methods that had no context at all**
(`OnlinePlayers`, `WhitelistEnforced`, `SetWhitelistEnforced`) plus their five callers, and
`wiring.go`'s `rconExec` closure. Everything had a context in scope one frame up, so nothing needed
`context.TODO()`.

> `shortcut: Execute takes ctx for the span and cancellation, but the per-call timeout is still the
> construction-time one on Client. Upgrade to a per-call deadline if a hung RCON ever blocks a
> request.`

### 6. Background jobs — an injected tracer per scheduler

Everything above is request-scoped. **Five loops run on timers with no request behind them**, and
would otherwise produce nothing, or parentless single spans. Each declares the same local seam —

```go
type JobTracer func(ctx context.Context, name string, run func(context.Context))
func WithJobTracer(t JobTracer) Option
```

— defaulting to `noJobTrace` (which just calls `run(ctx)`), with wiring passing `tracing.Job`:

| Package | Entry point | Span |
|---|---|---|
| `app/modupdates` | `Checker.Start` → `Refresh` | `job.modupdates.refresh` |
| `app/restarts` | `Queue.Run` → `tick` | `job.restarts.tick` |
| `app/backups` | `BackupScheduler.Start` → `RunDaily` | `job.backups.daily` |
| `app/health` | `Watcher.Start` → `Check` | `job.health.check` |
| `app/ingest` | `Supervisor.Run` → `Reconcile` | `job.ingest.reconcile` |

Each took a `context.Context` and passed **the same process-lifetime context** into every tick, with
no span in it. **This is where the value is:** `modupdates` is the job that hammers the content APIs
on a timer, and `restarts`/`backups` drive the Docker socket. A request-only design would trace the
operator clicking buttons and miss everything agrelha does on its own.

`backups` spans `RunDaily` rather than the tick, because the tick is mostly a clock check that does
nothing — spanning it would emit an empty span every 15 minutes.

**Two long-lived loops are excluded**, for the same reason as the SSE route: `occupancy.Watch`
(`tracker.go:71`) is a per-instance stream consumer, and `logtail.Follow` (`logtail.go:22`) returns an
open stream. Neither is a tick job.

### 7. Tests for the seams

`internal/app/health/jobtracer_test.go` and `internal/infra/rcon/tracer_test.go`. Both were verified
to **fail when the seam is bypassed**, which is the only thing that makes them worth having:

| Break introduced | Test output |
|---|---|
| call `w.Check(ctx)` directly instead of through the tracer | `job tracer was never invoked; Start does not route ticks through it` |
| swallow the error instead of passing it to the finish func | `tracer finish got nil error; a failing command must be reported even though callers discard it` |

Also covered: both no-tracer defaults still run, and `Execute` returns `context.Canceled` for a
cancelled context.

The **existing** ratchet did its job too: `internal/arch.TestPackageTable` failed twice during this
work, once for each new platform package, until `docs/architecture.md` gained a row. Those rows are
part of this change.

### 8. Deployment — `fogo/modules/agrelha.nix` (not yet done)

Not a Kubernetes Deployment. Two entries in the systemd service's `environment` block, beside the
existing `LOG_FORMAT = "json"`:

```nix
OTEL_EXPORTER_OTLP_ENDPOINT = "https://tempo.ykhi.xyz";
OTEL_EXPORTER_OTLP_PROTOCOL = "http/protobuf";
```

The basic-auth credential does **not** belong in `environment` — it is readable in
`/proc/<pid>/environ` and the nix store. It goes in the existing sops env template, which the service
already loads via `EnvironmentFile`:

```nix
sops.secrets.tempo_push_basic = { owner = "agrelha"; };

sops.templates."agrelha.env".content = ''
  …existing lines…
  OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic ${config.sops.placeholder.tempo_push_basic}
'';
```

Store the pre-encoded `user:password` base64 as the secret so the template does no encoding.

## What remains

1. **Tempo + the push route + the Grafana datasource** — Phase 5a of the yaya-ops runbook. Nothing
   here can be seen until that exists.
2. **The `tempo-push-auth` Secret** in the cluster and its password in fogo's sops store.
3. **The two nix env entries** above.
4. Then take the readings in "Done means".

## Done means

1. **Infrastructural** — a request produces a trace in Grafana; the span→logs button lands on that
   request's log lines; a log line's TraceID links back to the span.
2. **Both halves of the program are covered**, gated behind 1:
   - an operator action (start a Valheim server) shows the waterfall, including the `docker` socket
     call;
   - a **background** `modupdates` tick produces its own root span with the content-API spans parented
     under it — not bare parentless spans.
3. **An RCON admin action is visible** — `/op` on a Minecraft world yields an `rcon.execute /op` span,
   and a forced failure appears on it instead of being discarded.

## Verification done so far

- `go build ./...`, `go vet ./...` clean; `go test ./...` — **74 packages, 0 failures**.
- `grep -rl 'agrelha/internal/platform' internal/app internal/infra` prints nothing.
- Both new tests shown to fail when their seam is bypassed.
- Dependency set compiled together before being adopted.

Not verified: no span has reached a collector, because Tempo does not exist yet.
