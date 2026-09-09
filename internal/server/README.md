# Transitional Server Package (`internal/server`)

> ⚠️ **TRANSITIONAL PACKAGE — SLATED FOR COMPLETE DISSOLUTION**

This package is the remaining legacy core from the pre-modularization monolith. It is being incrementally emptied and will be deleted once all handlers and routing are migrated.

## Architectural Role
- **Status**: Temporary holder of the Fiber HTTP router and legacy composite handlers.
- **Goal**: Dissolve entirely into `internal/web` (for routing and presentation) and `internal/wiring` (for composition).

## Migration Destination
- **Route Definitions (`routes.go`)**: Moving to `internal/web/routes.go`.
- **Daunting Handlers**:
  - `handlers_mc_wizard.go` → Orchestration to `internal/app/wizard`, delivery to `internal/web/handlers/wizard`.
  - `handlers_mc_backups.go` → Delivery to `internal/web/handlers/backups`.
  - `handlers_dashboard.go` → Delivery to `internal/web/handlers/dashboard`.
  - `handlers_mods.go` → Delivery to `internal/web/handlers/content`.
- **Lifecycle & Sync (`apply.go`, `mc_scheduler.go`)**: Already unified into `ports.Runtime` and `app/backups`.
- **Server Constructor (`server.go`)**: Moving to `internal/wiring`.
