# Architecture Verification (`internal/arch`)

This package contains the automated architectural ratchet test for agrelha.

## Architectural Role
- **Ratchet Enforcement**: Validates that all packages in `internal/` obey layer dependency rules.
- **Failures Trigger When**:
  1. A forbidden cross-layer edge is introduced.
  2. A known exception is resolved but remains in the exception ledger ("stale exception").
  3. A new package is added that has not been classified into a known layer.
  4. An unclassified prefix is defined.
- **Run Command**: `go test -v ./internal/arch/...`
