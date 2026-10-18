# agrelha — plan & status

Multi-game server control panel (Go + Fiber + templ + Tailwind + Datastar) for
**Valheim**, **Minecraft Modded** and **Garry's Mod**. Two planes: desired state
committed to git, convergence by ArgoCD; lifecycle and telemetry direct against
the runtime.

**Live:** `registry.ykhi.xyz/agrelha:0.32.4` at `https://agrelha.ykhi.xyz`
(WireGuard-only, Zitadel OIDC with per-world roles).

This file is an index of what is left. The detail lives in the documents it
points at; duplicating it here would create a second thing to keep true.

## Where the package boundaries are documented

`docs/architecture.md` is authoritative and **ratchet-enforced** — a package with
no row there fails `internal/arch`. Do not keep a second layout map in prose; the
one that used to live here had drifted to describe directories that no longer
exist.

## The active project: per-instance Host

agrelha moves off the cluster onto `fogo` — `game-01` repurposed, Talos removed,
NixOS installed — so that a Valheim world and the tool managing it both survive
the nas being powered down overnight.

| | |
|---|---|
| Decision and rejected alternatives | `docs/adr/0007-per-instance-host.md` |
| Build order, prerequisites, verification | `docs/plans/0003-per-instance-host-and-agrelha-outside-the-cluster.md` |
| Cluster-side effects | `yaya-ops/docs/agrelha-host-migration-cluster-impact.md` |
| Running against a cluster agrelha is not in | `docs/running-against-an-external-cluster.md` |

**The critical path is code, not hardware.** `fogo` *is* `game-01`, so installing
NixOS kills the cluster's only gameserver node. All three games must render to
Compose before the wipe; there is no intermediate state and no pilot.

### Done

Slice 1 of plan 0003, except step 5 which is deferred and recorded as such:
`Host` on `domain.Instance` with a `host` column defaulting to `cluster`;
per-instance resolution of the `(Runtime, Renderer, Reconciler)` triple via
`infra/hostrouter`; out-of-cluster `KUBECONFIG` support; Host-unreachable
degradation end to end; the mod-reconciler script extracted to one file.

Beyond the plan: `infra/manifests/compose` (a `ports.SpecRenderer` that completes
GMod), and a cross-game ratchet asserting every path a game puts in its env is
backed by a declared volume.

### Left to do, in dependency order

| # | What | Size |
|---|---|---|
| 1 | `RenderCompose` takes a data root instead of `./` | small, specified |
| 2 | Reconciler: `-p <deployment-name>` and the `<game>/instance-NN` path | small, specified |
| 3 | **Valheim Compose shape** — the one-shot `mod-reconcile` service | the big one |
| 4 | Minecraft `/custom-configs` decision, then its spec is complete | one decision |
| 5 | Replace the `Capabilities.Mods` guard once 3 and 4 land | small |
| 6 | Wire the three routers; call `WriteAndConverge` | mechanical |
| 7 | Absorb `config-merge` and `config-publish` into agrelha | see ADR 0007 |

**1 must land before 3.** The data root changes volume paths *inside* every
rendered Compose file, so building Valheim's shape on the current `./` convention
means reworking it.

Outside this repo: prerequisite P3 (Cloudflare token) and P5 (evacuate
`game-01`), the `fogo` NixOS repo, the cutover, and decommissioning — three files
in `tal`, nine in `yaya-ops`. All listed in plan 0003.

### Open decisions

**Does Compose Minecraft support config overrides?** Its `RuntimeSpec` declares
`/config-mods`, which its environment objectively requires, but not
`/custom-configs`, which nothing in env references. Adding it is a feature
decision. Left out, overrides silently stop working on `fogo`.

## Other plans

| | |
|---|---|
| `docs/plans/0001-…` | **Superseded** by 0002 |
| `docs/plans/0002-…` | Game registry, per-instance resources, Garry's Mod — **done** |
| `docs/plans/0003-…` | Per-instance Host — **in progress**, above |
| `docs/plans/0004-zero-downtime-ha-rqlite.md` | Designed, **not built**. No rqlite in the code |

## Gotchas we already hit (don't re-debug these)

- **Every `*.ykhi.xyz` host** A-records to **`192.168.20.220`** (Traefik LB),
  DNS-only. Pointing at a public IP → Traefik serves its default self-signed cert.
  *This changes for `agrelha.ykhi.xyz` at the cutover: it repoints to `fogo`, which
  terminates TLS itself via Cloudflare DNS-01.*
- **Zot** rejects Docker schema-2 manifests with 415 unless `"compat":["docker2s2"]`
  is set (it is).
- **Datastar:** `data-on-load` does not fire — use `data-effect="@get('/sse')"`.
  SSE frames are hand-written (`internal/sse`) because the datastar-go SDK is
  net/http and Fiber is fasthttp.
- **Thunderstore** `v1/package/` is ~162 MB — never fetch it in a request.
- **htpasswd** for Zot is derived in tofu from `zot_password`; one source of truth.
- **ExternalSecret ↔ OpenBao drift:** a new property must be seeded in tal
  `apps-secrets.tf` and applied, or the pod sits in `CreateContainerConfigError`.
  An *empty* value is fine; a *missing* one fails the whole Secret.
- **A ConfigMap change does not restart the pod.** `envFrom` captures the
  environment at container start. `kubectl -n agrelha rollout restart deploy/agrelha`.
- **templ silently skips files it cannot parse and exits 0.** After adding a
  `.templ`, check the `_templ.go` exists — a parse error looks like success.

## Deploy & ops runbook

```sh
# build + push a new image
cd ~/projects/agrelha
task build:image TAG=<next>        # -> registry.ykhi.xyz/agrelha:<next>

# point the deployment at it
cd ~/projects/yaya-ops
#   edit manifests/agrelha-app.yaml image tag -> <next>
kubectl -n agrelha rollout status deploy/agrelha
```

- **Dev loop:** `cp .env.example .env` (leave `OIDC_ISSUER` empty to bypass auth),
  `go mod tidy` once, `task watch`. Kubernetes, metrics and mods degrade
  gracefully off-cluster.
- **Secrets** live in tal `02-platform-config` (`vault_kv_secret_v2 "agrelha"`):
  `codeberg-username/token`, `oidc-client-id/secret`, `zitadel-service-key`,
  `curseforge-api-key`; the Steam key is at `apps/gmod`. Registry pull creds reuse
  `var.zot_*`. `tofu apply` after changing any of them.
- **Zitadel app** needs redirect URI `https://agrelha.ykhi.xyz/auth/callback`,
  post-logout `https://agrelha.ykhi.xyz/login`, and — the one that actually
  matters — **"User roles inside ID token"** enabled. Without it every sign-in
  carries zero roles and nobody is authorized.
- `ALLOWED_EMAIL` **no longer exists.** It was removed in 0.30.x when per-world
  roles landed; rolling back past that needs both the image and the ConfigMap key.
