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

**No current node can run Docker.** All four nodes run Talos, an immutable
API-driven OS with no Docker and no shell. A Docker Host is necessarily a
different machine with a conventional OS.

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

| Host | Runtime | StateStore | Reconciler |
|---|---|---|---|
| `cluster` | k8s (client-go) | git | ArgoCD (async) |
| a Docker host | docker | local filesystem | compose (sync) |

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

Sharing the mod script means a fix made for the Docker host lands on running
cluster worlds. Extracting and shellchecking 141 lines of incrementally-grown
shell will likely surface real findings; those changes need testing against the
cluster, not only the box.

A Docker Host has no `JobRunner`, so its worlds have no agrelha-driven backups
until one is written.

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

**Making the Docker host a cluster node.** Impossible without re-provisioning off
Talos, and pointless regardless: a worker whose control plane is on the nas
provides no availability when the nas is off.

**A VM or container on the nas Proxmox host.** Fails the single requirement — the
nas is the thing that powers off.

**Deriving Host from the game** ("all Valheim is on the box"). Wrong the moment
one Valheim world stays in the cluster, which is exactly the pilot arrangement
chosen.
