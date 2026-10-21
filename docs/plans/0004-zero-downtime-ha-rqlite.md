# Plan: zero-downtime HA for agrelha (rqlite)

Extracted verbatim from the old `plan.md` on 2026-10-18, which was otherwise
replaced. Unbuilt: there is no rqlite anywhere in the code or `go.mod`.

Status: **designed, not built.** A deliberate "do it properly" project rather
than a fix for a real availability problem.

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
2. **Port `internal/adapters/store`** from modernc `database/sql` → `gorqlite` (pure-Go). SQL is
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
