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
NixOS kills the cluster's only gameserver node; there is no intermediate state
and no pilot.

**Only two games need to render, not three.** There are no Minecraft worlds —
none in the cluster, no instance directories in git. The cutover moves two
Valheim worlds and one GMod world, and both of those render today.

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

| # | What | State |
|---|---|---|
| 1 | `RenderCompose` takes a data root instead of `./` | **done** |
| 2 | Reconciler: `-p <deployment-name>` and the `<game>/instance-NN` path | **done** |
| 3 | **Valheim Compose shape** — the one-shot `mod-reconcile` service | **done**, except `build-watch` |
| 4 | Minecraft Compose shape, incl. the `/custom-configs` decision | **off the critical path** |
| 5 | The `Capabilities.Mods` guard | **correct as-is** — refusing Minecraft is the right end state |
| 6a | Materialise rendered files to local disk before converging | **done** |
| 6b | Wire the three routers in `wiring.go` | **done** |
| 7 | Absorb `config-merge` and `config-publish` into agrelha | see ADR 0007 |

### What 1–3 actually changed

`ports.ServerRef` carries `ID domain.InstanceID`, set centrally in
`InstanceManager.serverRef` after any resolver closure so none can forget it.
Compose volumes resolve under a data root and **refuse to render without one** —
beside the compose file they would sit inside the tree materialised from git,
where a `git clean` deletes the world. The reconciler resolves
`<workDir>/<game>/instance-NN` and passes `-p <deployment-name>`, so two games'
`instance-01` cannot share a Compose project and tear down each other's
containers under `--remove-orphans`. One shared `InstanceDataDir` helper keeps the
renderer and the reconciler from disagreeing about where volumes point.

Valheim has its own Compose renderer (`infra/manifests/valheim/compose.go`)
rather than using the generic one, built from the Compose structs so
`yaml.Marshal` escapes the inlined shell script correctly.

**Why it needed its own renderer.** Rendering Valheim from `RuntimeSpec` alone
produces a world that starts, reports healthy, and runs **completely vanilla**.
`PRE_SERVER_RUN_HOOK` is what publishes downloaded plugins into
`/opt/valheim/bepinex/BepInEx/plugins`, and it lived only in the Kubernetes
template — `inst.Env(ValheimProfile)` has never carried it. That is the same
silent-failure class as Minecraft's missing `/config-mods` mount, and it points
at a pattern: **`RuntimeSpec` has never been complete, because until now nothing
consumed it.**

The lifecycle hooks and the pinned server image are now single Go definitions
used by both the Kubernetes template and the Compose renderer, so the two Hosts
cannot drift. Both extractions were verified **byte-identical** against a
pre-change baseline of the modded and vanilla deployments.

**6a used a new optional port rather than `WriteAndConverge`.** `ports.Materializer`
is an optional Reconciler capability, type-asserted by the manager: Compose
implements it and writes the rendered files to the directory `Converge` runs in;
ArgoCD deliberately does **not**, because under GitOps the commit *is* the
delivery and writing files locally would create a second source of truth beside
the repo. A test asserts ArgoCD never gains the capability by accident.

This avoids the trap in `WriteAndConverge`, which re-renders from a
`RuntimeSpec`: that would be a second rendering path, bypassing Valheim's
Compose renderer and silently producing a different file from the one committed
to git. The manager now materialises the bytes it already rendered, and a test
pins that materialise happens **before** converge — the other order runs Compose
against an empty directory.

**6b is config-gated by `DOCKER_HOST_NAME`.** Unset — as in the cluster today —
each router holds only a `cluster` route, so rendering, lifecycle and
convergence behave exactly as before. Set it (plus `DATA_DIR`, `COMPOSE_DIR`,
`DOCKER_SOCKET`) and the same routers gain a second Host. The cutover is then a
configuration change rather than a code change.

Routing is applied even when no Docker Host is configured, deliberately: a world
declaring an unknown Host now fails loudly instead of silently rendering
Kubernetes YAML into a Compose path. The one exception is a nil runtime, which
stays nil — that is the supported "no cluster client" dev state, and wrapping it
would turn it into an error on every call.

Minecraft registers **no** Compose renderer, so a Minecraft world on `fogo` is
refused by the router. That is the intended end state while no Minecraft worlds
exist.

**Deferred from 3: the `build-watch` sidecar.** It answers "is a Valheim server
update available". A world runs correctly without it; update detection is simply
absent on `fogo` until it is added.

Outside this repo: prerequisite P3 (Cloudflare token) and P5 (evacuate
`game-01`), the `fogo` NixOS repo, the cutover, and decommissioning — three files
in `tal`, nine in `yaya-ops`. All listed in plan 0003.

### Open decisions

**Does Compose Minecraft support config overrides?** Deferred, and now low
stakes: there are no Minecraft worlds, so nothing regresses either way. The
question becomes real the first time someone wants a Minecraft world on `fogo`.
Until then the generic renderer refuses it with a clear message, which is better
than a world that starts and silently runs modless.

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
