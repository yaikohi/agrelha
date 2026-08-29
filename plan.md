# agrelha — plan & status

Valheim gameserver control panel (Go + Fiber + templ + Tailwind + Datastar), deployed
to the `yaya` Talos cluster via GitOps from `yaya-ops`, image in the self-hosted Zot
registry (`registry.ykhi.xyz/agrelha`). Reached at `https://agrelha.ykhi.xyz`
(WireGuard-only, behind Zitadel OIDC).

**Current version: `0.8.0`** (P1 in `0.7.0`; `0.7.1` History; `0.7.2` presence;
`0.7.3` mod `.cfg` editing; `0.7.4` "Update now" + backup tiles — **P2 complete**;
`0.8.0` modpack export).
Build+push `0.8.0` to ship it.

> Modpack export (`0.8.0`). `GET /mods/export` streams a `valheim-YYYY-MM-DD.r2z`
> (a zip) built from the live `valheim-mods` ConfigMap + the `valheim-mod-configs`
> `.cfg` keys. Layout is r2modman/Thunderstore-Mod-Manager's own: `export.r2x`
> (`profileName` + `mods[]` of `name: namespace-name`, `version:{major,minor,patch}`,
> `enabled: true`) plus each config under `config/<file>.cfg`. Friends import via
> r2modman → Import profile → From file. No comments, no external calls.
> Code: `internal/modpack/modpack.go`, `internal/server/handlers_modpack.go`. The
> route is registered before `/mods/:namespace/:name` so the static path wins.

> README styling: this Tailwind v4 build has no Typography plugin, so the `prose`
> classes were dead. README now uses a `.md` scope with hand-written markdown CSS
> in `cmd/web/assets/css/input.css` (rebuild `output.css` via `task tailwind:build`
> / the CLI when it changes).
>
> README images: the grilled "CDN-only images" rule stripped every body image
> (mods host them on GitHub/imgur, not gcdn — only the package icon is on gcdn),
> so READMEs showed bare alt text. `internal/mdrender` now allows any `https://`
> image src (still blocks `http`/`data:`/`javascript:`).
>
> Image proxy (0.6.3): the browser never contacts external image hosts.
> `mdrender` rewrites every sanitized `https://` `<img src>` to `/img?u=<escaped>`
> (post-sanitize HTML pass via `x/net/html`); `GET /img` (auth-gated) fetches the
> image server-side and re-serves it. Guards: https-only (+ on redirects), host
> must resolve to a public IP (blocks SSRF to loopback/private/link-local), 15s
> timeout, 12 MB cap, `Content-Type` must be `image/*`, 7-day cache. The package
> icons on the list/detail pages are gcdn direct (not proxied) — only README-body
> images go through the proxy.

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

### P1 — correctness / it-bugs-me — DONE (0.7.0)

- [x] **Session persistence.** Sessions are now stateless HMAC-SHA256-signed cookies
      (`internal/auth`): payload `{email, idToken, exp}` signed with a key derived
      `sha256("agrelha-session-v1:"+OIDCClientSecret)` (stable across restarts, no new
      secret to seed, no server-side store). Survives deploys; no re-login. Verified
      tamper/garbage/wrong-key all rejected.
- [x] **OIDC `state` + `nonce` CSRF.** `Login` generates random `state`+`nonce`, stores
      them in a short-lived signed `agrelha_oidc` cookie, passes `nonce` via
      `oidc.Nonce`. `Callback` constant-time-compares `state` and the ID token's `nonce`
      claim, then clears the cookie.
- [x] **Session eviction.** Moot — stateless cookies, nothing to evict (`exp` in payload).
- [x] **Inline error/success feedback.** Flash cookie (`internal/server/flash.go`):
      install/remove/grant/revoke set an ok/err message + redirect; the Mods/Admins pages
      render a banner (`flashBanner`). e.g. "Installed X (+3 dependencies) — committed;
      the server will restart to apply." / "Couldn't resolve …". No more raw Fiber error
      pages for these actions.

### P2 — features from the original design not yet built

- [x] **Mod browsing + metadata cache.** Done in 0.6.0 (see Done above); spec kept
      below for reference.
- [x] **Audit / event timeline UI (0.7.1).** `GET /history` — merged timeline of `audit`
      (who did what) + `events` (joins/leaves/backups/crashes), newest first, 200 rows.
      `store.ListHistory` UNIONs both and excludes `restart/stop/start` events (they'd
      duplicate the audit rows). Labels/badges via `pages.HistoryLabel`/`HistoryBadge`.
      Nav gained a History link.
- [x] **Mod config (.cfg) editing (0.7.3).** `/configs` lists the `.cfg` keys in the
      `valheim-mod-configs` ConfigMap; edit/new/delete each commit via new
      `gitops.Committer.SetData`/`DeleteData` (upsert-or-create a data key, literal block
      scalar for multiline, into an empty `data: {}` too) → `applyAfterSync` restarts so
      the mod-reconciler re-copies the file. Textarea editor, CRLF→LF normalized, filename
      validated `^[A-Za-z0-9][A-Za-z0-9._-]*\.cfg$`, flash feedback, nav link. New config
      env `MOD_CONFIGS_PATH`.
- [x] **Player presence (0.7.2).** `players.online`/`online_since` columns (idempotent
      ALTER migration). Ingester `SetOnline` on connect/disconnect; `ClearPresence` on
      startup (live state unknown across restarts — rebuilds from the 200-line tail, so a
      player connected >200 log-lines ago shows offline until they reconnect). Dashboard
      **Players tile now reads `store.CountOnline`** (replaces the broken A2S `status.json`
      that always `TimeoutError`'d). Admin roster sorts online-first with a green dot.
- [x] **"Update now" action (0.7.4).** `POST /server/update` = restart recorded as an
      `update` action/event (the lloesche image installs any Valheim update on boot).
      Dashboard "Update" button (sky, with a tooltip that it restarts).
- [x] **Last-backup / backup-size tiles (0.7.4).** agrelha NFS-mounts the NAS
      `valheim-backups` export **read-only** at `/backups` (direct `nfs:` volume, not a
      PVC — avoids cross-ns + RWO-accessMode). `internal/backups.Stat` → newest mtime,
      count, total + latest size. Dashboard shows "Last backup: 3h ago · N backups · X
      total · latest Y". Cached 60s + 3s-timeout-bounded (`s.backupInfo`) so a hung NAS
      can't block the SSE tick. (Live world-save size not shown — it's node-local RWO on
      game-01, unreadable without exec; the latest backup size is the proxy.)
      **Resilience caveat:** a raw `nfs:` volume mount blocks pod start, so if the NAS is
      down agrelha won't start. If that matters, switch to a decoupled collector
      (separate deployment writing a summary agrelha reads) or drop `BACKUPS_DIR`.

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
- [x] **Players tile fixed (0.7.2).** No longer depends on `status.json` A2S (which still
      `TimeoutError`s) — the tile now shows `store.CountOnline` from the log ingester.
      (`VALHEIM_STATUS_URL`/`valheim.FetchStatus` are now unused; drop later if desired.)
- [ ] **InfluxDB sparklines (optional).** CPU/Mem are instantaneous from metrics-server;
      historical mini-charts would need an InfluxDB read token.

---

## P4 — zero-downtime HA (rqlite)

Goal: run **2+ replicas with `RollingUpdate` so deploys have zero downtime** (wanted as
a technical feature, not out of necessity — it's a single-user tool, so this is a
deliberate "do it properly" project, not a fix for a real availability problem).

### Why it doesn't work today

- State is **embedded SQLite on a RWO, node-local `local-path` PVC** (`agrelha-data`).
  Two replicas can't share it: a replica on another node can't mount the RWO/local-path
  PV; two on the same node would be **two SQLite writers → `SQLITE_BUSY`/corruption**.
  That's exactly why the Deployment is `strategy: Recreate` (kill-then-start = a few
  seconds of downtime).
- The **log ingester is a singleton** by nature. Even with perfect HA storage, running it
  in two pods = two log tails = **duplicate join/leave events**. Same for the Thunderstore
  warm loop and `applyAfterSync`. HA storage does NOT solve this — it needs a single
  owner.
- Sessions are already **stateless signed cookies** (P1), so auth already survives
  multiple replicas — that part's done.

### Decision: rqlite (SQLite + Raft), not the alternatives

Evaluated the "distributed SQLite" field against our constraints (pure-Go /
`CGO_ENABLED=0`, Talos = no easy FUSE, tiny relational schema):

- **rqlite** ✅ — self-contained Raft in one binary, normal container (no FUSE/CGO),
  **pure-Go `gorqlite` client**, and it *is* SQLite so the existing SQL mostly ports
  as-is. Keeps the "still SQLite" ethos. **Chosen.**
- **Litestream** ❌ — backup/DR streaming only, single-writer, no HA.
- **LiteFS** ⚠️ — most SQLite-native (keep embedded SQLite) but needs **FUSE** →
  fragile on Talos, and semi-abandoned.
- **dqlite** ❌ — needs CGO + libdqlite; breaks the pure-Go build.
- **libSQL/Turso `sqld`** ⚠️ — embedded replicas need CGO; remote pure-Go mode works but
  self-hosted HA is immature.
- **Marmot** ❌ — eventually-consistent multi-master; wrong semantics.
- **SurrealDB** ❌ — overkill: HA needs a TiKV/FoundationDB cluster *underneath* it, a
  full SurrealQL rewrite, and a multi-model graph DB for ~6 tiny relational tables. Only
  worth it as a platform decision / if we specifically wanted its live-queries.

### Architecture: split web / worker (preferred over in-process leader election)

Rather than one Deployment doing leader-election, split responsibilities:

- **`agrelha-web` Deployment** — 2+ replicas, `RollingUpdate` with `maxUnavailable: 0`,
  fully stateless (talks to rqlite, sessions are cookies). This is what gives
  zero-downtime deploys and survives a node/pod loss.
- **`agrelha-worker` Deployment** — 1 replica; owns the **singleton** work: log ingester,
  Thunderstore warm loop, `applyAfterSync`. Its ~seconds restart is harmless (presence is
  rebuilt from the log tail anyway; index reloads from rqlite). No leader-election code —
  the "singleton" is just a 1-replica Deployment.
- Both talk to the same **`rqlite` StatefulSet** (3 nodes, Raft, one PVC each).

(Alternative if we ever want a single Deployment: client-go `coordination.k8s.io` Lease
leader-election gating the singleton goroutines, + RBAC for leases. More code; rejected
for now in favor of the split.)

### Work breakdown

1. **rqlite StatefulSet** manifests (3 replicas, headless Service, per-pod PVC, join via
   the headless DNS) in yaya-ops; ArgoCD app. Right-size for the small VMs.
2. **Port `internal/store`** from modernc `database/sql` → `gorqlite` (pure-Go). SQL is
   ~all compatible (it's SQLite). Rework the spots that assume a local file / interactive
   transactions:
   - `SaveModIndex` uses an interactive `BEGIN`+prepared-stmt loop → rqlite wants a single
     **batched write request** (queued statements), not an interactive txn. Rewrite as one
     batch (DELETE + parameterised INSERTs + meta upsert).
   - Drop the WAL/`foreign_keys` pragmas (N/A to rqlite).
   - Timestamps: keep the `asTime` tolerance (rqlite returns strings) — already robust.
   - Idempotent `ALTER TABLE` migration still works (run once at startup against rqlite).
3. **Split the binary's roles**: a `ROLE=web|worker` (or two entrypoints). `web` skips
   `ingest.Run`/`ts.WarmLoop`/`applyAfterSync`; `worker` runs them and serves nothing (or
   just `/healthz`). Config flag + wire in `server.New`.
4. **Manifests**: `agrelha-web` (2 replicas, RollingUpdate) + `agrelha-worker` (1 replica)
   Deployments; drop the `agrelha-data` PVC and the SQLite volume; keep the backups NFS
   mount on whichever role surfaces the tile (web). HTTPRoute → web Service.
5. **Config**: `RQLITE_URL` (e.g. `http://rqlite.agrelha.svc:4001`); remove `DB_PATH`.
6. Verify: kill a web pod under load → no blip; rolling deploy → zero 5xx; only one
   ingester writing (no duplicate events in History).

**Caveat to keep in view:** this is a 3-node Raft cluster + a role split to erase a
~3-second deploy blip on a single-user tool. Justified only as a deliberate technical
exercise (which is the stated intent).

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
internal/mdrender        goldmark + bluemonday README sanitizer; rewrites imgs to /img proxy
internal/server          also hosts GET /img (auth-gated SSRF-guarded image proxy)
internal/sse             Datastar v1.0 SSE frame writers
```
