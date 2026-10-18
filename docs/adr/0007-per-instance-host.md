# Per-instance Host: agrelha runs outside the cluster

An Instance declares which **Host** it runs on — the cluster, or a standalone
Docker host. agrelha itself moves off the cluster onto that Docker host, so that
one world and the tool managing it both survive the cluster being powered down.

## Context

The cluster is four Talos nodes, three of them VMs on the `nas` Proxmox host.
The nas is shut down overnight for noise. When it is off there is no control
plane, no agrelha (it runs on `w-02`), and no Cilium to announce the
LoadBalancer addresses that `valheim-*` services sit on.

The driver is availability: a Valheim world that stays up and stays joinable
while the nas is off.

Three facts shaped the design.

**The VPN is not the cluster's.** WireGuard terminates on the UniFi gateway, and
there is no in-cluster DNS that clients depend on. Only the last two hops are the
cluster's: a `CiliumL2AnnouncementPolicy` publishing an address from the
`external` pool, then the pod. A machine with its own address on VLAN 20 is
reachable over the same tunnel with the cluster entirely out of the path.

**No Talos node can run Docker.** All four nodes run Talos, an immutable
API-driven OS with no Docker and no shell. A Docker Host therefore needs a
conventional OS — either a new machine, or an existing node taken out of the
cluster and re-provisioned.

**The Docker Host is `game-01`, repurposed** (decided 2026-10-18). It is the
bare-metal minipc: 16 CPU, ~60 GiB RAM, ~475 GB disk, already x86_64 and already
at `192.168.20.31` — outside the Cilium pool, so it keeps that address. Talos
comes off, NixOS goes on.

**Most of the Docker support already exists, but is a whole-process mode.**
`infra/runtime/docker` implements `ports.Runtime` over the Docker Engine API,
`infra/reconcile/compose` implements `ports.Reconciler` and renders compose YAML
from a `domain.RuntimeSpec`, and `infra/state/local` implements `ports.StateStore`
on the filesystem. But `buildRuntime` and `buildStateStore` each branch on
`cfg.Runtime` **once at startup**, so one agrelha process serves exactly one
Host. Cluster worlds and a Docker world cannot coexist under one agrelha today.

## Decision

**Host becomes a per-Instance attribute**, stored as a `host` column defaulting
to `cluster`. It selects a *triple*, not just a runtime:

| Host | Runtime | Renderer | Reconciler |
|---|---|---|---|
| `cluster` | k8s (client-go) | Kubernetes YAML | ArgoCD (async) |
| a Docker host | docker | compose YAML | compose (sync) |

**The StateStore is not part of the triple. Git is the desired state for every
Host.** An earlier version of this decision gave a Docker Host a local
filesystem StateStore, to keep it independent of the cluster. That reasoning was
wrong: the git remote is Codeberg, which is *external to the cluster*, so a nas
shutdown does not affect it. A local store bought no availability and cost the
Docker Host its history and its disaster recovery.

So one store, one source of truth, for all Hosts. What differs is how desired
state reaches reality: ArgoCD applies it for cluster worlds, while agrelha
applies it itself for Docker worlds. `ports.Reconciler.Async()` already draws
exactly that line.

A Docker Host also keeps a **local materialisation** of its rendered files,
because `docker compose up` needs them on disk. That copy is a cache, not a
source: if it diverges from git, git wins and re-converging fixes it.

**Host is declared, never inferred.** Probing both runtimes to discover where a
world lives has to guess when it finds the world in both places or neither, and
guessing silently is the failure mode plan 0002 was written to remove. Divergence
between declared and observed Host is *drift*: reported in the UI, reusing the
pattern already established for tier drift, never silently adopted.

**agrelha moves onto the Docker host**, built natively with Nix as a systemd
service. It reaches the cluster with a kubeconfig bearing a long-lived token for
the existing `agrelha` ServiceAccount, preserving its current namespace-scoped
RBAC (`agrelha-valheim-ops`, `agrelha-minecraft-ops`, `agrelha-gmod-ops`).

**Scope is lifecycle plus mods.** Backups are deliberately not available on a
Docker Host: `ports.JobRunner` has only a Kubernetes implementation, and on a
standalone host a cron and a tarball is a more honest answer than reimplementing
the port.

**The mod reconciler is one script, two packagings.** The 141 lines currently
inline in the Valheim deployment template move to a single file; the Kubernetes
template embeds it at render time, and the Nix host wraps the same file with
pinned dependencies.

## Consequences

**Rendered compose files must live outside every ArgoCD-watched path.** The
per-game Applications watch `manifests/<game>` recursively with `prune: true`.
A `docker-compose.yml` under there would be applied as a Kubernetes manifest,
failing the sync and taking the *cluster* worlds down with it. Docker-Host
worlds therefore render to a separate tree, e.g. `compose/<game>/instance-NN/`.

**Mutating a Docker-Host world requires the git remote to be reachable.** Reads
and lifecycle — start, stop, restart, logs, status — never touch git. But
installing a mod or resizing a world is a commit. Codeberg being external means
a nas outage does not affect this; a home-internet outage would, leaving agrelha
reachable on the LAN but unable to accept changes. Accepted rather than solved:
a write-behind cache would add a second source of truth to avoid a narrow
window.

`agrelha.ykhi.xyz` repoints to the Docker host, which terminates TLS itself via
Cloudflare DNS-01. The hostname is unchanged, so the Zitadel redirect URI and
`OIDC_REDIRECT_URL` need no edit — but **the Docker host becomes a dependency for
managing cluster worlds too**. That is the price of one UI at one address.

Worlds on an unreachable Host report `Unknown` with a Host-unreachable banner and
disabled lifecycle buttons. This is an Availability answer, not a Lifecycle one,
and the existing vocabulary already separates the two. Hiding them would make a
nas outage look like data loss.

agrelha leaves ArgoCD and gains its own lifecycle on the Nix host. Its secrets can
no longer come from OpenBao at runtime, because OpenBao is in the cluster and down
exactly when the host matters. They are seeded into sops-nix from OpenBao at
*authoring* time — the host never calls OpenBao. OpenBao stays the source of
truth and sops is a derived cache, which introduces a **drift risk** of the same
class already documented for ExternalSecrets.

Sharing the mod script means a fix made for the Docker host would land on
running cluster worlds. In the event this cost nothing: the extracted 129 lines
are shellcheck-clean (verified against a control file, so the empty result is
trustworthy), and the rendered manifests are byte-identical to before. No
behaviour changed, so nothing had to be re-tested against the cluster. The
coupling remains for future edits.

A Docker Host has no `JobRunner`, so its worlds have no agrelha-driven backups
until one is written.

**The cluster loses its only gameserver node.** `game-01` is the sole holder of
`ykhi.xyz/gameserver=true`, so once it leaves, no game can run in the cluster at
all. Every world moves in one cutover — there is no pilot, no live A/B between
Hosts, and no rollback short of re-provisioning Talos through `tal`. Accepted
because nobody is playing right now, which is the cheapest window this will ever
have.

That also shifts what per-instance Host is *for*. With no game world left in the
cluster, mixed operation is not needed in steady state — `RUNTIME=docker` alone
would serve. The Host work keeps its value as the thing that makes the migration
incremental and reversible per world, and as the option to put a world back in
the cluster later, but the original premise of permanent mixed operation did not
survive this decision.

**Player-visible:** worlds answer on `192.168.20.31` afterwards rather than the
`.224`/`.225` LoadBalancer addresses. Saved server entries break.

## Alternatives rejected

**An agent on the Docker host, with agrelha staying in the cluster.** This was the
preferred design for most of the discussion and was rejected on two grounds. The
agent's justification was file placement — a remote Docker API can control
containers but cannot write the `mods.txt` that the reconciler needs, so SSH or
NFS would have been bolted on alongside it. Once agrelha runs *on* the host, there
is no remote host and the problem disappears. Decisively: with agrelha on `w-02`,
a nas outage leaves the world running and **unmanageable**, which fails the
requirement that motivated the work.

**Keeping agrelha in the cluster and exposing the Docker socket over TCP.**
Root-equivalent access to the host over the network, and still no answer for file
placement.

**Keeping the Docker Host in the cluster as a node.** A worker whose control
plane lives on the nas provides no availability when the nas is off, so the node
has to leave the cluster entirely — which is what repurposing `game-01` does.

**Buying a new machine and leaving `game-01` in the cluster.** Rejected on
2026-10-18. It would have preserved a pilot migration and a rollback target, but
`game-01` is better hardware than a purpose-bought box and costs nothing. The
trade is accepted below.

**A VM or container on the nas Proxmox host.** Fails the single requirement — the
nas is the thing that powers off.

**A local filesystem StateStore for the Docker Host.** Agreed initially, then
dropped once it was clear the git remote is external to the cluster and so
unaffected by the outage the Host exists for. It would have split the source of
truth in two, left Docker-Host worlds with no history and no off-box copy of
their desired state, and required Host-aware routing across nineteen call sites
— four of which (the global config operations) have no Instance and therefore no
Host at all. That last point had no answer under the old model.

**Deriving Host from the game** ("all Valheim is on the box"). Wrong the moment
one Valheim world stays in the cluster, which is exactly the pilot arrangement
chosen.
