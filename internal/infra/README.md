# Infrastructure Layer (`internal/infra`)

The infrastructure layer contains driven (outbound / secondary) adapters. It provides concrete technical implementations of the seams declared in `internal/ports`.

## Architectural Role
- **Technical Adapters**: Translates domain/port invocations into vendor-specific API calls, database queries, file operations, or protocol packets.
- **Driven / Outbound**: Only called by application services or composition root via ports. Never initiates application workflows itself.

## Import Rules
- **May import `internal/domain` and `internal/ports`.**
- Must **never** import `internal/app` (avoids cyclic dependencies and logic leakage).
- Must **never** import `internal/web` (presentation concerns).
- Internal adapters should not import each other, except self-contained helper packages (like `runtime/k8s` -> `kube`).

## Subpackages
- `store/`: SQLite storage adapter for instances, players, presence, and audit history.
- `kube/`: Kubernetes client-go adapter (deployments, pods, logs, exec, services, PVCs).
- `runtime/`:
  - `k8s/`: Kubernetes workload lifecycle implementing `ports.Runtime`.
  - `docker/`: Docker Engine API unix socket adapter implementing `ports.Runtime`.
- `gitops/`: Git committer and repository operations.
- `state/`: Declarative state store adapters (`git`, `local`, `unconfigured`).
- `reconcile/`: Declarative reconciler adapters (`argocd`, `compose`).
- `content/`: External mod registries (`modrinth`, `thunderstore`, `modpackindex`, `mcversions`).
- `auth/`: Authentication providers (`oidc`, `local`).
- `rcon/`: Minecraft RCON client and connection pool.
- `backups/`: Local filesystem and NFS backup file utilities and pruning.
- `manifests/`: Kubernetes YAML templates for instances.
