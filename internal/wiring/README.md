# Composition Root (`internal/wiring`)

The wiring package is the dependency injection and assembly root for the application.

## Architectural Role
- **Assembly**: Constructs all concrete infrastructure adapters, binds them to their respective ports, and passes them into application managers.
- **Pure Wiring**: Performs no business logic and no direct HTTP handling.
- **Testable Startup**: Extracted from `cmd/agrelha` so assembly decisions can be validated in unit tests without executing `main()`.

## Import Rules
- May import all layers (`domain`, `ports`, `app`, `infra`, `platform`).
- Nothing in `internal/domain`, `internal/ports`, `internal/app`, or `internal/infra` may ever import `internal/wiring`.
