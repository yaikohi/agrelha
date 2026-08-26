# agrelha — plan & status

Valheim gameserver control panel (Go + Fiber + templ + Tailwind + Datastar), deployed
to the `yaya` Talos cluster via GitOps from `yaya-ops`, image in the self-hosted Zot
registry (`registry.ykhi.xyz/agrelha`). Reached at `https://agrelha.ykhi.xyz`
(WireGuard-only, behind Zitadel OIDC).

**Current version: `0.6.1`** (mod browsing + metadata cache; `0.6.1` adds README
markdown styling). Build+push `0.6.1` to ship it.

> The rendered README had no styling because this Tailwind v4 build has no
> Typography plugin — the `prose` classes were dead. README now uses a `.md`
> scope with hand-written markdown CSS in `cmd/web/assets/css/input.css` (rebuild
> `output.css` via `task tailwind:build` / the CLI when it changes).

---

## Where we are

Two-plane hybrid (chosen because the `valheim` ArgoCD app has `selfHeal: true`, so
live cluster edits get reverted):

- **Declarative plane (git):** mods (`valheim-mods.yaml`), admin list
  (`valheim-admins.yaml`) → agrelha commits to `yaya-ops@main` as `agrelha
  <agrelha@ykhi.xyz>` → ArgoCD syncs → pod rolls.
- **Imperative plane (k8s API):** restart / stop / start / logs / metrics → `client-go`
  against a ServiceAccount scoped to the `valheim` namespace (no exec).

### Done

- **Infra:** Zot registry (`registry.ykhi.xyz`, `docker2s2` compat), agrelha ns + SA +
  scoped RBAC (pods, pods/log, deployments+scale, configmaps read, metrics.k8s.io),
  ExternalSecrets (codeberg token, OIDC client, registry pull creds) seeded via tal
  `02-platform-config/apps-secrets.tf`.
- **Auth:** Zitadel OIDC (`https://zitadel.o0o.zip`), single identity `ykhi@proton.me`,
  UserInfo fallback, `/login` landing page, **RP-initiated logout** (ends the Zitadel
  session too).
- **Dashboard:** live SSE tiles (Players / CPU / Memory / Uptime) + live log tail;
  restart/stop/start buttons; Grafana deep-link.
- **Mods:** Thunderstore search (background-streamed index) + dependency-resolved
  install + remove, each a git commit; direct install-by-identifier fallback.
- **Admins:** grant/revoke in-game admin (Steam64 → `ADMINLIST_IDS`) via git commit;
  player roster from the log ingester.
- **Auto-apply:** after a mod/admin commit, `applyAfterSync` waits for ArgoCD to
  reconcile the ConfigMap, then rolls the pod so the change takes effect.
- **Persistence:** SQLite (`players`, `audit`, `events`, `mod_index`, `mod_readme`,
  `meta`) at `/data`.
- **Mod browsing + metadata cache (0.6.0):** the streamed index now keeps
  icon/description/version/downloads/deprecated per package, is persisted to
  `mod_index` and preloaded on startup (instant browse, no startup re-pull, bg 6h
  refresh). Search results + installed list show icon + description and link to a
  detail page `GET /mods/{namespace}/{name}` (header, deprecated warning, direct
  dependency list, README, Install button, Thunderstore link). READMEs are lazily
  fetched and cached forever by immutable version in `mod_readme`, rendered via
  `internal/mdrender` (goldmark, raw HTML off → bluemonday allowlist; images
  restricted to `gcdn.thunderstore.io`, links forced `nofollow noopener _blank`).

---

## Remaining work

### P1 — correctness / it-bugs-me

- [ ] **Session persistence.** Sessions are an in-memory map (`internal/auth`), so every
      deploy/pod-restart forces a re-login. Persist to SQLite (a `sessions` table) or
      switch to signed/encrypted cookies (no server-side store). Prefer signed cookies.
- [ ] **OIDC `state` CSRF.** `Login` uses a fixed `"state-todo"`. Generate a random
      state, store it (cookie), verify in `Callback`. Same for a `nonce`.
- [ ] **Session eviction.** The in-memory map never evicts expired entries (minor leak).
      Moot if we move to signed cookies.
- [ ] **Inline error/success feedback.** Install/grant failures currently return a raw
      Fiber error page; redirects give no confirmation. Add flash messages (a signal or
      a small banner) so the user sees "installed X (+3 deps)" / "resolve failed: …".

### P2 — features from the original design not yet built

- [x] **Mod browsing + metadata cache.** Done in 0.6.0 (see Done above); spec kept
      below for reference.
- [ ] **Audit / event timeline UI.** `store` records audit + events but nothing renders
      them. Add a `/history` page (recent restarts, installs, grants, joins/leaves,
      auto-restarts).
- [ ] **Mod config (.cfg) editing.** `valheim-mod-configs` ConfigMap — edit per-mod
      `.cfg` from the UI via the same git-commit path. (Deferred v2 in the design.)
- [ ] **Player presence.** Roster tracks `last_seen`/`sessions` but not online/offline.
      Track connect/disconnect pairs from the ingester so "online now" is shown, and the
      admin-grant picker can highlight currently-connected players.
- [ ] **"Update now" action.** Only restart/stop/start today. Add a server-update trigger
      (the lloesche image updates on `UPDATE_CRON`; a manual path may just be a restart).
- [ ] **World / backup size + last-backup tiles.** Dropped in step ③ because we removed
      `pods/exec`. Options: read the NFS backups PVC from a tiny read-only sidecar, or
      parse backup log lines in the ingester into `events` and surface "last backup".

### P3 — polish / infra

- [ ] **Cut the periodic 162 MB pull (optional).** Index persistence (startup re-pull)
      is handled by the mod-browsing spec below; this item is only the *further*
      optimization of replacing the 162 MB v1 list with the gzip
      `/api/experimental/package-index/` JSONL endpoint to cut the 6-hourly bandwidth.
      Needs validating that endpoint carries description+icon and isn't Cloudflare-walled.
- [ ] **"Update available" detection.** Compare installed mod versions (mods.txt) against
      the Thunderstore index `latest` and flag upgrades on the mods page.
- [ ] **CI.** No CI — images are built manually (`task build:image TAG=x`). Add
      Forgejo Actions/Woodpecker on Codeberg to build+push on tag and (optionally) bump
      the yaya-ops image tag.
- [ ] **Lean image build.** `task setup` installs `air` (dev-only) in the Docker build,
      dragging in a go1.26 toolchain download. Split a `setup:ci` that skips it.
- [ ] **Tests.** None yet. Unit-test the pure logic: `mods.Parse`/Install/Remove,
      `admins` grant/revoke transforms, `thunderstore.entry`/`ResolveTree` parsing,
      `gitops` YAML round-trip.
- [ ] **Fix the Players tile at the source.** `status.json` returns
      `TimeoutError` (the server's A2S self-query fails) — pre-existing lloesche/Valheim
      issue, independent of agrelha. Until fixed, Players reads `—`.
- [ ] **InfluxDB sparklines (optional).** CPU/Mem are instantaneous from metrics-server;
      historical mini-charts would need an InfluxDB read token.

---

## Spec: mod browsing & metadata cache

Grilled + agreed 2026-08-24. Read-only browsing feature; does **not** change how
install works (still ResolveTree → git commit). Key facts that shaped it:

- Short **description + icon are already in the 162 MB list we stream** (per version) —
  enriching the index is free, no extra fetch.
- The **README (rich markdown) is a separate per-mod fetch**
  (`…/api/experimental/package/{ns}/{name}/{version}/readme/` → `{markdown}`).
- **Thunderstore versions are immutable** → a README cached by version is never stale
  (no TTL, no invalidation).

### Data model (SQLite) — replaces the unused `mod_cache` placeholder

```sql
mod_index(
  full_name TEXT PRIMARY KEY,   -- "namespace/name"
  namespace, name, owner, version, description, icon, package_url,
  downloads INTEGER, is_deprecated INTEGER, updated_at TIMESTAMP
);
mod_readme(
  full_name TEXT, version TEXT, markdown TEXT, fetched_at TIMESTAMP,
  PRIMARY KEY (full_name, version)   -- immutable, no expiry
);
```

### Index lifecycle (persisted, background refresh)

- Extend the streamed-parse (`thunderstore.warm`) to also keep `description`, `icon`,
  `version`, `downloads`, `is_deprecated` per package.
- **Startup:** load `mod_index` from SQLite into memory → browse instantly, survives
  restarts (no startup re-pull). Search stays in-memory substring, ordered
  **most-downloaded first**.
- **Background:** if the persisted index is >6 h old, stream Thunderstore and upsert
  `mod_index`. Never blocks startup/browsing. (The 162 MB pull still happens every 6 h —
  cutting it is the separate P3 gzip-package-index item.)

### Browse UI

- Search results **and the installed list** show icon + short description (looked up in
  the index), each linking to a detail page.
- Detail page `GET /mods/{namespace}/{name}`: header (icon, name, version, downloads,
  last-updated, **deprecated warning**), dependency list, README (sanitized markdown),
  **Install +deps** button, **View on Thunderstore ↗** link.

### README fetch + render

- **Lazy:** on detail view, cache hit → render from SQLite (zero network); miss → fetch
  the readme endpoint, store in `mod_readme`, render.
- **Render pipeline (untrusted mod-author markdown = stored-XSS surface):**
  `goldmark` (raw HTML **disabled**) → AST pass dropping `<img>` whose src host ≠
  `gcdn.thunderstore.io` → `bluemonday` sanitize; links forced
  `target=_blank rel="noopener nofollow"`. Basic markdown only (headings, emphasis,
  lists, links, code, blockquotes, tables, CDN-only images). Render markdown→HTML on
  each view (goldmark is fast); inject via `templ.Raw` on the sanitized output only.
- **New deps:** `github.com/yuin/goldmark`, `github.com/microcosm-cc/bluemonday`.

### Not doing

- No arbitrary-host images (privacy + CSP); rich visuals live behind the Thunderstore
  link.
- No mutable-metadata (downloads/rating) live fetch on detail view — use index values,
  so a detail view is at most one network call (the README) and a cache hit is zero.

---

## Deploy & ops runbook

```sh
# build + push a new image
cd ~/projects/agrelha
git add -A && git commit -m "…"
task build:image TAG=<next>        # -> registry.ykhi.xyz/agrelha:<next>

# point the deployment at it
cd ~/projects/yaya-ops
#   edit manifests/agrelha-app.yaml image tag -> <next>
git add manifests/agrelha-app.yaml && git commit && git push
kubectl -n agrelha rollout status deploy/agrelha
```

- **Dev loop:** `cp .env.example .env` (leave `OIDC_ISSUER` empty to bypass auth),
  `go mod tidy` once, `task watch`. k8s/metrics/mods degrade gracefully off-cluster.
- **Secrets** live in tal `02-platform-config` (`vault_kv_secret_v2 "agrelha"`):
  `agrelha_codeberg_username/token`, `agrelha_oidc_client_id/secret`; registry pull
  creds reuse `var.zot_*`. `tofu apply` after changing them.
- **Zitadel app** requires: redirect URI `https://agrelha.ykhi.xyz/auth/callback`,
  post-logout URI `https://agrelha.ykhi.xyz/login`, and **"User Info inside ID Token"**
  enabled. `ALLOWED_EMAIL=ykhi@proton.me` in `agrelha-config`.
- **DNS:** `agrelha.ykhi.xyz` → `192.168.20.220` (Traefik LB), grey-cloud/DNS-only,
  WireGuard-only.

---

## Gotchas we already hit (don't re-debug these)

- **Every `*.ykhi.xyz` host** must A-record to **`192.168.20.220`** (Traefik LB),
  DNS-only. Pointing at a public IP → Traefik serves its default self-signed cert.
- **Zot** rejects Docker schema-2 manifests with 415 unless `"compat":["docker2s2"]`
  is set (it is). `docker build` + `docker push` then works.
- **Datastar v1.0.2:** `data-on-load` does NOT fire (no `load` event on a div). Use
  `data-effect="@get('/sse')"`. SSE frames are hand-written (`internal/sse`) because
  the datastar-go SDK is net/http and Fiber is fasthttp.
- **Thunderstore** `v1/package/` is ~162 MB — never fetch it in a request; stream it in
  the background (done).
- **htpasswd** for Zot is derived in tofu (`loafoe/htpasswd`) from `zot_password` — one
  source of truth, no drift.
- **ExternalSecret ↔ OpenBao drift:** a new ExternalSecret property must be seeded in
  tal `apps-secrets.tf` + `tofu apply` or the pod sits in `CreateContainerConfigError`.

---

## Layout

```
cmd/api/main.go          entrypoint
cmd/web/pages/*.templ    Layout, nav, Login, Dashboard, Mods, Admins
cmd/web/assets/          Tailwind (output.css) + vendored Datastar (datastar.js)
internal/config          env -> Config
internal/server          Fiber server, routes, SSE + mod/admin handlers, applyAfterSync
internal/auth            Zitadel OIDC (login/callback/logout/middleware)
internal/k8s             restart/scale/status/logs/metrics/configmaps
internal/ingest          log tail -> player roster
internal/store           SQLite schema + queries
internal/gitops          go-git commit-a-ConfigMap-data-key
internal/mods            install/remove -> mods.txt
internal/admins          grant/revoke -> ADMINLIST_IDS
internal/thunderstore    search index (streamed, persisted) + deps + readme fetch
internal/mdrender        goldmark + bluemonday README sanitizer (CDN-only images)
internal/sse             Datastar v1.0 SSE frame writers
```
