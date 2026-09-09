# Domain Layer (`internal/domain`)

The domain layer is the innermost core of the system. It models pure business concepts, domain entities, value objects, and domain invariants.

## Architectural Role
- **Purity**: Zero knowledge of I/O, storage, networking, serialization, or third-party frameworks.
- **Invariants**: Contains only business truth and structural validation (e.g., RAM budget rules, slugification, game admission models, backup naming conventions).

## Import Rules
- **Imports NOTHING** outside the Go standard library (e.g., `fmt`, `strings`, `time`).
- Must **never** import `ports`, `app`, `infra`, `web`, `platform`, or external packages.

## Key Types
- `instance.go`: `Instance`, `Tier`, `Lifecycle`, `Availability`, `Loader`, `Source`, `BackupFile`, `FormatBackupFileName`.
- `game.go`: `GameID`, `RuntimeSpec`, `AdmissionModel`, `Bundle`.
- `mod.go`: `ModProject`, `ModVersion`, `ModVersionFile`, `VersionDependency`.
- `access.go`: `Admission`, `Operator`, `Player`.
- `budget.go`: Capacity rules and RAM allocations per tier.
- `history.go`: `HistoryEntry`.
