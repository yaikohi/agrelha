# Composition Root (`internal/wiring`)

The wiring package is the dependency injection and assembly root for the application.

## Architectural Role
- **Assembly**: Constructs all concrete infrastructure adapters, binds them to their respective ports, and passes them into application managers.
- **Pure Wiring**: Performs no business logic and no direct HTTP handling.
- **Server Composition (`BuildServer`)**: Assembles sub-handlers, wires background reconciliation callbacks (`applyAfterSync`, `applyMinecraftAfterSync`), starts background schedulers, and returns the runnable `*fiber.App`. (Dissolves the former `internal/server` package into the composition root).
- **Testable Startup**: Extracted from `cmd/agrelha` so assembly decisions can be validated in unit tests without executing `main()`.

## Key Entry Points
- `wiring.go`: `Build(ctx, cfg) (Deps, error)` — builds database, cache, K8s, Docker, GitOps, and manager instances.
- `server.go`: `BuildServer(ctx, cfg, deps) *fiber.App` — builds all HTTP handlers and returns the configured Fiber app via `web.New`.

## Import Rules
- May import all layers (`domain`, `ports`, `app`, `infra`, `platform`, `web`).
- Nothing in `internal/domain`, `internal/ports`, `internal/app`, `internal/infra`, or `internal/web` may ever import `internal/wiring`.
