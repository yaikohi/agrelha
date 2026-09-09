# Hexagonal Layering and Dissolution of the Monolith Server

We maintain a strict separation between domain core (`domain`, `ports`, `app`), driving adapters (`web`), and driven adapters (`infra`). `internal/server` is transitional and will be dissolved.

## Context

During architecture review, the question arose whether `internal/app` and `internal/web` should be merged under `internal/infra`, because both live outside pure domain entities (`internal/domain`). Furthermore, legacy handlers in `internal/server` grew complex because business orchestration was mixed directly into HTTP handlers.

## Decision

1. **Keep `domain`, `app`, `ports`, `infra`, and `web` as distinct top-level directories under `internal/`:**
   - **`domain`** holds pure business entities, value objects, and domain invariants. It imports nothing.
   - **`ports`** holds consumer-side inverted interfaces. It imports `domain` only.
   - **`app`** holds application use-case orchestrators (e.g., `InstanceManager`, `BackupScheduler`). It imports `domain` and `ports` only. It never imports `infra` or `web`.
   - **`infra`** holds driven (outbound) technical adapters (SQLite, Kubernetes, Docker, Modrinth, RCON). It imports `domain` and `ports`. It never imports `app` or `web`.
   - **`web`** holds driving (inbound) presentation adapters (Fiber routing, thin handlers, templ components, Datastar SSE). It imports `domain`, `ports`, and `app`. It never imports `infra`.

2. **Presentation (`web`) is separated from technical infrastructure (`infra`):**
   In Hexagonal and Onion Architecture, both driving (inbound) and driven (outbound) adapters exist on the outer ring, but their dependency directions are opposite: `web` drives `app`, whereas `app` drives `ports` implemented by `infra`. Keeping them as separate packages prevents circular imports and preserves clear architecture rules in `internal/arch/arch_test.go`.

3. **Orchestration belongs in `app`, delivery in `web`:**
   HTTP handlers in `web/handlers` must be thin delivery shims (unpacking HTTP requests, delegating to `app`, rendering responses). Business logic, loops, multi-port coordination, and lifecycle workflows belong entirely in `app`.

4. **Dissolve `internal/server`:**
   `internal/server` was a transitional monolith. Its remaining daunting handlers will be decomposed into `app` services and `web` handlers, its route registration moved to `web/routes.go`, and its composition root moved to `internal/wiring`. `internal/server` will be completely removed.

## Consequences

- The architecture test (`internal/arch/arch_test.go`) enforces clean acyclic layer boundaries with zero exceptions once `internal/server` is eliminated.
- Handlers remain tiny, testable, and presentation-focused.
- Application services can be tested with hermetic unit fakes without Fiber or HTTP overhead.
