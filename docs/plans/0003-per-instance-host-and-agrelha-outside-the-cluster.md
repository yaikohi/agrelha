# Plan: per-instance Host, and agrelha outside the cluster

Agreed 2026-10-17. Decision and rejected alternatives are in
`docs/adr/0007-per-instance-host.md`; vocabulary (**Host**, the rewritten
**Instance**) is in `CONTEXT.md`. This file is the build order only.

## Context

The nas is powered down overnight. The cluster is three VMs on it plus one Talos
bare-metal node, agrelha runs on `w-02`, and the Valheim LoadBalancer addresses
are announced by Cilium from cluster nodes. So an overnight shutdown takes the
world, the way in, and the tool to fix it.

WireGuard is not affected — it terminates on the UniFi gateway, and there is no
in-cluster DNS clients depend on. A machine with its own address on VLAN 20 is
reachable over the existing tunnel with the cluster out of the path entirely.

Most of the Docker support is already written and tested — `infra/runtime/docker`,
`infra/reconcile/compose` (including `RenderCompose`), and a `ports.Reconciler`
abstraction over sync-vs-async convergence. (`infra/state/local` also exists but
is **not** used by this plan: see ADR 0007 on why git is the store for every
Host.) The gap is that
`buildRuntime` and `buildStateStore` resolve **once at startup** from
`cfg.Runtime`, so one process serves one Host.

## The model

A **Host** selects a triple, and an Instance declares its Host.

| Host | Runtime | Renderer | Reconciler |
|---|---|---|---|
| `cluster` | k8s | Kubernetes YAML | ArgoCD (async) |
| `fogo` | docker | compose YAML | compose (sync) |

Declared, never inferred. Observed-vs-declared mismatch is drift, reported the
way tier drift already is.

**Git is the StateStore for every Host** — see ADR 0007. The remote is Codeberg,
which is external to the cluster, so it stays reachable during a nas shutdown
and a local store would have bought nothing. `fogo` additionally keeps a local
materialisation of its rendered files, because `docker compose up` needs them on
disk; that copy is a cache, and git wins on divergence.

Two constraints follow:

- **Compose files must render outside every ArgoCD-watched path.** The per-game
  Applications watch `manifests/<game>` recursively with `prune: true`, so a
  compose file there would be applied as a Kubernetes manifest and fail the
  sync, taking cluster worlds with it. Use a separate tree, e.g.
  `compose/<game>/instance-NN/`.
- **Changing a `fogo` world needs the git remote reachable.** Lifecycle and
  reads do not; commits do.

### Decisions

| | |
|---|---|
| Host storage | `host` column per game table, default `cluster` |
| Desired state | **Git for every Host**; `fogo` also materialises locally for compose |
| Compose path | Outside `manifests/`, so ArgoCD never tries to apply it |
| Host resolution | Declared; drift reported, never auto-adopted |
| agrelha placement | On the Docker host, built natively with Nix |
| Cluster access | Long-lived token for the existing `agrelha` ServiceAccount |
| Host OS | NixOS, in a new repo `fogo` |
| Secrets | sops-nix, seeded from OpenBao at authoring time |
| URL | `agrelha.ykhi.xyz` repointed; Caddy + Cloudflare DNS-01 |
| Scope | Lifecycle plus mods. **No backups on a Docker Host** |
| Mod reconciler | One script file, two packagings |
| Unreachable Host | `Unknown` + banner, lifecycle buttons disabled |

## Prerequisites

Slice 1 needs **none of these** — it is behaviour-preserving, defaults every
Instance to `host=cluster`, and can land before the box exists. These gate the
infrastructure work only, and all but the hardware can be done today.

### P0 — WireGuard independence (settled 2026-10-18)

Confirmed: WireGuard terminates on the router, not the cluster. The whole design
rests on this, so it is recorded rather than assumed.

### P1 — ServiceAccount token and an external kubeconfig

Proves agrelha can authenticate from outside the cluster *and* that its RBAC is
genuinely narrow. Do this first: if it fails, nothing downstream works.

Kubernetes 1.24+ no longer auto-creates ServiceAccount token Secrets, so declare
one. Add to `yaya-ops/manifests/agrelha-rbac.yaml` — it is covered by the
`agrelha-*.yaml` glob, and the manifest holds **no secret material**: the API
server populates `.data.token`, so committing it is safe.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: agrelha-token
  namespace: agrelha
  annotations:
    kubernetes.io/service-account.name: agrelha
type: kubernetes.io/service-account-token
```

Build a kubeconfig from it:

```sh
kubectl -n agrelha get secret agrelha-token -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt
TOKEN=$(kubectl -n agrelha get secret agrelha-token -o jsonpath='{.data.token}' | base64 -d)

KC=agrelha.kubeconfig
kubectl config set-cluster yaya --server=https://192.168.20.11:6443 \
  --certificate-authority=ca.crt --embed-certs=true --kubeconfig=$KC
kubectl config set-credentials agrelha --token="$TOKEN" --kubeconfig=$KC
kubectl config set-context agrelha --cluster=yaya --user=agrelha --kubeconfig=$KC
kubectl config use-context agrelha --kubeconfig=$KC
```

Verify **both directions** — that it can do its job, and that it cannot do
anything else:

```sh
export KUBECONFIG=agrelha.kubeconfig
kubectl -n valheim get pods            # expect: works
kubectl -n minecraft-modded get pods   # expect: works
kubectl -n gmod get pods               # expect: works

kubectl get nodes                      # expect: Forbidden
kubectl -n openbao get secrets         # expect: Forbidden
kubectl auth can-i --list -n valheim   # review the grant surface
```

The negative tests matter as much as the positive ones. This token does not
expire, so an over-broad grant here is a standing risk.

### P2 — A static address outside the Cilium pool (satisfied)

`game-01` already sits at `192.168.20.31`, which is outside the pool. It keeps
that address as `fogo`. The reasoning below is retained for any future Host.

#### Why the constraint exists

The `external` CiliumLoadBalancerIPPool spans **192.168.20.220–192.168.20.250**.
An address for the host inside that range can also be handed to a Service, giving
an intermittent conflict that is painful to diagnose. Pick outside it.

Also pick outside the UniFi DHCP range for VLAN 20, or DHCP may lease it to
something else. Check that range in the UniFi console before choosing.

Known occupants of VLAN 20: nodes at `.11`, `.21`, `.22`, `.31`; LB addresses
from `.220` upward. Something low and clearly reserved — around `.40`–`.60` —
avoids both.

Set it statically in the NixOS config *and* add a UniFi fixed-IP reservation for
the host's MAC. Either alone works; both together mean neither side can hand the
address away.

### P3 — A Cloudflare API token for DNS-01

The host is WireGuard-only and unreachable from the internet, so Let's Encrypt
cannot complete an HTTP-01 challenge. DNS-01 proves control of the domain
instead, which works for a host with no inbound internet exposure.

In the Cloudflare dashboard: **My Profile → API Tokens → Create Token**, starting
from the *Edit zone DNS* template.

- Permissions: `Zone → DNS → Edit`, plus `Zone → Zone → Read` (the Caddy
  Cloudflare module needs the zone lookup)
- Zone Resources: Include → Specific zone → the `ykhi.xyz` zone only
- The token value is shown **once** — capture it straight into sops

Two gotchas:

- **Stock Caddy has no DNS providers compiled in.** It needs the
  `caddy-dns/cloudflare` module — on NixOS via `pkgs.caddy.withPlugins`, or an
  xcaddy build. A plain `pkgs.caddy` will fail at certificate issuance, not at
  startup, so it looks fine until it doesn't.
- `agrelha.ykhi.xyz` will point at a private VLAN 20 address in public DNS. That
  is already the established pattern here (it currently resolves to the Traefik
  LoadBalancer address), so nothing new — but it does mean the record is
  world-readable.

### P4 — Hardware (satisfied)

**`fogo` is `game-01` repurposed**, not a new machine. Decided 2026-10-18:
Talos comes off, NixOS goes on. It is 16 CPU, ~60 GiB RAM, ~475 GB disk and
x86_64 — comfortably more than a box bought for the purpose.

x86_64 matters because Valheim's dedicated server has no ARM build.

### P5 — Evacuate `game-01` before the wipe

**This is the irreversible one.** Every local-path volume on `game-01` is game
data, and wiping Talos destroys all of it. Before touching the node, confirm a
current archive exists on the NFS export for each of the three live worlds and
copy them somewhere that is not `game-01`.

There is no second gameserver node to fall back to, and no rollback short of
re-provisioning Talos through `tal`.

### Not a prerequisite

**Moving worlds or backups to the NAS.** Already done — `valheim-backups` is an
NFS PV on `192.168.10.6:/bigdata/valheim-backups`, producing hourly ~32 MB
archives. The pilot migration restores from one of those, so there is no data
staging to do first.

## Slice 1 — per-instance Host in agrelha

Behaviour-preserving for cluster worlds: every existing Instance defaults to
`cluster` and must render, start and reconcile exactly as today.

| # | What |
|---|---|
| 1 | `host` column on each per-game instance table, default `cluster`; `Host` on `domain.Instance`; repo mappers carry it |
| 2 | Resolve the `(Runtime, Renderer, Reconciler)` triple **per Instance** instead of once in `buildRuntime`. The StateStore stays single and shared |
| 3 | Out-of-cluster kubeconfig support — `infra/kube/client.go:63` is `rest.InClusterConfig()` only; add a `KUBECONFIG` fallback |
| 4 | Host-unreachable degradation: `Unknown` status, banner, lifecycle buttons disabled |
| 5 | Host drift check: declared vs observed, surfaced like tier drift — **deferred**, see below |
| 6 | Extract the mod-reconciler script (141 lines, inline in the Valheim deployment template) to one file; template embeds it at render time |

**Step 5 is deferred.** It was specified for permanent mixed operation. With
`game-01` becoming the single Host, every world ends up on `fogo` and there is
nothing to drift against until one is deliberately moved back. Still correct,
just not yet worth building.

**Step 2 is the one that must not go wrong.** Any Instance whose Host is
`cluster` must resolve the identical triple it gets today, or worlds start being
reconciled by the wrong plane.

Step 2 splits in two. The **Runtime half** is done: `ports.ServerRef` carries a
`Host`, set centrally in `InstanceManager.serverRef` *after* any
`WithServerRefResolver` closure runs so none can forget it, and
`infra/runtime/hostrouter` dispatches on it — unknown Host is a hard error, and
an empty Host routes to the cluster. The **Renderer and Reconciler half** is
next, and is much smaller now that the StateStore is shared.

**Step 6 is done.** The 129-line script now lives in
`internal/infra/manifests/valheim/scripts/mod-reconcile.sh`, embedded and
re-indented into the template at render time. Verified byte-identical against a
pre-change baseline of both the modded and vanilla deployments, and
shellcheck-clean — the predicted findings did not materialise, so no change
reached running cluster worlds. Three tests pin it, including that blank lines
inside the block stay truly empty: padding them with indentation would rewrite
every Valheim manifest.

The script carries no shebang on purpose. The image runs it via `sh -c`, and
adding one would render into the YAML; Nix's `writeShellApplication` supplies
its own.

## Compose world shapes

Agreed 2026-10-18, after comparing what each game's Kubernetes deployment
actually contains.

### Per game

| game | shape on `fogo` |
|---|---|
| GMod | one service. `RuntimeSpec` describes it completely — **done** |
| Minecraft | one service plus a bind-mounted `mods.txt` and configs directory |
| Valheim | one-shot `mod-reconcile`, the main service, and the `build-watch` sidecar |

**Minecraft is structurally closer to GMod than to Valheim.** The itzg image
installs mods itself from `MODRINTH_PROJECTS=@/config-mods/mods.txt`; its only
init container copies config overrides. There is no reconciliation step of our
own to port.

**But its `RuntimeSpec` is incomplete, so it still cannot be rendered.** The
spec declares exactly one volume — `data` at `/data` — while its own
environment points at `/config-mods/mods.txt` and a custom-configs directory
that the spec never mentions. Rendered to compose as it stands, the container
would start, find no mods file, and bring the world up **with no mods and no
error**. A silently modless world is worse than a refusal.

So the `Capabilities.Mods` guard in `infra/manifests/compose` stays as written:
it refuses exactly the two games that are not ready, for two different reasons,
and admits GMod, whose spec is complete. The work item for Minecraft is to make
its `RuntimeSpec` declare the mounts its environment already depends on; the
single-service render is correct the moment it does.

This also suggests a check worth having: a `RuntimeSpec` whose env references a
path no declared volume provides is a bug in any runtime, not only compose.

### Two Valheim containers are absorbed, not ported

`config-merge` and `config-publish` exist only because agrelha cannot reach a
pod's filesystem under Kubernetes. On `fogo` it is the same machine, so:

- **`config-merge`** → agrelha runs its own `bepinex merge` before `compose up`.
  It already has the subcommand. Keeping it as a container would be actively
  harmful: it pulls `registry.ykhi.xyz/agrelha` from **Zot, which lives in the
  cluster**, so a world restart during a nas shutdown could not pull its image —
  reintroducing a cluster dependency into the host whose purpose is surviving
  cluster outages.
- **`config-publish`** → agrelha reads the generated `.cfg` files straight off
  the host directory.

`build-watch` stays a sidecar: it needs steamcmd from the game image, and the
logic already works.

### Paths

```
<COMPOSE_DIR>/<game>/instance-NN/docker-compose.yml   mirrors the git path exactly
<DATA_DIR>/<game>/instance-NN/<volume>/               runtime data, never in git
```

Compose is invoked with **`-p <deployment-name>`**.

Two traps this avoids:

- **Project-name collision.** Compose derives the project name from the
  directory, so `compose/valheim/instance-01` and `compose/gmod/instance-01`
  would both be project `instance-01`. The reconciler runs
  `up -d --remove-orphans`, so converging one world could tear down the other
  game's containers. An explicit `-p` removes the ambiguity.
- **World saves inside a git tree.** `RenderCompose` currently maps volumes as
  `./<name>`, relative to the compose file. With the compose file materialised
  from git, a stray `git clean` or `git checkout` would delete world data. The
  data root must be a separate absolute path.

Mirroring the git path is worth the extra flag: on a box you will debug over SSH
during an outage, "what is on disk is what is in the repo" is a property worth
having, and materialisation stays a copy rather than a transformation.

Follows two small code changes: the reconciler resolving the game/instance path
and passing `-p`, and `RenderCompose` taking a data root instead of `./`.

### Backups

Backups continue to go to the NAS export. Deliberately deferred: somewhere more
flexible can come later, and the NAS is the only machine with the storage.

**The mount must be `soft`, with a short `timeo`/`retrans`.** A hard NFS mount to
a powered-off NAS does not fail, it *hangs* — writes block in uninterruptible
sleep and can wedge the game container rather than merely skipping a backup.
That would defeat the entire point of the host. The rule for the `fogo` config
is: a missing NAS makes backups fail, never makes a world hang.

## Slice 2 — the `fogo` host

`game-01` repurposed: Talos removed, NixOS installed, keeping `192.168.20.31`.
Config lives in a new repo, `fogo`. Not scaffolded yet — the machine is still a
Talos node, so its disk layout and interface names are not yet known. Intended
shape:

| # | What |
|---|---|
| 7 | `fogo` repo: flake, one host, Docker enabled, static VLAN 20 address |
| 8 | agrelha as a Nix-built (`buildGoModule`) systemd service — no registry dependency |
| 9 | `sops-nix`, plus a seeding script reading OpenBao and writing the encrypted file |
| 10 | Caddy terminating TLS for `agrelha.ykhi.xyz` via Cloudflare DNS-01 |
| 11 | Kubeconfig with a long-lived `agrelha` ServiceAccount token (an explicit `kubernetes.io/service-account-token` Secret — 1.24+ no longer auto-creates them) |
| 12 | Mod reconciler packaged with `writeShellApplication`, pinning `curl`/`unzip`/`jq` |

Secrets to carry: `CODEBERG_TOKEN`, `CODEBERG_USERNAME`, `CURSEFORGE_API_KEY`,
`OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `ZITADEL_SERVICE_KEY`, plus the
kubeconfig token and a Cloudflare API token. A Nix-built binary needs **no** Zot
credentials.

The Codeberg token is still required: agrelha on the host keeps committing
manifests for the cluster worlds.

Step 12 removes a real fragility rather than only a cosmetic one — the current
initContainer runs `apk add --no-cache curl unzip jq` on **every pod start**, so
mod reconciliation depends on Alpine's package repos being reachable at restart.

## Slice 3 — the cutover

**There is no pilot.** `game-01` is the only node carrying
`ykhi.xyz/gameserver=true`, so once Talos is wiped the cluster has nowhere to run
any game. All three worlds move at once. Agreed 2026-10-18 on the grounds that
nobody is playing right now, which makes this the cheapest window it will ever
have.

| # | What |
|---|---|
| 13 | Evacuate: confirm a current archive on the NFS export for every world, and copy them somewhere off `game-01` |
| 14 | Wipe Talos from `game-01`, install NixOS, apply the `fogo` config |
| 15 | Restore all three worlds from their archives: Valheim ×2, GMod ×1 |
| 16 | Repoint `agrelha.ykhi.xyz` at the host; confirm OIDC still completes (hostname unchanged, so Zitadel needs no edit) |
| 17 | Decommission the node cluster-side (see below) |

Today's worlds: `valheim-lareira-v2-01` (`192.168.20.224`),
`valheim-boppo-02` (`192.168.20.225`) and `gmod-lareira-01`, on 20Gi, 20Gi and
5Gi node-local PVCs.

**Player-visible change:** the worlds answer on `192.168.20.31` afterwards, not
on `.224`/`.225`. Saved server entries break. Tell people before, not after.

### Decommissioning checklist

`game-01` is referenced in both other repos; leaving the references behind gives
pods a nodeSelector nothing satisfies.

`tal`: `00-infra/talconfig.yaml`, `00-infra/main.tf`,
`01-apps/modules/cilium/main.tf`.

`yaya-ops`: `manifests/agrelha-app.yaml` (`GAME_NODE_SELECTOR`,
`GAME_NODE_NAME`), `manifests/valheim/storage.yaml`,
`manifests/valheim/instance-0{1,2}/deployment.yaml`,
`manifests/gmod/instance-01/deployment.yaml`,
`manifests/observability-node-exporter.yaml`,
`manifests/observability-dashboard-{valheim,nodes}.yaml`,
`charts/alloy/values.yaml`.

Unaffected: Jellyfin, Shoko, Loki and agrelha's own database are all on
`w-01`/`w-02`. Every local-path volume on `game-01` is game data.

## Verification

**Slice 1 — nothing changes for cluster worlds**

1. Full suite green with no altered expectations; the architecture and guard
   ratchets still pass.
2. Render a Valheim and a Minecraft deployment before and after → **byte-identical**.
3. Every existing Instance reads `host=cluster` after migration, with no backfill
   step required.
4. An Instance whose Host is unknown produces a hard error, never a default Host.

**Slice 2 — the host stands on its own**

5. With the nas **off**: `agrelha.ykhi.xyz` resolves, serves valid TLS, and OIDC
   sign-in completes against Zitadel.
6. With the nas off: the Valheim world on the host is joinable over WireGuard,
   and cluster worlds show `Unknown` with the Host-unreachable banner.
7. `nixos-rebuild` from a clean checkout reproduces the host, secrets included.
8. agrelha reaches the cluster with the ServiceAccount kubeconfig and **cannot**
   exceed its three namespace Roles.

**Slice 3 — the cutover**

9. Every world keeps its save data and players reconnect with the same password.
10. Installing a mod on a host world works and resolves the same plugin set the
    cluster produced for the same mod list.
11. All three worlds survive a full nas shutdown, joinable throughout.
12. The cluster is healthy with `game-01` gone: no pod is left Pending on a
    nodeSelector nothing satisfies, and media and observability are untouched.

Note what is **not** verifiable here: there is no cluster world left to A/B
against, and no rollback rehearsal — re-provisioning `game-01` as a Talos node
means going back through `tal`. That is the accepted cost of the big-bang.

## Out of scope

Backups on a Docker Host — `ports.JobRunner` stays Kubernetes-only; use cron and
a tarball on the host until there is a reason to build the adapter. Moving
Minecraft or GMod off the cluster. A second Docker Host (the `host` column is a
string; a `hosts` table can come when one actually exists). Auto-migrating a
world between Hosts from the UI. Replacing OpenBao as the source of truth for the
host's secrets.

## A note on the sops drift risk

OpenBao remains the source of truth and sops is a derived cache, seeded at
authoring time from a workstation. Rotate a value in OpenBao and the host keeps
the stale copy until someone re-seeds. This is the same failure class as
`externalsecret-openbao-drift`, which already has a detector in
`yaya-ops/scripts/check-externalsecret-drift.sh`. A sops equivalent is worth
writing the first time a rotation actually bites, not before.
