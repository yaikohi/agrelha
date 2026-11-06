# Tracing agrelha

The app half of **Phase 5** of the yaya-ops observability runbook
(`yaya-ops/docs/observability-expansion-runbook.md`). That document covers Tempo, the Grafana
datasource and the correlation wiring; this one covers the Go changes. Designed in a `/grill-me`
session 2026-11-06. **Designed, not built.**

## Why

To answer *"where did that hang"* about agrelha's **outbound** work. agrelha is one process, so there
is no distributed call graph to reconstruct — but almost all of its interesting latency is in calls
that leave the process: git clone/fetch/push against the gitops plane, the Kubernetes API, and eleven
content APIs.

The concrete motivator: **the git plane has been silent since 2026-10-21** and the root cause is
still unknown. A span around the committer path distinguishes the three possibilities that logs
currently cannot — the push *fails*, the push *hangs*, or the commit path is *never reached*. That
diagnosis is the exit criterion for this work, not a side effect of it.

### What this deliberately is not

- **Not metric→trace.** Grafana jumps from a metric spike to a trace via *exemplars*, which are a
  Prometheus construct. There is no Prometheus server by design — `/metrics` is scraped by Telegraf
  into InfluxDB (`measurement=prometheus`). Exemplars have nowhere to live. Clicking a latency spike
  to get the slow trace **will not work**, and that is a known trade, not a defect.
- **Not domain spans everywhere.** Only the two non-HTTP outbound paths get hand-written spans.
  Spans for "mod install", "world create" or wizard steps get added when a specific question needs
  one.
- **Not cluster tracing.** No apiserver/kubelet spans, no mesh. See the runbook.

## Dependencies

Verified to compile together against this module (`go 1.25.1`, Fiber `v2.52.9`) — a throwaway module
with exactly these versions passed `go build` and `go vet` with exit 0 on 2026-11-06:

| Module | Version |
|---|---|
| `github.com/gofiber/contrib/otelfiber/v2` | `v2.2.3` |
| `go.opentelemetry.io/otel` | `v1.47.0` |
| `go.opentelemetry.io/otel/sdk` | `v1.47.0` |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | `v1.47.0` |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | `v0.72.0` |

> **The version skew is real but harmless.** `otelfiber v2.2.3` declares `otel v1.19.0` and
> `fiber v2.52.6` in its own `go.mod`. Go's minimal-version-selection resolves *upward* to
> `otel v1.47.0` and `fiber v2.52.9`, and the OTel Go trace API is stable across v1, so it compiles.
> This was checked by building, not assumed — **re-check it on any `otel` major bump**, because
> `otelfiber` is not actively tracking OTel releases.

## The changes

### 1. Tracer provider — `cmd/agrelha/main.go`

A `tracing.Init(ctx) (shutdown func(context.Context) error, err error)` helper, called from `main`
before the server starts, with its shutdown deferred alongside the existing graceful-shutdown path.

- Exporter: `otlptracegrpc`, endpoint from `OTEL_EXPORTER_OTLP_ENDPOINT`.
- Sampler: `sdktrace.AlwaysSample()` — **100%**. The thing being hunted is rare; sampling defeats the
  purpose. Revisit only if volume makes it expensive.
- Resource: `service.name=agrelha`, plus `service.version` and `service.instance.id` from the existing
  `internal/build` vars (`build.Version`, `build.Commit`) so a span says which build produced it.
- **Degrade quietly.** If the exporter cannot reach Tempo, agrelha must still start and serve. An
  observability backend being down is not an outage of the thing being observed — the OTLP exporter
  retries in the background and `Init` must not be fatal.

### 2. Structured logging — `cmd/agrelha/main.go` + `internal/web/shared/logging.go`

Two changes, and this is what makes the whole thing navigable:

- **`main.go` sets `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, …)))`.** Today no handler
  is installed, so the default emits `2026/11/06 12:00:00 INFO msg key=value` — a Go `log` timestamp
  prefix in front of logfmt pairs, which Loki's parser will not cleanly eat. The Tempo→Loki jump
  queries `| json | trace_id="…"`, so the lines have to be JSON.
- **`logging.go` logs `trace_id` instead of `rid`.** `rid` is `reqCounter.Add(1)`, a per-process
  counter: it resets to 1 on every restart and collides across pod restarts, so it cannot correlate
  anything beyond a single process lifetime. The trace ID is globally unique and is the join key
  Grafana uses. Take it from the span context:
  `trace.SpanContextFromContext(c.UserContext()).TraceID().String()`.

> Keep `rid` *as well* if the shorter value is useful when reading raw logs — but `trace_id` is the
> field the datasource links on.

### 3. Inbound HTTP — `internal/web/routes.go`

`otelfiber.Middleware(...)` registered alongside `RequestLogger`.

**The SSE route must be excluded.** `internal/web/handlers/dashboard/handler.go` opens a long-lived
Datastar SSE stream (`"sse open"` / `"sse close"`, stream `"main"`) that stays open for minutes or
hours. Wrapped naively, that is **one span per connection**, held open for the connection's life —
which shows in Tempo as a single useless multi-hour bar and never closes until the client navigates
away. This is the most common way Fiber auto-instrumentation looks broken.

Use `otelfiber.WithNext(func(c *fiber.Ctx) bool { … })` to skip it (verified present in v2.2.3).

**Factor the skip list.** `logging.go:21` already skips `/healthz`, `/metrics` and `/assets`. The
tracing middleware needs the same set plus the SSE path. Extract **one** predicate both call rather
than maintaining two lists — two lists that disagree is precisely the drift documented in
`docs/drift.md`, and the token work in this repo exists because of that pattern.

If per-frame visibility inside a stream is ever wanted, span the *work a frame triggers*, never the
connection.

### 4. Outbound HTTP — the eleven clients

Wrap each client's transport: `http.Client{Transport: otelhttp.NewTransport(base)}`. This yields a
child span per outbound request with method, URL, status and duration, for free.

| Package |
|---|
| `internal/infra/content/modrinth/client.go` |
| `internal/infra/content/curseforge/client.go` |
| `internal/infra/content/thunderstore/client.go` |
| `internal/infra/content/steam/client.go` |
| `internal/infra/content/modpackindex/client.go` |
| `internal/infra/content/mcversions/client.go` |
| `internal/infra/valheimstatus/client.go` |
| `internal/infra/roles/zitadel/client.go` |
| `internal/infra/runtime/docker/client.go` |
| `internal/web/handlers/content/handler.go` |
| `internal/app/modpack/mrpack.go` |

Requests must carry the request's context (`c.UserContext()` in Fiber) or the child spans orphan.

### 5. Outbound non-HTTP — the paths that actually matter

Auto-instrumentation **cannot** see these, which is exactly why the git-plane problem is still open:
the handler would look fast with a silent gap where the commit should be.

`ctx` is already the first parameter throughout both packages, so this costs no signature churn.

**`internal/infra/gitops/`** — span these, named `gitops.*`:

| Function | File | Why it matters |
|---|---|---|
| `ensureCloneLocked` | `committer.go:51` | clone / open — first place a hang can sit |
| `syncRemoteLocked` | `committer.go:110` | fetch / reset against the remote |
| `Patch` | `committer.go:172` | the commit+push path |
| `WriteDirectory` | `dir.go:19` | |
| `DeleteDirectory` | `dir.go:108` | |
| `SetData` / `DeleteData` / `ReplaceData` / `ReplaceConfigMap` | `data.go:18,24,30,39` | |
| `mutateRaw` | `data.go:79` | the shared mutate→commit core |

Record on the commit/push spans: the target path, the commit message, whether a change was actually
produced (these return `(bool, error)` — **a `false` is the "nothing to commit" case and is the prime
suspect for silence**), and the error.

**`internal/infra/kube/client.go`** — span the operations, named `kube.*`: `Restart`,
`RestartDeployment`, `Scale`, `Replicas`, `DeploymentReplicas`, `CreateBackupJob`,
`CreateRestoreJob`, `activeDeploymentName`.

### 6. The ratchet — a guard test

There are **eleven** HTTP clients today, so a twelfth is near-certain, and the failure mode is
silent: someone adds a client, forgets the transport, and it is untraced forever with nothing
complaining.

This repo already defends against omission this way — `internal/arch/arch_test.go` fails the build
when a package has no row in the architecture doc, and
`internal/web/pages/design_tokens_test.go:93` fails it when a CSS token has no documentation row,
**bidirectionally**. Add the same move for tracing:

1. Every `internal/infra/**/client.go` that constructs an `http.Client` must reference
   `otelhttp.NewTransport`.
2. Every package named as traced in this document must still exist (catches the doc rotting).

> **Named weakness:** this greps source, so it checks *spelling, not behaviour*. A client that wraps
> its transport but is never called still passes, and a client that builds its transport through a
> helper the regex doesn't match fails spuriously. It is still worth having, because the risk being
> defended against is **omission**, not misuse. `design_tokens_test.go` makes the same trade and has
> earned its keep.

### 7. Deployment — `yaya-ops`

One env var on the agrelha Deployment:

```yaml
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: http://tempo.observability.svc.cluster.local:4317
```

## Done means

1. **Infrastructural** — a request produces a trace in Grafana; the span→logs button lands on that
   request's log lines; a log line's TraceID links back to the span.
2. **Diagnostic**, gated behind 1 — trigger a world or config write and read the waterfall. The trace
   must show which of **clone → mutate → commit → push** fails, hangs, or is never entered.

**This is not finished at step 1.** The justification for the whole phase was a standing, unexplained
failure; shipping the instrument without taking the reading would leave the actual question exactly
where it was.

## Build order

1. `tracing.Init` + the `slog` JSON switch (verifiable alone: spans appear for inbound requests).
2. `otelhttp` on the eleven clients + the guard test.
3. Manual `gitops.*` and `kube.*` spans.
4. Take the git-plane reading.
