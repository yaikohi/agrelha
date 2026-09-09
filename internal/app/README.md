# Application Services Layer (`internal/app`)

The application layer contains the use cases of the system. It orchestrates business workflows by coordinating domain entities and ports.

## Architectural Role
- **Use Case Orchestration**: Implements complete application workflows (e.g. creating an instance, verifying memory budgets, managing whitelists/ops, scheduling daily backups, resolving mod compatibility).
- **Core, Not Infrastructure**: This is part of the application core alongside `internal/domain`. It houses business processes, not technical I/O.
- **Delivery Independence**: Has zero awareness of HTTP, Fiber, request parsing, or HTML templates.

## Import Rules
- **May import `internal/domain` and `internal/ports` only.**
- Must **never** import `internal/infra` (plumbing/adapters).
- Must **never** import `internal/web` (HTTP/presentation).
- Must **never** import `internal/platform/config` (takes narrow functional options or ports instead).

## Subpackages
- `instances/`: `InstanceManager` — manages instance lifecycle, tier budgets, IP assignments, and command execution.
- `access/`: `AccessManager` (Minecraft whitelist and ops persistence + live console synchronization).
- `admins/`: Valheim operators management and audit logging.
- `mods/`: Valheim mod collection management.
- `games/`: Concrete game engine implementations (`valheim`, `minecraft`) fulfilling `ports.Game`.
- `backups/`: `BackupScheduler` — daily world snapshot passes and retention pruning.
- `content/`: Mod compatibility filtering and dependency analysis.
- `modpack/`: Modrinth / MRPack bundle creation.
- `ingest/`: Presence background ingestion.
