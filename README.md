# agrelha

Single-user web UI to manage the Valheim gameserver running on the `yaya` Talos
cluster. Go + Fiber + templ + Tailwind + **Datastar** (SSE). Deployed via GitOps
from [yaya-ops](https://codeberg.org/ykhi/yaya-ops); image hosted in the
self-hosted Zot registry at `registry.ykhi.xyz/agrelha`.

## Architecture — two-plane hybrid

The `valheim` ArgoCD app has `selfHeal: true`, so live cluster edits get reverted.
agrelha therefore treats state and actions differently:

- **Declarative plane (git):** mods (`valheim-mods.yaml`), admin list
  (`valheim-admins.yaml`) → agrelha commits to `yaya-ops@main` (Codeberg bot
  token) → ArgoCD syncs → rollout.
- **Imperative plane (k8s API):** restart / stop / start / logs → `client-go`
  against a ServiceAccount scoped to the `valheim` namespace (no exec).

Auth: in-app **Zitadel OIDC**, single permitted identity (`ALLOWED_EMAIL`).
Persistence: embedded **SQLite** (player roster, action audit, mod cache, events).

## Layout

```
cmd/api/main.go          entrypoint (graceful shutdown)
cmd/web/                 embedded assets + templ pages
  pages/*.templ          Layout, Dashboard
  assets/                css (Tailwind v4) + js (vendored Datastar)
internal/config          env -> Config
internal/server          Fiber server, routes, templ render
internal/auth            Zitadel OIDC (login/callback/middleware)
internal/k8s             imperative plane (restart/scale/replicas)
internal/store           SQLite schema + helpers
```

## Dev

```sh
mise install          # go, bun, templ, air, task
cp .env.example .env  # leave OIDC_ISSUER empty to bypass auth locally
go mod tidy           # REQUIRED once: generates go.sum + resolves k8s/oidc deps
task build            # templ generate + tailwind + vendor datastar + go build
task watch            # air live-reload
```

## Ship

```sh
task build:image TAG=0.1.0      # docker build + push to registry.ykhi.xyz/agrelha
# then bump the image tag in yaya-ops manifests/agrelha-app.yaml and commit
```

## Status

Scaffold (step ②). Wired: config, server, auth, k8s restart/scale, SQLite schema,
dashboard shell + control buttons. **TODO (step ③):** Datastar SSE (metrics tiles
+ live log tail), log-ingester goroutine → player roster, Thunderstore search +
dependency resolution + git-commit mod install, admin grant via git commit.
