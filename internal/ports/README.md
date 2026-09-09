# Ports Layer (`internal/ports`)

The ports layer defines the consumer-side seams (interfaces) between the application core and external technology.

## Architectural Role
- **Dependency Inversion**: Declares what the application core requires from the outside world without specifying how it is implemented.
- **Driven (Outbound) Contracts**: Seams for persistence, container runtimes, declarative state stores, external content providers, and reconcilers.

## Import Rules
- **Imports `internal/domain` only.**
- Must **never** import `app`, `infra`, `web`, `platform`, `wiring`, or third-party adapters.

## Key Interfaces
- `runtime.go`: `Runtime` (`Start`, `Stop`, `Restart`, `Status`, `Metrics`, `Logs`, `WatchAvailability`), `ServerRef`, `Status`, `Metrics`.
- `state.go`: `StateStore` (declarative git/local file read/write).
- `reconciler.go`: `Reconciler` (`Converge`, `Status`).
- `repository.go`: `InstanceRepository`, `AuditRecorder`, `EventRecorder`, `HistoryReader`, `PlayerReader`.
- `game.go`: `Game`, `ModResolver`, `Console`.
- `auth.go`: `Auth` (authentication and authorization middleware/methods).
