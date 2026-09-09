# Platform Layer (`internal/platform`)

The platform layer provides cross-cutting environment and runtime support required to bootstrap the application.

## Architectural Role
- **Bootstrap Configuration**: Defines the complete application configuration struct, defaults, and environment variable loaders.
- **Cross-Cutting**: Used by `cmd/agrelha` and `internal/wiring` to parameterize the system at startup.

## Import Rules
- Contains no domain logic and no HTTP delivery code.
- Adapters and application services should not depend on `platform/config` directly; they should accept narrow options or interfaces (Phase I cleanup).

## Subpackages
- `config/`: Environment variable parsing, default values, and validation.
