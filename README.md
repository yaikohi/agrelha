# agrelha

Single-user web UI to manage the Valheim gameserver running on the `yaya` Talos
cluster. Go + Fiber + templ + Tailwind + **Datastar** (SSE). Deployed via GitOps
from [yaya-ops](https://codeberg.org/ykhi/yaya-ops); image hosted in the
self-hosted Zot registry at `registry.ykhi.xyz/agrelha`.

## Architecture — two-plane hybrid

The `valheim` ArgoCD app has `selfHeal: true`, so live cluster edits get reverted.
agrelha therefore treats state and actions differently:

- **Declarative plane (git):** mods (`valheim-mods` ConfigMap), mod `.cfg` files
  (`valheim-mod-configs`), and the admin list (`valheim-admins`) → agrelha commits
  to `yaya-ops@main` (Codeberg bot token) → ArgoCD syncs → rollout. After each
  commit a background watcher polls the live ConfigMap until ArgoCD reconciles,
  then rolls the pod so the change actually takes effect.
- **Imperative plane (k8s API):** restart / stop / start / logs / pod metrics →
  `client-go` against a ServiceAccount scoped to the `valheim` namespace (no exec).

Auth: in-app **Zitadel OIDC**, single permitted identity (`ALLOWED_EMAIL`),
stateless HMAC-signed session cookies. Persistence: embedded **SQLite** (modernc,
no cgo) holding the player roster/presence, action audit, server-event timeline,
and the Thunderstore mod index + README cache.

## Features

- **Dashboard** — live tiles (online players, CPU, memory, uptime) and a live log
  tail over SSE; last-backup summary from the NFS-mounted NAS export; Restart /
  Update / Stop / Start controls; deep-link to the Grafana Valheim dashboard.
- **Mods** — search/browse Thunderstore (background-indexed, instant), per-mod
  detail pages (README, dependencies), install-with-dependency-resolution and
  remove (git-committed). **Update detection**: a red dot + popover show which
  installed mods have newer versions (current → latest) with Update all / Update
  selected (full dependency re-resolve, auto-restart). **Modpack export**:
  download a `.r2z` profile of the installed mods + configs to import into
  r2modman / Thunderstore Mod Manager.
- **Configs** — edit mod `.cfg` files (the `valheim-mod-configs` ConfigMap).
- **Admins** — grant/revoke in-game admin by Steam64, picked from the auto-built
  player roster or entered manually.
- **History** — merged audit + server-event timeline.
- **Observability** — structured `slog` logging; Prometheus `/metrics` (HTTP, SSE,
  control-action, and Go runtime series) scraped into the cluster's InfluxDB.

## Layout

```
cmd/api/main.go          entrypoint (graceful shutdown, slog setup)
cmd/web/                 embedded assets + templ pages
  pages/*.templ          Layout, nav, Dashboard, Mods, ModDetail, Configs,
                         ConfigEdit, Admins, History, Login, flash, UpdateList
  assets/                css (Tailwind v4) + js (vendored Datastar v1.0.2)
internal/config          env -> Config
internal/server          Fiber server, routes, SSE handlers, render, metrics mw
internal/auth            Zitadel OIDC (login/callback/middleware, signed cookies)
internal/k8s             imperative plane (restart/scale/status/metrics/logs)
internal/gitops          go-git clone -> edit ConfigMap YAML -> commit/push
internal/mods            mods.txt install/remove/replace
internal/admins          admin-list grant/revoke
internal/thunderstore    package index (streamed + cached), dependency resolve
internal/modpack         .r2z export builder
internal/ingest          log-tailing goroutine -> player roster/presence
internal/store           SQLite schema + queries (roster/audit/events/mod index)
internal/backups         NAS backup dir stat
internal/sse             hand-written Datastar SSE frames (patch-signals/elements)
internal/mdrender        README markdown -> sanitized HTML + image proxy rewrite
internal/metrics         Prometheus collectors
internal/logging         slog handler setup
internal/valheim         status.json parsing (A2S; superseded by log presence)
```

## Dev

```sh
mise install          # go, bun, templ, air, task
cp .env.example .env  # leave OIDC_ISSUER empty to bypass auth locally
go mod tidy           # generates go.sum + resolves k8s/oidc deps
task build            # templ generate + tailwind + vendor datastar + go build
task watch            # air live-reload
go test ./...         # unit tests (server package)
```

Key env vars (see `.env.example`): `OIDC_*` + `ALLOWED_EMAIL` (auth),
`GIT_REPO_URL` / `CODEBERG_USERNAME` / `CODEBERG_TOKEN` (declarative plane),
`MODS_PATH` / `ADMINS_PATH` / `MOD_CONFIGS_PATH` (target files in the repo),
`VALHEIM_NAMESPACE` / `VALHEIM_DEPLOYMENT` (imperative plane),
`BACKUPS_DIR`, `INFLUXDB_URL`, `GRAFANA_DASHBOARD_URL`, `LOG_LEVEL` / `LOG_FORMAT`.

## Ship

```sh
task build:image TAG=0.8.6      # docker build + push to registry.ykhi.xyz/agrelha
# then bump the image tag in yaya-ops manifests/agrelha-app.yaml and commit
```

See `plan.md` for the full feature/version changelog and remaining work.
