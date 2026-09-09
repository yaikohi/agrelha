# Web Delivery Layer (`internal/web`)

The web layer contains driving (inbound / primary) presentation adapters. It handles HTTP requests, routes, server-sent events (SSE), and server-side rendered HTML.

## Architectural Role
- **Thin Delivery Shims**: Handlers unpack HTTP requests (URL parameters, query values, form submissions), invoke application services in `internal/app`, and return responses.
- **No Orchestration**: Business logic, loops, multi-port coordination, and lifecycle workflows belong in `internal/app`, not here.
- **Hypermedia**: Uses Datastar SSE protocol fragments and templ components.

## Import Rules
- **May import `internal/domain`, `internal/ports`, and `internal/app`.**
- Must **never** import `internal/infra` (keeps presentation decoupled from physical databases or cloud APIs).

## Subpackages & Entry Points
- `routes.go`: Top-level HTTP routing entry points:
  - `New(cfg ServerConfig) *fiber.App`: Constructs and returns the configured Fiber app.
  - `RegisterRoutes(app *fiber.App, cfg ServerConfig)`: Mounts public routes, auth group, protected routes, metrics, and static asset filesystem.
- `handlers/`: Feature-sliced HTTP delivery controllers:
  - `access/`: Admission (whitelist, ops, passwords) endpoints.
  - `instances/`: Instance list, details, and controls.
  - `console/`: Server console, RCON command execution, and live SSE log streams.
  - `backups/`: Manual backup triggers and downloads.
  - `content/`: Mod search and pack export.
  - `dashboard/`: Public status dashboard.
  - `wizard/`: New instance creation wizard.
- `pages/`: Templ HTML view components.
- `shared/`: HTTP rendering helpers, flash messages, and SSE toast formatters.
- `sse/`: Datastar wire protocol frame helpers.
- `metrics/`: Prometheus HTTP and SSE collectors.
