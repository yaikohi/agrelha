# One Deployment per Instance, not shared-deployment world slots

Minecraft servers were first designed as a single Deployment hosting several
"world slots", where the active world was selected by mounting `<slot>/data` from
one shared PVC via `subPathExpr`. That approach is unusable on this cluster
(k8s v1.36 + local-path): reproduced in isolation, `$(WORLD_SLOT)` expands
correctly and `ducktopia/data` exists owned `1000:1000`, yet the `subPathExpr`
mount resolves to a **root-owned directory that is not on the PVC at all**, so the
server cannot write `eula.txt` no matter how the directory is chowned. Two
init-container designs (including a root `chown`) were tried and discarded before
the mount itself was identified as the problem.

We therefore give each Instance its own Deployment, PVC, Service and config
ConfigMap, and the concept of a "slot" is retired in favour of **Instance**.

## Considered options

- **Shared Deployment + `subPathExpr` per slot** — rejected: broken as described above.
- **Shared Deployment + itzg `LEVEL`** (world at `/data/<slot>`, one PVC) — this
  works and was briefly adopted; rejected because mods and configs live in the
  same `/data` and would be shared across worlds, so switching packs rewrites the
  other world's mods. It survives as the mechanism *within* an Instance: `LEVEL`
  still names the World directory.
- **One Deployment per Instance** — chosen. Full isolation of world, mods and
  configs, and per-instance start/stop maps directly onto the RAM budget.

## Consequences

- Concurrency is bounded by RAM, not by the design: `MC_MAX_RUNNING` and per-Tier
  memory decide how many Instances run at once.
- Do not reintroduce `subPath`/`subPathExpr` for `/data` on this cluster.
- The retired `Slot` model still holds shared types (`Source`, `Loader`,
  `Provider`, `Pack`) that `Instance` consumes, and duplicates the itzg `TYPE`
  derivation. Removing `slot.go` means moving those types first.
